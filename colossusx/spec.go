package colossusx

import (
	"errors"
	"fmt"
	"math"
)

const (
	StrictInitialDAGSizeBytes     uint64 = 8 * 1024 * 1024 * 1024
	DefaultDAGGrowthBytesPerEpoch uint64 = 512 * 1024 * 1024
	StrictNodeSize                uint64 = 64
	StrictReadsPerHash            uint64 = 512
	StrictEpochBlocks             uint64 = 8000

	StrictV2TileSizeBytes     uint64 = 4096
	StrictV2MatDim            uint32 = 16
	StrictV2ComputeRounds     uint32 = 64
	StrictV2RoundCommitPeriod uint32 = 8
)

type Mode string

type ComputePrecision string

type MemoryModel string

const (
	ModeStrict   Mode = "strict"
	ModeResearch Mode = "research"

	ComputePrecisionInt8 ComputePrecision = "int8"
	ComputePrecisionFP16 ComputePrecision = "fp16"

	MemoryModelAny           MemoryModel = "any"
	MemoryModelUnifiedShared MemoryModel = "unified-shared"
)

type Spec struct {
	Mode                   Mode
	DAGSizeBytes           uint64
	InitialDAGSizeBytes    uint64
	DAGGrowthBytesPerEpoch uint64
	NodeSize               uint64
	ReadsPerHash           uint64
	EpochBlocks            uint64
	TileSizeBytes          uint64
	MatDim                 uint32
	ComputeRounds          uint32
	ComputePrecision       ComputePrecision
	MemoryModelRequired    MemoryModel
	DeviceExecutionOnly    bool
	RoundCommitInterval    uint32
	AlgorithmVersion       uint32
}

func StrictSpec() Spec {
	return Spec{
		Mode:                   ModeStrict,
		DAGSizeBytes:           StrictInitialDAGSizeBytes,
		InitialDAGSizeBytes:    StrictInitialDAGSizeBytes,
		DAGGrowthBytesPerEpoch: DefaultDAGGrowthBytesPerEpoch,
		NodeSize:               StrictNodeSize,
		ReadsPerHash:           StrictReadsPerHash,
		EpochBlocks:            StrictEpochBlocks,
		TileSizeBytes:          StrictV2TileSizeBytes,
		MatDim:                 StrictV2MatDim,
		ComputeRounds:          StrictV2ComputeRounds,
		ComputePrecision:       ComputePrecisionInt8,
		MemoryModelRequired:    MemoryModelUnifiedShared,
		DeviceExecutionOnly:    true,
		RoundCommitInterval:    StrictV2RoundCommitPeriod,
		AlgorithmVersion:       2,
	}
}

func ResearchSpec(initialDAGSizeBytes, readsPerHash, epochBlocks uint64) Spec {
	return ResearchSpecWithGrowth(initialDAGSizeBytes, DefaultDAGGrowthBytesPerEpoch, readsPerHash, epochBlocks)
}

func ResearchSpecWithGrowth(initialDAGSizeBytes, growthBytesPerEpoch, readsPerHash, epochBlocks uint64) Spec {
	s := StrictSpec()
	s.Mode = ModeResearch
	s.TileSizeBytes = 0
	s.MatDim = 0
	s.ComputeRounds = 0
	s.ComputePrecision = ""
	s.MemoryModelRequired = MemoryModelAny
	s.DeviceExecutionOnly = false
	s.RoundCommitInterval = 0
	s.AlgorithmVersion = 1
	if initialDAGSizeBytes != 0 {
		s.InitialDAGSizeBytes = initialDAGSizeBytes
		s.DAGSizeBytes = initialDAGSizeBytes
	}
	if growthBytesPerEpoch != 0 {
		s.DAGGrowthBytesPerEpoch = growthBytesPerEpoch
	}
	if readsPerHash != 0 {
		s.ReadsPerHash = readsPerHash
	}
	if epochBlocks != 0 {
		s.EpochBlocks = epochBlocks
	}
	return s
}

func (s Spec) Validate() error {
	if s.Mode == "" {
		s.Mode = ModeStrict
	}
	initial := s.initialDAGSize()
	growth := s.growthDAGSizePerEpoch()
	if initial == 0 {
		return errors.New("initial dag size must be > 0")
	}
	if growth == 0 {
		return errors.New("dag growth per epoch must be > 0")
	}
	if s.NodeSize != StrictNodeSize {
		return fmt.Errorf("COLOSSUS-X requires %d-byte nodes", StrictNodeSize)
	}
	if s.ReadsPerHash == 0 {
		return errors.New("reads/hash must be > 0")
	}
	if s.EpochBlocks == 0 {
		return errors.New("epoch blocks must be > 0")
	}
	if initial%s.NodeSize != 0 {
		return fmt.Errorf("initial dag size must be multiple of node size (%d)", s.NodeSize)
	}
	if growth%s.NodeSize != 0 {
		return fmt.Errorf("dag growth per epoch must be multiple of node size (%d)", s.NodeSize)
	}
	switch s.Mode {
	case ModeStrict:
		if s.NodeSize != StrictNodeSize || s.ReadsPerHash != StrictReadsPerHash || s.EpochBlocks != StrictEpochBlocks {
			return fmt.Errorf("strict COLOSSUS-X mode requires NODE_SIZE=%d READS_PER_H=%d EPOCH_BLOCKS=%d", StrictNodeSize, StrictReadsPerHash, StrictEpochBlocks)
		}
		if s.AlgorithmVersion != 2 {
			return fmt.Errorf("strict mode requires algorithm version 2")
		}
		if s.MemoryModelRequired != MemoryModelUnifiedShared {
			return fmt.Errorf("strict mode requires unified-shared memory model")
		}
		if !s.DeviceExecutionOnly {
			return fmt.Errorf("strict mode requires device execution")
		}
		if s.TileSizeBytes == 0 || s.MatDim == 0 || s.ComputeRounds == 0 || s.RoundCommitInterval == 0 {
			return fmt.Errorf("strict mode requires strict-v2 tensor parameters")
		}
	case ModeResearch:
		if s.AlgorithmVersion == 0 {
			s.AlgorithmVersion = 1
		}
	default:
		return fmt.Errorf("unsupported mode %q", s.Mode)
	}
	return nil
}

func (s Spec) initialDAGSize() uint64 {
	if s.InitialDAGSizeBytes != 0 {
		return s.InitialDAGSizeBytes
	}
	return s.DAGSizeBytes
}

func (s Spec) growthDAGSizePerEpoch() uint64 {
	if s.DAGGrowthBytesPerEpoch != 0 {
		return s.DAGGrowthBytesPerEpoch
	}
	return DefaultDAGGrowthBytesPerEpoch
}

func (s Spec) DAGSizeForEpoch(epoch uint64) uint64 {
	initial := s.initialDAGSize()
	growth := s.growthDAGSizePerEpoch()
	if initial == 0 {
		return 0
	}
	if growth == 0 || epoch == 0 {
		return initial
	}
	if epoch > (math.MaxUint64-initial)/growth {
		return math.MaxUint64 - (math.MaxUint64 % s.NodeSize)
	}
	return initial + epoch*growth
}

func (s Spec) DAGSizeForHeight(height uint64) uint64 {
	if s.EpochBlocks == 0 {
		return s.DAGSizeForEpoch(0)
	}
	return s.DAGSizeForEpoch(height / s.EpochBlocks)
}

func (s Spec) ResolvedForHeight(height uint64) Spec {
	resolved := s
	resolved.DAGSizeBytes = s.DAGSizeForHeight(height)
	return resolved
}

func (s Spec) NodeCount() uint64 {
	size := s.DAGSizeBytes
	if size == 0 {
		size = s.DAGSizeForHeight(0)
	}
	if s.NodeSize == 0 {
		return 0
	}
	return size / s.NodeSize
}
