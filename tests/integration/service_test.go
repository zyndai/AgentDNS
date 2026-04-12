package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
)

// signRegistration produces an Ed25519 signature over the canonical registration
// fields that handleRegisterAgent expects: {name, agent_url, category, tags, summary, public_key}.
func signRegistration(kp *identity.Keypair, name, agentURL, category string, tags []string, summary string) string {
	signable, _ := json.Marshal(map[string]interface{}{
		"name":       name,
		"agent_url":  agentURL,
		"category":   category,
		"tags":       tags,
		"summary":    summary,
		"public_key": kp.PublicKeyString(),
	})
	return kp.Sign(signable)
}

// registerAgent is a test helper that POSTs a registration request and returns
// the response recorder. Callers assert on status/body as needed.
func registerAgent(t *testing.T, handler http.Handler, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal registration body: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/agents", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// getAgent GETs /v1/agents/{id} and returns the response recorder.
func getAgent(t *testing.T, handler http.Handler, agentID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/agents/"+agentID, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// searchAgents POSTs a search request and returns the decoded SearchResponse.
func searchAgents(t *testing.T, handler http.Handler, searchReq models.SearchRequest) (*httptest.ResponseRecorder, *models.SearchResponse) {
	t.Helper()
	data, _ := json.Marshal(searchReq)
	req := httptest.NewRequest("POST", "/v1/search", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		return w, nil
	}

	var resp models.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}
	return w, &resp
}

// extractAgentID parses the agent_id from a 201 registration response.
func extractAgentID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode registration response: %v", err)
	}
	id, ok := resp["agent_id"]
	if !ok || id == "" {
		t.Fatal("registration response missing agent_id")
	}
	return id
}

func TestServiceRegistration(t *testing.T) {
	server, _, _ := setupTestServer(t)
	handler := server.Handler()

	kp, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	name := fmt.Sprintf("TestSvc-%s", t.Name())
	endpoint := "https://api.example.com/v1"
	openapiURL := "https://api.example.com/openapi.json"

	sig := signRegistration(kp, name, endpoint, "api-services", []string{"rest", "data"}, "A test service")

	body := map[string]interface{}{
		"name":             name,
		"agent_url":        endpoint,
		"category":         "api-services",
		"tags":             []string{"rest", "data"},
		"summary":          "A test service",
		"public_key":       kp.PublicKeyString(),
		"signature":        sig,
		"entity_type":      "service",
		"service_endpoint": endpoint,
		"openapi_url":      openapiURL,
		"entity_pricing": map[string]interface{}{
			"model":           "per_request",
			"base_price_usd":  0.01,
			"currency":        "USD",
			"payment_methods": []string{"x402"},
		},
	}

	w := registerAgent(t, handler, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	agentID := extractAgentID(t, w)

	// GET and verify service fields are persisted
	w = getAgent(t, handler, agentID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var record models.RegistryRecord
	if err := json.NewDecoder(w.Body).Decode(&record); err != nil {
		t.Fatalf("failed to decode agent: %v", err)
	}

	if record.EntityType != "service" {
		t.Errorf("expected entity_type 'service', got %q", record.EntityType)
	}
	if record.ServiceEndpoint != endpoint {
		t.Errorf("expected service_endpoint %q, got %q", endpoint, record.ServiceEndpoint)
	}
	if record.OpenAPIURL != openapiURL {
		t.Errorf("expected openapi_url %q, got %q", openapiURL, record.OpenAPIURL)
	}
	if record.EntityPricing == nil {
		t.Fatal("expected entity_pricing to be set")
	}
	if record.EntityPricing.Model != "per_request" {
		t.Errorf("expected pricing model 'per_request', got %q", record.EntityPricing.Model)
	}
	if record.EntityPricing.BasePriceUSD != 0.01 {
		t.Errorf("expected base_price_usd 0.01, got %f", record.EntityPricing.BasePriceUSD)
	}
}

func TestServiceRegistrationValidation(t *testing.T) {
	server, _, _ := setupTestServer(t)
	handler := server.Handler()

	kp, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	// entity_type="service" without service_endpoint should fail validation.
	// For services, the handler defaults agent_url to service_endpoint when agent_url is empty,
	// so we must provide agent_url to pass the required-fields check and reach Validate().
	name := fmt.Sprintf("TestSvc-%s", t.Name())
	sig := signRegistration(kp, name, "https://placeholder.example.com", "api-services", []string{}, "Missing endpoint")

	body := map[string]interface{}{
		"name":        name,
		"agent_url":   "https://placeholder.example.com",
		"category":    "api-services",
		"tags":        []string{},
		"summary":     "Missing endpoint",
		"public_key":  kp.PublicKeyString(),
		"signature":   sig,
		"entity_type": "service",
		// service_endpoint intentionally omitted
	}

	w := registerAgent(t, handler, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for service without service_endpoint, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSearchByEntityType(t *testing.T) {
	server, _, _ := setupTestServer(t)
	handler := server.Handler()

	// Register an agent (default entity_type)
	agentKP, _ := identity.GenerateKeypair()
	agentName := fmt.Sprintf("SearchAgent-%s", t.Name())
	agentSig := signRegistration(agentKP, agentName, "https://agent.example.com/.well-known/agent.json", "developer-tools", []string{"search-test"}, "search test agent")
	w := registerAgent(t, handler, map[string]interface{}{
		"name":       agentName,
		"agent_url":  "https://agent.example.com/.well-known/agent.json",
		"category":   "developer-tools",
		"tags":       []string{"search-test"},
		"summary":    "search test agent",
		"public_key": agentKP.PublicKeyString(),
		"signature":  agentSig,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to register agent: %d: %s", w.Code, w.Body.String())
	}

	// Register a service
	svcKP, _ := identity.GenerateKeypair()
	svcName := fmt.Sprintf("SearchService-%s", t.Name())
	svcEndpoint := "https://service.example.com/api"
	svcSig := signRegistration(svcKP, svcName, svcEndpoint, "developer-tools", []string{"search-test"}, "search test service")
	w = registerAgent(t, handler, map[string]interface{}{
		"name":             svcName,
		"agent_url":        svcEndpoint,
		"category":         "developer-tools",
		"tags":             []string{"search-test"},
		"summary":          "search test service",
		"public_key":       svcKP.PublicKeyString(),
		"signature":        svcSig,
		"entity_type":      "service",
		"service_endpoint": svcEndpoint,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to register service: %d: %s", w.Code, w.Body.String())
	}

	// Search with entity_type="service" -- should only return the service
	_, resp := searchAgents(t, handler, models.SearchRequest{
		Query:      "search test",
		EntityType: "service",
		MaxResults: 50,
	})
	if resp == nil {
		t.Fatal("search for services returned nil response")
	}
	for _, r := range resp.Results {
		if r.EntityType != "service" {
			t.Errorf("expected only services, got entity_type=%q for %s", r.EntityType, r.Name)
		}
	}
	foundService := false
	for _, r := range resp.Results {
		if r.Name == svcName {
			foundService = true
			break
		}
	}
	if !foundService {
		t.Errorf("service %q not found in service-filtered search results", svcName)
	}

	// Search with entity_type="agent" -- should only return the agent
	_, resp = searchAgents(t, handler, models.SearchRequest{
		Query:      "search test",
		EntityType: "agent",
		MaxResults: 50,
	})
	if resp == nil {
		t.Fatal("search for agents returned nil response")
	}
	for _, r := range resp.Results {
		if r.EntityType != "agent" && r.EntityType != "" {
			t.Errorf("expected only agents, got entity_type=%q for %s", r.EntityType, r.Name)
		}
	}
	foundAgent := false
	for _, r := range resp.Results {
		if r.Name == agentName {
			foundAgent = true
			break
		}
	}
	if !foundAgent {
		t.Errorf("agent %q not found in agent-filtered search results", agentName)
	}

	// Search without entity_type filter -- should return both
	_, resp = searchAgents(t, handler, models.SearchRequest{
		Query:      "search test",
		MaxResults: 50,
	})
	if resp == nil {
		t.Fatal("unfiltered search returned nil response")
	}
	hasAgent, hasSvc := false, false
	for _, r := range resp.Results {
		if r.Name == agentName {
			hasAgent = true
		}
		if r.Name == svcName {
			hasSvc = true
		}
	}
	if !hasAgent {
		t.Errorf("agent %q missing from unfiltered search", agentName)
	}
	if !hasSvc {
		t.Errorf("service %q missing from unfiltered search", svcName)
	}
}

func TestDefaultEntityType(t *testing.T) {
	server, _, _ := setupTestServer(t)
	handler := server.Handler()

	kp, _ := identity.GenerateKeypair()
	name := fmt.Sprintf("DefaultEntity-%s", t.Name())
	sig := signRegistration(kp, name, "https://default.example.com/.well-known/agent.json", "tools", []string{}, "default entity test")

	w := registerAgent(t, handler, map[string]interface{}{
		"name":       name,
		"agent_url":  "https://default.example.com/.well-known/agent.json",
		"category":   "tools",
		"tags":       []string{},
		"summary":    "default entity test",
		"public_key": kp.PublicKeyString(),
		"signature":  sig,
		// entity_type intentionally omitted
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	agentID := extractAgentID(t, w)

	w = getAgent(t, handler, agentID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var record models.RegistryRecord
	json.NewDecoder(w.Body).Decode(&record)

	if record.EntityType != "agent" {
		t.Errorf("expected entity_type 'agent' by default, got %q", record.EntityType)
	}
}

func TestServiceRouteAlias(t *testing.T) {
	server, _, _ := setupTestServer(t)
	handler := server.Handler()

	kp, _ := identity.GenerateKeypair()
	name := fmt.Sprintf("AliasService-%s", t.Name())
	endpoint := "https://alias.example.com/api"
	sig := signRegistration(kp, name, endpoint, "api-services", []string{"alias"}, "alias route test")

	body := map[string]interface{}{
		"name":             name,
		"agent_url":        endpoint,
		"category":         "api-services",
		"tags":             []string{"alias"},
		"summary":          "alias route test",
		"public_key":       kp.PublicKeyString(),
		"signature":        sig,
		"entity_type":      "service",
		"service_endpoint": endpoint,
	}

	data, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/services", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on /v1/services alias, got %d: %s", w.Code, w.Body.String())
	}
}
