package server

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPulseGoalWorkLifecycle(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ctx := context.Background()
	ws := "Workflow/example"

	idea, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{Title: "Nobody follows up with post commenters", Status: "idea", Metric: "followers"}, "pulse-1")
	if err != nil {
		t.Fatalf("create idea: %v", err)
	}
	if !strings.HasPrefix(idea.ID, "GW-") || idea.Kind != "goal_work" {
		t.Fatalf("idea = %+v", idea)
	}
	if _, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{ID: idea.ID, Status: "done"}, "pulse-2"); err == nil {
		t.Fatal("done without action_taken must be rejected")
	}
	done, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{ID: idea.ID, Status: "done", ActionTaken: "Drafted replies for 12 commenters", Links: []string{"pulse/work/2026-09-23/replies.md"}}, "pulse-2")
	if err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if done.Title != idea.Title || done.Metric != "followers" || done.CreatedByPulseRun != "pulse-1" || done.UpdatedByPulseRun != "pulse-2" {
		t.Fatalf("update must keep unspecified fields: %+v", done)
	}
	if _, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{ID: idea.ID, Effect: "worked", EffectNote: "+18 followers week over week"}, "pulse-3"); err != nil {
		t.Fatalf("record effect: %v", err)
	}
	if _, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{Title: "Post the drafts", Status: "needs_user"}, "pulse-3"); err == nil {
		t.Fatal("needs_user without decision_id must be rejected")
	}
	if _, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{Kind: "constraint_challenge", Title: "Posts limited to 350 words", Status: "needs_user", DecisionID: "goal-work-length"}, "pulse-3"); err == nil {
		t.Fatal("constraint_challenge without constraint text and class must be rejected")
	}
	challenge, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{Kind: "constraint_challenge", Title: "Posts limited to 350 words", Status: "needs_user", DecisionID: "goal-work-length", ConstraintText: "Posts remain 150-350 words", ConstraintClass: "choice"}, "pulse-3")
	if err != nil {
		t.Fatalf("constraint challenge: %v", err)
	}
	if _, err := recordPulseGoalWork(ctx, ws, PulseGoalWorkItem{ID: challenge.ID, ConstraintClass: "negotiable"}, "pulse-3"); err == nil {
		t.Fatal("unknown constraint class must be rejected")
	}

	items, err := listPulseGoalWork(ctx, ws, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	byID := map[string]PulseGoalWorkItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if got := byID[idea.ID]; got.Effect != "worked" || got.Status != "done" || len(got.Links) != 1 {
		t.Fatalf("stored item = %+v", got)
	}
	if got := byID[challenge.ID]; got.ConstraintClass != "choice" {
		t.Fatalf("challenge class changed by a rejected update: %+v", got)
	}
	if _, err := time.Parse(time.RFC3339, byID[idea.ID].UpdatedAt); err != nil {
		t.Fatalf("updated_at: %v", err)
	}
}

func TestPulseGoalWorkViewEmptyWorkflow(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	view, err := readPulseGoalWorkView(context.Background(), "Workflow/empty", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, `"goal_work": []`) {
		t.Fatalf("empty view = %s", view)
	}
}
