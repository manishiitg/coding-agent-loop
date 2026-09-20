package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

// Per-workflow Slack apps: CRUD over Slack app identities, under the same
// /api/human-feedback/slack prefix the single-app endpoints already use.
//
// One connection = one Slack app (bot token + app token pair) with its own
// Socket Mode listener. Workflows select one via
// WorkflowCapabilities.SlackConnectionID; anything without a selection uses
// the default connection.
//
// Ownership: a connection scoped to a workflow (workspace_path) is managed
// by that workflow's owners; unscoped connections, including the default,
// are managed by platform admins. Tokens are encrypted at rest and masked
// in every response.

// SlackConnectionResponse is the wire shape of one connection.
//
// Built by explicit projection rather than marshaling
// services.SlackConnection, so a field added to the model later cannot leak
// through this API by default. Tokens are always masked here.
type SlackConnectionResponse struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	BotToken      string `json:"bot_token,omitempty"` // masked
	AppToken      string `json:"app_token,omitempty"` // masked
	Enabled       bool   `json:"enabled"`
	Configured    bool   `json:"configured"`
	IsDefault     bool   `json:"is_default"`
	WorkspacePath string `json:"workspace_path,omitempty"`
}

// SlackConnectionsResponse is the list payload.
type SlackConnectionsResponse struct {
	Connections         []SlackConnectionResponse `json:"connections"`
	DefaultConnectionID string                    `json:"default_connection_id,omitempty"`
}

// SlackConnectionRequest is the create/update body. On update, empty or
// masked tokens keep the stored value; Enabled and WorkspacePath are
// pointers so "leave unchanged" stays distinct from an explicit value.
type SlackConnectionRequest struct {
	DisplayName   string  `json:"display_name,omitempty"`
	BotToken      string  `json:"bot_token,omitempty"`
	AppToken      string  `json:"app_token,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
	WorkspacePath *string `json:"workspace_path,omitempty"`
}

// SlackConnectionRoutes wires the connection registry API, mirroring the
// Gmail connections routes.
func SlackConnectionRoutes(router *mux.Router, api *StreamingAPI) {
	r := router.PathPrefix("/api/human-feedback/slack/connections").Subrouter()
	r.HandleFunc("", listSlackConnectionsHandler(api)).Methods("GET")
	r.HandleFunc("", createSlackConnectionHandler(api)).Methods("POST", "OPTIONS")
	r.HandleFunc("/{id}", getSlackConnectionHandler(api)).Methods("GET")
	r.HandleFunc("/{id}", updateSlackConnectionHandler(api)).Methods("PATCH", "POST", "OPTIONS")
	r.HandleFunc("/{id}", deleteSlackConnectionHandler(api)).Methods("DELETE", "OPTIONS")
	r.HandleFunc("/{id}/default", setDefaultSlackConnectionHandler(api)).Methods("POST", "OPTIONS")
	r.HandleFunc("/{id}/test", testSlackConnectionEntryHandler(api)).Methods("POST", "OPTIONS")
}

// projectSlackConnection is the single place a connection becomes JSON.
func projectSlackConnection(conn services.SlackConnection, defaultID string) SlackConnectionResponse {
	masked := conn
	// The service already masks on read, but projection must not depend on
	// that: re-mask defensively so a future unmasked caller cannot leak.
	masked.BotToken = maskSlackTokenForAPI("xoxb-", conn.BotToken)
	masked.AppToken = maskSlackTokenForAPI("xapp-", conn.AppToken)
	return SlackConnectionResponse{
		ID:            conn.ID,
		DisplayName:   conn.DisplayName,
		BotToken:      masked.BotToken,
		AppToken:      masked.AppToken,
		Enabled:       conn.Enabled,
		Configured:    strings.TrimSpace(conn.BotToken) != "" && strings.TrimSpace(conn.AppToken) != "",
		IsDefault:     conn.ID != "" && conn.ID == defaultID,
		WorkspacePath: conn.WorkspacePath,
	}
}

// maskSlackTokenForAPI masks a token that may already be masked. Masked
// values pass through unchanged so presence checks keep working.
func maskSlackTokenForAPI(prefix string, token string) string {
	if strings.Contains(token, "...") || token == "" {
		return token
	}
	if len(token) <= 4 {
		return prefix + "..."
	}
	return prefix + "..." + token[len(token)-4:]
}

func writeSlackConnection(w http.ResponseWriter, svc *services.SlackService, conn services.SlackConnection) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projectSlackConnection(conn, svc.DefaultConnectionID()))
}

// slackConnectionService resolves the service and the {id} path variable,
// writing the error response itself when either is unavailable.
func slackConnectionService(w http.ResponseWriter, r *http.Request) (*services.SlackService, string, bool) {
	svc, err := ensureSlackService()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
		return nil, "", false
	}
	return svc, strings.TrimSpace(mux.Vars(r)["id"]), true
}

// requireSlackConnectionCreateAccess enforces who may create a connection.
// Admins may create at any scope; anyone else must scope the connection to
// a workflow they own. Bot-route principals can never manage connections.
func requireSlackConnectionCreateAccess(r *http.Request, workspacePath string) error {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" {
		return fmt.Errorf("only an authenticated interactive user may manage Slack connections")
	}
	if currentUserIsAdmin(r) {
		return nil
	}
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return fmt.Errorf("only a platform admin may create a platform-managed Slack connection")
	}
	return requireSlackConnectionWorkflowOwner(r.Context(), workspacePath)
}

// requireSlackConnectionAccess enforces who may read-for-write, update,
// test, or delete a connection. Admins manage everything; anyone else must
// own the workflow the connection is scoped to. Unscoped connections are
// admin-only.
func requireSlackConnectionAccess(r *http.Request, conn services.SlackConnection) error {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" {
		return fmt.Errorf("only an authenticated interactive user may manage Slack connections")
	}
	if currentUserIsAdmin(r) {
		return nil
	}
	if strings.TrimSpace(conn.WorkspacePath) == "" {
		return fmt.Errorf("only a platform admin may manage this Slack connection")
	}
	return requireSlackConnectionWorkflowOwner(r.Context(), conn.WorkspacePath)
}

// requireSlackConnectionWorkflowOwner requires Owner (not merely write)
// access on the workflow, matching the bar for managing its Slack routes.
func requireSlackConnectionWorkflowOwner(ctx context.Context, workspacePath string) error {
	manifest, exists, err := ReadWorkflowManifest(ctx, strings.TrimSpace(workspacePath))
	if err != nil {
		return err
	}
	if !exists || manifest == nil {
		return fmt.Errorf("no workflow owns this Slack connection scope")
	}
	if workflowAccessForManifest(GetUserFromContext(ctx), manifest) != WorkflowAccessOwner {
		return fmt.Errorf("only a workflow owner may manage this Slack connection")
	}
	return nil
}

func listSlackConnectionsHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := ensureSlackService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
			return
		}
		defaultID := svc.DefaultConnectionID()
		conns := svc.ListConnections()
		out := SlackConnectionsResponse{
			Connections:         make([]SlackConnectionResponse, 0, len(conns)),
			DefaultConnectionID: defaultID,
		}
		for _, c := range conns {
			out.Connections = append(out.Connections, projectSlackConnection(c, defaultID))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func getSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, id, ok := slackConnectionService(w, r)
		if !ok {
			return
		}
		conn, found := svc.GetConnection(id)
		if !found {
			http.Error(w, fmt.Sprintf("slack connection %q not found", id), http.StatusNotFound)
			return
		}
		writeSlackConnection(w, svc, conn)
	}
}

func createSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, err := ensureSlackService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
			return
		}
		var req SlackConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		workspacePath := ""
		if req.WorkspacePath != nil {
			workspacePath = strings.TrimSpace(*req.WorkspacePath)
		}
		if err := requireSlackConnectionCreateAccess(r, workspacePath); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		conn, err := svc.CreateSlackConnection(r.Context(), services.SlackConnectionInput{
			DisplayName:   req.DisplayName,
			BotToken:      req.BotToken,
			AppToken:      req.AppToken,
			Enabled:       enabled,
			WorkspacePath: workspacePath,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeSlackConnection(w, svc, conn)
	}
}

func updateSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := slackConnectionService(w, r)
		if !ok {
			return
		}
		current, found := svc.GetConnection(id)
		if !found {
			http.Error(w, fmt.Sprintf("slack connection %q not found", id), http.StatusNotFound)
			return
		}
		if err := requireSlackConnectionAccess(r, current); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		var req SlackConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		enabled := current.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		workspacePath := current.WorkspacePath
		if req.WorkspacePath != nil {
			if !currentUserIsAdmin(r) {
				http.Error(w, "only a platform admin may change a Slack connection's scope", http.StatusForbidden)
				return
			}
			workspacePath = strings.TrimSpace(*req.WorkspacePath)
			if workspacePath != "" {
				if _, exists, err := ReadWorkflowManifest(r.Context(), workspacePath); err != nil || !exists {
					http.Error(w, "slack connection scope names an unknown workflow", http.StatusBadRequest)
					return
				}
			}
		}
		conn, err := svc.UpdateSlackConnection(r.Context(), id, services.SlackConnectionInput{
			DisplayName:   req.DisplayName,
			BotToken:      req.BotToken,
			AppToken:      req.AppToken,
			Enabled:       enabled,
			WorkspacePath: workspacePath,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSlackConnection(w, svc, conn)
	}
}

func deleteSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := slackConnectionService(w, r)
		if !ok {
			return
		}
		current, found := svc.GetConnection(id)
		if !found {
			http.Error(w, fmt.Sprintf("slack connection %q not found", id), http.StatusNotFound)
			return
		}
		if err := requireSlackConnectionAccess(r, current); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if ref := slackConnectionWorkflowReference(r.Context(), id); ref != "" {
			http.Error(w, fmt.Sprintf("slack connection is still selected by workflow %q; point the workflow elsewhere first", ref), http.StatusConflict)
			return
		}
		if err := svc.DeleteSlackConnection(r.Context(), id); err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// validateWorkflowSlackConnectionID fails a manifest save that selects an
// unknown Slack connection. Empty (inherit the default) always passes, as
// does any selection when the registry cannot be read: an infra outage must
// not block unrelated manifest edits, and sends fail loudly anyway.
func validateWorkflowSlackConnectionID(connID string) error {
	connID = strings.TrimSpace(connID)
	if connID == "" {
		return nil
	}
	exists, err := services.SlackConnectionExists(connID)
	if err != nil {
		log.Printf("[SLACK] connection validation skipped: %v", err)
		return nil
	}
	if !exists {
		return fmt.Errorf("slack_connection_id %q names an unknown Slack connection", connID)
	}
	return nil
}

// slackConnectionWorkflowReference names a workflow (label preferred) that
// still selects the connection, or "" when nothing references it. Discovery
// failures fail closed: deleting blind could strand a workflow's sends.
func slackConnectionWorkflowReference(ctx context.Context, connID string) string {
	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		log.Printf("[SLACK] workflow discovery failed during connection delete: %v", err)
		return "unknown (workflow discovery failed)"
	}
	for _, entry := range discovered {
		manifest := entry.Manifest
		if manifest == nil {
			continue
		}
		if strings.TrimSpace(manifest.Capabilities.SlackConnectionID) != connID {
			continue
		}
		if label := strings.TrimSpace(manifest.Label); label != "" {
			return label
		}
		return manifest.ID
	}
	return ""
}

func setDefaultSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		claims := GetUserFromContext(r.Context())
		if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" || !currentUserIsAdmin(r) {
			http.Error(w, "only a platform admin may change the default Slack connection", http.StatusForbidden)
			return
		}
		svc, id, ok := slackConnectionService(w, r)
		if !ok {
			return
		}
		if err := svc.SetDefaultSlackConnection(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		conn, found := svc.GetConnection(id)
		if !found {
			http.Error(w, fmt.Sprintf("slack connection %q not found", id), http.StatusNotFound)
			return
		}
		writeSlackConnection(w, svc, conn)
	}
}

func testSlackConnectionEntryHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := slackConnectionService(w, r)
		if !ok {
			return
		}
		current, found := svc.GetConnection(id)
		if !found {
			http.Error(w, fmt.Sprintf("slack connection %q not found", id), http.StatusNotFound)
			return
		}
		if err := requireSlackConnectionAccess(r, current); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		result := svc.DiagnoseConnectionFor(r.Context(), id)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// slackConnectionIDForRoute resolves the connection a channel route sends
// through: the destination workflow's manifest selection, or "" (platform
// default) for profile routes and workflows without a selection.
func slackConnectionIDForRoute(ctx context.Context, route ChannelRoute) string {
	if strings.TrimSpace(route.WorkflowID) == "" {
		return ""
	}
	manifest, found, err := ReadWorkflowManifest(ctx, route.WorkspacePath)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(manifest.Capabilities.SlackConnectionID)
}

// slackServiceForRoute resolves the live runtime a channel route sends
// through. Unknown or unavailable selections fail loudly; the default
// never silently substitutes for an explicit selection.
func slackServiceForRoute(ctx context.Context, route ChannelRoute) (*services.SlackService, error) {
	svc, err := ensureSlackService()
	if err != nil {
		return nil, err
	}
	connID := slackConnectionIDForRoute(ctx, route)
	return svc.ServiceForConnection(connID)
}

// slackToolConnectionID resolves the connection a bot-tool send uses. A
// live bot execution already carries its arrival connection, which wins so
// tool sends stay on the conversation's own Slack app; anything else falls
// back to the route workflow's manifest selection.
func slackToolConnectionID(ctx context.Context, api *StreamingAPI, session string, route ChannelRoute) string {
	if api != nil && session != "" {
		if execution, ok := api.botExecutionForSession(session); ok {
			if id := strings.TrimSpace(execution.Request.BotConnectionID); id != "" {
				return id
			}
		}
	}
	return slackConnectionIDForRoute(ctx, route)
}
