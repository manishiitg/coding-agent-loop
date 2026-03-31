package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// readWorkspaceFile reads a file from the workspace API using the given workspaceURL.
// Returns (content, true, nil) if found, ("", false, nil) if not found, or ("", false, err) on error.
func readWorkspaceFile(ctx context.Context, workspaceURL, filePath string) (string, bool, error) {
	pathSegments := strings.Split(filePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, seg := range pathSegments {
		encodedSegments[i] = strings.NewReplacer(" ", "%20", "#", "%23", "?", "%3F").Replace(seg)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	apiURL := workspaceURL + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("workspace API returned status %d", resp.StatusCode)
	}

	var apiResp struct {
		Success bool        `json:"success"`
		Error   string      `json:"error"`
		Message string      `json:"message"`
		Data    interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", false, fmt.Errorf("failed to parse API response: %w", err)
	}
	if !apiResp.Success {
		if strings.Contains(apiResp.Message, "File does not exist") || strings.Contains(apiResp.Error, "File not found") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	var content string
	if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if c, ok := dataMap["content"].(string); ok {
			content = c
		}
	}
	if content == "" {
		log.Printf("[WORKSPACE_CONFIG] Failed to extract content for %s", filePath)
		return "", false, fmt.Errorf("failed to extract content from workspace response")
	}
	return content, true, nil
}

// LoadDelegationTierConfig reads delegation tier config (plain JSON) from the workspace.
// Returns (nil, false, nil) if the file doesn't exist.
func LoadDelegationTierConfig(ctx context.Context, workspaceURL string) (map[string]interface{}, bool, error) {
	content, exists, err := readWorkspaceFile(ctx, workspaceURL, "config/delegation-tier-config.json")
	if err != nil || !exists {
		return nil, exists, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, false, fmt.Errorf("failed to parse tier config: %w", err)
	}
	return cfg, true, nil
}

// LoadProviderKeys reads and decrypts provider API keys from the workspace.
// Returns a map in the api_keys format used by llm_config.
// Returns (nil, false, nil) if the file doesn't exist.
func LoadProviderKeys(ctx context.Context, workspaceURL string) (map[string]interface{}, bool, error) {
	content, exists, err := readWorkspaceFile(ctx, workspaceURL, "config/provider-api-keys.json")
	if err != nil || !exists {
		return nil, exists, err
	}

	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, false, fmt.Errorf("failed to decode base64: %w", err)
	}

	plaintext, err := decryptWithAAD(data, "provider-keys")
	if err != nil {
		return nil, false, fmt.Errorf("failed to decrypt provider keys: %w", err)
	}

	var stored struct {
		OpenRouter        string `json:"openrouter,omitempty"`
		OpenAI            string `json:"openai,omitempty"`
		Anthropic         string `json:"anthropic,omitempty"`
		Vertex            string `json:"vertex,omitempty"`
		GeminiCLI         string `json:"gemini_cli,omitempty"`
		MiniMax           string `json:"minimax,omitempty"`
		MiniMaxCodingPlan string `json:"minimax_coding_plan,omitempty"`
		Bedrock           *struct {
			Region string `json:"region"`
		} `json:"bedrock,omitempty"`
		Azure *struct {
			Endpoint   string `json:"endpoint"`
			APIKey     string `json:"api_key"`
			APIVersion string `json:"api_version,omitempty"`
			Region     string `json:"region,omitempty"`
		} `json:"azure,omitempty"`
	}
	if err := json.Unmarshal(plaintext, &stored); err != nil {
		return nil, false, fmt.Errorf("failed to parse provider keys: %w", err)
	}

	m := map[string]interface{}{}
	if stored.OpenRouter != "" {
		m["openrouter"] = stored.OpenRouter
	}
	if stored.OpenAI != "" {
		m["openai"] = stored.OpenAI
	}
	if stored.Anthropic != "" {
		m["anthropic"] = stored.Anthropic
	}
	if stored.Vertex != "" {
		m["vertex"] = stored.Vertex
	}
	if stored.GeminiCLI != "" {
		m["gemini_cli"] = stored.GeminiCLI
	}
	if stored.MiniMax != "" {
		m["minimax"] = stored.MiniMax
	}
	if stored.MiniMaxCodingPlan != "" {
		m["minimax-coding-plan"] = stored.MiniMaxCodingPlan
	}
	if stored.Bedrock != nil {
		m["bedrock"] = map[string]interface{}{"region": stored.Bedrock.Region}
	}
	if stored.Azure != nil {
		m["azure"] = map[string]interface{}{
			"endpoint":    stored.Azure.Endpoint,
			"api_key":     stored.Azure.APIKey,
			"api_version": stored.Azure.APIVersion,
			"region":      stored.Azure.Region,
		}
	}
	return m, true, nil
}

// deriveSecretsKey derives the AES-256 key from AUTH_SECRET using HMAC-SHA256.
// Must match the derivation in server/secrets_routes.go.
func deriveSecretsKey() []byte {
	secret := os.Getenv("AUTH_SECRET")
	if secret == "" {
		secret = "dev-secret-change-in-production"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("secrets-encryption-key"))
	return mac.Sum(nil)
}

// decryptWithAAD decrypts AES-256-GCM encrypted data with the given AAD string.
func decryptWithAAD(data []byte, aad string) ([]byte, error) {
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
	return aesGCM.Open(nil, nonce, ciphertext, []byte(aad))
}
