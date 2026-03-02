// Package identity manages Ed25519 keypairs for agents and registries.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentdns/agent-dns/internal/models"
)

// Keypair holds an Ed25519 keypair.
type Keypair struct {
	PublicKey     ed25519.PublicKey  `json:"-"`
	PrivateKey    ed25519.PrivateKey `json:"-"`
	PublicKeyB64  string             `json:"public_key"`
	PrivateKeyB64 string             `json:"private_key"`
}

// GenerateKeypair creates a new Ed25519 keypair.
func GenerateKeypair() (*Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %w", err)
	}

	return &Keypair{
		PublicKey:     pub,
		PrivateKey:    priv,
		PublicKeyB64:  base64.StdEncoding.EncodeToString(pub),
		PrivateKeyB64: base64.StdEncoding.EncodeToString(priv),
	}, nil
}

// LoadKeypair loads a keypair from a JSON file.
func LoadKeypair(path string) (*Keypair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read keypair file: %w", err)
	}

	kp := &Keypair{}
	if err := json.Unmarshal(data, kp); err != nil {
		return nil, fmt.Errorf("failed to parse keypair: %w", err)
	}

	// Decode base64 keys
	pubBytes, err := base64.StdEncoding.DecodeString(kp.PublicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}
	privBytes, err := base64.StdEncoding.DecodeString(kp.PrivateKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}

	kp.PublicKey = ed25519.PublicKey(pubBytes)
	kp.PrivateKey = ed25519.PrivateKey(privBytes)

	return kp, nil
}

// SaveKeypair writes a keypair to a JSON file with restricted permissions.
func SaveKeypair(kp *Keypair, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(kp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keypair: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write keypair file: %w", err)
	}

	return nil
}

// Sign signs a message with the private key.
// Returns the signature as "ed25519:<base64>" string.
func (kp *Keypair) Sign(message []byte) string {
	sig := ed25519.Sign(kp.PrivateKey, message)
	return "ed25519:" + base64.StdEncoding.EncodeToString(sig)
}

// Verify checks a signature against a message and public key.
// Accepts signatures in "ed25519:<base64>" format.
func Verify(publicKeyB64 string, message []byte, signature string) (bool, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return false, fmt.Errorf("failed to decode public key: %w", err)
	}

	if len(signature) < 9 || signature[:8] != "ed25519:" {
		return false, fmt.Errorf("invalid signature format, expected ed25519:<base64>")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(signature[8:])
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	pubKey := ed25519.PublicKey(pubBytes)
	return ed25519.Verify(pubKey, message, sigBytes), nil
}

// AgentID returns the agent_id derived from this keypair's public key.
func (kp *Keypair) AgentID() string {
	return models.GenerateAgentID(kp.PublicKey)
}

// RegistryID returns the registry_id derived from this keypair's public key.
func (kp *Keypair) RegistryID() string {
	return models.GenerateRegistryID(kp.PublicKey)
}

// PublicKeyString returns the public key in "ed25519:<base64>" format.
func (kp *Keypair) PublicKeyString() string {
	return "ed25519:" + kp.PublicKeyB64
}
