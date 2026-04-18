package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ContextKey type for context values
type ContextKey string

// UserContextKey is the key for user claims in context
const UserContextKey ContextKey = "user"

// UserClaims represents the JWT claims for authenticated users
type UserClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Provider string `json:"provider,omitempty"` // Auth provider: "simple", "cognito", "supabase"
	jwt.RegisteredClaims
}

// HardcodedUser represents a user defined in environment variables
type HardcodedUser struct {
	Username string
	Password string // Plain text password from env
	UserID   string // Generated from username
}

var (
	hardcodedUsers     map[string]*HardcodedUser
	hardcodedUsersOnce sync.Once
)

// IsMultiUserMode returns true if MULTI_USER_MODE environment variable is set to "true"
func IsMultiUserMode() bool {
	return os.Getenv("MULTI_USER_MODE") == "true"
}

// IsLocalMode returns true if LOCAL_MODE environment variable is set to "true".
// When local mode is enabled, features like CDP browser connection are available.
func IsLocalMode() bool {
	return os.Getenv("LOCAL_MODE") == "true"
}

// IsHardcodedUserMode returns true if AUTH_USERS environment variable is set
// When hardcoded users are configured, registration is disabled
func IsHardcodedUserMode() bool {
	return os.Getenv("AUTH_USERS") != ""
}

// GetHardcodedUsers parses AUTH_USERS env var and returns the user map
// Format: AUTH_USERS=user1:pass1,user2:pass2
func GetHardcodedUsers() map[string]*HardcodedUser {
	hardcodedUsersOnce.Do(func() {
		hardcodedUsers = make(map[string]*HardcodedUser)
		usersEnv := os.Getenv("AUTH_USERS")
		if usersEnv == "" {
			return
		}

		pairs := strings.Split(usersEnv, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) != 2 {
				log.Printf("[AUTH] Invalid AUTH_USERS format for entry: %s (expected user:pass)", pair)
				continue
			}
			username := strings.TrimSpace(parts[0])
			password := strings.TrimSpace(parts[1])
			if username == "" || password == "" {
				log.Printf("[AUTH] Empty username or password in AUTH_USERS entry")
				continue
			}

			// Generate a deterministic user ID from username
			hash := sha256.Sum256([]byte("user:" + username))
			userID := hex.EncodeToString(hash[:16])

			hardcodedUsers[username] = &HardcodedUser{
				Username: username,
				Password: password,
				UserID:   userID,
			}
			log.Printf("[AUTH] Loaded hardcoded user: %s (id: %s)", username, userID)
		}

		if len(hardcodedUsers) > 0 {
			log.Printf("[AUTH] Hardcoded user mode enabled with %d users", len(hardcodedUsers))
		}
	})
	return hardcodedUsers
}

// ValidateHardcodedUser checks if username/password match a hardcoded user
// Returns the user if valid, nil otherwise
func ValidateHardcodedUser(username, password string) *HardcodedUser {
	users := GetHardcodedUsers()
	if user, ok := users[username]; ok {
		if user.Password == password {
			return user
		}
	}
	return nil
}

// GetDefaultUserID returns the default user ID for single-user mode
// Uses DEFAULT_USER_ID env var or falls back to "default-user"
func GetDefaultUserID() string {
	if id := os.Getenv("DEFAULT_USER_ID"); id != "" {
		return id
	}
	return "default"
}

// GetAuthSecret returns the JWT signing secret
func GetAuthSecret() []byte {
	secret := os.Getenv("AUTH_SECRET")
	if secret == "" {
		// Default secret for development - should be set in production
		secret = "dev-secret-change-in-production"
		log.Printf("[AUTH] WARNING: Using default AUTH_SECRET - set AUTH_SECRET env var in production")
	}
	return []byte(secret)
}

// AuthMiddleware handles JWT authentication for API routes
// In single-user mode, it injects the default user ID
// In multi-user mode, it validates JWT tokens
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for certain paths
		if shouldSkipAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// In single-user mode, use the hardcoded DEFAULT_USER_ID
		if !IsMultiUserMode() {
			ctx := context.WithValue(r.Context(), UserContextKey, &UserClaims{
				UserID:   GetDefaultUserID(),
				Username: "user",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Multi-user mode: require JWT authentication
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// Fallback: check query parameter (needed for EventSource/SSE which can't set headers)
			if qToken := r.URL.Query().Get("token"); qToken != "" {
				authHeader = "Bearer " + qToken
			}
		}
		if authHeader == "" {
			http.Error(w, `{"error": "Authorization header required"}`, http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error": "Invalid authorization header format"}`, http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims := &UserClaims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			// Validate signing method
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return GetAuthSecret(), nil
		})

		if err != nil {
			log.Printf("[AUTH] Token validation failed: %v", err)
			http.Error(w, `{"error": "Invalid or expired token"}`, http.StatusUnauthorized)
			return
		}

		if !token.Valid {
			http.Error(w, `{"error": "Invalid token"}`, http.StatusUnauthorized)
			return
		}

		// Token is valid, add claims to context
		ctx := context.WithValue(r.Context(), UserContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// shouldSkipAuth returns true for paths that don't require authentication
func shouldSkipAuth(path string) bool {
	// Public endpoints that don't require auth
	publicPaths := []string{
		"/api/auth/login",
		"/api/auth/register",
		"/api/auth/mode",
		"/api/auth/start",     // OAuth flow initiation (must be public)
		"/api/auth/callback",  // Multi-provider auth callback
		"/api/auth/providers", // Get available auth providers
		"/api/health",
		"/api/capabilities",
		"/api/shared/",        // Shared session links are public
		"/api/oauth/callback", // OAuth callback comes from external provider without our JWT
		"/api/downloads/",     // Chrome CDP launcher zip (macOS)
	}

	for _, p := range publicPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}

	// Static files don't need auth
	if !strings.HasPrefix(path, "/api/") {
		return true
	}

	return false
}

// GetUserFromContext extracts user claims from the request context
// Returns nil if no user is found (should not happen with middleware applied)
func GetUserFromContext(ctx context.Context) *UserClaims {
	if user, ok := ctx.Value(UserContextKey).(*UserClaims); ok {
		return user
	}
	return nil
}

// GetUserIDFromContext is a convenience function to get just the user ID
// Returns the default user ID if no user is found in context
func GetUserIDFromContext(ctx context.Context) string {
	user := GetUserFromContext(ctx)
	if user != nil {
		return user.UserID
	}
	return GetDefaultUserID()
}

// GenerateJWT creates a new JWT token for a user
func GenerateJWT(userID, username, email string) (string, error) {
	return GenerateJWTWithProvider(userID, username, email, "")
}

// GenerateJWTWithProvider creates a new JWT token for a user with provider information
func GenerateJWTWithProvider(userID, username, email, provider string) (string, error) {
	claims := &UserClaims{
		UserID:   userID,
		Username: username,
		Email:    email,
		Provider: provider,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)), // 7 day expiry
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "mcp-agent-builder",
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(GetAuthSecret())
}
