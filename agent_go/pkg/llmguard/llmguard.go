// Package llmguard enforces that agents run only through coding-agent CLIs.
package llmguard

import (
	"fmt"
	"strings"

	"github.com/manishiitg/mcpagent/llm"
)

// IsCodingAgentProvider reports whether provider names a coding-agent CLI.
func IsCodingAgentProvider(provider string) bool {
	return llm.IsCodingAgentProvider(llm.Provider(normalize(provider)), "")
}

// CodingAgentProviders lists the coding-agent CLI providers, sorted.
func CodingAgentProviders() []string {
	contracts := llm.CodingAgentProviderContracts()
	providers := make([]string, 0, len(contracts))
	seen := make(map[string]bool, len(contracts))
	for _, contract := range contracts {
		name := string(contract.Provider)
		if !seen[name] {
			seen[name] = true
			providers = append(providers, name)
		}
	}
	return providers
}

// RequireCodingAgentProvider rejects direct-API providers (openai, anthropic,
// vertex, bedrock, ...). Their native loop is unmaintained and no longer offered.
func RequireCodingAgentProvider(provider string) error {
	if IsCodingAgentProvider(provider) {
		return nil
	}
	return fmt.Errorf("LLM provider %q is not supported: agents run only through coding-agent CLIs (%s)",
		strings.TrimSpace(provider), strings.Join(CodingAgentProviders(), ", "))
}

func normalize(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
