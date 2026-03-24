package main

import (
	"os"
	"testing"
)

func TestParseFlagsCPUBackend(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"colossusd", "-miner-backend=cpu", "-miner-dag-alloc=go-heap"}
	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if got := string(cfg.MinerBackend); got != "cpu" {
		t.Fatalf("expected cpu backend, got %q", got)
	}
	if cfg.MinerDAGAlloc != "go-heap" {
		t.Fatalf("expected go-heap dag alloc, got %q", cfg.MinerDAGAlloc)
	}
}

func TestInitializeMiningUnifiedGoHeap(t *testing.T) {
	cfg := config{MinerBackend: "unified", MinerDAGAlloc: "go-heap"}
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
	cfg := config{MinerBackend: "gpu", MinerDAGAlloc: "opencl-svm"}
	_, _, _, err := initializeMining(cfg)
	if err == nil {
		t.Fatal("expected explicit gpu/opencl-svm request to fail gracefully in test environment")
	}
}
