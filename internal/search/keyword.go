// Package search implements BM25 keyword search and semantic vector search.
package search

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// BM25 parameters
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Document represents an indexed document for BM25 search.
type Document struct {
	ID       string
	Name     string
	Summary  string
	Tags     []string
	Category string
	// Combined text used for indexing
	text   string
	terms  map[string]int // term frequencies
	length int            // total number of terms
}

// KeywordIndex is a BM25-based keyword search engine.
type KeywordIndex struct {
	mu        sync.RWMutex
	docs      map[string]*Document
	df        map[string]int // document frequency per term
	avgDocLen float64
	docCount  int
}

// NewKeywordIndex creates a new BM25 keyword search index.
func NewKeywordIndex() *KeywordIndex {
	return &KeywordIndex{
		docs: make(map[string]*Document),
		df:   make(map[string]int),
	}
}

// IndexDocument adds or updates a document in the index.
func (idx *KeywordIndex) IndexDocument(id, name, summary, category string, tags []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old doc if exists (update document frequencies)
	if oldDoc, exists := idx.docs[id]; exists {
		for term := range oldDoc.terms {
			idx.df[term]--
			if idx.df[term] <= 0 {
				delete(idx.df, term)
			}
		}
		idx.docCount--
	}

	// Build combined text
	parts := []string{
		strings.ToLower(name),
		strings.ToLower(summary),
		strings.ToLower(category),
	}
	for _, tag := range tags {
		parts = append(parts, strings.ToLower(tag))
	}
	text := strings.Join(parts, " ")

	// Tokenize and count term frequencies
	terms := tokenize(text)
	tf := make(map[string]int)
	for _, term := range terms {
		tf[term]++
	}

	doc := &Document{
		ID:       id,
		Name:     name,
		Summary:  summary,
		Tags:     tags,
		Category: category,
		text:     text,
		terms:    tf,
		length:   len(terms),
	}

	idx.docs[id] = doc
	idx.docCount++

	// Update document frequencies
	for term := range tf {
		idx.df[term]++
	}

	// Recalculate average document length
	totalLen := 0
	for _, d := range idx.docs {
		totalLen += d.length
	}
	if idx.docCount > 0 {
		idx.avgDocLen = float64(totalLen) / float64(idx.docCount)
	}
}

// RemoveDocument removes a document from the index.
func (idx *KeywordIndex) RemoveDocument(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc, exists := idx.docs[id]
	if !exists {
		return
	}

	for term := range doc.terms {
		idx.df[term]--
		if idx.df[term] <= 0 {
			delete(idx.df, term)
		}
	}

	delete(idx.docs, id)
	idx.docCount--

	// Recalculate average document length
	totalLen := 0
	for _, d := range idx.docs {
		totalLen += d.length
	}
	if idx.docCount > 0 {
		idx.avgDocLen = float64(totalLen) / float64(idx.docCount)
	} else {
		idx.avgDocLen = 0
	}
}

// SearchResult holds a document ID and its BM25 score.
type KeywordResult struct {
	DocID string
	Score float64
}

// Search performs a BM25 search and returns results sorted by score.
func (idx *KeywordIndex) Search(query string, maxResults int) []KeywordResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.docCount == 0 {
		return nil
	}

	queryTerms := tokenize(strings.ToLower(query))
	if len(queryTerms) == 0 {
		return nil
	}

	scores := make(map[string]float64)

	for _, qTerm := range queryTerms {
		docFreq, exists := idx.df[qTerm]
		if !exists {
			continue
		}

		// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
		idf := math.Log((float64(idx.docCount)-float64(docFreq)+0.5)/(float64(docFreq)+0.5) + 1.0)

		for id, doc := range idx.docs {
			termFreq := doc.terms[qTerm]
			if termFreq == 0 {
				continue
			}

			// BM25 score for this term-document pair
			tf := float64(termFreq)
			docLen := float64(doc.length)
			numerator := tf * (bm25K1 + 1)
			denominator := tf + bm25K1*(1-bm25B+bm25B*docLen/idx.avgDocLen)
			scores[id] += idf * numerator / denominator
		}
	}

	// Sort by score descending
	var results []KeywordResult
	for id, score := range scores {
		results = append(results, KeywordResult{DocID: id, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// Count returns the number of documents in the index.
func (idx *KeywordIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}

// tokenize splits text on non-alphanumeric characters into lowercase terms.
// Tokens containing hyphens or underscores are ALSO emitted as their split
// parts, so "job-scrapper-service" yields {"job-scrapper-service", "job",
// "scrapper", "service"}. This lets natural-language queries match compound
// indexed names while preserving the higher BM25 score on exact compound matches.
func tokenize(text string) []string {
	var terms []string
	current := strings.Builder{}

	flush := func() {
		if current.Len() < 2 {
			current.Reset()
			return
		}
		full := current.String()
		terms = append(terms, full)
		if strings.ContainsAny(full, "-_") {
			for _, part := range strings.FieldsFunc(full, func(r rune) bool {
				return r == '-' || r == '_'
			}) {
				if part != full && len(part) >= 2 {
					terms = append(terms, part)
				}
			}
		}
		current.Reset()
	}

	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	return terms
}
