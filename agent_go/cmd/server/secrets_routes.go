package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// globalSecretEntry holds a single env-based global secret (name + plaintext value)
type globalSecretEntry struct {
	Name  string
	Value string
}

// globalSecrets is populated once at startup from GLOBAL_SECRET_* env vars
var globalSecrets []globalSecretEntry

// loadGlobalSecrets scans os.Environ() for GLOBAL_SECRET_ prefix and populates globalSecrets
func loadGlobalSecrets() {
	const prefix = "GLOBAL_SECRET_"
	globalSecrets = nil
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		eqIdx := strings.Index(env, "=")
		if eqIdx < 0 {
			continue
		}
		name := env[len(prefix):eqIdx]
		value := env[eqIdx+1:]
		if name == "" {
			continue
		}
		globalSecrets = append(globalSecrets, globalSecretEntry{Name: name, Value: value})
	}
	if len(globalSecrets) > 0 {
		names := make([]string, len(globalSecrets))
		for i, s := range globalSecrets {
			names[i] = s.Name
		}
		log.Printf("[SECRETS] Loaded %d global secrets from environment: %v", len(globalSecrets), names)
	}
}

// getGlobalSecrets returns the loaded global secrets (read-only after startup)
func getGlobalSecrets() []globalSecretEntry {
	return globalSecrets
}

// handleGetGlobalSecrets returns the names of global secrets (no values exposed)
// GET /api/secrets/global
func (api *StreamingAPI) handleGetGlobalSecrets(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Name string `json:"name"`
	}
	result := make([]entry, len(globalSecrets))
	for i, s := range globalSecrets {
		result[i] = entry{Name: s.Name}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// secretEncryptRequest is the request body for encrypting a secret value
type secretEncryptRequest struct {
	Value string `json:"value"`
}

// secretEncryptResponse is the response body with the encrypted value
type secretEncryptResponse struct {
	Encrypted string `json:"encrypted"`
}

// secretDecryptRequest is the request body for decrypting a secret value
type secretDecryptRequest struct {
	Encrypted string `json:"encrypted"`
}

// secretDecryptResponse is the response body with the decrypted value
type secretDecryptResponse struct {
	Value string `json:"value"`
}

// deriveSecretsKey derives a 32-byte AES-256 key from AUTH_SECRET using HMAC-SHA256
func deriveSecretsKey() []byte {
	authSecret := GetAuthSecret()
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte("secrets-encryption-key"))
	return mac.Sum(nil) // 32 bytes = AES-256
}

// handleEncryptSecret encrypts a plaintext value using AES-256-GCM
// POST /api/secrets/encrypt
func (api *StreamingAPI) handleEncryptSecret(w http.ResponseWriter, r *http.Request) {
	var req secretEncryptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Value == "" {
		http.Error(w, "Value is required", http.StatusBadRequest)
		return
	}

	userID := GetUserIDFromContext(r.Context())

	key := deriveSecretsKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create cipher: %v", err), http.StatusInternalServerError)
		return
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create GCM: %v", err), http.StatusInternalServerError)
		return
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate nonce: %v", err), http.StatusInternalServerError)
		return
	}

	// Use userID as additional authenticated data for per-user isolation
	aad := []byte(userID)
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(req.Value), aad)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(secretEncryptResponse{
		Encrypted: base64.StdEncoding.EncodeToString(ciphertext),
	})
}

// handleDecryptSecret decrypts an AES-256-GCM encrypted value
// POST /api/secrets/decrypt
func (api *StreamingAPI) handleDecryptSecret(w http.ResponseWriter, r *http.Request) {
	var req secretDecryptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Encrypted == "" {
		http.Error(w, "Encrypted value is required", http.StatusBadRequest)
		return
	}

	userID := GetUserIDFromContext(r.Context())

	data, err := base64.StdEncoding.DecodeString(req.Encrypted)
	if err != nil {
		http.Error(w, "Invalid base64 encoding", http.StatusBadRequest)
		return
	}

	key := deriveSecretsKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create cipher: %v", err), http.StatusInternalServerError)
		return
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create GCM: %v", err), http.StatusInternalServerError)
		return
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		http.Error(w, "Encrypted data too short", http.StatusBadRequest)
		return
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	// Use userID as AAD — prevents cross-user decryption
	aad := []byte(userID)
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		http.Error(w, "Decryption failed — invalid key or data", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(secretDecryptResponse{
		Value: string(plaintext),
	})
}
