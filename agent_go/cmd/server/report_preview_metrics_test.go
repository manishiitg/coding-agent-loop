package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
)

func TestReportPreviewCostsScopesLedgerAndForcesSummary(t *testing.T) {
	ledger := installWorkflowCostTestLedger(t)
	for _, entry := range []costledger.Entry{
		{EventID: "own", IdempotencyKey: "own", WorkflowID: "Workflow/demo", Scope: "pulse", Timestamp: time.Now().UTC(), TotalCostUSD: 3, LLMCallCount: 1},
		{EventID: "other", IdempotencyKey: "other", WorkflowID: "Workflow/other", Scope: "pulse", Timestamp: time.Now().UTC(), TotalCostUSD: 900, LLMCallCount: 1},
	} {
		if err := ledger.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, reportPreviewAPIPrefix+"costs?workspace=Workflow/demo&view=full&workspace_path=Workflow/other&days=7", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{Scope: reportPreviewScope, ScopeWorkspace: "Workflow/demo"}))
	rec := httptest.NewRecorder()
	(&StreamingAPI{}).handleReportPreviewMetrics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var data workflowCostsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.ScopedCosts == nil || data.ScopedCosts.Total.TotalCostUSD != 3 || data.History == nil || data.History.Days != 7 || len(data.Runs) != 0 {
		t.Fatalf("unexpected cost data: %#v", data)
	}
	if req.URL.Query().Get("workspace_path") != "Workflow/other" {
		t.Fatal("handler mutated original request")
	}
}

func TestReportPreviewMetricsRejectsOtherWorkspacesAndTraversal(t *testing.T) {
	for _, endpoint := range []string{"costs"} {
		for _, requested := range []string{"Workflow/other", "Workflow/demo/../other", "../outside", ""} {
			req := httptest.NewRequest(http.MethodGet, reportPreviewAPIPrefix+endpoint+"?workspace="+requested, nil)
			req = req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{Scope: reportPreviewScope, ScopeWorkspace: "Workflow/demo"}))
			rec := httptest.NewRecorder()
			(&StreamingAPI{}).handleReportPreviewMetrics(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %q got %d", endpoint, requested, rec.Code)
			}
		}
	}
	req := httptest.NewRequest(http.MethodGet, reportPreviewAPIPrefix+"costs?workspace=Workflow/demo/../../outside", nil)
	rec := httptest.NewRecorder()
	(&StreamingAPI{}).handleReportPreviewMetrics(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unscoped traversal got %d", rec.Code)
	}
}
