package step_based_workflow

import (
	"context"
	"fmt"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	WorkshopStepMessageSentToCLI     = "sent_to_cli"
	WorkshopStepMessageQueued        = "queued_for_injection"
	WorkshopStepMessageNotRunning    = "not_running"
	WorkshopStepMessageNoActiveAgent = "no_active_agent"
	WorkshopStepMessageUnsupported   = "unsupported"
)

type WorkshopStepMessageResult struct {
	ExecutionID     string `json:"execution_id"`
	StepID          string `json:"step_id,omitempty"`
	ExecutionStatus string `json:"execution_status,omitempty"`
	DeliveryStatus  string `json:"delivery_status"`
	Provider        string `json:"provider,omitempty"`
	Phase           string `json:"phase,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

type workshopStepMessageTarget struct {
	provider string
	phase    string
	deliver  func(context.Context, string) (string, error)
}

func (r *WorkshopStepRegistry) bindMessageTarget(executionID string, target workshopStepMessageTarget) (uint64, error) {
	r.mu.RLock()
	exec := r.executions[executionID]
	r.mu.RUnlock()
	if exec == nil {
		return 0, ErrWorkshopExecutionNotFound
	}

	exec.messageSendMu.Lock()
	defer exec.messageSendMu.Unlock()
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.Status != WorkshopStepRunning {
		return 0, fmt.Errorf("execution %q is not running", executionID)
	}
	exec.messageTargetGeneration++
	token := exec.messageTargetGeneration
	exec.messageTarget = &target
	return token, nil
}

func (r *WorkshopStepRegistry) clearMessageTarget(executionID string, token uint64) {
	r.mu.RLock()
	exec := r.executions[executionID]
	r.mu.RUnlock()
	if exec == nil {
		return
	}

	exec.messageSendMu.Lock()
	defer exec.messageSendMu.Unlock()
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.messageTargetGeneration == token {
		exec.messageTarget = nil
	}
}

func (r *WorkshopStepRegistry) SendMessage(ctx context.Context, executionID, message string) WorkshopStepMessageResult {
	executionID = strings.TrimSpace(executionID)
	message = strings.TrimSpace(message)
	result := WorkshopStepMessageResult{ExecutionID: executionID}
	if executionID == "" {
		result.DeliveryStatus = WorkshopStepMessageUnsupported
		result.Detail = "execution_id is required"
		return result
	}
	if message == "" {
		result.DeliveryStatus = WorkshopStepMessageUnsupported
		result.Detail = "message is required"
		return result
	}

	r.mu.RLock()
	exec := r.executions[executionID]
	r.mu.RUnlock()
	if exec == nil {
		result.DeliveryStatus = WorkshopStepMessageNotRunning
		result.Detail = "execution not found"
		return result
	}

	// Keep delivery serialized for this exact execution. Binding, phase changes,
	// completion, and cancellation use the same lock so a message cannot cross a
	// lifecycle boundary and land on the wrong child agent.
	exec.messageSendMu.Lock()
	defer exec.messageSendMu.Unlock()

	exec.mu.RLock()
	result.StepID = exec.StepID
	result.ExecutionStatus = string(exec.Status)
	target := exec.messageTarget
	if target != nil {
		result.Provider = target.provider
		result.Phase = target.phase
	}
	exec.mu.RUnlock()

	if result.ExecutionStatus != string(WorkshopStepRunning) {
		result.DeliveryStatus = WorkshopStepMessageNotRunning
		result.Detail = "execution is no longer running"
		return result
	}
	if target == nil {
		result.DeliveryStatus = WorkshopStepMessageNoActiveAgent
		result.Detail = "execution is between agent turns or is running a script/validation phase"
		return result
	}
	if target.deliver == nil {
		result.DeliveryStatus = WorkshopStepMessageUnsupported
		result.Detail = "active agent does not expose message delivery"
		return result
	}

	status, err := target.deliver(ctx, message)
	if err != nil {
		result.DeliveryStatus = WorkshopStepMessageUnsupported
		result.Detail = err.Error()
		return result
	}
	result.DeliveryStatus = status
	if status == "" {
		result.DeliveryStatus = WorkshopStepMessageUnsupported
		result.Detail = "agent returned an empty delivery status"
	}
	return result
}

func (hcpo *StepBasedWorkflowOrchestrator) withWorkshopMessageTarget(
	ctx context.Context,
	stepID string,
	phase string,
	agent agents.OrchestratorAgent,
	run func() (string, []llmtypes.MessageContent, error),
) (string, []llmtypes.MessageContent, error) {
	if run == nil {
		return "", nil, fmt.Errorf("agent execution callback is required")
	}
	executionID := currentWorkshopParentExecutionID(ctx)
	if executionID == "" || hcpo.workshopStepRegistry == nil || agent == nil {
		return run()
	}
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil || agent.GetConfig() == nil {
		return run()
	}
	underlyingAgent := baseAgent.Agent()
	sessionID := strings.TrimSpace(agent.GetConfig().MCPSessionID)
	provider := strings.TrimSpace(agent.GetConfig().LLMConfig.Primary.Provider)
	target := workshopStepMessageTarget{
		provider: provider,
		phase:    strings.TrimSpace(phase),
		deliver: func(deliveryCtx context.Context, message string) (string, error) {
			delivery, err := underlyingAgent.DeliverUserMessage(deliveryCtx, mcpagent.UserMessageDeliveryRequest{
				SessionID: sessionID,
				Message:   message,
				Intent:    mcpagent.UserMessageDeliveryIntentLiveInput,
			})
			return string(delivery.DeliveryStatus), err
		},
	}
	token, err := hcpo.workshopStepRegistry.bindMessageTarget(executionID, target)
	if err != nil {
		return run()
	}
	defer hcpo.workshopStepRegistry.clearMessageTarget(executionID, token)
	return run()
}
