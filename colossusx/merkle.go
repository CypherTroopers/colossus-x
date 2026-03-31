package colossusx

import "github.com/zeebo/blake3"

type MerkleProof [][32]byte

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
	if len(leaves) == 0 || index < 0 || index >= len(leaves) {
		return nil
	}
	level := append([][32]byte(nil), leaves...)
	proof := make(MerkleProof, 0, 32)
	idx := index
	for len(level) > 1 {
		sibling := idx ^ 1
		if sibling >= len(level) {
			proof = append(proof, level[idx])
		} else {
			proof = append(proof, level[sibling])
		}
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
		idx /= 2
		level = next
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
