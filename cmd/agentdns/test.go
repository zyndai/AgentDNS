package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentdns/agent-dns/internal/identity"
)

// testEntityEntry holds an entity ID and its private key for deregistration.
type testEntityEntry struct {
	EntityID   string `json:"entity_id"`
	PrivateKey string `json:"private_key"` // base64-encoded Ed25519 private key
}

// testEntityStore persists registered agents (ID + keypair) for deregister.
type testEntityStore struct {
	Agents       []testEntityEntry `json:"agents"`
	RegistryURL  string           `json:"registry_url"`
	RegisteredAt string           `json:"registered_at"`
}

func cmdTest() {
	if len(os.Args) < 3 {
		printTestUsage()
		os.Exit(1)
	}
	switch os.Args[2] {
	case "register":
		cmdTestRegister()
	case "deregister":
		cmdTestDeregister()
	default:
		fmt.Fprintf(os.Stderr, "unknown test subcommand: %s\n", os.Args[2])
		printTestUsage()
		os.Exit(1)
	}
}

func printTestUsage() {
	fmt.Println(`Load Testing Commands:

Usage:
  agentdns test <subcommand> [flags]

Subcommands:
  register    Register N agents for load testing
  deregister  Deregister all agents from previous test run

Flags (register):
  --count N            Number of agents to register (default: 10000)
  --concurrency N      Parallel workers (default: 50)
  --registry-url URL   Registry URL (default: http://localhost:8080)

Flags (deregister):
  --registry-url URL   Registry URL (default: http://localhost:8080)

Examples:
  agentdns test register --count 10000
  agentdns test register --count 5000 --concurrency 100 --registry-url http://my-registry:8080
  agentdns test deregister
  agentdns test deregister --registry-url http://my-registry:8080`)
}

// --- Test Register ---

func cmdTestRegister() {
	count := 10000
	concurrency := 50
	registryURL := "http://localhost:8080"

	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--count":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &count)
				i++
			}
		case "--concurrency":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &concurrency)
				i++
			}
		case "--registry-url":
			if i+1 < len(os.Args) {
				registryURL = os.Args[i+1]
				i++
			}
		}
	}

	fmt.Printf("Load Test: Register\n")
	fmt.Printf("  Registry:    %s\n", registryURL)
	fmt.Printf("  Count:       %d\n", count)
	fmt.Printf("  Concurrency: %d\n\n", concurrency)

	jobs := make(chan int, count)
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)

	var (
		successCount int64
		failCount    int64
		mu           sync.Mutex
		agents       []testEntityEntry
		latencies    []float64
	)

	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				reqStart := time.Now()
				entry, err := registerTestAgent(client, registryURL, i)
				latencyMs := float64(time.Since(reqStart).Milliseconds())

				if err != nil {
					atomic.AddInt64(&failCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
					mu.Lock()
					agents = append(agents, entry)
					latencies = append(latencies, latencyMs)
					mu.Unlock()
				}

				// Print progress every 500
				done := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&failCount)
				if done%500 == 0 {
					elapsed := time.Since(start).Seconds()
					rps := float64(done) / elapsed
					fmt.Printf("\r  Progress: %d/%d  |  RPS: %.0f  |  Failures: %d    ",
						done, count, rps, atomic.LoadInt64(&failCount))
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Printf("\r  Progress: %d/%d  |  Done                              \n\n", count, count)

	// Stats
	sort.Float64s(latencies)
	printLatencyStats(latencies, elapsed, successCount, failCount, count)

	// Persist agents (ID + private key) for deregister
	if len(agents) > 0 {
		if err := saveTestAgents(agents, registryURL); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not save agent data: %v\n", err)
		} else {
			fmt.Printf("  Saved %d agents to ~/.zynd/test-agents.json\n", len(agents))
			fmt.Printf("  Run 'agentdns test deregister' to clean up.\n")
		}
	}
}

// --- Test Deregister ---

func cmdTestDeregister() {
	registryURL := ""

	for i := 3; i < len(os.Args); i++ {
		if os.Args[i] == "--registry-url" && i+1 < len(os.Args) {
			registryURL = os.Args[i+1]
			i++
		}
	}

	store, err := loadTestAgents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no test agents found. Run 'agentdns test register' first.\n")
		os.Exit(1)
	}

	if registryURL == "" {
		registryURL = store.RegistryURL
	}
	if registryURL == "" {
		registryURL = "http://localhost:8080"
	}

	total := len(store.Agents)
	if total == 0 {
		fmt.Println("No agents to deregister.")
		return
	}

	fmt.Printf("Load Test: Deregister\n")
	fmt.Printf("  Registry: %s\n", registryURL)
	fmt.Printf("  Agents:   %d\n\n", total)

	jobs := make(chan testEntityEntry, total)
	for _, a := range store.Agents {
		jobs <- a
	}
	close(jobs)

	var successCount, failCount int64
	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()

	concurrency := 50
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range jobs {
				privBytes, _ := base64.StdEncoding.DecodeString(entry.PrivateKey)
				kp := &identity.Keypair{
					PrivateKey:    ed25519.PrivateKey(privBytes),
					PrivateKeyB64: entry.PrivateKey,
				}
				sig := kp.Sign([]byte(entry.EntityID))

				req, _ := http.NewRequest(http.MethodDelete, registryURL+"/v1/entities/"+entry.EntityID, nil)
				req.Header.Set("Authorization", "Bearer "+sig)
				resp, err := client.Do(req)
				if err != nil || resp.StatusCode != http.StatusOK {
					atomic.AddInt64(&failCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
				if resp != nil {
					resp.Body.Close()
				}

				done := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&failCount)
				if done%500 == 0 {
					elapsed := time.Since(start).Seconds()
					rps := float64(done) / elapsed
					fmt.Printf("\r  Progress: %d/%d  |  RPS: %.0f  |  Failures: %d    ",
						done, total, rps, atomic.LoadInt64(&failCount))
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Printf("\r  Progress: %d/%d  |  Done                              \n\n", total, total)
	fmt.Printf("Results:\n")
	fmt.Printf("  Total:    %d\n", total)
	fmt.Printf("  Success:  %d\n", successCount)
	fmt.Printf("  Failed:   %d\n", failCount)
	fmt.Printf("  Duration: %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  RPS:      %.0f\n", float64(total)/elapsed.Seconds())

	// Remove the store file on success
	if failCount == 0 {
		homeDir, _ := os.UserHomeDir()
		os.Remove(filepath.Join(homeDir, ".zynd", "test-agents.json"))
		fmt.Println("\n  Cleaned up test-agents.json")
	}
}

// --- Helpers ---

var (
	testCategories = []string{"tools", "translation", "code", "search", "data", "ai", "automation", "finance", "security", "devops"}
	testTags       = [][]string{
		{"python", "cli", "scripting"},
		{"nlp", "multilingual", "translation"},
		{"code-review", "linting", "refactor"},
		{"semantic-search", "embeddings", "retrieval"},
		{"etl", "pipeline", "analytics"},
		{"llm", "reasoning", "chat"},
		{"workflow", "scheduling", "triggers"},
		{"trading", "risk", "portfolio"},
		{"vulnerability", "audit", "pentest"},
		{"ci-cd", "docker", "kubernetes"},
	}
	testAdjectives = []string{"Fast", "Reliable", "Smart", "Autonomous", "Efficient", "Powerful", "Scalable", "Intelligent", "Adaptive", "Robust"}
	testNouns      = []string{"Analyzer", "Processor", "Assistant", "Engine", "Bot", "Agent", "Worker", "Resolver", "Optimizer", "Scanner"}
)

func registerTestAgent(client *http.Client, registryURL string, idx int) (testEntityEntry, error) {
	r := rand.New(rand.NewSource(int64(idx)))
	catIdx := r.Intn(len(testCategories))
	category := testCategories[catIdx]
	tags := testTags[catIdx%len(testTags)]
	name := fmt.Sprintf("%s%s-%d", testAdjectives[r.Intn(len(testAdjectives))], testNouns[r.Intn(len(testNouns))], idx)
	agentURL := fmt.Sprintf("https://agents.example.com/agent-%d/.well-known/agent.json", idx)
	summary := fmt.Sprintf("Test agent #%d: a %s %s for %s tasks.", idx, testAdjectives[r.Intn(len(testAdjectives))], testNouns[r.Intn(len(testNouns))], category)

	kp, err := identity.GenerateKeypair()
	if err != nil {
		return testEntityEntry{}, fmt.Errorf("keygen: %w", err)
	}

	payload := map[string]interface{}{
		"name":       name,
		"entity_url":  agentURL,
		"category":   category,
		"tags":       tags,
		"summary":    summary,
		"public_key": kp.PublicKeyString(),
	}
	signable, _ := json.Marshal(payload)
	payload["signature"] = kp.Sign(signable)

	data, _ := json.Marshal(payload)
	resp, err := client.Post(registryURL+"/v1/entities", "application/json", bytes.NewReader(data))
	if err != nil {
		return testEntityEntry{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return testEntityEntry{}, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	id, _ := result["entity_id"].(string)
	if id == "" {
		return testEntityEntry{}, fmt.Errorf("no entity_id in response")
	}
	return testEntityEntry{EntityID: id, PrivateKey: kp.PrivateKeyB64}, nil
}

func printLatencyStats(latencies []float64, elapsed time.Duration, success, fail int64, total int) {
	fmt.Printf("Results:\n")
	fmt.Printf("  Total:    %d\n", total)
	fmt.Printf("  Success:  %d\n", success)
	fmt.Printf("  Failed:   %d\n", fail)
	fmt.Printf("  Duration: %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  RPS:      %.0f\n\n", float64(total)/elapsed.Seconds())

	if len(latencies) == 0 {
		return
	}
	fmt.Printf("Latency (ms):\n")
	fmt.Printf("  P50:  %.0f\n", percentile(latencies, 50))
	fmt.Printf("  P90:  %.0f\n", percentile(latencies, 90))
	fmt.Printf("  P99:  %.0f\n", percentile(latencies, 99))
	fmt.Printf("  Min:  %.0f\n", latencies[0])
	fmt.Printf("  Max:  %.0f\n\n", latencies[len(latencies)-1])
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p / 100)
	return sorted[idx]
}

func saveTestAgents(agents []testEntityEntry, registryURL string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	s := testEntityStore{
		Agents:       agents,
		RegistryURL:  registryURL,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(homeDir, ".zynd", "test-agents.json"), data, 0600)
}


func loadTestAgents() (*testEntityStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".zynd", "test-agents.json"))
	if err != nil {
		return nil, err
	}
	var store testEntityStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return &store, nil
}
