package colossusx

import (
	"encoding/binary"
	"errors"

	"github.com/zeebo/blake3"
)

func GenerateTensorDAG(spec Spec, dag []byte, epochSeed []byte, workers int) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if spec.Mode != ModeStrict {
		return errors.New("tensor dag generation is strict-only")
	}
	for off, tile := uint64(0), uint64(0); off+spec.NodeSize <= uint64(len(dag)) && off < spec.DAGSizeBytes; off, tile = off+spec.NodeSize, tile+1 {
		var ctr [8]byte
		binary.LittleEndian.PutUint64(ctr[:], tile)
		xof := blake3.New()
		xof.Write(epochSeed)
		xof.Write(ctr[:])
		out := make([]byte, spec.NodeSize)
		xof.Digest().Read(out)
		copy(dag[off:off+spec.NodeSize], out)
	}
	return nil
}
