package types

import (
	"testing"

	cx "colossusx/colossusx"
)

func TestHeaderEncodingDeterministic(t *testing.T) {
	target, err := cx.ParseTargetHex("0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	spec := cx.ResearchSpecWithGrowth(8*1024*1024, 64*1024, 8, 16)
	h := BlockHeader{Version: 1, Height: 7, Timestamp: 42, Target: target, Nonce: 9, EpochSeed: EpochSeedForHeight(spec, 7), DAGSizeBytes: spec.DAGSizeForHeight(7)}
	if got, want := string(h.Encode()), string(h.Encode()); got != want {
		t.Fatalf("encoding not stable")
	}
	if h.HeaderHash() != h.HeaderHash() {
		t.Fatalf("header hash not stable")
	}
}

func TestDAGSizeForHeightGrowthBoundaries(t *testing.T) {
	spec := cx.ResearchSpecWithGrowth(8*1024*1024, 512*1024, 8, 16)
	cases := []struct {
		height uint64
		want   uint64
	}{
		{0, 8 * 1024 * 1024},
		{15, 8 * 1024 * 1024},
		{16, 8*1024*1024 + 512*1024},
		{32, 8*1024*1024 + 2*512*1024},
	}
	for _, tc := range cases {
		if got := spec.DAGSizeForHeight(tc.height); got != tc.want {
			t.Fatalf("height %d: got %d want %d", tc.height, got, tc.want)
		}
	}
}

func TestEpochSeedForHeightUsesResolvedDAGSize(t *testing.T) {
	spec := cx.ResearchSpecWithGrowth(8*1024*1024, 512*1024, 8, 16)
	seedSameEpochA := EpochSeedForHeight(spec, 1)
	seedSameEpochB := EpochSeedForHeight(spec, 15)
	if seedSameEpochA != seedSameEpochB {
		t.Fatal("expected same epoch seed within same epoch")
	}
	seedNextEpoch := EpochSeedForHeight(spec, 16)
	if seedSameEpochA == seedNextEpoch {
		t.Fatal("expected epoch seed to change at epoch boundary")
	}
}

func TestDAGSizeForEpochOverflowDoesNotPanic(t *testing.T) {
	spec := cx.ResearchSpecWithGrowth(8*1024*1024, 512*1024, 8, 16)
	_ = spec.DAGSizeForEpoch(^uint64(0))
}
