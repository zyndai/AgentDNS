package ranking

import (
	"testing"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/models"
)

func defaultRanker() *Ranker {
	return NewRanker(config.RankingConfig{
		Method:                   "weighted",
		TextRelevanceWeight:      0.30,
		SemanticSimilarityWeight: 0.30,
		TrustWeight:              0.20,
		FreshnessWeight:          0.10,
		AvailabilityWeight:       0.10,
	})
}

// --- ToSearchResults: entity fields propagation ----------------------------

func TestToSearchResults_EntityFieldsPropagated(t *testing.T) {
	pricing := &models.EntityPricing{
		Model:        "per_request",
		BasePriceUSD: 0.05,
		Currency:     "USD",
	}
	candidates := []*CandidateResult{
		{
			AgentID:         "zns:svc:001",
			Name:            "translate-svc",
			EntityType:      "service",
			ServiceEndpoint: "https://api.translate.com/v1",
			OpenAPIURL:      "https://api.translate.com/openapi.json",
			EntityPricing:   pricing,
			DeveloperID:     "dev-1",
			FQAN:            "translate@alice.dns01.zynd.ai",
			DeveloperHandle: "alice",
		},
	}

	results := ToSearchResults(candidates)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.EntityType != "service" {
		t.Errorf("EntityType: got %q, want %q", r.EntityType, "service")
	}
	if r.ServiceEndpoint != "https://api.translate.com/v1" {
		t.Errorf("ServiceEndpoint: got %q", r.ServiceEndpoint)
	}
	if r.OpenAPIURL != "https://api.translate.com/openapi.json" {
		t.Errorf("OpenAPIURL: got %q", r.OpenAPIURL)
	}
	if r.EntityPricing == nil {
		t.Fatal("EntityPricing was nil")
	}
	if r.EntityPricing.Model != "per_request" {
		t.Errorf("EntityPricing.Model: got %q", r.EntityPricing.Model)
	}
	if r.EntityPricing.BasePriceUSD != 0.05 {
		t.Errorf("EntityPricing.BasePriceUSD: got %f", r.EntityPricing.BasePriceUSD)
	}
	if r.DeveloperID != "dev-1" {
		t.Errorf("DeveloperID: got %q", r.DeveloperID)
	}
	if r.FQAN != "translate@alice.dns01.zynd.ai" {
		t.Errorf("FQAN: got %q", r.FQAN)
	}
}

func TestToSearchResults_AgentWithPricing(t *testing.T) {
	candidates := []*CandidateResult{
		{
			AgentID:    "zns:abc",
			Name:       "code-reviewer",
			EntityType: "agent",
			EntityPricing: &models.EntityPricing{
				Model:        "per_request",
				BasePriceUSD: 0.10,
			},
		},
	}

	results := ToSearchResults(candidates)
	r := results[0]
	if r.EntityType != "agent" {
		t.Errorf("EntityType: got %q, want agent", r.EntityType)
	}
	if r.EntityPricing == nil {
		t.Fatal("agent should carry EntityPricing")
	}
	if r.EntityPricing.BasePriceUSD != 0.10 {
		t.Errorf("BasePriceUSD: got %f, want 0.10", r.EntityPricing.BasePriceUSD)
	}
}

func TestToSearchResults_NilPricing(t *testing.T) {
	candidates := []*CandidateResult{
		{
			AgentID:       "zns:abc",
			Name:          "free-agent",
			EntityType:    "agent",
			EntityPricing: nil,
		},
	}

	results := ToSearchResults(candidates)
	if results[0].EntityPricing != nil {
		t.Error("expected nil EntityPricing for free agent")
	}
}

func TestToSearchResults_EmptyEntityType(t *testing.T) {
	candidates := []*CandidateResult{
		{AgentID: "zns:abc", Name: "legacy-agent"},
	}

	results := ToSearchResults(candidates)
	if results[0].EntityType != "" {
		t.Errorf("expected empty EntityType, got %q", results[0].EntityType)
	}
}

// --- Deduplication preserves entity fields ---------------------------------

func TestDeduplicate_PreservesEntityFields(t *testing.T) {
	candidates := []*CandidateResult{
		{
			AgentID:         "zns:svc:001",
			Name:            "svc-first",
			EntityType:      "service",
			ServiceEndpoint: "https://first.com",
			EntityPricing:   &models.EntityPricing{Model: "free"},
		},
		{
			AgentID:         "zns:svc:001",
			Name:            "svc-duplicate",
			EntityType:      "service",
			ServiceEndpoint: "https://second.com",
		},
	}

	result := Deduplicate(candidates)
	if len(result) != 1 {
		t.Fatalf("expected 1 after dedup, got %d", len(result))
	}
	if result[0].ServiceEndpoint != "https://first.com" {
		t.Errorf("dedup should keep first occurrence, got endpoint %q", result[0].ServiceEndpoint)
	}
	if result[0].EntityPricing == nil {
		t.Error("dedup should preserve EntityPricing from first occurrence")
	}
}

// --- Ranking with entity fields -------------------------------------------

func TestRankWeighted_MixedEntityTypes(t *testing.T) {
	ranker := defaultRanker()

	candidates := []*CandidateResult{
		{
			AgentID:       "zns:agent1",
			EntityType:    "agent",
			TextRelevance: 0.5,
			TrustScore:    0.8,
			Availability:  1.0,
			UpdatedAt:     models.NowRFC3339(),
		},
		{
			AgentID:         "zns:svc:001",
			EntityType:      "service",
			ServiceEndpoint: "https://api.example.com",
			EntityPricing:   &models.EntityPricing{Model: "per_request"},
			TextRelevance:   0.9,
			TrustScore:      0.9,
			Availability:    1.0,
			UpdatedAt:       models.NowRFC3339(),
		},
	}

	ranked := ranker.Rank(candidates)
	if len(ranked) != 2 {
		t.Fatalf("expected 2, got %d", len(ranked))
	}

	// Higher scoring service should be first
	if ranked[0].AgentID != "zns:svc:001" {
		t.Errorf("expected service ranked first, got %q", ranked[0].AgentID)
	}
	if ranked[0].EntityType != "service" {
		t.Errorf("first result EntityType: got %q, want service", ranked[0].EntityType)
	}
	if ranked[0].EntityPricing == nil {
		t.Error("EntityPricing lost during ranking")
	}

	// Agent should be second
	if ranked[1].EntityType != "agent" {
		t.Errorf("second result EntityType: got %q, want agent", ranked[1].EntityType)
	}
}

func TestRankRRF_PreservesEntityFields(t *testing.T) {
	ranker := NewRanker(config.RankingConfig{Method: "rrf"})

	candidates := []*CandidateResult{
		{
			AgentID:         "zns:svc:001",
			EntityType:      "service",
			ServiceEndpoint: "https://api.example.com",
			OpenAPIURL:      "https://api.example.com/openapi.json",
			EntityPricing:   &models.EntityPricing{Model: "subscription", BasePriceUSD: 9.99},
			TextRelevance:   0.8,
			TrustScore:      0.7,
		},
		{
			AgentID:       "zns:agent1",
			EntityType:    "agent",
			TextRelevance: 0.5,
			TrustScore:    0.6,
		},
	}

	ranked := ranker.Rank(candidates)

	// Find the service in results
	var svc *CandidateResult
	for _, c := range ranked {
		if c.AgentID == "zns:svc:001" {
			svc = c
			break
		}
	}

	if svc == nil {
		t.Fatal("service not found in ranked results")
	}
	if svc.EntityType != "service" {
		t.Errorf("EntityType lost: got %q", svc.EntityType)
	}
	if svc.ServiceEndpoint != "https://api.example.com" {
		t.Errorf("ServiceEndpoint lost: got %q", svc.ServiceEndpoint)
	}
	if svc.EntityPricing == nil {
		t.Fatal("EntityPricing lost during RRF ranking")
	}
	if svc.EntityPricing.BasePriceUSD != 9.99 {
		t.Errorf("BasePriceUSD: got %f, want 9.99", svc.EntityPricing.BasePriceUSD)
	}
	if svc.FinalScore <= 0 {
		t.Errorf("FinalScore should be positive, got %f", svc.FinalScore)
	}
}

// --- Score breakdown in results -------------------------------------------

func TestToSearchResults_ScoreBreakdown(t *testing.T) {
	candidates := []*CandidateResult{
		{
			AgentID:            "zns:svc:001",
			EntityType:         "service",
			TextRelevance:      0.85,
			SemanticSimilarity: 0.72,
			TrustScore:         0.90,
			Freshness:          0.95,
			Availability:       1.0,
			FinalScore:         0.88,
		},
	}

	results := ToSearchResults(candidates)
	r := results[0]

	if r.ScoreBreakdown == nil {
		t.Fatal("ScoreBreakdown was nil")
	}
	if r.ScoreBreakdown.TextRelevance != 0.85 {
		t.Errorf("TextRelevance: got %f, want 0.85", r.ScoreBreakdown.TextRelevance)
	}
	if r.ScoreBreakdown.SemanticSimilarity != 0.72 {
		t.Errorf("SemanticSimilarity: got %f", r.ScoreBreakdown.SemanticSimilarity)
	}
	if r.ScoreBreakdown.TrustScore != 0.90 {
		t.Errorf("TrustScore: got %f", r.ScoreBreakdown.TrustScore)
	}
	if r.Score != 0.88 {
		t.Errorf("Score: got %f, want 0.88", r.Score)
	}
}

// --- Edge cases -----------------------------------------------------------

func TestToSearchResults_Empty(t *testing.T) {
	results := ToSearchResults(nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(results))
	}
}

func TestDeduplicate_Empty(t *testing.T) {
	result := Deduplicate(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 for nil, got %d", len(result))
	}
}

func TestDeduplicate_NoDuplicates(t *testing.T) {
	candidates := []*CandidateResult{
		{AgentID: "a", EntityType: "agent"},
		{AgentID: "b", EntityType: "service"},
	}
	result := Deduplicate(candidates)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}
