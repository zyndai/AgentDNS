// Package main provides the CLI entry point for Agent DNS.
//
//	@title			Agent DNS Registry API
//	@version		0.2.0
//	@description	Decentralized Agent Registry Network — register, discover, and resolve AI agents across a federated mesh.
//
//	@contact.name	Agent DNS
//	@contact.url	https://github.com/agentdns/agent-dns
//
//	@license.name	MIT
//	@license.url	https://opensource.org/licenses/MIT
//
//	@host		localhost:8080
//	@BasePath	/
//	@schemes	http
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/agentdns/agent-dns/docs" // swagger generated docs

	"github.com/agentdns/agent-dns/internal/api"
	agcache "github.com/agentdns/agent-dns/internal/cache"
	"github.com/agentdns/agent-dns/internal/card"
	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/search"
	"github.com/agentdns/agent-dns/internal/store"
	"github.com/agentdns/agent-dns/internal/trust"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		cmdInit()
	case "start":
		cmdStart()
	case "dev-init":
		cmdDevInit()
	case "dev-register":
		cmdDevRegister()
	case "derive-agent":
		cmdDeriveAgent()
	case "register":
		cmdRegister()
	case "search":
		cmdSearch()
	case "resolve":
		cmdResolve()
	case "card":
		cmdCard()
	case "status":
		cmdStatus()
	case "peers":
		cmdPeers()
	case "deregister":
		cmdDeregister()
	case "models":
		cmdModels()
	case "test":
		cmdTest()
	case "version":
		fmt.Printf("agent-dns %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Agent DNS - Decentralized Agent Registry Network

Usage:
  agentdns <command> [flags]

Commands:
  init          Initialize a new registry node (generates keypair + config)
  start         Start the registry node

  Developer Identity:
  dev-init      Generate a developer keypair (stored at ~/.agentdns/developer.json)
  dev-register  Register developer identity on a registry node
  derive-agent  Derive an agent keypair from developer key at a given index

  Agent Management:
  register      Register an agent on this node (supports --developer-key for HD derivation)
  deregister    Remove an agent from the registry
  resolve       Get a specific agent's registry record
  card          Fetch an agent's dynamic Agent Card

  Discovery:
  search        Search the network for agents

  Network:
  status        Show node status
  peers         Show connected peers

  Maintenance:
  models        Manage embedding models (list, download, info)
  test          Load testing (register/deregister N agents)
  version       Print version
  help          Show this help

Examples:
  agentdns init
  agentdns start
  agentdns dev-init
  agentdns dev-register --name "Alice"
  agentdns derive-agent --index 0
  agentdns register --name "MyAgent" --agent-url "https://example.com/.well-known/agent.json" --category "tools" --tags "python,code" --summary "Does stuff"
  agentdns register --name "MyAgent" --agent-url "https://example.com/.well-known/agent.json" --category "tools" --developer-key ~/.agentdns/developer.json --agent-index 0
  agentdns search "code review agent for Python security"
  agentdns resolve agdns:7f3a9c2e...
  agentdns deregister agdns:7f3a9c2e...`)
}

// --- Init Command ---

func cmdInit() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}

	dataDir := filepath.Join(homeDir, ".agentdns")

	// Check if already initialized
	if _, err := os.Stat(filepath.Join(dataDir, "identity.json")); err == nil {
		fmt.Println("Node already initialized at", dataDir)
		fmt.Println("Delete ~/.agentdns to re-initialize.")
		return
	}

	// Create data directory
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0700); err != nil {
		log.Fatalf("failed to create data directory: %v", err)
	}

	// Generate node keypair
	kp, err := identity.GenerateKeypair()
	if err != nil {
		log.Fatalf("failed to generate keypair: %v", err)
	}

	if err := identity.SaveKeypair(kp, filepath.Join(dataDir, "identity.json")); err != nil {
		log.Fatalf("failed to save keypair: %v", err)
	}

	// Generate default config
	cfg := config.DefaultConfig()
	cfg.Node.DataDir = filepath.Join(dataDir, "data")
	if err := config.Save(cfg, filepath.Join(dataDir, "config.toml")); err != nil {
		log.Fatalf("failed to save config: %v", err)
	}

	fmt.Println("Agent DNS node initialized!")
	fmt.Printf("  Registry ID: %s\n", kp.RegistryID())
	fmt.Printf("  Data dir:    %s\n", dataDir)
	fmt.Printf("  Config:      %s\n", filepath.Join(dataDir, "config.toml"))
	fmt.Printf("  Identity:    %s\n", filepath.Join(dataDir, "identity.json"))
	fmt.Println()
	fmt.Println("Start the node with: agentdns start")
}

// --- Start Command ---

func cmdStart() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}

	dataDir := filepath.Join(homeDir, ".agentdns")

	// Parse flags
	configPath := filepath.Join(dataDir, "config.toml")
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--config" && i+1 < len(os.Args) {
			configPath = os.Args[i+1]
			i++
		}
	}

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Load identity
	kp, err := identity.LoadKeypair(filepath.Join(dataDir, "identity.json"))
	if err != nil {
		log.Fatalf("failed to load identity (run 'agentdns init' first): %v", err)
	}

	// Initialize PostgreSQL store
	if cfg.Registry.PostgresURL == "" {
		log.Fatalf("postgres_url is required in [registry] config section")
	}
	st, err := store.New(cfg.Registry.PostgresURL)
	if err != nil {
		log.Fatalf("failed to initialize PostgreSQL store: %v", err)
	}
	defer st.Close()

	// Initialize Redis cache (optional — nil if not configured)
	var redisCache *agcache.RedisCache
	if cfg.Redis.URL != "" {
		redisCache, err = agcache.NewRedisCache(agcache.RedisConfig{
			URL:      cfg.Redis.URL,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Prefix:   cfg.Redis.Prefix,
		})
		if err != nil {
			log.Printf("warning: failed to connect to Redis, running without cache: %v", err)
			redisCache = nil
		} else {
			defer redisCache.Close()
			log.Printf("Redis cache connected: %s", cfg.Redis.URL)
		}
	}

	// Initialize components
	lruCache := card.NewLRUCache(cfg.Cache.MaxAgentCards, cfg.Cache.AgentCardTTLSeconds)
	fetcher := card.NewFetcher(lruCache, redisCache, cfg.Cache.AgentCardTTLSeconds)
	embedder := search.NewEmbedderFromConfig(
		cfg.Search.EmbeddingBackend,
		cfg.Search.EmbeddingModel,
		cfg.Search.EmbeddingModelDir,
		cfg.Search.EmbeddingEndpoint,
		cfg.Search.EmbeddingDimensions,
	)
	engine := search.NewEngine(st, fetcher, cfg.Search, embedder)
	peerMgr := mesh.NewPeerManager(cfg.Mesh, cfg.Bloom)
	gossipHandler := mesh.NewGossipHandler(st, cfg.Gossip)
	eigenTrust := trust.NewEigenTrust(st, cfg.Trust.EigentrustIterations)

	// Set up gossip callback to index new entries
	gossipHandler.SetAnnounceCallback(func(ann *models.GossipAnnouncement) {
		entry := &models.GossipEntry{
			AgentID:      ann.AgentID,
			Name:         ann.Name,
			Category:     ann.Category,
			Tags:         ann.Tags,
			Summary:      ann.Summary,
			HomeRegistry: ann.HomeRegistry,
			AgentURL:     ann.AgentURL,
			ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		engine.IndexGossipEntry(entry)
	})

	// Rebuild search indexes from stored data
	if err := engine.RebuildIndexes(); err != nil {
		log.Printf("warning: failed to rebuild indexes: %v", err)
	}

	// Start mesh transport
	transport := mesh.NewTransport(cfg.Mesh, cfg.Bloom, cfg.Node.Name, peerMgr, gossipHandler, kp, st)

	// Wire federated search into the search engine
	fedSearch := mesh.NewFederatedSearch(transport, peerMgr, cfg.Search)
	engine.SetFederatedSearcher(fedSearch)

	// Wire the transport's local search handler (for incoming federated queries)
	transport.SetSearchHandler(engine.Search)

	// Wire gossip broadcasting into the gossip handler
	gossipHandler.SetBroadcastFunc(transport.Broadcast)

	// Start mesh listener
	if err := transport.Listen(); err != nil {
		log.Fatalf("failed to start mesh listener: %v", err)
	}

	// Bootstrap connect to peers (in background)
	go transport.BootstrapConnect()

	// Start heartbeat loop (in background)
	go transport.HeartbeatLoop()

	// Start reconnect loop (in background)
	go transport.ReconnectLoop()

	// Start tombstone garbage collection (in background)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			count, err := st.CleanExpiredTombstones()
			if err != nil {
				log.Printf("tombstone GC error: %v", err)
			} else if count > 0 {
				log.Printf("tombstone GC: cleaned %d expired entries", count)
			}
		}
	}()

	// Start the API server
	server := api.NewServer(cfg, st, engine, fetcher, peerMgr, gossipHandler, eigenTrust, kp)

	// Wire event bus into mesh and search components
	bus := server.EventBus()
	gossipHandler.SetEventBus(bus)
	transport.SetEventBus(bus)
	fedSearch.SetEventBus(bus)

	// Start liveness monitor if heartbeat is enabled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Heartbeat.Enabled {
		monitor := api.NewLivenessMonitor(st, cfg.Heartbeat, gossipHandler, kp, bus)
		go monitor.Run(ctx)
	}

	// Graceful shutdown

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		transport.Stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
		cancel()
	}()

	fmt.Printf("Agent DNS Registry Node v%s\n", version)
	fmt.Printf("  Registry ID: %s\n", kp.RegistryID())
	fmt.Printf("  Node name:   %s\n", cfg.Node.Name)
	fmt.Printf("  API:         http://%s\n", cfg.API.Listen)
	fmt.Printf("  Mesh:        0.0.0.0:%d\n", cfg.Mesh.ListenPort)
	fmt.Printf("  Storage:     PostgreSQL\n")
	fmt.Printf("  Embedder:    %s (dims=%d, ranking=%s)\n",
		cfg.Search.EmbeddingBackend, cfg.Search.EmbeddingDimensions, cfg.Search.Ranking.Method)
	if redisCache != nil {
		fmt.Printf("  Redis:       %s\n", cfg.Redis.URL)
	} else {
		fmt.Printf("  Redis:       disabled (in-process cache only)\n")
	}
	if len(cfg.Mesh.BootstrapPeers) > 0 {
		fmt.Printf("  Bootstrap:   %v\n", cfg.Mesh.BootstrapPeers)
	}
	fmt.Println()

	if err := server.Start(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

// --- Register Command ---

func cmdRegister() {
	var name, agentURL, category, tagsStr, summary, developerKeyPath string
	agentIndex := -1

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--name":
			if i+1 < len(os.Args) {
				name = os.Args[i+1]
				i++
			}
		case "--agent-url":
			if i+1 < len(os.Args) {
				agentURL = os.Args[i+1]
				i++
			}
		case "--category":
			if i+1 < len(os.Args) {
				category = os.Args[i+1]
				i++
			}
		case "--tags":
			if i+1 < len(os.Args) {
				tagsStr = os.Args[i+1]
				i++
			}
		case "--summary":
			if i+1 < len(os.Args) {
				summary = os.Args[i+1]
				i++
			}
		case "--developer-key":
			if i+1 < len(os.Args) {
				developerKeyPath = os.Args[i+1]
				i++
			}
		case "--agent-index":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &agentIndex)
				i++
			}
		}
	}

	if name == "" || agentURL == "" || category == "" {
		fmt.Fprintln(os.Stderr, "Usage: agentdns register --name NAME --agent-url URL --category CATEGORY [--tags TAG1,TAG2] [--summary TEXT] [--developer-key PATH --agent-index N]")
		os.Exit(1)
	}

	tags := []string{}
	if tagsStr != "" {
		tags = strings.Split(tagsStr, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	homeDir, _ := os.UserHomeDir()

	var agentKP *identity.Keypair

	if developerKeyPath != "" {
		// Developer-derived agent registration
		if agentIndex < 0 {
			log.Fatalf("--agent-index is required when using --developer-key")
		}

		devKP, err := identity.LoadKeypair(developerKeyPath)
		if err != nil {
			log.Fatalf("failed to load developer key: %v", err)
		}

		// Derive agent keypair
		agentKP, err = identity.DeriveAgentKeypair(devKP.PrivateKey, uint32(agentIndex))
		if err != nil {
			log.Fatalf("failed to derive agent keypair: %v", err)
		}

		developerID := devKP.DeveloperID()

		// Create derivation proof
		proof := identity.CreateDerivationProof(devKP, agentKP.PublicKey, uint32(agentIndex))

		reqBody := map[string]interface{}{
			"name":         name,
			"agent_url":    agentURL,
			"category":     category,
			"tags":         tags,
			"summary":      summary,
			"public_key":   agentKP.PublicKeyString(),
			"developer_id": developerID,
			"developer_proof": map[string]interface{}{
				"developer_public_key": proof.DeveloperPublicKey,
				"agent_index":          proof.AgentIndex,
				"developer_signature":  proof.DeveloperSignature,
			},
		}

		// Sign with agent key
		signable, _ := json.Marshal(map[string]interface{}{
			"name":       name,
			"agent_url":  agentURL,
			"category":   category,
			"tags":       tags,
			"summary":    summary,
			"public_key": agentKP.PublicKeyString(),
		})
		reqBody["signature"] = agentKP.Sign(signable)

		body, _ := json.Marshal(reqBody)
		resp, err := http.Post("http://localhost:8080/v1/agents", "application/json", strings.NewReader(string(body)))
		if err != nil {
			log.Fatalf("failed to connect to registry: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if resp.StatusCode == http.StatusCreated {
			fmt.Printf("Agent registered successfully (developer: %s, index: %d)!\n", developerID, agentIndex)
			fmt.Printf("  Agent ID: %s\n", result["agent_id"])
		} else {
			fmt.Fprintf(os.Stderr, "Registration failed: %v\n", result["error"])
			os.Exit(1)
		}
		return
	}

	// Standard registration (no developer)
	kp, err := identity.LoadKeypair(filepath.Join(homeDir, ".agentdns", "identity.json"))
	if err != nil {
		log.Fatalf("failed to load identity: %v", err)
	}
	agentKP = kp

	reqBody := map[string]interface{}{
		"name":       name,
		"agent_url":  agentURL,
		"category":   category,
		"tags":       tags,
		"summary":    summary,
		"public_key": agentKP.PublicKeyString(),
	}

	// Sign the registration
	signable, _ := json.Marshal(reqBody)
	reqBody["signature"] = agentKP.Sign(signable)

	body, _ := json.Marshal(reqBody)

	resp, err := http.Post("http://localhost:8080/v1/agents", "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode == http.StatusCreated {
		fmt.Printf("Agent registered successfully!\n")
		fmt.Printf("  Agent ID: %s\n", result["agent_id"])
	} else {
		fmt.Fprintf(os.Stderr, "Registration failed: %v\n", result["error"])
		os.Exit(1)
	}
}

// --- Search Command ---

func cmdSearch() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns search QUERY [--category CAT] [--min-trust SCORE] [--status STATUS] [--max-results N] [--offset N]")
		os.Exit(1)
	}

	query := os.Args[2]
	var category, status string
	maxResults := 20
	offset := 0
	minTrust := 0.0

	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--category":
			if i+1 < len(os.Args) {
				category = os.Args[i+1]
				i++
			}
		case "--min-trust":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%f", &minTrust)
				i++
			}
		case "--status":
			if i+1 < len(os.Args) {
				status = os.Args[i+1]
				i++
			}
		case "--max-results":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &maxResults)
				i++
			}
		case "--offset":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &offset)
				i++
			}
		}
	}

	reqBody := models.SearchRequest{
		Query:         query,
		Category:      category,
		MinTrustScore: minTrust,
		Status:        status,
		MaxResults:    maxResults,
		Offset:        offset,
		Federated:     true,
		Enrich:        false,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://localhost:8080/v1/search", "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	var result models.SearchResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Results) == 0 {
		fmt.Println("No agents found matching your query.")
		return
	}

	fmt.Printf("Found %d agents (showing %d):\n\n", result.TotalFound, len(result.Results))
	for i, r := range result.Results {
		fmt.Printf("  %d. %s (%s)\n", i+1, r.Name, r.AgentID)
		fmt.Printf("     Category: %s | Tags: %s\n", r.Category, strings.Join(r.Tags, ", "))
		fmt.Printf("     Summary:  %s\n", r.Summary)
		fmt.Printf("     Score:    %.3f\n", r.Score)
		if r.ScoreBreakdown != nil {
			fmt.Printf("     Breakdown: text=%.2f sem=%.2f trust=%.2f fresh=%.2f avail=%.2f\n",
				r.ScoreBreakdown.TextRelevance,
				r.ScoreBreakdown.SemanticSimilarity,
				r.ScoreBreakdown.TrustScore,
				r.ScoreBreakdown.Freshness,
				r.ScoreBreakdown.Availability)
		}
		fmt.Println()
	}

	if result.SearchStats != nil {
		fmt.Printf("Search stats: local=%d gossip=%d federated=%d peers=%d latency=%dms\n",
			result.SearchStats.LocalResults,
			result.SearchStats.GossipResults,
			result.SearchStats.FederatedResults,
			result.SearchStats.PeersQueried,
			result.SearchStats.LatencyMs)
	}
}

// --- Resolve Command ---

func cmdResolve() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns resolve AGENT_ID")
		os.Exit(1)
	}

	agentID := os.Args[2]
	resp, err := http.Get("http://localhost:8080/v1/agents/" + agentID)
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintln(os.Stderr, "Agent not found")
		os.Exit(1)
	}

	var agent models.RegistryRecord
	json.NewDecoder(resp.Body).Decode(&agent)

	data, _ := json.MarshalIndent(agent, "", "  ")
	fmt.Println(string(data))
}

// --- Card Command ---

func cmdCard() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns card AGENT_ID")
		os.Exit(1)
	}

	agentID := os.Args[2]
	resp, err := http.Get("http://localhost:8080/v1/agents/" + agentID + "/card")
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		fmt.Fprintf(os.Stderr, "Error: %s\n", errResp["error"])
		os.Exit(1)
	}

	var agentCard models.AgentCard
	json.NewDecoder(resp.Body).Decode(&agentCard)

	data, _ := json.MarshalIndent(agentCard, "", "  ")
	fmt.Println(string(data))
}

// --- Status Command ---

func cmdStatus() {
	resp, err := http.Get("http://localhost:8080/v1/network/status")
	if err != nil {
		log.Fatalf("failed to connect to registry (is it running?): %v", err)
	}
	defer resp.Body.Close()

	var status models.NetworkStatus
	json.NewDecoder(resp.Body).Decode(&status)

	fmt.Printf("Agent DNS Node Status\n")
	fmt.Printf("  Registry ID:    %s\n", status.RegistryID)
	fmt.Printf("  Name:           %s\n", status.Name)
	fmt.Printf("  Version:        %s\n", status.Version)
	fmt.Printf("  Type:           %s\n", status.NodeType)
	fmt.Printf("  Uptime:         %s\n", status.Uptime)
	fmt.Printf("  Peers:          %d\n", status.PeerCount)
	fmt.Printf("  Local agents:   %d\n", status.LocalAgents)
	fmt.Printf("  Gossip entries: %d\n", status.GossipEntries)
	fmt.Printf("  Cached cards:   %d\n", status.CachedCards)
}

// --- Peers Command ---

func cmdPeers() {
	resp, err := http.Get("http://localhost:8080/v1/network/peers")
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	var result map[string][]*models.PeerInfo
	json.NewDecoder(resp.Body).Decode(&result)

	peers := result["peers"]
	if len(peers) == 0 {
		fmt.Println("No connected peers.")
		return
	}

	fmt.Printf("Connected peers (%d):\n\n", len(peers))
	for i, p := range peers {
		fmt.Printf("  %d. %s (%s)\n", i+1, p.Name, p.RegistryID)
		fmt.Printf("     Address:     %s\n", p.Address)
		fmt.Printf("     Agents:      %d\n", p.AgentCount)
		fmt.Printf("     Connected:   %s\n", p.ConnectedAt)
		fmt.Printf("     Last seen:   %s\n", p.LastSeen)
		fmt.Printf("     Latency:     %dms\n", p.Latency)
		fmt.Println()
	}
}

// --- Developer Init Command ---

func cmdDevInit() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}

	devKeyPath := filepath.Join(homeDir, ".agentdns", "developer.json")

	// Check if already initialized
	if _, err := os.Stat(devKeyPath); err == nil {
		fmt.Println("Developer keypair already exists at", devKeyPath)
		fmt.Println("Delete it to re-initialize.")
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(devKeyPath), 0700); err != nil {
		log.Fatalf("failed to create directory: %v", err)
	}

	kp, err := identity.GenerateKeypair()
	if err != nil {
		log.Fatalf("failed to generate developer keypair: %v", err)
	}

	if err := identity.SaveKeypair(kp, devKeyPath); err != nil {
		log.Fatalf("failed to save developer keypair: %v", err)
	}

	fmt.Println("Developer keypair generated!")
	fmt.Printf("  Developer ID: %s\n", kp.DeveloperID())
	fmt.Printf("  Public Key:   %s\n", kp.PublicKeyString())
	fmt.Printf("  Saved to:     %s\n", devKeyPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Register your developer identity:  agentdns dev-register --name \"Your Name\"")
	fmt.Println("  2. Derive an agent keypair:           agentdns derive-agent --index 0")
	fmt.Println("  3. Register an agent:                 agentdns register --name \"Agent\" --agent-url URL --category CAT --developer-key", devKeyPath, "--agent-index 0")
}

// --- Developer Register Command ---

func cmdDevRegister() {
	var name, profileURL, github string

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--name":
			if i+1 < len(os.Args) {
				name = os.Args[i+1]
				i++
			}
		case "--profile-url":
			if i+1 < len(os.Args) {
				profileURL = os.Args[i+1]
				i++
			}
		case "--github":
			if i+1 < len(os.Args) {
				github = os.Args[i+1]
				i++
			}
		}
	}

	if name == "" {
		fmt.Fprintln(os.Stderr, "Usage: agentdns dev-register --name NAME [--profile-url URL] [--github HANDLE]")
		os.Exit(1)
	}

	homeDir, _ := os.UserHomeDir()
	devKeyPath := filepath.Join(homeDir, ".agentdns", "developer.json")

	kp, err := identity.LoadKeypair(devKeyPath)
	if err != nil {
		log.Fatalf("failed to load developer keypair (run 'agentdns dev-init' first): %v", err)
	}

	reqBody := map[string]interface{}{
		"name":        name,
		"public_key":  kp.PublicKeyString(),
		"profile_url": profileURL,
		"github":      github,
	}

	// Sign the registration
	signable, _ := json.Marshal(reqBody)
	reqBody["signature"] = kp.Sign(signable)

	body, _ := json.Marshal(reqBody)

	resp, err := http.Post("http://localhost:8080/v1/developers", "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode == http.StatusCreated {
		fmt.Printf("Developer registered successfully!\n")
		fmt.Printf("  Developer ID: %s\n", result["developer_id"])
	} else {
		fmt.Fprintf(os.Stderr, "Registration failed: %v\n", result["error"])
		os.Exit(1)
	}
}

// --- Derive Agent Command ---

func cmdDeriveAgent() {
	agentIndex := -1
	save := false

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--index":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &agentIndex)
				i++
			}
		case "--save":
			save = true
		}
	}

	if agentIndex < 0 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns derive-agent --index N [--save]")
		fmt.Fprintln(os.Stderr, "  Derives an agent keypair from your developer key at the given index.")
		fmt.Fprintln(os.Stderr, "  --save   Save the derived keypair to ~/.agentdns/agent-N.json")
		os.Exit(1)
	}

	homeDir, _ := os.UserHomeDir()
	devKeyPath := filepath.Join(homeDir, ".agentdns", "developer.json")

	devKP, err := identity.LoadKeypair(devKeyPath)
	if err != nil {
		log.Fatalf("failed to load developer keypair (run 'agentdns dev-init' first): %v", err)
	}

	agentKP, err := identity.DeriveAgentKeypair(devKP.PrivateKey, uint32(agentIndex))
	if err != nil {
		log.Fatalf("failed to derive agent keypair: %v", err)
	}

	fmt.Printf("Agent keypair derived (index %d):\n", agentIndex)
	fmt.Printf("  Developer ID: %s\n", devKP.DeveloperID())
	fmt.Printf("  Agent ID:     %s\n", agentKP.AgentID())
	fmt.Printf("  Public Key:   %s\n", agentKP.PublicKeyString())

	if save {
		agentKeyPath := filepath.Join(homeDir, ".agentdns", fmt.Sprintf("agent-%d.json", agentIndex))
		if err := identity.SaveKeypair(agentKP, agentKeyPath); err != nil {
			log.Fatalf("failed to save agent keypair: %v", err)
		}
		fmt.Printf("  Saved to:     %s\n", agentKeyPath)
	}

	// Create and display derivation proof
	proof := identity.CreateDerivationProof(devKP, agentKP.PublicKey, uint32(agentIndex))
	proofJSON, _ := json.MarshalIndent(proof, "  ", "  ")
	fmt.Printf("  Proof:        %s\n", string(proofJSON))
}

// --- Deregister Command ---

func cmdDeregister() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns deregister AGENT_ID")
		os.Exit(1)
	}

	agentID := os.Args[2]

	// Load keypair for signing the deregister request
	homeDir, _ := os.UserHomeDir()
	kp, err := identity.LoadKeypair(filepath.Join(homeDir, ".agentdns", "identity.json"))
	if err != nil {
		log.Fatalf("failed to load identity: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, "http://localhost:8080/v1/agents/"+agentID, nil)
	if err != nil {
		log.Fatalf("failed to create request: %v", err)
	}

	// Sign the agent ID to prove ownership
	sig := kp.Sign([]byte(agentID))
	req.Header.Set("Authorization", "Bearer "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("failed to connect to registry: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("Agent %s deregistered successfully.\n", agentID)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}
}
