package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

func testOnboardingServer(t *testing.T, mode, authURL, webhookSecret string) (*Server, store.Store) {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping onboarding tests")
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	nodeKP, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate node keypair: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Onboarding.Mode = mode
	cfg.Onboarding.AuthURL = authURL
	cfg.Onboarding.WebhookSecret = webhookSecret

	gossipHandler := mesh.NewGossipHandler(st, cfg.Gossip)

	s := &Server{
		cfg:          cfg,
		store:        st,
		nodeIdentity: nodeKP,
		gossip:       gossipHandler,
		eventBus:     events.NewBus(),
	}

	return s, st
}

// --- WebhookAuthMiddleware tests ---

func TestWebhookAuthMiddleware_ValidSecret(t *testing.T) {
	secret := "whsec_testsecret123"
	handler := WebhookAuthMiddleware(secret)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookAuthMiddleware_InvalidSecret(t *testing.T) {
	secret := "whsec_testsecret123"
	handler := WebhookAuthMiddleware(secret)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong_secret")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestWebhookAuthMiddleware_MissingHeader(t *testing.T) {
	secret := "whsec_testsecret123"
	handler := WebhookAuthMiddleware(secret)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestWebhookAuthMiddleware_WrongScheme(t *testing.T) {
	secret := "whsec_testsecret123"
	handler := WebhookAuthMiddleware(secret)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Basic "+secret)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// --- Registry Info tests ---

func TestHandleRegistryInfo_OpenMode(t *testing.T) {
	s, _ := testOnboardingServer(t, "open", "", "")

	req := httptest.NewRequest("GET", "/v1/info", nil)
	w := httptest.NewRecorder()

	s.handleRegistryInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info models.RegistryInfoResponse
	json.NewDecoder(w.Body).Decode(&info)

	if info.DeveloperOnboarding.Mode != "open" {
		t.Fatalf("expected mode 'open', got %q", info.DeveloperOnboarding.Mode)
	}
	if info.DeveloperOnboarding.AuthURL != "" {
		t.Fatalf("expected empty auth_url in open mode, got %q", info.DeveloperOnboarding.AuthURL)
	}
}

func TestHandleRegistryInfo_RestrictedMode(t *testing.T) {
	s, _ := testOnboardingServer(t, "restricted", "https://acme.com/onboard", "whsec_test")

	req := httptest.NewRequest("GET", "/v1/info", nil)
	w := httptest.NewRecorder()

	s.handleRegistryInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info models.RegistryInfoResponse
	json.NewDecoder(w.Body).Decode(&info)

	if info.DeveloperOnboarding.Mode != "restricted" {
		t.Fatalf("expected mode 'restricted', got %q", info.DeveloperOnboarding.Mode)
	}
	if info.DeveloperOnboarding.AuthURL != "https://acme.com/onboard" {
		t.Fatalf("expected auth_url, got %q", info.DeveloperOnboarding.AuthURL)
	}
}

// --- Restricted mode blocks self-registration ---

func TestHandleRegisterDeveloper_RestrictedMode_Returns403(t *testing.T) {
	s, _ := testOnboardingServer(t, "restricted", "https://acme.com/onboard", "whsec_test")

	body := `{"name":"Test Dev","public_key":"ed25519:AAAA","signature":"ed25519:BBBB"}`
	req := httptest.NewRequest("POST", "/v1/developers", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleRegisterDeveloper(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["auth_url"] != "https://acme.com/onboard" {
		t.Fatalf("expected auth_url in response, got %q", resp["auth_url"])
	}
}

// --- Approve developer endpoint ---

func TestHandleApproveDeveloper_Success(t *testing.T) {
	s, st := testOnboardingServer(t, "restricted", "https://acme.com/onboard", "whsec_test")

	body := `{"name":"Alice","state":"random-state-123","callback_port":9999}`
	req := httptest.NewRequest("POST", "/v1/admin/developers/approve", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleApproveDeveloper(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.DeveloperApprovalResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.DeveloperID == "" {
		t.Fatal("expected non-empty developer_id")
	}
	if resp.PrivateKeyEnc == "" {
		t.Fatal("expected non-empty private_key_enc")
	}

	// Verify developer was stored
	dev, err := st.GetDeveloper(resp.DeveloperID)
	if err != nil {
		t.Fatalf("failed to get developer: %v", err)
	}
	if dev == nil {
		t.Fatal("developer should have been stored")
	}
	if dev.Name != "Alice" {
		t.Fatalf("expected name 'Alice', got %q", dev.Name)
	}

	// Verify we can decrypt the private key
	decrypted, err := models.DecryptPrivateKey(resp.PrivateKeyEnc, "random-state-123")
	if err != nil {
		t.Fatalf("failed to decrypt private key: %v", err)
	}
	if decrypted == "" {
		t.Fatal("decrypted private key should not be empty")
	}

	// Cleanup
	st.DeleteDeveloper(resp.DeveloperID)
}

func TestHandleApproveDeveloper_MissingFields(t *testing.T) {
	s, _ := testOnboardingServer(t, "restricted", "https://acme.com/onboard", "whsec_test")

	body := `{"name":"Alice"}`
	req := httptest.NewRequest("POST", "/v1/admin/developers/approve", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleApproveDeveloper(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
