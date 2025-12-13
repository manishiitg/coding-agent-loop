package orchestrator

import (
	"context"
	"fmt"
	"time"

	"mcpagent/events"
)

// emitEvent emits an event through the event bridge
func (bo *BaseOrchestrator) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit event %s: %v", eventType, err))
	}
}

// EmitOrchestratorStart emits an orchestrator start event
func (bo *BaseOrchestrator) EmitOrchestratorStart(ctx context.Context, objective string, agentsCount int, executionMode string) {
	// Removed verbose logging

	eventData := &events.OrchestratorStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:        objective,
		AgentsCount:      agentsCount,
		ServersCount:     len(bo.selectedServers),
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorStart, eventData)
}

// EmitOrchestratorEnd emits an orchestrator end event
func (bo *BaseOrchestrator) EmitOrchestratorEnd(ctx context.Context, objective, result, status, message string, executionMode string) {
	// Removed verbose logging

	duration := time.Since(bo.startTime)
	eventData := &events.OrchestratorEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:        objective,
		Result:           result,
		Status:           status,
		Duration:         duration,
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorEnd, eventData)
}

// EmitUnifiedCompletionEvent emits a unified completion event
func (bo *BaseOrchestrator) EmitUnifiedCompletionEvent(ctx context.Context, agentType, agentMode, question, finalResult, status string, turns int) {
	// Removed verbose logging

	duration := time.Since(bo.startTime)
	completionEventData := events.NewUnifiedCompletionEvent(
		agentType,
		agentMode,
		question,
		finalResult,
		status,
		duration,
		turns,
	)

	agentEvent := events.NewAgentEvent(completionEventData)

	// Emit through event bridge directly
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit unified completion event: %v", err))
	}
}

// EmitOrchestratorAgentError emits an orchestrator agent error event
func (bo *BaseOrchestrator) EmitOrchestratorAgentError(ctx context.Context, agentType, agentName, objective, errorMsg string, stepIndex, iteration int) {
	eventData := &events.OrchestratorAgentErrorEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType: agentType,
		AgentName: agentName,
		Objective: objective,
		Error:     errorMsg,
		StepIndex: stepIndex,
		Iteration: iteration,
	}

	bo.emitEvent(ctx, events.OrchestratorAgentError, eventData)
}

// EmitStepFailedEvent emits a step failed event
func (bo *BaseOrchestrator) EmitStepFailedEvent(ctx context.Context, stepID, stepTitle, stepPath, errorMsg string, stepIndex int, isBranchStep bool) {
	eventData := &events.StepFailedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:       stepID,
		StepIndex:    stepIndex,
		StepTitle:    stepTitle,
		StepPath:     stepPath,
		IsBranchStep: isBranchStep,
		Error:        errorMsg,
	}

	bo.emitEvent(ctx, events.StepExecutionFailed, eventData)
}
