package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// secrets_store.go — an encrypted, local key-value store for credentials the
// parent wants Quill's tools to use (e.g. a school portal login) without the
// model ever seeing a raw value again once it's set. Deliberately kept
// OUTSIDE workspace/ entirely — unlike everything else in this app — so
// neither the parent's nor the child's execute_shell_command can ever read
// the key file or the encrypted blob via any path, however sandboxed; only
// this file's own Go code can decrypt it. Single flat namespace: this is a
// one-family local app, no multi-tenant scoping needed (unlike AgentWorks'
// per-user/per-workflow secrets, which this mirrors in spirit — encrypted at
// rest, names-only exposed to the model, injected as env vars at exec time —
// but not in storage shape).
var secretsMu sync.Mutex

func secretsKeyPath() string  { return filepath.Join(familyDataDir(), "secrets.key") }
func secretsBlobPath() string { return filepath.Join(familyDataDir(), "secrets.enc.json") }

func loadOrCreateSecretsKey() ([]byte, error) {
	if b, err := os.ReadFile(secretsKeyPath()); err == nil && len(b) == 32 {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secrets key: %w", err)
	}
	if err := os.MkdirAll(familyDataDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(secretsKeyPath(), key, 0o600); err != nil {
		return nil, fmt.Errorf("save secrets key: %w", err)
	}
	return key, nil
}

func secretsGCM() (cipher.AEAD, error) {
	key, err := loadOrCreateSecretsKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

type storedSecretsBlob struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func loadSecretsMapLocked() map[string]string {
	b, err := os.ReadFile(secretsBlobPath())
	if err != nil {
		return map[string]string{}
	}
	var stored storedSecretsBlob
	if err := json.Unmarshal(b, &stored); err != nil {
		return map[string]string{}
	}
	nonce, err := base64.StdEncoding.DecodeString(stored.Nonce)
	if err != nil {
		return map[string]string{}
	}
	ciphertext, err := base64.StdEncoding.DecodeString(stored.Ciphertext)
	if err != nil {
		return map[string]string{}
	}
	gcm, err := secretsGCM()
	if err != nil {
		return map[string]string{}
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(plain, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func saveSecretsMapLocked(m map[string]string) error {
	gcm, err := secretsGCM()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	plain, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	b, err := json.MarshalIndent(storedSecretsBlob{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(familyDataDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(secretsBlobPath(), b, 0o600)
}

// listSecretNames returns every stored secret's name, sorted — NEVER values.
func listSecretNames() []string {
	secretsMu.Lock()
	m := loadSecretsMapLocked()
	secretsMu.Unlock()
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func setSecretValue(name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	m := loadSecretsMapLocked()
	m[name] = value
	return saveSecretsMapLocked(m)
}

func deleteSecretValue(name string) error {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	m := loadSecretsMapLocked()
	delete(m, name)
	return saveSecretsMapLocked(m)
}

// allSecretValues returns every stored secret's raw value — used only for
// redacting text before it's persisted or logged, never returned over HTTP or
// exposed to the model.
func allSecretValues() []string {
	secretsMu.Lock()
	m := loadSecretsMapLocked()
	secretsMu.Unlock()
	values := make([]string, 0, len(m))
	for _, v := range m {
		if strings.TrimSpace(v) != "" {
			values = append(values, v)
		}
	}
	return values
}

var envNameSanitizeRE = regexp.MustCompile(`[^A-Z0-9_]+`)

// secretEnvName turns an arbitrary secret name into a valid, safely-prefixed
// env var name, e.g. "School Portal Password" -> "SECRET_SCHOOL_PORTAL_PASSWORD".
func secretEnvName(name string) string {
	s := envNameSanitizeRE.ReplaceAllString(strings.ToUpper(strings.TrimSpace(name)), "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "SECRET"
	}
	return "SECRET_" + s
}

// secretEnvPairs returns "SECRET_<NAME>=value" pairs for every stored secret,
// ready to append to an exec.Cmd's Env — the ONLY place a raw value leaves
// this file, and only into a sandboxed child process's environment, never
// into the model's own context.
func secretEnvPairs() []string {
	secretsMu.Lock()
	m := loadSecretsMapLocked()
	secretsMu.Unlock()
	out := make([]string, 0, len(m))
	for name, value := range m {
		out = append(out, secretEnvName(name)+"="+value)
	}
	return out
}

// redactSecrets replaces every occurrence of any given secret value inside
// text with a placeholder — used to keep a credential the parent typed in
// chat from surviving forever in the persisted conversation transcript.
func redactSecrets(text string, values []string) string {
	if text == "" {
		return text
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		text = strings.ReplaceAll(text, v, "[secret redacted]")
	}
	return text
}

// --- HTTP: settings-form-only management (never through chat/model) --------

type secretsListResp struct {
	Names []string `json:"names"`
}

// GET /api/secrets — names only. POST {name,value} — create/update.
// DELETE {name} — remove. This is the primary way secrets get set: a plain
// form in Settings, never touching the chat/model, so a credential typed here
// never enters any persisted conversation transcript at all.
func handleSecrets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, secretsListResp{Names: listSecretNames()})
	case http.MethodPost:
		var req struct{ Name, Value string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		req.Name, req.Value = strings.TrimSpace(req.Name), strings.TrimSpace(req.Value)
		if req.Name == "" || req.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and value are required"})
			return
		}
		if err := setSecretValue(req.Name, req.Value); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, secretsListResp{Names: listSecretNames()})
	case http.MethodDelete:
		var req struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if err := deleteSecretValue(strings.TrimSpace(req.Name)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, secretsListResp{Names: listSecretNames()})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
