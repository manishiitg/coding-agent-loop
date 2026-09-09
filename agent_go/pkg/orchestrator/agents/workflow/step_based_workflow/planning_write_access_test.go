package step_based_workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	workspacepkg "github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func TestPlanMutationWriteAccessDoesNotUnblockGeneralFileWrites(t *testing.T) {
	const sessionID = "plan-mutation-write-access"
	const planPath = "Workflow/demo/planning/plan.json"
	const evaluationPlanPath = "Workflow/demo/evaluation/evaluation_plan.json"
	workspacepkg.SetSessionFolderGuard(sessionID, []string{"Workflow/demo"}, []string{"Workflow/demo"})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{"Workflow/demo/planning", evaluationPlanPath})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	if err := client.ValidatePathWithContext(ctx, planPath, true); err == nil {
		t.Fatal("ordinary file write unexpectedly reached the guarded plan")
	}

	called := false
	writeFile := withPlanMutationWriteAccess("Workflow/demo", func(callCtx context.Context, path, _ string) error {
		called = true
		return client.ValidatePathWithContext(callCtx, path, true)
	})
	if err := writeFile(ctx, planPath, `{}`); err != nil {
		t.Fatalf("dedicated plan mutation write was blocked: %v", err)
	}
	if err := writeFile(ctx, evaluationPlanPath, `{}`); err != nil {
		t.Fatalf("dedicated evaluation-plan mutation write was blocked: %v", err)
	}
	if !called {
		t.Fatal("plan mutation write callback was not called")
	}

	if err := client.ValidatePathWithContext(ctx, planPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("plan capability leaked back into the caller context: %v", err)
	}
	if err := client.ValidatePathWithContext(ctx, evaluationPlanPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("evaluation-plan capability leaked back into the caller context: %v", err)
	}
}

func TestPlanMutationWriteAccessDoesNotUnlockEvaluationSiblings(t *testing.T) {
	const (
		sessionID     = "evaluation-plan-mutation-write-access"
		workspacePath = "Workflow/demo"
	)
	workspacepkg.SetSessionFolderGuard(sessionID, []string{workspacePath}, []string{workspacePath})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{workspacePath + "/evaluation"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	writeFile := withPlanMutationWriteAccess(workspacePath, func(callCtx context.Context, path, _ string) error {
		return client.ValidatePathWithContext(callCtx, path, true)
	})

	if err := writeFile(ctx, workspacePath+"/"+evaluationPlanRelPath, `{}`); err != nil {
		t.Fatalf("canonical evaluation-plan tool path was blocked: %v", err)
	}
	for _, sibling := range []string{
		workspacePath + "/evaluation/step_config.json",
		workspacePath + "/evaluation/other.json",
	} {
		if err := writeFile(ctx, sibling, `{}`); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
			t.Fatalf("managed evaluation-plan capability unlocked sibling %s: %v", sibling, err)
		}
	}
}

func TestPlanningFileMutationWriteAccessIsExact(t *testing.T) {
	const (
		sessionID     = "planning-file-mutation-write-access"
		workspacePath = "Workflow/demo"
		managedPath   = workspacePath + "/planning/changelog/changelog-test.json"
	)
	workspacepkg.SetSessionFolderGuard(sessionID, []string{workspacePath}, []string{workspacePath})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{workspacePath + "/planning"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	managedCtx := withPlanningFileMutationWriteAccess(ctx, workspacePath, "changelog/changelog-test.json")

	if err := client.ValidatePathWithContext(managedCtx, managedPath, true); err != nil {
		t.Fatalf("exact managed planning file should be writable: %v", err)
	}
	for _, blockedSibling := range []string{
		workspacePath + "/planning/plan.json",
		workspacePath + "/planning/changelog/other.json",
	} {
		if err := client.ValidatePathWithContext(managedCtx, blockedSibling, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
			t.Fatalf("managed file capability unlocked sibling %s: %v", blockedSibling, err)
		}
	}
	if err := client.ValidatePathWithContext(ctx, managedPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("managed file capability leaked into caller context: %v", err)
	}
}

func TestWritePlanChangelogEntryUsesManagedFileAccess(t *testing.T) {
	const (
		sessionID     = "plan-changelog-managed-write-access"
		workspacePath = "Workflow/demo"
		filename      = "changelog-test.json"
		changelogPath = workspacePath + "/planning/changelog/" + filename
	)
	workspacepkg.SetSessionFolderGuard(sessionID, []string{workspacePath}, []string{workspacePath})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{workspacePath + "/planning"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	planChangelogSessionMutex.Lock()
	previousFile := planChangelogSessionFile
	previousStart := planChangelogSessionStart
	planChangelogSessionFile = filename
	planChangelogSessionStart = time.Now().UTC()
	planChangelogSessionMutex.Unlock()
	t.Cleanup(func() {
		planChangelogSessionMutex.Lock()
		planChangelogSessionFile = previousFile
		planChangelogSessionStart = previousStart
		planChangelogSessionMutex.Unlock()
	})

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	ctx = withPlanChangeOrigin(ctx, "workflow-builder")
	wrote := false
	var written string
	err := writePlanChangelogEntry(
		ctx,
		workspacePath,
		PlanChangelogEntry{Tool: "update_step_config", Reason: "test managed changelog write"},
		func(context.Context, string) (string, error) { return "", errors.New("not found") },
		func(writeCtx context.Context, path, content string) error {
			wrote = true
			written = content
			if path != changelogPath {
				t.Fatalf("unexpected changelog path: %s", path)
			}
			return client.ValidatePathWithContext(writeCtx, path, true)
		},
		loggerv2.NewNoop(),
	)
	if err != nil {
		t.Fatalf("typed changelog write was blocked: %v", err)
	}
	if !wrote {
		t.Fatal("typed changelog writer was not called")
	}
	var changelog PlanChangelog
	if err := json.Unmarshal([]byte(written), &changelog); err != nil {
		t.Fatalf("decode written changelog: %v", err)
	}
	if len(changelog.Entries) != 1 || changelog.Entries[0].ChangeID == "" {
		t.Fatalf("missing stable change identity: %+v", changelog.Entries)
	}
	if changelog.Entries[0].Origin.Type != "user_chat" || changelog.Entries[0].Origin.SessionID != sessionID {
		t.Fatalf("origin = %+v", changelog.Entries[0].Origin)
	}
	if err := client.ValidatePathWithContext(ctx, changelogPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("changelog capability leaked into caller context: %v", err)
	}
}

func TestParsePlanDependencySurfaceReviewsRequiresEverySurface(t *testing.T) {
	complete := map[string]interface{}{}
	for _, surface := range requiredPlanDependencySurfaces {
		complete[surface] = map[string]interface{}{
			"disposition": "already_compatible",
			"evidence":    []interface{}{"checked the canonical consumer"},
		}
	}
	got, err := parsePlanDependencySurfaceReviews(complete)
	if err != nil || len(got) != len(requiredPlanDependencySurfaces) {
		t.Fatalf("complete surface review rejected: reviews=%+v err=%v", got, err)
	}
	delete(complete, "reporting")
	if _, err := parsePlanDependencySurfaceReviews(complete); err == nil || !strings.Contains(err.Error(), "reporting is required") {
		t.Fatalf("missing reporting surface was accepted: %v", err)
	}
}

func TestParsePlanDependencySurfaceReviewsRequiresIssueForUnresolvedSurface(t *testing.T) {
	complete := map[string]interface{}{}
	for _, surface := range requiredPlanDependencySurfaces {
		complete[surface] = map[string]interface{}{
			"disposition": "already_compatible",
			"evidence":    []interface{}{"checked the canonical consumer"},
		}
	}
	complete["reporting"] = map[string]interface{}{
		"disposition": "broken",
		"evidence":    []interface{}{"dashboard still reads the retired field"},
	}
	if _, err := parsePlanDependencySurfaceReviews(complete); err == nil || !strings.Contains(err.Error(), "durable Pulse issue") {
		t.Fatalf("broken surface without lifecycle issue was accepted: %v", err)
	}
	complete["reporting"].(map[string]interface{})["issue_ids"] = []interface{}{"PUL-1234ABCD"}
	got, err := parsePlanDependencySurfaceReviews(complete)
	if err != nil || len(got["reporting"].IssueIDs) != 1 {
		t.Fatalf("linked broken surface rejected: reviews=%+v err=%v", got, err)
	}
}

func TestPulseChangeReferencesPreserveAttemptFindingAndHumanDecision(t *testing.T) {
	ctx := context.Background()
	workspacePath := concernsWorkspace(t)
	db, err := openRunConcernsDB(ctx, workspacePath, true)
	if err != nil {
		t.Fatalf("open concerns db: %v", err)
	}
	defer db.Close()
	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatalf("ensure lifecycle schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_fix_attempts
		(attempt_id,module,pulse_run_id,started_at) VALUES ('fix-1','technical_review','pulse-1','2026-08-28T00:00:00Z')`); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_fix_attempt_findings
		(attempt_id,fingerprint,finding_id) VALUES ('fix-1','fp-1','PUL-1234ABCD')`); err != nil {
		t.Fatalf("insert finding: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_events
		(fingerprint,pulse_run_id,event_type,metadata_json,recorded_at)
		VALUES ('fp-1','pulse-old','awaiting_user','{"human_input_id":"technical-decision-1"}','2026-08-27T00:00:00Z')`); err != nil {
		t.Fatalf("insert decision event: %v", err)
	}
	issues, attemptID, humanInputID := pulseChangeReferences(ctx, workspacePath, "pulse-1")
	if len(issues) != 1 || issues[0] != "PUL-1234ABCD" || attemptID != "fix-1" || humanInputID != "technical-decision-1" {
		t.Fatalf("references = issues=%v attempt=%q human_input=%q", issues, attemptID, humanInputID)
	}
}

func TestManagedRetentionManifestWriteIsScoped(t *testing.T) {
	hcpo, dir := newPreValidationConcernTestOrchestrator(t)
	const sessionID = "retention-config-write"
	root := hcpo.GetWorkspacePath()
	workspacepkg.SetSessionFolderGuard(sessionID, []string{root}, []string{root + "/runs"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	manifest := `{"run_retention_count":10,"label":"keep"}`
	if err := hcpo.WriteWorkspaceFile(ctx, "workflow.json", manifest); err == nil {
		t.Fatal("raw manifest write must stay denied")
	}
	if err := hcpo.writeManagedWorkflowManifest(ctx, manifest); err != nil {
		t.Fatalf("managed retention write failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "workflow.json"))
	if err != nil || string(data) != manifest {
		t.Fatalf("manifest not persisted: %s %v", data, err)
	}
	for _, p := range []string{"workflow.json", "soul/soul.md", "../other/workflow.json"} {
		if err := hcpo.WriteWorkspaceFile(ctx, p, manifest); err == nil {
			t.Fatalf("managed capability leaked to raw %s", p)
		}
	}
}
