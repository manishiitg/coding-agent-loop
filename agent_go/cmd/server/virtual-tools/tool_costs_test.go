package virtualtools

import (
	"context"
	"path/filepath"
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

func TestRecordPricedToolCostPersistsWithoutOutputPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	recordPricedToolCost(context.Background(), "", "user-1", pricedToolCost{
		ToolName:   "image_gen",
		Capability: "image",
		Provider:   "openai",
		ModelID:    "gpt-image",
		TotalCost:  0.08,
		Estimated:  false,
	})

	ledger, err := costledger.NewSQLiteLedger(filepath.Join(root, "_system", "costs.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteLedger() error = %v", err)
	}
	defer ledger.Close()
	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary.Total.AccountingEventCount != 1 {
		t.Fatalf("AccountingEventCount = %d, want 1", summary.Total.AccountingEventCount)
	}
	if summary.Total.CallCount != 0 {
		t.Fatalf("CallCount = %d, want 0 for paid tool event", summary.Total.CallCount)
	}
	if summary.Total.ProviderActualCostUSD != 0.08 {
		t.Fatalf("ProviderActualCostUSD = %v, want 0.08", summary.Total.ProviderActualCostUSD)
	}
}
