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

// BuildWorkflowSecretPrompt builds the system prompt section with secret names only.
// Secret values must never be rendered into prompts or logs. Agents should read
// them from the injected environment at execution time.
func BuildWorkflowSecretPrompt(secrets []orchestrator.SecretEntry) string {
	if len(secrets) == 0 {
		return ""
	}

	var parts []string
	parts = append(parts, `
## Secrets

The following secret names are available for this task. Their raw values are intentionally hidden from the prompt and logs.
Read secrets only from the injected environment inside execute_shell_command. Never print, echo, or hardcode secret values.

Use the secret name shown below to access the corresponding injected env var at runtime.
`)

	for _, s := range secrets {
		parts = append(parts, fmt.Sprintf("- `%s`", s.Name))
	}

	return strings.Join(parts, "\n")
}
