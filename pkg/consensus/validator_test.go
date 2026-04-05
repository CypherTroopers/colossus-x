package consensus

import (
	"sync/atomic"
	"testing"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/types"
)

func testConfig(t *testing.T) (types.ChainConfig, types.GenesisConfig) {
	t.Helper()
	spec := cx.ColossusXSpecWithGrowth(1024*1024, 256*1024)
	target, err := cx.ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	econ := types.EconomicConfig{BlockReward: 50, TargetBlockTimeMillis: 1000, RetargetInterval: 4, MaxTarget: target}.Normalized()
	chainCfg := types.ChainConfig{NetworkID: "test", Spec: spec, Economics: econ}
	genesis := types.GenesisConfig{ChainID: "test", Message: "test", Timestamp: time.Now().Unix() - 1, Bits: target, Spec: spec, Economics: econ, Alloc: map[string]uint64{"alice": 100}}
	return chainCfg, genesis
}

type namedAlloc struct {
	name      string
	freeCount *int32
	buf       []byte
}

func (a *namedAlloc) Bytes() []byte { return a.buf }
func (a *namedAlloc) Name() string  { return a.name }
func (a *namedAlloc) Free() error {
	if a.freeCount != nil {
		atomic.AddInt32(a.freeCount, 1)
	}
	a.buf = nil
	return nil
}

type namedAllocator struct {
	name      string
	freeCount *int32
}

func (a namedAllocator) Alloc(size uint64) (cx.Allocation, error) {
	return &namedAlloc{name: a.name, freeCount: a.freeCount, buf: make([]byte, size)}, nil
}
func (a namedAllocator) Name() string { return a.name }

type capabilityAllocator struct {
	namedAllocator
	reuse bool
}

func (a capabilityAllocator) ValidationCanReuseDAG() bool { return a.reuse }

func testBlockHeader(chainCfg types.ChainConfig, genesis types.Block) types.BlockHeader {
	return types.BlockHeader{
		Version:          1,
		AlgorithmVersion: chainCfg.Spec.AlgorithmVersion,
		Height:           1,
		ParentHash:       genesis.BlockHash(),
		Timestamp:        genesis.Header.Timestamp + 1,
		Target:           genesis.Header.Target,
		Coinbase:         "miner-1",
		EpochSeed:        types.EpochSeedForHeight(chainCfg.Spec, 1),
		DAGSizeBytes:     chainCfg.Spec.DAGSizeForHeight(1),
		TxRoot:           types.ComputeTxRoot(nil),
	}
}

func TestValidatorInsertBlock(t *testing.T) {
	chainCfg, genesisCfg := testConfig(t)
	v, err := NewValidator(chainCfg, CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	store := chain.NewMemoryStore()
	genesis, _, err := v.SealBlock(types.NewGenesisBlock(genesisCfg), 10)
	if err != nil {
		t.Fatal(err)
	}
	work, becameTip, err := v.InsertBlock(store, genesis)
	if err != nil {
		t.Fatal(err)
	}
	if !becameTip || work.Sign() <= 0 {
		t.Fatalf("expected genesis to become tip")
	}

	state, err := types.ApplyTransactions(genesis.State, nil, "miner-1", chainCfg.Economics.BlockReward)
	if err != nil {
		t.Fatal(err)
	}
	next := types.Block{Header: testBlockHeader(chainCfg, genesis), State: state}
	next.Header.StateRoot = types.ComputeStateRoot(state)
	sealed, _, err := v.SealBlock(next, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, becameTip, err := v.InsertBlock(store, sealed); err != nil {
		t.Fatal(err)
	} else if !becameTip {
		t.Fatalf("expected child to become tip")
	}
}

func TestValidatorInsertBlock_PreservesCompetingFork(t *testing.T) {
	chainCfg, genesisCfg := testConfig(t)
	v, err := NewValidator(chainCfg, CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	store := chain.NewMemoryStore()
	genesis, _, err := v.SealBlock(types.NewGenesisBlock(genesisCfg), 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, becameTip, err := v.InsertBlock(store, genesis); err != nil || !becameTip {
		t.Fatalf("insert genesis err=%v becameTip=%v", err, becameTip)
	}

	mainState, _ := types.ApplyTransactions(genesis.State, nil, "miner-main", chainCfg.Economics.BlockReward)
	mainBlock := types.Block{Header: testBlockHeader(chainCfg, genesis), State: mainState}
	mainBlock.Header.Coinbase = "miner-main"
	mainBlock.Header.StateRoot = types.ComputeStateRoot(mainState)
	mainBlock, _, err = v.SealBlock(mainBlock, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, becameTip, err := v.InsertBlock(store, mainBlock); err != nil || !becameTip {
		t.Fatalf("insert main err=%v becameTip=%v", err, becameTip)
	}

	forkState, _ := types.ApplyTransactions(genesis.State, nil, "miner-fork", chainCfg.Economics.BlockReward)
	forkBlock := types.Block{Header: testBlockHeader(chainCfg, genesis), State: forkState}
	forkBlock.Header.Timestamp++
	forkBlock.Header.Coinbase = "miner-fork"
	forkBlock.Header.StateRoot = types.ComputeStateRoot(forkState)
	forkBlock, _, err = v.SealBlock(forkBlock, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, becameTip, err := v.InsertBlock(store, forkBlock); err != nil {
		t.Fatal(err)
	} else if becameTip {
		t.Fatalf("expected competing fork block to remain non-canonical")
	}
	if !store.HasBlock(forkBlock.BlockHash()) {
		t.Fatalf("expected non-tip block to be kept in store")
	}
}

func TestValidationAndMiningReuseSharedDAGForHostVisibleAllocator(t *testing.T) {
	chainCfg, genesisCfg := testConfig(t)
	v, err := NewValidator(chainCfg, CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	genesis := types.NewGenesisBlock(genesisCfg)
	header := testBlockHeader(chainCfg, genesis)

	miningDAG, err := v.sharedMiningDAGForHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	validationDAG, err := v.validationDAGForHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if miningDAG != validationDAG {
		t.Fatalf("expected validation to reuse mining DAG")
	}
}

func TestValidationFallsBackWhenAllocatorCapabilityDisallowsIt(t *testing.T) {
	chainCfg, genesisCfg := testConfig(t)
	v, err := NewValidator(chainCfg, CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	v.SetMiningBackend(CPUBackend{}, capabilityAllocator{namedAllocator: namedAllocator{name: "cuda-managed"}, reuse: false})
	genesis := types.NewGenesisBlock(genesisCfg)
	header := testBlockHeader(chainCfg, genesis)

	miningDAG, err := v.sharedMiningDAGForHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	validationDAG, err := v.validationDAGForHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if miningDAG == validationDAG {
		t.Fatalf("expected validation fallback DAG when allocator capability rejects reuse")
	}
}

func TestCloseDoesNotDoubleFreeSharedPointers(t *testing.T) {
	chainCfg, genesisCfg := testConfig(t)
	v, err := NewValidator(chainCfg, CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var frees int32
	v.allocator = namedAllocator{name: "go-heap", freeCount: &frees}
	v.miningAllocator = namedAllocator{name: "go-heap", freeCount: &frees}

	genesis := types.NewGenesisBlock(genesisCfg)
	header := testBlockHeader(chainCfg, genesis)
	shared, err := v.sharedMiningDAGForHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	v.fallbackValidationDAGs[v.fallbackValidationDAGCacheKey(header, v.validationAllocator())] = shared

	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&frees); got != 1 {
		t.Fatalf("expected shared DAG allocation to be freed once, got %d", got)
	}
}
