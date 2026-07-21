//nolint:misspell // "cancelled" is the established workshop status text and is surfaced to users.
package step_based_workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/guidance"
	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/instructions"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
	orchestrator_events "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/agent/prompt"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	_ "modernc.org/sqlite"
)

const maxCDPPortsPerWorkflow = 4

const workshopFixedIteration = "iteration-0"

const pulseReviewerCompletionPrefix = "PULSE_REVIEW_COMPLETE"

const pulseReviewerMaxConcurrency = 2

const statusPollWindow = 60 * time.Second

const statusPollNextAction = `**NEXT ACTION: End the current agent turn now.**
This execution sends an automatic completion notification. Do not call query_step or list_executions again unless the user explicitly requests another status check.`

const statusPollWarning = `**POLLING WARNING:** This is a repeated status check while background work is still running. End the current agent turn now; the runtime will resume the session automatically when the execution changes or completes.`

const statusPollSuppressed = `**POLLING SUPPRESSED**
The background execution state has not changed since the previous status check. A completion notification will resume this session automatically.

**NEXT ACTION: End the current agent turn now.** Do not call query_step or list_executions again unless the user explicitly requests another status check.`

var workshopStageAgentIdentityCounter atomic.Uint64

const workshopLearningScaffoldTemplate = `---
name: %s
description: "Global HOW-to-run notes for this workflow."
disable-model-invocation: true
user-invocable: false
---

# Overview

This skill captures reusable workflow knowledge for future runs.

## References

- Add topic-specific notes under ` + "`references/`" + ` as patterns emerge.
`

func parseWorkshopIterationNumber(iteration string) int {
	if iteration == "" {
		return 0
	}
	trimmed := strings.TrimSpace(iteration)
	trimmed = strings.TrimPrefix(trimmed, "iteration-")
	if n, err := strconv.Atoi(trimmed); err == nil {
		return n
	}
	return 0
}

func sanitizeWorkshopAgentIdentityPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	identity := strings.Trim(builder.String(), "-")
	if identity == "" {
		identity = "reviewer"
	}
	if len(identity) > 48 {
		identity = strings.TrimRight(identity[:48], "-")
	}
	return identity
}

func newWorkshopStageAgentIdentity(name string) string {
	return fmt.Sprintf("%s-%d-%d", sanitizeWorkshopAgentIdentityPart(name), time.Now().UnixNano(), workshopStageAgentIdentityCounter.Add(1))
}

func pulseReviewerCompletionMarker(todoID string) string {
	return fmt.Sprintf("%s todo_id=%s", pulseReviewerCompletionPrefix, sanitizeWorkshopAgentIdentityPart(todoID))
}

func buildPulseReviewerInstruction(workspacePath, resultPath, instructions, marker string) string {
	scopeHeader := fmt.Sprintf("READ-ONLY REVIEW SCOPE: inspect only %s. If any evidence path resolves outside this workflow, stop and return scope_error. Keep the complete response under 6000 characters and do not use wide tables. Do not emit progress text as the final answer.\n\n", workspacePath)
	artifactContract := ""
	if strings.TrimSpace(resultPath) != "" {
		artifactContract = fmt.Sprintf("ARTIFACT-FIRST RESULT CONTRACT: Your complete final response is the exact findings body that the backend will persist at %s. Write it as a durable Markdown review artifact, not as a conversational message to the parent or user. Do not add greetings, progress narration, notification prose, or a second summary. Do not attempt to write the file yourself: this reviewer is read-only and the trusted backend persists the validated response atomically. The parent receives only the artifact path and must read that file.\n\n", resultPath)
	}
	completionFooter := fmt.Sprintf("\n\nIMPORTANT COMPLETION CONTRACT: This overrides any earlier response-ending instruction or marker in the review brief. Only after the complete review is written, emit this exact final line and nothing after it:\n%s", marker)
	return scopeHeader + artifactContract + strings.TrimSpace(instructions) + completionFooter
}

func completedPulseReviewerResult(result, marker string) (string, error) {
	result = strings.TrimSpace(result)
	if !strings.HasSuffix(result, marker) {
		return "", fmt.Errorf("reviewer output did not end with %q", marker)
	}
	result = strings.TrimSpace(strings.TrimSuffix(result, marker))
	if result == "" {
		return "", fmt.Errorf("reviewer output was empty before %q", marker)
	}
	return result, nil
}

func validatePulseReviewIdentity(reviewRunID, module string) error {
	reviewRunID = strings.TrimSpace(reviewRunID)
	module = strings.TrimSpace(module)
	separator := strings.IndexByte(reviewRunID, '_')
	if separator <= 0 {
		return fmt.Errorf("review_run_id must start with a UTC date-time and underscore")
	}
	if _, err := time.Parse("2006-01-02T15-04-05.000Z", reviewRunID[:separator]); err != nil {
		return fmt.Errorf("review_run_id must start with YYYY-MM-DDTHH-MM-SS.mmmZ: %w", err)
	}
	for _, r := range reviewRunID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return fmt.Errorf("review_run_id contains unsupported path characters")
		}
	}
	validModules := map[string]bool{
		"bug_review": true, "artifact_review": true, "report_health": true,
		"eval_health": true, "learning_health": true, "knowledgebase_health": true,
		"db_health": true, "cost_llm_time": true, "llm_ops_review": true,
		"goal_advisor": true,
	}
	if !validModules[module] {
		return fmt.Errorf("module %q is not a valid Pulse review module", module)
	}
	return nil
}

func pulseReviewResultPath(reviewRunID, module string) (string, error) {
	if err := validatePulseReviewIdentity(reviewRunID, module); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("pulse", "reviews", reviewRunID, module+".md")), nil
}

func pulseReviewResultMarkdown(pulseRunID, reviewRunID, module, status, result string, completedAt time.Time) string {
	return fmt.Sprintf("# Pulse reviewer result\n\n- Pulse run: `%s`\n- Review run: `%s`\n- Module: `%s`\n- Status: `%s`\n- Completed at: `%s`\n\n## Findings\n\n%s\n",
		strings.TrimSpace(pulseRunID), reviewRunID, module, status, completedAt.UTC().Format(time.RFC3339Nano), strings.TrimSpace(result))
}

func acquirePulseReviewerSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func normalizeWorkshopBuilderRunFolder(runFolder string) string {
	if runFolder == "" {
		return workshopFixedIteration
	}
	parts := strings.SplitN(runFolder, "/", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("%s/%s", workshopFixedIteration, parts[1])
	}
	return workshopFixedIteration
}

func isMissingOrEmptyWorkspaceError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "no such file") ||
		strings.Contains(errStr, "no content found")
}

func (iwm *InteractiveWorkshopManager) readWorkflowLabelForBootstrap(ctx context.Context) string {
	label := filepath.Base(strings.TrimSpace(iwm.controller.GetWorkspacePath()))
	content, err := iwm.controller.ReadWorkspaceFile(ctx, "workflow.json")
	if err != nil || strings.TrimSpace(content) == "" {
		return label
	}

	var manifest struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return label
	}
	if strings.TrimSpace(manifest.Label) != "" {
		return strings.TrimSpace(manifest.Label)
	}
	return label
}

func workshopGlobalLearningScaffold(workflowLabel string) string {
	label := strings.TrimSpace(workflowLabel)
	if label == "" {
		label = "Workflow"
	}
	return stringsReplace(workshopLearningScaffoldTemplate, "%s", label, 1)
}

func (iwm *InteractiveWorkshopManager) ensureWorkshopStoreFoldersExist(ctx context.Context) error {
	workspacePath := iwm.controller.GetWorkspacePath()

	for _, folder := range []string{
		DBFolderName,
		filepath.Join(DBFolderName, DBAssetsFolderName),
		KnowledgebaseFolderName,
		filepath.Join(KnowledgebaseFolderName, KnowledgebaseContextFolderName),
		filepath.Join(KnowledgebaseFolderName, KBNotesFolderName),
		LearningsFolderName,
		filepath.Join(LearningsFolderName, "_global"),
		filepath.Join(LearningsFolderName, "_global", "references"),
	} {
		if err := createFolderViaAPI(ctx, folder, workspacePath); err != nil {
			return fmt.Errorf("bootstrap folder %s: %w", folder, err)
		}
	}

	if err := InitKBGraphFiles(ctx, iwm.controller.BaseOrchestrator, workspacePath, ""); err != nil {
		return fmt.Errorf("bootstrap knowledgebase files: %w", err)
	}

	skillPath := filepath.Join(LearningsFolderName, "_global", "SKILL.md")
	skillContent, err := iwm.controller.ReadWorkspaceFile(ctx, skillPath)
	if err == nil && strings.TrimSpace(skillContent) != "" {
		return nil
	}
	if err != nil && !isMissingOrEmptyWorkspaceError(err) {
		return fmt.Errorf("read %s: %w", skillPath, err)
	}

	if err := iwm.controller.WriteWorkspaceFile(ctx, skillPath, workshopGlobalLearningScaffold(iwm.readWorkflowLabelForBootstrap(ctx))); err != nil {
		return fmt.Errorf("bootstrap %s: %w", skillPath, err)
	}
	iwm.controller.GetLogger().Info("🆕 Bootstrapped learnings/_global/SKILL.md for workflow workshop")
	return nil
}

// ensureWorkshopBootstrapFilesExist bootstraps plan and shared workflow stores for
// brand-new workflows so the builder can start cleanly and edit those areas on the
// first turn without hitting missing-path errors.
func (iwm *InteractiveWorkshopManager) ensureWorkshopBootstrapFilesExist(ctx context.Context) error {
	if iwm.controller.isEvaluationMode {
		return nil
	}

	planContent, err := iwm.controller.ReadWorkspaceFile(ctx, "planning/plan.json")
	if err == nil && strings.TrimSpace(planContent) != "" {
		return iwm.ensureWorkshopStoreFoldersExist(ctx)
	}

	if err != nil {
		if !isMissingOrEmptyWorkspaceError(err) {
			return err
		}
	} else {
		iwm.controller.GetLogger().Warn("⚠️ planning/plan.json is empty — bootstrapping an empty plan so the workshop can start")
	}

	emptyPlan := &PlanningResponse{}
	data, marshalErr := json.MarshalIndent(emptyPlan, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal empty plan.json bootstrap: %w", marshalErr)
	}
	if writeErr := iwm.controller.writeManagedPlanningFile(ctx, "plan.json", string(data)); writeErr != nil {
		return fmt.Errorf("failed to bootstrap empty planning/plan.json: %w", writeErr)
	}

	iwm.controller.GetLogger().Info("🆕 Bootstrapped empty planning/plan.json for workflow workshop")
	return iwm.ensureWorkshopStoreFoldersExist(ctx)
}

// ============================================================================
// Workshop Step Hierarchy — recursive step discovery for inner steps
// ============================================================================

// WorkshopStepInfo describes a step in the plan, including its position in the hierarchy.
type WorkshopStepInfo struct {
	Step           PlanStepInterface
	ParentID       string // empty for top-level steps
	ParentType     StepType
	NestedLocation string // e.g. "route:route-id" or "todo_task_step"
	TopIndex       int    // 1-based index of the top-level step this belongs to (-1 if inner)
	IsOrphan       bool   // true for orphan steps (workshop-only, not in main execution flow)
}

// collectAllSteps returns a flat list of all steps in the plan, including inner steps
// from todo task routes and sub-agents.
func collectAllSteps(steps []PlanStepInterface) []WorkshopStepInfo {
	var result []WorkshopStepInfo
	for i, step := range steps {
		result = append(result, WorkshopStepInfo{
			Step:     step,
			TopIndex: i + 1,
		})
		result = append(result, collectInnerSteps(step)...)
	}
	return result
}

// collectInnerSteps recursively extracts inner steps from a step.
func collectInnerSteps(step PlanStepInterface) []WorkshopStepInfo {
	var result []WorkshopStepInfo
	parentID := step.GetID()
	parentType := step.StepType()

	switch s := step.(type) {
	case *TodoTaskPlanStep:
		for _, route := range s.PredefinedRoutes {
			if route.SubAgentStep != nil {
				result = append(result, WorkshopStepInfo{
					Step: route.SubAgentStep, ParentID: parentID, ParentType: parentType,
					NestedLocation: fmt.Sprintf("route:%s", route.RouteID), TopIndex: -1,
				})
				result = append(result, collectInnerSteps(route.SubAgentStep)...)
			}
		}
	}
	return result
}

// StepModeAgentic is the canonical mode name for steps where the LLM acts
// each turn with tools available (the default). Previously called
// "agentic" — the old name biased agents toward writing Python scripts
// when a tool call would do. "Agentic" describes the actual behavior:
// the LLM decides each turn what to call. Old name is still accepted on
// read for backward compatibility with persisted configs.
const StepModeAgentic = "agentic"

// StepModeScripted is the canonical mode name for steps that author a
// reusable main.py saved at learnings/{step-id}/main.py, replayed
// deterministically on future runs. Previously called "scripted".
// The on-disk directory name remains "learnings/" — the mode label and
// the directory are decoupled. Old name is still accepted on read.
const StepModeScripted = "scripted"

// canonicalDeclaredExecutionMode normalizes the declared execution mode
// string. It accepts both the new canonical names ("agentic", "scripted")
// and the legacy names ("agentic", "scripted") on input, always
// returning the canonical form. Empty input returns empty.
func canonicalDeclaredExecutionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "code_exec", StepModeAgentic:
		return StepModeAgentic
	case "learn_code", StepModeScripted:
		return StepModeScripted
	default:
		return strings.TrimSpace(mode)
	}
}

// isScriptedExecutionModeConfig returns true when the step is in scripted mode
// (persistent scripted code path where main.py is saved and reused across runs).
// agentic steps also use code execution but do NOT write persistent scripts;
// any leftover learnings/{step-id}/main.py for a agentic step is stale artifact
// debt and should be deleted.
func isScriptedExecutionModeConfig(cfg *AgentConfigs) bool {
	if cfg == nil {
		return false
	}
	return canonicalDeclaredExecutionMode(cfg.DeclaredExecutionMode) == StepModeScripted
}

// isOrchestratorScriptedEligible gates the todo_task fast path: the builder-authored
// main.py is only run when the step declares scripted and has at least one
// predefined route for the script to call. If either check fails the step runs as a
// normal LLM orchestrator — the script is never attempted.
// The orchestrator scripted path is read-only at runtime: the builder writes
// main.py at design time, the runtime only runs it. There is no repair loop and no
// save-back; any script failure falls back to the LLM orchestrator with a fresh start.
func isOrchestratorScriptedEligible(step *TodoTaskPlanStep, cfg *AgentConfigs) bool {
	if step == nil || !isScriptedExecutionModeConfig(cfg) {
		return false
	}
	if len(step.PredefinedRoutes) == 0 {
		return false
	}
	return true
}

func syncDeclaredExecutionModeConfig(cfg *AgentConfigs) {
	if cfg == nil {
		return
	}

	switch canonicalDeclaredExecutionMode(cfg.DeclaredExecutionMode) {
	case "agentic":
		trueVal := true
		cfg.DeclaredExecutionMode = "agentic"
		cfg.UseCodeExecutionMode = &trueVal
		falseVal := false
		cfg.LockCode = &falseVal
	case "scripted":
		trueVal := true
		cfg.DeclaredExecutionMode = "scripted"
		cfg.UseCodeExecutionMode = &trueVal
	}
}

// findWorkshopStepByID searches all steps (including inner) for a matching ID.
func findWorkshopStepByID(steps []PlanStepInterface, stepID string) *WorkshopStepInfo {
	all := collectAllSteps(steps)
	for _, info := range all {
		if info.Step.GetID() == stepID {
			return &info
		}
	}
	return nil
}

// workshopStepLogFolder mirrors the runtime artifact writer: declared step IDs
// are the canonical folder names for normal and directly addressed inner steps.
func workshopStepLogFolder(stepID string) string {
	return strings.TrimSpace(stepID)
}

// ============================================================================
// Complex Step Query Enrichment — lightweight hints for todo_task/orchestration
// ============================================================================

// enrichQueryForComplexStep detects todo_task/orchestration steps and returns
// a compact hint with iteration count and log paths for deeper inspection.
// Keeps responses small — the LLM can use execute_shell_command to dig deeper.
func (iwm *InteractiveWorkshopManager) enrichQueryForComplexStep(
	ctx context.Context,
	stepID string,
) string {
	if iwm.controller.approvedPlan == nil {
		return ""
	}

	stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID)
	if stepInfo == nil || stepInfo.TopIndex < 1 {
		return ""
	}

	stepType := stepInfo.Step.StepType()
	var logFileName, stepTypeName string
	switch stepType {
	case StepTypeTodoTask:
		logFileName = "todo-task-execution.json"
		stepTypeName = "Todo Task"
	case StepTypeRouting:
		logFileName = "orchestration-execution.json"
		stepTypeName = "Orchestration"
	default:
		return ""
	}

	runFolder := iwm.controller.selectedRunFolder
	if runFolder == "" {
		return ""
	}

	logFolder := workshopStepLogFolder(stepID)
	logPath := fmt.Sprintf("runs/%s/logs/%s/%s", runFolder, logFolder, logFileName)

	content, err := iwm.controller.ReadWorkspaceFile(ctx, logPath)
	if err != nil {
		return ""
	}

	// Count iterations and collect sub-agent paths + tier usage from JSONL
	lines := strings.Split(strings.TrimSpace(content), "\n")
	iterations := 0
	var subAgentPaths []string
	seen := make(map[string]bool)
	var lastEntry map[string]interface{} // retain last parsed entry for todo progress

	// Track tier usage per route/generic agent for optimization analysis
	type tierUsageEntry struct {
		RouteID   string
		RouteName string
		TodoID    string
		TodoTitle string
		Tier      int
		TierLabel string
		IsGeneric bool
	}
	var tierUsageLog []tierUsageEntry

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		iterations++

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		lastEntry = entry

		// Extract sub-agent path from the response
		var response map[string]interface{}
		if stepType == StepTypeTodoTask {
			response, _ = entry["todo_task_response"].(map[string]interface{})
		} else {
			response, _ = entry["orchestration_response"].(map[string]interface{})
		}
		if response == nil {
			continue
		}

		subPath, _ := response["selected_sub_agent_path"].(string)
		if subPath != "" && !seen[subPath] {
			seen[subPath] = true
			subAgentPaths = append(subAgentPaths, subPath)
		}

		// Extract tier usage data for todo_task steps
		if stepType == StepTypeTodoTask {
			nextAction, _ := response["next_action"].(string)
			if nextAction == "delegate" {
				tierNum := 0
				if t, ok := response["preferred_tier"].(float64); ok {
					tierNum = int(t)
				}
				tierLabel, _ := response["preferred_tier_label"].(string)
				routeID, _ := response["selected_route_id"].(string)
				routeName, _ := response["selected_route_name"].(string)
				todoID, _ := response["todo_id_to_execute"].(string)
				todoTitle, _ := response["todo_title"].(string)
				isGeneric, _ := response["use_generic_agent"].(bool)

				tierUsageLog = append(tierUsageLog, tierUsageEntry{
					RouteID:   routeID,
					RouteName: routeName,
					TodoID:    todoID,
					TodoTitle: todoTitle,
					Tier:      tierNum,
					TierLabel: tierLabel,
					IsGeneric: isGeneric,
				})
			}
		}
	}

	if iterations == 0 {
		return ""
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("\n\n[%s step — %d iterations", stepTypeName, iterations))

	// Show todo progress from the last entry (already parsed in main loop)
	if stepType == StepTypeTodoTask && lastEntry != nil {
		if todoSummary, ok := lastEntry["todo_summary"].(map[string]interface{}); ok {
			total, _ := todoSummary["total"].(float64)
			completed, _ := todoSummary["completed"].(float64)
			if total > 0 {
				summary.WriteString(fmt.Sprintf(", %.0f/%.0f tasks done", completed, total))
			}
		}
	}
	summary.WriteString("]\n")

	// Log paths for deeper inspection
	summary.WriteString(fmt.Sprintf("Routing log: %s\n", logPath))
	if len(subAgentPaths) > 0 {
		summary.WriteString("Sub-agent logs:\n")
		for _, p := range subAgentPaths {
			summary.WriteString(fmt.Sprintf("  - runs/%s/logs/%s/execution/\n", runFolder, p))
		}
	}
	summary.WriteString("Use execute_shell_command to read these files for details.")

	// Append tier usage summary for todo_task steps
	if len(tierUsageLog) > 0 {
		summary.WriteString("\n\nTier Usage per Sub-Agent:\n")
		summary.WriteString("| Route/Agent | Todo | Tier | Label |\n")
		summary.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, tu := range tierUsageLog {
			agentName := tu.RouteName
			if agentName == "" && tu.IsGeneric {
				agentName = "generic-agent"
			} else if agentName == "" {
				agentName = tu.RouteID
			}
			tierStr := "auto"
			if tu.Tier > 0 {
				tierStr = fmt.Sprintf("%d", tu.Tier)
			}
			tierLabel := tu.TierLabel
			if tierLabel == "" {
				tierLabel = "auto-selected"
			}
			summary.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", agentName, tu.TodoTitle, tierStr, tierLabel))
		}
	}

	return summary.String()
}

// ============================================================================
// Workshop Step Registry — tracks background step executions
// ============================================================================

// WorkshopStepStatus represents the status of a background step execution
type WorkshopStepStatus string

const (
	WorkshopStepRunning   WorkshopStepStatus = "running"
	WorkshopStepDone      WorkshopStepStatus = "done"
	WorkshopStepFailed    WorkshopStepStatus = "failed"
	WorkshopStepCancelled WorkshopStepStatus = "cancelled"
)

// ToolCallQueryFunc queries live tool calls associated with a workshop execution.
// Parameters: sessionID (main session), correlationID (agentSessionID for the step execution), stepID, toolCallID (empty for summary, specific ID for detail).
// Returns a formatted string summary of tool calls. Nil means the feature is unavailable.
type ToolCallQueryFunc func(sessionID, correlationID, stepID, toolCallID string) string

// TmuxLookupFunc resolves the live tmux session for a workshop step running a
// coding-CLI provider (tmux transport). Given the terminal-store session key and
// stepID, it returns the tmux session name and a freshly-captured tail of the pane
// (the latest terminal output), plus ok=true when the step is tmux-backed. paneTail
// may be empty if the live capture failed. Nil func means the feature is unavailable.
type TmuxLookupFunc func(ctx context.Context, mainSessionID, stepID string) (tmuxSession, paneTail string, ok bool)

// WorkshopStepExecution tracks a single background step execution
type WorkshopStepExecution struct {
	ID                      string
	StepID                  string
	AgentSessionID          string // correlation ID used to tag events for this execution
	Status                  WorkshopStepStatus
	CreatedAt               time.Time
	Result                  string
	Err                     error
	cancel                  context.CancelFunc
	mu                      sync.RWMutex
	messageSendMu           sync.Mutex
	messageTarget           *workshopStepMessageTarget
	messageTargetGeneration uint64
}

// WorkshopExecutionStart carries the canonical information needed to register a running execution.
type WorkshopExecutionStart struct {
	ID                string
	ParentExecutionID string
	Name              string
	Kind              string
	Metadata          map[string]string
	Cancel            context.CancelFunc
}

// WorkshopStepSnapshot is a read-only copy of a tracked execution for external callers.
type WorkshopStepSnapshot struct {
	ID                 string
	StepID             string
	AgentSessionID     string
	Status             WorkshopStepStatus
	CreatedAt          time.Time
	Result             string
	Err                error
	CanCancel          bool
	CanReceiveMessage  bool
	ActiveMessagePhase string
}

var (
	ErrWorkshopExecutionNotFound      = errors.New("workshop execution not found")
	ErrWorkshopExecutionNotCancelable = errors.New("workshop execution is not cancelable")
)

func (e *WorkshopStepExecution) Snapshot() WorkshopStepSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	snapshot := WorkshopStepSnapshot{
		ID:             e.ID,
		StepID:         e.StepID,
		AgentSessionID: e.AgentSessionID,
		Status:         e.Status,
		CreatedAt:      e.CreatedAt,
		Result:         e.Result,
		Err:            e.Err,
		CanCancel:      e.Status == WorkshopStepRunning && e.cancel != nil,
	}
	if e.Status == WorkshopStepRunning && e.messageTarget != nil {
		snapshot.CanReceiveMessage = true
		snapshot.ActiveMessagePhase = e.messageTarget.phase
	}
	return snapshot
}

// finalizeExecStatus sets the final status on a WorkshopStepExecution under lock,
// with context-cancellation detection.
//
// Returns true only when stop_step/stop_all already set the status to Cancelled
// (meaning OnExecutionTerminated was already called — skip OnExecutionComplete).
// Returns false for context-cancelled errors (status is set to Cancelled but
// OnExecutionComplete must still fire since OnExecutionTerminated was NOT called).
//
// Callers that need to distinguish cancel vs failure for display purposes should
// check execCtx.Err() != nil after this call.
//
// Usage at the top of every background execution goroutine:
//
//	var result string
//	var execErr error
//	defer func() {
//	    skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
//	    // ... eventBridge end event (use execCtx.Err() != nil for cancel display) ...
//	    if !skipNotify && notifier != nil {
//	        notifier.OnExecutionComplete(execID, name, result, meta, execErr)
//	    }
//	}()
func finalizeExecStatus(exec *WorkshopStepExecution, ctx context.Context, result *string, execErr *error) (skipNotify bool) {
	exec.messageSendMu.Lock()
	defer exec.messageSendMu.Unlock()
	exec.mu.Lock()
	defer exec.mu.Unlock()
	exec.messageTarget = nil
	exec.cancel = nil
	if exec.Status == WorkshopStepCancelled {
		log.Printf("[FINALIZE_EXEC] exec=%s step=%s — already cancelled by stop_step, skipNotify=true", exec.ID, exec.StepID)
		return true // stop_step already called OnExecutionTerminated
	}
	if *execErr != nil {
		errStr := strings.ToLower((*execErr).Error())
		isTimeout := errors.Is(*execErr, context.DeadlineExceeded) ||
			(ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) ||
			strings.Contains(errStr, "timed out") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "deadline exceeded")

		if isTimeout {
			exec.Status = WorkshopStepFailed
			exec.Err = *execErr
			log.Printf("[FINALIZE_EXEC] exec=%s step=%s — timeout detected (err=%v), status=Failed, skipNotify=false", exec.ID, exec.StepID, *execErr)
		} else if ctx.Err() == context.Canceled || errors.Is(*execErr, context.Canceled) {
			exec.Status = WorkshopStepCancelled
			exec.Err = *execErr
			log.Printf("[FINALIZE_EXEC] exec=%s step=%s — context cancelled (err=%v), status=Cancelled, skipNotify=false (OnExecutionComplete will fire)", exec.ID, exec.StepID, *execErr)
		} else {
			exec.Status = WorkshopStepFailed
			exec.Err = *execErr
			log.Printf("[FINALIZE_EXEC] exec=%s step=%s — failed (err=%v)", exec.ID, exec.StepID, *execErr)
		}
	} else {
		exec.Status = WorkshopStepDone
		log.Printf("[FINALIZE_EXEC] exec=%s step=%s — done (result_len=%d)", exec.ID, exec.StepID, len(*result))
		exec.Result = *result
	}
	return false
}

// WorkshopStepRegistry tracks all background step executions for a workshop session
type WorkshopStepRegistry struct {
	mu         sync.RWMutex
	executions map[string]*WorkshopStepExecution

	statusPollMu              sync.Mutex
	statusPollWindowStartedAt time.Time
	statusPollLastCalledAt    time.Time
	statusPollFingerprint     string
	statusPollCount           int
}

type statusPollDecision struct {
	Count    int
	Changed  bool
	Warn     bool
	Suppress bool
}

func newWorkshopStepRegistry() *WorkshopStepRegistry {
	return &WorkshopStepRegistry{
		executions: make(map[string]*WorkshopStepExecution),
	}
}

// NewWorkshopStepRegistry creates a new empty WorkshopStepRegistry (exported for use in server.go chat mode).
func NewWorkshopStepRegistry() *WorkshopStepRegistry {
	return newWorkshopStepRegistry()
}

// observeStatusPoll applies one shared rapid-poll budget across query_step and
// list_executions. The registry is session-scoped and survives tool
// re-registration between agent turns, so alternating tools cannot bypass the
// guard. Changed state is always returned, but it does not reset the rapid-poll
// count; the next unchanged call is still suppressed.
func (r *WorkshopStepRegistry) observeStatusPoll(now time.Time, fingerprint string) statusPollDecision {
	r.statusPollMu.Lock()
	defer r.statusPollMu.Unlock()

	if r.statusPollLastCalledAt.IsZero() ||
		now.Sub(r.statusPollLastCalledAt) > statusPollWindow ||
		now.Sub(r.statusPollWindowStartedAt) > statusPollWindow {
		r.statusPollWindowStartedAt = now
		r.statusPollCount = 0
		r.statusPollFingerprint = ""
	}

	changed := r.statusPollFingerprint != "" && r.statusPollFingerprint != fingerprint
	r.statusPollCount++
	r.statusPollLastCalledAt = now
	r.statusPollFingerprint = fingerprint

	decision := statusPollDecision{
		Count:   r.statusPollCount,
		Changed: changed,
		Warn:    r.statusPollCount >= 2,
	}
	decision.Suppress = r.statusPollCount >= 3 && !changed
	return decision
}

// Register adds a new execution entry to the registry
func (r *WorkshopStepRegistry) Register(exec *WorkshopStepExecution) {
	exec.mu.Lock()
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now()
	}
	exec.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.executions[exec.ID] = exec
}

// Get retrieves an execution by ID; returns nil if not found.
// Internal callers may use this for in-place status/result updates.
func (r *WorkshopStepRegistry) Get(id string) *WorkshopStepExecution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executions[id]
}

// GetSnapshot retrieves a read-only snapshot of an execution by ID.
func (r *WorkshopStepRegistry) GetSnapshot(id string) (WorkshopStepSnapshot, bool) {
	r.mu.RLock()
	exec := r.executions[id]
	r.mu.RUnlock()
	if exec == nil {
		return WorkshopStepSnapshot{}, false
	}
	return exec.Snapshot(), true
}

// LatestSnapshotForStep returns the newest tracked execution for a step.
// Running executions are preferred; completed executions are used only when
// nothing is running.
func (r *WorkshopStepRegistry) LatestSnapshotForStep(stepID string) (WorkshopStepSnapshot, bool, []WorkshopStepSnapshot) {
	r.mu.RLock()
	snapshots := make([]WorkshopStepSnapshot, 0)
	for _, exec := range r.executions {
		snap := exec.Snapshot()
		if snap.StepID == stepID {
			snapshots = append(snapshots, snap)
		}
	}
	r.mu.RUnlock()
	if len(snapshots) == 0 {
		return WorkshopStepSnapshot{}, false, nil
	}

	sort.SliceStable(snapshots, func(i, j int) bool {
		leftRunning := snapshots[i].Status == WorkshopStepRunning
		rightRunning := snapshots[j].Status == WorkshopStepRunning
		if leftRunning != rightRunning {
			return leftRunning
		}
		if snapshots[i].CreatedAt.Equal(snapshots[j].CreatedAt) {
			return snapshots[i].ID > snapshots[j].ID
		}
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})
	return snapshots[0], true, snapshots
}

// Cancel cancels an execution by ID and returns its updated snapshot.
func (r *WorkshopStepRegistry) Cancel(id string) (WorkshopStepSnapshot, error) {
	r.mu.RLock()
	exec := r.executions[id]
	r.mu.RUnlock()
	if exec == nil {
		return WorkshopStepSnapshot{}, ErrWorkshopExecutionNotFound
	}

	exec.messageSendMu.Lock()
	exec.mu.Lock()
	if exec.Status != WorkshopStepRunning || exec.cancel == nil {
		exec.mu.Unlock()
		exec.messageSendMu.Unlock()
		return exec.Snapshot(), ErrWorkshopExecutionNotCancelable
	}
	cancel := exec.cancel
	exec.Status = WorkshopStepCancelled
	exec.messageTarget = nil
	exec.cancel = nil
	exec.mu.Unlock()
	cancel()
	exec.messageSendMu.Unlock()
	return exec.Snapshot(), nil
}

// CancelAll cancels all running executions and returns their updated snapshots.
func (r *WorkshopStepRegistry) CancelAll() []WorkshopStepSnapshot {
	r.mu.RLock()
	var toCancel []*WorkshopStepExecution
	for _, exec := range r.executions {
		exec.mu.RLock()
		isRunning := exec.Status == WorkshopStepRunning
		exec.mu.RUnlock()
		if isRunning {
			toCancel = append(toCancel, exec)
		}
	}
	r.mu.RUnlock()

	cancelled := make([]WorkshopStepSnapshot, 0, len(toCancel))
	for _, exec := range toCancel {
		snapshot, err := r.Cancel(exec.ID)
		if err == nil {
			cancelled = append(cancelled, snapshot)
		}
	}
	return cancelled
}

// ListSnapshots returns read-only snapshots of all tracked executions.
func (r *WorkshopStepRegistry) ListSnapshots() []WorkshopStepSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]WorkshopStepSnapshot, 0, len(r.executions))
	for _, e := range r.executions {
		list = append(list, e.Snapshot())
	}
	return list
}

func (iwm *InteractiveWorkshopManager) statusPollStateFingerprint() string {
	parts := make([]string, 0)
	for _, exec := range iwm.stepRegistry.ListSnapshots() {
		parts = append(parts, fmt.Sprintf("registry:%s:%s", exec.ID, exec.Status))
	}
	if iwm.listServerAgents != nil {
		for _, agent := range iwm.listServerAgents() {
			parts = append(parts, fmt.Sprintf("server:%s:%s", agent.ID, agent.Status))
		}
	}
	if iwm.hasPendingCompletions != nil && iwm.hasPendingCompletions() {
		parts = append(parts, "pending-completions:true")
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func (iwm *InteractiveWorkshopManager) runningStatusPollGuidance() (string, bool) {
	decision := iwm.stepRegistry.observeStatusPoll(time.Now(), iwm.statusPollStateFingerprint())
	if decision.Suppress {
		return statusPollSuppressed, true
	}
	if decision.Warn {
		return statusPollWarning + "\n\n" + statusPollNextAction, false
	}
	return statusPollNextAction, false
}

// WorkshopExecutionNotifier is called when workshop step/background executions start and complete.
// Implemented by the server layer to register executions in bgAgentRegistry so that
// HasRunningAgents() returns true and the frontend keeps polling for events.
type WorkshopExecutionNotifier interface {
	OnExecutionStart(start WorkshopExecutionStart)
	OnExecutionComplete(execID, name, result string, meta map[string]string, err error)
	OnExecutionTerminated(execID, name string) // explicit cancellation via stop_step/stop_all
}

// ServerAgentInfo is a lightweight snapshot of a server-tracked background agent.
type ServerAgentInfo struct {
	ID     string
	Name   string
	Status string // "running", "completed", "failed", "canceled"
}

// ============================================================================
// InteractiveWorkshopManager
// ============================================================================

// InteractiveWorkshopManager manages the interactive workshop phase
type InteractiveWorkshopManager struct {
	controller             *StepBasedWorkflowOrchestrator
	workshopConfig         *WorkshopConfig
	presetLLM              *AgentLLMConfig
	sessionID              string
	workflowID             string
	stepRegistry           *WorkshopStepRegistry
	sessionCtx             context.Context                             // long-lived ctx for background goroutines
	toolCallQueryFunc      ToolCallQueryFunc                           // optional: query live tool calls for running steps
	tmuxLookupFunc         TmuxLookupFunc                              // optional: resolve live tmux session name for a coding-CLI step
	mainSessionID          string                                      // event store session ID for tool call queries
	schedulerWorkspacePath string                                      // workspace path for schedule management
	schedulerFuncs         *SchedulerCallbacks                         // schedule CRUD callbacks from server.go
	skillFuncs             *SkillCallbacks                             // skill import/delete callbacks from server.go
	llmToolsFuncs          *LLMToolsCallbacks                          // LLM management callbacks from server.go
	listAvailableSecrets   func(ctx context.Context) ([]string, error) // list all available secret names
	resolveSecretValues    func(ctx context.Context, names []string) map[string]string
	executionNotifier      WorkshopExecutionNotifier // optional: notifies server when executions start/complete
	hasPendingCompletions  func() bool               // optional: true if completions are queued for delivery
	hasRunningAgents       func() bool               // optional: true if server still has running background agents
	cancelAllServerAgents  func()                    // optional: cancel all running agents in server's bgAgentRegistry
	listServerAgents       func() []ServerAgentInfo  // optional: list all agents from server's bgAgentRegistry
	workshopModeOverride   string                    // frontend-selected workshop mode (takes priority over auto-detection)
	toolAgentSetupMu       sync.Mutex                // protects temporary controller/bridge state while tool agents are created
	pulseReviewerSlots     chan struct{}             // hard limit for synchronous Pulse reviewer fan-out
}

func uniqueStringsPreserveOrder(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// persistWorkflowConfigToManifest writes the current in-memory workflow config
// (servers, tools, skills, secrets) back to workflow.json so changes survive
// session end.
func (iwm *InteractiveWorkshopManager) persistWorkflowConfigToManifest(ctx context.Context, logger loggerv2.Logger) {
	wsPath := iwm.controller.GetWorkspacePath()
	if wsPath == "" {
		return
	}
	manifestPath := "workflow.json"

	// Read existing manifest
	content, err := iwm.controller.ReadWorkspaceFile(ctx, manifestPath)
	if err != nil {
		logger.Info("No workflow.json found — skipping manifest persist")
		return
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		logger.Warn(fmt.Sprintf("Failed to parse workflow.json: %v", err))
		return
	}

	// Get or create capabilities object
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		caps = make(map[string]interface{})
	}

	// Update from current controller state
	caps["selected_servers"] = iwm.controller.GetSelectedServers()
	caps["selected_tools"] = iwm.controller.GetSelectedTools()
	caps["selected_skills"] = iwm.controller.GetSelectedSkills()

	// Update secrets (names only, never values). Write to BOTH manifest fields:
	//   - selected_secrets           → used by loadSelectedSecrets to decrypt workflow/user-stored values
	//   - selected_global_secret_names → used by mergeGlobalSecrets to filter GLOBAL_SECRET_* env vars
	// Each loader path ignores names it can't resolve in its own bucket, so writing the
	// union to both fields lets user and global names each resolve via their own path.
	// Writing to only one field is the bug the builder chat hit: user secrets attached via
	// update_workflow_config never reached step runtime because selected_secrets stayed empty.
	secretNames := make([]string, 0)
	for _, s := range iwm.controller.GetSecrets() {
		if s.Name != "" {
			secretNames = append(secretNames, s.Name)
		}
	}
	caps["selected_secrets"] = secretNames
	caps["selected_global_secret_names"] = secretNames

	caps["use_code_execution_mode"] = iwm.controller.GetUseCodeExecutionMode()
	if mode := strings.ToLower(strings.TrimSpace(iwm.controller.GetBrowserMode())); mode != "" {
		caps["browser_mode"] = mode
	} else if _, exists := caps["browser_mode"]; !exists {
		caps["browser_mode"] = "none"
	}
	if ports := iwm.controller.GetCdpPorts(); len(ports) > 0 {
		caps["cdp_ports"] = ports
	} else {
		delete(caps, "cdp_ports")
	}

	// Persist lock_knowledgebase under capabilities.llm_config
	llmCfg, _ := caps["llm_config"].(map[string]interface{})
	if llmCfg == nil {
		llmCfg = make(map[string]interface{})
	}
	llmCfg["lock_knowledgebase"] = iwm.controller.LockKnowledgebase()
	if shape := iwm.controller.KBShape(); shape != "" {
		llmCfg["kb_shape"] = shape
	} else {
		delete(llmCfg, "kb_shape")
	}
	caps["llm_config"] = llmCfg

	manifest["capabilities"] = caps
	manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	// Write back
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to marshal workflow.json: %v", err))
		return
	}

	if err := iwm.controller.WriteWorkspaceFile(ctx, manifestPath, string(updated)); err != nil {
		logger.Warn(fmt.Sprintf("Failed to write workflow.json: %v", err))
		return
	}

	logger.Info("✅ Persisted workflow config changes to workflow.json")
}

// refreshVariablesManifest reloads the variables manifest from file into the controller.
// Call this before using iwm.controller.variablesManifest to avoid stale data.
func (iwm *InteractiveWorkshopManager) refreshVariablesManifest(ctx context.Context) {
	manifest, err := readVariablesFromFile(ctx, iwm.controller.GetWorkspacePath(), func(ctx context.Context, path string) (string, error) {
		return iwm.controller.ReadWorkspaceFile(ctx, path)
	})
	if err == nil && manifest != nil {
		iwm.controller.variablesManifest = manifest
	}
}

// workshopSubAgentNotifier implements SubAgentNotifier by registering todo task
// sub-agents into the workshop stepRegistry so query_step can find them.
type workshopSubAgentNotifier struct {
	registry *WorkshopStepRegistry
}

func (n *workshopSubAgentNotifier) OnSubAgentStart(start WorkshopExecutionStart) {
	exec := &WorkshopStepExecution{
		ID:     start.ID,
		StepID: start.Name,
		Status: WorkshopStepRunning,
		cancel: start.Cancel,
	}
	n.registry.Register(exec)
}

func (n *workshopSubAgentNotifier) OnSubAgentComplete(agentID, name, result string, err error) {
	exec := n.registry.Get(agentID)
	if exec == nil {
		return
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if err != nil {
		exec.Status = WorkshopStepFailed
		exec.Err = err
	} else {
		exec.Status = WorkshopStepDone
		exec.Result = result
	}
}

// NewInteractiveWorkshopManager creates a new InteractiveWorkshopManager
func NewInteractiveWorkshopManager(
	controller *StepBasedWorkflowOrchestrator,
	presetLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *InteractiveWorkshopManager {
	registry := newWorkshopStepRegistry()
	// Wire sub-agent notifier so todo task sub-agents appear in stepRegistry
	// and are queryable via query_step
	controller.SetSubAgentNotifier(&workshopSubAgentNotifier{registry: registry})
	return &InteractiveWorkshopManager{
		controller:         controller,
		presetLLM:          presetLLM,
		sessionID:          sessionID,
		workflowID:         workflowID,
		stepRegistry:       registry,
		pulseReviewerSlots: make(chan struct{}, pulseReviewerMaxConcurrency),
	}
}

func workflowAgentLLMConfig(agentConfig *AgentLLMConfig, defaultFallbacks []orchestrator.LLMModel, apiKeys *orchestrator.APIKeys) *orchestrator.LLMConfig {
	if agentConfig == nil || agentConfig.Provider == "" || agentConfig.ModelID == "" {
		return nil
	}
	fallbacks := convertAgentFallbacks(agentConfig.Fallbacks)
	if len(fallbacks) == 0 {
		fallbacks = defaultFallbacks
	}
	return &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{
			Provider: agentConfig.Provider,
			ModelID:  agentConfig.ModelID,
			Options:  agentConfig.Options,
		},
		Fallbacks: fallbacks,
		APIKeys:   apiKeys,
	}
}

// SetToolCallQuery configures the live tool call query capability.
// mainSessionID is the event store session ID; queryFunc queries tool calls by correlation ID.
func (iwm *InteractiveWorkshopManager) SetToolCallQuery(mainSessionID string, queryFunc ToolCallQueryFunc) {
	iwm.mainSessionID = mainSessionID
	iwm.toolCallQueryFunc = queryFunc
}

// GetToolsForWorkshopMode returns the list of tool names that should be available
// for the given workshop mode. This is used with Agent.SetToolAllowList() to dynamically
// restrict tools per-turn as the user switches modes from the frontend.
//
// Tools are grouped into categories:
//   - System tools: always included (shell, workspace, human interaction/notification, virtual tools)
//   - Workshop execution tools: execute_step, query_step, send_step_message, stop, list, run_in_background
//   - Step config/tools: update_step_config, review_step_code
//   - Plan modification tools: add/update/delete steps and routes
//   - Variable/config tools: update_variable, groups, workflow config
//   - Schedule tools: list/create/update/delete schedules
//   - Skill tools: list/search/install/uninstall skills
//   - Eval tools: validate_evaluation_plan, run_full_evaluation
func GetToolsForWorkshopMode(mode string) []string {
	// System tools — always available regardless of mode.
	// Includes workspace, shell, virtual tools, and human interaction/notification.
	system := []string{
		// Workspace advanced tools. Basic workspace file tools are intentionally
		// not in the central workspace registry; use shell/diff/image/media tools.
		"execute_shell_command", "diff_patch_workspace_file",
		"read_image", "generate_text_llm", "search_web_llm",
		"image_gen", "image_edit", "generate_video", "text_to_speech", "speech_to_text", "generate_music",
		// Secret management tools. Global secrets are read-only; workflow/user
		// encrypted stores are writable when the corresponding tools are registered.
		"list_secrets", "set_workflow_secret", "delete_workflow_secret", "set_user_secret", "delete_user_secret",
		// Human tools are appended below from virtualtools.WorkshopHumanToolNames()
		// (single source shared with registration, so the allow-list can't drift).
		// Browser (if registered)
		"agent_browser",
		// mcpagent virtual tools (get_api_spec, get_prompt, get_resource)
		"get_api_spec", "get_prompt", "get_resource",
		// Sub-agent execution tools — used by execution agents running inside steps.
		// These must always be allowed because SetToolAllowList also gates the code
		// execution registry (HTTP calls), which blocks execution agents from calling
		// sub-agents even though the restriction is intended only for the phase agent LLM.
		"call_sub_agent", "call_generic_agent", "get_sub_agent_conversation", "get_route_description",
	}
	// Human tools from the single source shared with registration (createCustomTools),
	// so the allow-list and what's actually registered cannot drift apart.
	system = append(system, virtualtools.WorkshopHumanToolNames()...)

	// Read-only info tools — safe in all modes
	readOnly := []string{
		"get_step_prompts", "get_workflow_config", "get_llm_config", "get_cost_summary",
	}

	// Workshop execution tools
	execution := []string{
		"execute_step", "query_step", "send_step_message", "stop_step", "stop_all_executions",
		"list_executions", "run_in_background",
	}

	// Step config & analysis tools
	stepConfig := []string{
		"update_step_config", "review_step_code",
	}

	// LLM config tools — inspect published/available models and save tiered LLM
	// configuration directly to workflow.json capabilities.llm_config.
	llmConfig := []string{
		"list_published_llms", "list_provider_models", "test_llm", "set_workflow_llm_config",
	}

	// Plan modification tools
	planMod := []string{
		"create_plan",
		"migrate_message_sequence_code_items",
		"add_scripted_step", "add_message_sequence_step", "add_routing_step",
		"add_human_input_step", "add_todo_task_step", "add_todo_task_route",
		"update_scripted_step", "update_message_sequence_step", "update_routing_step",
		"update_human_input_step", "update_todo_task_step", "update_todo_task_route",
		"delete_todo_task_route", "delete_plan_steps", "cleanup_orphan_step_configs",
		"update_validation_schema",
	}

	// Variable & config tools
	variableConfig := []string{
		"update_variable", "add_group", "update_group", "delete_group",
		"update_workflow_config",
	}

	// Schedule tools
	schedule := []string{
		"list_schedules", "create_schedule", "create_calendar_schedule", "update_schedule",
		"delete_schedule", "trigger_schedule", "get_schedule_runs",
	}

	// Skill tools
	skills := []string{
		"list_skills", "search_skills", "install_skill", "uninstall_skill", "import_skill",
	}

	// Eval tools
	eval := []string{
		"validate_evaluation_plan", "run_full_evaluation",
	}

	// Report tools — manage reports/report_plan.json and validate/preview against real db/ + KB sources.
	// Available in Workshop mode. Run mode may read report data
	// for answers, but does not author report_plan.json.
	report := []string{
		"get_report_plan", "upsert_report_widget", "remove_report_widget", "move_report_widget", "toggle_report_widget",
		"set_report_theme", "set_section_layout",
		"validate_report_plan", "preview_report_render",
	}

	// Auto-improvement tools. capture_context is also available in run mode so
	// users can say "remember this" while executing the workflow, with explicit
	// confirmation.
	autoImprovement := []string{
		"capture_context",
		"run_goal_advisor_review",
		"mark_cos_recommendation_status",
		"get_workflow_command_guidance", // canonical slash-command prose; see guidance package.
		"get_reference_doc",             // reference docs (system/*.md) loaded on demand; see guidance package.
	}

	// Pulse state tools are only for scheduled/manual Pulse maintenance in
	// workshop mode. Run mode can inspect outcomes, but should not mutate the
	// dynamic Pulse worklist/result state.
	pulseState := []string{
		"get_pulse_module_state",
		"record_pulse_worklist",
		"mark_pulse_module_result",
		"mark_pulse_final_command_result",
		"mark_changelog_artifact_reviewed",
	}

	var tools []string
	tools = append(tools, system...)
	tools = append(tools, readOnly...)

	// Normalize every legacy alias before selecting a tool surface. Production
	// callers normally do this earlier, but direct callers must not fall through
	// to an unintended tool set.
	if canonical := canonicalWorkshopMode(mode); canonical != "" {
		mode = canonical
	}

	switch mode {
	case "workshop":
		// WORKSHOP: full toolkit for designing,
		// running, evaluating, reviewing, and evolving a workflow. The agent
		// derives the current "phase" from workspace state (does a plan
		// exist? are there successful runs?) and uses the appropriate tools.
		// Tools that only make sense post-runs (Bug Review,
		// eval, and plan-edit tools are present here; their
		// downstream agents check evidence and refuse
		// when state isn't ready.
		tools = append(tools, execution...)
		tools = append(tools, stepConfig...)
		tools = append(tools, planMod...)
		tools = append(tools, variableConfig...)
		tools = append(tools, schedule...)
		tools = append(tools, skills...)
		tools = append(tools, llmConfig...)
		tools = append(tools, "debug_step")
		tools = append(tools, "run_full_workflow")
		tools = append(tools, "review_plan")
		tools = append(tools, "review_workflow_timing")
		tools = append(tools, "review_workflow_costs")
		tools = append(tools, eval...)
		tools = append(tools, report...)
		tools = append(tools, autoImprovement...)
		tools = append(tools, pulseState...)
		tools = append(tools, "get_reference_doc")

	case "run":
		// RUN: deployed/user-facing runtime for workflow-backed work, Slack, WhatsApp,
		// and direct operational requests. It can answer directly from workflow state,
		// run individual steps including orphan utility steps, or run the full workflow.
		// No plan changes, no optimization, and no config changes — those belong to
		// Workshop.
		// The only framework mutation allowed here is capture_context after user
		// confirmation, so context learned during a run is not lost.
		// Read-only review tools stay available for outcome inspection.
		tools = append(tools, execution...)
		tools = append(tools, "run_full_workflow")
		tools = append(tools, "debug_step")
		tools = append(tools, "review_plan")
		tools = append(tools, "review_workflow_timing")
		tools = append(tools, "review_workflow_costs")
		tools = append(tools, "get_workflow_command_guidance") // /review-* commands need this even in run mode
		tools = append(tools, "get_reference_doc")             // run-mode-allowed reference docs (tool-reference, stores, file-layout, browser).
		tools = append(tools, "capture_context")

	default:
		// Unknown mode — allow everything (no restriction)
		tools = append(tools, execution...)
		tools = append(tools, stepConfig...)
		tools = append(tools, planMod...)
		tools = append(tools, variableConfig...)
		tools = append(tools, schedule...)
		tools = append(tools, skills...)
		tools = append(tools, llmConfig...)
		tools = append(tools, eval...)
		tools = append(tools, report...)
		tools = append(tools, "debug_step")
	}

	return tools
}

func filterWorkspaceToolsByName(allTools []llmtypes.Tool, allExecutors map[string]interface{}, allowedToolNames []string) ([]llmtypes.Tool, map[string]interface{}) {
	allowed := make(map[string]bool, len(allowedToolNames))
	for _, name := range allowedToolNames {
		allowed[name] = true
	}

	var filteredTools []llmtypes.Tool
	filteredExecutors := make(map[string]interface{})

	for _, tool := range allTools {
		if tool.Function == nil {
			continue
		}
		name := tool.Function.Name
		if !allowed[name] {
			continue
		}
		filteredTools = append(filteredTools, tool)
		if exec, ok := allExecutors[name]; ok {
			filteredExecutors[name] = exec
		}
	}

	return filteredTools, filteredExecutors
}

type goalAdvisorStageAccess int

const (
	goalAdvisorStageReadOnly goalAdvisorStageAccess = iota
	goalAdvisorStageFinalizerProposal
	goalAdvisorStageFinalizerApprovedMutation
)

func goalAdvisorCommonMutationToolAgentAllowedToolNames() []string {
	return []string{
		// Workspace/file tools for evidence and bounded HTML/report updates.
		"execute_shell_command", "diff_patch_workspace_file",
		"read_image", "generate_text_llm", "search_web_llm",

		// Guidance/reference docs are mandatory for the advisor playbook.
		"get_workflow_command_guidance", "get_reference_doc",

		// Read-only workflow state.
		"get_step_prompts", "get_workflow_config", "get_llm_config", "get_cost_summary",
		"list_skills", "search_skills", "list_published_llms", "list_provider_models",

		// Non-plan workflow-facing state.
		"mark_cos_recommendation_status",

		// Report/dashboard shape plus the durable Pulse question flow.
		"get_report_plan", "upsert_report_widget", "remove_report_widget",
		"move_report_widget", "toggle_report_widget", "set_report_theme",
		"set_section_layout", "validate_report_plan", "preview_report_render",
		"create_human_input_request",
	}
}

func goalAdvisorFinalizerProposalToolAgentAllowedToolNames() []string {
	return goalAdvisorCommonMutationToolAgentAllowedToolNames()
}

func goalAdvisorFinalizerApprovedToolAgentAllowedToolNames() []string {
	tools := append([]string{}, goalAdvisorCommonMutationToolAgentAllowedToolNames()...)
	tools = append(tools,
		// Strategic plan/config/eval changes are only available when code has
		// verified an approved plan-proposal answer and a critic approve verdict.
		"create_plan",
		"add_scripted_step", "add_message_sequence_step", "add_routing_step",
		"add_human_input_step", "add_todo_task_step", "add_todo_task_route",
		"update_scripted_step", "update_message_sequence_step", "update_routing_step",
		"update_human_input_step", "update_todo_task_step", "update_todo_task_route",
		"delete_todo_task_route", "delete_plan_steps", "cleanup_orphan_step_configs",
		"update_step_config", "update_validation_schema", "validate_evaluation_plan",
		"update_variable", "add_group", "update_group", "delete_group",
		"update_workflow_config", "test_llm", "set_workflow_llm_config",
		"mark_human_input_consumed",
	)
	return tools
}

func goalAdvisorReadOnlyToolAgentAllowedToolNames() []string {
	return []string{
		// Evidence gathering only; FolderGuard is also configured read-only.
		"execute_shell_command", "read_image", "generate_text_llm", "search_web_llm",

		// Guidance/reference docs are mandatory for the advisor and critic playbooks.
		"get_workflow_command_guidance", "get_reference_doc",

		// Read-only workflow state and report inspection.
		"get_step_prompts", "get_workflow_config", "get_llm_config", "get_cost_summary",
		"list_skills", "search_skills", "list_published_llms", "list_provider_models",
		"get_report_plan", "validate_report_plan", "preview_report_render",
	}
}

func (iwm *InteractiveWorkshopManager) registerWorkshopMutationToolsForToolAgent(agent agents.OrchestratorAgent, workspacePath, agentName string, allowedToolNames []string, logger loggerv2.Logger) {
	if agent == nil || agent.GetBaseAgent() == nil || agent.GetBaseAgent().Agent() == nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: cannot register workshop mutation tools; base agent unavailable", agentName))
		return
	}
	mcpAgentRef := agent.GetBaseAgent().Agent()
	if err := RegisterPlanModificationTools(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
		iwm.controller.WriteWorkspaceFile,
		iwm.controller.MoveWorkspaceFile,
		agentName,
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: failed to register plan modification tools: %v", agentName, err))
	}
	registerInteractiveWorkshopTools(iwm, mcpAgentRef, logger)
	guidance.RegisterGuidanceTool(mcpAgentRef, "workshop", logger)
	guidance.RegisterReferenceDocTool(mcpAgentRef, "workshop", logger)
	if err := RegisterChiefOfStaffRecommendationStatusTool(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
		iwm.controller.WriteWorkspaceFile,
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: failed to register Chief of Staff recommendation status tool: %v", agentName, err))
	}
	if err := RegisterEvaluationValidationTools(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
		iwm.controller.WriteWorkspaceFile,
		iwm.controller.MoveWorkspaceFile,
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: failed to register evaluation validation tool: %v", agentName, err))
	}
	if err := RegisterReportPlanManagementTools(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
		iwm.controller.WriteWorkspaceFile,
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: failed to register report plan management tools: %v", agentName, err))
	}
	if err := RegisterReportPlanValidationTools(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: failed to register report validation tool: %v", agentName, err))
	}
	if err := RegisterReportRenderPreviewTool(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ %s: failed to register report preview tool: %v", agentName, err))
	}
	mcpAgentRef.SetToolAllowList(allowedToolNames)
	logger.Info(fmt.Sprintf("🔧 %s: registered workshop mutation tools and applied allow list (%d tools)", agentName, len(allowedToolNames)))
}

func (iwm *InteractiveWorkshopManager) registerMarkChangelogArtifactReviewedTool(mcpAgent *mcpagent.Agent, workspacePath string, logger loggerv2.Logger) error {
	if mcpAgent == nil {
		return fmt.Errorf("nil mcp agent")
	}
	return mcpAgent.RegisterCustomTool(
		"mark_changelog_artifact_reviewed",
		"Mark planning/changelog entries as fully inspected after the parent Pulse/workshop agent has recorded the Artifact Review in builder/improve.html. Generic read-only reviewers must only propose exact marks and must not call this tool. This is the only supported way to set artifact_review.done=true; do not edit changelog JSON directly.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"marks": map[string]interface{}{
					"type":        "array",
					"description": "Changelog files and zero-based entry indexes to mark reviewed.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"file": map[string]interface{}{
								"type":        "string",
								"description": "The changelog filename, e.g. changelog-2026-07-02-06-12-46.json. A planning/changelog/... path is accepted; only the basename is used.",
							},
							"entry_indexes": map[string]interface{}{
								"type":        "array",
								"description": "Zero-based indexes in the file's entries array that were fully inspected or cursor-backfilled.",
								"items":       map[string]interface{}{"type": "integer"},
							},
						},
						"required": []string{"file", "entry_indexes"},
					},
				},
				"result": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"clean", "findings", "cursor-backfill"},
					"description": "Review result to stamp on all marked entries.",
				},
				"report_entry_id": map[string]interface{}{
					"type":        "string",
					"description": "ID/heading slug of the builder/improve.html Artifact Review entry that records this review.",
				},
				"reviewed_at": map[string]interface{}{
					"type":        "string",
					"description": "Optional RFC3339 UTC timestamp. Defaults to now.",
				},
			},
			"required": []string{"marks", "result"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return iwm.markChangelogArtifactReviewed(ctx, workspacePath, args, logger)
		},
		"workflow",
	)
}

// registerWorkshopAgentTools is the single registration path for tools that are
// available only to a workshop/Pulse agent. Keep both the exported CLI wrapper
// and the legacy in-process workshop executor on this path so their actual tool
// registries cannot drift from one another.
func registerWorkshopAgentTools(iwm *InteractiveWorkshopManager, mcpAgent *mcpagent.Agent, workspacePath string, logger loggerv2.Logger) {
	registerInteractiveWorkshopTools(iwm, mcpAgent, logger)
	if err := iwm.registerMarkChangelogArtifactReviewedTool(mcpAgent, workspacePath, logger); err != nil {
		logger.Warn(fmt.Sprintf("Failed to register changelog artifact-review marker tool: %v", err))
	}
}

func (iwm *InteractiveWorkshopManager) markChangelogArtifactReviewed(ctx context.Context, workspacePath string, args map[string]interface{}, logger loggerv2.Logger) (string, error) {
	rawMarks, ok := args["marks"].([]interface{})
	if !ok || len(rawMarks) == 0 {
		return "marks is required", nil
	}
	result := strings.TrimSpace(asString(args["result"]))
	switch result {
	case "clean", "findings", "cursor-backfill":
	default:
		return `result must be one of "clean", "findings", or "cursor-backfill"`, nil
	}
	reviewedAt := strings.TrimSpace(asString(args["reviewed_at"]))
	if reviewedAt == "" {
		reviewedAt = time.Now().UTC().Format(time.RFC3339)
	}
	reportEntryID := strings.TrimSpace(asString(args["report_entry_id"]))

	marksByFile := map[string]map[int]bool{}
	for _, raw := range rawMarks {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return "each marks item must be an object", nil
		}
		file := filepath.Base(strings.TrimSpace(asString(obj["file"])))
		if file == "." || file == "" || !strings.HasPrefix(file, "changelog-") || !strings.HasSuffix(strings.ToLower(file), ".json") {
			return fmt.Sprintf("invalid changelog file %q", asString(obj["file"])), nil
		}
		rawIndexes, ok := obj["entry_indexes"].([]interface{})
		if !ok || len(rawIndexes) == 0 {
			return fmt.Sprintf("entry_indexes is required for %s", file), nil
		}
		if marksByFile[file] == nil {
			marksByFile[file] = map[int]bool{}
		}
		for _, rawIndex := range rawIndexes {
			var idx int
			switch v := rawIndex.(type) {
			case float64:
				idx = int(v)
				if float64(idx) != v {
					return fmt.Sprintf("entry index %v for %s is not an integer", rawIndex, file), nil
				}
			case int:
				idx = v
			default:
				return fmt.Sprintf("entry index %v for %s is not an integer", rawIndex, file), nil
			}
			if idx < 0 {
				return fmt.Sprintf("entry index %d for %s is negative", idx, file), nil
			}
			marksByFile[file][idx] = true
		}
	}

	totalMarked := 0
	touchedFiles := make([]string, 0, len(marksByFile))
	for file, indexSet := range marksByFile {
		changelogPath := strings.Trim(strings.TrimSpace(workspacePath), "/") + "/planning/changelog/" + file
		if strings.HasPrefix(changelogPath, "/") {
			changelogPath = strings.TrimPrefix(changelogPath, "/")
		}
		content, err := iwm.controller.ReadWorkspaceFile(ctx, changelogPath)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", changelogPath, err)
		}
		var changelog PlanChangelog
		if err := json.Unmarshal([]byte(content), &changelog); err != nil {
			return "", fmt.Errorf("parse %s: %w", changelogPath, err)
		}
		for idx := range indexSet {
			if idx >= len(changelog.Entries) {
				return fmt.Sprintf("entry index %d out of range for %s (entries=%d)", idx, file, len(changelog.Entries)), nil
			}
		}
		for idx := range indexSet {
			changelog.Entries[idx].ArtifactReview = &PlanChangelogArtifactReview{
				Done:          true,
				ReviewedAt:    reviewedAt,
				ReviewedBy:    "pulse_fixer",
				Result:        result,
				ReportEntryID: reportEntryID,
			}
			totalMarked++
		}
		data, err := json.MarshalIndent(changelog, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal %s: %w", changelogPath, err)
		}
		if err := iwm.controller.writeManagedPlanningFile(ctx, filepath.Join("changelog", file), string(data)); err != nil {
			return "", fmt.Errorf("write %s: %w", changelogPath, err)
		}
		touchedFiles = append(touchedFiles, file)
		logger.Info(fmt.Sprintf("📝 Artifact Review: marked %d changelog entries reviewed in %s", len(indexSet), file))
	}
	sort.Strings(touchedFiles)
	entryWord := "entry"
	if totalMarked != 1 {
		entryWord = "entries"
	}
	return fmt.Sprintf("Marked %d changelog %s artifact_review.done=true across %d file(s): %s", totalMarked, entryWord, len(touchedFiles), strings.Join(touchedFiles, ", ")), nil
}

// canonicalWorkshopMode keeps this package aligned with server-side chat-history
// normalization. Legacy editable-mode names all mean the unified Workshop mode.
func canonicalWorkshopMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "workshop", "builder", "optimizer", "reporting", "eval", "output":
		return "workshop"
	case "run", "ask", "debugger", "runner":
		return "run"
	default:
		return ""
	}
}

// detectWorkshopMode returns the editable Workshop default when the frontend
// has not provided an explicit Run override.
func detectWorkshopMode(plan *PlanningResponse, stepConfigs []StepConfig) string {
	return "workshop"
}

func (iwm *InteractiveWorkshopManager) currentWorkshopModeFromConfigs(stepConfigs []StepConfig) string {
	if iwm == nil {
		return "workshop"
	}
	if mode := canonicalWorkshopMode(iwm.workshopModeOverride); mode != "" {
		return mode
	}
	mode := detectWorkshopMode(iwm.controller.approvedPlan, stepConfigs)
	return mode
}

// newExecContext creates a child context for a step execution goroutine.
// Returns (nil, errSessionStopped) if the session was already canceled (user pressed stop),
// preventing orphaned CLI processes from being spawned. All step execution tool handlers
// MUST use this instead of calling context.WithCancel(iwm.sessionCtx) directly.
//
// Background: When the user stops a session, WorkshopChatSession.Close() cancels
// iwm.sessionCtx. But step execution goroutines may already be queued or in-flight.
// Without this check, they would create a derived context from a canceled parent,
// spawn a CLI process, and that process would either fail immediately or, in edge
// cases, run indefinitely if the cancel signal is not delivered quickly.
func (iwm *InteractiveWorkshopManager) newExecContext() (context.Context, context.CancelFunc, error) {
	if iwm.sessionCtx.Err() != nil {
		return nil, nil, fmt.Errorf("session was stopped")
	}
	ctx, cancel := context.WithCancel(iwm.sessionCtx)
	return ctx, cancel, nil
}

// InteractiveWorkshopOnly runs the interactive workshop phase
func (iwm *InteractiveWorkshopManager) InteractiveWorkshopOnly(ctx context.Context, workspacePath string, runFolder string) (string, error) {
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Starting Workflow Builder for workspace: %s", workspacePath))

	// Store session context so background goroutines outlive individual tool call contexts
	iwm.sessionCtx = ctx

	// Set workspace path
	iwm.controller.SetWorkspacePath(workspacePath)

	// Set shell working directory to workspace root so the workshop agent can use
	// relative paths in shell commands (e.g., `cat variables/variables.json` instead
	// of `cd 'Workflow/...' && cat variables/variables.json`).
	if iwm.controller.httpSessionID != "" {
		common.SetSessionWorkingDir(iwm.controller.httpSessionID, workspacePath)
		iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 [workshop] Set shell CWD to workspace: %s", workspacePath))
	}

	// Use the run folder passed from the frontend toolbar selection (if any).
	// Workshop mode is pinned to iteration-0, so normalize any incoming selection.
	// If empty, leave selectedRunFolder unset outside Workshop mode.
	if canonicalWorkshopMode(iwm.workshopModeOverride) == "workshop" {
		runFolder = normalizeWorkshopBuilderRunFolder(runFolder)
	}
	if runFolder != "" {
		iwm.controller.selectedRunFolder = runFolder
		iwm.controller.GetLogger().Info(fmt.Sprintf("📁 Using provided run folder: %s", runFolder))
	}

	// Brand-new workflows may not have plan/store scaffolding yet. Seed the minimal
	// builder workspace so the workshop can open cleanly on the first turn.
	if err := iwm.ensureWorkshopBootstrapFilesExist(ctx); err != nil {
		return "", fmt.Errorf("cannot initialize workshop bootstrap files: %w", err)
	}

	// Load plan — fail early if no plan exists
	if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
		return "", fmt.Errorf("cannot start workshop: %w", err)
	}

	// Read plan JSON for system prompt
	planContent, err := iwm.controller.ReadWorkspaceFile(ctx, "planning/plan.json")
	if err != nil {
		planContent = "{}"
	}

	// Read step config summary for system prompt
	stepConfigSummary := ""
	stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
	if len(stepConfigs) > 0 {
		stepConfigSummary = fmt.Sprintf("%d step configs loaded", len(stepConfigs))
	}

	// Read progress summary for system prompt
	progressSummary := ""
	if progress, err := iwm.controller.loadStepProgress(ctx); err == nil && progress != nil {
		progressSummary = fmt.Sprintf("Completed steps: %v", progress.CompletedStepIndices)
	}

	// Default user goal — in chat mode the user provides goals via conversation messages
	userGoal := "Help me build, run, and optimize the workflow steps."

	// Default workshop mode; an explicit frontend override may select Run.
	workshopMode := detectWorkshopMode(iwm.controller.approvedPlan, stepConfigs)
	iwm.controller.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Default mode: %s", workshopMode))
	// Apply frontend override if set
	if mode := canonicalWorkshopMode(iwm.workshopModeOverride); mode != "" {
		workshopMode = mode
		iwm.controller.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Frontend override applied: %s", workshopMode))
	}

	// Create workshop agent
	agent, err := iwm.createInteractiveWorkshopAgent(ctx, workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to create workshop agent: %w", err)
	}

	// Prepare template vars
	useKB := "false"
	if iwm.controller.UseKnowledgebase() {
		useKB = "true"
	}
	kbShape := workflowtypes.ResolveKBShape(iwm.controller.KBShape())
	// Objective and success_criteria are resolved from soul/soul.md (canonical)
	// with workflow.json as legacy fallback — see ResolveWorkflowObjective.
	workflowObjective, workflowSuccessCriteria := iwm.controller.ResolveWorkflowObjective(ctx)
	availableGroups := ""
	if iwm.controller.variablesManifest != nil && len(iwm.controller.variablesManifest.Groups) > 0 {
		var groupNames []string
		for _, g := range iwm.controller.variablesManifest.Groups {
			groupNames = append(groupNames, g.Name)
		}
		availableGroups = strings.Join(groupNames, ", ")
	}

	workspaceToolsInstructions := instructions.GetSpecialWorkspaceToolsInstructions()
	if isCodingCLIConfig(agent.GetConfig()) {
		workspaceToolsInstructions = instructions.GetSpecialWorkspaceToolsPointer()
	}

	templateVars := map[string]string{
		"WorkspacePath":                     workspacePath,
		"RunFolder":                         iwm.controller.selectedRunFolder,
		"PlanJSON":                          planContent,
		"StepConfigSummary":                 stepConfigSummary,
		"IsCodeExecutionMode":               fmt.Sprintf("%v", agent.GetConfig().UseCodeExecutionMode),
		"UseProjectedReferenceSkills":       fmt.Sprintf("%v", isCodingCLIConfig(agent.GetConfig())),
		"WorkshopMode":                      workshopMode,
		"ProgressSummary":                   progressSummary,
		"UserRequest":                       userGoal,
		"SessionID":                         iwm.sessionID,
		"WorkflowID":                        iwm.workflowID,
		"UseKnowledgebase":                  useKB,
		"KBShape":                           kbShape,
		"WorkflowObjective":                 workflowObjective,
		"WorkflowSuccessCriteria":           workflowSuccessCriteria,
		"AvailableGroups":                   availableGroups,
		"AbsWorkspacePath":                  absPromptWorkspacePath(workspacePath),
		"AbsDocsRoot":                       GetPromptDocsRoot(),
		"SpecialWorkspaceToolsInstructions": workspaceToolsInstructions,
	}

	// Execute workshop agent via OrchestratorAgent interface
	// Dispatches to WorkflowInteractiveWorkshopAgent.Execute (registered via createAgentFunc)
	iwm.controller.GetLogger().Info("🔧 Executing Workflow Builder agent...")
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("workshop agent execution failed: %w", err)
	}

	return result, nil
}

// createInteractiveWorkshopAgent creates the workshop agent following the createExecutionDebuggerAgent pattern
// workshopWritePaths returns the explicit per-subfolder write allow-list shared
// by the workshop main agent and every workshop sub-agent (review agents,
// maintenance background tasks). Keeping writes confined to
// these subfolders means a builder can't `cat > planning/plan.json` via shell
// or `diff_patch_workspace_file` and bypass the plan-mod tools that validate
// schemas and emit events. Workspace-root config files (workflow.json,
// mcp_config.json) are mutated through dedicated tools that go via the
// workspace API and bypass this sandbox altogether, so they don't appear here.
func workshopWritePaths(workspacePath string) []string {
	return []string{
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/knowledgebase", workspacePath),
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/evaluation", workspacePath),
		fmt.Sprintf("%s/reports", workspacePath),
		fmt.Sprintf("%s/db", workspacePath),
		fmt.Sprintf("%s/memory", workspacePath),
		fmt.Sprintf("%s/execution", workspacePath),
		fmt.Sprintf("%s/variables", workspacePath),
		fmt.Sprintf("%s/builder", workspacePath), // improve.html and archives
	}
}

func (iwm *InteractiveWorkshopManager) setupWorkshopToolAgentSession(agentKind string, readPaths []string, writePaths []string) string {
	sessionID := fmt.Sprintf("workshop-%s-%d", agentKind, time.Now().UnixNano())
	workspacePath := strings.TrimSpace(iwm.controller.GetWorkspacePath())

	common.SetSessionFolderGuard(sessionID, readPaths, writePaths)
	iwm.controller.grantSessionCDPHostDownloadsReadOnly(sessionID)
	if workspacePath != "" {
		common.SetSessionWorkingDir(sessionID, workspacePath)
	}

	iwm.controller.GetLogger().Info(fmt.Sprintf(
		"🔒 Workshop tool-agent session %q (%s) — cwd=%q Read=%v Write=%v",
		sessionID, agentKind, workspacePath, readPaths, writePaths,
	))
	return sessionID
}

func (iwm *InteractiveWorkshopManager) configureWorkshopToolAgentSession(config *agents.OrchestratorAgentConfig, agentKind string, readPaths []string, writePaths []string) func() {
	toolAgentSessionID := iwm.setupWorkshopToolAgentSession(agentKind, readPaths, writePaths)
	config.MCPSessionID = toolAgentSessionID
	config.FolderGuardReadPaths = readPaths
	config.FolderGuardWritePaths = writePaths
	return func() {
		common.ClearSessionShellConfig(toolAgentSessionID)
	}
}

func absPromptWorkspacePath(workspacePath string) string {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return ""
	}
	return filepath.Join(GetPromptDocsRoot(), workspacePath)
}

func (iwm *InteractiveWorkshopManager) createInteractiveWorkshopAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Folder guard paths
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	knowledgebasePath := getKnowledgebasePath(workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	planningPath := fmt.Sprintf("%s/planning", workspacePath)

	readPaths := []string{
		workspacePath,
		runsPath,
		knowledgebasePath,
		learningsPath,
		planningPath,
		"Chats", // Allow reading chat history for context
	}
	writePaths := workshopWritePaths(workspacePath)

	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Workshop folder guard - Read: %v, Write: %v", readPaths, writePaths))

	// LLM config
	if iwm.presetLLM == nil || iwm.presetLLM.Provider == "" || iwm.presetLLM.ModelID == "" {
		return nil, fmt.Errorf("no valid LLM configuration found for workflow builder agent")
	}
	llmConfigToUse := workflowAgentLLMConfig(iwm.presetLLM, iwm.controller.GetFallbacks(), iwm.controller.GetAPIKeys())
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Workshop agent LLM: %s/%s", iwm.presetLLM.Provider, iwm.presetLLM.ModelID))

	// Agent config
	config := iwm.controller.CreateStandardAgentConfigWithLLM("workflow-builder-agent", 100, agents.OutputFormatStructured, llmConfigToUse)
	forceWorkflowClaudeCodeInteractiveTransport(config)
	enableWorkflowMainCodingAgentKeepAlive(config)
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)

	// MCP Servers — use preset if available, else NoServers
	selectedServers := iwm.controller.GetSelectedServers()
	selectedTools := iwm.controller.GetSelectedTools()
	mcpConfigPath := iwm.controller.GetMCPConfigPath()

	if len(selectedServers) > 0 && mcpConfigPath != "" {
		config.ServerNames = selectedServers
		config.SelectedTools = selectedTools
		config.MCPConfigPath = mcpConfigPath
		config.MCPSessionID = iwm.controller.GetMCPSessionID()
	} else {
		config.ServerNames = []string{mcpclient.NoServers}
	}

	// Phase tools: shell_command + human_feedback
	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()

	// createAgentFunc captures iwm for use in Execute
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowInteractiveWorkshopAgent(cfg, logger, tracer, eventBridge, iwm)
	}

	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"workflow-builder",
		0, 0,
		"workflow-builder",
		createAgentFunc,
		phaseTools,
		phaseExecutors,
		true,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func enableWorkflowMainCodingAgentKeepAlive(config *agents.OrchestratorAgentConfig) {
	if config == nil || !common.IsCLIProvider(config.LLMConfig.Primary.Provider) {
		return
	}
	config.CodingAgentKeepAlive = true
}

// ============================================================================
// WorkflowInteractiveWorkshopAgent
// ============================================================================

// WorkflowInteractiveWorkshopAgent is the interactive workshop phase agent
type WorkflowInteractiveWorkshopAgent struct {
	*agents.BaseOrchestratorAgent
	iwm *InteractiveWorkshopManager
}

// newWorkflowInteractiveWorkshopAgent creates a new WorkflowInteractiveWorkshopAgent
func newWorkflowInteractiveWorkshopAgent(
	config *agents.OrchestratorAgentConfig,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	iwm *InteractiveWorkshopManager,
) *WorkflowInteractiveWorkshopAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config, logger, tracer,
		agents.TodoPlannerInteractiveWorkshopAgentType,
		eventBridge,
	)
	return &WorkflowInteractiveWorkshopAgent{
		BaseOrchestratorAgent: baseAgent,
		iwm:                   iwm,
	}
}

// interactiveWorkshopSystemTemplate is the system prompt for the workshop agent
var interactiveWorkshopSystemTemplate = MustRegisterTemplate("interactiveWorkshopSystem", `# Workflow Builder Agent

You are the intelligent orchestrator of an automated workflow system. Workflow steps are executed by smaller, cheaper LLM agents that follow instructions narrowly. Your role — running on a more capable model — is to design the workflow, run and monitor steps, diagnose failures, and encode what you learn into step instructions and learnings so the execution agents can reliably succeed. Think of yourself as the senior engineer; the step agents are junior engineers who need clear, specific guidance.

## Talking to the user — keep it short and non-technical

The person you talk to is almost always a **business owner / operator, not a developer.** Be the engineer internally, but talk to them like a helpful colleague, not a console:
- **Short replies.** A sentence or two by default. Lead with the outcome ("Done — your report now shows X" / "That failed because Y; I fixed it"). Don't narrate every tool call or step.
- **Plain language, no jargon.** Avoid file paths, code, SQL, schema, tool names, and internal mechanics (`+"`db.sqlite`"+`, FolderGuard, `+"`$DB_PATH`"+`, sandbox, JSON, etc.) unless the user is clearly technical or explicitly asks. Say "the data" not "`+"`db/db.sqlite`"+`"; "the report" not "`+"`report_plan.json`"+`".
- **Explain in business terms** — what changed and what it means for their workflow/results, not how the plumbing works.
- **Ask simple questions.** When you need a decision, ask one plain question with the trade-off in business terms; don't surface technical options.
- Keep the detail and precision **in the artifacts you build** (step descriptions, code, schemas) — that's where rigor belongs. The chat stays simple. Go technical in chat only when the user does, or when you must confirm a concrete change before applying it.

**Before doing anything else, read `+"`soul/soul.md`"+`.** This is the canonical source of truth for stable intent: objective, success criteria, and only explicit user-approved durable constraints. Ground every decision — design, run, debug, repair, or report — in that intent. Architecture, current step design, provider/tool/model choices, historical decisions, references, and agent-inferred assumptions are revisable and must not be written into soul.md. If the file is missing or empty, ask the user what the workflow is for before proceeding.

## CURRENT MODE: {{if eq .WorkshopMode "workshop"}}WORKSHOP{{else}}RUN{{end}}

{{if eq .WorkshopMode "workshop"}}
**First, determine the current phase from workspace state.** Read `+"`planning/plan.json`"+` (does a plan exist?) and `+"`runs/`"+` (any successful runs?) and choose your default behavior accordingly:

- **No plan / incomplete plan** → DESIGN phase. Talk through the workflow before adding steps. Use plan modification tools to build it out step by step. Set new steps to `+"`agentic`"+`. Do not run Pulse maintenance or eval tools — these need run evidence to be meaningful.
- **Plan exists, no successful runs yet** → STABILIZE phase. Use `+"`execute_step`"+` and `+"`run_full_workflow`"+` to find and fix problems. Update step descriptions, validation, config. Broad strategy changes still do not apply yet — there is no evidence to base them on.
- **Plan + successful runs** → REVIEW / IMPROVE phase. Use Pulse Bug Review, read-only improve reviewers, the parent fixer, eval/report tools, and normal plan modification tools. Use evidence from `+"`runs/`"+` and `+"`evaluation/`"+` to drive decisions. Strategy changes require the existing approval-card flow unless the user explicitly requests a bounded manual edit.

Until you have checked, do not assume the workflow needs repair or fresh design.
{{end}}
{{.SpecialWorkspaceToolsInstructions}}

## Execution policy

**Default to sequential per-group execution** for multi-group `+"`run_full_workflow`"+` calls: pass `+"`group_name=\"<single-group>\"`"+` and wait for each group to finish before starting the next. Only run groups in parallel when the user explicitly says so. If the user is ambiguous ("run the workflow"), default sequential and tell them so. For the full rationale (cleaner failure signal, fixes propagate forward, resource contention, iteration rotation), the loop recipe, and the exceptions where parallel is appropriate: `+"`get_reference_doc(kind=\"execution-policy\")`"+`.

{{if or (eq .WorkshopMode "run") (eq .WorkshopMode "workshop")}}
## Deployed channel runtime

Users may reach this workflow through Slack, WhatsApp, or another bot channel. Treat the routed message as a runtime request by default; identify the group from message context, ground in `+"`soul.md`"+` / `+"`learnings/_global/SKILL.md`"+` / KB / db before running. Use direct-answer (small ops), `+"`run_full_workflow(group_name=..., human_inputs=...)`"+` (normal path), or `+"`execute_step`"+` (targeted). Summarize final artifacts in plain language, not file paths. Don't reinterpret operational questions as design requests. For the full handling pattern (group inference rules, channel-context plumbing, Run vs Workshop boundary on failures): `+"`get_reference_doc(kind=\"deployed-channel\")`"+`.

{{end}}

## Reporting

The workflow has a **live frontend report viewer** at the top toolbar's "Report" tab. It reads `+"`reports/report_plan.json`"+` and renders the **HTML document(s)** registered there — each an HTML file under `+"`db/reports/`"+`. It may also render native `+"`interaction`"+` widgets only when the user explicitly configures a durable question/control in the Report page. HTML reads `+"`db/db.sqlite`"+` live via the `+"`window.report`"+` API; interaction answers are stored in the same workflow DB table `+"`report_widget_responses`"+` for later runs. **No separate "generate report" phase** — author the document/widget definition **once** and it remains live.

{{if eq .WorkshopMode "workshop"}}**Workshop owns `+"`reports/report_plan.json`"+`** — author HTML documents with `+"`upsert_report_widget(kind=\"file\", renderFormat=\"html\")`"+`. When the user explicitly asks for a persistent report-page input, add a native `+"`interaction`"+` widget with a stable widget id, question, responseKind, options, and optional subject/version/hash; do not create it automatically from Pulse findings. Also configure the intended workflow consumer step to query the framework-owned `+"`report_widget_responses`"+` rows through `+"`$DB_PATH`"+`. Keep report edits presentation-only unless the user also asked for workflow behavior changes. HTML reads `+"`db/db.sqlite`"+` live via `+"`window.report.query(sql)`"+`; author it once and never regenerate it per run. For the full policy: `+"`get_reference_doc(kind=\"reporting-policy\")`"+`.
{{else}}**Run mode does not author reports.** If the user asks to create/edit the report, themes, tabs, or `+"`reports/report_plan.json`"+`, tell them to switch to Workshop. Do not edit `+"`reports/report_plan.json`"+` via shell from Run mode. For policy details: `+"`get_reference_doc(kind=\"reporting-policy\")`"+`.
{{end}}

	{{if eq .WorkshopMode "run"}}
	## Context Capture — Allowed In Run Mode

	Run mode can execute workflow-backed work directly, run individual/orphan steps, run the full workflow, and inspect results. It may read KB/learnings/db/report/run artifacts whenever they are needed to answer correctly, but it does not edit plan/config/eval/report artifacts. One exception is durable user-owned runtime context. If the user says something that future workflow runs should remember — rules, preferences, constraints, ICP filters, approval rules, brand voice, examples, or domain assumptions — ask whether to capture it.

	If confirmed, call `+"`capture_context`"+` with a concise `+"`context_text`"+` and a section name. Do not manually edit `+"`knowledgebase/context/context.md`"+`; the tool writes that file for you.
	{{end}}

{{if eq .WorkshopMode "workshop"}}
### Report plan — reports/report_plan.json (brief)

You may maintain the live frontend report (`+"`reports/report_plan.json`"+`) so it stays aligned with current outputs, metrics, and evaluation evidence. Use report-plan tools for report edits; use workshop tools only when the underlying workflow behavior or eval coverage actually needs to change.

**Core toolchain:** `+"`get_report_plan`"+` (read IDs) → author/register HTML with `+"`upsert_report_widget(kind=\"file\", renderFormat=\"html\")`"+`, or add an explicitly requested native `+"`interaction`"+` widget → `+"`move_report_widget`"+` / `+"`toggle_report_widget`"+` / `+"`remove_report_widget`"+` → `+"`validate_report_plan`"+` after every edit → `+"`preview_report_render`"+`. HTML reads the DB via `+"`window.report.query(sql)`"+`; workflow steps read configured interaction answers from `+"`report_widget_responses`"+` through `+"`$DB_PATH`"+`.

**For the full toolchain (the two formats, `+"`window.report`"+` API, tabs, per-report themes, the good-document + design-quality guide, missing-data triage, full workflow), call:**
`+"`get_reference_doc(kind=\"report-plan\")`"+` — load before authoring or editing `+"`reports/report_plan.json`"+`.
{{end}}

{{if eq .WorkshopMode "workshop"}}
### Evaluation plan — evaluation/evaluation_plan.json (brief)

Workshop owns the eval plan: write it, validate it, run it against `+"`iteration-0`"+`, and keep it aligned as the workflow evolves. Each eval step needs `+"`id`"+` + `+"`title`"+` + `+"`description`"+`; eval step IDs must NOT collide with execution-plan step IDs (both share `+"`learnings/{stepID}/`"+`). **Evals measure GOAL achievement against `+"`soul.md`"+` success criteria — one eval step per criterion; Pulse Gate/Bug Review and `+"`pre_validation`"+` own operational checks (file-exists/format/step-ran), so never duplicate those in eval.** Compute facts in code, judge the verdict against the criterion: fully scripted only for contract-anchored mechanical checks, agentic with a frozen rubric for subjective quality. Eval runs after every execution, so keep it cheap: few steps, low tiers for extraction, route gating. A good eval must catch fake, placeholder, missing, or unverified data and score it 0 after checking the real source. **Do not equate empty with failed:** when trustworthy source evidence proves a legitimate zero-cardinality business state, score its semantic correctness against the criterion instead of deducting points merely because a list is empty. Every rubric must state what evidence distinguishes a valid zero from missing collection. Ground scoring in the real run via `+"`{{\"{{TARGET_RUN_PATH}}\"}}`"+` + `+"`db/db.sqlite`"+`. After every edit, call `+"`validate_evaluation_plan`"+`; to test, call `+"`run_full_evaluation(group_name=\"...\")`"+` (always targets `+"`iteration-0`"+`).

Files: plan at `+"`evaluation/evaluation_plan.json`"+`, per-step config at `+"`evaluation/step_config.json`"+`, eval runs/reports at `+"`evaluation/runs/iteration-0[/group]/`"+`.

**For the full contract (route gating with `+"`applies_to_routes`"+`, `+"`pre_validation`"+` rules, `+"`"+`{{"{{TARGET_RUN_PATH}}"}}`+"`"+` placeholder, declared_execution_mode + execution_tier rules, when-to-update triggers, full workflow), call:**
`+"`get_reference_doc(kind=\"evaluation-plan\")`"+` — load before editing `+"`evaluation/evaluation_plan.json`"+` or `+"`evaluation/step_config.json`"+`.
{{end}}

{{if eq .WorkshopMode "run"}}
## Workflow data surfaces — runtime use in this mode

The workflow may use three persistent stores. In Run mode, read them when they help execute, answer, or present results; do not redesign their schemas or manually rewrite their durable content.

- **learnings/_global/SKILL.md**: execution know-how for step agents and Run mode itself. Read it before doing workflow-specific operational work. Do not edit it here.
- **knowledgebase/context/**: user-supplied runtime business context. Read it if it helps execute the request or answer the user's question.
- **knowledgebase/notes/**: durable narrative observations the workflow has accumulated. Read them if they help execute the request or answer the user's question.
- **db/db.sqlite + db/assets/**: persistent workflow result data (SQLite tables) and durable media/file assets. HTML reports query tables live via `+"`window.report.query`"+` (and reference assets), and Run mode summaries should translate db rows into plain English.

If the user wants to change what gets stored, how db files are shaped, or how KB/learnings are written, switch to Workshop.
{{else}}
## Three persistent stores

Each workflow has three separate stores that survive across runs: `+"`learnings/_global/SKILL.md`"+` (HOW to run the task — selectors, API quirks, timing), `+"`knowledgebase/`"+` (business context + per-topic narrative notes), `+"`db/db.sqlite`"+` (workflow output state in SQLite tables — the only place HTML reports read live data from, via `+"`window.report.query`"+`). Hard rule: declare every table's PRIMARY KEY + upsert rule in `+"`db/README.md`"+` BEFORE writing. KB and per-step learning writes are opt-in via step config. For the full design contract (write rules, decision tree, schema discipline, opt-in questions, run-time grounding): `+"`get_reference_doc(kind=\"stores\")`"+` — load before designing or repairing any step that writes to db/, KB, or learnings.
{{end}}


{{if eq .WorkshopMode "workshop"}}
{{end}}

{{if eq .WorkshopMode "workshop"}}
**WORKSHOP MODE** — Design, run, evaluate, repair, and evolve the plan as a single mode. The agent decides the right action from workspace state (see the phase-detection directive near the top of this prompt). Make existing steps reliable across all groups and runs; build new steps when the plan needs extending.

**Foundation check:** verify `+"`soul/soul.md`"+` has both `+"`## Objective`"+` and `+"`## Success Criteria`"+` sections. If either is missing, ask the user and write via shell. Keep soul.md limited to stable intent; the current architecture and implementation belong in plan/config and remain open to improvement. `+"`planning/plan.json`"+` no longer stores root objective/success fields.

**Read previous builder conversations** from `+"`builder/`"+` folder (`+"`ls -t builder/*.json | head -3`"+`) to avoid repeating failed approaches.

**Core loop:** run → eval → classify → review → fix → verify. Treat Bug Review, approved plan change/proposal, eval improvement, and no-action/blocker as peer outcomes. Load `+"`get_reference_doc(kind=\"post-run-monitor\")`"+` for the parallel read-only reviewer and single Pulse Fixer contract.
{{else}}
**RUN MODE** — You're chatting with a workflow that's already been built and tuned. Most of the time you'll be running it and answering questions about results, often over WhatsApp / Slack / a phone screen rather than a desktop terminal.

### Primary job
Run mode is the user-facing runtime surface, including Slack and WhatsApp routes. It can do the work itself when the request is small, run one step, run an orphan utility step, or run the full workflow. Optimize for these five jobs:

1. **Do direct runtime work** when no workflow run is needed: use available tools plus workflow context to answer, look up, analyze, summarize, or take a small operational action. Before acting, ground in the generated skill, KB, and db state: `+"`learnings/_global/SKILL.md`"+` for HOW to operate, `+"`knowledgebase/context/`"+` and targeted `+"`knowledgebase/notes/`"+` for business context, and `+"`db/`"+` plus `+"`db/README.md`"+` for durable facts/results.
2. **Run the workflow** for one configured group at a time with `+"`run_full_workflow(group_name=\"...\")`"+`.
3. **Run a specific step or orphan utility step** with `+"`execute_step(step_id=\"...\", group_name=\"...\")`"+` when the user asks for a targeted action, retry, data check, or one-off investigation.
4. **Answer user questions** from current workflow state, latest run outputs, `+"`db/db.sqlite`"+` (query with sqlite3), `+"`db/assets/`"+` references, report data, eval reports, timing/cost reviews, KB context/notes, learnings, saved scripts, and prior step results.
5. **Inspect/debug execution** with `+"`list_executions`"+`, `+"`query_step`"+`, `+"`debug_step`"+`, and read-only review tools. Explain the issue and next action; do not mutate plan/config/learnings/KB/report/eval files in Run mode.

### Runtime context access
Run mode should read the workflow file system as normal operating memory, especially before doing work directly instead of running a step. Use:
- `+"`soul/soul.md`"+` to understand the workflow objective and success criteria.
- `+"`learnings/_global/SKILL.md`"+` as the first stop for HOW this workflow operates: target-system quirks, tool patterns, selectors, naming conventions, API behavior, auth/timing pitfalls, and reusable execution tricks.
- `+"`learnings/<step-id>/main.py`"+` for relevant `+"`scripted`"+` steps when the user's request maps to a known deterministic implementation pattern. Read it for behavior; do not edit it in Run mode.
- `+"`knowledgebase/context/context.md`"+` for user-provided rules, preferences, constraints, examples, and business context that should govern runtime behavior.
- `+"`knowledgebase/notes/_index.json`"+` first, then only targeted `+"`knowledgebase/notes/*.md`"+` files for workflow-discovered facts, history, patterns, and hypotheses.
- `+"`db/README.md`"+` to understand durable table contracts, primary keys, merge rules, and writer/consumer ownership before interpreting or updating any db-backed state through a step.
- `+"`db/db.sqlite`"+`, `+"`db/assets/`"+`, latest `+"`runs/iteration-0/`"+`, and reports/eval artifacts for current state and outcomes.

Reading these files is part of normal Run mode behavior. Writing persistent workflow design artifacts is not: do not manually edit plan/config/learnings/KB/report/eval files from Run mode. For user-confirmed durable runtime context, use `+"`capture_context`"+`.

Before direct runtime work, always check the generated workflow skill first when it exists: `+"`learnings/_global/SKILL.md`"+`. Then check the KB and db surfaces that match the request. Treat these as the workflow's playbook and memory. If the direct task maps to a `+"`scripted`"+` step, also inspect `+"`learnings/{step-id}/main.py`"+` for the proven implementation pattern, but do not edit it from Run mode.

### Audience
The user here is usually **non-technical** — a stakeholder, a teammate, an end user. They don't read JSON, they don't know step IDs, they don't want to see file paths or `+"`jq`"+` queries. They want answers in plain English.

### How to communicate
- **Be conversational, not terse.** "The run finished. 23 of 24 companies were processed successfully — one failed because the page wouldn't load. Would you like me to retry that one?" — not "completed: success_count=23 fail=1".
- **Translate, don't dump.** When you read a JSON file or run output, summarize it in human terms. Numbers get units (₹4,200, 12 minutes, 87%). Status gets adjectives (succeeded, failed, partial). Names from `+"`db/`"+` get used directly ("HDFC Bank's account") instead of IDs.
- **Bite-size replies.** Many users will read this on a phone. Default to a few short paragraphs or 3–5 short bullets. Avoid wide markdown tables. Save long output for when the user explicitly asks for "everything" or "details".
- **No filenames or paths unless asked.** Don't say "see `+"`runs/iteration-0/group-x/logs/...`"+`". If you mention a result, describe it; if the user wants the source, they'll ask.
- **No tech jargon.** "Pre-validation failed" → "the output didn't have the right fields". "Cron expression" → "scheduled for 9 AM weekdays".

### Things you do here
- **Do the work directly** when the user's request is narrower than a workflow run. Read the relevant KB/learnings/db/run artifacts, use available tools, and answer with the result.
- **Run the workflow** when asked: use `+"`run_full_workflow`"+` with an explicit `+"`group_name`"+`. For multi-group workflows, default to sequential one-group-at-a-time execution unless the user explicitly asks to run groups in parallel.
- **Run one step or orphan utility step** when asked: identify the step from the user's words, ask only if ambiguous, then call `+"`execute_step`"+` with `+"`group_name`"+`. Orphan steps are valid Run-mode tools when they are designed as manual utilities, data checks, shared route agents, or one-off investigations. Keep the `+"`execution_id`"+` for follow-up status checks.
- **Answer status questions**: if the user asks "is it running?", "what is it doing?", "why is it stuck?", or "what happened?", call `+"`query_step(step_id=...)`"+` for the relevant step. For completed steps, use `+"`debug_step(step_id, group_name=...)`"+` and targeted output reads.
- **Answer result questions**: read the latest run's outputs, `+"`db/`"+` data, report-bound JSON, KB notes, and the evaluation report when useful. Lead with the answer, then give the evidence in plain language.
- **Answer "did it work?" / "what happened?"**: give a one-paragraph human summary. Lead with the outcome (worked / partial / failed), then the headline numbers, then offer to dig deeper.
- **Answer "how much did it cost?" / "how long?"**: use the review tools and report numbers in plain language ("about ₹12, took 4 minutes").
- **Show the report**: if the user asks to "see the dashboard" or "show me the numbers", tell them to open the **Report tab**. The report is rendered live; you don't generate it.

### What's blocked here
Plan / config / learnings / evaluation design / knowledgebase / the report. If the user wants to change *what the workflow does* or *what the report looks like*, tell them which mode handles that — Workshop for design, report, and reliability changes — and offer to switch when they are ready. Do not try to make those changes from Run.

### When something fails
- Don't paste stack traces. Read the error, translate it: "the login page didn't load — looks like a temporary network issue" or "the Excel file we expected isn't there yet".
- Use `+"`query_step`"+` for running executions and `+"`debug_step`"+` for completed/failed steps before deciding what to say.
- Offer the next reasonable action: retry the step, retry the group, skip, or ask for missing input.
- If a failure looks transient (network, rate limit, temporary page load, missing external file that may appear soon), you may retry the same step or group.
- If the same failure repeats or points to bad workflow behavior, recommend switching to Workshop for a real fix instead of repeatedly retrying.

### Slash commands
Read-only review commands such as `+"`/review-plan`"+` are available if the user asks for a structured assessment, but don't run them by default — most users want a sentence, not a report.
{{end}}

## CURRENT STATE

- **Workspace**: {{.WorkspacePath}} (`+"`{{.AbsWorkspacePath}}/`"+`)
- **Run Folder**: {{.RunFolder}}
- **Objective & Success Criteria**: {{if and .WorkflowObjective .WorkflowSuccessCriteria}}defined in `+"`soul/soul.md`"+` — read it for the canonical objective and success criteria (not inlined here to keep this prompt small; you're instructed to read soul.md first regardless).{{else}}{{if not .WorkflowObjective}}⚠️ Objective not defined — {{if eq .WorkshopMode "run"}}tell the user it's missing and suggest switching to Workshop to define it; do not edit `+"`soul/soul.md`"+` in Run mode. {{else}}check `+"`soul/soul.md`"+` for a `+"`## Objective`"+` section and fill it in via shell (soul.md is canonical; plan.json no longer holds this). {{end}}{{end}}{{if not .WorkflowSuccessCriteria}}⚠️ Success criteria not defined — {{if eq .WorkshopMode "run"}}tell the user they're missing and suggest switching to Workshop; do not edit `+"`soul/soul.md`"+` in Run mode.{{else}}check `+"`soul/soul.md`"+` for a `+"`## Success Criteria`"+` section; if missing, ask the user what success looks like, then write it via shell.{{end}}{{end}}{{end}}
{{if .AvailableGroups}}- **Available Groups**: {{.AvailableGroups}}
{{end}}- **Step Configs**: {{if .StepConfigSummary}}{{.StepConfigSummary}}{{else}}No step configs yet{{end}}
- **Progress**: {{if .ProgressSummary}}{{.ProgressSummary}}{{else}}No progress tracked yet{{end}}

{{if .StepSummary}}### Plan Steps
{{.StepSummary}}
{{end}}
{{if .PlanJSON}}`+"```json\n{{.PlanJSON}}\n```"+`{{else}}Do NOT dump the full `+"`planning/plan.json`"+` by default. Read it precisely with targeted `+"`jq`"+` queries. The structure is: root `+"`steps[]`"+` for top-level steps, nested sub-agents in `+"`predefined_routes[].sub_agent_step`"+`, reusable definitions through `+"`predefined_routes[].orphan_step_ref`"+`, and deterministic transitions through `+"`routes[].next_step_id`"+` plus step-level `+"`next_step_id`"+`. Reusable orphan definitions live under `+"`orphan_steps[]`"+` and may expose `+"`shared_with.orchestrator_ids`"+` to allow specific todo_task steps to reuse them.

Use `+"`execute_shell_command`"+` with focused queries like:
- **Top-level overview only**: `+"`jq '[.steps[] | {id, title, type}]' planning/plan.json`"+`
- **Single step by `+"`step_id`"+` anywhere in the plan**: `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid)' planning/plan.json`"+`
- **Only the fields you need from one step**: `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid) | {id, title, type, description, context_dependencies, context_output}' planning/plan.json`"+`
- **Inspect only route structure for a todo_task or routing step**: `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid) | {id, type, predefined_routes, routes}' planning/plan.json`"+`

Use `+"`cat planning/plan.json`"+` only when you genuinely need the entire file.{{end}}

{{if eq .WorkshopMode "workshop"}}
## Planning steps

Take action by default: design and create the best-practice plan from available context. Ask only for blocking choices that materially change behavior, safety, credentials, schedules, external side effects, or irreversible actions; state reasonable assumptions briefly and proceed. For fixed branch choices the user already gave in chat, use deterministic `+"`routing`"+` and pass `+"`route_selections`"+` when running; do not add `+"`human_input`"+` just to ask the same branch choice again. Start with one large `+"`message_sequence`"+` per coherent shared-context span. It should complete that span, require run-specific proof/provenance, re-open the evidence, prove every criterion, repair gaps, and double-check the result. Improve its description, top-level `+"`validation_schema`"+`, and verify/repair turns before adding steps. Use multiple large sequences when their contexts should not be shared because of credentials/security, independent outputs/retries, clean-room independence, human/routing boundaries, or context contamination; the builder must be able to state the boundary. Every new conversational or judgment-heavy step uses `+"`message_sequence`"+`, even when it needs only one work turn. Use `+"`add_scripted_step`"+` only for deterministic API/SDK calls, CLI commands, data fetching, stable parsing/normalization, and mechanical persistence; it stores the internal `+"`regular`"+` JSON type and configures it as scripted automatically. Batch related calls under one source/auth/retry/output contract and feed their validated DB rows or artifacts into the relevant sequence for judgment, synthesis, evidence-based verification, and repair. Do not create one scripted step per endpoint, proof check, or routine subtask. Every step needs `+"`validation_schema`"+`. Context flow forward-only via `+"`context_dependencies`"+` → `+"`context_output`"+`. Step types: `+"`message_sequence`"+` · scripted (`+"`regular`"+` internally) · `+"`todo_task`"+` · `+"`routing`"+` · `+"`human_input`"+` · orphan.

`+"`message_sequence`"+` pattern catalog (named so you know what to ask for; full details in the `+"`message-sequence`"+` reference doc): Stateful Specialist · Test/Fix Loop · Maker+Reviewer · Panel · Clean-Room Retry · HITL Re-entry · Scripted Conversation.

For the design playbook (8-step walkthrough, step-type trade-offs, validation design, context flow, anti-patterns, orphan-route pattern): `+"`get_reference_doc(kind=\"plan-design\")`"+`. For per-step deep dives use the corresponding kinds: `+"`regular`"+`, `+"`todo-task`"+`, `+"`human-input`"+`, `+"`message-sequence`"+`, `+"`routing`"+`. For recurring multi-step shapes: `+"`workflow-patterns`"+`. A condensed composition overview is also available at `+"`get_reference_doc(kind=\"planning-steps\")`"+`.
{{end}}

## Running steps

Workshop builder always uses `+"`iteration-0`"+`. Every `+"`execute_step`"+` re-reads the latest `+"`plan.json`"+`. Before running anything, read `+"`cat variables/variables.json`"+` for `+"`group_name`"+` values and ALWAYS pass an explicit `+"`group_name`"+`. {{if .AvailableGroups}}Available groups: **{{.AvailableGroups}}**.{{end}}

Pass `+"`human_input`"+` to human-input steps inline (don't block on UI). **Always follow up after the automatic completion notification**; launching background work is not a completed final response. End the current agent turn while it runs—do not keep that turn open with `+"`query_step`"+` / `+"`list_executions`"+` polling. **To stop:** call `+"`stop_all_executions()`"+` or `+"`stop_step(execution_id)`"+` — text alone does NOT stop background tasks. Auto-notifications arrive prefixed `+"`[AUTO-NOTIFICATION]`"+` and are system-generated, not user messages.

For the full 6-step execution procedure (run / handle human_input / wait / success-failure handling), iteration & groups rules, auto-notification semantics (may be delayed, recency check), and stopping discipline: `+"`get_reference_doc(kind=\"running-steps\")`"+`.

## DEBUGGING

When a step doesn't do what it should — wrong output, missing actions, incomplete results — **don't just re-run it**. You have a smarter model — use it to investigate.

{{if eq .WorkshopMode "workshop"}}**Workshop:** bounded reliability fix / plan edit / manual edit per the workshop investigation workflow. When a step is stuck or repeatedly failing, run the task yourself using the same tools the step agent would use, after reading `+"`learnings/_global/SKILL.md`"+`; figure out what works, then update the step. **Act, don't just analyze.**
{{else}}**Run mode:** inspect via `+"`query_step`"+` (live) / `+"`debug_step`"+` (completed) / `+"`list_executions`"+`; explain the likely fix in plain English. Do not mutate plan/config/learnings/KB/report/eval here — redirect those to Workshop.
{{end}}

For the full debugging playbook (workshop vs run investigation workflow steps, root-cause → fix mapping table, fix options per mode): `+"`get_reference_doc(kind=\"debugging-flow\")`"+`. Load when a step has failed or is stuck and you need to decide between retry, Pulse Bug Review/Fixer, plan change, or mode switch.

{{if eq .WorkshopMode "workshop"}}
## Optimization

Priority order when reviewing a step: (1) Correctness — description precision, validation schema completeness, context I/O wiring. (2) Knowledge — learnings quality, lock lifecycle. (3) Efficiency — tool-call waste, workflow structure (merge/split/reorder).

	Hard rules: `+"`validation_schema`"+` is the only automated gate (catch stale files, field completeness, constraints); default `+"`learnings_access`"+` = `+"`\"read\"`"+`; use `+"`\"read-write\"`"+` + `+"`learning_objective`"+` only for reusable execution HOW (browser selectors/timing/auth, API/MCP quirks, CLI/SDK command patterns, parsing/retry/recovery rules). Routing, validation, mechanical transforms, aggregation/report shaping, human approval, pure db/KB readers, and mature scripted steps should usually stay read-only. `+"`db_access`"+` defaults to `+"`\"read-write\"`"+` (every step can read+write `+"`db/db.sqlite`"+` via `+"`$DB_PATH`"+`); set `+"`\"read\"`"+` for least-privilege steps that must never mutate the db (pure readers, report-shaping/aggregation, validation) so an accidental write is sandbox-denied. Deterministic API/SDK/CLI data fetching, stable parsing/normalization/transforms, and mechanical persistence start `+"`scripted`"+`; author and test `+"`learnings/<step-id>/main.py`"+` immediately, then feed durable results to agentic processing. Judgment, adaptive discovery, and browser/UI work stay `+"`agentic`"+`. No run-history threshold is needed to declare a deterministic step scripted. `+"`lock_learnings=true`"+` is a deliberate Workshop/user decision, never a runtime side effect; `+"`lock_code=true`"+` still requires 10+ representative scenario-covering runs. Three locks: `+"`lock_learnings`"+` (per-step, freezes SKILL.md), `+"`lock_code`"+` (per-step scripted, freezes main.py), `+"`lock_knowledgebase`"+` (workflow-wide, freezes notes/ auto-updates).

For the full playbook (validation design, learning config, three-locks decision tree, scripted debugging, mode promotion gates, evidence-based locking, orchestrator design + fast path, KB curation modes): `+"`get_reference_doc(kind=\"optimize-playbook\")`"+`. For the per-step config knobs themselves — all store-access modes (`+"`learnings_access`"+` / `+"`knowledgebase_access`"+` / `+"`db_access`"+`), the three locks, execution mode/tier/model, and `+"`update_step_config`"+`/clear usage — load `+"`get_reference_doc(kind=\"step-config\")`"+`. When patching `+"`learnings/{step-id}/main.py`"+`: also load `+"`code-authoring`"+`.
{{end}}

{{if eq .UseProjectedReferenceSkills "true"}}
## Tools

For `+"`human_feedback`"+`, use a foreground curl. Never use `+"`nohup`"+`, background the call, or poll a result file; the foreground response resumes the agent automatically. Cursor agents must keep `+"`timeout_seconds <= 45`"+`.

The native `+"`api-bridge`"+` exposes only `+"`execute_shell_command`"+`, `+"`diff_patch_workspace_file`"+`, `+"`agent_browser`"+`, and `+"`get_api_spec`"+`. Names such as `+"`execute_step`"+`, `+"`query_step`"+`, `+"`list_executions`"+`, and the other workflow tools below are logical HTTP-backed tools, not native `+"`api-bridge.<name>`"+` calls. Never call `+"`api-bridge.list_executions`"+` or guess another native bridge name. First call `+"`get_api_spec(server_name=\"workflow\", tool_name=\"<name>\")`"+`, then invoke the returned endpoint through `+"`execute_shell_command`"+` using the provided `+"`$MCP_MCP`"+`/`+"`$MCP_CUSTOM`"+` route and `+"`$MCP_AUTH`"+`; do not invent or hardcode a URL. The normal workflow loop uses `+"`execute_step`"+` / `+"`run_full_workflow`"+`, waits for the automatic completion notification rather than polling, and inspects a live step with `+"`query_step`"+` only when the user asks. Workshop decisions can use `+"`create_human_input_request`"+` and `+"`run_goal_advisor_review`"+`. Read the attached `+"`workflow-reference`"+` skill's `+"`references/workflow-tools.md`"+` (or call `+"`get_reference_doc(kind=\"workflow-tools\")`"+`) for the complete catalog, signatures, mode rules, schedules, secrets, notifications, and gotchas.
{{else}}
## TOOLS REFERENCE (cheat sheet)

{{if eq .IsCodeExecutionMode "true"}}**Code execution mode:** You do NOT have direct tool-call access. Bridge-native tools: `+"`execute_shell_command`"+`, `+"`diff_patch_workspace_file`"+`, `+"`agent_browser`"+`, `+"`get_api_spec`"+`. All other workflow tools are available via the workflow API path — use `+"`get_api_spec(server_name=\"workflow\", tool_name=\"...\")`"+` for their schemas. Do **not** hardcode raw HTTP requests.
{{end}}

This is the one-line-per-category map. For full signatures, parameters, when-to-use rules, and gotchas (especially Schedules and Secrets, which have multi-step flows), call **`+"`get_reference_doc(kind=\"workflow-tools\")`"+`**.

{{if or (eq .WorkshopMode "workshop") (eq .WorkshopMode "run")}}
- **Step execution & inspection**: `+"`execute_step`"+`, `+"`query_step`"+`, `+"`send_step_message`"+`, `+"`debug_step`"+`, `+"`list_executions`"+`, `+"`stop_step`"+`, `+"`stop_all_executions`"+`, `+"`run_in_background`"+`, `+"`run_full_workflow`"+`. {{if eq .WorkshopMode "workshop"}}Workshop also exposes `+"`execute_step(..., fast_path_only=true)`"+` for scripted main.py fast-path testing.{{end}}
{{end}}{{if eq .WorkshopMode "workshop"}}
- **Step config & analysis**: `+"`update_step_config`"+`, read-only improve/review tools, `+"`review_workflow_timing`"+`, `+"`review_workflow_costs`"+`, and `+"`get_cost_summary`"+`. Objective + success criteria live in `+"`soul/soul.md`"+`. Pulse uses parallel read-only reviewers and one parent Pulse Fixer. Strategy changes use normal plan tools only after approval or during an explicit bounded manual request.
- **Strategy review & decisions**: `+"`run_goal_advisor_review`"+` runs the dedicated advisor/critic/finalizer pipeline for a manual workshop review. Use `+"`create_human_input_request`"+` for durable approval/clarification cards; scheduled Pulse renders them in `+"`builder/improve.html`"+`.
{{end}}
- **Read-only info**: `+"`get_step_prompts`"+`, `+"`get_workflow_config`"+`, `+"`get_llm_config`"+`{{if eq .WorkshopMode "workshop"}}, `+"`get_workflow_command_guidance(kind=\"review-artifact-drift\")`"+`{{else}}. Artifact drift reviews belong in Workshop — switch modes and run `+"`/review-artifact-drift`"+` if needed{{end}}.
{{if eq .WorkshopMode "workshop"}}
- **Plan modification**: `+"`create_plan`"+`, `+"`add_<type>_step`"+`, `+"`update_<type>_step`"+`, `+"`delete_plan_steps`"+`, `+"`cleanup_orphan_step_configs`"+`, todo-task route tools, `+"`update_validation_schema`"+`.
- **Variables & config**: `+"`update_variable`"+`, `+"`add_group`"+`/`+"`update_group`"+`/`+"`delete_group`"+`, `+"`update_workflow_config`"+`. Use `+"`update_workflow_config`"+` for workflow MCP servers, workflow-level MCP tool allowlists, selected skills, selected secrets, the one-way Slack webhook secret reference, browser_mode, KB lock, run retention, and the per-run monitor (`+"`post_run_monitor`"+`). Do NOT edit `+"`workflow.json`"+` manually.
- **Schedule management**: `+"`list_schedules`"+`, `+"`create_schedule`"+`, `+"`create_calendar_schedule`"+`, `+"`update_schedule`"+`, `+"`delete_schedule`"+`, `+"`trigger_schedule`"+`, `+"`get_schedule_runs`"+`. Cron / message-authoring rules, normal Run schedules plus Pulse, the `+"`/pulse-setup`"+` setup path, and unattended-message discipline — all live in the `+"`workflow-tools`"+` ref doc. Workflow schedules always use the workshop path; do not create direct `+"`mode=\"workflow\"`"+` schedules. **Whenever you create a recurring schedule, also pair it with a backup** so unattended runs persist their state off-box — see `+"`get_reference_doc(kind=\"backup-strategy\")`"+`.
{{end}}
- **Shell & discovery**: `+"`execute_shell_command`"+`, `+"`diff_patch_workspace_file`"+`, `+"`read_image`"+`, `+"`generate_text_llm`"+`, `+"`search_web_llm`"+`.
- **Human attention**: `+"`human_feedback`"+` opens a blocking AgentWorks response card. It never sends through Gmail, workflow webhooks, `+"`notify_user`"+`, or account-level notification connectors. Use it only for an explicit in-app channel test or urgent, short-lived human-only input such as CAPTCHA/OTP/immediate approval; for an ordinary Builder question, ask in your normal response. In a bridge-only coding CLI, call `+"`$MCP_CUSTOM/human_feedback`"+` with a foreground curl and wait for that same call to return the answer. Never use `+"`nohup`"+`, append `+"`&`"+`, delegate/background it, write its result to a temporary file, poll it, or ask the user to message again after responding; the foreground response resumes the agent automatically. Do not make the shell timeout shorter than `+"`human_feedback.timeout_seconds`"+`. Cursor CLI has an approximately 60-second silent MCP-call ceiling, so Cursor agents must use `+"`timeout_seconds <= 45`"+`; after a real expiry, retry only if the input is still required. `+"`notify_user`"+` sends a non-blocking message to connected channels (Slack / WhatsApp / email) for FYIs, progress, alerts, or completion notices when no reply is required. Slack webhook delivery is backend-owned rich Block Kit by default; for structured summaries use `+"`slack_title`"+`, `+"`slack_color`"+`, `+"`slack_fields`"+`, `+"`slack_sections`"+`, and `+"`slack_footer`"+`. Never access or post to a webhook URL directly. For email it accepts `+"`email_subject`"+`, an HTML body (`+"`email_html`"+` or `+"`email_html_file`"+`), and `+"`email_attachments`"+`. Report delivery failures honestly. Workflow steps use the same tools through the `+"`human_tools`"+` step capability.
- **Skills**: `+"`list_skills`"+`, `+"`search_skills`"+`, `+"`install_skill`"+`, `+"`import_skill`"+`, `+"`uninstall_skill`"+`. Skills live at `+"`{{.AbsDocsRoot}}/skills/{folder}/SKILL.md`"+` (workspace root, shared across workflows). `+"`update_workflow_config(add_skills=[...])`"+` selects skills for workshop/builder discovery; step execution requires explicit `+"`update_step_config(step_id, enabled_skills=[...])`"+`. Shared workflow-specific HOW belongs in `+"`learnings/_global/SKILL.md`"+`.
- **Secrets**: `+"`set_workflow_secret`"+`, `+"`set_user_secret`"+`, `+"`list_secrets`"+`, `+"`delete_workflow_secret`"+`, `+"`delete_user_secret`"+`. Setting a secret **auto-attaches** it to the active workflow and injects `+"`$SECRET_<NAME>`"+` into the live shell — usable immediately, no separate `+"`update_workflow_config(add_secrets=[...])`"+` call needed (that's only for attaching an already-stored secret, e.g. a global or a reusable user secret you didn't just set). Three buckets (workflow / user / global). Values never appear in prompts or logs; step agents read them via `+"`$SECRET_<NAME>`"+` env vars only.
{{end}}

## File layout

**Shell working directory**: `+"`{{.AbsWorkspacePath}}/`"+`. Always use absolute paths in shell commands — prefix every path with `+"`{{.AbsWorkspacePath}}/`"+`. Do not use `+"`cd`"+` or relative paths.

Workspace roots: `+"`planning/`"+` (plan + step configs), `+"`runs/{iter}/{group}/execution|logs/{step-id}/`"+` (per-run outputs + logs), `+"`learnings/`"+` (saved scripts + global SKILL.md), `+"`evaluation/`"+` (eval plan + reports), `+"`db/`"+` (persistent state + assets + README.md schemas), `+"`knowledgebase/`"+` (context + notes), `+"`soul/soul.md`"+` (objective + success criteria), `+"`reports/report_plan.json`"+` (registers the report's HTML document(s)).

For the full layout (every log file's schema, timing-debug walkthrough, cost ledger paths, run metadata structure): `+"`get_reference_doc(kind=\"file-layout\")`"+`.

## CONSTRAINTS
1. **Use step IDs**: Step IDs come from plan.json (e.g., "step-create-report"), not positional numbers.
2. **Boolean config fields**: Only pass lock_learnings when explicitly changing it. Do NOT include it with false when updating other fields — this resets previously set values.
3. **Never hardcode variables or secrets**: Use variable placeholders (e.g., {USER_ID}) in descriptions and learnings. Actual values belong in variables/variables.json / variable groups.
4. **Never read application source code**: Do NOT search or read *.go, *.ts, or *.json files outside the workspace. You operate on workspace files only.
5. **Back up recurring schedules**: Whenever you create or update a recurring schedule, also set up a backup so unattended runs persist their state off-box — a final backup message for `+"`workshop`"+`-mode schedules, or a backup step in the plan for `+"`workflow`"+`-mode schedules (there is no message queue to carry the instruction). Load `+"`get_reference_doc(kind=\"backup-strategy\")`"+` for the playbook; confirm with the user before skipping.
`)

var interactiveWorkshopUserTemplate = MustRegisterTemplate("interactiveWorkshopUser", `{{if .UserRequest}}{{.UserRequest}}{{else}}What would you like to do in the workshop?{{end}}`)

// Execute implements OrchestratorAgent interface for the interactive workshop agent
func (agent *WorkflowInteractiveWorkshopAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	mcpAgentRef := baseAgent.Agent()
	iwm := agent.iwm
	workspacePath := templateVars["WorkspacePath"]

	// Logger (prefer mcpagent logger, fall back to controller logger)
	var logger loggerv2.Logger
	if mcpAgentRef != nil && mcpAgentRef.Logger != nil {
		logger = mcpAgentRef.Logger
	} else {
		logger = iwm.controller.GetLogger()
	}

	// Register plan modification tools
	if err := RegisterPlanModificationTools(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
		iwm.controller.WriteWorkspaceFile,
		iwm.controller.MoveWorkspaceFile,
		"workflow-builder",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register plan modification tools: %v", err))
	}

	// Variable management tools (update_variable, group CRUD)
	// are registered inside registerInteractiveWorkshopTools for both full and HAE modes.

	// Register custom workshop tools (execute_step, query_step, send_step_message, stop_step, update_step_config)
	registerWorkshopAgentTools(iwm, mcpAgentRef, workspacePath, logger)

	// Update the code execution registry for CLI providers.
	if agent.GetConfig().UseCodeExecutionMode {
		if err := mcpAgentRef.UpdateCodeExecutionRegistry(); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to update code execution registry with workshop tools: %v", err))
		} else {
			logger.Info("✅ Code execution registry updated with workshop tools")
		}
	}

	// Build system prompt and initial user message
	var systemPrompt, userMessage strings.Builder
	if err := interactiveWorkshopSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := interactiveWorkshopUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	// Append code execution instructions from mcpagent library
	// These include the {{TOOL_STRUCTURE}} placeholder (replaced by SetSystemPrompt with actual tool index)
	if agent.GetConfig().UseCodeExecutionMode {
		codeExecInstructions := prompt.GetCodeExecutionInstructions(workspacePath)
		if codeExecInstructions != "" {
			systemPrompt.WriteString("\n\n")
			systemPrompt.WriteString(codeExecInstructions)
			logger.Info("Added code execution instructions with tool structure to workshop agent")
		}
	}

	// Append browser instructions if browser tools are available in this workflow.
	// Replace the ~5-10KB BuildBrowserInstructions block with a one-line
	// pointer; the full guide lives in the workflow-reference mega-skill as
	// `browser-usage` and is fetched on demand. Mirrors the pattern at
	// server.go:5024 (workflow_phase path).
	browserCfg := iwm.controller.resolveBrowserConfig(iwm.controller.GetSelectedServers(), iwm.controller.GetSelectedSkills())
	if browserCfg.HasAgentBrowser {
		systemPrompt.WriteString("\n\n## Browser\n\nThis workflow has a browser tool available (mode=")
		systemPrompt.WriteString(browserCfg.Mode)
		systemPrompt.WriteString("). Read `get_reference_doc(kind=\"browser-usage\")` for Builder-specific mode, tab, file, and safety rules. ")
		if browserCfg.HasAgentBrowser && browserCfg.Mode == "cdp" {
			ports := append([]int{browserCfg.CdpPort}, browserCfg.CdpPorts...)
			endpoints := browser.ConfiguredCDPEndpoints(ports)
			endpoint := browser.ConfiguredCDPEndpoint(browserCfg.CdpPort)
			if len(endpoints) > 0 {
				endpoint = endpoints[0]
			}
			if len(endpoints) > 1 {
				systemPrompt.WriteString("This workflow explicitly authorizes independently-profiled Chrome browsers at `" + strings.Join(endpoints, "`, `") + "`. Choose the endpoint matching the intended login/account on every call; multiple ports are for specialized multi-login testing, not normal workflow concurrency. ")
			}
			systemPrompt.WriteString("Every agent_browser call must explicitly include one authorized `--cdp <endpoint>`. Before the first browser action, load the version-matched core skill with `agent_browser(command=\"skills\", args=[\"--cdp\", \"" + endpoint + "\", \"get\", \"core\"], session=\"default\")`; this docs call does not require a selected tab.\n")
		} else if browserCfg.HasAgentBrowser {
			systemPrompt.WriteString("Before the first browser action, load the version-matched core skill with `agent_browser(command=\"skills\", args=[\"get\", \"core\"], session=\"default\")`.\n")
		}
		logger.Info(fmt.Sprintf("🌐 Added browser-skill pointer to workflow builder system prompt (mode=%s, agent-browser=%v)",
			browserCfg.Mode, browserCfg.HasAgentBrowser))
	}

	// NOTE: Secrets are injected by the server-level handler (server.go) via AppendSystemPrompt
	// after the agent is created. Do NOT inject here — it causes duplication in the prompt.

	sessionID := templateVars["SessionID"]
	workflowID := templateVars["WorkflowID"]

	// Emit start event
	eventBridge := iwm.controller.GetContextAwareBridge()
	if eventBridge != nil {
		startedEvent := &orchestrator_events.OrchestratorAgentStartEvent{
			BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
			AgentType:     "workflow-builder",
			AgentName:     "workflow-builder-agent",
			Objective:     "Workflow builder session",
			InputData:     templateVars,
		}
		eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
			Type:      orchestrator_events.OrchestratorAgentStart,
			Timestamp: time.Now(),
			Data:      startedEvent,
		})
	}

	currentResult := ""
	currentConversationHistory := conversationHistory

	// Free-flow loop — no cap; ends only when user approves or provides empty feedback
	for {
		inputProcessor := func(map[string]string) string { return userMessage.String() }

		result, updatedHistory, err := agent.ExecuteWithTemplateValidation(
			ctx, templateVars, inputProcessor,
			currentConversationHistory, struct{}{},
			systemPrompt.String(), true,
		)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedHistory

		// Ask user if done or what to do next
		requestID := fmt.Sprintf("workshop_continue_%d", time.Now().UnixNano())
		approved, feedback, err := iwm.controller.RequestHumanFeedback(
			ctx, requestID,
			"Done? Or tell me what to do next.",
			currentResult,
			sessionID,
			workflowID,
		)
		if err != nil {
			break
		}
		if approved || feedback == "" {
			break
		}
		// Continue with user feedback as next message
		var feedbackBuilder strings.Builder
		feedbackBuilder.WriteString(feedback)
		userMessage = feedbackBuilder
	}

	// Emit completion event
	if eventBridge != nil {
		completedEvent := &orchestrator_events.OrchestratorAgentEndEvent{
			BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
			AgentType:     "workflow-builder",
			AgentName:     "workflow-builder-agent",
			Objective:     "Workflow builder session",
			Result:        currentResult,
			Success:       true,
		}
		eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
			Type:      orchestrator_events.OrchestratorAgentEnd,
			Timestamp: time.Now(),
			Data:      completedEvent,
		})
	}

	return currentResult, currentConversationHistory, nil
}

// ============================================================================
// Custom Workshop Tools
// ============================================================================

// resolveWorkshopStepID resolves a user-provided step reference to an actual step ID from the plan.
// Accepts exact IDs, 1-based positions ("1", "step-1", "step1"), and falls back with suggestions.
// Requires plan to be loaded on the controller (call LoadPlanForWorkshop first).
func resolveWorkshopStepID(controller *StepBasedWorkflowOrchestrator, inputID string) (string, error) {
	plan := controller.approvedPlan
	if plan == nil || len(plan.Steps) == 0 {
		return "", fmt.Errorf("no plan loaded")
	}

	// 1. Exact match (top-level + inner steps)
	if info := findWorkshopStepByID(plan.Steps, inputID); info != nil {
		return inputID, nil
	}

	// 2. Positional match: extract number from "1", "step-1", "step1", "step 1"
	numStr := inputID
	for _, prefix := range []string{"step-", "step ", "step"} {
		if strings.HasPrefix(strings.ToLower(inputID), prefix) {
			numStr = inputID[len(prefix):]
			break
		}
	}
	var pos int
	if _, err := fmt.Sscanf(strings.TrimSpace(numStr), "%d", &pos); err == nil && pos >= 1 && pos <= len(plan.Steps) {
		return plan.Steps[pos-1].GetID(), nil
	}

	// 3. Not found — return valid IDs for helpful error message
	allSteps := collectAllSteps(plan.Steps)
	ids := make([]string, 0, len(allSteps))
	for _, info := range allSteps {
		label := fmt.Sprintf("%q (step %d)", info.Step.GetID(), info.TopIndex)
		if info.TopIndex <= 0 {
			label = fmt.Sprintf("%q (inner, parent=%s, location=%s)", info.Step.GetID(), info.ParentID, info.NestedLocation)
		}
		ids = append(ids, label)
	}
	return "", fmt.Errorf("step %q not found in plan. Valid IDs: %s", inputID, strings.Join(ids, ", "))
}

func resolveWorkshopStepConfigTarget(ctx context.Context, controller *StepBasedWorkflowOrchestrator, inputID string) (resolvedID string, configSubdir string, isEvalStep bool, err error) {
	originalEvalMode := controller.isEvaluationMode
	originalPlan := controller.approvedPlan
	defer func() {
		controller.isEvaluationMode = originalEvalMode
		controller.approvedPlan = originalPlan
	}()

	controller.isEvaluationMode = false
	if loadErr := controller.LoadPlanForWorkshop(ctx); loadErr == nil {
		if id, resolveErr := resolveWorkshopStepID(controller, inputID); resolveErr == nil {
			return id, "planning", false, nil
		}
	}

	controller.isEvaluationMode = true
	if loadErr := controller.LoadPlanForWorkshop(ctx); loadErr != nil {
		return "", "", false, fmt.Errorf("step %q not found in planning/plan.json, and failed to load evaluation/evaluation_plan.json: %w", inputID, loadErr)
	}
	if id, resolveErr := resolveWorkshopStepID(controller, inputID); resolveErr == nil {
		return id, "evaluation", true, nil
	}

	return "", "", false, fmt.Errorf("step %q not found in planning/plan.json or evaluation/evaluation_plan.json", inputID)
}

// registerInteractiveWorkshopTools registers the custom workshop tools on the agent.
func registerGetCostSummaryTool(iwm *InteractiveWorkshopManager, mcpAgent *mcpagent.Agent, logger loggerv2.Logger) {
	if err := mcpAgent.RegisterCustomTool(
		"get_cost_summary",
		"Show token usage and cost breakdown for the current run plus builder/Pulse overhead. Displays per-step, per-model, and phase-level totals with USD costs when priced.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"run_folder": map[string]interface{}{
					"type":        "string",
					"description": "Optional run folder such as 'iteration-0/group-1'. If omitted, uses the currently selected run folder.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			runFolder, _ := args["run_folder"].(string)
			runFolder = strings.TrimSpace(runFolder)
			if runFolder == "" {
				runFolder = strings.TrimSpace(iwm.controller.selectedRunFolder)
			}
			if runFolder == "" {
				return "no run folder selected; pass run_folder like 'iteration-0/group-1'", nil
			}

			var tokenFile *orchestrator.TokenUsageFile
			runMissingReason := ""
			if strings.TrimSpace(runFolder) == strings.TrimSpace(iwm.controller.selectedRunFolder) {
				tokenFile = iwm.controller.GetCurrentRunTokenUsageFile()
			} else {
				tokenPath := filepath.ToSlash(filepath.Join("runs", runFolder, "token_usage.json"))
				tokenContent, err := iwm.controller.ReadWorkspaceFile(ctx, tokenPath)
				if err != nil {
					runMissingReason = fmt.Sprintf("missing evidence: no run token usage data found at %s", tokenPath)
				} else {
					var parsed orchestrator.TokenUsageFile
					if err := json.Unmarshal([]byte(tokenContent), &parsed); err != nil {
						runMissingReason = fmt.Sprintf("missing evidence: failed to parse %s: %v", tokenPath, err)
					} else {
						tokenFile = &parsed
					}
				}
			}
			if tokenFile == nil || len(tokenFile.ByModel) == 0 && len(tokenFile.ByStepAndModel) == 0 {
				if runMissingReason == "" {
					runMissingReason = fmt.Sprintf("missing evidence: no run token usage data found for %s", runFolder)
				}
			}

			tok := func(s string) string {
				if s == "" {
					return "0"
				}
				return s
			}

			var result strings.Builder
			result.WriteString(fmt.Sprintf("## Cost Summary — %s\n\n", runFolder))

			if runMissingReason != "" {
				result.WriteString("### Run Cost\n\n")
				result.WriteString(runMissingReason)
				result.WriteString("\n\n")
			}

			if tokenFile != nil && len(tokenFile.ByStepAndModel) > 0 {
				result.WriteString("### Per-Step Breakdown\n\n")
				result.WriteString("| Step | Model | Input | Output | Cache | Cost |\n")
				result.WriteString("|------|-------|-------|--------|-------|------|\n")

				stepKeys := make([]string, 0, len(tokenFile.ByStepAndModel))
				for k := range tokenFile.ByStepAndModel {
					stepKeys = append(stepKeys, k)
				}
				sort.Strings(stepKeys)

				grandTotalCost := 0.0
				for _, stepKey := range stepKeys {
					models := tokenFile.ByStepAndModel[stepKey]
					modelKeys := make([]string, 0, len(models))
					for k := range models {
						modelKeys = append(modelKeys, k)
					}
					sort.Strings(modelKeys)
					for _, modelID := range modelKeys {
						usage := models[modelID]
						grandTotalCost += usage.TotalCost
						result.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | $%.4f |\n",
							stepKey, modelID,
							tok(usage.InputTokensM), tok(usage.OutputTokensM), tok(usage.CacheTokensM),
							usage.TotalCost,
						))
					}
				}
				result.WriteString(fmt.Sprintf("\n**Grand total: $%.4f**\n\n", grandTotalCost))
			}

			if tokenFile != nil && len(tokenFile.ByModel) > 0 {
				result.WriteString("### Per-Model Totals\n\n")
				result.WriteString("| Model | Input | Output | Cache R/W | Reasoning | Calls | Cost |\n")
				result.WriteString("|-------|-------|--------|-----------|-----------|-------|------|\n")

				modelKeys := make([]string, 0, len(tokenFile.ByModel))
				for k := range tokenFile.ByModel {
					modelKeys = append(modelKeys, k)
				}
				sort.Strings(modelKeys)

				totalCost := 0.0
				for _, modelID := range modelKeys {
					usage := tokenFile.ByModel[modelID]
					totalCost += usage.TotalCost
					cacheRW := fmt.Sprintf("%s / %s", tok(usage.CacheReadTokensM), tok(usage.CacheWriteTokensM))
					result.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %d | $%.4f |\n",
						modelID,
						tok(usage.InputTokensM), tok(usage.OutputTokensM), cacheRW,
						tok(usage.ReasoningTokensM), usage.LLMCallCount,
						usage.TotalCost,
					))
				}
				result.WriteString(fmt.Sprintf("\n**Total: $%.4f**\n", totalCost))
			}

			phaseTokenPath := orchestrator.ResolvePhaseTokenUsagePath("")
			phaseContent, phaseErr := iwm.controller.ReadWorkspaceFile(ctx, phaseTokenPath)
			if phaseErr != nil {
				result.WriteString("\n### Builder/Pulse Overhead (phase-level costs)\n\n")
				result.WriteString(fmt.Sprintf("missing evidence: no phase token usage data found at %s\n", phaseTokenPath))
			} else {
				var phaseFile orchestrator.PhaseTokenUsageFile
				if err := json.Unmarshal([]byte(phaseContent), &phaseFile); err != nil {
					result.WriteString("\n### Builder/Pulse Overhead (phase-level costs)\n\n")
					result.WriteString(fmt.Sprintf("missing evidence: failed to parse %s: %v\n", phaseTokenPath, err))
				} else if len(phaseFile.ByPhaseAndModel) == 0 {
					result.WriteString("\n### Builder/Pulse Overhead (phase-level costs)\n\n")
					result.WriteString(fmt.Sprintf("missing evidence: no by_phase_and_model entries in %s\n", phaseTokenPath))
				} else {
					result.WriteString("\n### Builder/Pulse Overhead (phase-level costs)\n\n")
					result.WriteString("| Phase | Model | Input | Output | Cost |\n")
					result.WriteString("|-------|-------|-------|--------|------|\n")

					phaseKeys := make([]string, 0, len(phaseFile.ByPhaseAndModel))
					for k := range phaseFile.ByPhaseAndModel {
						phaseKeys = append(phaseKeys, k)
					}
					sort.Strings(phaseKeys)

					phaseTotalCost := 0.0
					for _, phase := range phaseKeys {
						models := phaseFile.ByPhaseAndModel[phase]
						pModelKeys := make([]string, 0, len(models))
						for k := range models {
							pModelKeys = append(pModelKeys, k)
						}
						sort.Strings(pModelKeys)
						for _, modelID := range pModelKeys {
							usage := models[modelID]
							phaseTotalCost += usage.TotalCost
							result.WriteString(fmt.Sprintf("| %s | %s | %s | %s | $%.4f |\n",
								phase, modelID,
								tok(usage.InputTokensM), tok(usage.OutputTokensM),
								usage.TotalCost,
							))
						}
					}
					result.WriteString(fmt.Sprintf("\n**Phase total: $%.4f**\n", phaseTotalCost))
				}
			}

			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_cost_summary tool: %v", err))
	}
}

func registerInteractiveWorkshopTools(iwm *InteractiveWorkshopManager, mcpAgent *mcpagent.Agent, logger loggerv2.Logger) {
	// Tool 1: execute_step — start step in background
	if err := mcpAgent.RegisterCustomTool(
		"execute_step",
		"Start a workflow step in the background, including a normal plan step, nested route step, or plan-local orphan utility step. Returns an execution_id immediately. You will be automatically notified when it completes. Idempotent while running: if the step already has a running execution, this returns that existing execution_id instead of starting a duplicate — use send_step_message with the returned execution_id to steer its currently active agent turn. Learnings follow the step's persistent config (`learnings_access`, `learning_objective`, `lock_learnings`). When learning writes are enabled, SKILL.md updates run as the step agent's direct post-completion continuation before the step is fully finalized. Workshop mode only: set fast_path_only=true to run ONLY the saved learnings/{step-id}/main.py script with no LLM fallback when testing scripted patches.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json or orphan_steps[] (e.g., 'step-create-report', 'validate-environment') or positional reference (e.g., '1', 'step-1', 'step1')",
				},
				"group_name": map[string]interface{}{
					"type":        "string",
					"description": "Variable group ID (e.g., 'group-1', 'saurabh'). Required. Read variables/variables.json to see available groups.",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "Optional orchestrator instructions for inner steps (sub-agents from todo_task/orchestration routes). Appended to the step description as '## Orchestrator Instructions'. Simulates what the parent orchestrator would provide when delegating. Ignored for top-level steps.",
				},
				"human_input": map[string]interface{}{
					"type":        "string",
					"description": "Optional human input/custom instructions for this run. For a standalone message_sequence step, this is opening context and the configured queue still runs from the beginning; standalone sessions are not resumed across execute_step calls. For human_input steps, it is used as the response. For other executable steps, it is injected as high-priority context.",
				},
				"message_sequence_restart": map[string]interface{}{
					"type":        "boolean",
					"description": "Message_sequence steps only. Request a clean run and clear route-local runtime artifacts before replaying the configured queue. Standalone execute_step calls already start a fresh queue and do not provide durable conversation resume. In-memory route re-entry exists only while a todo-task workflow run is still active.",
				},
				"tier": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"high", "medium", "low"},
					"description": "Optional LLM tier override for this execution. 'high' = Tier 1 (most capable), 'medium' = Tier 2, 'low' = Tier 3 (fastest/cheapest). Overrides the default maturity-based tier selection. Only works in tiered mode.",
				},
				"fast_path_only": map[string]interface{}{
					"type":        "boolean",
					"description": "Workshop mode only. If true, run ONLY the saved learnings/{step-id}/main.py script with no LLM fallback. Fails if no saved script exists, the step is not in scripted mode, or the current workshop mode is Run. Use this to quickly test scripted main.py patches.",
				},
			},
			"required": []string{"step_id", "group_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			// Extract group_name and other options
			groupNameRaw, _ := args["group_name"]
			groupName, _ := groupNameRaw.(string)
			if groupName == "" {
				return "group_name is required. Read variables/variables.json to see available groups.", nil
			}

			// Fallback to session-level group from toolbar selection
			if groupName == "" && len(iwm.controller.enabledGroupNames) > 0 {
				groupName = iwm.controller.enabledGroupNames[0]
			}

			// Validate a group is available — cannot run steps without one
			if groupName == "" {
				iwm.refreshVariablesManifest(ctx)
				if iwm.controller.variablesManifest == nil || len(iwm.controller.variablesManifest.Groups) == 0 {
					return "No variable groups exist. Create a group first using add_group before running steps.", nil
				}
				// Auto-select the first available group
				groupName = iwm.controller.variablesManifest.Groups[0].Name
			}

			iteration := "iteration-0"

			// Build run_folder from iteration + group folder name
			// Refresh manifest from file to avoid stale group data
			iwm.refreshVariablesManifest(ctx)
			// Resolve group folder name from name (uses sanitized name)
			groupFolderName := groupName
			resolvedGroupName := ""
			if iwm.controller.variablesManifest != nil && groupName != "" {
				for _, g := range iwm.controller.variablesManifest.Groups {
					if g.Name == groupName || iwm.controller.sanitizeDisplayNameForFolder(g.Name) == groupName {
						if g.Name != "" {
							resolvedGroupName = g.Name
							sanitized := iwm.controller.sanitizeDisplayNameForFolder(g.Name)
							if sanitized != "" {
								groupFolderName = sanitized
							}
						}
						break
					}
				}
			}
			runFolder := fmt.Sprintf("%s/%s", iteration, groupFolderName)

			// Optional orchestrator instructions for inner steps
			instructions, _ := args["instructions"].(string)

			// Optional human input for any step type
			humanInput, _ := args["human_input"].(string)

			// Optional tier override (high=1, medium=2, low=3)
			tierValue := 0
			if tierStr, ok := args["tier"].(string); ok && tierStr != "" {
				switch tierStr {
				case "high":
					tierValue = 1
				case "medium":
					tierValue = 2
				case "low":
					tierValue = 3
				}
			}

			// fast_path_only: run saved script only, no LLM fallback
			fastPathOnly := false
			if val, ok := args["fast_path_only"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					fastPathOnly = b
				}
			}
			messageSequenceRestart := false
			if val, ok := args["message_sequence_restart"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					messageSequenceRestart = b
				}
			}
			if fastPathOnly && iwm.currentWorkshopModeFromConfigs(nil) == "workshop" {
				return "fast_path_only requires a scripted step. Workshop mode tests steps through agentic with execute_step(step_id, group_name) and leaves scripted main.py fast-path debugging to Workshop mode.", nil
			}

			execOpts := &WorkshopExecuteOptions{
				GroupName:              resolvedGroupName,
				Iteration:              iteration,
				RunFolder:              runFolder,
				SavedScriptOnly:        fastPathOnly,
				Instructions:           instructions,
				HumanInput:             humanInput,
				Tier:                   tierValue,
				MessageSequenceRestart: messageSequenceRestart,
			}

			// Resolve flexible step ID (handles "1", "step-1", "step1" etc.)
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v. Cannot resolve step ID.", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}
			stepID = resolvedID

			// Guard against duplicate concurrent runs of the SAME step. If an
			// execution for this step is already running (e.g. the agent
			// re-issued execute_step, or any double-trigger), return the live
			// execution instead of forking a second concurrent run. Two runs of
			// one step race on shared state (db/ rows, the browser/CDP session)
			// and can double-act — e.g. post the same tweet twice.
			if existing, found, _ := iwm.stepRegistry.LatestSnapshotForStep(stepID); found && existing.Status == WorkshopStepRunning {
				logger.Info(fmt.Sprintf("⏭️ Workshop: execute_step(%q) skipped — already running (execution_id=%q)", stepID, existing.ID))
				return fmt.Sprintf("Step %q is ALREADY RUNNING (execution_id: %q) — not starting a duplicate. You'll be notified when it completes. End the current agent turn instead of polling; use query_step(step_id=%q) only if the user explicitly requests a live status check, or stop that execution first if the user wants a fresh run. (Concurrent runs of the same step race on shared state and can double-act.)", stepID, existing.ID, stepID), nil
			}

			isScriptedStep := false
			if iwm.controller.approvedPlan != nil {
				if stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID); stepInfo != nil {
					if cfg := getAgentConfigs(stepInfo.Step); isScriptedExecutionModeConfig(cfg) {
						isScriptedStep = true
					}
				}
			}
			if !isScriptedStep {
				if configs, err := iwm.controller.ReadStepConfigs(ctx); err == nil {
					for _, sc := range configs {
						if sc.ID == stepID && isScriptedExecutionModeConfig(sc.AgentConfigs) {
							isScriptedStep = true
							break
						}
					}
				}
			}

			execID := fmt.Sprintf("exec-%s-%d", stepID, time.Now().UnixNano())
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			// Inject correlation IDs so step execution events are tagged as sub-agent
			// events. ForceCorrelationIDKey survives child agent context overwrites
			// (child agents overwrite AgentSessionIDKey but not ForceCorrelationIDKey),
			// so all nested events share the same correlation_id as the
			// orchestrator_agent_start event, enabling frontend grouping.
			agentSessionID := fmt.Sprintf("workshop-step-%s-%d", stepID, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         stepID,
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)
			workflowSessionID := ""
			if iwm.mainSessionID != "" {
				parentGroupName := resolvedGroupName
				if parentGroupName == "" {
					parentGroupName = groupName
				}
				parentChat := &virtualtools.ParentChatContext{
					SessionID:    iwm.mainSessionID,
					WorkflowPath: iwm.controller.GetWorkspacePath(),
					GroupName:    parentGroupName,
					AgentID:      execID,
				}
				if workflowSessionID = iwm.controller.GetMCPSessionID(); workflowSessionID != "" {
					virtualtools.RegisterParentChat(workflowSessionID, &virtualtools.ParentChatContext{
						SessionID:    parentChat.SessionID,
						WorkflowPath: parentChat.WorkflowPath,
						GroupName:    parentChat.GroupName,
						AgentID:      parentChat.AgentID,
					})
				}
				virtualtools.RegisterParentChat(agentSessionID, parentChat)
			}

			go func() {
				if workflowSessionID != "" {
					defer virtualtools.UnregisterParentChat(workflowSessionID)
				}
				defer virtualtools.UnregisterParentChat(agentSessionID)

				var result string
				var execErr error

				// Inject tier override into context if specified (concurrent-safe via context, not shared field)
				if execOpts.Tier >= 1 && execOpts.Tier <= 3 {
					execCtx = context.WithValue(execCtx, WorkshopTierOverrideKey, execOpts.Tier)
				}

				// Resolve step title and type for the wrapper event (use plan step if available)
				stepDisplayName := stepID
				if iwm.controller.approvedPlan != nil {
					if stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID); stepInfo != nil {
						stepDisplayName = stepInfo.Step.GetTitle()
					}
				}
				// Include group name in display name so notifications clearly identify which group they belong to
				if resolvedGroupName != "" {
					stepDisplayName = fmt.Sprintf("%s [%s]", stepDisplayName, resolvedGroupName)
				} else if groupName != "" {
					stepDisplayName = fmt.Sprintf("%s [%s]", stepDisplayName, groupName)
				}

				// Notify server layer so bgAgentRegistry tracks this execution (keeps frontend polling alive)
				if iwm.executionNotifier != nil {
					iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
						ID:                execID,
						ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
						Name:              stepDisplayName,
						Cancel:            cancel,
					})
				}
				execCtx = virtualtools.WithBackgroundAgentID(execCtx, execID)
				execCtx = context.WithValue(execCtx, orchestrator_events.ParentExecutionIDKey, execID)

				// Variables captured after execution for metadata
				var isLockCode bool
				var isLockLearnings bool
				var lockCodeConsecutiveFailures int
				var lockCodeNeedsReview bool
				var workshopModeForMeta string

				eventBridge := iwm.controller.GetContextAwareBridge()
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if !skipNotify && iwm.executionNotifier != nil {
						execMeta := map[string]string{
							"iteration":  iteration,
							"group_name": groupName,
						}
						if isLockCode {
							execMeta["lock_code"] = "true"
						}
						if isLockLearnings {
							execMeta["lock_learnings"] = "true"
						}
						if lockCodeConsecutiveFailures > 0 {
							execMeta["lock_code_consecutive_failures"] = fmt.Sprintf("%d", lockCodeConsecutiveFailures)
						}
						if lockCodeNeedsReview {
							execMeta["lock_code_needs_review"] = "true"
						}
						// Use frontend-selected mode if available, else fall back to auto-detection
						if iwm.workshopModeOverride != "" {
							execMeta["workshop_mode"] = iwm.workshopModeOverride
						} else if workshopModeForMeta != "" {
							execMeta["workshop_mode"] = workshopModeForMeta
						}
						iwm.executionNotifier.OnExecutionComplete(execID, stepDisplayName, result, execMeta, execErr)
					}
				}()

				// Emit orchestrator_agent_start so the frontend creates a grouping card
				if eventBridge != nil {
					inputData := map[string]string{}
					if execOpts != nil {
						if execOpts.GroupName != "" {
							inputData["group_name"] = execOpts.GroupName
						}
						if execOpts.Iteration != "" {
							inputData["iteration"] = execOpts.Iteration
						}
						if execOpts.RunFolder != "" {
							inputData["run_folder"] = execOpts.RunFolder
						}
					}
					if isScriptedStep {
						inputData["workshop_mode"] = "scripted"
						inputData["IsScriptedMode"] = "true"
					}
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData:        baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:            "workshop-step-execution",
						AgentName:            fmt.Sprintf("Step: %s", stepDisplayName),
						InputData:            inputData,
						Iteration:            parseWorkshopIterationNumber(execOpts.Iteration),
						UseCodeExecutionMode: true,
						UseScriptedMode:      isScriptedStep,
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				result, execErr = iwm.controller.ExecuteStepForWorkshop(execCtx, stepID, execOpts)

				// Capture step lock flags so the auto-notification can tailor
				// recovery guidance (e.g. fast-path failure on a locked step has only two
				// recovery paths: fix main.py after unlocking, or rerun with fast_path_only=false).
				if configs, configErr := iwm.controller.ReadStepConfigs(execCtx); configErr == nil {
					for _, sc := range configs {
						if sc.ID == stepID && sc.AgentConfigs != nil {
							if sc.AgentConfigs.LockCode != nil && *sc.AgentConfigs.LockCode {
								isLockCode = true
							}
							if sc.AgentConfigs.LockLearnings != nil && *sc.AgentConfigs.LockLearnings {
								isLockLearnings = true
							}
							break
						}
					}
					workshopModeForMeta = detectWorkshopMode(iwm.controller.approvedPlan, configs)
				}

				// If the step is locked, surface its locked-script run history so the auto-
				// notification can flag a "this frozen script keeps failing" pattern to the
				// builder rather than letting it accumulate silently in script_metadata.json.
				if isLockCode {
					if meta := iwm.controller.readScriptedMetadataAPI(execCtx, stepID); meta != nil && meta.LockCodeStats != nil {
						lockCodeConsecutiveFailures = meta.LockCodeStats.ConsecutiveFailures
						lockCodeNeedsReview = meta.LockCodeStats.NeedsReview
					}
				}
			}()

			groupInfo := ""
			if groupName != "" {
				groupInfo = fmt.Sprintf(", group=%q", groupName)
			}
			logger.Info(fmt.Sprintf("🚀 Workshop: step %q started in background, execution_id=%q%s, fast_path_only=%v", stepID, execID, groupInfo, fastPathOnly))
			return fmt.Sprintf("Step %q started in background.\nexecution_id: %q\nYou will be automatically notified when it completes. End the current agent turn now instead of polling. Use query_step(step_id=%q) only if the user explicitly requests a live status check. Use send_step_message(execution_id=%q, message=...) only for a necessary live correction while an agent turn is active.", stepID, execID, stepID, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register execute_step tool: %v", err))
	}

	// Tool: run_in_background — spawn independent background agent (not tied to a workflow step)
	if err := mcpAgent.RegisterCustomTool(
		"run_in_background",
		"Spawn an independent background agent to run a task with the same tools as the workflow. Returns an execution_id immediately. You will be notified when it completes. Use this to offload context-heavy work or run tasks in parallel.\n\nagent_type controls the agent model:\n- \"executor\" (default): single-pass execution agent — best for focused, well-defined tasks\n- \"orchestrator\": todo task orchestrator with call_generic_agent — best for complex multi-step tasks that benefit from task management and sub-agent delegation. Sub-agent completions also auto-notify you.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Short descriptive name (e.g., 'Research APIs', 'Validate data')",
				},
				"instruction": map[string]interface{}{
					"type":        "string",
					"description": "Comprehensive instructions for the background agent. This is the agent's task — be specific about what it should do, inputs, expected outputs.",
				},
				"agent_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"executor", "orchestrator"},
					"description": "executor (default): single-pass agent. orchestrator: todo task orchestrator with sub-agent delegation.",
				},
			},
			"required": []string{"name", "instruction"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			nameRaw, ok := args["name"]
			if !ok || nameRaw == nil {
				return "name is required", nil
			}
			name, ok := nameRaw.(string)
			if !ok || name == "" {
				return "name must be a non-empty string", nil
			}

			instructionRaw, ok := args["instruction"]
			if !ok || instructionRaw == nil {
				return "instruction is required", nil
			}
			instruction, ok := instructionRaw.(string)
			if !ok || instruction == "" {
				return "instruction must be a non-empty string", nil
			}

			agentType := "executor"
			if v, ok := args["agent_type"].(string); ok && v != "" {
				agentType = v
			}

			// Create slug from name for execution ID
			nameSlug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
			// Trim to reasonable length
			if len(nameSlug) > 30 {
				nameSlug = nameSlug[:30]
			}

			execID := fmt.Sprintf("bg-%s-%05d", nameSlug, time.Now().UnixNano()%100000)
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			// Inject correlation IDs for sub-agent event tagging (same pattern as execute_step)
			agentSessionID := fmt.Sprintf("workshop-bg-%s-%d", nameSlug, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         name, // Use name as the "step" identifier for display
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			// Notify server layer so bgAgentRegistry tracks this execution (keeps frontend polling alive)
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                execID,
					ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
					Name:              name,
					Cancel:            cancel,
				})
			}
			execCtx = virtualtools.WithBackgroundAgentID(execCtx, execID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ParentExecutionIDKey, execID)

			go func() {
				var result string
				var execErr error
				eventBridge := iwm.controller.GetContextAwareBridge()
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						isCancelled := skipNotify || execCtx.Err() != nil
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-background-task",
							AgentName:     fmt.Sprintf("Background: %s", name),
							Success:       execErr == nil,
						}
						if execErr != nil {
							if isCancelled {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = result
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type:          orchestrator_events.OrchestratorAgentEnd,
							Timestamp:     time.Now(),
							Data:          endEvent,
							CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, name, result, nil, execErr)
					}
				}()

				// Emit orchestrator_agent_start so the frontend creates a grouping card
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-background-task",
						AgentName:     fmt.Sprintf("Background: %s", name),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				if agentType == "orchestrator" {
					result, execErr = iwm.runBackgroundTodoTaskAgent(execCtx, name, instruction)
				} else {
					result, execErr = iwm.runBackgroundTaskAgent(execCtx, name, instruction)
				}
			}()

			logger.Info(fmt.Sprintf("🚀 Workshop: background task %q started (type=%s), execution_id=%q", name, agentType, execID))
			return fmt.Sprintf("Background task %q started (type=%s).\nexecution_id: %q\nYou will be automatically notified when it completes.", name, agentType, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_in_background tool: %v", err))
	}

	// Pulse reviewers are synchronous so the parent cannot enter its fixer phase
	// until every parallel review result has returned. This executor is registered
	// on the exact workshop session, preventing another workflow's todo_task
	// executor from being selected through the global code-exec registry.
	if err := mcpAgent.RegisterCustomTool(
		"call_generic_agent",
		"Run one read-only reviewer in an isolated context for this workflow. In coding-agent code-execution mode, invoke this custom tool through the documented API bridge shell call; that transport is supported. Every reviewer is tracked as a child execution and sends a compact automatic start/completion notification to the parent. If the outer MCP shell call moves to the background, end the current turn and wait for that automatic notification instead of polling. Pulse permits at most two concurrent calls. For Pulse, pass pulse_run_id, review_run_id, and module: the reviewer is instructed to produce a durable Markdown artifact rather than a conversational completion message; the trusted backend persists that complete artifact to pulse/reviews/<dated-review-run-id>/<module>.md and returns/notifies only its compact path reference. The reviewer cannot mutate files, configuration, plans, reports, evaluations, human inputs, or module state. Incomplete provider snapshots are rejected and retried once. Do not put a custom completion marker in instructions; this tool appends and validates its own marker.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todo_id": map[string]interface{}{
					"type":        "string",
					"description": "Stable reviewer task ID used for correlation.",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "Complete read-only review instructions, including workflow path, Pulse run id, module, evidence, and response contract. Do not add a final completion marker; the tool appends its own authoritative marker.",
				},
				"preferred_tier": map[string]interface{}{
					"type":        "integer",
					"enum":        []int{1, 2, 3},
					"description": "Required intended reasoning tier. Pulse reviewers execute with the workflow maintenance model.",
				},
				"pulse_run_id": map[string]interface{}{
					"type":        "string",
					"description": "Pulse worklist run ID. Required together with review_run_id and module for a persisted Pulse review.",
				},
				"review_run_id": map[string]interface{}{
					"type":        "string",
					"description": "Exact scheduler-provided dated review ID (YYYY-MM-DDTHH-MM-SS.mmmZ_<pulse-id>). Required for Pulse.",
				},
				"module": map[string]interface{}{
					"type":        "string",
					"description": "Exact Pulse module name. Required for Pulse and used as the separate result filename.",
				},
			},
			"required": []string{"todo_id", "instructions", "preferred_tier"},
		},
		func(ctx context.Context, args map[string]interface{}) (toolResult string, toolErr error) {
			todoID, _ := args["todo_id"].(string)
			todoID = strings.TrimSpace(todoID)
			if todoID == "" {
				return "", fmt.Errorf("todo_id is required")
			}
			instructions, _ := args["instructions"].(string)
			instructions = strings.TrimSpace(instructions)
			if instructions == "" {
				return "", fmt.Errorf("instructions are required")
			}
			preferredTier, ok := args["preferred_tier"].(float64)
			if !ok || preferredTier < 1 || preferredTier > 3 {
				return "", fmt.Errorf("preferred_tier is required and must be 1, 2, or 3")
			}
			pulseRunID, _ := args["pulse_run_id"].(string)
			pulseRunID = strings.TrimSpace(pulseRunID)
			reviewRunID, _ := args["review_run_id"].(string)
			reviewRunID = strings.TrimSpace(reviewRunID)
			module, _ := args["module"].(string)
			module = strings.TrimSpace(module)
			pulseMetadataCount := 0
			for _, value := range []string{pulseRunID, reviewRunID, module} {
				if value != "" {
					pulseMetadataCount++
				}
			}
			looksLikePulseReview := strings.Contains(strings.ToLower(instructions), "pulse_run_id") ||
				strings.Contains(strings.ToLower(instructions), "pulse run id")
			if (pulseMetadataCount != 0 || looksLikePulseReview) && pulseMetadataCount != 3 {
				return "", fmt.Errorf("pulse_run_id, review_run_id, and module must be provided together")
			}
			isPulseReview := pulseMetadataCount == 3
			var resultPath string
			if isPulseReview {
				var pathErr error
				resultPath, pathErr = pulseReviewResultPath(reviewRunID, module)
				if pathErr != nil {
					return "", pathErr
				}
				if iwm.pulseReviewerSlots == nil {
					iwm.toolAgentSetupMu.Lock()
					if iwm.pulseReviewerSlots == nil {
						iwm.pulseReviewerSlots = make(chan struct{}, pulseReviewerMaxConcurrency)
					}
					iwm.toolAgentSetupMu.Unlock()
				}
				if err := acquirePulseReviewerSlot(ctx, iwm.pulseReviewerSlots); err != nil {
					return "", fmt.Errorf("wait for Pulse reviewer slot: %w", err)
				}
				defer func() { <-iwm.pulseReviewerSlots }()
			}

			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "", ctxErr
			}
			defer cancel()
			executionPrefix := "generic-agent"
			reviewName := "Generic agent: " + strings.ReplaceAll(todoID, "-", " ")
			if isPulseReview {
				executionPrefix = "pulse-review"
				reviewName = "Pulse reviewer: " + strings.ReplaceAll(todoID, "-", " ")
			}
			reviewExecID := fmt.Sprintf("%s-%s-%d", executionPrefix, sanitizeWorkshopAgentIdentityPart(todoID), time.Now().UnixNano())
			agentSessionID := fmt.Sprintf("workshop-review-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			trackedStepID := "generic-agent:" + todoID
			if isPulseReview {
				trackedStepID = "pulse-review:" + todoID
			}
			reviewExec := &WorkshopStepExecution{
				ID:             reviewExecID,
				StepID:         trackedStepID,
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(reviewExec)
			executionType := "generic-agent"
			executionKind := "generic_agent"
			if isPulseReview {
				executionType = "pulse-reviewer"
				executionKind = "pulse_reviewer"
			}
			reviewMeta := map[string]string{
				"execution_type": executionType,
				"todo_id":        todoID,
			}
			if isPulseReview {
				reviewMeta["pulse_reviewer"] = "true"
				reviewMeta["pulse_run_id"] = pulseRunID
				reviewMeta["review_run_id"] = reviewRunID
				reviewMeta["module"] = module
				reviewMeta["review_result_path"] = resultPath
			}
			parentExecutionID := currentWorkshopParentExecutionID(ctx)
			if parentExecutionID == "" && strings.TrimSpace(iwm.mainSessionID) != "" {
				parentExecutionID = "main:" + strings.TrimSpace(iwm.mainSessionID)
			}
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                reviewExecID,
					ParentExecutionID: parentExecutionID,
					Name:              reviewName,
					Kind:              executionKind,
					Metadata:          reviewMeta,
					Cancel:            cancel,
				})
			}
			execCtx = virtualtools.WithBackgroundAgentID(execCtx, reviewExecID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ParentExecutionIDKey, reviewExecID)
			defer func() {
				skipNotify := finalizeExecStatus(reviewExec, execCtx, &toolResult, &toolErr)
				if !skipNotify && iwm.executionNotifier != nil {
					iwm.executionNotifier.OnExecutionComplete(reviewExecID, reviewName, toolResult, reviewMeta, toolErr)
				}
			}()

			workspacePath := iwm.controller.GetWorkspacePath()
			marker := pulseReviewerCompletionMarker(todoID)
			reviewerInstruction := buildPulseReviewerInstruction(workspacePath, resultPath, instructions, marker)
			persistFailure := func(message string) error {
				if !isPulseReview {
					return nil
				}
				body := pulseReviewResultMarkdown(pulseRunID, reviewRunID, module, "failed", message, time.Now())
				return iwm.controller.WriteWorkspaceFile(ctx, resultPath, body)
			}

			var incompleteErr error
			for attempt := 1; attempt <= 2; attempt++ {
				stageName := "Generic agent - " + todoID
				if isPulseReview {
					stageName = "Pulse reviewer - " + todoID
				}
				if attempt > 1 {
					stageName += " - completion retry"
				}
				result, runErr := iwm.runGoalAdvisorStageAgent(execCtx, stageName, reviewerInstruction, goalAdvisorStageReadOnly)
				if runErr != nil {
					if writeErr := persistFailure(runErr.Error()); writeErr != nil {
						return "", fmt.Errorf("%w; additionally failed to persist Pulse reviewer failure at %s: %w", runErr, resultPath, writeErr)
					}
					if isPulseReview {
						return "", fmt.Errorf("%w; failure recorded at %s", runErr, resultPath)
					}
					return "", runErr
				}
				completed, completionErr := completedPulseReviewerResult(result, marker)
				if completionErr == nil {
					if !isPulseReview {
						return completed, nil
					}
					body := pulseReviewResultMarkdown(pulseRunID, reviewRunID, module, "completed", completed, time.Now())
					if writeErr := iwm.controller.WriteWorkspaceFile(ctx, resultPath, body); writeErr != nil {
						return "", fmt.Errorf("persist Pulse reviewer result %s: %w", resultPath, writeErr)
					}
					return fmt.Sprintf("Pulse reviewer completed and was persisted.\nmodule: %s\nreview_result_path: %s\nRead that file before applying or recording fixes.", module, resultPath), nil
				}
				incompleteErr = completionErr
				logger.Warn(fmt.Sprintf("⚠️ Pulse reviewer %q attempt %d returned incomplete output: %v", todoID, attempt, completionErr))
			}
			finalErr := fmt.Errorf("Pulse reviewer %q returned incomplete output twice; partial findings were rejected: %w", todoID, incompleteErr)
			if writeErr := persistFailure(finalErr.Error()); writeErr != nil {
				return "", fmt.Errorf("%w; additionally failed to persist Pulse reviewer failure at %s: %w", finalErr, resultPath, writeErr)
			}
			if isPulseReview {
				return "", fmt.Errorf("%w; failure recorded at %s", finalErr, resultPath)
			}
			return "", finalErr
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register workshop call_generic_agent tool: %v", err))
	}

	// Tool: run_goal_advisor_review — dedicated strategic review background pipeline.
	if err := mcpAgent.RegisterCustomTool(
		"run_goal_advisor_review",
		"Start the dedicated background Goal Advisor pipeline. Use this for Pulse-selected strategic review: goal recovery, healthy 10x/headroom exploration, advancing or measuring the one active advisor experiment, approved proposal application, and Chief of Staff strategic recommendations. The pipeline runs advisor -> critic -> finalizer in separate background agents. It is analysis-first: advisor and critic never format HTML, and the finalizer may make only one bounded in-place Advisor log update when state materially changes. The durable experiment lifecycle lives in builder/improve.html; the parent Pulse turn should capture the returned execution_id and end its turn. Automatic completion notification will resume the session so it can record mark_pulse_module_result; do not poll query_step/list_executions while waiting.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pulse_run_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional Pulse run id from the current Pulse Gate/worklist.",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus from the Pulse Gate evidence or user request.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			pulseRunID := ""
			if val, ok := args["pulse_run_id"]; ok && val != nil {
				if s, ok := val.(string); ok {
					pulseRunID = strings.TrimSpace(s)
				}
			}
			focus := ""
			if val, ok := args["focus"]; ok && val != nil {
				if s, ok := val.(string); ok {
					focus = strings.TrimSpace(s)
				}
			}

			name := "Goal Advisor Review"
			execID := fmt.Sprintf("goal-advisor-%05d", time.Now().UnixNano()%100000)
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			agentSessionID := fmt.Sprintf("workshop-goal-advisor-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         "goal-advisor",
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                execID,
					ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
					Name:              name,
					Cancel:            cancel,
				})
			}
			execCtx = virtualtools.WithBackgroundAgentID(execCtx, execID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ParentExecutionIDKey, execID)

			go func() {
				var result string
				var execErr error
				eventBridge := iwm.controller.GetContextAwareBridge()
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						isCancelled := skipNotify || execCtx.Err() != nil
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-background-task",
							AgentName:     name,
							Success:       execErr == nil,
						}
						if execErr != nil {
							if isCancelled {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = result
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type:          orchestrator_events.OrchestratorAgentEnd,
							Timestamp:     time.Now(),
							Data:          endEvent,
							CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, name, result, nil, execErr)
					}
				}()

				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-background-task",
						AgentName:     name,
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				result, execErr = iwm.runGoalAdvisorReviewPipeline(execCtx, pulseRunID, focus)
			}()

			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			logger.Info(fmt.Sprintf("✨ Workshop: goal advisor review started in background, execution_id=%q, pulse_run_id=%q", execID, pulseRunID))
			return fmt.Sprintf("Goal Advisor review started in background.\nexecution_id: %q%s\nYou will be automatically notified when it completes.", execID, focusInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_goal_advisor_review tool: %v", err))
	}

	// Tool 2: query_step — execution status + structured MCP tool call visibility
	// When running: shows status + captured structured tool calls (auto-enriched)
	// When done/failed/cancelled: shows result
	if err := mcpAgent.RegisterCustomTool(
		"query_step",
		"One-off live status check for a tracked execution. For a workflow step, pass step_id and the backend resolves its latest execution automatically. For call_generic_agent/Pulse reviewers, pass the execution_id from the start notification; step_id is not required. When running, shows registry status and structured MCP tool calls captured so far. Important: coding CLI providers can show terminal/TUI activity before structured tool_call events exist, so 'no tool calls observed yet' does NOT mean the execution failed to start or is stuck. When a coding-CLI provider runs in tmux, query_step also captures the latest terminal lines and the tmux session name. After one running-status check, end the current agent turn; automatic completion notification will resume the session. Never alternate query_step and list_executions as a polling loop. Do not stop or re-run solely because no tool calls are listed. Pass tool_call_id to get full input/output for a specific tool call. Use debug_step for workflow-step file insights.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "Workflow step ID to inspect. Required unless execution_id is supplied. The backend resolves the latest execution for this step automatically.",
				},
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "Exact tracked execution ID. Use this without step_id for call_generic_agent and Pulse reviewer executions; it can also disambiguate workflow-step executions.",
				},
				"tool_call_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: a specific tool_call_id from a previous query_step summary to get full input/output details for that call",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepID := ""
			if stepIDRaw, ok := args["step_id"]; ok && stepIDRaw != nil {
				value, valueOK := stepIDRaw.(string)
				if !valueOK {
					return "step_id must be a string", nil
				}
				stepID = strings.TrimSpace(value)
			}

			// Optional: specific tool_call_id for detailed view
			toolCallID := ""
			if val, ok := args["tool_call_id"]; ok && val != nil {
				if s, ok := val.(string); ok {
					toolCallID = s
				}
			}

			execID := ""
			if execIDRaw, ok := args["execution_id"]; ok && execIDRaw != nil {
				if s, ok := execIDRaw.(string); ok {
					execID = strings.TrimSpace(s)
				}
			}
			if stepID == "" && execID == "" {
				return "step_id or execution_id is required", nil
			}
			if stepID != "" {
				if err := iwm.controller.LoadPlanForWorkshop(ctx); err == nil {
					if resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID); resolveErr == nil {
						stepID = resolvedID
					}
				}
			}

			var exec WorkshopStepSnapshot
			var found bool
			if execID != "" {
				exec, found = iwm.stepRegistry.GetSnapshot(execID)
				if !found {
					return fmt.Sprintf("execution %q not found", execID), nil
				}
				if stepID == "" {
					stepID = exec.StepID
				} else if exec.StepID != stepID {
					return fmt.Sprintf("execution %q belongs to step %q, not step %q", execID, exec.StepID, stepID), nil
				}
			} else {
				var matches []WorkshopStepSnapshot
				exec, found, matches = iwm.stepRegistry.LatestSnapshotForStep(stepID)
				if !found {
					return fmt.Sprintf("No tracked execution found for step %q. Start it with execute_step(step_id=%q, group_name=...).", stepID, stepID), nil
				}
				execID = exec.ID
				runningCount := 0
				for _, match := range matches {
					if match.Status == WorkshopStepRunning {
						runningCount++
					}
				}
				if runningCount > 1 {
					var ids []string
					for _, match := range matches {
						if match.Status == WorkshopStepRunning {
							ids = append(ids, match.ID)
						}
					}
					return fmt.Sprintf("Step %q has multiple running executions: %s. Use list_executions to inspect them or stop the duplicates.", stepID, strings.Join(ids, ", ")), nil
				}
			}

			status := exec.Status
			result := exec.Result
			execErr := exec.Err
			agentSessID := exec.AgentSessionID
			isGenericAgent := strings.HasPrefix(execID, "generic-agent-")
			isPulseReviewer := strings.HasPrefix(execID, "pulse-review-")
			executionLabel := fmt.Sprintf("Step %q", stepID)
			if isGenericAgent {
				executionLabel = fmt.Sprintf("Generic agent %q", strings.TrimPrefix(stepID, "generic-agent:"))
			} else if isPulseReviewer {
				executionLabel = fmt.Sprintf("Pulse reviewer %q", strings.TrimPrefix(stepID, "pulse-review:"))
			}

			switch status {
			case WorkshopStepRunning:
				pollGuidance := ""
				if toolCallID == "" {
					var suppressed bool
					pollGuidance, suppressed = iwm.runningStatusPollGuidance()
					if suppressed {
						return pollGuidance, nil
					}
				}
				messageHint := "\n\n**Live steering:** no active agent turn is currently available; the execution may be validating or running script-only work. Wait for the next phase or completion notification."
				if exec.CanReceiveMessage {
					messageHint = fmt.Sprintf("\n\n**Live steering:** send_step_message(execution_id=%q, message=...) can steer the active %s phase.", execID, exec.ActiveMessagePhase)
				}
				mainSessID := iwm.mainSessionID
				if mainSessID == "" {
					mainSessID = iwm.sessionID
				}

				// Auto-enrich with live tool calls when running
				var toolCallInfo string
				if iwm.toolCallQueryFunc != nil {
					summary := iwm.toolCallQueryFunc(mainSessID, agentSessID, stepID, toolCallID)
					if toolCallID != "" && summary != "" {
						return fmt.Sprintf("%s (execution_id: %s) — tool call detail:\n%s", executionLabel, execID, summary), nil
					}
					if summary != "" {
						toolCallInfo = fmt.Sprintf("\n\n**Structured MCP tool calls:**\n%s", summary)
					}
				}

				// If the step runs a coding CLI in tmux, surface the live tmux session
				// (the same name shown in the UI terminal panes) so the builder can
				// inspect the actual terminal pane via execute_shell_command. Coding-CLI
				// progress is terminal/TUI output that does NOT show up as MCP tool calls.
				var tmuxInfo string
				if iwm.tmuxLookupFunc != nil {
					if tmuxSession, paneTail, ok := iwm.tmuxLookupFunc(ctx, mainSessID, stepID); ok && tmuxSession != "" {
						tmuxInfo = fmt.Sprintf("\n\n**Live tmux session:** `%s` (coding CLI in tmux).", tmuxSession)
						if strings.TrimSpace(paneTail) != "" {
							tmuxInfo += fmt.Sprintf(" Latest terminal output:\n```\n%s\n```", paneTail)
						}
						tmuxInfo += fmt.Sprintf("\nFor more history, run `tmux capture-pane -pt %s -S -200` via execute_shell_command (read-only; do NOT use `tmux attach`, it blocks).", tmuxSession)
					}
				}

				// Detect execution type from ID prefix and add context
				isAnalysisAgent := strings.HasPrefix(execID, "learn-") || strings.HasPrefix(execID, "debug-")
				var hint string
				if isAnalysisAgent {
					hint = "\n\nNote: This is a learning/optimization agent — it only uses workspace tools (execute_shell_command, diff_patch_workspace_file). For richer insights, use debug_step(step_id) instead."
				}

				if toolCallInfo == "" {
					return fmt.Sprintf("%s is registered and running.\nexecution_id: %s\n\nNo structured MCP tool calls have been captured for this execution yet. This is normal for coding CLI providers while they are booting, thinking, using terminal/TUI output, or before they make their first api-bridge call. It does not mean the execution failed to start or is stuck.\n\nDo not stop or re-run this execution solely because no tool calls are listed; wait for the automatic completion notification unless the user explicitly asks you to stop/retry.%s%s%s\n\n%s", executionLabel, execID, messageHint, tmuxInfo, hint, pollGuidance), nil
				}
				return fmt.Sprintf("%s is still running.\nexecution_id: %s%s%s%s%s\n\n%s", executionLabel, execID, messageHint, toolCallInfo, tmuxInfo, hint, pollGuidance), nil

			case WorkshopStepDone:
				if isGenericAgent || isPulseReviewer {
					return fmt.Sprintf("%s completed.\nexecution_id: %s\n\n%s", executionLabel, execID, result), nil
				}
				// Background tasks get a generic completion response (no step-specific hints)
				if strings.HasPrefix(execID, "bg-") {
					return fmt.Sprintf("Background task %q completed.\n\n%s", stepID, result), nil
				}
				return fmt.Sprintf("Step %q completed.\nexecution_id: %s\n\n%s\n\n**Next actions (do these now):**\n1. Review the result against the step's success criteria\n2. Read shared workflow guidance: 'cat learnings/_global/SKILL.md'. If this is a scripted step, also inspect 'cat learnings/%s/main.py'.\n3. Check learning metadata: 'cat learnings/%s/.learning_metadata.json'. If the Workshop user decides this step should stop writing SKILL.md, set lock_learnings=true intentionally with review_notes. For scripted, lock_code requires explicit user intent plus 10+ scenario-covering successful runs.\n4. Note the highest-priority optimization from Post-Execution Step Review.\n5. If output looks wrong, investigate with debug_step(%q) and fix the root cause before re-running.", stepID, execID, result, stepID, stepID, stepID), nil
			case WorkshopStepFailed:
				if isGenericAgent || isPulseReviewer {
					return fmt.Sprintf("%s failed.\nexecution_id: %s\nerror: %v", executionLabel, execID, execErr), nil
				}
				if strings.HasPrefix(execID, "bg-") {
					return fmt.Sprintf("Background task %q failed: %v", stepID, execErr), nil
				}
				return fmt.Sprintf("Step %q failed.\nexecution_id: %s\nerror: %v\n\n**Next**: Investigate the failure. Call debug_step(%q) for detailed execution insights, then fix the root cause (description, validation, context deps) before re-running.", stepID, execID, execErr, stepID), nil
			case WorkshopStepCancelled:
				return fmt.Sprintf("%s was cancelled.\nexecution_id: %s", executionLabel, execID), nil
			default:
				return fmt.Sprintf("Step %q has unknown status: %s\nexecution_id: %s", stepID, status, execID), nil
			}
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register query_step tool: %v", err))
	}

	// Tool 2a: send_step_message — steer the exact child agent currently active
	// inside a running execution. This never starts or resumes an execution.
	if err := mcpAgent.RegisterCustomTool(
		"send_step_message",
		"Send a follow-up message to the active agent turn inside one exact running workflow execution. Use the execution_id returned by execute_step or run_full_workflow. This is live steering only: it never starts, restarts, or resumes a completed step. Coding CLI agents receive provider-native live input; API agents queue the message for the next safe turn boundary. If the execution is currently validating or running script-only work, the result is no_active_agent and you should wait rather than retry in a loop.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "Exact running execution_id returned by execute_step or run_full_workflow.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Follow-up instruction or correction to deliver to the currently active child agent.",
				},
			},
			"required": []string{"execution_id", "message"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			executionID, _ := args["execution_id"].(string)
			message, _ := args["message"].(string)
			result := iwm.stepRegistry.SendMessage(ctx, executionID, message)
			payload, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", err
			}
			return string(payload), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register send_step_message tool: %v", err))
	}

	// Tool 2b: debug_step — rich insights about a step's execution
	if err := mcpAgent.RegisterCustomTool(
		"debug_step",
		"Get detailed insights about a step's execution: learning status, validation result, iteration details for complex steps (todo_task/orchestration), and log paths. Use after a step completes or to inspect a running complex step's progress.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1')",
				},
				"group_name": map[string]interface{}{
					"type":        "string",
					"description": "Variable group ID (e.g., 'group-1', 'saurabh'). Required. Read variables/variables.json to see available groups.",
				},
			},
			"required": []string{"step_id", "group_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			// Extract group_name
			groupName, _ := args["group_name"].(string)

			iteration := "iteration-0"
			if groupName == "" {
				return "group_name is required (e.g., 'group-1'). Read variables/variables.json to see available groups.", nil
			}

			// Refresh manifest from file to avoid stale group data
			iwm.refreshVariablesManifest(ctx)
			// Resolve group folder name and build run folder
			groupFolderName := groupName
			if iwm.controller.variablesManifest != nil {
				for _, g := range iwm.controller.variablesManifest.Groups {
					if g.Name == groupName || iwm.controller.sanitizeDisplayNameForFolder(g.Name) == groupName {
						if g.Name != "" {
							sanitized := iwm.controller.sanitizeDisplayNameForFolder(g.Name)
							if sanitized != "" {
								groupFolderName = sanitized
							}
						}
						break
					}
				}
			}
			runFolder := fmt.Sprintf("%s/%s", iteration, groupFolderName)
			iwm.controller.SetSelectedRunFolder(runFolder)

			// Resolve step ID
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedID)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}

			var result strings.Builder
			result.WriteString(fmt.Sprintf("## Debug: %s (%s)\n\n", stepInfo.Step.GetTitle(), resolvedID))

			// Section 1: Learning status
			learningsPath := getLearningFolderPathByStepID("", resolvedID, "", iwm.controller.isEvaluationMode)
			learningFiles, _ := iwm.controller.readStepLearningFiles(ctx, learningsPath)

			stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
			lockStatus := "unlocked"
			for _, sc := range stepConfigs {
				if sc.ID == resolvedID && sc.AgentConfigs != nil {
					if sc.AgentConfigs.LockLearnings != nil && *sc.AgentConfigs.LockLearnings {
						lockStatus = "locked"
					}
					// "disabled" in this summary means the step contributes NOTHING to
					// global learnings — true only when learnings_access="none" or when
					// the effective access is not write-capable.
					if resolveLearningsAccess(sc.AgentConfigs) != LearningsAccessReadWrite {
						lockStatus = "disabled"
					}
					break
				}
			}

			result.WriteString("### Learnings\n")
			if len(learningFiles) > 0 {
				fileNames := make([]string, 0, len(learningFiles))
				for f := range learningFiles {
					fileNames = append(fileNames, f)
				}
				sort.Strings(fileNames)
				result.WriteString(fmt.Sprintf("Files: %d | Status: %s | Path: %s\n", len(learningFiles), lockStatus, learningsPath))
				for _, f := range fileNames {
					result.WriteString(fmt.Sprintf("  - %s\n", f))
				}
			} else {
				result.WriteString(fmt.Sprintf("No learnings yet | Status: %s | Path: %s\n", lockStatus, learningsPath))
			}
			result.WriteString("\n")

			// Section 2: Validation result
			logFolder := workshopStepLogFolder(resolvedID)
			validationLogDir := fmt.Sprintf("runs/%s/logs/%s", runFolder, logFolder)

			result.WriteString("### Validation\n")
			foundValidation := false
			for i := 5; i >= 2; i-- {
				vPath := fmt.Sprintf("%s/validation-%d.json", validationLogDir, i)
				if content, err := iwm.controller.ReadWorkspaceFile(ctx, vPath); err == nil {
					result.WriteString(content)
					result.WriteString("\n")
					foundValidation = true
					break
				}
			}
			if !foundValidation {
				if content, err := iwm.controller.ReadWorkspaceFile(ctx, fmt.Sprintf("%s/validation.json", validationLogDir)); err == nil {
					result.WriteString(content)
					result.WriteString("\n")
				} else {
					result.WriteString("No validation result found.\n")
				}
			}
			result.WriteString("\n")

			// Section 3: Complex step details (todo_task/orchestration)
			complexSummary := iwm.enrichQueryForComplexStep(ctx, resolvedID)
			if complexSummary != "" {
				result.WriteString("### Execution Details")
				result.WriteString(complexSummary)
				result.WriteString("\n")
			}

			// Section 4: Log paths for manual inspection
			result.WriteString("### Log Paths\n")
			result.WriteString(fmt.Sprintf("Execution logs: runs/%s/logs/%s/execution/\n", runFolder, logFolder))
			result.WriteString(fmt.Sprintf("Validation: %s/\n", validationLogDir))
			result.WriteString(fmt.Sprintf("Learnings: %s/\n", learningsPath))
			result.WriteString("Use execute_shell_command to read these files for details.\n")

			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register debug_step tool: %v", err))
	}

	// Tool 2b: list_executions — list all tracked background executions
	if err := mcpAgent.RegisterCustomTool(
		"list_executions",
		"One-off execution lookup for finding an execution_id or resolving ambiguity. Shows execution_id, step_id, status (running/done/failed/cancelled), and whether a running execution currently accepts send_step_message. Do not use it as a progress poll. After a result shows running work, end the current agent turn; automatic completion notification will resume the session. Never alternate list_executions and query_step as a polling loop.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status_filter": map[string]interface{}{
					"type":        "string",
					"description": "Optional filter: 'running', 'done', 'failed', 'cancelled'. If omitted, returns all.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			statusFilter, _ := args["status_filter"].(string)

			allExecs := iwm.stepRegistry.ListSnapshots()

			// Sort by creation time for chronological order.
			sort.Slice(allExecs, func(i, j int) bool {
				if allExecs[i].CreatedAt.Equal(allExecs[j].CreatedAt) {
					return allExecs[i].ID < allExecs[j].ID
				}
				return allExecs[i].CreatedAt.Before(allExecs[j].CreatedAt)
			})

			// Build set of registry IDs for dedup against server agents
			registryIDs := make(map[string]struct{}, len(allExecs))
			for _, exec := range allExecs {
				registryIDs[exec.ID] = struct{}{}
			}

			var sb strings.Builder
			count := 0
			hasRunning := false
			for _, exec := range allExecs {
				status := string(exec.Status)
				execErr := exec.Err

				if statusFilter != "" && status != statusFilter {
					continue
				}

				count++
				if status == "running" {
					hasRunning = true
				}
				sb.WriteString(fmt.Sprintf("- **%s** | step: %s | status: %s", exec.ID, exec.StepID, status))
				if exec.CanReceiveMessage {
					sb.WriteString(fmt.Sprintf(" | messageable: %s", exec.ActiveMessagePhase))
				}
				if status == "failed" && execErr != nil {
					sb.WriteString(fmt.Sprintf(" | error: %v", execErr))
				}
				sb.WriteString("\n")
			}

			// Merge server-tracked agents not in stepRegistry
			if iwm.listServerAgents != nil {
				for _, agent := range iwm.listServerAgents() {
					if _, exists := registryIDs[agent.ID]; exists {
						continue
					}
					if statusFilter != "" && agent.Status != statusFilter {
						continue
					}
					count++
					if agent.Status == "running" {
						hasRunning = true
					}
					sb.WriteString(fmt.Sprintf("- **%s** | step: %s | status: %s (server)\n", agent.ID, agent.Name, agent.Status))
				}
			}

			hasPending := iwm.hasPendingCompletions != nil && iwm.hasPendingCompletions()
			if hasPending {
				sb.WriteString("\n⚠️ Completions pending delivery (agents finished while session was busy).\n")
			}

			pollGuidance := ""
			if (statusFilter == "" || statusFilter == "running") && (hasRunning || hasPending) {
				var suppressed bool
				pollGuidance, suppressed = iwm.runningStatusPollGuidance()
				if suppressed {
					return pollGuidance, nil
				}
			}

			if count == 0 && hasPending {
				return "No running executions, but **completions are pending delivery** — results will arrive shortly.\nDo NOT report \"all clear\".\n\n" + pollGuidance, nil
			}

			if count == 0 {
				if statusFilter != "" {
					return fmt.Sprintf("No executions with status %q. Total tracked: %d.", statusFilter, len(allExecs)), nil
				}
				if len(allExecs) == 0 {
					return "No background executions tracked in this session.", nil
				}
				return "No background executions tracked.", nil
			}

			return fmt.Sprintf("**%d execution(s)**%s:\n%s\n%s", count, func() string {
				if statusFilter != "" {
					return fmt.Sprintf(" (filter: %s)", statusFilter)
				}
				return ""
			}(), sb.String(), pollGuidance), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_executions tool: %v", err))
	}

	// Tool 3: stop_step — cancel a running step
	if err := mcpAgent.RegisterCustomTool(
		"stop_step",
		"Cancel a tracked workflow step, call_generic_agent execution, or Pulse reviewer only after query_step currently reports that exact execution_id as running. A wait timeout does not prove the execution is still running because completion notifications can be delayed. Never use this as cleanup for completed, failed, or already-cancelled work; those states are rejected.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "Exact execution_id returned by a start call or shown in a generic-agent/Pulse-reviewer start notification.",
				},
			},
			"required": []string{"execution_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			execIDRaw, ok := args["execution_id"]
			if !ok || execIDRaw == nil {
				return "execution_id is required", nil
			}
			execID, ok := execIDRaw.(string)
			if !ok || execID == "" {
				return "execution_id must be a non-empty string", nil
			}

			exec, err := iwm.stepRegistry.Cancel(execID)
			if errors.Is(err, ErrWorkshopExecutionNotFound) {
				return fmt.Sprintf("execution %q not found", execID), nil
			}
			if errors.Is(err, ErrWorkshopExecutionNotCancelable) {
				logger.Warn(fmt.Sprintf("⚠️ Workshop: step %q (execution_id=%q) has no cancel function", exec.StepID, execID))
				return fmt.Sprintf("Step %q (execution_id=%q) is tracked but cannot be cancelled individually.", exec.StepID, execID), nil
			}
			if err != nil {
				return "", err
			}

			// Notify server layer so bgAgentRegistry marks this as terminated and frontend updates
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionTerminated(execID, exec.StepID)
			}

			logger.Info(fmt.Sprintf("🛑 Workshop: step %q (execution_id=%q) cancelled", exec.StepID, execID))
			return fmt.Sprintf("Step %q (execution_id=%q) has been cancelled.", exec.StepID, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register stop_step tool: %v", err))
	}

	// Tool 3b: stop_all_executions — cancel all running background executions at once
	if err := mcpAgent.RegisterCustomTool(
		"stop_all_executions",
		"Cancel ALL running background executions (steps, learnings, optimizations, background agents). Use this when the user asks to stop, cancel, or abort everything.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			// Cancel in stepRegistry first
			cancelledExecs := iwm.stepRegistry.CancelAll()
			if iwm.executionNotifier != nil {
				for _, exec := range cancelledExecs {
					iwm.executionNotifier.OnExecutionTerminated(exec.ID, exec.StepID)
				}
			}

			// Also cancel through server's bgAgentRegistry (catches anything stepRegistry missed)
			if iwm.cancelAllServerAgents != nil {
				iwm.cancelAllServerAgents()
			}

			if len(cancelledExecs) == 0 {
				return "No running executions found to cancel.", nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Cancelled %d running execution(s):\n", len(cancelledExecs)))
			for _, exec := range cancelledExecs {
				sb.WriteString(fmt.Sprintf("- %s (step: %s)\n", exec.ID, exec.StepID))
			}
			logger.Info(fmt.Sprintf("🛑 Workshop: cancelled all %d running executions", len(cancelledExecs)))
			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register stop_all_executions tool: %v", err))
	}

	// === Builder tools: config, optimization, learning ===

	// Tool 4: update_step_config — update step_config.json for a specific step
	declaredExecutionModeEnum := []interface{}{"agentic", "scripted"}
	declaredExecutionModeDescription := "Required mode declaration for this step. Always set this intentionally so the improve pass records the final decision explicitly. Set scripted from initial design for deterministic API/SDK calls, CLI commands, data fetching, stable parsing/normalization/transforms, and mechanical persistence; no run-history threshold is required to choose scripted. Keep judgment, adaptive discovery, and browser/UI work agentic. Freezing a saved script afterwards with lock_code still requires 10+ successful representative scenario-covering runs."
	lockCodeDescription := "If true, lock the saved main.py script — prevents LLM-rewritten scripts from being saved back to learnings, and skips the fix loop (falls back directly to agentic mode). Only applies to scripted steps. Use only when the user explicitly wanted scripted, the script is deterministic, and script_metadata/eval evidence shows 10+ successful scenario-covering runs."
	if err := mcpAgent.RegisterCustomTool(
		"update_step_config",
		"Update step_config.json for a specific workflow or evaluation step. The tool auto-detects whether step_id belongs to planning/plan.json or evaluation/evaluation_plan.json, then writes planning/step_config.json or evaluation/step_config.json accordingly. Changes take effect on the next execute_step or run_full_evaluation call. To REMOVE a field (so the step falls back to preset/default behavior where that field has a fallback, or removes the explicit setting otherwise), list its name in clear_fields — sending null in a value field does NOT clear; it's ignored.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from planning/plan.json or evaluation/evaluation_plan.json.",
				},
				"clear_fields": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Field names to CLEAR (remove from step_config.json) so the step uses preset/default behavior again. Clearing enabled_skills removes explicit step skills; step execution does not inherit workflow-selected skills, so set enabled_skills explicitly when the step needs installed skills. Only fields with a corresponding setter in this tool are clearable. Valid names: execution_llm, execution_tier, servers, tools, enabled_custom_tools, enabled_skills, learning_objective, lock_learnings, lock_code, use_code_execution_mode, disable_parallel_tool_execution, coding_agent_tmux_lifecycle, transport, description_reviewed, knowledgebase_access, knowledgebase_contribution, knowledgebase_write_method, learnings_access, learnings_write_method, db_access, review_notes, declared_execution_mode, declared_execution_mode_reason, global_skill_objective, validation_schema. Unknown names are reported as errors; nothing else in the same call is applied.",
				},
				"servers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "MCP server names to use for this step",
				},
				"tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tool names to enable for this step (format: 'server:tool' or 'server:*')",
				},
				"learning_objective": map[string]interface{}{
					"type":        "string",
					"description": "Extraction instruction for the step agent's direct post-completion learning turn. Use only for reusable execution HOW: browser selectors/timing/auth flows, API/MCP request and response quirks, CLI/SDK command patterns, parsing rules, retries, recovery, or file-format pitfalls. Do not use for facts/results, report data, routing decisions, validation-only steps, mechanical transforms, human approvals, pure db/KB readers, or mature scripted steps whose main.py already captures the method. Example: 'Capture the Buffer API create-update request shape, success fields, 401/429 handling, and output id parsing.' Required when learnings_access=\"read-write\" (the validator rejects write access with an empty objective).",
				},
				"lock_learnings": map[string]interface{}{
					"type":        "boolean",
					"description": "Freeze SKILL.md writes for this step. Existing SKILL.md still flows into execution prompts. Runtime execution never auto-sets or auto-clears this field; set it only as an intentional builder/user decision, and include review_notes explaining why learning should stop. Does NOT affect saved main.py — use lock_code for that.",
				},
				"lock_code": map[string]interface{}{
					"type":        "boolean",
					"description": lockCodeDescription,
				},
				"enabled_custom_tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Workspace/custom tools to enable (format: 'category:tool' or 'category:*'). Categories: workspace_advanced (execute_shell_command, diff_patch_workspace_file, read_image, generate_text_llm, search_web_llm), human_tools (human_feedback, notify_user), workspace_browser (agent_browser). Example: ['workspace_advanced:execute_shell_command', 'workspace_advanced:diff_patch_workspace_file']",
				},
				"enabled_skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Skill folder names to enable for this step. Step execution only receives skills listed here; workflow-level selected skills are builder/workshop context and do not cascade into runtime steps. Use list_skills to see installed skills and get_workflow_config to see the workflow's currently selected skills for discovery/reference.",
				},
				"knowledgebase_access": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"read", "write", "read-write", "none"},
					"description": "Access mode for this step against knowledgebase/ (per-topic notes/ + notes/_index.json registry). Defaults to 'none' — KB is opt-in per step. 'read' — may consume existing narrative (read notes via index-first then selective cat); 'write' / 'read-write' — may contribute (writer is decided by knowledgebase_write_method: direct = normal path where the step agent writes notes/ inline with diff_patch_workspace_file, agent = separate post-step KB writer only when the user explicitly asks); 'none' — no access. Omit to keep the default.",
				},
				"learnings_access": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"read", "read-write", "none"},
					"description": "Access mode for this step against learnings/_global/ (SKILL.md + references/). Defaults to 'read' — every step sees the workflow's accumulated how-to knowledge in its prompt. 'read-write' — step contributes reusable execution HOW and requires a concrete learning_objective; use for browser/API/CLI/SDK/MCP/parsing/retry discoveries. Keep routing, validation, mechanical transform, aggregation/report-shaping, human approval, pure db/KB reader, and mature scripted steps read-only. 'none' — step neither reads global skill nor contributes; use rarely, only when shared HOW would mislead the step or token isolation is important. Omit to keep the default.",
				},
				"db_access": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"read", "read-write"},
					"description": "Access mode for this step against db/db.sqlite (and db/). Defaults to 'read-write' — every step can read AND write the workflow db (the back-compat default; db is the shared structured-state surface). Set 'read' for least-privilege read-only steps that should never mutate the db — pure readers, report-shaping/aggregation, validation/preflight checks: db/ stays readable but is removed from the step's write paths, so an accidental write is sandbox-denied. Steps always reference the db via the absolute $DB_PATH env var. Omit to keep the read-write default.",
				},
				"knowledgebase_contribution": map[string]interface{}{
					"type":        "string",
					"description": "Natural-language contribution instruction. In knowledgebase_write_method='direct', it becomes the step agent's contribution contract, injected into its post-completion self-review turn. In knowledgebase_write_method='agent', it is handed to a separate post-step KB update agent; choose agent only when the user explicitly asks for that separate reviewer/writer. KB writes only happen when this is non-empty AND knowledgebase_access grants write. Leave empty to skip KB updates for this step.",
				},
				"knowledgebase_write_method": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"agent", "direct"},
					"description": "How KB writes happen when knowledgebase_access permits them. Set 'direct' explicitly for new KB-writing steps: the step agent writes notes/ itself with diff_patch_workspace_file during execution, with an automatic post-completion self-review turn that enumerates contributions against the contract. Choose 'agent' only when the user explicitly asks for a separate post-step KB writer/reviewer. Do not choose agent merely because the output is long, messy, or analytical. If omitted, runtime fallback may be agent, so do not omit this field when enabling KB writes.",
				},
				"learnings_write_method": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"direct"},
					"description": "Optional and effectively ignored. SKILL.md writes always happen through the step agent's own dedicated post-completion turn (shell + diff_patch_workspace_file, folder guard widens only for that turn). Omit this field from new plans; if set, only 'direct' is accepted. Concurrency across parallel sub-agents is serialized by an in-process mutex.",
				},
				"disable_parallel_tool_execution": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, force the LLM to emit only one tool call per turn for this step. Use when tool calls must run strictly sequentially (e.g., stateful browser sessions, file edits with ordering dependencies, or when the agent is making mistakes by racing parallel calls). Default (omit/false) = parallel tool calls allowed. For todo_task steps, child tasks inherit this setting from the parent.",
				},
				"coding_agent_tmux_lifecycle": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"close_on_completion", "keep_alive"},
					"description": "Lifecycle for tmux-backed coding providers on this step. Default/omit is close_on_completion: the step/sub-agent gets a bounded terminal that is closed when its turn completes. Use keep_alive only when a step intentionally needs its native coding-CLI session to survive after completion for later live steering or debugging; this can leave more tmux sessions open.",
				},
				"use_code_execution_mode": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, enable code execution mode — the agent writes and executes Python/shell code via mcpbridge to interact with MCP tools, rather than calling them directly. Useful for complex data processing or programmatic control over MCP tools. If false, explicitly disables code execution. Omit to inherit the preset default.",
				},
				"declared_execution_mode": map[string]interface{}{
					"type":        "string",
					"enum":        declaredExecutionModeEnum,
					"description": declaredExecutionModeDescription,
				},
				"declared_execution_mode_reason": map[string]interface{}{
					"type":        "string",
					"description": "Audit trail: why the chosen execution mode is the best fit for this step. Not consumed by Go runtime, but preserved so future Pulse and plan-change reviewers reading step_config.json see the original rationale.",
				},
				"description_reviewed": map[string]interface{}{
					"type":        "boolean",
					"description": "True when the step description has been reviewed — covers BOTH clarity/optimization for execution AND confirmation that the description contains no secrets, hardcoded credentials, or user/run-specific values. Clear this (via clear_fields) if the description meaningfully changes.",
				},
				"review_notes": map[string]interface{}{
					"type":        "string",
					"description": "Free-form rationale covering why the config, locks, learning/KB choices, or description review state are justified. Cite concrete evidence — e.g., 'description is clear and secret-free; passed 3 groups with eval >= 9; learnings stable; pre-validation catches format regressions'. Persisted so later Pulse, plan-change, and review passes see the context.",
				},
				"execution_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the execution LLM for this step. Use get_llm_config to see available models.",
					"properties": map[string]interface{}{
						"published_llm_id": map[string]interface{}{"type": "string", "description": "Optional id from the published LLM set."},
						"provider":         map[string]interface{}{"type": "string", "description": "LLM provider (e.g., 'openai', 'anthropic', 'bedrock', 'openrouter', 'vertex', 'azure')"},
						"model_id":         map[string]interface{}{"type": "string", "description": "Model ID (e.g., 'gpt-4o', 'claude-sonnet-4-20250514')"},
						"options":          map[string]interface{}{"type": "object", "description": "Provider-specific runtime options copied from the published LLM, such as reasoning_effort.", "additionalProperties": true},
						"fallbacks": map[string]interface{}{
							"type":        "array",
							"description": "Optional ordered fallback models.",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"published_llm_id": map[string]interface{}{"type": "string"},
									"provider":         map[string]interface{}{"type": "string"},
									"model_id":         map[string]interface{}{"type": "string"},
									"options":          map[string]interface{}{"type": "object", "additionalProperties": true},
								},
							},
						},
					},
				},
				"execution_tier": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"high", "medium", "low"},
					"description": "Persistent execution tier override for this workflow step or evaluation step in tiered mode. Use high for subjective/ambiguous judgment, medium for normal checks, low for deterministic/file-shape checks. execution_llm still takes precedence, and execute_step(..., tier=...) can still override workflow/eval step tier for a single run.",
				},
				"validation_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the validation LLM for this step.",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{"type": "string"},
						"model_id": map[string]interface{}{"type": "string"},
					},
				},
				"validation_schema": map[string]interface{}{
					"type":        "object",
					"description": "Override the pre-validation schema for this step. Takes precedence over plan.json validation_schema. Defines file existence checks and JSON structure validation rules.",
					"properties": map[string]interface{}{
						"files": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"file_name":   map[string]interface{}{"type": "string", "description": "File name to validate (e.g., 'results.json')"},
									"must_exist":  map[string]interface{}{"type": "boolean", "description": "Whether the file must exist"},
									"json_checks": map[string]interface{}{"type": "array", "description": "JSON structure validation checks"},
								},
							},
						},
					},
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "REQUIRED: One-sentence rationale for why this step config is being updated. Captured into the plan changelog.",
				},
			},
			"required": []string{"step_id", "reason"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}
			reasonRaw, _ := args["reason"].(string)
			if strings.TrimSpace(reasonRaw) == "" {
				return "reason is required (one-sentence rationale captured into the plan changelog)", nil
			}

			resolvedStepID, configSubdir, isEvalStep, resolveErr := resolveWorkshopStepConfigTarget(ctx, iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}
			stepID = resolvedStepID

			// Read existing configs from the durable config file matching the target step:
			// planning/step_config.json for workflow steps, evaluation/step_config.json for eval steps.
			configs, err := iwm.controller.ReadStepConfigsFromSubdir(ctx, configSubdir)
			if err != nil {
				configs = []StepConfig{}
			}
			// Find or create entry for this step
			var targetConfig *StepConfig
			for i := range configs {
				if configs[i].ID == stepID {
					targetConfig = &configs[i]
					break
				}
			}
			if targetConfig == nil {
				configs = append(configs, StepConfig{ID: stepID})
				targetConfig = &configs[len(configs)-1]
			}
			if targetConfig.AgentConfigs == nil {
				targetConfig.AgentConfigs = &AgentConfigs{}
			}

			// Apply provided fields
			if val, ok := args["servers"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					servers := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							servers = append(servers, s)
						}
					}
					targetConfig.AgentConfigs.SelectedServers = servers
				}
			}
			if val, ok := args["tools"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					tools := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							tools = append(tools, s)
						}
					}
					targetConfig.AgentConfigs.SelectedTools = tools
				}
			}
			if val, ok := args["learning_objective"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.LearningObjective = strings.TrimSpace(s)
				}
			}
			if val, ok := args["lock_learnings"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					// Same protection: don't let accidental false overwrite a true value.
					if b || targetConfig.AgentConfigs.LockLearnings == nil || !*targetConfig.AgentConfigs.LockLearnings {
						targetConfig.AgentConfigs.LockLearnings = &b
					}
				}
			}
			if val, ok := args["lock_code"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					if b || targetConfig.AgentConfigs.LockCode == nil || !*targetConfig.AgentConfigs.LockCode {
						targetConfig.AgentConfigs.LockCode = &b
					}
				}
			}
			if val, ok := args["enabled_custom_tools"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					customTools := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							customTools = append(customTools, s)
						}
					}
					targetConfig.AgentConfigs.EnabledCustomTools = customTools
				}
			}
			if val, ok := args["enabled_skills"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					enabledSkills := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							enabledSkills = append(enabledSkills, s)
						}
					}
					targetConfig.AgentConfigs.EnabledSkills = enabledSkills
				}
			}
			if val, ok := args["knowledgebase_access"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.KnowledgebaseAccess = s
				}
			}
			if val, ok := args["knowledgebase_contribution"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.KnowledgebaseContribution = s
				}
			}
			if val, ok := args["knowledgebase_write_method"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.KnowledgebaseWriteMethod = s
				}
			}
			if val, ok := args["learnings_access"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.LearningsAccess = s
				}
			}
			if val, ok := args["learnings_write_method"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.LearningsWriteMethod = s
				}
			}
			if val, ok := args["db_access"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.DBAccess = s
				}
			}
			if val, ok := args["disable_parallel_tool_execution"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					targetConfig.AgentConfigs.DisableParallelToolExecution = &b
				}
			}
			if val, ok := args["coding_agent_tmux_lifecycle"]; ok && val != nil {
				if s, ok := val.(string); ok {
					lifecycle := strings.TrimSpace(s)
					if normalized := normalizeCodingAgentTmuxLifecycle(lifecycle); normalized != "" {
						lifecycle = normalized
					}
					targetConfig.AgentConfigs.CodingAgentTmuxLifecycle = lifecycle
				}
			}
			if val, ok := args["use_code_execution_mode"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					targetConfig.AgentConfigs.UseCodeExecutionMode = &b
				}
			}
			if val, ok := args["declared_execution_mode"]; ok && val != nil {
				if s, ok := val.(string); ok && s != "" {
					targetConfig.AgentConfigs.DeclaredExecutionMode = s
				}
			}
			if val, ok := args["declared_execution_mode_reason"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.DeclaredExecutionModeReason = strings.TrimSpace(s)
				}
			}
			if val, ok := args["description_reviewed"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					targetConfig.AgentConfigs.DescriptionReviewed = &b
				}
			}
			if val, ok := args["review_notes"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.ReviewNotes = strings.TrimSpace(s)
				}
			}
			if val, ok := args["execution_tier"]; ok && val != nil {
				if s, ok := val.(string); ok {
					targetConfig.AgentConfigs.ExecutionTier = strings.ToLower(strings.TrimSpace(s))
				}
			}

			// If the caller declared a mode, sync the low-level mode flags to match it.
			syncDeclaredExecutionModeConfig(targetConfig.AgentConfigs)

			// Parse LLM override fields
			parseLLMFallbacks := func(raw interface{}) []AgentLLMFallback {
				arr, ok := raw.([]interface{})
				if !ok {
					return nil
				}
				fallbacks := make([]AgentLLMFallback, 0, len(arr))
				for _, item := range arr {
					m, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					provider, _ := m["provider"].(string)
					modelID, _ := m["model_id"].(string)
					if provider == "" || modelID == "" {
						continue
					}
					publishedLLMID, _ := m["published_llm_id"].(string)
					options, _ := m["options"].(map[string]interface{})
					fallbacks = append(fallbacks, AgentLLMFallback{
						PublishedLLMID: publishedLLMID,
						Provider:       provider,
						ModelID:        modelID,
						Options:        options,
					})
				}
				return fallbacks
			}
			llmFields := []struct {
				key    string
				target **AgentLLMConfig
			}{
				{"execution_llm", &targetConfig.AgentConfigs.ExecutionLLM},
			}
			for _, f := range llmFields {
				if val, ok := args[f.key]; ok && val != nil {
					if llmMap, ok := val.(map[string]interface{}); ok {
						provider, _ := llmMap["provider"].(string)
						modelID, _ := llmMap["model_id"].(string)
						if provider != "" && modelID != "" {
							publishedLLMID, _ := llmMap["published_llm_id"].(string)
							options, _ := llmMap["options"].(map[string]interface{})
							*f.target = &AgentLLMConfig{
								PublishedLLMID: publishedLLMID,
								Provider:       provider,
								ModelID:        modelID,
								Options:        options,
								Fallbacks:      parseLLMFallbacks(llmMap["fallbacks"]),
							}
						}
					}
				}
			}

			// Parse validation_schema override
			if val, ok := args["validation_schema"]; ok && val != nil {
				// Marshal back to JSON and unmarshal into ValidationSchema struct
				vsJSON, jsonErr := json.Marshal(val)
				if jsonErr == nil {
					var vs ValidationSchema
					if jsonErr := json.Unmarshal(vsJSON, &vs); jsonErr == nil {
						targetConfig.ValidationSchema = &vs
						logger.Info(fmt.Sprintf("🔧 Step config for %q: validation_schema updated (%d file rules)", stepID, len(vs.Files)))
					} else {
						logger.Warn(fmt.Sprintf("⚠️ Failed to parse validation_schema for step %q: %v", stepID, jsonErr))
					}
				}
			}

			// Apply clear_fields LAST so explicit clears override any sets in the same call.
			// Writing null to a setter field is a no-op by design (LLMs send false/nil for
			// unrelated booleans); clear_fields is the explicit opt-in for removal.
			var clearedFields []string
			var unknownClearFields []string
			if rawClear, ok := args["clear_fields"]; ok && rawClear != nil {
				if arr, ok := rawClear.([]interface{}); ok {
					for _, v := range arr {
						name, ok := v.(string)
						if !ok || name == "" {
							continue
						}
						if clearStepConfigField(targetConfig, name) {
							clearedFields = append(clearedFields, name)
						} else {
							unknownClearFields = append(unknownClearFields, name)
						}
					}
				}
			}
			if len(unknownClearFields) > 0 {
				return fmt.Sprintf("unknown clear_fields entries %v — no changes applied. Valid names are listed in the tool's clear_fields description.", unknownClearFields), nil
			}

			// --- Code-level validations ---
			// Collect errors (block save) and warnings (save but inform).
			var errors []string
			warnings := make([]string, 0)

			// 1. Validate step ID exists in the plan.
			// Refresh from disk first so steps just added by other plan-mod tools in the
			// same turn (e.g. add_todo_task_route on a nested parent) are visible — the
			// controller's approvedPlan cache is otherwise stale until the next reload.
			if _, _, _, resolveErr := resolveWorkshopStepConfigTarget(ctx, iwm.controller, stepID); resolveErr != nil {
				targetFile := "planning/plan.json"
				if isEvalStep {
					targetFile = "evaluation/evaluation_plan.json"
				}
				errors = append(errors, fmt.Sprintf("Step ID %q not found in %s.", stepID, targetFile))
			}

			// 2. Validate servers exist in workflow-level selection
			workflowServers := iwm.controller.GetSelectedServers()
			workflowServerSet := make(map[string]bool, len(workflowServers))
			for _, s := range workflowServers {
				workflowServerSet[s] = true
			}
			if len(targetConfig.AgentConfigs.SelectedServers) > 0 {
				var badServers []string
				for _, s := range targetConfig.AgentConfigs.SelectedServers {
					if !workflowServerSet[s] {
						badServers = append(badServers, s)
					}
				}
				if len(badServers) > 0 {
					errors = append(errors, fmt.Sprintf("Servers %v are NOT in the workflow-level selection %v and will be IGNORED at execution time. Remove them or add them to the workflow first.", badServers, workflowServers))
				}
			}

			// 3. Validate selected_tools format (should be "server:tool" or "server:*")
			if len(targetConfig.AgentConfigs.SelectedTools) > 0 {
				for _, t := range targetConfig.AgentConfigs.SelectedTools {
					if !strings.Contains(t, ":") {
						errors = append(errors, fmt.Sprintf("Tool %q is missing server prefix. Expected format: 'server:tool_name' or 'server:*'.", t))
					}
				}
			}

			// 4. Validate tools reference servers that are selected
			if len(targetConfig.AgentConfigs.SelectedTools) > 0 && len(targetConfig.AgentConfigs.SelectedServers) > 0 {
				stepServerSet := make(map[string]bool, len(targetConfig.AgentConfigs.SelectedServers))
				for _, s := range targetConfig.AgentConfigs.SelectedServers {
					stepServerSet[s] = true
				}
				for _, t := range targetConfig.AgentConfigs.SelectedTools {
					if idx := strings.Index(t, ":"); idx >= 0 {
						serverPart := t[:idx]
						if !stepServerSet[serverPart] {
							errors = append(errors, fmt.Sprintf("Tool %q references server %q which is not in selected_servers %v. Add the server or remove the tool.", t, serverPart, targetConfig.AgentConfigs.SelectedServers))
						}
					}
				}
			}

			// 5. Validate enabled_custom_tools format and categories
			validCustomCategories := map[string]bool{
				"workspace_advanced": true,
				"human_tools":        true,
				"workspace_browser":  true,
			}
			if len(targetConfig.AgentConfigs.EnabledCustomTools) > 0 {
				for _, t := range targetConfig.AgentConfigs.EnabledCustomTools {
					if idx := strings.Index(t, ":"); idx >= 0 {
						cat := t[:idx]
						if !validCustomCategories[cat] {
							errors = append(errors, fmt.Sprintf("Custom tool %q uses unknown category %q. Valid categories: workspace_advanced, human_tools, workspace_browser.", t, cat))
						}
					} else {
						errors = append(errors, fmt.Sprintf("Custom tool %q is missing category prefix. Expected format: 'category:tool_name' or 'category:*'.", t))
					}
				}

				// 5b. Ensure required workspace tools are present (execute_shell_command, diff_patch_workspace_file)
				existingSet := make(map[string]bool, len(targetConfig.AgentConfigs.EnabledCustomTools))
				for _, t := range targetConfig.AgentConfigs.EnabledCustomTools {
					existingSet[t] = true
				}
				if !existingSet["workspace_advanced:*"] {
					required := map[string]string{
						"workspace_advanced:execute_shell_command":     "execute_shell_command",
						"workspace_advanced:diff_patch_workspace_file": "diff_patch_workspace_file",
					}
					var missing []string
					for key, name := range required {
						if !existingSet[key] {
							missing = append(missing, name)
						}
					}
					if len(missing) > 0 {
						errors = append(errors, fmt.Sprintf("Required workspace tools missing from enabled_custom_tools: %v. These are essential for every step (file operations, script execution). Add them as 'workspace_advanced:<tool_name>' or use 'workspace_advanced:*' to include all.", missing))
					}
				}
			}

			// 6. Validate learning config consistency.
			// Learnings access ↔ objective consistency. Mirror of the KB access ↔
			// contribution rule below: write-capable access is meaningless without an
			// extraction instruction for the direct post-completion learning turn.
			learningsAccessRaw := strings.TrimSpace(targetConfig.AgentConfigs.LearningsAccess)
			if learningsAccessRaw != "" {
				validLearningsModes := map[string]bool{
					LearningsAccessRead: true, LearningsAccessReadWrite: true, LearningsAccessNone: true,
				}
				if !validLearningsModes[learningsAccessRaw] {
					errors = append(errors, fmt.Sprintf("learnings_access %q is not recognized. Valid values: \"read\", \"read-write\", \"none\".", learningsAccessRaw))
				}
			}
			hasObjective := strings.TrimSpace(targetConfig.AgentConfigs.LearningObjective) != ""
			effectiveAccess := resolveLearningsAccess(targetConfig.AgentConfigs)
			if effectiveAccess == LearningsAccessReadWrite && !hasObjective {
				errors = append(errors, "learnings_access=\"read-write\" requires a non-empty learning_objective. The direct learnings turn needs an extraction instruction; set learning_objective or drop access to \"read\"/\"none\".")
			}
			isLocked := targetConfig.AgentConfigs.LockLearnings != nil && *targetConfig.AgentConfigs.LockLearnings
			if !hasObjective && isLocked {
				errors = append(errors, "lock_learnings=true requires a non-empty learning_objective. Locking a step with no objective means learning never ran; set learning_objective first or unlock.")
			}

			// 6b. Validate execution_tier.
			if rawExecutionTier := strings.TrimSpace(targetConfig.AgentConfigs.ExecutionTier); rawExecutionTier != "" {
				if NormalizeTierOverride(rawExecutionTier) == "" {
					errors = append(errors, fmt.Sprintf("execution_tier %q is not recognized. Valid values: \"high\", \"medium\", \"low\".", rawExecutionTier))
				}
				if targetConfig.AgentConfigs.ExecutionLLM != nil {
					warnings = append(warnings, "execution_tier is set but execution_llm takes precedence, so the tier override will be ignored until execution_llm is cleared.")
				}
			}
			if rawLifecycle := strings.TrimSpace(targetConfig.AgentConfigs.CodingAgentTmuxLifecycle); rawLifecycle != "" {
				if normalizeCodingAgentTmuxLifecycle(rawLifecycle) == "" {
					errors = append(errors, fmt.Sprintf("coding_agent_tmux_lifecycle %q is not recognized. Valid values: \"close_on_completion\", \"keep_alive\".", rawLifecycle))
				}
			}
			// 7. Validate KB access ↔ contribution consistency.
			// When knowledgebase_access grants write, knowledgebase_contribution MUST be
			// non-empty — otherwise the post-step KB update agent is silently skipped
			// (controller_kb_update.go:33 gates on a non-empty contribution). Mirror of
			// the learning rule: opting in to the write-capable access is meaningless
			// without an extraction instruction for the KB agent to act on.
			if kbAccessAllowsWrite(targetConfig.AgentConfigs.KnowledgebaseAccess) &&
				strings.TrimSpace(targetConfig.AgentConfigs.KnowledgebaseContribution) == "" {
				errors = append(errors, fmt.Sprintf("knowledgebase_access=%q requires a non-empty knowledgebase_contribution. Write access without an extraction instruction means the post-step KB update agent never runs; set knowledgebase_contribution or drop access to \"read\"/\"none\".", targetConfig.AgentConfigs.KnowledgebaseAccess))
			}

			// 8. Validate KB write-method enum + direct-mode pairing: direct write
			// needs access permitting writes AND a non-empty contribution string
			// (which becomes the self-review turn's contract).
			kbWriteMethodRaw := strings.TrimSpace(targetConfig.AgentConfigs.KnowledgebaseWriteMethod)
			if kbWriteMethodRaw != "" && kbWriteMethodRaw != KBWriteMethodAgent && kbWriteMethodRaw != KBWriteMethodDirect {
				errors = append(errors, fmt.Sprintf("knowledgebase_write_method %q is not recognized. Valid values: \"agent\", \"direct\".", kbWriteMethodRaw))
			}
			if kbWriteMethodRaw == KBWriteMethodDirect {
				if !kbAccessAllowsWrite(targetConfig.AgentConfigs.KnowledgebaseAccess) {
					errors = append(errors, fmt.Sprintf("knowledgebase_write_method=\"direct\" requires knowledgebase_access that permits writes (\"write\" or \"read-write\"). Current value: %q.", targetConfig.AgentConfigs.KnowledgebaseAccess))
				}
				if strings.TrimSpace(targetConfig.AgentConfigs.KnowledgebaseContribution) == "" {
					errors = append(errors, "knowledgebase_write_method=\"direct\" requires a non-empty knowledgebase_contribution. The contribution becomes the step agent's contract for the automatic post-completion self-review turn; without it the self-review has nothing to verify against.")
				}
			}

			// Nudge: write access + a contribution are set but write-method is
			// omitted, so runtime silently falls back to the separate post-step
			// "agent" writer. Surface it (non-blocking) so the inline ("direct")
			// vs separate ("agent") writer choice is explicit, not defaulted.
			if kbWriteMethodRaw == "" &&
				kbAccessAllowsWrite(targetConfig.AgentConfigs.KnowledgebaseAccess) &&
				strings.TrimSpace(targetConfig.AgentConfigs.KnowledgebaseContribution) != "" {
				warnings = append(warnings, "knowledgebase_write_method is unset while KB write access is granted; runtime defaults to the separate post-step \"agent\" writer. Set it to \"direct\" for inline writes by the step agent, or \"agent\" to make that choice explicit.")
			}

			// 9. Validate learnings write-method enum + direct-mode pairing.
			// SKILL.md writes always happen through the step agent's own
			// post-completion turn (resolveLearningsWriteMethod is hardcoded to
			// direct). The legacy "agent" value still parses cleanly — it is
			// silently coerced to direct so old plan.json files keep loading.
			// The access + objective gates below are the actual write contract.
			learningsWriteMethodRaw := strings.TrimSpace(targetConfig.AgentConfigs.LearningsWriteMethod)
			if learningsWriteMethodRaw != "" && learningsWriteMethodRaw != LearnWriteMethodAgent && learningsWriteMethodRaw != LearnWriteMethodDirect {
				errors = append(errors, fmt.Sprintf("learnings_write_method %q is not recognized. Omit the field or set it to \"direct\".", learningsWriteMethodRaw))
			}
			if effectiveAccess == LearningsAccessReadWrite {
				if !hasObjective {
					errors = append(errors, "learnings_access=\"read-write\" requires a non-empty learning_objective. The objective is injected into the step's dedicated learnings turn as the SKILL.md contribution contract; without it the turn has nothing to instruct.")
				}
			}

			// If there are errors, reject the update and return feedback
			if len(errors) > 0 {
				result := fmt.Sprintf("❌ Step config for %q was NOT saved due to validation errors:\n", stepID)
				for i, e := range errors {
					result += fmt.Sprintf("\n%d. %s", i+1, e)
				}
				result += "\n\nFix the errors above and try again."
				if len(warnings) > 0 {
					result += "\n\nAlso note these warnings:"
					for i, w := range warnings {
						result += fmt.Sprintf("\n%d. %s", i+1, w)
					}
				}
				return result, nil
			}

			cleanupStaleMainPy := targetConfig.AgentConfigs != nil &&
				canonicalDeclaredExecutionMode(targetConfig.AgentConfigs.DeclaredExecutionMode) == "agentic"

			// Write updated configs back to the matching durable config file.
			if err := iwm.controller.WriteStepConfigsToSubdir(ctx, configSubdir, configs); err != nil {
				return fmt.Sprintf("Failed to update step config: %v", err), nil
			}

			workspacePath := iwm.controller.GetWorkspacePath()
			cleanupMessages := make([]string, 0, 1)
			if cleanupStaleMainPy {
				mainPyRelPath := fmt.Sprintf("learnings/%s/main.py", stepID)
				if exists, existsErr := iwm.controller.CheckWorkspaceFileExists(ctx, mainPyRelPath); existsErr == nil && exists {
					if deleteErr := iwm.controller.DeleteWorkspaceFile(ctx, mainPyRelPath); deleteErr != nil {
						warnings = append(warnings, fmt.Sprintf("Declared agentic but failed to delete stale %s: %v", mainPyRelPath, deleteErr))
					} else {
						cleanupMessages = append(cleanupMessages, fmt.Sprintf("Deleted stale %s because agentic steps do not keep persistent main.py.", mainPyRelPath))
						logger.Info(fmt.Sprintf("🧹 Workshop: deleted stale main.py for agentic step %q", stepID))
					}
				} else if existsErr != nil {
					warnings = append(warnings, fmt.Sprintf("Declared agentic but could not check stale learnings/%s/main.py: %v", stepID, existsErr))
				}
			}
			if err := writePlanChangelogEntry(ctx, workspacePath, PlanChangelogEntry{
				Tool:    "update_step_config",
				Reason:  strings.TrimSpace(reasonRaw),
				StepIDs: []string{stepID},
			}, iwm.controller.ReadWorkspaceFile, iwm.controller.WriteWorkspaceFile, logger); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Plan changelog write failed (non-fatal): %v", err))
			}

			logger.Info(fmt.Sprintf("📝 Workshop: step config updated for step %q in %s/step_config.json", stepID, configSubdir))
			configPath := configSubdir + "/step_config.json"
			if isEvalStep {
				configPath = "evaluation/step_config.json"
			}
			result := fmt.Sprintf("Step config for %q updated successfully in %s. Changes will take effect on the next execute_step/run_full_evaluation call.", stepID, configPath)
			if len(cleanupMessages) > 0 {
				result += "\n\nCleanup:"
				for _, msg := range cleanupMessages {
					result += "\n- " + msg
				}
			}
			if len(warnings) > 0 {
				result += "\n\n⚠️ WARNINGS:"
				for i, w := range warnings {
					result += fmt.Sprintf("\n%d. %s", i+1, w)
				}
			}
			return result, nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_step_config tool: %v", err))
	}

	// Tool 5: get_step_prompts — read saved system prompt + user message for a step run
	if err := mcpAgent.RegisterCustomTool(
		"get_step_prompts",
		"Get the system prompt and user message for a step. Works both during execution (prompts saved at start) and after completion. Useful for debugging what instructions the agent received. For sub-agent steps, pass the inner step ID directly (e.g., 'step-icici-login') or use route_id with the parent step.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or inner step ID (e.g., 'step-icici-login')",
				},
				"route_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional route ID for sub-agent steps (e.g., 'icici-login'). When provided with a parent step_id, looks up logs at step-{N}-sub-{route_id}/",
				},
				"attempt": map[string]interface{}{
					"type":        "integer",
					"description": "Retry attempt number (default: 1)",
				},
				"iteration": map[string]interface{}{
					"type":        "integer",
					"description": "Loop iteration number (default: 0)",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			routeID, _ := args["route_id"].(string)

			// Ensure plan is loaded (best-effort — for get_step_prompts we only need step index)
			if iwm.controller.approvedPlan == nil {
				_ = iwm.controller.LoadPlanForWorkshop(ctx) // ignore error; nil check below handles failure
			}
			if iwm.controller.approvedPlan == nil {
				return "no plan loaded; run execute_step first or ensure plan.json exists", nil
			}

			// Resolve flexible step_id
			resolvedForPrompts, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			// Find step info (handles both top-level and inner steps)
			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedForPrompts)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}

			attempt := 1
			if v, ok := args["attempt"]; ok && v != nil {
				if f, ok := v.(float64); ok {
					attempt = int(f)
				}
			}
			iteration := 0
			if v, ok := args["iteration"]; ok && v != nil {
				if f, ok := v.(float64); ok {
					iteration = int(f)
				}
			}

			runFolder := iwm.controller.selectedRunFolder
			if runFolder == "" {
				return "no run folder selected", nil
			}

			// Determine the correct log path based on step type
			var stepPath string
			if routeID != "" && stepInfo.TopIndex > 0 {
				// Generated route executions retain their historical composite folder name.
				stepPath = fmt.Sprintf("step-%d-sub-%s", stepInfo.TopIndex, routeID)
			} else {
				stepPath = workshopStepLogFolder(resolvedForPrompts)
			}
			logDir := fmt.Sprintf("runs/%s/logs/%s/execution", runFolder, stepPath)
			filenameBase := fmt.Sprintf("execution-attempt-%d-iteration-%d", attempt, iteration)

			var result strings.Builder
			hasUserMessage := false // Track if user message was already included from prompts.json

			// Read system prompt and user message from prompts.json (saved pre-execution and updated post-execution).
			// Todo-task orchestrators use a stable non-attempt filename.
			promptsPath := fmt.Sprintf("%s/%s-prompts.json", logDir, filenameBase)
			promptsContent, err := iwm.controller.ReadWorkspaceFile(ctx, promptsPath)
			if err != nil {
				altPath := fmt.Sprintf("%s/todo-task-prompts.json", logDir)
				if tc, te := iwm.controller.ReadWorkspaceFile(ctx, altPath); te == nil {
					promptsContent = tc
					err = nil
					promptsPath = altPath
				}
			}
			if err != nil {
				result.WriteString(fmt.Sprintf("⚠️ Prompts file not found (%s).\nNote: only available for runs after this feature was added.\n\n", promptsPath))
			} else {
				var promptsData map[string]interface{}
				if jsonErr := workshopJSONUnmarshal([]byte(promptsContent), &promptsData); jsonErr == nil {
					// Show when the prompt was saved (pre_execution = still running, post_execution = completed)
					if savedAt, ok := promptsData["saved_at"].(string); ok {
						result.WriteString(fmt.Sprintf("**Prompt saved at**: %s\n", savedAt))
					}
					if model, ok := promptsData["model"].(string); ok && model != "" {
						result.WriteString(fmt.Sprintf("**Model**: %s\n\n", model))
					}
					result.WriteString("## System Prompt\n\n")
					if sp, ok := promptsData["system_prompt"].(string); ok {
						result.WriteString(sp)
					} else {
						result.WriteString("(system_prompt field missing)")
					}
					result.WriteString("\n\n")
					// User message from prompts.json (available from pre-execution save)
					if um, ok := promptsData["user_message"].(string); ok && um != "" {
						result.WriteString("## User Message\n\n")
						result.WriteString(um)
						result.WriteString("\n\n")
						hasUserMessage = true
					}
				} else {
					result.WriteString("## System Prompt\n\n")
					result.WriteString(promptsContent)
					result.WriteString("\n\n")
				}
			}

			// Read user message from conversation.json (first human message in history)
			// Skip if user message was already included from prompts.json
			if hasUserMessage {
				return result.String(), nil
			}
			convPath := fmt.Sprintf("%s/%s-conversation.json", logDir, filenameBase)
			convContent, err := iwm.controller.ReadWorkspaceFile(ctx, convPath)
			if err != nil {
				result.WriteString(fmt.Sprintf("⚠️ Conversation file not found (%s): %v\n", convPath, err))
			} else {
				result.WriteString("## User Message (first turn)\n\n")
				var convData map[string]interface{}
				if jsonErr := workshopJSONUnmarshal([]byte(convContent), &convData); jsonErr == nil {
					if history, ok := convData["conversation_history"].([]interface{}); ok && len(history) > 0 {
						// Find the first human message (skip system message at index 0).
						// Role field is PascalCase ("Role") since MessageContent has no JSON tags.
						var humanMsg map[string]interface{}
						for _, msg := range history {
							m, ok := msg.(map[string]interface{})
							if !ok {
								continue
							}
							role, _ := m["Role"].(string)
							if role == "" {
								role, _ = m["role"].(string)
							}
							if role == "human" || role == "Human" || role == "user" {
								humanMsg = m
								break
							}
						}
						if humanMsg != nil {
							parts, ok := humanMsg["Parts"].([]interface{})
							if !ok {
								parts, ok = humanMsg["parts"].([]interface{})
							}
							if ok {
								for _, part := range parts {
									if partMap, ok := part.(map[string]interface{}); ok {
										text, found := partMap["Text"].(string)
										if !found {
											text, found = partMap["text"].(string)
										}
										if found {
											result.WriteString(text)
											break
										}
									}
								}
							}
						} else {
							result.WriteString("(no human message found in conversation history)")
						}
					} else {
						result.WriteString("(conversation history empty or unparseable)")
					}
				} else {
					result.WriteString(fmt.Sprintf("(could not parse conversation JSON: %v)", jsonErr))
				}
			}

			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_step_prompts tool: %v", err))
	}

	// === Tools: analyze, learn, optimize, background tasks ===

	// Tool 7f: review_plan — background agent that critically reviews current workflow design and artifacts
	if err := mcpAgent.RegisterCustomTool(
		"review_plan",
		"Start a background agent that critically reviews the current workflow design and dependent artifacts: plan structure, step descriptions, context flow, validation, learnings, saved scripts, knowledgebase notes, db/db.sqlite table contracts, report wiring, evaluation coverage, portability, and whether decisions are justified by objective, success criteria, and optional run evidence. Read-only. Returns execution_id immediately — you will be automatically notified when it completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus for the review, e.g., 'step boundaries', 'mode decisions', 'context flow', 'hardcoded values', 'learnings', 'knowledgebase', 'db schema', 'report wiring', 'evaluation coverage'.",
				},
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Optional iteration folder (e.g., 'iteration-3'). If omitted, the review stays plan-centric unless a run folder is already selected.",
				},
				"group_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional group/user subfolder within the iteration (e.g., 'saurabh'). Use together with iteration for grouped workflows.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			focus := ""
			if val, ok := args["focus"]; ok && val != nil {
				if s, ok := val.(string); ok {
					focus = s
				}
			}
			targetRunFolder := ""
			if iter, ok := args["iteration"]; ok && iter != nil {
				if s, ok := iter.(string); ok && strings.TrimSpace(s) != "" {
					targetRunFolder = strings.TrimSpace(s)
					if gid, ok := args["group_name"]; ok && gid != nil {
						if g, ok := gid.(string); ok && strings.TrimSpace(g) != "" {
							targetRunFolder += "/" + strings.TrimSpace(g)
						}
					}
				}
			}
			if targetRunFolder == "" {
				targetRunFolder = strings.TrimSpace(iwm.controller.selectedRunFolder)
			}

			execID := fmt.Sprintf("review-plan-%05d", time.Now().UnixNano()%100000)
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			agentSessionID := fmt.Sprintf("workshop-review-plan-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         "review-plan",
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                execID,
					ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
					Name:              "Review Workflow Plan",
					Cancel:            cancel,
				})
			}

			go func() {
				var result string
				var execErr error
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if !skipNotify && iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, "Review Workflow Plan", result, nil, execErr)
					}
				}()

				result, execErr = iwm.runReviewPlanAgent(execCtx, targetRunFolder, focus)
			}()

			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			runInfo := ""
			if targetRunFolder != "" {
				runInfo = fmt.Sprintf("\nTarget run folder: %s", targetRunFolder)
			}
			logger.Info(fmt.Sprintf("🧪 Workshop: review_plan agent started in background, execution_id=%q, target_run_folder=%q", execID, targetRunFolder))
			return fmt.Sprintf("Workflow plan review agent started in background.\nexecution_id: %q%s%s\nYou will be automatically notified when it completes.", execID, runInfo, focusInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register review_plan tool: %v", err))
	}

	// Tool 7f3: review_workflow_timing — read-only review of runtime latency and speedup opportunities
	if err := mcpAgent.RegisterCustomTool(
		"review_workflow_timing",
		"Start a background agent that reviews workflow runtime and step timing from actual run evidence, identifies the main latency bottlenecks, and recommends how to make the workflow faster without compromising the objective or success criteria. Read-only. Returns execution_id immediately — you will be automatically notified when it completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus for the review, e.g., 'slowest step', 'tool latency', 'too many steps', 'merge opportunities', 'description ambiguity'.",
				},
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Optional iteration folder (e.g., 'iteration-3'). If omitted, the selected run folder is used when present; otherwise the reviewer finds the latest meaningful run evidence.",
				},
				"group_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional group/user subfolder within the iteration (e.g., 'saurabh'). Use together with iteration for grouped workflows.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			focus := ""
			if val, ok := args["focus"]; ok && val != nil {
				if s, ok := val.(string); ok {
					focus = s
				}
			}
			targetRunFolder := ""
			if iter, ok := args["iteration"]; ok && iter != nil {
				if s, ok := iter.(string); ok && strings.TrimSpace(s) != "" {
					targetRunFolder = strings.TrimSpace(s)
					if gid, ok := args["group_name"]; ok && gid != nil {
						if g, ok := gid.(string); ok && strings.TrimSpace(g) != "" {
							targetRunFolder += "/" + strings.TrimSpace(g)
						}
					}
				}
			}
			if targetRunFolder == "" {
				targetRunFolder = strings.TrimSpace(iwm.controller.selectedRunFolder)
			}

			execID := fmt.Sprintf("review-timing-%05d", time.Now().UnixNano()%100000)
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			agentSessionID := fmt.Sprintf("workshop-review-timing-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         "review-workflow-timing",
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                execID,
					ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
					Name:              "Review Workflow Timing",
					Cancel:            cancel,
				})
			}

			go func() {
				var result string
				var execErr error
				eventBridge := iwm.controller.GetContextAwareBridge()
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						isCancelled := skipNotify || execCtx.Err() != nil
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-review-timing",
							AgentName:     "Review Workflow Timing",
							Success:       execErr == nil,
						}
						if execErr != nil {
							if isCancelled {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = result
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type: orchestrator_events.OrchestratorAgentEnd, Timestamp: time.Now(),
							Data: endEvent, CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, "Review Workflow Timing", result, nil, execErr)
					}
				}()

				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-review-timing",
						AgentName:     "Review Workflow Timing",
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type: orchestrator_events.OrchestratorAgentStart, Timestamp: time.Now(),
						Data: startEvent, CorrelationID: agentSessionID,
					})
				}

				result, execErr = iwm.runReviewWorkflowTimingAgent(execCtx, targetRunFolder, focus)
			}()

			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			runInfo := ""
			if targetRunFolder != "" {
				runInfo = fmt.Sprintf("\nTarget run folder: %s", targetRunFolder)
			}
			logger.Info(fmt.Sprintf("⏱️ Workshop: review_workflow_timing agent started in background, execution_id=%q, target_run_folder=%q", execID, targetRunFolder))
			return fmt.Sprintf("Workflow timing review agent started in background.\nexecution_id: %q%s%s\nYou will be automatically notified when it completes.", execID, runInfo, focusInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register review_workflow_timing tool: %v", err))
	}

	// Tool 7f4: review_workflow_costs — read-only review of cost drivers and reduction opportunities
	if err := mcpAgent.RegisterCustomTool(
		"review_workflow_costs",
		"Start a background agent that reviews workflow token/cost data from actual run evidence, identifies the biggest cost drivers, and recommends how to reduce cost without compromising the objective or success criteria. Read-only. Returns execution_id immediately — you will be automatically notified when it completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus for the review, e.g., 'expensive step', 'model tier', 'retry waste', 'evaluation cost', 'merge opportunities'.",
				},
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Optional iteration folder (e.g., 'iteration-3'). If omitted, the selected run folder is used when present; otherwise the reviewer finds the latest meaningful cost/run evidence.",
				},
				"group_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional group/user subfolder within the iteration (e.g., 'saurabh'). Use together with iteration for grouped workflows.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			focus := ""
			if val, ok := args["focus"]; ok && val != nil {
				if s, ok := val.(string); ok {
					focus = s
				}
			}
			targetRunFolder := ""
			if iter, ok := args["iteration"]; ok && iter != nil {
				if s, ok := iter.(string); ok && strings.TrimSpace(s) != "" {
					targetRunFolder = strings.TrimSpace(s)
					if gid, ok := args["group_name"]; ok && gid != nil {
						if g, ok := gid.(string); ok && strings.TrimSpace(g) != "" {
							targetRunFolder += "/" + strings.TrimSpace(g)
						}
					}
				}
			}
			if targetRunFolder == "" {
				targetRunFolder = strings.TrimSpace(iwm.controller.selectedRunFolder)
			}

			execID := fmt.Sprintf("review-costs-%05d", time.Now().UnixNano()%100000)
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			agentSessionID := fmt.Sprintf("workshop-review-costs-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         "review-workflow-costs",
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                execID,
					ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
					Name:              "Review Workflow Costs",
					Cancel:            cancel,
				})
			}

			go func() {
				var result string
				var execErr error
				eventBridge := iwm.controller.GetContextAwareBridge()
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						isCancelled := skipNotify || execCtx.Err() != nil
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-review-costs",
							AgentName:     "Review Workflow Costs",
							Success:       execErr == nil,
						}
						if execErr != nil {
							if isCancelled {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = result
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type: orchestrator_events.OrchestratorAgentEnd, Timestamp: time.Now(),
							Data: endEvent, CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, "Review Workflow Costs", result, nil, execErr)
					}
				}()

				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-review-costs",
						AgentName:     "Review Workflow Costs",
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type: orchestrator_events.OrchestratorAgentStart, Timestamp: time.Now(),
						Data: startEvent, CorrelationID: agentSessionID,
					})
				}

				result, execErr = iwm.runReviewWorkflowCostsAgent(execCtx, targetRunFolder, focus)
			}()

			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			runInfo := ""
			if targetRunFolder != "" {
				runInfo = fmt.Sprintf("\nTarget run folder: %s", targetRunFolder)
			}
			logger.Info(fmt.Sprintf("💸 Workshop: review_workflow_costs agent started in background, execution_id=%q, target_run_folder=%q", execID, targetRunFolder))
			return fmt.Sprintf("Workflow cost review agent started in background.\nexecution_id: %q%s%s\nYou will be automatically notified when it completes.", execID, runInfo, focusInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register review_workflow_costs tool: %v", err))
	}

	// Tool: review_step_code — background agent that checks if saved scripts match step descriptions
	if err := mcpAgent.RegisterCustomTool(
		"review_step_code",
		"Start a background agent that compares saved main.py scripts with current descriptions to detect drift. Reviews workflow steps and evaluation steps. Over time, descriptions get updated but scripts don't — this tool finds where they've gone out of sync. Read-only. Returns execution_id immediately — you will be automatically notified when it completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional workflow step ID or evaluation step ID to review. If omitted, reviews all saved main.py scripts across workflow and evaluation code.",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus for the review, e.g., 'output format', 'missing features', 'hardcoded values', 'portability'.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepID := ""
			if val, ok := args["step_id"]; ok && val != nil {
				if s, ok := val.(string); ok {
					stepID = strings.TrimSpace(s)
				}
			}
			focus := ""
			if val, ok := args["focus"]; ok && val != nil {
				if s, ok := val.(string); ok {
					focus = s
				}
			}

			execID := fmt.Sprintf("review-step-code-%05d", time.Now().UnixNano()%100000)
			execCtx, cancel, ctxErr := iwm.newExecContext()
			if ctxErr != nil {
				return "Session was stopped — execution skipped", nil
			}

			agentSessionID := fmt.Sprintf("workshop-review-step-code-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         "review-step-code",
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(WorkshopExecutionStart{
					ID:                execID,
					ParentExecutionID: currentWorkshopParentExecutionID(execCtx),
					Name:              "Review Step Code",
					Cancel:            cancel,
				})
			}

			go func() {
				var result string
				var execErr error
				eventBridge := iwm.controller.GetContextAwareBridge()
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						isCancelled := skipNotify || execCtx.Err() != nil
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-review-step-code",
							AgentName:     "Review Step Code",
							Success:       execErr == nil,
						}
						if execErr != nil {
							if isCancelled {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = result
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type: orchestrator_events.OrchestratorAgentEnd, Timestamp: time.Now(),
							Data: endEvent, CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, "Review Step Code", result, nil, execErr)
					}
				}()

				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-review-step-code",
						AgentName:     "Review Step Code",
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type: orchestrator_events.OrchestratorAgentStart, Timestamp: time.Now(),
						Data: startEvent, CorrelationID: agentSessionID,
					})
				}

				result, execErr = iwm.runReviewStepCodeAgent(execCtx, stepID, focus)
			}()

			stepInfo := ""
			if stepID != "" {
				stepInfo = fmt.Sprintf("\nStep: %s", stepID)
			} else {
				stepInfo = "\nScope: all saved main.py scripts across workflow, evaluation, and scoring"
			}
			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			logger.Info(fmt.Sprintf("🔍 Workshop: review_step_code agent started in background, execution_id=%q, step_id=%q", execID, stepID))
			return fmt.Sprintf("Step code review agent started in background.\nexecution_id: %q%s%s\nYou will be automatically notified when it completes.", execID, stepInfo, focusInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register review_step_code tool: %v", err))
	}

	// Tool 8: get_cost_summary — parse token_usage.json and show formatted cost breakdown
	registerGetCostSummaryTool(iwm, mcpAgent, logger)

	// === Tools: background tasks, LLM config, workflow config ===

	// Tool: get_llm_config — show current LLM configuration (read-only)
	if err := mcpAgent.RegisterCustomTool(
		"get_llm_config",
		"Show the current LLM configuration for the workflow: tiered config (tier 1/2/3 with fallbacks), phase LLM, and any per-step LLM overrides from step_config.json.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var sb strings.Builder
			sb.WriteString("## LLM Configuration\n\n")

			// Show tiered config if enabled
			if iwm.controller.tierResolver != nil && iwm.controller.tierResolver.config != nil {
				tc := iwm.controller.tierResolver.config
				sb.WriteString("\n### Tiered LLM Config (active)\n")
				writeTierWithFallbacks := func(label string, cfg *AgentLLMConfig) {
					if cfg == nil {
						return
					}
					sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", label, cfg.Provider, cfg.ModelID))
					if len(cfg.Fallbacks) > 0 {
						fallbackStrs := make([]string, len(cfg.Fallbacks))
						for i, fb := range cfg.Fallbacks {
							fallbackStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
						}
						sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fallbackStrs, ", ")))
					}
					sb.WriteString("\n")
				}
				writeTierWithFallbacks("Tier 1 (high)", tc.Tier1)
				writeTierWithFallbacks("Tier 2 (medium)", tc.Tier2)
				writeTierWithFallbacks("Tier 3 (low)", tc.Tier3)
			}

			// Show per-step overrides from step_config.json
			stepConfigs, err := iwm.controller.ReadStepConfigs(ctx)
			if err != nil {
				sb.WriteString("\n### Per-Step LLM Overrides\nCould not read step_config.json.\n")
			} else {
				hasOverrides := false
				for _, sc := range stepConfigs {
					if sc.AgentConfigs == nil {
						continue
					}
					ac := sc.AgentConfigs
					if ac.ExecutionLLM == nil {
						continue
					}
					if !hasOverrides {
						sb.WriteString("\n### Per-Step LLM Overrides\n")
						hasOverrides = true
					}
					sb.WriteString(fmt.Sprintf("\n**%s**:\n", sc.ID))
					if ac.ExecutionLLM != nil {
						sb.WriteString(fmt.Sprintf("  - execution: %s/%s\n", ac.ExecutionLLM.Provider, ac.ExecutionLLM.ModelID))
					}
				}
				if !hasOverrides {
					sb.WriteString("\n### Per-Step LLM Overrides\nNone — all steps use workflow defaults.\n")
				}
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_llm_config tool: %v", err))
	}

	// Tool: update_variable — add, update, or delete variables
	updateVariableSchema := getUpdateVariableSchema()
	updateVariableParams, parseErr := parseSchemaForToolParameters(updateVariableSchema)
	if parseErr != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to parse update_variable schema: %v", parseErr))
	} else if err := mcpAgent.RegisterCustomTool(
		"update_variable",
		"Update, add, or delete variables in variables/variables.json. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update (name, value, description). The variables/variables.json file is updated immediately.",
		updateVariableParams,
		createUpdateVariableExecutor(iwm.controller.GetWorkspacePath(), logger,
			func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			},
			func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}),
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_variable tool: %v", err))
	}

	// Tool: add_group — create a new variable group
	if err := mcpAgent.RegisterCustomTool(
		"add_group",
		"Create a new variable group. Optionally provide a name and initial values. The new group will have all defined variables with empty values by default.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name for the group (e.g., 'Production', 'Staging'). If not provided, an auto-generated name is used.",
				},
				"values": map[string]interface{}{
					"type":        "object",
					"description": "Optional initial variable values as key-value pairs (e.g., {\"API_URL\": \"https://prod.example.com\"}). Variables not specified will have empty values.",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			readFile := func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			}
			writeFile := func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}
			workspacePath := iwm.controller.GetWorkspacePath()

			manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
			if err != nil {
				// Create new manifest if none exists
				manifest = &VariablesManifest{
					Variables:      []Variable{},
					Groups:         []VariableGroup{},
					ExtractionDate: time.Now().Format(time.RFC3339),
				}
			}

			newGroup := manifest.AddGroup()

			// Set name if provided
			if name, ok := args["name"].(string); ok && name != "" {
				newGroup.Name = name
			}

			// Set initial values if provided
			if values, ok := args["values"].(map[string]interface{}); ok {
				for k, v := range values {
					if strVal, ok := v.(string); ok {
						newGroup.Values[k] = strVal
					}
				}
			}

			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf("failed to write variables: %w", err)
			}

			return fmt.Sprintf("Created new group: %s with %d variables", newGroup.Name, len(newGroup.Values)), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register add_group tool: %v", err))
	}

	// Tool: update_group — update an existing variable group's name, values, or enabled status
	if err := mcpAgent.RegisterCustomTool(
		"update_group",
		"Update a variable group. Provide name (required) and fields to change: new_name, values (key-value map), enabled (true/false). Only provided fields are updated.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "The name of the group to update (e.g., 'group-1')",
				},
				"new_name": map[string]interface{}{
					"type":        "string",
					"description": "New name for the group",
				},
				"values": map[string]interface{}{
					"type":        "object",
					"description": "Variable values to set or update as key-value pairs. Only specified variables are updated; others remain unchanged.",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable or disable the group for execution",
				},
			},
			"required": []interface{}{"name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			groupName, ok := args["name"].(string)
			if !ok || groupName == "" {
				return "", fmt.Errorf("name is required")
			}

			readFile := func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			}
			writeFile := func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}
			workspacePath := iwm.controller.GetWorkspacePath()

			manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
			if err != nil {
				return "", fmt.Errorf("failed to read variables: %w", err)
			}

			// Find the group
			groupIdx := -1
			for i := range manifest.Groups {
				if manifest.Groups[i].Name == groupName {
					groupIdx = i
					break
				}
			}
			if groupIdx == -1 {
				return "", fmt.Errorf("group %s not found", groupName)
			}

			changes := []string{}

			// Update name
			if newName, ok := args["new_name"].(string); ok {
				manifest.Groups[groupIdx].Name = newName
				changes = append(changes, fmt.Sprintf("name=%s", newName))
			}

			// Update enabled
			if enabled, ok := args["enabled"].(bool); ok {
				manifest.Groups[groupIdx].Enabled = enabled
				changes = append(changes, fmt.Sprintf("enabled=%v", enabled))
			}

			// Update values (merge, don't replace)
			if values, ok := args["values"].(map[string]interface{}); ok {
				if manifest.Groups[groupIdx].Values == nil {
					manifest.Groups[groupIdx].Values = make(map[string]string)
				}
				for k, v := range values {
					if strVal, ok := v.(string); ok {
						manifest.Groups[groupIdx].Values[k] = strVal
						changes = append(changes, fmt.Sprintf("%s=%s", k, strVal))
					}
				}
			}

			if len(changes) == 0 {
				return "No changes specified", nil
			}

			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf("failed to write variables: %w", err)
			}

			return fmt.Sprintf("Updated group %s: %s", groupName, strings.Join(changes, ", ")), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_group tool: %v", err))
	}

	// Tool: delete_group — remove a variable group
	if err := mcpAgent.RegisterCustomTool(
		"delete_group",
		"Delete a variable group by name. Cannot delete the last remaining group.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "The name of the group to delete (e.g., 'group-2')",
				},
			},
			"required": []interface{}{"name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			groupName, ok := args["name"].(string)
			if !ok || groupName == "" {
				return "", fmt.Errorf("name is required")
			}

			readFile := func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			}
			writeFile := func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}
			workspacePath := iwm.controller.GetWorkspacePath()

			manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
			if err != nil {
				return "", fmt.Errorf("failed to read variables: %w", err)
			}

			if len(manifest.Groups) <= 1 {
				return "", fmt.Errorf("cannot delete the last remaining group")
			}

			if !manifest.DeleteGroup(groupName) {
				return "", fmt.Errorf("group %s not found", groupName)
			}

			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf("failed to write variables: %w", err)
			}

			return fmt.Sprintf("Deleted group %s. Remaining groups: %d", groupName, len(manifest.Groups)), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register delete_group tool: %v", err))
	}

	// Tool: get_workflow_config — read-only view of workflow-level settings (MCP servers, skills, secrets, LLM config)
	if err := mcpAgent.RegisterCustomTool(
		"get_workflow_config",
		"Show current workflow configuration: selected workflow MCP servers, selected workflow skills, secrets (names only, no values), one-way notification destinations, run retention, LLM config (tiered allocation with fallbacks, preset defaults), and schedules.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			ctrl := iwm.controller
			selected := ctrl.GetSelectedServers()
			var sb strings.Builder
			sb.WriteString("## Workflow Configuration\n\n")

			// --- MCP Servers ---
			sb.WriteString("### Selected MCP Servers\n")
			if len(selected) == 0 {
				sb.WriteString("No MCP servers selected for this workflow.\n")
			} else {
				for _, s := range selected {
					sb.WriteString(fmt.Sprintf("- %s\n", s))
				}
			}

			// --- Skills ---
			selectedSkills := ctrl.GetSelectedSkills()

			sb.WriteString("\n### Selected Skills\n")
			if len(selectedSkills) == 0 {
				sb.WriteString("No skills selected for this workflow.\n")
			} else {
				for _, sk := range selectedSkills {
					sb.WriteString(fmt.Sprintf("- **%s** — instructions at `skills/%s/SKILL.md`\n", sk, sk))
				}
			}

			// --- Browser ---
			sb.WriteString("\n### Browser\n")
			mode := strings.ToLower(strings.TrimSpace(ctrl.GetBrowserMode()))
			if mode == "" {
				mode = "none"
			}
			sb.WriteString(fmt.Sprintf("- browser_mode: %s\n", mode))
			if ports := ctrl.GetCdpPorts(); len(ports) > 0 {
				sb.WriteString(fmt.Sprintf("- cdp_ports: %v (independent Chrome profiles/login identities)\n", ports))
			} else {
				sb.WriteString("- cdp_ports: (default single port when CDP is selected)\n")
			}

			// --- Secrets (names only) ---
			secrets := ctrl.GetSecrets()
			sb.WriteString("\n### Secrets\n")
			if len(secrets) == 0 {
				sb.WriteString("No secrets configured for this workflow.\n")
			} else {
				sb.WriteString("**Selected** (values hidden):\n")
				for _, s := range secrets {
					sb.WriteString(fmt.Sprintf("- **%s**\n", s.Name))
				}
			}

			// --- One-way notifications (secret references only) ---
			sb.WriteString("\n### Notifications\n")
			var slackWebhookSecretName string
			if content, readErr := ctrl.ReadWorkspaceFile(ctx, "workflow.json"); readErr == nil {
				var manifest struct {
					Capabilities struct {
						Notifications struct {
							SlackWebhookSecretName string `json:"slack_webhook_secret_name"`
						} `json:"notifications"`
					} `json:"capabilities"`
				}
				if json.Unmarshal([]byte(content), &manifest) == nil {
					slackWebhookSecretName = strings.TrimSpace(manifest.Capabilities.Notifications.SlackWebhookSecretName)
				}
			}
			if slackWebhookSecretName == "" {
				sb.WriteString("- Slack Incoming Webhook: not configured\n")
			} else {
				sb.WriteString(fmt.Sprintf("- Slack Incoming Webhook: configured via encrypted secret **%s** (one-way notify_user delivery; URL hidden)\n", slackWebhookSecretName))
			}

			// Show available secrets that can be added
			if iwm.listAvailableSecrets != nil {
				allSecretNames, listErr := iwm.listAvailableSecrets(ctx)
				if listErr == nil && len(allSecretNames) > 0 {
					selectedSet := make(map[string]bool, len(secrets))
					for _, s := range secrets {
						selectedSet[s.Name] = true
					}
					var available []string
					for _, name := range allSecretNames {
						if !selectedSet[name] {
							available = append(available, name)
						}
					}
					if len(available) > 0 {
						sb.WriteString("\n**Available to add** (use update_workflow_config with add_secrets):\n")
						for _, name := range available {
							sb.WriteString(fmt.Sprintf("- %s\n", name))
						}
					}
				}
			}

			// --- LLM Configuration ---
			sb.WriteString("\n### LLM Configuration\n")

			// Tiered config
			if ctrl.tierResolver != nil && ctrl.tierResolver.config != nil {
				tc := ctrl.tierResolver.config
				sb.WriteString("\n**Tiered Allocation (active)**:\n")
				writeTierEntry := func(label string, cfg *AgentLLMConfig) {
					if cfg == nil {
						return
					}
					sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", label, cfg.Provider, cfg.ModelID))
					if len(cfg.Fallbacks) > 0 {
						fallbackStrs := make([]string, len(cfg.Fallbacks))
						for i, fb := range cfg.Fallbacks {
							fallbackStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
						}
						sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fallbackStrs, ", ")))
					}
					sb.WriteString("\n")
				}
				writeTierEntry("Tier 1 (high)", tc.Tier1)
				writeTierEntry("Tier 2 (medium)", tc.Tier2)
				writeTierEntry("Tier 3 (low)", tc.Tier3)
			}

			// Knowledgebase lock state
			sb.WriteString("\n**Knowledgebase**:\n")
			sb.WriteString(fmt.Sprintf("- use_knowledgebase: %v\n", ctrl.UseKnowledgebase()))
			sb.WriteString(fmt.Sprintf("- kb_shape: %s", workflowtypes.ResolveKBShape(ctrl.KBShape())))
			if ctrl.KBShape() == "" {
				sb.WriteString(" (default — not explicitly set)")
			}
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("- lock_knowledgebase: %v", ctrl.LockKnowledgebase()))
			if ctrl.LockKnowledgebase() {
				sb.WriteString(" — post-step KB update agent is FROZEN workflow-wide; use the /improve-knowledge checklist with a generic read-only reviewer and parent fixer for intentional note changes")
			}
			sb.WriteString("\n")

			runRetentionCount := defaultRunRetentionCount
			runRetentionNote := " (default)"
			if content, err := ctrl.ReadWorkspaceFile(ctx, "workflow.json"); err == nil {
				var manifest struct {
					RunRetentionCount *int `json:"run_retention_count"`
				}
				if json.Unmarshal([]byte(content), &manifest) == nil && manifest.RunRetentionCount != nil {
					if *manifest.RunRetentionCount >= 1 && *manifest.RunRetentionCount <= maxRunRetentionCount {
						runRetentionCount = *manifest.RunRetentionCount
						runRetentionNote = ""
					} else {
						runRetentionNote = fmt.Sprintf(" (invalid; runtime uses default %d)", defaultRunRetentionCount)
					}
				}
			}
			sb.WriteString(fmt.Sprintf("- run_retention_count: %d%s\n", runRetentionCount, runRetentionNote))

			// Preset-level defaults
			sb.WriteString("\n**Preset Defaults**:\n")
			writeLLMDefault := func(label string, llm *AgentLLMConfig) {
				if llm != nil {
					sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", label, llm.Provider, llm.ModelID))
					if len(llm.Fallbacks) > 0 {
						fallbackStrs := make([]string, len(llm.Fallbacks))
						for i, fb := range llm.Fallbacks {
							fallbackStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
						}
						sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fallbackStrs, ", ")))
					}
					sb.WriteString("\n")
				} else {
					sb.WriteString(fmt.Sprintf("- **%s**: (not set — uses LLM config default)\n", label))
				}
			}
			writeLLMDefault("Builder", ctrl.presetPhaseLLM)
			writeLLMDefault("Maintenance", ctrl.presetMaintenanceLLM)

			// --- Schedules ---
			if iwm.schedulerFuncs != nil && iwm.schedulerWorkspacePath != "" {
				sb.WriteString("\n### Schedules\n")
				scheduleList, schedErr := iwm.schedulerFuncs.ListSchedules(ctx, iwm.schedulerWorkspacePath)
				if schedErr != nil {
					sb.WriteString(fmt.Sprintf("_Could not load schedules: %v_\n", schedErr))
				} else if strings.TrimSpace(scheduleList) == "" {
					sb.WriteString("No schedules configured.\n")
				} else {
					sb.WriteString(scheduleList)
					sb.WriteString("\n")
				}
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_workflow_config tool: %v", err))
	}

	// === Tool: update_workflow_config ===
	// Tool: update_workflow_config — add/remove MCP servers, skills, secrets, and workflow-level knobs
	if err := mcpAgent.RegisterCustomTool(
		"update_workflow_config",
		"Update workflow configuration: add/remove MCP servers, workflow-level tool allowlist entries, skills and secrets; configure a one-way Slack Incoming Webhook by encrypted secret name; set browser mode and optional specialized multi-profile CDP ports; and set run retention. Use get_workflow_config to inspect current workflow settings and list_skills to discover installed skill folder names. Most changes take effect immediately for subsequent steps; changing cdp_ports or Slack webhook configuration takes effect on the next workflow execution.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"add_servers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "MCP server names to add to the workflow",
				},
				"remove_servers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "MCP server names to remove from the workflow",
				},
				"add_tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Workflow-level MCP tool allowlist entries to add. Format: 'server:tool_name' or 'server:*'. The server must already be selected with add_servers. This is for MCP servers, not workspace custom tools; use update_step_config(enabled_custom_tools=...) for workspace_advanced/workspace_browser/human_tools.",
				},
				"remove_tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Workflow-level MCP tool allowlist entries to remove. Format: 'server:tool_name' or 'server:*'.",
				},
				"add_skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Skill folder names to add to the workflow (use list_skills to see installed skills)",
				},
				"remove_skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Skill folder names to remove from the workflow",
				},
				"add_secrets": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Secret names to attach to the workflow. Each name MUST already have a stored value — either a GLOBAL_SECRET_* env var, a workflow secret (store via set_workflow_secret), or a reusable user secret (store via set_user_secret) — otherwise the request is rejected. Attaching only wires the name: runtime injects $SECRET_<NAME> with the looked-up value. Use list_secrets to see what's available.",
				},
				"remove_secrets": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Secret names to remove/disable from the workflow",
				},
				"slack_webhook_secret_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of an existing encrypted secret containing a complete Slack Incoming Webhook URL. This converts the credential into backend-only notification configuration and removes it from selected_secrets, selected_global_secret_names, and SECRET_* injection. notify_user sends a rich Block Kit card to it automatically and applies the change in the current builder turn; human_feedback never does because webhooks are one-way. Pass an empty string to disable workflow webhook delivery. Never put the URL itself in workflow.json or post to it directly.",
				},
				"update_tier_fallbacks": map[string]interface{}{
					"type":        "object",
					"description": "Update fallback LLMs for tiered allocation. Keys: 'tier_1', 'tier_2', 'tier_3'. Value: array of {provider, model_id, optional published_llm_id, optional options} objects. Use get_workflow_config or get_llm_config to see current config.",
					"properties": map[string]interface{}{
						"tier_1": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"published_llm_id": map[string]interface{}{"type": "string"}, "provider": map[string]interface{}{"type": "string"}, "model_id": map[string]interface{}{"type": "string"}, "options": map[string]interface{}{"type": "object", "additionalProperties": true}}, "required": []string{"provider", "model_id"}},
						},
						"tier_2": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"published_llm_id": map[string]interface{}{"type": "string"}, "provider": map[string]interface{}{"type": "string"}, "model_id": map[string]interface{}{"type": "string"}, "options": map[string]interface{}{"type": "object", "additionalProperties": true}}, "required": []string{"provider", "model_id"}},
						},
						"tier_3": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"published_llm_id": map[string]interface{}{"type": "string"}, "provider": map[string]interface{}{"type": "string"}, "model_id": map[string]interface{}{"type": "string"}, "options": map[string]interface{}{"type": "object", "additionalProperties": true}}, "required": []string{"provider", "model_id"}},
						},
					},
				},
				"lock_knowledgebase": map[string]interface{}{
					"type":        "boolean",
					"description": "Workflow-level freeze on the post-step KB update agent. When true, automatic notes/ writes stop; use the /improve-knowledge checklist with a generic read-only reviewer and parent fixer for intentional changes (reads unaffected). Set after KB is stable to save LLM cost per step.",
				},
				"browser_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"none", "auto", "headless", "cdp"},
					"description": "Workflow-level browser automation mode. 'none' disables browser capability; 'auto' uses the operator's shared Chrome through CDP when reachable and otherwise headless agent_browser; 'headless' forces isolated agent_browser; 'cdp' requires the operator's shared Chrome. For steps that actually drive the browser, also set update_step_config(enabled_custom_tools=[...]) and enabled_skills=['agent-browser'].",
				},
				"cdp_ports": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 65535},
					"maxItems":    maxCDPPortsPerWorkflow,
					"uniqueItems": true,
					"description": "Optional specialized CDP browser ports (maximum 4). Use multiple ports only when one workflow needs independent Chrome profiles/login identities, such as testing two accounts on the same site. Each port must be launched with a distinct --user-data-dir. Normal concurrent workflows should share the default single CDP browser.",
				},
				"run_retention_count": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     maxRunRetentionCount,
					"description": "Number of backup run/eval iterations to keep, excluding active iteration-0. Defaults to 5 when omitted. Raise this for workflows whose Pulse or Goal Advisor reviews need a wider evidence window.",
				},
				"post_run_monitor": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable the per-run monitor (Pulse): after each scheduled run Gate selects evidence-backed read-only reviews, one parent Pulse Fixer applies bounded changes, and the finalizer updates builder/improve.html, backup/publish status, and notification. Set true for workflows where a silent failure matters; default off. /pulse-setup turns this on as part of recurring setup; /goal-advisor does not change it.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var sb strings.Builder
			anyChanged := false

			// Helper to extract string array from args
			extractStringArray := func(key string) []string {
				val, ok := args[key]
				if !ok || val == nil {
					return nil
				}
				arr, ok := val.([]interface{})
				if !ok {
					return nil
				}
				result := make([]string, 0, len(arr))
				for _, v := range arr {
					if s, ok := v.(string); ok && s != "" {
						result = append(result, s)
					}
				}
				return result
			}

			// --- MCP Servers ---
			addServers := extractStringArray("add_servers")
			removeServers := extractStringArray("remove_servers")
			if len(addServers) > 0 || len(removeServers) > 0 {
				servers := iwm.controller.GetSelectedServers()
				result := make([]string, len(servers))
				copy(result, servers)
				changed := false

				if len(addServers) > 0 {
					existSet := make(map[string]bool, len(result))
					for _, s := range result {
						existSet[s] = true
					}
					for _, s := range addServers {
						if !existSet[s] {
							result = append(result, s)
							existSet[s] = true
							changed = true
						}
					}
				}

				if len(removeServers) > 0 {
					removeSet := make(map[string]bool, len(removeServers))
					for _, s := range removeServers {
						removeSet[s] = true
					}
					filtered := result[:0]
					for _, s := range result {
						if !removeSet[s] {
							filtered = append(filtered, s)
						} else {
							changed = true
						}
					}
					result = filtered
				}

				if changed {
					iwm.controller.SetSelectedServers(result)
					anyChanged = true
					sb.WriteString("### MCP Servers (updated)\n")
					if len(result) == 0 {
						sb.WriteString("No MCP servers configured.\n")
					} else {
						for _, s := range result {
							sb.WriteString(fmt.Sprintf("- %s\n", s))
						}
					}
					if len(removeServers) > 0 {
						removeSet := make(map[string]bool, len(removeServers))
						for _, s := range removeServers {
							removeSet[s] = true
						}
						selectedTools := iwm.controller.GetSelectedTools()
						filteredTools := selectedTools[:0]
						for _, tool := range selectedTools {
							serverName := tool
							if idx := strings.Index(tool, ":"); idx >= 0 {
								serverName = tool[:idx]
							}
							if !removeSet[serverName] {
								filteredTools = append(filteredTools, tool)
							}
						}
						if len(filteredTools) != len(selectedTools) {
							iwm.controller.SetSelectedTools(filteredTools)
							sb.WriteString("\nRemoved workflow-level tool allowlist entries for removed server(s).\n")
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow MCP servers: %v", result))
				}
			}

			// --- Workflow-level MCP tool allowlist ---
			addTools := extractStringArray("add_tools")
			removeTools := extractStringArray("remove_tools")
			if len(addTools) > 0 || len(removeTools) > 0 {
				selectedServers := iwm.controller.GetSelectedServers()
				serverSet := make(map[string]bool, len(selectedServers))
				for _, s := range selectedServers {
					serverSet[s] = true
				}
				var invalidTools []string
				var missingServers []string
				for _, tool := range addTools {
					idx := strings.Index(tool, ":")
					if idx <= 0 || idx == len(tool)-1 {
						invalidTools = append(invalidTools, tool)
						continue
					}
					serverName := tool[:idx]
					if !serverSet[serverName] {
						missingServers = append(missingServers, serverName)
					}
				}
				if len(invalidTools) > 0 {
					return fmt.Sprintf("Error: selected tool entries must use 'server:tool_name' or 'server:*'. Invalid entries: %v.", invalidTools), nil
				}
				if len(missingServers) > 0 {
					return fmt.Sprintf("Error: selected tool entries reference server(s) not selected at workflow level: %v. Add the server first with update_workflow_config(add_servers=[...]).", uniqueStringsPreserveOrder(missingServers)), nil
				}

				tools := iwm.controller.GetSelectedTools()
				result := make([]string, len(tools))
				copy(result, tools)
				changed := false

				if len(addTools) > 0 {
					existSet := make(map[string]bool, len(result))
					for _, t := range result {
						existSet[t] = true
					}
					for _, t := range addTools {
						if !existSet[t] {
							result = append(result, t)
							existSet[t] = true
							changed = true
						}
					}
				}

				if len(removeTools) > 0 {
					removeSet := make(map[string]bool, len(removeTools))
					for _, t := range removeTools {
						removeSet[t] = true
					}
					filtered := result[:0]
					for _, t := range result {
						if !removeSet[t] {
							filtered = append(filtered, t)
						} else {
							changed = true
						}
					}
					result = filtered
				}

				if changed {
					iwm.controller.SetSelectedTools(result)
					anyChanged = true
					sb.WriteString("\n### MCP Tools (updated)\n")
					if len(result) == 0 {
						sb.WriteString("No workflow-level tool allowlist configured; selected servers expose all their tools unless step-level tools override them.\n")
					} else {
						for _, t := range result {
							sb.WriteString(fmt.Sprintf("- %s\n", t))
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow MCP tools: %v", result))
				}
			}

			// --- Skills ---
			addSkills := extractStringArray("add_skills")
			removeSkills := extractStringArray("remove_skills")
			if len(addSkills) > 0 || len(removeSkills) > 0 {
				currentSkills := iwm.controller.GetSelectedSkills()
				result := make([]string, len(currentSkills))
				copy(result, currentSkills)
				changed := false

				if len(addSkills) > 0 {
					existSet := make(map[string]bool, len(result))
					for _, s := range result {
						existSet[s] = true
					}
					for _, s := range addSkills {
						if !existSet[s] {
							result = append(result, s)
							existSet[s] = true
							changed = true
						}
					}
				}

				if len(removeSkills) > 0 {
					removeSet := make(map[string]bool, len(removeSkills))
					for _, s := range removeSkills {
						removeSet[s] = true
					}
					filtered := result[:0]
					for _, s := range result {
						if !removeSet[s] {
							filtered = append(filtered, s)
						} else {
							changed = true
						}
					}
					result = filtered
				}

				if changed {
					iwm.controller.SetSelectedSkills(result)
					anyChanged = true
					sb.WriteString("\n### Skills (updated)\n")
					if len(result) == 0 {
						sb.WriteString("No skills configured.\n")
					} else {
						for _, s := range result {
							sb.WriteString(fmt.Sprintf("- %s\n", s))
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow skills: %v", result))
				}
			}

			// --- Secrets ---
			addSecrets := extractStringArray("add_secrets")
			removeSecrets := extractStringArray("remove_secrets")
			if len(addSecrets) > 0 || len(removeSecrets) > 0 {
				currentSecrets := iwm.controller.GetSecrets()
				changed := false

				if len(addSecrets) > 0 {
					// Attaching a name without a stored value is a silent foot-gun: runtime
					// drops the empty entry and $SECRET_<NAME> ends up unset, masking the
					// real "no value stored" error with a downstream graceful-fallback.
					// Reject up front if any requested name has no value in workflow store,
					// user store, or global env. The caller must store the value first.
					availableNames := map[string]bool{}
					if iwm.listAvailableSecrets != nil {
						if names, listErr := iwm.listAvailableSecrets(ctx); listErr == nil {
							for _, n := range names {
								availableNames[n] = true
							}
						} else {
							logger.Warn(fmt.Sprintf("Could not list available secrets for validation: %v", listErr))
						}
					}
					var missing []string
					if len(availableNames) > 0 {
						for _, name := range addSecrets {
							if !availableNames[name] {
								missing = append(missing, name)
							}
						}
					}
					if len(missing) > 0 {
						return fmt.Sprintf(
							"Error: cannot attach secret(s) with no stored value: %v.\n\n"+
								"These names have no value in the workflow secret store, reusable user secret store, or matching GLOBAL_SECRET_* env var. "+
								"Attaching them would set $SECRET_<NAME> to an empty string at runtime and silently break any step that reads them.\n\n"+
								"Fix:\n"+
								"  1. Store the value first: set_workflow_secret(name=\"%s\", value=\"<plaintext>\") for this workflow, or set_user_secret(...) for a reusable value.\n"+
								"  2. Then re-run update_workflow_config(add_secrets=[...]).",
							missing, missing[0],
						), nil
					}

					existSet := make(map[string]bool, len(currentSecrets))
					for _, s := range currentSecrets {
						existSet[s.Name] = true
					}
					for _, name := range addSecrets {
						if !existSet[name] {
							currentSecrets = append(currentSecrets, orchestrator.SecretEntry{Name: name, Value: ""})
							existSet[name] = true
							changed = true
						}
					}
				}

				if len(removeSecrets) > 0 {
					removeSet := make(map[string]bool, len(removeSecrets))
					for _, s := range removeSecrets {
						removeSet[s] = true
					}
					filtered := currentSecrets[:0]
					for _, s := range currentSecrets {
						if !removeSet[s.Name] {
							filtered = append(filtered, s)
						} else {
							changed = true
						}
					}
					currentSecrets = filtered
				}

				if changed {
					// Resolve plaintext values for every attached name so the workshop shell's
					// SECRET_* env vars stay in sync with workflow.json mid-session. Without
					// this, add_secrets would silently give you an empty $SECRET_<NAME> in
					// the workshop's own execute_shell_command (step runs still see values
					// because their env is rebuilt per-step, but builder shell does not).
					if iwm.resolveSecretValues != nil {
						names := make([]string, 0, len(currentSecrets))
						for _, s := range currentSecrets {
							names = append(names, s.Name)
						}
						values := iwm.resolveSecretValues(ctx, names)
						for i, s := range currentSecrets {
							if v, ok := values[s.Name]; ok {
								currentSecrets[i].Value = v
							}
						}
					}

					iwm.controller.SetSecrets(currentSecrets)
					if iwm.workshopConfig != nil {
						cloned := make([]orchestrator.SecretEntry, len(currentSecrets))
						copy(cloned, currentSecrets)
						iwm.workshopConfig.Secrets = cloned
					}

					// Sync SECRET_* into the workshop shell's persistent env map, so
					// `execute_shell_command` from the builder agent picks up new keys
					// without a session restart. Also strips removed secrets.
					if envRef := iwm.controller.GetWorkspaceEnvRef(); envRef != nil {
						nameSet := make(map[string]bool, len(currentSecrets))
						iwm.controller.LockWorkspaceEnv()
						for _, s := range currentSecrets {
							if s.Name == "" {
								continue
							}
							nameSet[s.Name] = true
							envRef["SECRET_"+s.Name] = s.Value
						}
						for k := range envRef {
							if strings.HasPrefix(k, "SECRET_") && !nameSet[strings.TrimPrefix(k, "SECRET_")] {
								delete(envRef, k)
							}
						}
						iwm.controller.UnlockWorkspaceEnv()
						logger.Info(fmt.Sprintf("Refreshed workshop shell env with %d SECRET_* entries", len(currentSecrets)))
					}

					anyChanged = true
					sb.WriteString("\n### Secrets (updated)\n")
					if len(currentSecrets) == 0 {
						sb.WriteString("No secrets configured.\n")
					} else {
						for _, s := range currentSecrets {
							sb.WriteString(fmt.Sprintf("- %s\n", s.Name))
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow secrets: %d entries", len(currentSecrets)))
				}
			}

			// --- Workflow-scoped one-way Slack webhook ---
			if raw, provided := args["slack_webhook_secret_name"]; provided && raw != nil {
				secretName, isString := raw.(string)
				if !isString {
					return "Error: slack_webhook_secret_name must be a string.", nil
				}
				secretName = strings.TrimSpace(secretName)
				var secretValue string
				if secretName != "" {
					if iwm.resolveSecretValues != nil {
						secretValue = iwm.resolveSecretValues(ctx, []string{secretName})[secretName]
					}
					if strings.TrimSpace(secretValue) == "" {
						for _, secret := range iwm.controller.GetSecrets() {
							if secret.Name == secretName {
								secretValue = secret.Value
								break
							}
						}
					}
					if strings.TrimSpace(secretValue) == "" {
						return fmt.Sprintf("Error: Slack webhook secret %q has no stored value. Save the full Incoming Webhook URL with set_workflow_secret first.", secretName), nil
					}
					if validateErr := services.ValidateSlackIncomingWebhookURL(secretValue); validateErr != nil {
						return fmt.Sprintf("Error: secret %q is not a valid Slack Incoming Webhook URL: %v", secretName, validateErr), nil
					}
				}

				content, readErr := iwm.controller.ReadWorkspaceFile(ctx, "workflow.json")
				if readErr != nil {
					return fmt.Sprintf("Failed to read workflow.json: %v", readErr), nil
				}
				var manifest map[string]interface{}
				if parseErr := json.Unmarshal([]byte(content), &manifest); parseErr != nil {
					return fmt.Sprintf("Failed to parse workflow.json: %v", parseErr), nil
				}
				caps, _ := manifest["capabilities"].(map[string]interface{})
				if caps == nil {
					caps = make(map[string]interface{})
				}
				if secretName == "" {
					if notifications, ok := caps["notifications"].(map[string]interface{}); ok {
						delete(notifications, "slack_webhook_secret_name")
						if len(notifications) == 0 {
							delete(caps, "notifications")
						} else {
							caps["notifications"] = notifications
						}
					}
				} else {
					notifications, _ := caps["notifications"].(map[string]interface{})
					if notifications == nil {
						notifications = make(map[string]interface{})
					}
					notifications["slack_webhook_secret_name"] = secretName
					caps["notifications"] = notifications
				}
				// A webhook referenced by Notifications is backend-only. Remove any
				// legacy/auto-attached copies from both secret selection lists so
				// future builders and workflow steps never receive SECRET_<NAME>.
				if secretName != "" {
					for _, key := range []string{"selected_secrets", "selected_global_secret_names"} {
						if rawSelected, ok := caps[key].([]interface{}); ok {
							filtered := make([]interface{}, 0, len(rawSelected))
							for _, entry := range rawSelected {
								if name, _ := entry.(string); strings.TrimSpace(name) != secretName {
									filtered = append(filtered, entry)
								}
							}
							caps[key] = filtered
						}
					}
				}
				manifest["capabilities"] = caps
				manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)
				updated, marshalErr := json.MarshalIndent(manifest, "", "  ")
				if marshalErr != nil {
					return fmt.Sprintf("Failed to marshal workflow.json: %v", marshalErr), nil
				}
				if writeErr := iwm.controller.WriteWorkspaceFile(ctx, "workflow.json", string(updated)); writeErr != nil {
					return fmt.Sprintf("Failed to write workflow.json: %v", writeErr), nil
				}

				if secretName != "" {
					currentSecrets := iwm.controller.GetSecrets()
					filtered := make([]orchestrator.SecretEntry, 0, len(currentSecrets))
					for _, secret := range currentSecrets {
						if strings.TrimSpace(secret.Name) != secretName {
							filtered = append(filtered, secret)
						}
					}
					iwm.controller.SetSecrets(filtered)
					if iwm.workshopConfig != nil {
						iwm.workshopConfig.Secrets = append([]orchestrator.SecretEntry(nil), filtered...)
					}
					if envRef := iwm.controller.GetWorkspaceEnvRef(); envRef != nil {
						iwm.controller.LockWorkspaceEnv()
						delete(envRef, "SECRET_"+secretName)
						iwm.controller.UnlockWorkspaceEnv()
					}
				}
				// Refresh the current builder turn immediately. The agent still never
				// sees the URL; only notify_user's destination context receives it.
				if destination, ok := ctx.Value(virtualtools.BotNotificationDestinationKey).(*services.NotificationDestination); ok && destination != nil {
					if secretName == "" {
						destination.SlackWebhook = nil
					} else {
						destination.SlackWebhook = &services.SlackWebhookDest{SecretName: secretName, URL: secretValue}
					}
				}
				var currentWebhook *services.SlackWebhookDest
				if secretName != "" {
					currentWebhook = &services.SlackWebhookDest{SecretName: secretName, URL: secretValue}
				}
				virtualtools.UpdateSessionSlackWebhook(iwm.sessionID, currentWebhook)

				anyChanged = true
				if secretName == "" {
					sb.WriteString("\n### Slack Incoming Webhook (disabled)\nWorkflow notify_user calls no longer send to a dedicated Slack webhook. Interactive Slack bot behavior is unchanged.\n")
				} else {
					sb.WriteString(fmt.Sprintf("\n### Slack Incoming Webhook (configured)\n- Encrypted backend-only secret: %s\n- Applies immediately to notify_user in this builder turn and to future workflow runs. notify_user renders rich Block Kit by default; the agent never receives the URL. It is one-way and is not used for human_feedback.\n", secretName))
				}
				logger.Info(fmt.Sprintf("Updated workflow Slack webhook secret reference: configured=%v", secretName != ""))
			}

			// --- Tier Fallbacks ---
			if tierFallbacksRaw, ok := args["update_tier_fallbacks"]; ok && tierFallbacksRaw != nil {
				if iwm.controller.tierResolver == nil || iwm.controller.tierResolver.config == nil {
					sb.WriteString("\n### Tier Fallbacks\n⚠️ Tiered allocation is not enabled. Cannot update fallbacks.\n")
				} else if tierMap, ok := tierFallbacksRaw.(map[string]interface{}); ok {
					tc := iwm.controller.tierResolver.config
					parseFallbacks := func(raw interface{}) []AgentLLMFallback {
						arr, ok := raw.([]interface{})
						if !ok {
							return nil
						}
						var fbs []AgentLLMFallback
						for _, item := range arr {
							m, ok := item.(map[string]interface{})
							if !ok {
								continue
							}
							provider, _ := m["provider"].(string)
							modelID, _ := m["model_id"].(string)
							if provider != "" && modelID != "" {
								publishedLLMID, _ := m["published_llm_id"].(string)
								options, _ := m["options"].(map[string]interface{})
								fbs = append(fbs, AgentLLMFallback{
									PublishedLLMID: publishedLLMID,
									Provider:       provider,
									ModelID:        modelID,
									Options:        options,
								})
							}
						}
						return fbs
					}

					tierChanged := false
					for _, entry := range []struct {
						key  string
						tier **AgentLLMConfig
						name string
					}{
						{"tier_1", &tc.Tier1, "Tier 1 (high)"},
						{"tier_2", &tc.Tier2, "Tier 2 (medium)"},
						{"tier_3", &tc.Tier3, "Tier 3 (low)"},
					} {
						if raw, exists := tierMap[entry.key]; exists {
							fbs := parseFallbacks(raw)
							if *entry.tier == nil {
								sb.WriteString(fmt.Sprintf("⚠️ %s has no primary LLM configured, skipping fallback update.\n", entry.name))
								continue
							}
							(*entry.tier).Fallbacks = fbs
							tierChanged = true
							sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", entry.name, (*entry.tier).Provider, (*entry.tier).ModelID))
							if len(fbs) > 0 {
								fbStrs := make([]string, len(fbs))
								for i, fb := range fbs {
									fbStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
								}
								sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fbStrs, ", ")))
							} else {
								sb.WriteString(" → fallbacks: (cleared)")
							}
							sb.WriteString("\n")
						}
					}
					if tierChanged {
						anyChanged = true
						sb.WriteString("\n### Tier Fallbacks (updated)\n")
						logger.Info("Updated tier fallback LLMs")
					}
				}
			}

			// --- Lock Knowledgebase ---
			if raw, ok := args["lock_knowledgebase"]; ok && raw != nil {
				if lockVal, ok := raw.(bool); ok {
					iwm.controller.SetLockKnowledgebase(lockVal)
					anyChanged = true
					if lockVal {
						sb.WriteString("\n### Knowledgebase Lock (enabled)\nPost-step KB update agent is now frozen workflow-wide. Use the `/improve-knowledge` checklist with a generic read-only reviewer and parent fixer for intentional `notes/` changes; reads are unaffected.\n")
					} else {
						sb.WriteString("\n### Knowledgebase Lock (disabled)\nPost-step KB update agent resumes for steps with `knowledgebase_contribution` set and write access.\n")
					}
					logger.Info(fmt.Sprintf("Updated workflow lock_knowledgebase=%v", lockVal))
				}
			}

			// --- Browser mode ---
			if raw, ok := args["browser_mode"]; ok && raw != nil {
				mode, _ := raw.(string)
				mode = strings.ToLower(strings.TrimSpace(mode))
				validModes := map[string]bool{"none": true, "auto": true, "headless": true, "cdp": true}
				if !validModes[mode] {
					return "Error: browser_mode must be one of: none, auto, headless, cdp.", nil
				}
				iwm.controller.SetBrowserMode(mode)
				if mode != "cdp" && mode != "auto" {
					iwm.controller.SetCdpPorts(nil)
				}
				anyChanged = true
				sb.WriteString(fmt.Sprintf("\n### Browser Mode (updated)\n- %s\n", mode))
				logger.Info(fmt.Sprintf("Updated workflow browser_mode=%s", mode))
			}

			// --- Specialized multi-profile CDP ports ---
			if raw, ok := args["cdp_ports"]; ok && raw != nil {
				arr, ok := raw.([]interface{})
				if !ok {
					return "Error: cdp_ports must be an array of integer ports.", nil
				}
				if len(arr) > maxCDPPortsPerWorkflow {
					return fmt.Sprintf("Error: cdp_ports supports at most %d ports.", maxCDPPortsPerWorkflow), nil
				}
				ports := make([]int, 0, len(arr))
				seen := make(map[int]bool, len(arr))
				for _, item := range arr {
					value, ok := item.(float64)
					if !ok || value != float64(int(value)) || value < 1 || value > 65535 {
						return "Error: every cdp_ports entry must be an integer between 1 and 65535.", nil
					}
					port := int(value)
					if seen[port] {
						return fmt.Sprintf("Error: duplicate cdp_ports entry %d.", port), nil
					}
					seen[port] = true
					ports = append(ports, port)
				}
				mode := strings.ToLower(strings.TrimSpace(iwm.controller.GetBrowserMode()))
				if len(ports) > 0 && mode != "cdp" && mode != "auto" {
					return "Error: non-empty cdp_ports requires browser_mode='cdp' or browser_mode='auto'. Set both in the same update_workflow_config call if needed.", nil
				}
				iwm.controller.SetCdpPorts(ports)
				anyChanged = true
				sb.WriteString(fmt.Sprintf("\n### CDP Profiles (updated)\n- Authorized ports: %v\n- Each port must use a distinct Chrome --user-data-dir. The change applies to the next workflow execution.\n", ports))
				logger.Info(fmt.Sprintf("Updated workflow cdp_ports=%v", ports))
			}

			// --- Run Retention ---
			if raw, ok := args["run_retention_count"]; ok && raw != nil {
				parseRetention := func(raw interface{}) (int, bool) {
					switch v := raw.(type) {
					case int:
						return v, true
					case int64:
						return int(v), true
					case float64:
						if v == float64(int(v)) {
							return int(v), true
						}
					case json.Number:
						if n, err := v.Int64(); err == nil {
							return int(n), true
						}
					}
					return 0, false
				}

				count, ok := parseRetention(raw)
				if !ok || count < 1 || count > maxRunRetentionCount {
					return fmt.Sprintf("Error: run_retention_count must be an integer between 1 and %d.", maxRunRetentionCount), nil
				}

				content, err := iwm.controller.ReadWorkspaceFile(ctx, "workflow.json")
				if err != nil {
					return fmt.Sprintf("Failed to read workflow.json: %v", err), nil
				}
				var manifest map[string]interface{}
				if err := json.Unmarshal([]byte(content), &manifest); err != nil {
					return fmt.Sprintf("Failed to parse workflow.json: %v", err), nil
				}
				manifest["run_retention_count"] = count
				manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)

				out, err := json.MarshalIndent(manifest, "", "  ")
				if err != nil {
					return fmt.Sprintf("Failed to marshal workflow.json: %v", err), nil
				}
				if err := iwm.controller.WriteWorkspaceFile(ctx, "workflow.json", string(out)); err != nil {
					return fmt.Sprintf("Failed to write workflow.json: %v", err), nil
				}

				anyChanged = true
				sb.WriteString(fmt.Sprintf("\n### Run Retention (updated)\nKeeping %d backup run/eval iteration(s), excluding active iteration-0.\n", count))
				logger.Info(fmt.Sprintf("Updated workflow run_retention_count=%d", count))
			}

			// --- Per-run monitor ---
			if raw, ok := args["post_run_monitor"]; ok && raw != nil {
				enabled, isBool := raw.(bool)
				if !isBool {
					return "Error: post_run_monitor must be a boolean.", nil
				}
				content, err := iwm.controller.ReadWorkspaceFile(ctx, "workflow.json")
				if err != nil {
					return fmt.Sprintf("Failed to read workflow.json: %v", err), nil
				}
				var manifest map[string]interface{}
				if err := json.Unmarshal([]byte(content), &manifest); err != nil {
					return fmt.Sprintf("Failed to parse workflow.json: %v", err), nil
				}
				manifest["post_run_monitor"] = enabled
				manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)
				out, err := json.MarshalIndent(manifest, "", "  ")
				if err != nil {
					return fmt.Sprintf("Failed to marshal workflow.json: %v", err), nil
				}
				if err := iwm.controller.WriteWorkspaceFile(ctx, "workflow.json", string(out)); err != nil {
					return fmt.Sprintf("Failed to write workflow.json: %v", err), nil
				}
				anyChanged = true
				state := "disabled"
				if enabled {
					state = "enabled"
				}
				sb.WriteString(fmt.Sprintf("\n### Per-run monitor (%s)\nThe per-run review-only pass is now %s for this workflow's scheduled runs.\n", state, state))
				logger.Info(fmt.Sprintf("Updated workflow post_run_monitor=%v", enabled))
			}

			if !anyChanged {
				return "No changes applied. Provide at least one of: add_servers, remove_servers, add_tools, remove_tools, add_skills, remove_skills, add_secrets, remove_secrets, slack_webhook_secret_name, update_tier_fallbacks, lock_knowledgebase, browser_mode, cdp_ports, run_retention_count, post_run_monitor.", nil
			}

			// Persist config changes to workflow.json manifest (file-backed)
			iwm.persistWorkflowConfigToManifest(ctx, logger)

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_workflow_config tool: %v", err))
	}

	// === Schedule management tools ===

	// Tool: list_schedules — List schedules for this workflow
	if err := mcpAgent.RegisterCustomTool(
		"list_schedules",
		"List all schedules for this workflow from workflow.json, including IDs, type, cron/calendar shape, timezone, enabled state, mode, workshop_mode, groups, and recent runtime state. Use this before update_schedule/delete_schedule/trigger_schedule/get_schedule_runs.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			if iwm.schedulerWorkspacePath == "" {
				return "No workspace path associated with this workflow session.", nil
			}
			return iwm.schedulerFuncs.ListSchedules(ctx, iwm.schedulerWorkspacePath)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_schedules tool: %v", err))
	}

	// Tool: create_schedule — Create a new cron schedule
	if err := mcpAgent.RegisterCustomTool(
		"create_schedule",
		"Create a new cron schedule for this workflow. Workflow schedules use mode='workshop' with workshop_mode='run'. Messages are optional; when omitted, the scheduler asks Run mode to execute the full workflow. Continuous improvement, including Goal Advisor, is selected dynamically by Pulse after normal scheduled runs; do not create a separate optimizer schedule.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Display name for the schedule (e.g., 'Daily morning run').",
				},
				"cron_expression": map[string]interface{}{
					"type":        "string",
					"description": "5-field cron expression (minute hour day-of-month month day-of-week). Examples: '0 9 * * *' (daily 9 AM), '*/30 * * * *' (every 30 min), '0 0 * * 1' (weekly Monday midnight).",
				},
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "Required IANA timezone (e.g. 'UTC', 'America/New_York', 'Asia/Kolkata'). Do not use abbreviations like EST, PST, or IST.",
				},
				"group_names": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Required. Variable group names to run (e.g., 'group-1', 'group-2'). Read variables/variables.json to see available groups. Do not leave empty.",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"description": "Execution mode. Only 'workshop' is supported for workflow schedules; legacy 'workflow' input is normalized to 'workshop'.",
					"enum":        []string{"workshop"},
				},
				"messages": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional predefined message queue sent one-by-one to the LLM. Omit for the default full-workflow run message. Messages should reference tools with full parameters. Example: ['Run the full workflow using run_full_workflow(group_name=\"group-1\")']. Read variables/variables.json for available group names.",
				},
				"workshop_mode": map[string]interface{}{
					"type":        "string",
					"description": "Run mode is the only supported value for new schedules. Pulse selects maintenance and Goal Advisor work after runs.",
					"enum":        []string{"run"},
				},
				"resume_previous": map[string]interface{}{
					"type":        "boolean",
					"description": "Optional opt-in when this workflow runs on a coding-agent CLI (claude-code, cursor-cli, codex-cli, pi-cli, agy-cli). When true, each scheduled run resumes the previous run's thread (same CLI) instead of starting a fresh session, so the agent keeps prior context across runs. API model providers and non-resumable runs start fresh. Defaults to false; omit for fresh sessions.",
				},
			},
			"required": []string{"name", "cron_expression", "timezone", "group_names"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			if iwm.schedulerWorkspacePath == "" {
				return "No workspace path associated with this workflow session.", nil
			}
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			timezone, _ := args["timezone"].(string)
			var groupNames []string
			if raw, ok := args["group_names"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							groupNames = append(groupNames, s)
						}
					}
				}
			}
			mode, _ := args["mode"].(string)
			var messages []string
			if raw, ok := args["messages"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							messages = append(messages, s)
						}
					}
				}
			}
			workshopMode, _ := args["workshop_mode"].(string)
			var resumePrevious *bool
			if raw, ok := args["resume_previous"]; ok && raw != nil {
				if b, ok2 := raw.(bool); ok2 {
					resumePrevious = &b
				}
			}
			if name == "" {
				return "name is required.", nil
			}
			if cronExpr == "" {
				return "cron_expression is required.", nil
			}
			if err := validateWorkshopScheduleTimezone(timezone); err != nil {
				return err.Error(), nil
			}
			if len(groupNames) == 0 {
				return "group_names is required. Read variables/variables.json and provide at least one explicit group_name, e.g. ['group-1'].", nil
			}
			return iwm.schedulerFuncs.CreateSchedule(ctx, iwm.schedulerWorkspacePath, name, cronExpr, timezone, groupNames, mode, messages, workshopMode, resumePrevious)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register create_schedule tool: %v", err))
	}

	// Tool: create_calendar_schedule — Create dated one-time runs for content calendars
	if err := mcpAgent.RegisterCustomTool(
		"create_calendar_schedule",
		"Create a dated calendar schedule for this workflow, such as a full-month Instagram content calendar. Use this when the user provides specific dates/times instead of a repeating cron pattern. Workflow calendar schedules always run through the workshop builder path; omit mode or use mode='workshop'.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "Display name for the calendar schedule."},
				"timezone":    map[string]interface{}{"type": "string", "description": "Required IANA timezone (e.g. 'UTC', 'America/New_York', 'Asia/Kolkata')."},
				"group_names": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Required variable group names to run."},
				"calendar_items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"date":        map[string]interface{}{"type": "string", "description": "YYYY-MM-DD in the schedule timezone."},
							"time":        map[string]interface{}{"type": "string", "description": "HH:MM in the schedule timezone."},
							"description": map[string]interface{}{"type": "string", "description": "Optional note for this item."},
							"messages":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional per-item workshop messages."},
						},
						"required": []string{"date", "time"},
					},
				},
				"mode":          map[string]interface{}{"type": "string", "description": "Execution mode. Only 'workshop' is supported for workflow schedules; legacy 'workflow' input is normalized to 'workshop'.", "enum": []string{"workshop"}},
				"messages":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional default workshop messages for all items. Omit for the default full-workflow run message."},
				"workshop_mode": map[string]interface{}{"type": "string", "description": "Run mode is the only supported value for new schedules; Pulse selects maintenance after runs.", "enum": []string{"run"}},
			},
			"required": []string{"name", "timezone", "calendar_items", "group_names"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil || iwm.schedulerFuncs.CreateCalendarSchedule == nil {
				return "Calendar schedule management not available in this session.", nil
			}
			if iwm.schedulerWorkspacePath == "" {
				return "No workspace path associated with this workflow session.", nil
			}
			name, _ := args["name"].(string)
			timezone, _ := args["timezone"].(string)
			if name == "" {
				return "name is required.", nil
			}
			if err := validateWorkshopScheduleTimezone(timezone); err != nil {
				return err.Error(), nil
			}
			rawItems, ok := args["calendar_items"]
			if !ok || rawItems == nil {
				return "calendar_items is required.", nil
			}
			calendarItemsJSON, err := json.Marshal(rawItems)
			if err != nil {
				return "", err
			}
			var groupNames []string
			if raw, ok := args["group_names"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							groupNames = append(groupNames, s)
						}
					}
				}
			}
			if len(groupNames) == 0 {
				return "group_names is required. Read variables/variables.json and provide at least one explicit group_name.", nil
			}
			mode, _ := args["mode"].(string)
			var messages []string
			if raw, ok := args["messages"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							messages = append(messages, s)
						}
					}
				}
			}
			workshopMode, _ := args["workshop_mode"].(string)
			return iwm.schedulerFuncs.CreateCalendarSchedule(ctx, iwm.schedulerWorkspacePath, name, timezone, groupNames, string(calendarItemsJSON), mode, messages, workshopMode)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register create_calendar_schedule tool: %v", err))
	}

	// Tool: update_schedule — Update a schedule
	if err := mcpAgent.RegisterCustomTool(
		"update_schedule",
		"Update an existing schedule. Only provided fields are changed; omitted fields keep their current values.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to update (from list_schedules).",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "New display name.",
				},
				"cron_expression": map[string]interface{}{
					"type":        "string",
					"description": "New 5-field cron expression.",
				},
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "New IANA timezone (e.g. 'UTC', 'America/New_York', 'Asia/Kolkata'). Do not use abbreviations like EST, PST, or IST.",
				},
				"group_names": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Explicit variable group names for the schedule (e.g., 'group-1', 'group-2'). Read variables/variables.json to see available groups. Omit to keep the current groups; do not pass an empty array.",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable or disable the schedule.",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"description": "Execution mode. Only 'workshop' is supported for workflow schedules; legacy 'workflow' input is normalized to 'workshop'.",
					"enum":        []string{"workshop"},
				},
				"messages": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Replaces existing messages. Messages should reference tools with full parameters, e.g. ['Run the full workflow using run_full_workflow(group_name=\"group-1\")'].",
				},
				"workshop_mode": map[string]interface{}{
					"type":        "string",
					"description": "Use 'run'. Omit this field to preserve an existing legacy schedule value.",
					"enum":        []string{"run"},
				},
				"resume_previous": map[string]interface{}{
					"type":        "boolean",
					"description": "Optional opt-in when this workflow runs on a coding-agent CLI. When true, scheduled runs resume the previous thread (same CLI) instead of starting fresh. Set false to go back to fresh sessions. Omit to keep the current setting.",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			timezone, _ := args["timezone"].(string)
			if timezone != "" {
				if err := validateWorkshopScheduleTimezone(timezone); err != nil {
					return err.Error(), nil
				}
			}
			var groupNames []string
			setGroupNames := false
			if raw, ok := args["group_names"]; ok && raw != nil {
				setGroupNames = true
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							groupNames = append(groupNames, s)
						}
					}
				}
			}
			if setGroupNames && len(groupNames) == 0 {
				return "group_names cannot be empty. Provide at least one explicit group_name from variables/variables.json, or omit group_names to keep the current selection.", nil
			}
			var enabled *bool
			if raw, ok := args["enabled"]; ok && raw != nil {
				if b, ok := raw.(bool); ok {
					enabled = &b
				}
			}
			mode, _ := args["mode"].(string)
			var messages []string
			if raw, ok := args["messages"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							messages = append(messages, s)
						}
					}
				}
			}
			workshopMode, _ := args["workshop_mode"].(string)
			var resumePrevious *bool
			if raw, ok := args["resume_previous"]; ok && raw != nil {
				if b, ok := raw.(bool); ok {
					resumePrevious = &b
				}
			}
			return iwm.schedulerFuncs.UpdateSchedule(ctx, jobID, name, cronExpr, timezone, groupNames, setGroupNames, enabled, mode, messages, workshopMode, resumePrevious)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_schedule tool: %v", err))
	}

	// Tool: delete_schedule — Delete a schedule
	if err := mcpAgent.RegisterCustomTool(
		"delete_schedule",
		"Permanently delete a schedule. This cannot be undone.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to delete (from list_schedules).",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			if err := iwm.schedulerFuncs.DeleteSchedule(ctx, jobID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Schedule `%s` deleted.", jobID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register delete_schedule tool: %v", err))
	}

	// Tool: trigger_schedule — Manually trigger a schedule run
	if err := mcpAgent.RegisterCustomTool(
		"trigger_schedule",
		"Manually trigger a schedule to run immediately, outside its normal cron timing.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to trigger (from list_schedules).",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			return iwm.schedulerFuncs.TriggerSchedule(ctx, jobID)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register trigger_schedule tool: %v", err))
	}

	// Tool: get_schedule_runs — Get run history for a schedule
	if err := mcpAgent.RegisterCustomTool(
		"get_schedule_runs",
		"View the execution history for a specific schedule, including status, duration, and errors.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to get runs for (from list_schedules).",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of runs to return. Defaults to 10.",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			limit := 10
			if raw, ok := args["limit"]; ok && raw != nil {
				if f, ok := raw.(float64); ok {
					limit = int(f)
				}
			}
			return iwm.schedulerFuncs.GetScheduleRuns(ctx, jobID, limit)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_schedule_runs tool: %v", err))
	}

	// === Skill management tools ===

	// Tool: list_skills — List all available skills in the workspace
	if err := mcpAgent.RegisterCustomTool(
		"list_skills",
		"List all available skills in the workspace. Shows both selected skills (used by this workflow) and all discovered skills.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil {
				return "Skill management not available in this session.", nil
			}
			return iwm.skillFuncs.ListSkills(ctx)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_skills tool: %v", err))
	}

	// Tool: import_skill — Import a skill from GitHub
	if err := mcpAgent.RegisterCustomTool(
		"import_skill",
		"Import a skill from GitHub into the workspace. The skill will be downloaded and available for use in workflows. Use list_skills first to see what's already available.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"github_url": map[string]interface{}{
					"type":        "string",
					"description": "GitHub URL of the skill to import. Can be a repository URL or a path to a specific skill folder (e.g., 'https://github.com/org/repo/tree/main/skills/my-skill').",
				},
				"token": map[string]interface{}{
					"type":        "string",
					"description": "Optional GitHub personal access token for private repositories.",
				},
			},
			"required": []string{"github_url"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil {
				return "Skill management not available in this session.", nil
			}
			githubURL, _ := args["github_url"].(string)
			if githubURL == "" {
				return "github_url is required.", nil
			}
			token, _ := args["token"].(string)
			return iwm.skillFuncs.ImportSkill(ctx, githubURL, token)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register import_skill tool: %v", err))
	}

	// Tool: uninstall_skill — Uninstall a skill from the workspace
	if err := mcpAgent.RegisterCustomTool(
		"uninstall_skill",
		"Uninstall a skill from the workspace. Removes skill files and version tracking. Use list_skills first to see available skills and their folder names.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"folder_name": map[string]interface{}{
					"type":        "string",
					"description": "The folder name of the skill to uninstall (from list_skills).",
				},
			},
			"required": []string{"folder_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil {
				return "Skill management not available in this session.", nil
			}
			folderName, _ := args["folder_name"].(string)
			if folderName == "" {
				return "folder_name is required.", nil
			}
			if err := iwm.skillFuncs.DeleteSkill(ctx, folderName); err != nil {
				return fmt.Sprintf("Failed to uninstall skill %q: %v", folderName, err), nil
			}
			return fmt.Sprintf("Successfully uninstalled skill %q from workspace.", folderName), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register uninstall_skill tool: %v", err))
	}

	// Tool: search_skills — Search the skills registry
	if err := mcpAgent.RegisterCustomTool(
		"search_skills",
		"Search for skills in the public skills registry. Returns matching skills with install commands. Use install_skill to install a result.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query (e.g., 'social media', 'browser automation', 'data visualization').",
				},
			},
			"required": []string{"query"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil || iwm.skillFuncs.SearchSkills == nil {
				return "Skill search not available. The skills CLI (npx) may not be installed.", nil
			}
			query, _ := args["query"].(string)
			if query == "" {
				return "query is required.", nil
			}
			return iwm.skillFuncs.SearchSkills(ctx, query)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register search_skills tool: %v", err))
	}

	// Tool: install_skill — Install a skill via the skills CLI
	if err := mcpAgent.RegisterCustomTool(
		"install_skill",
		"Install a skill from the public skills registry using owner/repo@skill-name format. Use search_skills first to find available skills.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source": map[string]interface{}{
					"type":        "string",
					"description": "Skill source in owner/repo@skill-name format (e.g., 'anthropics/skills@skill-creator').",
				},
			},
			"required": []string{"source"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil || iwm.skillFuncs.InstallSkill == nil {
				return "Skill installation not available. The skills CLI (npx) may not be installed.", nil
			}
			source, _ := args["source"].(string)
			if source == "" {
				return "source is required (e.g., 'owner/repo@skill-name').", nil
			}
			return iwm.skillFuncs.InstallSkill(ctx, source)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register install_skill tool: %v", err))
	}

	// ── LLM management tools (only when server callbacks are available) ──
	if iwm.llmToolsFuncs != nil {
		registerWorkshopLLMTools(iwm, mcpAgent, logger)
	}

}

func workshopLLMOptionsSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"properties": map[string]interface{}{
			"reasoning_effort": map[string]interface{}{
				"type":        "string",
				"description": "Reasoning/effort level for providers that support it. For Codex CLI this becomes model_reasoning_effort; for Claude Code this becomes --effort. Prefer values from list_provider_models.reasoning_effort_levels for the selected model.",
				"enum":        []string{"none", "minimal", "low", "medium", "high", "max", "xhigh"},
			},
			"verbosity": map[string]interface{}{
				"type":        "string",
				"description": "Response verbosity for providers that support it.",
				"enum":        []string{"low", "medium", "high"},
			},
			"thinking_level": map[string]interface{}{
				"type":        "string",
				"description": "Thinking level for providers that support a named thinking setting.",
				"enum":        []string{"low", "medium", "high"},
			},
			"thinking_budget": map[string]interface{}{
				"type":        "integer",
				"description": "Thinking budget in tokens for providers that support token-budgeted thinking.",
			},
			"top_p": map[string]interface{}{
				"type":        "number",
				"description": "Optional nucleus sampling value for providers that support it.",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "Optional top-k sampling value for providers that support it.",
			},
			"stop_sequences": map[string]interface{}{
				"type":        "array",
				"description": "Optional stop sequences for providers that support them.",
				"items":       map[string]interface{}{"type": "string"},
			},
			"endpoint": map[string]interface{}{
				"type":        "string",
				"description": "Azure endpoint override when validating an Azure model.",
			},
			"region": map[string]interface{}{
				"type":        "string",
				"description": "Azure or Bedrock region override.",
			},
			"api_version": map[string]interface{}{
				"type":        "string",
				"description": "Azure API version override.",
			},
		},
		"additionalProperties": true,
	}
}

// registerWorkshopLLMTools registers saved model-library, provider discovery,
// validation, and workflow role configuration tools on the workshop agent.
func registerWorkshopLLMTools(iwm *InteractiveWorkshopManager, mcpAgent *mcpagent.Agent, logger loggerv2.Logger) {
	cb := iwm.llmToolsFuncs

	// list_published_llms
	if err := mcpAgent.RegisterCustomTool(
		"list_published_llms",
		"List all published LLMs available for selection in the workflow tier config.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, _ map[string]interface{}) (string, error) {
			return cb.ListPublishedLLMs(ctx)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_published_llms tool: %v", err))
	}

	// list_provider_models
	if err := mcpAgent.RegisterCustomTool(
		"list_provider_models",
		"List the frontend-visible models for a provider. Fixed providers use shared metadata; dynamic providers use the same dynamic picker source as the UI.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id such as openai, openrouter, anthropic, vertex, azure, minimax, bedrock, pi-cli, claude-code.",
				},
			},
			"required": []string{"provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			provider := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", args["provider"])))
			if provider == "" {
				return "provider is required.", nil
			}
			return cb.ListProviderModels(ctx, provider)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_provider_models tool: %v", err))
	}

	// test_llm
	if err := mcpAgent.RegisterCustomTool(
		"test_llm",
		"Validate an LLM provider/model configuration. Uses workspace-backed provider auth by default, but temporary overrides can be provided.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id such as openai, openrouter, anthropic, vertex, azure, minimax, bedrock, pi-cli, claude-code, codex-cli.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional model id to validate.",
				},
				"api_key": map[string]interface{}{
					"type":        "string",
					"description": "Optional temporary API key override. If omitted, uses workspace-backed provider auth.",
				},
				"options": workshopLLMOptionsSchema("Optional model-specific options. Use reasoning_effort for Codex CLI and Claude Code effort control, and use list_provider_models to discover supported levels."),
				"endpoint": map[string]interface{}{
					"type":        "string",
					"description": "Optional Azure endpoint override.",
				},
				"region": map[string]interface{}{
					"type":        "string",
					"description": "Optional region override for Azure or Bedrock.",
				},
				"api_version": map[string]interface{}{
					"type":        "string",
					"description": "Optional Azure API version override.",
				},
			},
			"required": []string{"provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return cb.ValidateLLM(ctx, args)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register test_llm tool: %v", err))
	}

	llmEntrySchema := func(description string, fallbackDescription string) map[string]interface{} {
		return map[string]interface{}{
			"type":        "object",
			"description": description,
			"properties": map[string]interface{}{
				"published_llm_id": map[string]interface{}{"type": "string", "description": "Optional id from the published LLM set."},
				"provider":         map[string]interface{}{"type": "string", "description": "Provider id."},
				"model_id":         map[string]interface{}{"type": "string", "description": "Model id."},
				"options":          map[string]interface{}{"type": "object", "description": "Provider-specific runtime options copied from the published LLM, such as reasoning_effort.", "additionalProperties": true},
				"fallbacks": map[string]interface{}{
					"type":        "array",
					"description": fallbackDescription,
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"published_llm_id": map[string]interface{}{"type": "string"},
							"provider":         map[string]interface{}{"type": "string"},
							"model_id":         map[string]interface{}{"type": "string"},
							"options":          map[string]interface{}{"type": "object", "additionalProperties": true},
						},
					},
				},
			},
		}
	}

	// set_workflow_llm_config saves either a provider profile or fully explicit
	// workflow role configuration directly to workflow.json.
	if err := mcpAgent.RegisterCustomTool(
		"set_workflow_llm_config",
		"Save the workflow's LLM configuration to workflow.json capabilities.llm_config. Requires get_reference_doc(kind=\"llm-selection\") first. In provider_profile mode, provide one coding-agent provider and its current Builder, execution-tier, Maintenance, and Pulse defaults resolve at runtime. In explicit mode, provide builder_llm, maintenance_llm, pulse_llm, and all three execution tiers; each entry directly pins provider, model_id, options, and optional fallbacks. Saved model-library entries are optional reusable shortcuts, not a prerequisite.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{workflowtypes.LLMConfigModeProviderProfile, workflowtypes.LLMConfigModeExplicit},
					"description": "provider_profile follows the coding-agent provider defaults; explicit pins every workflow role.",
				},
				"provider":        map[string]interface{}{"type": "string", "description": "Coding-agent provider id. Required only in provider_profile mode."},
				"builder_llm":     llmEntrySchema("Builder model for planning, eval design, debugging, and normal workflow-builder chat.", "Ordered fallback models tried if the Builder model fails."),
				"tier_1":          llmEntrySchema("High-reasoning execution tier: first-time or difficult work.", "Ordered fallback models tried if the primary fails."),
				"tier_2":          llmEntrySchema("Medium-reasoning execution tier: established work with useful context.", "Ordered fallback models tried if the primary fails."),
				"tier_3":          llmEntrySchema("Low-reasoning execution tier: validation and mature routine work.", "Ordered fallback models tried if the primary fails."),
				"maintenance_llm": llmEntrySchema("Maintenance model for expensive Pulse modules such as Goal Advisor, Bug Review, report/eval improvement, KB health, and DB health.", "Ordered fallback models tried if the Maintenance model fails."),
				"pulse_llm":       llmEntrySchema("Pulse coordinator model for gate, worklist, reporting, and notification turns.", "Ordered fallback models tried if the Pulse model fails."),
			},
			"required": []string{"mode"},
		},
		guidance.WithDocPrecondition([]string{"llm-selection"}, guidance.DefaultTracker(), func(ctx context.Context, args map[string]interface{}) (string, error) {
			wsPath := iwm.controller.GetWorkspacePath()
			if wsPath == "" {
				return "Cannot determine workspace path.", nil
			}

			content, err := iwm.controller.ReadWorkspaceFile(ctx, "workflow.json")
			if err != nil {
				return fmt.Sprintf("Failed to read workflow.json: %v", err), nil
			}

			var manifest map[string]interface{}
			if err := json.Unmarshal([]byte(content), &manifest); err != nil {
				return fmt.Sprintf("Failed to parse workflow.json: %v", err), nil
			}

			caps, _ := manifest["capabilities"].(map[string]interface{})
			if caps == nil {
				caps = make(map[string]interface{})
			}
			existingLLMCfg, _ := caps["llm_config"].(map[string]interface{})
			llmCfg := map[string]interface{}{
				"schema_version": workflowtypes.LLMConfigSchemaVersion,
			}
			for _, key := range []string{
				"use_knowledgebase", "lock_knowledgebase", "kb_shape",
				"enable_context_summarization", "enable_context_editing",
				"enable_image_generation", "image_gen_provider", "image_gen_model_id",
			} {
				if value, ok := existingLLMCfg[key]; ok {
					llmCfg[key] = value
				}
			}

			updated := []string{}

			buildTierEntry := func(raw interface{}) map[string]interface{} {
				m, ok := raw.(map[string]interface{})
				if !ok {
					return nil
				}
				provider, _ := m["provider"].(string)
				modelID, _ := m["model_id"].(string)
				if provider == "" || modelID == "" {
					return nil
				}
				entry := map[string]interface{}{"provider": provider, "model_id": modelID}
				if publishedLLMID, _ := m["published_llm_id"].(string); publishedLLMID != "" {
					entry["published_llm_id"] = publishedLLMID
				}
				if options, _ := m["options"].(map[string]interface{}); len(options) > 0 {
					entry["options"] = options
				}
				if fbs, ok := m["fallbacks"].([]interface{}); ok && len(fbs) > 0 {
					fallbacks := make([]interface{}, 0, len(fbs))
					for _, fbRaw := range fbs {
						fbMap, ok := fbRaw.(map[string]interface{})
						if !ok {
							continue
						}
						fbProvider, _ := fbMap["provider"].(string)
						fbModelID, _ := fbMap["model_id"].(string)
						if fbProvider == "" || fbModelID == "" {
							continue
						}
						fallback := map[string]interface{}{"provider": fbProvider, "model_id": fbModelID}
						if publishedLLMID, _ := fbMap["published_llm_id"].(string); publishedLLMID != "" {
							fallback["published_llm_id"] = publishedLLMID
						}
						if options, _ := fbMap["options"].(map[string]interface{}); len(options) > 0 {
							fallback["options"] = options
						}
						fallbacks = append(fallbacks, fallback)
					}
					if len(fallbacks) > 0 {
						entry["fallbacks"] = fallbacks
					}
				}
				return entry
			}

			mode := strings.TrimSpace(fmt.Sprintf("%v", args["mode"]))
			switch mode {
			case workflowtypes.LLMConfigModeProviderProfile:
				provider := strings.TrimSpace(fmt.Sprintf("%v", args["provider"]))
				if provider == "" || provider == "<nil>" {
					return "provider is required in provider_profile mode. Use list_provider_models to inspect available coding-agent providers.", nil
				}
				llmCfg["mode"] = workflowtypes.LLMConfigModeProviderProfile
				llmCfg["provider"] = provider
				updated = append(updated, fmt.Sprintf("provider_profile=%s", provider))

			case workflowtypes.LLMConfigModeExplicit:
				llmCfg["mode"] = workflowtypes.LLMConfigModeExplicit
				missing := []string{}
				for _, key := range []string{"builder_llm", "maintenance_llm", "pulse_llm"} {
					entry := buildTierEntry(args[key])
					if entry == nil {
						missing = append(missing, key)
						continue
					}
					llmCfg[key] = entry
					updated = append(updated, fmt.Sprintf("%s=%v/%v", key, entry["provider"], entry["model_id"]))
				}

				tieredConfig := map[string]interface{}{}
				for _, key := range []string{"tier_1", "tier_2", "tier_3"} {
					entry := buildTierEntry(args[key])
					if entry == nil {
						missing = append(missing, key)
						continue
					}
					tieredConfig[key] = entry
					updated = append(updated, fmt.Sprintf("%s=%v/%v", key, entry["provider"], entry["model_id"]))
				}
				if len(missing) > 0 {
					return fmt.Sprintf("Explicit mode requires complete role configuration. Missing or invalid: %s.", strings.Join(missing, ", ")), nil
				}
				llmCfg["tiered_config"] = tieredConfig

			default:
				return fmt.Sprintf("mode must be %q or %q.", workflowtypes.LLMConfigModeProviderProfile, workflowtypes.LLMConfigModeExplicit), nil
			}

			caps["llm_config"] = llmCfg
			manifest["capabilities"] = caps

			out, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return fmt.Sprintf("Failed to marshal workflow.json: %v", err), nil
			}
			if err := iwm.controller.WriteWorkspaceFile(ctx, "workflow.json", string(out)); err != nil {
				return fmt.Sprintf("Failed to write workflow.json: %v", err), nil
			}

			logger.Info(fmt.Sprintf("✅ Workshop: workflow LLM config updated: %s", strings.Join(updated, ", ")))
			return fmt.Sprintf("Saved to workflow.json capabilities.llm_config:\n%s\n\nChanges take effect on the next workflow run.", strings.Join(updated, "\n")), nil
		}),
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register set_workflow_llm_config tool: %v", err))
	}
}

// workshopJSONUnmarshal is a local alias to avoid import conflicts
func workshopJSONUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

var reviewPlanAgentSystemTemplate = MustRegisterTemplate("reviewPlanAgentSystem", `# Workflow Plan Review Agent

You are a critical reviewer of the current workflow design. Your job is not to optimize or rewrite the plan. Your job is to challenge the decisions already made and identify where the current plan or its dependent artifacts are weak, unjustified, risky, overfit, stale, or internally inconsistent.

This is a **read-only review**:
- do not modify files
- do not invent missing evidence
- focus on findings first, not redesign first

## RULES
1. **Read-Only**: Do NOT modify any files.
2. **Findings first**: Lead with concrete problems, ordered by severity. Do not hide important issues under summaries.
3. **Be specific**: Always reference exact step IDs, and nested IDs as `+"`parent-id > sub-id`"+`.
4. **Review current decisions**: Critique the decisions that exist now: step boundaries, step types, mode declarations, context dependencies, context outputs, validation shape, portability, eval coverage, skill selection/scoping, learning configuration, KB configuration, db schema contracts, report wiring, and variables.
5. **Challenge assumptions**: If a decision appears to depend on unstated assumptions, call that out explicitly.
6. **Use evidence when available**: If a target run folder is provided, use run outputs/logs/eval reports to test whether the current workflow decisions were actually justified.
7. **Do not drift into full redesign**: You may suggest a concrete correction, but the primary task is to review and explain what is wrong with the current decision.
8. **Check portability and secrecy**: Flag plan-visible secrets, user-specific values, absolute paths, run-folder-specific values, and brittle environment assumptions.
9. **Check persistent-store discipline**: Stores survive across runs — `+"`"+`learnings/`+"`"+` (HOW to run), `+"`knowledgebase/context/`"+` (user-supplied runtime business context), `+"`"+`knowledgebase/notes/`+"`"+` (workflow-discovered durable narrative observations), `+"`"+`db/db.sqlite`+"`"+` (structured durable SQLite tables for cross-run state, read live by HTML reports via `+"`window.report.query`"+`), and `+"`db/assets/`"+` (durable media/file assets referenced by db rows or reports). Flag steps that confuse these stores: stashing durable facts in learnings or plan.json, stashing user-owned context in notes, writing report data only to run folders, embedding assets as blobs, or failing to declare `+"`"+`knowledgebase_contribution`+"`"+` / `+"`"+`db/README.md`+"`"+` contracts when steps produce persistent facts.
10. **Check learning and mode discipline**: A step should write learnings only when it has reusable HOW-to-run knowledge worth capturing across runs, and it must have a concrete `+"`"+`learning_objective`+"`"+`. `+"`"+`learnings_write_method`+"`"+` is compatibility-only; do not add it to new plans. Deterministic API/SDK calls, CLI commands, data fetching, known pagination, stable parsing/normalization/transforms, and mechanical persistence should be `+"`"+`scripted`+"`"+` from initial design with an authored/tested `+"`learnings/{step-id}/main.py`"+`; no run-history threshold is required to choose that mode. Browser/UI work, adaptive discovery, and judgment stay agentic. The 10+ representative-run bar applies to `+"`"+`lock_code`+"`"+`, not to declaring deterministic work scripted.
11. **Check KB discipline**: KB writes require a useful `+"`"+`knowledgebase_contribution`+"`"+`, correct read/write access, and preferably `+"`"+`knowledgebase_write_method=\"direct\"`+"`"+`. `+"`knowledgebase/context/`"+` should contain user-supplied runtime context; `+"`knowledgebase/notes/`"+` should contain workflow-discovered durable narrative observations, not execution recipes, raw rows, or volatile run state. If `+"`"+`knowledgebase/notes/_index.json`+"`"+` exists, it must point to coherent topic notes.
12. **Check db discipline**: `+"`"+`db/db.sqlite`+"`"+` should be a clean relational surface: each table documented in `+"`"+`db/README.md`+"`"+` with DDL, PRIMARY KEY, upsert rule (`+"`INSERT ... ON CONFLICT`"+`), indexes, writer ownership, group separation, report consumers (the HTML report `+"`window.report.query`"+` SQL that reads it), and correct references to durable assets under `+"`db/assets/`"+`.
13. **Check skill discipline**: Installed skills live under `+"`skills/{folder}/SKILL.md`"+` and are reusable capability instructions shared across workflows. Review workflow-selected skills and per-step `+"`enabled_skills`"+` against the actual plan. Flag missing needed skills, selected-but-unused skills, descriptions that reference skills not enabled for the execution agent, malformed skill folders, and skills that duplicate workflow-specific learnings or contain workflow-specific secrets/paths/run state. Do not assume workflow-level selected skills automatically reach step execution; verify step-level `+"`enabled_skills`"+` when runtime requires explicit scoping.
14. **Check lock consistency**: Three locks freeze workflow state — `+"`"+`lock_learnings`+"`"+` (per-step: freezes SKILL.md writes), `+"`"+`lock_code`+"`"+` (per-step, scripted only: freezes `+"`"+`learnings/{step-id}/main.py`+"`"+` against fix-loop rewrites), `+"`"+`lock_knowledgebase`+"`"+` (workflow-level: freezes post-step KB update agent). Flag inconsistency like `+"`"+`lock_code=true`+"`"+` without the scripted evidence gate or `+"`"+`lock_learnings=true`+"`"+` with stale/mismatched learning metadata. If a step description has meaningfully changed since the last review, recommend clearing `+"`"+`description_reviewed`+"`"+` and re-reviewing before keeping the locks.

## STEP BOUNDARY STANDARD

Modern agents can handle long context and many tool calls. Do not flag a step merely because it performs many actions, tool calls, screen interactions, or small transformations. A step is a durable workflow boundary: it has an output contract, validation gate, retry behavior, and persistent-store responsibilities.

Start from one large `+"`message_sequence`"+` per coherent shared-context span. Prefer the fewest durable steps that preserve real control boundaries. One agentic step may own a substantial end-to-end outcome when the work shares one objective, context, tool/security envelope, output contract, retry domain, and final validation gate. Do not require a separate scripted step for each subtask, checklist item, source, tool, proof check, double-check, or intermediate thought.

When that coherent outcome needs staged assurance, prefer one `+"`message_sequence`"+` over several regular steps. Give the first work turn the whole outcome; add only decision-useful follow-up turns that inspect evidence, challenge completion, and repair gaps (for example: `+"`re-open the result, verify every success criterion, and fix anything unsupported or incomplete`"+`). Do not turn a large task into one tiny sequence item per routine action.

Treat validation as an improvement to that large step before treating it as a topology change. Require run-specific proof/provenance in the output, tighten the top-level `+"`validation_schema`"+`, and add evidence-based double-check and repair turns. Flag a separate validation/reviewer step unless it needs an independently rerunnable artifact/failure domain, different permissions/tools, or genuine clean-room independence from the maker.

Multiple large sequences are correct when their contexts should not be shared—for example different credentials/security exposure, independent durable outputs/retries, clean-room independence, human or routing boundaries, or unrelated context that would distract or contaminate the next agent. The builder should decide this intelligently from workflow semantics, and the plan should make the boundary explainable. Flag action-count splitting that has no context-boundary rationale.

Separate deterministic acquisition from agentic processing even when both serve one business outcome. Fixed API/SDK requests, CLI commands, data fetching, known pagination, stable parsing/normalization, and mechanical persistence belong in one or a few scripted regular fetcher steps, batched by source/auth/retry/output contract. Their validated DB rows or artifacts feed a large message sequence for judgment, synthesis, semantic verification, and repair. Flag LLM turns that repeatedly perform deterministic retrieval/parsing, and flag one-scripted-step-per-endpoint fragmentation.

Flag **over-merged** steps when one step mixes unrelated durable outputs, validation gates, retry/failure domains, tool/security contexts, downstream contracts, persistent stores, human approvals, or routing decisions.

Flag **over-split** steps when adjacent steps share one objective and output contract, use the same tools/security context, fail and retry together, produce only scratch/pass-through intermediates, and one validation schema could verify the result. Also flag message sequences whose items merely enumerate routine sub-actions instead of reserving follow-up turns for validation, critique, correction, a real intermediate gate, or new external input.

Boundary truth: many tool calls can belong in one step; many durable contracts should not.

## CONTEXT

- **Workspace**: {{.WorkspacePath}}
{{if .WorkflowObjective}}- **Workflow Objective**: {{.WorkflowObjective}}{{else}}- **Workflow Objective**: ⚠️ NOT SET — treat missing objective as a top-level review finding{{end}}
{{if .WorkflowSuccessCriteria}}- **Success Criteria**: {{.WorkflowSuccessCriteria}}{{else}}- **Success Criteria**: ⚠️ NOT SET — treat missing success criteria as a top-level review finding{{end}}
{{if .WorkflowSelectedSkills}}- **Workflow-Selected Skills**: {{.WorkflowSelectedSkills}}{{else}}- **Workflow-Selected Skills**: none configured{{end}}
{{if .TargetRunFolder}}- **Target Run Folder**: {{.TargetRunFolder}}{{end}}

## PATH DISCIPLINE

For shell commands, use absolute workspace paths: `+"`{{.AbsWorkspacePath}}/...`"+`. For workspace file tools that expect workspace-relative paths, use `+"`{{.WorkspacePath}}/...`"+`. Do not use bare `+"`runs/...`"+`, `+"`evaluation/...`"+`, `+"`db/...`"+`, or similar paths unless a tool explicitly requires a path relative to the workflow root. Do not use host paths outside workspace-docs.

{{if .PlanJSON}}## CURRENT PLAN
`+"```json\n{{.PlanJSON}}\n```"+`
{{else}}Read the plan from `+"`planning/plan.json`"+` using shell commands before starting the review.{{end}}

{{if .StepConfigSummary}}## STEP CONFIG SUMMARY
{{.StepConfigSummary}}
{{end}}

## EVALUATION PLAN
Read `+"`evaluation/evaluation_plan.json`"+` using shell commands if it exists. If it does not exist, treat missing evaluation as a review finding when the workflow clearly needs measurable verification.

## DEPENDENT ARTIFACTS
Review these files/directories when present. Stay read-only:
- `+"`variables/variables.json`"+`: check whether plan-visible hardcoded values should be variables and whether required variables are declared.
- `+"`skills/{folder}/SKILL.md`"+`: read every skill selected at workflow level or enabled per step. Check whether the skill is actually needed, scoped to the right step(s), and not duplicating workflow-specific learnings.
- `+"`learnings/_global/SKILL.md`"+`: check whether HOW-to-run learnings match current step descriptions and do not duplicate task instructions.
- `+"`learnings/{step-id}/.learning_metadata.json`"+`: inspect for every step with learning writes or `+"`lock_learnings=true`"+`. Check `+"`successful_runs`"+`, `+"`description_hash_runs`"+`, and latest detection history. Flag locks that look stale, lack a clear builder/user rationale in review_notes, or contradict the current step description/config.
- `+"`learnings/{step-id}/main.py`"+` and `+"`learnings/{step-id}/script_metadata.json`"+`: inspect for scripted steps. For `+"`agentic`"+` steps, verify `+"`learnings/{step-id}/main.py`"+` does NOT exist; if it does, flag it as a stale artifact that should be deleted because agentic never runs or maintains persistent main.py.
- `+"`knowledgebase/context/context.md`"+`: check whether user-supplied runtime context is present when steps appear to rely on chat memory, and verify maintenance-owned notes did not absorb user-owned rules/preferences that belong here.
- `+"`knowledgebase/notes/_index.json`"+` and relevant `+"`knowledgebase/notes/*.md`"+`: check topic registry, stale/duplicated notes, and whether steps that produce domain facts have matching KB contribution contracts.
- `+"`db/README.md`"+`, `+"`db/db.sqlite`"+`, and `+"`db/assets/`"+`: check schema/DDL documentation, table shape, primary keys, upsert rules, indexes, writer ownership, group separation, durable asset metadata/provenance, and report compatibility.
- `+"`reports/report_plan.json`"+` + the HTML report document(s): check whether the report's `+"`window.report.query`"+` SQL reads durable `+"`db/db.sqlite`"+` tables (and references `+"`db/assets/`"+`/KB via `+"`window.report.get`"+`/`+"`fileUrl`"+`) rather than volatile run paths, whether referenced columns exist, and whether derived report helper tables could be collapsed into the report's query (JOIN/GROUP BY).
- `+"`builder/improve.html`"+`: read if present to avoid repeating already-known findings and to see unresolved prior review items.

{{if .TargetRunFolder}}## OPTIONAL RUN EVIDENCE
If useful, read:
- execution outputs under `+"`runs/{{.TargetRunFolder}}/execution/`"+`
- logs under `+"`runs/{{.TargetRunFolder}}/logs/`"+`
- evaluation report at `+"`evaluation/runs/{{.TargetRunFolder}}/evaluation_report.json`"+`
Use the run evidence to assess whether the plan decisions were justified in practice.
{{end}}

{{if .Focus}}## FOCUS
Prioritize this area: **{{.Focus}}**
{{end}}

## REVIEW CHECKS

Review the plan through these lenses:

1. **Decision justification** — Does each important design choice have a clear reason, or does it look accidental?
2. **Step boundaries** — Are steps split or merged according to the durable-boundary standard above, rather than by raw action/tool-call count?
3. **Step type choice** — Is each step using the right type for the actual job?
4. **Execution mode choice** — Does the declared mode fit the work? Deterministic API/SDK/CLI fetchers, known pagination, stable parsing/normalization/transforms, and mechanical persistence should be scripted from initial design and own an authored/tested `+"`learnings/{step-id}/main.py`"+`; they do not need 10 prior runs just to select scripted mode. The 10+ representative-run threshold applies only before `+"`lock_code=true`"+`. Judgment, adaptive discovery, and browser/UI work should remain agentic. If an agentic step has `+"`main.py`"+`, flag stale mode debt; if an obviously deterministic fetcher is agentic, flag avoidable LLM execution.
5. **Context flow** — Are dependencies/output contracts minimal, correct, and sufficient? Are there artificial file handoffs or missing dependencies?
6. **Validation & evaluation** — Is the workflow validating and evaluating the things that actually matter for success?
7. **Portability & secrecy** — Does the current plan leak secrets or overfit to one user, machine, run, or folder structure?
8. **Skill quality** — Are installed/selected skills the right reusable capabilities for the steps, scoped correctly via `+"`enabled_skills`"+` where needed, and free of workflow-specific learned state that belongs in `+"`learnings/_global/`"+`?
9. **Learning quality** — For every learning-enabled step, is there a good reason, a concrete objective, direct write method unless explicitly requested otherwise, and no stale/stretched learning scope?
10. **KB quality** — Are KB producer/consumer steps correctly declared, and do notes/index files contain durable domain knowledge rather than run logs or execution recipes?
11. **DB quality** — Are `+"`"+`db/db.sqlite`+"`"+` tables documented, keyed, upsert-safe, group-safe, connected to report consumers (the HTML report `+"`window.report.query`"+` SQL), and correctly referencing durable files under `+"`db/assets/`"+` when assets exist?
12. **Report wiring** — Are the report's `+"`window.report.query`"+` SQL statements backed by durable tables with matching columns and clear ownership? Do they push joins/aggregation into SQL where it would avoid helper tables or a `+"`step-generate-report`"+` flatten step?
13. **Operational risk** — Which current choices are most likely to fail, confuse the agent, or create maintenance burden later?

## OUTPUT FORMAT

### Findings
List only real findings, ordered by severity. If there are none, say so explicitly.

For each finding use:
- **[severity: high|medium|low] [actual-step-id or plan-wide]**: <what decision is weak or risky>
  - **Why this is a problem**: <impact on objective / success criteria / maintainability>
  - **Evidence**: <plan field, mode setting, step config, skill selection/SKILL.md, learning/KB/db/report/eval artifact, run evidence, hardcoded value, etc.>
  - **Better decision**: <what decision should likely replace it, briefly>

### Decisions That Look Sound
Call out 0-5 current decisions that appear well-justified so the builder knows what not to churn.

### Open Risks
List any important uncertainties where the plan might be fine, but the current evidence is too weak to trust the decision yet.

### Priority Rechecks
Give the top 3-5 follow-up checks or tool calls to validate the riskiest decisions next.
`)

var reviewPlanAgentUserTemplate = MustRegisterTemplate("reviewPlanAgentUser", `Critically review the current workflow design and dependent artifacts, then produce a findings-first report.{{if .TargetRunFolder}} Use run evidence from "{{.TargetRunFolder}}" where it helps test whether current decisions are justified.{{end}}{{if .Focus}} Focus especially on: {{.Focus}}{{end}}`)

var reviewWorkflowTimingAgentSystemTemplate = MustRegisterTemplate("reviewWorkflowTimingAgentSystem", `# Workflow Timing Review Agent

You are a read-only reviewer of workflow runtime performance. Your job is to determine:
1. where the workflow is actually spending time
2. which latency is necessary versus wasteful
3. how to make the workflow faster without compromising the objective or success criteria

This is a **read-only review**:
- do not modify files
- do not recommend speedups that obviously sacrifice the real goal
- separate evidence-backed bottlenecks from speculation

## RULES
1. **Read-Only**: Do NOT modify files.
2. **Evidence first**: Use run_metadata, execution summaries, timing files, and conversation logs from a real run.
3. **Read timing in the right order**: workflow wall-clock first, then step summaries, then detailed timing for the slowest steps, then conversation logs only to explain why the slow call happened.
4. **Separate bottleneck classes**:
   - LLM latency
   - tool latency
   - orchestration / validation / file IO overhead
   - too many step boundaries or handoffs
   - unclear descriptions causing extra tool calls, retries, or thinking loops
5. **Protect the objective**: A speedup is only good if it still preserves the stated objective and success criteria. Flag risky suggestions clearly.
6. **Recommend the smallest effective fix**: Prefer a tighter description over a structural plan change when the issue is prompt ambiguity. Prefer a structural change only when the current plan shape is causing waste.
7. **If no target run folder is preset**: Use shell to find the latest meaningful run evidence. If no run evidence exists, say you cannot assess real workflow speed yet.

## CONTEXT

- **Workspace**: {{.WorkspacePath}}
{{if .WorkflowObjective}}- **Workflow Objective**: {{.WorkflowObjective}}{{else}}- **Workflow Objective**: ⚠️ NOT SET — treat missing objective as a top-level finding{{end}}
{{if .WorkflowSuccessCriteria}}- **Success Criteria**: {{.WorkflowSuccessCriteria}}{{else}}- **Success Criteria**: ⚠️ NOT SET — treat missing success criteria as a top-level finding{{end}}
{{if .TargetRunFolder}}- **Target Run Folder**: {{.TargetRunFolder}}{{else}}- **Target Run Folder**: not preset — find the latest meaningful run evidence before judging speed{{end}}

## PATH DISCIPLINE

For shell commands, use absolute workspace paths: `+"`{{.AbsWorkspacePath}}/...`"+`. For workspace file tools that expect workspace-relative paths, use `+"`{{.WorkspacePath}}/...`"+`. Do not use bare `+"`runs/...`"+`, `+"`evaluation/...`"+`, or similar paths unless a tool explicitly requires a path relative to the workflow root. Do not use host paths outside workspace-docs.

{{if .PlanJSON}}## CURRENT PLAN
`+"```json\n{{.PlanJSON}}\n```"+`
{{else}}Read the plan from `+"`planning/plan.json`"+` using shell commands before starting the review.{{end}}

{{if .StepConfigSummary}}## STEP CONFIG SUMMARY
{{.StepConfigSummary}}
{{end}}

## REQUIRED EVIDENCE TO READ

1. Read `+"`runs/<target>/run_metadata.json`"+` if present.
2. Read the step execution summaries under `+"`runs/<target>/logs/<step-id>/execution/`"+` to rank the slowest step attempts using:
   - `+"`duration_ms`"+`
   - `+"`llm_duration_ms`"+`
   - `+"`tool_duration_ms`"+`
3. For the slowest step attempts, read the matching `+"`*-timing.json`"+` files and interpret:
   - `+"`agent.duration_ms`"+` as full wall-clock
   - `+"`llm.total_duration_ms`"+` as total model time
   - `+"`tools.total_duration_ms`"+` as total tool time
   - `+"`tools.calls[]`"+` as per-tool breakdown
4. Read conversation logs only after the timing files show where the time went.
5. Read `+"`evaluation/evaluation_plan.json`"+` and the eval report if they help determine whether a proposed speedup would threaten success criteria.

{{if .Focus}}## FOCUS
Prioritize this area: **{{.Focus}}**
{{end}}

## REVIEW TASKS

1. Identify the workflow-level wall-clock and whether one group or one step dominates.
2. Identify the top latency bottlenecks by class:
   - slow model calls
   - slow tools
   - orchestration/overhead gap
   - unnecessary step boundaries / file handoffs
   - ambiguous descriptions causing extra loops
3. For each major bottleneck, decide the safest improvement lever:
   - tighten a step description
   - reduce tool thrash
   - merge/remove/reorder steps
   - route work differently
   - change model/config only if the current tier is clearly overkill
4. Distinguish:
   - **safe speedups** that should preserve outcome quality
   - **risky speedups** that could harm success criteria
5. Prefer concrete recommendations over generic advice.

## OUTPUT FORMAT

### Verdict
- **Speed status**: fast enough | mixed | too slow | cannot assess
- **Main bottleneck class**: llm | tools | orchestration | plan shape | mixed
- **Confidence**: high | medium | low

### Biggest Bottlenecks
List the top 3-5 bottlenecks.

For each:
- **[workflow|group|step-id]**
  - **Where time went**: <wall-clock, llm, tools, overhead>
  - **Evidence**: <file/field values>
  - **Why it happened**: <actual cause, not vague speculation>

### Recommended Speedups
Group recommendations into:
- **Description / prompt changes**
- **Plan / step-shape changes**
- **Model / tool / config changes**

For each recommendation:
- **Change**
- **Expected impact**: high | medium | low
- **Risk to success criteria**: low | medium | high
- **Why**

### Priority Next Actions
Give the top 3-5 next actions. Prefer concrete tool calls where possible.
`)

var reviewWorkflowTimingAgentUserTemplate = MustRegisterTemplate("reviewWorkflowTimingAgentUser", `Review workflow latency and recommend how to make it faster without weakening the objective or success criteria.{{if .TargetRunFolder}} Use run evidence from "{{.TargetRunFolder}}".{{else}} Find the latest meaningful run evidence first.{{end}}{{if .Focus}} Focus especially on: {{.Focus}}{{end}}`)

var reviewWorkflowCostsAgentSystemTemplate = MustRegisterTemplate("reviewWorkflowCostsAgentSystem", `# Workflow Cost Review Agent

You are a read-only reviewer of workflow cost efficiency. Your job is to determine:
1. where the workflow is spending tokens and money
2. which spend is necessary versus wasteful
3. how to reduce cost without compromising the objective or success criteria

This is a **read-only review**:
- do not modify files
- do not recommend cheaper settings that obviously undermine the real goal
- separate necessary spend from avoidable waste

## RULES
1. **Read-Only**: Do NOT modify files.
2. **Evidence first**: Use actual cost ledgers, `+"`get_cost_summary`"+`, execution summaries, and run/eval evidence from a real run.
3. **Protect the objective**: First preserve success; then reduce cost. A cheap failure is not an optimization.
4. **Separate cost sources**:
   - expensive model usage
   - too many retries or loops
   - too many step boundaries / handoffs
   - unnecessary tool calls
   - expensive evaluation that is not measuring the real objective well
   - background learning / KB updates / fix loops that are still unlocked without enough benefit
5. **Recommend the smallest effective fix**: Prefer a tighter description or config change over a structural plan change when the issue is local. Recommend structural changes only when the plan shape itself is creating waste.
6. **If no target run folder is preset**: Use shell to find the latest meaningful cost/run evidence. If no cost evidence exists, say you cannot assess real workflow cost yet.

## CONTEXT

- **Workspace**: {{.WorkspacePath}}
{{if .WorkflowObjective}}- **Workflow Objective**: {{.WorkflowObjective}}{{else}}- **Workflow Objective**: ⚠️ NOT SET — treat missing objective as a top-level finding{{end}}
{{if .WorkflowSuccessCriteria}}- **Success Criteria**: {{.WorkflowSuccessCriteria}}{{else}}- **Success Criteria**: ⚠️ NOT SET — treat missing success criteria as a top-level finding{{end}}
{{if .TargetRunFolder}}- **Target Run Folder**: {{.TargetRunFolder}}{{else}}- **Target Run Folder**: not preset — find the latest meaningful cost/run evidence before judging cost{{end}}

## PATH DISCIPLINE

For shell commands, use absolute workspace paths: `+"`{{.AbsWorkspacePath}}/...`"+`. For workspace file tools that expect workspace-relative paths, use `+"`{{.WorkspacePath}}/...`"+`. Do not use bare `+"`runs/...`"+`, `+"`evaluation/...`"+`, `+"`costs/...`"+`, or similar paths unless a tool explicitly requires a path relative to the workflow root. Do not use host paths outside workspace-docs.

{{if .PlanJSON}}## CURRENT PLAN
`+"```json\n{{.PlanJSON}}\n```"+`
{{else}}Read the plan from `+"`planning/plan.json`"+` using shell commands before starting the review.{{end}}

{{if .StepConfigSummary}}## STEP CONFIG SUMMARY
{{.StepConfigSummary}}
{{end}}

## REQUIRED EVIDENCE TO READ

1. Call `+"`get_cost_summary(run_folder=\"<target>\")`"+` for the target run if available.
2. Read cost ledgers under:
   - `+"`costs/phase/token_usage.json`"+`
   - `+"`costs/execution/`"+`
   - `+"`costs/evaluation/`"+` when evaluation spend matters
3. Read step execution summaries under `+"`runs/<target>/logs/<step-id>/execution/`"+` to correlate cost with retries, LLM calls, and tool calls.
4. Read `+"`evaluation/evaluation_plan.json`"+` and eval reports when evaluation cost might be disproportionate or misaligned to the real goal.
5. Read enough run evidence to judge whether a lower-cost recommendation would threaten success criteria.

{{if .Focus}}## FOCUS
Prioritize this area: **{{.Focus}}**
{{end}}

## REVIEW TASKS

1. Identify the biggest cost drivers by step, model, and phase.
2. Distinguish:
   - **necessary spend** that is directly supporting success
   - **avoidable waste** from retries, ambiguous descriptions, too many handoffs, or overpowered models
3. Decide the safest reduction lever for each major cost driver:
   - tighten a description to reduce retries/tool calls
   - merge/remove/reorder steps
   - reduce evaluation waste or misaligned eval breadth
   - change model/config only when success evidence suggests it is safe
   - lock mature learnings/code/knowledgebase only when the current evidence supports freezing them
4. Distinguish:
   - **safe cost reductions** that should preserve outcome quality
   - **risky cost reductions** that could hurt success criteria
5. Prefer concrete recommendations over generic advice.

## OUTPUT FORMAT

### Verdict
- **Cost status**: efficient | mixed | too expensive | cannot assess
- **Main cost driver**: model usage | retries/loops | plan shape | evaluation | mixed
- **Confidence**: high | medium | low

### Biggest Cost Drivers
List the top 3-5 cost drivers.

For each:
- **[phase|group|step-id|model]**
  - **Cost evidence**: <tool output, ledger values, call counts>
  - **Why cost is high**: <actual cause>
  - **Necessary vs wasteful**: <one sentence>

### Recommended Cost Reductions
Group recommendations into:
- **Description / prompt changes**
- **Plan / step-shape changes**
- **Model / tool / config changes**

For each recommendation:
- **Change**
- **Expected savings**: high | medium | low
- **Risk to success criteria**: low | medium | high
- **Why**

### Priority Next Actions
Give the top 3-5 next actions. Prefer concrete tool calls where possible.
`)

var reviewWorkflowCostsAgentUserTemplate = MustRegisterTemplate("reviewWorkflowCostsAgentUser", `Review workflow cost and recommend how to reduce it without weakening the objective or success criteria.{{if .TargetRunFolder}} Use run and cost evidence from "{{.TargetRunFolder}}".{{else}} Find the latest meaningful run and cost evidence first.{{end}}{{if .Focus}} Focus especially on: {{.Focus}}{{end}}`)

// --- review_step_code templates ---

var reviewStepCodeAgentSystemTemplate = MustRegisterTemplate("reviewStepCodeAgentSystem", `# Step Code Review Agent

{{.MainPyAuthoringRules}}

You are a code reviewer that checks whether saved Python scripts (main.py) still match their step descriptions AND comply with the authoring rules above. Over time, step descriptions get updated with new requirements, but the saved scripts don't get regenerated — causing silent drift. And manual patches sometimes bypass the rules.

Your job is to compare each step's **current description** with its **saved main.py** and identify:
1. **Missing functionality** — things the description requires that main.py doesn't do
2. **Extra functionality** — things main.py does that are no longer in the description
3. **Incorrect behavior** — things main.py does differently than what the description specifies
4. **Anti-patterns** — hardcoded paths, hardcoded credentials, missing env var usage, non-portable code
5. **Fabricated data** — script writes output data without actually fetching/computing it from real sources (MCP tools, APIs, input files)
6. **MCP tool usage** — incorrect server/tool names in call_mcp(), missing API discovery, guessed parameter names instead of using get_api_spec
7. **Persistent-store confusion** — script writes to the wrong store. Stores: `+"`"+`learnings/`+"`"+` (HOW to run, updated by direct step-learning turns), `+"`knowledgebase/context/`"+` (user-supplied runtime context, never step-written), `+"`"+`knowledgebase/notes/`+"`"+` (workflow-discovered durable narrative observations — written by the post-step KB update agent in agent mode, or by step agents in direct-write mode), `+"`"+`db/db.sqlite`+"`"+` (structured durable SQLite tables — step-owned; upsert via `+"`INSERT ... ON CONFLICT`"+`, never recreate a table), and `+"`db/assets/`"+` (durable media/file assets referenced by db rows/reports). Flag scripts that write directly under `+"`"+`knowledgebase/notes/`+"`"+` outside of direct-write mode, write under `+"`knowledgebase/context/`"+`, stash durable cross-run state in per-step output files when it belongs in `+"`"+`db/`+"`"+`, store large assets in the DB instead of `+"`db/assets/`"+`, or DROP/recreate `+"`"+`db/db.sqlite`+"`"+` tables instead of upserting.

This is a **read-only review** — do not modify any files.

## RULES
1. **Read-Only**: Do NOT modify any files.
2. **Be specific**: Reference exact line numbers in main.py and exact phrases from the description.
3. **Severity matters**: Flag critical drift (wrong output, missing steps) higher than cosmetic issues.
4. **Consider reusability**: The script runs across different groups/users — flag anything that would break portability.
5. **Check output contract**: Verify that main.py produces the exact output files, field names, and formats specified in the validation schema.
6. **Check data authenticity**: Verify that every value written to output files traces back to a real data source (MCP tool call, API response, or input file). Flag any hardcoded/fabricated data in output construction.
7. **Check MCP tool usage**: Verify that call_mcp() calls use consistent server/tool naming. Flag suspicious tool names that look guessed rather than discovered via get_api_spec.
8. **Check browser automation quality**: For scripts using agent_browser, verify:
   - **Browser-heavy scripted is usually the wrong fit.** If a browser-enabled step has a saved `+"`main.py`"+`, flag it unless the user explicitly requested scripted browser execution AND the script uses durable selectors, state-driven waits, fresh snapshots, and has evidence of stability across runs. Browser/UI automation should generally remain `+"`agentic`"+` so the agent can adapt to live UI state, auth, dynamic selectors, pagination, and third-party page timing.
   - **agentic must not keep main.py.** If a step is declared `+"`agentic`"+` but still has `+"`learnings/{step-id}/main.py`"+`, flag the file as stale artifact debt. Recommend deleting it and clearing `+"`lock_code`"+`; agentic does not run or maintain persistent scripts.
   - **Selectors are DURABLE, not refs.** Hardcoded string refs like `+"`'abc123'`"+` or `+"`'@e1'`"+` in main.py are BUGS — refs are session-local. The durable alternatives are (in priority order): data-testid / hand-written id / aria-label / role+name / get_by_label|placeholder|text. Flag any ref that appears as a literal string (not a variable parsed from a current-run snapshot).
   - Uses agent_browser `+"`snapshot`"+` before interacting — never clicks or types blindly
   - Ref-based interaction is acceptable ONLY when the ref value is parsed from a snapshot taken earlier in the SAME run (`+"`ref = extract_ref(snapshot, role=..., name=...)` then `browser('click', [ref])`"+`). Hardcoded refs in main.py must be flagged.
   - Does NOT use raw page JavaScript for actions when a dedicated agent_browser command exists. Read-only eval for discovery is fine.
   - Uses wait loops that check page state via agent_browser `+"`snapshot`"+`, not hardcoded time.sleep()
   - Prints diagnostic snapshots/state on failure so the fix loop can debug what went wrong
   - Avoids structural CSS selectors (nth-child chains, deep descendant paths) — flag those in favor of durable hooks

## CORRECT BROWSER AUTOMATION PATTERNS

**The invariant, independent of tool choice:** the selectors a saved `+"`main.py`"+` carries must be DETERMINISTIC across future runs — i.e. on every replay they must resolve to the same element, even across browser restarts, page rebuilds, deploys that change class names / DOM shape / React keys. Refs (`+"`'abc123'`"+`, `+"`'@e1'`"+`) are session-local — a snapshot assigns them fresh each run, so hardcoded refs are the opposite of deterministic. **Any ref value that appears as a string literal in main.py is a bug** — the replay tomorrow will click the wrong thing.

Deterministic selectors that survive future runs (in descending durability): data-testid → hand-written id/name → aria-label → role+name → get_by_label / get_by_placeholder / get_by_text. Structural CSS (`+"`nth-child`"+`, deep descendant chains, auto-generated classes like `+"`css-8xy3zb`"+`) is NOT deterministic — DOM rearrangements or style-system rebuilds break it.

Refs are fine *in-session* (parse a snapshot, use the ref for the next call in the same run). They are never fine *persisted*. Flag any saved script where the ref is a hardcoded string.

### Agent Browser (tool='agent_browser')

Direct tool call (NOT via call_mcp). Uses command + args pattern.

CDP mode: call agent_browser(command='tab', args=[], session='main') to get the selected tab hint. If none is selected, create a stable labeled tab with the tab command. Call open with URL-only args. Then include the chosen tab inline in later page actions, e.g. args=['tab', 't1', '-i'] for snapshot and args=['tab', 't1', ref] for click. Calls without an inline tab are rejected in shared-CDP mode except open, which uses the selected tab and passes only the URL to agent-browser.

**Correct patterns:**
`+"```"+`python
# CORRECT: Durable CSS selector (id / aria-label / testid) as the args target
agent_browser(command='click', args=['tab', 't1', '#panAdhaarUserId'], session='main')
agent_browser(command='fill', args=['tab', 't1', '[aria-label="Password"]', password], session='main')
agent_browser(command='tab', args=['t1'], session='main')
agent_browser(command='open', args=['https://example.com'], session='main')

# CORRECT: Snapshot+ref derived AT RUNTIME (the @e1 is parsed from the snapshot variable)
snapshot = agent_browser(command='snapshot', args=['tab', 't1', '-i'], session='main')
ref = extract_ref(snapshot, role='button', name='Continue')  # your helper returns '@eN' parsed from snapshot
agent_browser(command='click', args=['tab', 't1', ref], session='main')

# CORRECT: Wait by polling
agent_browser(command='wait', args=['tab', 't1', 'text', 'Dashboard'], session='main')
`+"```"+`

**Anti-patterns for agent_browser (WRONG):**
`+"```"+`python
# WRONG: Hardcoded @e1 / @e2 ref in main.py — ref is session-local
agent_browser(command='click', args=['@e1'], session='main')
agent_browser(command='fill', args=['@e2', 'search text'], session='main')

# WRONG: eval for actions
agent_browser(command='eval', args=['document.querySelector("button").click()'], session='main')

# WRONG: Structural CSS selector that will break on DOM shape change
agent_browser(command='click', args=['.form > div:nth-child(3)'], session='main')
`+"```"+`

## CONTEXT

- **Workspace**: {{.WorkspacePath}}
{{if .WorkflowObjective}}- **Workflow Objective**: {{.WorkflowObjective}}{{end}}

## PATH DISCIPLINE

For shell commands, use absolute workspace paths: `+"`{{.AbsWorkspacePath}}/...`"+`. For workspace file tools that expect workspace-relative paths, use `+"`{{.WorkspacePath}}/...`"+`. Do not use bare `+"`planning/...`"+`, `+"`evaluation/...`"+`, `+"`learnings/...`"+`, or similar paths unless a tool explicitly requires a path relative to the workflow root. Do not use host paths outside workspace-docs.

## STEPS TO REVIEW

{{.StepsToReview}}

## OUTPUT FORMAT

For each step reviewed, output:

### [step-id]: <step title>
- **Status**: `+"`IN_SYNC`"+` | `+"`DRIFTED`"+` | `+"`NO_SCRIPT`"+`
- **Findings** (if drifted):
  1. **[severity: high|medium|low]**: <what is mismatched>
     - **Description says**: <relevant excerpt>
     - **Script does**: <what the code actually does, with line reference>
     - **Fix needed**: <brief suggestion>

### Summary
- Total steps reviewed: N
- In sync: N
- Drifted: N
- No script: N
- **Top priority fixes**: List the 3 most critical drifts that should be addressed first.
- **Stale lock warning**: For every step marked `+"`"+`DRIFTED`+"`"+` whose `+"`"+`step_config`+"`"+` has `+"`"+`lock_code=true`+"`"+` or `+"`"+`lock_learnings=true`+"`"+`, explicitly call out that the lock is stale — the frozen main.py or SKILL.md no longer matches the description, and the builder should `+"`"+`update_step_config(step_id, lock_code=false, lock_learnings=false, description_reviewed=false)`+"`"+` before regenerating.
`)

var reviewStepCodeAgentUserTemplate = MustRegisterTemplate("reviewStepCodeAgentUser", `Review the saved main.py scripts against their step descriptions and report any drift.{{if .Focus}} Focus especially on: {{.Focus}}{{end}}

For each step listed above, carefully compare the description with the script and check:
1. Does the script do everything the description asks?
2. Does the script produce the correct output format?
3. Are there hardcoded values that should use environment variables?
4. Would the script work correctly for a different group/user?
5. Does the script actually fetch/compute data from real sources, or does it fabricate/hardcode output values?
6. Are MCP tool calls using correct server names, tool names, and parameters?
7. For browser automation: does the script use agent_browser snapshots plus fresh refs or durable selectors, or does it blindly inject JavaScript?`)

// ============================================================================
// Background Task Agent — standalone agent for run_in_background tool
// ============================================================================

var backgroundTaskAgentSystemTemplate = MustRegisterTemplate("backgroundTaskAgentSystem", `# Background Task Agent

You are a background agent spawned by the workflow builder to perform a specific task. You have access to the same workspace tools as the workflow execution agents.

**Workspace folder:** {{.WorkspacePath}}

## Path Discipline

For shell commands, use absolute workspace paths: `+"`{{.AbsWorkspacePath}}/...`"+`. For workspace file tools that expect workspace-relative paths, use `+"`{{.WorkspacePath}}/...`"+`. Do not use bare `+"`runs/...`"+`, `+"`evaluation/...`"+`, `+"`planning/...`"+`, `+"`db/...`"+`, or similar paths unless a tool explicitly requires a path relative to the workflow root. Do not use host paths outside workspace-docs.

## Instructions
Complete the task described in the user message below. Be thorough and specific in your output.
When you finish, summarize what you did and any important findings.

{{.SkillPrompt}}
{{.SecretPrompt}}
{{.BrowserPrompt}}
`)

var backgroundTaskAgentUserTemplate = MustRegisterTemplate("backgroundTaskAgentUser", `{{.Instruction}}`)

// StepCodeReviewAgent compares step descriptions with their saved main.py scripts to detect drift.
type StepCodeReviewAgent struct {
	*agents.BaseOrchestratorAgent
}

func newStepCodeReviewAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *StepCodeReviewAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(config, logger, tracer, agents.TodoPlannerExecutionQAAgentType, eventBridge)
	return &StepCodeReviewAgent{BaseOrchestratorAgent: baseAgent}
}

func (agent *StepCodeReviewAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	if agent.BaseOrchestratorAgent.BaseAgent() == nil || agent.BaseOrchestratorAgent.BaseAgent().Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}
	if _, has := templateVars["MainPyAuthoringRules"]; !has {
		templateVars["MainPyAuthoringRules"] = BuildMainPyAuthoringRules() + BrowserAuthoringRulesFromTemplateVars(templateVars)
	}
	var systemPrompt, userMessage strings.Builder
	if err := reviewStepCodeAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := reviewStepCodeAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}
	inputProcessor := func(map[string]string) string { return userMessage.String() }
	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, struct{}{}, systemPrompt.String(), true)
	if err != nil {
		return "", nil, err
	}
	return result, updatedHistory, nil
}

// WorkflowPlanReviewAgent critically reviews current workflow design and dependent artifacts without modifying files.
type WorkflowPlanReviewAgent struct {
	*agents.BaseOrchestratorAgent
}

func newWorkflowPlanReviewAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowPlanReviewAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(config, logger, tracer, agents.TodoPlannerExecutionQAAgentType, eventBridge)
	return &WorkflowPlanReviewAgent{BaseOrchestratorAgent: baseAgent}
}

func (agent *WorkflowPlanReviewAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	if agent.BaseOrchestratorAgent.BaseAgent() == nil || agent.BaseOrchestratorAgent.BaseAgent().Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}
	var systemPrompt, userMessage strings.Builder
	if err := reviewPlanAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := reviewPlanAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}
	inputProcessor := func(map[string]string) string { return userMessage.String() }
	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, struct{}{}, systemPrompt.String(), true)
	if err != nil {
		return "", nil, err
	}
	return result, updatedHistory, nil
}

// WorkflowTimingReviewAgent reviews actual run latency and speedup opportunities.
type WorkflowTimingReviewAgent struct {
	*agents.BaseOrchestratorAgent
}

func newWorkflowTimingReviewAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowTimingReviewAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(config, logger, tracer, agents.TodoPlannerExecutionQAAgentType, eventBridge)
	return &WorkflowTimingReviewAgent{BaseOrchestratorAgent: baseAgent}
}

func (agent *WorkflowTimingReviewAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	if agent.BaseOrchestratorAgent.BaseAgent() == nil || agent.BaseOrchestratorAgent.BaseAgent().Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}
	var systemPrompt, userMessage strings.Builder
	if err := reviewWorkflowTimingAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := reviewWorkflowTimingAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}
	inputProcessor := func(map[string]string) string { return userMessage.String() }
	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, struct{}{}, systemPrompt.String(), true)
	if err != nil {
		return "", nil, err
	}
	return result, updatedHistory, nil
}

// WorkflowCostReviewAgent reviews actual run costs and cost-reduction opportunities.
type WorkflowCostReviewAgent struct {
	*agents.BaseOrchestratorAgent
}

func newWorkflowCostReviewAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowCostReviewAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(config, logger, tracer, agents.TodoPlannerExecutionQAAgentType, eventBridge)
	return &WorkflowCostReviewAgent{BaseOrchestratorAgent: baseAgent}
}

func (agent *WorkflowCostReviewAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	if agent.BaseOrchestratorAgent.BaseAgent() == nil || agent.BaseOrchestratorAgent.BaseAgent().Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}
	var systemPrompt, userMessage strings.Builder
	if err := reviewWorkflowCostsAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := reviewWorkflowCostsAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}
	inputProcessor := func(map[string]string) string { return userMessage.String() }
	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, struct{}{}, systemPrompt.String(), true)
	if err != nil {
		return "", nil, err
	}
	return result, updatedHistory, nil
}

// runReviewPlanAgent performs a read-only critical review of the current workflow design and dependent artifacts.
func (iwm *InteractiveWorkshopManager) runReviewPlanAgent(ctx context.Context, targetRunFolder string, focus string) (string, error) {
	workspacePath := iwm.controller.GetWorkspacePath()
	logger := iwm.controller.GetLogger()

	planJSON := ""
	if planContent, err := iwm.controller.ReadWorkspaceFile(ctx, "planning/plan.json"); err == nil {
		planJSON = planContent
	}

	stepConfigSummary := ""
	if stepConfigs, err := iwm.controller.ReadStepConfigs(ctx); err == nil && len(stepConfigs) > 0 {
		var sb strings.Builder
		for _, sc := range stepConfigs {
			mode := "agentic"
			declaredMode := ""
			successfulRuns := 0
			lockLearnings := false
			lockCode := false
			learningAccess := ""
			learningObjective := ""
			learningsWriteMethod := ""
			learningOptedIn := false
			kbAccess := ""
			kbContribution := ""
			kbWriteMethod := ""
			enabledSkills := []string{}
			reviewNotes := ""
			descriptionReviewed := false
			if sc.AgentConfigs != nil {
				if isScriptedExecutionModeConfig(sc.AgentConfigs) {
					mode = "scripted"
				}
				declaredMode = sc.AgentConfigs.DeclaredExecutionMode
				if sc.AgentConfigs.SuccessfulRuns != nil {
					successfulRuns = *sc.AgentConfigs.SuccessfulRuns
				}
				if sc.AgentConfigs.LockLearnings != nil {
					lockLearnings = *sc.AgentConfigs.LockLearnings
				}
				if sc.AgentConfigs.LockCode != nil {
					lockCode = *sc.AgentConfigs.LockCode
				}
				learningAccess = resolveLearningsAccess(sc.AgentConfigs)
				learningObjective = sc.AgentConfigs.LearningObjective
				learningsWriteMethod = resolveLearningsWriteMethod(sc.AgentConfigs)
				learningOptedIn = learningAccess == LearningsAccessReadWrite && strings.TrimSpace(learningObjective) != ""
				kbAccess = sc.AgentConfigs.KnowledgebaseAccess
				kbContribution = sc.AgentConfigs.KnowledgebaseContribution
				kbWriteMethod = resolveKnowledgebaseWriteMethod(sc.AgentConfigs)
				enabledSkills = append([]string(nil), sc.AgentConfigs.EnabledSkills...)
				reviewNotes = sc.AgentConfigs.ReviewNotes
				if sc.AgentConfigs.DescriptionReviewed != nil {
					descriptionReviewed = *sc.AgentConfigs.DescriptionReviewed
				}
			}
			sb.WriteString(fmt.Sprintf("- %s: mode=%s, declared_mode=%s, successful_runs=%d, lock_learnings=%v, lock_code=%v, enabled_skills=%v, learnings_access=%s, learning_objective=%q, learnings_write_method=%s, learning_opted_in=%v, kb_access=%s, kb_contribution=%q, kb_write_method=%s, description_reviewed=%v, review_notes=%q\n", sc.ID, mode, declaredMode, successfulRuns, lockLearnings, lockCode, enabledSkills, learningAccess, learningObjective, learningsWriteMethod, learningOptedIn, kbAccess, kbContribution, kbWriteMethod, descriptionReviewed, reviewNotes))
		}
		stepConfigSummary = sb.String()
	}

	if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ review_plan: failed to reload plan for objective: %v (using cached value)", err))
	}
	workflowObjective, workflowSuccessCriteria := iwm.controller.ResolveWorkflowObjective(ctx)

	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/builder", workspacePath),
		fmt.Sprintf("%s/db", workspacePath),
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/knowledgebase", workspacePath),
		fmt.Sprintf("%s/reports", workspacePath),
		fmt.Sprintf("%s/evaluation", workspacePath),
		fmt.Sprintf("%s/variables", workspacePath),
	}
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, []string{})

	llmConfigToUse := iwm.controller.selectMaintenanceLLM("review_plan agent")
	if llmConfigToUse == nil {
		return "", fmt.Errorf("no valid LLM configuration for review_plan agent")
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM("review-plan-agent", 50, agents.OutputFormatStructured, llmConfigToUse)
	// Isolate in a fresh tmp dir; don't project CLAUDE.md/.claude into the
	// builder's live workflow folder. See improve-db-agent above.
	config.IsolateCodingAgentWorkspace = true
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)
	config.ServerNames = []string{mcpclient.NoServers}
	defer iwm.configureWorkshopToolAgentSession(config, "review-plan", readPaths, []string{})()

	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowPlanReviewAgent(cfg, log, tracer, eventBridge)
	}
	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx, config, "review-plan", 0, 0, "review-plan",
		createAgentFunc, phaseTools, phaseExecutors, true,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create review_plan agent: %w", err)
	}

	workflowSelectedSkills := strings.Join(iwm.controller.GetSelectedSkills(), ", ")
	templateVars := map[string]string{
		"WorkspacePath":           workspacePath,
		"AbsWorkspacePath":        absPromptWorkspacePath(workspacePath),
		"TargetRunFolder":         targetRunFolder,
		"PlanJSON":                planJSON,
		"StepConfigSummary":       stepConfigSummary,
		"WorkflowObjective":       workflowObjective,
		"WorkflowSuccessCriteria": workflowSuccessCriteria,
		"WorkflowSelectedSkills":  workflowSelectedSkills,
		"Focus":                   focus,
		"SessionID":               iwm.sessionID,
		"WorkflowID":              iwm.workflowID,
	}

	logger.Info(fmt.Sprintf("🧪 Running review_plan agent (target_run_folder: %q, objective: %q, success_criteria: %q, focus: %q)", targetRunFolder, workflowObjective, workflowSuccessCriteria, focus))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("review_plan agent failed: %w", err)
	}
	return result, nil
}

// runReviewWorkflowTimingAgent reviews actual run latency and speedup opportunities.
func (iwm *InteractiveWorkshopManager) runReviewWorkflowTimingAgent(ctx context.Context, targetRunFolder string, focus string) (string, error) {
	workspacePath := iwm.controller.GetWorkspacePath()
	logger := iwm.controller.GetLogger()

	planJSON := ""
	if planContent, err := iwm.controller.ReadWorkspaceFile(ctx, "planning/plan.json"); err == nil {
		planJSON = planContent
	}

	stepConfigSummary := ""
	if stepConfigs, err := iwm.controller.ReadStepConfigs(ctx); err == nil && len(stepConfigs) > 0 {
		var sb strings.Builder
		for _, sc := range stepConfigs {
			mode := "agentic"
			declaredMode := ""
			successfulRuns := 0
			lockLearnings := false
			lockCode := false
			if sc.AgentConfigs != nil {
				if isScriptedExecutionModeConfig(sc.AgentConfigs) {
					mode = "scripted"
				}
				declaredMode = sc.AgentConfigs.DeclaredExecutionMode
				if sc.AgentConfigs.SuccessfulRuns != nil {
					successfulRuns = *sc.AgentConfigs.SuccessfulRuns
				}
				if sc.AgentConfigs.LockLearnings != nil {
					lockLearnings = *sc.AgentConfigs.LockLearnings
				}
				if sc.AgentConfigs.LockCode != nil {
					lockCode = *sc.AgentConfigs.LockCode
				}
			}
			sb.WriteString(fmt.Sprintf("- %s: mode=%s, declared_mode=%s, successful_runs=%d, lock_learnings=%v, lock_code=%v\n", sc.ID, mode, declaredMode, successfulRuns, lockLearnings, lockCode))
		}
		stepConfigSummary = sb.String()
	}

	if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ review_workflow_timing: failed to reload plan for objective: %v (using cached value)", err))
	}
	workflowObjective, workflowSuccessCriteria := iwm.controller.ResolveWorkflowObjective(ctx)

	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/evaluation", workspacePath),
	}
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, []string{})

	llmConfigToUse := iwm.controller.selectMaintenanceLLM("review_workflow_timing agent")
	if llmConfigToUse == nil {
		return "", fmt.Errorf("no valid LLM configuration for review_workflow_timing agent")
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM("review-workflow-timing-agent", 60, agents.OutputFormatStructured, llmConfigToUse)
	// Isolate in a fresh tmp dir; don't project CLAUDE.md/.claude into the
	// builder's live workflow folder. See improve-db-agent above.
	config.IsolateCodingAgentWorkspace = true
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)
	config.ServerNames = []string{mcpclient.NoServers}
	defer iwm.configureWorkshopToolAgentSession(config, "review-workflow-timing", readPaths, []string{})()

	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowTimingReviewAgent(cfg, log, tracer, eventBridge)
	}
	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx, config, "review-workflow-timing", 0, 0, "review-workflow-timing",
		createAgentFunc, phaseTools, phaseExecutors, true,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create review_workflow_timing agent: %w", err)
	}

	templateVars := map[string]string{
		"WorkspacePath":           workspacePath,
		"AbsWorkspacePath":        absPromptWorkspacePath(workspacePath),
		"TargetRunFolder":         targetRunFolder,
		"PlanJSON":                planJSON,
		"StepConfigSummary":       stepConfigSummary,
		"WorkflowObjective":       workflowObjective,
		"WorkflowSuccessCriteria": workflowSuccessCriteria,
		"Focus":                   focus,
		"SessionID":               iwm.sessionID,
		"WorkflowID":              iwm.workflowID,
	}

	logger.Info(fmt.Sprintf("⏱️ Running review_workflow_timing agent (target_run_folder: %q, objective: %q, success_criteria: %q, focus: %q)", targetRunFolder, workflowObjective, workflowSuccessCriteria, focus))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("review_workflow_timing agent failed: %w", err)
	}
	return result, nil
}

// runReviewWorkflowCostsAgent reviews actual run costs and cost-reduction opportunities.
func (iwm *InteractiveWorkshopManager) runReviewWorkflowCostsAgent(ctx context.Context, targetRunFolder string, focus string) (string, error) {
	workspacePath := iwm.controller.GetWorkspacePath()
	logger := iwm.controller.GetLogger()

	planJSON := ""
	if planContent, err := iwm.controller.ReadWorkspaceFile(ctx, "planning/plan.json"); err == nil {
		planJSON = planContent
	}

	stepConfigSummary := ""
	if stepConfigs, err := iwm.controller.ReadStepConfigs(ctx); err == nil && len(stepConfigs) > 0 {
		var sb strings.Builder
		for _, sc := range stepConfigs {
			mode := "agentic"
			declaredMode := ""
			successfulRuns := 0
			lockLearnings := false
			lockCode := false
			if sc.AgentConfigs != nil {
				if isScriptedExecutionModeConfig(sc.AgentConfigs) {
					mode = "scripted"
				}
				declaredMode = sc.AgentConfigs.DeclaredExecutionMode
				if sc.AgentConfigs.SuccessfulRuns != nil {
					successfulRuns = *sc.AgentConfigs.SuccessfulRuns
				}
				if sc.AgentConfigs.LockLearnings != nil {
					lockLearnings = *sc.AgentConfigs.LockLearnings
				}
				if sc.AgentConfigs.LockCode != nil {
					lockCode = *sc.AgentConfigs.LockCode
				}
			}
			sb.WriteString(fmt.Sprintf("- %s: mode=%s, declared_mode=%s, successful_runs=%d, lock_learnings=%v, lock_code=%v\n", sc.ID, mode, declaredMode, successfulRuns, lockLearnings, lockCode))
		}
		stepConfigSummary = sb.String()
	}

	if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ review_workflow_costs: failed to reload plan for objective: %v (using cached value)", err))
	}
	workflowObjective, workflowSuccessCriteria := iwm.controller.ResolveWorkflowObjective(ctx)

	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/evaluation", workspacePath),
		fmt.Sprintf("%s/costs", workspacePath),
	}
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, []string{})

	llmConfigToUse := iwm.controller.selectMaintenanceLLM("review_workflow_costs agent")
	if llmConfigToUse == nil {
		return "", fmt.Errorf("no valid LLM configuration for review_workflow_costs agent")
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM("review-workflow-costs-agent", 60, agents.OutputFormatStructured, llmConfigToUse)
	// Isolate in a fresh tmp dir; don't project CLAUDE.md/.claude into the
	// builder's live workflow folder. See improve-db-agent above.
	config.IsolateCodingAgentWorkspace = true
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)
	config.ServerNames = []string{mcpclient.NoServers}
	defer iwm.configureWorkshopToolAgentSession(config, "review-workflow-costs", readPaths, []string{})()

	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowCostReviewAgent(cfg, log, tracer, eventBridge)
	}
	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx, config, "review-workflow-costs", 0, 0, "review-workflow-costs",
		createAgentFunc, phaseTools, phaseExecutors, true,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create review_workflow_costs agent: %w", err)
	}
	if agent.GetBaseAgent() != nil && agent.GetBaseAgent().Agent() != nil {
		registerGetCostSummaryTool(iwm, agent.GetBaseAgent().Agent(), logger)
	}

	templateVars := map[string]string{
		"WorkspacePath":           workspacePath,
		"AbsWorkspacePath":        absPromptWorkspacePath(workspacePath),
		"TargetRunFolder":         targetRunFolder,
		"PlanJSON":                planJSON,
		"StepConfigSummary":       stepConfigSummary,
		"WorkflowObjective":       workflowObjective,
		"WorkflowSuccessCriteria": workflowSuccessCriteria,
		"Focus":                   focus,
		"SessionID":               iwm.sessionID,
		"WorkflowID":              iwm.workflowID,
	}

	logger.Info(fmt.Sprintf("💸 Running review_workflow_costs agent (target_run_folder: %q, objective: %q, success_criteria: %q, focus: %q)", targetRunFolder, workflowObjective, workflowSuccessCriteria, focus))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("review_workflow_costs agent failed: %w", err)
	}
	return result, nil
}

// runReviewStepCodeAgent compares step descriptions with saved main.py scripts to detect drift.
func (iwm *InteractiveWorkshopManager) runReviewStepCodeAgent(ctx context.Context, stepID string, focus string) (string, error) {
	workspacePath := iwm.controller.GetWorkspacePath()
	logger := iwm.controller.GetLogger()

	if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
		return "", fmt.Errorf("failed to load plan: %w", err)
	}
	if iwm.controller.approvedPlan == nil {
		return "", fmt.Errorf("no approved plan found")
	}

	workflowObjective, _ := iwm.controller.ResolveWorkflowObjective(ctx)

	// Collect saved code to review — either a specific step/scoring agent or all
	// saved main.py scripts across workflow, evaluation, and scoring code.
	allSteps := collectAllSteps(iwm.controller.approvedPlan.Steps)
	stepConfigs, _ := iwm.controller.ReadStepConfigsFromSubdir(ctx, "planning")
	workflowStepConfigMap := map[string]*StepConfig{}
	for i := range stepConfigs {
		workflowStepConfigMap[stepConfigs[i].ID] = &stepConfigs[i]
	}
	evalStepConfigs, _ := iwm.controller.ReadStepConfigsFromSubdir(ctx, "evaluation")
	evalStepConfigMap := map[string]*StepConfig{}
	for i := range evalStepConfigs {
		evalStepConfigMap[evalStepConfigs[i].ID] = &evalStepConfigs[i]
	}
	evalPlanExists, evalPlan, evalPlanErr := iwm.controller.checkExistingEvaluationPlan(ctx, "evaluation/evaluation_plan.json")
	if evalPlanErr != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to read evaluation/evaluation_plan.json for review_step_code: %v", evalPlanErr))
	}

	var stepsToReview strings.Builder
	reviewCount := 0

	appendCodeReviewTarget := func(scope string, sid string, title string, description string, validation *ValidationSchema, sc *StepConfig) {
		if stepID != "" && sid != stepID {
			return
		}
		scriptRelPath := fmt.Sprintf("learnings/%s/main.py", sid)
		scriptContent, scriptErr := iwm.controller.ReadWorkspaceFile(ctx, scriptRelPath)
		hasSavedScript := scriptErr == nil && strings.TrimSpace(scriptContent) != ""
		isScriptedConfig := sc != nil && sc.AgentConfigs != nil && isScriptedExecutionModeConfig(sc.AgentConfigs)

		if !hasSavedScript && !isScriptedConfig {
			if stepID != "" {
				stepsToReview.WriteString(fmt.Sprintf("### %s\n- **Scope**: %s\n- **Status**: NO_SAVED_CODE — this target is not configured for saved code and no learnings/%s/main.py exists\n\n", sid, scope, sid))
				reviewCount++
			}
			return
		}

		validationSchema := ""
		if validation != nil {
			if schemaBytes, jsonErr := json.Marshal(validation); jsonErr == nil {
				validationSchema = string(schemaBytes)
			}
		}
		if validationSchema == "" && sc != nil && sc.ValidationSchema != nil {
			if schemaBytes, jsonErr := json.Marshal(sc.ValidationSchema); jsonErr == nil {
				validationSchema = string(schemaBytes)
			}
		}

		stepsToReview.WriteString(fmt.Sprintf("---\n### Step: %s\n", sid))
		stepsToReview.WriteString(fmt.Sprintf("**Scope**: %s\n", scope))
		stepsToReview.WriteString(fmt.Sprintf("**Title**: %s\n", title))
		stepsToReview.WriteString(fmt.Sprintf("\n**Description**:\n%s\n", description))

		if validationSchema != "" {
			stepsToReview.WriteString(fmt.Sprintf("\n**Validation Schema**:\n```json\n%s\n```\n", validationSchema))
		}

		if scriptErr != nil || strings.TrimSpace(scriptContent) == "" {
			stepsToReview.WriteString("\n**Saved Script**: ⚠️ No main.py found in learnings\n\n")
		} else {
			// Run static review too
			staticIssues := reviewMainPyScript(scriptContent)
			stepsToReview.WriteString(fmt.Sprintf("\n**Saved Script** (`learnings/%s/main.py`):\n```python\n%s\n```\n", sid, scriptContent))
			if !isScriptedConfig {
				stepsToReview.WriteString("\n**Mode Fit Issue**: ⚠️ This step is not declared scripted but still has a saved main.py. For agentic steps, this file is stale artifact debt and should be deleted; agentic does not run or maintain persistent main.py.\n")
			}
			if len(staticIssues) > 0 {
				stepsToReview.WriteString("\n**Static Analysis Issues**:\n")
				for idx, issue := range staticIssues {
					stepsToReview.WriteString(fmt.Sprintf("%d. %s\n", idx+1, issue))
				}
			}
			stepsToReview.WriteString("\n")
		}
		reviewCount++
	}

	for _, info := range allSteps {
		sid := info.Step.GetID()
		appendCodeReviewTarget("workflow", sid, info.Step.GetTitle(), info.Step.GetDescription(), info.Step.GetValidationSchema(), workflowStepConfigMap[sid])
	}

	if evalPlanExists && evalPlan != nil {
		for _, evalStep := range evalPlan.Steps {
			if evalStep == nil {
				continue
			}
			appendCodeReviewTarget("evaluation", evalStep.ID, evalStep.Title, evalStep.Description, evalStep.PreValidation, evalStepConfigMap[evalStep.ID])
		}
	}

	if reviewCount == 0 {
		return "No saved main.py scripts found to review across workflow steps or evaluation steps.", nil
	}

	// Set up read-only folder guard
	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/planning", workspacePath),
		fmt.Sprintf("%s/evaluation", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
	}
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, []string{})

	llmConfigToUse := iwm.controller.selectMaintenanceLLM("review_step_code agent")
	if llmConfigToUse == nil {
		return "", fmt.Errorf("no valid LLM configuration for review_step_code agent")
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM("review-step-code-agent", 50, agents.OutputFormatStructured, llmConfigToUse)
	// Isolate in a fresh tmp dir; don't project CLAUDE.md/.claude into the
	// builder's live workflow folder. See improve-db-agent above.
	config.IsolateCodingAgentWorkspace = true
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)
	config.ServerNames = []string{mcpclient.NoServers}
	defer iwm.configureWorkshopToolAgentSession(config, "review-step-code", readPaths, []string{})()

	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newStepCodeReviewAgent(cfg, log, tracer, eventBridge)
	}
	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx, config, "review-step-code", 0, 0, "review-step-code",
		createAgentFunc, phaseTools, phaseExecutors, true,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create review_step_code agent: %w", err)
	}

	templateVars := map[string]string{
		"WorkspacePath":     workspacePath,
		"AbsWorkspacePath":  absPromptWorkspacePath(workspacePath),
		"WorkflowObjective": workflowObjective,
		"StepsToReview":     stepsToReview.String(),
		"Focus":             focus,
		// Browser-authoring rules slot is populated inside the agent's Execute
		// method via BrowserAuthoringRulesFromTemplateVars; without this flag
		// the review agent is blind to the durable-selector contract it's
		// supposed to enforce on main.py drift.
		"HasBrowserAccess": fmt.Sprintf("%t", iwm.controller.HasBrowserCapability()),
	}

	logger.Info(fmt.Sprintf("🔍 Running review_step_code agent (%d steps, focus: %q)", reviewCount, focus))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("review_step_code agent failed: %w", err)
	}
	return result, nil
}

func buildGoalAdvisorStageHeader(pulseRunID, focus string) string {
	var sb strings.Builder
	if strings.TrimSpace(pulseRunID) != "" {
		sb.WriteString("Pulse run id: ")
		sb.WriteString(strings.TrimSpace(pulseRunID))
		sb.WriteString("\n")
	}
	if strings.TrimSpace(focus) != "" {
		sb.WriteString("Focus from Pulse Gate: ")
		sb.WriteString(strings.TrimSpace(focus))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func buildGoalAdvisorAdvisorInstruction(pulseRunID, focus string) string {
	var sb strings.Builder
	sb.WriteString("Goal Advisor pipeline stage 1/3: ADVISOR DRAFT.\n\n")
	if header := buildGoalAdvisorStageHeader(pulseRunID, focus); header != "" {
		sb.WriteString(header)
		sb.WriteString("\n\n")
	}
	sb.WriteString(`
Use ` + "`get_workflow_command_guidance(kind=\"goal-advisor\", focus=\"Pulse-selected Goal Advisor module; expert strategy advisor, not routine maintenance\")`" + ` as the strategy playbook, but this stage is read-only.

Read the workflow evidence yourself: builder/improve.html including the Pulse Gate/worklist, soul/soul.md, latest run/eval evidence, planning/changelog, report/dashboard evidence, answered human inputs in db/db.sqlite/report_human_inputs, and queued Chief of Staff recommendations (.cos-rec, especially data-status="queued_goal_advisor").

Analysis budget:
- Spend this stage on goal reality, strategy ceiling, credible alternatives, and experiment evidence.
- From builder/improve.html read only verdicts, the active .advisor-experiment, recent Goal Advisor entries, answered outcomes, and queued Chief of Staff recommendations. Use targeted search/extraction when it is large.
- Do not inspect CSS, visual design, unrelated timeline history, or page formatting. Do not load HTML style/skeleton guidance.
- Return the strategic verdict even if the report markup is imperfect; formatting belongs to Report Health.

Strict boundaries for this Advisor stage:
- Do NOT launch nested maintenance reviewers or call mark_human_input_consumed, create_human_input_request, notify_user, backup, publish, or mark_pulse_module_result.
- Do NOT modify plan/config/eval/report/HTML files.
- Produce an evidence-backed draft only. The Critic and Finalizer stages decide what survives.

Think like an expert operator in the workflow's domain: look for why goals are not moving, strategy assumptions the user missed, better measurement, new channels/approaches, or places where the plan is capped even when execution is clean. Also apply a 10x counterfactual when this is a healthy-headroom review: estimate the current strategy ceiling and ask what materially different approach could change the order of magnitude. Treat 10x as a thinking lens, not a promise.

Before drafting, inspect builder/improve.html for .advisor-experiment cards. Exactly one experiment may be active. Active statuses are proposed, deferred, approved, running, measuring, and blocked; terminal statuses are adopted, rejected, and retired. If one is active, advance or measure that same experiment instead of inventing another. If none is active, at most one new thesis may survive this draft.

Return a concise structured draft with these sections:
- Review mode: recovery | headroom | active_experiment | approved_answer.
- Active experiment: stable id + status, or none.
- Verdict: no_action | apply_approved_answer | propose_user_decision | log_advisor_idea | blocked.
- Plain-language takeaway.
- Goal alignment: cite the exact success criterion or objective.
- Evidence used: concrete paths/run ids/report widgets/HTML cards.
- Current baseline and current strategy ceiling.
- 10x thesis: the one highest-leverage materially different approach, or none with reason.
- Advisor hypothesis: what the current plan may be missing.
- Bounded experiment: exact intended plan/config/eval/report edits if any, primary success metric, guardrails, review checkpoint, and rollback condition.
- Expected impact.
- Risks, assumptions, and what could falsify this idea.
- User decision needed: yes/no; if yes, proposed options approve/reject/defer with short descriptions.
- Routine-maintenance deferrals: any bug/KB/DB/learning/report issue that should go to Pulse modules instead.
`)
	return strings.TrimSpace(sb.String())
}

func buildGoalAdvisorCriticInstruction(pulseRunID, focus, advisorOutput string) string {
	var sb strings.Builder
	sb.WriteString("Goal Advisor pipeline stage 2/3: INDEPENDENT CRITIC.\n\n")
	if header := buildGoalAdvisorStageHeader(pulseRunID, focus); header != "" {
		sb.WriteString(header)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Advisor draft to critique:\n\n")
	sb.WriteString(advisorOutput)
	sb.WriteString(`

Your job is to challenge the Advisor draft before anything is logged, proposed, or applied.

Strict boundaries for this Critic stage:
- Read evidence yourself when needed; do not rely only on the Advisor's wording.
- Do NOT launch nested maintenance reviewers or call mark_human_input_consumed, create_human_input_request, notify_user, backup, publish, or mark_pulse_module_result.
- Do NOT modify plan/config/eval/report/HTML files.

Critique against these checks:
- Is the proposal aligned with soul/soul.md objective and success criteria?
- Is every important claim backed by concrete run/eval/report/HTML/db evidence?
- Is this really strategy/measurement work, or should routine Pulse Bug Review/KB/DB/learning/report modules handle it?
- Does it hallucinate unavailable data, user intent, external facts, costs, or success?
- Is the 10x thesis materially different from incremental tuning, while still being grounded enough to test? Treat 10x as a thinking lens, not a promised result.
- Did the Advisor preserve the current successful baseline and define a primary metric, guardrails, review checkpoint, and rollback condition?
- Does builder/improve.html already contain an active .advisor-experiment? If yes, reject any second active proposal and require the existing stable id to be advanced or closed.
- Are the intended edits specific enough to be applied safely later?
- Does it introduce unacceptable risk, scope creep, or cost?
- Does it need user approval, or is it only an observation?

Return a structured critic verdict:
- Verdict: approve | revise | reject | needs_user | no_action.
- Critical objections.
- Missing evidence.
- What to remove or downgrade.
- What the Finalizer is allowed to do.
- What the Finalizer must not do.
- If user approval is needed, the exact question/options that are safe to present.
`)
	return strings.TrimSpace(sb.String())
}

type goalAdvisorApprovedPlanProposal struct {
	ID               string
	Context          string
	Evidence         string
	SelectedOptionID string
	Note             string
}

func formatGoalAdvisorApprovedPlanProposals(proposals []goalAdvisorApprovedPlanProposal) string {
	if len(proposals) == 0 {
		return "- none verified in db/db.sqlite/report_human_inputs"
	}
	var sb strings.Builder
	for _, proposal := range proposals {
		sb.WriteString("- input_id: ")
		sb.WriteString(proposal.ID)
		sb.WriteString("\n  selected_option_id: ")
		sb.WriteString(proposal.SelectedOptionID)
		if strings.TrimSpace(proposal.Context) != "" {
			sb.WriteString("\n  context: ")
			sb.WriteString(proposal.Context)
		}
		if strings.TrimSpace(proposal.Note) != "" {
			sb.WriteString("\n  user_note: ")
			sb.WriteString(proposal.Note)
		}
		if strings.TrimSpace(proposal.Evidence) != "" {
			sb.WriteString("\n  evidence: ")
			sb.WriteString(proposal.Evidence)
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func buildGoalAdvisorFinalizerInstruction(pulseRunID, focus, advisorOutput, criticOutput string, approvedProposals []goalAdvisorApprovedPlanProposal, planMutationToolsEnabled bool) string {
	var sb strings.Builder
	sb.WriteString("Goal Advisor pipeline stage 3/3: FINALIZER.\n\n")
	if header := buildGoalAdvisorStageHeader(pulseRunID, focus); header != "" {
		sb.WriteString(header)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Advisor draft:\n\n")
	sb.WriteString(advisorOutput)
	sb.WriteString("\n\nCritic verdict:\n\n")
	sb.WriteString(criticOutput)
	sb.WriteString("\n\nCode-enforced mutation gate:\n")
	if planMutationToolsEnabled {
		sb.WriteString("- plan/config/eval mutation tools: ENABLED because code verified at least one answered goal_advisor plan-proposal-* request with selected_option_id=approve and the Critic verdict starts with approve.\n")
		sb.WriteString("- mark_human_input_consumed: ENABLED for the approved proposal ids listed below.\n")
	} else {
		sb.WriteString("- plan/config/eval mutation tools: DISABLED. You do not have create/update/delete plan/config/eval tools in this stage.\n")
		sb.WriteString("- mark_human_input_consumed: DISABLED. Do not claim to consume or apply an answer.\n")
	}
	sb.WriteString("Verified approved plan proposals:\n")
	sb.WriteString(formatGoalAdvisorApprovedPlanProposals(approvedProposals))
	sb.WriteString(`

You are the only stage allowed to make durable changes, and only within the critic-approved bounds.

Analysis-first close-out budget:
- The Advisor and Critic results are already complete. Do not repeat their research or turn this stage into an HTML design task.
- Re-read only the current Advisor experiment/card and the stable insertion anchor immediately before writing.
- Make at most one targeted builder/improve.html patch: update the existing Advisor card in place, or insert one new semantic Advisor card when a material proposal/state change exists.
- Do not load review-improve-log-skeleton or html-output, migrate the page, rewrite CSS, restyle unrelated cards, reorder history, or regenerate the full file. Route format problems to Report Health.
- If the verdict is no_action and no experiment, assumption, question outcome, or recommendation status changed, do not edit builder/improve.html merely to log activity.
- Update builder/card.progress.html only when goal status, active experiment, or the Advisor decision materially changed.

Follow this decision policy:
- If the Critic verdict is reject or no_action: do not change the workflow. Add at most a short skipped/rejected note to builder/improve.html if useful.
- If the Critic verdict is revise and the safe revision is obvious: log/propose only the narrowed version. Do not start a new advisor loop.
- If plan/config/eval mutation tools are ENABLED: apply only the verified approved plan-proposal ids listed above, only within the Critic-approved bounds, call mark_human_input_consumed with the concrete outcome for each applied id, and remove or replace the matching visible question card in builder/improve.html with a short outcome.
- If plan/config/eval mutation tools are DISABLED: do not apply plan/config/eval changes; write only report/proposal updates or create/refine human-input requests.
- If the proposal is new and material: do not change the plan. Create or refresh a create_human_input_request(source="goal_advisor", input_id="plan-proposal-...", options approve/reject/defer) with exact intended edits, rationale, expected impact, risk, and evidence.
- If the idea is useful but not ready for a user decision: log it as a proposal-only Advisor idea in builder/improve.html with evidence and risk.

Experiment lifecycle contract:
- Read the current Advisor card in builder/improve.html before writing. It is the durable experiment source of truth.
- Never leave more than one active .advisor-experiment. Active statuses: proposed, deferred, approved, running, measuring, blocked. Terminal statuses: adopted, rejected, retired.
- Use one stable card for its full lifecycle:
  <div class="entry decision major advisor-experiment" data-advisor-experiment-id="advisor-exp-<stable-slug>" data-input-id="plan-proposal-<stable-slug>" data-status="<status>" data-review-after="<ISO date/time, run id, or outcome milestone>">.
- The visible card must include Current baseline, Current strategy ceiling, 10x thesis, Bounded experiment, Primary success metric, Guardrails, Review checkpoint, Rollback condition, Evidence, and Outcome when measuring/terminal.
- A new material thesis becomes proposed and uses the matching stable plan-proposal slug. Approved edits move that same card to running. At the checkpoint move it to measuring; then adopt only with metric improvement and intact guardrails, reject on user rejection, retire when disproved/stale/rolled back, or block with a concrete unblock condition.
- Update the existing card in place. Do not append lifecycle duplicates. A deferred answer keeps the same experiment active with a future review checkpoint.

Do not launch nested maintenance reviewers. Learning, KB, and DB health are separate generic read-only Pulse reviews.
Do not call mark_pulse_module_result; the parent Pulse turn records the module result after reading your completion.

When writing builder/improve.html, show both:
- Advisor proposal/takeaway.
- Critic verdict/objections.

Overwrite builder/card.progress.html with a compact progress card when useful.

Finish with a concise summary: changed/applied/proposed/skipped/blocked, critic verdict, evidence paths, files touched, and any human input request ids created or consumed.
`)
	return strings.TrimSpace(sb.String())
}

func goalAdvisorCriticApprovesPlanMutation(criticOutput string) bool {
	for _, line := range strings.Split(criticOutput, "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "-*• \t"))
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "verdict:") {
			continue
		}
		verdict := strings.TrimSpace(strings.TrimPrefix(lower, "verdict:"))
		return strings.HasPrefix(verdict, "approve")
	}
	return false
}

func normalizeGoalAdvisorWorkspacePath(workspacePath string) (string, error) {
	cleaned := strings.Trim(filepath.ToSlash(strings.TrimSpace(workspacePath)), "/")
	if cleaned == "" {
		return "", fmt.Errorf("workspace path is required")
	}
	if filepath.IsAbs(workspacePath) || strings.Contains(cleaned, "\x00") {
		return "", fmt.Errorf("invalid workspace path %q", workspacePath)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid workspace path %q", workspacePath)
		}
	}
	return cleaned, nil
}

func isGoalAdvisorHumanInputsMissingTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table: report_human_inputs")
}

func (iwm *InteractiveWorkshopManager) approvedGoalAdvisorPlanProposals(ctx context.Context, workspacePath string) ([]goalAdvisorApprovedPlanProposal, error) {
	normalized, err := normalizeGoalAdvisorWorkspacePath(workspacePath)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(normalized), "db", "db.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `SELECT id, context, evidence, selected_option_id, note
		FROM report_human_inputs
		WHERE workspace_path = ?
			AND source = 'goal_advisor'
			AND status = 'answered'
			AND id LIKE 'plan-proposal-%'
		ORDER BY answered_at DESC, updated_at DESC`, normalized)
	if err != nil {
		if isGoalAdvisorHumanInputsMissingTable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var proposals []goalAdvisorApprovedPlanProposal
	for rows.Next() {
		var proposal goalAdvisorApprovedPlanProposal
		if err := rows.Scan(&proposal.ID, &proposal.Context, &proposal.Evidence, &proposal.SelectedOptionID, &proposal.Note); err != nil {
			return nil, err
		}
		selected := strings.ToLower(strings.TrimSpace(proposal.SelectedOptionID))
		if selected != "approve" && selected != "approved" {
			continue
		}
		proposals = append(proposals, proposal)
	}
	return proposals, rows.Err()
}

func truncateGoalAdvisorStageOutput(value string) string {
	const maxChars = 20_000
	value = strings.TrimSpace(value)
	if len(value) <= maxChars {
		return value
	}
	half := maxChars / 2
	return strings.TrimSpace(value[:half] + "\n\n... [Goal Advisor stage output truncated for next-stage review] ...\n\n" + value[len(value)-half:])
}

func (iwm *InteractiveWorkshopManager) runGoalAdvisorStageAgent(ctx context.Context, name string, instruction string, access goalAdvisorStageAccess) (string, error) {
	logger := iwm.controller.GetLogger()
	workspacePath := iwm.controller.GetWorkspacePath()
	stageAgentIdentity := newWorkshopStageAgentIdentity(name)
	// Every advisor/reviewer stage owns a separate terminal. Without a
	// goroutine-local execution owner, parallel stages can inherit the parent
	// main-agent identity and repeatedly replace its tmux pane in the UI.
	if parentExecutionID, _ := ctx.Value(orchestrator_events.ParentExecutionIDKey).(string); strings.TrimSpace(parentExecutionID) == "" {
		ctx = context.WithValue(ctx, orchestrator_events.ParentExecutionIDKey, stageAgentIdentity)
	}
	ctx = context.WithValue(ctx, orchestrator_events.IsSubAgentContextKey, true)
	knowledgebasePath := getKnowledgebasePath(workspacePath)
	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		fmt.Sprintf("%s/evaluation", workspacePath),
		knowledgebasePath,
		"Chats",
	}
	writePaths := []string{}
	allowedToolNames := goalAdvisorReadOnlyToolAgentAllowedToolNames()
	switch access {
	case goalAdvisorStageFinalizerProposal:
		writePaths = workshopWritePaths(workspacePath)
		allowedToolNames = goalAdvisorFinalizerProposalToolAgentAllowedToolNames()
	case goalAdvisorStageFinalizerApprovedMutation:
		writePaths = workshopWritePaths(workspacePath)
		allowedToolNames = goalAdvisorFinalizerApprovedToolAgentAllowedToolNames()
	}

	// Agent creation temporarily mutates controller folder-guard and bridge state.
	// Serialize only this setup window; the independent agents execute in parallel
	// after their session-specific guards have been materialized.
	iwm.toolAgentSetupMu.Lock()
	setupRestored := false
	prevReadPaths, prevWritePaths := iwm.controller.GetFolderGuardPaths()
	prevReadPaths = append([]string(nil), prevReadPaths...)
	prevWritePaths = append([]string(nil), prevWritePaths...)
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	restoreSetup := func() {
		if setupRestored {
			return
		}
		iwm.controller.SetWorkspacePathForFolderGuard(prevReadPaths, prevWritePaths)
		setupRestored = true
		iwm.toolAgentSetupMu.Unlock()
	}
	defer restoreSetup()

	llmConfigToUse := iwm.controller.selectMaintenanceLLM(name)
	if llmConfigToUse == nil {
		return "", fmt.Errorf("no valid LLM configuration found for %s", name)
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM(fmt.Sprintf("Background: %s", name), 100, agents.OutputFormatStructured, llmConfigToUse)
	config.IsolateCodingAgentWorkspace = true
	config.UseCodeExecutionMode = false
	config.EnableParallelToolExecution = true
	config.ServerNames = []string{mcpclient.NoServers}
	defer iwm.configureWorkshopToolAgentSession(config, stageAgentIdentity, readPaths, writePaths)()

	toolsToRegister, executorsToUse := filterWorkspaceToolsByName(iwm.controller.WorkspaceTools, iwm.controller.WorkspaceToolExecutors, allowedToolNames)
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowBackgroundTaskAgent(cfg, log, tracer, eventBridge)
	}

	if cab, ok := iwm.controller.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext(stageAgentIdentity, 0, stageAgentIdentity, fmt.Sprintf("Background: %s", name))
	}
	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		stageAgentIdentity,
		0, 0,
		stageAgentIdentity,
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		true,
	)
	if cab, ok := iwm.controller.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}
	if err != nil {
		return "", fmt.Errorf("failed to create %s agent: %w", name, err)
	}

	iwm.registerWorkshopMutationToolsForToolAgent(agent, workspacePath, stageAgentIdentity, allowedToolNames, logger)
	if err := iwm.controller.applyPostSetupToAgent(agent, stageAgentIdentity+"-agent", false); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", stageAgentIdentity, err))
	}
	restoreSetup()

	templateVars := map[string]string{
		"WorkspacePath":    workspacePath,
		"AbsWorkspacePath": absPromptWorkspacePath(workspacePath),
		"Instruction":      instruction,
		"SkillPrompt":      "",
		"SecretPrompt":     "",
		"BrowserPrompt":    "",
	}

	logger.Info(fmt.Sprintf("✨ Running Goal Advisor stage agent: %q (access=%d)", name, access))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("%s agent failed: %w", name, err)
	}
	return result, nil
}

func (iwm *InteractiveWorkshopManager) runGoalAdvisorReviewPipeline(ctx context.Context, pulseRunID, focus string) (string, error) {
	advisorResult, err := iwm.runGoalAdvisorStageAgent(ctx, "Goal Advisor - Advisor", buildGoalAdvisorAdvisorInstruction(pulseRunID, focus), goalAdvisorStageReadOnly)
	if err != nil {
		return fmt.Sprintf("Goal Advisor advisor stage failed: %v", err), err
	}

	advisorForNextStage := truncateGoalAdvisorStageOutput(advisorResult)
	criticResult, err := iwm.runGoalAdvisorStageAgent(ctx, "Goal Advisor - Critic", buildGoalAdvisorCriticInstruction(pulseRunID, focus, advisorForNextStage), goalAdvisorStageReadOnly)
	if err != nil {
		return fmt.Sprintf("Goal Advisor critic stage failed: %v\n\nAdvisor draft:\n%s", err, advisorForNextStage), err
	}

	criticForNextStage := truncateGoalAdvisorStageOutput(criticResult)
	approvedProposals, proposalErr := iwm.approvedGoalAdvisorPlanProposals(ctx, iwm.controller.GetWorkspacePath())
	if proposalErr != nil {
		iwm.controller.GetLogger().Warn(fmt.Sprintf("⚠️ Goal Advisor: failed to read approved plan proposals; finalizer plan tools disabled: %v", proposalErr))
		approvedProposals = nil
	}
	enablePlanMutationTools := len(approvedProposals) > 0 && goalAdvisorCriticApprovesPlanMutation(criticForNextStage)
	finalizerAccess := goalAdvisorStageFinalizerProposal
	if enablePlanMutationTools {
		finalizerAccess = goalAdvisorStageFinalizerApprovedMutation
	}
	finalizerPrompt := buildGoalAdvisorFinalizerInstruction(pulseRunID, focus, advisorForNextStage, criticForNextStage, approvedProposals, enablePlanMutationTools)
	finalizerResult, err := iwm.runGoalAdvisorStageAgent(ctx, "Goal Advisor - Finalizer", finalizerPrompt, finalizerAccess)
	if err != nil {
		return fmt.Sprintf("Goal Advisor finalizer stage failed: %v\n\nAdvisor draft:\n%s\n\nCritic verdict:\n%s", err, advisorForNextStage, criticForNextStage), err
	}

	return strings.TrimSpace(fmt.Sprintf("Goal Advisor pipeline complete.\n\n## Advisor draft\n%s\n\n## Critic verdict\n%s\n\n## Finalizer result\n%s", advisorResult, criticResult, finalizerResult)), nil
}

// WorkflowBackgroundTaskAgent is a standalone agent spawned by run_in_background
type WorkflowBackgroundTaskAgent struct {
	*agents.BaseOrchestratorAgent
}

func newWorkflowBackgroundTaskAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowBackgroundTaskAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		"workshop-background-task", // Must match the manual start/end events in the goroutine for frontend dedup
		eventBridge,
	)
	return &WorkflowBackgroundTaskAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements OrchestratorAgent interface for the background task agent
func (agent *WorkflowBackgroundTaskAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	// Templates
	var systemPrompt, userMessage strings.Builder
	if err := backgroundTaskAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := backgroundTaskAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	// Single-pass execution
	inputProcessor := func(map[string]string) string { return userMessage.String() }

	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(
		ctx, templateVars, inputProcessor,
		conversationHistory, struct{}{},
		systemPrompt.String(), true,
	)
	if err != nil {
		return "", nil, err
	}

	return result, updatedHistory, nil
}

func validateWorkshopScheduleTimezone(timezone string) error {
	if timezone == "" {
		return fmt.Errorf("timezone is required; use an IANA timezone like UTC, Asia/Kolkata, or America/New_York")
	}
	if timezone != "UTC" && !strings.Contains(timezone, "/") {
		return fmt.Errorf("invalid timezone %q: use an IANA timezone like UTC, Asia/Kolkata, or America/New_York; abbreviations like EST, PST, or IST are not accepted", timezone)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: use an IANA timezone like UTC, Asia/Kolkata, or America/New_York", timezone)
	}
	return nil
}

// runBackgroundTodoTaskAgent runs a todo task orchestrator as a background agent.
// Unlike runBackgroundTaskAgent (single-pass), this supports multi-step task management
// and sub-agent delegation via call_generic_agent. Sub-agent completions auto-notify
// the main workshop agent via the subAgentNotifier already set on the controller.
func (iwm *InteractiveWorkshopManager) runBackgroundTodoTaskAgent(ctx context.Context, name, instruction string) (string, error) {
	stepID := fmt.Sprintf("bg-todo-%s-%d", strings.ToLower(strings.ReplaceAll(name, " ", "-")), time.Now().UnixNano()%100000)

	// Build a minimal TodoTaskPlanStep from the instruction
	todoStep := &TodoTaskPlanStep{
		Type: StepTypeTodoTask,
		CommonStepFields: CommonStepFields{
			ID:          stepID,
			Title:       name,
			Description: instruction,
		},
		PredefinedRoutes: nil, // generic agent only
		NextStepID:       "end",
	}

	execCtx := &ExecutionContext{
		SkipHumanInput:    true,
		RunSingleStepOnly: false,
		SingleStepTarget:  -1,
		IsEvaluationMode:  false,
	}

	_, _, err := iwm.controller.executeTodoTaskStep(
		ctx,
		todoStep,
		0,
		&StepProgress{},
		[]string{},
		[]string{},
		0,
		execCtx,
		[]PlanStepInterface{todoStep},
		stepID,
	)
	if err != nil {
		return fmt.Sprintf("Background todo task %q failed: %v", name, err), err
	}
	return fmt.Sprintf("Background todo task %q completed.", name), nil
}

// runBackgroundTaskAgent creates and runs a standalone background agent
func (iwm *InteractiveWorkshopManager) runBackgroundTaskAgent(ctx context.Context, name string, instruction string) (string, error) {
	logger := iwm.controller.GetLogger()

	// --- Folder guard: same as workshop agent ---
	workspacePath := iwm.controller.GetWorkspacePath()
	knowledgebasePath := getKnowledgebasePath(workspacePath)
	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		knowledgebasePath,
		"Chats",
	}
	writePaths := workshopWritePaths(workspacePath)
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// --- LLM: generic run_in_background follows the normal workshop/phase model.
	// Maintenance-heavy agents (Goal Advisor, Bug Review, KB/DB/report review) use
	// their own dedicated runners and selectMaintenanceLLM instead.
	llmConfigToUse := iwm.controller.selectPhaseLLM("background task agent")
	if llmConfigToUse == nil && iwm.presetLLM != nil && iwm.presetLLM.Provider != "" && iwm.presetLLM.ModelID != "" {
		llmConfigToUse = workflowAgentLLMConfig(iwm.presetLLM, iwm.controller.GetFallbacks(), iwm.controller.GetAPIKeys())
	}
	if llmConfigToUse == nil {
		return "", fmt.Errorf("no valid LLM configuration found for background task agent")
	}

	// --- Agent config ---
	config := iwm.controller.CreateStandardAgentConfigWithLLM(fmt.Sprintf("Background: %s", name), 80, agents.OutputFormatStructured, llmConfigToUse)
	isCodeExecMode := iwm.controller.GetUseCodeExecutionMode()
	config.UseCodeExecutionMode = isCodeExecMode
	config.EnableParallelToolExecution = true
	// Run the coding-CLI session in a fresh os.MkdirTemp dir instead of
	// CodingAgentWorkingDir — same protection workflow-step agents get via
	// applyStepConfigToAgentConfig. Background-task agents are spawned by
	// the workshop chat's `run_in_background` tool, which means the
	// workshop chat itself is already attached to the workflow folder
	// with the chat's MCP config; without isolation, the background
	// agent's agy-cli session collides with the chat's session on the
	// same dir with different MCP configs, and the run fails with
	// "agy-cli does not support concurrent sessions ...". File access to
	// the user's workspace continues to flow through the MCP api-bridge
	// tools, which take absolute workspace paths and do not depend on
	// CLI CWD.
	config.IsolateCodingAgentWorkspace = true
	defer iwm.configureWorkshopToolAgentSession(config, "background-task", readPaths, writePaths)()

	// --- Tools: same as default execution agent (all workspace tools) ---
	toolsToRegister, executorsToUse := iwm.controller.prepareCustomTools(nil) // nil = default tools

	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowBackgroundTaskAgent(cfg, log, tracer, eventBridge)
	}

	// PushContext before setup so the shared bridge context is preserved for concurrent agents.
	// setupStandardAgent calls SetOrchestratorContext which overwrites the bridge — without
	// push/pop this corrupts the main agent's metadata when the bg task runs in a goroutine.
	if cab, ok := iwm.controller.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext("background-task", 0, "background-task", fmt.Sprintf("Background: %s", name))
	}

	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"background-task",
		0, 0,
		"background-task",
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		true, // overwriteSystemPrompt — we provide our own
	)

	// Immediately restore bridge context — bg task events use ForceCorrelationIDKey from ctx,
	// not the bridge's current context, so restoring here is safe.
	if cab, ok := iwm.controller.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}

	if err != nil {
		return "", fmt.Errorf("failed to create background task agent: %w", err)
	}

	// --- Post-setup: add skill/secret/browser prompts ---
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return "", fmt.Errorf("base agent is nil after creation")
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return "", fmt.Errorf("mcp agent is nil after creation")
	}

	// Build supplementary prompts
	//
	// Phase 3 rewire: skills attach to the agent directly; the listing
	// goes into the system prompt via mcpagent.ensureSystemPrompt, and
	// CLI adapters project SKILL.md folders to disk. The legacy
	// {{.SkillPrompt}} template variable stays empty — kept for
	// template backward compatibility but no longer carries content.
	//
	// Note: after Phase 5 hard cut, GetEffectiveSkills(nil, ...) returns
	// nil (no fallback to orchestrator.SelectedSkills), so the workshop
	// background-task agent inherits no skills from the workflow-level
	// selection. If a background task needs a specific skill, declare
	// it on the step config it's spawned from.
	skillPrompt := ""
	effectiveSkills := GetEffectiveSkills(nil, iwm.controller.BaseOrchestrator)
	if len(effectiveSkills) > 0 {
		if attached := skills.LoadAttachable(getWorkspaceAPIURL(), effectiveSkills); len(attached) > 0 {
			for _, s := range attached {
				mcpAgent.AttachSkill(s)
			}
		}
	}

	secretPrompt := ""
	effectiveSecrets := GetEffectiveSecrets(iwm.controller.BaseOrchestrator)
	if len(effectiveSecrets) > 0 {
		secretPrompt = BuildWorkflowSecretPrompt(effectiveSecrets)
	}

	// Same trim as workshop main path: emit a one-line pointer to the
	// browser-usage skill instead of the ~5-10KB BuildBrowserInstructions
	// block. Background-task agents fetch the full guide on demand.
	bgBrowserCfg := iwm.controller.resolveBrowserConfig(config.ServerNames, effectiveSkills)
	browserPrompt := ""
	if bgBrowserCfg.HasAgentBrowser {
		browserPrompt = "\n## Browser\n\nThis task has a browser tool available (mode=" + bgBrowserCfg.Mode +
			"). Read `get_reference_doc(kind=\"browser-usage\")` for Builder-specific mode, tab, file, and safety rules. "
		if bgBrowserCfg.HasAgentBrowser && bgBrowserCfg.Mode == "cdp" {
			ports := append([]int{bgBrowserCfg.CdpPort}, bgBrowserCfg.CdpPorts...)
			endpoints := browser.ConfiguredCDPEndpoints(ports)
			endpoint := browser.ConfiguredCDPEndpoint(bgBrowserCfg.CdpPort)
			if len(endpoints) > 0 {
				endpoint = endpoints[0]
			}
			if len(endpoints) > 1 {
				browserPrompt += "This workflow explicitly authorizes independently-profiled Chrome browsers at `" + strings.Join(endpoints, "`, `") + "`. Choose the endpoint matching the intended login/account on every call. "
			}
			browserPrompt += "Every agent_browser call must explicitly include one authorized `--cdp <endpoint>`. Before the first browser action, load the version-matched core skill with `agent_browser(command=\"skills\", args=[\"--cdp\", \"" + endpoint + "\", \"get\", \"core\"], session=\"default\")`; this docs call does not require a selected tab.\n"
		} else {
			browserPrompt += "Before the first browser action, load the version-matched core skill with `agent_browser(command=\"skills\", args=[\"get\", \"core\"], session=\"default\")`.\n"
		}
	}

	// Apply post-setup configuration (folder guard + registry for code execution mode)
	if err := iwm.controller.applyPostSetupToAgent(agent, "background-task-agent", isCodeExecMode); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for background-task-agent: %v", err))
	}

	// --- Template vars ---
	templateVars := map[string]string{
		"WorkspacePath":    workspacePath,
		"AbsWorkspacePath": absPromptWorkspacePath(workspacePath),
		"Instruction":      instruction,
		"SkillPrompt":      skillPrompt,
		"SecretPrompt":     secretPrompt,
		"BrowserPrompt":    browserPrompt,
	}

	// --- Execute ---
	logger.Info(fmt.Sprintf("🚀 Running background task agent: %q", name))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("background task agent failed: %w", err)
	}

	return result, nil
}
