package chain

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"colossusx/pkg/types"
)

type DiskStore struct {
	mu               sync.RWMutex
	datadir          string
	metaPath         string
	legacyPath       string
	blocksDir        string
	heightsDir       string
	genesisHash      types.Hash
	currentTip       types.Hash
	canonicalHeights map[uint64]types.Hash
}

type diskMeta struct {
	GenesisHash string `json:"genesis_hash"`
	CurrentTip  string `json:"current_tip"`
}

type diskBlockEntry struct {
	Block     types.Block `json:"block"`
	TotalWork string      `json:"total_work"`
}

type diskSnapshot struct {
	GenesisHash string            `json:"genesis_hash"`
	CurrentTip  string            `json:"current_tip"`
	Blocks      []diskBlockRecord `json:"blocks"`
	Heights     map[string]string `json:"heights"`
	TotalWork   map[string]string `json:"total_work"`
}

type diskBlockRecord struct {
	Hash  string      `json:"hash"`
	Block types.Block `json:"block"`
}

func NewDiskStore(datadir string) (*DiskStore, error) {
	if datadir == "" {
		return nil, fmt.Errorf("datadir is required")
	}
	if err := os.MkdirAll(datadir, 0o755); err != nil {
		return nil, err
	}
	store := &DiskStore{
		datadir:          datadir,
		metaPath:         filepath.Join(datadir, "chain_meta.json"),
		legacyPath:       filepath.Join(datadir, "chain.json"),
		blocksDir:        filepath.Join(datadir, "blocks"),
		heightsDir:       filepath.Join(datadir, "heights"),
		canonicalHeights: make(map[uint64]types.Hash),
	}
	if err := os.MkdirAll(store.blocksDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(store.heightsDir, 0o755); err != nil {
		return nil, err
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
	entry := diskBlockEntry{Block: block, TotalWork: bigIntToString(totalWork)}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(d.blockPath(hash), data, 0o644); err != nil {
		return err
	}
	if block.Header.Height == 0 && d.genesisHash == (types.Hash{}) {
		d.genesisHash = hash
	}
	if d.currentTip == (types.Hash{}) {
		d.currentTip = hash
		return d.flushCanonicalLocked()
	}
	return nil
}

func (d *DiskStore) GetBlock(hash types.Hash) (types.Block, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	entry, err := d.readBlockEntry(hash)
	if err != nil {
		return types.Block{}, err
	}
	return entry.Block, nil
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
	hash, ok := d.canonicalHeights[height]
	if !ok {
		return types.Block{}, fmt.Errorf("height %d: %w", height, ErrBlockNotFound)
	}
	entry, err := d.readBlockEntry(hash)
	if err != nil {
		return types.Block{}, err
	}
	return entry.Block, nil
}

func (d *DiskStore) CurrentTip() (types.Block, *big.Int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.currentTip == (types.Hash{}) {
		return types.Block{}, nil, ErrBlockNotFound
	}
	entry, err := d.readBlockEntry(d.currentTip)
	if err != nil {
		return types.Block{}, nil, err
	}
	work, err := bigIntFromString(entry.TotalWork)
	if err != nil {
		return types.Block{}, nil, err
	}
	return entry.Block, work, nil
}

func (d *DiskStore) SetCurrentTip(hash types.Hash) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.blockExists(hash) {
		return ErrBlockNotFound
	}
	d.currentTip = hash
	canonical, genesis, err := d.rebuildCanonicalLocked(hash)
	if err != nil {
		return err
	}
	d.canonicalHeights = canonical
	if d.genesisHash == (types.Hash{}) {
		d.genesisHash = genesis
	}
	return d.flushCanonicalLocked()
}

func (d *DiskStore) TotalWork(hash types.Hash) (*big.Int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	entry, err := d.readBlockEntry(hash)
	if err != nil {
		return nil, err
	}
	return bigIntFromString(entry.TotalWork)
}

func (d *DiskStore) HasBlock(hash types.Hash) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.blockExists(hash)
}

func (d *DiskStore) load() error {
	if data, err := os.ReadFile(d.metaPath); err == nil {
		var meta diskMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return err
		}
		var err error
		d.genesisHash, err = hashFromString(meta.GenesisHash)
		if err != nil && strings.TrimSpace(meta.GenesisHash) != "" {
			return err
		}
		d.currentTip, err = hashFromString(meta.CurrentTip)
		if err != nil && strings.TrimSpace(meta.CurrentTip) != "" {
			return err
		}
		return d.loadCanonicalHeightsLocked()
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := d.migrateLegacySnapshot(); err != nil {
		return err
	}
	return d.loadCanonicalHeightsLocked()
}

func (d *DiskStore) loadCanonicalHeightsLocked() error {
	entries, err := os.ReadDir(d.heightsDir)
	if err != nil {
		return err
	}
	d.canonicalHeights = make(map[uint64]types.Hash, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		heightStr := strings.TrimSuffix(entry.Name(), ".txt")
		height, err := strconv.ParseUint(heightStr, 10, 64)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(d.heightsDir, entry.Name()))
		if err != nil {
			return err
		}
		hash, err := hashFromString(strings.TrimSpace(string(data)))
		if err != nil {
			return err
		}
		d.canonicalHeights[height] = hash
	}
	return nil
}

func (d *DiskStore) migrateLegacySnapshot() error {
	data, err := os.ReadFile(d.legacyPath)
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
	for _, record := range snapshot.Blocks {
		hash, err := hashFromString(record.Hash)
		if err != nil {
			return err
		}
		workStr := "0"
		if w, ok := snapshot.TotalWork[record.Hash]; ok {
			workStr = w
		}
		entry := diskBlockEntry{Block: record.Block, TotalWork: workStr}
		encoded, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFileAtomic(d.blockPath(hash), encoded, 0o644); err != nil {
			return err
		}
	}
	if snapshot.GenesisHash != "" {
		hash, err := hashFromString(snapshot.GenesisHash)
		if err != nil {
			return err
		}
		d.genesisHash = hash
	}
	if snapshot.CurrentTip != "" {
		hash, err := hashFromString(snapshot.CurrentTip)
		if err != nil {
			return err
		}
		d.currentTip = hash
		canonical, genesis, err := d.rebuildCanonicalLocked(hash)
		if err != nil {
			return err
		}
		d.canonicalHeights = canonical
		if d.genesisHash == (types.Hash{}) {
			d.genesisHash = genesis
		}
	}
	return d.flushCanonicalLocked()
}

func (d *DiskStore) rebuildCanonicalLocked(tip types.Hash) (map[uint64]types.Hash, types.Hash, error) {
	canonical := make(map[uint64]types.Hash)
	seen := map[types.Hash]struct{}{}
	cursor := tip
	var genesis types.Hash
	for {
		if _, ok := seen[cursor]; ok {
			return nil, types.Hash{}, fmt.Errorf("canonical rebuild loop detected")
		}
		seen[cursor] = struct{}{}
		entry, err := d.readBlockEntry(cursor)
		if err != nil {
			return nil, types.Hash{}, err
		}
		canonical[entry.Block.Header.Height] = cursor
		if entry.Block.Header.Height == 0 {
			genesis = cursor
			break
		}
		cursor = entry.Block.Header.ParentHash
	}
	return canonical, genesis, nil
}

func (d *DiskStore) blockPath(hash types.Hash) string {
	return filepath.Join(d.blocksDir, hash.String()+".json")
}

func (d *DiskStore) heightPath(height uint64) string {
	return filepath.Join(d.heightsDir, strconv.FormatUint(height, 10)+".txt")
}

func (d *DiskStore) blockExists(hash types.Hash) bool {
	_, err := os.Stat(d.blockPath(hash))
	return err == nil
}

func (d *DiskStore) readBlockEntry(hash types.Hash) (diskBlockEntry, error) {
	data, err := os.ReadFile(d.blockPath(hash))
	if err != nil {
		if os.IsNotExist(err) {
			return diskBlockEntry{}, ErrBlockNotFound
		}
		return diskBlockEntry{}, err
	}
	var entry diskBlockEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return diskBlockEntry{}, err
	}
	return entry, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (d *DiskStore) flushCanonicalLocked() error {
	if err := os.MkdirAll(d.heightsDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(d.heightsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(d.heightsDir, entry.Name())); err != nil {
			return err
		}
	}
	heights := make([]uint64, 0, len(d.canonicalHeights))
	for h := range d.canonicalHeights {
		heights = append(heights, h)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	for _, height := range heights {
		hash := d.canonicalHeights[height]
		if err := writeFileAtomic(d.heightPath(height), []byte(hash.String()), 0o644); err != nil {
			return err
		}
	}
	meta := diskMeta{GenesisHash: d.genesisHash.String(), CurrentTip: d.currentTip.String()}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(d.metaPath, data, 0o644)
}

func bigIntToString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

func bigIntFromString(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return big.NewInt(0), nil
	}
	out, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid bigint %q", s)
	}
	return out, nil
}

func hashFromString(s string) (types.Hash, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.Hash{}, nil
	}
	var out types.Hash
	if err := out.UnmarshalJSON([]byte(`"` + s + `"`)); err != nil {
		return types.Hash{}, err
	}
	return out, nil
}

func marshalSnapshot(snapshot diskSnapshot) ([]byte, error) {
	return json.MarshalIndent(snapshot, "", "  ")
}

func unmarshalSnapshot(data []byte) (diskSnapshot, error) {
	var snapshot diskSnapshot
	err := json.Unmarshal(data, &snapshot)
	return snapshot, err
}
