package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	DefaultFinalOutputFilename = "final_output.md"
	DefaultOutputPlanPath      = "planning/output_plan.json"
	LegacyOutputPlanPath       = "output/output_plan.json"
	defaultOutputStepID        = "final-output"
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
	systemPrompt := `You are a workflow output agent.

Your job is to produce a useful markdown report for a completed workflow group run.

Rules:
- Base everything strictly on the provided run artifacts and metadata.
- Do not invent actions, files, or outcomes that are not supported by the artifacts.
- Focus on what the workflow actually did, what it produced, what succeeded, what failed, and any important retries/branching.
- Output VALID markdown only.
- Do not wrap the answer in code fences.
- Start with a level-1 heading.
- Make the report easy for a human to review later without opening every artifact manually.`

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
	planPaths := []string{DefaultOutputPlanPath, LegacyOutputPlanPath}
	var content string
	var err error
	for _, path := range planPaths {
		content, err = hcpo.ReadWorkspaceFile(ctx, path)
		if err == nil {
			break
		}
		if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "no such file") {
			content = ""
			err = nil
			continue
		}
		return nil, err
	}

	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	var plan WorkflowOutputPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse output_plan.json: %w", err)
	}
	plan.Normalize()
	return &plan, nil
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
	readPaths := []string{filepath.ToSlash(filepath.Join(hcpo.GetWorkspacePath(), "runs", runFolder))}
	writePaths := []string{filepath.ToSlash(filepath.Join(hcpo.GetWorkspacePath(), "runs", runFolder))}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	_, artifactSummary, artifactContents, err := hcpo.collectFinalOutputArtifacts(ctx, runRelativePath, outputStep.OutputFilename)
	if err != nil {
		return nil, err
	}

	llmConfig, err := hcpo.selectFinalOutputLLM()
	if err != nil {
		return nil, err
	}

	configForAgent := hcpo.CreateStandardAgentConfigWithLLM("workflow-final-output-agent", 30, agents.OutputFormatStructured, llmConfig)
	configForAgent.ServerNames = []string{}
	configForAgent.SelectedTools = []string{"workspace_advanced:execute_shell_command"}
	configForAgent.UseCodeExecutionMode = false
	configForAgent.UseToolSearchMode = false

	// Final output generation is intentionally not a normal workflow step:
	// no validation phase, no learning phase, and only the shell workspace tool
	// is exposed so the agent can inspect artifacts when needed.
	shellTools, shellExecutors := orchestrator.FilterCustomToolsByCategory(
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
		[]string{"workspace_advanced:execute_shell_command"},
	)
	shellTools, shellExecutors = hcpo.PrepareWorkspaceToolsWithFolderGuard(shellTools, shellExecutors)

	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		configForAgent,
		"final-output",
		0, 0, outputStep.ID,
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowFinalOutputAgent(cfg, logger, tracer, eventBridge)
		},
		shellTools, shellExecutors, true,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create final output agent: %w", err)
	}

	finalOutputAgent, ok := agent.(*WorkflowFinalOutputAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast final output agent")
	}

	result, _, err := finalOutputAgent.Execute(ctx, map[string]string{
		"WorkflowTitle":    workflowTitle,
		"ConfiguredTitle":  outputStep.Title,
		"RunFolder":        runFolder,
		"OutputPath":       outputRelativePath,
		"Instructions":     outputStep.Instructions,
		"ArtifactSummary":  artifactSummary,
		"ArtifactContents": artifactContents,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("final output generation failed: %w", err)
	}

	result = strings.TrimSpace(result)
	if result == "" {
		return nil, fmt.Errorf("final output agent returned empty markdown")
	}

	if err := hcpo.WriteWorkspaceFile(ctx, outputRelativePath, result); err != nil {
		return nil, fmt.Errorf("failed to write final output markdown: %w", err)
	}

	preValidationResults, preValidationErr := RunPreValidation(ctx, outputStep.PreValidation, runRelativePath, hcpo.BaseOrchestrator)
	if preValidationErr != nil {
		return nil, fmt.Errorf("final output pre-validation failed to run: %w", preValidationErr)
	}
	if outputStep.PreValidation != nil && len(outputStep.PreValidation.Files) > 0 && !preValidationResults.OverallPass {
		return nil, fmt.Errorf("final output pre-validation failed:\n%s", formatWorkspaceResults(preValidationResults))
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
		planPaths := []string{DefaultOutputPlanPath, LegacyOutputPlanPath}
		for _, relativePath := range planPaths {
			planPath := relativePath
			if workspacePath != "" {
				planPath = filepath.ToSlash(filepath.Join(workspacePath, relativePath))
			}
			content, err := readFile(ctx, planPath)
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "no such file") {
					continue
				}
				return nil, err
			}

			if strings.TrimSpace(content) == "" {
				return &WorkflowOutputPlan{}, nil
			}

			var plan WorkflowOutputPlan
			if err := json.Unmarshal([]byte(content), &plan); err != nil {
				return nil, fmt.Errorf("failed to parse output plan: %w", err)
			}
			plan.Normalize()
			return &plan, nil
		}
		return &WorkflowOutputPlan{}, nil
	}

	savePlan := func(ctx context.Context, plan *WorkflowOutputPlan) error {
		if plan == nil {
			plan = &WorkflowOutputPlan{}
		}
		plan.Normalize()
		planPath := DefaultOutputPlanPath
		if workspacePath != "" {
			planPath = filepath.ToSlash(filepath.Join(workspacePath, DefaultOutputPlanPath))
		}
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output plan: %w", err)
		}
		if err := writeFile(ctx, planPath, string(data)); err != nil {
			return err
		}
		if workspacePath != "" {
			legacyPath := filepath.ToSlash(filepath.Join(workspacePath, LegacyOutputPlanPath))
			_ = deleteFile(ctx, legacyPath, "")
		}
		return nil
	}

	parseIDs := func(raw interface{}) ([]string, error) {
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				return nil, nil
			}
			trimmed := strings.TrimSpace(v)
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				var ids []string
				if err := json.Unmarshal([]byte(strings.ReplaceAll(trimmed, "'", "\"")), &ids); err == nil {
					return ids, nil
				}
			}
			return []string{v}, nil
		case []string:
			return v, nil
		case []interface{}:
			ids := make([]string, 0, len(v))
			for _, item := range v {
				id, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("ids must contain only strings")
				}
				ids = append(ids, id)
			}
			return ids, nil
		default:
			return nil, fmt.Errorf("ids must be a string or array of strings")
		}
	}

	parsePreValidation := func(raw interface{}) (*ValidationSchema, error) {
		if raw == nil {
			return nil, nil
		}
		pv, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid argument: pre_validation must be an object")
		}
		pvJSON, err := json.Marshal(pv)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal pre_validation: %w", err)
		}
		var schema ValidationSchema
		if err := json.Unmarshal(pvJSON, &schema); err != nil {
			return nil, fmt.Errorf("failed to unmarshal pre_validation into ValidationSchema: %w", err)
		}
		if err := validateRegexPatternsInSchema(&schema); err != nil {
			return nil, fmt.Errorf("invalid regex patterns in pre_validation schema: %w", err)
		}
		if err := validateJSONPathSyntax(&schema); err != nil {
			return nil, fmt.Errorf("invalid JSONPath syntax in pre_validation schema: %w", err)
		}
		if err := validateArrayLengthConsistencyChecks(&schema); err != nil {
			return nil, fmt.Errorf("invalid array_length consistency checks in pre_validation schema: %w", err)
		}
		return &schema, nil
	}

	if err := mcpAgent.RegisterCustomTool(
		"get_output_plan",
		"Read the current workflow report plan from planning/output_plan.json.",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			plan, err := loadPlan(ctx)
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register get_output_plan tool: %w", err)
	}

	if err := mcpAgent.RegisterCustomTool(
		"add_output_step",
		"Add a workflow report step to planning/output_plan.json. This defines what final markdown artifact should be generated automatically after a workflow group run completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":              map[string]interface{}{"type": "string", "description": "Unique ID for the output step"},
				"title":           map[string]interface{}{"type": "string", "description": "Short title for the output"},
				"instructions":    map[string]interface{}{"type": "string", "description": "Detailed instructions describing what the final output should show"},
				"pre_validation":  map[string]interface{}{"type": "object", "description": "Optional pre-validation schema, same shape used by workflow and evaluation steps"},
				"output_filename": map[string]interface{}{"type": "string", "description": "Markdown filename to write under the group run folder"},
				"enabled":         map[string]interface{}{"type": "boolean", "description": "Whether this output step should run automatically after workflow completion"},
			},
			"required": []string{"id", "title", "instructions"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			plan, err := loadPlan(ctx)
			if err != nil {
				return "", err
			}

			id, _ := args["id"].(string)
			title, _ := args["title"].(string)
			instructions, _ := args["instructions"].(string)
			if strings.TrimSpace(id) == "" || strings.TrimSpace(title) == "" || strings.TrimSpace(instructions) == "" {
				return "", fmt.Errorf("id, title, and instructions are required")
			}
			if existing := plan.FirstStep(); existing != nil {
				return "", fmt.Errorf("output plan already has a single step (%q). Use update_output_step instead", existing.ID)
			}

			step := &WorkflowOutputPlanStep{
				ID:             id,
				Title:          title,
				Instructions:   instructions,
				OutputFilename: DefaultFinalOutputFilename,
				Enabled:        true,
			}
			if v, ok := args["output_filename"].(string); ok && strings.TrimSpace(v) != "" {
				step.OutputFilename = v
			}
			if val, ok := args["pre_validation"]; ok && val != nil {
				schema, err := parsePreValidation(val)
				if err != nil {
					return "", err
				}
				step.PreValidation = schema
			}
			if v, ok := args["enabled"].(bool); ok {
				step.Enabled = v
			}
			step.Normalize()

			plan.Step = step
			if err := savePlan(ctx, plan); err != nil {
				return "", err
			}
			return fmt.Sprintf("Added report step %q. Report plan now has a single configured step.", step.ID), nil
		},
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_output_step tool: %w", err)
	}

	if err := mcpAgent.RegisterCustomTool(
		"update_output_step",
		"Update an existing workflow report step in planning/output_plan.json.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":                   map[string]interface{}{"type": "string", "description": "ID of the output step to update"},
				"title":                map[string]interface{}{"type": "string"},
				"instructions":         map[string]interface{}{"type": "string"},
				"pre_validation":       map[string]interface{}{"type": "object", "description": "Optional pre-validation schema, same shape used by workflow and evaluation steps"},
				"clear_pre_validation": map[string]interface{}{"type": "boolean", "description": "If true, remove any existing pre-validation schema from the output step"},
				"output_filename":      map[string]interface{}{"type": "string"},
				"enabled":              map[string]interface{}{"type": "boolean"},
			},
			"required": []string{"id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			plan, err := loadPlan(ctx)
			if err != nil {
				return "", err
			}

			id, _ := args["id"].(string)
			if strings.TrimSpace(id) == "" {
				return "", fmt.Errorf("id is required")
			}

			step := plan.FirstStep()
			if step == nil {
				return "", fmt.Errorf("no output step exists yet. Use add_output_step first")
			}
			if step.ID != id {
				return "", fmt.Errorf("output plan has a single step %q, not %q", step.ID, id)
			}

			if v, ok := args["title"].(string); ok {
				step.Title = v
			}
			if v, ok := args["instructions"].(string); ok {
				step.Instructions = v
			}
			if clearSchema, ok := args["clear_pre_validation"].(bool); ok && clearSchema {
				step.PreValidation = nil
			}
			if val, ok := args["pre_validation"]; ok && val != nil {
				schema, err := parsePreValidation(val)
				if err != nil {
					return "", err
				}
				step.PreValidation = schema
			}
			if v, ok := args["output_filename"].(string); ok && strings.TrimSpace(v) != "" {
				step.OutputFilename = v
			}
			if v, ok := args["enabled"].(bool); ok {
				step.Enabled = v
			}
			step.Normalize()
			plan.Step = step
			if err := savePlan(ctx, plan); err != nil {
				return "", err
			}
			return fmt.Sprintf("Updated output step %q.", id), nil
		},
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_output_step tool: %w", err)
	}

	if err := mcpAgent.RegisterCustomTool(
		"delete_output_steps",
		"Delete one or more workflow report steps from planning/output_plan.json.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ids": map[string]interface{}{
					"type":        "array",
					"description": "IDs of the output steps to delete",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"ids"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			plan, err := loadPlan(ctx)
			if err != nil {
				return "", err
			}

			ids, err := parseIDs(args["ids"])
			if err != nil {
				return "", err
			}
			if len(ids) == 0 {
				return "", fmt.Errorf("at least one id is required")
			}

			step := plan.FirstStep()
			if step == nil {
				return "", fmt.Errorf("no output step exists")
			}
			shouldDelete := false
			for _, id := range ids {
				if id == step.ID {
					shouldDelete = true
					break
				}
			}
			if !shouldDelete {
				return "", fmt.Errorf("output plan has a single step %q, which was not included in ids", step.ID)
			}

			plan.Step = nil

			if err := savePlan(ctx, plan); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted output step %q.", step.ID), nil
		},
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_output_steps tool: %w", err)
	}

	logger.Info("✅ Registered output modification tools")
	return nil
}
