package colossusx

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestHashHeaderStatelessMatchesMaterializedResearchDAG(t *testing.T) {
	spec := ResearchSpec(64*16, 8, 32)
	seed := []byte("0123456789abcdef0123456789abcdef")
	header := []byte("external-verifier-header")
	nonce := NewUint64Nonce(42)

	buf := make([]byte, spec.DAGSizeBytes)
	if err := GenerateDAG(spec, buf, seed, 2); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	materialized := sliceAccessor{spec: spec, buf: buf}
	want := LatticeHash(spec, header, nonce, materialized, nil)

	got, err := HashHeaderStateless(spec, header, nonce, seed)
	if err != nil {
		t.Fatalf("HashHeaderStateless: %v", err)
	}
	if got != want {
		t.Fatalf("stateless hash mismatch\n got=%s\nwant=%s", hex.EncodeToString(got.Pow256[:]), hex.EncodeToString(want.Pow256[:]))
	}
}

func TestStatelessDAGReadNodeMatchesGenerateDAGResearchNode(t *testing.T) {
	spec := ResearchSpec(64*4, 4, 16)
	seed := []byte("fedcba9876543210fedcba9876543210")
	buf := make([]byte, spec.DAGSizeBytes)
	if err := GenerateDAG(spec, buf, seed, 1); err != nil {
		t.Fatalf("GenerateDAG: %v", err)
	}
	dag, err := NewStatelessDAG(spec, seed)
	if err != nil {
		t.Fatalf("NewStatelessDAG: %v", err)
	}
	for i := uint64(0); i < spec.NodeCount(); i++ {
		var got [64]byte
		dag.ReadNode(i, &got)
		off := i * spec.NodeSize
		if want := buf[off : off+spec.NodeSize]; !bytes.Equal(got[:], want) {
			t.Fatalf("node %d mismatch", i)
		}
	}
}

func TestVerifyHeaderStatelessChecksTarget(t *testing.T) {
	spec := ResearchSpec(64*16, 8, 32)
	seed := []byte("0123456789abcdef0123456789abcdef")
	header := []byte("verify-target")
	nonce := NewUint64Nonce(7)

	hash, err := HashHeaderStateless(spec, header, nonce, seed)
	if err != nil {
		t.Fatalf("HashHeaderStateless: %v", err)
	}
	var exact Target
	copy(exact[:], hash.Pow256[:])
	if _, ok, err := VerifyHeaderStateless(spec, header, nonce, seed, exact); err != nil {
		t.Fatalf("VerifyHeaderStateless exact target: %v", err)
	} else if !ok {
		t.Fatal("expected exact target to validate")
	}

	var lower Target
	copy(lower[:], hash.Pow256[:])
	lower[31]--
	if _, ok, err := VerifyHeaderStateless(spec, header, nonce, seed, lower); err != nil {
		t.Fatalf("VerifyHeaderStateless lower target: %v", err)
	} else if ok {
		t.Fatal("expected lower target to fail")
	}
}

func TestHashHeaderStatelessSupportsDifferentResolvedSizesAcrossEpochs(t *testing.T) {
	spec := ResearchSpecWithGrowth(64*16, 64, 8, 8)
	seedEpoch0 := []byte("0123456789abcdef0123456789abcdef")
	seedEpoch1 := []byte("fedcba9876543210fedcba9876543210")
	header := []byte("external-verifier-header")
	nonce := NewUint64Nonce(42)

	resolved0 := spec.ResolvedForHeight(0)
	resolved1 := spec.ResolvedForHeight(8)
	if resolved1.DAGSizeBytes <= resolved0.DAGSizeBytes {
		t.Fatal("expected DAG to grow across epochs")
	}
	if _, err := HashHeaderStateless(resolved0, header, nonce, seedEpoch0); err != nil {
		t.Fatalf("HashHeaderStateless epoch0: %v", err)
	}
	if _, err := HashHeaderStateless(resolved1, header, nonce, seedEpoch1); err != nil {
		t.Fatalf("HashHeaderStateless epoch1: %v", err)
	}
}
