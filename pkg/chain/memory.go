package chain

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"colossusx/pkg/types"
)

var ErrBlockNotFound = errors.New("block not found")

type Store interface {
	StoreBlock(block types.Block, totalWork *big.Int) error
	GetBlock(hash types.Hash) (types.Block, error)
	GetHeader(hash types.Hash) (types.BlockHeader, error)
	GetBlockByHeight(height uint64) (types.Block, error)
	CurrentTip() (types.Block, *big.Int, error)
	SetCurrentTip(hash types.Hash) error
	TotalWork(hash types.Hash) (*big.Int, error)
	HasBlock(hash types.Hash) bool
}

type MemoryStore struct {
	mu               sync.RWMutex
	genesisHash      types.Hash
	currentTip       types.Hash
	blocks           map[types.Hash]types.Block
	totalWork        map[types.Hash]*big.Int
	canonicalHeights map[uint64]types.Hash
	heightIndex      map[uint64][]types.Hash
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		blocks:           make(map[types.Hash]types.Block),
		totalWork:        make(map[types.Hash]*big.Int),
		canonicalHeights: make(map[uint64]types.Hash),
		heightIndex:      make(map[uint64][]types.Hash),
	}
}

func (m *MemoryStore) StoreBlock(block types.Block, totalWork *big.Int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hash := block.BlockHash()
	m.blocks[hash] = block
	m.totalWork[hash] = new(big.Int).Set(totalWork)
	if !containsHash(m.heightIndex[block.Header.Height], hash) {
		m.heightIndex[block.Header.Height] = append(m.heightIndex[block.Header.Height], hash)
	}
	if block.Header.Height == 0 && m.genesisHash == (types.Hash{}) {
		m.genesisHash = hash
	}
	if m.currentTip == (types.Hash{}) {
		m.currentTip = hash
		return m.rebuildCanonicalLocked(hash)
	}
	return nil
}

func (m *MemoryStore) GetBlock(hash types.Hash) (types.Block, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	block, ok := m.blocks[hash]
	if !ok {
		return types.Block{}, ErrBlockNotFound
	}
	return block, nil
}

func (m *MemoryStore) GetHeader(hash types.Hash) (types.BlockHeader, error) {
	block, err := m.GetBlock(hash)
	if err != nil {
		return types.BlockHeader{}, err
	}
	return block.Header, nil
}

func (m *MemoryStore) GetBlockByHeight(height uint64) (types.Block, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hash, ok := m.canonicalHeights[height]
	if !ok {
		return types.Block{}, fmt.Errorf("height %d: %w", height, ErrBlockNotFound)
	}
	block, ok := m.blocks[hash]
	if !ok {
		return types.Block{}, fmt.Errorf("height %d: %w", height, ErrBlockNotFound)
	}
	return block, nil
}

func (m *MemoryStore) CurrentTip() (types.Block, *big.Int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentTip == (types.Hash{}) {
		return types.Block{}, nil, ErrBlockNotFound
	}
	block, ok := m.blocks[m.currentTip]
	if !ok {
		return types.Block{}, nil, ErrBlockNotFound
	}
	work := new(big.Int)
	if tw, ok := m.totalWork[m.currentTip]; ok {
		work.Set(tw)
	}
	return block, work, nil
}

func (m *MemoryStore) SetCurrentTip(hash types.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blocks[hash]; !ok {
		return ErrBlockNotFound
	}
	m.currentTip = hash
	return m.rebuildCanonicalLocked(hash)
}

func (m *MemoryStore) TotalWork(hash types.Hash) (*big.Int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tw, ok := m.totalWork[hash]
	if !ok {
		return nil, ErrBlockNotFound
	}
	return new(big.Int).Set(tw), nil
}

func (m *MemoryStore) HasBlock(hash types.Hash) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.blocks[hash]
	return ok
}

func (m *MemoryStore) rebuildCanonicalLocked(tip types.Hash) error {
	canonical := make(map[uint64]types.Hash)
	seen := make(map[types.Hash]struct{})
	cursor := tip
	for {
		if _, ok := seen[cursor]; ok {
			return fmt.Errorf("canonical rebuild loop detected")
		}
		seen[cursor] = struct{}{}
		block, ok := m.blocks[cursor]
		if !ok {
			return ErrBlockNotFound
		}
		canonical[block.Header.Height] = cursor
		if block.Header.Height == 0 {
			break
		}
		cursor = block.Header.ParentHash
	}
	m.canonicalHeights = canonical
	return nil
}

func containsHash(list []types.Hash, target types.Hash) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func sortedHeights(in map[uint64]types.Hash) []uint64 {
	out := make([]uint64, 0, len(in))
	for h := range in {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
