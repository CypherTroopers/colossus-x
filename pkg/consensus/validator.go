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
	"sync/atomic"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/types"
	"github.com/zeebo/blake3"
)

var (
	ErrInvalidParent    = errors.New("invalid parent linkage")
	ErrInvalidTimestamp = errors.New("invalid timestamp")
	ErrInvalidTarget    = errors.New("invalid target")
	ErrInvalidPoW       = errors.New("invalid proof of work")
	ErrInvalidEpoch     = errors.New("invalid epoch parameters")
	ErrInvalidState     = errors.New("invalid block state")
)

type Validator struct {
	config                 types.ChainConfig
	backend                cx.HashBackend
	workers                int
	lightValidation        bool
	now                    func() time.Time
	mu                     sync.Mutex
	sharedDAGs             map[string]*cx.DAG
	fallbackValidationDAGs map[string]*cx.DAG
	allocator              cx.Allocator
	miningBackend          cx.HashBackend
	miningAllocator        cx.Allocator
	colossusxMerkleRoots   map[string][32]byte
}

type dagKey struct{ seed string; size uint64 }

type sliceAllocation struct{ buf []byte }
func (a *sliceAllocation) Bytes() []byte { return a.buf }
func (a *sliceAllocation) Free() error { a.buf = nil; return nil }
func (a *sliceAllocation) Name() string { return "go-slice" }

type validationReusableAllocator interface{ ValidationCanReuseDAG() bool }
type sliceAllocator struct{}
func (sliceAllocator) Alloc(size uint64) (cx.Allocation, error) { return &sliceAllocation{buf: make([]byte, size)}, nil }
func (sliceAllocator) Name() string { return "go-slice" }
func (sliceAllocator) ValidationCanReuseDAG() bool { return true }

type CPUBackend struct{}
func (CPUBackend) Mode() cx.BackendMode { return cx.BackendCPU }
func (CPUBackend) Description() string { return "consensus cpu backend" }
func (CPUBackend) Prepare(*cx.DAG) error { return nil }
func (CPUBackend) Hash(header []byte, nonce cx.Nonce, dag *cx.DAG) cx.HashResult {
	if dag.Spec().AlgorithmVersion >= 2 { return cx.ColossusXHash(dag.Spec(), header, nonce, dag) }
	return cx.LatticeHash(dag.Spec(), header, nonce, dag, nil)
}

func NewValidator(cfg types.ChainConfig, backend cx.HashBackend, workers int) (*Validator, error) {
	if err := cfg.Spec.Validate(); err != nil { return nil, err }
	cfg.Economics = cfg.Economics.Normalized()
	if workers <= 0 { workers = runtime.NumCPU() }
	if backend == nil { backend = CPUBackend{} }
	return &Validator{
		config: cfg, backend: backend, workers: workers, now: time.Now,
		sharedDAGs: make(map[string]*cx.DAG), fallbackValidationDAGs: make(map[string]*cx.DAG),
		allocator: sliceAllocator{}, miningBackend: backend, miningAllocator: sliceAllocator{},
		colossusxMerkleRoots: make(map[string][32]byte),
	}, nil
}

func (v *Validator) SetMiningBackend(backend cx.HashBackend, allocator cx.Allocator) {
	v.mu.Lock(); defer v.mu.Unlock()
	if backend != nil { v.miningBackend = backend }
	if allocator != nil { v.miningAllocator = allocator }
	for k, dag := range v.sharedDAGs { _ = dag.Close(); delete(v.sharedDAGs, k) }
	for k := range v.colossusxMerkleRoots { delete(v.colossusxMerkleRoots, k) }
}
func (v *Validator) MiningBackend() cx.HashBackend { v.mu.Lock(); defer v.mu.Unlock(); if v.miningBackend == nil { return v.backend }; return v.miningBackend }
func (v *Validator) MiningAllocatorName() string { v.mu.Lock(); defer v.mu.Unlock(); if v.miningAllocator == nil { return "" }; return v.miningAllocator.Name() }
func (v *Validator) SetLightValidation(enabled bool) { v.mu.Lock(); defer v.mu.Unlock(); v.lightValidation = enabled }
func (v *Validator) LightValidationEnabled() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.lightValidation }
func (v *Validator) SharedCacheSize() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.sharedDAGs) }
func (v *Validator) ValidationCacheSize() int { v.mu.Lock(); defer v.mu.Unlock(); return len(v.fallbackValidationDAGs) }
func (v *Validator) DAGReuseEnabled() bool { return v.canValidationReuseMiningDAG() }

func (v *Validator) ValidateHeader(store chain.Store, header types.BlockHeader) error { return v.validateHeader(store, header, true) }
func (v *Validator) ValidateBlock(store chain.Store, block types.Block) error {
	if err := v.validateHeader(store, block.Header, false); err != nil { return err }
	if err := v.validateBlockSemantics(store, block); err != nil { return err }
	return v.validatePoW(block)
}

func (v *Validator) validateHeader(store chain.Store, header types.BlockHeader, verifyDAGMerkle bool) error {
	if header.AlgorithmVersion != v.config.Spec.AlgorithmVersion { return fmt.Errorf("%w: algorithm version mismatch", ErrInvalidEpoch) }
	if err := v.validateEpochParameters(header); err != nil { return err }
	if header.Target == (cx.Target{}) { return ErrInvalidTarget }
	if header.Height == 0 {
		if header.ParentHash != (types.Hash{}) { return fmt.Errorf("%w: genesis parent must be zero", ErrInvalidParent) }
	} else {
		parent, err := store.GetBlock(header.ParentHash)
		if err != nil { return fmt.Errorf("%w: %v", ErrInvalidParent, err) }
		if parent.Header.Height+1 != header.Height { return fmt.Errorf("%w: expected height %d got %d", ErrInvalidParent, parent.Header.Height+1, header.Height) }
		if header.Timestamp <= parent.Header.Timestamp { return fmt.Errorf("%w: child timestamp %d <= parent %d", ErrInvalidTimestamp, header.Timestamp, parent.Header.Timestamp) }
		expected, err := ExpectedTargetForChild(store, v.config, parent)
		if err != nil { return err }
		if header.Target != expected { return fmt.Errorf("%w: expected=%s got=%s", ErrInvalidTarget, expected.String(), header.Target.String()) }
	}
	if header.Timestamp > v.now().Unix()+2*60*60 { return fmt.Errorf("%w: timestamp too far in future", ErrInvalidTimestamp) }
	if header.AlgorithmVersion >= 2 || v.config.Spec.Mode == cx.ModeColossusX {
		if header.DAGMerkleRoot == (types.Hash{}) { return fmt.Errorf("%w: colossusx header missing dag merkle root", ErrInvalidPoW) }
		if verifyDAGMerkle && !v.LightValidationEnabled() {
			if err := v.validateDAGMerkleRoot(header); err != nil { return err }
		}
		return nil
	}
	return v.validatePoW(types.Block{Header: header})
}

func (v *Validator) validateBlockSemantics(store chain.Store, block types.Block) error {
	if block.Header.Height == 0 {
		if block.Header.TxRoot != types.ComputeTxRoot(block.Transactions) { return fmt.Errorf("%w: genesis tx root mismatch", ErrInvalidState) }
		if block.Header.StateRoot != types.ComputeStateRoot(block.State) { return fmt.Errorf("%w: genesis state root mismatch", ErrInvalidState) }
		return nil
	}
	if block.State == nil { return fmt.Errorf("%w: block state is required", ErrInvalidState) }
	parent, err := store.GetBlock(block.Header.ParentHash)
	if err != nil { return fmt.Errorf("%w: parent state unavailable: %v", ErrInvalidState, err) }
	computed, err := types.ApplyTransactions(parent.State, block.Transactions, block.Header.Coinbase, v.config.Economics.Normalized().BlockReward)
	if err != nil { return fmt.Errorf("%w: %v", ErrInvalidState, err) }
	if block.Header.TxRoot != types.ComputeTxRoot(block.Transactions) { return fmt.Errorf("%w: tx root mismatch", ErrInvalidState) }
	expectedState := types.ComputeStateRoot(computed)
	if block.Header.StateRoot != expectedState { return fmt.Errorf("%w: state root mismatch", ErrInvalidState) }
	if types.ComputeStateRoot(block.State) != expectedState { return fmt.Errorf("%w: embedded state payload mismatch", ErrInvalidState) }
	return nil
}

func (v *Validator) validateEpochParameters(header types.BlockHeader) error {
	curSize := v.config.Spec.DAGSizeForHeight(header.Height)
	curSeed := types.EpochSeedForHeight(v.config.Spec, header.Height)
	if header.DAGSizeBytes == curSize && header.EpochSeed == curSeed { return nil }
	if v.config.Spec.IsAppendOnlyScratchpad() {
		return fmt.Errorf("%w: append-only scratchpad size/seed mismatch", ErrInvalidEpoch)
	}
	epochBlocks := v.config.Spec.EpochBlocks
	if epochBlocks == 0 { return fmt.Errorf("%w: invalid epoch config", ErrInvalidEpoch) }
	if header.Height < epochBlocks { return fmt.Errorf("%w: dag size/seed mismatch", ErrInvalidEpoch) }
	if header.Height%epochBlocks >= cx.ColossusXEpochGraceBlocks { return fmt.Errorf("%w: dag size/seed mismatch outside grace window", ErrInvalidEpoch) }
	prevHeight := header.Height - epochBlocks
	if header.DAGSizeBytes == v.config.Spec.DAGSizeForHeight(prevHeight) && header.EpochSeed == types.EpochSeedForHeight(v.config.Spec, prevHeight) { return nil }
	return fmt.Errorf("%w: epoch seed/dag size mismatch", ErrInvalidEpoch)
}

func CalcBlockWork(target cx.Target) *big.Int {
	max := new(big.Int).Lsh(big.NewInt(1), 256)
	targetInt := new(big.Int).SetBytes(target[:])
	if targetInt.Sign() == 0 { return big.NewInt(0) }
	return max.Div(max, targetInt.Add(targetInt, big.NewInt(1)))
}
func SelectBestChainByTotalWork(currentHash types.Hash, currentWork *big.Int, candidateHash types.Hash, candidateWork *big.Int) types.Hash {
	cmp := candidateWork.Cmp(currentWork)
	if cmp > 0 { return candidateHash }
	if cmp < 0 { return currentHash }
	if candidateHash.String() < currentHash.String() { return candidateHash }
	return currentHash
}
func ParseWorkString(s string) *big.Int { s = strings.TrimSpace(s); if s == "" { return big.NewInt(0) }; if out, ok := new(big.Int).SetString(s, 10); ok { return out }; return big.NewInt(0) }

func ExpectedTargetForChild(store chain.Store, cfg types.ChainConfig, parent types.Block) (cx.Target, error) {
	econ := cfg.Economics.Normalized(); childHeight := parent.Header.Height + 1
	if econ.RetargetInterval == 0 || childHeight%econ.RetargetInterval != 0 { return parent.Header.Target, nil }
	cursor := parent
	for i := uint64(1); i < econ.RetargetInterval && cursor.Header.Height > 0; i++ {
		next, err := store.GetBlock(cursor.Header.ParentHash); if err != nil { return parent.Header.Target, nil }; cursor = next
	}
	expectedSpan := int64(econ.RetargetInterval) * int64(econ.TargetBlockTimeMillis) / int64(time.Second/time.Millisecond)
	if expectedSpan <= 0 { expectedSpan = 1 }
	actualSpan := parent.Header.Timestamp - cursor.Header.Timestamp
	if actualSpan < expectedSpan/4 { actualSpan = expectedSpan / 4 }
	if actualSpan > expectedSpan*4 { actualSpan = expectedSpan * 4 }
	next := new(big.Int).Mul(new(big.Int).SetBytes(parent.Header.Target[:]), big.NewInt(actualSpan))
	next.Div(next, big.NewInt(expectedSpan))
	if next.Sign() <= 0 { next = big.NewInt(1) }
	maxTarget := econ.MaxTarget; if maxTarget == (cx.Target{}) { maxTarget = parent.Header.Target }
	maxBig := new(big.Int).SetBytes(maxTarget[:]); if next.Cmp(maxBig) > 0 { next = maxBig }
	var out cx.Target; b := next.Bytes(); if len(b) > 32 { b = b[len(b)-32:] }; copy(out[32-len(b):], b); return out, nil
}

func (v *Validator) InsertBlock(store chain.Store, block types.Block) (*big.Int, bool, error) {
	if err := v.ValidateBlock(store, block); err != nil { return nil, false, err }
	h := block.BlockHash(); if store.HasBlock(h) { work, err := store.TotalWork(h); return work, false, err }
	totalWork := CalcBlockWork(block.Header.Target)
	if block.Header.Height > 0 {
		pw, err := store.TotalWork(block.Header.ParentHash); if err != nil { return nil, false, err }
		totalWork = new(big.Int).Add(totalWork, pw)
	}
	if err := store.StoreBlock(block, totalWork); err != nil { return nil, false, err }
	current, currentWork, err := store.CurrentTip()
	if err != nil { if err := store.SetCurrentTip(h); err != nil { return nil, false, err }; return totalWork, true, nil }
	if SelectBestChainByTotalWork(current.BlockHash(), currentWork, h, totalWork) == h {
		if err := store.SetCurrentTip(h); err != nil { return nil, false, err }
		return totalWork, true, nil
	}
	return totalWork, false, nil
}

func (v *Validator) SealBlock(block types.Block, maxNonces uint64) (types.Block, cx.MineResult, error) {
	backend := v.MiningBackend()
	dag, err := v.sharedMiningDAGForHeader(block.Header)
	if err != nil { return types.Block{}, cx.MineResult{}, err }
	if err := backend.Prepare(dag); err != nil { return types.Block{}, cx.MineResult{}, err }
	var merkleRoot [32]byte
	if block.Header.AlgorithmVersion >= 2 || v.config.Spec.Mode == cx.ModeColossusX {
		merkleRoot = v.merkleRootForDAG(block.Header, dag)
		block.Header.DAGMerkleRoot = types.Hash(merkleRoot)
	}
	m, err := cx.NewMiner(v.config.Spec, dag, v.workers, sealSkipPrepareBackend{backend})
	if err != nil { return types.Block{}, cx.MineResult{}, err }
	res, ok := m.Mine(block.Header.EncodeForMining(), block.Header.Target, cx.NewUint64Nonce(0), maxNonces)
	if !ok { return types.Block{}, cx.MineResult{}, fmt.Errorf("no solution found in %d nonces", maxNonces) }
	nonce, ok := res.Nonce.(cx.Uint64Nonce); if !ok { return types.Block{}, cx.MineResult{}, errors.New("unexpected nonce type") }
	block.Header.Nonce = nonce.Uint64()
	if block.Header.AlgorithmVersion >= 2 || v.config.Spec.Mode == cx.ModeColossusX {
		solution, root, err := cx.BuildColossusXSolutionStreaming(dag.Spec(), block.Header.EncodeForMining(), nonce.Uint64(), dag)
		if err != nil { return types.Block{}, cx.MineResult{}, err }
		if root != merkleRoot { return types.Block{}, cx.MineResult{}, fmt.Errorf("dag merkle root mismatch between cached root and streaming proof root") }
		compact := cx.CompactColossusXSolution(solution); block.ColossusXSolutionCompact = &compact; block.ColossusXSolution = nil
	}
	return block, res, nil
}

func (v *Validator) PrewarmMiningDAGAtHeight(height uint64) error {
	spec := v.config.Spec.ResolvedForHeight(height)
	header := types.BlockHeader{Height: height, EpochSeed: types.EpochSeedForHeight(spec, height), DAGSizeBytes: spec.DAGSizeBytes}
	dag, err := v.sharedMiningDAGForHeader(header); if err != nil { return err }
	if spec.AlgorithmVersion >= 2 || spec.Mode == cx.ModeColossusX { _ = v.merkleRootForDAG(header, dag) }
	return nil
}

type sealSkipPrepareBackend struct{ cx.HashBackend }
func (b sealSkipPrepareBackend) Prepare(*cx.DAG) error { return nil }

func (v *Validator) Close() error {
	v.mu.Lock(); defer v.mu.Unlock(); seen := make(map[*cx.DAG]struct{})
	for k, dag := range v.fallbackValidationDAGs { if _, ok := seen[dag]; !ok { _ = dag.Close(); seen[dag] = struct{}{} }; delete(v.fallbackValidationDAGs, k); delete(v.colossusxMerkleRoots, k) }
	for k, dag := range v.sharedDAGs { if _, ok := seen[dag]; !ok { _ = dag.Close(); seen[dag] = struct{}{} }; delete(v.sharedDAGs, k); delete(v.colossusxMerkleRoots, k) }
	return nil
}

func (v *Validator) validatePoW(block types.Block) error {
	h := block.Header
	if h.AlgorithmVersion >= 2 || v.config.Spec.Mode == cx.ModeColossusX {
		spec := v.config.Spec.ResolvedForHeight(h.Height); spec.DAGSizeBytes = h.DAGSizeBytes
		var solution cx.ColossusXSolution
		switch {
		case block.ColossusXSolution != nil: solution = *block.ColossusXSolution
		case block.ColossusXSolutionCompact != nil:
			expanded, err := cx.ExpandCompactColossusXSolution(*block.ColossusXSolutionCompact); if err != nil { return fmt.Errorf("%w: invalid compact colossusx solution: %v", ErrInvalidPoW, err) }
			solution = expanded
		default: return fmt.Errorf("%w: colossusx solution is required", ErrInvalidPoW)
		}
		root := [32]byte(h.DAGMerkleRoot)
		if !v.LightValidationEnabled() {
			dag, err := v.validationDAGForHeader(h); if err != nil { return err }
			root = v.merkleRootForDAG(h, dag)
			if err := v.validateDAGMerkleRootWithRoot(h, root); err != nil { return err }
		}
		if err := cx.VerifyColossusXSolution(spec, h.EncodeForMining(), h.Target, root, solution); err != nil { return fmt.Errorf("%w: colossusx solution verify failed: %v", ErrInvalidPoW, err) }
		return nil
	}
	dag, err := v.validationDAGForHeader(h); if err != nil { return err }
	hash := v.backend.Hash(h.EncodeForMining(), cx.NewUint64Nonce(h.Nonce), dag)
	if !cx.LessOrEqualBE(hash.Pow256, h.Target) { return fmt.Errorf("%w: pow=%s target=%s", ErrInvalidPoW, hex.EncodeToString(hash.Pow256[:]), h.Target.String()) }
	return nil
}

func (v *Validator) validateDAGMerkleRoot(header types.BlockHeader) error {
	dag, err := v.validationDAGForHeader(header); if err != nil { return err }
	return v.validateDAGMerkleRootWithRoot(header, v.merkleRootForDAG(header, dag))
}
func (v *Validator) validateDAGMerkleRootWithRoot(header types.BlockHeader, root [32]byte) error {
	if root != [32]byte(header.DAGMerkleRoot) { return fmt.Errorf("%w: dag merkle root mismatch", ErrInvalidPoW) }
	return nil
}
func dagMerkleLeaves(dag *cx.DAG) [][32]byte { if dag == nil { return nil }; cells := make([][]byte, dag.NodeCount()); for i := uint64(0); i < dag.NodeCount(); i++ { cells[i] = dag.Node(i) }; return cx.BuildMerkleLeaves(cells) }
func hashMerklePair(left, right [32]byte) [32]byte { var in [64]byte; copy(in[:32], left[:]); copy(in[32:], right[:]); return blake3.Sum256(in[:]) }
func dagMerkleRootStreaming(dag *cx.DAG) [32]byte {
	if dag == nil || dag.NodeCount() == 0 { return [32]byte{} }
	frontier := make([][32]byte, 0, 64); present := make([]bool, 0, 64)
	for i := uint64(0); i < dag.NodeCount(); i++ {
		h := blake3.Sum256(dag.Node(i)); level := 0
		for {
			if level >= len(frontier) { frontier = append(frontier, [32]byte{}); present = append(present, false) }
			if !present[level] { frontier[level] = h; present[level] = true; break }
			h = hashMerklePair(frontier[level], h); present[level] = false; level++
		}
	}
	var acc [32]byte; accLevel := 0; hasAcc := false
	for level := 0; level < len(frontier); level++ {
		if !present[level] { continue }
		node := frontier[level]
		if !hasAcc { acc = node; accLevel = level; hasAcc = true; continue }
		for accLevel < level { acc = hashMerklePair(acc, acc); accLevel++ }
		acc = hashMerklePair(node, acc); accLevel = level + 1
	}
	return acc
}
func (v *Validator) merkleRootForDAG(header types.BlockHeader, dag *cx.DAG) [32]byte {
	key := v.sharedDAGCacheKey(header)
	v.mu.Lock(); if root, ok := v.colossusxMerkleRoots[key]; ok { v.mu.Unlock(); return root }; v.mu.Unlock()
	root := dagMerkleRootStreaming(dag); v.cacheMerkleRoot(key, root); return root
}
func (v *Validator) cacheMerkleRoot(key string, root [32]byte) { v.mu.Lock(); defer v.mu.Unlock(); v.colossusxMerkleRoots[key] = root }
func (v *Validator) validationDAGForHeader(header types.BlockHeader) (*cx.DAG, error) {
	alloc := v.miningAllocatorOrDefault()
	if v.canValidationReuseMiningDAG() { log.Printf("validator DAG reuse enabled allocator=%s shared=true", allocatorName(alloc)); return v.sharedMiningDAGForHeader(header) }
	log.Printf("validator DAG reuse fallback allocator=%s shared=false", allocatorName(alloc))
	alloc = v.validationAllocator(); return v.cachedDAGForHeader(header, alloc, v.fallbackValidationDAGs, v.fallbackValidationDAGCacheKey(header, alloc))
}
func (v *Validator) sharedMiningDAGForHeader(header types.BlockHeader) (*cx.DAG, error) {
	alloc := v.miningAllocatorOrDefault(); return v.cachedDAGForHeader(header, alloc, v.sharedDAGs, v.sharedDAGCacheKey(header))
}
func (v *Validator) miningAllocatorOrDefault() cx.Allocator { v.mu.Lock(); defer v.mu.Unlock(); if v.miningAllocator != nil { return v.miningAllocator }; return v.allocator }
func (v *Validator) validationAllocator() cx.Allocator { v.mu.Lock(); defer v.mu.Unlock(); return v.allocator }
func (v *Validator) canValidationReuseMiningDAG() bool {
	alloc := v.miningAllocatorOrDefault(); if alloc == nil { return false }
	if reusable, ok := alloc.(validationReusableAllocator); ok { return reusable.ValidationCanReuseDAG() }
	name := strings.ToLower(strings.TrimSpace(alloc.Name()))
	switch { case name == "", name == "go", name == "go-slice", name == "go-heap", name == "auto", name == "pinned", name == "pinned-host": return true; case strings.Contains(name, "unified"): return true; default: return false }
}
func (v *Validator) sharedDAGCacheKey(header types.BlockHeader) string { return fmt.Sprintf("%s/%d", header.EpochSeed.String(), header.DAGSizeBytes) }
func (v *Validator) fallbackValidationDAGCacheKey(header types.BlockHeader, alloc cx.Allocator) string { return fmt.Sprintf("%s/%s/validation", v.sharedDAGCacheKey(header), allocatorName(alloc)) }
func allocatorName(alloc cx.Allocator) string { if alloc == nil { return "" }; return alloc.Name() }
func (v *Validator) evictAppendOnlyScratchpadEntries(cache map[string]*cx.DAG, seedPrefix string, keepKey string, keepSize uint64) {
	for k, dag := range cache {
		if k == keepKey || dag == nil {
			continue
		}
		if !strings.HasPrefix(k, seedPrefix+"/") {
			continue
		}
		if dag.Spec().DAGSizeBytes >= keepSize {
			continue
		}
		_ = dag.Close()
		delete(cache, k)
		delete(v.colossusxMerkleRoots, k)
	}
}
func (v *Validator) cachedDAGForHeader(header types.BlockHeader, alloc cx.Allocator, cache map[string]*cx.DAG, key string) (*cx.DAG, error) {
	v.mu.Lock(); defer v.mu.Unlock(); if dag, ok := cache[key]; ok { return dag, nil }
	spec := v.config.Spec.ResolvedForHeight(header.Height); spec.DAGSizeBytes = header.DAGSizeBytes
	dag, err := cx.NewDAGWithAllocator(spec, alloc); if err != nil { return nil, err }
	if err := populateDAGWithLogging(dag, header.EpochSeed[:], v.workers); err != nil { _ = dag.Close(); return nil, err }
	cache[key] = dag; if spec.AlgorithmVersion >= 2 || spec.Mode == cx.ModeColossusX { v.colossusxMerkleRoots[key] = dagMerkleRootStreaming(dag) }
	if spec.IsAppendOnlyScratchpad() {
		v.evictAppendOnlyScratchpadEntries(cache, header.EpochSeed.String(), key, spec.DAGSizeBytes)
	}
	return dag, nil
}
func populateDAGWithLogging(dag *cx.DAG, epochSeed []byte, workers int) error {
	if dag == nil { return fmt.Errorf("dag cannot be nil") }
	label := "dag generation"
	unit := "nodes"
	if dag.Spec().IsAppendOnlyScratchpad() {
		label = "scratchpad build"
		unit = "cells"
	}
	total := dag.NodeCount()
	start := time.Now()
	log.Printf("%s started %s=%d workers=%d", label, unit, total, workers)
	var finalDone atomic.Uint64
	err := cx.PopulateDAGWithProgress(dag, epochSeed, workers, func(done, total uint64) {
		finalDone.Store(done)
		if total == 0 { return }
		log.Printf("%s progress: %.1f%% (%d/%d %s) elapsed=%s", label, float64(done)*100/float64(total), done, total, unit, time.Since(start).Round(time.Second))
	})
	if err != nil { return err }
	log.Printf("%s completed in %s (%d/%d %s)", label, time.Since(start).Round(time.Second), finalDone.Load(), total, unit)
	return nil
}
