package colossusx

import (
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
)

type Allocation interface {
	Bytes() []byte
	Free() error
	Name() string
}

type Allocator interface {
	Alloc(size uint64) (Allocation, error)
	Name() string
}

type DAG struct {
	spec      Spec
	alloc     Allocation
	ownership bool
}

func NewDAGWithAllocation(spec Spec, alloc Allocation, ownership bool) (*DAG, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if alloc == nil {
		return nil, errors.New("dag allocation cannot be nil")
	}
	if uint64(len(alloc.Bytes())) < spec.DAGSizeBytes {
		if ownership {
			_ = alloc.Free()
		}
		return nil, errors.New("managed allocation is smaller than the DAG")
	}
	return &DAG{spec: spec, alloc: alloc, ownership: ownership}, nil
}

func NewDAGWithAllocator(spec Spec, allocator Allocator) (*DAG, error) {
	if allocator == nil {
		return nil, errors.New("dag allocator cannot be nil")
	}
	alloc, err := allocator.Alloc(spec.DAGSizeBytes)
	if err != nil {
		return nil, err
	}
	return NewDAGWithAllocation(spec, alloc, true)
}

func (d *DAG) Spec() Spec { return d.spec }
func (d *DAG) AllocationName() string {
	if d == nil || d.alloc == nil {
		return ""
	}
	return d.alloc.Name()
}
func (d *DAG) NodeCount() uint64 { return d.spec.NodeCount() }
func (d *DAG) Bytes() []byte {
	if d == nil || d.alloc == nil {
		return nil
	}
	buf := d.alloc.Bytes()
	if uint64(len(buf)) > d.spec.DAGSizeBytes {
		buf = buf[:d.spec.DAGSizeBytes]
	}
	return buf
}
func (d *DAG) Node(i uint64) []byte {
	off := i * d.spec.NodeSize
	buf := d.Bytes()
	return buf[off : off+d.spec.NodeSize]
}
func (d *DAG) ReadNode(i uint64, out *[64]byte) { copy(out[:], d.Node(i)) }

func (d *DAG) TileCount() uint64 { return d.NodeCount() }
func (d *DAG) ReadTensorTile(i uint64, out *TensorTile) {
	raw := d.Node(i)
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
func (d *DAG) Close() error {
	if d == nil || d.alloc == nil || !d.ownership {
		return nil
	}
	err := d.alloc.Free()
	d.alloc = nil
	d.ownership = false
	return err
}

func GenerateDAG(spec Spec, dag []byte, epochSeed []byte, workers int) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if len(epochSeed) == 0 {
		return errors.New("epoch seed cannot be empty")
	}
	if uint64(len(dag)) < spec.DAGSizeBytes {
		return errors.New("managed allocation is smaller than the DAG")
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	nodeCount := spec.NodeCount()
	chunk := nodeCount / uint64(workers)
	if chunk == 0 {
		chunk = 1
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		from := uint64(w) * chunk
		to := from + chunk
		if w == workers-1 || to > nodeCount {
			to = nodeCount
		}
		if from >= nodeCount {
			break
		}
		wg.Add(1)
		go func(from, to uint64) {
			defer wg.Done()
			tmp := make([]byte, len(epochSeed)+8)
			copy(tmp, epochSeed)
			for i := from; i < to; i++ {
				binary.LittleEndian.PutUint64(tmp[len(epochSeed):], i)
				sum := keccak512(tmp)
				off := i * spec.NodeSize
				copy(dag[off:off+spec.NodeSize], sum[:])
			}
		}(from, to)
	}
	wg.Wait()
	return nil
}

func PopulateDAG(dag *DAG, epochSeed []byte, workers int) error {
	if dag == nil {
		return errors.New("dag cannot be nil")
	}
	if dag.spec.Mode == ModeStrict {
		return GenerateTensorDAG(dag.spec, dag.Bytes(), epochSeed, workers)
	}
	return GenerateDAG(dag.spec, dag.Bytes(), epochSeed, workers)
}
