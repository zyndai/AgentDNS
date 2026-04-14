package search

import (
	"testing"
)

func TestKeywordIndex_BasicSearch(t *testing.T) {
	idx := NewKeywordIndex()

	// Index some documents
	idx.IndexDocument("agent-1", "CodeReviewer", "Reviews Python code for security issues", "developer-tools", []string{"python", "security", "code-review"})
	idx.IndexDocument("agent-2", "Translator", "Translates documents between English and Japanese", "translation", []string{"english", "japanese", "legal"})
	idx.IndexDocument("agent-3", "DataAnalyzer", "Analyzes data sets and produces visualizations", "analytics", []string{"data", "visualization", "python"})

	if idx.Count() != 3 {
		t.Errorf("expected 3 documents, got %d", idx.Count())
	}

	// Search for "python security"
	results := idx.Search("python security", 10)
	if len(results) == 0 {
		t.Fatal("expected results for 'python security'")
	}
	if results[0].DocID != "agent-1" {
		t.Errorf("expected agent-1 as top result, got %s", results[0].DocID)
	}

	// Search for "translate japanese"
	results = idx.Search("translate japanese", 10)
	if len(results) == 0 {
		t.Fatal("expected results for 'translate japanese'")
	}
	if results[0].DocID != "agent-2" {
		t.Errorf("expected agent-2 as top result, got %s", results[0].DocID)
	}
}

func TestKeywordIndex_RemoveDocument(t *testing.T) {
	idx := NewKeywordIndex()
	idx.IndexDocument("agent-1", "Test", "Test agent", "test", []string{"test"})

	if idx.Count() != 1 {
		t.Errorf("expected 1 document, got %d", idx.Count())
	}

	idx.RemoveDocument("agent-1")

	if idx.Count() != 0 {
		t.Errorf("expected 0 documents after removal, got %d", idx.Count())
	}

	results := idx.Search("test", 10)
	if len(results) != 0 {
		t.Errorf("expected no results after removal, got %d", len(results))
	}
}

func TestKeywordIndex_UpdateDocument(t *testing.T) {
	idx := NewKeywordIndex()
	idx.IndexDocument("agent-1", "OldName", "Old description", "old-category", []string{"old-tag"})

	// Update (re-index with same ID)
	idx.IndexDocument("agent-1", "NewName", "New description about Python", "new-category", []string{"python"})

	if idx.Count() != 1 {
		t.Errorf("expected 1 document after update, got %d", idx.Count())
	}

	// Should find by new content
	results := idx.Search("python", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'python', got %d", len(results))
	}
	if results[0].DocID != "agent-1" {
		t.Errorf("expected agent-1, got %s", results[0].DocID)
	}

	// Should NOT find by old content (if old tag not in new content)
	results = idx.Search("old-tag", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'old-tag', got %d", len(results))
	}
}

func TestKeywordIndex_EmptyQuery(t *testing.T) {
	idx := NewKeywordIndex()
	idx.IndexDocument("agent-1", "Test", "Test agent", "test", []string{"test"})

	results := idx.Search("", 10)
	if len(results) != 0 {
		t.Errorf("expected no results for empty query, got %d", len(results))
	}
}

func TestKeywordIndex_EmptyIndex(t *testing.T) {
	idx := NewKeywordIndex()

	results := idx.Search("test", 10)
	if results != nil {
		t.Errorf("expected nil results for empty index, got %v", results)
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello world", 2},
		{"code-review", 3}, // hyphenated: compound + split parts ("code-review", "code", "review")
		{"a b c", 0},       // single chars filtered out
		{"python javascript go rust", 4},
		{"", 0},
	}

	for _, tt := range tests {
		tokens := tokenize(tt.input)
		if len(tokens) != tt.expected {
			t.Errorf("tokenize(%q): expected %d tokens, got %d: %v", tt.input, tt.expected, len(tokens), tokens)
		}
	}
}
