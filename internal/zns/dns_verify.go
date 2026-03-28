package zns

import (
	"fmt"
	"net"
	"strings"
)

// DNSResolver abstracts DNS lookups for testing.
type DNSResolver interface {
	LookupTXT(name string) ([]string, error)
}

// NetResolver uses the standard library for DNS lookups.
type NetResolver struct{}

func (NetResolver) LookupTXT(name string) ([]string, error) {
	return net.LookupTXT(name)
}

// defaultResolver is the production DNS resolver.
var defaultResolver DNSResolver = NetResolver{}

// VerifyDeveloperDNS checks a DNS TXT record at _zynd-verify.{domain} for
// a developer's public key. Returns true if a matching record is found.
func VerifyDeveloperDNS(domain, expectedPublicKey string) (bool, error) {
	return VerifyDeveloperDNSWithResolver(domain, expectedPublicKey, defaultResolver)
}

// VerifyDeveloperDNSWithResolver is like VerifyDeveloperDNS but accepts a custom resolver.
func VerifyDeveloperDNSWithResolver(domain, expectedPublicKey string, resolver DNSResolver) (bool, error) {
	if domain == "" {
		return false, fmt.Errorf("domain is required")
	}
	if expectedPublicKey == "" {
		return false, fmt.Errorf("expected public key is required")
	}

	lookupName := "_zynd-verify." + domain
	records, err := resolver.LookupTXT(lookupName)
	if err != nil {
		return false, fmt.Errorf("DNS lookup for %s failed: %w", lookupName, err)
	}

	// The TXT record should contain the developer's public key (with or without ed25519: prefix)
	cleanExpected := strings.TrimPrefix(expectedPublicKey, "ed25519:")
	for _, record := range records {
		cleanRecord := strings.TrimSpace(record)
		cleanRecord = strings.TrimPrefix(cleanRecord, "ed25519:")
		if cleanRecord == cleanExpected {
			return true, nil
		}
	}

	return false, nil
}

// VerifyRegistryDNS checks a DNS TXT record at _zynd.{domain} for a registry's
// Ed25519 public key. The record format is:
//
//	v=zynd1 id=agdns:registry:... key=ed25519:...
//
// Returns true if a matching key is found.
func VerifyRegistryDNS(domain, expectedPublicKey string) (bool, error) {
	return VerifyRegistryDNSWithResolver(domain, expectedPublicKey, defaultResolver)
}

// VerifyRegistryDNSWithResolver is like VerifyRegistryDNS but accepts a custom resolver.
func VerifyRegistryDNSWithResolver(domain, expectedPublicKey string, resolver DNSResolver) (bool, error) {
	if domain == "" {
		return false, fmt.Errorf("domain is required")
	}
	if expectedPublicKey == "" {
		return false, fmt.Errorf("expected public key is required")
	}

	lookupName := "_zynd." + domain
	records, err := resolver.LookupTXT(lookupName)
	if err != nil {
		return false, fmt.Errorf("DNS lookup for %s failed: %w", lookupName, err)
	}

	cleanExpected := strings.TrimPrefix(expectedPublicKey, "ed25519:")

	for _, record := range records {
		// Parse "v=zynd1 id=... key=ed25519:..."
		fields := parseZyndTXTRecord(record)
		if fields["v"] != "zynd1" {
			continue
		}
		key := strings.TrimPrefix(fields["key"], "ed25519:")
		if key == cleanExpected {
			return true, nil
		}
	}

	return false, nil
}

// parseZyndTXTRecord parses a space-separated key=value TXT record.
func parseZyndTXTRecord(record string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Fields(record) {
		if idx := strings.Index(part, "="); idx > 0 {
			result[part[:idx]] = part[idx+1:]
		}
	}
	return result
}

// ResolveDNSBridge looks up _zynd.{domain} TXT records to find an FQAN pointer.
// This enables DNS-native discovery: an agent at translator.acme-corp.com can publish
// a TXT record pointing to its Zynd FQAN.
// Record format: fqan=dns01.zynd.ai/acme-corp/doc-translator
func ResolveDNSBridge(domain string) (string, error) {
	return ResolveDNSBridgeWithResolver(domain, defaultResolver)
}

// ResolveDNSBridgeWithResolver is like ResolveDNSBridge but accepts a custom resolver.
func ResolveDNSBridgeWithResolver(domain string, resolver DNSResolver) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}

	lookupName := "_zynd." + domain
	records, err := resolver.LookupTXT(lookupName)
	if err != nil {
		return "", fmt.Errorf("DNS lookup for %s failed: %w", lookupName, err)
	}

	for _, record := range records {
		fields := parseZyndTXTRecord(record)
		if fqan, ok := fields["fqan"]; ok && fqan != "" {
			return fqan, nil
		}
	}

	return "", fmt.Errorf("no FQAN found in DNS records for %s", lookupName)
}
