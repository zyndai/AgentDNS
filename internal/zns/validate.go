// Package zns implements the Zynd Naming Service — human-readable agent naming,
// handle validation, FQAN construction/parsing, and DNS-based verification.
package zns

import (
	"fmt"
	"regexp"
	"strings"
)

// handleRegex matches valid handles and agent names: lowercase alphanumeric + hyphens, 3-40 chars, starts with letter.
var handleRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{2,39}$`)

// reservedHandles are blocked on all registries.
var reservedHandles = map[string]bool{
	"zynd":      true,
	"system":    true,
	"admin":     true,
	"test":      true,
	"root":      true,
	"registry":  true,
	"anonymous": true,
	"unknown":   true,
}

// ValidateHandle checks whether a developer handle is valid.
// Rules: lowercase alphanumeric + hyphens, 3-40 chars, starts with a letter, not reserved.
func ValidateHandle(handle string) error {
	if handle == "" {
		return fmt.Errorf("handle is required")
	}
	if reservedHandles[handle] {
		return fmt.Errorf("handle %q is reserved", handle)
	}
	if !handleRegex.MatchString(handle) {
		return fmt.Errorf("handle must be 3-40 lowercase alphanumeric characters or hyphens, starting with a letter")
	}
	if strings.HasSuffix(handle, "-") {
		return fmt.Errorf("handle must not end with a hyphen")
	}
	if strings.Contains(handle, "--") {
		return fmt.Errorf("handle must not contain consecutive hyphens")
	}
	return nil
}

// ValidateAgentName checks whether an agent name is valid.
// Same rules as handles: lowercase alphanumeric + hyphens, 3-40 chars, starts with a letter.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	if !handleRegex.MatchString(name) {
		return fmt.Errorf("agent name must be 3-40 lowercase alphanumeric characters or hyphens, starting with a letter")
	}
	if strings.HasSuffix(name, "-") {
		return fmt.Errorf("agent name must not end with a hyphen")
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("agent name must not contain consecutive hyphens")
	}
	return nil
}

// BuildFQAN constructs a Fully Qualified Agent Name from its components.
func BuildFQAN(registryHost, devHandle, agentName string) string {
	return registryHost + "/" + devHandle + "/" + agentName
}

// ParseFQAN splits a FQAN into its three components: registry host, developer handle, agent name.
// The input may optionally include @version and #capability qualifiers, which are stripped.
func ParseFQAN(fqan string) (registryHost, devHandle, agentName string, err error) {
	// Strip qualifiers first
	base, _, _, err := ParseQualifiedFQAN(fqan)
	if err != nil {
		return "", "", "", err
	}

	parts := strings.SplitN(base, "/", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid FQAN %q: expected format registry-host/developer/agent", fqan)
	}

	registryHost = parts[0]
	devHandle = parts[1]
	agentName = parts[2]

	if registryHost == "" {
		return "", "", "", fmt.Errorf("invalid FQAN: registry host is empty")
	}
	if err := ValidateHandle(devHandle); err != nil {
		return "", "", "", fmt.Errorf("invalid FQAN developer handle: %w", err)
	}
	if err := ValidateAgentName(agentName); err != nil {
		return "", "", "", fmt.Errorf("invalid FQAN agent name: %w", err)
	}

	return registryHost, devHandle, agentName, nil
}

// ParseQualifiedFQAN extracts the base FQAN, optional version, and optional capability from
// a qualified FQAN string like "host/dev/agent@2.1.0#nlp.translation".
func ParseQualifiedFQAN(fqan string) (base, version, capability string, err error) {
	if fqan == "" {
		return "", "", "", fmt.Errorf("FQAN is empty")
	}

	// Strip zns:// or legacy agdns:// scheme if present
	fqan = strings.TrimPrefix(fqan, "zns://")
	fqan = strings.TrimPrefix(fqan, "agdns://")

	s := fqan

	// Extract #capability (must come after @version if both present)
	if idx := strings.LastIndex(s, "#"); idx >= 0 {
		capability = s[idx+1:]
		s = s[:idx]
		if capability == "" {
			return "", "", "", fmt.Errorf("empty capability qualifier in %q", fqan)
		}
	}

	// Extract @version
	if idx := strings.LastIndex(s, "@"); idx >= 0 {
		version = s[idx+1:]
		s = s[:idx]
		if version == "" {
			return "", "", "", fmt.Errorf("empty version qualifier in %q", fqan)
		}
	}

	base = s
	if base == "" {
		return "", "", "", fmt.Errorf("empty base FQAN in %q", fqan)
	}

	return base, version, capability, nil
}

// BuildZNSURI constructs the zns:// URI form of a FQAN.
func BuildZNSURI(fqan string) string {
	return "zns://" + fqan
}

// BuildAgdnsURI is a legacy alias for BuildZNSURI.
func BuildAgdnsURI(fqan string) string {
	return BuildZNSURI(fqan)
}

// IsReservedHandle returns true if the handle is in the reserved list.
func IsReservedHandle(handle string) bool {
	return reservedHandles[handle]
}
