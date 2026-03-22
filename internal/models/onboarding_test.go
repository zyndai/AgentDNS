package models

import (
	"encoding/base64"
	"testing"
)

func TestEncryptDecryptPrivateKey(t *testing.T) {
	privateKeyB64 := base64.StdEncoding.EncodeToString([]byte("test-private-key-32-bytes-long!!"))
	state := "random-state-value-abc123"

	encrypted, err := EncryptPrivateKey(privateKeyB64, state)
	if err != nil {
		t.Fatalf("EncryptPrivateKey failed: %v", err)
	}

	if encrypted == "" {
		t.Fatal("encrypted result should not be empty")
	}

	if encrypted == privateKeyB64 {
		t.Fatal("encrypted should differ from plaintext")
	}

	decrypted, err := DecryptPrivateKey(encrypted, state)
	if err != nil {
		t.Fatalf("DecryptPrivateKey failed: %v", err)
	}

	if decrypted != privateKeyB64 {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, privateKeyB64)
	}
}

func TestDecryptPrivateKeyWrongState(t *testing.T) {
	privateKeyB64 := base64.StdEncoding.EncodeToString([]byte("test-private-key-32-bytes-long!!"))
	state := "correct-state"
	wrongState := "wrong-state"

	encrypted, err := EncryptPrivateKey(privateKeyB64, state)
	if err != nil {
		t.Fatalf("EncryptPrivateKey failed: %v", err)
	}

	_, err = DecryptPrivateKey(encrypted, wrongState)
	if err == nil {
		t.Fatal("DecryptPrivateKey should fail with wrong state")
	}
}

func TestDecryptPrivateKeyInvalidCiphertext(t *testing.T) {
	_, err := DecryptPrivateKey("not-valid-base64!!!", "some-state")
	if err == nil {
		t.Fatal("DecryptPrivateKey should fail with invalid base64")
	}
}

func TestDecryptPrivateKeyTooShort(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := DecryptPrivateKey(short, "some-state")
	if err == nil {
		t.Fatal("DecryptPrivateKey should fail with too-short ciphertext")
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	privateKeyB64 := base64.StdEncoding.EncodeToString([]byte("test-key"))
	state := "same-state"

	enc1, _ := EncryptPrivateKey(privateKeyB64, state)
	enc2, _ := EncryptPrivateKey(privateKeyB64, state)

	if enc1 == enc2 {
		t.Fatal("two encryptions should produce different ciphertexts due to random nonce")
	}
}
