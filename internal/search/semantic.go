package search

import (
	"math"
	"sort"
	"sync"
)

// Vector is a float32 embedding vector.
type Vector []float32

// SemanticIndex provides approximate nearest neighbor search using a brute-force
// approach for Phase 1. This can be replaced with HNSW for production scale.
//
// For Phase 1 (< 100K agents), brute-force cosine similarity is fast enough
// (sub-millisecond for 100K 384-dim vectors). HNSW can be added later.
type SemanticIndex struct {
	mu         sync.RWMutex
	vectors    map[string]Vector
	dimensions int
}

// NewSemanticIndex creates a new semantic search index.
func NewSemanticIndex(dimensions int) *SemanticIndex {
	return &SemanticIndex{
		vectors:    make(map[string]Vector),
		dimensions: dimensions,
	}
}

// Index adds or updates a vector for a document.
func (idx *SemanticIndex) Index(id string, vec Vector) {
	if len(vec) != idx.dimensions {
		return // silently ignore dimension mismatch
	}

	// Normalize the vector for cosine similarity
	normalized := normalize(vec)

	idx.mu.Lock()
	idx.vectors[id] = normalized
	idx.mu.Unlock()
}

// Remove deletes a vector from the index.
func (idx *SemanticIndex) Remove(id string) {
	idx.mu.Lock()
	delete(idx.vectors, id)
	idx.mu.Unlock()
}

// SemanticResult holds a document ID and its cosine similarity score.
type SemanticResult struct {
	DocID string
	Score float64
}

// Search finds the nearest neighbors to the query vector.
func (idx *SemanticIndex) Search(queryVec Vector, maxResults int) []SemanticResult {
	if len(queryVec) != idx.dimensions {
		return nil
	}

	normalizedQuery := normalize(queryVec)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var results []SemanticResult
	for id, vec := range idx.vectors {
		sim := cosineSimilarity(normalizedQuery, vec)
		if sim > 0 { // only positive similarities
			results = append(results, SemanticResult{DocID: id, Score: sim})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// Count returns the number of vectors in the index.
func (idx *SemanticIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.vectors)
}

// cosineSimilarity computes the cosine similarity between two normalized vectors.
// Since both are pre-normalized, this is just the dot product.
func cosineSimilarity(a, b Vector) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// normalize returns a unit-length copy of the vector.
func normalize(v Vector) Vector {
	var mag float64
	for _, val := range v {
		mag += float64(val) * float64(val)
	}
	mag = math.Sqrt(mag)
	if mag == 0 {
		return v
	}

	normalized := make(Vector, len(v))
	for i, val := range v {
		normalized[i] = float32(float64(val) / mag)
	}
	return normalized
}
