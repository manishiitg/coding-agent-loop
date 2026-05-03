package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type countingWorkspaceAPI struct {
	base *mockWorkspaceAPI

	mu     sync.Mutex
	counts map[string]int
}

func (m *countingWorkspaceAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + decodeWorkspacePath(strings.TrimPrefix(r.URL.Path, "/api/documents/"))
	if r.URL.Path == "/api/documents" {
		key = r.Method + " " + r.URL.Path + "?folder=" + r.URL.Query().Get("folder")
	}

	m.mu.Lock()
	m.counts[key]++
	m.mu.Unlock()

	m.base.ServeHTTP(w, r)
}

func (m *countingWorkspaceAPI) count(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := 0
	for key, count := range m.counts {
		if strings.HasPrefix(key, prefix) {
			total += count
		}
	}
	return total
}

func TestEvaluationScoreMigrationDoesNotRecordMeasurement(t *testing.T) {
	resetEvaluationScoreMigrationStateForTest()

	const workspacePath = "Workflow/social-media"
	report := testEvaluationReport("iteration-1/default", "2026-05-01T10:00:00Z")
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	api := &countingWorkspaceAPI{
		base: &mockWorkspaceAPI{files: map[string]string{
			workspacePath + "/evaluation/runs/iteration-1/default/evaluation_report.json": string(reportJSON),
			workspacePath + "/experiments/active.json": `{
				"experiments": [{
					"id": "exp-1",
					"status": "measuring",
					"target_metrics": ["run_score"],
					"expected_direction": "increase",
					"expected_magnitude": 1,
					"baseline": {"mean": {"run_score": 1}, "values": {"run_score": [1]}},
					"measurement": {"target_runs": 5, "completed_runs": 0, "values": {}}
				}]
			}`,
			workspacePath + "/planning/metrics.json": `{
				"metrics": [{
					"id": "run_score",
					"unit": "points",
					"direction": "higher_better",
					"mode": "target",
					"target": 10,
					"source": {"type": "eval_step", "id": "eval-workflow-success", "field": "score"}
				}]
			}`,
		}},
		counts: map[string]int{},
	}

	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	if _, err := readAllEvaluationReportsFromScores(context.Background(), workspacePath); err != nil {
		t.Fatalf("read reports: %v", err)
	}

	// Give any accidental async hook a chance to run. Migration writes must not
	// count as new run completions or mutate active experiment state.
	time.Sleep(50 * time.Millisecond)

	if got := api.count("PUT " + workspacePath + "/experiments/active.json"); got != 0 {
		t.Fatalf("migration unexpectedly wrote active experiment state %d time(s)", got)
	}
	if got := api.count("PUT " + workspacePath + "/scores/evaluation/"); got != 1 {
		t.Fatalf("migration should write one score file, wrote %d", got)
	}
}

func TestEvaluationScoreMigrationIsSingleFlightPerWorkspace(t *testing.T) {
	resetEvaluationScoreMigrationStateForTest()

	const workspacePath = "Workflow/social-media"
	files := map[string]string{}
	for _, run := range []string{"iteration-1/default", "iteration-2/default"} {
		report := testEvaluationReport(run, "2026-05-01T10:00:00Z")
		reportJSON, err := json.Marshal(report)
		if err != nil {
			t.Fatalf("marshal report: %v", err)
		}
		files[workspacePath+"/evaluation/runs/"+run+"/evaluation_report.json"] = string(reportJSON)
	}

	api := &countingWorkspaceAPI{
		base:   &mockWorkspaceAPI{files: files},
		counts: map[string]int{},
	}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := readAllEvaluationReportsFromScores(context.Background(), workspacePath)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("read reports: %v", err)
		}
	}

	if got := api.count("PUT " + workspacePath + "/scores/evaluation/"); got != 2 {
		t.Fatalf("concurrent reads should migrate each legacy report once, wrote %d score files", got)
	}
}

func resetEvaluationScoreMigrationStateForTest() {
	migratedEvaluationScoreWorkspaces = sync.Map{}
}

func testEvaluationReport(runFolder, generatedAt string) EvaluationReport {
	return EvaluationReport{
		TargetRunFolder:  runFolder,
		GeneratedAt:      generatedAt,
		TotalScore:       8,
		MaxPossibleScore: 10,
		ScorePercentage:  80,
		StepScores: []EvaluationStepScore{{
			StepID:   "eval-workflow-success",
			Score:    8,
			MaxScore: 10,
		}},
	}
}
