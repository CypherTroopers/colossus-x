package colossusx

import (
	"encoding/binary"
	"errors"

	"github.com/zeebo/blake3"
)

type StatelessDAG struct {
	spec      Spec
	epochSeed []byte
}

func NewStatelessDAG(spec Spec, epochSeed []byte) (*StatelessDAG, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if len(epochSeed) == 0 {
		return nil, errors.New("epoch seed cannot be empty")
	}
	seed := append([]byte(nil), epochSeed...)
	return &StatelessDAG{spec: spec, epochSeed: seed}, nil
}

func (d *StatelessDAG) NodeCount() uint64 {
	if d == nil {
		return 0
	}
	return d.spec.NodeCount()
}

func (d *StatelessDAG) TileCount() uint64 { return d.NodeCount() }

func (d *StatelessDAG) ReadNode(i uint64, out *[64]byte) {
	if d == nil || out == nil {
		return
	}
	if d.spec.Mode == ModeStrict {
		node := d.strictNode(i)
		copy(out[:], node[:])
		return
	}
	node := d.researchNode(i)
	copy(out[:], node[:])
}

func (d *StatelessDAG) ReadTensorTile(i uint64, out *TensorTile) {
	if d == nil || out == nil {
		return
	}
	var raw [64]byte
	d.ReadNode(i, &raw)
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

func (d *StatelessDAG) researchNode(i uint64) [64]byte {
	tmp := make([]byte, len(d.epochSeed)+8)
	copy(tmp, d.epochSeed)
	binary.LittleEndian.PutUint64(tmp[len(d.epochSeed):], i)
	return keccak512(tmp)
}

func (d *StatelessDAG) strictNode(i uint64) [64]byte {
	var ctr [8]byte
	binary.LittleEndian.PutUint64(ctr[:], i)
	xof := blake3.New()
	_, _ = xof.Write(d.epochSeed)
	_, _ = xof.Write(ctr[:])
	var raw [64]byte
	_, _ = xof.Digest().Read(raw[:])
	return raw
}

func HashHeaderStateless(spec Spec, header []byte, nonce Nonce, epochSeed []byte) (HashResult, error) {
	dag, err := NewStatelessDAG(spec, epochSeed)
	if err != nil {
		return HashResult{}, err
	}
	if spec.AlgorithmVersion >= 2 {
		return StrictV2Hash(spec, header, nonce, dag), nil
	}
	return LatticeHash(spec, header, nonce, dag, nil), nil
}

func VerifyHeaderStateless(spec Spec, header []byte, nonce Nonce, epochSeed []byte, target Target) (HashResult, bool, error) {
	hash, err := HashHeaderStateless(spec, header, nonce, epochSeed)
	if err != nil {
		return HashResult{}, false, err
	}
	return hash, LessOrEqualBE(hash.Pow256, target), nil
}
