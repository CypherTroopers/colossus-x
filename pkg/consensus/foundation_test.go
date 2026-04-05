package consensus

import (
	"testing"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/types"
)

func foundationChainConfig(t *testing.T) (types.ChainConfig, types.GenesisConfig) {
	t.Helper()
	spec := cx.ColossusXSpecWithGrowth(1024*1024, cx.DefaultDAGGrowthBytesPerEpoch)
	target, err := cx.ParseTargetHex("0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	econ := types.EconomicConfig{BlockReward: 50, TargetBlockTimeMillis: 1000, RetargetInterval: 2, MaxTarget: target}.Normalized()
	cfg := types.ChainConfig{NetworkID: "foundation", Spec: spec, Economics: econ}
	genesis := types.GenesisConfig{ChainID: "foundation", Message: "genesis", Timestamp: time.Now().Unix() - 5, Bits: target, Spec: spec, Economics: econ, Alloc: map[string]uint64{"alice": 100}}
	return cfg, genesis
}

func TestInsertBlockStoresSideBranch(t *testing.T) {
	cfg, genesis := foundationChainConfig(t)
	validator, err := NewValidator(cfg, CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer validator.Close()

	store := chain.NewMemoryStore()
	genesisBlock := types.NewGenesisBlock(genesis)
	work := CalcBlockWork(genesisBlock.Header.Target)
	if err := store.StoreBlock(genesisBlock, work); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentTip(genesisBlock.BlockHash()); err != nil {
		t.Fatal(err)
	}

	stateA, err := types.ApplyTransactions(genesisBlock.State, nil, "miner-a", cfg.Economics.BlockReward)
	if err != nil {
		t.Fatal(err)
	}
	blockA := types.Block{
		Header: types.BlockHeader{
			Version:          1,
			AlgorithmVersion: cfg.Spec.AlgorithmVersion,
			Height:           1,
			ParentHash:       genesisBlock.BlockHash(),
			Timestamp:        genesisBlock.Header.Timestamp + 1,
			Target:           genesisBlock.Header.Target,
			Coinbase:         "miner-a",
			EpochSeed:        types.EpochSeedForHeight(cfg.Spec, 1),
			DAGSizeBytes:     cfg.Spec.DAGSizeForHeight(1),
			TxRoot:           types.ComputeTxRoot(nil),
			StateRoot:        types.ComputeStateRoot(stateA),
		},
		State: stateA,
	}
	blockB := blockA
	blockB.Header.Timestamp++
	blockB.Header.Coinbase = "miner-b"
	stateB, err := types.ApplyTransactions(genesisBlock.State, nil, "miner-b", cfg.Economics.BlockReward)
	if err != nil {
		t.Fatal(err)
	}
	blockB.State = stateB
	blockB.Header.StateRoot = types.ComputeStateRoot(stateB)

	if _, becameTip, err := validator.InsertBlock(store, blockA); err != nil || !becameTip {
		t.Fatalf("insert blockA err=%v becameTip=%v", err, becameTip)
	}
	if _, becameTip, err := validator.InsertBlock(store, blockB); err != nil {
		t.Fatalf("insert blockB err=%v", err)
	} else if becameTip {
		t.Fatalf("expected tie-break to keep canonical tip")
	}
	if !store.HasBlock(blockA.BlockHash()) || !store.HasBlock(blockB.BlockHash()) {
		t.Fatal("expected both competing blocks to be stored")
	}
}
