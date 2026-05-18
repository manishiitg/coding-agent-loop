package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	unifiedevents "github.com/manishiitg/mcpagent/events"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
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
