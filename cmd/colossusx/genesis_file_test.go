package main

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/types"
)

func writeGenesisFile(t *testing.T, doc genesisFileDocument) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal genesis doc: %v", err)
	}
	path := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write genesis file: %v", err)
	}
	return path
}

func TestLoadGenesisConfigFromFile(t *testing.T) {
	doc := genesisFileDocument{
		ChainID:              "devnet-shared",
		Message:              "shared genesis",
		Timestamp:            1704067200,
		TargetHex:            "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Mode:                 "colossusx",
		InitialDAGMiB:        32,
		DAGGrowthMiBPerEpoch: 8,
		ExtraData:            "mode=colossusx",
	}
	path := writeGenesisFile(t, doc)

	cfg, err := loadGenesisConfigFromFile(path)
	if err != nil {
		t.Fatalf("loadGenesisConfigFromFile: %v", err)
	}
	if cfg.ChainID != doc.ChainID {
		t.Fatalf("chain_id mismatch: got=%q want=%q", cfg.ChainID, doc.ChainID)
	}
	if cfg.Timestamp != doc.Timestamp {
		t.Fatalf("timestamp mismatch: got=%d want=%d", cfg.Timestamp, doc.Timestamp)
	}
	if cfg.Bits.String() != doc.TargetHex {
		t.Fatalf("target mismatch: got=%s want=%s", cfg.Bits.String(), doc.TargetHex)
	}
	if cfg.Spec.InitialDAGSizeBytes != 32*1024*1024 {
		t.Fatalf("initial dag mismatch: got=%d", cfg.Spec.InitialDAGSizeBytes)
	}
	if cfg.Spec.DAGGrowthBytesPerEpoch != 8*1024*1024 {
		t.Fatalf("dag growth mismatch: got=%d", cfg.Spec.DAGGrowthBytesPerEpoch)
	}
}

func TestParseDaemonFlagsUsesGenesisFile(t *testing.T) {
	doc := genesisFileDocument{
		ChainID:              "devnet-shared",
		Message:              "shared genesis",
		Timestamp:            1704067200,
		TargetHex:            "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Mode:                 "colossusx",
		InitialDAGMiB:        cx.ColossusXInitialDAGSizeBytes / (1024 * 1024),
		DAGGrowthMiBPerEpoch: 8,
		ExtraData:            "mode=colossusx",
	}
	path := writeGenesisFile(t, doc)

	cfg, err := parseDaemonFlags([]string{"-genesis-file", path})
	if err != nil {
		t.Fatalf("parseDaemonFlags: %v", err)
	}
	if cfg.Chain.NetworkID != doc.ChainID {
		t.Fatalf("network mismatch: got=%q want=%q", cfg.Chain.NetworkID, doc.ChainID)
	}
	if cfg.Genesis.Timestamp != doc.Timestamp {
		t.Fatalf("genesis timestamp mismatch: got=%d want=%d", cfg.Genesis.Timestamp, doc.Timestamp)
	}
	if cfg.Chain.Spec.InitialDAGSizeBytes != cx.ColossusXInitialDAGSizeBytes {
		t.Fatalf("initial dag mismatch: got=%d", cfg.Chain.Spec.InitialDAGSizeBytes)
	}
}

func TestParseDaemonFlagsRejectsGenesisFileWithNonFixedInitialDAG(t *testing.T) {
	doc := genesisFileDocument{
		ChainID:              "devnet-shared",
		Message:              "shared genesis",
		Timestamp:            1704067200,
		TargetHex:            "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Mode:                 "colossusx",
		InitialDAGMiB:        32,
		DAGGrowthMiBPerEpoch: 8,
		ExtraData:            "mode=colossusx",
	}
	path := writeGenesisFile(t, doc)

	if _, err := parseDaemonFlags([]string{"-genesis-file", path}); err == nil {
		t.Fatal("expected fixed initial DAG validation error")
	}
}

func TestParseDaemonFlagsRejectsGenesisFileNetworkMismatch(t *testing.T) {
	doc := genesisFileDocument{
		ChainID:   "devnet-shared",
		Message:   "shared genesis",
		Timestamp: 1704067200,
		TargetHex: "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Mode:      "colossusx",
		ExtraData: "mode=colossusx",
	}
	path := writeGenesisFile(t, doc)

	if _, err := parseDaemonFlags([]string{"-genesis-file", path, "-network", "othernet"}); err == nil {
		t.Fatal("expected network mismatch error")
	}
}

func TestValidateStoredGenesis(t *testing.T) {
	spec := cx.ColossusXSpecWithGrowth(32*1024*1024, 8*1024*1024)
	target, err := cx.ParseTargetHex("0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	cfg := types.GenesisConfig{
		ChainID:   "devnet",
		Message:   "genesis",
		Timestamp: 1704067200,
		Bits:      target,
		Spec:      spec,
		ExtraData: "mode=colossusx",
	}
	store := chain.NewMemoryStore()
	if err := store.StoreBlock(types.NewGenesisBlock(cfg), big.NewInt(1)); err != nil {
		t.Fatalf("StoreBlock: %v", err)
	}
	if err := validateStoredGenesis(store, cfg); err != nil {
		t.Fatalf("validateStoredGenesis (match): %v", err)
	}

	mismatch := cfg
	mismatch.Message = "other"
	if err := validateStoredGenesis(store, mismatch); err == nil {
		t.Fatal("expected mismatch error")
	}
}
