package search

import (
	"strings"
	"unicode"
)

// TokenizerConfig configures advanced tokenization options.
type TokenizerConfig struct {
	MinTermLength int                 // Minimum term length (default: 2)
	Stemming      bool                // Apply Porter stemmer
	Stopwords     map[string]bool     // Common words to ignore
	Synonyms      map[string][]string // Synonym expansion
	NGramMin      int                 // Min n-gram size (0 = disabled)
	NGramMax      int                 // Max n-gram size
}

// DefaultTokenizerConfig returns sensible defaults.
func DefaultTokenizerConfig() TokenizerConfig {
	return TokenizerConfig{
		MinTermLength: 2,
		Stemming:      true,
		Stopwords:     DefaultStopwords(),
		Synonyms:      DefaultSynonyms(),
		NGramMin:      0,
		NGramMax:      0,
	}
}

// DefaultStopwords returns common English stopwords.
func DefaultStopwords() map[string]bool {
	words := []string{
		"a", "an", "and", "are", "as", "at", "be", "by", "for",
		"from", "has", "he", "in", "is", "it", "its", "of", "on",
		"that", "the", "to", "was", "will", "with",
	}
	set := make(map[string]bool)
	for _, w := range words {
		set[w] = true
	}
	return set
}

// DefaultSynonyms returns common synonym mappings for agent search.
func DefaultSynonyms() map[string][]string {
	return map[string][]string{
		"ai":         {"artificial-intelligence", "machine-learning", "ml"},
		"ml":         {"machine-learning", "ai"},
		"llm":        {"large-language-model", "language-model"},
		"gpt":        {"openai", "chatgpt"},
		"code":       {"programming", "software", "development"},
		"review":     {"analysis", "audit", "check"},
		"translate":  {"translation", "translator"},
		"python":     {"py"},
		"javascript": {"js", "node"},
		"typescript": {"ts"},
	}
}

// AdvancedTokenizer performs tokenization with stemming, stopwords, and synonyms.
type AdvancedTokenizer struct {
	cfg TokenizerConfig
}

// NewAdvancedTokenizer creates a new tokenizer with the given config.
func NewAdvancedTokenizer(cfg TokenizerConfig) *AdvancedTokenizer {
	return &AdvancedTokenizer{cfg: cfg}
}

// Tokenize splits text into processed terms.
func (t *AdvancedTokenizer) Tokenize(text string) []string {
	// Step 1: Basic tokenization (split on non-alphanumeric, preserve hyphens)
	raw := t.basicTokenize(strings.ToLower(text))

	// Step 2: Filter stopwords
	var filtered []string
	for _, term := range raw {
		if len(term) < t.cfg.MinTermLength {
			continue
		}
		if t.cfg.Stopwords != nil && t.cfg.Stopwords[term] {
			continue
		}
		filtered = append(filtered, term)
	}

	// Step 3: Apply stemming
	var stemmed []string
	if t.cfg.Stemming {
		for _, term := range filtered {
			stemmed = append(stemmed, porterStem(term))
		}
	} else {
		stemmed = filtered
	}

	// Step 4: Expand with synonyms
	result := make([]string, 0, len(stemmed)*2)
	result = append(result, stemmed...)

	if t.cfg.Synonyms != nil {
		for _, term := range filtered { // Use unstemmed for synonym matching
			if syns, ok := t.cfg.Synonyms[term]; ok {
				result = append(result, syns...)
			}
		}
	}

	// Step 5: Generate n-grams
	if t.cfg.NGramMin > 0 && t.cfg.NGramMax > 0 {
		ngrams := t.generateNGrams(filtered, t.cfg.NGramMin, t.cfg.NGramMax)
		result = append(result, ngrams...)
	}

	return result
}

// basicTokenize splits text on non-alphanumeric characters, preserving hyphens and
// underscores. When a token contains hyphens or underscores, it ALSO emits the
// split parts — so "job-scrapper-service" yields {"job-scrapper-service", "job",
// "scrapper", "service"}. This lets a natural-language query ("give me a job
// scrapper service") match a compound indexed name, while preserving exact
// compound matches (which still score highest via BM25 + name field boost).
func (t *AdvancedTokenizer) basicTokenize(text string) []string {
	var terms []string
	current := strings.Builder{}

	flush := func() {
		if current.Len() == 0 {
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
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	return terms
}

// generateNGrams creates n-grams from the token sequence.
func (t *AdvancedTokenizer) generateNGrams(tokens []string, minN, maxN int) []string {
	var ngrams []string
	for n := minN; n <= maxN && n <= len(tokens); n++ {
		for i := 0; i <= len(tokens)-n; i++ {
			ngram := strings.Join(tokens[i:i+n], "_")
			ngrams = append(ngrams, ngram)
		}
	}
	return ngrams
}

// porterStem is a simplified Porter stemmer implementation.
// This is a minimal version for demonstration. For production, use a proper stemming library.
func porterStem(word string) string {
	// Simple suffix removal rules (very basic Porter stemmer)
	// In production, use github.com/kljensen/snowball or similar

	if len(word) < 4 {
		return word
	}

	// Rule: Remove common suffixes
	suffixes := []string{
		"ing", "ed", "ly", "ness", "ment", "able", "ible",
		"tion", "sion", "ation", "ies", "es", "s",
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(word, suffix) && len(word)-len(suffix) >= 3 {
			return word[:len(word)-len(suffix)]
		}
	}

	return word
}

// FieldBoosts defines relative importance of different fields in search.
type FieldBoosts struct {
	Name     float64
	Tags     float64
	Summary  float64
	Category float64
}

// DefaultFieldBoosts returns sensible defaults.
func DefaultFieldBoosts() FieldBoosts {
	return FieldBoosts{
		Name:     3.0, // Name matches are 3x more important
		Tags:     2.0, // Tag matches are 2x
		Summary:  1.0, // Summary is baseline
		Category: 1.5, // Category is 1.5x
	}
}
