package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
)

// These tests require a running agent-dns server at localhost:8080
// with developer and agents already registered via the CLI e2e tests.
// Run: go test ./tests/integration/ -run TestDeveloper -v

func serverRunning() bool {
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func TestDeveloperUpdateAgentWithAgentKey(t *testing.T) {
	if !serverRunning() {
		t.Skip("server not running at localhost:8080")
	}

	homeDir, _ := os.UserHomeDir()
	agentKeyPath := filepath.Join(homeDir, ".agentdns", "agent-0.json")

	agentKP, err := identity.LoadKeypair(agentKeyPath)
	if err != nil {
		t.Skipf("agent-0 key not found (run dev-init + derive-agent first): %v", err)
	}

	agentID := agentKP.AgentID()

	// Create update body
	newSummary := "Updated via AGENT key test"
	body := fmt.Sprintf(`{"summary":"%s","signature":"dummy"}`, newSummary)
	bodyBytes := []byte(body)

	// Sign with agent key
	sig := agentKP.Sign(bodyBytes)

	req, _ := http.NewRequest("PUT", "http://localhost:8080/v1/agents/"+agentID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var updated models.RegistryRecord
	json.NewDecoder(resp.Body).Decode(&updated)

	if updated.Summary != newSummary {
		t.Errorf("expected summary=%q, got %q", newSummary, updated.Summary)
	}

	t.Logf("SUCCESS: Agent updated with agent key. Summary: %s", updated.Summary)
}

func TestDeveloperUpdateAgentWithDeveloperKey(t *testing.T) {
	if !serverRunning() {
		t.Skip("server not running at localhost:8080")
	}

	homeDir, _ := os.UserHomeDir()
	devKeyPath := filepath.Join(homeDir, ".agentdns", "developer.json")
	agentKeyPath := filepath.Join(homeDir, ".agentdns", "agent-0.json")

	devKP, err := identity.LoadKeypair(devKeyPath)
	if err != nil {
		t.Skipf("developer key not found: %v", err)
	}

	agentKP, err := identity.LoadKeypair(agentKeyPath)
	if err != nil {
		t.Skipf("agent-0 key not found: %v", err)
	}

	agentID := agentKP.AgentID()

	// Create update body
	newSummary := "Updated via DEVELOPER key test"
	body := fmt.Sprintf(`{"summary":"%s","signature":"dummy"}`, newSummary)
	bodyBytes := []byte(body)

	// Sign with DEVELOPER key (using Bearer-Dev scheme)
	sig := devKP.Sign(bodyBytes)

	req, _ := http.NewRequest("PUT", "http://localhost:8080/v1/agents/"+agentID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer-Dev "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var updated models.RegistryRecord
	json.NewDecoder(resp.Body).Decode(&updated)

	if updated.Summary != newSummary {
		t.Errorf("expected summary=%q, got %q", newSummary, updated.Summary)
	}

	t.Logf("SUCCESS: Agent updated with developer key (Bearer-Dev). Summary: %s", updated.Summary)
}

func TestDeveloperUpdateAgentWithWrongKey(t *testing.T) {
	if !serverRunning() {
		t.Skip("server not running at localhost:8080")
	}

	homeDir, _ := os.UserHomeDir()
	agentKeyPath := filepath.Join(homeDir, ".agentdns", "agent-0.json")

	agentKP, err := identity.LoadKeypair(agentKeyPath)
	if err != nil {
		t.Skipf("agent-0 key not found: %v", err)
	}

	agentID := agentKP.AgentID()

	// Generate a random key -- should NOT be authorized
	wrongKP, _ := identity.GenerateKeypair()

	body := `{"summary":"Should not work","signature":"dummy"}`
	bodyBytes := []byte(body)
	sig := wrongKP.Sign(bodyBytes)

	req, _ := http.NewRequest("PUT", "http://localhost:8080/v1/agents/"+agentID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	t.Logf("SUCCESS: Update with wrong key correctly rejected (401)")
}

func TestDeveloperUpdateAgentWithWrongDevKey(t *testing.T) {
	if !serverRunning() {
		t.Skip("server not running at localhost:8080")
	}

	homeDir, _ := os.UserHomeDir()
	agentKeyPath := filepath.Join(homeDir, ".agentdns", "agent-0.json")

	agentKP, err := identity.LoadKeypair(agentKeyPath)
	if err != nil {
		t.Skipf("agent-0 key not found: %v", err)
	}

	agentID := agentKP.AgentID()

	// Generate a random key and try using it as a developer key
	wrongDevKP, _ := identity.GenerateKeypair()

	body := `{"summary":"Should not work with wrong dev key","signature":"dummy"}`
	bodyBytes := []byte(body)
	sig := wrongDevKP.Sign(bodyBytes)

	req, _ := http.NewRequest("PUT", "http://localhost:8080/v1/agents/"+agentID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer-Dev "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	t.Logf("SUCCESS: Update with wrong developer key correctly rejected (401)")
}

func TestDeveloperKeyDerivationDeterministic(t *testing.T) {
	if !serverRunning() {
		t.Skip("server not running at localhost:8080")
	}

	homeDir, _ := os.UserHomeDir()
	devKeyPath := filepath.Join(homeDir, ".agentdns", "developer.json")

	devKP, err := identity.LoadKeypair(devKeyPath)
	if err != nil {
		t.Skipf("developer key not found: %v", err)
	}

	// Derive agent at index 0 -- should match the stored agent-0.json
	derivedKP, err := identity.DeriveAgentKeypair(devKP.PrivateKey, 0)
	if err != nil {
		t.Fatalf("failed to derive: %v", err)
	}

	agentKeyPath := filepath.Join(homeDir, ".agentdns", "agent-0.json")
	storedKP, err := identity.LoadKeypair(agentKeyPath)
	if err != nil {
		t.Skipf("agent-0 key not found: %v", err)
	}

	if derivedKP.PublicKeyB64 != storedKP.PublicKeyB64 {
		t.Errorf("re-derived key doesn't match stored key")
		t.Errorf("  derived: %s", derivedKP.PublicKeyB64)
		t.Errorf("  stored:  %s", storedKP.PublicKeyB64)
	} else {
		t.Logf("SUCCESS: Re-derived key matches stored agent-0 key")
	}

	// Verify the agent in the registry matches
	resp, err := http.Get("http://localhost:8080/v1/agents/" + derivedKP.AgentID())
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("agent not found in registry: %d", resp.StatusCode)
	}

	var agent models.RegistryRecord
	json.NewDecoder(resp.Body).Decode(&agent)

	if agent.PublicKey != derivedKP.PublicKeyString() {
		t.Errorf("registry agent pubkey doesn't match derived key")
	}
	if agent.DeveloperID != devKP.DeveloperID() {
		t.Errorf("registry agent developer_id=%q, expected %q", agent.DeveloperID, devKP.DeveloperID())
	}

	t.Logf("SUCCESS: Registry agent matches re-derived key, developer_id=%s", agent.DeveloperID)
}
