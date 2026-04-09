package store

import (
	"strings"
	"testing"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

// newZNSTestStore creates a store and cleans ZNS tables.
func newZNSTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	s := newTestStore(t).(*PostgresStore)

	// Clean ZNS tables (order matters for foreign keys)
	s.pool.Exec(t.Context(), "DELETE FROM zns_versions")
	s.pool.Exec(t.Context(), "DELETE FROM zns_names")
	s.pool.Exec(t.Context(), "DELETE FROM gossip_zns_names")
	s.pool.Exec(t.Context(), "DELETE FROM peer_attestations")
	s.pool.Exec(t.Context(), "DELETE FROM registry_identity_proofs")
	s.pool.Exec(t.Context(), "DELETE FROM developers")
	s.pool.Exec(t.Context(), "DELETE FROM gossip_developers")

	return s
}

func createTestDeveloper(t *testing.T, s *PostgresStore, id, handle string) *models.DeveloperRecord {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	dev := &models.DeveloperRecord{
		DeveloperID:   id,
		Name:          "Test Dev",
		PublicKey:     "ed25519:testpubkey-" + id,
		HomeRegistry:  "zns:registry:test",
		SchemaVersion: "1.0",
		RegisteredAt:  now,
		UpdatedAt:     now,
		Signature:     "ed25519:testsig",
		DevHandle:     handle,
	}
	if err := s.CreateDeveloper(dev); err != nil {
		t.Fatalf("failed to create test developer: %v", err)
	}
	return dev
}

func createTestAgent(t *testing.T, s *PostgresStore, agentID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	agent := &models.RegistryRecord{
		AgentID:      agentID,
		Name:         "TestAgent",
		Owner:        "did:key:testowner",
		AgentURL:     "https://example.com/.well-known/agent.json",
		Category:     "tools",
		Tags:         []string{"test"},
		Summary:      "test",
		PublicKey:     "ed25519:testpubkey-" + agentID,
		HomeRegistry: "zns:registry:test",
		RegisteredAt: now,
		UpdatedAt:    now,
		TTL:          86400,
		Signature:    "ed25519:testsig",
	}
	if err := s.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}
}

// --- Handle Tests ---

func TestStore_ClaimHandle(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:claim1", "")

	if err := s.ClaimHandle("zns:dev:claim1", "acme-corp", "zns:registry:test"); err != nil {
		t.Fatalf("ClaimHandle() error: %v", err)
	}

	dev, err := s.GetDeveloper("zns:dev:claim1")
	if err != nil {
		t.Fatalf("GetDeveloper() error: %v", err)
	}
	if dev.DevHandle != "acme-corp" {
		t.Errorf("expected handle 'acme-corp', got %q", dev.DevHandle)
	}
}

func TestStore_ClaimHandle_Duplicate(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:dup1", "taken-handle")
	createTestDeveloper(t, s, "zns:dev:dup2", "")

	err := s.ClaimHandle("zns:dev:dup2", "taken-handle", "zns:registry:test")
	if err == nil {
		t.Fatal("expected error for duplicate handle, got nil")
	}
	if !strings.Contains(err.Error(), "taken") && !strings.Contains(err.Error(), "already") {
		t.Errorf("expected 'taken' error, got: %v", err)
	}
}

func TestStore_ClaimHandle_AlreadyHasOne(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:has1", "existing-handle")

	err := s.ClaimHandle("zns:dev:has1", "new-handle", "zns:registry:test")
	if err == nil {
		t.Fatal("expected error when developer already has a handle")
	}
}

func TestStore_GetDeveloperByHandle(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:byhandle", "find-me")

	dev, err := s.GetDeveloperByHandle("find-me", "zns:registry:test")
	if err != nil {
		t.Fatalf("GetDeveloperByHandle() error: %v", err)
	}
	if dev == nil {
		t.Fatal("expected developer, got nil")
	}
	if dev.DeveloperID != "zns:dev:byhandle" {
		t.Errorf("expected dev ID 'zns:dev:byhandle', got %q", dev.DeveloperID)
	}

	// Nonexistent handle
	dev2, _ := s.GetDeveloperByHandle("no-such-handle", "zns:registry:test")
	if dev2 != nil {
		t.Error("expected nil for nonexistent handle")
	}
}

func TestStore_ReleaseHandle(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:release1", "release-me")

	if err := s.ReleaseHandle("zns:dev:release1", "release-me"); err != nil {
		t.Fatalf("ReleaseHandle() error: %v", err)
	}

	dev, _ := s.GetDeveloperByHandle("release-me", "zns:registry:test")
	if dev != nil {
		t.Error("expected nil after release")
	}

	// Developer still exists
	devByID, _ := s.GetDeveloper("zns:dev:release1")
	if devByID == nil {
		t.Error("developer should still exist after handle release")
	}
}

func TestStore_UpdateHandleVerification(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:verify1", "verify-me")

	if err := s.UpdateHandleVerification("zns:dev:verify1", true, "dns", "example.com"); err != nil {
		t.Fatalf("UpdateHandleVerification() error: %v", err)
	}

	dev, _ := s.GetDeveloper("zns:dev:verify1")
	if !dev.DevHandleVerified {
		t.Error("expected verified=true")
	}
	if dev.VerificationMethod != "dns" {
		t.Errorf("expected method 'dns', got %q", dev.VerificationMethod)
	}
	if dev.VerificationProof != "example.com" {
		t.Errorf("expected proof 'example.com', got %q", dev.VerificationProof)
	}
}

func TestStore_CreateDeveloper_WithHandle(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	dev := &models.DeveloperRecord{
		DeveloperID:   "zns:dev:atomic1",
		Name:          "Atomic Dev",
		PublicKey:     "ed25519:atomic-key",
		HomeRegistry:  "zns:registry:test",
		SchemaVersion: "1.0",
		RegisteredAt:  now,
		UpdatedAt:     now,
		Signature:     "ed25519:sig",
		DevHandle:     "atomic-handle",
	}

	if err := s.CreateDeveloper(dev); err != nil {
		t.Fatalf("CreateDeveloper with handle error: %v", err)
	}

	got, _ := s.GetDeveloperByHandle("atomic-handle", "zns:registry:test")
	if got == nil {
		t.Fatal("expected developer by handle after atomic creation")
	}
	if got.DeveloperID != "zns:dev:atomic1" {
		t.Errorf("wrong developer ID: %s", got.DeveloperID)
	}
}

// --- ZNS Name Tests ---

func TestStore_CreateAndGetZNSName(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:name1", "name-dev")
	createTestAgent(t, s, "zns:nameagent1")

	now := time.Now().UTC().Format(time.RFC3339)
	name := &models.ZNSName{
		FQAN:            "test.example.com/name-dev/my-agent",
		AgentName:       "my-agent",
		DeveloperHandle: "name-dev",
		RegistryHost:    "test.example.com",
		AgentID:         "zns:nameagent1",
		DeveloperID:     "zns:dev:name1",
		CurrentVersion:  "1.0.0",
		CapabilityTags:  []string{"nlp"},
		RegisteredAt:    now,
		UpdatedAt:       now,
		Signature:       "ed25519:sig",
	}

	if err := s.CreateZNSName(name); err != nil {
		t.Fatalf("CreateZNSName() error: %v", err)
	}

	// Get by FQAN
	got, err := s.GetZNSName("test.example.com/name-dev/my-agent")
	if err != nil || got == nil {
		t.Fatalf("GetZNSName() error: %v, got: %v", err, got)
	}
	if got.AgentName != "my-agent" {
		t.Errorf("expected agent_name 'my-agent', got %q", got.AgentName)
	}

	// Get by parts
	got2, _ := s.GetZNSNameByParts("name-dev", "my-agent", "test.example.com")
	if got2 == nil {
		t.Fatal("GetZNSNameByParts() returned nil")
	}

	// Get by agent ID
	got3, _ := s.GetZNSNameByAgentID("zns:nameagent1")
	if got3 == nil {
		t.Fatal("GetZNSNameByAgentID() returned nil")
	}
}

func TestStore_CreateZNSName_Duplicate(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:dupname", "dup-dev")
	createTestAgent(t, s, "zns:dupnameagent")

	now := time.Now().UTC().Format(time.RFC3339)
	name := &models.ZNSName{
		FQAN:            "test.example.com/dup-dev/same-name",
		AgentName:       "same-name",
		DeveloperHandle: "dup-dev",
		RegistryHost:    "test.example.com",
		AgentID:         "zns:dupnameagent",
		DeveloperID:     "zns:dev:dupname",
		RegisteredAt:    now,
		UpdatedAt:       now,
		Signature:       "ed25519:sig",
	}

	s.CreateZNSName(name)
	err := s.CreateZNSName(name)
	if err == nil {
		t.Fatal("expected error for duplicate FQAN")
	}
}

func TestStore_UpdateZNSName(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:upd", "upd-dev")
	createTestAgent(t, s, "zns:updagent")

	now := time.Now().UTC().Format(time.RFC3339)
	name := &models.ZNSName{
		FQAN:            "test.example.com/upd-dev/upd-agent",
		AgentName:       "upd-agent",
		DeveloperHandle: "upd-dev",
		RegistryHost:    "test.example.com",
		AgentID:         "zns:updagent",
		DeveloperID:     "zns:dev:upd",
		CurrentVersion:  "1.0.0",
		RegisteredAt:    now,
		UpdatedAt:       now,
		Signature:       "ed25519:sig",
	}
	s.CreateZNSName(name)

	name.CurrentVersion = "2.0.0"
	name.CapabilityTags = []string{"nlp", "translation"}
	name.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateZNSName(name); err != nil {
		t.Fatalf("UpdateZNSName() error: %v", err)
	}

	got, _ := s.GetZNSName(name.FQAN)
	if got.CurrentVersion != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", got.CurrentVersion)
	}
}

func TestStore_DeleteZNSName(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:del", "del-dev")
	createTestAgent(t, s, "zns:delagent")

	now := time.Now().UTC().Format(time.RFC3339)
	name := &models.ZNSName{
		FQAN:            "test.example.com/del-dev/del-agent",
		AgentName:       "del-agent",
		DeveloperHandle: "del-dev",
		RegistryHost:    "test.example.com",
		AgentID:         "zns:delagent",
		DeveloperID:     "zns:dev:del",
		RegisteredAt:    now,
		UpdatedAt:       now,
		Signature:       "ed25519:sig",
	}
	s.CreateZNSName(name)

	if err := s.DeleteZNSName(name.FQAN); err != nil {
		t.Fatalf("DeleteZNSName() error: %v", err)
	}

	got, _ := s.GetZNSName(name.FQAN)
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestStore_ListZNSNamesByDeveloper(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:list", "list-dev")
	createTestAgent(t, s, "zns:listagent1")
	createTestAgent(t, s, "zns:listagent2")

	now := time.Now().UTC().Format(time.RFC3339)
	for _, an := range []string{"alpha-agent", "beta-agent"} {
		aid := "zns:listagent1"
		if an == "beta-agent" {
			aid = "zns:listagent2"
		}
		s.CreateZNSName(&models.ZNSName{
			FQAN:            "test.example.com/list-dev/" + an,
			AgentName:       an,
			DeveloperHandle: "list-dev",
			RegistryHost:    "test.example.com",
			AgentID:         aid,
			DeveloperID:     "zns:dev:list",
			RegisteredAt:    now,
			UpdatedAt:       now,
			Signature:       "ed25519:sig",
		})
	}

	names, err := s.ListZNSNamesByDeveloper("list-dev", "test.example.com")
	if err != nil {
		t.Fatalf("ListZNSNamesByDeveloper() error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0].AgentName != "alpha-agent" {
		t.Errorf("expected first name 'alpha-agent', got %q", names[0].AgentName)
	}
}

// --- Version Tests ---

func TestStore_CreateAndGetZNSVersion(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:ver", "ver-dev")
	createTestAgent(t, s, "zns:veragent")

	now := time.Now().UTC().Format(time.RFC3339)
	s.CreateZNSName(&models.ZNSName{
		FQAN: "test.example.com/ver-dev/ver-agent", AgentName: "ver-agent",
		DeveloperHandle: "ver-dev", RegistryHost: "test.example.com",
		AgentID: "zns:veragent", DeveloperID: "zns:dev:ver",
		RegisteredAt: now, UpdatedAt: now, Signature: "ed25519:sig",
	})

	v := &models.ZNSVersion{
		FQAN: "test.example.com/ver-dev/ver-agent", Version: "1.0.0",
		AgentID: "zns:veragent", RegisteredAt: now, Signature: "ed25519:sig",
	}
	if err := s.CreateZNSVersion(v); err != nil {
		t.Fatalf("CreateZNSVersion() error: %v", err)
	}

	got, err := s.GetZNSVersion("test.example.com/ver-dev/ver-agent", "1.0.0")
	if err != nil || got == nil {
		t.Fatalf("GetZNSVersion() error: %v", err)
	}
	if got.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", got.Version)
	}
}

func TestStore_GetZNSVersions_Ordering(t *testing.T) {
	s := newZNSTestStore(t)
	createTestDeveloper(t, s, "zns:dev:vord", "vord-dev")
	createTestAgent(t, s, "zns:vordagent")

	now := time.Now().UTC().Format(time.RFC3339)
	fqan := "test.example.com/vord-dev/vord-agent"
	s.CreateZNSName(&models.ZNSName{
		FQAN: fqan, AgentName: "vord-agent", DeveloperHandle: "vord-dev",
		RegistryHost: "test.example.com", AgentID: "zns:vordagent",
		DeveloperID: "zns:dev:vord", RegisteredAt: now, UpdatedAt: now, Signature: "ed25519:sig",
	})

	for _, ver := range []string{"1.0.0", "2.0.0"} {
		s.CreateZNSVersion(&models.ZNSVersion{
			FQAN: fqan, Version: ver, AgentID: "zns:vordagent",
			RegisteredAt: now, Signature: "ed25519:sig",
		})
	}

	versions, _ := s.GetZNSVersions(fqan)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
}

// --- Gossip ZNS Tests ---

func TestStore_UpsertGossipZNSName(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	entry := &models.GossipZNSName{
		FQAN: "remote.example.com/remote-dev/remote-agent", AgentName: "remote-agent",
		DeveloperHandle: "remote-dev", RegistryHost: "remote.example.com",
		AgentID: "zns:remoteagent", CurrentVersion: "1.0.0", ReceivedAt: now,
	}

	if err := s.UpsertGossipZNSName(entry); err != nil {
		t.Fatalf("UpsertGossipZNSName() error: %v", err)
	}

	got, err := s.GetGossipZNSName("remote.example.com/remote-dev/remote-agent")
	if err != nil || got == nil {
		t.Fatalf("GetGossipZNSName() error: %v", err)
	}

	// Upsert again with updated version
	entry.CurrentVersion = "2.0.0"
	if err := s.UpsertGossipZNSName(entry); err != nil {
		t.Fatalf("UpsertGossipZNSName (update) error: %v", err)
	}

	got2, _ := s.GetGossipZNSName("remote.example.com/remote-dev/remote-agent")
	if got2.CurrentVersion != "2.0.0" {
		t.Errorf("expected version '2.0.0' after upsert, got %q", got2.CurrentVersion)
	}
}

func TestStore_GetGossipZNSNameByParts(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	s.UpsertGossipZNSName(&models.GossipZNSName{
		FQAN: "r.example.com/parts-dev/parts-agent", AgentName: "parts-agent",
		DeveloperHandle: "parts-dev", RegistryHost: "r.example.com",
		AgentID: "zns:partsagent", ReceivedAt: now,
	})

	got, _ := s.GetGossipZNSNameByParts("parts-dev", "parts-agent")
	if got == nil {
		t.Fatal("expected gossip ZNS name by parts, got nil")
	}
}

func TestStore_TombstoneGossipZNSName(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	fqan := "r.example.com/tomb-dev/tomb-agent"
	s.UpsertGossipZNSName(&models.GossipZNSName{
		FQAN: fqan, AgentName: "tomb-agent", DeveloperHandle: "tomb-dev",
		RegistryHost: "r.example.com", AgentID: "zns:tombagent", ReceivedAt: now,
	})

	s.TombstoneGossipZNSName(fqan)

	got, _ := s.GetGossipZNSName(fqan)
	if got != nil {
		t.Error("expected nil after tombstone")
	}
}

// --- Registry Proof Tests ---

func TestStore_UpsertRegistryProof(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	proof := &models.RegistryIdentityProof{
		Type: "registry-identity-proof", Version: "1.0",
		RegistryID: "zns:registry:proof1", Domain: "proof.example.com",
		Ed25519PublicKey: "testkey", TLSSPKIFingerprint: "sha256:abc",
		Signature: "ed25519:sig", VerificationTier: "domain-verified",
		IssuedAt: now, ExpiresAt: now, ReceivedAt: now,
	}

	if err := s.UpsertRegistryProof(proof); err != nil {
		t.Fatalf("UpsertRegistryProof() error: %v", err)
	}

	got, err := s.GetRegistryProof("zns:registry:proof1")
	if err != nil || got == nil {
		t.Fatalf("GetRegistryProof() error: %v", err)
	}
	if got.Domain != "proof.example.com" {
		t.Errorf("expected domain 'proof.example.com', got %q", got.Domain)
	}

	// Get by domain
	got2, _ := s.GetRegistryProofByDomain("proof.example.com")
	if got2 == nil {
		t.Fatal("GetRegistryProofByDomain() returned nil")
	}
}

func TestStore_PeerAttestations(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	// Create registry proof first (for foreign key if needed)
	s.UpsertRegistryProof(&models.RegistryIdentityProof{
		Type: "registry-identity-proof", Version: "1.0",
		RegistryID: "zns:registry:att-subject", Domain: "att.example.com",
		Ed25519PublicKey: "key", TLSSPKIFingerprint: "sha256:xyz",
		Signature: "sig", VerificationTier: "self-announced",
		IssuedAt: now, ExpiresAt: now, ReceivedAt: now,
	})

	for i, attID := range []string{"peer1", "peer2", "peer3"} {
		att := &models.PeerAttestation{
			AttesterID:     attID,
			SubjectID:      "zns:registry:att-subject",
			VerifiedLayers: []string{"tls", "rip"},
			AttestedAt:     now,
			Signature:      "ed25519:sig" + string(rune('0'+i)),
		}
		if err := s.CreatePeerAttestation(att); err != nil {
			t.Fatalf("CreatePeerAttestation() error: %v", err)
		}
	}

	count, err := s.CountPeerAttestations("zns:registry:att-subject")
	if err != nil {
		t.Fatalf("CountPeerAttestations() error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 attestations, got %d", count)
	}
}

func TestStore_UpdateRegistryVerificationTier(t *testing.T) {
	s := newZNSTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	s.UpsertRegistryProof(&models.RegistryIdentityProof{
		Type: "registry-identity-proof", Version: "1.0",
		RegistryID: "zns:registry:tier1", Domain: "tier.example.com",
		Ed25519PublicKey: "key", TLSSPKIFingerprint: "sha256:aaa",
		Signature: "sig", VerificationTier: "self-announced",
		IssuedAt: now, ExpiresAt: now, ReceivedAt: now,
	})

	s.UpdateRegistryVerificationTier("zns:registry:tier1", "mesh-verified")

	got, _ := s.GetRegistryProof("zns:registry:tier1")
	if got.VerificationTier != "mesh-verified" {
		t.Errorf("expected tier 'mesh-verified', got %q", got.VerificationTier)
	}
}
