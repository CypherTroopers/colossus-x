package node

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/consensus"
	"colossusx/pkg/p2p"
	"colossusx/pkg/types"
)

func testChainConfig(t *testing.T, network string) (types.ChainConfig, types.GenesisConfig) {
	t.Helper()
	spec := cx.ColossusXSpecWithGrowth(1024*1024, cx.DefaultDAGGrowthBytesPerEpoch)
	target, err := cx.ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	econ := types.EconomicConfig{BlockReward: 50, TargetBlockTimeMillis: 1000, RetargetInterval: 4, MaxTarget: target}.Normalized()
	chainCfg := types.ChainConfig{NetworkID: network, Spec: spec, Economics: econ}
	genesis := types.GenesisConfig{ChainID: network, Message: "genesis", Timestamp: time.Now().Unix() - 10, Bits: target, Spec: spec, Economics: econ, Alloc: map[string]uint64{"alice": 100}}
	return chainCfg, genesis
}

func TestNodeUsesSelectedMiningConfiguration(t *testing.T) {
	chainCfg, genesis := testChainConfig(t, "test")
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
	chainCfg, genesis := testChainConfig(t, "sync-test")

	remoteValidator, err := consensus.NewValidator(chainCfg, consensus.CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer remoteValidator.Close()
	remoteStore := chain.NewMemoryStore()
	remoteNode, err := New(Config{Chain: chainCfg, Genesis: genesis, Mine: false, MaxNonces: 32, Coinbase: "miner-remote"}, remoteValidator, remoteStore)
	if err != nil {
		t.Fatalf("remote node: %v", err)
	}
	if _, err := remoteNode.InitGenesis(); err != nil {
		t.Fatalf("remote InitGenesis: %v", err)
	}
	if err := remoteNode.SubmitTransaction(types.Transaction{From: "alice", To: "bob", Value: 10, Nonce: 0}); err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	for i := 0; i < 2; i++ {
		block, _, err := remoteNode.mineNextBlock()
		if err != nil {
			t.Fatalf("remote mineNextBlock(%d): %v", i, err)
		}
		if _, _, err := remoteValidator.InsertBlock(remoteStore, block); err != nil {
			t.Fatalf("remote InsertBlock(%d): %v", i, err)
		}
		remoteNode.pruneMempool(block.Transactions)
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
		Coinbase:  "miner-local",
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
	if tip.State["bob"].Balance != 10 {
		t.Fatalf("expected synced state for bob balance=10, got=%d", tip.State["bob"].Balance)
	}

	applied = localNode.applySyncBlocks("peer-1", collected)
	if applied != 0 {
		t.Fatalf("applySyncBlocks duplicate applied=%d want=0", applied)
	}
}

func TestInitGenesisStoresDeterministicUnsealedGenesis(t *testing.T) {
	chainCfg, genesisCfg := testChainConfig(t, "genesis-lite")
	validator, err := consensus.NewValidator(chainCfg, consensus.CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer validator.Close()
	n, err := New(Config{
		Chain:     chainCfg,
		Genesis:   genesisCfg,
		Mine:      false,
		MaxNonces: 32,
		Logf:      func(string, ...any) {},
	}, validator, chain.NewMemoryStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	genesis, err := n.InitGenesis()
	if err != nil {
		t.Fatalf("InitGenesis: %v", err)
	}
	if genesis.Header.Nonce != 0 {
		t.Fatalf("expected unsealed genesis nonce=0, got %d", genesis.Header.Nonce)
	}
	if genesis.ColossusXSolution != nil || genesis.ColossusXSolutionCompact != nil {
		t.Fatal("expected unsealed genesis to have no colossusx solution payload")
	}
	if genesis.State["alice"].Balance != 100 {
		t.Fatalf("expected alloc state, got %#v", genesis.State)
	}
}

func TestParseBootnodes(t *testing.T) {
	got := ParseBootnodes(" 127.0.0.1:30333, ,127.0.0.1:30334 ")
	want := []string{"127.0.0.1:30333", "127.0.0.1:30334"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ParseBootnodes=%v want=%v", got, want)
	}
}

func TestInitialSyncReady(t *testing.T) {
	localWork := bigInt(10)
	moreWork := bigInt(12)
	tests := []struct {
		name              string
		localTip          uint64
		localWork         *big.Int
		peers             []*p2p.Peer
		wantReady         bool
		wantRemoteBest    uint64
		wantPeersWithStat int
	}{
		{name: "no peers starts mining immediately", localTip: 0, localWork: localWork, peers: nil, wantReady: true, wantRemoteBest: 0},
		{name: "connected peers without status wait for sync metadata", localTip: 0, localWork: localWork, peers: []*p2p.Peer{{ID: "p1"}, {ID: "p2"}}, wantReady: false},
		{name: "ready once local work catches known remote best", localTip: 5, localWork: moreWork, peers: []*p2p.Peer{{Status: types.PeerStatus{PeerID: "p1", BestHeight: 3, TotalWork: "9"}}, {Status: types.PeerStatus{PeerID: "p2", BestHeight: 5, TotalWork: "12"}}}, wantReady: true, wantRemoteBest: 5, wantPeersWithStat: 2},
		{name: "not ready while local work is behind", localTip: 6, localWork: localWork, peers: []*p2p.Peer{{Status: types.PeerStatus{PeerID: "p1", BestHeight: 6, TotalWork: "11"}}}, wantReady: false, wantRemoteBest: 6, wantPeersWithStat: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready, remoteBest, peersWithStatus := initialSyncReady(tc.localTip, tc.localWork, tc.peers)
			if ready != tc.wantReady {
				t.Fatalf("ready=%v want=%v", ready, tc.wantReady)
			}
			if remoteBest != tc.wantRemoteBest {
				t.Fatalf("remoteBest=%d want=%d", remoteBest, tc.wantRemoteBest)
			}
			if peersWithStatus != tc.wantPeersWithStat {
				t.Fatalf("peersWithStatus=%d want=%d", peersWithStatus, tc.wantPeersWithStat)
			}
		})
	}
}

func bigInt(v int64) *big.Int { return big.NewInt(v) }
