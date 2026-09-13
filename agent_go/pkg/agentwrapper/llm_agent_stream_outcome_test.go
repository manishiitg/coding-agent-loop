package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

type outcomeErrorModel struct{}

func (outcomeErrorModel) GenerateContent(context.Context, []llmtypes.MessageContent, ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	return nil, errors.New("turn already occupied")
}
func (outcomeErrorModel) GetModelID() string { return "outcome-error-test" }
func (outcomeErrorModel) GetModelMetadata(string) (*llmtypes.ModelMetadata, error) {
	return &llmtypes.ModelMetadata{ModelID: "outcome-error-test"}, nil
}

// P0: StreamWithEvents used to close normally after Session.Run failed, causing
// the background notification caller to mark a rejected turn delivered. The
// terminal outcome must preserve that asynchronous error for retry logic.
func TestP0StreamWithEventsAndOutcomeReportsAsynchronousTurnFailure(t *testing.T) {
	const sessionID = "wrapper-stream-outcome-error"
	mcpagent.CloseSession(sessionID)
	t.Cleanup(func() { mcpagent.CloseSession(sessionID) })
	runtime := mcpagent.RuntimeConfig{
		Model:         outcomeErrorModel{},
		Generation:    mcpagent.GenerationRuntimeConfig{MaxTurns: 1},
		MCP:           mcpagent.MCPRuntimeConfig{SessionID: sessionID},
		Observability: mcpagent.ObservabilityRuntimeConfig{Logger: loggerv2.NewNoop()},
	}
	draft, err := mcpagent.NewAgentFromDefinition(context.Background(), mcpagent.AgentDefinition{}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &LLMAgentWrapper{
		agent: draft, config: LLMAgentConfig{},
		metrics: &agentMetricsImpl{MinLatency: time.Duration(^uint64(0) >> 1)},
		logger:  loggerv2.NewNoop(), runtime: runtime, definition: mcpagent.AgentDefinition{},
	}
	defer wrapper.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	chunks, outcome, err := wrapper.StreamWithEventsAndOutcome(ctx, "deliver completion")
	if err != nil {
		t.Fatalf("setup error = %v", err)
	}
	for range chunks {
	}
	turnErr := <-outcome
	if turnErr == nil {
		t.Fatalf("terminal outcome = %v, want asynchronous turn failure", turnErr)
	}
}
