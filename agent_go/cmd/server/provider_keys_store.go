package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const providerKeysFilePath = "config/provider-api-keys.json"

// StoredProviderKeys holds all LLM provider API keys in a single global structure.
// Encrypted and stored in the workspace config folder.
type StoredProviderKeys struct {
	OpenRouter        string               `json:"openrouter,omitempty"`
	OpenAI            string               `json:"openai,omitempty"`
	Anthropic         string               `json:"anthropic,omitempty"`
	Vertex            string               `json:"vertex,omitempty"`
	GeminiCLI         string               `json:"gemini_cli,omitempty"`
	MiniMax           string               `json:"minimax,omitempty"`
	MiniMaxCodingPlan string               `json:"minimax_coding_plan,omitempty"`
	Bedrock           *StoredBedrockConfig `json:"bedrock,omitempty"`
	Azure             *StoredAzureConfig   `json:"azure,omitempty"`
}

// StoredBedrockConfig holds Bedrock region config.
type StoredBedrockConfig struct {
	Region string `json:"region"`
}

// StoredAzureConfig holds Azure OpenAI connection config.
type StoredAzureConfig struct {
	Endpoint   string `json:"endpoint"`
	APIKey     string `json:"api_key"`
	APIVersion string `json:"api_version,omitempty"`
	Region     string `json:"region,omitempty"`
}

// SaveProviderKeys encrypts and stores provider API keys to the workspace.
func SaveProviderKeys(ctx context.Context, keys *StoredProviderKeys) error {
	plaintext, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("failed to marshal keys: %w", err)
	}

	encrypted, err := encryptProviderKeys(plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt keys: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(encrypted)
	if err := writeFileToWorkspace(ctx, providerKeysFilePath, encoded); err != nil {
		return fmt.Errorf("failed to write encrypted keys: %w", err)
	}

	return nil
}

// LoadProviderKeys reads and decrypts provider API keys from the workspace.
// Returns nil, nil if the file doesn't exist.
func LoadProviderKeys(ctx context.Context) (*StoredProviderKeys, error) {
	content, exists, err := readFileFromWorkspace(ctx, providerKeysFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read keys file: %w", err)
	}
	if !exists {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	plaintext, err := decryptProviderKeys(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt keys: %w", err)
	}

	var keys StoredProviderKeys
	if err := json.Unmarshal(plaintext, &keys); err != nil {
		return nil, fmt.Errorf("failed to unmarshal keys: %w", err)
	}

	return &keys, nil
}

// ProviderKeysToAPIKeysMap converts stored keys to the map format used in reqMap["llm_config"]["api_keys"].
func ProviderKeysToAPIKeysMap(keys *StoredProviderKeys) map[string]interface{} {
	m := map[string]interface{}{}
	if keys.OpenRouter != "" {
		m["openrouter"] = keys.OpenRouter
	}
	if keys.OpenAI != "" {
		m["openai"] = keys.OpenAI
	}
	if keys.Anthropic != "" {
		m["anthropic"] = keys.Anthropic
	}
	if keys.Vertex != "" {
		m["vertex"] = keys.Vertex
	}
	if keys.GeminiCLI != "" {
		m["gemini_cli"] = keys.GeminiCLI
	}
	if keys.MiniMax != "" {
		m["minimax"] = keys.MiniMax
	}
	if keys.MiniMaxCodingPlan != "" {
		m["minimax-coding-plan"] = keys.MiniMaxCodingPlan
	}
	if keys.Bedrock != nil {
		m["bedrock"] = map[string]interface{}{"region": keys.Bedrock.Region}
	}
	if keys.Azure != nil {
		m["azure"] = map[string]interface{}{
			"endpoint":    keys.Azure.Endpoint,
			"api_key":     keys.Azure.APIKey,
			"api_version": keys.Azure.APIVersion,
			"region":      keys.Azure.Region,
		}
	}
	return m
}

// encryptProviderKeys encrypts data using AES-256-GCM with the derived secrets key.
// Uses "provider-keys" as AAD (global, not per-user).
func encryptProviderKeys(plaintext []byte) ([]byte, error) {
	key := deriveSecretsKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	aad := []byte("provider-keys")
	return aesGCM.Seal(nonce, nonce, plaintext, aad), nil
}

// decryptProviderKeys decrypts data using AES-256-GCM with the derived secrets key.
func decryptProviderKeys(data []byte) ([]byte, error) {
	key := deriveSecretsKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("encrypted data too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	aad := []byte("provider-keys")
	return aesGCM.Open(nil, nonce, ciphertext, aad)
}

// handleSaveProviderKeys saves provider API keys (encrypted) to the workspace.
// PUT /api/provider-keys
func (api *StreamingAPI) handleSaveProviderKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var keys StoredProviderKeys
	if err := json.NewDecoder(r.Body).Decode(&keys); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := SaveProviderKeys(r.Context(), &keys); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save keys: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// handleLoadProviderKeys loads and returns decrypted provider API keys.
// GET /api/provider-keys
func (api *StreamingAPI) handleLoadProviderKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	keys, err := LoadProviderKeys(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load keys: %v", err), http.StatusInternalServerError)
		return
	}

	if keys == nil {
		keys = &StoredProviderKeys{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}
