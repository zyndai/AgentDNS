// Package ranking implements the scoring and ranking algorithms for search results.
package ranking

import (
	"math"
	"sort"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/models"
)

// Ranker computes final scores for search results using weighted scoring or RRF.
type Ranker struct {
	weights config.RankingConfig
}

// NewRanker creates a new Ranker with the given weight configuration.
func NewRanker(weights config.RankingConfig) *Ranker {
	return &Ranker{weights: weights}
}

// CandidateResult holds intermediate scoring data for a search candidate.
type CandidateResult struct {
	AgentID      string
	Name         string
	Summary      string
	Category     string
	Tags         []string
	AgentURL     string
	HomeRegistry string
	UpdatedAt    string

	// Raw scores from different sources
	TextRelevance      float64
	SemanticSimilarity float64
	TrustScore         float64
	Freshness          float64
	Availability       float64

	// Final computed score
	FinalScore float64

	// Optional enriched Agent Card
	Card *models.AgentCard
}

// RankWeighted scores candidates using the weighted linear combination.
// final_score = w1*text + w2*semantic + w3*trust + w4*freshness + w5*availability
func (r *Ranker) RankWeighted(candidates []*CandidateResult) []*CandidateResult {
	for _, c := range candidates {
		c.Freshness = computeFreshness(c.UpdatedAt)
		c.FinalScore = r.weights.TextRelevanceWeight*c.TextRelevance +
			r.weights.SemanticSimilarityWeight*c.SemanticSimilarity +
			r.weights.TrustWeight*c.TrustScore +
			r.weights.FreshnessWeight*c.Freshness +
			r.weights.AvailabilityWeight*c.Availability
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].FinalScore > candidates[j].FinalScore
	})

	return candidates
}

// RankRRF scores candidates using Reciprocal Rank Fusion.
// RRF_score(agent) = sum(1/(k + rank_i(agent))) for each ranking source
// This method doesn't require weight tuning and works well out of the box.
func (r *Ranker) RankRRF(candidates []*CandidateResult) []*CandidateResult {
	const k = 60.0 // standard RRF constant

	// Create separate ranked lists
	textRanked := make([]*CandidateResult, len(candidates))
	semanticRanked := make([]*CandidateResult, len(candidates))
	trustRanked := make([]*CandidateResult, len(candidates))

	copy(textRanked, candidates)
	copy(semanticRanked, candidates)
	copy(trustRanked, candidates)

	// Sort each list by its respective score
	sort.Slice(textRanked, func(i, j int) bool {
		return textRanked[i].TextRelevance > textRanked[j].TextRelevance
	})
	sort.Slice(semanticRanked, func(i, j int) bool {
		return semanticRanked[i].SemanticSimilarity > semanticRanked[j].SemanticSimilarity
	})
	sort.Slice(trustRanked, func(i, j int) bool {
		return trustRanked[i].TrustScore > trustRanked[j].TrustScore
	})

	// Build rank maps
	textRank := make(map[string]int)
	semanticRank := make(map[string]int)
	trustRank := make(map[string]int)

	for i, c := range textRanked {
		textRank[c.AgentID] = i + 1
	}
	for i, c := range semanticRanked {
		semanticRank[c.AgentID] = i + 1
	}
	for i, c := range trustRanked {
		trustRank[c.AgentID] = i + 1
	}

	// Compute RRF score
	for _, c := range candidates {
		c.FinalScore = 1.0/(k+float64(textRank[c.AgentID])) +
			1.0/(k+float64(semanticRank[c.AgentID])) +
			1.0/(k+float64(trustRank[c.AgentID]))
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].FinalScore > candidates[j].FinalScore
	})

	return candidates
}

// Deduplicate removes duplicate candidates by agent_id, keeping the highest scored.
func Deduplicate(candidates []*CandidateResult) []*CandidateResult {
	seen := make(map[string]bool)
	var result []*CandidateResult
	for _, c := range candidates {
		if !seen[c.AgentID] {
			seen[c.AgentID] = true
			result = append(result, c)
		}
	}
	return result
}

// ToSearchResults converts ranked candidates to API SearchResult format.
func ToSearchResults(candidates []*CandidateResult) []models.SearchResult {
	results := make([]models.SearchResult, len(candidates))
	for i, c := range candidates {
		results[i] = models.SearchResult{
			AgentID:      c.AgentID,
			Name:         c.Name,
			Summary:      c.Summary,
			Category:     c.Category,
			Tags:         c.Tags,
			AgentURL:     c.AgentURL,
			HomeRegistry: c.HomeRegistry,
			Score:        c.FinalScore,
			ScoreBreakdown: &models.ScoreBreakdown{
				TextRelevance:      c.TextRelevance,
				SemanticSimilarity: c.SemanticSimilarity,
				TrustScore:         c.TrustScore,
				Freshness:          c.Freshness,
				Availability:       c.Availability,
			},
			Card: c.Card,
		}
	}
	return results
}

// computeFreshness returns a score 0-1 based on how recently the agent was updated.
// Uses exponential decay: score = exp(-lambda * days_since_update)
func computeFreshness(updatedAt string) float64 {
	if updatedAt == "" {
		return 0.5 // neutral if unknown
	}

	t, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return 0.5
	}

	daysSince := time.Since(t).Hours() / 24.0
	if daysSince < 0 {
		daysSince = 0
	}

	// Lambda chosen so that score=0.5 at ~30 days, score=0.1 at ~100 days
	lambda := 0.023
	return math.Exp(-lambda * daysSince)
}
