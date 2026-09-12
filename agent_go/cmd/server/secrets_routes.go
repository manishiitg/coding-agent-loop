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

	"github.com/gorilla/mux"
)

// globalSecretEntry holds a single env-based global secret (name + plaintext value)
type globalSecretEntry struct {
	Name    string
	Value   string
	Managed bool
}

// globalSecrets is populated once at startup from GLOBAL_SECRET_* env vars
var globalSecrets []globalSecretEntry

// loadGlobalSecrets scans os.Environ() for GLOBAL_SECRET_ prefix and populates globalSecrets
func loadGlobalSecrets() {
	managedGlobalsMu.Lock()
	defer managedGlobalsMu.Unlock()
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

// getGlobalSecrets snapshots environment and encrypted admin-managed globals.
func getGlobalSecrets() []globalSecretEntry {
	return sortedGlobalSecrets()
}

// handleGetGlobalSecrets returns the names of global secrets (no values exposed)
// GET /api/secrets/global
func (api *StreamingAPI) handleGetGlobalSecrets(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Name    string `json:"name"`
		Managed bool   `json:"managed"`
	}
	globals := getGlobalSecrets()
	result := make([]entry, len(globals))
	for i, s := range globals {
		result[i] = entry{Name: s.Name, Managed: s.Managed}
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

// secretDecryptRequest is the request body for decrypting a secret value.
// WorkspacePath marks the blob as a shared workflow secret (bound to the
// workflow, not the caller) and makes the request a reveal that only the
// workflow's owners may perform.
type secretDecryptRequest struct {
	Encrypted     string `json:"encrypted"`
	WorkspacePath string `json:"workspace_path,omitempty"`
}

// secretDecryptResponse is the response body with the decrypted value
type secretDecryptResponse struct {
	Value string `json:"value"`
}

// deriveSecretsKey derives a 32-byte AES-256 key from AUTH_SECRET using HMAC-SHA256
func deriveSecretsKey() []byte {
	return deriveSecretsKeyFromSecret(GetAuthSecret())
}

// deriveSecretsKeyFromSecret derives a 32-byte AES-256 key from the provided secret using HMAC-SHA256.
func deriveSecretsKeyFromSecret(secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
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

	if strings.TrimSpace(req.WorkspacePath) != "" {
		// Reveal of a shared workflow secret: owners only. Readers can run the
		// workflow with the value but never see it.
		if !requireWorkflowOwner(w, r, req.WorkspacePath) {
			return
		}
		aad, err := sharedWorkflowSecretAAD(req.WorkspacePath)
		if err != nil {
			http.Error(w, "Invalid workspace_path", http.StatusBadRequest)
			return
		}
		plaintext, err := decryptSecretValueWithAAD(req.Encrypted, aad)
		if err != nil {
			http.Error(w, "Decryption failed — invalid key or data", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(secretDecryptResponse{Value: plaintext})
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

// decryptSecretValue decrypts an AES-256-GCM encrypted base64 value using userID as AAD.
// Extracted from handleDecryptSecret for reuse by the bot secrets loader.
func decryptSecretValue(encryptedBase64 string, userID string) (string, error) {
	return decryptSecretValueWithAAD(encryptedBase64, []byte(userID))
}

// storeSecretRequest is the request body for storing a user secret server-side
type storeSecretRequest struct {
	Name           string `json:"name"`
	EncryptedValue string `json:"encrypted_value"`
	WorkspacePath  string `json:"workspace_path,omitempty"`
}

// handleStoreUserSecret upserts a user secret in the database
// PUT /api/secrets/store
func (api *StreamingAPI) handleStoreUserSecret(w http.ResponseWriter, r *http.Request) {
	var req storeSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.EncryptedValue == "" {
		http.Error(w, "name and encrypted_value are required", http.StatusBadRequest)
		return
	}

	userID := GetUserIDFromContext(r.Context())

	if err := api.chatStore.UpsertUserSecret(r.Context(), userID, req.Name, req.EncryptedValue); err != nil {
		log.Printf("[SECRETS] Failed to store user secret: %v", err)
		http.Error(w, "Failed to store secret", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleDeleteUserSecret deletes a user secret from the database
// DELETE /api/secrets/store/{name}
func (api *StreamingAPI) handleDeleteUserSecret(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "Secret name is required", http.StatusBadRequest)
		return
	}

	userID := GetUserIDFromContext(r.Context())

	if err := api.chatStore.DeleteUserSecret(r.Context(), userID, name); err != nil {
		log.Printf("[SECRETS] Failed to delete user secret: %v", err)
		http.Error(w, "Failed to delete secret", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleListStoredSecrets returns secret names stored server-side (no values exposed)
// GET /api/secrets/stored
func (api *StreamingAPI) handleListStoredSecrets(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())

	secrets, err := api.chatStore.ListUserSecrets(r.Context(), userID)
	if err != nil {
		log.Printf("[SECRETS] Failed to list stored secrets: %v", err)
		http.Error(w, "Failed to list secrets", http.StatusInternalServerError)
		return
	}

	// The encrypted value is returned so this endpoint is a complete read of
	// the caller's own secrets. Withholding it bought no security -- the same
	// authenticated session can already POST any blob to /api/secrets/decrypt --
	// while forcing the client to keep its own parallel copy of every secret it
	// wanted to display or edit. That copy was the real hazard: it lived in one
	// browser, so a secret saved anywhere else was invisible, and a secret saved
	// here survived only until site data was cleared.
	type entry struct {
		ID             string `json:"id,omitempty"`
		Name           string `json:"name"`
		EncryptedValue string `json:"encrypted_value,omitempty"`
	}
	result := make([]entry, len(secrets))
	for i, s := range secrets {
		result[i] = entry{ID: s.ID, Name: s.Name, EncryptedValue: s.EncryptedValue}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleStoreWorkflowSecret upserts a workflow secret in the shared,
// workflow-scoped store (see workflow_shared_secrets.go). Owners only.
// PUT /api/secrets/workflow/store
//
// The client keeps sending encrypted_value from /api/secrets/encrypt (bound to
// the caller); it is re-bound to the workflow here so every user with access
// resolves it.
func (api *StreamingAPI) handleStoreWorkflowSecret(w http.ResponseWriter, r *http.Request) {
	var req storeSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.Name == "" || req.EncryptedValue == "" {
		http.Error(w, "workspace_path, name, and encrypted_value are required", http.StatusBadRequest)
		return
	}
	if !requireWorkflowOwner(w, r, req.WorkspacePath) {
		return
	}

	userID := GetUserIDFromContext(r.Context())
	plaintext, err := decryptSecretValue(req.EncryptedValue, userID)
	if err != nil {
		http.Error(w, "encrypted_value must be produced by /api/secrets/encrypt in this session", http.StatusBadRequest)
		return
	}
	if err := api.upsertSharedWorkflowSecret(r.Context(), req.WorkspacePath, req.Name, plaintext); err != nil {
		log.Printf("[SECRETS] Failed to store workflow secret: %v", err)
		http.Error(w, "Failed to store workflow secret", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleDeleteWorkflowSecret deletes a shared workflow secret. Owners only.
// DELETE /api/secrets/workflow/store/{name}?workspace_path=Workflow/foo
func (api *StreamingAPI) handleDeleteWorkflowSecret(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	workspacePath := r.URL.Query().Get("workspace_path")
	if name == "" || workspacePath == "" {
		http.Error(w, "Secret name and workspace_path are required", http.StatusBadRequest)
		return
	}
	if !requireWorkflowOwner(w, r, workspacePath) {
		return
	}

	userID := GetUserIDFromContext(r.Context())
	if err := api.deleteSharedWorkflowSecret(r.Context(), workspacePath, name, userID); err != nil {
		log.Printf("[SECRETS] Failed to delete workflow secret: %v", err)
		http.Error(w, "Failed to delete workflow secret", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleListStoredWorkflowSecrets returns the workflow's shared secret names
// for anyone who can see the workflow. Owners additionally receive the
// ciphertext so the pane can reveal a value through /api/secrets/decrypt
// (which re-checks ownership); readers get names only.
// GET /api/secrets/workflow/stored?workspace_path=Workflow/foo
func (api *StreamingAPI) handleListStoredWorkflowSecrets(w http.ResponseWriter, r *http.Request) {
	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	level := currentUserWorkflowAccess(r, workspacePath)
	if level == WorkflowAccessNone {
		writeWorkflowPermissionDenied(w, "read")
		return
	}
	canReveal := level == WorkflowAccessOwner || level == WorkflowAccessWrite

	userID := GetUserIDFromContext(r.Context())
	secrets, err := api.ensureSharedWorkflowSecrets(r.Context(), workspacePath, userID)
	if err != nil {
		log.Printf("[SECRETS] Failed to list workflow secrets: %v", err)
		http.Error(w, "Failed to list workflow secrets", http.StatusInternalServerError)
		return
	}

	type entry struct {
		Name           string `json:"name"`
		EncryptedValue string `json:"encrypted_value,omitempty"`
	}
	result := make([]entry, len(secrets))
	for i, s := range secrets {
		result[i] = entry{Name: s.Name}
		if canReveal {
			result[i].EncryptedValue = s.EncryptedValue
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
