package zns

import (
	"fmt"
	"testing"
)

// mockResolver returns pre-configured TXT records for testing.
type mockResolver struct {
	records map[string][]string
}

func (m *mockResolver) LookupTXT(name string) ([]string, error) {
	if recs, ok := m.records[name]; ok {
		return recs, nil
	}
	return nil, fmt.Errorf("no such host: %s", name)
}

func TestVerifyDeveloperDNS(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"_zynd-verify.acme-corp.com": {"ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw="},
			"_zynd-verify.example.com":   {"wrong-key"},
		},
	}

	tests := []struct {
		domain    string
		pubKey    string
		wantMatch bool
		wantErr   bool
	}{
		{"acme-corp.com", "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=", true, false},
		{"acme-corp.com", "gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=", true, false}, // without prefix
		{"example.com", "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=", false, false},
		{"noexist.com", "ed25519:some-key", false, true}, // DNS lookup error
		{"", "ed25519:some-key", false, true},             // empty domain
		{"acme-corp.com", "", false, true},                // empty key
	}
	for _, tt := range tests {
		match, err := VerifyDeveloperDNSWithResolver(tt.domain, tt.pubKey, resolver)
		if tt.wantErr {
			if err == nil {
				t.Errorf("VerifyDeveloperDNS(%q, %q) returned nil error, want error", tt.domain, tt.pubKey)
			}
			continue
		}
		if err != nil {
			t.Errorf("VerifyDeveloperDNS(%q, %q) returned error: %v", tt.domain, tt.pubKey, err)
			continue
		}
		if match != tt.wantMatch {
			t.Errorf("VerifyDeveloperDNS(%q, %q) = %v, want %v", tt.domain, tt.pubKey, match, tt.wantMatch)
		}
	}
}

func TestVerifyRegistryDNS(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"_zynd.dns01.zynd.ai": {"v=zynd1 id=agdns:registry:abc123 key=ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw="},
			"_zynd.bad.example":   {"v=zynd1 id=agdns:registry:xyz key=ed25519:wrong-key"},
		},
	}

	tests := []struct {
		domain    string
		pubKey    string
		wantMatch bool
		wantErr   bool
	}{
		{"dns01.zynd.ai", "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=", true, false},
		{"dns01.zynd.ai", "gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=", true, false},
		{"bad.example", "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=", false, false},
		{"noexist.com", "ed25519:key", false, true},
	}
	for _, tt := range tests {
		match, err := VerifyRegistryDNSWithResolver(tt.domain, tt.pubKey, resolver)
		if tt.wantErr {
			if err == nil {
				t.Errorf("VerifyRegistryDNS(%q, %q) returned nil error, want error", tt.domain, tt.pubKey)
			}
			continue
		}
		if err != nil {
			t.Errorf("VerifyRegistryDNS(%q, %q) returned error: %v", tt.domain, tt.pubKey, err)
			continue
		}
		if match != tt.wantMatch {
			t.Errorf("VerifyRegistryDNS(%q, %q) = %v, want %v", tt.domain, tt.pubKey, match, tt.wantMatch)
		}
	}
}

func TestResolveDNSBridge(t *testing.T) {
	resolver := &mockResolver{
		records: map[string][]string{
			"_zynd.translator.acme-corp.com": {"fqan=dns01.zynd.ai/acme-corp/doc-translator"},
			"_zynd.no-fqan.example.com":     {"v=zynd1 id=agdns:registry:abc"},
		},
	}

	// Valid bridge record
	fqan, err := ResolveDNSBridgeWithResolver("translator.acme-corp.com", resolver)
	if err != nil {
		t.Fatalf("ResolveDNSBridge() returned error: %v", err)
	}
	if fqan != "dns01.zynd.ai/acme-corp/doc-translator" {
		t.Errorf("ResolveDNSBridge() = %q, want %q", fqan, "dns01.zynd.ai/acme-corp/doc-translator")
	}

	// No FQAN in record
	_, err = ResolveDNSBridgeWithResolver("no-fqan.example.com", resolver)
	if err == nil {
		t.Error("ResolveDNSBridge() returned nil error for missing FQAN")
	}

	// Domain doesn't exist
	_, err = ResolveDNSBridgeWithResolver("noexist.com", resolver)
	if err == nil {
		t.Error("ResolveDNSBridge() returned nil error for nonexistent domain")
	}
}
