package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	unifiedevents "github.com/manishiitg/mcpagent/events"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
)

func TestExtractCacheTokens(t *testing.T) {
	tests := []struct {
		name      string
		info      map[string]interface{}
		wantRead  int
		wantWrite int
	}{
		{
			name:      "nil info returns zeros",
			info:      nil,
			wantRead:  0,
			wantWrite: 0,
		},
		{
			name:      "empty info returns zeros",
			info:      map[string]interface{}{},
			wantRead:  0,
			wantWrite: 0,
		},
		{
			name: "float64 values (JSON default)",
			info: map[string]interface{}{
				"cache_read_input_tokens":     float64(1500),
				"cache_creation_input_tokens": float64(200),
			},
			wantRead:  1500,
			wantWrite: 200,
		},
		{
			name: "int values",
			info: map[string]interface{}{
				"cache_read_input_tokens":     1500,
				"cache_creation_input_tokens": 200,
			},
			wantRead:  1500,
			wantWrite: 200,
		},
		{
			name: "json.Number values",
			info: map[string]interface{}{
				"cache_read_input_tokens":     json.Number("1500"),
				"cache_creation_input_tokens": json.Number("200"),
			},
			wantRead:  1500,
			wantWrite: 200,
		},
		{
			name: "only read tokens present",
			info: map[string]interface{}{
				"cache_read_input_tokens": float64(500),
			},
			wantRead:  500,
			wantWrite: 0,
		},
		{
			name: "string values return zero (unsupported type)",
			info: map[string]interface{}{
				"cache_read_input_tokens": "not a number",
			},
			wantRead:  0,
			wantWrite: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRead, gotWrite := extractCacheTokens(tt.info)
			if gotRead != tt.wantRead {
				t.Fatalf("read = %d, want %d", gotRead, tt.wantRead)
			}
			if gotWrite != tt.wantWrite {
				t.Fatalf("write = %d, want %d", gotWrite, tt.wantWrite)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want int
	}{
		{"float64", float64(42), 42},
		{"int", 42, 42},
		{"int64", int64(42), 42},
		{"json.Number", json.Number("42"), 42},
		{"string", "42", 0},
		{"nil", nil, 0},
		{"bool", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toInt(tt.val); got != tt.want {
				t.Fatalf("toInt(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestCostObserverHandleEventOnlyTokenUsage(t *testing.T) {
	srv := costledger.NewTestServer(t)
	defer srv.Close()
	ledger := costledger.NewLedger(srv.URL)

	obs := newCostObserver(ledger, "sess-1", "user-1", "chat")

	// Non-token-usage event should be ignored
	err := obs.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.StreamingChunk,
		Timestamp: time.Now(),
		Data: &unifiedevents.StreamingChunkEvent{
			Content: "hello",
		},
	})
	if err != nil {
		t.Fatalf("HandleEvent(streaming_chunk) error: %v", err)
	}

	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if summary.Total.CallCount != 0 {
		t.Fatalf("non-token_usage event should not produce ledger entry, got CallCount=%d", summary.Total.CallCount)
	}
}

func TestCostObserverHandleEventRecordsTokenUsage(t *testing.T) {
	srv := costledger.NewTestServer(t)
	defer srv.Close()
	ledger := costledger.NewLedger(srv.URL)

	obs := newCostObserver(ledger, "sess-1", "user-1", "chat")

	err := obs.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
		Component: "main-agent",
		Data: &unifiedevents.TokenUsageEvent{
			Provider:         "anthropic",
			ModelID:          "claude-sonnet-4-6",
			PromptTokens:     1000,
			CompletionTokens: 500,
			ReasoningTokens:  200,
			TotalCost:        0.015,
			GenerationInfo: map[string]interface{}{
				"cache_read_input_tokens":     float64(300),
				"cache_creation_input_tokens": float64(50),
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}

	if summary.Total.CallCount != 1 {
		t.Fatalf("CallCount = %d, want 1", summary.Total.CallCount)
	}
	if summary.Total.PromptTokens != 1000 {
		t.Fatalf("PromptTokens = %d, want 1000", summary.Total.PromptTokens)
	}
	if summary.Total.CompletionTokens != 500 {
		t.Fatalf("CompletionTokens = %d, want 500", summary.Total.CompletionTokens)
	}
	if summary.Total.CacheReadTokens != 300 {
		t.Fatalf("CacheReadTokens = %d, want 300", summary.Total.CacheReadTokens)
	}
	if summary.Total.CacheWriteTokens != 50 {
		t.Fatalf("CacheWriteTokens = %d, want 50", summary.Total.CacheWriteTokens)
	}

	modelAgg := summary.ByModel["claude-sonnet-4-6"]
	if modelAgg == nil {
		t.Fatal("ByModel missing claude-sonnet-4-6")
	}
	if modelAgg.TotalCostUSD != 0.015 {
		t.Fatalf("ByModel cost = %v, want 0.015", modelAgg.TotalCostUSD)
	}
}

func TestCostObserverDoesNotInventCallForEmptyFallbackEvent(t *testing.T) {
	ledger, err := costledger.NewSQLiteLedger(t.TempDir() + "/costs.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	obs := newCostObserver(ledger, "empty-turn", "user-1", "chat")
	if err := obs.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now().UTC(),
		Data:      &unifiedevents.TokenUsageEvent{},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total.CallCount != 0 || summary.Total.AccountingEventCount != 1 {
		t.Fatalf("empty fallback totals = %#v", summary.Total)
	}
	if inferCostScope("chat", "") != "chat" {
		t.Fatal("plain chat was not attributed to chat scope")
	}
}

func TestCostObserverMultiTurnAccumulation(t *testing.T) {
	srv := costledger.NewTestServer(t)
	defer srv.Close()
	ledger := costledger.NewLedger(srv.URL)

	obs := newCostObserver(ledger, "sess-1", "user-1", "chat")

	for i := 0; i < 3; i++ {
		_ = obs.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
			Type:      unifiedevents.TokenUsage,
			Timestamp: time.Date(2026, 5, 18, 10, i, 0, 0, time.UTC),
			Data: &unifiedevents.TokenUsageEvent{
				Provider:         "openai",
				ModelID:          "gpt-5",
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalCost:        0.01,
			},
		})
	}

	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if summary.Total.CallCount != 3 {
		t.Fatalf("CallCount = %d, want 3", summary.Total.CallCount)
	}
	if summary.Total.PromptTokens != 300 {
		t.Fatalf("PromptTokens = %d, want 300", summary.Total.PromptTokens)
	}
	if summary.Total.TotalCostUSD != 0.03 {
		t.Fatalf("TotalCostUSD = %v, want 0.03", summary.Total.TotalCostUSD)
	}
}

func TestCostObserverRecordsPerCallAndIgnoresCumulativeFallback(t *testing.T) {
	ledger, err := costledger.NewSQLiteLedger(t.TempDir() + "/costs.sqlite")
	if err != nil {
		t.Fatalf("NewSQLiteLedger() error = %v", err)
	}
	defer ledger.Close()
	obs := newCostObserver(
		ledger,
		"sess-per-call",
		"user-1",
		"workflow_phase",
		withCostModel("codex-cli", "auto"),
		withCostAttribution("pulse", "Workflow/demo", "iteration-0/default", "exec-1"),
	)

	perCall := &unifiedevents.AgentEvent{
		Type:      unifiedevents.LLMGenerationEnd,
		Timestamp: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		SpanID:    "span-call-1",
		Component: "llm",
		Data: &unifiedevents.LLMGenerationEndEvent{
			UsageMetrics: unifiedevents.UsageMetrics{
				PromptTokens: 120, CompletionTokens: 30, CacheTokens: 40,
			},
			BaseEventData: unifiedevents.BaseEventData{Metadata: map[string]interface{}{
				"provider":                "codex-cli",
				"codex_effective_model":   "gpt-5.6-sol",
				"cost_usd_estimated":      0.12,
				"cache_read_input_tokens": 40,
			}},
		},
	}
	if err := obs.HandleEvent(context.Background(), perCall); err != nil {
		t.Fatalf("HandleEvent(per call) error = %v", err)
	}
	if err := obs.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: perCall.Timestamp.Add(time.Second),
		SpanID:    "span-cumulative",
		Data: &unifiedevents.TokenUsageEvent{
			Provider: "codex-cli", ModelID: "auto", PromptTokens: 120,
			CompletionTokens: 30, TotalCost: 0.12,
			GenerationInfo: map[string]interface{}{"llm_call_count": 1},
		},
	}); err != nil {
		t.Fatalf("HandleEvent(cumulative) error = %v", err)
	}
	// A duplicate observer delivery of the same event is also idempotent.
	if err := obs.HandleEvent(context.Background(), perCall); err != nil {
		t.Fatalf("HandleEvent(duplicate) error = %v", err)
	}

	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary.Total.CallCount != 1 || summary.Total.AccountingEventCount != 1 {
		t.Fatalf("summary total = %#v, want one immutable call", summary.Total)
	}
	if summary.Total.SubscriptionShadowUSD != 0.12 {
		t.Fatalf("SubscriptionShadowUSD = %v, want 0.12", summary.Total.SubscriptionShadowUSD)
	}
	if got := summary.ByModel["gpt-5.6-sol"]; got == nil || got.PromptTokens != 120 {
		t.Fatalf("effective model aggregate = %#v", got)
	}
}

func TestCostObserverNilSafety(t *testing.T) {
	obs := newCostObserver(nil, "", "", "")
	if err := obs.HandleEvent(context.Background(), nil); err != nil {
		t.Fatalf("nil event should not error: %v", err)
	}

	var nilObs *costObserver
	if err := nilObs.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type: unifiedevents.TokenUsage,
	}); err != nil {
		t.Fatalf("nil observer should not error: %v", err)
	}
}
