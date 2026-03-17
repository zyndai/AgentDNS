package search

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// ImprovedKeywordIndex is an enhanced BM25 search engine with:
// - Field-specific boosting (name > tags > category > summary)
// - Advanced tokenization (stemming, stopwords, synonyms)
// - Phrase matching bonuses
// - Configurable scoring
type ImprovedKeywordIndex struct {
	mu        sync.RWMutex
	docs      map[string]*FieldedDocument
	df        map[string]int     // document frequency per term
	avgDocLen map[string]float64 // average length per field
	docCount  int
	tokenizer *AdvancedTokenizer
	boosts    FieldBoosts
	bm25K1    float64
	bm25B     float64
}

// FieldedDocument stores terms for each field separately for field boosting.
type FieldedDocument struct {
	ID       string
	Name     string
	Summary  string
	Tags     []string
	Category string

	// Term frequencies per field
	nameTerms     map[string]int
	tagsTerms     map[string]int
	summaryTerms  map[string]int
	categoryTerms map[string]int

	// Field lengths
	nameLen     int
	tagsLen     int
	summaryLen  int
	categoryLen int
}

// NewImprovedKeywordIndex creates an enhanced BM25 index.
func NewImprovedKeywordIndex() *ImprovedKeywordIndex {
	return &ImprovedKeywordIndex{
		docs: make(map[string]*FieldedDocument),
		df:   make(map[string]int),
		avgDocLen: map[string]float64{
			"name":     0,
			"tags":     0,
			"summary":  0,
			"category": 0,
		},
		tokenizer: NewAdvancedTokenizer(DefaultTokenizerConfig()),
		boosts:    DefaultFieldBoosts(),
		bm25K1:    1.2,
		bm25B:     0.75,
	}
}

// IndexDocument adds or updates a document with field-aware indexing.
func (idx *ImprovedKeywordIndex) IndexDocument(id, name, summary, category string, tags []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old doc if exists
	if oldDoc, exists := idx.docs[id]; exists {
		idx.removeTerms(oldDoc)
		idx.docCount--
	}

	// Tokenize each field separately
	nameTerms := idx.tokenizer.Tokenize(name)
	summaryTerms := idx.tokenizer.Tokenize(summary)
	categoryTerms := idx.tokenizer.Tokenize(category)
	tagsTerms := []string{}
	for _, tag := range tags {
		tagsTerms = append(tagsTerms, idx.tokenizer.Tokenize(tag)...)
	}

	// Build term frequency maps
	nameTF := make(map[string]int)
	for _, t := range nameTerms {
		nameTF[t]++
	}
	tagsTF := make(map[string]int)
	for _, t := range tagsTerms {
		tagsTF[t]++
	}
	summaryTF := make(map[string]int)
	for _, t := range summaryTerms {
		summaryTF[t]++
	}
	categoryTF := make(map[string]int)
	for _, t := range categoryTerms {
		categoryTF[t]++
	}

	doc := &FieldedDocument{
		ID:            id,
		Name:          name,
		Summary:       summary,
		Tags:          tags,
		Category:      category,
		nameTerms:     nameTF,
		tagsTerms:     tagsTF,
		summaryTerms:  summaryTF,
		categoryTerms: categoryTF,
		nameLen:       len(nameTerms),
		tagsLen:       len(tagsTerms),
		summaryLen:    len(summaryTerms),
		categoryLen:   len(categoryTerms),
	}

	idx.docs[id] = doc
	idx.docCount++

	// Update document frequencies
	allTerms := make(map[string]bool)
	for t := range nameTF {
		allTerms[t] = true
	}
	for t := range tagsTF {
		allTerms[t] = true
	}
	for t := range summaryTF {
		allTerms[t] = true
	}
	for t := range categoryTF {
		allTerms[t] = true
	}
	for t := range allTerms {
		idx.df[t]++
	}

	// Update average field lengths
	idx.recalculateAvgLengths()
}

// RemoveDocument removes a document from the index.
func (idx *ImprovedKeywordIndex) RemoveDocument(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc, exists := idx.docs[id]
	if !exists {
		return
	}

	idx.removeTerms(doc)
	delete(idx.docs, id)
	idx.docCount--
	idx.recalculateAvgLengths()
}

// removeTerms decrements document frequency for all terms in a doc.
func (idx *ImprovedKeywordIndex) removeTerms(doc *FieldedDocument) {
	allTerms := make(map[string]bool)
	for t := range doc.nameTerms {
		allTerms[t] = true
	}
	for t := range doc.tagsTerms {
		allTerms[t] = true
	}
	for t := range doc.summaryTerms {
		allTerms[t] = true
	}
	for t := range doc.categoryTerms {
		allTerms[t] = true
	}
	for t := range allTerms {
		idx.df[t]--
		if idx.df[t] <= 0 {
			delete(idx.df, t)
		}
	}
}

// recalculateAvgLengths updates average field lengths.
func (idx *ImprovedKeywordIndex) recalculateAvgLengths() {
	if idx.docCount == 0 {
		idx.avgDocLen = map[string]float64{
			"name": 0, "tags": 0, "summary": 0, "category": 0,
		}
		return
	}

	totalName, totalTags, totalSummary, totalCategory := 0, 0, 0, 0
	for _, doc := range idx.docs {
		totalName += doc.nameLen
		totalTags += doc.tagsLen
		totalSummary += doc.summaryLen
		totalCategory += doc.categoryLen
	}

	idx.avgDocLen["name"] = float64(totalName) / float64(idx.docCount)
	idx.avgDocLen["tags"] = float64(totalTags) / float64(idx.docCount)
	idx.avgDocLen["summary"] = float64(totalSummary) / float64(idx.docCount)
	idx.avgDocLen["category"] = float64(totalCategory) / float64(idx.docCount)
}

// Search performs field-boosted BM25 search with phrase matching.
func (idx *ImprovedKeywordIndex) Search(query string, maxResults int) []KeywordResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.docCount == 0 {
		return nil
	}

	// Tokenize query
	queryTerms := idx.tokenizer.Tokenize(strings.ToLower(query))
	if len(queryTerms) == 0 {
		return nil
	}

	// Detect phrases (quoted text)
	phrases := extractPhrases(query)

	scores := make(map[string]float64)

	// Calculate BM25 score for each query term across all fields
	for _, qTerm := range queryTerms {
		docFreq, exists := idx.df[qTerm]
		if !exists {
			continue
		}

		// IDF component
		idf := math.Log((float64(idx.docCount)-float64(docFreq)+0.5)/(float64(docFreq)+0.5) + 1.0)

		for id, doc := range idx.docs {
			// Score each field separately with its boost
			nameScore := idx.bm25FieldScore(qTerm, doc.nameTerms, doc.nameLen, idx.avgDocLen["name"], idf)
			tagsScore := idx.bm25FieldScore(qTerm, doc.tagsTerms, doc.tagsLen, idx.avgDocLen["tags"], idf)
			summaryScore := idx.bm25FieldScore(qTerm, doc.summaryTerms, doc.summaryLen, idx.avgDocLen["summary"], idf)
			categoryScore := idx.bm25FieldScore(qTerm, doc.categoryTerms, doc.categoryLen, idx.avgDocLen["category"], idf)

			// Apply field boosts
			fieldScore := nameScore*idx.boosts.Name +
				tagsScore*idx.boosts.Tags +
				summaryScore*idx.boosts.Summary +
				categoryScore*idx.boosts.Category

			scores[id] += fieldScore
		}
	}

	// Apply phrase matching bonus
	for id, doc := range idx.docs {
		for _, phrase := range phrases {
			if idx.containsPhrase(doc, phrase) {
				scores[id] += 5.0 // significant bonus for exact phrase match
			}
		}
	}

	// Sort by score
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

// bm25FieldScore calculates BM25 score for a single field.
func (idx *ImprovedKeywordIndex) bm25FieldScore(term string, termFreqs map[string]int, docLen int, avgDocLen, idf float64) float64 {
	tf := float64(termFreqs[term])
	if tf == 0 {
		return 0
	}

	dl := float64(docLen)
	numerator := tf * (idx.bm25K1 + 1)
	denominator := tf + idx.bm25K1*(1-idx.bm25B+idx.bm25B*dl/avgDocLen)

	return idf * numerator / denominator
}

// containsPhrase checks if a document contains an exact phrase.
func (idx *ImprovedKeywordIndex) containsPhrase(doc *FieldedDocument, phrase string) bool {
	phrase = strings.ToLower(phrase)
	return strings.Contains(strings.ToLower(doc.Name), phrase) ||
		strings.Contains(strings.ToLower(doc.Summary), phrase) ||
		strings.Contains(strings.ToLower(doc.Category), phrase) ||
		containsInSlice(doc.Tags, phrase)
}

// extractPhrases finds quoted phrases in the query.
func extractPhrases(query string) []string {
	var phrases []string
	inQuote := false
	current := strings.Builder{}

	for _, r := range query {
		if r == '"' {
			if inQuote {
				// End of phrase
				phrases = append(phrases, current.String())
				current.Reset()
			}
			inQuote = !inQuote
		} else if inQuote {
			current.WriteRune(r)
		}
	}

	return phrases
}

func containsInSlice(slice []string, substr string) bool {
	substr = strings.ToLower(substr)
	for _, s := range slice {
		if strings.Contains(strings.ToLower(s), substr) {
			return true
		}
	}
	return false
}

// Count returns the number of documents in the index.
func (idx *ImprovedKeywordIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}
