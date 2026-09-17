package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/mcpagent/events"
)

// internalBotRequestContext gives bot-originated turns the same authenticated
// identity as an HTTP turn from the paired account. The user directory remains
// authoritative for permissions: we deliberately persist only the user ID on
// the connector, then resolve the current username/email/role for every turn so
// a promotion or demotion takes effect without re-pairing WhatsApp.
func internalBotRequestContext(ctx context.Context, userID string, reqMaps ...map[string]interface{}) context.Context {
	if userID == "" || GetUserFromContext(ctx) != nil {
		return ctx
	}
	claims := &UserClaims{UserID: userID, Username: userID}
	if record := directoryUserFor(userID, "", ""); record != nil {
		claims.Username = record.Username
		claims.Email = record.Email
	}
	if len(reqMaps) > 0 {
		applyBotRouteClaims(claims, reqMaps[0])
	}
	return context.WithValue(ctx, UserContextKey, claims)
}

func applyBotRouteClaims(claims *UserClaims, reqMap map[string]interface{}) {
	if claims == nil || reqMap == nil {
		return
	}
	platform, _ := reqMap["bot_platform"].(string)
	grant, _ := reqMap["bot_route_grant"].(string)
	grant = services.NormalizeBotRouteGrant(grant, "")
	if strings.TrimSpace(platform) != "slack" || stringFromRequestMap(reqMap, "bot_route_grant") == "" {
		return
	}
	claims.Provider = "bot_route"
	claims.BotRouteGrant = grant
	claims.BotRouteWorkflowID = strings.TrimSpace(stringFromRequestMap(reqMap, "preset_query_id"))
	claims.BotRouteProfileID = strings.TrimSpace(stringFromRequestMap(reqMap, "agent_profile_id"))
	claims.BotRouteConversationKey = strings.TrimSpace(stringFromRequestMap(reqMap, "agent_profile_conversation_key"))
	claims.BotRouteWorkspacePath = strings.TrimSpace(stringFromRequestMap(reqMap, "selected_folder"))
}

func stringFromRequestMap(reqMap map[string]interface{}, key string) string {
	value, _ := reqMap[key].(string)
	return strings.TrimSpace(value)
}

func botRouteUserClaims(userID string, route services.ChannelRoute) *UserClaims {
	userID = strings.TrimSpace(userID)
	return &UserClaims{
		UserID:                  userID,
		Username:                userID,
		Provider:                "bot_route",
		BotRouteGrant:           services.NormalizeBotRouteGrant(route.BotGrant, route.WorkshopMode),
		BotRouteWorkflowID:      strings.TrimSpace(route.WorkflowID),
		BotRouteProfileID:       strings.TrimSpace(route.ProfileID),
		BotRouteConversationKey: strings.TrimSpace(route.ConversationKey),
		BotRouteWorkspacePath:   strings.TrimSpace(route.WorkspacePath),
	}
}

func botRouteProfileAccessForRequest(claims *UserClaims, req QueryRequest) (WorkflowAccessLevel, bool) {
	if claims == nil || claims.Provider != "bot_route" || strings.TrimSpace(claims.BotRouteProfileID) == "" {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(claims.BotRouteProfileID), strings.TrimSpace(req.AgentProfileID)) {
		return WorkflowAccessNone, true
	}
	if !strings.EqualFold(strings.TrimSpace(claims.BotRouteConversationKey), strings.TrimSpace(req.AgentProfileConversationKey)) {
		return WorkflowAccessNone, true
	}
	if strings.TrimSpace(claims.BotRouteWorkflowID) != "" {
		return WorkflowAccessNone, true
	}
	if strings.TrimSpace(claims.BotRouteWorkspacePath) == "" ||
		filepath.Clean(claims.BotRouteWorkspacePath) != filepath.Clean(strings.TrimSpace(req.SelectedFolder)) {
		return WorkflowAccessNone, true
	}
	if strings.EqualFold(strings.TrimSpace(claims.BotRouteGrant), "owner") {
		return WorkflowAccessOwner, true
	}
	return WorkflowAccessRead, true
}

// checkBotWorkflowAccess decides whether an external chat user may drive a
// routed workflow. Unlike the web paths it deliberately fails CLOSED at every
// step: a browser caller has already authenticated into the account before any
// permission is resolved, whereas a bot caller has only proved that they are in
// a Slack channel. The legacy "no ownership record means the account tier
// applies" fallback (workflow_access.go rule 3) is therefore not honoured here
// — bot access must be granted explicitly or not at all.
func (api *StreamingAPI) checkBotWorkflowAccess(ctx context.Context, workspaceUserID, userEmail string, route services.ChannelRoute) (string, bool, error) {
	fallbackID := strings.TrimSpace(workspaceUserID)

	var manifest *WorkflowManifest
	var exists bool
	var err error
	workspacePath := strings.TrimSpace(route.WorkspacePath)
	if workspacePath != "" {
		manifest, exists, err = ReadWorkflowManifest(ctx, workspacePath)
		if err != nil {
			return fallbackID, false, err
		}
	}
	if !exists || manifest == nil {
		manifest, exists, err = api.workflowManifestByBotRoute(ctx, route)
		if err != nil {
			return fallbackID, false, err
		}
		if !exists || manifest == nil {
			log.Printf("[BOT_ACCESS] Denied: no manifest for route workflow=%q workspace=%q", strings.TrimSpace(route.WorkflowID), workspacePath)
			return fallbackID, false, nil
		}
	}

	// 1. An unowned workflow grants the account tier on the web. Over a bot that
	// would hand every channel member write access to any manifest the builder
	// wrote without a created_by stamp, so require a real ownership record.
	if !manifest.hasOwnershipRecord() {
		log.Printf("[BOT_ACCESS] Denied: workflow %s has no owner recorded; claim or share it before routing a bot to it", manifest.ID)
		return fallbackID, false, nil
	}

	// 2. The configured route grant is resolved through the same manifest access
	// function used by AgentWorks chat. Slack sender identity stays audit
	// metadata and does not alter the route principal's authority.
	claims := botRouteUserClaims(fallbackID, route)
	if workflowAccessForManifest(claims, manifest) == WorkflowAccessNone {
		log.Printf("[BOT_ACCESS] Denied: route principal %s has no access to workflow %s", claims.UserID, manifest.ID)
		return fallbackID, false, nil
	}
	return claims.UserID, true, nil
}

func (api *StreamingAPI) workflowManifestByBotRoute(ctx context.Context, route services.ChannelRoute) (*WorkflowManifest, bool, error) {
	workflowID := strings.TrimSpace(route.WorkflowID)
	if workflowID == "" {
		return nil, false, nil
	}
	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, workflow := range discovered {
		if workflow.Manifest != nil && strings.EqualFold(strings.TrimSpace(workflow.Manifest.ID), workflowID) {
			return workflow.Manifest, true, nil
		}
	}
	return nil, false, nil
}

// startSessionInternal starts an agent session programmatically (used by bot connector).
// It constructs a QueryRequest from the provided map and invokes handleQuery internally.
// This blocks until the exact query execution and all descendants complete.
func (api *StreamingAPI) startSessionInternal(
	ctx context.Context,
	reqMap map[string]interface{},
	sessionID string,
	userID string,
	eventCallback func(event *events.AgentEvent),
) error {
	if failure, ok := reqMap["_bot_prepare_error"].(string); ok {
		return fmt.Errorf("prepare conversation: %s", failure)
	}
	// Marshal the request map to JSON
	wireRequest := maps.Clone(reqMap)
	delete(wireRequest, "_trusted_resume_target")
	body, err := json.Marshal(wireRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal query request: %w", err)
	}

	// Create a fake HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "/api/query", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fmt.Errorf("failed to create internal request: %w", err)
	}
	httpReq = httpReq.WithContext(internalBotRequestContext(httpReq.Context(), userID, reqMap))
	if target, ok := reqMap["_trusted_resume_target"].(*resolvedResumeTarget); ok {
		httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), resolvedResumeTargetContextKey{}, target))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Session-ID", sessionID)
	if userID != "" {
		httpReq.Header.Set("X-User-ID", userID)
	}

	// Use a ResponseRecorder to capture the response
	recorder := httptest.NewRecorder()

	// Call handleQuery synchronously — but it starts processing async and returns immediately
	api.handleQuery(recorder, httpReq)

	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("handleQuery returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the response to get the actual queryID
	var queryResp QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		if isScheduledSession(sessionID) {
			scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Failed to parse handleQuery response: %v", err)
		}
	}

	if isScheduledSession(sessionID) {
		scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Internal session started: sessionID=%s queryID=%s", sessionID, queryResp.QueryID)
	}

	if queryResp.Status == queryStatusLiveInputDelivered {
		if isScheduledSession(sessionID) {
			scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Internal session delivered as live input; waiting for exact execution %s", queryResp.QueryID)
		}
	}
	if strings.TrimSpace(queryResp.QueryID) == "" {
		return fmt.Errorf("handleQuery did not return a query execution id")
	}
	err = api.waitForConversationTurnTree(ctx, sessionID, queryResp.QueryID, schedulerWorkshopMaxInactivity)
	if execution, ok := api.botExecutionForSession(sessionID); ok && execution.Request.PresetQueryID != "" {
		if pruneErr := api.pruneSlackRuns(execution.Request.SelectedFolder); pruneErr != nil {
			log.Printf("[SLACK_RETENTION] %v", pruneErr)
		}
	}
	return err
}

// sendFollowUpInternal injects a follow-up message into an existing session.
// It reuses the handleQuery path with the same session ID but does NOT block on completion.
// Events flow via EventStore → BotEventFilter → thread automatically.
//
// The reqMap is built by BotConversationManager.buildQueryRequest() so the follow-up agent
// gets the exact same config (servers, skills, delegation mode, API keys, etc.) as the initial session.
func (api *StreamingAPI) sendFollowUpInternal(
	ctx context.Context,
	reqMap map[string]interface{},
	sessionID string,
	userID string,
) error {
	if failure, ok := reqMap["_bot_prepare_error"].(string); ok {
		return fmt.Errorf("prepare conversation: %s", failure)
	}
	wireRequest := maps.Clone(reqMap)
	delete(wireRequest, "_trusted_resume_target")
	body, err := json.Marshal(wireRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal follow-up request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "/api/query", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fmt.Errorf("failed to create follow-up request: %w", err)
	}
	httpReq = httpReq.WithContext(internalBotRequestContext(httpReq.Context(), userID, reqMap))
	if target, ok := reqMap["_trusted_resume_target"].(*resolvedResumeTarget); ok {
		httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), resolvedResumeTargetContextKey{}, target))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Session-ID", sessionID)
	if userID != "" {
		httpReq.Header.Set("X-User-ID", userID)
	}

	recorder := httptest.NewRecorder()
	api.handleQuery(recorder, httpReq)

	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("follow-up failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	if execution, ok := api.botExecutionForSession(sessionID); ok && execution.Request.PresetQueryID != "" {
		var result QueryResponse
		if json.NewDecoder(resp.Body).Decode(&result) == nil && result.QueryID != "" {
			go func() {
				_ = api.waitForConversationTurnTree(context.Background(), sessionID, result.QueryID, schedulerWorkshopMaxInactivity)
				if err := api.pruneSlackRuns(execution.Request.SelectedFolder); err != nil {
					log.Printf("[SLACK_RETENTION] %v", err)
				}
			}()
		}
	}

	if isScheduledSession(sessionID) {
		scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Follow-up injected into session %s", sessionID)
	}
	return nil
}

// botWorkflowTurn is the workflow adapter to the application-owned request
// builder. Connector code supplies only normalized text, target and thread.
func (api *StreamingAPI) botWorkflowTurn(ctx context.Context, query string, route services.ChannelRoute, thread services.ThreadID) (map[string]interface{}, error) {
	manifest, found, err := ReadWorkflowManifest(ctx, route.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if !found || manifest.ID != route.WorkflowID {
		return nil, fmt.Errorf("workflow route target is unavailable")
	}
	if api.scheduler == nil {
		return nil, fmt.Errorf("shared conversation builder unavailable")
	}
	principalID := services.BotPrincipalIDForRoute(thread.Platform, route)
	req := api.scheduler.buildWorkshopRequest(ctx, &ScheduleContext{WorkspacePath: route.WorkspacePath, WorkflowID: route.WorkflowID, WorkflowLabel: manifest.Label, OwnerUserID: principalID, Capabilities: manifest.Capabilities, Schedule: WorkflowSchedule{Name: manifest.Label}, TriggerSource: "bot:" + thread.Platform})
	req["query"] = query
	req["bot_platform"] = thread.Platform
	req["bot_channel_id"] = thread.ChannelID
	req["bot_thread_ts"] = thread.ThreadTS
	req["bot_route_grant"] = route.BotGrant
	req["workshop_mode"] = services.WorkshopModeForBotGrant(route.BotGrant)
	req["execution_options"].(map[string]interface{})["workshop_mode"] = services.WorkshopModeForBotGrant(route.BotGrant)
	req["bot_send_full_details"] = route.SendFullDetails
	delete(req, "disable_live_input_delivery")
	return req, nil
}
