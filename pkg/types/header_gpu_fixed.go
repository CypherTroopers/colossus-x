package types

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// GPUFixedMiningHeaderV1 is a fixed-length proposal for GPU/device execution.
// It is intentionally NOT consensus-active yet. CPU and device paths must be
// switched together before it can replace EncodeForMining.
//
// Layout size: 256 bytes exactly.
// Nonce is not included here; device kernels append the nonce separately.
// That means kernels using this proposal need an input buffer >= 264 bytes.
type GPUFixedMiningHeaderV1 struct {
	Version          uint32
	AlgorithmVersion uint32
	Height           uint64
	ParentHash       Hash
	Timestamp        uint64
	Target           [32]byte
	CoinbaseHash     [32]byte
	EpochSeed        Hash
	DAGSizeBytes     uint64
	DAGMerkleRoot    Hash
	TxRoot           Hash
	StateRoot        Hash
}

const GPUFixedMiningHeaderV1Size = 256

func (h BlockHeader) GPUFixedMiningHeaderV1() GPUFixedMiningHeaderV1 {
	return GPUFixedMiningHeaderV1{
		Version:          h.Version,
		AlgorithmVersion: h.AlgorithmVersion,
		Height:           h.Height,
		ParentHash:       h.ParentHash,
		Timestamp:        uint64(h.Timestamp),
		Target:           h.Target,
		CoinbaseHash:     sha3.Sum256([]byte(h.Coinbase)),
		EpochSeed:        h.EpochSeed,
		DAGSizeBytes:     h.DAGSizeBytes,
		DAGMerkleRoot:    h.DAGMerkleRoot,
		TxRoot:           h.TxRoot,
		StateRoot:        h.StateRoot,
	}
}

func (h GPUFixedMiningHeaderV1) Encode() [GPUFixedMiningHeaderV1Size]byte {
	var out [GPUFixedMiningHeaderV1Size]byte
	off := 0
	binary.BigEndian.PutUint32(out[off:], h.Version)
	off += 4
	binary.BigEndian.PutUint32(out[off:], h.AlgorithmVersion)
	off += 4
	binary.BigEndian.PutUint64(out[off:], h.Height)
	off += 8
	copy(out[off:], h.ParentHash[:])
	off += 32
	binary.BigEndian.PutUint64(out[off:], h.Timestamp)
	off += 8
	copy(out[off:], h.Target[:])
	off += 32
	copy(out[off:], h.CoinbaseHash[:])
	off += 32
	copy(out[off:], h.EpochSeed[:])
	off += 32
	binary.BigEndian.PutUint64(out[off:], h.DAGSizeBytes)
	off += 8
	copy(out[off:], h.DAGMerkleRoot[:])
	off += 32
	copy(out[off:], h.TxRoot[:])
	off += 32
	copy(out[off:], h.StateRoot[:])
	return out
}

func (h BlockHeader) EncodeForMiningGPUFixedV1() [GPUFixedMiningHeaderV1Size]byte {
	return h.GPUFixedMiningHeaderV1().Encode()
}
