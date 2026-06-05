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
	"sort"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

// registerSecretManagementTools registers list_secrets, set_user_secret, and
// delete_user_secret on the given agent. In workflow contexts it also registers
// set_workflow_secret and delete_workflow_secret. Global secrets (env-backed)
// are exposed read-only; user/workflow secrets support CRUD on encrypted values.
// toolCategory groups the tools in the agent's tool registry (e.g. "secret_tools").
// afterUpsert, if non-nil, is invoked after a successful set_workflow_secret /
// set_user_secret so callers can auto-attach the secret to workspace state
// (workflow.json manifest + live workshop shell env) — collapsing the old
// store-then-attach two-step into one action so $SECRET_<NAME> is immediately
// available in the same session. It receives the plaintext value (the handler
// already holds it) to avoid a DB re-resolve. Errors are surfaced to the agent
// but do NOT fail the store (the value is already persisted).
// afterDelete, if non-nil, is invoked after a successful delete_user_secret so
// callers can clean up workspace state (e.g. detach from workflow.json + refresh
// workshop shell env). Errors returned by afterDelete are surfaced to the agent.
func (api *StreamingAPI) registerSecretManagementTools(agent *mcpagent.Agent, userID, workflowPath, toolCategory string, afterUpsert func(ctx context.Context, name, value string) error, afterDelete func(ctx context.Context, name string) error) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}
	if userID == "" {
		return fmt.Errorf("userID is required for secret tools")
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return agent.RegisterCustomTool(name, description, params, exec, toolCategory)
	}

	encryptValue := func(plaintext string) (string, error) {
		key := deriveSecretsKey()
		block, err := aes.NewCipher(key)
		if err != nil {
			return "", fmt.Errorf("cipher error: %w", err)
		}
		aesGCM, err := cipher.NewGCM(block)
		if err != nil {
			return "", fmt.Errorf("GCM error: %w", err)
		}
		nonce := make([]byte, aesGCM.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return "", fmt.Errorf("nonce error: %w", err)
		}
		ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), []byte(userID))
		return base64.StdEncoding.EncodeToString(ciphertext), nil
	}

	isGlobalName := func(name string) bool {
		for _, gs := range getGlobalSecrets() {
			if gs.Name == name {
				return true
			}
		}
		return false
	}

	if err := registerTool(
		"list_secrets",
		"List all secrets available to the current user. Returns JSON buckets: 'global' (env-backed, read-only), 'workflow' (encrypted per-user and scoped to this workflow when applicable), and 'user' (encrypted per-user, reusable). Values are never returned — only names. Use this before setting, deleting, or attaching secrets.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, _ map[string]interface{}) (string, error) {
			globals := getGlobalSecrets()
			globalNames := make([]string, 0, len(globals))
			for _, gs := range globals {
				globalNames = append(globalNames, gs.Name)
			}
			sort.Strings(globalNames)

			userSecrets, err := api.chatStore.ListUserSecrets(ctx, userID)
			if err != nil {
				return "", fmt.Errorf("failed to list user secrets: %w", err)
			}
			userNames := make([]string, 0, len(userSecrets))
			for _, us := range userSecrets {
				userNames = append(userNames, us.Name)
			}
			sort.Strings(userNames)

			workflowNames := []string{}
			if strings.TrimSpace(workflowPath) != "" {
				workflowSecrets, err := api.chatStore.ListWorkflowSecrets(ctx, userID, workflowPath)
				if err != nil {
					return "", fmt.Errorf("failed to list workflow secrets: %w", err)
				}
				workflowNames = make([]string, 0, len(workflowSecrets))
				for _, ws := range workflowSecrets {
					workflowNames = append(workflowNames, ws.Name)
				}
				sort.Strings(workflowNames)
			}

			out, _ := json.MarshalIndent(map[string]interface{}{
				"global": map[string]interface{}{
					"read_only": true,
					"source":    "GLOBAL_SECRET_* env vars",
					"names":     globalNames,
				},
				"workflow": map[string]interface{}{
					"read_only":     false,
					"source":        "per-user encrypted workflow store",
					"workflow_path": workflowPath,
					"names":         workflowNames,
				},
				"user": map[string]interface{}{
					"read_only": false,
					"source":    "per-user encrypted store",
					"names":     userNames,
				},
				"note": "Secret VALUES are never returned by any tool; they are injected as SECRET_<name> env vars at step execution time.",
			}, "", "  ")
			return string(out), nil
		},
	); err != nil {
		return fmt.Errorf("register list_secrets: %w", err)
	}

	if err := registerTool(
		"set_user_secret",
		"Create or update a user-owned secret. The value is AES-256-GCM encrypted with the user's key and stored server-side. Use for API keys, tokens, credentials the workflow needs. REJECTS names that collide with global (env-backed) secrets — those are managed by the operator, not the user. In workflow-builder/workshop contexts the secret is AUTO-ATTACHED to the active workflow and injected into the live shell env, so $SECRET_<NAME> is usable immediately in this session — no separate update_workflow_config call needed.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Secret name. Becomes SECRET_<name> env var at execution. Use UPPER_SNAKE_CASE (e.g. SLACK_TOKEN, OPENAI_API_KEY).",
				},
				"value": map[string]interface{}{
					"type":        "string",
					"description": "Plaintext secret value. Encrypted before storage; not logged.",
				},
			},
			"required": []string{"name", "value"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			name, _ := args["name"].(string)
			value, _ := args["value"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "Error: 'name' is required.", nil
			}
			if value == "" {
				return "Error: 'value' is required (use delete_user_secret to remove a secret).", nil
			}
			if isGlobalName(name) {
				return fmt.Sprintf("Error: %q is a GLOBAL secret (read-only, managed via GLOBAL_SECRET_%s env var). Pick a different name or ask the operator to update the env var.", name, name), nil
			}
			encrypted, err := encryptValue(value)
			if err != nil {
				return "", fmt.Errorf("encrypt failed: %w", err)
			}
			if err := api.chatStore.UpsertUserSecret(ctx, userID, name, encrypted); err != nil {
				return "", fmt.Errorf("store secret: %w", err)
			}
			if afterUpsert != nil {
				if hookErr := afterUpsert(ctx, name, value); hookErr != nil {
					return fmt.Sprintf("✅ Stored user secret %q (encrypted), but auto-attach to the workflow failed: %v\nAttach it manually with update_workflow_config(add_secrets=[%q]) so steps receive $SECRET_%s.", name, hookErr, name, name), nil
				}
				return fmt.Sprintf("✅ Stored user secret %q (encrypted) and attached it to the active workflow. $SECRET_%s is available in this session now.", name, name), nil
			}
			return fmt.Sprintf("✅ Stored user secret %q (encrypted). If this is for a workflow, attach it with update_workflow_config add_secrets=[%q].", name, name), nil
		},
	); err != nil {
		return fmt.Errorf("register set_user_secret: %w", err)
	}

	if err := registerTool(
		"delete_user_secret",
		"Delete a user-owned secret from the encrypted store. Only user secrets can be deleted; global (env-backed) secrets are read-only. Does NOT detach the secret from workflows that reference it — call update_workflow_config remove_secrets separately if needed.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the user secret to delete.",
				},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			name, _ := args["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "Error: 'name' is required.", nil
			}
			if isGlobalName(name) {
				return fmt.Sprintf("Error: %q is a GLOBAL secret and cannot be deleted via chat. Global secrets are managed via GLOBAL_SECRET_%s env var on the server.", name, name), nil
			}
			if err := api.chatStore.DeleteUserSecret(ctx, userID, name); err != nil {
				return "", fmt.Errorf("delete secret: %w", err)
			}
			detached := false
			if afterDelete != nil {
				if hookErr := afterDelete(ctx, name); hookErr != nil {
					return fmt.Sprintf("✅ Deleted user secret %q from store, but workshop cleanup failed: %v\nYou may need to run update_workflow_config(remove_secrets=[%q]) manually.", name, hookErr, name), nil
				}
				detached = true
			}
			if detached {
				return fmt.Sprintf("✅ Deleted user secret %q and detached from the active workflow. $SECRET_%s is no longer available in this session.", name, name), nil
			}
			return fmt.Sprintf("✅ Deleted user secret %q. If any workflow still references it, run update_workflow_config(remove_secrets=[%q]) to detach.", name, name), nil
		},
	); err != nil {
		return fmt.Errorf("register delete_user_secret: %w", err)
	}

	if strings.TrimSpace(workflowPath) != "" {
		if err := registerTool(
			"set_workflow_secret",
			"Create or update a secret scoped only to the active workflow. The value is AES-256-GCM encrypted and stored server-side under this workflow/user scope, so other workflows cannot list or attach it. The secret is AUTO-ATTACHED to the active workflow and injected into the live shell env, so $SECRET_<NAME> is usable immediately in this session — no separate update_workflow_config call needed.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Secret name. Becomes SECRET_<name> env var at execution. Use UPPER_SNAKE_CASE (e.g. SLACK_TOKEN, OPENAI_API_KEY).",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "Plaintext secret value. Encrypted before storage; not logged.",
					},
				},
				"required": []string{"name", "value"},
			},
			func(ctx context.Context, args map[string]interface{}) (string, error) {
				name, _ := args["name"].(string)
				value, _ := args["value"].(string)
				name = strings.TrimSpace(name)
				if name == "" {
					return "Error: 'name' is required.", nil
				}
				if value == "" {
					return "Error: 'value' is required (use delete_workflow_secret to remove a workflow secret).", nil
				}
				encrypted, err := encryptValue(value)
				if err != nil {
					return "", fmt.Errorf("encrypt failed: %w", err)
				}
				if err := api.chatStore.UpsertWorkflowSecret(ctx, userID, workflowPath, name, encrypted); err != nil {
					return "", fmt.Errorf("store workflow secret: %w", err)
				}
				if afterUpsert != nil {
					if hookErr := afterUpsert(ctx, name, value); hookErr != nil {
						return fmt.Sprintf("✅ Stored workflow secret %q (encrypted) for %s, but auto-attach failed: %v\nAttach it manually with update_workflow_config(add_secrets=[%q]) so steps receive $SECRET_%s.", name, workflowPath, hookErr, name, name), nil
					}
					return fmt.Sprintf("✅ Stored workflow secret %q (encrypted) for %s and attached it. $SECRET_%s is available in this session now.", name, workflowPath, name), nil
				}
				return fmt.Sprintf("✅ Stored workflow-scoped secret %q (encrypted) for %s. Attach it with update_workflow_config add_secrets=[%q] so steps receive $SECRET_%s.", name, workflowPath, name, name), nil
			},
		); err != nil {
			return fmt.Errorf("register set_workflow_secret: %w", err)
		}

		if err := registerTool(
			"delete_workflow_secret",
			"Delete a workflow-scoped secret from the encrypted store. Does NOT detach the name from workflows that reference it — call update_workflow_config remove_secrets separately if needed.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the workflow-scoped secret to delete.",
					},
				},
				"required": []string{"name"},
			},
			func(ctx context.Context, args map[string]interface{}) (string, error) {
				name, _ := args["name"].(string)
				name = strings.TrimSpace(name)
				if name == "" {
					return "Error: 'name' is required.", nil
				}
				if err := api.chatStore.DeleteWorkflowSecret(ctx, userID, workflowPath, name); err != nil {
					return "", fmt.Errorf("delete workflow secret: %w", err)
				}
				detached := false
				if afterDelete != nil {
					if hookErr := afterDelete(ctx, name); hookErr != nil {
						return fmt.Sprintf("✅ Deleted workflow secret %q from store, but workshop cleanup failed: %v\nYou may need to run update_workflow_config(remove_secrets=[%q]) manually.", name, hookErr, name), nil
					}
					detached = true
				}
				if detached {
					return fmt.Sprintf("✅ Deleted workflow secret %q and detached from the active workflow. $SECRET_%s is no longer available in this session.", name, name), nil
				}
				return fmt.Sprintf("✅ Deleted workflow secret %q. If this workflow still references it, run update_workflow_config(remove_secrets=[%q]) to detach.", name, name), nil
			},
		); err != nil {
			return fmt.Errorf("register delete_workflow_secret: %w", err)
		}
	}

	return nil
}
