package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateKeypair(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	if len(kp.PublicKey) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != 64 {
		t.Errorf("expected 64-byte private key, got %d", len(kp.PrivateKey))
	}
	if kp.PublicKeyB64 == "" {
		t.Error("public key base64 is empty")
	}
	if kp.PrivateKeyB64 == "" {
		t.Error("private key base64 is empty")
	}
}

func TestSignAndVerify(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	message := []byte("hello agent dns")
	sig := kp.Sign(message)

	if sig == "" {
		t.Fatal("signature is empty")
	}
	if len(sig) < 10 || sig[:8] != "ed25519:" {
		t.Fatalf("unexpected signature format: %s", sig)
	}

	// Verify with correct message
	valid, err := Verify(kp.PublicKeyB64, message, sig)
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}
	if !valid {
		t.Error("signature should be valid")
	}

	// Verify with wrong message
	valid, err = Verify(kp.PublicKeyB64, []byte("wrong message"), sig)
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}
	if valid {
		t.Error("signature should be invalid for wrong message")
	}
}

func TestAgentID(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	agentID := kp.AgentID()
	if agentID == "" {
		t.Fatal("agent ID is empty")
	}
	if len(agentID) < 7 || agentID[:6] != "agdns:" {
		t.Fatalf("unexpected agent ID format: %s", agentID)
	}

	// Same keypair should produce the same ID
	if kp.AgentID() != agentID {
		t.Error("agent ID should be deterministic")
	}
}

func TestRegistryID(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	regID := kp.RegistryID()
	if regID == "" {
		t.Fatal("registry ID is empty")
	}
	if len(regID) < 16 || regID[:15] != "agdns:registry:" {
		t.Fatalf("unexpected registry ID format: %s", regID)
	}
}

func TestSaveAndLoadKeypair(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test-keypair.json")

	// Generate and save
	kp1, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	if err := SaveKeypair(kp1, path); err != nil {
		t.Fatalf("failed to save keypair: %v", err)
	}

	// Check file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat keypair file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// Load and compare
	kp2, err := LoadKeypair(path)
	if err != nil {
		t.Fatalf("failed to load keypair: %v", err)
	}

	if kp1.PublicKeyB64 != kp2.PublicKeyB64 {
		t.Error("public keys don't match after load")
	}
	if kp1.PrivateKeyB64 != kp2.PrivateKeyB64 {
		t.Error("private keys don't match after load")
	}

	// Verify the loaded key can sign and verify
	msg := []byte("test message")
	sig := kp2.Sign(msg)
	valid, err := Verify(kp2.PublicKeyB64, msg, sig)
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}
	if !valid {
		t.Error("loaded key should produce valid signatures")
	}
}

func TestDifferentKeypairsDifferentIDs(t *testing.T) {
	kp1, _ := GenerateKeypair()
	kp2, _ := GenerateKeypair()

	if kp1.AgentID() == kp2.AgentID() {
		t.Error("different keypairs should produce different agent IDs")
	}
}
