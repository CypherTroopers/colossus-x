package chain

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"colossusx/pkg/types"
)

type DiskStore struct {
	mu          sync.RWMutex
	path        string
	genesisHash types.Hash
	currentTip  types.Hash
	blocks      map[types.Hash]types.Block
	heights     map[uint64]types.Hash
	totalWork   map[types.Hash]*big.Int
}

func NewDiskStore(datadir string) (*DiskStore, error) {
	if datadir == "" {
		return nil, fmt.Errorf("datadir is required")
	}
	if err := os.MkdirAll(datadir, 0o755); err != nil {
		return nil, err
	}
	store := &DiskStore{
		path:      filepath.Join(datadir, "chain.json"),
		blocks:    make(map[types.Hash]types.Block),
		heights:   make(map[uint64]types.Hash),
		totalWork: make(map[types.Hash]*big.Int),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (d *DiskStore) StoreBlock(block types.Block, totalWork *big.Int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	hash := block.BlockHash()
	d.blocks[hash] = block
	d.heights[block.Header.Height] = hash
	d.totalWork[hash] = new(big.Int).Set(totalWork)
	if block.Header.Height == 0 && d.genesisHash == (types.Hash{}) {
		d.genesisHash = hash
	}
	if d.currentTip == (types.Hash{}) {
		d.currentTip = hash
	}
	return d.flushLocked()
}

func (d *DiskStore) GetBlock(hash types.Hash) (types.Block, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	block, ok := d.blocks[hash]
	if !ok {
		return types.Block{}, ErrBlockNotFound
	}
	return block, nil
}

func (d *DiskStore) GetHeader(hash types.Hash) (types.BlockHeader, error) {
	block, err := d.GetBlock(hash)
	if err != nil {
		return types.BlockHeader{}, err
	}
	return block.Header, nil
}

func (d *DiskStore) GetBlockByHeight(height uint64) (types.Block, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	hash, ok := d.heights[height]
	if !ok {
		return types.Block{}, fmt.Errorf("height %d: %w", height, ErrBlockNotFound)
	}
	return d.blocks[hash], nil
}

func (d *DiskStore) CurrentTip() (types.Block, *big.Int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.currentTip == (types.Hash{}) {
		return types.Block{}, nil, ErrBlockNotFound
	}
	block := d.blocks[d.currentTip]
	work := new(big.Int)
	if tw, ok := d.totalWork[d.currentTip]; ok {
		work.Set(tw)
	}
	return block, work, nil
}

func (d *DiskStore) SetCurrentTip(hash types.Hash) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.blocks[hash]; !ok {
		return ErrBlockNotFound
	}
	d.currentTip = hash
	return d.flushLocked()
}

func (d *DiskStore) TotalWork(hash types.Hash) (*big.Int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	tw, ok := d.totalWork[hash]
	if !ok {
		return nil, ErrBlockNotFound
	}
	return new(big.Int).Set(tw), nil
}

func (d *DiskStore) HasBlock(hash types.Hash) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.blocks[hash]
	return ok
}

func (d *DiskStore) load() error {
	data, err := os.ReadFile(d.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	snapshot, err := unmarshalSnapshot(data)
	if err != nil {
		return err
	}
	d.genesisHash, err = hashFromString(snapshot.GenesisHash)
	if err != nil {
		return err
	}
	d.currentTip, err = hashFromString(snapshot.CurrentTip)
	if err != nil {
		return err
	}
	for _, record := range snapshot.Blocks {
		hash, err := hashFromString(record.Hash)
		if err != nil {
			return err
		}
		d.blocks[hash] = record.Block
	}
	for height, hashStr := range snapshot.Heights {
		h, err := strconv.ParseUint(height, 10, 64)
		if err != nil {
			return err
		}
		hash, err := hashFromString(hashStr)
		if err != nil {
			return err
		}
		d.heights[h] = hash
	}
	for hashStr, workStr := range snapshot.TotalWork {
		hash, err := hashFromString(hashStr)
		if err != nil {
			return err
		}
		work, err := bigIntFromString(workStr)
		if err != nil {
			return err
		}
		d.totalWork[hash] = work
	}
	return nil
}

func (d *DiskStore) flushLocked() error {
	snapshot := diskSnapshot{
		GenesisHash: d.genesisHash.String(),
		CurrentTip:  d.currentTip.String(),
		Blocks:      make([]diskBlockRecord, 0, len(d.blocks)),
		Heights:     make(map[string]string, len(d.heights)),
		TotalWork:   make(map[string]string, len(d.totalWork)),
	}
	hashes := make([]string, 0, len(d.blocks))
	byString := make(map[string]types.Hash, len(d.blocks))
	for hash := range d.blocks {
		hs := hash.String()
		hashes = append(hashes, hs)
		byString[hs] = hash
	}
	sort.Strings(hashes)
	for _, hs := range hashes {
		hash := byString[hs]
		snapshot.Blocks = append(snapshot.Blocks, diskBlockRecord{Hash: hs, Block: d.blocks[hash]})
	}
	for height, hash := range d.heights {
		snapshot.Heights[strconv.FormatUint(height, 10)] = hash.String()
	}
	for hash, work := range d.totalWork {
		snapshot.TotalWork[hash.String()] = bigIntToString(work)
	}
	data, err := marshalSnapshot(snapshot)
	if err != nil {
		return err
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, d.path)
}
