package colossusx

import "testing"

type memoryAccessor struct {
	spec Spec
	buf  []byte
}

func (m memoryAccessor) NodeCount() uint64 { return m.spec.NodeCount() }
func (m memoryAccessor) ReadNode(i uint64, out []byte) {
	off := i * m.spec.NodeSize
	copy(out, m.buf[off:off+m.spec.NodeSize])
}

func TestBuildAndVerifyColossusXSolution(t *testing.T) {
	spec := ColossusXSpec()
	spec.InitialDAGSizeBytes = 256 * 16
	spec.DAGSizeBytes = spec.InitialDAGSizeBytes
	spec.DAGGrowthBytesPerEpoch = 256
	seed := []byte("0123456789abcdef0123456789abcdef")
	buf := make([]byte, spec.DAGSizeBytes)
	if err := GenerateDAG(spec, buf, seed, 1); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	cells := make([][]byte, spec.NodeCount())
	for i := range cells {
		off := uint64(i) * spec.NodeSize
		cells[i] = append([]byte(nil), buf[off:off+spec.NodeSize]...)
	}
	leaves := BuildMerkleLeaves(cells)
	root := BuildMerkleRoot(leaves)
	accessor := memoryAccessor{spec: spec, buf: buf}
	header := []byte("colossusx-solution-header")
	target, _ := ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	solution, _, err := BuildColossusXSolution(spec, header, 1, accessor, leaves)
	if err != nil {
		t.Fatalf("BuildColossusXSolution: %v", err)
	}
	if err := VerifyColossusXSolution(spec, header, target, root, solution); err != nil {
		t.Fatalf("VerifyColossusXSolution: %v", err)
	}
	compact := CompactColossusXSolution(solution)
	expanded, err := ExpandCompactColossusXSolution(compact)
	if err != nil {
		t.Fatalf("ExpandCompactColossusXSolution: %v", err)
	}
	if err := VerifyColossusXSolution(spec, header, target, root, expanded); err != nil {
		t.Fatalf("VerifyColossusXSolution compact-expanded: %v", err)
	}
}
