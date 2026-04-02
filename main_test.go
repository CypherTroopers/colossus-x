package miner

import (
	"testing"

	cx "colossusx/colossusx"
)

func TestParseBackendMode(t *testing.T) {
	for _, mode := range []string{"unified", "cpu", "gpu", "auto"} {
		if _, err := ParseBackendMode(mode); err != nil {
			t.Fatalf("ParseBackendMode(%q) returned error: %v", mode, err)
		}
	}
	if _, err := ParseBackendMode("bogus"); err == nil {
		t.Fatal("expected invalid backend to fail")
	}
}

func TestParseBackendModeAutoMapsToUnified(t *testing.T) {
	restore := setAutoBackendProbesForTest(false, false, false)
	defer restore()

	got, err := ParseBackendMode("auto")
	if err != nil {
		t.Fatalf("ParseBackendMode(auto) returned error: %v", err)
	}
	if got != BackendUnified {
		t.Fatalf("expected auto backend to resolve to %q, got %q", BackendUnified, got)
	}
}

func TestParseBackendModeAutoPrefersCUDAThenMetalThenOpenCL(t *testing.T) {
	tests := []struct {
		name   string
		cuda   bool
		metal  bool
		opencl bool
		want   BackendMode
	}{
		{name: "cuda-first", cuda: true, metal: true, opencl: true, want: BackendCUDA},
		{name: "metal-second", cuda: false, metal: true, opencl: true, want: BackendMetal},
		{name: "opencl-third", cuda: false, metal: false, opencl: true, want: BackendOpenCL},
		{name: "fallback-unified", cuda: false, metal: false, opencl: false, want: BackendUnified},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restore := setAutoBackendProbesForTest(tc.cuda, tc.metal, tc.opencl)
			defer restore()

			got, err := ParseBackendMode("auto")
			if err != nil {
				t.Fatalf("ParseBackendMode(auto) returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func setAutoBackendProbesForTest(cuda, metal, opencl bool) func() {
	prevCUDA := autoBackendCUDAProbe
	prevMetal := autoBackendMetalProbe
	prevOpenCL := autoBackendOpenCLProbe

	autoBackendCUDAProbe = func() bool { return cuda }
	autoBackendMetalProbe = func() bool { return metal }
	autoBackendOpenCLProbe = func() bool { return opencl }

	return func() {
		autoBackendCUDAProbe = prevCUDA
		autoBackendMetalProbe = prevMetal
		autoBackendOpenCLProbe = prevOpenCL
	}
}

func TestParseCLIConfigColossusXModeAllowsDynamicDAGProfile(t *testing.T) {
	cfg, err := ParseCLIConfig([]string{"-mode", "colossusx"})
	if err != nil {
		t.Fatalf("ParseCLIConfig: %v", err)
	}
	if cfg.Spec.InitialDAGSizeBytes != 32*1024*1024*1024 {
		t.Fatalf("unexpected colossusx initial DAG size: %d", cfg.Spec.InitialDAGSizeBytes)
	}
	if cfg.Spec.DAGGrowthBytesPerEpoch != 256*1024*1024 {
		t.Fatalf("unexpected colossusx DAG growth: %d", cfg.Spec.DAGGrowthBytesPerEpoch)
	}
}

func TestParseCLIConfigColossusXModeAllowsDagSizeOverrides(t *testing.T) {
	cfg, err := ParseCLIConfig([]string{"-mode", "colossusx", "-initial-dag-mib", "1", "-dag-growth-mib-per-epoch", "2"})
	if err != nil {
		t.Fatalf("ParseCLIConfig: %v", err)
	}
	if cfg.Spec.Mode != cx.ModeColossusX || cfg.Spec.InitialDAGSizeBytes != 1024*1024 || cfg.Spec.DAGGrowthBytesPerEpoch != 2*1024*1024 {
		t.Fatalf("unexpected colossusx spec: %+v", cfg.Spec)
	}
}

func TestCPUAndUnifiedBackendsProduceSameHash(t *testing.T) {
	spec := Spec{Mode: cx.ModeColossusX, DAGSizeBytes: 1024 * 1024, NodeSize: DefaultNodeSize, ReadsPerHash: 8, EpochBlocks: DefaultEpochBlocks}
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
	spec := Spec{Mode: cx.ModeColossusX, DAGSizeBytes: 64 * 8, NodeSize: DefaultNodeSize, ReadsPerHash: 4, EpochBlocks: DefaultEpochBlocks}
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

func TestCPUBackendUsesDAGAllocationDirectly(t *testing.T) {
	spec := Spec{Mode: cx.ModeColossusX, DAGSizeBytes: 64 * 8, NodeSize: DefaultNodeSize, ReadsPerHash: 4, EpochBlocks: DefaultEpochBlocks}
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
	if original == mutated {
		t.Fatal("expected cpu backend to observe DAG mutations through shared memory")
	}
}

func TestRunInitializesBackendRuntimeBeforeResolvingAllocator(t *testing.T) {
	spec := Spec{Mode: cx.ModeColossusX, DAGSizeBytes: 64 * 64, NodeSize: DefaultNodeSize, ReadsPerHash: 4, EpochBlocks: DefaultEpochBlocks}
	cfg := CLIConfig{Mode: cx.ModeColossusX, Backend: BackendGPU, DAGAlloc: "auto", Spec: spec, Workers: 1, Header: []byte("01"), EpochSeed: []byte("seedseedseedseedseedseedseedseed"), Target: cx.Target{}, MaxNonces: 1, BenchOnly: true}
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

func TestColossusXSpecLocksSectionTwoConstants(t *testing.T) {
	spec := cx.ColossusXSpec()
	if spec.ReadsPerHash != cx.ColossusXReadsPerHash {
		t.Fatalf("expected colossusx reads/hash %d, got %d", cx.ColossusXReadsPerHash, spec.ReadsPerHash)
	}
	if spec.ReadsPerHash != 128 {
		t.Fatalf("expected colossusx spec to preserve v2 reads/hash target, got %d", spec.ReadsPerHash)
	}
	if spec.EpochBlocks != 7200 {
		t.Fatalf("expected colossusx epoch blocks 7200, got %d", spec.EpochBlocks)
	}
}

func TestColossusXDAGRequiresFullLogicalImage(t *testing.T) {
	spec := cx.ColossusXSpec()
	alloc := &testAllocation{buf: make([]byte, 1024)}
	_, err := NewDAGWithAllocation(spec, alloc, false)
	if err == nil {
		t.Fatal("expected colossusx DAG allocation to require the full logical DAG image")
	}
}

func TestParseCLIConfigColossusXModeDagMibAliasSetsInitialDag(t *testing.T) {
	cfg, err := ParseCLIConfig([]string{"-mode", "colossusx", "-dag-mib", "3"})
	if err != nil {
		t.Fatalf("ParseCLIConfig: %v", err)
	}
	if cfg.Spec.InitialDAGSizeBytes != 3*1024*1024 {
		t.Fatalf("expected dag-mib alias to set initial DAG size, got %d", cfg.Spec.InitialDAGSizeBytes)
	}
}
