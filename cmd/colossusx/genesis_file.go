package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/types"
)

type genesisFileDocument struct {
	ChainID              string `json:"chain_id"`
	Message              string `json:"message,omitempty"`
	Timestamp            int64  `json:"timestamp"`
	TargetHex            string `json:"target"`
	Mode                 string `json:"mode,omitempty"`
	InitialDAGMiB        uint64 `json:"initial_dag_mib,omitempty"`
	DAGGrowthMiBPerEpoch uint64 `json:"dag_growth_mib_per_epoch,omitempty"`
	ExtraData            string `json:"extra_data,omitempty"`
}

func loadGenesisConfigFromFile(path string) (types.GenesisConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return types.GenesisConfig{}, fmt.Errorf("read genesis file %q: %w", path, err)
	}
	var doc genesisFileDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return types.GenesisConfig{}, fmt.Errorf("decode genesis file %q: %w", path, err)
	}
	if doc.ChainID == "" {
		return types.GenesisConfig{}, fmt.Errorf("invalid genesis file %q: chain_id is required", path)
	}
	if doc.Timestamp <= 0 {
		return types.GenesisConfig{}, fmt.Errorf("invalid genesis file %q: timestamp must be > 0", path)
	}
	target, err := cx.ParseTargetHex(doc.TargetHex)
	if err != nil {
		return types.GenesisConfig{}, fmt.Errorf("invalid genesis file %q target: %w", path, err)
	}
	spec := cx.ColossusXSpec()
	if doc.Mode != "" {
		spec.Mode = cx.Mode(doc.Mode)
	}
	if doc.InitialDAGMiB != 0 {
		spec.InitialDAGSizeBytes = doc.InitialDAGMiB * 1024 * 1024
		spec.DAGSizeBytes = spec.InitialDAGSizeBytes
	}
	if doc.DAGGrowthMiBPerEpoch != 0 {
		spec.DAGGrowthBytesPerEpoch = doc.DAGGrowthMiBPerEpoch * 1024 * 1024
	}
	if err := spec.Validate(); err != nil {
		return types.GenesisConfig{}, fmt.Errorf("invalid genesis file %q spec: %w", path, err)
	}
	cfg := types.GenesisConfig{
		ChainID:   doc.ChainID,
		Message:   doc.Message,
		Timestamp: doc.Timestamp,
		Bits:      target,
		Spec:      spec,
		ExtraData: doc.ExtraData,
	}
	return cfg, nil
}

func validateStoredGenesis(store chain.Store, cfg types.GenesisConfig) error {
	existing, err := store.GetBlockByHeight(0)
	if err != nil {
		if errors.Is(err, chain.ErrBlockNotFound) {
			return nil
		}
		return fmt.Errorf("load stored genesis: %w", err)
	}
	expected := types.NewGenesisBlock(cfg)
	if existing.Header.Version != expected.Header.Version ||
		existing.Header.AlgorithmVersion != expected.Header.AlgorithmVersion ||
		existing.Header.Height != expected.Header.Height ||
		existing.Header.ParentHash != expected.Header.ParentHash ||
		existing.Header.Timestamp != expected.Header.Timestamp ||
		existing.Header.Target != expected.Header.Target ||
		existing.Header.EpochSeed != expected.Header.EpochSeed ||
		existing.Header.DAGSizeBytes != expected.Header.DAGSizeBytes ||
		existing.Header.TxRoot != expected.Header.TxRoot ||
		existing.Header.StateRoot != expected.Header.StateRoot {
		return fmt.Errorf("stored genesis mismatch (stored=%s expected=%s): datadir was initialized with a different genesis", existing.BlockHash().String(), expected.BlockHash().String())
	}
	return nil
}
