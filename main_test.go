package miner

import (
	"testing"

	cx "colossusx/colossusx"
)

func TestParseBackendMode(t *testing.T) {
	for _, mode := range []string{"unified", "cpu", "gpu"} {
		if _, err := ParseBackendMode(mode); err != nil {
			t.Fatalf("ParseBackendMode(%q) returned error: %v", mode, err)
		}
	}
	if _, err := ParseBackendMode("bogus"); err == nil {
		t.Fatal("expected invalid backend to fail")
	}
}

func TestParseCLIConfigStrictModeAllowsDynamicDAGProfile(t *testing.T) {
	cfg, err := ParseCLIConfig([]string{"-mode", "strict"})
	if err != nil {
		t.Fatalf("ParseCLIConfig: %v", err)
	}
	if cfg.Spec.InitialDAGSizeBytes != 8*1024*1024*1024 {
		t.Fatalf("unexpected strict initial DAG size: %d", cfg.Spec.InitialDAGSizeBytes)
	}
	if cfg.Spec.DAGGrowthBytesPerEpoch != 512*1024*1024 {
		t.Fatalf("unexpected strict DAG growth: %d", cfg.Spec.DAGGrowthBytesPerEpoch)
	}
}

func TestParseCLIConfigResearchModeAllowsOverrides(t *testing.T) {
	cfg, err := ParseCLIConfig([]string{"-mode", "research", "-initial-dag-mib", "1", "-dag-growth-mib-per-epoch", "2", "-reads", "8", "-epoch-blocks", "16"})
	if err != nil {
		t.Fatalf("ParseCLIConfig: %v", err)
	}
	if cfg.Spec.Mode != cx.ModeResearch || cfg.Spec.InitialDAGSizeBytes != 1024*1024 || cfg.Spec.DAGGrowthBytesPerEpoch != 2*1024*1024 || cfg.Spec.ReadsPerHash != 8 || cfg.Spec.EpochBlocks != 16 {
		t.Fatalf("unexpected research spec: %+v", cfg.Spec)
	}
}

func TestCPUAndUnifiedBackendsProduceSameHash(t *testing.T) {
	spec := Spec{Mode: cx.ModeResearch, DAGSizeBytes: 1024 * 1024, NodeSize: DefaultNodeSize, ReadsPerHash: 8, EpochBlocks: DefaultEpochBlocks}
	dag, err := NewDAG(spec)
	if err != nil {
		t.Fatalf("NewDAG: %v", err)
	}
	defer dag.Close()
	seed := []byte("0123456789abcdef0123456789abcdef")
	if err := GenerateDAG(dag, seed, 2); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	header := []byte("header")
	nonce := cx.NewUint64Nonce(42)

	cpu := &CPUBackend{}
	unified := &UnifiedBackend{}
	if err := cpu.Prepare(dag); err != nil {
		t.Fatalf("cpu Prepare: %v", err)
	}
	if err := unified.Prepare(dag); err != nil {
		t.Fatalf("unified Prepare: %v", err)
	}

	cpuHash := cpu.Hash(header, nonce, dag)
	unifiedHash := unified.Hash(header, nonce, dag)
	if cpuHash != unifiedHash {
		t.Fatalf("expected cpu and unified backends to match; cpu=%x unified=%x", cpuHash.Pow256, unifiedHash.Pow256)
	}
}

func TestUnifiedBackendUsesDAGAllocationDirectly(t *testing.T) {
	spec := Spec{Mode: cx.ModeResearch, DAGSizeBytes: 64 * 8, NodeSize: DefaultNodeSize, ReadsPerHash: 4, EpochBlocks: DefaultEpochBlocks}
	dag, err := NewDAG(spec)
	if err != nil {
		t.Fatalf("NewDAG: %v", err)
	}
	defer dag.Close()
	if err := GenerateDAG(dag, []byte("seedseedseedseedseedseedseedseed"), 1); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	backend := &UnifiedBackend{}
	if err := backend.Prepare(dag); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	original := backend.Hash([]byte("header"), cx.NewUint64Nonce(1), dag)

	copy(dag.Bytes(), make([]byte, len(dag.Bytes())))
	mutated := backend.Hash([]byte("header"), cx.NewUint64Nonce(1), dag)
	if original == mutated {
		t.Fatal("expected unified backend to observe DAG mutations through shared memory")
	}
}

func TestCPUBackendCopiesPreparedDAG(t *testing.T) {
	spec := Spec{Mode: cx.ModeResearch, DAGSizeBytes: 64 * 8, NodeSize: DefaultNodeSize, ReadsPerHash: 4, EpochBlocks: DefaultEpochBlocks}
	dag, err := NewDAG(spec)
	if err != nil {
		t.Fatalf("NewDAG: %v", err)
	}
	defer dag.Close()
	if err := GenerateDAG(dag, []byte("seedseedseedseedseedseedseedseed"), 1); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	backend := &CPUBackend{}
	if err := backend.Prepare(dag); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	original := backend.Hash([]byte("header"), cx.NewUint64Nonce(1), dag)

	copy(dag.Bytes(), make([]byte, len(dag.Bytes())))
	mutated := backend.Hash([]byte("header"), cx.NewUint64Nonce(1), dag)
	if original != mutated {
		t.Fatal("expected prepared CPU backend to keep using its own copied DAG")
	}
}

func TestRunInitializesBackendRuntimeBeforeResolvingAllocator(t *testing.T) {
	spec := Spec{Mode: cx.ModeResearch, DAGSizeBytes: 64 * 64, NodeSize: DefaultNodeSize, ReadsPerHash: 4, EpochBlocks: DefaultEpochBlocks}
	cfg := CLIConfig{Mode: cx.ModeResearch, Backend: BackendGPU, DAGAlloc: "auto", Spec: spec, Workers: 1, Header: []byte("01"), EpochSeed: []byte("seedseedseedseedseedseedseedseed"), Target: cx.Target{}, MaxNonces: 1, BenchOnly: true}
	backend := &fakeGPUBackend{}
	if err := Run(cfg, backend); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !backend.runtimeCalled {
		t.Fatal("expected run to initialize backend runtime before allocator resolution")
	}
	if !backend.prepared {
		t.Fatal("expected run to prepare backend after dag allocation/population")
	}
}

func TestStrictSpecLocksSectionTwoConstants(t *testing.T) {
	spec := cx.StrictSpec()
	if spec.ReadsPerHash != cx.StrictReadsPerHash {
		t.Fatalf("expected strict reads/hash %d, got %d", cx.StrictReadsPerHash, spec.ReadsPerHash)
	}
	if spec.ReadsPerHash != 512 {
		t.Fatalf("expected strict spec to preserve Section 2 bandwidth target reads/hash, got %d", spec.ReadsPerHash)
	}
	if spec.EpochBlocks != 8000 {
		t.Fatalf("expected strict epoch blocks 8000, got %d", spec.EpochBlocks)
	}
}

func TestStrictDAGRequiresFullLogicalImage(t *testing.T) {
	spec := cx.StrictSpec()
	alloc := &testAllocation{buf: make([]byte, 1024)}
	_, err := NewDAGWithAllocation(spec, alloc, false)
	if err == nil {
		t.Fatal("expected strict DAG allocation to require the full logical DAG image")
	}
}

func TestParseCLIConfigResearchModeDagMibAliasSetsInitialDag(t *testing.T) {
	cfg, err := ParseCLIConfig([]string{"-mode", "research", "-dag-mib", "3"})
	if err != nil {
		t.Fatalf("ParseCLIConfig: %v", err)
	}
	if cfg.Spec.InitialDAGSizeBytes != 3*1024*1024 {
		t.Fatalf("expected dag-mib alias to set initial DAG size, got %d", cfg.Spec.InitialDAGSizeBytes)
	}
}
