package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
)

func TestBuildCostOverviewFoldsAndFiltersByAccess(t *testing.T) {
	ledger, err := costledger.NewSQLiteLedger(filepath.Join(t.TempDir(), "costs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })

	ts := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	add := func(id, workflowID, scope, provider, model string, usd float64) {
		t.Helper()
		if err := ledger.Append(costledger.Entry{
			EventID: id, Timestamp: ts, WorkflowID: workflowID, Scope: scope,
			Provider: provider, ModelID: model, LLMCallCount: 1,
			PromptTokens: 100, TotalCostUSD: usd, BillingBasis: "provider_actual",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("e1", "Workflow/sales", "workflow_execution", "claude-code", "opus", 2)
	add("e2", "Workflow/sales/runs/iteration-3", "pulse", "codex-cli", "gpt", 1)
	add("e3", "Workflow/secret", "workflow_execution", "claude-code", "opus", 5)
	add("e4", "_users/alice/Chats/Work/projects/writer-ab12/code", "chat", "claude-code", "sonnet", 0.5)
	add("e5", "_users/alice/Chats", "chat", "claude-code", "sonnet", 3)
	add("e6", "", "unknown", "claude-code", "sonnet", 4)

	summary, err := ledger.Summarize("2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	visible := func(id, kind string) bool { return id != "Workflow/secret" }

	member := buildCostOverview(summary, visible, false)
	if got, want := len(member.Items), 2; got != want {
		t.Fatalf("member items = %d, want %d: %+v", got, want, member.Items)
	}
	sales := member.Items[0]
	if sales.ID != "Workflow/sales" || sales.Kind != costOverviewKindWorkflow || sales.TotalCostUSD != 3 {
		t.Fatalf("sales row = %+v, want Workflow/sales folded to $3", sales)
	}
	if sales.ByScope["pulse"] == nil || sales.ByScope["pulse"].TotalCostUSD != 1 {
		t.Fatalf("sales pulse scope = %+v", sales.ByScope)
	}
	crew := member.Items[1]
	if crew.ID != "_users/alice/Chats/Work/projects/writer-ab12" || crew.Kind != costOverviewKindCrew || crew.OwnerID != "alice" || crew.Name != "writer-ab12" {
		t.Fatalf("crew row = %+v", crew)
	}
	if member.Total.TotalCostUSD != 3.5 {
		t.Fatalf("member total = %v, want only visible spend 3.5", member.Total.TotalCostUSD)
	}
	if member.ByProvider["codex-cli"] == nil || member.ByProvider["codex-cli"].TotalCostUSD != 1 {
		t.Fatalf("member by_provider = %+v", member.ByProvider)
	}

	admin := buildCostOverview(summary, func(string, string) bool { return true }, true)
	if admin.Total.TotalCostUSD != 15.5 {
		t.Fatalf("admin total = %v, want 15.5", admin.Total.TotalCostUSD)
	}
	var other *costOverviewItem
	for _, item := range admin.Items {
		if item.Kind == costOverviewKindOther {
			other = item
		}
	}
	if other == nil || other.TotalCostUSD != 7 {
		t.Fatalf("admin other row = %+v, want chat+unattributed folded to $7", other)
	}
}
