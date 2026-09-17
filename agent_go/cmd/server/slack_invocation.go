package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// bindSlackInvocation uses the same immutable execution binding as schedules.
// The adapter cannot select Builder's iteration-0 or another invocation's path.
func (api *StreamingAPI) bindSlackInvocation(ctx context.Context, req *QueryRequest, sessionID string) error {
	claims := GetUserFromContext(ctx)
	if claims != nil && claims.ExecutionPrincipal != nil {
		lock := scheduleRunFileLock("bot-conversation:" + sessionID)
		lock.Lock()
		defer lock.Unlock()
		if previous, ok := api.botExecutionSessions.Load(sessionID); ok {
			bound := previous.(botExecutionSession)
			if bound.Request.BotChannelID != req.BotChannelID || bound.Request.BotThreadTS != req.BotThreadTS {
				active, known := api.getActiveSession(sessionID)
				if !known || active.Status == "running" || active.HasRunningBackgroundAgents || active.NeedsUserInput {
					return fmt.Errorf("product conversation is active in another Slack thread")
				}
			}
		}
		api.botExecutionSessions.Store(sessionID, botExecutionSession{Claims: claims, Request: *req})
	}
	if claims == nil || claims.ExecutionPrincipal == nil || req.BotPlatform != "slack" || claims.BotRouteWorkflowID == "" {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" || req.BotThreadTS == "" {
		return fmt.Errorf("Slack invocation requires a server session and thread")
	}
	invocationID := claims.ExecutionPrincipal.ID + ":" + req.BotChannelID + ":" + req.BotThreadTS + ":" + sessionID

	sum := sha256.Sum256([]byte(invocationID))
	bindingPath := fmt.Sprintf("config/slack-invocations/%x.json", sum)
	executionID := ""
	if raw, found, err := readFileFromWorkspace(ctx, bindingPath); err != nil {
		return err
	} else if found {
		var saved stepworkflow.ExternalInvocation
		if json.Unmarshal([]byte(raw), &saved) != nil || saved.RunID == "" {
			return fmt.Errorf("invalid persisted Slack invocation")
		}
		invocationID = saved.RunID
		executionID = saved.ExecutionID
	}
	var allocate func(context.Context, string, string) (*stepworkflow.ExternalInvocation, error)
	allocate = func(callCtx context.Context, runID, executionID string) (*stepworkflow.ExternalInvocation, error) {
		if _, err := api.revalidateExecutionPrincipal(context.WithValue(callCtx, UserContextKey, claims), *req); err != nil {
			return nil, err
		}
		folder, err := allocateSlackRunFolder(req.SelectedFolder, runID)
		if err != nil {
			return nil, err
		}
		invocation := &stepworkflow.ExternalInvocation{Kind: "slack", ExecutionID: executionID, RunID: runID, ScheduleID: claims.ExecutionPrincipal.ID, RunFolder: folder, TriggerSource: "slack", AllowParallel: true}
		invocation.Next = func(nextCtx context.Context, executionID string) (*stepworkflow.ExternalInvocation, error) {
			lock := scheduleRunFileLock(bindingPath)
			lock.Lock()
			defer lock.Unlock()
			nextRunID := invocationID + ":" + executionID
			if value, ok := api.scheduleInvocations.Load(sessionID); ok {
				current := value.(*stepworkflow.ExternalInvocation)
				if current.ExecutionID == "" || current.ExecutionID == executionID {
					nextRunID = current.RunID
				}
			}
			return allocate(nextCtx, nextRunID, executionID)
		}
		raw, err := json.Marshal(invocation)
		if err != nil {
			return nil, err
		}
		if err := writeFileToWorkspace(callCtx, bindingPath, string(raw)); err != nil {
			return nil, err
		}
		api.scheduleInvocations.Store(sessionID, invocation)
		return invocation, nil
	}
	invocation, err := allocate(ctx, invocationID, executionID)
	if err != nil {
		return err
	}
	// Session tools follow the current run; each dispatched controller receives
	// its own immutable snapshot from Next.
	current := *invocation
	current.Current = func() *stepworkflow.ExternalInvocation {
		value, ok := api.scheduleInvocations.Load(sessionID)
		if !ok {
			return nil
		}
		return value.(*stepworkflow.ExternalInvocation)
	}
	api.scheduleInvocations.Store(sessionID, &current)
	folder := current.RunFolder
	if req.ExecutionOptions == nil {
		req.ExecutionOptions = &ExecutionOptions{}
	}
	req.ExecutionOptions.SelectedRunFolder = folder
	return nil
}

// Reuse the platform's cancellation path: a removed or changed grant stops
// active native runtimes and descendants, not only the next chat message.
func (api *StreamingAPI) revokeChangedBotSessions(routes map[string]ChannelRoute) {
	api.botExecutionSessions.Range(func(key, value interface{}) bool {
		binding := value.(botExecutionSession)
		route, found := routes[binding.Request.BotChannelID]
		if !found || !sameSlackRouteDestination(route, binding.Claims.ExecutionPrincipal.Target) || route.BotGrant != binding.Claims.BotRouteGrant {
			api.cancelSessionRuntimeWork(key.(string), "bot route grant changed or revoked", runtimePhaseCanceled)
		}
		return true
	})
}
