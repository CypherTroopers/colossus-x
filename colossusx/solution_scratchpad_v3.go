package colossusx

import (
	"encoding/binary"
	"errors"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/sha3"
)

func BuildColossusXSolutionStreamingV3(spec Spec, header []byte, nonce uint64, dag DAGAccessor) (ColossusXSolution, [32]byte, error) {
	if dag == nil || dag.NodeCount() == 0 {
		return ColossusXSolution{}, [32]byte{}, errors.New("dag is empty")
	}
	trace := ColossusXTraceHashV3(spec, header, NewUint64Nonce(nonce), dag)
	auditIdx := ColossusXAuditIndicesFromSolutionHash(trace.SolutionHash, dag.NodeCount(), ColossusXAuditCellCount)
	needed := make([]uint64, 0, len(trace.Accessed)+len(auditIdx))
	seen := make(map[uint64]struct{}, len(trace.Accessed)+len(auditIdx))
	for _, idx := range trace.Accessed {
		key := uint64(idx)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		needed = append(needed, key)
	}
	for _, idx := range auditIdx {
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		needed = append(needed, idx)
	}
	root, proofs, err := BuildMerkleMultiProofFromAccessor(dag, spec.NodeSize, needed)
	if err != nil {
		return ColossusXSolution{}, [32]byte{}, err
	}
	out := ColossusXSolution{
		Nonce:       nonce,
		MixDigest:   trace.MixDigest,
		MiningCells: make([]SolutionCell, 0, len(trace.Accessed)),
		AuditCells:  make([]SolutionCell, 0, ColossusXAuditCellCount),
	}
	for _, idx := range trace.Accessed {
		cell := make([]byte, spec.NodeSize)
		dag.ReadNode(uint64(idx), cell)
		out.MiningCells = append(out.MiningCells, SolutionCell{Index: idx, Data: cell, Proof: append(MerkleProof(nil), proofs[uint64(idx)]...)})
	}
	for _, idx := range auditIdx {
		cell := make([]byte, spec.NodeSize)
		dag.ReadNode(idx, cell)
		out.AuditCells = append(out.AuditCells, SolutionCell{Index: uint32(idx), Data: cell, Proof: append(MerkleProof(nil), proofs[idx]...)})
	}
	return out, root, nil
}

func VerifyColossusXSolutionV3(spec Spec, header []byte, target Target, merkleRoot [32]byte, solution ColossusXSolution) error {
	initialInput := append([]byte{}, header...)
	var nonceLE [8]byte
	binary.LittleEndian.PutUint64(nonceLE[:], solution.Nonce)
	initialInput = append(initialInput, nonceLE[:]...)
	initial := sha3.Sum512(initialInput)
	state := initial
	reads := spec.ReadsPerHash
	if reads == 0 {
		reads = ColossusXScratchpadReadsPerHash
	}
	if len(solution.MiningCells) != int(reads) {
		return errors.New("invalid mining cell count")
	}
	for round, c := range solution.MiningCells {
		expect := uint32(uint64(fnv1a32(uint32(round), binary.LittleEndian.Uint32(state[:4]))) % spec.NodeCount())
		if c.Index != expect {
			return errors.New("invalid mining index")
		}
		if len(c.Data) != int(spec.NodeSize) {
			return errors.New("invalid mining cell size")
		}
		leaf := blake3.Sum256(c.Data)
		if !VerifyMerkleProof(merkleRoot, leaf, int(c.Index), c.Proof) {
			return errors.New("invalid mining merkle proof")
		}
		state = colossusXScratchpadRoundV3(state, c.Data, uint64(round), uint64(c.Index))
	}
	mix := sha3.Sum512(state[:])
	if mix != solution.MixDigest {
		return errors.New("mix digest mismatch")
	}
	finalInput := append(initial[:], mix[:]...)
	result := blake3.Sum256(finalInput)
	if !LessOrEqualBE(result, target) {
		return errors.New("pow target mismatch")
	}
	solutionSeed := append(append(initial[:], mix[:]...), nonceLE[:]...)
	solutionHash := blake3.Sum256(solutionSeed)
	expectAudit := ColossusXAuditIndicesFromSolutionHash(solutionHash, spec.NodeCount(), ColossusXAuditCellCount)
	if len(solution.AuditCells) != len(expectAudit) {
		return errors.New("invalid audit cell count")
	}
	for i, c := range solution.AuditCells {
		if uint64(c.Index) != expectAudit[i] {
			return errors.New("invalid audit index")
		}
		if len(c.Data) != int(spec.NodeSize) {
			return errors.New("invalid audit cell size")
		}
		leaf := blake3.Sum256(c.Data)
		if !VerifyMerkleProof(merkleRoot, leaf, int(c.Index), c.Proof) {
			return errors.New("invalid audit merkle proof")
		}
	}
	return nil
}
