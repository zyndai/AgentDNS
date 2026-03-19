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

// --- Developer Identity Tests ---

func TestDeveloperID(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	devID := kp.DeveloperID()
	if devID == "" {
		t.Fatal("developer ID is empty")
	}
	if len(devID) < 11 || devID[:10] != "agdns:dev:" {
		t.Fatalf("unexpected developer ID format: %s", devID)
	}

	// Deterministic
	if kp.DeveloperID() != devID {
		t.Error("developer ID should be deterministic")
	}
}

func TestDeriveAgentKeypair(t *testing.T) {
	devKP, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate developer keypair: %v", err)
	}

	// Derive agent at index 0
	agentKP0, err := DeriveAgentKeypair(devKP.PrivateKey, 0)
	if err != nil {
		t.Fatalf("failed to derive agent keypair at index 0: %v", err)
	}

	if len(agentKP0.PublicKey) != 32 {
		t.Errorf("expected 32-byte agent public key, got %d", len(agentKP0.PublicKey))
	}
	if len(agentKP0.PrivateKey) != 64 {
		t.Errorf("expected 64-byte agent private key, got %d", len(agentKP0.PrivateKey))
	}

	// Derived agent should have a different key from developer
	if agentKP0.PublicKeyB64 == devKP.PublicKeyB64 {
		t.Error("derived agent key should differ from developer key")
	}

	// Derived agent should have a different agent_id from developer
	if agentKP0.AgentID() == devKP.AgentID() {
		t.Error("derived agent ID should differ from developer's agent ID")
	}
}

func TestDeriveAgentKeypairDeterministic(t *testing.T) {
	devKP, _ := GenerateKeypair()

	// Same index should produce same key
	kp1, _ := DeriveAgentKeypair(devKP.PrivateKey, 42)
	kp2, _ := DeriveAgentKeypair(devKP.PrivateKey, 42)

	if kp1.PublicKeyB64 != kp2.PublicKeyB64 {
		t.Error("same developer + same index should produce same agent key")
	}
	if kp1.PrivateKeyB64 != kp2.PrivateKeyB64 {
		t.Error("same developer + same index should produce same agent private key")
	}
}

func TestDeriveAgentKeypairDifferentIndexes(t *testing.T) {
	devKP, _ := GenerateKeypair()

	kp0, _ := DeriveAgentKeypair(devKP.PrivateKey, 0)
	kp1, _ := DeriveAgentKeypair(devKP.PrivateKey, 1)
	kp2, _ := DeriveAgentKeypair(devKP.PrivateKey, 2)

	if kp0.PublicKeyB64 == kp1.PublicKeyB64 {
		t.Error("different indexes should produce different agent keys")
	}
	if kp1.PublicKeyB64 == kp2.PublicKeyB64 {
		t.Error("different indexes should produce different agent keys")
	}
}

func TestDeriveAgentKeypairDifferentDevelopers(t *testing.T) {
	dev1, _ := GenerateKeypair()
	dev2, _ := GenerateKeypair()

	kp1, _ := DeriveAgentKeypair(dev1.PrivateKey, 0)
	kp2, _ := DeriveAgentKeypair(dev2.PrivateKey, 0)

	if kp1.PublicKeyB64 == kp2.PublicKeyB64 {
		t.Error("same index but different developers should produce different agent keys")
	}
}

func TestDeriveAgentKeypairCanSign(t *testing.T) {
	devKP, _ := GenerateKeypair()
	agentKP, _ := DeriveAgentKeypair(devKP.PrivateKey, 0)

	msg := []byte("test message from derived agent")
	sig := agentKP.Sign(msg)

	valid, err := Verify(agentKP.PublicKeyB64, msg, sig)
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}
	if !valid {
		t.Error("derived agent key should produce valid signatures")
	}

	// Developer key should NOT verify agent's signature
	valid, _ = Verify(devKP.PublicKeyB64, msg, sig)
	if valid {
		t.Error("developer key should NOT verify agent's signature")
	}
}

func TestCreateAndVerifyDerivationProof(t *testing.T) {
	devKP, _ := GenerateKeypair()
	agentKP, _ := DeriveAgentKeypair(devKP.PrivateKey, 5)

	// Create proof
	proof := CreateDerivationProof(devKP, agentKP.PublicKey, 5)

	if proof.DeveloperPublicKey != devKP.PublicKeyString() {
		t.Error("proof should contain developer's public key")
	}
	if proof.AgentIndex != 5 {
		t.Errorf("proof should have index 5, got %d", proof.AgentIndex)
	}
	if proof.DeveloperSignature == "" {
		t.Error("proof signature should not be empty")
	}

	// Verify proof
	valid, err := VerifyDerivationProof(proof, agentKP.PublicKeyString())
	if err != nil {
		t.Fatalf("proof verification error: %v", err)
	}
	if !valid {
		t.Error("derivation proof should be valid")
	}
}

func TestVerifyDerivationProofWrongAgentKey(t *testing.T) {
	devKP, _ := GenerateKeypair()
	agentKP, _ := DeriveAgentKeypair(devKP.PrivateKey, 0)
	wrongKP, _ := GenerateKeypair()

	proof := CreateDerivationProof(devKP, agentKP.PublicKey, 0)

	// Verify with wrong agent key should fail
	valid, err := VerifyDerivationProof(proof, wrongKP.PublicKeyString())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("proof should be invalid for wrong agent key")
	}
}

func TestVerifyDerivationProofWrongIndex(t *testing.T) {
	devKP, _ := GenerateKeypair()
	agentKP, _ := DeriveAgentKeypair(devKP.PrivateKey, 0)

	// Create proof with wrong index
	proof := CreateDerivationProof(devKP, agentKP.PublicKey, 0)
	proof.AgentIndex = 999 // tamper with index

	valid, err := VerifyDerivationProof(proof, agentKP.PublicKeyString())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("proof should be invalid for wrong index")
	}
}

func TestVerifyDerivationProofWrongDeveloper(t *testing.T) {
	dev1, _ := GenerateKeypair()
	dev2, _ := GenerateKeypair()
	agentKP, _ := DeriveAgentKeypair(dev1.PrivateKey, 0)

	// Create proof with wrong developer
	proof := CreateDerivationProof(dev2, agentKP.PublicKey, 0)

	// The proof should verify against dev2's key (since dev2 signed it),
	// but this proves dev2 authorized this agent -- not dev1.
	// The registry validates that proof.DeveloperPublicKey matches the stored developer.
	valid, err := VerifyDerivationProof(proof, agentKP.PublicKeyString())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// This IS valid because dev2 did sign the proof -- the registry-level
	// check ensures the developer_public_key matches the registered developer.
	if !valid {
		t.Error("proof signed by dev2 should verify with dev2's key")
	}
}

func TestVerifyDerivationProofNil(t *testing.T) {
	_, err := VerifyDerivationProof(nil, "somekey")
	if err == nil {
		t.Error("should return error for nil proof")
	}
}

func TestFullDerivationFlow(t *testing.T) {
	// Simulate the full flow: developer creates key, derives agents, creates proofs
	devKP, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate developer keypair: %v", err)
	}

	devID := devKP.DeveloperID()
	if devID[:10] != "agdns:dev:" {
		t.Fatalf("unexpected developer ID format: %s", devID)
	}

	// Derive 3 agents
	for i := uint32(0); i < 3; i++ {
		agentKP, err := DeriveAgentKeypair(devKP.PrivateKey, i)
		if err != nil {
			t.Fatalf("failed to derive agent %d: %v", i, err)
		}

		// Create and verify proof
		proof := CreateDerivationProof(devKP, agentKP.PublicKey, i)
		valid, err := VerifyDerivationProof(proof, agentKP.PublicKeyString())
		if err != nil {
			t.Fatalf("proof verification error for agent %d: %v", i, err)
		}
		if !valid {
			t.Errorf("proof should be valid for agent %d", i)
		}

		// Agent should be able to sign independently
		msg := []byte("hello from agent")
		sig := agentKP.Sign(msg)
		ok, _ := Verify(agentKP.PublicKeyB64, msg, sig)
		if !ok {
			t.Errorf("agent %d should produce valid signatures", i)
		}
	}
}
