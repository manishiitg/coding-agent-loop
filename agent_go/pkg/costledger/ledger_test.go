package costledger

import (
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newLedgerTestServer(t *testing.T) *httptest.Server {
	return NewTestServer(t)
}

func TestSQLiteLedgerConcurrentAppendsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "costs.sqlite")
	first, err := NewSQLiteLedger(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteLedger(first) error = %v", err)
	}
	defer first.Close()
	second, err := NewSQLiteLedger(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteLedger(second) error = %v", err)
	}
	defer second.Close()

	const eventCount = 50
	var wg sync.WaitGroup
	errCh := make(chan error, eventCount*2)
	for i := 0; i < eventCount; i++ {
		entry := Entry{
			EventID:          fmt.Sprintf("event-%d", i),
			IdempotencyKey:   fmt.Sprintf("call-%d", i),
			Timestamp:        time.Date(2026, 7, 13, 10, 0, i, 0, time.UTC),
			Provider:         "codex-cli",
			ModelID:          "auto",
			EffectiveModelID: "gpt-5.6-sol",
			LLMCallCount:     1,
			PromptTokens:     100,
			TotalCostUSD:     0.01,
			BillingBasis:     "subscription_shadow",
		}
		for _, ledger := range []*Ledger{first, second} {
			wg.Add(1)
			go func(ledger *Ledger, entry Entry) {
				defer wg.Done()
				if err := ledger.Append(entry); err != nil {
					errCh <- err
				}
			}(ledger, entry)
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent Append error = %v", err)
	}

	summary, err := first.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary.Total.CallCount != eventCount {
		t.Fatalf("CallCount = %d, want %d", summary.Total.CallCount, eventCount)
	}
	if summary.Total.AccountingEventCount != eventCount {
		t.Fatalf("AccountingEventCount = %d, want %d", summary.Total.AccountingEventCount, eventCount)
	}
	if got := summary.ByModel["gpt-5.6-sol"]; got == nil || got.CallCount != eventCount {
		t.Fatalf("effective model aggregate = %#v, want %d calls", got, eventCount)
	}
	if math.Abs(summary.Total.SubscriptionShadowUSD-0.5) > 1e-9 {
		t.Fatalf("SubscriptionShadowUSD = %v, want 0.5", summary.Total.SubscriptionShadowUSD)
	}
}

func TestSQLiteLedgerMigrationQuarantinesMalformedRowsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "costs.jsonl")
	valid := `{"ts":"2026-07-13T23:59:59Z","provider":"anthropic","model_id":"claude-sonnet","prompt_tokens":10,"total_cost_usd":0.02,"cost_usd_source":"estimated"}`
	if err := os.WriteFile(legacyPath, []byte(valid+"\n{not-json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ledger, err := NewSQLiteLedger(filepath.Join(root, "costs.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteLedger() error = %v", err)
	}
	defer ledger.Close()

	report, err := ledger.MigrateLegacyJSONL(legacyPath)
	if err != nil {
		t.Fatalf("MigrateLegacyJSONL(first) error = %v", err)
	}
	if report.Imported != 1 || report.Quarantined != 1 {
		t.Fatalf("first report = %#v, want 1 imported and 1 quarantined", report)
	}
	report, err = ledger.MigrateLegacyJSONL(legacyPath)
	if err != nil {
		t.Fatalf("MigrateLegacyJSONL(second) error = %v", err)
	}
	if report.Duplicates != 1 || report.Imported != 0 || report.Quarantined != 0 {
		t.Fatalf("second report = %#v, want one duplicate only", report)
	}

	summary, err := ledger.Summarize("2026-07-13", "2026-07-13")
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary.Total.CallCount != 1 || summary.Coverage.QuarantinedEventCount != 1 {
		t.Fatalf("summary = %#v, want one call and one quarantined row", summary)
	}
	if summary.Total.TokenEstimateCostUSD != 0.02 {
		t.Fatalf("TokenEstimateCostUSD = %v, want 0.02", summary.Total.TokenEstimateCostUSD)
	}
}

func TestSQLiteLedgerSeparatesCallsFromToolEventsAndUTCDateBounds(t *testing.T) {
	ledger, err := NewSQLiteLedger(filepath.Join(t.TempDir(), "costs.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteLedger() error = %v", err)
	}
	defer ledger.Close()
	entries := []Entry{
		{
			EventID: "llm", IdempotencyKey: "llm", Timestamp: time.Date(2026, 7, 13, 23, 59, 59, 0, time.UTC),
			Provider: "openai", ModelID: "gpt-5.6-sol", LLMCallCount: 2, BillingBasis: "unpriced",
		},
		{
			EventID: "tool", IdempotencyKey: "tool", Timestamp: time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
			Component: "tool:image_gen", ToolName: "image_gen", TotalCostUSD: 0.04, BillingBasis: "provider_actual",
		},
	}
	for _, entry := range entries {
		if err := ledger.Append(entry); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	dayOne, err := ledger.Summarize("2026-07-13", "2026-07-13")
	if err != nil {
		t.Fatalf("Summarize(day one) error = %v", err)
	}
	if dayOne.Total.CallCount != 2 || dayOne.Total.AccountingEventCount != 1 || dayOne.Total.UnpricedCallCount != 2 {
		t.Fatalf("day one total = %#v", dayOne.Total)
	}
	dayTwo, err := ledger.Summarize("2026-07-14", "2026-07-14")
	if err != nil {
		t.Fatalf("Summarize(day two) error = %v", err)
	}
	if dayTwo.Total.CallCount != 0 || dayTwo.Total.AccountingEventCount != 1 || dayTwo.Total.ProviderActualCostUSD != 0.04 {
		t.Fatalf("day two total = %#v", dayTwo.Total)
	}
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
