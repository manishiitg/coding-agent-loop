package agent

import "testing"

func TestRuntimeConfigProjectsAdditionalBridgeTools(t *testing.T) {
	config := LLMAgentConfig{
		AdditionalBridgeTools: []string{"read_image"},
	}

	runtime := runtimeConfigForLLMAgent(config, nil, nil, "", nil)
	if len(runtime.Tools.AdditionalBridge) != 1 || runtime.Tools.AdditionalBridge[0] != "read_image" {
		t.Fatalf("additional bridge tools = %v, want [read_image]", runtime.Tools.AdditionalBridge)
	}
}
