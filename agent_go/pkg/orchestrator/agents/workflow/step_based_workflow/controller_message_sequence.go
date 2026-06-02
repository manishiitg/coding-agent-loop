package step_based_workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

type messageSequenceFolderGuardOverrideKey struct{}
type messageSequenceRuntimeSessionOverrideKey struct{}

type messageSequenceFolderGuardOverride struct {
	ReadPaths  []string
	WritePaths []string
}

type messageSequenceRuntimeSessionOverride struct {
	SessionID string
	KeepAlive bool
}

type messageSequenceSession struct {
	SessionID           string                    `json:"session_id"`
	StepID              string                    `json:"step_id"`
	RunFolder           string                    `json:"run_folder"`
	Status              string                    `json:"status"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
	ConversationHistory []llmtypes.MessageContent `json:"conversation_history"`
	LastRuntimeContext  string                    `json:"last_runtime_context,omitempty"`
	RuntimeSessionID    string                    `json:"runtime_session_id,omitempty"`
	Entries             []messageSequenceEntry    `json:"entries,omitempty"`

	runtime *messageSequenceRuntime
}

type messageSequenceRuntime struct {
	Agent     agents.OrchestratorAgent
	SessionID string
	Provider  string
}

type messageSequenceEntry struct {
	EntryID   string    `json:"entry_id"`
	ItemID    string    `json:"item_id,omitempty"`
	ItemType  string    `json:"item_type,omitempty"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

type messageSequenceCallOptions struct {
	Source         string
	ReentryMessage string
	Restart        bool
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	progress *StepProgress,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
	opts messageSequenceCallOptions,
) (string, []llmtypes.MessageContent, error) {
	_ = progress
	_ = execCtx
	_ = allSteps

	sequenceStep, ok := step.(*MessageSequencePlanStep)
	if !ok {
		return "", nil, fmt.Errorf("step %q is not a message_sequence step", step.GetID())
	}
	if stepPath == "" {
		stepPath = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// How a message_sequence behaves depends on how it is invoked:
	//   - ROUTE (Source=="orchestrator_reentry"): the todo_task orchestrator re-enters the
	//     same specialist across calls within one run. The conversation is kept in an
	//     in-memory cache on this orchestrator so it remembers prior calls. This memory is
	//     NEVER read back from disk — it lives only for the lifetime of the run.
	//   - STANDALONE (top-level step / workshop): a fixed item queue that runs once. No
	//     memory and no re-entry — re-running simply re-runs the queue.
	// session.json is still written in both cases as a one-way observability log (never read
	// back for resume).
	isRoute := opts.Source == "orchestrator_reentry"
	routeKey := hcpo.msgSeqRouteKey(stepPath, sequenceStep.GetID())
	sessionRelPath := hcpo.messageSequenceSessionPath(stepPath, sequenceStep.GetID())

	if isRoute && opts.Restart {
		hcpo.clearMsgSeqRouteSession(routeKey)
		if err := hcpo.cleanupMessageSequenceRuntime(ctx, stepPath, sequenceStep.GetID()); err != nil {
			return "", nil, err
		}
	}

	var existing *messageSequenceSession
	var hasExisting bool
	if isRoute && !opts.Restart {
		existing, hasExisting = hcpo.loadMsgSeqRouteSession(routeKey)
	}

	var session *messageSequenceSession
	var plannedItems []MessageSequenceItem
	source := opts.Source
	if source == "" {
		source = "configured_queue"
	}
	if hasExisting {
		// Route re-entry: continue the in-memory conversation with one new user message.
		session = existing
		msg := strings.TrimSpace(opts.ReentryMessage)
		if msg == "" {
			return "", session.ConversationHistory, fmt.Errorf("message_sequence route %q already has an active conversation; provide a re-entry message or restart", sequenceStep.GetID())
		}
		plannedItems = []MessageSequenceItem{{
			ID:      fmt.Sprintf("reentry-%d", len(session.Entries)),
			Type:    "user_message",
			Kind:    "execution",
			Message: msg,
		}}
		if source == "configured_queue" {
			source = "builder_resume"
		}
	} else {
		// First route call, or any standalone run: run the configured queue.
		session = &messageSequenceSession{
			SessionID: sequenceStep.GetID(),
			StepID:    sequenceStep.GetID(),
			RunFolder: hcpo.selectedRunFolder,
			Status:    "running",
			CreatedAt: time.Now(),
		}
		plannedItems = sequenceStep.Items
		// description = turn 0 (consistent across all sequence-like steps): the step
		// description is the opening instruction and leads the first conversational
		// turn — it is prepended to items[0] in executeMessageSequenceUserMessage.
		// For a ROUTE the orchestrator supplies that opening instruction dynamically
		// via ReentryMessage (the route's description + the call_sub_agent
		// instructions), so it takes precedence. For a STANDALONE run we fall back to
		// the step's own description so it is no longer silently dropped — matching the
		// todo_task orchestrator, whose description is likewise its first user turn.
		if reentry := strings.TrimSpace(opts.ReentryMessage); reentry != "" {
			session.LastRuntimeContext = "Builder/orchestrator initial instruction:\n" + reentry
		} else if desc := strings.TrimSpace(sequenceStep.GetDescription()); desc != "" {
			session.LastRuntimeContext = "Step description (opening instruction):\n" + desc
		}
		source = "configured_queue"
	}

	if !isRoute {
		defer hcpo.closeMessageSequenceRuntime(session, "standalone message_sequence completed")
	}

	for _, item := range plannedItems {
		started := time.Now()
		notificationID, notificationName, notificationMeta, notifyItem := hcpo.startMessageSequenceItemNotification(ctx, sequenceStep, item, stepIndex, stepPath, source, started)
		summary, err := hcpo.executeMessageSequenceItem(ctx, sequenceStep, item, stepIndex, stepPath, session)
		ended := time.Now()
		entry := messageSequenceEntry{
			EntryID:   fmt.Sprintf("%s-%d", item.ID, started.UnixNano()),
			ItemID:    item.ID,
			ItemType:  item.Type,
			Source:    source,
			Status:    "completed",
			Summary:   summary,
			StartedAt: started,
			EndedAt:   ended,
		}
		terminalErr := err
		if err != nil {
			entry.Status = "failed"
			entry.Summary = err.Error()
			session.Status = "failed"
			session.Entries = append(session.Entries, entry)
			session.UpdatedAt = time.Now()
			if isRoute {
				hcpo.storeMsgSeqRouteSession(routeKey, session)
			}
			_ = hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session)
			hcpo.completeMessageSequenceItemNotification(ctx, notificationID, notificationName, entry.Summary, notificationMeta, notifyItem, terminalErr)
			return "", session.ConversationHistory, terminalErr
		}
		// A user_message/foreach turn that self-reported STATUS: FAILED is a terminal
		// item failure — stop the queue here instead of running the remaining items.
		if itemType := item.Type; itemType == "" || itemType == "user_message" || itemType == "foreach" {
			if reason, failedStatus := messageSequenceItemReportedFailure(summary); failedStatus {
				entry.Status = "failed"
				session.Status = "failed"
				terminalErr = fmt.Errorf("message_sequence step %q item %q reported STATUS: FAILED: %s", sequenceStep.GetID(), item.ID, reason)
				session.Entries = append(session.Entries, entry)
				session.UpdatedAt = time.Now()
				if isRoute {
					hcpo.storeMsgSeqRouteSession(routeKey, session)
				}
				_ = hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session)
				hcpo.completeMessageSequenceItemNotification(ctx, notificationID, notificationName, summary, notificationMeta, notifyItem, terminalErr)
				return "", session.ConversationHistory, terminalErr
			}
		}
		session.Entries = append(session.Entries, entry)
		session.UpdatedAt = time.Now()
		_ = hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session)
		hcpo.completeMessageSequenceItemNotification(ctx, notificationID, notificationName, summary, notificationMeta, notifyItem, nil)
	}

	session.Status = "completed"
	session.UpdatedAt = time.Now()
	if isRoute {
		hcpo.storeMsgSeqRouteSession(routeKey, session)
	}
	_ = hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session)
	return hcpo.summarizeMessageSequenceSession(session), session.ConversationHistory, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) startMessageSequenceItemNotification(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, source string, started time.Time) (string, string, map[string]string, bool) {
	if hcpo == nil || step == nil {
		return "", "", nil, false
	}
	// Fail loud, not silent: a nil notifier means this execution path forgot to
	// wire SetWorkshopExecutionNotifier, so the main agent gets NO notification
	// for this message_sequence item. That used to be an invisible no-op (a whole
	// step ran with the main agent never told). Log it so the wiring gap is
	// obvious instead of silently swallowed.
	if hcpo.workshopExecutionNotifier == nil {
		if hcpo.GetLogger() != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ message_sequence item %q/%q: workshopExecutionNotifier is nil — auto-notification skipped (notifier not wired for this execution path)", step.GetID(), item.ID))
		}
		return "", "", nil, false
	}
	execID := messageSequenceItemExecutionID(step.GetID(), item.ID, started)
	name := messageSequenceItemExecutionName(step, item)
	meta := hcpo.messageSequenceItemNotificationMeta(step, item, stepIndex, stepPath, source)
	hcpo.workshopExecutionNotifier.OnExecutionStart(WorkshopExecutionStart{
		ID:                execID,
		ParentExecutionID: currentWorkshopParentExecutionID(ctx),
		Name:              name,
		Kind:              "message_sequence_item",
		Metadata:          meta,
	})
	return execID, name, meta, true
}

func (hcpo *StepBasedWorkflowOrchestrator) completeMessageSequenceItemNotification(_ context.Context, execID string, name string, summary string, meta map[string]string, active bool, err error) {
	if hcpo == nil || hcpo.workshopExecutionNotifier == nil || !active {
		return
	}
	result := strings.TrimSpace(summary)
	if result == "" && err != nil {
		result = err.Error()
	}
	if result == "" {
		result = "message sequence item completed"
	}
	hcpo.workshopExecutionNotifier.OnExecutionComplete(execID, name, result, meta, err)
}

func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceItemNotificationMeta(step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, source string) map[string]string {
	itemType := strings.TrimSpace(item.Type)
	if itemType == "" {
		itemType = "user_message"
	}
	meta := map[string]string{
		"execution_type": "message-sequence-item",
		"step_id":        step.GetID(),
		"step_index":     fmt.Sprintf("%d", stepIndex),
		"step_path":      stepPath,
		"item_id":        item.ID,
		"item_type":      itemType,
		"source":         source,
	}
	if title := strings.TrimSpace(step.GetTitle()); title != "" {
		meta["step_title"] = title
	}
	if kind := strings.TrimSpace(item.Kind); kind != "" {
		meta["item_kind"] = kind
	}
	if runFolder := strings.TrimSpace(hcpo.selectedRunFolder); runFolder != "" {
		meta["run_folder"] = runFolder
		meta["iteration"] = runFolder
	}
	if groupName := strings.TrimSpace(hcpo.currentGroupName); groupName != "" {
		meta["group_name"] = groupName
	}
	return meta
}

func messageSequenceItemExecutionName(step *MessageSequencePlanStep, item MessageSequenceItem) string {
	stepLabel := strings.TrimSpace(step.GetTitle())
	if stepLabel == "" {
		stepLabel = step.GetID()
	}
	itemType := strings.TrimSpace(item.Type)
	if itemType == "" {
		itemType = "user_message"
	}
	itemID := strings.TrimSpace(item.ID)
	if itemID == "" {
		itemID = "item"
	}
	return fmt.Sprintf("Message sequence item -> %s / %s (%s)", stepLabel, itemID, itemType)
}

func messageSequenceItemExecutionID(stepID string, itemID string, started time.Time) string {
	return fmt.Sprintf("msgseq-%s-%s-%d", sanitizeMessageSequenceExecutionIDPart(stepID), sanitizeMessageSequenceExecutionIDPart(itemID), started.UnixNano())
}

func sanitizeMessageSequenceExecutionIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "item"
	}
	s = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if s == "" {
		return "item"
	}
	if len(s) > 48 {
		return s[:48]
	}
	return s
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceItem(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, session *messageSequenceSession) (string, error) {
	switch item.Type {
	case "user_message", "":
		return hcpo.executeMessageSequenceUserMessage(ctx, step, item, stepIndex, stepPath, session)
	case "foreach":
		return hcpo.executeMessageSequenceForeachItem(ctx, step, item, stepIndex, stepPath, session)
	case "code":
		return hcpo.executeMessageSequenceCodeItem(ctx, step, item, stepPath, session)
	case "prevalidation":
		schema := item.ValidationSchema
		if schema == nil {
			schema = item.Prevalidation
		}
		if schema == nil {
			schema = step.ValidationSchema
		}
		if schema == nil {
			return "prevalidation skipped: no schema", nil
		}
		// Prevalidation is a self-validation gate with a repair loop, mirroring the
		// retry-with-feedback behavior of regular steps: on failure we send the
		// concrete validation errors back to the SAME conversation as a fix-it turn
		// (the session continues, so the agent keeps full context and can re-create
		// or correct the required output files), then re-run the same checks. Only
		// after maxMessageSequencePrevalidationRepairs failed repair turns does the
		// gate fail the sequence. A prevalidation that cannot even RUN (err != nil)
		// is a terminal infrastructure failure, not retried.
		const maxMessageSequencePrevalidationRepairs = 3
		for attempt := 0; ; attempt++ {
			results, err := RunPreValidation(ctx, schema, hcpo.messageSequenceExecutionRelPath(stepPath, step.GetID()), hcpo.BaseOrchestrator)
			if err != nil {
				results = &WorkspaceVerificationResult{
					OverallPass:  false,
					FilesChecked: []FileCheckResult{},
					Summary: ValidationSummary{
						TotalChecks:  0,
						PassedChecks: 0,
						FailedChecks: 1,
						Errors: []ValidationError{{
							CheckType: "pre_validation_error",
							Expected:  "pre-validation to run successfully",
							Actual:    "error occurred",
							Message:   fmt.Sprintf("Pre-validation failed to run for message sequence item %q: %v", item.ID, err),
						}},
						SchemaWarnings: []ValidationError{},
					},
				}
				hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, false, results)
				return "", err
			}
			if results == nil {
				results = &WorkspaceVerificationResult{
					OverallPass:  false,
					FilesChecked: []FileCheckResult{},
					Summary: ValidationSummary{
						TotalChecks:  0,
						PassedChecks: 0,
						FailedChecks: 1,
						Errors: []ValidationError{{
							CheckType: "pre_validation_error",
							Expected:  "pre-validation to return a result",
							Actual:    "no result returned",
							Message:   fmt.Sprintf("Pre-validation returned no result for message sequence item %q", item.ID),
						}},
						SchemaWarnings: []ValidationError{},
					},
				}
			}
			hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, results.OverallPass, results)
			if results.OverallPass {
				if attempt == 0 {
					return "prevalidation passed", nil
				}
				return fmt.Sprintf("prevalidation passed after %d repair turn(s)", attempt), nil
			}
			if attempt >= maxMessageSequencePrevalidationRepairs {
				return "", fmt.Errorf("message sequence prevalidation failed for item %q after %d repair attempt(s): %s",
					item.ID, maxMessageSequencePrevalidationRepairs, summarizeMessageSequencePrevalidationErrors(results))
			}
			// Send the failure back as a fix-it turn on the same conversation, then loop to re-validate.
			feedback := formatMessageSequencePrevalidationFeedback(item.ID, results)
			repairItem := MessageSequenceItem{
				ID:      fmt.Sprintf("%s-repair-%d", item.ID, attempt+1),
				Type:    "user_message",
				Kind:    "execution",
				Message: feedback,
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🔁 message_sequence prevalidation %q failed — sending repair turn %d/%d", item.ID, attempt+1, maxMessageSequencePrevalidationRepairs))
			if _, rerr := hcpo.executeMessageSequenceUserMessage(ctx, step, repairItem, stepIndex, stepPath, session); rerr != nil {
				return "", fmt.Errorf("message sequence prevalidation %q repair turn %d failed: %w", item.ID, attempt+1, rerr)
			}
		}
	default:
		return "", fmt.Errorf("unsupported message_sequence item type %q", item.Type)
	}
}

// summarizeMessageSequencePrevalidationErrors renders a one-line, comma-joined
// summary of a failed prevalidation result for inclusion in the terminal error.
func summarizeMessageSequencePrevalidationErrors(results *WorkspaceVerificationResult) string {
	if results == nil {
		return "no validation result"
	}
	parts := make([]string, 0, len(results.Summary.Errors))
	for _, e := range results.Summary.Errors {
		msg := strings.TrimSpace(e.Message)
		if msg == "" {
			msg = strings.TrimSpace(fmt.Sprintf("%s check failed (expected %s, got %s)", e.CheckType, e.Expected, e.Actual))
		}
		loc := strings.TrimSpace(strings.TrimSpace(e.File) + " " + strings.TrimSpace(e.Path))
		if loc != "" {
			parts = append(parts, loc+": "+msg)
		} else {
			parts = append(parts, msg)
		}
	}
	if len(parts) == 0 {
		return "validation did not pass (no specific errors reported)"
	}
	return strings.Join(parts, "; ")
}

// formatMessageSequencePrevalidationFeedback builds the fix-it instruction sent
// back to the agent when a prevalidation gate fails. It mirrors the regular-step
// "## Pre-Validation Failed" feedback: name the failing checks concretely and
// instruct the agent to actually correct/recreate the output files (not merely
// re-report success), since the same checks run again afterward.
func formatMessageSequencePrevalidationFeedback(itemID string, results *WorkspaceVerificationResult) string {
	var b strings.Builder
	b.WriteString("## Pre-Validation Failed (Previous Attempt)\n\n")
	b.WriteString(fmt.Sprintf("The output validation gate %q did not pass. Fix the issues below by inspecting and correcting the required output files (create missing files, set every required field to the correct value/type), then finish. The exact same validation will run again immediately after this turn.\n\n", itemID))
	b.WriteString("Failing checks:\n")
	wrote := false
	if results != nil {
		for _, fc := range results.FilesChecked {
			if !fc.Exists {
				b.WriteString(fmt.Sprintf("- Missing file: %s — create it.\n", fc.FileName))
				wrote = true
				continue
			}
			for _, jc := range fc.JSONChecks {
				if jc.Passed {
					continue
				}
				detail := strings.TrimSpace(jc.ErrorMsg)
				if detail == "" {
					detail = fmt.Sprintf("%s check failed (expected %v, got %v)", jc.CheckType, jc.Expected, jc.Actual)
				}
				b.WriteString(fmt.Sprintf("- %s @ %s: %s\n", fc.FileName, jc.Path, detail))
				wrote = true
			}
		}
		if !wrote {
			for _, e := range results.Summary.Errors {
				msg := strings.TrimSpace(e.Message)
				if msg == "" {
					msg = fmt.Sprintf("%s check failed (expected %s, got %s)", e.CheckType, e.Expected, e.Actual)
				}
				loc := strings.TrimSpace(strings.TrimSpace(e.File) + " " + strings.TrimSpace(e.Path))
				if loc != "" {
					b.WriteString(fmt.Sprintf("- %s: %s\n", loc, msg))
				} else {
					b.WriteString(fmt.Sprintf("- %s\n", msg))
				}
				wrote = true
			}
		}
	}
	if !wrote {
		b.WriteString("- Validation did not pass; no specific error detail was reported. Re-check that every required output file exists with the correct structure.\n")
	}
	b.WriteString("\nDo NOT just reply that it is done — actually read the files, correct the data, and write them so every required field is present with the correct type and value.")
	return b.String()
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceUserMessage(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, session *messageSequenceSession) (string, error) {
	writeAccess := resolveMessageSequenceItemWriteAccess(item)
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard(stepPath, step.GetID(), writeAccess)
	runtime, agentCtx, err := hcpo.getMessageSequenceRuntime(ctx, step, stepPath, session, readPaths, writePaths)
	if err != nil {
		return "", err
	}

	message := strings.TrimSpace(item.Message)
	if session.LastRuntimeContext != "" {
		message = session.LastRuntimeContext + "\n\n## Next instruction\n" + message
	}
	templateVars := hcpo.buildMessageSequenceTemplateVars(step, item, stepIndex, stepPath, message, readPaths, writePaths)
	result, history, err := runtime.Agent.Execute(agentCtx, templateVars, session.ConversationHistory)
	if err != nil {
		return "", err
	}
	session.ConversationHistory = history
	session.LastRuntimeContext = ""
	return strings.TrimSpace(result), nil
}

// executeMessageSequenceForeachItem expands a foreach item into one user_message turn per row
// of its db source and runs each through the same conversation (auto-summarization keeps the
// growing context bounded). Each row's templated text is sent as an ordinary user_message,
// inheriting the foreach item's kind / write_access.
func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceForeachItem(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, session *messageSequenceSession) (string, error) {
	messages, err := hcpo.expandForeach(ctx, item.SourceSQL, item.Message, item.MaxIterations)
	if err != nil {
		return "", fmt.Errorf("foreach item %q: %w", item.ID, err)
	}
	if len(messages) == 0 {
		return fmt.Sprintf("foreach %s: 0 rows, nothing to process", item.ID), nil
	}
	for idx, msg := range messages {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("foreach item %q canceled: %w", item.ID, ctx.Err())
		default:
		}
		synth := MessageSequenceItem{
			ID:          fmt.Sprintf("%s-%d", item.ID, idx),
			Type:        "user_message",
			Kind:        item.Kind,
			Message:     msg,
			WriteAccess: item.WriteAccess,
		}
		if _, err := hcpo.executeMessageSequenceUserMessage(ctx, step, synth, stepIndex, stepPath, session); err != nil {
			return "", fmt.Errorf("foreach %s row %d: %w", item.ID, idx, err)
		}
	}
	return fmt.Sprintf("foreach %s: processed %d row(s)", item.ID, len(messages)), nil
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceCodeItem(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepPath string, session *messageSequenceSession) (string, error) {
	if strings.TrimSpace(item.ScriptPath) == "" {
		return "", fmt.Errorf("code item %q missing script_path", item.ID)
	}
	source, err := hcpo.ReadWorkspaceFile(ctx, item.ScriptPath)
	if err != nil {
		return "", fmt.Errorf("read code item script %q: %w", item.ScriptPath, err)
	}
	itemRel := hcpo.messageSequenceItemRelPath(stepPath, step.GetID(), item.ID)
	codeRel := filepath.Join(itemRel, "code")
	mainRel := filepath.Join(codeRel, "main.py")
	if err := hcpo.WriteWorkspaceFile(ctx, mainRel, source); err != nil {
		return "", fmt.Errorf("write code item working copy: %w", err)
	}

	inputContract := map[string]interface{}{
		"input_json":   item.InputJSON,
		"input_files":  item.InputFiles,
		"output_files": item.OutputFiles,
		"read_access": map[string]bool{
			"knowledgebase": true,
			"db":            true,
			"learnings":     true,
		},
		"write_access": resolveMessageSequenceItemWriteAccess(item),
	}
	inputBytes, _ := json.MarshalIndent(inputContract, "", "  ")
	if err := hcpo.WriteWorkspaceFile(ctx, filepath.Join(itemRel, "input.json"), string(inputBytes)); err != nil {
		return "", fmt.Errorf("write code item input contract: %w", err)
	}

	maxRepairAttempts := 0
	if item.OnFailure.Action == "repair_with_llm" || item.OnFailure.Action == "repair_same_session" {
		maxRepairAttempts = item.OnFailure.MaxRetries
		if maxRepairAttempts <= 0 {
			maxRepairAttempts = 1
		}
	}

	var output string
	var exitCode int
	for attempt := 0; attempt <= maxRepairAttempts; attempt++ {
		output, exitCode, err = hcpo.runMessageSequencePython(ctx, stepPath, step.GetID(), item, mainRel, codeRel, itemRel)
		hcpo.writeMessageSequenceCodeResult(ctx, item, itemRel, mainRel, output, exitCode, err)
		if err == nil && exitCode == 0 {
			if item.SaveRepaired && attempt > 0 {
				if repairedSource, readErr := hcpo.ReadWorkspaceFile(ctx, mainRel); readErr == nil {
					_ = hcpo.WriteWorkspaceFile(ctx, item.ScriptPath, repairedSource)
				}
			}
			session.LastRuntimeContext = hcpo.buildCodeItemRuntimeContext(item, mainRel, output, exitCode)
			return fmt.Sprintf("code item %s succeeded", item.ID), nil
		}
		if attempt >= maxRepairAttempts {
			break
		}
		failureContext := hcpo.buildCodeItemFailureContext(item, itemRel, mainRel, output, exitCode, err)
		if repairErr := hcpo.executeMessageSequenceCodeRepair(ctx, step, item, stepPath, session, itemRel, codeRel, failureContext, attempt+1); repairErr != nil {
			return "", repairErr
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("code item %q failed with exit code %d", item.ID, exitCode)
}

func (hcpo *StepBasedWorkflowOrchestrator) writeMessageSequenceCodeResult(ctx context.Context, item MessageSequenceItem, itemRel string, scriptPath string, output string, exitCode int, execErr error) {
	result := map[string]interface{}{
		"item_id":      item.ID,
		"status":       "success",
		"exit_code":    exitCode,
		"script_path":  scriptPath,
		"stdout_path":  filepath.Join(itemRel, "stdout.txt"),
		"stderr_path":  filepath.Join(itemRel, "stderr.txt"),
		"output_files": item.OutputFiles,
	}
	if execErr != nil || exitCode != 0 {
		result["status"] = "failed"
		if execErr != nil {
			result["error"] = execErr.Error()
		}
	}
	if strings.TrimSpace(output) != "" {
		result["log_excerpt"] = truncateMessageSequenceLog(output, 2000)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	_ = hcpo.WriteWorkspaceFile(ctx, filepath.Join(itemRel, "result.json"), string(resultBytes))
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceCodeRepair(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepPath string, session *messageSequenceSession, itemRel string, codeRel string, failureContext string, attempt int) error {
	writeAccess := resolveMessageSequenceItemWriteAccess(item)
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard(stepPath, step.GetID(), writeAccess)
	readPaths = append(readPaths, itemRel, codeRel)
	writePaths = append(writePaths, itemRel, codeRel)
	override := &messageSequenceFolderGuardOverride{
		ReadPaths:  common.DeduplicateStrings(append(readPaths, writePaths...)),
		WritePaths: common.DeduplicateStrings(writePaths),
	}
	runtime, agentCtx, err := hcpo.getMessageSequenceRuntime(ctx, step, stepPath, session, override.ReadPaths, override.WritePaths)
	if err != nil {
		return err
	}
	message := failureContext + "\n\nRepair the working copy at " + hcpo.messageSequenceAbsPath(filepath.Join(codeRel, "main.py")) + ". Keep the fix narrowly scoped. Do not announce success; the runtime will rerun the script after your edit."
	templateVars := hcpo.buildMessageSequenceTemplateVars(step, item, 0, stepPath, message, override.ReadPaths, override.WritePaths)
	_, history, err := runtime.Agent.Execute(agentCtx, templateVars, session.ConversationHistory)
	if err != nil {
		return err
	}
	session.ConversationHistory = history
	return nil
}

func (hcpo *StepBasedWorkflowOrchestrator) getMessageSequenceRuntime(ctx context.Context, step *MessageSequencePlanStep, stepPath string, session *messageSequenceSession, readPaths, writePaths []string) (*messageSequenceRuntime, context.Context, error) {
	if session == nil {
		return nil, ctx, fmt.Errorf("message_sequence session is nil")
	}

	sessionID := hcpo.messageSequenceRuntimeSessionID(stepPath, step.GetID())
	if session.runtime != nil && strings.TrimSpace(session.runtime.SessionID) != "" {
		sessionID = strings.TrimSpace(session.runtime.SessionID)
	}
	session.RuntimeSessionID = sessionID
	hcpo.configureSubAgentSessionGuard(sessionID, "message-sequence", step.GetID(), readPaths, writePaths)

	folderOverride := &messageSequenceFolderGuardOverride{ReadPaths: readPaths, WritePaths: writePaths}
	sessionOverride := &messageSequenceRuntimeSessionOverride{SessionID: sessionID, KeepAlive: true}
	agentCtx := context.WithValue(ctx, messageSequenceFolderGuardOverrideKey{}, folderOverride)
	agentCtx = context.WithValue(agentCtx, messageSequenceRuntimeSessionOverrideKey{}, sessionOverride)

	if session.runtime != nil && session.runtime.Agent != nil {
		return session.runtime, agentCtx, nil
	}

	agentName := fmt.Sprintf("message-sequence-%s", step.GetID())
	agent, err := hcpo.createExecutionOnlyAgent(agentCtx, "execution_only", stepPath, agentName, step.AgentConfigs, step.GetID(), "")
	if err != nil {
		return nil, agentCtx, err
	}
	provider := ""
	if cfg := agent.GetConfig(); cfg != nil {
		provider = cfg.LLMConfig.Primary.Provider
		if strings.TrimSpace(cfg.MCPSessionID) != "" {
			sessionID = strings.TrimSpace(cfg.MCPSessionID)
			session.RuntimeSessionID = sessionID
			hcpo.configureSubAgentSessionGuard(sessionID, "message-sequence", step.GetID(), readPaths, writePaths)
		}
	}
	session.runtime = &messageSequenceRuntime{
		Agent:     agent,
		SessionID: sessionID,
		Provider:  provider,
	}
	return session.runtime, agentCtx, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceRuntimeSessionID(stepPath string, stepID string) string {
	parts := []string{"msgseq"}
	if strings.TrimSpace(hcpo.selectedRunFolder) != "" {
		parts = append(parts, sanitizeMessageSequenceExecutionIDPart(hcpo.selectedRunFolder))
	}
	if strings.TrimSpace(hcpo.currentGroupName) != "" {
		parts = append(parts, sanitizeMessageSequenceExecutionIDPart(hcpo.currentGroupName))
	}
	parts = append(parts, sanitizeMessageSequenceExecutionIDPart(stepPath), sanitizeMessageSequenceExecutionIDPart(stepID))
	return strings.Join(parts, "-")
}

func (hcpo *StepBasedWorkflowOrchestrator) closeMessageSequenceRuntime(session *messageSequenceSession, reason string) {
	if session == nil || session.runtime == nil {
		return
	}
	runtime := session.runtime
	session.runtime = nil
	if runtime.Agent != nil {
		_ = runtime.Agent.Close()
	}
	closeMessageSequenceCodingSession(runtime.Provider, runtime.SessionID, reason)
	common.ClearSessionShellConfig(runtime.SessionID)
}

func closeMessageSequenceCodingSession(provider string, ownerSessionID string, reason string) {
	if strings.TrimSpace(ownerSessionID) == "" {
		return
	}
	switch strings.TrimSpace(provider) {
	case string(llmproviders.ProviderAgyCLI):
		llmproviders.CloseAgyCLIInteractiveSessionForOwner(ownerSessionID, reason)
	case string(llmproviders.ProviderClaudeCode):
		llmproviders.CloseClaudeCodeInteractiveSessionForOwner(ownerSessionID, reason)
	case string(llmproviders.ProviderCodexCLI):
		llmproviders.CloseCodexCLIInteractiveSessionForOwner(ownerSessionID, reason)
	case string(llmproviders.ProviderCursorCLI):
		llmproviders.CloseCursorCLIInteractiveSessionForOwner(ownerSessionID, reason)
	case string(llmproviders.ProviderGeminiCLI):
		llmproviders.CloseGeminiCLIInteractiveSessionForOwner(ownerSessionID, reason)
	}
}

// A write verb followed (within one sentence, a short window) by a db/ or kb/
// path. The verbs are stems so writes/writing/saved/appending/... all match.
// The bounded, non-greedy gap and \b anchor keep this from firing on reads
// ("read db/x.json" has no preceding write verb) or on a write whose target is
// elsewhere ("compare against db/baseline and write the report" — the verb
// comes after the path, and is too far / wrong order).
var messageSequenceWriteVerbStem = `\b(writ|sav|append|updat|persist|stor|record|creat|produc|output|overwrit|populat|emit|dump|flush)[a-z]*`

var messageSequenceDBWriteIntentRe = regexp.MustCompile(messageSequenceWriteVerbStem + `[^\n.;!?]{0,40}?\bdb/`)

var messageSequenceKBWriteIntentRe = regexp.MustCompile(messageSequenceWriteVerbStem + `[^\n.;!?]{0,40}?\b(knowledgebase|kb)/`)
var messageSequenceLearningWriteIntentRe = regexp.MustCompile(messageSequenceWriteVerbStem + `[^\n.;!?]{0,40}?\blearnings/`)

// messageSequenceItemWriteIntent reports whether an item is going to WRITE to
// db/ or the knowledgebase, inferred from its declared output_files (definitive)
// and its message prose (write verb adjacent to a db/ or kb/ path). It is the
// counterpart to resolveMessageSequenceItemWriteAccess: the former says what the
// item is GRANTED, this says what it APPEARS to need, and validation flags the
// gap so an item can't quietly ask to write a file it has no access to.
// messageSequenceItemReportedFailure reports whether an LLM turn ended with the
// agent's terminal STATUS: FAILED marker (the execution_only Completion
// contract). When a turn self-reports failure there's no point running the rest
// of the queue — especially a following prevalidation gate, which would just
// re-confirm the failure — so the sequence short-circuits and fails the step
// with the reported reason.
func messageSequenceItemReportedFailure(summary string) (reason string, failed bool) {
	for _, line := range strings.Split(summary, "\n") {
		trimmed := strings.TrimSpace(line)
		compact := strings.ToUpper(strings.Join(strings.Fields(trimmed), " "))
		if !strings.HasPrefix(compact, "STATUS: FAILED") && !strings.HasPrefix(compact, "STATUS:FAILED") {
			continue
		}
		if idx := strings.Index(strings.ToUpper(trimmed), "FAILED"); idx >= 0 {
			reason = strings.TrimSpace(strings.TrimLeft(trimmed[idx+len("FAILED"):], " —-:"))
		}
		if reason == "" {
			reason = trimmed
		}
		return reason, true
	}
	return "", false
}

func messageSequenceItemWriteIntent(item MessageSequenceItem) (needsDB, needsKB, needsLearnings bool) {
	for _, out := range item.OutputFiles {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(out)))
		if clean == "" || clean == "." {
			continue
		}
		if clean == DBFolderName || strings.HasPrefix(clean, DBFolderName+"/") || strings.Contains(clean, "/"+DBFolderName+"/") {
			needsDB = true
		}
		if strings.HasPrefix(clean, KnowledgebaseFolderName+"/notes/") || strings.Contains(clean, "/"+KnowledgebaseFolderName+"/notes/") {
			needsKB = true
		}
		if clean == LearningsFolderName || strings.HasPrefix(clean, LearningsFolderName+"/") || strings.Contains(clean, "/"+LearningsFolderName+"/") {
			needsLearnings = true
		}
	}
	if msg := strings.ToLower(item.Message); msg != "" {
		if messageSequenceDBWriteIntentRe.MatchString(msg) {
			needsDB = true
		}
		if messageSequenceKBWriteIntentRe.MatchString(msg) {
			needsKB = true
		}
		if messageSequenceLearningWriteIntentRe.MatchString(msg) {
			needsLearnings = true
		}
	}
	return needsDB, needsKB, needsLearnings
}

func resolveMessageSequenceItemWriteAccess(item MessageSequenceItem) MessageSequenceWriteAccess {
	if item.WriteAccess != (MessageSequenceWriteAccess{}) {
		return item.WriteAccess
	}
	var access MessageSequenceWriteAccess
	switch item.Kind {
	case "learning":
		access.Learnings = true
	case "knowledgebase":
		access.Knowledgebase = true
	case "db":
		access.DB = true
	case "code":
		for _, output := range item.OutputFiles {
			clean := filepath.ToSlash(filepath.Clean(output))
			if clean == DBFolderName || strings.HasPrefix(clean, DBFolderName+"/") || strings.Contains(clean, "/"+DBFolderName+"/") {
				access.DB = true
			}
			if strings.HasPrefix(clean, KnowledgebaseFolderName+"/notes/") || strings.Contains(clean, "/"+KnowledgebaseFolderName+"/notes/") {
				access.Knowledgebase = true
			}
		}
	}
	return access
}

func (hcpo *StepBasedWorkflowOrchestrator) setupMessageSequenceFolderGuard(stepPath string, stepID string, itemWriteAccess MessageSequenceWriteAccess) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	runWorkspacePath := baseWorkspacePath
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	// Build from executionWorkspacePath (which includes baseWorkspacePath, the
	// workflow root) so the guard's writable step folder matches downloadsPath /
	// getDBPath and the agent-facing StepExecutionPath. messageSequenceExecutionRelPath
	// is workflow-root-RELATIVE (for the workspace-file API) and must NOT be used
	// directly as a guard path or it omits the workflow root.
	stepFolderPath := filepath.Join(executionWorkspacePath, getArtifactFolderName(stepID, stepPath))
	downloadsPath := fmt.Sprintf("%s/Downloads", executionWorkspacePath)

	readPaths = []string{
		executionWorkspacePath,
		fmt.Sprintf("%s/soul", baseWorkspacePath),
		fmt.Sprintf("%s/builder", baseWorkspacePath),
		getDBPath(baseWorkspacePath),
		getKnowledgebasePath(baseWorkspacePath),
		filepath.Join(baseWorkspacePath, LearningsFolderName, GlobalLearningID),
	}
	writePaths = []string{stepFolderPath, downloadsPath}
	if itemWriteAccess.DB {
		writePaths = append(writePaths, getDBPath(baseWorkspacePath))
	}
	if itemWriteAccess.Knowledgebase {
		writePaths = append(writePaths, filepath.Join(getKnowledgebasePath(baseWorkspacePath), "notes"))
	}
	if itemWriteAccess.Learnings {
		writePaths = append(writePaths, filepath.Join(baseWorkspacePath, LearningsFolderName, GlobalLearningID))
	}
	return common.DeduplicateStrings(readPaths), common.DeduplicateStrings(writePaths)
}

func (hcpo *StepBasedWorkflowOrchestrator) buildMessageSequenceTemplateVars(step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, message string, readPaths []string, writePaths []string) map[string]string {
	stepExecRel := hcpo.messageSequenceExecutionRelPath(stepPath, step.GetID())
	docsRoot := GetPromptDocsRoot()
	// Honor the step's declared context_output so the sequence writes the file
	// downstream steps expect (in execution/<stepID>/, the normal step folder).
	// Fall back to the generic name only when the step declares no output.
	contextOutput := strings.TrimSpace(step.GetContextOutput().String())
	if contextOutput == "" {
		contextOutput = "message_sequence_result.json"
	}
	return map[string]string{
		"StepTitle":                step.GetTitle(),
		"StepDescription":          message,
		"BaseDescription":          message,
		"OrchestratorInstructions": message,
		"StepContextDependencies":  strings.Join(step.GetContextDependencies(), "\n"),
		"StepContextOutput":        contextOutput,
		"WorkspacePath":            hcpo.messageSequenceAbsPath(filepath.Join("runs", hcpo.selectedRunFolder, "execution")),
		"WorkflowRoot":             hcpo.messageSequenceAbsPath(""),
		"DocsRoot":                 docsRoot,
		"StepExecutionPath":        hcpo.messageSequenceAbsPath(stepExecRel),
		"DBPath":                   hcpo.messageSequenceAbsPath(DBFolderName),
		"KnowledgebasePath":        hcpo.messageSequenceAbsPath(KnowledgebaseFolderName),
		"FolderGuardReadPaths":     strings.Join(toAbsPaths(docsRoot, readPaths), ", "),
		"FolderGuardWritePaths":    strings.Join(toAbsPaths(docsRoot, writePaths), ", "),
		"StepNumber":               fmt.Sprintf("%d", stepIndex+1),
		"IsCodeExecutionMode":      "false",
		"UseCodeStyleRules":        "",
		"KbAccess":                 KBAccessRead,
		"KbAccessLabel":            kbAccessLabel(KBAccessRead),
		"KbWriteMethod":            KBWriteMethodDirect,
		"HasLearnings":             "false",
		"CurrentDate":              time.Now().Format("2006-01-02"),
		"CurrentTime":              time.Now().Format("15:04:05"),
	}
}

func (hcpo *StepBasedWorkflowOrchestrator) runMessageSequencePython(ctx context.Context, stepPath string, stepID string, item MessageSequenceItem, mainRel string, codeRel string, itemRel string) (string, int, error) {
	writeAccess := resolveMessageSequenceItemWriteAccess(item)
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard(stepPath, stepID, writeAccess)
	itemAbs := hcpo.messageSequenceAbsPath(itemRel)
	codeAbs := hcpo.messageSequenceAbsPath(codeRel)
	mainAbs := hcpo.messageSequenceAbsPath(mainRel)
	// Guard paths are docs-root-relative (workflow-root-inclusive), matching the
	// other entries from setupMessageSequenceFolderGuard — so prefix the
	// workflow-root-relative item/code rels with the workflow root.
	itemGuard := filepath.Join(hcpo.GetWorkspacePath(), itemRel)
	codeGuard := filepath.Join(hcpo.GetWorkspacePath(), codeRel)
	readPaths = append(readPaths, itemGuard, codeGuard)
	writePaths = append(writePaths, itemGuard, codeGuard)

	var cmd strings.Builder
	cmd.WriteString("python3 -B ")
	cmd.WriteString(shellQuotePath(mainAbs))
	for _, input := range item.InputFiles {
		cmd.WriteString(" ")
		cmd.WriteString(shellQuotePath(hcpo.messageSequenceAbsPath(input)))
	}
	timeout := 0
	useShell := true
	extraEnv := map[string]string{
		"STEP_OUTPUT_DIR":                    itemAbs,
		"STEP_EXECUTION_DIR":                 itemAbs,
		"MESSAGE_SEQUENCE_STEP_ID":           stepID,
		"MESSAGE_SEQUENCE_ITEM_ID":           item.ID,
		"MESSAGE_SEQUENCE_ITEM_DIR":          itemAbs,
		"MESSAGE_SEQUENCE_INPUT_JSON":        filepath.Join(itemAbs, "input.json"),
		"MESSAGE_SEQUENCE_OUTPUT_FILES_JSON": strings.Join(item.OutputFiles, ","),
		"PYTHONDONTWRITEBYTECODE":            "1",
		"SCRIPT_VERBOSE":                     "1",
	}
	if envRef := hcpo.GetWorkspaceEnvRef(); envRef != nil {
		hcpo.LockWorkspaceEnv()
		for k, v := range envRef {
			if _, reserved := extraEnv[k]; reserved {
				continue
			}
			extraEnv[k] = v
		}
		hcpo.UnlockWorkspaceEnv()
	}
	reqParams := workspace.ExecuteShellCommandParams{
		Command:          cmd.String(),
		WorkingDirectory: strings.TrimPrefix(codeAbs, GetPromptDocsRoot()+"/"),
		Timeout:          &timeout,
		UseShell:         &useShell,
		FolderGuard: &workspace.FolderGuardConfig{
			Enabled:    true,
			ReadPaths:  common.DeduplicateStrings(append(readPaths, writePaths...)),
			WritePaths: common.DeduplicateStrings(writePaths),
		},
		ExtraEnv: extraEnv,
	}

	jsonBody, err := json.Marshal(reqParams)
	if err != nil {
		return "", -1, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", getWorkspaceAPIURL()+"/api/execute", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", -1, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", -1, err
	}
	var apiResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", -1, fmt.Errorf("parse shell response: %w", err)
	}
	_ = hcpo.WriteWorkspaceFile(ctx, filepath.Join(itemRel, "stdout.txt"), apiResp.Data.Stdout)
	_ = hcpo.WriteWorkspaceFile(ctx, filepath.Join(itemRel, "stderr.txt"), apiResp.Data.Stderr)
	combined := strings.TrimSpace(apiResp.Data.Stdout)
	if strings.TrimSpace(apiResp.Data.Stderr) != "" {
		combined = strings.TrimSpace(combined + "\n" + apiResp.Data.Stderr)
	}
	if !apiResp.Success && apiResp.Data.ExitCode == 0 {
		return combined, -1, fmt.Errorf("workspace shell execute: %s", apiResp.Error)
	}
	return combined, apiResp.Data.ExitCode, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) buildCodeItemRuntimeContext(item MessageSequenceItem, scriptPath string, output string, exitCode int) string {
	snippet := truncateMessageSequenceLog(output, 2000)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Runtime context from previous code item: %s\n", item.ID))
	sb.WriteString("Status: success\n")
	sb.WriteString(fmt.Sprintf("Exit code: %d\n", exitCode))
	sb.WriteString(fmt.Sprintf("Executed script:\n%s\n", scriptPath))
	if len(item.OutputFiles) > 0 {
		sb.WriteString("Outputs:\n")
		for _, outputFile := range item.OutputFiles {
			sb.WriteString("- " + outputFile + "\n")
		}
	}
	if snippet != "" {
		sb.WriteString("Stdout/stderr summary:\n")
		sb.WriteString(snippet + "\n")
	}
	return sb.String()
}

func (hcpo *StepBasedWorkflowOrchestrator) buildCodeItemFailureContext(item MessageSequenceItem, itemRel string, scriptPath string, output string, exitCode int, execErr error) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Code item failed: %s\n", item.ID))
	sb.WriteString(fmt.Sprintf("Exit code: %d\n", exitCode))
	if execErr != nil {
		sb.WriteString(fmt.Sprintf("Runtime error: %s\n", execErr.Error()))
	}
	sb.WriteString(fmt.Sprintf("Working script:\n%s\n", scriptPath))
	sb.WriteString(fmt.Sprintf("Input contract:\n%s\n", hcpo.messageSequenceAbsPath(filepath.Join(itemRel, "input.json"))))
	if len(item.OutputFiles) > 0 {
		sb.WriteString("Expected outputs:\n")
		for _, outputFile := range item.OutputFiles {
			sb.WriteString("- " + outputFile + "\n")
		}
	}
	if snippet := truncateMessageSequenceLog(output, 4000); snippet != "" {
		sb.WriteString("Stdout/stderr excerpt:\n")
		sb.WriteString(snippet + "\n")
	}
	return sb.String()
}

func truncateMessageSequenceLog(output string, maxChars int) string {
	snippet := strings.TrimSpace(output)
	if maxChars <= 0 || len(snippet) <= maxChars {
		return snippet
	}
	half := maxChars / 2
	return snippet[:half] + "\n... (truncated) ...\n" + snippet[len(snippet)-half:]
}

// messageSequenceAbsPath lifts a WORKFLOW-ROOT-RELATIVE path (e.g.
// "runs/<run>/execution/<stepID>", as returned by messageSequenceExecutionRelPath
// and friends) to the absolute on-disk path the step agent uses:
// <docsRoot>/<workflowRoot>/<path>. The workflow root (GetWorkspacePath, e.g.
// "Workflow/social-media") MUST be included — otherwise the agent is told to
// write to <docsRoot>/runs/... , OUTSIDE its workflow folder, where the normal
// context system can't see the file (this was the message_sequence forward-pipe
// bug). The *Rel helpers stay workflow-root-relative for the workspace-file API
// (which resolves relative to the workflow root); this is the single place that
// converts them to absolute.
func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceAbsPath(workflowRel string) string {
	full := filepath.Join(hcpo.GetWorkspacePath(), workflowRel)
	docsRoot := GetPromptDocsRoot()
	if docsRoot == "" {
		return full
	}
	return filepath.Join(docsRoot, full)
}

// messageSequenceExecutionRelPath returns the step's execution folder — the SAME
// folder regular and orchestrator steps use (execution/<stepID>). The sequence's
// per-item artifacts and session.json live in subfolders under it, and its
// declared context_output lands directly here, so downstream context_dependencies
// resolve it exactly like any other step's output. (Previously this was an
// isolated execution/message_sequences/<stepPath>/<stepID> folder that was not on
// the dependency-resolution path, so a sequence could not hand off a local output
// file to later steps.)
func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceExecutionRelPath(stepPath string, stepID string) string {
	return filepath.Join("runs", hcpo.selectedRunFolder, "execution", getArtifactFolderName(stepID, stepPath))
}

func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceItemRelPath(stepPath string, stepID string, itemID string) string {
	return filepath.Join(hcpo.messageSequenceExecutionRelPath(stepPath, stepID), "items", itemID)
}

func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceSessionPath(stepPath string, stepID string) string {
	return filepath.Join(hcpo.messageSequenceExecutionRelPath(stepPath, stepID), "session.json")
}

func (hcpo *StepBasedWorkflowOrchestrator) cleanupMessageSequenceStepPath(ctx context.Context, stepPath string) error {
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf("selectedRunFolder not set - cannot cleanup message_sequence execution path")
	}
	if strings.TrimSpace(stepPath) == "" {
		return fmt.Errorf("stepPath not set - cannot cleanup message_sequence execution path")
	}
	relPath := filepath.Join("runs", hcpo.selectedRunFolder, "execution", "message_sequences", stepPath)
	hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning message_sequence execution path: %s", relPath))
	if err := hcpo.CleanupDirectory(ctx, relPath, fmt.Sprintf("execution/message_sequences/%s", stepPath)); err != nil {
		return fmt.Errorf("failed to cleanup message_sequence execution path %s: %w", stepPath, err)
	}
	return nil
}

func (hcpo *StepBasedWorkflowOrchestrator) cleanupMessageSequenceStepPathsForStep(ctx context.Context, stepNumber int) error {
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf("selectedRunFolder not set - cannot cleanup message_sequence execution paths")
	}
	root := filepath.Join(hcpo.GetWorkspacePath(), "runs", hcpo.selectedRunFolder, "execution", "message_sequences")
	stepPaths, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, root)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") || strings.Contains(errStr, "does not exist") {
			return nil
		}
		return err
	}
	for _, stepPath := range stepPaths {
		if !isMessageSequenceStepPathForStep(stepPath, stepNumber) {
			continue
		}
		if err := hcpo.cleanupMessageSequenceStepPath(ctx, stepPath); err != nil {
			return err
		}
	}
	return nil
}

// cleanupMessageSequenceRuntime wipes a route's on-disk execution artifacts (working code
// copies, stdout, the session.json log). Called on restart so a fresh attempt starts clean.
func (hcpo *StepBasedWorkflowOrchestrator) cleanupMessageSequenceRuntime(ctx context.Context, stepPath string, stepID string) error {
	relPath := hcpo.messageSequenceExecutionRelPath(stepPath, stepID)
	hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning message_sequence runtime: %s", relPath))
	if err := hcpo.CleanupDirectory(ctx, relPath, fmt.Sprintf("execution/message_sequences/%s/%s", stepPath, stepID)); err != nil {
		return fmt.Errorf("failed to cleanup message_sequence runtime %s/%s: %w", stepPath, stepID, err)
	}
	return nil
}

// msgSeqRouteKey identifies a message_sequence route's in-memory conversation within a run.
func (hcpo *StepBasedWorkflowOrchestrator) msgSeqRouteKey(stepPath, stepID string) string {
	return stepPath + "/" + stepID
}

// loadMsgSeqRouteSession returns a route's in-memory conversation if the orchestrator has
// already run it in this run. Route memory is never read back from disk.
func (hcpo *StepBasedWorkflowOrchestrator) loadMsgSeqRouteSession(key string) (*messageSequenceSession, bool) {
	hcpo.msgSeqRoutesMu.Lock()
	defer hcpo.msgSeqRoutesMu.Unlock()
	s, ok := hcpo.msgSeqRoutes[key]
	return s, ok
}

// storeMsgSeqRouteSession records a route's conversation so a later re-entry in the same run
// continues from where it left off.
func (hcpo *StepBasedWorkflowOrchestrator) storeMsgSeqRouteSession(key string, session *messageSequenceSession) {
	hcpo.msgSeqRoutesMu.Lock()
	defer hcpo.msgSeqRoutesMu.Unlock()
	if hcpo.msgSeqRoutes == nil {
		hcpo.msgSeqRoutes = make(map[string]*messageSequenceSession)
	}
	hcpo.msgSeqRoutes[key] = session
}

// clearMsgSeqRouteSession drops a route's in-memory conversation (used on restart).
func (hcpo *StepBasedWorkflowOrchestrator) clearMsgSeqRouteSession(key string) {
	hcpo.msgSeqRoutesMu.Lock()
	session := hcpo.msgSeqRoutes[key]
	delete(hcpo.msgSeqRoutes, key)
	hcpo.msgSeqRoutesMu.Unlock()
	hcpo.closeMessageSequenceRuntime(session, "message_sequence route restarted")
}

func (hcpo *StepBasedWorkflowOrchestrator) saveMessageSequenceSession(ctx context.Context, relPath string, session *messageSequenceSession) error {
	out, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return hcpo.WriteWorkspaceFile(ctx, relPath, string(out))
}

func (hcpo *StepBasedWorkflowOrchestrator) summarizeMessageSequenceSession(session *messageSequenceSession) string {
	completed := 0
	for _, entry := range session.Entries {
		if entry.Status == "completed" {
			completed++
		}
	}
	return fmt.Sprintf("Message sequence %s completed: %d item(s) completed", session.StepID, completed)
}
