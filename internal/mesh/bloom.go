// Package mesh implements the peer-to-peer mesh protocol for registry nodes.
package mesh

import (
	"hash"
	"hash/fnv"
	"math"
	"sync"
)

// BloomFilter is a probabilistic data structure for fast membership testing.
// Used for routing federated search queries to relevant peers.
type BloomFilter struct {
	mu      sync.RWMutex
	bits    []bool
	size    uint
	hashNum uint
}

// NewBloomFilter creates a bloom filter sized for the expected number of items
// at the given false positive rate.
func NewBloomFilter(expectedItems int, fpRate float64) *BloomFilter {
	if expectedItems <= 0 {
		expectedItems = 1000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}

	// Optimal size: m = -(n * ln(p)) / (ln(2)^2)
	m := uint(math.Ceil(-float64(expectedItems) * math.Log(fpRate) / (math.Log(2) * math.Log(2))))
	// Optimal hash functions: k = (m/n) * ln(2)
	k := uint(math.Ceil(float64(m) / float64(expectedItems) * math.Log(2)))

	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	return &BloomFilter{
		bits:    make([]bool, m),
		size:    m,
		hashNum: k,
	}
}

// Add inserts an item into the bloom filter.
func (bf *BloomFilter) Add(item string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for _, idx := range bf.indices([]byte(item)) {
		bf.bits[idx] = true
	}
}

// Contains checks if an item might be in the set.
// False positives are possible; false negatives are not.
func (bf *BloomFilter) Contains(item string) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	for _, idx := range bf.indices([]byte(item)) {
		if !bf.bits[idx] {
			return false
		}
	}
	return true
}

// MatchCount returns how many of the given items match the bloom filter.
func (bf *BloomFilter) MatchCount(items []string) int {
	count := 0
	for _, item := range items {
		if bf.Contains(item) {
			count++
		}
	}
	return count
}

// Bytes returns the bloom filter as a byte slice for transmission.
func (bf *BloomFilter) Bytes() []byte {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	byteLen := (len(bf.bits) + 7) / 8
	data := make([]byte, byteLen)
	for i, bit := range bf.bits {
		if bit {
			data[i/8] |= 1 << (uint(i) % 8)
		}
	}
	return data
}

// FromBytes reconstructs a bloom filter from a byte slice.
func (bf *BloomFilter) FromBytes(data []byte) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for i := range bf.bits {
		bf.bits[i] = false
	}

	for i := 0; i < len(bf.bits) && i/8 < len(data); i++ {
		if data[i/8]&(1<<(uint(i)%8)) != 0 {
			bf.bits[i] = true
		}
	}
}

// Clear resets the bloom filter.
func (bf *BloomFilter) Clear() {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	for i := range bf.bits {
		bf.bits[i] = false
	}
}

// Size returns the size of the bloom filter in bits.
func (bf *BloomFilter) Size() uint {
	return bf.size
}

// indices computes the bloom filter bit positions for an item.
// Uses double hashing: h(i) = h1 + i*h2 (mod m)
func (bf *BloomFilter) indices(data []byte) []uint {
	h1 := bf.hash1(data)
	h2 := bf.hash2(data)

	indices := make([]uint, bf.hashNum)
	for i := uint(0); i < bf.hashNum; i++ {
		indices[i] = (h1 + i*h2) % bf.size
	}
	return indices
}

func (bf *BloomFilter) hash1(data []byte) uint {
	h := fnv.New64a()
	h.Write(data)
	return uint(h.Sum64() % uint64(bf.size))
}

func (bf *BloomFilter) hash2(data []byte) uint {
	var h hash.Hash64 = fnv.New64()
	h.Write(data)
	result := uint(h.Sum64() % uint64(bf.size))
	if result == 0 {
		result = 1 // ensure h2 is non-zero
	}
	return result
}
