package colossusx

import "testing"

type testBatchBackend struct {
	mode       BackendMode
	hashCalls  int
	batchCalls int
	targetHit  uint64
}

func (b *testBatchBackend) Mode() BackendMode   { return b.mode }
func (b *testBatchBackend) Description() string { return "test batch backend" }
func (b *testBatchBackend) Prepare(*DAG) error  { return nil }

func (b *testBatchBackend) Hash(_ []byte, nonce Nonce, _ *DAG) HashResult {
	b.hashCalls++
	var out HashResult
	if n64, ok := nonce.(Uint64Nonce); ok && uint64(n64) == b.targetHit {
		return out
	}
	for i := range out.Pow256 {
		out.Pow256[i] = 0xff
	}
	for i := range out.Full512 {
		out.Full512[i] = 0xff
	}
	return out
}

func (b *testBatchBackend) HashBatch(_ []byte, startNonce Nonce, count uint64, _ *DAG) ([]HashResult, error) {
	b.batchCalls++
	out := make([]HashResult, count)
	start, ok := startNonce.(Uint64Nonce)
	if !ok {
		return out, nil
	}
	for i := uint64(0); i < count; i++ {
		if uint64(start)+i == b.targetHit {
			continue
		}
		for j := range out[i].Pow256 {
			out[i].Pow256[j] = 0xff
		}
		for j := range out[i].Full512 {
			out[i].Full512[j] = 0xff
		}
	}
	return out, nil
}

func tinyMinerSpec() Spec {
	return ColossusXSpecWithGrowth(ColossusXNodeSize*4, ColossusXNodeSize)
}

func allFFTarget() Target {
	var target Target
	for i := range target {
		target[i] = 0xff
	}
	return target
}

func TestMineUsesWorkerPathForCPUModeEvenWhenBatchSupported(t *testing.T) {
	backend := &testBatchBackend{mode: BackendCPU, targetHit: 0}
	miner, err := NewMiner(tinyMinerSpec(), nil, 2, backend)
	if err != nil {
		t.Fatalf("NewMiner: %v", err)
	}

	res, ok := miner.Mine([]byte("header"), allFFTarget(), NewUint64Nonce(0), 16)
	if !ok {
		t.Fatal("expected mining to succeed through the worker hash path")
	}
	if backend.batchCalls != 0 {
		t.Fatalf("expected batch path to be skipped for cpu mode, batchCalls=%d", backend.batchCalls)
	}
	if backend.hashCalls == 0 {
		t.Fatal("expected direct Hash calls for cpu mode")
	}
	if got := res.Nonce.(Uint64Nonce).Uint64(); got != 0 {
		t.Fatalf("unexpected nonce: got %d want 0", got)
	}
}

func TestMineBatchTreatsZeroMaxNoncesAsUnbounded(t *testing.T) {
	const targetNonce = 100005

	backend := &testBatchBackend{mode: BackendOpenCL, targetHit: targetNonce}
	miner, err := NewMiner(tinyMinerSpec(), nil, 1, backend)
	if err != nil {
		t.Fatalf("NewMiner: %v", err)
	}

	var target Target
	res, ok := miner.Mine([]byte("header"), target, NewUint64Nonce(0), 0)
	if !ok {
		t.Fatal("expected unbounded batch mining to continue until the target nonce was found")
	}
	if backend.batchCalls < 2 {
		t.Fatalf("expected multiple batch dispatches past the old 100000 nonce limit, batchCalls=%d", backend.batchCalls)
	}
	if got := res.Nonce.(Uint64Nonce).Uint64(); got != targetNonce {
		t.Fatalf("unexpected nonce: got %d want %d", got, targetNonce)
	}
	if res.Hashes != targetNonce+1 {
		t.Fatalf("unexpected hash count: got %d want %d", res.Hashes, targetNonce+1)
	}
}
