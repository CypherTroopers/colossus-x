package miner

import cx "colossusx/colossusx"

type cpuNode [64]byte

type cpuDAGView struct{ nodes []cpuNode }

func (v cpuDAGView) NodeCount() uint64                { return uint64(len(v.nodes)) }
func (v cpuDAGView) ReadNode(i uint64, out *[64]byte) { *out = [64]byte(v.nodes[i]) }

type CPUBackend struct {
	spec    Spec
	nodes   []cpuNode
	scratch *pooledScratch
}

func (b *CPUBackend) Mode() BackendMode { return BackendCPU }
func (b *CPUBackend) Description() string {
	return "cpu backend with a prepared node table and shared core algorithm"
}
func (b *CPUBackend) Prepare(dag *DAG) error {
	if dag == nil {
		return ErrNilDAG
	}
	if b.scratch == nil {
		b.scratch = newPooledScratch()
	}
	b.spec = dag.Spec()
	count := dag.NodeCount()
	b.nodes = make([]cpuNode, count)
	for i := uint64(0); i < count; i++ {
		copy(b.nodes[i][:], dag.Node(i))
	}
	return nil
}
func (b *CPUBackend) Hash(header []byte, nonce cx.Nonce, dag *DAG) HashResult {
	if len(b.nodes) == 0 && dag != nil {
		_ = b.Prepare(dag)
	}
	s := b.scratch.acquire(len(header))
	defer b.scratch.release(s)
	return latticeHashWithAccessor(b.spec, header, nonce, cpuDAGView{nodes: b.nodes}, s)
}

func (b *CPUBackend) HashBatch(header []byte, startNonce cx.Nonce, count uint64, dag *DAG) ([]HashResult, error) {
	results := make([]HashResult, count)
	for i := uint64(0); i < count; i++ {
		nonce, ok := startNonce.AddUint64(i)
		if !ok {
			return results[:i], nil
		}
		results[i] = b.Hash(header, nonce, dag)
	}
	return results, nil
}
