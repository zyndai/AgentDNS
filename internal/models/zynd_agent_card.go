package models

// ZyndAgentCard is the new Zynd-native agent card format served at /.well-known/zynd-agent.json.
// It separates identity (zynd), metadata (agent), endpoints, protocol-specific info, and trust.
type ZyndAgentCard struct {
	Zynd      ZyndCardSection      `json:"zynd"`
	Agent     AgentCardSection     `json:"agent"`
	Endpoints EndpointsCardSection `json:"endpoints"`
	Protocols ProtocolsSection     `json:"protocols"`
	Trust     TrustCardSection     `json:"trust,omitempty"`
}

// ZyndCardSection contains Zynd-specific identity and registry info.
type ZyndCardSection struct {
	Version         string `json:"version"`                    // "1.0"
	FQAN            string `json:"fqan,omitempty"`             // e.g., "dns01.zynd.ai/acme-corp/doc-translator"
	AgentID         string `json:"agent_id"`                   // agdns:<hash>
	DeveloperID     string `json:"developer_id,omitempty"`     // agdns:dev:<hash>
	DeveloperHandle string `json:"developer_handle,omitempty"` // e.g., "acme-corp"
	PublicKey       string `json:"public_key"`                 // ed25519:<base64>
	HomeRegistry    string `json:"home_registry"`              // e.g., "dns01.zynd.ai"
	SignedAt        string `json:"signed_at"`
	Signature       string `json:"signature"`
}

// AgentCardSection contains protocol-neutral agent metadata.
type AgentCardSection struct {
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Version      string           `json:"version,omitempty"`
	Category     string           `json:"category"`
	Tags         []string         `json:"tags,omitempty"`
	Capabilities []CardCapability `json:"capabilities,omitempty"`
}

// CardCapability describes a single agent capability in the card.
type CardCapability struct {
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
}

// EndpointsCardSection provides base URL and health check.
type EndpointsCardSection struct {
	BaseURL string `json:"base_url"`
	Health  string `json:"health,omitempty"`
}

// ProtocolsSection maps protocol names to their native metadata.
type ProtocolsSection struct {
	A2A  *A2AProtocol  `json:"a2a,omitempty"`
	MCP  *MCPProtocol  `json:"mcp,omitempty"`
	REST *RESTProtocol `json:"rest,omitempty"`
}

// A2AProtocol describes A2A-specific metadata.
type A2AProtocol struct {
	Version string     `json:"version"`
	CardURL string     `json:"card_url,omitempty"` // points to /.well-known/agent-card.json
	Skills  []A2ASkill `json:"skills,omitempty"`
}

// A2ASkill describes an A2A skill.
type A2ASkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// MCPProtocol describes MCP-specific metadata.
type MCPProtocol struct {
	Version   string    `json:"version"`
	Transport string    `json:"transport"` // "streamable-http", "stdio"
	Endpoint  string    `json:"endpoint"`
	Tools     []MCPTool `json:"tools,omitempty"`
}

// MCPTool describes an MCP tool.
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// RESTProtocol describes REST-specific metadata.
type RESTProtocol struct {
	OpenAPIURL string `json:"openapi_url,omitempty"`
	Invoke     string `json:"invoke,omitempty"`
	InvokeAsync string `json:"invoke_async,omitempty"`
}

// TrustCardSection surfaces trust/reputation data.
type TrustCardSection struct {
	TrustScore       float64 `json:"trust_score"`
	VerificationTier string  `json:"verification_tier,omitempty"`
	ZTPCount         int     `json:"ztp_count,omitempty"`
}
