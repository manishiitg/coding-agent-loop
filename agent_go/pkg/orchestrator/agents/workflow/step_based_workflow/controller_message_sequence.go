package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"

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
	ExecutionTurnCount  int                       `json:"execution_turn_count,omitempty"`
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

// messageSequenceClosingItems builds synthetic trailing items so a standalone
// message_sequence honors its step-level learning_objective and
// knowledgebase_contribution — the same post-step learnings/KB a regular step
// runs (a message_sequence otherwise skips the learning/KB phase entirely). Each
// is a user_message turn carrying the matching write access; the item machinery
// already grants learnings/_global or notes/ from kind + write_access.
func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceClosingItems(ctx context.Context, seq *MessageSequencePlanStep, stepIndex int) []MessageSequenceItem {
	cfg := seq.AgentConfigs
	if cfg == nil {
		return nil
	}
	var items []MessageSequenceItem
	stepID := seq.GetID()
	desc := seq.GetDescription()

	// Learnings: same gate the regular-step path uses (learnings_access write +
	// a non-empty learning_objective; BuildLearningsContributionTurn returns ""
	// when the objective is empty, so this is double-gated).
	if shouldDirectWriteLearnings(cfg, seq, hcpo.isEvaluationMode) && !hcpo.shouldSkipDirectLearningsDueToLock(ctx, cfg, stepIndex) {
		if msg := hcpo.buildLearningsContributionTurn(stepID, desc, strings.TrimSpace(cfg.LearningObjective), false); msg != "" {
			items = append(items, MessageSequenceItem{
				ID:          fmt.Sprintf("%s-learnings-contribution", stepID),
				Type:        "user_message",
				Kind:        "learning",
				Title:       "Learnings contribution",
				Message:     msg,
				WriteAccess: MessageSequenceWriteAccess{Learnings: true},
			})
		}
	}

	// Knowledgebase: write-capable access + a non-empty contribution instruction.
	if contribution := strings.TrimSpace(kbContributionForPrompt(cfg)); contribution != "" && kbAccessAllowsWrite(cfg.KnowledgebaseAccess) {
		var b strings.Builder
		b.WriteString("## Knowledgebase Contribution (dedicated turn)\n\n")
		b.WriteString("The sequence is complete. In this turn you have WRITE access to the knowledgebase. Fulfill this step's knowledgebase contribution, then stop.\n\n")
		b.WriteString("**Contribution instruction:**\n")
		b.WriteString(contribution)
		b.WriteString("\n\nWrite durable, deduplicated notes under `knowledgebase/notes/`. If there is nothing new worth recording, say so explicitly and write nothing.")
		items = append(items, MessageSequenceItem{
			ID:          fmt.Sprintf("%s-kb-contribution", stepID),
			Type:        "user_message",
			Kind:        "knowledgebase",
			Title:       "Knowledgebase contribution",
			Message:     b.String(),
			WriteAccess: MessageSequenceWriteAccess{Knowledgebase: true},
		})
	}
	return items
}

// Jump-repeat limits for next_step_id navigation, per step kind. LLM-driven
// steps get a tight cap because every extra cycle burns tokens; human_input
// loops are inherently self-limiting (each pass blocks on a human response),
// so their cap is only a failsafe against auto-responders. Routing passes 0:
// it has its own per-route evaluation guard.
const (
	maxLLMJumpRepeats   = 5
	maxHumanJumpRepeats = 25
)

// navigateToNextStepID advances the execution loop to the step whose ID matches
// nextStepID, mirroring routing's navigation so any route can converge to a
// shared downstream step (sibling steps between here and the target are skipped).
// sourceStepID identifies the jumping step for loop accounting; maxRepeats > 0
// bounds how many times the same source→target jump may fire within one run
// (the counter is in-memory for the run, like RoutingEvaluationCounts). Pass 0
// to disable the guard when the caller has its own loop protection.
// Returns: "end" (nextStepID=="end" — caller should break the loop), "jump" (i was
// repointed to land on the target next iteration; caller should continue), or
// "none" (empty/unknown id — caller falls through to the next sequential step),
// plus a non-nil error when the jump-repeat limit is exceeded — the workflow
// should terminate rather than keep cycling.
func (hcpo *StepBasedWorkflowOrchestrator) navigateToNextStepID(ctx context.Context, sourceStepID, nextStepID string, breakdownSteps []PlanStepInterface, progress *StepProgress, i *int, startFromStep *int, maxRepeats int) (string, error) {
	nextStepID = strings.TrimSpace(nextStepID)
	if nextStepID == "" {
		return "none", nil
	}
	if nextStepID == "end" {
		return "end", nil
	}
	targetStepIndex := -1
	for idx, s := range breakdownSteps {
		if s.GetID() == nextStepID {
			targetStepIndex = idx
			break
		}
	}
	if targetStepIndex < 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ next_step_id %q not found in plan - falling through to next sequential step", nextStepID))
		return "none", nil
	}
	if maxRepeats > 0 {
		if progress.JumpCounts == nil {
			progress.JumpCounts = make(map[string]int)
		}
		jumpKey := sourceStepID + "->" + nextStepID
		progress.JumpCounts[jumpKey]++
		if count := progress.JumpCounts[jumpKey]; count > maxRepeats {
			errMsg := fmt.Sprintf("infinite loop detected: step %q has jumped to next_step_id %q %d times in this run (limit %d)", sourceStepID, nextStepID, count, maxRepeats)
			hcpo.GetLogger().Error(errMsg, nil)
			hcpo.EmitOrchestratorAgentError(ctx, "workflow", "next-step-id-loop-detection", fmt.Sprintf("Jump from step %s", sourceStepID), errMsg, *i, 0)
			return "none", fmt.Errorf("workflow error: %s", errMsg)
		}
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔗 Jumping to step %d (ID: %s) as specified by next_step_id", targetStepIndex+1, nextStepID))
	if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup progress from step %d: %v (continuing anyway)", targetStepIndex+1, err))
	}
	runNumber := hcpo.getNextArchivalRunNumber(ctx, progress, targetStepIndex+1)
	for stepNum := targetStepIndex + 1; stepNum <= len(breakdownSteps); stepNum++ {
		if err := hcpo.archiveStepExecutionFolder(ctx, stepNum, runNumber); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive execution folder for step %d: %v", stepNum, err))
		}
	}
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after next_step_id navigation: %v", err))
	}
	if targetStepIndex < *startFromStep {
		*startFromStep = targetStepIndex
	}
	*i = targetStepIndex - 1
	return "jump", nil
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
	// session.json is intentionally a one-way observability log, not a resume checkpoint.
	// If the backend or workflow run is interrupted, that run is abandoned and a fresh run
	// starts the workflow from the beginning with a new execution ID. Mid-sequence recovery
	// is deliberately unsupported; steps that produce external side effects must follow the
	// same idempotency/deduplication contract as every other rerunnable workflow step.
	isRoute := opts.Source == "orchestrator_reentry"
	routeKey := hcpo.msgSeqRouteKey(stepPath, sequenceStep.GetID())
	sessionRelPath := hcpo.messageSequenceSessionPath(stepPath, sequenceStep.GetID())
	if isRoute {
		unlockRoute := hcpo.lockMsgSeqRoute(routeKey)
		defer unlockRoute()
	}

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
		plannedItems = appendMessageSequenceFinalValidation([]MessageSequenceItem{{
			ID:      fmt.Sprintf("reentry-%d", len(session.Entries)),
			Type:    "user_message",
			Kind:    "execution",
			Message: msg,
		}}, sequenceStep.ValidationSchema)
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
		// Run the configured queue, automatically enforce the step-level final
		// validation contract, then append synthetic learnings/KB contribution
		// turns so a standalone message_sequence honors its step-level
		// learning_objective / knowledgebase_contribution — the same post-step
		// learnings/KB a regular step runs. (Copy first so we never mutate the plan's
		// Items slice.)
		plannedItems = appendMessageSequenceFinalValidation(sequenceStep.Items, sequenceStep.ValidationSchema)
		plannedItems = append(plannedItems, hcpo.messageSequenceClosingItems(ctx, sequenceStep, stepIndex)...)
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
		summary, err := hcpo.executeMessageSequenceItem(ctx, sequenceStep, item, stepIndex, stepPath, session, isRoute)
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
	finalSummary := hcpo.summarizeMessageSequenceSession(session)
	if err := hcpo.saveFinalExecutionSummary(sequenceStep.GetID(), stepPath, finalSummary); err != nil {
		hcpo.recordRunPersistenceError(context.Background(), sequenceStep.GetID(), err)
	}
	return finalSummary, session.ConversationHistory, nil
}

// appendMessageSequenceFinalValidation makes a message_sequence obey the same
// step-level validation contract as a regular step. Explicit prevalidation items
// remain useful as intermediate gates, but authors do not need to duplicate the
// top-level schema as the final configured item. The synthetic gate deliberately
// runs before learnings/KB closing turns, so those bookkeeping turns cannot make
// an otherwise invalid work result look successful.
func appendMessageSequenceFinalValidation(items []MessageSequenceItem, schema *ValidationSchema) []MessageSequenceItem {
	planned := append([]MessageSequenceItem(nil), items...)
	if schema == nil {
		return planned
	}

	if len(planned) > 0 {
		last := planned[len(planned)-1]
		if strings.TrimSpace(last.Type) == "prevalidation" {
			lastSchema := last.ValidationSchema
			if lastSchema == nil {
				lastSchema = last.Prevalidation
			}
			if lastSchema == nil {
				lastSchema = schema
			}
			if equalValidationSchemas(lastSchema, schema) {
				return planned
			}
		}
	}

	id := "__automatic_final_validation__"
	usedIDs := make(map[string]struct{}, len(planned))
	for _, item := range planned {
		usedIDs[item.ID] = struct{}{}
	}
	for suffix := 2; ; suffix++ {
		if _, exists := usedIDs[id]; !exists {
			break
		}
		id = fmt.Sprintf("__automatic_final_validation_%d__", suffix)
	}

	return append(planned, MessageSequenceItem{
		ID:               id,
		Type:             "prevalidation",
		Title:            "Final validation",
		ValidationSchema: schema,
	})
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

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceItem(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, session *messageSequenceSession, isNestedExecution bool) (string, error) {
	switch item.Type {
	case "user_message", "":
		return hcpo.executeMessageSequenceUserMessage(ctx, step, item, stepIndex, stepPath, session)
	case "foreach":
		return hcpo.executeMessageSequenceForeachItem(ctx, step, item, stepIndex, stepPath, session)
	case "code":
		return "", fmt.Errorf("message_sequence step %q item %q uses removed type \"code\"; upgrade the workflow to contract v1.0.10 before running it", step.ID, item.ID)
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
				hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, isNestedExecution, results)
				hcpo.saveMessageSequencePreValidationLog(ctx, step, stepPath, results, schema)
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
			hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, isNestedExecution, results)
			hcpo.saveMessageSequencePreValidationLog(ctx, step, stepPath, results, schema)
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

// saveMessageSequencePreValidationLog preserves the latest gate result in the
// same compact log used by regular steps. Each later attempt overwrites the
// previous one, so Pulse gets durable evidence without accumulating per-attempt
// log files.
func (hcpo *StepBasedWorkflowOrchestrator) saveMessageSequencePreValidationLog(
	ctx context.Context,
	step *MessageSequencePlanStep,
	stepPath string,
	results *WorkspaceVerificationResult,
	schema *ValidationSchema,
) {
	if hcpo == nil || step == nil || results == nil || strings.TrimSpace(hcpo.selectedRunFolder) == "" {
		return
	}
	preValidationLogPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	SavePreValidationLog(ctx, hcpo.BaseOrchestrator, preValidationLogPath, step.GetID(), stepPath, results, schema)
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
	writeAccess := hcpo.resolveMessageSequenceItemWriteAccess(getAgentConfigs(step), item)
	if writeAccess.Learnings && hcpo.shouldSkipDirectLearningsDueToLock(ctx, step.AgentConfigs, stepIndex) {
		writeAccess.Learnings = false
	}
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard(stepPath, step.GetID(), getAgentConfigs(step), writeAccess)
	runtime, agentCtx, err := hcpo.getMessageSequenceRuntime(ctx, step, stepPath, session, readPaths, writePaths)
	if err != nil {
		return "", err
	}
	if writeAccess.Learnings {
		restoreDirectLearningTurn := hcpo.prepareDirectLearningTurn(runtime.Agent, []string{filepath.Join(hcpo.GetWorkspacePath(), LearningsFolderName, GlobalLearningID)})
		defer restoreDirectLearningTurn()
	}

	message := strings.TrimSpace(item.Message)
	if session.LastRuntimeContext != "" {
		message = session.LastRuntimeContext + "\n\n## Next instruction\n" + message
	}
	templateVars := hcpo.buildMessageSequenceTemplateVars(step, item, stepIndex, stepPath, message, readPaths, writePaths, writeAccess)

	turnCtx := agentCtx
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		turnCtx = cab.StartTimingCaptureFor(turnCtx)
	}
	session.ExecutionTurnCount++
	turnNumber := session.ExecutionTurnCount
	attemptStartedAt := time.Now().UTC()
	result, history, err := hcpo.withWorkshopMessageTarget(turnCtx, step.GetID(), "message-sequence:"+item.ID, runtime.Agent, func() (string, []llmtypes.MessageContent, error) {
		return runtime.Agent.Execute(turnCtx, templateVars, session.ConversationHistory)
	})
	attemptCompletedAt := time.Now().UTC()
	attemptDuration := attemptCompletedAt.Sub(attemptStartedAt)

	loggedHistory := history
	if len(loggedHistory) == 0 {
		loggedHistory = session.ConversationHistory
	}
	loggedResult := formatMessageSequenceTurnLogResult(item, result, err)
	executionLLM := agentConfigModelLabel(runtime.Agent.GetConfig())
	var logErr error
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		timingCapture := cab.DrainTimingCaptureFor(turnCtx)
		logErr = hcpo.saveExecutionConversationLogs(
			stepIndex, step.GetID(), stepPath, 1, turnNumber,
			loggedResult, executionLLM, loggedHistory, runtime.Agent,
			timingCapture.ToolCalls, timingCapture.LLMCalls,
			attemptStartedAt, attemptCompletedAt, attemptDuration,
		)
	} else {
		logErr = hcpo.saveExecutionConversationLogs(
			stepIndex, step.GetID(), stepPath, 1, turnNumber,
			loggedResult, executionLLM, loggedHistory, runtime.Agent,
			nil, nil,
			attemptStartedAt, attemptCompletedAt, attemptDuration,
		)
	}
	if logErr != nil {
		hcpo.recordRunPersistenceError(context.Background(), step.GetID(), logErr)
	}

	if err != nil {
		if len(history) > 0 {
			session.ConversationHistory = history
		}
		return "", err
	}
	session.ConversationHistory = history
	session.LastRuntimeContext = ""
	return strings.TrimSpace(result), nil
}

func formatMessageSequenceTurnLogResult(item MessageSequenceItem, result string, err error) string {
	itemType := strings.TrimSpace(item.Type)
	if itemType == "" {
		itemType = "user_message"
	}
	header := fmt.Sprintf("Message sequence item: %s (%s)", item.ID, itemType)
	trimmedResult := strings.TrimSpace(result)
	if err != nil {
		if trimmedResult == "" {
			return fmt.Sprintf("%s\nSTATUS: FAILED\n%s", header, err.Error())
		}
		return fmt.Sprintf("%s\nSTATUS: FAILED\n%s\n\n%s", header, err.Error(), trimmedResult)
	}
	if trimmedResult == "" {
		return header
	}
	return header + "\n" + trimmedResult
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
	hcpo.setMessageSequenceShellEnv(sessionID, stepPath, step.GetID())

	folderOverride := &messageSequenceFolderGuardOverride{ReadPaths: readPaths, WritePaths: writePaths}
	sessionOverride := &messageSequenceRuntimeSessionOverride{SessionID: sessionID, KeepAlive: true}
	agentCtx := context.WithValue(ctx, messageSequenceFolderGuardOverrideKey{}, folderOverride)
	agentCtx = context.WithValue(agentCtx, messageSequenceRuntimeSessionOverrideKey{}, sessionOverride)

	if session.runtime != nil && session.runtime.Agent != nil {
		return session.runtime, agentCtx, nil
	}

	agentName := fmt.Sprintf("message-sequence-%s", step.GetID())
	agent, err := hcpo.createExecutionOnlyAgent(agentCtx, "execution_only", stepPath, agentName, step.AgentConfigs, step.GetID(), "", false)
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
			hcpo.setMessageSequenceShellEnv(sessionID, stepPath, step.GetID())
		}
	}
	session.runtime = &messageSequenceRuntime{
		Agent:     agent,
		SessionID: sessionID,
		Provider:  provider,
	}
	return session.runtime, agentCtx, nil
}

// setMessageSequenceShellEnv exports the per-step shell env (DB_PATH,
// STEP_OUTPUT_DIR, STEP_EXECUTION_DIR) onto the session so the server-side bridge
// shell (api-bridge.execute_shell_command) resolves "$DB_PATH" the same way the
// in-process built-in executor does. Without this, message-sequence items routed
// to the bridge shell run with an unset $DB_PATH and every sqlite3 "$DB_PATH"
// write fails (falling back to the read-only relative db path).
func (hcpo *StepBasedWorkflowOrchestrator) setMessageSequenceShellEnv(sessionID, stepPath, stepID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	stepOutputAbs := hcpo.messageSequenceAbsPath(hcpo.messageSequenceExecutionRelPath(stepPath, stepID))
	dbAbs := filepath.Join(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), DBFolderName, "db.sqlite")
	common.SetSessionShellEnv(sessionID, map[string]string{
		"DB_PATH":            dbAbs,
		"STEP_OUTPUT_DIR":    stepOutputAbs,
		"STEP_EXECUTION_DIR": filepath.Dir(stepOutputAbs),
	})
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
	case string(llmproviders.ProviderPiCLI):
		llmproviders.ClosePiCLIInteractiveSessionForOwner(ownerSessionID, reason)
	}
}

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

func requestedMessageSequenceItemWriteAccess(item MessageSequenceItem) (MessageSequenceWriteAccess, bool) {
	if item.WriteAccess != (MessageSequenceWriteAccess{}) {
		return item.WriteAccess, true
	}
	var access MessageSequenceWriteAccess
	switch item.Kind {
	case "learning":
		access.Learnings = true
	case "knowledgebase":
		access.Knowledgebase = true
	case "db":
		access.DB = true
	default:
		return MessageSequenceWriteAccess{}, false
	}
	return access, true
}

// resolveMessageSequenceItemWriteAccess applies the same step-level store
// permissions as a regular execution step. A non-empty item write_access (or
// kind) is an optional narrowing override; it can never escalate beyond the
// step's configured permissions. This prevents a plain sequence turn from being
// silently read-only when the step itself is configured to write.
func (hcpo *StepBasedWorkflowOrchestrator) resolveMessageSequenceItemWriteAccess(stepConfig *AgentConfigs, item MessageSequenceItem) MessageSequenceWriteAccess {
	requested, hasItemOverride := requestedMessageSequenceItemWriteAccess(item)
	if hasItemOverride {
		return hcpo.constrainMessageSequenceWriteAccess(stepConfig, requested)
	}

	kbAccess := resolveKnowledgebaseAccess(stepConfig, hcpo.UseKnowledgebase())
	return MessageSequenceWriteAccess{
		DB:            resolveDBAccess(stepConfig) == DBAccessReadWrite,
		Knowledgebase: kbAccessAllowsWrite(kbAccess) && resolveKnowledgebaseWriteMethod(stepConfig) == KBWriteMethodDirect,
		Learnings:     resolveLearningsAccess(stepConfig) == LearningsAccessReadWrite,
	}
}

func (hcpo *StepBasedWorkflowOrchestrator) constrainMessageSequenceWriteAccess(stepConfig *AgentConfigs, requested MessageSequenceWriteAccess) MessageSequenceWriteAccess {
	return MessageSequenceWriteAccess{
		DB:            requested.DB && resolveDBAccess(stepConfig) == DBAccessReadWrite,
		Knowledgebase: requested.Knowledgebase && kbAccessAllowsWrite(resolveKnowledgebaseAccess(stepConfig, hcpo.UseKnowledgebase())) && resolveKnowledgebaseWriteMethod(stepConfig) == KBWriteMethodDirect,
		Learnings:     requested.Learnings && resolveLearningsAccess(stepConfig) == LearningsAccessReadWrite,
	}
}

func (hcpo *StepBasedWorkflowOrchestrator) setupMessageSequenceFolderGuard(stepPath string, stepID string, stepConfig *AgentConfigs, itemWriteAccess MessageSequenceWriteAccess) (readPaths, writePaths []string) {
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
	}
	dbAccess := resolveDBAccess(stepConfig)
	kbAccess := resolveKnowledgebaseAccess(stepConfig, hcpo.UseKnowledgebase())
	learningsAccess := resolveLearningsAccess(stepConfig)
	readPaths = append(readPaths, getDBPath(baseWorkspacePath))
	if kbAccessAllowsRead(kbAccess) {
		readPaths = append(readPaths, getKnowledgebasePath(baseWorkspacePath))
	}
	if learningsAccess != LearningsAccessNone {
		readPaths = appendLearningReadPaths(readPaths, baseWorkspacePath, stepID)
	}
	writePaths = []string{stepFolderPath, downloadsPath}
	if itemWriteAccess.DB && dbAccess == DBAccessReadWrite {
		writePaths = append(writePaths, getDBPath(baseWorkspacePath))
	}
	if itemWriteAccess.Knowledgebase && kbAccessAllowsWrite(kbAccess) && resolveKnowledgebaseWriteMethod(stepConfig) == KBWriteMethodDirect {
		writePaths = append(writePaths, filepath.Join(getKnowledgebasePath(baseWorkspacePath), "notes"))
	}
	if itemWriteAccess.Learnings && learningsAccess == LearningsAccessReadWrite {
		writePaths = append(writePaths, filepath.Join(baseWorkspacePath, LearningsFolderName, GlobalLearningID))
	}
	readPaths = hcpo.appendCDPHostDownloadsReadPath(readPaths)
	return common.DeduplicateStrings(readPaths), common.DeduplicateStrings(writePaths)
}

func (hcpo *StepBasedWorkflowOrchestrator) buildMessageSequenceTemplateVars(step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, message string, readPaths []string, writePaths []string, writeAccess MessageSequenceWriteAccess) map[string]string {
	stepExecRel := hcpo.messageSequenceExecutionRelPath(stepPath, step.GetID())
	docsRoot := GetPromptDocsRoot()
	kbAccess := KBAccessRead
	if writeAccess.Knowledgebase {
		kbAccess = KBAccessReadWrite
	}
	// Honor the step's declared context_output so the sequence writes the file
	// downstream steps expect (in execution/<stepID>/, the normal step folder).
	// Fall back to the generic name only when the step declares no output.
	contextOutput := strings.TrimSpace(step.GetContextOutput().String())
	if contextOutput == "" {
		contextOutput = "message_sequence_result.json"
	}
	return map[string]string{
		"StepTitle":                 step.GetTitle(),
		"StepDescription":           message,
		"BaseDescription":           message,
		"OrchestratorInstructions":  message,
		"StepContextDependencies":   strings.Join(step.GetContextDependencies(), "\n"),
		"StepContextOutput":         contextOutput,
		"WorkspacePath":             hcpo.messageSequenceAbsPath(filepath.Join("runs", hcpo.selectedRunFolder, "execution")),
		"WorkflowRoot":              hcpo.messageSequenceAbsPath(""),
		"DocsRoot":                  docsRoot,
		"StepExecutionPath":         hcpo.messageSequenceAbsPath(stepExecRel),
		"DBPath":                    hcpo.messageSequenceAbsPath(DBFolderName),
		"KnowledgebasePath":         hcpo.messageSequenceAbsPath(KnowledgebaseFolderName),
		"FolderGuardReadPaths":      strings.Join(toAbsPaths(docsRoot, readPaths), ", "),
		"FolderGuardWritePaths":     strings.Join(toAbsPaths(docsRoot, writePaths), ", "),
		"StepNumber":                fmt.Sprintf("%d", stepIndex+1),
		"IsCodeExecutionMode":       "false",
		"UseCodeStyleRules":         "",
		"KbAccess":                  kbAccess,
		"KbAccessLabel":             kbAccessLabel(kbAccess),
		"KbWriteMethod":             KBWriteMethodDirect,
		"KBGuidanceBlock":           BuildStepKBGuidanceWithTarget(kbAccess, KBWriteMethodDirect, "", hcpo.messageSequenceAbsPath(filepath.Join(KnowledgebaseFolderName, KBNotesFolderName))),
		"MessageSequenceAccessNote": buildMessageSequenceAccessNote(writeAccess),
		"HasLearnings":              "false",
		"CurrentDate":               time.Now().Format("2006-01-02"),
		"CurrentTime":               time.Now().Format("15:04:05"),
	}
}

func buildMessageSequenceAccessNote(writeAccess MessageSequenceWriteAccess) string {
	grants := []string{"step folder", "Downloads"}
	if writeAccess.DB {
		grants = append(grants, "db/")
	}
	if writeAccess.Knowledgebase {
		grants = append(grants, "knowledgebase/notes/")
	}
	if writeAccess.Learnings {
		grants = append(grants, "learnings/_global/")
	}
	return "Reads are available for execution outputs, soul, builder logs, db/, knowledgebase/, learnings/_global/, and this step's learnings folder. Writes for this item are limited to: " + strings.Join(grants, ", ") + "."
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

// cleanupMessageSequenceRuntime wipes a route's on-disk execution artifacts
// (item state, output snapshots, and session.json). Called on restart so a fresh attempt starts clean.
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

// lockMsgSeqRoute prevents concurrent calls from mutating the same stateful
// route conversation. Different routes retain full parallelism.
func (hcpo *StepBasedWorkflowOrchestrator) lockMsgSeqRoute(key string) func() {
	hcpo.msgSeqRoutesMu.Lock()
	if hcpo.msgSeqRouteLocks == nil {
		hcpo.msgSeqRouteLocks = make(map[string]*sync.Mutex)
	}
	lock := hcpo.msgSeqRouteLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		hcpo.msgSeqRouteLocks[key] = lock
	}
	hcpo.msgSeqRoutesMu.Unlock()

	lock.Lock()
	return lock.Unlock
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

// clearAllMsgSeqRouteSessions drops every route's in-memory conversation and
// closes the underlying runtimes. Route memory is scoped to one execution
// phase — without this drain, a reused orchestrator instance (next iteration
// or next run) silently resumes a prior run's route conversations and the
// runtimes leak.
func (hcpo *StepBasedWorkflowOrchestrator) clearAllMsgSeqRouteSessions(reason string) {
	hcpo.msgSeqRoutesMu.Lock()
	sessions := hcpo.msgSeqRoutes
	hcpo.msgSeqRoutes = nil
	hcpo.msgSeqRouteLocks = nil
	hcpo.msgSeqRoutesMu.Unlock()
	for key, session := range sessions {
		hcpo.GetLogger().Info(fmt.Sprintf("🧹 Dropping message_sequence route session %q (%s)", key, reason))
		hcpo.closeMessageSequenceRuntime(session, reason)
	}
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
	var concerns []string
	for _, entry := range session.Entries {
		if entry.Status == "completed" {
			completed++
		}
		for _, line := range strings.Split(entry.Summary, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(strings.ToUpper(line), "CONCERNS:") {
				continue
			}
			concern := strings.TrimSpace(line[len("CONCERNS:"):])
			if concern == "" {
				continue
			}
			itemID := strings.TrimSpace(entry.ItemID)
			if itemID == "" {
				itemID = strings.TrimSpace(entry.EntryID)
			}
			if itemID != "" {
				concern = fmt.Sprintf("%s: %s", itemID, concern)
			}
			concerns = append(concerns, concern)
		}
	}
	summary := fmt.Sprintf("Message sequence %s completed: %d item(s) completed", session.StepID, completed)
	if len(concerns) > 0 {
		summary += "\nCONCERNS: " + strings.Join(concerns, "; ")
	}
	return summary
}
