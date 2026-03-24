package colossusx

import (
	"bytes"
	"testing"
)

type sliceAccessor struct {
	spec Spec
	buf  []byte
}

func (a sliceAccessor) NodeCount() uint64 { return a.spec.NodeCount() }
func (a sliceAccessor) ReadNode(i uint64, out *[64]byte) {
	off := i * a.spec.NodeSize
	copy(out[:], a.buf[off:off+a.spec.NodeSize])
}

func testSpec() Spec {
	return ResearchSpec(64*16, 8, StrictEpochBlocks)
}

func TestGenerateDAGDeterministic(t *testing.T) {
	spec := testSpec()
	seed := []byte("0123456789abcdef0123456789abcdef")
	left := make([]byte, spec.DAGSizeBytes)
	right := make([]byte, spec.DAGSizeBytes)

	if err := GenerateDAG(spec, left, seed, 2); err != nil {
		t.Fatalf("GenerateDAG left: %v", err)
	}
	if err := GenerateDAG(spec, right, seed, 3); err != nil {
		t.Fatalf("GenerateDAG right: %v", err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("expected identical DAG output for the same seed")
	}
}

func TestGenerateDAGDiffersAcrossSeeds(t *testing.T) {
	spec := testSpec()
	left := make([]byte, spec.DAGSizeBytes)
	right := make([]byte, spec.DAGSizeBytes)
	if err := GenerateDAG(spec, left, []byte("0123456789abcdef0123456789abcdef"), 2); err != nil {
		t.Fatalf("GenerateDAG left: %v", err)
	}
	if err := GenerateDAG(spec, right, []byte("fedcba9876543210fedcba9876543210"), 2); err != nil {
		t.Fatalf("GenerateDAG right: %v", err)
	}
	if bytes.Equal(left, right) {
		t.Fatal("expected different DAG output for different seeds")
	}
}

func TestLatticeHashDeterministic(t *testing.T) {
	spec := testSpec()
	seed := []byte("0123456789abcdef0123456789abcdef")
	dag := make([]byte, spec.DAGSizeBytes)
	if err := GenerateDAG(spec, dag, seed, 2); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	accessor := sliceAccessor{spec: spec, buf: dag}
	header := []byte("header")
	nonce := NewUint64Nonce(42)

	first := LatticeHash(spec, header, nonce, accessor, nil)
	second := LatticeHash(spec, header, nonce, accessor, nil)
	if first != second {
		t.Fatalf("expected deterministic lattice hash; first=%x second=%x", first.Pow256, second.Pow256)
	}

	thirdNonce, _ := nonce.AddUint64(1)
	third := LatticeHash(spec, header, thirdNonce, accessor, nil)
	if first == third {
		t.Fatal("expected nonce change to alter lattice hash")
	}
}

func TestLessOrEqualBETargetComparison(t *testing.T) {
	target, err := ParseTargetHex("00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatalf("ParseTargetHex: %v", err)
	}
	var lower [32]byte
	var equal [32]byte
	copy(equal[:], target[:])
	var higher [32]byte
	copy(higher[:], target[:])
	lower[31] = 1
	higher[0] = 0x01

	if !LessOrEqualBE(lower, target) {
		t.Fatal("expected lower digest to satisfy target")
	}
	if !LessOrEqualBE(equal, target) {
		t.Fatal("expected equal digest to satisfy target")
	}
	if LessOrEqualBE(higher, target) {
		t.Fatal("expected higher digest to fail target comparison")
	}
}

func TestStrictModeDynamicDAGProfile(t *testing.T) {
	strict := StrictSpec()
	if err := strict.Validate(); err != nil {
		t.Fatalf("StrictSpec should validate: %v", err)
	}
	if strict.InitialDAGSizeBytes != 8*1024*1024*1024 {
		t.Fatalf("expected strict initial DAG size 8GiB, got %d", strict.InitialDAGSizeBytes)
	}
	if strict.DAGGrowthBytesPerEpoch != 512*1024*1024 {
		t.Fatalf("expected strict DAG growth 512MiB, got %d", strict.DAGGrowthBytesPerEpoch)
	}
	if strict.DAGSizeForHeight(strict.EpochBlocks) <= strict.DAGSizeForHeight(0) {
		t.Fatal("expected strict DAG size to grow after an epoch")
	}
}

func TestGenerateDAGUsesKeccak512(t *testing.T) {
	spec := testSpec()
	seed := []byte("0123456789abcdef0123456789abcdef")
	dag := make([]byte, spec.DAGSizeBytes)
	if err := GenerateDAG(spec, dag, seed, 1); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	want := keccak512(append(append([]byte{}, seed...), make([]byte, 8)...))
	if got := dag[:64]; !bytes.Equal(got, want[:]) {
		t.Fatalf("node 0 mismatch: got=%x want=%x", got, want[:])
	}
}

func TestBlake3RoundInputUsesBothNodeHalves(t *testing.T) {
	var mix [32]byte
	var node [64]byte
	for i := range mix {
		mix[i] = byte(i + 1)
	}
	for i := range node {
		node[i] = byte(255 - i)
	}
	in := blake3RoundInput(mix, node)
	for i := 0; i < 32; i++ {
		if in[i] != mix[i]^node[i] {
			t.Fatalf("first half mismatch at %d", i)
		}
		if in[32+i] != mix[i]^node[32+i] {
			t.Fatalf("second half mismatch at %d", i)
		}
	}
}
