package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseStepConcernLines(t *testing.T) {
	text := "Collected 12 rows.\n`CONCERNS: source returned 0 rows for group b`\n- CONCERNS: stale cache from 2026-09-01\nCONCERNS: <what happened, the exact affected artifact or operation, and the evidence>\nconcerns: none\nCONCERNS:\nSTATUS: COMPLETED"
	got := parseStepConcernLines(text)
	want := []string{"source returned 0 rows for group b", "stale cache from 2026-09-01"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("parseStepConcernLines = %q, want %q", got, want)
	}
}

func writeStepSummary(t *testing.T, path string, value interface{}, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestCollectStepConcernsReadsBothSummaryKindsSinceTheWindow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	ws := "Workflow/example"
	runs := filepath.Join(root, "Workflow", "example", "runs")
	since := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	fresh := since.Add(2 * time.Hour)
	later := since.Add(26 * time.Hour)

	// Scripted/regular step: script stdout is the execution_result.
	writeStepSummary(t, filepath.Join(runs, "iteration-5", "default", "logs", "collect", "execution", "execution-final-summary.json"),
		map[string]string{"step_id": "collect", "run_folder": "iteration-5", "execution_result": "done\nCONCERNS: prospect source returned 0 rows"}, fresh)
	// Same concern again in a later run: grouped, counted twice.
	writeStepSummary(t, filepath.Join(runs, "iteration-6", "default", "logs", "collect", "execution", "execution-final-summary.json"),
		map[string]string{"step_id": "collect", "run_folder": "iteration-6", "execution_result": "CONCERNS: prospect source returned 0 rows"}, later)
	// Message-sequence turn summaries.
	writeStepSummary(t, filepath.Join(runs, "iteration-6", "engage", "execution", "engage-comment", "session.json"),
		map[string]interface{}{"step_id": "engage-comment", "run_folder": "iteration-6", "entries": []map[string]string{
			{"summary": "Commented on 4 posts.\nCONCERNS: reply rate fell to 1% from 6% last week\nSTATUS: COMPLETED"},
			{"summary": "No concern here."},
		}}, later)
	// Before the window: ignored.
	writeStepSummary(t, filepath.Join(runs, "iteration-4", "default", "logs", "collect", "execution", "execution-final-summary.json"),
		map[string]string{"step_id": "collect", "run_folder": "iteration-4", "execution_result": "CONCERNS: old news"}, since.Add(-time.Hour))
	// Pulse's own run folder: not step evidence.
	writeStepSummary(t, filepath.Join(runs, "pulse", "p1", "logs", "x", "execution", "execution-final-summary.json"),
		map[string]string{"step_id": "x", "execution_result": "CONCERNS: reviewer text"}, later)

	view := collectStepConcerns(ws, since)
	if view.Total != 2 {
		t.Fatalf("total = %d, want 2: %+v", view.Total, view.Concerns)
	}
	byStep := map[string]StepConcern{}
	for _, c := range view.Concerns {
		byStep[c.StepID] = c
	}
	collect := byStep["collect"]
	if collect.Text != "prospect source returned 0 rows" || collect.SeenCount != 2 || len(collect.RunFolders) != 2 {
		t.Fatalf("collect concern = %+v", collect)
	}
	if engage := byStep["engage-comment"]; engage.Text != "reply rate fell to 1% from 6% last week" || engage.SeenCount != 1 {
		t.Fatalf("engage concern = %+v", engage)
	}
	for _, c := range view.Concerns {
		if c.Text == "old news" || c.Text == "reviewer text" {
			t.Fatalf("collected out-of-scope concern: %+v", c)
		}
	}
}

func TestStepConcernWindowStartsAtThePreviousPulse(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ctx := context.Background()
	ws := "Workflow/example"
	before := time.Now().UTC()
	if got := stepConcernWindowStart(ctx, ws); before.Sub(got) < 6*24*time.Hour {
		t.Fatalf("without a previous Pulse the window should be about a week, got %v", got)
	}
	first := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	second := time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)
	if err := markPulseStarted(ctx, ws, "p1", first); err != nil {
		t.Fatal(err)
	}
	if err := markPulseStarted(ctx, ws, "p2", second); err != nil {
		t.Fatal(err)
	}
	// While p2 runs, its evidence window starts where p1 started.
	if got := stepConcernWindowStart(ctx, ws); !got.Equal(first) {
		t.Fatalf("window start = %v, want the previous Pulse start %v", got, first)
	}
}
