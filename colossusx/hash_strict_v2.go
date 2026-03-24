package colossusx

import (
	"crypto/sha256"
	"encoding/binary"
)

type tensorView struct{ dag *DAG }

func (v tensorView) TileCount() uint64 { return v.dag.NodeCount() }
func (v tensorView) ReadTensorTile(i uint64, out *TensorTile) {
	raw := v.dag.Node(i)
	for j := 0; j < 256; j++ {
		out.MatrixA[j] = int8(raw[j%64])
		out.MatrixB[j] = int8(raw[(j+17)%64])
	}
	for j := 0; j < 16; j++ {
		out.Bias[j] = int32(int8(raw[j]))
	}
	copy(out.Permute[:], raw[:32])
	copy(out.Meta[:], raw[32:64])
}

func StrictV2Hash(spec Spec, header []byte, nonce Nonce, dag TensorDAGAccessor) HashResult {
	var out HashResult
	if dag == nil || dag.TileCount() == 0 {
		return out
	}
	seedInput := append([]byte{}, header...)
	if nonce != nil {
		seedInput = nonce.AppendTo(seedInput)
	}
	seed := sha256.Sum256(seedInput)
	state := seed
	for r := uint32(0); r < spec.ComputeRounds; r++ {
		idx := binary.LittleEndian.Uint64(state[:8]) % dag.TileCount()
		var tile TensorTile
		dag.ReadTensorTile(idx, &tile)
		var lane [16]int32
		for i := 0; i < 16; i++ {
			acc := tile.Bias[i] + int32(state[i]) - int32(state[31-i])
			for j := 0; j < 16; j++ {
				acc += int32(tile.MatrixA[i*16+j]) * int32(tile.MatrixB[j*16+((i+int(r))%16)])
			}
			lane[i] = acc
		}
		var buf [64]byte
		for i := 0; i < 16; i++ {
			v := uint32(lane[i]) ^ uint32(tile.Meta[i]) ^ uint32(tile.Permute[i]) ^ uint32(r)
			binary.LittleEndian.PutUint32(buf[i*4:], v)
		}
		state = sha256.Sum256(append(state[:], buf[:]...))
		if spec.RoundCommitInterval > 0 && (r+1)%spec.RoundCommitInterval == 0 {
			state = RoundCommit(r+1, state)
		}
	}
	final := sha256.Sum256(state[:])
	copy(out.Pow256[:], final[:])
	dbl := sha256.Sum256(append(seed[:], final[:]...))
	copy(out.Full512[:32], final[:])
	copy(out.Full512[32:], dbl[:])
	return out
}
