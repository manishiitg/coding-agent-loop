package oauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// TokenStore manages persistent storage of OAuth tokens
type TokenStore struct {
	filePath string
	mu       sync.RWMutex
}

// NewTokenStore creates a new token store for the given file path
func NewTokenStore(filePath string) *TokenStore {
	// Expand ~ to home directory
	if strings.HasPrefix(filePath, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			filePath = filepath.Join(home, filePath[2:])
		}
	}

	return &TokenStore{
		filePath: filePath,
	}
}

// Save saves a token to the file with secure permissions (0600)
func (ts *TokenStore) Save(token *oauth2.Token) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(ts.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	// Marshal token to JSON
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Write with secure permissions (owner read/write only)
	//nolint:gosec // G306: We intentionally set 0600 permissions for security
	if err := os.WriteFile(ts.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// Load loads a token from the file
func (ts *TokenStore) Load() (*oauth2.Token, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Check if file exists
	if _, err := os.Stat(ts.filePath); os.IsNotExist(err) {
		return nil, ErrTokenFileNotFound
	}

	// Read file
	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	// Unmarshal token
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTokenFile, err)
	}

	return &token, nil
}

// Delete removes the token file
func (ts *TokenStore) Delete() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if err := os.Remove(ts.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}

	return nil
}

// IsValid checks if a stored token exists and is valid (not expired)
func (ts *TokenStore) IsValid() bool {
	token, err := ts.Load()
	if err != nil {
		return false
	}

	return token.Valid()
}

// ExpiresIn returns the duration until token expiry, or 0 if expired/invalid
// Returns a special large duration for tokens with no expiry (zero time)
func (ts *TokenStore) ExpiresIn() time.Duration {
	token, err := ts.Load()
	if err != nil {
		return 0
	}

	if !token.Valid() {
		return 0
	}

	// Check for zero expiry time (means token never expires)
	// Go's zero time is "0001-01-01 00:00:00 +0000 UTC"
	if token.Expiry.IsZero() {
		// Return a large duration indicating "never expires"
		// Using 100 years as a symbolic "forever" value
		return 100 * 365 * 24 * time.Hour
	}

	return time.Until(token.Expiry)
}

// GetFilePath returns the token file path
func (ts *TokenStore) GetFilePath() string {
	return ts.filePath
}
