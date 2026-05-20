package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	opencodecliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/opencodecli"

	"mcp-agent-builder-go/agent_go/internal/inspector"
)

// TestInspectorHTTPCapturesRealOpenCodeEvents is the opencode-cli
// counterpart to TestInspectorHTTPCapturesRealAnthropicEvents. It
// proves the inspector chain works for the structured-CLI transport,
// not just the API transport:
//
//	real opencode run
//	  → opencode-cli adapter emits InspectorEvents at request/completion
//	  → ScopedInspectorSink injects StepContext
//	  → inspector.Store.Sink() forwards into the per-session ring
//	  → handleInspectorEvents reads the ring and serves JSON
//
// The opencode adapter routes through WithObservability the same way
// the API adapters do (opencodecli_adapter.go:84), so if this test
// fails the regression is either in the adapter's observability
// wiring or in the builder's session bucketing.
//
// Gated on RUN_OPENCODE_CLI_REAL_E2E=1 + ZHIPU_API_KEY (for the
// GLM coding-plan sub-provider tile) + opencode binary in PATH.
func TestInspectorHTTPCapturesRealOpenCodeEvents(t *testing.T) {
	if os.Getenv("RUN_OPENCODE_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_OPENCODE_CLI_REAL_E2E=1 to run this inspector HTTP e2e")
	}
	apiKey := strings.TrimSpace(os.Getenv("ZHIPU_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ZAI_API_KEY"))
	}
	if apiKey == "" {
		t.Skip("ZHIPU_API_KEY (or ZAI_API_KEY) required for opencode-cli inspector e2e")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skipf("opencode binary not found: %v", err)
	}

	// Spin up an isolated API with an inspector store.
	store := inspector.NewStore()
	api := &StreamingAPI{inspectorStore: store}

	const sessionID = "test-session-inspector-opencode-001"

	stepCtx := llmtypes.StepContext{
		SessionID:     sessionID,
		StepID:        "opencode-glm-call",
		StepType:      "regular",
		Phase:         "execution",
		StepName:      "OpenCode GLM call",
		StepIndex:     1,
		StepTotal:     1,
		StepStartedAt: time.Now().UTC(),
		AgentName:     "worker",
		Attempt:       1,
		CallPurpose:   "main_generation",
		WorkflowName:  "test-inspector-opencode-workflow",
	}
	sink := llmtypes.NewScopedInspectorSink(store.Sink(), stepCtx)

	// Real opencode-cli call scoped to the GLM coding-plan tile. We
	// pick this tile because (a) it's the one the user actually has
	// a verified working key for, and (b) it exercises the new
	// opencode-cli-glm-coding-plan entry added in opencodecli_subproviders.go.
	adapter := opencodecliadapter.NewOpenCodeCLIAdapter("", "opencode-cli", &e2eMockLogger{})

	// opencode is slow on cold start (~20-30s typical, plus possible
	// silent-empty retry inside the adapter); budget generously.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	_, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
			}},
		},
		opencodecliadapter.WithOpenCodeSubProvider("opencode-cli-glm-coding-plan"),
		opencodecliadapter.WithOpenCodeSubProviderAPIKey("ZHIPU_API_KEY", apiKey),
		opencodecliadapter.WithOpenCodeModel("medium"),
		llmtypes.WithInspectorSink(sink),
	)
	if err != nil {
		t.Fatalf("opencode GenerateContent: %v", err)
	}

	// HTTP roundtrip: /api/inspector/<session_id>
	req := httptest.NewRequest(http.MethodGet, "/api/inspector/"+sessionID, nil)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rec := httptest.NewRecorder()
	api.handleInspectorEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/api/inspector/<sess> status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var summary inspectorSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v\nbody=%s", err, rec.Body.String())
	}

	if summary.SessionID != sessionID {
		t.Fatalf("summary.SessionID = %q, want %q", summary.SessionID, sessionID)
	}
	if summary.Count == 0 {
		t.Fatal("no events in summary; opencode adapter→sink→store chain not working")
	}
	if summary.LatestSeq == 0 {
		t.Fatal("LatestSeq = 0; store didn't assign sequence numbers")
	}

	// Phase ordering: request first, completion last, no errors.
	first := summary.Events[0].Event
	last := summary.Events[len(summary.Events)-1].Event
	if first.Phase != llmtypes.InspectorPhaseRequest {
		t.Fatalf("first phase = %q, want request", first.Phase)
	}
	if last.Phase != llmtypes.InspectorPhaseCompletion {
		t.Fatalf("last phase = %q, want completion", last.Phase)
	}
	for i, se := range summary.Events {
		if se.Event.Phase == llmtypes.InspectorPhaseError {
			t.Fatalf("events[%d] unexpectedly carries an error phase: %+v", i, se.Event)
		}
		if se.Event.Provider != "opencode-cli" {
			t.Fatalf("events[%d].Provider = %q, want opencode-cli", i, se.Event.Provider)
		}
	}

	// Step attribution: every event must carry the scope's StepID and
	// WorkflowName — regression for the scope→event plumbing.
	for i, se := range summary.Events {
		if se.Event.StepContext.SessionID != sessionID {
			t.Fatalf("events[%d].StepContext.SessionID = %q, want %q", i, se.Event.StepContext.SessionID, sessionID)
		}
		if se.Event.StepContext.StepID != "opencode-glm-call" {
			t.Fatalf("events[%d].StepContext.StepID = %q, want opencode-glm-call", i, se.Event.StepContext.StepID)
		}
		if se.Event.StepContext.WorkflowName != "test-inspector-opencode-workflow" {
			t.Fatalf("events[%d].StepContext.WorkflowName lost", i)
		}
	}

	t.Logf("✅ opencode-cli inspector HTTP e2e: %d events captured for session %q (latest_seq=%d)",
		summary.Count, sessionID, summary.LatestSeq)
}
