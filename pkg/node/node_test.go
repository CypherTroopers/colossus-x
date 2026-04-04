package node

import (
	"fmt"
	"strings"
	"testing"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/consensus"
	"colossusx/pkg/types"
)

func TestNodeUsesSelectedMiningConfiguration(t *testing.T) {
	spec := cx.ColossusXSpecWithGrowth(1024*1024, cx.DefaultDAGGrowthBytesPerEpoch)
	target, err := cx.ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	chainCfg := types.ChainConfig{NetworkID: "test", Spec: spec}
	genesis := types.GenesisConfig{ChainID: "test", Message: "test", Timestamp: time.Now().Unix() - 1, Bits: target, Spec: spec}
	validator, err := consensus.NewValidator(chainCfg, consensus.CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer validator.Close()
	var logs []string
	_, err = New(Config{
		Chain:              chainCfg,
		Genesis:            genesis,
		Mine:               false,
		MaxNonces:          16,
		MinerBackend:       "unified",
		MinerDAGAlloc:      "go-heap",
		ResolvedDAGAlloc:   "go-heap",
		RuntimeInitStatus:  "not-required",
		MinerExecutionPath: "unified-memory-compatible backend (dag-allocation=go-heap)",
		Logf: func(format string, args ...any) {
			logs = append(logs, format)
		},
	}, validator, chain.NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "node mining configured backend=%s") {
		t.Fatalf("expected mining configuration log, got %#v", logs)
	}
}

func TestNodeCollectBlocksAndApplySyncBlocks(t *testing.T) {
	spec := cx.ColossusXSpecWithGrowth(1024*1024, cx.DefaultDAGGrowthBytesPerEpoch)
	target, err := cx.ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	chainCfg := types.ChainConfig{NetworkID: "sync-test", Spec: spec}
	genesis := types.GenesisConfig{ChainID: "sync-test", Message: "sync", Timestamp: time.Now().Unix() - 1, Bits: target, Spec: spec}

	remoteValidator, err := consensus.NewValidator(chainCfg, consensus.CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer remoteValidator.Close()
	remoteStore := chain.NewMemoryStore()
	remoteNode, err := New(Config{Chain: chainCfg, Genesis: genesis, Mine: false, MaxNonces: 32}, remoteValidator, remoteStore)
	if err != nil {
		t.Fatalf("remote node: %v", err)
	}
	if _, err := remoteNode.InitGenesis(); err != nil {
		t.Fatalf("remote InitGenesis: %v", err)
	}
	for i := 0; i < 2; i++ {
		block, _, err := remoteNode.mineNextBlock()
		if err != nil {
			t.Fatalf("remote mineNextBlock(%d): %v", i, err)
		}
		if _, _, err := remoteValidator.InsertBlock(remoteStore, block); err != nil {
			t.Fatalf("remote InsertBlock(%d): %v", i, err)
		}
	}

	collected := remoteNode.collectBlocks(1, 4)
	if len(collected) != 2 {
		t.Fatalf("collectBlocks len=%d want=2", len(collected))
	}
	if collected[0].Header.Height != 1 || collected[1].Header.Height != 2 {
		t.Fatalf("collectBlocks heights=%d,%d", collected[0].Header.Height, collected[1].Header.Height)
	}

	localValidator, err := consensus.NewValidator(chainCfg, consensus.CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer localValidator.Close()
	localStore := chain.NewMemoryStore()
	localNode, err := New(Config{
		Chain:     chainCfg,
		Genesis:   genesis,
		Mine:      false,
		MaxNonces: 32,
		Logf:      func(string, ...any) {},
	}, localValidator, localStore)
	if err != nil {
		t.Fatalf("local node: %v", err)
	}
	if _, err := localNode.InitGenesis(); err != nil {
		t.Fatalf("local InitGenesis: %v", err)
	}

	applied := localNode.applySyncBlocks("peer-1", collected)
	if applied != 2 {
		t.Fatalf("applySyncBlocks applied=%d want=2", applied)
	}
	tip, _, err := localStore.CurrentTip()
	if err != nil {
		t.Fatalf("local CurrentTip: %v", err)
	}
	if tip.Header.Height != 2 {
		t.Fatalf("local tip height=%d want=2", tip.Header.Height)
	}

	// duplicate batch should be ignored without modifying chain state.
	applied = localNode.applySyncBlocks("peer-1", collected)
	if applied != 0 {
		t.Fatalf("applySyncBlocks duplicate applied=%d want=0", applied)
	}

	got := localNode.collectBlocks(1, 2)
	if len(got) != 2 {
		t.Fatalf("local collectBlocks len=%d want=2", len(got))
	}
	if got[0].Header.Height != 1 || got[1].Header.Height != 2 {
		t.Fatalf("local collected heights mismatch: %v", []uint64{got[0].Header.Height, got[1].Header.Height})
	}
}

func TestParseBootnodes(t *testing.T) {
	got := ParseBootnodes(" 127.0.0.1:30333, ,127.0.0.1:30334 ")
	want := []string{"127.0.0.1:30333", "127.0.0.1:30334"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ParseBootnodes=%v want=%v", got, want)
	}
}
