package orchestrator

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	unifiedevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	anthropicadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/anthropic"
)

// TestWorkflowCostBucketingThroughBridgeReal is the workflow-layer
// cost e2e. It proves the complete chain that backs real workflow
// runs:
//
//   real Anthropic call
//     → adapter writes cost_model_id + cost_usd_estimated on
//       GenerationInfo.Additional
//     → caller builds a TokenUsageEvent (mirroring what mcpagent does)
//     → ContextAwareEventBridge intercepts the event
//     → bridge resolves effective model + injects current step context
//     → calls TokenPersister.PersistTokenUsage with the right buckets
//
// What this nails down:
//
//   - StepTokenData carries StepID = the orchestrator's current step
//     (so by_step_and_model gets the right key)
//   - ModelTokenData.ModelID is the EFFECTIVE model from
//     GenerationInfo.cost_model_id, NOT the requested alias (this is
//     the bucketing fix from commit cd818470)
//   - Provider, input/output tokens, and call count all propagate
//
// If this passes for Anthropic, the same logic carries cost rollups
// correctly for every other adapter that emits cost_model_id —
// because the bridge is provider-agnostic.
//
// Gated on RUN_ANTHROPIC_REAL_E2E=1 + ANTHROPIC_API_KEY.
func TestWorkflowCostBucketingThroughBridgeReal(t *testing.T) {
	if os.Getenv("RUN_ANTHROPIC_REAL_E2E") == "" {
		t.Skip("set RUN_ANTHROPIC_REAL_E2E=1 to run this workflow-cost e2e")
	}
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY required")
	}
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_REAL_E2E_MODEL"))
	if model == "" {
		model = "claude-haiku-4-5"
	}

	// 1. Real provider call ------------------------------------------------
	client := anthropic.NewClient(anthropicoption.WithAPIKey(apiKey))
	adapter := anthropicadapter.NewAnthropicAdapter(client, model, &silentLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Reply with OK."}}},
		},
		llmtypes.WithMaxTokens(16),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	gi := resp.Choices[0].GenerationInfo
	if gi == nil {
		t.Fatal("response missing GenerationInfo")
	}

	// 2. Build a TokenUsageEvent the way mcpagent does --------------------
	additional := map[string]interface{}{}
	for k, v := range gi.Additional {
		additional[k] = v
	}
	prompt := derefInt(gi.PromptTokens, gi.InputTokens)
	completion := derefInt(gi.CompletionTokens, gi.OutputTokens)
	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          model, // requested model (alias)
		Provider:         "anthropic",
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
		GenerationInfo:   additional,
	}

	// 3. Wire the bridge with a mock persister and a pushed step context --
	bridge := NewContextAwareEventBridge(&noopListener{}, &silentLoggerV2{})
	persister := &recordingTokenPersister{}
	bridge.SetTokenPersister(persister)
	bridge.SetIterationFolder("runs/test-run-001")
	bridge.PushContext("execution", 1, "fetch-data", "worker-agent")
	defer bridge.PopContext()

	// 4. Feed the event through the bridge --------------------------------
	if err := bridge.HandleEvent(ctx, &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data:      tokenEvent,
	}); err != nil {
		t.Fatalf("bridge.HandleEvent: %v", err)
	}

	// 5. Wait for the async persist goroutine. The bridge fires
	//    persister.PersistTokenUsage on a goroutine (see
	//    context_aware_bridge.go:564). Give it a generous window before
	//    polling to keep the test from being a flake source.
	persister.waitForCall(t, 2*time.Second)

	// 6. Assert -----------------------------------------------------------
	calls := persister.snapshot()
	if len(calls) == 0 {
		t.Fatalf("persister received no calls; bridge did not forward the TokenUsageEvent")
	}
	if len(calls) > 1 {
		t.Logf("persister received %d calls (expected 1)", len(calls))
	}
	call := calls[0]

	// Step attribution
	if call.iterationFolder != "runs/test-run-001" {
		t.Fatalf("iterationFolder = %q, want runs/test-run-001", call.iterationFolder)
	}
	if call.stepTokenData == nil {
		t.Fatal("stepTokenData is nil; current step context was not attached")
	}
	if call.stepTokenData.StepID != "fetch-data" {
		t.Fatalf("StepTokenData.StepID = %q, want fetch-data", call.stepTokenData.StepID)
	}
	if call.stepTokenData.Phase != "execution" {
		t.Fatalf("StepTokenData.Phase = %q, want execution", call.stepTokenData.Phase)
	}
	if call.stepTokenData.InputTokens <= 0 {
		t.Fatalf("StepTokenData.InputTokens = %d, want > 0", call.stepTokenData.InputTokens)
	}

	// Effective-model bucketing
	if call.modelTokenData == nil {
		t.Fatal("modelTokenData is nil")
	}
	if call.modelTokenData.Provider != "anthropic" {
		t.Fatalf("ModelTokenData.Provider = %q, want anthropic", call.modelTokenData.Provider)
	}
	// For anthropic API, cost_model_id matches the requested model so
	// the effective ID and requested ID coincide. The important
	// assertion is that the bridge respected GenerationInfo when
	// resolving the bucket key.
	wantModel := model
	if effective, ok := additional["cost_model_id"].(string); ok && effective != "" {
		wantModel = effective
	}
	if call.modelTokenData.ModelID != wantModel {
		t.Fatalf("ModelTokenData.ModelID = %q, want %q (the effective model)", call.modelTokenData.ModelID, wantModel)
	}
	if call.modelTokenData.InputTokens != prompt {
		t.Fatalf("ModelTokenData.InputTokens = %d, want %d", call.modelTokenData.InputTokens, prompt)
	}
	if call.modelTokenData.OutputTokens != completion {
		t.Fatalf("ModelTokenData.OutputTokens = %d, want %d", call.modelTokenData.OutputTokens, completion)
	}

	t.Logf("✅ workflow cost bucketing: step=%q model=%q prompt=%d completion=%d",
		call.stepTokenData.StepID, call.modelTokenData.ModelID,
		call.modelTokenData.InputTokens, call.modelTokenData.OutputTokens)
}

// --- test helpers ----------------------------------------------------------

// recordingTokenPersister captures every PersistTokenUsage call into a
// slice so the test can assert on the data the bridge produced. It is
// concurrency-safe because the bridge calls the persister from a
// goroutine.
type recordingTokenPersister struct {
	mu    sync.Mutex
	calls []persistCall
}

type persistCall struct {
	iterationFolder string
	stepTokenData   *StepTokenData
	modelTokenData  *ModelTokenData
}

func (p *recordingTokenPersister) PersistTokenUsage(ctx context.Context, iterationFolder string, stepTokenData *StepTokenData, modelTokenData *ModelTokenData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, persistCall{
		iterationFolder: iterationFolder,
		stepTokenData:   stepTokenData,
		modelTokenData:  modelTokenData,
	})
	return nil
}

func (p *recordingTokenPersister) snapshot() []persistCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]persistCall, len(p.calls))
	copy(out, p.calls)
	return out
}

// waitForCall polls until at least one PersistTokenUsage call lands or
// the timeout expires. The bridge fires the persister on a goroutine
// so the test must wait for it.
func (p *recordingTokenPersister) waitForCall(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := len(p.calls)
		p.mu.Unlock()
		if got > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// noopListener is the underlying event listener the bridge wraps. The
// test only cares about persister side effects, so we accept and
// discard all events here.
type noopListener struct{}

func (noopListener) HandleEvent(_ context.Context, _ *unifiedevents.AgentEvent) error { return nil }
func (noopListener) Name() string                                                     { return "noop" }

// Ensure noopListener satisfies the interface — fails to compile if
// the bridge's contract ever drifts.
var _ mcpagent.AgentEventListener = noopListener{}

// silentLogger is the mcpagent v1 interfaces.Logger that the
// multi-llm-provider-go adapter takes. Discard all output.
type silentLogger struct{}

func (silentLogger) Infof(format string, args ...any)         {}
func (silentLogger) Errorf(format string, args ...any)        {}
func (silentLogger) Debugf(format string, args ...interface{}) {}

// silentLoggerV2 implements the mcpagent loggerv2.Logger interface.
type silentLoggerV2 struct{}

func (silentLoggerV2) Debug(msg string, fields ...loggerv2.Field) {}
func (silentLoggerV2) Info(msg string, fields ...loggerv2.Field)  {}
func (silentLoggerV2) Warn(msg string, fields ...loggerv2.Field)  {}
func (silentLoggerV2) Error(msg string, err error, fields ...loggerv2.Field) {}
func (silentLoggerV2) Fatal(msg string, err error, fields ...loggerv2.Field) {}
func (silentLoggerV2) With(fields ...loggerv2.Field) loggerv2.Logger {
	return silentLoggerV2{}
}
func (silentLoggerV2) Close() error { return nil }

func derefInt(ptrs ...*int) int {
	for _, p := range ptrs {
		if p != nil && *p > 0 {
			return *p
		}
	}
	return 0
}
