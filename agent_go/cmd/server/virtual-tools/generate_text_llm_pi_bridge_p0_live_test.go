//go:build generate_text_llm_pi_bridge_p0_live

package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/agentreview"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

var generateTextLLMToolP0Criteria = []string{
	"for each of low, medium, and high, Pi-dev invoked generate_text_llm exactly once through the real MCP bridge rather than answering directly",
	"each requested tier returned a non-empty provider/model identity and its own real canary response",
	"each Pi-dev final answer relayed its canary from the corresponding tool result without credentials or bridge transport noise",
}

var generateTextLLMP0Tiers = []string{"low", "medium", "high"}

type generateTextLLMP0TierRun struct {
	Tier          string                `json:"tier"`
	Prompt        string                `json:"prompt"`
	ToolCalls     int                   `json:"tool_calls"`
	ToolResult    generateTextLLMResult `json:"tool_result"`
	FinalResponse string                `json:"final_response"`
}

// TestGenerateTextLLMPiBridgeP0 is the durable cross-repository P0 for this
// special tool: Pi-dev → mcpagent mcpbridge → builder generate_text_llm →
// each configured tier → actual response. It captures an agent-review receipt
// that the no-network release gate validates.
func TestGenerateTextLLMPiBridgeP0(t *testing.T) {
	if os.Getenv("RUN_GENERATE_TEXT_LLM_PI_BRIDGE_P0") != "1" {
		t.Skip("set RUN_GENERATE_TEXT_LLM_PI_BRIDGE_P0=1 to run the real Pi-dev generate_text_llm P0")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skip("Pi CLI is required for the Pi-dev generate_text_llm P0")
	}
	apiKey := strings.TrimSpace(os.Getenv("PI_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if apiKey == "" {
		t.Skip("PI_API_KEY or GEMINI_API_KEY is required for the Pi-dev generate_text_llm P0")
	}

	workspaceURL := strings.TrimSpace(os.Getenv("GENERATE_TEXT_LLM_P0_WORKSPACE_URL"))
	if workspaceURL == "" {
		workspaceURL = strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	}
	if workspaceURL == "" {
		workspaceURL = "http://127.0.0.1:18744"
	}

	bridgeBin := buildPiBridgeBinary(t)
	t.Setenv("MCP_BRIDGE_BINARY", bridgeBin)
	apiURL, apiToken := startPiBridgeExecutor(t)

	modelID := strings.TrimSpace(os.Getenv("GENERATE_TEXT_LLM_PI_P0_MODEL"))
	if modelID == "" {
		modelID = "google/gemini-3.8-flash"
	}
	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderPiCLI,
		ModelID:  modelID,
		APIKeys:  &llm.ProviderAPIKeys{PiCLI: &apiKey},
		Logger:   loggerv2.NewDefault(),
	})
	if err != nil {
		t.Fatalf("initialize Pi CLI: %v", err)
	}

	tierProvider := strings.TrimSpace(os.Getenv("GENERATE_TEXT_LLM_P0_TIER_PROVIDER"))
	tierModelID := strings.TrimSpace(os.Getenv("GENERATE_TEXT_LLM_P0_TIER_MODEL"))
	if tierProvider == "" || tierModelID == "" {
		t.Skip("GENERATE_TEXT_LLM_P0_TIER_PROVIDER and GENERATE_TEXT_LLM_P0_TIER_MODEL are required as the current-workflow tier fixture")
	}
	tierModel := &TierModel{Provider: tierProvider, ModelID: tierModelID}
	executor := createGenerateTextLLMExecutor(workspaceURL, &WorkflowLLMTierConfig{
		High: tierModel, Medium: tierModel, Low: tierModel,
	})
	runs := make([]generateTextLLMP0TierRun, 0, len(generateTextLLMP0Tiers))
	for _, tier := range generateTextLLMP0Tiers {
		runs = append(runs, runGenerateTextLLMPiBridgeP0Tier(t, model, apiKey, apiURL, apiToken, executor, tier))
	}

	record := agentreview.WriteWithCriteria(t,
		"TestGenerateTextLLMPiBridgeP0",
		"Pi-dev invoked builder generate_text_llm through mcpagent's api bridge and relayed real responses from low, medium, and high configured tiers.",
		generateTextLLMToolP0Criteria,
		map[string]any{"runs": runs},
		map[string]any{"provider": "pi-cli", "tool": "generate_text_llm", "tiers": generateTextLLMP0Tiers, "workspace_url": workspaceURL},
	)
	agentreview.RequireReviewed(t, record)
}

func runGenerateTextLLMPiBridgeP0Tier(
	t *testing.T,
	model llmtypes.Model,
	apiKey, apiURL, apiToken string,
	executor func(context.Context, map[string]any) (string, error),
	tier string,
) generateTextLLMP0TierRun {
	t.Helper()
	canary := "GENERATE_TEXT_LLM_P0_" + strings.ToUpper(tier) + "_OK"
	toolMessage := "Reply with exactly " + canary + ". Do not use markdown or tools."
	toolCalls := 0
	var toolResult generateTextLLMResult
	agent, err := mcpagent.NewAgentFromDefinition(context.Background(), mcpagent.AgentDefinition{
		Instructions: "You are a tool-routing agent. Call generate_text_llm exactly once with tier " + tier + " and the exact user_message supplied by the user. Do not answer directly and do not use provider-native tools. In your final answer, repeat the response field from the tool result exactly.",
		Tools: mcpagent.ToolSet{Direct: []mcpagent.ToolDefinition{{
			Name:         "generate_text_llm",
			Description:  "Generate text through the configured workspace tier. Required arguments: user_message and tier=" + tier + ".",
			DisplayGroup: "workspace_advanced",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"user_message": map[string]interface{}{"type": "string"},
					"tier":         map[string]interface{}{"type": "string", "enum": []string{tier}},
				},
				"required": []string{"user_message", "tier"},
			},
			Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
				toolCalls++
				if got := strings.TrimSpace(fmt.Sprint(args["tier"])); got != tier {
					return "", fmt.Errorf("generate_text_llm tier = %q, want %q", got, tier)
				}
				if got := strings.TrimSpace(fmt.Sprint(args["user_message"])); got != toolMessage {
					return "", fmt.Errorf("generate_text_llm user_message = %q, want exact P0 canary prompt", got)
				}
				raw, err := executor(ctx, args)
				if err != nil {
					return "", err
				}
				if err := json.Unmarshal([]byte(raw), &toolResult); err != nil {
					return "", fmt.Errorf("decode generate_text_llm payload: %w", err)
				}
				return raw, nil
			},
		}}},
	}, mcpagent.RuntimeConfig{
		Model: model,
		Generation: mcpagent.GenerationRuntimeConfig{
			Provider: llm.ProviderPiCLI,
			MaxTurns: 6,
			APIKeys:  &mcpagent.AgentAPIKeys{PiCLI: &apiKey},
		},
		Tools:  mcpagent.ToolRuntimeConfig{CodeExecution: true, AdditionalBridge: []string{"generate_text_llm"}},
		Coding: mcpagent.CodingRuntimeConfig{AgentToolsMode: "mcp_only"},
		MCP: mcpagent.MCPRuntimeConfig{
			SessionID:  "generate-text-llm-pi-p0-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + tier,
			APIBaseURL: apiURL,
			APIToken:   apiToken,
		},
		Workspace:     mcpagent.WorkspaceRuntimeConfig{CodingAgentWorkingDir: t.TempDir(), IsolatedSession: true},
		Observability: mcpagent.ObservabilityRuntimeConfig{Logger: loggerv2.NewDefault()},
	})
	if err != nil {
		t.Fatalf("create Pi-dev mcpagent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	result, err := agent.Run(ctx, mcpagent.Turn{Input: toolMessage})
	if err != nil {
		t.Fatalf("Pi-dev generate_text_llm %s-tier turn: %v", tier, err)
	}
	if toolCalls != 1 {
		t.Fatalf("Pi-dev generate_text_llm %s-tier calls = %d, want 1; output: %s", tier, toolCalls, truncateMCPTestResult(result.Text, 1_000))
	}
	if strings.TrimSpace(toolResult.Tier) != tier || strings.TrimSpace(toolResult.Provider) == "" || strings.TrimSpace(toolResult.ModelID) == "" {
		t.Fatalf("generate_text_llm did not return configured %s-tier identity: %#v", tier, toolResult)
	}
	if !strings.Contains(toolResult.Response, canary) {
		t.Fatalf("%s-tier response lacks P0 canary: %q", tier, toolResult.Response)
	}
	if !strings.Contains(result.Text, canary) {
		t.Fatalf("Pi-dev %s-tier final answer did not relay tool canary: %s", tier, truncateMCPTestResult(result.Text, 1_000))
	}
	return generateTextLLMP0TierRun{
		Tier:          tier,
		Prompt:        toolMessage,
		ToolCalls:     toolCalls,
		ToolResult:    toolResult,
		FinalResponse: result.Text,
	}
}
