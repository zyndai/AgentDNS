package zns

import (
	"testing"
)

func TestValidateHandle(t *testing.T) {
	tests := []struct {
		handle string
		valid  bool
	}{
		{"acme-corp", true},
		{"johndoe", true},
		{"abc", true},
		{"a-b-c", true},
		{"dev123", true},
		{"abcdefghijklmnopqrstuvwxyz1234567890abcd", true}, // 40 chars exactly

		// Invalid cases
		{"", false},           // empty
		{"ab", false},         // too short
		{"AB", false},         // uppercase
		{"1abc", false},       // starts with number
		{"-abc", false},       // starts with hyphen
		{"abc-", false},       // ends with hyphen
		{"abc--def", false},   // consecutive hyphens
		{"zynd", false},       // reserved
		{"admin", false},      // reserved
		{"system", false},     // reserved
		{"test", false},       // reserved
		{"root", false},       // reserved
		{"registry", false},   // reserved
		{"anonymous", false},  // reserved
		{"unknown", false},    // reserved
		{"abc.def", false},    // dots not allowed
		{"abc_def", false},    // underscores not allowed
		{"abc def", false},    // spaces not allowed
		{"abcdefghijklmnopqrstuvwxyz1234567890abcde", false}, // 41 chars, too long
	}
	for _, tt := range tests {
		err := ValidateHandle(tt.handle)
		if tt.valid && err != nil {
			t.Errorf("ValidateHandle(%q) returned error: %v, want valid", tt.handle, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("ValidateHandle(%q) returned nil, want error", tt.handle)
		}
	}
}

func TestValidateAgentName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"doc-translator", true},
		{"my-agent", true},
		{"sentiment-api", true},
		{"abc", true},

		{"", false},
		{"ab", false},
		{"ABC", false},
		{"1agent", false},
		{"agent-", false},
		{"agent--name", false},
	}
	for _, tt := range tests {
		err := ValidateAgentName(tt.name)
		if tt.valid && err != nil {
			t.Errorf("ValidateAgentName(%q) returned error: %v, want valid", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("ValidateAgentName(%q) returned nil, want error", tt.name)
		}
	}
}

func TestBuildFQAN(t *testing.T) {
	fqan := BuildFQAN("dns01.zynd.ai", "acme-corp", "doc-translator")
	expected := "dns01.zynd.ai/acme-corp/doc-translator"
	if fqan != expected {
		t.Errorf("BuildFQAN() = %q, want %q", fqan, expected)
	}
}

func TestParseFQAN(t *testing.T) {
	tests := []struct {
		fqan     string
		host     string
		dev      string
		agent    string
		wantErr  bool
	}{
		{"dns01.zynd.ai/acme-corp/doc-translator", "dns01.zynd.ai", "acme-corp", "doc-translator", false},
		{"registry.agentmesh.io/opentools/sentiment-api", "registry.agentmesh.io", "opentools", "sentiment-api", false},
		{"local-node.example.com/johndoe/my-bot", "local-node.example.com", "johndoe", "my-bot", false},

		// With qualifiers (stripped by ParseFQAN)
		{"dns01.zynd.ai/acme-corp/doc-translator@2.1.0", "dns01.zynd.ai", "acme-corp", "doc-translator", false},
		{"dns01.zynd.ai/acme-corp/doc-translator#nlp.translation", "dns01.zynd.ai", "acme-corp", "doc-translator", false},
		{"dns01.zynd.ai/acme-corp/doc-translator@2.1.0#nlp.translation", "dns01.zynd.ai", "acme-corp", "doc-translator", false},

		// With agdns:// scheme
		{"agdns://dns01.zynd.ai/acme-corp/doc-translator", "dns01.zynd.ai", "acme-corp", "doc-translator", false},

		// Invalid
		{"", "", "", "", true},
		{"no-slashes", "", "", "", true},
		{"one/slash", "", "", "", true},
		{"host/AB/agent", "", "", "", true},  // invalid dev handle (uppercase)
	}
	for _, tt := range tests {
		host, dev, agent, err := ParseFQAN(tt.fqan)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseFQAN(%q) returned nil error, want error", tt.fqan)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseFQAN(%q) returned error: %v", tt.fqan, err)
			continue
		}
		if host != tt.host || dev != tt.dev || agent != tt.agent {
			t.Errorf("ParseFQAN(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.fqan, host, dev, agent, tt.host, tt.dev, tt.agent)
		}
	}
}

func TestParseQualifiedFQAN(t *testing.T) {
	tests := []struct {
		input      string
		base       string
		version    string
		capability string
		wantErr    bool
	}{
		{"dns01.zynd.ai/acme-corp/doc-translator", "dns01.zynd.ai/acme-corp/doc-translator", "", "", false},
		{"dns01.zynd.ai/acme-corp/doc-translator@2.1.0", "dns01.zynd.ai/acme-corp/doc-translator", "2.1.0", "", false},
		{"dns01.zynd.ai/acme-corp/doc-translator@2", "dns01.zynd.ai/acme-corp/doc-translator", "2", "", false},
		{"dns01.zynd.ai/acme-corp/doc-translator#nlp.translation", "dns01.zynd.ai/acme-corp/doc-translator", "", "nlp.translation", false},
		{"dns01.zynd.ai/acme-corp/doc-translator@2.1.0#nlp.translation", "dns01.zynd.ai/acme-corp/doc-translator", "2.1.0", "nlp.translation", false},
		{"agdns://dns01.zynd.ai/acme-corp/doc-translator@2.1.0", "dns01.zynd.ai/acme-corp/doc-translator", "2.1.0", "", false},

		// Invalid
		{"", "", "", "", true},
		{"host/dev/agent@", "", "", "", true},  // empty version
		{"host/dev/agent#", "", "", "", true},  // empty capability
	}
	for _, tt := range tests {
		base, ver, cap, err := ParseQualifiedFQAN(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseQualifiedFQAN(%q) returned nil error, want error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseQualifiedFQAN(%q) returned error: %v", tt.input, err)
			continue
		}
		if base != tt.base || ver != tt.version || cap != tt.capability {
			t.Errorf("ParseQualifiedFQAN(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.input, base, ver, cap, tt.base, tt.version, tt.capability)
		}
	}
}

func TestBuildAgdnsURI(t *testing.T) {
	uri := BuildAgdnsURI("dns01.zynd.ai/acme-corp/doc-translator")
	expected := "agdns://dns01.zynd.ai/acme-corp/doc-translator"
	if uri != expected {
		t.Errorf("BuildAgdnsURI() = %q, want %q", uri, expected)
	}
}

func TestIsReservedHandle(t *testing.T) {
	if !IsReservedHandle("zynd") {
		t.Error("expected zynd to be reserved")
	}
	if IsReservedHandle("acme-corp") {
		t.Error("expected acme-corp to not be reserved")
	}
}
