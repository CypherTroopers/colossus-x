package consensus

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/types"
)

var (
	ErrInvalidParent    = errors.New("invalid parent linkage")
	ErrInvalidTimestamp = errors.New("invalid timestamp")
	ErrInvalidTarget    = errors.New("invalid target")
	ErrInvalidPoW       = errors.New("invalid proof of work")
	ErrInvalidEpoch     = errors.New("invalid epoch parameters")
)

type Validator struct {
	config                 types.ChainConfig
	backend                cx.HashBackend
	workers                int
	now                    func() time.Time
	mu                     sync.Mutex
	sharedDAGs             map[string]*cx.DAG
	fallbackValidationDAGs map[string]*cx.DAG
	allocator              cx.Allocator
	miningBackend          cx.HashBackend
	miningAllocator        cx.Allocator
}

type dagKey struct {
	seed string
	size uint64
}

type sliceAllocation struct{ buf []byte }

func (a *sliceAllocation) Bytes() []byte { return a.buf }
func (a *sliceAllocation) Free() error   { a.buf = nil; return nil }
func (a *sliceAllocation) Name() string  { return "go-slice" }

type validationReusableAllocator interface {
	ValidationCanReuseDAG() bool
}

type sliceAllocator struct{}

func (sliceAllocator) Alloc(size uint64) (cx.Allocation, error) {
	return &sliceAllocation{buf: make([]byte, size)}, nil
}
func (sliceAllocator) Name() string                { return "go-slice" }
func (sliceAllocator) ValidationCanReuseDAG() bool { return true }

type CPUBackend struct{}

func (CPUBackend) Mode() cx.BackendMode  { return cx.BackendCPU }
func (CPUBackend) Description() string   { return "consensus cpu backend" }
func (CPUBackend) Prepare(*cx.DAG) error { return nil }
func (CPUBackend) Hash(header []byte, nonce cx.Nonce, dag *cx.DAG) cx.HashResult {
	if dag.Spec().AlgorithmVersion >= 2 {
		return cx.StrictV2Hash(dag.Spec(), header, nonce, dag)
	}
	return cx.LatticeHash(dag.Spec(), header, nonce, dag, nil)
}

func NewValidator(cfg types.ChainConfig, backend cx.HashBackend, workers int) (*Validator, error) {
	if err := cfg.Spec.Validate(); err != nil {
		return nil, err
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if backend == nil {
		backend = CPUBackend{}
	}
	return &Validator{
		config:                 cfg,
		backend:                backend,
		workers:                workers,
		now:                    time.Now,
		sharedDAGs:             make(map[string]*cx.DAG),
		fallbackValidationDAGs: make(map[string]*cx.DAG),
		allocator:              sliceAllocator{},
		miningBackend:          backend,
		miningAllocator:        sliceAllocator{},
	}, nil
}

func (v *Validator) SetMiningBackend(backend cx.HashBackend, allocator cx.Allocator) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if backend != nil {
		v.miningBackend = backend
	}
	if allocator != nil {
		v.miningAllocator = allocator
	}
	for key, dag := range v.sharedDAGs {
		_ = dag.Close()
		delete(v.sharedDAGs, key)
	}
}

func (v *Validator) MiningBackend() cx.HashBackend {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.miningBackend == nil {
		return v.backend
	}
	return v.miningBackend
}

func (v *Validator) MiningAllocatorName() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.miningAllocator == nil {
		return ""
	}
	return v.miningAllocator.Name()
}

func (v *Validator) ValidateHeader(store chain.Store, header types.BlockHeader) error {
	if header.AlgorithmVersion != v.config.Spec.AlgorithmVersion {
		return fmt.Errorf("%w: algorithm version mismatch", ErrInvalidEpoch)
	}
	expectedDAGSize := v.config.Spec.DAGSizeForHeight(header.Height)
	if header.DAGSizeBytes != expectedDAGSize {
		return fmt.Errorf("%w: dag size mismatch expected=%d got=%d", ErrInvalidEpoch, expectedDAGSize, header.DAGSizeBytes)
	}
	expectedSeed := types.EpochSeedForHeight(v.config.Spec, header.Height)
	if expectedSeed != header.EpochSeed {
		return fmt.Errorf("%w: epoch seed mismatch", ErrInvalidEpoch)
	}
	if header.Target == (cx.Target{}) {
		return ErrInvalidTarget
	}
	if header.Height == 0 {
		if header.ParentHash != (types.Hash{}) {
			return fmt.Errorf("%w: genesis parent must be zero", ErrInvalidParent)
		}
	} else {
		parent, err := store.GetHeader(header.ParentHash)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidParent, err)
		}
		if parent.Height+1 != header.Height {
			return fmt.Errorf("%w: expected height %d got %d", ErrInvalidParent, parent.Height+1, header.Height)
		}
		if header.Timestamp <= parent.Timestamp {
			return fmt.Errorf("%w: child timestamp %d <= parent %d", ErrInvalidTimestamp, header.Timestamp, parent.Timestamp)
		}
	}
	now := v.now().Unix() + 2*60*60
	if header.Timestamp > now {
		return fmt.Errorf("%w: timestamp %d too far ahead of %d", ErrInvalidTimestamp, header.Timestamp, now)
	}
	return v.validatePoW(header)
}

func (v *Validator) ValidateBlock(store chain.Store, block types.Block) error {
	return v.ValidateHeader(store, block.Header)
}

func CalcBlockWork(target cx.Target) *big.Int {
	max := new(big.Int).Lsh(big.NewInt(1), 256)
	targetInt := new(big.Int).SetBytes(target[:])
	if targetInt.Sign() == 0 {
		return big.NewInt(0)
	}
	return max.Div(max, targetInt.Add(targetInt, big.NewInt(1)))
}

func SelectBestChainByTotalWork(currentHash types.Hash, currentWork *big.Int, candidateHash types.Hash, candidateWork *big.Int) types.Hash {
	cmp := candidateWork.Cmp(currentWork)
	if cmp > 0 {
		return candidateHash
	}
	if cmp < 0 {
		return currentHash
	}
	if candidateHash.String() < currentHash.String() {
		return candidateHash
	}
	return currentHash
}

func (v *Validator) InsertBlock(store chain.Store, block types.Block) (*big.Int, bool, error) {
	if err := v.ValidateBlock(store, block); err != nil {
		return nil, false, err
	}
	blockHash := block.BlockHash()
	if store.HasBlock(blockHash) {
		work, err := store.TotalWork(blockHash)
		return work, false, err
	}
	blockWork := CalcBlockWork(block.Header.Target)
	totalWork := new(big.Int).Set(blockWork)
	if block.Header.Height > 0 {
		parentWork, err := store.TotalWork(block.Header.ParentHash)
		if err != nil {
			return nil, false, err
		}
		totalWork.Add(totalWork, parentWork)
	}
	if err := store.StoreBlock(block, totalWork); err != nil {
		return nil, false, err
	}
	current, currentWork, err := store.CurrentTip()
	if err != nil {
		if err := store.SetCurrentTip(blockHash); err != nil {
			return nil, false, err
		}
		return totalWork, true, nil
	}
	best := SelectBestChainByTotalWork(current.BlockHash(), currentWork, blockHash, totalWork)
	if best == blockHash {
		if err := store.SetCurrentTip(blockHash); err != nil {
			return nil, false, err
		}
		return totalWork, true, nil
	}
	return totalWork, false, nil
}

func (v *Validator) SealBlock(block types.Block, maxNonces uint64) (types.Block, cx.MineResult, error) {
	backend := v.MiningBackend()
	dag, err := v.sharedMiningDAGForHeader(block.Header)
	if err != nil {
		return types.Block{}, cx.MineResult{}, err
	}
	if err := backend.Prepare(dag); err != nil {
		return types.Block{}, cx.MineResult{}, err
	}
	miner, err := cx.NewMiner(v.config.Spec, dag, v.workers, sealSkipPrepareBackend{backend})
	if err != nil {
		return types.Block{}, cx.MineResult{}, err
	}
	res, ok := miner.Mine(block.Header.EncodeForMining(), block.Header.Target, cx.NewUint64Nonce(0), maxNonces)
	if !ok {
		return types.Block{}, cx.MineResult{}, fmt.Errorf("no solution found in %d nonces", maxNonces)
	}
	nonce, ok := res.Nonce.(cx.Uint64Nonce)
	if !ok {
		return types.Block{}, cx.MineResult{}, errors.New("unexpected nonce type")
	}
	block.Header.Nonce = nonce.Uint64()
	return block, res, nil
}

type sealSkipPrepareBackend struct{ cx.HashBackend }

func (b sealSkipPrepareBackend) Prepare(*cx.DAG) error { return nil }

func (v *Validator) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	seen := make(map[*cx.DAG]struct{}, len(v.sharedDAGs)+len(v.fallbackValidationDAGs))
	for key, dag := range v.fallbackValidationDAGs {
		if _, ok := seen[dag]; !ok {
			_ = dag.Close()
			seen[dag] = struct{}{}
		}
		delete(v.fallbackValidationDAGs, key)
	}
	for key, dag := range v.sharedDAGs {
		if _, ok := seen[dag]; !ok {
			_ = dag.Close()
			seen[dag] = struct{}{}
		}
		delete(v.sharedDAGs, key)
	}
	return nil
}

func (v *Validator) validatePoW(header types.BlockHeader) error {
	dag, err := v.validationDAGForHeader(header)
	if err != nil {
		return err
	}
	hash := v.backend.Hash(header.EncodeForMining(), cx.NewUint64Nonce(header.Nonce), dag)
	if !cx.LessOrEqualBE(hash.Pow256, header.Target) {
		return fmt.Errorf("%w: pow=%s target=%s", ErrInvalidPoW, hex.EncodeToString(hash.Pow256[:]), header.Target.String())
	}
	return nil
}

func (v *Validator) validationDAGForHeader(header types.BlockHeader) (*cx.DAG, error) {
	allocator := v.miningAllocatorOrDefault()
	if v.canValidationReuseMiningDAG() {
		log.Printf("validator DAG reuse enabled allocator=%s shared=true", allocatorName(allocator))
		return v.sharedMiningDAGForHeader(header)
	}
	log.Printf("validator DAG reuse fallback allocator=%s shared=false", allocatorName(allocator))
	allocator = v.validationAllocator()
	return v.cachedDAGForHeader(header, allocator, v.fallbackValidationDAGs, v.fallbackValidationDAGCacheKey(header, allocator))
}

func (v *Validator) sharedMiningDAGForHeader(header types.BlockHeader) (*cx.DAG, error) {
	allocator := v.miningAllocatorOrDefault()
	return v.cachedDAGForHeader(header, allocator, v.sharedDAGs, v.sharedDAGCacheKey(header))
}

func (v *Validator) SharedCacheSize() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.sharedDAGs)
}

func (v *Validator) ValidationCacheSize() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.fallbackValidationDAGs)
}

func (v *Validator) DAGReuseEnabled() bool {
	return v.canValidationReuseMiningDAG()
}

func (v *Validator) miningAllocatorOrDefault() cx.Allocator {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.miningAllocator != nil {
		return v.miningAllocator
	}
	return v.allocator
}

func (v *Validator) validationAllocator() cx.Allocator {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.allocator
}

func (v *Validator) canValidationReuseMiningDAG() bool {
	allocator := v.miningAllocatorOrDefault()
	if allocator == nil {
		return false
	}
	if reusable, ok := allocator.(validationReusableAllocator); ok {
		return reusable.ValidationCanReuseDAG()
	}
	name := strings.ToLower(strings.TrimSpace(allocator.Name()))
	switch {
	case name == "", name == "go", name == "go-slice", name == "go-heap", name == "auto", name == "pinned", name == "pinned-host":
		return true
	case strings.Contains(name, "unified"):
		return true
	default:
		return false
	}
}

func (v *Validator) sharedDAGCacheKey(header types.BlockHeader) string {
	key := dagKey{seed: header.EpochSeed.String(), size: header.DAGSizeBytes}
	return fmt.Sprintf("%s/%d", key.seed, key.size)
}

func (v *Validator) fallbackValidationDAGCacheKey(header types.BlockHeader, allocator cx.Allocator) string {
	return fmt.Sprintf("%s/%s/validation", v.sharedDAGCacheKey(header), allocatorName(allocator))
}

func allocatorName(allocator cx.Allocator) string {
	if allocator == nil {
		return ""
	}
	return allocator.Name()
}

func (v *Validator) cachedDAGForHeader(header types.BlockHeader, allocator cx.Allocator, cache map[string]*cx.DAG, key string) (*cx.DAG, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if dag, ok := cache[key]; ok {
		return dag, nil
	}
	spec := v.config.Spec.ResolvedForHeight(header.Height)
	spec.DAGSizeBytes = header.DAGSizeBytes
	dag, err := cx.NewDAGWithAllocator(spec, allocator)
	if err != nil {
		return nil, err
	}
	if err := cx.PopulateDAG(dag, header.EpochSeed[:], v.workers); err != nil {
		_ = dag.Close()
		return nil, err
	}
	cache[key] = dag
	return dag, nil
}
