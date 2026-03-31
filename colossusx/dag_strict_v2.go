package colossusx

import (
	"encoding/binary"
	"runtime"
	"sync"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/sha3"
)

const (
	strictV2SeedCacheBytes   = 512 * 1024 * 1024
	strictV2SeedCacheEntry   = 64
	strictV2MinCacheEntries  = 1024
	strictV2SeedCachePasses  = 3
	strictV2CellCacheLookups = 256
)

func strictV2CacheEntriesForSpec(spec Spec) int {
	bytes := uint64(strictV2SeedCacheBytes)
	// Keep production strict profile at the full 512 MiB cache, while allowing
	// smaller deterministic fixtures used by tests and simulations to scale down.
	if initial := spec.initialDAGSize(); initial > 0 && initial < StrictInitialDAGSizeBytes {
		scaled := initial / 160 // preserve 512 MiB : 80 GiB ratio
		minBytes := uint64(strictV2MinCacheEntries * strictV2SeedCacheEntry)
		if scaled < minBytes {
			scaled = minBytes
		}
		bytes = scaled
	}
	entries := int(bytes / strictV2SeedCacheEntry)
	if entries < strictV2MinCacheEntries {
		return strictV2MinCacheEntries
	}
	return entries
}

func buildStrictV2SeedCache(seed []byte, entries int) [][64]byte {
	cache := make([][64]byte, entries)
	s := sha3.Sum256(seed)
	cache[0] = sha3.Sum512(s[:])
	for i := 1; i < entries; i++ {
		cache[i] = sha3.Sum512(cache[i-1][:])
	}
	for pass := 0; pass < strictV2SeedCachePasses; pass++ {
		for i := 0; i < entries; i++ {
			target := binary.LittleEndian.Uint32(cache[i][:4]) % uint32(entries)
			var x [64]byte
			for j := 0; j < 64; j++ {
				x[j] = cache[i][j] ^ cache[target][j]
			}
			cache[i] = sha3.Sum512(x[:])
		}
	}
	return cache
}

func strictV2Node(index uint64, nodeSize uint64, cache [][64]byte) []byte {
	seed := cache[index%uint64(len(cache))]
	var idx [8]byte
	binary.LittleEndian.PutUint64(idx[:], index)
	var initialIn [64]byte
	copy(initialIn[:], seed[:])
	for i := 0; i < 8; i++ {
		initialIn[i] ^= idx[i]
	}
	mix := sha3.Sum512(initialIn[:])
	for j := uint32(0); j < strictV2CellCacheLookups; j++ {
		mi := mix[j%64]
		cacheIndex := fnv1a32(uint32(index)^j, uint32(mi)) % uint32(len(cache))
		var x [64]byte
		for k := 0; k < 64; k++ {
			x[k] = mix[k] ^ cache[cacheIndex][k]
		}
		mix = sha3.Sum512(x[:])
	}
	keyed, _ := blake3.NewKeyed(mix[:32])
	_, _ = keyed.Write(mix[:])
	_, _ = keyed.Write(idx[:])
	out := make([]byte, nodeSize)
	_, _ = keyed.Digest().Read(out)
	return out
}

func generateStrictV2DAG(spec Spec, dag []byte, epochSeed []byte, workers int, done func()) {
	cache := buildStrictV2SeedCache(epochSeed, strictV2CacheEntriesForSpec(spec))
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	count := spec.NodeCount()
	chunk := count / uint64(workers)
	if chunk == 0 {
		chunk = 1
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		from := uint64(w) * chunk
		to := from + chunk
		if w == workers-1 || to > count {
			to = count
		}
		if from >= count {
			break
		}
		wg.Add(1)
		go func(from, to uint64) {
			defer wg.Done()
			for i := from; i < to; i++ {
				off := i * spec.NodeSize
				copy(dag[off:off+spec.NodeSize], strictV2Node(i, spec.NodeSize, cache))
				if done != nil {
					done()
				}
			}
		}(from, to)
	}
	wg.Wait()
}
