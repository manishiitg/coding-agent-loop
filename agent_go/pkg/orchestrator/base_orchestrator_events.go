package orchestrator

import (
	"context"
	"fmt"
	"time"

	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "github.com/manishiitg/mcpagent/events"
)

// emitEvent emits an event through the event bridge
func (bo *BaseOrchestrator) emitEvent(ctx context.Context, eventType baseevents.EventType, data baseevents.EventData) {
	// Create agent event
	agentEvent := &baseevents.AgentEvent{
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

	eventData := &orchestrator_events.OrchestratorStartEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:        objective,
		AgentsCount:      agentsCount,
		ServersCount:     len(bo.selectedServers),
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, orchestrator_events.OrchestratorStart, eventData)
}

// EmitOrchestratorEnd emits an orchestrator end event
func (bo *BaseOrchestrator) EmitOrchestratorEnd(ctx context.Context, objective, result, status, message string, executionMode string) {
	// Removed verbose logging

	duration := time.Since(bo.startTime)
	eventData := &orchestrator_events.OrchestratorEndEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:        objective,
		Result:           result,
		Status:           status,
		Duration:         duration,
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, orchestrator_events.OrchestratorEnd, eventData)
}

// EmitUnifiedCompletionEvent emits a unified completion event
func (bo *BaseOrchestrator) EmitUnifiedCompletionEvent(ctx context.Context, agentType, agentMode, question, finalResult, status string, turns int) {
	// Removed verbose logging

	duration := time.Since(bo.startTime)
	completionEventData := baseevents.NewUnifiedCompletionEvent(
		agentType,
		agentMode,
		question,
		finalResult,
		status,
		duration,
		turns,
	)

	agentEvent := baseevents.NewAgentEvent(completionEventData)

	// Emit through event bridge directly
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit unified completion event: %v", err))
	}
}

// EmitOrchestratorAgentError emits an orchestrator agent error event
func (bo *BaseOrchestrator) EmitOrchestratorAgentError(ctx context.Context, agentType, agentName, objective, errorMsg string, stepIndex, iteration int) {
	eventData := &orchestrator_events.OrchestratorAgentErrorEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType: agentType,
		AgentName: agentName,
		Objective: objective,
		Error:     errorMsg,
		StepIndex: stepIndex,
		Iteration: iteration,
	}

	bo.emitEvent(ctx, orchestrator_events.OrchestratorAgentError, eventData)
}

