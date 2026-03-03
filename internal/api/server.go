package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/agentdns/agent-dns/internal/card"
	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/search"
	"github.com/agentdns/agent-dns/internal/store"
	"github.com/agentdns/agent-dns/internal/trust"
)

// Server is the HTTP API server for a registry node.
type Server struct {
	cfg          *config.Config
	store        store.Store
	searchEngine *search.Engine
	cardFetcher  *card.Fetcher
	peerManager  *mesh.PeerManager
	gossip       *mesh.GossipHandler
	eigentrust   *trust.EigenTrust
	nodeIdentity *identity.Keypair
	httpServer   *http.Server
	startTime    time.Time
}

// NewServer creates a new API server with all dependencies.
func NewServer(
	cfg *config.Config,
	st store.Store,
	searchEngine *search.Engine,
	cardFetcher *card.Fetcher,
	peerManager *mesh.PeerManager,
	gossipHandler *mesh.GossipHandler,
	eigentrust *trust.EigenTrust,
	nodeIdentity *identity.Keypair,
) *Server {
	return &Server{
		cfg:          cfg,
		store:        st,
		searchEngine: searchEngine,
		cardFetcher:  cardFetcher,
		peerManager:  peerManager,
		gossip:       gossipHandler,
		eigentrust:   eigentrust,
		nodeIdentity: nodeIdentity,
		startTime:    time.Now(),
	}
}

// Start begins serving the HTTP API.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Agent management
	mux.HandleFunc("POST /v1/agents", s.handleRegisterAgent)
	mux.HandleFunc("GET /v1/agents/{agentID}", s.handleGetAgent)
	mux.HandleFunc("PUT /v1/agents/{agentID}", s.handleUpdateAgent)
	mux.HandleFunc("DELETE /v1/agents/{agentID}", s.handleDeleteAgent)
	mux.HandleFunc("GET /v1/agents/{agentID}/card", s.handleGetAgentCard)

	// Search
	mux.HandleFunc("POST /v1/search", s.handleSearch)
	mux.HandleFunc("GET /v1/categories", s.handleGetCategories)
	mux.HandleFunc("GET /v1/tags", s.handleGetTags)

	// Network
	mux.HandleFunc("GET /v1/network/status", s.handleNetworkStatus)
	mux.HandleFunc("GET /v1/network/peers", s.handleGetPeers)
	mux.HandleFunc("POST /v1/network/peers", s.handleAddPeer)
	mux.HandleFunc("GET /v1/network/stats", s.handleNetworkStats)

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)

	// Apply middleware
	var handler http.Handler = mux
	handler = CORSMiddleware(s.cfg.API.CORSOrigins)(handler)

	// Different rate limiters for different endpoint groups
	searchRL := NewRateLimiter(s.cfg.API.RateLimitSearch)
	registerRL := NewRateLimiter(s.cfg.API.RateLimitRegister)
	_ = searchRL
	_ = registerRL

	handler = LoggingMiddleware(handler)

	s.httpServer = &http.Server{
		Addr:         s.cfg.API.Listen,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("API server starting on %s", s.cfg.API.Listen)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// --- Agent Handlers ---

func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req models.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.Name == "" || req.AgentURL == "" || req.Category == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "name, agent_url, category, and public_key are required")
		return
	}

	// Decode public key to generate agent_id
	pubKeyStr := req.PublicKey
	if strings.HasPrefix(pubKeyStr, "ed25519:") {
		pubKeyStr = pubKeyStr[8:]
	}

	// Verify the registration signature
	if req.Signature != "" {
		signable, _ := json.Marshal(map[string]interface{}{
			"name":       req.Name,
			"agent_url":  req.AgentURL,
			"category":   req.Category,
			"tags":       req.Tags,
			"summary":    req.Summary,
			"public_key": req.PublicKey,
		})
		valid, err := identity.Verify(pubKeyStr, signable, req.Signature)
		if err != nil || !valid {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	// Generate agent_id from public key
	import_b64 := pubKeyStr
	_ = import_b64
	// For now, generate a deterministic ID from the public key string
	agentID := "agdns:" + hashString(pubKeyStr)[:32]

	now := models.NowRFC3339()
	record := &models.RegistryRecord{
		AgentID:      agentID,
		Name:         req.Name,
		Owner:        "did:key:" + pubKeyStr[:20],
		AgentURL:     req.AgentURL,
		Category:     req.Category,
		Tags:         req.Tags,
		Summary:      req.Summary,
		PublicKey:    req.PublicKey,
		HomeRegistry: s.nodeIdentity.RegistryID(),
		RegisteredAt: now,
		UpdatedAt:    now,
		TTL:          86400,
		Signature:    req.Signature,
	}

	if record.Tags == nil {
		record.Tags = []string{}
	}

	if err := record.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Store the record
	if err := s.store.CreateAgent(record); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "agent already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to register agent: "+err.Error())
		return
	}

	// Index in search engine
	s.searchEngine.IndexAgent(record)

	// Gossip the announcement
	ann := s.gossip.CreateAnnouncement(record, "register", s.nodeIdentity.RegistryID(), s.nodeIdentity.Sign)
	_ = ann // TODO: broadcast to peers in Phase 2

	writeJSON(w, http.StatusCreated, map[string]string{
		"agent_id": agentID,
		"message":  "agent registered successfully",
	})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agent, err := s.store.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent: "+err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Get existing record
	existing, err := s.store.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	var req models.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply updates
	if req.AgentURL != nil {
		existing.AgentURL = *req.AgentURL
	}
	if req.Category != nil {
		existing.Category = *req.Category
	}
	if req.Tags != nil {
		existing.Tags = req.Tags
	}
	if req.Summary != nil {
		existing.Summary = *req.Summary
	}
	if req.TTL != nil {
		existing.TTL = *req.TTL
	}
	existing.UpdatedAt = models.NowRFC3339()
	existing.Signature = req.Signature

	if err := s.store.UpdateAgent(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	// Re-index
	s.searchEngine.IndexAgent(existing)

	// Gossip the update
	ann := s.gossip.CreateAnnouncement(existing, "update", s.nodeIdentity.RegistryID(), s.nodeIdentity.Sign)
	_ = ann

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Get the agent first to verify it exists
	agent, err := s.store.GetAgent(agentID)
	if err != nil || agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	// TODO: verify ownership signature from Authorization header

	if err := s.store.DeleteAgent(agentID, agent.Owner); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent: "+err.Error())
		return
	}

	// Remove from search index
	s.searchEngine.RemoveAgent(agentID)

	// Create tombstone
	tombstone := &models.Tombstone{
		AgentID:   agentID,
		Reason:    "owner-deregistered",
		CreatedAt: models.NowRFC3339(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339),
		Signature: s.nodeIdentity.Sign([]byte(agentID)),
	}
	s.store.CreateTombstone(tombstone)

	// Gossip the tombstone
	ann := s.gossip.CreateAnnouncement(agent, "deregister", s.nodeIdentity.RegistryID(), s.nodeIdentity.Sign)
	_ = ann

	writeJSON(w, http.StatusOK, map[string]string{"message": "agent deregistered"})
}

func (s *Server) handleGetAgentCard(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agent, err := s.store.GetAgent(agentID)
	if err != nil || agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	card, err := s.cardFetcher.FetchCard(agentID, agent.AgentURL, agent.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch agent card: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, card)
}

// --- Search Handlers ---

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req models.SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	resp, err := s.searchEngine.Search(&req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := s.store.GetAllCategories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get categories")
		return
	}
	if categories == nil {
		categories = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"categories": categories})
}

func (s *Server) handleGetTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.GetAllTags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tags")
		return
	}
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"tags": tags})
}

// --- Network Handlers ---

func (s *Server) handleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	agentCount, _ := s.store.CountAgents()
	gossipCount, _ := s.store.CountGossipEntries()

	status := models.NetworkStatus{
		RegistryID:    s.nodeIdentity.RegistryID(),
		Name:          s.cfg.Node.Name,
		Version:       "0.1.0",
		Uptime:        time.Since(s.startTime).String(),
		PeerCount:     s.peerManager.PeerCount(),
		LocalAgents:   agentCount,
		GossipEntries: gossipCount,
		CachedCards:   s.cardFetcher.CacheSize(),
		NodeType:      s.cfg.Node.Type,
	}

	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	peers := s.peerManager.GetPeers()
	if peers == nil {
		peers = []*models.PeerInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"peers": peers})
}

func (s *Server) handleAddPeer(w http.ResponseWriter, r *http.Request) {
	var peer models.PeerInfo
	if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if peer.Address == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}

	s.peerManager.AddPeer(&peer)
	writeJSON(w, http.StatusCreated, map[string]string{"message": "peer added"})
}

func (s *Server) handleNetworkStats(w http.ResponseWriter, r *http.Request) {
	agentCount, _ := s.store.CountAgents()
	gossipCount, _ := s.store.CountGossipEntries()

	stats := models.NetworkStats{
		EstimatedRegistries: s.peerManager.PeerCount() + 1,
		EstimatedAgents:     agentCount + gossipCount,
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
