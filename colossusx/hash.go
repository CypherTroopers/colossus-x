package colossusx

import (
	"encoding/binary"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/sha3"
)

type HashResult struct {
	Pow256  [32]byte
	Full512 [64]byte
}

type DAGAccessor interface {
	NodeCount() uint64
	ReadNode(uint64, *[64]byte)
}

type HashScratch struct {
	seedInput  []byte
	finalInput [96]byte
	fnvInput   [40]byte
	blakeInput [64]byte
}

func NewHashScratch(headerLen int) *HashScratch {
	return &HashScratch{seedInput: make([]byte, 0, headerLen+8)}
}

func EnsureSeedInput(s *HashScratch, headerLen int, nonce Nonce) {
	need := headerLen + 8
	if nonce != nil {
		buf := make([]byte, 0, headerLen+32)
		buf = nonce.AppendTo(buf)
		if len(buf)+headerLen > need {
			need = headerLen + len(buf)
		}
	}
	if cap(s.seedInput) < need {
		s.seedInput = make([]byte, 0, need)
		return
	}
	s.seedInput = s.seedInput[:0]
}

func LatticeHash(spec Spec, header []byte, nonce Nonce, accessor DAGAccessor, scratch *HashScratch) HashResult {
	if spec.AlgorithmVersion >= 2 {
		if dag, ok := accessor.(*DAG); ok {
			return StrictV2Hash(spec, header, nonce, tensorView{dag: dag})
		}
		if dag, ok := accessor.(interface{ UnderlyingDAG() *DAG }); ok && dag.UnderlyingDAG() != nil {
			return StrictV2Hash(spec, header, nonce, tensorView{dag: dag.UnderlyingDAG()})
		}
	}

	var out HashResult
	if accessor == nil || accessor.NodeCount() == 0 {
		return out
	}
	if scratch == nil {
		scratch = NewHashScratch(len(header))
	}
	EnsureSeedInput(scratch, len(header), nonce)
	scratch.seedInput = append(scratch.seedInput, header...)
	if nonce != nil {
		scratch.seedInput = nonce.AppendTo(scratch.seedInput)
	}
	seed512 := sha3.Sum512(scratch.seedInput)

	var mix [32]byte
	copy(mix[:], seed512[:32])

	var node [64]byte
	for r := uint64(0); r < spec.ReadsPerHash; r++ {
		copy(scratch.fnvInput[:32], mix[:])
		binary.LittleEndian.PutUint64(scratch.fnvInput[32:], r)

		nodeIdx := fnv1a64(scratch.fnvInput[:]) % accessor.NodeCount()
		accessor.ReadNode(nodeIdx, &node)

		scratch.blakeInput = blake3RoundInput(mix, node)

		sum := blake3.Sum256(scratch.blakeInput[:])
		copy(mix[:], sum[:])
	}

	copy(scratch.finalInput[:64], seed512[:])
	copy(scratch.finalInput[64:], mix[:])
	final512 := sha3.Sum512(scratch.finalInput[:])
	copy(out.Full512[:], final512[:])
	copy(out.Pow256[:], final512[:32])
	return out
}

// The Blake3 round input is a deterministic 64-byte buffer formed by XORing
// the 32-byte mix against each 32-byte half of the 64-byte DAG node.
func blake3RoundInput(mix [32]byte, node [64]byte) [64]byte {
	var in [64]byte
	for i := 0; i < 32; i++ {
		in[i] = mix[i] ^ node[i]
		in[32+i] = mix[i] ^ node[32+i]
	}
	return in
}

func fnv1a64(data []byte) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var h uint64 = offset64
	for _, b := range data {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}
