package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// DeveloperApprovalRequest is what the org website sends to the registry
// after completing KYC/onboarding for a developer.
type DeveloperApprovalRequest struct {
	Name         string            `json:"name"`
	State        string            `json:"state"`                    // from CLI, used to encrypt private key
	CallbackPort int               `json:"callback_port"`            // CLI's localhost port
	Metadata     map[string]string `json:"metadata,omitempty"`       // org-specific (email, kyc_id, etc.)
}

// DeveloperApprovalResponse is returned by the registry to the org website
// after approving a developer.
type DeveloperApprovalResponse struct {
	DeveloperID   string `json:"developer_id"`
	PrivateKeyEnc string `json:"private_key_enc"` // AES-GCM encrypted with SHA256(state)
}

// RegistryInfoResponse is returned by GET /v1/info.
type RegistryInfoResponse struct {
	RegistryID          string                    `json:"registry_id"`
	Name                string                    `json:"name"`
	DeveloperOnboarding *DeveloperOnboardingInfo  `json:"developer_onboarding"`
}

// DeveloperOnboardingInfo describes the onboarding mode for a registry.
type DeveloperOnboardingInfo struct {
	Mode    string `json:"mode"`
	AuthURL string `json:"auth_url,omitempty"` // only set when mode=restricted
}

// EncryptPrivateKey encrypts a base64-encoded private key using AES-256-GCM
// with SHA256(state) as the key.
func EncryptPrivateKey(privateKeyB64, state string) (string, error) {
	key := sha256.Sum256([]byte(state))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	plaintext := []byte(privateKeyB64)
	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptPrivateKey decrypts an AES-256-GCM encrypted private key using SHA256(state).
func DecryptPrivateKey(ciphertextB64, state string) (string, error) {
	key := sha256.Sum256([]byte(state))

	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
