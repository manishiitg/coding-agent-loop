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
	"log"
	"net/http"

	"github.com/manishiitg/mcpagent/llm"
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

// LoadProviderKeysAsLLMKeys loads workspace-encrypted keys and converts to *llm.ProviderAPIKeys.
// Returns an empty (non-nil) struct if keys don't exist or decryption fails.
func LoadProviderKeysAsLLMKeys(ctx context.Context) *llm.ProviderAPIKeys {
	keys, err := LoadProviderKeys(ctx)
	if err != nil {
		log.Printf("[PROVIDER_KEYS] Failed to load workspace provider keys: %v", err)
		return &llm.ProviderAPIKeys{}
	}
	if keys == nil {
		log.Printf("[PROVIDER_KEYS] No workspace provider keys file found")
		return &llm.ProviderAPIKeys{}
	}
	result := &llm.ProviderAPIKeys{}
	if keys.OpenRouter != "" {
		result.OpenRouter = &keys.OpenRouter
	}
	if keys.OpenAI != "" {
		result.OpenAI = &keys.OpenAI
	}
	if keys.Anthropic != "" {
		result.Anthropic = &keys.Anthropic
	}
	if keys.Vertex != "" {
		result.Vertex = &keys.Vertex
	}
	if keys.GeminiCLI != "" {
		result.GeminiCLI = &keys.GeminiCLI
	}
	if keys.MiniMax != "" {
		result.MiniMax = &keys.MiniMax
	}
	if keys.MiniMaxCodingPlan != "" {
		result.MiniMaxCodingPlan = &keys.MiniMaxCodingPlan
	}
	if keys.Bedrock != nil {
		result.Bedrock = &llm.BedrockConfig{Region: keys.Bedrock.Region}
	}
	if keys.Azure != nil {
		result.Azure = &llm.AzureAPIConfig{
			Endpoint:   keys.Azure.Endpoint,
			APIKey:     keys.Azure.APIKey,
			APIVersion: keys.Azure.APIVersion,
			Region:     keys.Azure.Region,
		}
	}
	// Log which providers have keys loaded
	var loaded []string
	if result.GeminiCLI != nil { loaded = append(loaded, "gemini-cli") }
	if result.OpenRouter != nil { loaded = append(loaded, "openrouter") }
	if result.OpenAI != nil { loaded = append(loaded, "openai") }
	if result.Anthropic != nil { loaded = append(loaded, "anthropic") }
	if result.Vertex != nil { loaded = append(loaded, "vertex") }
	if result.MiniMax != nil { loaded = append(loaded, "minimax") }
	if result.Bedrock != nil { loaded = append(loaded, "bedrock") }
	if result.Azure != nil { loaded = append(loaded, "azure") }
	log.Printf("[PROVIDER_KEYS] Loaded workspace keys for providers: %v", loaded)

	return result
}

// MergedProviderAPIKeys returns a single *llm.ProviderAPIKeys that merges
// env-var keys (.env) and workspace-encrypted keys. Base layer is .env,
// workspace keys override when present. This is the single source of truth
// for "give me all available API keys".
func MergedProviderAPIKeys(ctx context.Context) *llm.ProviderAPIKeys {
	envKeys := buildProviderAPIKeysFromEnv()
	wsKeys := LoadProviderKeysAsLLMKeys(ctx)

	if envKeys == nil && wsKeys == nil {
		return &llm.ProviderAPIKeys{}
	}
	if envKeys == nil {
		return wsKeys
	}
	if wsKeys == nil {
		return envKeys
	}

	// Start from env, workspace overrides when set
	pick := func(env, ws *string) *string {
		if ws != nil { return ws }
		return env
	}
	result := &llm.ProviderAPIKeys{
		OpenRouter:        pick(envKeys.OpenRouter, wsKeys.OpenRouter),
		OpenAI:            pick(envKeys.OpenAI, wsKeys.OpenAI),
		Anthropic:         pick(envKeys.Anthropic, wsKeys.Anthropic),
		Vertex:            pick(envKeys.Vertex, wsKeys.Vertex),
		GeminiCLI:         pick(envKeys.GeminiCLI, wsKeys.GeminiCLI),
		CodexCLI:          pick(envKeys.CodexCLI, wsKeys.CodexCLI),
		MiniMax:           pick(envKeys.MiniMax, wsKeys.MiniMax),
		MiniMaxCodingPlan: pick(envKeys.MiniMaxCodingPlan, wsKeys.MiniMaxCodingPlan),
	}
	// Bedrock / Azure: workspace wins if present, else env
	if wsKeys.Bedrock != nil {
		result.Bedrock = wsKeys.Bedrock
	} else if envKeys.Bedrock != nil {
		result.Bedrock = envKeys.Bedrock
	}
	if wsKeys.Azure != nil {
		result.Azure = wsKeys.Azure
	} else if envKeys.Azure != nil {
		result.Azure = envKeys.Azure
	}
	return result
}

// encryptProviderKeys encrypts data using AES-256-GCM with the derived secrets key.
// Uses "provider-keys" as AAD (global, not per-user).
func encryptProviderKeys(plaintext []byte) ([]byte, error) {
	return encryptProviderKeysWithSecret(plaintext, GetAuthSecret())
}

func encryptProviderKeysWithSecret(plaintext []byte, authSecret []byte) ([]byte, error) {
	key := deriveSecretsKeyFromSecret(authSecret)
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
	return decryptProviderKeysWithSecret(data, GetAuthSecret())
}

func decryptProviderKeysWithSecret(data []byte, authSecret []byte) ([]byte, error) {
	key := deriveSecretsKeyFromSecret(authSecret)
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

	var incoming StoredProviderKeys
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Merge: load existing keys first, then overlay non-empty incoming fields.
	// This prevents the frontend from wiping keys it didn't send.
	merged := mergeStoredProviderKeys(r.Context(), &incoming)

	if err := SaveProviderKeys(r.Context(), merged); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save keys: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// mergeStoredProviderKeys loads existing keys and overlays incoming non-empty values.
// Empty strings in incoming are treated as "not sent" and won't erase existing keys.
// To explicitly delete a key, send the sentinel value "__DELETE__".
func mergeStoredProviderKeys(ctx context.Context, incoming *StoredProviderKeys) *StoredProviderKeys {
	existing, err := LoadProviderKeys(ctx)
	if err != nil || existing == nil {
		return incoming // nothing to merge with
	}

	pick := func(existingVal, incomingVal string) string {
		if incomingVal == "__DELETE__" {
			return ""
		}
		if incomingVal != "" {
			return incomingVal
		}
		return existingVal
	}

	merged := &StoredProviderKeys{
		OpenRouter:        pick(existing.OpenRouter, incoming.OpenRouter),
		OpenAI:            pick(existing.OpenAI, incoming.OpenAI),
		Anthropic:         pick(existing.Anthropic, incoming.Anthropic),
		Vertex:            pick(existing.Vertex, incoming.Vertex),
		GeminiCLI:         pick(existing.GeminiCLI, incoming.GeminiCLI),
		MiniMax:           pick(existing.MiniMax, incoming.MiniMax),
		MiniMaxCodingPlan: pick(existing.MiniMaxCodingPlan, incoming.MiniMaxCodingPlan),
	}

	// Bedrock / Azure: incoming wins if present, else keep existing
	if incoming.Bedrock != nil {
		merged.Bedrock = incoming.Bedrock
	} else {
		merged.Bedrock = existing.Bedrock
	}
	if incoming.Azure != nil {
		merged.Azure = incoming.Azure
	} else {
		merged.Azure = existing.Azure
	}

	return merged
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
