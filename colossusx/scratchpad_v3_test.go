package colossusx

import (
	"testing"

	"github.com/zeebo/blake3"
)

type v3TestAlloc struct{ buf []byte }

func (a *v3TestAlloc) Bytes() []byte { return a.buf }
func (a *v3TestAlloc) Free() error   { a.buf = nil; return nil }
func (a *v3TestAlloc) Name() string  { return "test-heap" }

type v3TestAllocator struct{}

func (v3TestAllocator) Alloc(size uint64) (Allocation, error) { return &v3TestAlloc{buf: make([]byte, size)}, nil }
func (v3TestAllocator) Name() string                          { return "test-heap" }

func TestAppendOnlySpecGrowthSchedule(t *testing.T) {
	spec := ColossusXSpecAppendOnly()
	base := spec.InitialDAGSizeBytes
	growth := spec.DAGGrowthBytesPerEpoch
	if got := spec.ScratchpadActiveSizeForHeight(0); got != base {
		t.Fatalf("height 0 size mismatch: got=%d want=%d", got, base)
	}
	if got := spec.ScratchpadActiveSizeForHeight(6899); got != base {
		t.Fatalf("height 6899 size mismatch: got=%d want=%d", got, base)
	}
	if got := spec.ScratchpadActiveSizeForHeight(6900); got <= base {
		t.Fatalf("height 6900 should begin growth: got=%d base=%d", got, base)
	}
	if got := spec.ScratchpadActiveSizeForHeight(7199); got != base+growth {
		t.Fatalf("height 7199 size mismatch: got=%d want=%d", got, base+growth)
	}
	if got := spec.ScratchpadActiveSizeForHeight(7200); got != base+growth {
		t.Fatalf("height 7200 size mismatch: got=%d want=%d", got, base+growth)
	}
}

func TestAppendOnlyScratchpadBuildHashAndProof(t *testing.T) {
	spec := ColossusXSpecAppendOnly()
	spec.InitialDAGSizeBytes = 4096 * 4
	spec.DAGSizeBytes = spec.InitialDAGSizeBytes
	spec.DAGGrowthBytesPerEpoch = 4096 * 2
	spec.EpochBlocks = 20
	spec.ReadsPerHash = 4
	seed := []byte("0123456789abcdef0123456789abcdef")
	dag, err := NewDAGWithAllocator(spec, v3TestAllocator{})
	if err != nil {
		t.Fatalf("NewDAGWithAllocator: %v", err)
	}
	defer dag.Close()
	if err := PopulateAppendOnlyScratchpadV3(dag, seed, [32]byte{}, 1); err != nil {
		t.Fatalf("PopulateAppendOnlyScratchpadV3: %v", err)
	}
	header := []byte("cx-sp-v3-test-header")
	nonce := NewUint64Nonce(7)
	trace := ColossusXTraceHashV3(spec, header, nonce, dag)
	if len(trace.Accessed) != int(spec.ReadsPerHash) {
		t.Fatalf("trace accessed mismatch: got=%d want=%d", len(trace.Accessed), spec.ReadsPerHash)
	}
	target, _ := ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	solution, root, err := BuildColossusXSolutionStreamingV3(spec, header, 7, dag)
	if err != nil {
		t.Fatalf("BuildColossusXSolutionStreamingV3: %v", err)
	}
	if err := VerifyColossusXSolutionV3(spec, header, target, root, solution); err != nil {
		t.Fatalf("VerifyColossusXSolutionV3: %v", err)
	}
	sidecar, err := NewMerkleSidecarFromAccessor(dag, spec.NodeSize)
	if err != nil {
		t.Fatalf("NewMerkleSidecarFromAccessor: %v", err)
	}
	if got := sidecar.Root(); got != root {
		t.Fatalf("sidecar root mismatch")
	}
	proof, err := sidecar.Proof(uint64(solution.MiningCells[0].Index))
	if err != nil {
		t.Fatalf("sidecar proof: %v", err)
	}
	leaf := blake3.Sum256(solution.MiningCells[0].Data)
	if !VerifyMerkleProof(root, leaf, int(solution.MiningCells[0].Index), proof) {
		t.Fatalf("sidecar proof verification failed")
	}
}
