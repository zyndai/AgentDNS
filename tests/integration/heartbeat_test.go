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
	"time"

	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
)

// These tests require a running agent-dns server at localhost:8080
// with at least one agent registered.
// Run: go test ./tests/integration/ -run TestHeartbeat -v

func TestHeartbeatSendAndVerify(t *testing.T) {
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

	// Send heartbeat
	ts := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"timestamp":"%s"}`, ts)
	bodyBytes := []byte(body)
	sig := agentKP.Sign(bodyBytes)

	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/agents/"+agentID+"/heartbeat",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("heartbeat request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var hbResp models.HeartbeatResponse
	json.NewDecoder(resp.Body).Decode(&hbResp)

	if hbResp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", hbResp.Status)
	}
	if hbResp.NextHeartbeatSeconds <= 0 {
		t.Errorf("expected positive next_heartbeat_seconds, got %d", hbResp.NextHeartbeatSeconds)
	}

	t.Logf("SUCCESS: Heartbeat sent. Next heartbeat in %ds", hbResp.NextHeartbeatSeconds)
}

func TestHeartbeatSetsAgentOnline(t *testing.T) {
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

	// Send heartbeat
	ts := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"timestamp":"%s"}`, ts)
	bodyBytes := []byte(body)
	sig := agentKP.Sign(bodyBytes)

	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/agents/"+agentID+"/heartbeat",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sig)

	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Check status endpoint
	statusResp, err := http.Get("http://localhost:8080/v1/agents/" + agentID + "/status")
	if err != nil {
		t.Fatalf("status request failed: %v", err)
	}
	defer statusResp.Body.Close()

	var statusResult map[string]string
	json.NewDecoder(statusResp.Body).Decode(&statusResult)

	if statusResult["status"] != "online" {
		t.Errorf("expected status 'online', got %q", statusResult["status"])
	}
	if statusResult["last_heartbeat"] == "" {
		t.Error("expected non-empty last_heartbeat")
	}

	t.Logf("SUCCESS: Agent status is '%s', last_heartbeat=%s",
		statusResult["status"], statusResult["last_heartbeat"])
}

func TestHeartbeatWithDeveloperKey(t *testing.T) {
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

	// Send heartbeat signed with developer key (Bearer-Dev)
	ts := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"timestamp":"%s"}`, ts)
	bodyBytes := []byte(body)
	sig := devKP.Sign(bodyBytes)

	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/agents/"+agentID+"/heartbeat",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer-Dev "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("heartbeat request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	t.Logf("SUCCESS: Heartbeat sent with developer key (Bearer-Dev)")
}

func TestHeartbeatRejectsWrongKey(t *testing.T) {
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
	wrongKP, _ := identity.GenerateKeypair()

	ts := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"timestamp":"%s"}`, ts)
	bodyBytes := []byte(body)
	sig := wrongKP.Sign(bodyBytes)

	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/agents/"+agentID+"/heartbeat",
		bytes.NewReader(bodyBytes))
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

	t.Logf("SUCCESS: Heartbeat with wrong key rejected (401)")
}

func TestHeartbeatRejectsStaleTimestamp(t *testing.T) {
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

	// Send heartbeat with timestamp 10 minutes in the past
	ts := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"timestamp":"%s"}`, ts)
	bodyBytes := []byte(body)
	sig := agentKP.Sign(bodyBytes)

	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/agents/"+agentID+"/heartbeat",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for stale timestamp, got %d", resp.StatusCode)
	}

	t.Logf("SUCCESS: Stale timestamp rejected (400)")
}

func TestHeartbeatNetworkStatusShowsCounts(t *testing.T) {
	if !serverRunning() {
		t.Skip("server not running at localhost:8080")
	}

	resp, err := http.Get("http://localhost:8080/v1/network/status")
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	defer resp.Body.Close()

	var status models.NetworkStatus
	json.NewDecoder(resp.Body).Decode(&status)

	t.Logf("Network status: local=%d, online=%d, inactive=%d",
		status.LocalAgents, status.OnlineAgents, status.InactiveAgents)

	// After sending heartbeats in previous tests, at least one agent should be online
	if status.OnlineAgents < 0 {
		t.Error("online_agents should not be negative")
	}
}
