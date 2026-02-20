package vertex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/interfaces"

	"cloud.google.com/go/auth/credentials"
)

// TokenCache manages OAuth token caching with expiration
type TokenCache struct {
	token     string
	expiresAt time.Time
	mu        sync.RWMutex
}

var globalTokenCache = &TokenCache{}

// GetAccessToken retrieves an access token using multiple authentication methods
// Tries in order: gcloud auth, service account, Application Default Credentials
func GetAccessToken(ctx context.Context, logger interfaces.Logger) (string, error) {
	// Check cache first
	globalTokenCache.mu.RLock()
	if globalTokenCache.token != "" && time.Now().Before(globalTokenCache.expiresAt) {
		token := globalTokenCache.token
		globalTokenCache.mu.RUnlock()
		if logger != nil {
			logger.Debugf("Using cached access token (expires at %v)", globalTokenCache.expiresAt)
		}
		return token, nil
	}
	globalTokenCache.mu.RUnlock()

	// Try authentication methods in order
	var token string
	var err error

	// Method 1: Try gcloud auth
	token, err = getGCloudToken(ctx, logger)
	if err == nil && token != "" {
		// Cache the token (gcloud tokens typically expire in 1 hour)
		globalTokenCache.mu.Lock()
		globalTokenCache.token = token
		globalTokenCache.expiresAt = time.Now().Add(55 * time.Minute) // Cache for 55 minutes
		globalTokenCache.mu.Unlock()
		if logger != nil {
			logger.Infof("✅ Authenticated using gcloud auth")
		}
		return token, nil
	}
	if logger != nil {
		logger.Debugf("gcloud auth failed: %v", err)
	}

	// Method 2: Try Application Default Credentials
	token, err = getADCToken(ctx, logger)
	if err == nil && token != "" {
		// Cache the token
		globalTokenCache.mu.Lock()
		globalTokenCache.token = token
		globalTokenCache.expiresAt = time.Now().Add(55 * time.Minute)
		globalTokenCache.mu.Unlock()
		if logger != nil {
			logger.Infof("✅ Authenticated using Application Default Credentials")
		}
		return token, nil
	}
	if logger != nil {
		logger.Debugf("ADC auth failed: %v", err)
	}

	return "", fmt.Errorf("all authentication methods failed. Last error: %w", err)
}

// getGCloudToken retrieves an access token using gcloud CLI
func getGCloudToken(ctx context.Context, logger interfaces.Logger) (string, error) {
	if logger != nil {
		logger.Debugf("Attempting gcloud authentication...")
	}

	// Check if gcloud is available
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gcloud auth failed: %w", err)
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("gcloud returned empty token")
	}

	return token, nil
}

// getServiceAccountToken retrieves an access token using service account JSON
//
//nolint:unused // Reserved for future use with service account authentication
func getServiceAccountToken(ctx context.Context, logger interfaces.Logger) (string, error) {
	if logger != nil {
		logger.Debugf("Attempting service account authentication...")
	}

	// Check for service account path
	serviceAccountPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if serviceAccountPath == "" {
		serviceAccountPath = os.Getenv("VERTEX_SERVICE_ACCOUNT_PATH")
	}
	if serviceAccountPath == "" {
		return "", fmt.Errorf("no service account path found (set GOOGLE_APPLICATION_CREDENTIALS or VERTEX_SERVICE_ACCOUNT_PATH)")
	}

	// Read service account JSON
	data, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return "", fmt.Errorf("failed to read service account file: %w", err)
	}

	var saKey struct {
		Type                string `json:"type"`
		ProjectID           string `json:"project_id"`
		PrivateKeyID        string `json:"private_key_id"`
		PrivateKey          string `json:"private_key"`
		ClientEmail         string `json:"client_email"`
		ClientID            string `json:"client_id"`
		AuthURI             string `json:"auth_uri"`
		TokenURI            string `json:"token_uri"`
		AuthProviderX509URL string `json:"auth_provider_x509_cert_url"`
		ClientX509CertURL   string `json:"client_x509_cert_url"`
	}

	if err := json.Unmarshal(data, &saKey); err != nil {
		return "", fmt.Errorf("failed to parse service account JSON: %w", err)
	}

	// Create credentials from service account
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform"},
		CredentialsJSON: data,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create credentials: %w", err)
	}

	// Get token
	token, err := creds.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	return token.Value, nil
}

// getADCToken retrieves an access token using Application Default Credentials
func getADCToken(ctx context.Context, logger interfaces.Logger) (string, error) {
	if logger != nil {
		logger.Debugf("Attempting Application Default Credentials authentication...")
	}

	// Use Google Cloud auth library to get default credentials
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to detect default credentials: %w", err)
	}

	// Get token
	token, err := creds.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get token from ADC: %w", err)
	}

	return token.Value, nil
}

// ClearTokenCache clears the cached token (useful for testing or forced refresh)
func ClearTokenCache() {
	globalTokenCache.mu.Lock()
	defer globalTokenCache.mu.Unlock()
	globalTokenCache.token = ""
	globalTokenCache.expiresAt = time.Time{}
}
