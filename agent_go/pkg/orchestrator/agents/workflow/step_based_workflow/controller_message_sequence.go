package step_based_workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

type messageSequenceFolderGuardOverrideKey struct{}

type messageSequenceFolderGuardOverride struct {
	ReadPaths  []string
	WritePaths []string
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
	Entries             []messageSequenceEntry    `json:"entries,omitempty"`
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

	sessionRelPath := hcpo.messageSequenceSessionPath(stepPath, sequenceStep.GetID())
	session, sessionExists, err := hcpo.loadMessageSequenceSession(ctx, sessionRelPath)
	if err != nil {
		return "", nil, err
	}
	if opts.Restart {
		if sessionExists {
			if err := hcpo.archiveMessageSequenceSession(ctx, sessionRelPath); err != nil {
				return "", nil, err
			}
		}
		if err := hcpo.cleanupMessageSequenceRuntime(ctx, stepPath, sequenceStep.GetID(), true); err != nil {
			return "", nil, err
		}
		sessionExists = false
		session = nil
	}

	var plannedItems []MessageSequenceItem
	source := opts.Source
	if source == "" {
		source = "configured_queue"
	}
	if !sessionExists {
		session = &messageSequenceSession{
			SessionID: sequenceStep.GetID(),
			StepID:    sequenceStep.GetID(),
			RunFolder: hcpo.selectedRunFolder,
			Status:    "running",
			CreatedAt: time.Now(),
		}
		plannedItems = sequenceStep.Items
		if strings.TrimSpace(opts.ReentryMessage) != "" {
			session.LastRuntimeContext = "Builder/orchestrator initial instruction:\n" + opts.ReentryMessage
		}
		source = "configured_queue"
	} else {
		msg := strings.TrimSpace(opts.ReentryMessage)
		if msg == "" {
			return "", session.ConversationHistory, fmt.Errorf("message_sequence %q already has a session; provide a re-entry message or restart", sequenceStep.GetID())
		}
		plannedItems = []MessageSequenceItem{{
			ID:      fmt.Sprintf("reentry-%d", time.Now().UnixNano()),
			Type:    "user_message",
			Kind:    "execution",
			Message: msg,
		}}
		if source == "configured_queue" {
			source = "builder_resume"
		}
	}

	for _, item := range plannedItems {
		started := time.Now()
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
		if err != nil {
			entry.Status = "failed"
			entry.Summary = err.Error()
			session.Status = "failed"
			session.Entries = append(session.Entries, entry)
			session.UpdatedAt = time.Now()
			_ = hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session)
			return "", session.ConversationHistory, err
		}
		session.Entries = append(session.Entries, entry)
		session.UpdatedAt = time.Now()
		if err := hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session); err != nil {
			return "", session.ConversationHistory, err
		}
	}

	session.Status = "completed"
	session.UpdatedAt = time.Now()
	if err := hcpo.saveMessageSequenceSession(ctx, sessionRelPath, session); err != nil {
		return "", session.ConversationHistory, err
	}
	return hcpo.summarizeMessageSequenceSession(session), session.ConversationHistory, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceItem(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, session *messageSequenceSession) (string, error) {
	switch item.Type {
	case "user_message", "":
		return hcpo.executeMessageSequenceUserMessage(ctx, step, item, stepIndex, stepPath, session)
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
		results, err := RunPreValidation(ctx, schema, hcpo.messageSequenceExecutionRelPath(stepPath, step.GetID()), hcpo.BaseOrchestrator)
		if err != nil {
			results = &WorkspaceVerificationResult{
				OverallPass:  false,
				FilesChecked: []FileCheckResult{},
				Summary: ValidationSummary{
					TotalChecks:  0,
					PassedChecks: 0,
					FailedChecks: 1,
					SchemaErrors: 0,
					Errors: []ValidationError{{
						File:      "",
						Path:      "",
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
					SchemaErrors: 0,
					Errors: []ValidationError{{
						File:      "",
						Path:      "",
						CheckType: "pre_validation_error",
						Expected:  "pre-validation to return a result",
						Actual:    "no result returned",
						Message:   fmt.Sprintf("Pre-validation returned no result for message sequence item %q", item.ID),
					}},
					SchemaWarnings: []ValidationError{},
				},
			}
		}
		hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, false, results)
		if !results.OverallPass {
			return "", fmt.Errorf("message sequence prevalidation failed for item %q", item.ID)
		}
		return "prevalidation passed", nil
	default:
		return "", fmt.Errorf("unsupported message_sequence item type %q", item.Type)
	}
}

func (hcpo *StepBasedWorkflowOrchestrator) executeMessageSequenceUserMessage(ctx context.Context, step *MessageSequencePlanStep, item MessageSequenceItem, stepIndex int, stepPath string, session *messageSequenceSession) (string, error) {
	writeAccess := resolveMessageSequenceItemWriteAccess(item)
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard(stepPath, step.GetID(), writeAccess)
	override := &messageSequenceFolderGuardOverride{ReadPaths: readPaths, WritePaths: writePaths}
	agentCtx := context.WithValue(ctx, messageSequenceFolderGuardOverrideKey{}, override)

	agentName := fmt.Sprintf("message-sequence-%s-%s", step.GetID(), item.ID)
	agent, err := hcpo.createExecutionOnlyAgent(agentCtx, "execution_only", stepPath, agentName, step.AgentConfigs, step.GetID(), "")
	if err != nil {
		return "", err
	}

	message := strings.TrimSpace(item.Message)
	if session.LastRuntimeContext != "" {
		message = session.LastRuntimeContext + "\n\n## Next instruction\n" + message
	}
	templateVars := hcpo.buildMessageSequenceTemplateVars(step, item, stepIndex, stepPath, message, readPaths, writePaths)
	result, history, err := agent.Execute(agentCtx, templateVars, session.ConversationHistory)
	if err != nil {
		return "", err
	}
	session.ConversationHistory = history
	session.LastRuntimeContext = ""
	return strings.TrimSpace(result), nil
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
	agentCtx := context.WithValue(ctx, messageSequenceFolderGuardOverrideKey{}, override)
	agentName := fmt.Sprintf("message-sequence-%s-%s-repair-%d", step.GetID(), item.ID, attempt)
	agent, err := hcpo.createExecutionOnlyAgent(agentCtx, "execution_only", stepPath, agentName, step.AgentConfigs, step.GetID(), "")
	if err != nil {
		return err
	}
	message := failureContext + "\n\nRepair the working copy at " + messageSequenceAbsPath(filepath.Join(codeRel, "main.py")) + ". Keep the fix narrowly scoped. Do not announce success; the runtime will rerun the script after your edit."
	templateVars := hcpo.buildMessageSequenceTemplateVars(step, item, 0, stepPath, message, override.ReadPaths, override.WritePaths)
	_, history, err := agent.Execute(agentCtx, templateVars, session.ConversationHistory)
	if err != nil {
		return err
	}
	session.ConversationHistory = history
	return nil
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
	stepFolderPath := hcpo.messageSequenceExecutionRelPath(stepPath, stepID)
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
	return map[string]string{
		"StepTitle":                step.GetTitle(),
		"StepDescription":          message,
		"BaseDescription":          message,
		"OrchestratorInstructions": message,
		"StepContextDependencies":  strings.Join(step.GetContextDependencies(), "\n"),
		"StepContextOutput":        "message_sequence_result.json",
		"WorkspacePath":            messageSequenceAbsPath(filepath.Join(hcpo.GetWorkspacePath(), "runs", hcpo.selectedRunFolder, "execution")),
		"WorkflowRoot":             messageSequenceAbsPath(hcpo.GetWorkspacePath()),
		"DocsRoot":                 docsRoot,
		"StepExecutionPath":        messageSequenceAbsPath(stepExecRel),
		"DBPath":                   messageSequenceAbsPath(getDBPath(hcpo.GetWorkspacePath())),
		"KnowledgebasePath":        messageSequenceAbsPath(getKnowledgebasePath(hcpo.GetWorkspacePath())),
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
	itemAbs := messageSequenceAbsPath(itemRel)
	codeAbs := messageSequenceAbsPath(codeRel)
	mainAbs := messageSequenceAbsPath(mainRel)
	readPaths = append(readPaths, itemRel, codeRel)
	writePaths = append(writePaths, itemRel, codeRel)

	var cmd strings.Builder
	cmd.WriteString("python3 -B ")
	cmd.WriteString(shellQuotePath(mainAbs))
	for _, input := range item.InputFiles {
		cmd.WriteString(" ")
		cmd.WriteString(shellQuotePath(messageSequenceAbsPath(input)))
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
	sb.WriteString(fmt.Sprintf("Input contract:\n%s\n", messageSequenceAbsPath(filepath.Join(itemRel, "input.json"))))
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

func messageSequenceAbsPath(path string) string {
	docsRoot := GetPromptDocsRoot()
	if path == "" || docsRoot == "" {
		return path
	}
	return filepath.Join(docsRoot, path)
}

func (hcpo *StepBasedWorkflowOrchestrator) messageSequenceExecutionRelPath(stepPath string, stepID string) string {
	return filepath.Join("runs", hcpo.selectedRunFolder, "execution", "message_sequences", stepPath, stepID)
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

func (hcpo *StepBasedWorkflowOrchestrator) cleanupMessageSequenceRuntime(ctx context.Context, stepPath string, stepID string, preserveArchive bool) error {
	relPath := hcpo.messageSequenceExecutionRelPath(stepPath, stepID)
	if !preserveArchive {
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning message_sequence runtime: %s", relPath))
		if err := hcpo.CleanupDirectory(ctx, relPath, fmt.Sprintf("execution/message_sequences/%s/%s", stepPath, stepID)); err != nil {
			return fmt.Errorf("failed to cleanup message_sequence runtime %s/%s: %w", stepPath, stepID, err)
		}
		return nil
	}

	entries, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, relPath)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") || strings.Contains(errStr, "does not exist") {
			return nil
		}
		return fmt.Errorf("failed to list message_sequence runtime %s/%s: %w", stepPath, stepID, err)
	}
	for _, entry := range entries {
		if entry == "archive" {
			continue
		}
		entryRelPath := filepath.Join(relPath, entry)
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning message_sequence runtime entry: %s", entryRelPath))
		if err := hcpo.CleanupDirectory(ctx, entryRelPath, fmt.Sprintf("execution/message_sequences/%s/%s/%s", stepPath, stepID, entry)); err != nil {
			return fmt.Errorf("failed to cleanup message_sequence runtime entry %s: %w", entryRelPath, err)
		}
	}
	return nil
}

func (hcpo *StepBasedWorkflowOrchestrator) loadMessageSequenceSession(ctx context.Context, relPath string) (*messageSequenceSession, bool, error) {
	content, err := hcpo.ReadWorkspaceFile(ctx, relPath)
	if err != nil {
		return nil, false, nil
	}
	var session messageSequenceSession
	if err := json.Unmarshal([]byte(content), &session); err != nil {
		return nil, false, fmt.Errorf("parse message sequence session %s: %w", relPath, err)
	}
	return &session, true, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) saveMessageSequenceSession(ctx context.Context, relPath string, session *messageSequenceSession) error {
	out, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return hcpo.WriteWorkspaceFile(ctx, relPath, string(out))
}

func (hcpo *StepBasedWorkflowOrchestrator) archiveMessageSequenceSession(ctx context.Context, relPath string) error {
	content, err := hcpo.ReadWorkspaceFile(ctx, relPath)
	if err != nil {
		return nil
	}
	archivePath := filepath.Join(filepath.Dir(relPath), "archive", fmt.Sprintf("%d-session.json", time.Now().UnixNano()))
	return hcpo.WriteWorkspaceFile(ctx, archivePath, content)
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
