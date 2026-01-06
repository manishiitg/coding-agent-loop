package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// EvaluationManager handles evaluation plan creation and management independently.
type EvaluationManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Phase LLM config
	presetPhaseLLM *AgentLLMConfig

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string
}

// NewEvaluationManager creates a new EvaluationManager
func NewEvaluationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetPhaseLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *EvaluationManager {
	return &EvaluationManager{
		BaseOrchestrator: baseOrchestrator,
		presetPhaseLLM:   presetPhaseLLM,
		sessionID:        sessionID,
		workflowID:       workflowID,
	}
}

// Methods for StepBasedWorkflowOrchestrator (for ExecuteEvaluationOnly)

func (hcpo *StepBasedWorkflowOrchestrator) checkExistingEvaluationPlan(ctx context.Context, planPath string) (bool, *EvaluationPlan, error) {
	content, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return false, nil, nil
		}
		return false, nil, err
	}

	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return false, nil, err
	}
	return true, &plan, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) loadEvaluationPlan(ctx context.Context) (*EvaluationPlan, error) {
	content, err := hcpo.ReadWorkspaceFile(ctx, "planning/evaluation_plan.json")
	if err != nil {
		return nil, err
	}
	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) saveEvaluationPlan(ctx context.Context, plan *EvaluationPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return hcpo.WriteWorkspaceFile(ctx, "planning/evaluation_plan.json", string(data))
}

// Methods for EvaluationManager

// CreateEvaluationPlanOnly runs only the evaluation planning phase
func (em *EvaluationManager) CreateEvaluationPlanOnly(ctx context.Context, objective, workspacePath string) (string, error) {
	em.GetLogger().Info(fmt.Sprintf("📋 Starting evaluation planning for objective: %s", objective))

	// Set objective and workspace path
	em.SetObjective(objective)
	em.SetWorkspacePath(workspacePath)

	// Check if evaluation_plan.json already exists
	evalPlanPath := "planning/evaluation_plan.json"
	planExists, existingPlan, err := em.checkExistingEvaluationPlan(ctx, evalPlanPath)
	if err != nil {
		em.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check for existing evaluation plan: %v", err))
		planExists = false
	}

	var approvedPlan *EvaluationPlan
	var humanFeedback string
	var evaluationConversationHistory []llmtypes.MessageContent

	maxRevisions := 20
	for revisionAttempt := 1; revisionAttempt <= maxRevisions; revisionAttempt++ {
		em.GetLogger().Info(fmt.Sprintf("🔄 Evaluation plan creation/approval attempt %d/%d", revisionAttempt, maxRevisions))

		var planToUpdate *EvaluationPlan
		if revisionAttempt == 1 && planExists {
			planToUpdate = existingPlan
		} else if approvedPlan != nil {
			planToUpdate = approvedPlan
		}

		var err error
		approvedPlan, evaluationConversationHistory, err = em.runEvaluationPlanningPhase(ctx, revisionAttempt, humanFeedback, evaluationConversationHistory, planToUpdate)
		if err != nil {
			return "", fmt.Errorf("evaluation planning phase failed: %w", err)
		}

		// Request human approval for Evaluation Plan
		approvedInternal, feedbackInternal, err := em.requestEvaluationPlanApproval(ctx, revisionAttempt)
		if err != nil {
			return "", fmt.Errorf("evaluation plan approval request failed: %w", err)
		}

		if approvedInternal {
			em.GetLogger().Info(fmt.Sprintf("✅ Evaluation plan approved by human"))
			break
		}

		em.GetLogger().Info(fmt.Sprintf("🔄 Evaluation plan revision requested: %s", feedbackInternal))
		humanFeedback = feedbackInternal
	}

	if approvedPlan != nil {
		var summary strings.Builder
		summary.WriteString("Evaluation planning completed successfully.\n\n")
		summary.WriteString(fmt.Sprintf("Created evaluation plan with %d steps:\n", len(approvedPlan.Steps)))
		for i, step := range approvedPlan.Steps {
			summary.WriteString(fmt.Sprintf("%d. %s\n", i+1, step.Description))
		}
		return summary.String(), nil
	}

	return "Evaluation planning completed (no plan created).", nil
}

func (em *EvaluationManager) checkExistingEvaluationPlan(ctx context.Context, planPath string) (bool, *EvaluationPlan, error) {
	content, err := em.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return false, nil, nil
		}
		return false, nil, err
	}

	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return false, nil, err
	}
	return true, &plan, nil
}

func (em *EvaluationManager) loadEvaluationPlan(ctx context.Context) (*EvaluationPlan, error) {
	content, err := em.ReadWorkspaceFile(ctx, "planning/evaluation_plan.json")
	if err != nil {
		return nil, err
	}
	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (em *EvaluationManager) saveEvaluationPlan(ctx context.Context, plan *EvaluationPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return em.WriteWorkspaceFile(ctx, "planning/evaluation_plan.json", string(data))
}

func (em *EvaluationManager) requestEvaluationPlanApproval(ctx context.Context, revisionAttempt int) (bool, string, error) {
	requestID := fmt.Sprintf("evaluation_plan_approval_%d_%d", time.Now().UnixNano(), revisionAttempt)
	return em.RequestHumanFeedback(
		ctx,
		requestID,
		"Please review the evaluation plan and provide approval or feedback",
		"",
		em.sessionID,
		em.workflowID,
	)
}

func (em *EvaluationManager) runEvaluationPlanningPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent, existingPlan *EvaluationPlan) (*EvaluationPlan, []llmtypes.MessageContent, error) {
	templateVars := map[string]string{
		"WorkspacePath": em.GetWorkspacePath(),
	}

	// Read the execution plan to provide context for evaluation
	executionPlanContent, err := em.ReadWorkspaceFile(ctx, "planning/plan.json")
	if err != nil {
		em.GetLogger().Warn(fmt.Sprintf("⚠️ Could not read execution plan for evaluation designer: %v", err))
		templateVars["ExecutionPlanJSON"] = "No execution plan found."
	} else {
		templateVars["ExecutionPlanJSON"] = executionPlanContent
	}

	if existingPlan != nil {
		existingJSON, _ := json.MarshalIndent(existingPlan, "", "  ")
		templateVars["ExistingEvaluationPlanJSON"] = string(existingJSON)
	}

	var userMessage string
	if humanFeedback != "" {
		userMessage = humanFeedback
	} else if existingPlan != nil {
		userMessage = "An existing evaluation plan has been loaded. Please use human_feedback tool to ask the user what improvements they would like to make to the existing plan."
	} else {
		userMessage = "Analyze the execution plan (plan.json), infer the overall goal, and propose a holistic evaluation plan. Focus on quality and correctness. Always use human_feedback tool first to confirm the strategy with me."
	}

	// Create evaluation agent
	agent, err := em.createEvaluationAgent(ctx, "evaluation-planning", 0, iteration)
	if err != nil {
		return nil, nil, err
	}

	// Execute evaluation agent
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Cast to the concrete type to set the user message processor
	evaluationAgent, ok := agent.(*HumanControlledEvaluationAgent)
	if !ok {
		return nil, nil, fmt.Errorf("failed to cast agent to HumanControlledEvaluationAgent")
	}
	evaluationAgent.SetUserMessageProcessor(inputProcessor)

	_, updatedHistory, err := evaluationAgent.Execute(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, updatedHistory, err
	}

	// Read the plan back
	_, plan, err := em.checkExistingEvaluationPlan(ctx, "planning/evaluation_plan.json")
	if err != nil {
		return nil, updatedHistory, err
	}

	return plan, updatedHistory, nil
}

func (em *EvaluationManager) createEvaluationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	baseWorkspacePath := em.GetWorkspacePath()
	planningPath := fmt.Sprintf("%s/planning", baseWorkspacePath)

	// Evaluation designer agent: read/write access to planning/ folder only
	// Can read planning/plan.json and write planning/evaluation_plan.json
	// NO access to runs/ or evaluation/ folders - evaluation designer only analyzes the plan
	em.SetWorkspacePathForFolderGuard([]string{planningPath}, []string{planningPath})

	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := em.GetLLMConfig()
	if em.presetPhaseLLM != nil {
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: em.presetPhaseLLM.Provider,
				ModelID:  em.presetPhaseLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys,
		}
	} else {
		llmConfigToUse = orchestratorLLMConfig
	}

	agentConfig := em.CreateStandardAgentConfigWithLLM("evaluation-planning-agent", 100, agents.OutputFormatStructured, llmConfigToUse)
	agentConfig.ServerNames = []string{mcpclient.NoServers}
	agentConfig.UseCodeExecutionMode = false

	// Register WorkspaceTools (including human_feedback)
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}
	if em.BaseOrchestrator != nil {
		if em.WorkspaceTools != nil && em.WorkspaceToolExecutors != nil {
			toolsToRegister, executorsToUse = em.BaseOrchestrator.PrepareWorkspaceToolsWithFolderGuard(
				em.WorkspaceTools,
				em.WorkspaceToolExecutors,
			)
		}
	}

	agent, err := em.CreateAndSetupStandardAgentWithConfig(
		ctx,
		agentConfig,
		phase,
		step,
		iteration,
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledEvaluationAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister,
		executorsToUse,
		false,
	)
	if err != nil {
		return nil, err
	}

	// Register tools
	evaluationAgent, ok := agent.(*HumanControlledEvaluationAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast agent to HumanControlledEvaluationAgent")
	}
	mcpAgent := evaluationAgent.BaseOrchestratorAgent.BaseAgent().Agent()
	em.registerEvaluationTools(mcpAgent)

	return agent, nil
}

func (em *EvaluationManager) registerEvaluationTools(mcpAgent *mcpagent.Agent) {
	// add_evaluation_step
	mcpAgent.RegisterCustomTool(
		"add_evaluation_step",
		"Add a new evaluation step to the plan",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":               map[string]interface{}{"type": "string", "description": "Unique ID for the step"},
				"title":            map[string]interface{}{"type": "string", "description": "Short title"},
				"description":      map[string]interface{}{"type": "string", "description": "Detailed description of what to evaluate"},
				"pre_validation":   map[string]interface{}{"type": "object", "description": "Optional validation schema"},
				"success_criteria": map[string]interface{}{"type": "string", "description": "Score-based success criteria (0-10)"},
			},
			"required": []string{"id", "title", "description", "success_criteria"},
		},
		em.createAddEvaluationStepTool(),
		"workspace",
	)

	// update_evaluation_step
	mcpAgent.RegisterCustomTool(
		"update_evaluation_step",
		"Update an existing evaluation step",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":               map[string]interface{}{"type": "string", "description": "ID of the step to update"},
				"title":            map[string]interface{}{"type": "string"},
				"description":      map[string]interface{}{"type": "string"},
				"pre_validation":   map[string]interface{}{"type": "object"},
				"success_criteria": map[string]interface{}{"type": "string"},
			},
			"required": []string{"id"},
		},
		em.createUpdateEvaluationStepTool(),
		"workspace",
	)

	// delete_evaluation_step
	mcpAgent.RegisterCustomTool(
		"delete_evaluation_step",
		"Delete one or more evaluation steps",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ids": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"ids"},
		},
		em.createDeleteEvaluationStepTool(),
		"workspace",
	)
}

func (em *EvaluationManager) createAddEvaluationStepTool() func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		if args == nil {
			return "", fmt.Errorf("arguments cannot be nil")
		}

		plan, err := em.loadEvaluationPlan(ctx)
		if err != nil {
			plan = &EvaluationPlan{Steps: []*EvaluationStep{}}
		}

		id, ok := args["id"].(string)
		if !ok || id == "" {
			return "", fmt.Errorf("missing or invalid argument: id (string required)")
		}

		title, ok := args["title"].(string)
		if !ok || title == "" {
			return "", fmt.Errorf("missing or invalid argument: title (string required)")
		}

		description, ok := args["description"].(string)
		if !ok || description == "" {
			return "", fmt.Errorf("missing or invalid argument: description (string required)")
		}

		successCriteria, ok := args["success_criteria"].(string)
		if !ok || successCriteria == "" {
			return "", fmt.Errorf("missing or invalid argument: success_criteria (string required)")
		}

		step := &EvaluationStep{
			ID:              id,
			Title:           title,
			Description:     description,
			SuccessCriteria: successCriteria,
		}

		if val, ok := args["pre_validation"]; ok && val != nil {
			pv, ok := val.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("invalid argument: pre_validation must be an object")
			}
			pvJSON, err := json.Marshal(pv)
			if err != nil {
				return "", fmt.Errorf("failed to marshal pre_validation: %w", err)
			}
			var schema ValidationSchema
			if err := json.Unmarshal(pvJSON, &schema); err != nil {
				return "", fmt.Errorf("failed to unmarshal pre_validation into ValidationSchema: %w", err)
			}
			step.PreValidation = &schema
		}

		plan.Steps = append(plan.Steps, step)
		if err := em.saveEvaluationPlan(ctx, plan); err != nil {
			return "", err
		}
		return fmt.Sprintf("Added evaluation step: %s", step.ID), nil
	}
}

func (em *EvaluationManager) createUpdateEvaluationStepTool() func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		if args == nil {
			return "", fmt.Errorf("arguments cannot be nil")
		}

		plan, err := em.loadEvaluationPlan(ctx)
		if err != nil {
			return "", err
		}

		id, ok := args["id"].(string)
		if !ok || id == "" {
			return "", fmt.Errorf("missing or invalid argument: id (string required)")
		}

		found := false
		for i, s := range plan.Steps {
			if s.ID == id {
				if v, ok := args["title"].(string); ok {
					plan.Steps[i].Title = v
				}
				if v, ok := args["description"].(string); ok {
					plan.Steps[i].Description = v
				}
				if v, ok := args["success_criteria"].(string); ok {
					plan.Steps[i].SuccessCriteria = v
				}
				if val, ok := args["pre_validation"]; ok && val != nil {
					pv, ok := val.(map[string]interface{})
					if !ok {
						return "", fmt.Errorf("invalid argument: pre_validation must be an object")
					}
					pvJSON, err := json.Marshal(pv)
					if err != nil {
						return "", fmt.Errorf("failed to marshal pre_validation: %w", err)
					}
					var schema ValidationSchema
					if err := json.Unmarshal(pvJSON, &schema); err != nil {
						return "", fmt.Errorf("failed to unmarshal pre_validation into ValidationSchema: %w", err)
					}
					plan.Steps[i].PreValidation = &schema
				}
				found = true
				break
			}
		}

		if !found {
			return "", fmt.Errorf("step %s not found", id)
		}

		if err := em.saveEvaluationPlan(ctx, plan); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated evaluation step: %s", id), nil
	}
}

func (em *EvaluationManager) createDeleteEvaluationStepTool() func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		if args == nil {
			return "", fmt.Errorf("arguments cannot be nil")
		}

		plan, err := em.loadEvaluationPlan(ctx)
		if err != nil {
			return "", err
		}

		idsRaw, ok := args["ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf("missing or invalid argument: ids (array of strings required)")
		}

		idsToDelete := make(map[string]bool)
		for _, idRaw := range idsRaw {
			id, ok := idRaw.(string)
			if !ok {
				return "", fmt.Errorf("invalid argument in ids: element is not a string")
			}
			idsToDelete[id] = true
		}

		newSteps := []*EvaluationStep{}
		for _, s := range plan.Steps {
			if !idsToDelete[s.ID] {
				newSteps = append(newSteps, s)
			}
		}

		plan.Steps = newSteps
		if err := em.saveEvaluationPlan(ctx, plan); err != nil {
			return "", err
		}
		return "Deleted evaluation steps", nil
	}
}
