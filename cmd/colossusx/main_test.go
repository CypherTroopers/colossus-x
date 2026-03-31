package main

import "testing"

func TestParseDaemonFlagsAllowsCPUBackendInColossusXProduction(t *testing.T) {
	cfg, err := parseDaemonFlags([]string{"-miner-backend=cpu", "-miner-dag-alloc=go-heap"})
	if err != nil {
		t.Fatalf("expected colossusx production config validation to allow cpu backend, got err=%v", err)
	}
	if cfg.MinerBackend != "cpu" {
		t.Fatalf("expected cpu miner backend, got %q", cfg.MinerBackend)
	}
}

func TestInitializeMiningUnifiedGoHeap(t *testing.T) {
	cfg := daemonConfig{MinerBackend: "unified", MinerDAGAlloc: "go-heap"}
	cfg.Chain.Spec.Mode = "colossusx"
	backend, strategy, status, err := initializeMining(cfg)
	if err != nil {
		t.Fatalf("initializeMining: %v", err)
	}
	if backend.Mode() != "unified" {
		t.Fatalf("expected unified backend, got %q", backend.Mode())
	}
	if strategy.Name() != "go-heap" {
		t.Fatalf("expected go-heap strategy, got %q", strategy.Name())
	}
	if status != "not-required" {
		t.Fatalf("expected not-required runtime status, got %q", status)
	}
}

func TestInitializeMiningExplicitGPUAllocatorRequest(t *testing.T) {
	cfg := daemonConfig{MinerBackend: "gpu", MinerDAGAlloc: "opencl-svm"}
	cfg.Chain.Spec.Mode = "colossusx"
	_, _, _, err := initializeMining(cfg)
	if err == nil {
		t.Fatal("expected explicit gpu/opencl-svm request to fail gracefully in test environment")
	}
}

func TestResolveCommand(t *testing.T) {
	cases := []struct {
		args []string
		cmd  string
	}{
		{args: nil, cmd: "mine"},
		{args: []string{"--help"}, cmd: "help"},
		{args: []string{"-bench"}, cmd: "mine"},
		{args: []string{"mine", "-bench"}, cmd: "mine"},
		{args: []string{"daemon"}, cmd: "daemon"},
		{args: []string{"node"}, cmd: "daemon"},
		{args: []string{"verify"}, cmd: "verify"},
		{args: []string{"unknown"}, cmd: "unknown"},
	}
	for _, tc := range cases {
		cmd, _ := resolveCommand(tc.args)
		if cmd != tc.cmd {
			t.Fatalf("resolveCommand(%v) = %q want %q", tc.args, cmd, tc.cmd)
		}
	}
}
