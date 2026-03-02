package search

import (
	"math"
	"testing"
)

func TestSemanticIndex_BasicSearch(t *testing.T) {
	idx := NewSemanticIndex(8) // small dimensions for testing

	// Create some test vectors
	v1 := Vector{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := Vector{0, 1, 0, 0, 0, 0, 0, 0}
	v3 := Vector{0.9, 0.1, 0, 0, 0, 0, 0, 0} // similar to v1

	idx.Index("agent-1", v1)
	idx.Index("agent-2", v2)
	idx.Index("agent-3", v3)

	if idx.Count() != 3 {
		t.Errorf("expected 3 vectors, got %d", idx.Count())
	}

	// Search near v1
	results := idx.Search(v1, 3)
	if len(results) < 2 {
		t.Fatal("expected at least 2 results")
	}

	// agent-1 should be most similar to itself, agent-3 second
	if results[0].DocID != "agent-1" {
		t.Errorf("expected agent-1 as top result, got %s", results[0].DocID)
	}
	if results[1].DocID != "agent-3" {
		t.Errorf("expected agent-3 as second result, got %s", results[1].DocID)
	}
}

func TestSemanticIndex_Remove(t *testing.T) {
	idx := NewSemanticIndex(4)
	idx.Index("agent-1", Vector{1, 0, 0, 0})

	if idx.Count() != 1 {
		t.Errorf("expected 1 vector, got %d", idx.Count())
	}

	idx.Remove("agent-1")

	if idx.Count() != 0 {
		t.Errorf("expected 0 vectors after removal, got %d", idx.Count())
	}
}

func TestNormalize(t *testing.T) {
	v := Vector{3, 4}
	n := normalize(v)

	// Magnitude should be 1.0
	mag := math.Sqrt(float64(n[0]*n[0]) + float64(n[1]*n[1]))
	if math.Abs(mag-1.0) > 0.001 {
		t.Errorf("expected unit vector, got magnitude %f", mag)
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := normalize(Vector{1, 0})
	b := normalize(Vector{0, 1})
	c := normalize(Vector{1, 0})

	// Orthogonal vectors should have ~0 similarity
	sim := cosineSimilarity(a, b)
	if math.Abs(sim) > 0.001 {
		t.Errorf("expected ~0 similarity for orthogonal vectors, got %f", sim)
	}

	// Identical vectors should have ~1 similarity
	sim = cosineSimilarity(a, c)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("expected ~1 similarity for identical vectors, got %f", sim)
	}
}

func TestHashEmbedder(t *testing.T) {
	embedder := NewHashEmbedder(384)

	v1 := embedder.Embed("python code review security")
	v2 := embedder.Embed("python code review security")
	v3 := embedder.Embed("translate japanese legal documents")

	if len(v1) != 384 {
		t.Errorf("expected 384 dimensions, got %d", len(v1))
	}

	// Same text should produce identical vectors
	sim12 := cosineSimilarity(v1, v2)
	if math.Abs(sim12-1.0) > 0.001 {
		t.Errorf("expected identical vectors for same text, got similarity %f", sim12)
	}

	// Different text should produce different vectors
	sim13 := cosineSimilarity(v1, v3)
	if sim13 > 0.9 {
		t.Errorf("expected lower similarity for different text, got %f", sim13)
	}
}
