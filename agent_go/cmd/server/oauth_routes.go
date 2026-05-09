package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/oauth"
)

// OAuthFlowState tracks ongoing OAuth flows
type OAuthFlowState struct {
	ServerName   string
	State        string
	CodeChan     chan string
	ErrChan      chan error
	Manager      *oauth.Manager
	ServerConfig mcpclient.MCPServerConfig // The server config with OAuth settings to persist
}

var (
	oauthFlows   = make(map[string]*OAuthFlowState) // state -> flow
	oauthFlowsMu sync.RWMutex
)

func deriveOAuthRedirectURI(r *http.Request) string {
	if publicURL := os.Getenv("PUBLIC_URL"); publicURL != "" {
		return fmt.Sprintf("%s/api/oauth/callback", strings.TrimRight(publicURL, "/"))
	}

	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
	}

	return fmt.Sprintf("%s://%s/api/oauth/callback", scheme, host)
}

// OAuthLoginRequest represents a request to start OAuth flow
type OAuthLoginRequest struct {
	ServerName string `json:"server_name"`
	ClientID   string `json:"client_id,omitempty"` // User-provided client_id for servers without DCR
}

// OAuthDiscoveryResponse is returned when the server doesn't support DCR and needs a client_id
type OAuthDiscoveryResponse struct {
	Status          string   `json:"status"` // "needs_client_id"
	ServerName      string   `json:"server_name"`
	AuthURL         string   `json:"auth_url,omitempty"`         // Discovered authorization endpoint
	TokenURL        string   `json:"token_url,omitempty"`        // Discovered token endpoint
	Resource        string   `json:"resource,omitempty"`         // RFC 8707 resource indicator
	ScopesSupported []string `json:"scopes_supported,omitempty"` // Discovered scopes
	Message         string   `json:"message"`
}

// OAuthStartResponse represents the response when starting OAuth flow
type OAuthStartResponse struct {
	ServerName string `json:"server_name"`
	AuthURL    string `json:"auth_url"`
	State      string `json:"state"`
	Message    string `json:"message"`
}

// OAuthStatusResponse represents the OAuth token status
type OAuthStatusResponse struct {
	ServerName string `json:"server_name"`
	Valid      bool   `json:"valid"`
	ExpiresIn  string `json:"expires_in"`
	TokenPath  string `json:"token_path"`
}

// OAuthLogoutRequest represents a request to logout (remove token)
type OAuthLogoutRequest struct {
	ServerName string `json:"server_name"`
}

// handleOAuthCallback handles GET /api/oauth/callback - receives OAuth authorization code
func (api *StreamingAPI) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	api.logger.Info(fmt.Sprintf("🔔 OAuth callback received: state=%s, code_present=%v, error=%s",
		r.URL.Query().Get("state"), r.URL.Query().Get("code") != "", r.URL.Query().Get("error")))

	query := r.URL.Query()

	// Get state parameter
	state := query.Get("state")
	if state == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head><title>OAuth Error</title></head>
<body style="font-family: Arial, sans-serif; text-align: center; padding: 50px;">
	<h1>❌ Invalid Request</h1>
	<p>Missing state parameter</p>
	<p>You can close this window.</p>
</body>
</html>`)
		return
	}

	// Find the OAuth flow
	oauthFlowsMu.RLock()
	flow, exists := oauthFlows[state]
	oauthFlowsMu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head><title>OAuth Error</title></head>
<body style="font-family: Arial, sans-serif; text-align: center; padding: 50px;">
	<h1>❌ Invalid or Expired State</h1>
	<p>OAuth flow not found or expired</p>
	<p>You can close this window and try again.</p>
</body>
</html>`)
		return
	}

	// Check for error from OAuth provider
	if errCode := query.Get("error"); errCode != "" {
		errDesc := query.Get("error_description")
		if errDesc == "" {
			errDesc = errCode
		}

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head><title>Authentication Failed</title></head>
<body style="font-family: Arial, sans-serif; text-align: center; padding: 50px;">
	<h1>❌ Authentication Failed</h1>
	<p>%s</p>
	<p>You can close this window.</p>
</body>
</html>`, errDesc)

		// Send error to flow
		select {
		case flow.ErrChan <- fmt.Errorf("OAuth error: %s - %s", errCode, errDesc):
		default:
		}
		return
	}

	// Get authorization code
	code := query.Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head><title>OAuth Error</title></head>
<body style="font-family: Arial, sans-serif; text-align: center; padding: 50px;">
	<h1>❌ Missing Authorization Code</h1>
	<p>No authorization code received</p>
	<p>You can close this window.</p>
</body>
</html>`)
		return
	}

	// Send code to flow
	select {
	case flow.CodeChan <- code:
		// Success - show nice page
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
	<title>Authentication Successful</title>
	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
			display: flex;
			align-items: center;
			justify-content: center;
			min-height: 100vh;
			margin: 0;
			background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
		}
		.container {
			background: white;
			border-radius: 16px;
			padding: 48px;
			box-shadow: 0 20px 60px rgba(0,0,0,0.3);
			text-align: center;
			max-width: 400px;
		}
		.success-icon {
			font-size: 64px;
			margin-bottom: 24px;
		}
		h1 {
			color: #2d3748;
			margin: 0 0 16px 0;
			font-size: 24px;
		}
		p {
			color: #718096;
			margin: 0;
			font-size: 16px;
		}
	</style>
</head>
<body>
	<div class="container">
		<div class="success-icon">✅</div>
		<h1>Authentication Successful!</h1>
		<p>You can close this window and return to the application.</p>
	</div>
</body>
</html>`)
	default:
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head><title>OAuth Error</title></head>
<body style="font-family: Arial, sans-serif; text-align: center; padding: 50px;">
	<h1>❌ Internal Error</h1>
	<p>Failed to process authorization code</p>
	<p>You can close this window and try again.</p>
</body>
</html>`)
	}
}

// handleOAuthStart handles POST /api/oauth/start - initiates OAuth flow
func (api *StreamingAPI) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	var req OAuthLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.ServerName == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}

	// Get user ID from context for per-user token storage
	userID := GetUserIDFromContext(r.Context())
	api.logger.Info(fmt.Sprintf("🔐 OAuth start for server %s, user %s", req.ServerName, userID))

	// Ensure user token directory exists
	if _, err := ensureUserTokenDir(userID); err != nil {
		api.logger.Error(fmt.Sprintf("Failed to create user token directory: %v", err), err)
		http.Error(w, "Failed to create token directory", http.StatusInternalServerError)
		return
	}

	// Load server config
	config, err := mcpclient.LoadMergedConfig(api.mcpConfigPath, api.logger)
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to load config: %v", err), err)
		http.Error(w, "Failed to load server config", http.StatusInternalServerError)
		return
	}

	serverConfig, err := config.GetServer(req.ServerName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Server '%s' not found", req.ServerName), http.StatusNotFound)
		return
	}

	// Derive redirect URI from PUBLIC_URL env var (for production) or incoming request (for local)
	redirectURI := deriveOAuthRedirectURI(r)

	// Use per-user token file path
	userTokenFile := getUserTokenFilePath(userID, req.ServerName)

	// Apply user-provided client_id if present (from the "needs_client_id" flow)
	if req.ClientID != "" {
		if serverConfig.OAuth == nil {
			serverConfig.OAuth = &oauth.OAuthConfig{
				AutoDiscover: true,
				UsePKCE:      true,
				TokenFile:    userTokenFile,
			}
		}
		serverConfig.OAuth.ClientID = req.ClientID
		api.logger.Info(fmt.Sprintf("Using user-provided client_id for %s: %s", req.ServerName, req.ClientID))
	}

	// Initialize OAuth config if not present
	// Also support mcp-remote pattern: command=npx, args=["mcp-remote", "<url>"]
	effectiveURL := serverConfig.URL
	if effectiveURL == "" {
		effectiveURL = extractMCPRemoteURL(serverConfig.Command, serverConfig.Args)
	}
	if serverConfig.OAuth == nil {
		if effectiveURL == "" {
			http.Error(w, fmt.Sprintf("Server '%s' does not have OAuth configured and no URL for auto-discovery", req.ServerName), http.StatusBadRequest)
			return
		}
		serverConfig.OAuth = &oauth.OAuthConfig{
			AutoDiscover: true,
			UsePKCE:      true,
			TokenFile:    userTokenFile,
		}
	}

	// Always update TokenFile to user-specific path
	serverConfig.OAuth.TokenFile = userTokenFile

	// Always use the current callback URL for a new flow. User configs can
	// contain stale localhost ports from previous runs, which otherwise get
	// sent to the OAuth provider and make the callback unreachable.
	serverConfig.OAuth.RedirectURL = redirectURI

	// Track discovered info for the "needs_client_id" response
	var discoveredResource string
	var discoveredScopes []string

	// Auto-discover endpoints if needed (auto_discover is true OR endpoints are missing)
	needsDiscovery := serverConfig.OAuth.AutoDiscover || serverConfig.OAuth.AuthURL == "" || serverConfig.OAuth.TokenURL == ""
	if needsDiscovery && effectiveURL != "" {
		api.logger.Info(fmt.Sprintf("Auto-discovering OAuth endpoints for %s", req.ServerName))

		// For mcp-remote servers, temporarily set URL so discovery client can reach the remote
		if serverConfig.URL == "" && effectiveURL != "" {
			serverConfig.URL = effectiveURL
		}

		metadata, err := oauth.FetchAuthServerMetadata(effectiveURL)
		if err != nil {
			api.logger.Warn(fmt.Sprintf("RFC 8414 discovery failed: %v", err))
			// Fallback to 401 response discovery
			// This also handles RFC 9728 Protected Resource Metadata (resource_metadata parameter)
			client := mcpclient.New(serverConfig, api.logger)
			endpoints, err := client.DiscoverOAuthEndpoints(context.Background())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to auto-discover OAuth endpoints: %v", err), http.StatusInternalServerError)
				return
			}
			serverConfig.OAuth.AuthURL = endpoints.AuthURL
			serverConfig.OAuth.TokenURL = endpoints.TokenURL
			api.logger.Info(fmt.Sprintf("Discovered OAuth endpoints from 401 - Auth: %s, Token: %s", endpoints.AuthURL, endpoints.TokenURL))

			// Capture Resource and Scopes from discovery
			if endpoints.Resource != "" {
				discoveredResource = endpoints.Resource
				serverConfig.OAuth.Resource = endpoints.Resource
				api.logger.Info(fmt.Sprintf("Discovered resource for %s: %s", req.ServerName, endpoints.Resource))
			}
			if len(endpoints.ScopesSupported) > 0 {
				discoveredScopes = endpoints.ScopesSupported
				// Apply discovered scopes if none configured
				if len(serverConfig.OAuth.Scopes) == 0 {
					serverConfig.OAuth.Scopes = endpoints.ScopesSupported
					api.logger.Info(fmt.Sprintf("Applied discovered scopes for %s: %v", req.ServerName, endpoints.ScopesSupported))
				}
			}

			// Perform Dynamic Client Registration if no client_id and discovered endpoints have registration endpoint
			if serverConfig.OAuth.ClientID == "" && endpoints.RegistrationEndpoint != "" {
				api.logger.Info(fmt.Sprintf("Performing Dynamic Client Registration for %s (via 401 discovery)", req.ServerName))

				regResponse, err := oauth.RegisterClient(endpoints.RegistrationEndpoint, serverConfig.OAuth.RedirectURL)
				if err != nil {
					api.logger.Warn(fmt.Sprintf("Dynamic Client Registration failed: %v", err))
					// Continue without client_id - will prompt user below
				} else {
					serverConfig.OAuth.ClientID = regResponse.ClientID
					serverConfig.OAuth.ClientSecret = regResponse.ClientSecret
					api.logger.Info(fmt.Sprintf("Registered client_id via 401 discovery: %s", regResponse.ClientID))
				}
			}
		} else {
			// Successfully got metadata from .well-known
			serverConfig.OAuth.AuthURL = metadata.AuthorizationEndpoint
			serverConfig.OAuth.TokenURL = metadata.TokenEndpoint
			api.logger.Info(fmt.Sprintf("Discovered OAuth endpoints from .well-known - Auth: %s, Token: %s", metadata.AuthorizationEndpoint, metadata.TokenEndpoint))

			// Capture scopes from auth server metadata
			if len(metadata.ScopesSupported) > 0 {
				discoveredScopes = metadata.ScopesSupported
				if len(serverConfig.OAuth.Scopes) == 0 {
					serverConfig.OAuth.Scopes = metadata.ScopesSupported
					api.logger.Info(fmt.Sprintf("Applied discovered scopes from well-known for %s: %v", req.ServerName, metadata.ScopesSupported))
				}
			}

			// Perform Dynamic Client Registration if no client_id and server supports it
			if serverConfig.OAuth.ClientID == "" && metadata.RegistrationEndpoint != "" {
				api.logger.Info(fmt.Sprintf("Performing Dynamic Client Registration for %s", req.ServerName))

				regResponse, err := oauth.RegisterClient(metadata.RegistrationEndpoint, serverConfig.OAuth.RedirectURL)
				if err != nil {
					api.logger.Warn(fmt.Sprintf("Dynamic Client Registration failed: %v", err))
					// Continue without client_id - will prompt user below
				} else {
					serverConfig.OAuth.ClientID = regResponse.ClientID
					serverConfig.OAuth.ClientSecret = regResponse.ClientSecret
					api.logger.Info(fmt.Sprintf("Registered client_id: %s", regResponse.ClientID))
				}
			}
		}
	}

	// Instead of hard-failing, return a discovery response prompting for client_id
	if serverConfig.OAuth.ClientID == "" {
		api.logger.Info(fmt.Sprintf("No client_id for %s, returning needs_client_id response", req.ServerName))
		discoveryResp := OAuthDiscoveryResponse{
			Status:          "needs_client_id",
			ServerName:      req.ServerName,
			AuthURL:         serverConfig.OAuth.AuthURL,
			TokenURL:        serverConfig.OAuth.TokenURL,
			Resource:        discoveredResource,
			ScopesSupported: discoveredScopes,
			Message:         fmt.Sprintf("Server '%s' does not support Dynamic Client Registration. Please provide your OAuth App client_id.", req.ServerName),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discoveryResp)
		return
	}

	// Create OAuth manager with the fully configured OAuth settings
	oauthMgr := oauth.NewManager(serverConfig.OAuth, api.logger)

	// Generate state and authorization URL using the manager's helper
	state, authURL, err := oauthMgr.GenerateAuthURL()
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to generate auth URL for %s: %v", req.ServerName, err), err)
		http.Error(w, fmt.Sprintf("Failed to generate authorization URL: %v", err), http.StatusInternalServerError)
		return
	}

	// Log the config being stored in flow
	api.logger.Info(fmt.Sprintf("🔐 Storing OAuth config in flow for %s: AuthURL=%s, TokenURL=%s, TokenFile=%s",
		req.ServerName, serverConfig.OAuth.AuthURL, serverConfig.OAuth.TokenURL, serverConfig.OAuth.TokenFile))

	// Register the OAuth flow state
	flow := &OAuthFlowState{
		ServerName:   req.ServerName,
		State:        state,
		CodeChan:     make(chan string, 1),
		ErrChan:      make(chan error, 1),
		Manager:      oauthMgr,
		ServerConfig: serverConfig, // Store server config for persistence after OAuth success
	}

	oauthFlowsMu.Lock()
	oauthFlows[state] = flow
	oauthFlowsMu.Unlock()

	// Clean up flow state after timeout
	go func() {
		time.Sleep(5 * time.Minute)
		oauthFlowsMu.Lock()
		delete(oauthFlows, state)
		oauthFlowsMu.Unlock()
	}()

	// Start OAuth flow in background goroutine
	go func() {
		// Recover from panics so the goroutine doesn't die silently
		defer func() {
			if r := recover(); r != nil {
				api.logger.Error(fmt.Sprintf("🔥 PANIC in OAuth goroutine for %s: %v", req.ServerName, r), fmt.Errorf("%v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		api.logger.Info(fmt.Sprintf("⏳ Waiting for OAuth callback for %s (state: %s)", req.ServerName, state))

		// Wait for authorization code from callback
		var code string
		select {
		case code = <-flow.CodeChan:
			api.logger.Info(fmt.Sprintf("📥 Received authorization code for %s (code length: %d)", req.ServerName, len(code)))
		case err := <-flow.ErrChan:
			api.logger.Error(fmt.Sprintf("OAuth flow failed for %s: %v", req.ServerName, err), err)
			return
		case <-ctx.Done():
			api.logger.Error(fmt.Sprintf("OAuth flow timed out for %s", req.ServerName), ctx.Err())
			return
		}

		// Exchange code for token
		api.logger.Info(fmt.Sprintf("🔄 Exchanging authorization code for token for %s (redirect_uri: %s, token_url: %s)",
			req.ServerName, oauthMgr.GetRedirectURI(), oauthMgr.GetTokenURL()))
		token, err := oauthMgr.ExchangeCodeForToken(ctx, code)
		if err != nil {
			api.logger.Error(fmt.Sprintf("❌ Failed to exchange code for token for %s: %v", req.ServerName, err), err)
			return
		}

		api.logger.Info(fmt.Sprintf("✅ OAuth token obtained for %s, expires: %s, has_refresh: %v",
			req.ServerName, token.Expiry, token.RefreshToken != ""))

		// Check if we can write to the token directory
		tokenFile := flow.ServerConfig.OAuth.TokenFile
		api.logger.Info(fmt.Sprintf("📁 Token file target: %s", tokenFile))

		// Persist OAuth config to the user config file so MCP client uses it for future connections
		api.logger.Info(fmt.Sprintf("💾 Persisting OAuth config for %s", req.ServerName))
		if err := api.persistOAuthConfig(req.ServerName, flow.ServerConfig); err != nil {
			api.logger.Error(fmt.Sprintf("Failed to persist OAuth config for %s: %v", req.ServerName, err), err)
		} else {
			api.logger.Info(fmt.Sprintf("✅ OAuth config persisted for %s", req.ServerName))
		}

		// Invalidate cache for this server so tools are re-discovered with OAuth token
		api.logger.Info(fmt.Sprintf("🔄 Invalidating cache for %s to refresh tools with OAuth", req.ServerName))
		cacheManager := mcpcache.GetCacheManager(api.logger)
		if err := cacheManager.InvalidateByServer(api.mcpConfigPath, req.ServerName); err != nil {
			api.logger.Warn(fmt.Sprintf("Failed to invalidate cache for %s: %v", req.ServerName, err))
		} else {
			api.logger.Info(fmt.Sprintf("✅ Cache invalidated for %s - tools will be refreshed on next request", req.ServerName))
		}

		// Also invalidate the in-memory tool status cache
		api.toolStatusMux.Lock()
		delete(api.toolStatus, req.ServerName)
		api.toolStatusMux.Unlock()
		api.logger.Info(fmt.Sprintf("✅ In-memory tool status cleared for %s", req.ServerName))

		// OAuth success means prior auth-related discovery failures are no
		// longer permanent. Clear the skip marker and rediscover tools now,
		// otherwise /api/tools returns "loading" forever and the frontend keeps
		// polling.
		api.clearDiscoveryFailure(req.ServerName)
		api.appendServerLog(req.ServerName, "info", "Authentication succeeded, rediscovering tools...")
		api.startBackgroundDiscovery()
	}()

	// Return auth URL for the frontend to open
	response := OAuthStartResponse{
		ServerName: req.ServerName,
		AuthURL:    authURL,
		State:      state,
		Message:    "Please authorize in your browser",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleOAuthStatus handles GET /api/oauth/status/:server_name - get token status
func (api *StreamingAPI) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	serverName := r.URL.Query().Get("server_name")
	if serverName == "" {
		http.Error(w, "server_name query parameter is required", http.StatusBadRequest)
		return
	}

	// Get user ID from context for per-user token storage
	userID := GetUserIDFromContext(r.Context())
	userTokenFile := getUserTokenFilePath(userID, serverName)
	api.logger.Info(fmt.Sprintf("🔍 OAuth status check for server %s, user %s, token file: %s", serverName, userID, userTokenFile))

	// Load server config
	config, err := mcpclient.LoadMergedConfig(api.mcpConfigPath, api.logger)
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to load config: %v", err), err)
		http.Error(w, "Failed to load server config", http.StatusInternalServerError)
		return
	}

	serverConfig, err := config.GetServer(serverName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Server '%s' not found", serverName), http.StatusNotFound)
		return
	}

	// If OAuth not configured but server has URL, try auto-discovery
	if serverConfig.OAuth == nil {
		if serverConfig.URL == "" {
			http.Error(w, fmt.Sprintf("Server '%s' does not have OAuth configured and no URL for auto-discovery", serverName), http.StatusBadRequest)
			return
		}

		// Auto-discover OAuth endpoints
		endpoints, err := oauth.DiscoverFromWellKnown(serverConfig.URL)
		if err != nil {
			// Fallback to trying a request and parsing 401 response
			client := mcpclient.New(serverConfig, api.logger)
			endpoints, err = client.DiscoverOAuthEndpoints(context.Background())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to auto-discover OAuth endpoints: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Create default OAuth config for auto-discovered server with user-specific token path
		serverConfig.OAuth = &oauth.OAuthConfig{
			AuthURL:      endpoints.AuthURL,
			TokenURL:     endpoints.TokenURL,
			UsePKCE:      true,
			AutoDiscover: false,
			TokenFile:    userTokenFile,
		}
	}

	// Always set token file to user-specific path for status check
	serverConfig.OAuth.TokenFile = userTokenFile

	// Auto-discover endpoints if needed (for token refresh to work)
	api.logger.Info(fmt.Sprintf("🔍 Checking if auto-discovery needed for %s: AutoDiscover=%v, TokenURL='%s', URL='%s'",
		serverName, serverConfig.OAuth.AutoDiscover, serverConfig.OAuth.TokenURL, serverConfig.URL))
	if serverConfig.OAuth.AutoDiscover || serverConfig.OAuth.TokenURL == "" {
		if serverConfig.URL != "" {
			api.logger.Info(fmt.Sprintf("🔍 Auto-discovering OAuth endpoints for %s from URL: %s", serverName, serverConfig.URL))
			metadata, err := oauth.FetchAuthServerMetadata(serverConfig.URL)
			if err != nil {
				api.logger.Info(fmt.Sprintf("⚠️ RFC 8414 discovery failed for %s: %v, trying 401 discovery", serverName, err))
				// Try 401 response discovery
				client := mcpclient.New(serverConfig, api.logger)
				endpoints, err := client.DiscoverOAuthEndpoints(context.Background())
				if err == nil {
					serverConfig.OAuth.AuthURL = endpoints.AuthURL
					serverConfig.OAuth.TokenURL = endpoints.TokenURL
					api.logger.Info(fmt.Sprintf("✅ Discovered endpoints via 401 for %s: Auth=%s, Token=%s", serverName, endpoints.AuthURL, endpoints.TokenURL))
				} else {
					api.logger.Info(fmt.Sprintf("⚠️ 401 discovery also failed for %s: %v", serverName, err))
				}
			} else {
				serverConfig.OAuth.AuthURL = metadata.AuthorizationEndpoint
				serverConfig.OAuth.TokenURL = metadata.TokenEndpoint
				api.logger.Info(fmt.Sprintf("✅ Discovered endpoints via well-known for %s: Auth=%s, Token=%s", serverName, metadata.AuthorizationEndpoint, metadata.TokenEndpoint))
			}
		}
	}

	// Log the OAuth config for debugging
	api.logger.Info(fmt.Sprintf("📋 OAuth status check for %s - Config: AuthURL=%s, TokenURL=%s, TokenFile=%s",
		serverName, serverConfig.OAuth.AuthURL, serverConfig.OAuth.TokenURL, serverConfig.OAuth.TokenFile))

	// Get token status - this also attempts token refresh if expired
	oauthMgr := oauth.NewManager(serverConfig.OAuth, api.logger)

	// Try to get a valid access token (this will attempt refresh if expired)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accessToken, err := oauthMgr.GetAccessToken(ctx)
	tokenRefreshed := err == nil

	if err != nil {
		api.logger.Info(fmt.Sprintf("⚠️ OAuth token refresh failed for %s: %v", serverName, err))
	} else {
		api.logger.Info(fmt.Sprintf("✅ OAuth token valid/refreshed for %s (token prefix: %s...)", serverName, accessToken[:min(20, len(accessToken))]))
	}

	valid, expiresIn, tokenPath := oauthMgr.GetTokenStatus()

	// If token refresh succeeded, the status should now be valid
	if tokenRefreshed && !valid {
		// Re-check status after refresh
		valid, expiresIn, tokenPath = oauthMgr.GetTokenStatus()
	}

	api.logger.Info(fmt.Sprintf("📊 OAuth status result for %s: valid=%v, expiresIn=%s", serverName, valid, expiresIn))

	response := OAuthStatusResponse{
		ServerName: serverName,
		Valid:      valid,
		ExpiresIn:  expiresIn,
		TokenPath:  tokenPath,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleOAuthLogout handles POST /api/oauth/logout - remove OAuth token
func (api *StreamingAPI) handleOAuthLogout(w http.ResponseWriter, r *http.Request) {
	var req OAuthLogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.ServerName == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}

	// Get user ID from context for per-user token storage
	userID := GetUserIDFromContext(r.Context())
	userTokenFile := getUserTokenFilePath(userID, req.ServerName)
	api.logger.Info(fmt.Sprintf("🔐 OAuth logout for server %s, user %s, token file: %s", req.ServerName, userID, userTokenFile))

	// Load server config
	config, err := mcpclient.LoadMergedConfig(api.mcpConfigPath, api.logger)
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to load config: %v", err), err)
		http.Error(w, "Failed to load server config", http.StatusInternalServerError)
		return
	}

	serverConfig, err := config.GetServer(req.ServerName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Server '%s' not found", req.ServerName), http.StatusNotFound)
		return
	}

	// If OAuth not configured but server has URL, try auto-discovery
	if serverConfig.OAuth == nil {
		if serverConfig.URL == "" {
			http.Error(w, fmt.Sprintf("Server '%s' does not have OAuth configured and no URL for auto-discovery", req.ServerName), http.StatusBadRequest)
			return
		}

		// Auto-discover OAuth endpoints
		endpoints, err := oauth.DiscoverFromWellKnown(serverConfig.URL)
		if err != nil {
			// Fallback to trying a request and parsing 401 response
			client := mcpclient.New(serverConfig, api.logger)
			endpoints, err = client.DiscoverOAuthEndpoints(context.Background())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to auto-discover OAuth endpoints: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Create default OAuth config for auto-discovered server with user-specific token path
		serverConfig.OAuth = &oauth.OAuthConfig{
			AuthURL:      endpoints.AuthURL,
			TokenURL:     endpoints.TokenURL,
			UsePKCE:      true,
			AutoDiscover: false,
			TokenFile:    userTokenFile,
		}
	}

	// Always set token file to user-specific path for logout
	serverConfig.OAuth.TokenFile = userTokenFile

	// Logout (remove token) - only removes this user's token
	oauthMgr := oauth.NewManager(serverConfig.OAuth, api.logger)
	if err := oauthMgr.Logout(); err != nil {
		api.logger.Error(fmt.Sprintf("Failed to logout from %s: %v", req.ServerName, err), err)
		http.Error(w, fmt.Sprintf("Failed to logout: %v", err), http.StatusInternalServerError)
		return
	}

	api.logger.Info(fmt.Sprintf("Successfully logged out user %s from %s", userID, req.ServerName))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": fmt.Sprintf("Successfully logged out from %s", req.ServerName),
	})
}

// persistOAuthConfig saves the OAuth configuration to the user config file
// This ensures future MCP connections will use the OAuth token
func (api *StreamingAPI) persistOAuthConfig(serverName string, serverConfig mcpclient.MCPServerConfig) error {
	// Get the user config path
	userConfigPath := api.getUserConfigPath()
	api.logger.Info(fmt.Sprintf("💾 Persisting OAuth config to: %s", userConfigPath))

	// Log the OAuth config being persisted
	if serverConfig.OAuth != nil {
		api.logger.Info(fmt.Sprintf("💾 OAuth config for %s: AuthURL=%s, TokenURL=%s, TokenFile=%s, ClientID=%s, Resource=%s",
			serverName, serverConfig.OAuth.AuthURL, serverConfig.OAuth.TokenURL, serverConfig.OAuth.TokenFile, serverConfig.OAuth.ClientID, serverConfig.OAuth.Resource))
	}

	// Load existing user config
	userConfig, err := mcpclient.LoadConfig(userConfigPath, api.logger)
	if err != nil {
		api.logger.Info(fmt.Sprintf("💾 User config doesn't exist, creating new: %v", err))
		// File doesn't exist, create new config
		userConfig = &mcpclient.MCPConfig{
			MCPServers: make(map[string]mcpclient.MCPServerConfig),
		}
	} else {
		api.logger.Info(fmt.Sprintf("💾 Loaded existing user config with %d servers", len(userConfig.MCPServers)))
	}

	// Update or add the server with OAuth config
	userConfig.MCPServers[serverName] = serverConfig

	// Save back to user config file
	err = mcpclient.SaveConfig(userConfigPath, userConfig)
	if err != nil {
		api.logger.Error(fmt.Sprintf("💾 Failed to save config: %v", err), err)
	} else {
		api.logger.Info(fmt.Sprintf("💾 Successfully saved OAuth config for %s", serverName))
	}
	return err
}

// getUserConfigPath returns the user config file path (derived from base config path)
func (api *StreamingAPI) getUserConfigPath() string {
	// Replace .json with _user.json
	if len(api.mcpConfigPath) > 5 && api.mcpConfigPath[len(api.mcpConfigPath)-5:] == ".json" {
		return api.mcpConfigPath[:len(api.mcpConfigPath)-5] + "_user.json"
	}
	return api.mcpConfigPath + "_user"
}

// expandPath expands ~ to the user's home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ensureUserTokenDir creates the user-specific token directory if it doesn't exist
// Returns the expanded path to the user's token directory
func ensureUserTokenDir(userID string) (string, error) {
	tokenDir := expandPath(fmt.Sprintf("~/.config/mcpagent/tokens/%s", userID))
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create user token directory: %w", err)
	}
	return tokenDir, nil
}

// getUserTokenFilePath returns the token file path for a specific user and server
func getUserTokenFilePath(userID, serverName string) string {
	return fmt.Sprintf("~/.config/mcpagent/tokens/%s/%s.json", userID, serverName)
}
