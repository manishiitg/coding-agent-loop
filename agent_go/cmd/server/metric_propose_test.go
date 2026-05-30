package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProposeMetricAmendsExistingMetricAndArchivesPriorDefinition(t *testing.T) {
	const workspacePath = "Workflow/social-media"
	oldTarget := 50.0
	newTarget := 35.0
	api := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/soul/soul.md": `## Objective
Grow useful social-media reach.

## Success Criteria
- Follow enough relevant builders every day.
`,
		workspacePath + "/planning/metrics.json": `{
  "active_mode": "explore",
  "metrics": [{
    "id": "builder.follow_count",
    "label": "Builder follows",
    "unit": "count",
    "direction": "higher_better",
    "mode": "target",
    "target": 50,
    "source": {"type": "telemetry", "field": "run.duration_seconds"},
    "success_criteria": "Follow enough relevant builders every day.",
    "version": 1
  }]
}`,
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	out, err := ProposeMetric(context.Background(), workspacePath, "test", ProposeMetricInput{
		ID:              "builder.follow_count",
		Label:           "Builder follows",
		Unit:            "count",
		Direction:       HigherBetter,
		Mode:            MetricModeTarget,
		Target:          &newTarget,
		Source:          MetricSource{Type: MetricSourceTelemetry, Field: "run.duration_seconds"},
		SuccessCriteria: "Follow enough relevant builders every day.",
		Amend:           &AmendMetricRequest{ID: "builder.follow_count", Reason: "correct target from weekly goal to daily goal"},
	})
	if err != nil {
		t.Fatalf("ProposeMetric amend returned error: %v", err)
	}
	if out.Status != "amended" || out.Version != 2 || out.ArchivedVersion != 1 {
		t.Fatalf("unexpected output: %+v", out)
	}

	api.mu.Lock()
	metricsJSON := api.files[workspacePath+"/planning/metrics.json"]
	improveMD := api.files[workspacePath+"/builder/improve.md"]
	improveHTML := api.files[workspacePath+"/builder/improve.html"]
	api.mu.Unlock()

	var file MetricsFile
	if err := json.Unmarshal([]byte(metricsJSON), &file); err != nil {
		t.Fatalf("metrics.json did not unmarshal: %v\n%s", err, metricsJSON)
	}
	if file.ActiveMode != "explore" {
		t.Fatalf("active_mode not preserved: %q", file.ActiveMode)
	}
	if len(file.Metrics) != 1 {
		t.Fatalf("active metric count = %d", len(file.Metrics))
	}
	if got := file.Metrics[0].Version; got != 2 {
		t.Fatalf("active metric version = %d", got)
	}
	if got := *file.Metrics[0].Target; got != newTarget {
		t.Fatalf("active target = %v", got)
	}
	if len(file.Archive) != 1 {
		t.Fatalf("archive count = %d; json=%s", len(file.Archive), metricsJSON)
	}
	archived := file.Archive[0]
	if archived.ID != "builder.follow_count" || archived.Version != 1 {
		t.Fatalf("bad archive metadata: %+v", archived)
	}
	if archived.ArchivedReason != "correct target from weekly goal to daily goal" {
		t.Fatalf("bad archive reason: %q", archived.ArchivedReason)
	}
	if archived.Definition.Version != 1 || archived.Definition.Target == nil || *archived.Definition.Target != oldTarget {
		t.Fatalf("prior definition not preserved: %+v", archived.Definition)
	}
	// propose_metric must NOT touch the improvement ledger; narrating the
	// change into builder/improve.html is the agent's job.
	if improveMD != "" || improveHTML != "" {
		t.Fatalf("propose_metric should not write the improve ledger; md=%q html=%q", improveMD, improveHTML)
	}
}

func TestProposeMetricExistingIDWithoutAmendReturnsActionableError(t *testing.T) {
	const workspacePath = "Workflow/social-media"
	target := 50.0
	api := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/soul/soul.md": `## Objective
Grow useful social-media reach.

## Success Criteria
- Follow enough relevant builders every day.
`,
		workspacePath + "/planning/metrics.json": `{
  "metrics": [{
    "id": "builder.follow_count",
    "unit": "count",
    "direction": "higher_better",
    "mode": "target",
    "target": 50,
    "source": {"type": "telemetry", "field": "run.duration_seconds"}
  }]
}`,
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	_, err := ProposeMetric(context.Background(), workspacePath, "test", ProposeMetricInput{
		ID:              "builder.follow_count",
		Unit:            "count",
		Direction:       HigherBetter,
		Mode:            MetricModeTarget,
		Target:          &target,
		Source:          MetricSource{Type: MetricSourceTelemetry, Field: "run.duration_seconds"},
		SuccessCriteria: "Follow enough relevant builders every day.",
	})
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
	if !strings.Contains(err.Error(), "amend_existing") {
		t.Fatalf("duplicate error should point to amend_existing, got: %v", err)
	}
}

func TestRetireMetricArchivesDefinition(t *testing.T) {
	const workspacePath = "Workflow/social-media"
	api := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/planning/metrics.json": `{
  "active_mode": "exploit",
  "metrics": [{
    "id": "builder.follow_count",
    "unit": "count",
    "direction": "higher_better",
    "mode": "target",
    "target": 50,
    "source": {"type": "telemetry", "field": "run.duration_seconds"},
    "version": 3
  }]
}`,
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	out, err := RetireMetric(context.Background(), workspacePath, "test", RetireMetricInput{
		ID:     "builder.follow_count",
		Reason: "superseded by a quality-weighted follow metric",
	})
	if err != nil {
		t.Fatalf("RetireMetric returned error: %v", err)
	}
	if out.Status != "retired" || out.ArchivedVersion != 3 {
		t.Fatalf("unexpected output: %+v", out)
	}

	api.mu.Lock()
	metricsJSON := api.files[workspacePath+"/planning/metrics.json"]
	api.mu.Unlock()

	var file MetricsFile
	if err := json.Unmarshal([]byte(metricsJSON), &file); err != nil {
		t.Fatalf("metrics.json did not unmarshal: %v\n%s", err, metricsJSON)
	}
	if file.ActiveMode != "exploit" {
		t.Fatalf("active_mode not preserved: %q", file.ActiveMode)
	}
	if len(file.Metrics) != 0 {
		t.Fatalf("metric should be removed from active list: %+v", file.Metrics)
	}
	if len(file.Archive) != 1 || file.Archive[0].Version != 3 {
		t.Fatalf("archive not written correctly: %+v", file.Archive)
	}
}
