package types

import "testing"

func TestApplyTransactionsAndRoots(t *testing.T) {
	parent := map[string]AccountState{
		"alice": {Balance: 100, Nonce: 0},
	}
	txs := []Transaction{{From: "alice", To: "bob", Value: 25, Nonce: 0}}
	state, err := ApplyTransactions(parent, txs, "miner-1", 50)
	if err != nil {
		t.Fatalf("ApplyTransactions: %v", err)
	}
	if state["alice"].Balance != 75 || state["alice"].Nonce != 1 {
		t.Fatalf("unexpected alice state: %#v", state["alice"])
	}
	if state["bob"].Balance != 25 {
		t.Fatalf("unexpected bob state: %#v", state["bob"])
	}
	if state["miner-1"].Balance != 50 {
		t.Fatalf("unexpected miner state: %#v", state["miner-1"])
	}
	if ComputeTxRoot(txs) == (Hash{}) {
		t.Fatal("expected non-zero tx root")
	}
	if ComputeStateRoot(state) == (Hash{}) {
		t.Fatal("expected non-zero state root")
	}
}
