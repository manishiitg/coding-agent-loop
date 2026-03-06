package oauth

// OAuthConfig defines the OAuth 2.1 configuration for MCP servers
type OAuthConfig struct {
	// Auto-discovery settings
	AutoDiscover bool `json:"auto_discover,omitempty"` // Auto-discover endpoints from 401 responses

	// OAuth endpoints (required if not using auto-discovery)
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"` // Optional for public clients (PKCE)
	AuthURL      string   `json:"auth_url,omitempty"`
	TokenURL     string   `json:"token_url,omitempty"`
	RedirectURL  string   `json:"redirect_url,omitempty"` // Default: http://localhost:8080/callback
	Scopes       []string `json:"scopes,omitempty"`

	// RFC 8707 Resource Indicator
	Resource string `json:"resource,omitempty"` // Resource URI for token audience restriction

	// Security & Storage
	UsePKCE   bool   `json:"use_pkce,omitempty"`   // Default: true (recommended)
	TokenFile string `json:"token_file,omitempty"` // Path to cache tokens
}

// SetDefaults sets default values for optional fields
func (c *OAuthConfig) SetDefaults() {
	if c.RedirectURL == "" {
		c.RedirectURL = "http://localhost:8080/callback"
	}

	// Always use PKCE for security unless explicitly disabled
	if !c.UsePKCE {
		c.UsePKCE = true
	}

	if c.TokenFile == "" {
		c.TokenFile = "~/.config/mcpagent/tokens/default.json"
	}
}

// Validate checks if the OAuth configuration is valid
func (c *OAuthConfig) Validate() error {
	// If not auto-discovering, require endpoints
	if !c.AutoDiscover {
		if c.AuthURL == "" {
			return ErrMissingAuthURL
		}
		if c.TokenURL == "" {
			return ErrMissingTokenURL
		}
	}

	return nil
}

// OAuthEndpoints contains discovered OAuth endpoint URLs
type OAuthEndpoints struct {
	AuthURL              string   // Authorization endpoint
	TokenURL             string   // Token endpoint
	Issuer               string   // Issuer URL (optional)
	RegistrationEndpoint string   // Dynamic Client Registration endpoint (optional)
	Resource             string   // RFC 8707 resource indicator (from Protected Resource Metadata)
	ScopesSupported      []string // Scopes supported (from Protected Resource Metadata)
}
