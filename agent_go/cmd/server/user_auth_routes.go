package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// LoginRequest represents a user login request
type LoginRequest struct {
	Provider string `json:"provider,omitempty"` // Provider name: "simple", "cognito", "supabase"
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse represents the response for login/register operations
type AuthResponse struct {
	Token string   `json:"token"`
	User  UserInfo `json:"user"`
}

// UserInfo represents public user information
type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// AuthModeResponse represents the response for GET /api/auth/mode
type AuthModeResponse struct {
	MultiUserMode       bool           `json:"multi_user_mode"`
	RegistrationEnabled bool           `json:"registration_enabled"`
	Providers           []ProviderInfo `json:"providers"`
}

// MultiProviderOAuthStartRequest represents a request to start OAuth flow for multi-provider auth
type MultiProviderOAuthStartRequest struct {
	Provider    string `json:"provider"`
	RedirectURI string `json:"redirect_uri"`
}

// MultiProviderOAuthStartResponse represents the response for starting OAuth flow
type MultiProviderOAuthStartResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// OAuth state management for CSRF protection
var (
	oauthStates     = make(map[string]*oauthStateEntry)
	oauthStatesMu   sync.RWMutex
	stateExpiration = 10 * time.Minute
)

type oauthStateEntry struct {
	Provider    string
	RedirectURI string
	CreatedAt   time.Time
}

func init() {
	// Start cleanup goroutine for expired states
	go cleanupExpiredStates()
}

func cleanupExpiredStates() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		oauthStatesMu.Lock()
		now := time.Now()
		for state, entry := range oauthStates {
			if now.Sub(entry.CreatedAt) > stateExpiration {
				delete(oauthStates, state)
			}
		}
		oauthStatesMu.Unlock()
	}
}

func generateState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func storeState(state, provider, redirectURI string) {
	oauthStatesMu.Lock()
	defer oauthStatesMu.Unlock()
	oauthStates[state] = &oauthStateEntry{
		Provider:    provider,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	}
}

func validateAndConsumeState(state string) (*oauthStateEntry, bool) {
	oauthStatesMu.Lock()
	defer oauthStatesMu.Unlock()
	entry, ok := oauthStates[state]
	if !ok {
		return nil, false
	}
	if time.Since(entry.CreatedAt) > stateExpiration {
		delete(oauthStates, state)
		return nil, false
	}
	delete(oauthStates, state)
	return entry, true
}

// UserAuthRoutes registers authentication routes
func UserAuthRoutes(router http.Handler, api *StreamingAPI) {
	// Routes are registered in server.go using the handler functions below
}

// handleRegister handles user registration
// Registration is always disabled - users are managed via AUTH_USERS env var
func (api *StreamingAPI) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, `{"error": "Registration is disabled - users are configured via AUTH_USERS environment variable"}`, http.StatusForbidden)
}

// handleLogin handles user login (for credentials-based providers)
func (api *StreamingAPI) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// In single-user mode, return a token for the default user (no login required)
	if !IsMultiUserMode() {
		token, _ := GenerateJWT(GetDefaultUserID(), "user", "")
		json.NewEncoder(w).Encode(AuthResponse{
			Token: token,
			User: UserInfo{
				ID:       GetDefaultUserID(),
				Username: "user",
			},
		})
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error": "Username and password are required"}`, http.StatusBadRequest)
		return
	}

	// Determine which provider to use
	providerName := req.Provider
	if providerName == "" {
		providerName = "simple" // Default to simple provider
	}

	provider, ok := GetProvider(providerName)
	if !ok {
		http.Error(w, `{"error": "Unknown authentication provider"}`, http.StatusBadRequest)
		return
	}

	if !provider.IsConfigured() {
		log.Printf("[AUTH] Provider %s is not configured", providerName)
		http.Error(w, fmt.Sprintf(`{"error": "Provider %s is not configured"}`, providerName), http.StatusInternalServerError)
		return
	}

	// Validate credentials using the provider
	extUser, err := provider.ValidateCredentials(req.Username, req.Password)
	if err != nil {
		log.Printf("[AUTH] Login failed for user %s via provider %s: %v", req.Username, providerName, err)
		http.Error(w, `{"error": "Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Generate a deterministic user ID if not provided
	userID := extUser.ExternalID
	if userID == "" {
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", extUser.Provider, extUser.Username)))
		userID = hex.EncodeToString(hash[:16])
	}

	// Generate JWT token with provider information
	token, err := GenerateJWTWithProvider(userID, extUser.Username, extUser.Email, extUser.Provider)
	if err != nil {
		log.Printf("[AUTH] Failed to generate token: %v", err)
		http.Error(w, `{"error": "Failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[AUTH] User %s logged in successfully via provider %s", extUser.Username, extUser.Provider)
	json.NewEncoder(w).Encode(AuthResponse{
		Token: token,
		User: UserInfo{
			ID:       userID,
			Username: extUser.Username,
			Email:    extUser.Email,
			Provider: extUser.Provider,
		},
	})
}

// handleAuthStart initiates OAuth flow for a provider
func (api *StreamingAPI) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !IsMultiUserMode() {
		http.Error(w, `{"error": "OAuth not available in single-user mode"}`, http.StatusBadRequest)
		return
	}

	var req MultiProviderOAuthStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Provider == "" {
		http.Error(w, `{"error": "Provider is required"}`, http.StatusBadRequest)
		return
	}

	if req.RedirectURI == "" {
		http.Error(w, `{"error": "Redirect URI is required"}`, http.StatusBadRequest)
		return
	}

	provider, ok := GetProvider(req.Provider)
	if !ok {
		http.Error(w, `{"error": "Unknown authentication provider"}`, http.StatusBadRequest)
		return
	}

	if provider.Type() != "oauth" {
		http.Error(w, `{"error": "Provider does not support OAuth flow"}`, http.StatusBadRequest)
		return
	}

	if !provider.IsConfigured() {
		http.Error(w, fmt.Sprintf(`{"error": "Provider %s is not configured"}`, req.Provider), http.StatusInternalServerError)
		return
	}

	// Generate and store state for CSRF protection
	state := generateState()
	storeState(state, req.Provider, req.RedirectURI)

	// Get the OAuth authorization URL
	authURL := provider.GetAuthURL(state, req.RedirectURI)
	if authURL == "" {
		http.Error(w, `{"error": "Failed to generate authorization URL"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[AUTH] OAuth flow started for provider %s", req.Provider)
	json.NewEncoder(w).Encode(MultiProviderOAuthStartResponse{
		AuthURL: authURL,
		State:   state,
	})
}

// handleAuthCallback handles OAuth callback from provider
func (api *StreamingAPI) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get query parameters
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		errorDesc := r.URL.Query().Get("error_description")
		log.Printf("[AUTH] OAuth callback error: %s - %s", errorParam, errorDesc)
		http.Error(w, fmt.Sprintf(`{"error": "%s", "error_description": "%s"}`, errorParam, errorDesc), http.StatusBadRequest)
		return
	}

	if code == "" {
		http.Error(w, `{"error": "Authorization code is required"}`, http.StatusBadRequest)
		return
	}

	if state == "" {
		http.Error(w, `{"error": "State parameter is required"}`, http.StatusBadRequest)
		return
	}

	// Validate and consume state
	stateEntry, valid := validateAndConsumeState(state)
	if !valid {
		http.Error(w, `{"error": "Invalid or expired state parameter"}`, http.StatusBadRequest)
		return
	}

	provider, ok := GetProvider(stateEntry.Provider)
	if !ok {
		http.Error(w, `{"error": "Unknown authentication provider"}`, http.StatusBadRequest)
		return
	}

	// Exchange code for user info
	extUser, err := provider.ExchangeCode(r.Context(), code, stateEntry.RedirectURI)
	if err != nil {
		log.Printf("[AUTH] Failed to exchange code for provider %s: %v", stateEntry.Provider, err)
		http.Error(w, `{"error": "Failed to authenticate with provider"}`, http.StatusInternalServerError)
		return
	}

	// Generate a deterministic user ID if not provided
	userID := extUser.ExternalID
	if userID == "" {
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", extUser.Provider, extUser.Username)))
		userID = hex.EncodeToString(hash[:16])
	}

	// Generate JWT token with provider information
	token, err := GenerateJWTWithProvider(userID, extUser.Username, extUser.Email, extUser.Provider)
	if err != nil {
		log.Printf("[AUTH] Failed to generate token: %v", err)
		http.Error(w, `{"error": "Failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[AUTH] User %s authenticated via OAuth provider %s", extUser.Username, extUser.Provider)
	json.NewEncoder(w).Encode(AuthResponse{
		Token: token,
		User: UserInfo{
			ID:       userID,
			Username: extUser.Username,
			Email:    extUser.Email,
			Provider: extUser.Provider,
		},
	})
}

// handleLogout handles user logout
// Since we use stateless JWTs, logout is handled client-side by clearing the token
func (api *StreamingAPI) handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Logged out successfully",
	})
}

// handleGetCurrentUser returns the current authenticated user's info
func (api *StreamingAPI) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user := GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Not authenticated"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UserInfo{
		ID:       user.UserID,
		Username: user.Username,
		Email:    user.Email,
		Provider: user.Provider,
	})
}

// handleGetAuthMode returns the current authentication mode and available providers
func (api *StreamingAPI) handleGetAuthMode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get base URL for generating OAuth URLs
	baseURL := getBaseURL(r)

	response := AuthModeResponse{
		MultiUserMode:       IsMultiUserMode(),
		RegistrationEnabled: false, // Registration is always disabled - use providers
		Providers:           GetProviderInfoList(baseURL),
	}

	json.NewEncoder(w).Encode(response)
}

// getBaseURL extracts the base URL from the request for generating callback URLs
func getBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	// Check for X-Forwarded-Proto header (common behind reverse proxies)
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
