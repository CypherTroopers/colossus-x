package colossusx

import (
	"encoding/binary"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/sha3"
)

func ColossusXHashV3(spec Spec, header []byte, nonce Nonce, dag DAGAccessor) HashResult {
	trace := ColossusXTraceHashV3(spec, header, nonce, dag)
	var out HashResult
	copy(out.Pow256[:], trace.Result[:])
	copy(out.Full512[:], append(trace.InitialHash[:32], trace.MixDigest[:32]...))
	return out
}

func ColossusXTraceHashV3(spec Spec, header []byte, nonce Nonce, dag DAGAccessor) ColossusXTrace {
	var trace ColossusXTrace
	if dag == nil || dag.NodeCount() == 0 {
		return trace
	}
	seedInput := append([]byte{}, header...)
	if nonce != nil {
		seedInput = nonce.AppendTo(seedInput)
	}
	initial := sha3.Sum512(seedInput)
	state := initial
	cell := make([]byte, spec.NodeSize)
	reads := spec.ReadsPerHash
	if reads == 0 {
		reads = ColossusXScratchpadReadsPerHash
	}
	accessed := make([]uint32, 0, reads)
	for round := uint64(0); round < reads; round++ {
		index := uint64(fnv1a32(uint32(round), binary.LittleEndian.Uint32(state[:4]))) % dag.NodeCount()
		accessed = append(accessed, uint32(index))
		dag.ReadNode(index, cell)
		state = colossusXScratchpadRoundV3(state, cell, round, index)
	}
	mix := sha3.Sum512(state[:])
	finalInput := make([]byte, 0, len(initial)+len(mix))
	finalInput = append(finalInput, initial[:]...)
	finalInput = append(finalInput, mix[:]...)
	pow := blake3.Sum256(finalInput)
	var nonceBytes [8]byte
	if n64, ok := nonce.(Uint64Nonce); ok {
		binary.LittleEndian.PutUint64(nonceBytes[:], n64.Uint64())
	}
	solutionSeed := append(append(initial[:], mix[:]...), nonceBytes[:]...)
	trace.SolutionHash = blake3.Sum256(solutionSeed)
	trace.InitialHash = initial
	trace.MixDigest = mix
	trace.Result = pow
	trace.Accessed = accessed
	return trace
}

func colossusXScratchpadRoundV3(state [64]byte, cell []byte, round uint64, index uint64) [64]byte {
	var v [4][16]int8
	for lane := 0; lane < 16; lane++ {
		v[0][lane] = int8(state[lane])
		v[1][lane] = int8(state[16+lane])
		v[2][lane] = int8(state[32+lane])
		v[3][lane] = int8(state[48+lane])
	}
	var a, b, c, d [16]int32
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			m := int32(int8(cell[row*16+col]))
			mt := int32(int8(cell[col*16+row]))
			a[row] += m * int32(v[0][col])
			b[row] += mt * int32(v[1][col])
			c[row] += m * int32(v[2][col])
			d[row] += mt * int32(v[3][col])
		}
	}
	var saltIn [64 + 8 + 8]byte
	copy(saltIn[:64], state[:])
	binary.LittleEndian.PutUint64(saltIn[64:], round)
	binary.LittleEndian.PutUint64(saltIn[72:], index)
	salt := sha3.Sum512(saltIn[:])
	var next [64]byte
	for lane := 0; lane < 16; lane++ {
		next[lane] = byte(clampInt8((a[lane] + c[(lane+5)%16] + int32(int8(salt[lane]))) >> 8))
		next[16+lane] = byte(clampInt8((b[lane] + d[(lane+7)%16] + int32(int8(salt[16+lane]))) >> 8))
		next[32+lane] = byte(clampInt8((a[(lane+3)%16] + b[lane] + int32(int8(salt[32+lane]))) >> 8))
		next[48+lane] = byte(clampInt8((c[lane] + d[(lane+11)%16] + int32(int8(salt[48+lane]))) >> 8))
	}
	return next
}

func clampInt8(v int32) int8 {
	if v > 127 {
		return 127
	}
	if v < -128 {
		return -128
	}
	return int8(v)
}
