package types

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// GPUFixedMiningHeaderV1 is the canonical fixed-length mining header encoding
// used by CPU, validator, and device execution paths.
//
// Layout size: 256 bytes exactly.
// Nonce is appended separately by mining backends/device kernels, so the full
// hashing input size is 264 bytes.
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
