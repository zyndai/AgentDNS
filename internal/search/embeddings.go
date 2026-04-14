package search

import (
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"strings"
	"sync"
)

// Embedder generates embedding vectors for text.
// Implement this interface and call RegisterEmbedder to plug in a custom model.
//
// Example (place in your own package):
//
//	func init() {
//	    search.RegisterEmbedder("my-model", func(cfg search.EmbedderConfig) (search.Embedder, error) {
//	        return NewMyEmbedder(cfg.Endpoint, cfg.Dimensions)
//	    })
//	}
type Embedder interface {
	Embed(text string) Vector
	Dimensions() int
}

// EmbedderConfig carries configuration to an embedder factory.
type EmbedderConfig struct {
	// Dimensions is the output vector size expected by the search engine.
	Dimensions int
	// ModelDir is the local directory for model files (used by "onnx" backend).
	ModelDir string
	// ModelName is the specific model to use (e.g., "all-MiniLM-L6-v2", "bge-small-en-v1.5").
	ModelName string
	// Endpoint is the HTTP URL for remote embedding services (used by "http" backend).
	Endpoint string
}

// EmbedderFactory creates an Embedder from config. Return an error if the
// backend is unavailable (e.g. missing model files, native lib not linked,
// model download blocked). Errors from a factory bubble all the way up and
// fail the server startup — there is intentionally no fallback.
type EmbedderFactory func(cfg EmbedderConfig) (Embedder, error)

var (
	embedderMu       sync.RWMutex
	embedderRegistry = map[string]EmbedderFactory{}
)

// RegisterEmbedder registers a named embedder factory. Safe to call from init().
// Built-in names: "hash", "onnx" (requires CGO + model files), "http".
// Registering an existing name overwrites the previous factory.
func RegisterEmbedder(name string, factory EmbedderFactory) {
	embedderMu.Lock()
	defer embedderMu.Unlock()
	embedderRegistry[name] = factory
}

// NewEmbedderFromConfig returns the embedder for the given backend name.
// Fail-fast by design: if `backend` is empty, unregistered, or if the factory
// returns an error, this returns an error and the server startup MUST abort.
// There is no silent fallback to the hash embedder — hash is only available
// if you explicitly set `embedding_backend = "hash"` in config.
func NewEmbedderFromConfig(backend, modelName, modelDir, endpoint string, dims int) (Embedder, error) {
	if backend == "" {
		return nil, fmt.Errorf("search: embedding_backend is required in config (got empty string)")
	}
	if dims <= 0 {
		return nil, fmt.Errorf("search: embedding_dimensions must be > 0 (got %d)", dims)
	}

	cfg := EmbedderConfig{
		Dimensions: dims,
		ModelDir:   modelDir,
		ModelName:  modelName,
		Endpoint:   endpoint,
	}

	embedderMu.RLock()
	factory, ok := embedderRegistry[backend]
	registered := make([]string, 0, len(embedderRegistry))
	for name := range embedderRegistry {
		registered = append(registered, name)
	}
	embedderMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf(
			"search: embedding_backend %q is not registered (available: %v). "+
				"If you expected %q to be available, check that the binary was built "+
				"with the required build tags (onnx needs CGO_ENABLED=1) and that the "+
				"native libraries are installed",
			backend, registered, backend)
	}

	embedder, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("search: embedding_backend %q failed to initialize: %w", backend, err)
	}
	if embedder == nil {
		return nil, fmt.Errorf("search: embedding_backend %q factory returned nil embedder without an error", backend)
	}

	if modelName != "" {
		log.Printf("search: using embedding backend %q with model %q (dims=%d)", backend, modelName, dims)
	} else {
		log.Printf("search: using embedding backend %q (dims=%d)", backend, dims)
	}
	return embedder, nil
}

// ListEmbedders returns the names of all registered embedder backends.
func ListEmbedders() []string {
	embedderMu.RLock()
	defer embedderMu.RUnlock()
	names := make([]string, 0, len(embedderRegistry))
	for name := range embedderRegistry {
		names = append(names, name)
	}
	return names
}

// -- Built-in: HashEmbedder --------------------------------------------------

func init() {
	RegisterEmbedder("hash", func(cfg EmbedderConfig) (Embedder, error) {
		if cfg.Dimensions <= 0 {
			return nil, fmt.Errorf("dimensions must be > 0")
		}
		return NewHashEmbedder(cfg.Dimensions), nil
	})
}

// HashEmbedder is a simple feature-hashing embedder (no ML model required).
// It uses FNV hashing to project tokens into a fixed-dimension space.
// Fast and dependency-free, but does NOT understand synonyms or semantics.
// Suitable for development and as a fallback when real models are unavailable.
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

		idx := int(hash % uint64(e.dims))
		sign := float32(1.0)
		if hash&1 == 0 {
			sign = -1.0
		}
		vec[idx] += sign
	}

	// Bigrams for slightly better term co-occurrence capture
	for i := 0; i < len(tokens)-1; i++ {
		bigram := tokens[i] + "_" + tokens[i+1]
		h := fnv.New64a()
		h.Write([]byte(bigram))
		hash := h.Sum64()

		idx := int(hash % uint64(e.dims))
		sign := float32(0.5)
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
