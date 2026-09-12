package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebhookEnvelopeOptions(t *testing.T) {
	s := WorkflowSchedule{GroupNames: []string{"dev"}, Webhook: &WorkflowWebhookConfig{InputMode: "envelope", AllowedVariables: []string{"base_url"}}}
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"group":"dev","variables":{"base_url":"https://example.test"},"payload":{"pr":12}}`, true},
		{`{"group":"prod"}`, false}, {`{"variables":{"TOKEN":"bad"}}`, false}, {`{"variables":{"base_url":false}}`, false},
	} {
		d := &WorkflowWebhookDelivery{Payload: json.RawMessage(tc.body)}
		err := resolveWebhookDeliveryOptions(s, d)
		if (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if tc.ok && (d.Group != "dev" || d.Variables["base_url"] != "https://example.test" || string(d.Payload) != `{"pr":12}`) {
			t.Fatalf("bad envelope: %+v", d)
		}
	}
	s.Webhook.InputMode = "raw"
	d := &WorkflowWebhookDelivery{Payload: json.RawMessage(`{"group":"prod","variables":{"TOKEN":"data"}}`)}
	if err := resolveWebhookDeliveryOptions(s, d); err != nil || d.Group != "" || d.Variables != nil {
		t.Fatal("raw event interpreted as config")
	}
}
func TestWebhookLeasesIndependentFromSchedules(t *testing.T) {
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &SchedulerService{stateStore: store}
	ctx := context.Background()
	normal := &ScheduleContext{WorkspacePath: "Workflow/test", Schedule: WorkflowSchedule{ID: "cron", ScheduleType: "cron"}}
	hook := &ScheduleContext{WorkspacePath: normal.WorkspacePath, Schedule: WorkflowSchedule{ID: "hook", ScheduleType: "webhook"}, WebhookInput: &WorkflowWebhookDelivery{}, TriggerSource: "webhook"}
	for _, c := range []struct {
		s  *ScheduleContext
		id string
	}{{normal, "normal-run"}, {hook, "hook-run"}} {
		if err := svc.claimScheduleRun(ctx, c.s, c.id, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.claimScheduleRun(ctx, hook, "second-hook", time.Now()); err == nil {
		t.Fatal("same trigger overlap allowed")
	}
	hook.Schedule.ID = "other-hook"
	if err := svc.claimScheduleRun(ctx, hook, "other-hook-run", time.Now()); err != nil {
		t.Fatal(err)
	}
	normal.Schedule.ID = "other-cron"
	if err := svc.claimScheduleRun(ctx, normal, "other-normal-run", time.Now()); err == nil {
		t.Fatal("schedule lock lost")
	}
}
func TestWebhookRetentionPreservesActiveAndOrdinaryRuns(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	workspace := "Workflow/test"
	if err := os.MkdirAll(filepath.Join(docs, workspace, "runs/iteration-0"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &SchedulerService{stateStore: store}
	ctx := context.Background()
	folders := []string{}
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("run-%d", i)
		folder, err := allocateWebhookRunFolder(workspace, id)
		if err != nil {
			t.Fatal(err)
		}
		folders = append(folders, folder)
		if err := store.BeginRun(ctx, schedulerstate.Run{RunID: id, ScopeType: "workflow", ScopeID: workspace, LockKey: id, ScheduleID: id, TriggerSource: "webhook"}); err != nil {
			t.Fatal(err)
		}
		if err := store.AssignRunFolder(ctx, id, folder); err != nil {
			t.Fatal(err)
		}
		if i < 13 {
			if err := store.Transition(ctx, schedulerstate.Transition{RunID: id, To: schedulerstate.StateFailed, At: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err := svc.pruneWebhookRunsChecked(workspace); err != nil {
			t.Fatal(err)
		}
	}
	for i, folder := range folders {
		_, err := os.Stat(filepath.Join(docs, workspace, "runs", folder))
		if i < 3 {
			if !os.IsNotExist(err) || !webhookArtifactsExpired(workspace, fmt.Sprintf("run-%d", i)) {
				t.Fatalf("old run retained: %s %v", folder, err)
			}
		} else if err != nil {
			t.Fatalf("retained/active removed: %s", folder)
		}
	}
	if _, err := os.Stat(filepath.Join(docs, workspace, "runs/iteration-0")); err != nil {
		t.Fatal("normal removed")
	}
	if _, err := store.GetRun(ctx, "run-0"); err != nil {
		t.Fatal("history removed")
	}
}
func TestWebhookConcurrentOutcomeIsolation(t *testing.T) {
	all := []RunFolderInfo{{Name: "iteration-0/dev"}, {Name: "iteration-1-hook/dev"}, {Name: "iteration-2-hook/prod"}}
	if got := webhookInvocationFolders(all, "iteration-1-hook", true); len(got) != 1 || got[0].Name != "iteration-1-hook/dev" {
		t.Fatalf("mixed hook result: %+v", got)
	}
	if got := webhookInvocationFolders(all, "iteration-0", false); len(got) != 1 || got[0].Name != "iteration-0/dev" {
		t.Fatalf("mixed schedule result: %+v", got)
	}
}

func TestWebhookRejectsProtectedAndUndeclaredVariables(t *testing.T) {
	manifest := NewWorkflowManifest("test")
	manifest.Capabilities.SelectedSecrets = []string{"DB_URL"}
	raw, _ := json.Marshal(manifest)
	mock := &mockWorkspaceAPI{files: map[string]string{manifestPath("Workflow/test"): string(raw), "Workflow/test/variables/variables.json": `{"variables":[{"name":"base_url"},{"name":"DB_URL"},{"name":"PATH"},{"name":"access_token"}]}`}}
	ws := httptest.NewServer(mock)
	defer ws.Close()
	t.Setenv("WORKSPACE_API_URL", ws.URL)
	if err := validateWebhookVariableNames(context.Background(), "Workflow/test", []string{"base_url"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"DB_URL", "PATH", "access_token", "unknown"} {
		if err := validateWebhookVariableNames(context.Background(), "Workflow/test", []string{name}); err == nil {
			t.Fatalf("unsafe variable %s accepted", name)
		}
	}
}

func TestWebhookFolderNumbersSurviveCleanup(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	workspace := "Workflow/test"
	if err := os.MkdirAll(filepath.Join(docs, workspace), 0700); err != nil {
		t.Fatal(err)
	}
	first, err := allocateWebhookRunFolder(workspace, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(docs, workspace, "runs", first)); err != nil {
		t.Fatal(err)
	}
	next, err := allocateWebhookRunFolder(workspace, "two")
	if err != nil || next == first {
		t.Fatalf("folder number reused: %s %v", next, err)
	}
}

func TestStoppingIdleWebhookDoesNotStopConcurrentSchedule(t *testing.T) {
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewSchedulerService(&StreamingAPI{})
	svc.stateStore = store
	ctx := context.Background()
	normal := &ScheduleContext{WorkspacePath: "Workflow/test", Schedule: WorkflowSchedule{ID: "cron", ScheduleType: "cron"}}
	if err := svc.claimScheduleRun(ctx, normal, "normal-run", time.Now()); err != nil {
		t.Fatal(err)
	}
	svc.StopRunningJobForWorkflow("Workflow/test", "idle-hook")
	run, err := store.GetRun(ctx, "normal-run")
	if err != nil || run.CompletedAt != nil {
		t.Fatalf("unrelated schedule stopped: %+v %v", run, err)
	}
}
