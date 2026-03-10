package mesh

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/models"
)

// FederatedSearch fans out a search query to relevant peers, collects results,
// and returns the merged response.
type FederatedSearch struct {
	transport *Transport
	peerMgr   *PeerManager
	cfg       config.SearchConfig
}

// NewFederatedSearch creates a federated search handler.
func NewFederatedSearch(transport *Transport, peerMgr *PeerManager, cfg config.SearchConfig) *FederatedSearch {
	return &FederatedSearch{
		transport: transport,
		peerMgr:   peerMgr,
		cfg:       cfg,
	}
}

// Search fans out the query to relevant peers and merges their results.
// Returns the combined results and the number of peers queried.
func (fs *FederatedSearch) Search(req *models.SearchRequest) ([]models.SearchResult, int, int) {
	if fs.transport.ConnectedPeerCount() == 0 {
		return nil, 0, 0
	}

	// Select peers to query using bloom filter routing
	queryTokens := fs.tokenizeQuery(req.Query, req.Category, req.Tags)
	maxPeers := fs.cfg.MaxFederatedPeers
	if maxPeers <= 0 {
		maxPeers = 5
	}
	relevantPeers := fs.peerMgr.GetRelevantPeers(queryTokens, maxPeers)
	if len(relevantPeers) == 0 {
		return nil, 0, 0
	}

	// Build the search message
	requestID := generateRequestID()
	msg := &SearchMessage{
		RequestID: requestID,
		Request:   req,
		OriginID:  fs.transport.kp.RegistryID(),
		TTL:       1, // single-hop for now
	}

	// Fan out to peers with timeout
	timeout := time.Duration(fs.cfg.FederatedTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}

	type peerResult struct {
		results []models.SearchResult
		err     error
	}

	resultCh := make(chan peerResult, len(relevantPeers))
	var wg sync.WaitGroup

	for _, peer := range relevantPeers {
		wg.Add(1)
		go func(p *models.PeerInfo) {
			defer wg.Done()

			// Create a per-peer timeout
			done := make(chan peerResult, 1)
			go func() {
				ack, err := fs.transport.SendSearchRequest(p.RegistryID, msg)
				if err != nil {
					done <- peerResult{err: err}
					return
				}
				done <- peerResult{results: ack.Results}
			}()

			select {
			case r := <-done:
				resultCh <- r
			case <-time.After(timeout):
				resultCh <- peerResult{err: fmt.Errorf("timeout")}
			}
		}(peer)
	}

	// Close result channel when all goroutines are done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	var allResults []models.SearchResult
	peersQueried := len(relevantPeers)
	federatedCount := 0

	for r := range resultCh {
		if r.err != nil {
			log.Printf("mesh: federated search error: %v", r.err)
			continue
		}
		allResults = append(allResults, r.results...)
		federatedCount += len(r.results)
	}

	return allResults, peersQueried, federatedCount
}

// tokenizeQuery extracts tokens from the search query for bloom filter matching.
func (fs *FederatedSearch) tokenizeQuery(query, category string, tags []string) []string {
	var tokens []string

	// Tokenize the query string
	for _, token := range tokenize(query) {
		tokens = append(tokens, strings.ToLower(token))
	}

	// Add category if specified
	if category != "" {
		tokens = append(tokens, strings.ToLower(category))
	}

	// Add tags
	for _, tag := range tags {
		tokens = append(tokens, strings.ToLower(tag))
	}

	return tokens
}

// generateRequestID creates a random request ID for tracking federated searches.
func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
