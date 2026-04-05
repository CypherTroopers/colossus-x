package colossusx

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/sha3"
)

const (
	ColossusXAlgorithmVersionScratchpad uint32 = 3

	ColossusXScratchpadBaseSizeBytes   uint64 = 32 * 1024 * 1024 * 1024
	ColossusXScratchpadGrowthBytes     uint64 = 48 * 1024 * 1024
	ColossusXScratchpadGrowthWindow    uint64 = 300
	ColossusXScratchpadReadsPerHash    uint64 = 64
	ColossusXScratchpadPrefixParents   uint32 = 6
	ColossusXScratchpadSuffixParents   uint32 = 2
	ColossusXScratchpadParentCount     uint32 = ColossusXScratchpadPrefixParents + ColossusXScratchpadSuffixParents
)

func ColossusXSpecAppendOnly() Spec {
	s := ColossusXSpec()
	s.InitialDAGSizeBytes = ColossusXScratchpadBaseSizeBytes
	s.DAGSizeBytes = s.InitialDAGSizeBytes
	s.DAGGrowthBytesPerEpoch = ColossusXScratchpadGrowthBytes
	s.ReadsPerHash = ColossusXScratchpadReadsPerHash
	s.AlgorithmVersion = ColossusXAlgorithmVersionScratchpad
	return s
}

func (s Spec) IsAppendOnlyScratchpad() bool {
	return s.Mode == ModeColossusX && s.AlgorithmVersion >= ColossusXAlgorithmVersionScratchpad
}

func (s Spec) ScratchpadGrowthWindowBlocks() uint64 {
	if !s.IsAppendOnlyScratchpad() || s.EpochBlocks == 0 {
		return 0
	}
	if s.EpochBlocks < ColossusXScratchpadGrowthWindow {
		return s.EpochBlocks
	}
	return ColossusXScratchpadGrowthWindow
}

func (s Spec) ScratchpadGrowthStartOffset() uint64 {
	window := s.ScratchpadGrowthWindowBlocks()
	if window == 0 || s.EpochBlocks <= window {
		return 0
	}
	return s.EpochBlocks - window
}

func (s Spec) ScratchpadGrowthTilesPerCycle() uint64 {
	tileBytes := s.TileSizeBytes
	if tileBytes == 0 {
		tileBytes = ColossusXTileSizeBytes
	}
	if tileBytes == 0 {
		return 0
	}
	return s.growthDAGSizePerEpoch() / tileBytes
}

func (s Spec) ScratchpadActiveGrowthTilesAtHeight(height uint64) uint64 {
	if !s.IsAppendOnlyScratchpad() || s.EpochBlocks == 0 {
		return 0
	}
	offset := height % s.EpochBlocks
	start := s.ScratchpadGrowthStartOffset()
	if offset < start {
		return 0
	}
	window := s.ScratchpadGrowthWindowBlocks()
	if window == 0 {
		return 0
	}
	steps := offset - start + 1
	tiles := s.ScratchpadGrowthTilesPerCycle()
	return (steps * tiles) / window
}

func (s Spec) ScratchpadActiveSizeForHeight(height uint64) uint64 {
	if !s.IsAppendOnlyScratchpad() {
		return s.DAGSizeForEpoch(height / max64(1, s.EpochBlocks))
	}
	base := s.initialDAGSize()
	growth := s.growthDAGSizePerEpoch()
	if s.EpochBlocks == 0 {
		return base
	}
	cycle := height / s.EpochBlocks
	if cycle > 0 {
		if cycle > (math.MaxUint64-base)/growth {
			return math.MaxUint64 - (math.MaxUint64 % s.NodeSize)
		}
		base += cycle * growth
	}
	tileBytes := s.TileSizeBytes
	if tileBytes == 0 {
		tileBytes = ColossusXTileSizeBytes
	}
	return base + s.ScratchpadActiveGrowthTilesAtHeight(height)*tileBytes
}

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

type MerkleSidecar struct {
	levels [][][32]byte
}

func NewMerkleSidecarFromAccessor(accessor DAGAccessor, nodeSize uint64) (*MerkleSidecar, error) {
	if accessor == nil || accessor.NodeCount() == 0 {
		return nil, fmt.Errorf("scratchpad is empty")
	}
	if nodeSize == 0 {
		return nil, fmt.Errorf("node size must be > 0")
	}
	leaves := make([][32]byte, accessor.NodeCount())
	cell := make([]byte, nodeSize)
	for i := uint64(0); i < accessor.NodeCount(); i++ {
		accessor.ReadNode(i, cell)
		leaves[i] = blake3.Sum256(cell)
	}
	return &MerkleSidecar{levels: buildMerkleLevels(leaves)}, nil
}

func (m *MerkleSidecar) Root() [32]byte {
	if m == nil || len(m.levels) == 0 {
		return [32]byte{}
	}
	top := m.levels[len(m.levels)-1]
	if len(top) == 0 {
		return [32]byte{}
	}
	return top[0]
}

func (m *MerkleSidecar) Proof(index uint64) (MerkleProof, error) {
	if m == nil || len(m.levels) == 0 {
		return nil, fmt.Errorf("merkle sidecar is empty")
	}
	if index >= uint64(len(m.levels[0])) {
		return nil, fmt.Errorf("proof index %d out of range", index)
	}
	proof := make(MerkleProof, 0, len(m.levels)-1)
	idx := int(index)
	for level := 0; level < len(m.levels)-1; level++ {
		nodes := m.levels[level]
		sibling := idx ^ 1
		if sibling >= len(nodes) {
			sibling = idx
		}
		proof = append(proof, nodes[sibling])
		idx /= 2
	}
	return proof, nil
}

func PopulateAppendOnlyScratchpadV3(dag *DAG, epochSeed []byte, prevRoot [32]byte, workers int) error {
	return PopulateAppendOnlyScratchpadV3Range(dag, epochSeed, prevRoot, 0, dag.NodeCount(), workers, nil)
}

func PopulateAppendOnlyScratchpadV3Range(dag *DAG, epochSeed []byte, prevRoot [32]byte, startCell, endCell uint64, workers int, progress func(done, total uint64)) error {
	if dag == nil {
		return fmt.Errorf("dag cannot be nil")
	}
	if !dag.Spec().IsAppendOnlyScratchpad() {
		return fmt.Errorf("append-only scratchpad requires algorithm_version >= %d", ColossusXAlgorithmVersionScratchpad)
	}
	if len(epochSeed) == 0 {
		return fmt.Errorf("epoch seed cannot be empty")
	}
	if endCell > dag.NodeCount() {
		return fmt.Errorf("cell range [%d,%d) exceeds scratchpad node count %d", startCell, endCell, dag.NodeCount())
	}
	if startCell > endCell {
		return fmt.Errorf("invalid cell range [%d,%d)", startCell, endCell)
	}
	_ = workers // reserved for future tile-parallel generator
	if progress != nil {
		progress(0, endCell-startCell)
	}
	buf := dag.Bytes()
	nodeSize := dag.Spec().NodeSize
	cell := make([]byte, nodeSize)
	for i := startCell; i < endCell; i++ {
		generateAppendOnlyScratchpadCellV3(buf, dag.Spec(), epochSeed, prevRoot, startCell, i, cell)
		copy(buf[i*nodeSize:(i+1)*nodeSize], cell)
		if progress != nil {
			progress(i-startCell+1, endCell-startCell)
		}
	}
	return nil
}

func generateAppendOnlyScratchpadCellV3(buf []byte, spec Spec, epochSeed []byte, prevRoot [32]byte, growthStartCell, cellIndex uint64, out []byte) {
	for i := range out {
		out[i] = 0
	}
	var seedInput [32 + 32 + 8]byte
	copy(seedInput[:32], prevRoot[:])
	copy(seedInput[32:64], epochSeed)
	binary.LittleEndian.PutUint64(seedInput[64:], cellIndex)
	mix := sha3.Sum512(seedInput[:])
	parent := make([]byte, spec.NodeSize)
	prefixCount := growthStartCell
	suffixCount := uint64(0)
	if cellIndex > growthStartCell {
		suffixCount = cellIndex - growthStartCell
	}
	for parentNo := uint32(0); parentNo < ColossusXScratchpadParentCount; parentNo++ {
		var sourceIndex uint64
		switch {
		case parentNo < ColossusXScratchpadPrefixParents && prefixCount > 0:
			sourceIndex = scratchpadSampleIndexV3(mix, cellIndex, parentNo, prefixCount)
		case suffixCount > 0:
			sourceIndex = growthStartCell + scratchpadSampleIndexV3(mix, cellIndex, parentNo, suffixCount)
		case prefixCount > 0:
			sourceIndex = scratchpadSampleIndexV3(mix, cellIndex, parentNo, prefixCount)
		default:
			continue
		}
		readNodeRawV3(buf, spec.NodeSize, sourceIndex, parent)
		mix = colossusXRoundFold(mix, parent)
		mix = sha3.Sum512(mix[:])
	}
	xof := blake3.New()
	_, _ = xof.Write([]byte("cx-sp-v3-cell"))
	_, _ = xof.Write(prevRoot[:])
	_, _ = xof.Write(epochSeed)
	var idx [8]byte
	binary.LittleEndian.PutUint64(idx[:], cellIndex)
	_, _ = xof.Write(idx[:])
	_, _ = xof.Write(mix[:])
	_, _ = io.ReadFull(xof.Digest(), out)
}

func scratchpadSampleIndexV3(mix [64]byte, cellIndex uint64, parentNo uint32, modulo uint64) uint64 {
	if modulo == 0 {
		return 0
	}
	var in [64 + 8 + 4]byte
	copy(in[:64], mix[:])
	binary.LittleEndian.PutUint64(in[64:], cellIndex)
	binary.LittleEndian.PutUint32(in[72:], parentNo)
	sum := sha3.Sum256(in[:])
	return binary.LittleEndian.Uint64(sum[:8]) % modulo
}

func readNodeRawV3(buf []byte, nodeSize uint64, index uint64, out []byte) {
	off := index * nodeSize
	copy(out, buf[off:off+nodeSize])
}
