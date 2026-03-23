package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	DefaultFinalOutputFilename = "final_output.md"
	DefaultOutputPlanPath      = "planning/output_plan.json"
	defaultOutputStepID        = "final-output"
	internalOutputRunFolder    = "__report_generation"
	internalOutputStepID       = "final-output-generation"
	internalOutputStepPath     = "step-1"
	maxFinalOutputFiles        = 80
	maxFinalOutputFileChars    = 12000
)

// WorkflowFinalOutputConfig is a simplified view of the primary output step.
// It is kept for API compatibility with the existing frontend viewer response.
type WorkflowFinalOutputConfig struct {
	Enabled        bool   `json:"enabled"`
	Title          string `json:"title,omitempty"`
	Instructions   string `json:"instructions,omitempty"`
	OutputFilename string `json:"output_filename,omitempty"`
}

func (c *WorkflowFinalOutputConfig) Normalize() {
	if c.OutputFilename == "" {
		c.OutputFilename = DefaultFinalOutputFilename
	}
	if !strings.HasSuffix(strings.ToLower(c.OutputFilename), ".md") {
		c.OutputFilename = DefaultFinalOutputFilename
	}
}

func (c *WorkflowFinalOutputConfig) ToOutputPlan() *WorkflowOutputPlan {
	if c == nil {
		return &WorkflowOutputPlan{}
	}
	c.Normalize()
	return &WorkflowOutputPlan{
		Step: &WorkflowOutputPlanStep{
			ID:             defaultOutputStepID,
			Title:          c.Title,
			Instructions:   c.Instructions,
			OutputFilename: c.OutputFilename,
			Enabled:        c.Enabled,
		},
	}
}

type WorkflowOutputPlan struct {
	Step  *WorkflowOutputPlanStep   `json:"step,omitempty"`
	Steps []*WorkflowOutputPlanStep `json:"steps,omitempty"` // legacy compatibility
}

type WorkflowOutputPlanStep struct {
	ID             string            `json:"id"`
	Title          string            `json:"title,omitempty"`
	Instructions   string            `json:"instructions,omitempty"`
	PreValidation  *ValidationSchema `json:"pre_validation,omitempty"`
	OutputFilename string            `json:"output_filename,omitempty"`
	Enabled        bool              `json:"enabled"`
}

// ParseWorkflowOutputPlan parses planning/output_plan.json and accepts both the
// current nested plan shape ({ "step": {...} }) and the flat single-step shape
// ({ "id": "...", "instructions": "...", ... }) that may already exist in
// workspaces created before the nested wrapper was enforced.
func ParseWorkflowOutputPlan(content string) (*WorkflowOutputPlan, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	var plan WorkflowOutputPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse output_plan.json: %w", err)
	}
	plan.Normalize()
	if plan.Step != nil || len(plan.Steps) > 0 {
		return &plan, nil
	}

	var flatStep WorkflowOutputPlanStep
	if err := json.Unmarshal([]byte(content), &flatStep); err == nil {
		if workflowOutputStepLooksConfigured(&flatStep) {
			flatStep.Normalize()
			return &WorkflowOutputPlan{Step: &flatStep}, nil
		}
	}

	var flatConfig WorkflowFinalOutputConfig
	if err := json.Unmarshal([]byte(content), &flatConfig); err == nil {
		if workflowFinalOutputConfigLooksConfigured(&flatConfig) {
			plan := flatConfig.ToOutputPlan()
			plan.Normalize()
			return plan, nil
		}
	}

	return &plan, nil
}

func (p *WorkflowOutputPlan) Normalize() {
	if p == nil {
		return
	}
	if p.Step == nil {
		for _, step := range p.Steps {
			if step != nil {
				p.Step = step
				break
			}
		}
	}
	if p.Step != nil {
		p.Step.Normalize()
	}
	p.Steps = nil
}

func (p *WorkflowOutputPlan) PrimaryStep() *WorkflowOutputPlanStep {
	if p == nil {
		return nil
	}
	if p.Step != nil {
		p.Step.Normalize()
		if p.Step.Enabled {
			return p.Step
		}
	}
	return nil
}

func (p *WorkflowOutputPlan) FirstStep() *WorkflowOutputPlanStep {
	if p == nil {
		return nil
	}
	if p.Step != nil {
		p.Step.Normalize()
		return p.Step
	}
	return nil
}

func (s *WorkflowOutputPlanStep) Normalize() {
	if s.ID == "" {
		s.ID = defaultOutputStepID
	}
	if s.OutputFilename == "" {
		s.OutputFilename = DefaultFinalOutputFilename
	}
	if !strings.HasSuffix(strings.ToLower(s.OutputFilename), ".md") {
		s.OutputFilename = DefaultFinalOutputFilename
	}
}

func workflowOutputStepLooksConfigured(step *WorkflowOutputPlanStep) bool {
	if step == nil {
		return false
	}
	return strings.TrimSpace(step.ID) != "" ||
		strings.TrimSpace(step.Title) != "" ||
		strings.TrimSpace(step.Instructions) != "" ||
		strings.TrimSpace(step.OutputFilename) != "" ||
		step.Enabled ||
		step.PreValidation != nil
}

func workflowFinalOutputConfigLooksConfigured(cfg *WorkflowFinalOutputConfig) bool {
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.Title) != "" ||
		strings.TrimSpace(cfg.Instructions) != "" ||
		strings.TrimSpace(cfg.OutputFilename) != "" ||
		cfg.Enabled
}

func ConvertOutputPlanToFinalOutputConfig(plan *WorkflowOutputPlan) *WorkflowFinalOutputConfig {
	if plan == nil {
		return nil
	}
	step := plan.PrimaryStep()
	if step == nil {
		step = plan.FirstStep()
	}
	if step == nil {
		return nil
	}
	return &WorkflowFinalOutputConfig{
		Enabled:        step.Enabled,
		Title:          step.Title,
		Instructions:   step.Instructions,
		OutputFilename: step.OutputFilename,
	}
}

type WorkflowFinalOutputAgent struct {
	*agents.BaseOrchestratorAgent
}

func NewWorkflowFinalOutputAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowFinalOutputAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.AgentType("workflow-final-output"),
		eventBridge,
	)
	return &WorkflowFinalOutputAgent{BaseOrchestratorAgent: baseAgent}
}

func (a *WorkflowFinalOutputAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	systemPrompt := strings.TrimSpace("You are a workflow output agent.\n\n" +
		"Your job is to produce a useful markdown report for a completed workflow group run.\n\n" +
		"Rules:\n" +
		"- Base everything strictly on the provided run artifacts and metadata.\n" +
		"- Do not invent actions, files, or outcomes that are not supported by the artifacts.\n" +
		"- Focus on what the workflow actually did, what it produced, what succeeded, what failed, and any important retries/branching.\n" +
		"- Output VALID markdown only.\n" +
		"- Do not wrap the whole answer in outer code fences.\n" +
		"- Start with a level-1 heading.\n" +
		"- Make the report easy for a human to review later without opening every artifact manually.\n" +
		"- When a compact numeric summary would help, you MAY include fenced `chart` blocks inside the markdown body.\n" +
		"- Supported `chart` block types are `bar` and `line` only.\n" +
		"- A `chart` block must contain valid JSON with this shape:\n" +
		"  ```chart\n" +
		"  {\n" +
		"    \"type\": \"bar\",\n" +
		"    \"title\": \"Step outcomes\",\n" +
		"    \"description\": \"Optional short caption\",\n" +
		"    \"xLabel\": \"Step\",\n" +
		"    \"yLabel\": \"Count\",\n" +
		"    \"data\": [\n" +
		"      { \"label\": \"Posted\", \"value\": 12 },\n" +
		"      { \"label\": \"Failed\", \"value\": 2 }\n" +
		"    ]\n" +
		"  }\n" +
		"  ```\n" +
		"- Use chart blocks only for simple comparisons or trend summaries grounded in the artifacts. For everything else, prefer normal markdown paragraphs, bullets, and tables.")

	inputProcessor := func(vars map[string]string) string {
		return fmt.Sprintf(`Workflow Title: %s
Configured Output Title: %s
Run Folder: %s
Output File Path: %s

User Instructions:
%s

Run Artifact Summary:
%s

Artifact Contents:
%s

Write the final markdown output now.`, vars["WorkflowTitle"], vars["ConfiguredTitle"], vars["RunFolder"], vars["OutputPath"], vars["Instructions"], vars["ArtifactSummary"], vars["ArtifactContents"])
	}

	return a.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

type finalOutputArtifact struct {
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	ReadError string `json:"read_error,omitempty"`
}

type finalOutputResponse struct {
	Success    bool                       `json:"success"`
	RunFolder  string                     `json:"run_folder"`
	OutputPath string                     `json:"output_path"`
	Content    string                     `json:"content,omitempty"`
	Exists     bool                       `json:"exists"`
	Config     *WorkflowFinalOutputConfig `json:"config,omitempty"`
}

func (hcpo *StepBasedWorkflowOrchestrator) readOutputPlan(ctx context.Context) (*WorkflowOutputPlan, error) {
	content, err := hcpo.ReadWorkspaceFile(ctx, DefaultOutputPlanPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "no such file") {
			content = ""
		} else {
			return nil, err
		}
	}

	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	return ParseWorkflowOutputPlan(content)
}

func (hcpo *StepBasedWorkflowOrchestrator) readFinalOutputConfig(ctx context.Context) (*WorkflowFinalOutputConfig, error) {
	plan, err := hcpo.readOutputPlan(ctx)
	if err != nil {
		return nil, err
	}
	return ConvertOutputPlanToFinalOutputConfig(plan), nil
}

func (hcpo *StepBasedWorkflowOrchestrator) GenerateFinalOutput(ctx context.Context, workflowTitle string, runFolder string) (*finalOutputResponse, error) {
	if runFolder == "" || !strings.Contains(runFolder, "/") {
		return nil, fmt.Errorf("final output generation requires a group-scoped run folder like iteration-X/group-name")
	}

	outputPlan, err := hcpo.readOutputPlan(ctx)
	if err != nil {
		return nil, err
	}
	outputStep := outputPlan.PrimaryStep()
	if outputStep == nil {
		return nil, fmt.Errorf("no enabled output step found in planning/output_plan.json")
	}

	config := ConvertOutputPlanToFinalOutputConfig(outputPlan)
	outputRelativePath := filepath.ToSlash(filepath.Join("runs", runFolder, outputStep.OutputFilename))
	runRelativePath := filepath.ToSlash(filepath.Join("runs", runFolder))
	_, artifactSummary, _, err := hcpo.collectFinalOutputArtifacts(ctx, runRelativePath, outputStep.OutputFilename)
	if err != nil {
		return nil, err
	}

	llmConfig, err := hcpo.selectFinalOutputLLM()
	if err != nil {
		return nil, err
	}
	internalRunFolder := filepath.ToSlash(filepath.Join(runFolder, internalOutputRunFolder))
	internalOutputRelativePath := filepath.ToSlash(filepath.Join("runs", internalRunFolder, "execution", internalOutputStepPath, outputStep.OutputFilename))
	targetRunAbsPath := filepath.ToSlash(filepath.Join(hcpo.GetWorkspacePath(), "runs", runFolder))
	originalSelectedRunFolder := hcpo.selectedRunFolder
	originalIterationFolder := hcpo.GetIterationFolder()
	originalWorkspaceTools := hcpo.WorkspaceTools
	originalWorkspaceToolExecutors := hcpo.WorkspaceToolExecutors
	originalTargetRunPath, hadTargetRunPath := hcpo.variableValues["TARGET_RUN_PATH"]
	if hcpo.variableValues == nil {
		hcpo.variableValues = make(map[string]string)
	}
	hcpo.variableValues["TARGET_RUN_PATH"] = targetRunAbsPath
	hcpo.selectedRunFolder = internalRunFolder
	hcpo.SetIterationFolder(internalRunFolder)
	defer func() {
		hcpo.selectedRunFolder = originalSelectedRunFolder
		hcpo.SetIterationFolder(originalIterationFolder)
		hcpo.WorkspaceTools = originalWorkspaceTools
		hcpo.WorkspaceToolExecutors = originalWorkspaceToolExecutors
		if hadTargetRunPath {
			hcpo.variableValues["TARGET_RUN_PATH"] = originalTargetRunPath
		} else {
			delete(hcpo.variableValues, "TARGET_RUN_PATH")
		}
	}()

	shellTools, shellExecutors := orchestrator.FilterCustomToolsByCategory(
		originalWorkspaceTools,
		originalWorkspaceToolExecutors,
		[]string{"workspace_advanced:execute_shell_command"},
	)
	hcpo.WorkspaceTools = shellTools
	// Keep the full executor map so the report pipeline can still use internal
	// workspace read/write/list helpers, while the LLM-visible tool definitions
	// remain shell-only.
	hcpo.WorkspaceToolExecutors = originalWorkspaceToolExecutors
	_ = shellExecutors

	reportStep := hcpo.buildFinalOutputExecutionStep(workflowTitle, runFolder, outputStep, artifactSummary, llmConfig)
	progress := &StepProgress{
		CompletedStepIndices:     []int{},
		TotalSteps:               1,
		LastUpdated:              time.Now(),
		BranchSteps:              make(map[int]BranchStepProgress),
		ValidationFailures:       make(map[string]int),
		DecisionEvaluationCounts: make(DecisionEvaluationCount),
	}
	execCtx := hcpo.buildExecutionContext()
	execCtx.SkipHumanInput = true
	execCtx.RunSingleStepOnly = true

	_, _, err = hcpo.executeSingleStep(
		ctx,
		reportStep,
		0,
		internalOutputStepPath,
		1,
		1,
		nil,
		progress,
		false,
		execCtx,
		[]PlanStepInterface{reportStep},
		false,
		nil,
		"",
		false,
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("final output generation failed: %w", err)
	}

	result, err := hcpo.ReadWorkspaceFile(ctx, internalOutputRelativePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated report markdown from %s: %w", internalOutputRelativePath, err)
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return nil, fmt.Errorf("final output step created an empty markdown file")
	}

	if err := hcpo.WriteWorkspaceFile(ctx, outputRelativePath, result); err != nil {
		return nil, fmt.Errorf("failed to publish final output markdown: %w", err)
	}

	return &finalOutputResponse{
		Success:    true,
		RunFolder:  runFolder,
		OutputPath: outputRelativePath,
		Content:    result,
		Exists:     true,
		Config:     config,
	}, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) buildFinalOutputExecutionStep(
	workflowTitle string,
	runFolder string,
	outputStep *WorkflowOutputPlanStep,
	artifactSummary string,
	llmConfig *orchestrator.LLMConfig,
) *RegularPlanStep {
	disableLearning := true
	disableTempLLM := true
	executionMaxTurns := 30
	useToolSearchMode := false
	selectedServers := []string{mcpclient.NoServers}
	stepID := outputStep.ID
	if stepID == "" {
		stepID = internalOutputStepID
	}

	description := strings.TrimSpace(fmt.Sprintf(`Generate the final workflow report for the completed run "%s".

Read the actual execution artifacts under runs/%s and use them as the only source of truth.

Workflow title: %s
Configured report title: %s

User instructions:
%s

Artifact inventory:
%s

Rules:
- Produce valid markdown only.
- Start with a level-1 heading.
- Do not invent actions, files, outcomes, retries, or failures that are not supported by the artifacts.
- Focus on what the workflow actually did, what it produced, what succeeded, what failed, and anything a human should review later.
- If a compact numeric summary helps, you may include fenced chart blocks with JSON using only supported types "bar" or "line".
- Read more files from the target run folder when needed before writing the report.
- Write the final markdown to the required output file.`, runFolder, runFolder, workflowTitle, outputStep.Title, outputStep.Instructions, artifactSummary))

	successCriteria := fmt.Sprintf("A grounded markdown report is written to %s and it passes pre-validation.", outputStep.OutputFilename)

	return &RegularPlanStep{
		Type: StepTypeRegular,
		CommonStepFields: CommonStepFields{
			ID:                  stepID,
			Title:               firstNonEmpty(outputStep.Title, "Final Report"),
			Description:         description,
			SuccessCriteria:     successCriteria,
			ContextDependencies: nil,
			ContextOutput:       FlexibleContextOutput(outputStep.OutputFilename),
			ValidationSchema:    ensureOutputFileValidation(outputStep.OutputFilename, outputStep.PreValidation),
		},
		HasLoop: false,
		AgentConfigs: &AgentConfigs{
			ExecutionLLM:         convertLLMConfigToAgentLLMConfig(llmConfig),
			ExecutionMaxTurns:    &executionMaxTurns,
			DisableLearning:      &disableLearning,
			DisableTempLLM:       &disableTempLLM,
			UseToolSearchMode:    &useToolSearchMode,
			SelectedServers:      selectedServers,
			EnabledCustomTools:   []string{"workspace_advanced:execute_shell_command"},
			SelectedTools:        []string{},
			PreDiscoveredTools:   []string{},
			DisableKnowledgebase: finalOutputBoolPtr(true),
		},
	}
}

func ensureOutputFileValidation(outputFilename string, schema *ValidationSchema) *ValidationSchema {
	if outputFilename == "" {
		outputFilename = DefaultFinalOutputFilename
	}
	if schema == nil {
		return &ValidationSchema{
			Files: []FileValidationRule{{
				FileName:  outputFilename,
				MustExist: true,
			}},
		}
	}

	clone := &ValidationSchema{
		Files: make([]FileValidationRule, len(schema.Files)),
	}
	copy(clone.Files, schema.Files)
	for i := range clone.Files {
		jsonChecks := make([]JSONValidationCheck, len(schema.Files[i].JSONChecks))
		copy(jsonChecks, schema.Files[i].JSONChecks)
		clone.Files[i].JSONChecks = jsonChecks
	}

	for i := range clone.Files {
		if strings.EqualFold(clone.Files[i].FileName, outputFilename) {
			clone.Files[i].MustExist = true
			return clone
		}
	}

	clone.Files = append(clone.Files, FileValidationRule{
		FileName:  outputFilename,
		MustExist: true,
	})
	return clone
}

func convertLLMConfigToAgentLLMConfig(llmConfig *orchestrator.LLMConfig) *AgentLLMConfig {
	if llmConfig == nil {
		return nil
	}
	result := &AgentLLMConfig{
		Provider: llmConfig.Primary.Provider,
		ModelID:  llmConfig.Primary.ModelID,
	}
	if len(llmConfig.Fallbacks) > 0 {
		result.Fallbacks = make([]AgentLLMFallback, 0, len(llmConfig.Fallbacks))
		for _, fallback := range llmConfig.Fallbacks {
			result.Fallbacks = append(result.Fallbacks, AgentLLMFallback{
				Provider: fallback.Provider,
				ModelID:  fallback.ModelID,
			})
		}
	}
	return result
}

func finalOutputBoolPtr(v bool) *bool {
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (hcpo *StepBasedWorkflowOrchestrator) MaybeRunAutoFinalOutput(ctx context.Context) error {
	outputPlan, err := hcpo.readOutputPlan(ctx)
	if err != nil {
		return fmt.Errorf("failed to check output plan: %w", err)
	}
	if outputPlan == nil || outputPlan.PrimaryStep() == nil {
		hcpo.GetLogger().Info("ℹ️ No enabled output step found - skipping auto-output")
		return nil
	}

	targetRunFolder := hcpo.selectedRunFolder
	if targetRunFolder == "" {
		hcpo.GetLogger().Warn("⚠️ No run folder set - skipping auto-output")
		return nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📝 Starting auto-output generation for run folder: %s", targetRunFolder))
	_, err = hcpo.GenerateFinalOutput(ctx, hcpo.GetObjective(), targetRunFolder)
	if err != nil {
		return fmt.Errorf("auto-output generation failed: %w", err)
	}
	return nil
}

// ExecuteFinalOutputOnly runs only the report/final-output generation phase for a
// completed group-scoped run folder.
func (hcpo *StepBasedWorkflowOrchestrator) ExecuteFinalOutputOnly(ctx context.Context, objective, workspacePath, targetRunFolder string) (string, error) {
	hcpo.GetLogger().Info("🚀 Starting final output execution")

	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	outputPlan, err := hcpo.readOutputPlan(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check report plan: %w", err)
	}
	if outputPlan == nil || outputPlan.PrimaryStep() == nil {
		return "", fmt.Errorf("no enabled output step found in %s", DefaultOutputPlanPath)
	}
	if targetRunFolder == "" {
		return "", fmt.Errorf("targetRunFolder is required for report execution")
	}
	if !strings.Contains(targetRunFolder, "/") {
		return "", fmt.Errorf("targetRunFolder must be group-scoped, e.g. iteration-2/manish")
	}

	response, err := hcpo.GenerateFinalOutput(ctx, objective, targetRunFolder)
	if err != nil {
		return "", err
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Final output execution completed successfully: %s", response.OutputPath))
	return fmt.Sprintf("Report generated successfully.\nrun_folder: %s\noutput_path: %s", response.RunFolder, response.OutputPath), nil
}

// selectFinalOutputLLM selects the LLM config for final output generation.
// Priority: tiered medium > presetPhaseLLM.
func (hcpo *StepBasedWorkflowOrchestrator) selectFinalOutputLLM() (*orchestrator.LLMConfig, error) {
	if hcpo.tierResolver != nil {
		llmConfig := hcpo.tierResolver.ResolveTier(TierMedium)
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ Using Tier 2 (Medium) for final output generation: %s/%s", llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			return llmConfig, nil
		}
	}

	orchestratorLLMConfig := hcpo.GetLLMConfig()

	if hcpo.presetPhaseLLM != nil && hcpo.presetPhaseLLM.Provider != "" && hcpo.presetPhaseLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for final output generation: %s/%s", hcpo.presetPhaseLLM.Provider, hcpo.presetPhaseLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetPhaseLLM.Provider,
				ModelID:  hcpo.presetPhaseLLM.ModelID,
			},
			Fallbacks: convertAgentFallbacks(hcpo.presetPhaseLLM.Fallbacks),
			APIKeys:   orchestratorLLMConfig.APIKeys,
		}, nil
	}

	return nil, fmt.Errorf("no valid LLM configuration found for final output generation: tiered medium and preset phase LLM are unavailable")
}

func (hcpo *StepBasedWorkflowOrchestrator) collectFinalOutputArtifacts(ctx context.Context, runRelativePath string, excludedFilename string) ([]finalOutputArtifact, string, string, error) {
	files, err := hcpo.ListWorkspaceFiles(ctx, runRelativePath)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to list run artifacts: %w", err)
	}

	filteredFiles := make([]string, 0, len(files))
	excludedFilename = strings.TrimSpace(strings.ToLower(excludedFilename))
	for _, file := range files {
		if excludedFilename != "" && strings.EqualFold(filepath.Base(file), excludedFilename) {
			continue
		}
		filteredFiles = append(filteredFiles, file)
	}

	sort.Strings(filteredFiles)
	if len(filteredFiles) > maxFinalOutputFiles {
		filteredFiles = filteredFiles[:maxFinalOutputFiles]
	}

	artifacts := make([]finalOutputArtifact, 0, len(filteredFiles))
	var summary strings.Builder
	var contents strings.Builder

	summary.WriteString(fmt.Sprintf("Total files included: %d\n", len(filteredFiles)))

	for _, file := range filteredFiles {
		relPath := filepath.ToSlash(filepath.Join(runRelativePath, file))
		artifact := finalOutputArtifact{Path: relPath}
		content, err := hcpo.ReadWorkspaceFile(ctx, relPath)
		if err != nil {
			artifact.ReadError = err.Error()
			artifacts = append(artifacts, artifact)
			summary.WriteString(fmt.Sprintf("- %s (unreadable: %s)\n", relPath, err.Error()))
			continue
		}

		if len(content) > maxFinalOutputFileChars {
			content = content[:maxFinalOutputFileChars] + "\n...[truncated]..."
			artifact.Truncated = true
		}
		artifact.Content = content
		artifacts = append(artifacts, artifact)
		summary.WriteString(fmt.Sprintf("- %s%s\n", relPath, map[bool]string{true: " [truncated]", false: ""}[artifact.Truncated]))
		contents.WriteString(fmt.Sprintf("\n## %s\n%s\n", relPath, content))
	}

	return artifacts, summary.String(), contents.String(), nil
}

func RegisterOutputModificationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	deleteFile func(context.Context, string, string) error,
) error {
	loadPlan := func(ctx context.Context) (*WorkflowOutputPlan, error) {
		planPath := DefaultOutputPlanPath
		if workspacePath != "" {
			planPath = filepath.ToSlash(filepath.Join(workspacePath, DefaultOutputPlanPath))
		}
		content, err := readFile(ctx, planPath)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "no such file") {
				return &WorkflowOutputPlan{}, nil
			}
			return nil, err
		}

		if strings.TrimSpace(content) == "" {
			return &WorkflowOutputPlan{}, nil
		}

		return ParseWorkflowOutputPlan(content)
	}

	if err := mcpAgent.RegisterCustomTool(
		"validate_report_plan",
		"Validate planning/output_plan.json after you edit it via shell or file tools. Checks that the file contains valid JSON, matches the single-step report-plan shape, and that any pre_validation schema is valid.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			plan, err := loadPlan(ctx)
			if err != nil {
				return "", err
			}

			if plan.Step != nil && len(plan.Steps) > 0 {
				return "", fmt.Errorf("output plan must use a single 'step' field only; legacy 'steps' array is not allowed")
			}

			step := plan.FirstStep()
			if step == nil {
				return "Report plan is valid JSON, but no report step is configured.", nil
			}

			if strings.TrimSpace(step.ID) == "" {
				return "", fmt.Errorf("output plan step.id is required")
			}
			if strings.TrimSpace(step.Instructions) == "" {
				return "", fmt.Errorf("output plan step.instructions is required")
			}
			step.Normalize()
			if step.PreValidation != nil {
				if err := validateRegexPatternsInSchema(step.PreValidation); err != nil {
					return "", fmt.Errorf("invalid regex patterns in output pre_validation schema: %w", err)
				}
				if err := validateJSONPathSyntax(step.PreValidation); err != nil {
					return "", fmt.Errorf("invalid JSONPath syntax in output pre_validation schema: %w", err)
				}
				if err := validateArrayLengthConsistencyChecks(step.PreValidation); err != nil {
					return "", fmt.Errorf("invalid array_length consistency checks in output pre_validation schema: %w", err)
				}
			}

			stepJSON, err := json.MarshalIndent(step, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal normalized report step: %w", err)
			}
			return fmt.Sprintf("Report plan is valid.\nEnabled: %t\nOutput filename: %s\nNormalized step:\n%s", step.Enabled, step.OutputFilename, string(stepJSON)), nil
		},
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register validate_report_plan tool: %w", err)
	}

	logger.Info("✅ Registered report plan validation tool")
	return nil
}
