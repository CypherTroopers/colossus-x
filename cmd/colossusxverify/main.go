package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	cx "colossusx/colossusx"
	"colossusx/pkg/types"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("colossusxverify", flag.ContinueOnError)
	modeName := fs.String("mode", string(cx.ModeResearch), "verification mode: strict or research")
	headerPath := fs.String("header", "", "path to a JSON-encoded types.BlockHeader")
	blockPath := fs.String("block", "", "path to a JSON-encoded types.Block")
	initialDAGMiB := fs.Uint64("initial-dag-mib", cx.StrictInitialDAGSizeBytes/(1024*1024), "initial DAG size in MiB")
	dagMiB := fs.Uint64("dag-mib", 0, "deprecated alias for -initial-dag-mib")
	dagGrowthMiB := fs.Uint64("dag-growth-mib-per-epoch", cx.DefaultDAGGrowthBytesPerEpoch/(1024*1024), "DAG growth per epoch in MiB")
	reads := fs.Uint64("reads", 32, "reads/hash for research-mode verification")
	epochBlocks := fs.Uint64("epoch-blocks", 32, "epoch length for research-mode verification")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dagMiB != 0 {
		*initialDAGMiB = *dagMiB
	}

	header, err := loadHeader(*headerPath, *blockPath)
	if err != nil {
		return err
	}
	spec, err := specFromHeader(cx.Mode(*modeName), header, (*initialDAGMiB)*1024*1024, (*dagGrowthMiB)*1024*1024, *reads, *epochBlocks)
	if err != nil {
		return err
	}
	expectedSeed := types.EpochSeedForHeight(spec, header.Height)
	if expectedSeed != header.EpochSeed {
		return fmt.Errorf("epoch seed mismatch: expected=%s got=%s", expectedSeed.String(), header.EpochSeed.String())
	}

	hash, ok, err := cx.VerifyHeaderStateless(spec.ResolvedForHeight(header.Height), header.EncodeForMining(), cx.NewUint64Nonce(header.Nonce), header.EpochSeed[:], header.Target)
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

func loadHeader(headerPath, blockPath string) (types.BlockHeader, error) {
	switch {
	case headerPath == "" && blockPath == "":
		return types.BlockHeader{}, errors.New("set one of --header or --block")
	case headerPath != "" && blockPath != "":
		return types.BlockHeader{}, errors.New("use either --header or --block, not both")
	case blockPath != "":
		var block types.Block
		if err := readJSONFile(blockPath, &block); err != nil {
			return types.BlockHeader{}, err
		}
		return block.Header, nil
	default:
		var header types.BlockHeader
		if err := readJSONFile(headerPath, &header); err != nil {
			return types.BlockHeader{}, err
		}
		return header, nil
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

func specFromHeader(mode cx.Mode, header types.BlockHeader, initialDAGBytes, growthBytes, reads, epochBlocks uint64) (cx.Spec, error) {
	var spec cx.Spec
	switch mode {
	case cx.ModeStrict:
		spec = cx.StrictSpec()
		if initialDAGBytes != 0 {
			spec.InitialDAGSizeBytes = initialDAGBytes
			spec.DAGSizeBytes = initialDAGBytes
		}
		if growthBytes != 0 {
			spec.DAGGrowthBytesPerEpoch = growthBytes
		}
	case cx.ModeResearch:
		spec = cx.ResearchSpecWithGrowth(initialDAGBytes, growthBytes, reads, epochBlocks)
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
