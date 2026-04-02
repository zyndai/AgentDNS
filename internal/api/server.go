package api

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/agentdns/agent-dns/internal/card"
	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/search"
	"github.com/agentdns/agent-dns/internal/store"
	"github.com/agentdns/agent-dns/internal/zns"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Server is the HTTP API server for a registry node.
// DHTLookup is the interface the API server uses for DHT-based agent lookups.
type DHTLookup interface {
	FindValue(key interface{}) interface{}
}

type Server struct {
	cfg          *config.Config
	store        store.Store
	searchEngine *search.Engine
	cardFetcher  *card.Fetcher
	peerManager  *mesh.PeerManager
	gossip       *mesh.GossipHandler
	nodeIdentity *identity.Keypair
	dht          *dhtNode // optional DHT for fallback lookups
	httpServer   *http.Server
	startTime    time.Time
	eventBus     *events.Bus
}

// dhtNode wraps the DHT for API use.
type dhtNode struct {
	FindValueFn func(agentID string) *dhtRecord
}

// DHTRecord represents an agent record from the Kademlia DHT.
type DHTRecord = dhtRecord

type dhtRecord struct {
	AgentID      string   `json:"agent_id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	AgentURL     string   `json:"agent_url"`
	PublicKey    string   `json:"public_key"`
	HomeRegistry string   `json:"home_registry"`
	DeveloperID  string   `json:"developer_id,omitempty"`
	Status       string   `json:"status,omitempty"`
}

// SetDHT sets the DHT lookup function for fallback agent resolution.
func (s *Server) SetDHT(findValue func(agentID string) *dhtRecord) {
	s.dht = &dhtNode{FindValueFn: findValue}
}

// NewServer creates a new API server with all dependencies.
func NewServer(
	cfg *config.Config,
	st store.Store,
	searchEngine *search.Engine,
	cardFetcher *card.Fetcher,
	peerManager *mesh.PeerManager,
	gossipHandler *mesh.GossipHandler,
	nodeIdentity *identity.Keypair,
) *Server {
	return &Server{
		cfg:          cfg,
		store:        st,
		searchEngine: searchEngine,
		cardFetcher:  cardFetcher,
		peerManager:  peerManager,
		gossip:       gossipHandler,
		nodeIdentity: nodeIdentity,
		startTime:    time.Now(),
		eventBus:     events.NewBus(),
	}
}

// EventBus returns the server's event bus so callers can wire it into other components.
func (s *Server) EventBus() *events.Bus {
	return s.eventBus
}

// Start begins serving the HTTP API.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Rate limiters for endpoint groups
	searchRL := NewRateLimiter(s.cfg.API.RateLimitSearch)
	registerRL := NewRateLimiter(s.cfg.API.RateLimitRegister)

	// Developer identity management
	mux.HandleFunc("POST /v1/developers", rateLimited(registerRL, s.handleRegisterDeveloper))
	mux.HandleFunc("GET /v1/developers/{developerID}", s.handleGetDeveloper)
	mux.HandleFunc("PUT /v1/developers/{developerID}", s.handleUpdateDeveloper)
	mux.HandleFunc("DELETE /v1/developers/{developerID}", s.handleDeleteDeveloper)
	mux.HandleFunc("GET /v1/developers/{developerID}/agents", s.handleListDeveloperAgents)

	// Agent management
	mux.HandleFunc("POST /v1/agents", rateLimited(registerRL, s.handleRegisterAgent))
	mux.HandleFunc("GET /v1/agents/{agentID}", s.handleGetAgent)
	mux.HandleFunc("PUT /v1/agents/{agentID}", s.handleUpdateAgent)
	mux.HandleFunc("DELETE /v1/agents/{agentID}", s.handleDeleteAgent)
	mux.HandleFunc("GET /v1/agents/{agentID}/card", s.handleGetAgentCard)
	mux.HandleFunc("GET /v1/agents/{agentID}/ws", s.handleAgentHeartbeat)

	// Search
	mux.HandleFunc("POST /v1/search", rateLimited(searchRL, s.handleSearch))
	mux.HandleFunc("GET /v1/categories", s.handleGetCategories)
	mux.HandleFunc("GET /v1/tags", s.handleGetTags)

	// ZNS: Developer handles
	mux.HandleFunc("POST /v1/handles", rateLimited(registerRL, s.handleClaimHandle))
	mux.HandleFunc("GET /v1/handles/{handle}", s.handleGetHandle)
	mux.HandleFunc("GET /v1/handles/{handle}/available", s.handleCheckHandleAvailable)
	mux.HandleFunc("DELETE /v1/handles/{handle}", s.handleReleaseHandle)
	mux.HandleFunc("POST /v1/handles/{handle}/verify", s.handleVerifyHandle)
	mux.HandleFunc("GET /v1/handles/{handle}/agents", s.handleListHandleAgents)

	// ZNS: Name bindings
	mux.HandleFunc("POST /v1/names", rateLimited(registerRL, s.handleRegisterName))
	mux.HandleFunc("GET /v1/names/{developer}/{agent}", s.handleGetName)
	mux.HandleFunc("GET /v1/names/{developer}/{agent}/available", s.handleCheckNameAvailable)
	mux.HandleFunc("PUT /v1/names/{developer}/{agent}", s.handleUpdateName)
	mux.HandleFunc("DELETE /v1/names/{developer}/{agent}", s.handleReleaseName)
	mux.HandleFunc("GET /v1/names/{developer}/{agent}/versions", s.handleListVersions)

	// ZNS: Resolution
	mux.HandleFunc("GET /v1/resolve/{developer}/{agent}", s.handleResolveName)

	// ZNS: Registry identity proof
	mux.HandleFunc("GET /.well-known/zynd-registry.json", s.handleRegistryIdentityProof)

	// Network
	mux.HandleFunc("GET /v1/network/status", s.handleNetworkStatus)
	mux.HandleFunc("GET /v1/network/peers", s.handleGetPeers)
	mux.HandleFunc("POST /v1/network/peers", s.handleAddPeer)
	mux.HandleFunc("GET /v1/network/stats", s.handleNetworkStats)

	// Registry info / discovery
	mux.HandleFunc("GET /v1/info", s.handleRegistryInfo)

	// Admin endpoints (webhook-authenticated)
	if s.cfg.Onboarding.Mode == "restricted" && s.cfg.Onboarding.WebhookSecret != "" {
		webhookAuth := WebhookAuthMiddleware(s.cfg.Onboarding.WebhookSecret)
		mux.HandleFunc("POST /v1/admin/developers/approve", webhookAuth(s.handleApproveDeveloper))
	}

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)

	// WebSocket activity stream
	mux.HandleFunc("GET /v1/ws/activity", s.handleActivityStream)

	// Swagger UI
	mux.Handle("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// Apply middleware
	var handler http.Handler = mux
	handler = CORSMiddleware(s.cfg.API.CORSOrigins)(handler)
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

// --- Developer Handlers ---

// handleRegisterDeveloper registers a new developer identity.
//
//	@Summary		Register a new developer
//	@Description	Register a new developer identity with name, public_key, and signature. Self-registration is disabled in restricted mode.
//	@Tags			Developers
//	@Accept			json
//	@Produce		json
//	@Param			body	body		models.DeveloperRegistrationRequest	true	"Developer registration payload"
//	@Success		201		{object}	map[string]string					"developer_id and success message"
//	@Failure		400		{object}	map[string]string					"Validation error"
//	@Failure		401		{object}	map[string]string					"Invalid signature"
//	@Failure		403		{object}	map[string]string					"Self-registration disabled"
//	@Failure		409		{object}	map[string]string					"Developer already registered"
//	@Failure		500		{object}	map[string]string					"Internal server error"
//	@Router			/v1/developers [post]
func (s *Server) handleRegisterDeveloper(w http.ResponseWriter, r *http.Request) {
	// In restricted mode, self-registration is disabled
	if s.cfg.Onboarding.Mode == "restricted" {
		resp := map[string]string{
			"error": "self-registration is disabled; use 'agentdns auth login --registry <url>'",
		}
		if s.cfg.Onboarding.AuthURL != "" {
			resp["auth_url"] = s.cfg.Onboarding.AuthURL
		}
		writeJSON(w, http.StatusForbidden, resp)
		return
	}

	var req models.DeveloperRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "name and public_key are required")
		return
	}

	// Strip ed25519: prefix for verification
	pubKeyStr := req.PublicKey
	if strings.HasPrefix(pubKeyStr, "ed25519:") {
		pubKeyStr = pubKeyStr[8:]
	}

	// Verify the registration signature
	if req.Signature == "" {
		writeError(w, http.StatusBadRequest, "signature is required")
		return
	}
	signable, _ := json.Marshal(map[string]interface{}{
		"name":        req.Name,
		"public_key":  req.PublicKey,
		"profile_url": req.ProfileURL,
		"github":      req.GitHub,
	})
	valid, err := identity.Verify(pubKeyStr, signable, req.Signature)
	if err != nil || !valid {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Generate developer_id from public key bytes
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid public key encoding")
		return
	}
	developerID := models.GenerateDeveloperID(ed25519.PublicKey(pubKeyBytes))

	// If handle provided, validate it before creating the developer
	if req.Handle != "" {
		if err := zns.ValidateHandle(req.Handle); err != nil {
			writeError(w, http.StatusBadRequest, "invalid handle: "+err.Error())
			return
		}
		// Check handle availability on this registry
		registryHost := s.cfg.RegistryHost()
		existing, _ := s.store.GetDeveloperByHandle(req.Handle, registryHost)
		if existing != nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("handle %q is already taken on this registry", req.Handle))
			return
		}
	}

	now := models.NowRFC3339()
	dev := &models.DeveloperRecord{
		DeveloperID:   developerID,
		Name:          req.Name,
		PublicKey:     req.PublicKey,
		ProfileURL:    req.ProfileURL,
		GitHub:        req.GitHub,
		HomeRegistry:  s.nodeIdentity.RegistryID(),
		SchemaVersion: models.CurrentSchemaVersion,
		RegisteredAt:  now,
		UpdatedAt:     now,
		Signature:     req.Signature,
		DevHandle:     req.Handle, // set handle atomically if provided
	}

	if err := dev.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.CreateDeveloper(dev); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeError(w, http.StatusConflict, "developer already registered (or handle already taken)")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to register developer: "+err.Error())
		return
	}

	// Gossip the developer identity to mesh peers
	ann := s.gossip.CreateDeveloperAnnouncement(dev, "register", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	// If handle was claimed, also gossip the handle
	if req.Handle != "" {
		handleAnn := s.gossip.CreateHandleAnnouncement(dev, req.Handle, false, "", "claim",
			s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
		s.gossip.BroadcastAnnouncement(handleAnn)

		s.eventBus.Publish(events.EventHandleClaimed, events.ZNSEventData{
			Handle:      req.Handle,
			DeveloperID: developerID,
			Action:      "claim",
		})
	}

	resp := map[string]string{
		"developer_id": developerID,
		"message":      "developer registered successfully",
	}
	if req.Handle != "" {
		resp["handle"] = req.Handle
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleGetDeveloper retrieves a developer by ID.
//
//	@Summary		Get developer by ID
//	@Description	Retrieve a developer record by their developer_id. Falls back to gossip entries for remote developers.
//	@Tags			Developers
//	@Produce		json
//	@Param			developerID	path		string					true	"Developer ID"
//	@Success		200			{object}	models.DeveloperRecord	"Developer record"
//	@Failure		404			{object}	map[string]string		"Developer not found"
//	@Failure		500			{object}	map[string]string		"Internal server error"
//	@Router			/v1/developers/{developerID} [get]
func (s *Server) handleGetDeveloper(w http.ResponseWriter, r *http.Request) {
	developerID := r.PathValue("developerID")
	if developerID == "" {
		writeError(w, http.StatusBadRequest, "developer_id is required")
		return
	}

	dev, err := s.store.GetDeveloper(developerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get developer: "+err.Error())
		return
	}
	if dev == nil {
		// Check gossip entries for remote developers
		gossipDev, err := s.store.GetGossipDeveloper(developerID)
		if err != nil || gossipDev == nil {
			writeError(w, http.StatusNotFound, "developer not found")
			return
		}
		// Convert gossip entry to developer record for consistent response
		dev = &models.DeveloperRecord{
			DeveloperID:  gossipDev.DeveloperID,
			Name:         gossipDev.Name,
			PublicKey:    gossipDev.PublicKey,
			ProfileURL:   gossipDev.ProfileURL,
			GitHub:       gossipDev.GitHub,
			HomeRegistry: gossipDev.HomeRegistry,
		}
	}

	writeJSON(w, http.StatusOK, dev)
}

// handleUpdateDeveloper updates a developer's profile.
//
//	@Summary		Update developer profile
//	@Description	Update a developer's profile fields. Requires Authorization header with Bearer ed25519 signature.
//	@Tags			Developers
//	@Accept			json
//	@Produce		json
//	@Param			developerID		path		string							true	"Developer ID"
//	@Param			Authorization	header		string							true	"Bearer ed25519:<base64sig>"
//	@Param			body			body		models.DeveloperUpdateRequest	true	"Fields to update"
//	@Success		200				{object}	models.DeveloperRecord			"Updated developer record"
//	@Failure		400				{object}	map[string]string				"Invalid request body"
//	@Failure		401				{object}	map[string]string				"Ownership verification failed"
//	@Failure		404				{object}	map[string]string				"Developer not found"
//	@Failure		500				{object}	map[string]string				"Internal server error"
//	@Router			/v1/developers/{developerID} [put]
func (s *Server) handleUpdateDeveloper(w http.ResponseWriter, r *http.Request) {
	developerID := r.PathValue("developerID")
	if developerID == "" {
		writeError(w, http.StatusBadRequest, "developer_id is required")
		return
	}

	existing, err := s.store.GetDeveloper(developerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get developer")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "developer not found")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Verify ownership -- must be signed by the developer's key
	if err := verifyOwnership(existing.PublicKey, bodyBytes, r.Header.Get("Authorization")); err != nil {
		writeError(w, http.StatusUnauthorized, "ownership verification failed: "+err.Error())
		return
	}

	var req models.DeveloperUpdateRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.ProfileURL != nil {
		existing.ProfileURL = *req.ProfileURL
	}
	if req.GitHub != nil {
		existing.GitHub = *req.GitHub
	}
	existing.UpdatedAt = models.NowRFC3339()
	existing.Signature = req.Signature

	if err := s.store.UpdateDeveloper(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update developer: "+err.Error())
		return
	}

	// Gossip the update
	ann := s.gossip.CreateDeveloperAnnouncement(existing, "update", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusOK, existing)
}

// handleDeleteDeveloper deregisters a developer identity.
//
//	@Summary		Delete developer
//	@Description	Deregister a developer identity. Requires Authorization header with Bearer ed25519 signature.
//	@Tags			Developers
//	@Produce		json
//	@Param			developerID		path		string				true	"Developer ID"
//	@Param			Authorization	header		string				true	"Bearer ed25519:<base64sig>"
//	@Success		200				{object}	map[string]string	"Deregistration confirmation"
//	@Failure		401				{object}	map[string]string	"Ownership verification failed"
//	@Failure		404				{object}	map[string]string	"Developer not found"
//	@Failure		500				{object}	map[string]string	"Internal server error"
//	@Router			/v1/developers/{developerID} [delete]
func (s *Server) handleDeleteDeveloper(w http.ResponseWriter, r *http.Request) {
	developerID := r.PathValue("developerID")
	if developerID == "" {
		writeError(w, http.StatusBadRequest, "developer_id is required")
		return
	}

	existing, err := s.store.GetDeveloper(developerID)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "developer not found")
		return
	}

	// Verify ownership
	if err := verifyOwnership(existing.PublicKey, []byte(developerID), r.Header.Get("Authorization")); err != nil {
		writeError(w, http.StatusUnauthorized, "ownership verification failed: "+err.Error())
		return
	}

	if err := s.store.DeleteDeveloper(developerID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete developer: "+err.Error())
		return
	}

	// Gossip the deregistration
	ann := s.gossip.CreateDeveloperAnnouncement(existing, "deregister", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusOK, map[string]string{"message": "developer deregistered"})
}

// handleListDeveloperAgents lists all agents registered by a developer.
//
//	@Summary		List agents by developer
//	@Description	Retrieve all agents registered by a given developer_id, including developer_id, agents array, and count.
//	@Tags			Developers
//	@Produce		json
//	@Param			developerID	path		string				true	"Developer ID"
//	@Success		200			{object}	map[string]interface{}	"developer_id, agents, and count"
//	@Failure		400			{object}	map[string]string		"Missing developer_id"
//	@Failure		500			{object}	map[string]string		"Internal server error"
//	@Router			/v1/developers/{developerID}/agents [get]
func (s *Server) handleListDeveloperAgents(w http.ResponseWriter, r *http.Request) {
	developerID := r.PathValue("developerID")
	if developerID == "" {
		writeError(w, http.StatusBadRequest, "developer_id is required")
		return
	}

	agents, err := s.store.ListAgentsByDeveloper(developerID, 100, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents: "+err.Error())
		return
	}
	if agents == nil {
		agents = []*models.RegistryRecord{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"developer_id": developerID,
		"agents":       agents,
		"count":        len(agents),
	})
}

// --- Agent Handlers ---

// handleRegisterAgent registers a new agent on the registry.
//
//	@Summary		Register a new agent
//	@Description	Register a new AI agent on the registry network. Requires name, agent_url, category, and public_key. Optionally includes developer_id and developer_proof for developer chain of trust.
//	@Tags			Agents
//	@Accept			json
//	@Produce		json
//	@Param			body	body		models.RegistrationRequest	true	"Agent registration payload"
//	@Success		201		{object}	map[string]string			"agent_id and success message"
//	@Failure		400		{object}	map[string]string			"Validation error"
//	@Failure		401		{object}	map[string]string			"Invalid signature"
//	@Failure		409		{object}	map[string]string			"Agent already registered"
//	@Failure		500		{object}	map[string]string			"Internal server error"
//	@Router			/v1/agents [post]
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

	// Strip ed25519: prefix for cryptographic operations
	pubKeyStr := req.PublicKey
	if strings.HasPrefix(pubKeyStr, "ed25519:") {
		pubKeyStr = pubKeyStr[8:]
	}

	// Verify the registration signature (agent key signs the payload)
	if req.Signature == "" {
		writeError(w, http.StatusBadRequest, "signature is required")
		return
	}
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
		writeError(w, http.StatusUnauthorized, "invalid agent signature")
		return
	}

	// Generate agent_id from public key bytes (canonical derivation)
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid public key encoding")
		return
	}
	agentID := models.GenerateAgentID(ed25519.PublicKey(pubKeyBytes))

	// Determine owner and verify developer proof if provided
	var developerID string
	var agentIndex *int
	var developerProof *models.DeveloperProof
	owner := developerID // will be set below

	if req.DeveloperID != "" && req.DeveloperProof != nil {
		// Verify developer exists (locally or via gossip)
		dev, devErr := s.store.GetDeveloper(req.DeveloperID)
		if devErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to look up developer")
			return
		}
		if dev == nil {
			// Check gossip entries
			gossipDev, gErr := s.store.GetGossipDeveloper(req.DeveloperID)
			if gErr != nil || gossipDev == nil {
				writeError(w, http.StatusBadRequest, "developer_id not found; register developer first")
				return
			}
			// Verify the developer public key matches gossip
			if gossipDev.PublicKey != req.DeveloperProof.DeveloperPublicKey {
				writeError(w, http.StatusBadRequest, "developer_proof.developer_public_key does not match registered developer")
				return
			}
		} else {
			// Verify the developer public key matches
			if dev.PublicKey != req.DeveloperProof.DeveloperPublicKey {
				writeError(w, http.StatusBadRequest, "developer_proof.developer_public_key does not match registered developer")
				return
			}
		}

		// Verify the derivation proof
		proofValid, proofErr := identity.VerifyDerivationProof(
			&identity.DeveloperProof{
				DeveloperPublicKey: req.DeveloperProof.DeveloperPublicKey,
				AgentIndex:         req.DeveloperProof.AgentIndex,
				DeveloperSignature: req.DeveloperProof.DeveloperSignature,
			},
			req.PublicKey,
		)
		if proofErr != nil || !proofValid {
			writeError(w, http.StatusUnauthorized, "invalid developer derivation proof")
			return
		}

		developerID = req.DeveloperID
		idx := req.DeveloperProof.AgentIndex
		agentIndex = &idx
		developerProof = req.DeveloperProof
		owner = developerID
	} else {
		// No developer -- agent is self-sovereign
		owner = "did:key:" + pubKeyStr[:20]
	}

	now := models.NowRFC3339()
	record := &models.RegistryRecord{
		AgentID:        agentID,
		Name:           req.Name,
		Owner:          owner,
		AgentURL:       req.AgentURL,
		Category:       req.Category,
		Tags:           req.Tags,
		Summary:        req.Summary,
		PublicKey:      req.PublicKey,
		HomeRegistry:   s.nodeIdentity.RegistryID(),
		SchemaVersion:  models.CurrentSchemaVersion,
		RegisteredAt:   now,
		UpdatedAt:      now,
		TTL:            86400,
		Signature:      req.Signature,
		DeveloperID:    developerID,
		AgentIndex:     agentIndex,
		DeveloperProof: developerProof,
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
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeError(w, http.StatusConflict, "agent already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to register agent: "+err.Error())
		return
	}

	// Index in search engine
	s.searchEngine.IndexAgent(record)

	// Gossip the announcement to mesh peers
	ann := s.gossip.CreateAnnouncement(record, "register", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	// Publish registration event
	s.eventBus.Publish(events.EventAgentRegistered, events.AgentEventData{
		AgentID:  agentID,
		Name:     record.Name,
		Category: record.Category,
		Tags:     record.Tags,
		Summary:  record.Summary,
	})

	// ZNS: If agent_name is provided and developer has a handle, create FQAN atomically
	var fqanResult string
	if req.AgentName != "" && developerID != "" {
		registryHost := s.cfg.RegistryHost()
		if registryHost != "" {
			fqanResult, _ = s.createZNSNameBinding(record, req.AgentName, req.Version, registryHost)
		}
	}

	resp := map[string]string{
		"agent_id": agentID,
		"message":  "agent registered successfully",
	}
	if fqanResult != "" {
		resp["fqan"] = fqanResult
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleGetAgent retrieves a single agent by ID.
//
//	@Summary		Get agent by ID
//	@Description	Retrieve a registry record for a specific agent by its agent_id.
//	@Tags			Agents
//	@Produce		json
//	@Param			agentID	path		string					true	"Agent ID (e.g. agdns:7f3a9c2e...)"
//	@Success		200		{object}	models.RegistryRecord	"Agent registry record"
//	@Failure		400		{object}	map[string]string		"Missing agent_id"
//	@Failure		404		{object}	map[string]string		"Agent not found"
//	@Failure		500		{object}	map[string]string		"Internal server error"
//	@Router			/v1/agents/{agentID} [get]
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// 1. Check local agents table
	agent, err := s.store.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent: "+err.Error())
		return
	}
	if agent != nil {
		agent.AgentURL = "" // agent_url is private — only accessible via /card
		writeJSON(w, http.StatusOK, agent)
		return
	}

	// 2. Check gossip entries (remote agents replicated via gossip)
	gossipEntry, err := s.store.GetGossipEntry(agentID)
	if err == nil && gossipEntry != nil {
		gossipEntry.AgentURL = ""
		writeJSON(w, http.StatusOK, gossipEntry)
		return
	}

	// 3. DHT lookup (O(log n) across the network)
	if s.dht != nil && s.dht.FindValueFn != nil {
		rec := s.dht.FindValueFn(agentID)
		if rec != nil {
			rec.AgentURL = ""
			writeJSON(w, http.StatusOK, rec)
			return
		}
	}

	writeError(w, http.StatusNotFound, "agent not found")
}

// handleUpdateAgent updates an existing agent's registry record.
//
//	@Summary		Update an agent
//	@Description	Update fields on an existing agent registry record. Only provided fields are changed.
//	@Tags			Agents
//	@Accept			json
//	@Produce		json
//	@Param			agentID	path		string					true	"Agent ID"
//	@Param			body	body		models.UpdateRequest	true	"Fields to update"
//	@Success		200		{object}	models.RegistryRecord	"Updated agent record"
//	@Failure		400		{object}	map[string]string		"Invalid request body"
//	@Failure		404		{object}	map[string]string		"Agent not found"
//	@Failure		500		{object}	map[string]string		"Internal server error"
//	@Router			/v1/agents/{agentID} [put]
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

	// Read body for signature verification and decoding
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Dual-key authorization: accept either agent key OR developer key
	authErr := verifyDualKeyOwnership(s.store, existing, bodyBytes, r.Header.Get("Authorization"))
	if authErr != nil {
		writeError(w, http.StatusUnauthorized, "ownership verification failed: "+authErr.Error())
		return
	}

	var req models.UpdateRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply updates
	if req.Name != nil {
		existing.Name = *req.Name
	}
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
	if req.CodebaseHash != nil {
		existing.CodebaseHash = *req.CodebaseHash
	}
	existing.UpdatedAt = models.NowRFC3339()
	existing.Signature = req.Signature

	if err := s.store.UpdateAgent(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	// Re-index
	s.searchEngine.IndexAgent(existing)

	// Gossip the update to mesh peers
	ann := s.gossip.CreateAnnouncement(existing, "update", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusOK, existing)
}

// handleDeleteAgent deregisters an agent from the registry.
//
//	@Summary		Delete an agent
//	@Description	Deregister an agent from the registry. Creates a tombstone that propagates via gossip.
//	@Tags			Agents
//	@Produce		json
//	@Param			agentID	path		string			true	"Agent ID"
//	@Success		200		{object}	map[string]string	"Deregistration confirmation"
//	@Failure		400		{object}	map[string]string	"Missing agent_id"
//	@Failure		404		{object}	map[string]string	"Agent not found"
//	@Failure		500		{object}	map[string]string	"Internal server error"
//	@Router			/v1/agents/{agentID} [delete]
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

	// Dual-key authorization: accept either agent key OR developer key
	if authErr := verifyDualKeyOwnership(s.store, agent, []byte(agentID), r.Header.Get("Authorization")); authErr != nil {
		writeError(w, http.StatusUnauthorized, "ownership verification failed: "+authErr.Error())
		return
	}

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

	// Gossip the tombstone to mesh peers
	ann := s.gossip.CreateAnnouncement(agent, "deregister", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	// Publish deregistration event
	s.eventBus.Publish(events.EventAgentDeregistered, events.AgentEventData{
		AgentID:  agentID,
		Name:     agent.Name,
		Category: agent.Category,
		Tags:     agent.Tags,
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "agent deregistered"})
}

// handleGetAgentCard fetches an agent's dynamic Agent Card.
//
//	@Summary		Get agent card
//	@Description	Fetch the live Agent Card from the agent's endpoint. The card contains capabilities, pricing, status, and more.
//	@Tags			Agents
//	@Produce		json
//	@Param			agentID	path		string				true	"Agent ID"
//	@Success		200		{object}	models.AgentCard	"Agent card"
//	@Failure		400		{object}	map[string]string	"Missing agent_id"
//	@Failure		404		{object}	map[string]string	"Agent not found"
//	@Failure		502		{object}	map[string]string	"Failed to fetch agent card from remote"
//	@Router			/v1/agents/{agentID}/card [get]
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

	// Fetch raw JSON from the agent — preserves all SDK fields
	rawCard, err := s.cardFetcher.FetchCardRaw(agentID, agent.AgentURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch agent card: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(rawCard)
}

// --- Search Handlers ---

// handleSearch performs a ranked search across local and gossip indexes.
//
//	@Summary		Search for agents
//	@Description	Search the registry for agents by natural language query, with optional category/tag/trust filters.
//	@Tags			Search
//	@Accept			json
//	@Produce		json
//	@Param			body	body		models.SearchRequest	true	"Search query and filters"
//	@Success		200		{object}	models.SearchResponse	"Search results"
//	@Failure		400		{object}	map[string]string		"Invalid request or missing query"
//	@Failure		500		{object}	map[string]string		"Search failed"
//	@Router			/v1/search [post]
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

// handleGetCategories returns all known agent categories.
//
//	@Summary		List categories
//	@Description	Get all agent categories currently registered in the system.
//	@Tags			Search
//	@Produce		json
//	@Success		200	{object}	map[string][]string	"List of categories"
//	@Failure		500	{object}	map[string]string	"Internal server error"
//	@Router			/v1/categories [get]
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

// handleGetTags returns all known agent tags.
//
//	@Summary		List tags
//	@Description	Get all agent tags currently in use across registered agents.
//	@Tags			Search
//	@Produce		json
//	@Success		200	{object}	map[string][]string	"List of tags"
//	@Failure		500	{object}	map[string]string	"Internal server error"
//	@Router			/v1/tags [get]
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

// handleNetworkStatus returns the current node's status.
//
//	@Summary		Get node status
//	@Description	Returns the current registry node's identity, uptime, peer count, and agent statistics.
//	@Tags			Network
//	@Produce		json
//	@Success		200	{object}	models.NetworkStatus	"Node status"
//	@Router			/v1/network/status [get]
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

// handleGetPeers returns all connected mesh peers.
//
//	@Summary		List peers
//	@Description	Returns a list of all connected peer registries in the mesh network.
//	@Tags			Network
//	@Produce		json
//	@Success		200	{object}	map[string][]models.PeerInfo	"Connected peers"
//	@Router			/v1/network/peers [get]
func (s *Server) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	peers := s.peerManager.GetPeers()
	if peers == nil {
		peers = []*models.PeerInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"peers": peers})
}

// handleAddPeer manually adds a peer to the mesh.
//
//	@Summary		Add a peer
//	@Description	Manually add a peer registry node to the mesh network. Requires at minimum the peer's address.
//	@Tags			Network
//	@Accept			json
//	@Produce		json
//	@Param			body	body		models.PeerInfo		true	"Peer information"
//	@Success		201		{object}	map[string]string	"Peer added confirmation"
//	@Failure		400		{object}	map[string]string	"Invalid request or missing address"
//	@Router			/v1/network/peers [post]
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

// handleNetworkStats returns estimated global network statistics.
//
//	@Summary		Get network stats
//	@Description	Returns estimated global network statistics including registry and agent counts.
//	@Tags			Network
//	@Produce		json
//	@Success		200	{object}	models.NetworkStats	"Network statistics"
//	@Router			/v1/network/stats [get]
func (s *Server) handleNetworkStats(w http.ResponseWriter, r *http.Request) {
	agentCount, _ := s.store.CountAgents()
	gossipCount, _ := s.store.CountGossipEntries()

	stats := models.NetworkStats{
		EstimatedRegistries: s.peerManager.PeerCount() + 1,
		EstimatedAgents:     agentCount + gossipCount,
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleHealth returns a simple health check.
//
//	@Summary		Health check
//	@Description	Returns OK if the registry node is running.
//	@Tags			Health
//	@Produce		json
//	@Success		200	{object}	map[string]string	"Health status"
//	@Router			/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleActivityStream upgrades to a WebSocket and streams real-time network
// activity events: agent registrations/deregistrations, gossip in/out,
// federated search in/out, and peer connect/disconnect.
func (s *Server) handleActivityStream(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ch := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(ch)

	// Send a welcome event so clients know they're connected
	welcome := events.Event{
		Type:      "connected",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data: map[string]string{
			"registry_id": s.nodeIdentity.RegistryID(),
			"message":     "streaming network activity",
		},
	}
	if err := conn.WriteJSON(welcome); err != nil {
		return
	}

	// Handle client disconnection in background
	closeCh := make(chan struct{})
	go func() {
		defer close(closeCh)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-closeCh:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		}
	}
}

// --- Registry Info / Onboarding ---

// handleRegistryInfo returns public information about this registry node.
func (s *Server) handleRegistryInfo(w http.ResponseWriter, r *http.Request) {
	info := models.RegistryInfoResponse{
		RegistryID: s.nodeIdentity.RegistryID(),
		Name:       s.cfg.Node.Name,
		DeveloperOnboarding: &models.DeveloperOnboardingInfo{
			Mode: s.cfg.Onboarding.Mode,
		},
	}
	if s.cfg.Onboarding.Mode == "restricted" {
		info.DeveloperOnboarding.AuthURL = s.cfg.Onboarding.AuthURL
	}
	writeJSON(w, http.StatusOK, info)
}

// handleApproveDeveloper is called by the org website after KYC approval.
//
//	@Summary		Approve developer registration
//	@Description	Approve a developer registration in restricted mode. Generates a keypair and returns the encrypted private key. Requires Bearer webhook token.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Param			Authorization	header		string								true	"Bearer <webhook-secret>"
//	@Param			body			body		models.DeveloperApprovalRequest		true	"Approval request with name and state"
//	@Success		201				{object}	models.DeveloperApprovalResponse	"Developer approval response with encrypted private key"
//	@Failure		400				{object}	map[string]string					"Validation error"
//	@Failure		409				{object}	map[string]string					"Developer already registered"
//	@Failure		500				{object}	map[string]string					"Internal server error"
//	@Router			/v1/admin/developers/approve [post]
func (s *Server) handleApproveDeveloper(w http.ResponseWriter, r *http.Request) {
	var req models.DeveloperApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" || req.State == "" {
		writeError(w, http.StatusBadRequest, "name and state are required")
		return
	}

	// Generate Ed25519 keypair for the developer
	kp, err := identity.GenerateKeypair()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate keypair")
		return
	}

	developerID := models.GenerateDeveloperID(kp.PublicKey)

	now := models.NowRFC3339()
	dev := &models.DeveloperRecord{
		DeveloperID:   developerID,
		Name:          req.Name,
		PublicKey:     kp.PublicKeyString(),
		HomeRegistry:  s.nodeIdentity.RegistryID(),
		SchemaVersion: models.CurrentSchemaVersion,
		RegisteredAt:  now,
		UpdatedAt:     now,
		Signature:     "", // registry-generated; no self-signature
	}

	if err := s.store.CreateDeveloper(dev); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "developer already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to register developer: "+err.Error())
		return
	}

	// Encrypt the private key with SHA256(state)
	privateKeyEnc, err := models.EncryptPrivateKey(kp.PrivateKeyB64, req.State)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt private key")
		return
	}

	// Gossip the developer identity to mesh peers
	ann := s.gossip.CreateDeveloperAnnouncement(dev, "register", s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusCreated, models.DeveloperApprovalResponse{
		DeveloperID:   developerID,
		PrivateKeyEnc: privateKeyEnc,
		PublicKey:     kp.PublicKeyString(),
	})
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

// verifyOwnership checks that the Authorization header contains a valid
// signature over data, signed by the private key matching publicKey.
// Accepts: "Authorization: Bearer ed25519:<base64sig>"
func verifyOwnership(publicKey string, data []byte, authHeader string) error {
	if authHeader == "" {
		return fmt.Errorf("missing Authorization header")
	}

	var signature string
	if strings.HasPrefix(authHeader, "Bearer-Dev ") {
		signature = strings.TrimPrefix(authHeader, "Bearer-Dev ")
	} else {
		signature = strings.TrimPrefix(authHeader, "Bearer ")
		if signature == authHeader {
			return fmt.Errorf("Authorization header must use Bearer or Bearer-Dev scheme")
		}
	}

	pubKey := publicKey
	if strings.HasPrefix(pubKey, "ed25519:") {
		pubKey = pubKey[8:]
	}

	valid, err := identity.Verify(pubKey, data, signature)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid ownership signature")
	}
	return nil
}

// verifyDualKeyOwnership checks authorization using either the agent's key
// or the developer's key. This enables two authorization paths:
//   - "Authorization: Bearer ed25519:<sig>" -- verified against agent's public key
//   - "Authorization: Bearer-Dev ed25519:<sig>" -- verified against developer's public key
//
// If the agent has a developer_id, the developer key is looked up from the store.
func verifyDualKeyOwnership(st store.Store, agent *models.RegistryRecord, data []byte, authHeader string) error {
	if authHeader == "" {
		return fmt.Errorf("missing Authorization header")
	}

	// Try developer key first if Bearer-Dev scheme is used
	if strings.HasPrefix(authHeader, "Bearer-Dev ") {
		if agent.DeveloperID == "" {
			return fmt.Errorf("agent has no developer; cannot use Bearer-Dev auth")
		}
		// Look up developer's public key
		dev, err := st.GetDeveloper(agent.DeveloperID)
		if err != nil {
			return fmt.Errorf("failed to look up developer: %w", err)
		}
		if dev == nil {
			// Try gossip developers
			gossipDev, gErr := st.GetGossipDeveloper(agent.DeveloperID)
			if gErr != nil || gossipDev == nil {
				return fmt.Errorf("developer not found")
			}
			return verifyOwnership(gossipDev.PublicKey, data, authHeader)
		}
		return verifyOwnership(dev.PublicKey, data, authHeader)
	}

	// Default: verify against agent's own key
	return verifyOwnership(agent.PublicKey, data, authHeader)
}

// rateLimited wraps an http.HandlerFunc with per-IP rate limiting.
func rateLimited(rl *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		if !rl.Allow(ip) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	}
}

// ============================================================
// ZNS (Zynd Naming Service) Handlers
// ============================================================

// --- Handle Endpoints ---

// handleClaimHandle claims a ZNS developer handle.
//
//	@Summary		Claim a handle
//	@Description	Claim a ZNS developer handle. Requires an existing developer identity and a valid signature.
//	@Tags			Handles
//	@Accept			json
//	@Produce		json
//	@Param			body	body		models.HandleClaimRequest	true	"Handle claim payload"
//	@Success		201		{object}	map[string]string			"handle, developer_id, and success message"
//	@Failure		400		{object}	map[string]string			"Validation error"
//	@Failure		401		{object}	map[string]string			"Invalid signature"
//	@Failure		404		{object}	map[string]string			"Developer not found"
//	@Failure		409		{object}	map[string]string			"Handle already taken"
//	@Failure		500		{object}	map[string]string			"Internal server error"
//	@Router			/v1/handles [post]
func (s *Server) handleClaimHandle(w http.ResponseWriter, r *http.Request) {
	var req models.HandleClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Handle == "" || req.DeveloperID == "" || req.PublicKey == "" || req.Signature == "" {
		writeError(w, http.StatusBadRequest, "handle, developer_id, public_key, and signature are required")
		return
	}

	// Validate handle format
	if err := zns.ValidateHandle(req.Handle); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Verify developer exists and signature is valid
	dev, err := s.store.GetDeveloper(req.DeveloperID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up developer")
		return
	}
	if dev == nil {
		writeError(w, http.StatusNotFound, "developer not found; register developer first")
		return
	}

	// Verify signature
	pubKey := strings.TrimPrefix(req.PublicKey, "ed25519:")
	signable, _ := json.Marshal(map[string]string{
		"handle":       req.Handle,
		"developer_id": req.DeveloperID,
		"public_key":   req.PublicKey,
	})
	valid, vErr := identity.Verify(pubKey, signable, req.Signature)
	if vErr != nil || !valid {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	registryHost := s.cfg.RegistryHost()

	// Claim the handle
	if err := s.store.ClaimHandle(req.DeveloperID, req.Handle, registryHost); err != nil {
		if strings.Contains(err.Error(), "taken") || strings.Contains(err.Error(), "already") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to claim handle: "+err.Error())
		return
	}

	// Gossip the handle claim
	ann := s.gossip.CreateHandleAnnouncement(dev, req.Handle, false, "", "claim",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	s.eventBus.Publish(events.EventHandleClaimed, events.ZNSEventData{
		Handle:      req.Handle,
		DeveloperID: req.DeveloperID,
		Action:      "claim",
	})

	writeJSON(w, http.StatusCreated, map[string]string{
		"handle":       req.Handle,
		"developer_id": req.DeveloperID,
		"message":      "handle claimed successfully",
	})
}

// handleGetHandle gets the developer associated with a ZNS handle.
//
//	@Summary		Get developer by handle
//	@Description	Retrieve the developer record associated with a ZNS handle, including verification status.
//	@Tags			Handles
//	@Produce		json
//	@Param			handle	path		string				true	"ZNS handle (e.g. alice)"
//	@Success		200		{object}	map[string]interface{}	"handle, developer_id, developer_name, verified, verification_method"
//	@Failure		400		{object}	map[string]string		"Missing handle"
//	@Failure		404		{object}	map[string]string		"Handle not found"
//	@Failure		500		{object}	map[string]string		"Internal server error"
//	@Router			/v1/handles/{handle} [get]
func (s *Server) handleGetHandle(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeError(w, http.StatusBadRequest, "handle is required")
		return
	}

	registryHost := s.cfg.RegistryHost()
	dev, err := s.store.GetDeveloperByHandle(handle, registryHost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up handle")
		return
	}
	if dev == nil {
		writeError(w, http.StatusNotFound, "handle not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"handle":              dev.DevHandle,
		"developer_id":        dev.DeveloperID,
		"developer_name":      dev.Name,
		"verified":            dev.DevHandleVerified,
		"verification_method": dev.VerificationMethod,
	})
}

// handleCheckHandleAvailable checks whether a ZNS handle is available.
//
//	@Summary		Check handle availability
//	@Description	Check whether a ZNS handle is available on this registry. Returns availability and optional reason if taken.
//	@Tags			Handles
//	@Produce		json
//	@Param			handle	path		string				true	"ZNS handle to check"
//	@Success		200		{object}	map[string]interface{}	"handle, available, and optional reason"
//	@Failure		400		{object}	map[string]string		"Missing handle"
//	@Failure		500		{object}	map[string]string		"Internal server error"
//	@Router			/v1/handles/{handle}/available [get]
func (s *Server) handleCheckHandleAvailable(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeError(w, http.StatusBadRequest, "handle is required")
		return
	}

	// Validate format first
	if err := zns.ValidateHandle(handle); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"handle":    handle,
			"available": false,
			"reason":    err.Error(),
		})
		return
	}

	registryHost := s.cfg.RegistryHost()
	dev, err := s.store.GetDeveloperByHandle(handle, registryHost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check handle")
		return
	}

	available := dev == nil
	resp := map[string]interface{}{
		"handle":    handle,
		"available": available,
	}
	if !available {
		resp["reason"] = "handle is already taken on this registry"
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReleaseHandle releases a claimed ZNS handle.
//
//	@Summary		Release a handle
//	@Description	Release a previously claimed ZNS handle. Requires Authorization header with Bearer ed25519 signature.
//	@Tags			Handles
//	@Produce		json
//	@Param			handle			path		string				true	"ZNS handle to release"
//	@Param			Authorization	header		string				true	"Bearer ed25519:<base64sig>"
//	@Success		200				{object}	map[string]string	"Release confirmation"
//	@Failure		400				{object}	map[string]string	"Missing handle"
//	@Failure		401				{object}	map[string]string	"Ownership verification failed"
//	@Failure		404				{object}	map[string]string	"Handle not found"
//	@Failure		500				{object}	map[string]string	"Internal server error"
//	@Router			/v1/handles/{handle} [delete]
func (s *Server) handleReleaseHandle(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeError(w, http.StatusBadRequest, "handle is required")
		return
	}

	// Verify ownership via Authorization header
	authHeader := r.Header.Get("Authorization")
	registryHost := s.cfg.RegistryHost()
	dev, err := s.store.GetDeveloperByHandle(handle, registryHost)
	if err != nil || dev == nil {
		writeError(w, http.StatusNotFound, "handle not found")
		return
	}

	signable := []byte("release:" + handle)
	if err := verifyOwnership(dev.PublicKey, signable, authHeader); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized: "+err.Error())
		return
	}

	if err := s.store.ReleaseHandle(dev.DeveloperID, handle); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to release handle: "+err.Error())
		return
	}

	// Gossip the release
	ann := s.gossip.CreateHandleAnnouncement(dev, handle, false, "", "release",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusOK, map[string]string{"message": "handle released"})
}

// handleVerifyHandle verifies a ZNS handle via DNS or GitHub.
//
//	@Summary		Verify a handle
//	@Description	Verify a ZNS handle ownership via DNS TXT record or GitHub. Sets the verified flag and verification method on success.
//	@Tags			Handles
//	@Accept			json
//	@Produce		json
//	@Param			handle	path		string						true	"ZNS handle to verify"
//	@Param			body	body		models.HandleVerifyRequest	true	"Verification method and proof"
//	@Success		200		{object}	map[string]string			"Verification confirmation"
//	@Failure		400		{object}	map[string]string			"Verification failed or invalid method"
//	@Failure		404		{object}	map[string]string			"Handle not found"
//	@Failure		500		{object}	map[string]string			"Internal server error"
//	@Router			/v1/handles/{handle}/verify [post]
func (s *Server) handleVerifyHandle(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeError(w, http.StatusBadRequest, "handle is required")
		return
	}

	var req models.HandleVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	registryHost := s.cfg.RegistryHost()
	dev, err := s.store.GetDeveloperByHandle(handle, registryHost)
	if err != nil || dev == nil {
		writeError(w, http.StatusNotFound, "handle not found")
		return
	}

	switch req.Method {
	case "dns":
		// Verify DNS TXT record at _zynd-verify.{domain}
		matched, dnsErr := zns.VerifyDeveloperDNS(req.Proof, dev.PublicKey)
		if dnsErr != nil {
			writeError(w, http.StatusBadRequest, "DNS verification failed: "+dnsErr.Error())
			return
		}
		if !matched {
			writeError(w, http.StatusBadRequest, "DNS TXT record does not contain the developer's public key")
			return
		}
	case "github":
		// GitHub OAuth verification (simplified — just records the claim for now)
		if req.Proof == "" {
			writeError(w, http.StatusBadRequest, "github username is required as proof")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "method must be 'dns' or 'github'")
		return
	}

	if err := s.store.UpdateHandleVerification(dev.DeveloperID, true, req.Method, req.Proof); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update verification: "+err.Error())
		return
	}

	// Gossip the verification
	ann := s.gossip.CreateHandleAnnouncement(dev, handle, true, req.Method, "verify",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	s.eventBus.Publish(events.EventHandleVerified, events.ZNSEventData{
		Handle:      handle,
		DeveloperID: dev.DeveloperID,
		Action:      "verify",
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "handle verified via " + req.Method,
		"handle":  handle,
	})
}

// handleListHandleAgents lists all ZNS name bindings (agent bindings) for a handle.
//
//	@Summary		List agents for a handle
//	@Description	List all ZNS name bindings (agent names) registered under a given developer handle.
//	@Tags			Handles
//	@Produce		json
//	@Param			handle	path		string				true	"ZNS developer handle"
//	@Success		200		{array}		models.ZNSName		"List of ZNS name bindings"
//	@Failure		400		{object}	map[string]string	"Missing handle"
//	@Failure		500		{object}	map[string]string	"Internal server error"
//	@Router			/v1/handles/{handle}/agents [get]
func (s *Server) handleListHandleAgents(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeError(w, http.StatusBadRequest, "handle is required")
		return
	}

	registryHost := s.cfg.RegistryHost()
	names, err := s.store.ListZNSNamesByDeveloper(handle, registryHost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	if names == nil {
		names = []*models.ZNSName{}
	}
	writeJSON(w, http.StatusOK, names)
}

// --- Name Binding Endpoints ---

// handleRegisterName registers an agent name binding (ZNS FQAN).
//
//	@Summary		Register an agent name
//	@Description	Register a ZNS agent name binding, creating a Fully Qualified Agent Name (FQAN) under a developer handle.
//	@Tags			Names
//	@Accept			json
//	@Produce		json
//	@Param			body	body		models.NameBindingRequest	true	"Name binding payload"
//	@Success		201		{object}	map[string]string			"fqan, agent_id, and success message"
//	@Failure		400		{object}	map[string]string			"Validation error"
//	@Failure		401		{object}	map[string]string			"Invalid signature"
//	@Failure		404		{object}	map[string]string			"Developer handle or agent not found"
//	@Failure		409		{object}	map[string]string			"Name already registered"
//	@Failure		500		{object}	map[string]string			"Internal server error"
//	@Router			/v1/names [post]
func (s *Server) handleRegisterName(w http.ResponseWriter, r *http.Request) {
	var req models.NameBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.AgentName == "" || req.DeveloperHandle == "" || req.AgentID == "" || req.Signature == "" {
		writeError(w, http.StatusBadRequest, "agent_name, developer_handle, agent_id, and signature are required")
		return
	}

	if err := zns.ValidateAgentName(req.AgentName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	registryHost := s.cfg.RegistryHost()
	if registryHost == "" {
		writeError(w, http.StatusBadRequest, "registry has no HTTPS endpoint configured; ZNS naming is unavailable")
		return
	}

	// Verify developer handle exists
	dev, err := s.store.GetDeveloperByHandle(req.DeveloperHandle, registryHost)
	if err != nil || dev == nil {
		writeError(w, http.StatusNotFound, "developer handle not found")
		return
	}

	// Verify agent exists
	agent, err := s.store.GetAgent(req.AgentID)
	if err != nil || agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Check if this agent name already exists under the same developer with a different key
	existingName, _ := s.store.GetZNSNameByParts(req.DeveloperHandle, req.AgentName, registryHost)
	if existingName != nil {
		existingAgent, _ := s.store.GetAgent(existingName.AgentID)
		if existingAgent != nil && existingAgent.PublicKey != agent.PublicKey {
			writeError(w, http.StatusConflict,
				fmt.Sprintf("agent name %q is already registered under %s with a different key; choose a different name",
					req.AgentName, req.DeveloperHandle))
			return
		}
	}

	// Verify signature (developer signs the binding)
	pubKey := strings.TrimPrefix(dev.PublicKey, "ed25519:")
	signable, _ := json.Marshal(map[string]interface{}{
		"agent_name":       req.AgentName,
		"developer_handle": req.DeveloperHandle,
		"agent_id":         req.AgentID,
		"version":          req.Version,
		"capability_tags":  req.CapabilityTags,
	})
	valid, vErr := identity.Verify(pubKey, signable, req.Signature)
	if vErr != nil || !valid {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	fqan := zns.BuildFQAN(registryHost, req.DeveloperHandle, req.AgentName)
	now := models.NowRFC3339()

	name := &models.ZNSName{
		FQAN:            fqan,
		AgentName:       req.AgentName,
		DeveloperHandle: req.DeveloperHandle,
		RegistryHost:    registryHost,
		AgentID:         req.AgentID,
		DeveloperID:     dev.DeveloperID,
		CurrentVersion:  req.Version,
		CapabilityTags:  req.CapabilityTags,
		RegisteredAt:    now,
		UpdatedAt:       now,
		Signature:       req.Signature,
	}

	if err := s.store.CreateZNSName(name); err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeError(w, http.StatusConflict, "name already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to register name: "+err.Error())
		return
	}

	// Create version record if version provided
	if req.Version != "" {
		ver := &models.ZNSVersion{
			FQAN:         fqan,
			Version:      req.Version,
			AgentID:      req.AgentID,
			RegisteredAt: now,
			Signature:    req.Signature,
		}
		s.store.CreateZNSVersion(ver)
	}

	// Gossip the name binding
	ann := s.gossip.CreateNameBindingAnnouncement(name, "register",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	s.eventBus.Publish(events.EventNameRegistered, events.ZNSEventData{
		FQAN:        fqan,
		DeveloperID: dev.DeveloperID,
		AgentID:     req.AgentID,
		Action:      "register",
	})

	writeJSON(w, http.StatusCreated, map[string]string{
		"fqan":     fqan,
		"agent_id": req.AgentID,
		"message":  "name registered successfully",
	})
}

// handleGetName gets a ZNS name binding by developer handle and agent name.
//
//	@Summary		Get name binding
//	@Description	Retrieve a ZNS name binding record by developer handle and agent name path parameters.
//	@Tags			Names
//	@Produce		json
//	@Param			developer	path		string			true	"Developer handle"
//	@Param			agent		path		string			true	"Agent name"
//	@Success		200			{object}	models.ZNSName	"ZNS name binding"
//	@Failure		400			{object}	map[string]string	"Missing path parameters"
//	@Failure		404			{object}	map[string]string	"Name not found"
//	@Failure		500			{object}	map[string]string	"Internal server error"
//	@Router			/v1/names/{developer}/{agent} [get]
func (s *Server) handleGetName(w http.ResponseWriter, r *http.Request) {
	devHandle := r.PathValue("developer")
	agentName := r.PathValue("agent")
	if devHandle == "" || agentName == "" {
		writeError(w, http.StatusBadRequest, "developer and agent are required")
		return
	}

	registryHost := s.cfg.RegistryHost()
	name, err := s.store.GetZNSNameByParts(devHandle, agentName, registryHost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up name")
		return
	}
	if name == nil {
		writeError(w, http.StatusNotFound, "name not found")
		return
	}

	writeJSON(w, http.StatusOK, name)
}

// handleCheckNameAvailable checks whether a ZNS agent name is available.
//
//	@Summary		Check name availability
//	@Description	Check whether a ZNS agent name is available under a given developer handle.
//	@Tags			Names
//	@Produce		json
//	@Param			developer	path		string				true	"Developer handle"
//	@Param			agent		path		string				true	"Agent name"
//	@Success		200			{object}	map[string]interface{}	"developer, agent_name, available, and optional reason"
//	@Failure		400			{object}	map[string]string		"Missing path parameters"
//	@Failure		500			{object}	map[string]string		"Internal server error"
//	@Router			/v1/names/{developer}/{agent}/available [get]
func (s *Server) handleCheckNameAvailable(w http.ResponseWriter, r *http.Request) {
	devHandle := r.PathValue("developer")
	agentName := r.PathValue("agent")
	if devHandle == "" || agentName == "" {
		writeError(w, http.StatusBadRequest, "developer and agent are required")
		return
	}

	// Validate format
	if err := zns.ValidateAgentName(agentName); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"developer":  devHandle,
			"agent_name": agentName,
			"available":  false,
			"reason":     err.Error(),
		})
		return
	}

	registryHost := s.cfg.RegistryHost()
	existing, err := s.store.GetZNSNameByParts(devHandle, agentName, registryHost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check name availability")
		return
	}

	available := existing == nil
	resp := map[string]interface{}{
		"developer":  devHandle,
		"agent_name": agentName,
		"available":  available,
	}
	if !available {
		resp["reason"] = "agent name is already registered under this developer"
		resp["existing_agent_id"] = existing.AgentID
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateName updates a ZNS name binding's version or capability tags.
//
//	@Summary		Update name binding
//	@Description	Update the version and/or capability_tags of an existing ZNS agent name binding.
//	@Tags			Names
//	@Accept			json
//	@Produce		json
//	@Param			developer	path		string			true	"Developer handle"
//	@Param			agent		path		string			true	"Agent name"
//	@Param			body		body		object			true	"Fields to update (version, capability_tags, signature)"
//	@Success		200			{object}	models.ZNSName	"Updated ZNS name binding"
//	@Failure		400			{object}	map[string]string	"Invalid request body"
//	@Failure		404			{object}	map[string]string	"Name not found"
//	@Failure		500			{object}	map[string]string	"Internal server error"
//	@Router			/v1/names/{developer}/{agent} [put]
func (s *Server) handleUpdateName(w http.ResponseWriter, r *http.Request) {
	devHandle := r.PathValue("developer")
	agentName := r.PathValue("agent")

	registryHost := s.cfg.RegistryHost()
	name, err := s.store.GetZNSNameByParts(devHandle, agentName, registryHost)
	if err != nil || name == nil {
		writeError(w, http.StatusNotFound, "name not found")
		return
	}

	var req struct {
		Version        string   `json:"version,omitempty"`
		CapabilityTags []string `json:"capability_tags,omitempty"`
		Signature      string   `json:"signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := models.NowRFC3339()
	if req.Version != "" {
		name.CurrentVersion = req.Version
	}
	if req.CapabilityTags != nil {
		name.CapabilityTags = req.CapabilityTags
	}
	name.UpdatedAt = now
	name.Signature = req.Signature

	if err := s.store.UpdateZNSName(name); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update name: "+err.Error())
		return
	}

	// Create version record if new version
	if req.Version != "" {
		ver := &models.ZNSVersion{
			FQAN:         name.FQAN,
			Version:      req.Version,
			AgentID:      name.AgentID,
			RegisteredAt: now,
			Signature:    req.Signature,
		}
		s.store.CreateZNSVersion(ver)
	}

	// Gossip the update
	ann := s.gossip.CreateNameBindingAnnouncement(name, "update",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusOK, name)
}

// handleReleaseName releases a ZNS agent name binding.
//
//	@Summary		Release a name binding
//	@Description	Release a ZNS agent name binding. Requires Authorization header with Bearer ed25519 signature.
//	@Tags			Names
//	@Produce		json
//	@Param			developer		path		string				true	"Developer handle"
//	@Param			agent			path		string				true	"Agent name"
//	@Param			Authorization	header		string				true	"Bearer ed25519:<base64sig>"
//	@Success		200				{object}	map[string]string	"Release confirmation"
//	@Failure		401				{object}	map[string]string	"Ownership verification failed"
//	@Failure		404				{object}	map[string]string	"Name or developer not found"
//	@Failure		500				{object}	map[string]string	"Internal server error"
//	@Router			/v1/names/{developer}/{agent} [delete]
func (s *Server) handleReleaseName(w http.ResponseWriter, r *http.Request) {
	devHandle := r.PathValue("developer")
	agentName := r.PathValue("agent")

	registryHost := s.cfg.RegistryHost()
	name, err := s.store.GetZNSNameByParts(devHandle, agentName, registryHost)
	if err != nil || name == nil {
		writeError(w, http.StatusNotFound, "name not found")
		return
	}

	// Verify ownership via Authorization header
	authHeader := r.Header.Get("Authorization")
	dev, _ := s.store.GetDeveloperByHandle(devHandle, registryHost)
	if dev == nil {
		writeError(w, http.StatusNotFound, "developer not found")
		return
	}
	signable := []byte("release:" + name.FQAN)
	if err := verifyOwnership(dev.PublicKey, signable, authHeader); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized: "+err.Error())
		return
	}

	if err := s.store.DeleteZNSName(name.FQAN); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to release name: "+err.Error())
		return
	}

	// Gossip the release
	ann := s.gossip.CreateNameBindingAnnouncement(name, "release",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	writeJSON(w, http.StatusOK, map[string]string{"message": "name released"})
}

// handleListVersions lists version history for a ZNS name binding.
//
//	@Summary		List version history
//	@Description	Retrieve the full version history for a ZNS agent name binding.
//	@Tags			Names
//	@Produce		json
//	@Param			developer	path		string				true	"Developer handle"
//	@Param			agent		path		string				true	"Agent name"
//	@Success		200			{array}		models.ZNSVersion	"List of version records"
//	@Failure		404			{object}	map[string]string	"Name not found"
//	@Failure		500			{object}	map[string]string	"Internal server error"
//	@Router			/v1/names/{developer}/{agent}/versions [get]
func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	devHandle := r.PathValue("developer")
	agentName := r.PathValue("agent")

	registryHost := s.cfg.RegistryHost()
	name, err := s.store.GetZNSNameByParts(devHandle, agentName, registryHost)
	if err != nil || name == nil {
		writeError(w, http.StatusNotFound, "name not found")
		return
	}

	versions, err := s.store.GetZNSVersions(name.FQAN)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	if versions == nil {
		versions = []*models.ZNSVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}

// --- Resolution Endpoint ---

// handleResolveName resolves a FQAN to agent details.
//
//	@Summary		Resolve a FQAN
//	@Description	Resolve a Fully Qualified Agent Name (FQAN) to its agent details, including agent_url, public_key, and trust information.
//	@Tags			Resolution
//	@Produce		json
//	@Param			developer	path		string						true	"Developer handle"
//	@Param			agent		path		string						true	"Agent name"
//	@Success		200			{object}	models.ZNSResolveResponse	"Resolved agent details"
//	@Failure		400			{object}	map[string]string			"Missing path parameters"
//	@Failure		404			{object}	map[string]string			"Name not found"
//	@Failure		500			{object}	map[string]string			"Internal server error"
//	@Router			/v1/resolve/{developer}/{agent} [get]
func (s *Server) handleResolveName(w http.ResponseWriter, r *http.Request) {
	devHandle := r.PathValue("developer")
	agentName := r.PathValue("agent")
	if devHandle == "" || agentName == "" {
		writeError(w, http.StatusBadRequest, "developer and agent path parameters are required")
		return
	}

	registryHost := s.cfg.RegistryHost()

	// 1. Try local ZNS names
	name, err := s.store.GetZNSNameByParts(devHandle, agentName, registryHost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolution failed")
		return
	}

	if name != nil {
		agent, _ := s.store.GetAgent(name.AgentID)
		dev, _ := s.store.GetDeveloperByHandle(devHandle, registryHost)
		resp := s.buildResolveResponse(name, agent, dev)

		s.eventBus.Publish(events.EventNameResolved, events.ZNSEventData{
			FQAN:    name.FQAN,
			AgentID: name.AgentID,
			Action:  "resolve",
		})

		writeJSON(w, http.StatusOK, resp)
		return
	}

	// 2. Try gossip ZNS names
	gossipName, err := s.store.GetGossipZNSNameByParts(devHandle, agentName)
	if err == nil && gossipName != nil {
		resp := &models.ZNSResolveResponse{
			FQAN:            gossipName.FQAN,
			AgentID:         gossipName.AgentID,
			DeveloperHandle: gossipName.DeveloperHandle,
			RegistryHost:    gossipName.RegistryHost,
			Version:         gossipName.CurrentVersion,
			Status:          "unknown",
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	writeError(w, http.StatusNotFound, "name not found")
}

func (s *Server) buildResolveResponse(name *models.ZNSName, agent *models.RegistryRecord, dev *models.DeveloperRecord) *models.ZNSResolveResponse {
	resp := &models.ZNSResolveResponse{
		FQAN:            name.FQAN,
		AgentID:         name.AgentID,
		DeveloperID:     name.DeveloperID,
		DeveloperHandle: name.DeveloperHandle,
		RegistryHost:    name.RegistryHost,
		Version:         name.CurrentVersion,
	}

	if agent != nil {
		resp.AgentURL = agent.AgentURL
		resp.PublicKey = agent.PublicKey
		resp.Status = agent.Status
		if agent.CapabilitySummary != nil {
			resp.Protocols = agent.CapabilitySummary.Protocols
		}
	}

	if dev != nil {
		resp.VerificationTier = "self-claimed"
		if dev.DevHandleVerified {
			resp.VerificationTier = dev.VerificationMethod + "-verified"
		}
	}

	resp.TrustScore = 0.5

	return resp
}

// createZNSNameBinding is a helper used by handleRegisterAgent for atomic naming.
func (s *Server) createZNSNameBinding(record *models.RegistryRecord, agentName, version, registryHost string) (string, error) {
	if err := zns.ValidateAgentName(agentName); err != nil {
		return "", err
	}

	// Look up the developer's handle
	dev, err := s.store.GetDeveloper(record.DeveloperID)
	if err != nil || dev == nil || dev.DevHandle == "" {
		return "", fmt.Errorf("developer has no handle")
	}

	// Check if this agent name already exists under the same developer
	existing, _ := s.store.GetZNSNameByParts(dev.DevHandle, agentName, registryHost)
	if existing != nil {
		// Same name exists — only allow if it's the same agent (same public key)
		existingAgent, _ := s.store.GetAgent(existing.AgentID)
		if existingAgent != nil && existingAgent.PublicKey != record.PublicKey {
			return "", fmt.Errorf("agent name %q is already registered under %s with a different key; choose a different name", agentName, dev.DevHandle)
		}
		// Same agent re-registering with same name — return existing FQAN
		if existing.AgentID == record.AgentID {
			return existing.FQAN, nil
		}
	}

	fqan := zns.BuildFQAN(registryHost, dev.DevHandle, agentName)
	now := models.NowRFC3339()

	name := &models.ZNSName{
		FQAN:            fqan,
		AgentName:       agentName,
		DeveloperHandle: dev.DevHandle,
		RegistryHost:    registryHost,
		AgentID:         record.AgentID,
		DeveloperID:     record.DeveloperID,
		CurrentVersion:  version,
		RegisteredAt:    now,
		UpdatedAt:       now,
		Signature:       record.Signature,
	}

	if err := s.store.CreateZNSName(name); err != nil {
		return "", err
	}

	if version != "" {
		ver := &models.ZNSVersion{
			FQAN:         fqan,
			Version:      version,
			AgentID:      record.AgentID,
			RegisteredAt: now,
			Signature:    record.Signature,
		}
		s.store.CreateZNSVersion(ver)
	}

	// Gossip the name binding
	ann := s.gossip.CreateNameBindingAnnouncement(name, "register",
		s.nodeIdentity.RegistryID(), s.nodeIdentity.PublicKeyString(), s.nodeIdentity.Sign)
	s.gossip.BroadcastAnnouncement(ann)

	s.eventBus.Publish(events.EventNameRegistered, events.ZNSEventData{
		FQAN:        fqan,
		DeveloperID: record.DeveloperID,
		AgentID:     record.AgentID,
		Action:      "register",
	})

	return fqan, nil
}

// --- Registry Identity Proof ---

func (s *Server) handleRegistryIdentityProof(w http.ResponseWriter, r *http.Request) {
	// Serve the local registry's identity proof
	regID := s.nodeIdentity.RegistryID()
	proof, err := s.store.GetRegistryProof(regID)
	if err != nil || proof == nil {
		writeError(w, http.StatusNotFound, "no registry identity proof configured")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(proof)
}
