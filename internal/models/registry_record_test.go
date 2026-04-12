package models

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
)

// validBase returns a minimal valid RegistryRecord for agents.
func validBase() RegistryRecord {
	return RegistryRecord{
		AgentID:   "zns:abc123",
		Name:      "test-agent",
		Owner:     "owner-1",
		EntityURL: "https://example.com/.well-known/agent.json",
		Category:  "developer-tools",
		PublicKey: "ed25519:testkey",
		Tags:      []string{"go"},
	}
}

// --- Validate tests --------------------------------------------------------

func TestValidate_ValidAgent(t *testing.T) {
	r := validBase()
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
}

func TestValidate_RequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*RegistryRecord)
		errMsg string
	}{
		{"missing agent_id", func(r *RegistryRecord) { r.AgentID = "" }, "agent_id is required"},
		{"missing name", func(r *RegistryRecord) { r.Name = "" }, "name is required"},
		{"missing owner", func(r *RegistryRecord) { r.Owner = "" }, "owner is required"},
		{"missing entity_url for agent", func(r *RegistryRecord) { r.EntityURL = "" }, "entity_url is required"},
		{"missing category", func(r *RegistryRecord) { r.Category = "" }, "category is required"},
		{"missing public_key", func(r *RegistryRecord) { r.PublicKey = "" }, "public_key is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validBase()
			tt.modify(&r)
			err := r.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}

func TestValidate_FieldLimits(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*RegistryRecord)
		errMsg string
	}{
		{
			"name too long",
			func(r *RegistryRecord) { r.Name = strings.Repeat("a", 101) },
			"name must be 100 characters",
		},
		{
			"name at limit passes",
			func(r *RegistryRecord) { r.Name = strings.Repeat("a", 100) },
			"",
		},
		{
			"summary too long",
			func(r *RegistryRecord) { r.Summary = strings.Repeat("x", 201) },
			"summary must be 200 characters",
		},
		{
			"summary at limit passes",
			func(r *RegistryRecord) { r.Summary = strings.Repeat("x", 200) },
			"",
		},
		{
			"too many tags",
			func(r *RegistryRecord) {
				r.Tags = make([]string, 21)
				for i := range r.Tags {
					r.Tags[i] = "tag"
				}
			},
			"maximum 20 tags",
		},
		{
			"20 tags passes",
			func(r *RegistryRecord) {
				r.Tags = make([]string, 20)
				for i := range r.Tags {
					r.Tags[i] = "tag"
				}
			},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validBase()
			tt.modify(&r)
			err := r.Validate()
			if tt.errMsg == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

// --- EntityType validation -------------------------------------------------

func TestValidate_EntityType(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		wantErr    bool
		errMsg     string
	}{
		{"empty defaults to agent (valid)", "", false, ""},
		{"agent is valid", "agent", false, ""},
		{"service needs endpoint", "service", true, "service_endpoint is required"},
		{"unknown type rejected", "bot", true, "invalid entity_type"},
		{"daemon type rejected", "daemon", true, "invalid entity_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validBase()
			r.EntityType = tt.entityType
			err := r.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_ServiceEntityType(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*RegistryRecord)
		wantErr bool
		errMsg  string
	}{
		{
			"valid service with endpoint",
			func(r *RegistryRecord) {
				r.EntityType = "service"
				r.ServiceEndpoint = "https://api.example.com/v1"
			},
			false, "",
		},
		{
			"service missing endpoint",
			func(r *RegistryRecord) {
				r.EntityType = "service"
			},
			true, "service_endpoint is required",
		},
		{
			"service with valid pricing",
			func(r *RegistryRecord) {
				r.EntityType = "service"
				r.ServiceEndpoint = "https://api.example.com/v1"
				r.EntityPricing = &EntityPricing{Model: "per_request", BasePriceUSD: 0.01}
			},
			false, "",
		},
		{
			"service with invalid pricing model",
			func(r *RegistryRecord) {
				r.EntityType = "service"
				r.ServiceEndpoint = "https://api.example.com/v1"
				r.EntityPricing = &EntityPricing{Model: "metered"}
			},
			true, "invalid pricing model",
		},
		{
			"service entity_url not required",
			func(r *RegistryRecord) {
				r.EntityType = "service"
				r.EntityURL = "" // services don't need entity_url
				r.ServiceEndpoint = "https://api.example.com/v1"
			},
			false, "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validBase()
			tt.modify(&r)
			err := r.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// --- EntityPricing on agents (not just services) ---------------------------

func TestValidate_AgentWithPricing(t *testing.T) {
	r := validBase()
	r.EntityType = "agent"
	r.EntityPricing = &EntityPricing{
		Model:        "per_request",
		BasePriceUSD: 0.05,
		Currency:     "USD",
	}
	if err := r.Validate(); err != nil {
		t.Errorf("agents should accept entity_pricing, got: %v", err)
	}
}

func TestValidate_AgentWithFreePricing(t *testing.T) {
	r := validBase()
	r.EntityPricing = &EntityPricing{Model: "free"}
	if err := r.Validate(); err != nil {
		t.Errorf("agents with free pricing should validate, got: %v", err)
	}
}

// --- JSON serialization ---------------------------------------------------

func TestRegistryRecord_JSONRoundTrip(t *testing.T) {
	r := validBase()
	r.EntityType = "service"
	r.ServiceEndpoint = "https://api.example.com/v1"
	r.OpenAPIURL = "https://api.example.com/openapi.json"
	r.EntityPricing = &EntityPricing{
		Model:          "per_request",
		BasePriceUSD:   0.01,
		Currency:       "USD",
		PaymentMethods: []string{"x402"},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got RegistryRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.EntityType != "service" {
		t.Errorf("EntityType: got %q, want %q", got.EntityType, "service")
	}
	if got.ServiceEndpoint != r.ServiceEndpoint {
		t.Errorf("ServiceEndpoint: got %q, want %q", got.ServiceEndpoint, r.ServiceEndpoint)
	}
	if got.OpenAPIURL != r.OpenAPIURL {
		t.Errorf("OpenAPIURL: got %q, want %q", got.OpenAPIURL, r.OpenAPIURL)
	}
	if got.EntityPricing == nil {
		t.Fatal("EntityPricing was nil after round-trip")
	}
	if got.EntityPricing.Model != "per_request" {
		t.Errorf("EntityPricing.Model: got %q, want %q", got.EntityPricing.Model, "per_request")
	}
	if got.EntityPricing.BasePriceUSD != 0.01 {
		t.Errorf("EntityPricing.BasePriceUSD: got %f, want 0.01", got.EntityPricing.BasePriceUSD)
	}
}

func TestRegistryRecord_JSONFieldNames(t *testing.T) {
	r := validBase()
	r.EntityType = "service"
	r.ServiceEndpoint = "https://api.example.com"
	r.OpenAPIURL = "https://api.example.com/openapi.json"
	r.EntityPricing = &EntityPricing{Model: "free"}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}

	// Verify snake_case JSON keys
	requiredKeys := map[string]bool{
		"entity_type":      true,
		"service_endpoint": true,
		"openapi_url":      true,
		"entity_pricing":   true,
		"entity_url":       true,
	}
	for key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q not found in output", key)
		}
	}

	// Should NOT have old naming
	badKeys := []string{"service_pricing", "agent_url", "pricing_model", "type"}
	for _, key := range badKeys {
		if _, ok := raw[key]; ok {
			t.Errorf("unexpected legacy key %q in JSON output", key)
		}
	}
}

func TestRegistryRecord_NilEntityPricingOmitted(t *testing.T) {
	r := validBase()
	r.EntityPricing = nil

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if strings.Contains(string(data), "entity_pricing") {
		t.Error("entity_pricing should be omitted when nil")
	}
}

// --- ID generation ---------------------------------------------------------

func TestGenerateServiceID(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	id := GenerateServiceID(pub)
	if !strings.HasPrefix(id, "zns:svc:") {
		t.Errorf("service ID should start with 'zns:svc:', got %q", id)
	}
	if len(id) != len("zns:svc:")+32 { // 16 bytes = 32 hex chars
		t.Errorf("unexpected ID length: %d", len(id))
	}
}

func TestGenerateAgentID(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	id := GenerateAgentID(pub)
	if !strings.HasPrefix(id, "zns:") {
		t.Errorf("agent ID should start with 'zns:', got %q", id)
	}
	// Should NOT have svc: prefix
	if strings.HasPrefix(id, "zns:svc:") {
		t.Error("agent ID should not have svc: prefix")
	}
}

func TestGenerateServiceID_Deterministic(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	id1 := GenerateServiceID(pub)
	id2 := GenerateServiceID(pub)
	if id1 != id2 {
		t.Errorf("service ID not deterministic: %q != %q", id1, id2)
	}
}

func TestGenerateServiceID_DifferentFromAgentID(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	agentID := GenerateAgentID(pub)
	serviceID := GenerateServiceID(pub)
	if agentID == serviceID {
		t.Error("agent ID and service ID should differ for the same key")
	}
}

// --- SearchRequest EntityType -----------------------------------------------

func TestSearchRequest_EntityTypeJSON(t *testing.T) {
	req := SearchRequest{
		Query:      "translation",
		EntityType: "service",
		MaxResults: 10,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if v, ok := raw["entity_type"]; !ok || v != "service" {
		t.Errorf("expected entity_type=service in JSON, got %v", v)
	}

	// Should NOT have old "type" key
	if _, ok := raw["type"]; ok {
		t.Error("search request should not have legacy 'type' field")
	}
}

// --- GossipAnnouncement entity fields --------------------------------------

func TestGossipAnnouncement_EntityFields(t *testing.T) {
	ann := GossipAnnouncement{
		Type:            "agent_announce",
		AgentID:         "zns:svc:abc",
		Name:            "test-service",
		HomeRegistry:    "zns:registry:xyz",
		Action:          "register",
		Timestamp:       NowRFC3339(),
		EntityType:      "service",
		ServiceEndpoint: "https://api.example.com/v1",
		OpenAPIURL:      "https://api.example.com/openapi.json",
		EntityPricing: &EntityPricing{
			Model:        "per_request",
			BasePriceUSD: 0.01,
		},
	}

	data, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got GossipAnnouncement
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.EntityType != "service" {
		t.Errorf("EntityType: got %q, want %q", got.EntityType, "service")
	}
	if got.ServiceEndpoint != ann.ServiceEndpoint {
		t.Errorf("ServiceEndpoint mismatch")
	}
	if got.EntityPricing == nil {
		t.Fatal("EntityPricing was nil after round-trip")
	}
	if got.EntityPricing.Model != "per_request" {
		t.Errorf("EntityPricing.Model: got %q, want %q", got.EntityPricing.Model, "per_request")
	}
}

// --- GossipEntry entity fields ---------------------------------------------

func TestGossipEntry_EntityFields(t *testing.T) {
	entry := GossipEntry{
		AgentID:         "zns:svc:abc",
		Name:            "test-service",
		Category:        "translation",
		HomeRegistry:    "zns:registry:xyz",
		EntityType:      "service",
		ServiceEndpoint: "https://api.example.com/v1",
		OpenAPIURL:      "https://api.example.com/openapi.json",
		EntityPricing: &EntityPricing{
			Model:        "subscription",
			BasePriceUSD: 29.99,
			Currency:     "USDC",
		},
		Status: "active",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got GossipEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.EntityType != "service" {
		t.Errorf("EntityType: got %q, want %q", got.EntityType, "service")
	}
	if got.ServiceEndpoint != entry.ServiceEndpoint {
		t.Errorf("ServiceEndpoint mismatch")
	}
	if got.EntityPricing == nil {
		t.Fatal("EntityPricing nil after round-trip")
	}
	if got.EntityPricing.BasePriceUSD != 29.99 {
		t.Errorf("BasePriceUSD: got %f, want 29.99", got.EntityPricing.BasePriceUSD)
	}
}
