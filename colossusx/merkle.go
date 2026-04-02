package colossusx

import (
	"fmt"

	"github.com/zeebo/blake3"
)

type MerkleProof [][32]byte
type MerkleProver interface {
	LeafCount() uint64
	Root() [32]byte
	Proof(index int) (MerkleProof, error)
}

type levelMerkleProver struct {
	levels [][][32]byte
}

func (p *levelMerkleProver) LeafCount() uint64 {
	if p == nil || len(p.levels) == 0 {
		return 0
	}
	return uint64(len(p.levels[0]))
}

func (p *levelMerkleProver) Root() [32]byte {
	if p == nil || len(p.levels) == 0 {
		return [32]byte{}
	}
	top := p.levels[len(p.levels)-1]
	if len(top) == 0 {
		return [32]byte{}
	}
	return top[0]
}

func (p *levelMerkleProver) Proof(index int) (MerkleProof, error) {
	if p == nil || len(p.levels) == 0 {
		return nil, fmt.Errorf("merkle prover is empty")
	}
	if index < 0 || index >= len(p.levels[0]) {
		return nil, fmt.Errorf("merkle proof index %d out of range", index)
	}
	proof := make(MerkleProof, 0, len(p.levels)-1)
	idx := index
	for level := 0; level < len(p.levels)-1; level++ {
		nodes := p.levels[level]
		sibling := idx ^ 1
		if sibling >= len(nodes) {
			sibling = idx
		}
		proof = append(proof, nodes[sibling])
		idx /= 2
	}
	return proof, nil
}

func NewMerkleProverFromLeaves(leaves [][32]byte) (MerkleProver, error) {
	if len(leaves) == 0 {
		return nil, fmt.Errorf("merkle leaves are empty")
	}
	return &levelMerkleProver{levels: buildMerkleLevels(leaves)}, nil
}

func NewMerkleProverFromAccessor(accessor DAGAccessor, nodeSize uint64) (MerkleProver, error) {
	if accessor == nil || accessor.NodeCount() == 0 {
		return nil, fmt.Errorf("dag is empty")
	}
	if nodeSize == 0 {
		return nil, fmt.Errorf("node size must be > 0")
	}
	leaves := make([][32]byte, accessor.NodeCount())
	node := make([]byte, nodeSize)
	for i := uint64(0); i < accessor.NodeCount(); i++ {
		accessor.ReadNode(i, node)
		leaves[i] = blake3.Sum256(node)
	}
	return &levelMerkleProver{levels: buildMerkleLevels(leaves)}, nil
}

func buildMerkleLevels(leaves [][32]byte) [][][32]byte {
	levels := make([][][32]byte, 0, 32)
	level := append([][32]byte(nil), leaves...)
	levels = append(levels, level)
	for len(level) > 1 {
		next := make([][32]byte, (len(level)+1)/2)
		for i, out := 0, 0; i < len(level); i, out = i+2, out+1 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			var in [64]byte
			copy(in[:32], left[:])
			copy(in[32:], right[:])
			next[out] = blake3.Sum256(in[:])
		}
		level = next
		levels = append(levels, level)
	}
	return levels
}

func BuildMerkleLeaves(cells [][]byte) [][32]byte {
	leaves := make([][32]byte, len(cells))
	for i := range cells {
		leaves[i] = blake3.Sum256(cells[i])
	}
	return leaves
}

func BuildMerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	level := append([][32]byte(nil), leaves...)
	for len(level) > 1 {
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			var in [64]byte
			copy(in[:32], left[:])
			copy(in[32:], right[:])
			next = append(next, blake3.Sum256(in[:]))
		}
		level = next
	}
	return level[0]
}

func BuildMerkleProof(leaves [][32]byte, index int) MerkleProof {
	prover, err := NewMerkleProverFromLeaves(leaves)
	if err != nil {
		return nil
	}
	proof, err := prover.Proof(index)
	if err != nil {
		return nil
	}
	return proof
}

func VerifyMerkleProof(root [32]byte, leaf [32]byte, index int, proof MerkleProof) bool {
	h := leaf
	idx := index
	for _, sib := range proof {
		var in [64]byte
		if idx%2 == 0 {
			copy(in[:32], h[:])
			copy(in[32:], sib[:])
		} else {
			copy(in[:32], sib[:])
			copy(in[32:], h[:])
		}
		h = blake3.Sum256(in[:])
		idx /= 2
	}
	return h == root
}
