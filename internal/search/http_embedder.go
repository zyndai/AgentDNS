package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func init() {
	RegisterEmbedder("http", func(cfg EmbedderConfig) (Embedder, error) {
		if cfg.Endpoint == "" {
			return nil, fmt.Errorf("embedding_endpoint is required for http backend")
		}
		dims := cfg.Dimensions
		if dims <= 0 {
			dims = 384
		}
		return NewHTTPEmbedder(cfg.Endpoint, dims), nil
	})
}

// HTTPEmbedder calls an external HTTP embedding service.
// It accepts two response formats:
//
//  1. Simple:   {"embedding": [0.1, 0.2, ...]}
//  2. OpenAI:   {"data": [{"embedding": [0.1, 0.2, ...]}]}
//
// The request body sent is: {"input": "<text>", "dimensions": <N>}
// This is compatible with OpenAI, Ollama, and most self-hosted models.
//
// Example config.toml:
//
//	[search]
//	embedding_backend  = "http"
//	embedding_endpoint = "http://localhost:11434/api/embeddings"
//	embedding_dimensions = 384
type HTTPEmbedder struct {
	endpoint   string
	dimensions int
	client     *http.Client
}

// NewHTTPEmbedder creates an embedder that calls the given endpoint.
func NewHTTPEmbedder(endpoint string, dimensions int) *HTTPEmbedder {
	return &HTTPEmbedder{
		endpoint:   endpoint,
		dimensions: dimensions,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

type httpEmbedRequest struct {
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

// httpEmbedResponse handles both simple and OpenAI-style responses.
type httpEmbedResponse struct {
	// Simple format: {"embedding": [...]}
	Embedding []float32 `json:"embedding"`
	// OpenAI format: {"data": [{"embedding": [...]}]}
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed sends the text to the HTTP endpoint and returns the embedding vector.
// On error, returns a zero vector so search degrades gracefully.
func (e *HTTPEmbedder) Embed(text string) Vector {
	reqBody, err := json.Marshal(httpEmbedRequest{
		Input:      text,
		Dimensions: e.dimensions,
	})
	if err != nil {
		return make(Vector, e.dimensions)
	}

	resp, err := e.client.Post(e.endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return make(Vector, e.dimensions)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return make(Vector, e.dimensions)
	}

	var result httpEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return make(Vector, e.dimensions)
	}

	// Prefer simple format, fall back to OpenAI format.
	raw := result.Embedding
	if len(raw) == 0 && len(result.Data) > 0 {
		raw = result.Data[0].Embedding
	}
	if len(raw) == 0 {
		return make(Vector, e.dimensions)
	}

	// Trim or pad to expected dimensions.
	vec := make(Vector, e.dimensions)
	copy(vec, raw)

	return normalize(vec)
}

// Dimensions returns the expected embedding dimension.
func (e *HTTPEmbedder) Dimensions() int {
	return e.dimensions
}
