package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPulseReviewNotesWorkingThenCompletion(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ws, run, module := "Workflow/notes", "pulse-notes", pulseModuleStrategicReview
	decisions := completePulseWorklistDecisions(nil)
	for i := range decisions {
		decisions[i].Due = true
	}
	if _, err := recordPulseWorklist(ctx, ws, run, decisions); err != nil {
		t.Fatal(err)
	}
	if err := beginDuePulseReviewRecoveries(ctx, ws, run, module); err != nil {
		t.Fatal(err)
	}
	if err := markPulseReviewRecovery(ctx, ws, module, run, "", "Interrupted earlier"); err != nil {
		t.Fatal(err)
	}
	call := map[string]interface{}{"workspace_path": ws, "pulse_run_id": run, "module": module, "result": "running", "reason": "Investigating", "review_note": "Source A contradicts the current hypothesis.", "note_only": true}
	if _, err := recordPulseResultFromToolArgs(ctx, call); err != nil {
		t.Fatal(err)
	}
	notes, err := loadPulseReviewNotes(ctx, ws, module, run, 3)
	if err != nil || len(notes) != 1 || notes[0].Result != "incomplete" {
		t.Fatalf("working note: %+v %v", notes, err)
	}
	pending, err := pendingPulseReviewRecoveries(ctx, ws)
	if err != nil || len(pending) != 1 {
		t.Fatalf("working note cleared recovery: %+v %v", pending, err)
	}
	call["result"] = "done"
	if _, err := recordPulseResultFromToolArgs(ctx, call); err == nil {
		t.Fatal("note-only falsely completed")
	}
	call["note_only"] = false
	call["reason"] = "No worthwhile change found"
	call["review_note"] = "Alternative rejected after comparing sources. Revisit when the sample matures."
	call["evidence"] = []interface{}{"sources/a"}
	if _, err := recordPulseResultFromToolArgs(ctx, call); err != nil {
		t.Fatal(err)
	}
	notes, err = loadPulseReviewNotes(ctx, ws, module, run, 3)
	if err != nil || len(notes) != 1 || notes[0].Result != "done" || notes[0].Conclusion != call["reason"] || len(notes[0].Evidence) != 1 {
		t.Fatalf("completion: %+v %v", notes, err)
	}
	pending, err = pendingPulseReviewRecoveries(ctx, ws)
	if err != nil || len(pending) != 0 {
		t.Fatalf("completion left recovery: %+v %v", pending, err)
	}
	// A later repair/result without a note must not erase the existing reasoning.
	delete(call, "review_note")
	if _, err := recordPulseResultFromToolArgs(ctx, call); err != nil {
		t.Fatal(err)
	}
	notes, _ = loadPulseReviewNotes(ctx, ws, module, run, 3)
	if !strings.Contains(notes[0].Content, "Alternative rejected") {
		t.Fatal("note erased")
	}
	if err := savePulseWorkingNote(ctx, ws, module, run, "overwrite terminal"); err == nil {
		t.Fatal("late working note accepted")
	}
	reports := pulseReportsWithNotes(nil, notes)
	if len(reports) != 1 || reports[0].Path != "" || reports[0].Source != "review_note" || !strings.Contains(reports[0].Content, "sources/a") {
		t.Fatalf("report: %+v", reports)
	}
	text, err := readPulseReviewNotesView(ctx, ws, module, run, 3)
	var payload map[string]interface{}
	if err != nil || json.Unmarshal([]byte(text), &payload) != nil || len(payload["notes"].([]interface{})) != 1 {
		t.Fatalf("agent read: %s %v", text, err)
	}
	other, _ := loadPulseReviewNotes(ctx, ws, pulseModuleArchitectureReview, "", 3)
	if len(other) != 0 {
		t.Fatal("module isolation failed")
	}
	other, _ = loadPulseReviewNotes(ctx, "Workflow/other", module, "", 3)
	if len(other) != 0 {
		t.Fatal("workspace isolation failed")
	}
}

func TestPulseReviewNoteAndResultRollbackTogether(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ws, run, module := "Workflow/rollback", "pulse-note-rollback", pulseModuleTechnicalReview
	decisions := completePulseWorklistDecisions(nil)
	for i := range decisions {
		decisions[i].Due = true
	}
	if _, err := recordPulseWorklist(ctx, ws, run, decisions); err != nil {
		t.Fatal(err)
	}
	_, db, err := openPulseModuleStateDB(ctx, ws, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_note BEFORE INSERT ON pulse_review_notes BEGIN SELECT RAISE(ABORT,'note storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := markPulseModuleResultFromAgentWithAudit(ctx, ws, module, run, "done", "Checked", nil, PulseModuleAuditInput{ReviewNote: "New reasoning"}); err == nil {
		t.Fatal("storage failure ignored")
	}
	state, err := getPulseModuleStateByModule(ctx, db, ws, module)
	if err != nil || state.LastResult != "" {
		t.Fatalf("false completion: %+v %v", state, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pulse_module_audit`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial audit: %d %v", count, err)
	}
	if err := savePulseWorkingNote(ctx, ws, module, "wrong-run", "stale"); err == nil {
		t.Fatal("stale run accepted")
	}
}

func TestPulseReviewNotesLegacySummaryAndNoFileRequired(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ws := "Workflow/legacy-notes"
	if _, err := markPulseModuleResult(ctx, ws, pulseModuleTechnicalReview, "old-run", "done", "Legacy conclusion", []string{"proof"}); err != nil {
		t.Fatal(err)
	}
	notes, err := loadPulseReviewNotes(ctx, ws, "", "", 3)
	if err != nil || len(notes) != 1 || notes[0].Conclusion != "Legacy conclusion" || notes[0].Content != "" {
		t.Fatalf("legacy: %+v %v", notes, err)
	}
	files, err := listPulseReviewReports(ws, "")
	if err != nil || len(files) != 0 {
		t.Fatalf("unexpected file requirement: %+v %v", files, err)
	}
}

func TestPulseReviewNotesAPIRendersSQLiteWithoutFiles(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ws := "Workflow/notes-api"
	if _, err := markPulseModuleResult(ctx, ws, pulseModuleArchitectureReview, "old-run", "done", "Architecture checked", nil); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual UI response, including the no-Markdown-file path.
	api := &StreamingAPI{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/pulse/reviews?workspace_path="+ws, nil)
	api.handleGetPulseReviews(response, request)
	if response.Code != 200 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	var data struct {
		Reports []PulseReviewReport `json:"reports"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Reports) != 1 || data.Reports[0].Source != "review_note" || data.Reports[0].Content != "Architecture checked" {
		t.Fatalf("response: %s", response.Body.String())
	}
}
