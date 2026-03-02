package search

import (
	"hash/fnv"
	"math"
	"strings"
)

// Embedder generates embedding vectors for text.
// Phase 1 uses a simple bag-of-words hashing approach as a placeholder.
// Production should use ONNX Runtime + all-MiniLM-L6-v2 for real semantic embeddings.
//
// The interface is designed so that swapping in a real model requires no changes
// to the rest of the codebase.
type Embedder interface {
	Embed(text string) Vector
	Dimensions() int
}

// HashEmbedder is a simple feature-hashing embedder for Phase 1.
// It uses FNV hashing to project tokens into a fixed-dimension space.
// This gives basic semantic-ish similarity (words that appear together get similar vectors)
// but is NOT a real neural embedding. Replace with ONNX model in production.
type HashEmbedder struct {
	dims int
}

// NewHashEmbedder creates a new hash-based embedder with the given dimensions.
func NewHashEmbedder(dimensions int) *HashEmbedder {
	return &HashEmbedder{dims: dimensions}
}

// Embed generates a vector for the given text using feature hashing.
func (e *HashEmbedder) Embed(text string) Vector {
	vec := make(Vector, e.dims)
	tokens := tokenize(strings.ToLower(text))

	if len(tokens) == 0 {
		return vec
	}

	for _, token := range tokens {
		h := fnv.New64a()
		h.Write([]byte(token))
		hash := h.Sum64()

		// Use hash to determine index and sign
		idx := int(hash % uint64(e.dims))
		sign := float32(1.0)
		if hash&1 == 0 {
			sign = -1.0
		}
		vec[idx] += sign
	}

	// Also add bigrams for slightly better semantic capture
	for i := 0; i < len(tokens)-1; i++ {
		bigram := tokens[i] + "_" + tokens[i+1]
		h := fnv.New64a()
		h.Write([]byte(bigram))
		hash := h.Sum64()

		idx := int(hash % uint64(e.dims))
		sign := float32(0.5) // bigrams weighted less
		if hash&1 == 0 {
			sign = -0.5
		}
		vec[idx] += sign
	}

	// L2 normalize
	var mag float64
	for _, v := range vec {
		mag += float64(v) * float64(v)
	}
	mag = math.Sqrt(mag)
	if mag > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / mag)
		}
	}

	return vec
}

// Dimensions returns the embedding dimension.
func (e *HashEmbedder) Dimensions() int {
	return e.dims
}
