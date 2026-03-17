package search

import (
	"strings"
	"sync"
	"time"

	"github.com/agentdns/agent-dns/internal/card"
	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/ranking"
	"github.com/agentdns/agent-dns/internal/store"
)

// FederatedSearcher is implemented by the mesh layer to handle federated search.
type FederatedSearcher interface {
	Search(req *models.SearchRequest) ([]models.SearchResult, int, int)
}

// Engine orchestrates search across local store, gossip index, and federated peers.
type Engine struct {
	store           store.Store
	keyword         *KeywordIndex
	improvedKeyword *ImprovedKeywordIndex
	semantic        *SemanticIndex
	embedder        Embedder
	ranker          *ranking.Ranker
	fetcher         *card.Fetcher
	cfg             config.SearchConfig
	federated       FederatedSearcher
	useImprovedKW   bool
}

// NewEngine creates a new search engine.
// embedder is the embedding backend to use; build one with NewEmbedderFromConfig
// and pass it here so the embedder lifecycle is managed by the caller.
func NewEngine(
	st store.Store,
	fetcher *card.Fetcher,
	cfg config.SearchConfig,
	embedder Embedder,
) *Engine {
	if embedder == nil {
		embedder = NewHashEmbedder(cfg.EmbeddingDimensions)
	}

	var keyword *KeywordIndex
	var improvedKeyword *ImprovedKeywordIndex
	useImproved := cfg.UseImprovedKeyword

	if useImproved {
		improvedKeyword = NewImprovedKeywordIndex()
	} else {
		keyword = NewKeywordIndex()
	}

	return &Engine{
		store:           st,
		keyword:         keyword,
		improvedKeyword: improvedKeyword,
		semantic:        NewSemanticIndex(embedder.Dimensions()),
		embedder:        embedder,
		ranker:          ranking.NewRanker(cfg.Ranking),
		fetcher:         fetcher,
		cfg:             cfg,
		useImprovedKW:   useImproved,
	}
}

// SetFederatedSearcher registers the federated search handler from the mesh layer.
func (e *Engine) SetFederatedSearcher(fs FederatedSearcher) {
	e.federated = fs
}

// IndexAgent adds an agent to the keyword and semantic indexes.
func (e *Engine) IndexAgent(agent *models.RegistryRecord) {
	// Index in keyword search
	if e.useImprovedKW {
		e.improvedKeyword.IndexDocument(agent.AgentID, agent.Name, agent.Summary, agent.Category, agent.Tags)
	} else {
		e.keyword.IndexDocument(agent.AgentID, agent.Name, agent.Summary, agent.Category, agent.Tags)
	}

	// Index in semantic search
	text := agent.Name + " " + agent.Summary + " " + strings.Join(agent.Tags, " ")
	vec := e.embedder.Embed(text)
	e.semantic.Index(agent.AgentID, vec)
}

// IndexGossipEntry adds a gossip entry to the search indexes.
func (e *Engine) IndexGossipEntry(entry *models.GossipEntry) {
	if e.useImprovedKW {
		e.improvedKeyword.IndexDocument(entry.AgentID, entry.Name, entry.Summary, entry.Category, entry.Tags)
	} else {
		e.keyword.IndexDocument(entry.AgentID, entry.Name, entry.Summary, entry.Category, entry.Tags)
	}

	text := entry.Name + " " + entry.Summary + " " + strings.Join(entry.Tags, " ")
	vec := e.embedder.Embed(text)
	e.semantic.Index(entry.AgentID, vec)
}

// RemoveAgent removes an agent from all indexes.
func (e *Engine) RemoveAgent(agentID string) {
	if e.useImprovedKW {
		e.improvedKeyword.RemoveDocument(agentID)
	} else {
		e.keyword.RemoveDocument(agentID)
	}
	e.semantic.Remove(agentID)
}

// Search performs a full search combining keyword, semantic, and federated results.
func (e *Engine) Search(req *models.SearchRequest) (*models.SearchResponse, error) {
	startTime := time.Now()

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = e.cfg.DefaultMaxResults
	}

	// Internal limit for each search phase (fetch more than needed for ranking)
	internalLimit := maxResults * 3
	if internalLimit < 50 {
		internalLimit = 50
	}

	var allCandidates []*ranking.CandidateResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	var localCount, gossipCount int

	// Step 1 & 2: Search local agents and gossip index in parallel
	wg.Add(2)

	// Local keyword + semantic search
	go func() {
		defer wg.Done()
		candidates := e.searchLocal(req, internalLimit)
		mu.Lock()
		localCount = len(candidates)
		allCandidates = append(allCandidates, candidates...)
		mu.Unlock()
	}()

	// Gossip index search
	go func() {
		defer wg.Done()
		candidates := e.searchGossip(req, internalLimit)
		mu.Lock()
		gossipCount = len(candidates)
		allCandidates = append(allCandidates, candidates...)
		mu.Unlock()
	}()

	wg.Wait()

	// Step 3: Federated search
	federatedCount := 0
	peersQueried := 0
	if req.Federated && e.federated != nil {
		fedResults, peers, fedCount := e.federated.Search(req)
		peersQueried = peers
		federatedCount = fedCount
		// Convert federated results to candidates
		for _, r := range fedResults {
			mu.Lock()
			allCandidates = append(allCandidates, &ranking.CandidateResult{
				AgentID:            r.AgentID,
				Name:               r.Name,
				Summary:            r.Summary,
				Category:           r.Category,
				Tags:               r.Tags,
				AgentURL:           r.AgentURL,
				HomeRegistry:       r.HomeRegistry,
				TextRelevance:      0.0,
				SemanticSimilarity: 0.0,
				TrustScore:         0.3,
				Availability:       0.7,
				FinalScore:         r.Score, // preserve the remote score as a baseline
			})
			mu.Unlock()
		}
	}

	// Step 4: Deduplicate and rank
	allCandidates = ranking.Deduplicate(allCandidates)
	allCandidates = e.ranker.Rank(allCandidates)

	totalFound := localCount + gossipCount + federatedCount

	// Apply offset for pagination
	if req.Offset > 0 {
		if req.Offset >= len(allCandidates) {
			allCandidates = nil
		} else {
			allCandidates = allCandidates[req.Offset:]
		}
	}

	// Check if there are more results beyond this page
	hasMore := len(allCandidates) > maxResults

	// Trim to max results
	if len(allCandidates) > maxResults {
		allCandidates = allCandidates[:maxResults]
	}

	// Step 5: Enrich top results with Agent Cards if requested
	if req.Enrich {
		enrichLimit := 10
		if enrichLimit > len(allCandidates) {
			enrichLimit = len(allCandidates)
		}
		e.enrichCandidates(allCandidates[:enrichLimit])
	}

	// Build response
	latencyMs := int(time.Since(startTime).Milliseconds())
	return &models.SearchResponse{
		Results:    ranking.ToSearchResults(allCandidates),
		TotalFound: totalFound,
		Offset:     req.Offset,
		HasMore:    hasMore,
		SearchStats: &models.SearchStats{
			LocalResults:     localCount,
			GossipResults:    gossipCount,
			FederatedResults: federatedCount,
			PeersQueried:     peersQueried,
			LatencyMs:        latencyMs,
		},
	}, nil
}

// searchLocal searches local agents using both keyword and semantic search.
func (e *Engine) searchLocal(req *models.SearchRequest, limit int) []*ranking.CandidateResult {
	// Keyword search
	var keywordResults []KeywordResult
	if e.useImprovedKW {
		keywordResults = e.improvedKeyword.Search(req.Query, limit)
	} else {
		keywordResults = e.keyword.Search(req.Query, limit)
	}

	// Semantic search
	queryVec := e.embedder.Embed(req.Query)
	semanticResults := e.semantic.Search(queryVec, limit)

	// Build semantic score map
	semanticScores := make(map[string]float64)
	for _, r := range semanticResults {
		semanticScores[r.DocID] = r.Score
	}

	// Merge into candidate results
	candidateMap := make(map[string]*ranking.CandidateResult)

	// Normalize keyword scores
	maxKeyword := 0.0
	for _, r := range keywordResults {
		if r.Score > maxKeyword {
			maxKeyword = r.Score
		}
	}

	for _, kr := range keywordResults {
		normalizedScore := 0.0
		if maxKeyword > 0 {
			normalizedScore = kr.Score / maxKeyword
		}

		agent, err := e.store.GetAgent(kr.DocID)
		if err != nil || agent == nil {
			continue
		}

		// Apply category/tag filters
		if req.Category != "" && agent.Category != req.Category {
			continue
		}
		if len(req.Tags) > 0 && !hasAnyTag(agent.Tags, req.Tags) {
			continue
		}

		candidateMap[kr.DocID] = &ranking.CandidateResult{
			AgentID:       agent.AgentID,
			Name:          agent.Name,
			Summary:       agent.Summary,
			Category:      agent.Category,
			Tags:          agent.Tags,
			AgentURL:      agent.AgentURL,
			HomeRegistry:  agent.HomeRegistry,
			UpdatedAt:     agent.UpdatedAt,
			TextRelevance: normalizedScore,
			TrustScore:    0.5, // default until trust system is active
			Availability:  1.0, // assume available for local agents
		}
	}

	// Merge semantic scores
	for _, sr := range semanticResults {
		if c, exists := candidateMap[sr.DocID]; exists {
			c.SemanticSimilarity = sr.Score
		} else {
			agent, err := e.store.GetAgent(sr.DocID)
			if err != nil || agent == nil {
				continue
			}
			if req.Category != "" && agent.Category != req.Category {
				continue
			}
			if len(req.Tags) > 0 && !hasAnyTag(agent.Tags, req.Tags) {
				continue
			}

			candidateMap[sr.DocID] = &ranking.CandidateResult{
				AgentID:            agent.AgentID,
				Name:               agent.Name,
				Summary:            agent.Summary,
				Category:           agent.Category,
				Tags:               agent.Tags,
				AgentURL:           agent.AgentURL,
				HomeRegistry:       agent.HomeRegistry,
				UpdatedAt:          agent.UpdatedAt,
				SemanticSimilarity: sr.Score,
				TrustScore:         0.5,
				Availability:       1.0,
			}
		}
	}

	var candidates []*ranking.CandidateResult
	for _, c := range candidateMap {
		candidates = append(candidates, c)
	}
	return candidates
}

// searchGossip searches the gossip index (remote agents).
func (e *Engine) searchGossip(req *models.SearchRequest, limit int) []*ranking.CandidateResult {
	// Search gossip entries from store
	entries, err := e.store.SearchGossipByKeyword(req.Query, req.Category, req.Tags, limit)
	if err != nil {
		return nil
	}

	var candidates []*ranking.CandidateResult
	for _, entry := range entries {
		// Get keyword score from the index
		var keywordResults []KeywordResult
		if e.useImprovedKW {
			keywordResults = e.improvedKeyword.Search(req.Query, 1)
		} else {
			keywordResults = e.keyword.Search(req.Query, 1)
		}
		textScore := 0.0
		for _, kr := range keywordResults {
			if kr.DocID == entry.AgentID {
				textScore = kr.Score
				break
			}
		}

		// Get semantic score
		queryVec := e.embedder.Embed(req.Query)
		text := entry.Name + " " + entry.Summary + " " + strings.Join(entry.Tags, " ")
		docVec := e.embedder.Embed(text)
		semanticScore := cosineSimilarity(queryVec, docVec)

		candidates = append(candidates, &ranking.CandidateResult{
			AgentID:            entry.AgentID,
			Name:               entry.Name,
			Summary:            entry.Summary,
			Category:           entry.Category,
			Tags:               entry.Tags,
			AgentURL:           entry.AgentURL,
			HomeRegistry:       entry.HomeRegistry,
			UpdatedAt:          entry.ReceivedAt,
			TextRelevance:      textScore,
			SemanticSimilarity: semanticScore,
			TrustScore:         0.3, // lower default for remote agents
			Availability:       0.8, // slightly less confident about availability
		})
	}

	return candidates
}

// enrichCandidates fetches Agent Cards for the top candidates.
func (e *Engine) enrichCandidates(candidates []*ranking.CandidateResult) {
	var wg sync.WaitGroup
	for _, c := range candidates {
		wg.Add(1)
		go func(candidate *ranking.CandidateResult) {
			defer wg.Done()

			// Look up public key from local store first
			agent, err := e.store.GetAgent(candidate.AgentID)
			pubKey := ""
			if err == nil && agent != nil {
				pubKey = agent.PublicKey
			}

			card, err := e.fetcher.FetchCard(candidate.AgentID, candidate.AgentURL, pubKey)
			if err == nil && card != nil {
				candidate.Card = card
				// Update availability based on card status
				if card.Status == "online" {
					candidate.Availability = 1.0
				} else if card.Status == "degraded" {
					candidate.Availability = 0.5
				} else {
					candidate.Availability = 0.0
				}
			}
		}(c)
	}
	wg.Wait()
}

// RebuildIndexes rebuilds keyword and semantic indexes from the store.
func (e *Engine) RebuildIndexes() error {
	// Index local agents
	agents, err := e.store.ListAgents("", 100000, 0)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		e.IndexAgent(agent)
	}

	return nil
}

func hasAnyTag(agentTags, filterTags []string) bool {
	tagSet := make(map[string]bool)
	for _, t := range agentTags {
		tagSet[strings.ToLower(t)] = true
	}
	for _, t := range filterTags {
		if tagSet[strings.ToLower(t)] {
			return true
		}
	}
	return false
}
