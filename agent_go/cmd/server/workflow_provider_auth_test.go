package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/mcpagent/llm"
)

func TestWorkflowSessionIDsAreUserAndWorkflowScoped(t *testing.T) {
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		"alice-target": {
			SessionID:     "alice-target",
			UserID:        "alice",
			WorkspacePath: "Workflow/customer-renewals/",
			LastActivity:  time.Now(),
		},
		"bob-same-path": {
			SessionID:     "bob-same-path",
			UserID:        "bob",
			WorkspacePath: "Workflow/customer-renewals",
		},
		"alice-other": {
			SessionID:     "alice-other",
			UserID:        "alice",
			WorkspacePath: "Workflow/support-triage",
		},
	}}

	ids := api.workflowSessionIDs("alice", `Workflow\customer-renewals`)
	if len(ids) != 1 || ids[0] != "alice-target" {
		t.Fatalf("workflowSessionIDs() = %v, want [alice-target]", ids)
	}
	if ids := api.workflowSessionIDs("", "Workflow/customer-renewals"); len(ids) != 0 {
		t.Fatalf("workflowSessionIDs(empty user) = %v, want none", ids)
	}
}

func TestValidateClaudeCodeOAuthTokenUsesIsolatedAuthEnvironment(t *testing.T) {
	fakeBin := t.TempDir()
	capturePath := filepath.Join(fakeBin, "auth-env.txt")
	claudePath := filepath.Join(fakeBin, "claude")
	script := `#!/bin/sh
printf '%s|%s|%s|%s|%s' "$ANTHROPIC_API_KEY" "$ANTHROPIC_AUTH_TOKEN" "$ANTHROPIC_BASE_URL" "$CLAUDE_CODE_OAUTH_TOKEN" "$CLAUDE_CONFIG_DIR" > "$CLAUDE_AUTH_CAPTURE"
printf '{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}\n'
`
	if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_AUTH_CAPTURE", capturePath)
	t.Setenv("ANTHROPIC_API_KEY", "ambient-api-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "ambient-auth-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://ambient.example")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "ambient-oauth-token")

	const workflowToken = "workflow-oauth-token"
	if err := validateClaudeCodeOAuthToken(context.Background(), workflowToken); err != nil {
		t.Fatalf("validateClaudeCodeOAuthToken() error = %v", err)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured auth env: %v", err)
	}
	parts := strings.Split(string(captured), "|")
	if len(parts) != 5 {
		t.Fatalf("captured env = %q", string(captured))
	}
	if parts[0] != "" || parts[1] != "" || parts[2] != "" {
		t.Fatalf("ambient Anthropic credentials reached validation: %q", string(captured))
	}
	if parts[3] != workflowToken {
		t.Fatalf("OAuth token = %q, want workflow token", parts[3])
	}
	if strings.TrimSpace(parts[4]) == "" {
		t.Fatal("validation did not isolate Claude config directory")
	}
}

func TestWorkflowProviderAPIKeysAreUserAndWorkflowScoped(t *testing.T) {
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	api := &StreamingAPI{chatStore: store}
	ctx := context.Background()
	const userID = "alice"
	const workflowA = "Workflow/customer-renewals"
	const workflowB = "Workflow/support-triage"
	const workflowToken = "workflow-oauth-token"

	encrypted := encryptProviderCredentialForTest(t, workflowToken, userID)
	if err := store.UpsertWorkflowProviderCredential(ctx, userID, workflowA, claudeCodeProviderID, encrypted); err != nil {
		t.Fatalf("UpsertWorkflowProviderCredential() error = %v", err)
	}
	apiKey := "direct-anthropic-key"
	base := &llm.ProviderAPIKeys{Anthropic: &apiKey}

	keysA, err := api.workflowProviderAPIKeys(ctx, userID, workflowA, base)
	if err != nil {
		t.Fatalf("workflowProviderAPIKeys(workflowA) error = %v", err)
	}
	if keysA.ClaudeCodeOAuthToken == nil || *keysA.ClaudeCodeOAuthToken != workflowToken {
		t.Fatalf("workflow A OAuth token = %#v", keysA.ClaudeCodeOAuthToken)
	}
	if keysA.Anthropic == nil || *keysA.Anthropic != apiKey {
		t.Fatalf("direct Anthropic provider key was not preserved: %#v", keysA.Anthropic)
	}

	keysB, err := api.workflowProviderAPIKeys(ctx, userID, workflowB, base)
	if err != nil {
		t.Fatalf("workflowProviderAPIKeys(workflowB) error = %v", err)
	}
	if keysB.ClaudeCodeOAuthToken != nil {
		t.Fatalf("workflow B received workflow A token: %#v", keysB.ClaudeCodeOAuthToken)
	}
	keysOtherUser, err := api.workflowProviderAPIKeys(ctx, "bob", workflowA, base)
	if err != nil {
		t.Fatalf("workflowProviderAPIKeys(other user) error = %v", err)
	}
	if keysOtherUser.ClaudeCodeOAuthToken != nil {
		t.Fatalf("other user received Alice's token: %#v", keysOtherUser.ClaudeCodeOAuthToken)
	}

	workflowSecrets, err := store.ListWorkflowSecrets(ctx, userID, workflowA)
	if err != nil {
		t.Fatalf("ListWorkflowSecrets() error = %v", err)
	}
	if len(workflowSecrets) != 0 {
		t.Fatalf("provider credential leaked into workflow secrets: %+v", workflowSecrets)
	}
}

func encryptProviderCredentialForTest(t *testing.T, value, userID string) string {
	t.Helper()
	key := deriveSecretsKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher() error = %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM() error = %v", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(value), []byte(userID))
	return base64.StdEncoding.EncodeToString(ciphertext)
}
