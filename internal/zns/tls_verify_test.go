package zns

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createSelfSignedCert(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPath := filepath.Join(t.TempDir(), "cert.pem")
	f, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	defer f.Close()

	pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return certPath
}

func TestExtractSPKIFingerprint_ValidCert(t *testing.T) {
	certPath := createSelfSignedCert(t)

	fp, err := ExtractSPKIFingerprint(certPath)
	if err != nil {
		t.Fatalf("ExtractSPKIFingerprint() error: %v", err)
	}

	if !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", fp)
	}

	// SHA-256 hex is 64 chars after the prefix
	hex := strings.TrimPrefix(fp, "sha256:")
	if len(hex) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(hex))
	}

	// Deterministic: same cert produces same fingerprint
	fp2, _ := ExtractSPKIFingerprint(certPath)
	if fp != fp2 {
		t.Error("SPKI fingerprint is not deterministic")
	}
}

func TestExtractSPKIFingerprint_InvalidPEM(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bad.pem")
	os.WriteFile(tmpFile, []byte("not a PEM file"), 0644)

	_, err := ExtractSPKIFingerprint(tmpFile)
	if err == nil {
		t.Error("expected error for invalid PEM, got nil")
	}
}

func TestExtractSPKIFingerprint_NonexistentFile(t *testing.T) {
	_, err := ExtractSPKIFingerprint("/nonexistent/cert.pem")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}
