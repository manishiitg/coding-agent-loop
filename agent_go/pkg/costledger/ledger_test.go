package costledger

import (
	"net/http/httptest"
	"testing"
	"time"
)

func newLedgerTestServer(t *testing.T) *httptest.Server {
	return NewTestServer(t)
}

func TestLedgerAppendAndSummarizeViaWorkspaceAPI(t *testing.T) {
	server := newLedgerTestServer(t)
	defer server.Close()

	ledger := NewLedger(server.URL)
	if err := ledger.Append(Entry{
		Timestamp:        time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC),
		ModelID:          "gpt-5.2",
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalCostUSD:     0.12,
	}); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := ledger.Append(Entry{
		Timestamp:        time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC),
		ModelID:          "gpt-5.2",
		PromptTokens:     4,
		CompletionTokens: 6,
		TotalCostUSD:     0.08,
	}); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}

	summary, err := ledger.Summarize("2026-04-13", "2026-04-14")
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary.Total.CallCount != 2 {
		t.Fatalf("summary.Total.CallCount = %d, want 2", summary.Total.CallCount)
	}
	if got := summary.ByModel["gpt-5.2"].TotalCostUSD; got != 0.20 {
		t.Fatalf("summary.ByModel total cost = %v, want 0.20", got)
	}
	if len(summary.SortedDates()) != 2 {
		t.Fatalf("SortedDates() len = %d, want 2", len(summary.SortedDates()))
	}
}
