package chain

import (
	"math/big"
	"testing"

	cx "colossusx/colossusx"
	"colossusx/pkg/types"
)

func TestDiskStorePersistsBlocksAndTip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("new disk store: %v", err)
	}
	target, err := cx.ParseTargetHex("0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	block := types.NewGenesisBlock(types.GenesisConfig{
		ChainID:   "testnet",
		Message:   "genesis",
		Timestamp: 1,
		Bits:      target,
		Spec:      cx.ResearchSpec(8*1024*1024, 32, 32),
	})
	work := big.NewInt(123)
	if err := store.StoreBlock(block, work); err != nil {
		t.Fatalf("store block: %v", err)
	}
	if err := store.SetCurrentTip(block.BlockHash()); err != nil {
		t.Fatalf("set tip: %v", err)
	}

	reopened, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("reopen disk store: %v", err)
	}
	loaded, loadedWork, err := reopened.CurrentTip()
	if err != nil {
		t.Fatalf("current tip: %v", err)
	}
	if loaded.BlockHash() != block.BlockHash() {
		t.Fatalf("wrong tip hash: got %s want %s", loaded.BlockHash(), block.BlockHash())
	}
	if loadedWork.Cmp(work) != 0 {
		t.Fatalf("wrong total work: got %s want %s", loadedWork, work)
	}
}
