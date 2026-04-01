package node

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/consensus"
	"colossusx/pkg/p2p"
	"colossusx/pkg/types"
)

func TestOnStatusSendsMissingBlocksToBehindPeer(t *testing.T) {
	spec := cx.ColossusXSpecWithGrowth(1024*1024, cx.DefaultDAGGrowthBytesPerEpoch)
	target, err := cx.ParseTargetHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	chainCfg := types.ChainConfig{NetworkID: "test", Spec: spec}
	genesisCfg := types.GenesisConfig{ChainID: "test", Message: "genesis", Timestamp: time.Now().Unix() - 10, Bits: target, Spec: spec}
	validator, err := consensus.NewValidator(chainCfg, consensus.CPUBackend{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer validator.Close()

	store := chain.NewMemoryStore()
	node, err := New(Config{Chain: chainCfg, Genesis: genesisCfg, NodeID: "node-a"}, validator, store)
	if err != nil {
		t.Fatal(err)
	}

	genesis := types.NewGenesisBlock(genesisCfg)
	if err := store.StoreBlock(genesis, big.NewInt(1)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentTip(genesis.BlockHash()); err != nil {
		t.Fatal(err)
	}
	block1 := types.Block{Header: types.BlockHeader{
		Version:          1,
		AlgorithmVersion: genesis.Header.AlgorithmVersion,
		Height:           1,
		ParentHash:       genesis.BlockHash(),
		Timestamp:        genesis.Header.Timestamp + 1,
		Target:           target,
		EpochSeed:        genesis.Header.EpochSeed,
		DAGSizeBytes:     genesis.Header.DAGSizeBytes,
	}}
	if err := store.StoreBlock(block1, big.NewInt(2)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentTip(block1.BlockHash()); err != nil {
		t.Fatal(err)
	}

	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	peer := &p2p.Peer{ID: "node-b", Conn: local}

	done := make(chan p2p.Message, 1)
	go func() {
		msg, _ := readFramedMessage(remote)
		done <- msg
	}()

	node.onStatus(peer, p2p.StatusMessage{
		Status: types.PeerStatus{
			PeerID:     "node-b",
			BestHeight: 0,
		},
	})

	select {
	case msg := <-done:
		if msg.Type != p2p.MessageNewBlk {
			t.Fatalf("expected %q, got %q", p2p.MessageNewBlk, msg.Type)
		}
		payload, err := json.Marshal(msg.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body p2p.NewBlockMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		if body.Block.Header.Height != 1 {
			t.Fatalf("expected synced block height 1, got %d", body.Block.Header.Height)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync block message")
	}
}

func readFramedMessage(r io.Reader) (p2p.Message, error) {
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return p2p.Message{}, err
	}
	size := binary.BigEndian.Uint32(sizeBuf[:])
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return p2p.Message{}, err
	}
	var msg p2p.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return p2p.Message{}, err
	}
	return msg, nil
}
