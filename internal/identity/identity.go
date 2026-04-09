// Package identity manages Ed25519 keypairs for agents, developers, and registries.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// DeveloperID returns the developer_id derived from this keypair's public key.
// Format: zns:dev:<first 16 bytes of SHA-256 of public key as hex>
func (kp *Keypair) DeveloperID() string {
	return models.GenerateDeveloperID(kp.PublicKey)
}

// DeriveAgentKeypair deterministically derives an agent Ed25519 keypair
// from a developer's private key and an index. This uses HD-style derivation:
//
//	agent_seed = SHA-512(developer_private_key_seed || "zns:agent:" || uint32_be(index))[:32]
//	agent_keypair = Ed25519_from_seed(agent_seed)
//
// The derivation is deterministic: same developer key + same index always
// produces the same agent keypair. If the developer loses agent key files,
// they can re-derive them.
func DeriveAgentKeypair(developerPrivateKey ed25519.PrivateKey, index uint32) (*Keypair, error) {
	if len(developerPrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid developer private key size: %d", len(developerPrivateKey))
	}

	// Build derivation input: developer seed || domain separator || index
	seed := developerPrivateKey.Seed()
	indexBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(indexBytes, index)

	input := make([]byte, 0, len(seed)+len("zns:agent:")+4)
	input = append(input, seed...)
	input = append(input, []byte("zns:agent:")...)
	input = append(input, indexBytes...)

	// SHA-512 for full entropy, take first 32 bytes as Ed25519 seed
	hash := sha512.Sum512(input)
	agentSeed := hash[:ed25519.SeedSize]

	agentPrivKey := ed25519.NewKeyFromSeed(agentSeed)
	agentPubKey := agentPrivKey.Public().(ed25519.PublicKey)

	return &Keypair{
		PublicKey:     agentPubKey,
		PrivateKey:    agentPrivKey,
		PublicKeyB64:  base64.StdEncoding.EncodeToString(agentPubKey),
		PrivateKeyB64: base64.StdEncoding.EncodeToString(agentPrivKey),
	}, nil
}

// DeveloperProof is a cryptographic proof that a developer authorized a
// specific agent key at a specific index. Anyone can verify this proof
// using only the developer's public key -- no private key or online
// connectivity needed.
type DeveloperProof struct {
	DeveloperPublicKey string `json:"developer_public_key"` // ed25519:<base64>
	AgentIndex         int    `json:"agent_index"`
	DeveloperSignature string `json:"developer_signature"` // ed25519:<base64>
}

// CreateDerivationProof creates a proof that a developer authorized an agent key.
// The developer signs (agent_public_key || big_endian_uint32(index)).
// This proof can be verified offline by anyone with the developer's public key.
func CreateDerivationProof(developerKP *Keypair, agentPubKey ed25519.PublicKey, index uint32) *DeveloperProof {
	proofMsg := buildProofMessage(agentPubKey, index)

	return &DeveloperProof{
		DeveloperPublicKey: developerKP.PublicKeyString(),
		AgentIndex:         int(index),
		DeveloperSignature: developerKP.Sign(proofMsg),
	}
}

// VerifyDerivationProof verifies a developer-agent chain of trust.
// Returns true if the developer_signature is a valid Ed25519 signature
// over (agent_public_key_bytes || big_endian_uint32(agent_index))
// using the developer_public_key.
func VerifyDerivationProof(proof *DeveloperProof, agentPubKeyB64 string) (bool, error) {
	if proof == nil {
		return false, fmt.Errorf("proof is nil")
	}

	// Decode agent public key
	agentPubStr := agentPubKeyB64
	if strings.HasPrefix(agentPubStr, "ed25519:") {
		agentPubStr = agentPubStr[8:]
	}
	agentPubBytes, err := base64.StdEncoding.DecodeString(agentPubStr)
	if err != nil {
		return false, fmt.Errorf("failed to decode agent public key: %w", err)
	}

	if proof.AgentIndex < 0 {
		return false, fmt.Errorf("agent_index must be non-negative")
	}

	proofMsg := buildProofMessage(ed25519.PublicKey(agentPubBytes), uint32(proof.AgentIndex))

	// Verify using developer's public key
	devPubKey := proof.DeveloperPublicKey
	if strings.HasPrefix(devPubKey, "ed25519:") {
		devPubKey = devPubKey[8:]
	}

	return Verify(devPubKey, proofMsg, proof.DeveloperSignature)
}

// buildProofMessage constructs the canonical message for derivation proofs:
// agent_public_key_bytes || big_endian_uint32(index)
func buildProofMessage(agentPubKey ed25519.PublicKey, index uint32) []byte {
	indexBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(indexBytes, index)

	msg := make([]byte, 0, len(agentPubKey)+4)
	msg = append(msg, agentPubKey...)
	msg = append(msg, indexBytes...)
	return msg
}

// GenerateTLSConfig creates a TLS configuration using a self-signed certificate
// derived from this Ed25519 keypair. The certificate is used for mutual TLS
// between mesh peers; identity is verified at the application layer via HELLO.
func (kp *Keypair) GenerateTLSConfig() (*tls.Config, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, kp.PublicKey, kp.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  kp.PrivateKey,
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		// Identity is verified at the application layer via HELLO handshake,
		// not via the certificate chain.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}, nil
}
