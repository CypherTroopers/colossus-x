package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	miner "colossusx"
	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/consensus"
	"colossusx/pkg/node"
	"colossusx/pkg/types"
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}

	validator, err := consensus.NewValidator(cfg.Chain, consensus.CPUBackend{}, cfg.Workers)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := validator.Close(); err != nil {
			log.Printf("validator close: %v", err)
		}
	}()

	miningBackend, strategy, runtimeStatus, err := initializeMining(cfg)
	if err != nil {
		log.Fatal(err)
	}
	validator.SetMiningBackend(miningBackend, strategy)

	store, err := chain.NewDiskStore(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}

	n, err := node.New(node.Config{
		Chain:              cfg.Chain,
		Genesis:            cfg.Genesis,
		Mine:               cfg.Mine,
		MaxNonces:          cfg.MaxNonces,
		BlockTime:          cfg.BlockTime,
		NodeID:             cfg.NodeID,
		ListenAddr:         cfg.ListenAddr,
		Bootnodes:          cfg.Bootnodes,
		MinerBackend:       string(cfg.MinerBackend),
		MinerDAGAlloc:      cfg.MinerDAGAlloc,
		ResolvedDAGAlloc:   strategy.Name(),
		RuntimeInitStatus:  runtimeStatus,
		MinerExecutionPath: miningBackend.Description(),
	}, validator, store)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("colossusd starting network=%s mode=%s initial_dag=%dMiB dag_growth=%dMiB/epoch workers=%d mine=%t datadir=%s listen=%s bootnodes=%d node_id=%s miner_backend=%s miner_dag_alloc=%s resolved_alloc=%s runtime_init=%s execution=%s\n", cfg.Chain.NetworkID, cfg.Chain.Spec.Mode, cfg.Chain.Spec.InitialDAGSizeBytes/(1024*1024), cfg.Chain.Spec.DAGGrowthBytesPerEpoch/(1024*1024), cfg.Workers, cfg.Mine, cfg.DataDir, cfg.ListenAddr, len(cfg.Bootnodes), cfg.NodeID, cfg.MinerBackend, cfg.MinerDAGAlloc, strategy.Name(), runtimeStatus, miningBackend.Description())
	if err := n.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}

type config struct {
	Chain         types.ChainConfig
	Genesis       types.GenesisConfig
	Mine          bool
	Workers       int
	MaxNonces     uint64
	BlockTime     time.Duration
	DataDir       string
	ListenAddr    string
	Bootnodes     []string
	NodeID        string
	MinerBackend  miner.BackendMode
	MinerDAGAlloc string
}

func parseFlags() (config, error) {
	fs := flag.NewFlagSet("colossusd", flag.ContinueOnError)
	modeName := fs.String("mode", string(cx.ModeResearch), "chain mode: strict or research")
	networkID := fs.String("network", "devnet", "network identifier")
	initialDAGMiB := fs.Uint64("initial-dag-mib", cx.StrictInitialDAGSizeBytes/(1024*1024), "initial DAG size in MiB")
	dagMiB := fs.Uint64("dag-mib", 0, "deprecated alias for -initial-dag-mib")
	dagGrowthMiB := fs.Uint64("dag-growth-mib-per-epoch", cx.DefaultDAGGrowthBytesPerEpoch/(1024*1024), "DAG growth per epoch in MiB")
	reads := fs.Uint64("reads", 32, "DAG reads per hash for research mode")
	epochBlocks := fs.Uint64("epoch-blocks", 32, "blocks per epoch for research mode")
	mine := fs.Bool("mine", true, "enable local mining loop")
	noMine := fs.Bool("no-mine", false, "disable local mining loop")
	workers := fs.Int("workers", runtime.NumCPU(), "mining workers")
	maxNonces := fs.Uint64("max-nonces", 500000, "maximum nonce range per block template")
	blockTime := fs.Duration("block-time", 500*time.Millisecond, "delay between mined blocks")
	genesisMessage := fs.String("genesis-message", "colossusx devnet genesis", "genesis message")
	dataDir := fs.String("datadir", filepath.Join(".", "data"), "node data directory")
	listenAddr := fs.String("listen", ":30333", "tcp listen address")
	bootnodes := fs.String("bootnodes", "", "comma-separated bootnode addresses")
	nodeID := fs.String("node-id", "", "stable node identifier")
	targetHex := fs.String("target", "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "mining target in hex")
	minerBackend := fs.String("miner-backend", string(miner.BackendOpenCL), "mining backend: cuda, opencl, metal, cpu, unified, or gpu")
	minerDAGAlloc := fs.String("miner-dag-alloc", "auto", "mining DAG allocation strategy: auto, go-heap, pinned-host, cuda-managed, opencl-svm, metal-shared")
	rpcListen := fs.String("rpc-listen", "", "reserved for future RPC listener")
	_ = rpcListen
	if err := fs.Parse(os.Args[1:]); err != nil {
		return config{}, err
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	mode := cx.Mode(*modeName)
	var spec cx.Spec
	if *dagMiB != 0 {
		*initialDAGMiB = *dagMiB
	}
	if mode == cx.ModeResearch && !setFlags["initial-dag-mib"] && !setFlags["dag-mib"] {
		*initialDAGMiB = 8
	}
	switch mode {
	case cx.ModeStrict:
		spec = cx.StrictSpec()
		if *initialDAGMiB != cx.StrictInitialDAGSizeBytes/(1024*1024) {
			spec.InitialDAGSizeBytes = (*initialDAGMiB) * 1024 * 1024
			spec.DAGSizeBytes = spec.InitialDAGSizeBytes
		}
		if *dagGrowthMiB != cx.DefaultDAGGrowthBytesPerEpoch/(1024*1024) {
			spec.DAGGrowthBytesPerEpoch = (*dagGrowthMiB) * 1024 * 1024
		}
	case cx.ModeResearch:
		spec = cx.ResearchSpecWithGrowth((*initialDAGMiB)*1024*1024, (*dagGrowthMiB)*1024*1024, *reads, *epochBlocks)
	default:
		return config{}, fmt.Errorf("unsupported mode %q", *modeName)
	}
	if err := spec.Validate(); err != nil {
		return config{}, err
	}
	backendMode, err := miner.ParseBackendMode(*minerBackend)
	if err != nil {
		return config{}, err
	}
	if err := miner.ValidateStrictProductionConfig(mode, backendMode, *minerDAGAlloc); err != nil {
		return config{}, err
	}
	target, err := cx.ParseTargetHex(*targetHex)
	if err != nil {
		return config{}, err
	}
	if *noMine {
		*mine = false
	}
	chainCfg := types.ChainConfig{NetworkID: *networkID, Spec: spec}
	genesis := types.GenesisConfig{
		ChainID:   *networkID,
		Message:   *genesisMessage,
		Timestamp: time.Now().Unix(),
		Bits:      target,
		Spec:      spec,
		ExtraData: fmt.Sprintf("mode=%s", spec.Mode),
	}
	return config{Chain: chainCfg, Genesis: genesis, Mine: *mine, Workers: *workers, MaxNonces: *maxNonces, BlockTime: *blockTime, DataDir: *dataDir, ListenAddr: *listenAddr, Bootnodes: node.ParseBootnodes(*bootnodes), NodeID: *nodeID, MinerBackend: backendMode, MinerDAGAlloc: *minerDAGAlloc}, nil
}

func initializeMining(cfg config) (cx.HashBackend, miner.MemoryStrategy, string, error) {
	backend, err := miner.NewBackend(cfg.MinerBackend)
	if err != nil {
		return nil, nil, "failed", err
	}
	runtimeState, err := miner.InitializeBackendRuntime(backend)
	if err != nil {
		return nil, nil, "failed", err
	}
	status := "not-required"
	if runtimeState != nil {
		status = "ok"
	}
	strategy, err := miner.ResolveDAGStrategyForMode(cfg.Chain.Spec.Mode, cfg.MinerBackend, runtimeState, cfg.MinerDAGAlloc)
	if err != nil {
		return nil, nil, status, err
	}
	return backend, strategy, status, nil
}
