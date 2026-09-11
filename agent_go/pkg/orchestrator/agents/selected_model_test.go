package agents

import (
	"strings"
	"testing"
)

func TestCreateLLMRequiresSelectedProviderAndModel(t *testing.T) {
	for _, tc := range []struct{ name, provider, model string }{
		{"empty", "", ""},
		{"missing provider", "", "selected"},
		{"missing model", "codex-cli", ""},
		{"blank provider", " ", "selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &OrchestratorAgentConfig{}
			config.LLMConfig.Primary.Provider = tc.provider
			config.LLMConfig.Primary.ModelID = tc.model
			agent := &BaseOrchestratorAgent{config: config}
			model, err := agent.createLLM()
			if model != nil || err == nil || !strings.Contains(err.Error(), "selected LLM") {
				t.Fatalf("incomplete selection should fail before provider initialization: model=%v err=%v", model, err)
			}
		})
	}
	if _, err := (&BaseOrchestratorAgent{}).createLLM(); err == nil {
		t.Fatal("missing configuration should fail")
	}
}
