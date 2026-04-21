package step_based_workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	orchestratorevents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Post-step KB update orchestration. Serialized through kbUpdateQueue so concurrent
// step completions can't race on knowledgebase/graph.json writes. The agent reads and
// writes the file directly via shell; Go only schedules and tracks.

// maybeEnqueueKBUpdate enqueues a post-step KB update when all gates hold:
// KB enabled, KB not locked at workflow level, step access grants write,
// knowledgebase_contribution non-empty. Silent no-op otherwise.
// Returns true when enqueued.
func (hcpo *StepBasedWorkflowOrchestrator) maybeEnqueueKBUpdate(
	stepIndex int,
	stepPath string,
	step PlanStepInterface,
) bool {
	if !hcpo.UseKnowledgebase() || hcpo.LockKnowledgebase() {
		return false
	}
	agentConfigs := getAgentConfigs(step)
	kbAccess := resolveKnowledgebaseAccess(agentConfigs, hcpo.UseKnowledgebase())
	if !kbAccessAllowsWrite(kbAccess) {
		return false
	}
	contribution := ""
	if agentConfigs != nil {
		contribution = strings.TrimSpace(agentConfigs.KnowledgebaseContribution)
	}
	if contribution == "" {
		return false
	}

	// Snapshot step state — by the time the worker runs, the next step may already be
	// in flight, so the closure must capture immutable values rather than reading `step`.
	stepID := step.GetID()
	stepTitle := step.GetTitle()
	stepDescription := step.GetDescription()
	stepContextOutput := strings.TrimSpace(step.GetContextOutput().String())
	runFolder := hcpo.selectedRunFolder
	stepLabel := strings.TrimSpace(stepTitle)
	if stepLabel == "" {
		stepLabel = stepID
	}
	if stepLabel == "" {
		stepLabel = stepPath
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📚 Enqueued KB update for step %s (contribution=%d chars)", stepID, len(contribution)))

	// Surface the KB update as a tracked execution so the workshop UI shows it
	// alongside learning, harden, replan, etc. Mirrors the learning-agent pattern
	// at controller_learning.go:410-454. Without this the KB agent runs invisibly
	// and users have no signal that graph.json/notes are about to change.
	execLabel := fmt.Sprintf("KB Update: %s", stepLabel)
	execID := fmt.Sprintf("kb-update-%s-%05d", stepID, time.Now().UnixNano()%100000)
	baseCtx := hcpo.workshopSessionCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	execCtx, cancel := context.WithCancel(baseCtx)
	agentSessionID := fmt.Sprintf("workshop-kb-update-%s-%d", stepID, time.Now().UnixNano())
	execCtx = context.WithValue(execCtx, orchestratorevents.AgentSessionIDKey, agentSessionID)
	execCtx = context.WithValue(execCtx, orchestratorevents.ForceCorrelationIDKey, agentSessionID)
	execCtx = context.WithValue(execCtx, orchestratorevents.IsSubAgentContextKey, true)

	if hcpo.workshopStepRegistry != nil {
		exec := &WorkshopStepExecution{
			ID:             execID,
			StepID:         execLabel,
			AgentSessionID: agentSessionID,
			Status:         WorkshopStepRunning,
			cancel:         cancel,
		}
		hcpo.workshopStepRegistry.Register(exec)
	}
	if hcpo.workshopExecutionNotifier != nil {
		hcpo.workshopExecutionNotifier.OnExecutionStart(WorkshopExecutionStart{
			ID:                execID,
			ParentExecutionID: currentWorkshopParentExecutionID(baseCtx),
			Name:              execLabel,
			Cancel:            cancel,
		})
	}

	enqueueKBUpdateJob(func() {
		var execErr error
		var resultMsg string
		defer func() {
			cancel()
			if hcpo.workshopExecutionNotifier != nil {
				hcpo.workshopExecutionNotifier.OnExecutionComplete(execID, execLabel, resultMsg, nil, execErr)
			}
		}()
		execErr = hcpo.runKBUpdatePhase(execCtx, stepIndex, stepPath, stepID, stepTitle, stepDescription, runFolder, contribution, stepContextOutput)
		if execErr != nil {
			resultMsg = fmt.Sprintf("KB update failed for %s: %v", stepLabel, execErr)
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ KB update phase failed for %s: %v", stepLabel, execErr))
		} else {
			resultMsg = fmt.Sprintf("KB update completed for %s", stepLabel)
			hcpo.GetLogger().Info(fmt.Sprintf("✅ KB update completed for %s", stepLabel))
		}
	})
	return true
}

func (hcpo *StepBasedWorkflowOrchestrator) runKBUpdatePhase(
	ctx context.Context,
	stepIndex int,
	stepPath string,
	stepID string,
	stepTitle string,
	stepDescription string,
	runFolder string,
	contribution string,
	stepContextOutput string,
) error {
	hcpo.GetLogger().Info(fmt.Sprintf("📚 Starting KB update for step %s/%s", stepID, stepPath))

	agentSessionID := fmt.Sprintf("kb-update-%s-%s", stepID, runFolder)
	ctx = context.WithValue(ctx, orchestratorevents.AgentSessionIDKey, agentSessionID)
	ctx = context.WithValue(ctx, orchestratorevents.ForceCorrelationIDKey, agentSessionID)
	ctx = context.WithValue(ctx, orchestratorevents.IsSubAgentContextKey, true)

	// Absolute paths — execution agents already use absolute paths in prompts; match
	// that convention so the KB agent's shell commands work without prefixing.
	docsRoot := GetPromptDocsRoot()
	baseWorkspacePath := hcpo.GetWorkspacePath()
	graphFilePath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBGraphFileName)
	indexFilePath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBIndexFileName)
	notesFolderPath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBNotesFolderName)
	notesIndexPath := filepath.Join(notesFolderPath, KBNotesIndexFileName)

	var runWorkspacePath string
	if runFolder != "" {
		runWorkspacePath = filepath.Join(baseWorkspacePath, "runs", runFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	stepOutputPath := filepath.Join(docsRoot, runWorkspacePath, "execution", stepID)
	// Execution logs folder (agent conversation + tool calls + result summary). Distinct
	// from stepOutputPath above: that folder holds declared context_output artifacts,
	// while this folder holds the full run trace. KB extraction sometimes needs facts
	// that only appear in the conversation — e.g. intermediate findings the step
	// surfaced but didn't write to its final output file.
	executionLogsPath := filepath.Join(docsRoot, getExecutionFolderPathForLogs(runWorkspacePath, stepID, stepPath))

	var agentConfigs *AgentConfigs
	if plan := hcpo.approvedPlan; plan != nil {
		for _, s := range plan.Steps {
			if s != nil && s.GetID() == stepID {
				agentConfigs = getAgentConfigs(s)
				break
			}
		}
	}

	resolvedTitle := ResolveVariables(stepTitle, hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	agentName := fmt.Sprintf("%s-kb-update-%s", stepID, sanitizedTitle)

	agent, err := hcpo.createKBUpdateAgent(ctx, "kb_update", agentName, agentConfigs, stepID, stepPath, stepIndex)
	if err != nil {
		return fmt.Errorf("failed to create KB update agent: %w", err)
	}

	templateVars := map[string]string{
		"StepID":                    stepID,
		"StepTitle":                 stepTitle,
		"StepDescription":           stepDescription,
		"RunFolder":                 runFolder,
		"StepOutputPath":            stepOutputPath,
		"StepContextOutput":         stepContextOutput,
		"StepOutputFilesListing":    BuildStepFilesListing(stepOutputPath),
		"ExecutionLogsPath":         executionLogsPath,
		"ExecutionLogsFilesListing": BuildStepFilesListing(executionLogsPath),
		"ContributionInstruction":   contribution,
		"GraphFilePath":             graphFilePath,
		"IndexFilePath":             indexFilePath,
		"NotesFolderPath":           notesFolderPath,
		"NotesIndexPath":            notesIndexPath,
		"KBShape":                   workflowtypes.ResolveKBShape(hcpo.KBShape()),
	}

	result, _, err := agent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
	if err != nil {
		return fmt.Errorf("KB update agent execution failed: %w", err)
	}
	logKBAgentSummary(hcpo, "📚", result)
	return nil
}

// runKBReorganizePhase is invoked by the reorganize_knowledgebase builder tool via
// kbUpdateQueue. Returns the agent's final summary line.
func (hcpo *StepBasedWorkflowOrchestrator) runKBReorganizePhase(ctx context.Context, instruction string) (string, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🧹 Starting KB reorganize (instruction=%d chars)", len(instruction)))

	nano := time.Now().UnixNano()
	agentSessionID := fmt.Sprintf("kb-reorganize-%d", nano)
	ctx = context.WithValue(ctx, orchestratorevents.AgentSessionIDKey, agentSessionID)
	ctx = context.WithValue(ctx, orchestratorevents.ForceCorrelationIDKey, agentSessionID)
	ctx = context.WithValue(ctx, orchestratorevents.IsSubAgentContextKey, true)

	docsRoot := GetPromptDocsRoot()
	baseWorkspacePath := hcpo.GetWorkspacePath()
	graphFilePath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBGraphFileName)
	indexFilePath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBIndexFileName)
	notesFolderPath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBNotesFolderName)
	notesIndexPath := filepath.Join(notesFolderPath, KBNotesIndexFileName)

	agentName := fmt.Sprintf("kb-reorganize-%d", nano)
	agent, err := hcpo.createKBReorganizeAgent(ctx, "kb_reorganize", agentName, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create KB reorganize agent: %w", err)
	}

	templateVars := map[string]string{
		"Instruction":     instruction,
		"GraphFilePath":   graphFilePath,
		"IndexFilePath":   indexFilePath,
		"NotesFolderPath": notesFolderPath,
		"NotesIndexPath":  notesIndexPath,
		"KBShape":         workflowtypes.ResolveKBShape(hcpo.KBShape()),
	}

	result, _, err := agent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("KB reorganize agent execution failed: %w", err)
	}
	summary := lastNonEmptyLine(result)
	if summary != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🧹 %s", summary))
	}
	return summary, nil
}

// RunKBReorganize enqueues a reorganize job through kbUpdateQueue and blocks until the
// worker finishes. Serializing against kbUpdateQueue prevents races with live post-step
// updates. Returns early if ctx is canceled (e.g. workshop session closed).
//
// Wraps the job in a tracked WorkshopStepExecution so the workshop UI surfaces it
// alongside learning, harden, replan, etc. — same pattern as maybeEnqueueKBUpdate.
func (hcpo *StepBasedWorkflowOrchestrator) RunKBReorganize(ctx context.Context, instruction string) (string, error) {
	type reorgResult struct {
		summary string
		err     error
	}

	execLabel := "KB Reorganize"
	execID := fmt.Sprintf("kb-reorganize-%05d", time.Now().UnixNano()%100000)
	jobCtx, cancel := context.WithCancel(context.Background())
	if hcpo.workshopStepRegistry != nil {
		exec := &WorkshopStepExecution{
			ID:             execID,
			StepID:         execLabel,
			AgentSessionID: "",
			Status:         WorkshopStepRunning,
			cancel:         cancel,
		}
		hcpo.workshopStepRegistry.Register(exec)
	}
	if hcpo.workshopExecutionNotifier != nil {
		hcpo.workshopExecutionNotifier.OnExecutionStart(WorkshopExecutionStart{
			ID:                execID,
			ParentExecutionID: currentWorkshopParentExecutionID(ctx),
			Name:              execLabel,
			Cancel:            cancel,
		})
	}

	done := make(chan reorgResult, 1)
	enqueueKBUpdateJob(func() {
		summary, err := hcpo.runKBReorganizePhase(jobCtx, instruction)
		done <- reorgResult{summary: summary, err: err}
	})

	finalize := func(summary string, err error) {
		cancel()
		if hcpo.workshopExecutionNotifier != nil {
			resultMsg := summary
			if err != nil {
				resultMsg = fmt.Sprintf("KB reorganize failed: %v", err)
			} else if resultMsg == "" {
				resultMsg = "KB reorganize completed"
			}
			hcpo.workshopExecutionNotifier.OnExecutionComplete(execID, execLabel, resultMsg, nil, err)
		}
	}

	select {
	case r := <-done:
		finalize(r.summary, r.err)
		return r.summary, r.err
	case <-ctx.Done():
		finalize("", ctx.Err())
		return "", ctx.Err()
	}
}

func logKBAgentSummary(hcpo *StepBasedWorkflowOrchestrator, emoji, result string) {
	if summary := lastNonEmptyLine(result); summary != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("%s %s", emoji, summary))
	}
}

func lastNonEmptyLine(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// runKBConsolidatePhase is invoked by the consolidate_knowledgebase builder tool via
// kbUpdateQueue. Gathers every step's knowledgebase_contribution + the list of step
// output folders from the selected run, renders them into the consolidate agent's user
// message, and runs the agent. Returns the agent's final summary line.
func (hcpo *StepBasedWorkflowOrchestrator) runKBConsolidatePhase(ctx context.Context, objective string) (string, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return "", fmt.Errorf("objective is required")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🧩 Starting KB consolidate (objective=%d chars)", len(objective)))

	nano := time.Now().UnixNano()
	agentSessionID := fmt.Sprintf("kb-consolidate-%d", nano)
	ctx = context.WithValue(ctx, orchestratorevents.AgentSessionIDKey, agentSessionID)
	ctx = context.WithValue(ctx, orchestratorevents.ForceCorrelationIDKey, agentSessionID)
	ctx = context.WithValue(ctx, orchestratorevents.IsSubAgentContextKey, true)

	docsRoot := GetPromptDocsRoot()
	baseWorkspacePath := hcpo.GetWorkspacePath()
	graphFilePath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBGraphFileName)
	indexFilePath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBIndexFileName)
	notesFolderPath := filepath.Join(docsRoot, baseWorkspacePath, KnowledgebaseFolderName, KBNotesFolderName)
	notesIndexPath := filepath.Join(notesFolderPath, KBNotesIndexFileName)

	contributionsBlock := hcpo.buildKBContributionsBlock()
	stepOutputFoldersBlock := hcpo.buildStepOutputFoldersBlock(docsRoot, baseWorkspacePath)

	agentName := fmt.Sprintf("kb-consolidate-%d", nano)
	agent, err := hcpo.createKBConsolidateAgent(ctx, "kb_consolidate", agentName, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create KB consolidate agent: %w", err)
	}

	templateVars := map[string]string{
		"Objective":              objective,
		"ContributionsBlock":     contributionsBlock,
		"StepOutputFoldersBlock": stepOutputFoldersBlock,
		"GraphFilePath":          graphFilePath,
		"IndexFilePath":          indexFilePath,
		"NotesFolderPath":        notesFolderPath,
		"NotesIndexPath":         notesIndexPath,
		"KBShape":                workflowtypes.ResolveKBShape(hcpo.KBShape()),
	}

	result, _, err := agent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("KB consolidate agent execution failed: %w", err)
	}
	summary := lastNonEmptyLine(result)
	if summary != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🧩 %s", summary))
	}
	return summary, nil
}

// RunKBConsolidate enqueues a consolidate job through kbUpdateQueue and blocks until the
// worker finishes. Serialized against per-step KB updates AND reorganize so graph/notes
// writes can't interleave. Returns early if ctx is canceled (e.g. workshop session closed).
//
// Wraps the job in a tracked WorkshopStepExecution so the workshop UI surfaces it
// alongside learning, harden, reorganize, etc.
func (hcpo *StepBasedWorkflowOrchestrator) RunKBConsolidate(ctx context.Context, objective string) (string, error) {
	type consolidateResult struct {
		summary string
		err     error
	}

	execLabel := "KB Consolidate"
	execID := fmt.Sprintf("kb-consolidate-%05d", time.Now().UnixNano()%100000)
	jobCtx, cancel := context.WithCancel(context.Background())
	if hcpo.workshopStepRegistry != nil {
		exec := &WorkshopStepExecution{
			ID:             execID,
			StepID:         execLabel,
			AgentSessionID: "",
			Status:         WorkshopStepRunning,
			cancel:         cancel,
		}
		hcpo.workshopStepRegistry.Register(exec)
	}
	if hcpo.workshopExecutionNotifier != nil {
		hcpo.workshopExecutionNotifier.OnExecutionStart(WorkshopExecutionStart{
			ID:                execID,
			ParentExecutionID: currentWorkshopParentExecutionID(ctx),
			Name:              execLabel,
			Cancel:            cancel,
		})
	}

	done := make(chan consolidateResult, 1)
	enqueueKBUpdateJob(func() {
		summary, err := hcpo.runKBConsolidatePhase(jobCtx, objective)
		done <- consolidateResult{summary: summary, err: err}
	})

	finalize := func(summary string, err error) {
		cancel()
		if hcpo.workshopExecutionNotifier != nil {
			resultMsg := summary
			if err != nil {
				resultMsg = fmt.Sprintf("KB consolidate failed: %v", err)
			} else if resultMsg == "" {
				resultMsg = "KB consolidate completed"
			}
			hcpo.workshopExecutionNotifier.OnExecutionComplete(execID, execLabel, resultMsg, nil, err)
		}
	}

	select {
	case r := <-done:
		finalize(r.summary, r.err)
		return r.summary, r.err
	case <-ctx.Done():
		finalize("", ctx.Err())
		return "", ctx.Err()
	}
}

// buildKBContributionsBlock produces a deterministic markdown block listing every step's
// declared knowledgebase_contribution. This is what the consolidate agent uses as the
// "declared schema" across the workflow — any type-name / property-name drift between
// these strings is what it should reconcile.
func (hcpo *StepBasedWorkflowOrchestrator) buildKBContributionsBlock() string {
	plan := hcpo.approvedPlan
	if plan == nil || len(plan.Steps) == 0 {
		return "_No plan loaded — cannot enumerate step contributions._"
	}
	var sb strings.Builder
	count := 0
	for _, s := range plan.Steps {
		if s == nil {
			continue
		}
		cfg := getAgentConfigs(s)
		if cfg == nil {
			continue
		}
		contrib := strings.TrimSpace(cfg.KnowledgebaseContribution)
		if contrib == "" {
			continue
		}
		count++
		access := strings.TrimSpace(cfg.KnowledgebaseAccess)
		if access == "" {
			access = "none"
		}
		sb.WriteString(fmt.Sprintf("### step: %s — %s\n", s.GetID(), s.GetTitle()))
		sb.WriteString(fmt.Sprintf("- access: `%s`\n", access))
		sb.WriteString("- contribution:\n")
		for _, line := range strings.Split(contrib, "\n") {
			sb.WriteString("  > ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if count == 0 {
		return "_No steps in this workflow have a non-empty `knowledgebase_contribution`. Consolidation has nothing to reconcile from declared schema — fall back to reading graph.json + notes directly._"
	}
	header := fmt.Sprintf("_%d step(s) declare a knowledgebase_contribution. Review for type-name / property-name drift._\n\n", count)
	return header + sb.String()
}

// buildStepOutputFoldersBlock lists the step output folders under the selected run.
// These are ABSOLUTE paths the agent can `cat` into, but it MUST pick targeted files —
// don't glob the lot. We list folders not files so the agent doesn't pre-commit to
// reading everything; it discovers files on demand.
func (hcpo *StepBasedWorkflowOrchestrator) buildStepOutputFoldersBlock(docsRoot, baseWorkspacePath string) string {
	runFolder := hcpo.selectedRunFolder
	if runFolder == "" {
		return "_No run folder selected — consolidate without step output access. Read graph.json + notes directly and rely on the contributions block for cross-step patterns._"
	}
	executionPath := filepath.Join(docsRoot, baseWorkspacePath, "runs", runFolder, "execution")
	entries, err := os.ReadDir(executionPath)
	if err != nil {
		return fmt.Sprintf("_Selected run `%s` has no execution folder at `%s` (%v). Read graph.json + notes directly._", runFolder, executionPath, err)
	}
	var folders []string
	for _, e := range entries {
		if e.IsDir() {
			folders = append(folders, e.Name())
		}
	}
	sort.Strings(folders)
	if len(folders) == 0 {
		return fmt.Sprintf("_Run `%s` execution folder is empty (`%s`)._", runFolder, executionPath)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Run folder: `%s`\nExecution root: `%s`\n\nStep output folders (cat specific files inside as needed; DO NOT glob-read):\n", runFolder, executionPath))
	for _, name := range folders {
		sb.WriteString(fmt.Sprintf("- `%s/%s/`\n", executionPath, name))
	}
	return sb.String()
}
