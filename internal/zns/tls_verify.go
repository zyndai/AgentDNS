package zns

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
)

// ExtractSPKIFingerprint reads a PEM-encoded certificate file and returns
// the SHA-256 fingerprint of its Subject Public Key Info (SPKI).
// Format: "sha256:<hex>"
func ExtractSPKIFingerprint(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("failed to read certificate file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from %s", certPath)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %w", err)
	}

	// SPKI is the raw DER-encoded SubjectPublicKeyInfo
	spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256:" + hex.EncodeToString(spkiHash[:]), nil
}

// VerifySPKIMatch connects to a TLS host and checks whether the server's
// certificate SPKI fingerprint matches the expected value.
func VerifySPKIMatch(addr, expectedFingerprint string) (bool, error) {
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return false, fmt.Errorf("TLS connection to %s failed: %w", addr, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return false, fmt.Errorf("no peer certificates from %s", addr)
	}

	// Check the leaf certificate's SPKI
	spkiHash := sha256.Sum256(certs[0].RawSubjectPublicKeyInfo)
	actual := "sha256:" + hex.EncodeToString(spkiHash[:])

	return actual == expectedFingerprint, nil
}
