package server

import (
	"context"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	unifiedevents "github.com/manishiitg/mcpagent/events"
)

func newConversationTurnQueueTestAPI(files map[string]string) *StreamingAPI {
	return &StreamingAPI{
		sessionInputLanes:              map[string]*sessionInputLane{},
		conversationTurnQueueOwners:    map[string]string{},
		conversationTurnQueueDraining:  map[string]bool{},
		conversationTurnQueueWaiters:   map[string]chan queuedConversationTurnResult{},
		conversationTurnQueueCallbacks: map[string]func(event *unifiedevents.AgentEvent){},
		internalTurnQueueRead: func(_ context.Context, path string) (string, bool, error) {
			value, ok := files[path]
			return value, ok, nil
		},
		internalTurnQueueWrite: func(_ context.Context, path, value string) error {
			files[path] = value
			return nil
		},
	}
}

func TestDurableConversationTurnQueueCoversEveryConversationProducer(t *testing.T) {
	tests := []struct {
		name string
		req  QueryRequest
		want bool
	}{
		{"ordinary chat", QueryRequest{AgentMode: "multi-agent"}, true},
		{"Crew chat", QueryRequest{AgentMode: "multi-agent", AgentProfileID: "work"}, true},
		{"workflow builder chat", QueryRequest{AgentMode: "workflow_phase", TriggeredBy: "manual"}, true},
		{"workflow schedule", QueryRequest{AgentMode: "workflow_phase", TriggeredBy: "cron"}, true},
		{"webhook trigger", QueryRequest{AgentMode: "workflow_phase", TriggeredBy: "webhook"}, true},
		{"bot turn", QueryRequest{AgentMode: "multi-agent", TriggeredBy: "bot:slack", BotPlatform: "slack"}, true},
		{"synthetic notification", QueryRequest{AgentMode: "multi-agent", IsAutoNotification: true}, false},
		{"headless workflow execution", QueryRequest{AgentMode: "workflow"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldUseDurableConversationTurnQueue(test.req); got != test.want {
				t.Fatalf("queue coverage=%v, want %v", got, test.want)
			}
		})
	}
}

func TestDurableConversationTurnQueueIsFIFOAndSurvivesAPIReplacement(t *testing.T) {
	files := map[string]string{}
	firstAPI := newConversationTurnQueueTestAPI(files)
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "u1", Username: "u1"})
	for _, message := range []string{"first", "second"} {
		_, position, err := firstAPI.enqueueConversationTurn(ctx, "u1", "session-1", QueryRequest{Query: message, AgentMode: "multi-agent"})
		if err != nil {
			t.Fatal(err)
		}
		if position != len(firstAPI.mustReadTurnQueueForTest(t, "u1")) {
			t.Fatalf("position=%d for %q", position, message)
		}
	}

	// A new API instance has no in-memory queue state. The workspace document
	// remains the source of truth and preserves submission order.
	restarted := newConversationTurnQueueTestAPI(files)
	first, ok := restarted.claimNextConversationTurn(context.Background(), "u1", "session-1")
	if !ok || first.Request.Query != "first" || first.StartedAt == nil {
		t.Fatalf("first=%+v ok=%v", first, ok)
	}
	if err := restarted.removeConversationTurn(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second, ok := restarted.claimNextConversationTurn(context.Background(), "u1", "session-1")
	if !ok || second.Request.Query != "second" {
		t.Fatalf("second=%+v ok=%v", second, ok)
	}
}

func TestDurableConversationTurnQueueNeverPersistsResolvedSecrets(t *testing.T) {
	files := map[string]string{}
	api := newConversationTurnQueueTestAPI(files)
	secret := "provider-secret"
	req := QueryRequest{Query: "hello", AgentMode: "multi-agent", LLMConfig: &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{Provider: "openai", ModelID: "gpt", APIKey: &secret},
		APIKeys: &orchestrator.APIKeys{OpenAI: &secret},
	}}
	req.DecryptedSecrets = append(req.DecryptedSecrets, struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}{Name: "TOKEN", Value: "workspace-secret"})
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "u1"})
	if _, _, err := api.enqueueConversationTurn(ctx, "u1", "session-1", req); err != nil {
		t.Fatal(err)
	}
	raw := files[conversationTurnQueuePath("u1")]
	if strings.Contains(raw, secret) || strings.Contains(raw, "workspace-secret") {
		t.Fatalf("queue persisted resolved secret: %s", raw)
	}
}

func (api *StreamingAPI) mustReadTurnQueueForTest(t *testing.T, userID string) []queuedConversationTurn {
	t.Helper()
	turns, err := api.readConversationTurnQueue(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return turns
}
