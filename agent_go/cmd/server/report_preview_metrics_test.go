package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	for _, endpoint := range []string{"costs", "evaluations"} {
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

func TestReportPreviewEvaluationsReadsSavedScoresAndPlan(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	dbDir := filepath.Join(root, "Workflow", "demo", "db")
	evalDir := filepath.Join(root, "Workflow", "demo", "evaluation")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evalDir, "evaluation_plan.json"), []byte(`{"steps":[{"id":"quality","title":"Output quality"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE eval_results(run_folder TEXT,group_name TEXT,step_id TEXT,score REAL,max_score REAL,score_captured INTEGER,reasoning TEXT,evidence TEXT,skipped INTEGER,generated_at TEXT,updated_at TEXT,PRIMARY KEY(run_folder,step_id));
 INSERT INTO eval_results VALUES('run-1','','quality',0,10,1,'Measured zero','db/proof.json',0,'2026-09-11T10:00:00Z','2026-09-11T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, reportPreviewAPIPrefix+"evaluations?workspace=Workflow/demo", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{Scope: reportPreviewScope, ScopeWorkspace: "Workflow/demo"}))
	rec := httptest.NewRecorder()
	(&StreamingAPI{}).handleReportPreviewMetrics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Results []struct {
			Title    string  `json:"title"`
			Score    float64 `json:"score"`
			Captured bool    `json:"score_captured"`
			Evidence string  `json:"evidence"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || len(response.Results) != 1 || response.Results[0].Title != "Output quality" || response.Results[0].Score != 0 || !response.Results[0].Captured || response.Results[0].Evidence != "db/proof.json" {
		t.Fatalf("unexpected results: %#v", response)
	}
}
