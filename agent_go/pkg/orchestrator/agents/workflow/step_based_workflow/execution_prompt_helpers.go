package step_based_workflow

import mcpagent "github.com/manishiitg/mcpagent/agent"

// composePromptWithAppendedSystemPrompts reconstructs the effective system prompt
// seen by the agent after SetSystemPrompt re-appends tracked supplementary prompts.
func composePromptWithAppendedSystemPrompts(basePrompt string, agent *mcpagent.Agent) string {
	if agent == nil {
		return basePrompt
	}

	composed := basePrompt
	for _, prompt := range agent.GetAppendedSystemPrompts() {
		if prompt == "" {
			continue
		}
		if composed == "" {
			composed = prompt
			continue
		}
		composed += "\n\n" + prompt
	}

	return composed
}
