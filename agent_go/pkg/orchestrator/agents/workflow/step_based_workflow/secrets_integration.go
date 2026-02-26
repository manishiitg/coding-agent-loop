package step_based_workflow

import (
	"fmt"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// GetEffectiveSecrets returns the secrets stored on the orchestrator.
// Unlike skills, secrets have no step-level override — they are always orchestrator-level.
func GetEffectiveSecrets(bo *orchestrator.BaseOrchestrator) []orchestrator.SecretEntry {
	return bo.GetSecrets()
}

// BuildWorkflowSecretPrompt builds the system prompt section with secret values.
// Secrets can contain any text (not just key-value pairs), so each entry is
// rendered as a named block that the agent can reference.
func BuildWorkflowSecretPrompt(secrets []orchestrator.SecretEntry) string {
	if len(secrets) == 0 {
		return ""
	}

	var parts []string
	parts = append(parts, `
## 🔐 Secrets

The following secrets/credentials have been provided for this task. Use them as needed.
These are also available as environment variables in execute_shell_command (e.g., os.environ["SECRET_NAME"] in Python or $SECRET_NAME in bash).
`)

	for _, s := range secrets {
		parts = append(parts, fmt.Sprintf("### %s\n```\n%s\n```", s.Name, s.Value))
	}

	return strings.Join(parts, "\n")
}
