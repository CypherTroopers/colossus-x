package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	cmd, rest := resolveCommand(args)
	if cmd == "help" {
		printTopLevelUsage()
		return nil
	}
	switch cmd {
	case "mine":
		return miner.Main(rest)
	case "daemon":
		return runDaemon(rest)
	case "verify":
		return runVerify(rest)
	default:
		return fmt.Errorf("unsupported command %q", cmd)
	}
}

func resolveCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "mine", args
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return "help", nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "mine", args
	}
	switch args[0] {
	case "mine":
		return "mine", args[1:]
	case "daemon", "node":
		return "daemon", args[1:]
	case "verify":
		return "verify", args[1:]
	default:
		return args[0], args[1:]
	}
}

func printTopLevelUsage() {
	fmt.Println("colossusx command")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  colossusx [mine flags]")
	fmt.Println("  colossusx mine [flags]")
	fmt.Println("  colossusx daemon [flags]")
	fmt.Println("  colossusx verify [flags]")
	fmt.Println("")
	fmt.Println("Subcommands:")
	fmt.Println("  mine    run miner (default command)")
	fmt.Println("  daemon  run node/daemon (alias: node)")
	fmt.Println("  verify  verify PoW for header/block JSON")
}

type daemonConfig struct {
	Chain         types.ChainConfig
	Genesis       types.GenesisConfig
	GenesisFile   string
	NodeRole      nodeRole
	Mine          bool
	Workers       int
	MaxNonces     uint64
	BlockTime     time.Duration
	DataDir       string
	ListenAddr    string
	Bootnodes     []string
	NodeID        string
	MinerBackend  miner.BackendMode
	AutoBackend   bool
	MinerDAGAlloc string
}

type nodeRole string

const (
	nodeRoleFull  nodeRole = "full"
	nodeRoleMiner nodeRole = "miner"
	nodeRoleLight nodeRole = "light"
)

const (
	daemonAutoBenchmarkDAGBytes uint64 = 64 * 1024 * 1024
	daemonAutoBenchmarkNonces   uint64 = 4096
	daemonFixedBlockTime               = 10 * time.Minute
)

var daemonAutoBenchmarkHeader = []byte("colossusx-daemon-auto-backend-benchmark")

func runDaemon(args []string) error {
	cfg, err := parseDaemonFlags(args)
	if err != nil {
		return err
	}

	validator, err := consensus.NewValidator(cfg.Chain, consensus.CPUBackend{}, cfg.Workers)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := validator.Close(); closeErr != nil {
			log.Printf("validator close: %v", closeErr)
		}
	}()
	validator.SetLightValidation(cfg.NodeRole == nodeRoleLight)

	miningBackend := cx.HashBackend(consensus.CPUBackend{})
	strategy := miner.MemoryStrategy(miner.GoHeapMemory{})
	runtimeStatus := "not-required"
	if cfg.NodeRole != nodeRoleLight {
		miningBackend, strategy, runtimeStatus, err = initializeMining(cfg)
		if err != nil {
			return err
		}
		validator.SetMiningBackend(miningBackend, strategy)
	}

	store, err := chain.NewDiskStore(cfg.DataDir)
	if err != nil {
		return err
	}
	if err := validateStoredGenesis(store, cfg.Genesis); err != nil {
		return err
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
		MinerBackend:       string(miningBackend.Mode()),
		MinerDAGAlloc:      cfg.MinerDAGAlloc,
		ResolvedDAGAlloc:   strategy.Name(),
		RuntimeInitStatus:  runtimeStatus,
		MinerExecutionPath: miningBackend.Description(),
	}, validator, store)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("colossusx daemon starting network=%s mode=%s node_role=%s initial_dag=%dMiB dag_growth=%dMiB/epoch workers=%d mine=%t datadir=%s listen=%s bootnodes=%d node_id=%s miner_backend=%s miner_dag_alloc=%s resolved_alloc=%s runtime_init=%s execution=%s\n", cfg.Chain.NetworkID, cfg.Chain.Spec.Mode, cfg.NodeRole, cfg.Chain.Spec.InitialDAGSizeBytes/(1024*1024), cfg.Chain.Spec.DAGGrowthBytesPerEpoch/(1024*1024), cfg.Workers, cfg.Mine, cfg.DataDir, cfg.ListenAddr, len(cfg.Bootnodes), cfg.NodeID, miningBackend.Mode(), cfg.MinerDAGAlloc, strategy.Name(), runtimeStatus, miningBackend.Description())
	if err := n.Run(ctx); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

func parseDaemonFlags(args []string) (daemonConfig, error) {
	fs := flag.NewFlagSet("colossusx daemon", flag.ContinueOnError)
	modeName := fs.String("mode", string(cx.ModeColossusX), "chain mode (colossusx only)")
	networkID := fs.String("network", "devnet", "network identifier")
	dagGrowthMiB := fs.Uint64("dag-growth-mib-per-epoch", cx.DefaultDAGGrowthBytesPerEpoch/(1024*1024), "DAG growth per epoch in MiB")
	nodeRoleName := fs.String("node-role", string(nodeRoleFull), "node role: full, miner, or light")
	mine := fs.Bool("mine", true, "enable local mining loop")
	noMine := fs.Bool("no-mine", false, "disable local mining loop")
	workers := fs.Int("workers", runtime.NumCPU(), "mining workers")
	maxNonces := fs.Uint64("max-nonces", 500000, "maximum nonce range per block template")
	genesisMessage := fs.String("genesis-message", "colossusx devnet genesis", "genesis message")
	genesisFile := fs.String("genesis-file", "", "path to genesis JSON file (recommended for shared chain bootstrap)")
	dataDir := fs.String("datadir", filepath.Join(".", "data"), "node data directory")
	listenAddr := fs.String("listen", ":30333", "tcp listen address")
	bootnodes := fs.String("bootnodes", "", "comma-separated bootnode addresses")
	nodeID := fs.String("node-id", "", "stable node identifier")
	targetHex := fs.String("target", "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "mining target in hex")
	minerBackend := fs.String("miner-backend", string(miner.BackendOpenCL), "mining backend: auto, cuda, opencl, metal, cpu, unified, or gpu (auto selects best available)")
	minerDAGAlloc := fs.String("miner-dag-alloc", "auto", "mining DAG allocation strategy: auto, go-heap, pinned-host, cuda-managed, opencl-svm, metal-shared")
	if err := fs.Parse(args); err != nil {
		return daemonConfig{}, err
	}

	role, err := parseNodeRole(*nodeRoleName)
	if err != nil {
		return daemonConfig{}, err
	}
	if flagProvided(fs, "node-role") && (flagProvided(fs, "mine") || flagProvided(fs, "no-mine")) {
		return daemonConfig{}, errors.New("use either --node-role or --mine/--no-mine, not both")
	}
	if !flagProvided(fs, "node-role") {
		if flagProvided(fs, "no-mine") && *noMine {
			role = nodeRoleFull
		} else if flagProvided(fs, "mine") && *mine {
			role = nodeRoleMiner
		}
	}

	mode := cx.Mode(*modeName)
	var spec cx.Spec
	switch mode {
	case cx.ModeColossusX:
		spec = cx.ColossusXSpec()
		if *dagGrowthMiB != cx.DefaultDAGGrowthBytesPerEpoch/(1024*1024) {
			spec.DAGGrowthBytesPerEpoch = (*dagGrowthMiB) * 1024 * 1024
		}
	default:
		return daemonConfig{}, fmt.Errorf("unsupported mode %q", *modeName)
	}
	if err := spec.Validate(); err != nil {
		return daemonConfig{}, err
	}
	autoBackend := strings.EqualFold(strings.TrimSpace(*minerBackend), "auto")
	backendMode, err := miner.ParseBackendMode(*minerBackend)
	if err != nil {
		return daemonConfig{}, err
	}
	if err := miner.ValidateColossusXProductionConfig(mode, backendMode, *minerDAGAlloc); err != nil {
		return daemonConfig{}, err
	}
	target, err := cx.ParseTargetHex(*targetHex)
	if err != nil {
		return daemonConfig{}, err
	}
	switch role {
	case nodeRoleMiner:
		*mine = true
	case nodeRoleFull, nodeRoleLight:
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
	if strings.TrimSpace(*genesisFile) != "" {
		loadedGenesis, err := loadGenesisConfigFromFile(*genesisFile)
		if err != nil {
			return daemonConfig{}, err
		}
		if loadedGenesis.Spec.Mode == cx.ModeColossusX && (loadedGenesis.Spec.InitialDAGSizeBytes != cx.ColossusXInitialDAGSizeBytes || loadedGenesis.Spec.DAGSizeBytes != cx.ColossusXInitialDAGSizeBytes) {
			return daemonConfig{}, fmt.Errorf("genesis-file initial DAG must be fixed at %d bytes for colossusx daemon", cx.ColossusXInitialDAGSizeBytes)
		}
		if flagProvided(fs, "network") && *networkID != loadedGenesis.ChainID {
			return daemonConfig{}, fmt.Errorf("genesis-file chain_id %q does not match -network %q", loadedGenesis.ChainID, *networkID)
		}
		if flagProvided(fs, "mode") && mode != loadedGenesis.Spec.Mode {
			return daemonConfig{}, fmt.Errorf("genesis-file spec.mode %q does not match -mode %q", loadedGenesis.Spec.Mode, mode)
		}
		if flagProvided(fs, "target") && target != loadedGenesis.Bits {
			return daemonConfig{}, fmt.Errorf("genesis-file target does not match -target")
		}
		if flagProvided(fs, "dag-growth-mib-per-epoch") && spec.DAGGrowthBytesPerEpoch != loadedGenesis.Spec.DAGGrowthBytesPerEpoch {
			return daemonConfig{}, fmt.Errorf("genesis-file dag_growth_bytes_per_epoch does not match -dag-growth-mib-per-epoch")
		}
		if flagProvided(fs, "genesis-message") && *genesisMessage != loadedGenesis.Message {
			return daemonConfig{}, fmt.Errorf("genesis-file message does not match -genesis-message")
		}
		genesis = loadedGenesis
		chainCfg = types.ChainConfig{NetworkID: loadedGenesis.ChainID, Spec: loadedGenesis.Spec}
	}
	return daemonConfig{Chain: chainCfg, Genesis: genesis, GenesisFile: *genesisFile, NodeRole: role, Mine: *mine, Workers: *workers, MaxNonces: *maxNonces, BlockTime: daemonFixedBlockTime, DataDir: *dataDir, ListenAddr: *listenAddr, Bootnodes: node.ParseBootnodes(*bootnodes), NodeID: *nodeID, MinerBackend: backendMode, AutoBackend: autoBackend, MinerDAGAlloc: *minerDAGAlloc}, nil
}

func parseNodeRole(raw string) (nodeRole, error) {
	role := nodeRole(strings.ToLower(strings.TrimSpace(raw)))
	switch role {
	case nodeRoleFull, nodeRoleMiner, nodeRoleLight:
		return role, nil
	default:
		return "", fmt.Errorf("unsupported node role %q (expected full|miner|light)", raw)
	}
}

func flagProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func initializeMining(cfg daemonConfig) (cx.HashBackend, miner.MemoryStrategy, string, error) {
	if cfg.AutoBackend {
		mode, rate, err := benchmarkDaemonBackends(cfg)
		if err != nil {
			return nil, nil, "failed", err
		}
		fmt.Printf("daemon startup auto backend benchmark selected: %s (%.2f H/s, nonces=%d)\n", mode, rate, daemonAutoBenchmarkNonces)
		cfg.MinerBackend = mode
	}
	backend, err := miner.NewBackend(cfg.MinerBackend)
	if err != nil {
		return nil, nil, "failed", err
	}
	runtimeState, err := miner.InitializeBackendRuntime(backend)
	if err != nil {
		return nil, nil, "failed", err
	}
	status := runtimeInitStatus(runtimeState)
	strategy, err := miner.ResolveDAGStrategyForMode(cfg.Chain.Spec.Mode, cfg.MinerBackend, runtimeState, cfg.MinerDAGAlloc)
	if err != nil {
		return nil, nil, status, err
	}
	return backend, strategy, status, nil
}

func benchmarkDaemonBackends(cfg daemonConfig) (miner.BackendMode, float64, error) {
	benchmarkSpec := cfg.Chain.Spec
	if benchmarkSpec.NodeSize == 0 {
		benchmarkSpec.NodeSize = cx.ColossusXNodeSize
	}
	maxDAG := daemonAutoBenchmarkDAGBytes - (daemonAutoBenchmarkDAGBytes % benchmarkSpec.NodeSize)
	if maxDAG == 0 {
		maxDAG = benchmarkSpec.NodeSize
	}
	initial := benchmarkSpec.InitialDAGSizeBytes
	if initial == 0 {
		initial = benchmarkSpec.DAGSizeBytes
	}
	if initial == 0 || initial > maxDAG {
		benchmarkSpec.InitialDAGSizeBytes = maxDAG
		benchmarkSpec.DAGSizeBytes = maxDAG
	}

	dag, err := cx.NewDAGWithAllocator(benchmarkSpec, miner.GoHeapMemory{})
	if err != nil {
		return "", 0, err
	}
	defer dag.Close()

	seed := types.EpochSeedForHeight(benchmarkSpec, 0)
	if err := cx.PopulateDAG(dag, seed[:], cfg.Workers); err != nil {
		return "", 0, err
	}

	candidates := []miner.BackendMode{
		miner.BackendCUDA,
		miner.BackendMetal,
		miner.BackendOpenCL,
		miner.BackendUnified,
		miner.BackendCPU,
	}
	var (
		bestMode miner.BackendMode
		bestRate float64
		found    bool
	)
	for _, mode := range candidates {
		backend, err := miner.NewBackend(mode)
		if err != nil {
			continue
		}
		if _, err := miner.InitializeBackendRuntime(backend); err != nil {
			continue
		}
		if err := backend.Prepare(dag); err != nil {
			continue
		}
		m, err := cx.NewMiner(benchmarkSpec, dag, cfg.Workers, daemonSkipPrepareBackend{backend})
		if err != nil {
			continue
		}
		rate := cx.Benchmark(m, daemonAutoBenchmarkHeader, cx.NewUint64Nonce(0), daemonAutoBenchmarkNonces).HashRate
		if !found || rate > bestRate {
			found = true
			bestRate = rate
			bestMode = mode
		}
	}
	if !found {
		return "", 0, fmt.Errorf("auto backend benchmark failed: no candidate backend could be initialized")
	}
	return bestMode, bestRate, nil
}

type daemonSkipPrepareBackend struct{ cx.HashBackend }

func (b daemonSkipPrepareBackend) Prepare(*cx.DAG) error { return nil }

type runtimeCapabilityView interface {
	CUDADeviceOrdinal() (int, bool)
	OpenCLContext() (miner.OpenCLContext, bool)
	MetalContext() (miner.MetalContext, bool)
}

func runtimeInitStatus(state any) string {
	if state == nil {
		return "not-required"
	}
	runtime, ok := state.(runtimeCapabilityView)
	if !ok {
		return "ok"
	}
	if _, ok := runtime.CUDADeviceOrdinal(); ok {
		return "ok"
	}
	if _, ok := runtime.OpenCLContext(); ok {
		return "ok"
	}
	if _, ok := runtime.MetalContext(); ok {
		return "ok"
	}
	return "probed-no-accel"
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("colossusx verify", flag.ContinueOnError)
	modeName := fs.String("mode", string(cx.ModeColossusX), "verification mode (colossusx only)")
	headerPath := fs.String("header", "", "path to a JSON-encoded types.BlockHeader")
	blockPath := fs.String("block", "", "path to a JSON-encoded types.Block")
	initialDAGMiB := fs.Uint64("initial-dag-mib", cx.ColossusXInitialDAGSizeBytes/(1024*1024), "initial DAG size in MiB")
	dagMiB := fs.Uint64("dag-mib", 0, "deprecated alias for -initial-dag-mib")
	dagGrowthMiB := fs.Uint64("dag-growth-mib-per-epoch", cx.DefaultDAGGrowthBytesPerEpoch/(1024*1024), "DAG growth per epoch in MiB")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dagMiB != 0 {
		*initialDAGMiB = *dagMiB
	}

	header, block, err := loadVerifyInput(*headerPath, *blockPath)
	if err != nil {
		return err
	}
	spec, err := specFromHeader(cx.Mode(*modeName), header, (*initialDAGMiB)*1024*1024, (*dagGrowthMiB)*1024*1024)
	if err != nil {
		return err
	}
	expectedSeed := types.EpochSeedForHeight(spec, header.Height)
	if expectedSeed != header.EpochSeed {
		return fmt.Errorf("epoch seed mismatch: expected=%s got=%s", expectedSeed.String(), header.EpochSeed.String())
	}

	resolved := spec.ResolvedForHeight(header.Height)
	resolved.DAGSizeBytes = header.DAGSizeBytes
	if resolved.AlgorithmVersion >= 2 || resolved.Mode == cx.ModeColossusX {
		if block == nil {
			return errors.New("colossusx verify requires --block with colossusx_solution or colossusx_solution_compact")
		}
		var solution cx.ColossusXSolution
		switch {
		case block.ColossusXSolution != nil:
			solution = *block.ColossusXSolution
		case block.ColossusXSolutionCompact != nil:
			expanded, err := cx.ExpandCompactColossusXSolution(*block.ColossusXSolutionCompact)
			if err != nil {
				return fmt.Errorf("invalid compact colossusx solution: %w", err)
			}
			solution = expanded
		default:
			return errors.New("block is missing colossusx solution fields")
		}
		if header.DAGMerkleRoot == (types.Hash{}) {
			return errors.New("header is missing dag_merkle_root")
		}
		if err := cx.VerifyColossusXSolution(resolved, header.EncodeForMining(), header.Target, [32]byte(header.DAGMerkleRoot), solution); err != nil {
			return err
		}
		fmt.Printf("valid=true\n")
		fmt.Printf("target=%s\n", header.Target.String())
		fmt.Printf("mode=%s\n", spec.Mode)
		fmt.Printf("algorithm_version=%d\n", spec.AlgorithmVersion)
		fmt.Printf("dag_size_bytes=%d\n", header.DAGSizeBytes)
		return nil
	}
	hash, ok, err := cx.VerifyHeaderStateless(resolved, header.EncodeForMining(), cx.NewUint64Nonce(header.Nonce), header.EpochSeed[:], header.Target)
	if err != nil {
		return err
	}
	fmt.Printf("valid=%t\n", ok)
	fmt.Printf("pow256=%s\n", hex.EncodeToString(hash.Pow256[:]))
	fmt.Printf("target=%s\n", header.Target.String())
	fmt.Printf("mode=%s\n", spec.Mode)
	fmt.Printf("algorithm_version=%d\n", spec.AlgorithmVersion)
	fmt.Printf("dag_size_bytes=%d\n", header.DAGSizeBytes)
	if !ok {
		return errors.New("proof-of-work verification failed")
	}
	return nil
}

func loadVerifyInput(headerPath, blockPath string) (types.BlockHeader, *types.Block, error) {
	switch {
	case headerPath == "" && blockPath == "":
		return types.BlockHeader{}, nil, errors.New("set one of --header or --block")
	case headerPath != "" && blockPath != "":
		return types.BlockHeader{}, nil, errors.New("use either --header or --block, not both")
	case blockPath != "":
		var block types.Block
		if err := readJSONFile(blockPath, &block); err != nil {
			return types.BlockHeader{}, nil, err
		}
		return block.Header, &block, nil
	default:
		var header types.BlockHeader
		if err := readJSONFile(headerPath, &header); err != nil {
			return types.BlockHeader{}, nil, err
		}
		return header, nil, nil
	}
}

func readJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func specFromHeader(mode cx.Mode, header types.BlockHeader, initialDAGBytes, growthBytes uint64) (cx.Spec, error) {
	var spec cx.Spec
	switch mode {
	case cx.ModeColossusX:
		spec = cx.ColossusXSpec()
		if initialDAGBytes != 0 {
			spec.InitialDAGSizeBytes = initialDAGBytes
			spec.DAGSizeBytes = initialDAGBytes
		}
		if growthBytes != 0 {
			spec.DAGGrowthBytesPerEpoch = growthBytes
		}
	default:
		return cx.Spec{}, fmt.Errorf("unsupported mode %q", mode)
	}
	if err := spec.Validate(); err != nil {
		return cx.Spec{}, err
	}
	resolved := spec.ResolvedForHeight(header.Height)
	if header.DAGSizeBytes != resolved.DAGSizeBytes {
		return cx.Spec{}, fmt.Errorf("dag size mismatch: expected=%d got=%d", resolved.DAGSizeBytes, header.DAGSizeBytes)
	}
	if header.AlgorithmVersion != spec.AlgorithmVersion {
		return cx.Spec{}, fmt.Errorf("algorithm version mismatch: header=%d spec=%d", header.AlgorithmVersion, spec.AlgorithmVersion)
	}
	return spec, nil
}
