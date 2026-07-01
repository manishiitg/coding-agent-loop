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
	picliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/picli"

	"mcp-agent-builder-go/agent_go/internal/inspector"
)

// TestInspectorHTTPCapturesRealPiEvents is the pi-cli
// counterpart to TestInspectorHTTPCapturesRealAnthropicEvents. It
// proves the inspector chain works for the Pi CLI transport, not just
// the API transport.
func TestInspectorHTTPCapturesRealPiEvents(t *testing.T) {
	if os.Getenv("RUN_PI_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_PI_CLI_REAL_E2E=1 to run this inspector HTTP e2e")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi binary not found: %v", err)
	}
	apiKey := firstNonEmptyInspectorPiEnv("GEMINI_API_KEY", "GOOGLE_API_KEY", "PI_API_KEY")
	modelID := strings.TrimSpace(os.Getenv("PI_CLI_REAL_CONTRACT_MODEL"))
	if modelID == "" {
		modelID = picliadapter.DefaultModelID
	}

	// Spin up an isolated API with an inspector store.
	store := inspector.NewStore()
	api := &StreamingAPI{inspectorStore: store}

	const sessionID = "test-session-inspector-pi-001"

	stepCtx := llmtypes.StepContext{
		SessionID:     sessionID,
		StepID:        "pi-call",
		StepType:      "regular",
		Phase:         "execution",
		StepName:      "Pi CLI call",
		StepIndex:     1,
		StepTotal:     1,
		StepStartedAt: time.Now().UTC(),
		AgentName:     "worker",
		Attempt:       1,
		CallPurpose:   "main_generation",
		WorkflowName:  "test-inspector-pi-workflow",
	}
	sink := llmtypes.NewScopedInspectorSink(store.Sink(), stepCtx)

	adapter := picliadapter.NewPiCLIAdapter(apiKey, modelID, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	_, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
			}},
		},
		llmtypes.WithInspectorSink(sink),
	)
	if err != nil {
		t.Fatalf("pi GenerateContent: %v", err)
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
		t.Fatal("no events in summary; pi adapter→sink→store chain not working")
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
		if se.Event.Provider != "pi-cli" {
			t.Fatalf("events[%d].Provider = %q, want pi-cli", i, se.Event.Provider)
		}
	}

	// Step attribution: every event must carry the scope's StepID and
	// WorkflowName — regression for the scope→event plumbing.
	for i, se := range summary.Events {
		if se.Event.StepContext.SessionID != sessionID {
			t.Fatalf("events[%d].StepContext.SessionID = %q, want %q", i, se.Event.StepContext.SessionID, sessionID)
		}
		if se.Event.StepContext.StepID != "pi-call" {
			t.Fatalf("events[%d].StepContext.StepID = %q, want pi-call", i, se.Event.StepContext.StepID)
		}
		if se.Event.StepContext.WorkflowName != "test-inspector-pi-workflow" {
			t.Fatalf("events[%d].StepContext.WorkflowName lost", i)
		}
	}

	t.Logf("✅ pi-cli inspector HTTP e2e: %d events captured for session %q (latest_seq=%d)",
		summary.Count, sessionID, summary.LatestSeq)
}

func firstNonEmptyInspectorPiEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
