package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// Per-workflow and per-project Slack apps: CRUD over Slack app identities,
// under the same /api/human-feedback/slack prefix the single-app endpoints
// already use.
//
// One connection = one Slack app (bot token + app token pair) with its own
// Socket Mode listener. Workflows select one via
// WorkflowCapabilities.SlackConnectionID and crew projects via
// capabilities.slack_connection_id in their runtime manifest; anything
// without a selection uses the default connection.
//
// Ownership: a connection scoped to a workflow or product project
// (workspace_path, plus profile_id for products) is managed by that
// workflow's owners or the product's owners; unscoped connections,
// including the default, are managed by platform admins. Tokens are
// encrypted at rest and masked in every response.

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
	ProfileID     string `json:"profile_id,omitempty"`
}

// SlackConnectionsResponse is the list payload.
type SlackConnectionsResponse struct {
	Connections         []SlackConnectionResponse `json:"connections"`
	DefaultConnectionID string                    `json:"default_connection_id,omitempty"`
}

// SlackConnectionRequest is the create/update body. On update, empty or
// masked tokens keep the stored value; Enabled, WorkspacePath, and
// ProfileID are pointers so "leave unchanged" stays distinct from an
// explicit value.
type SlackConnectionRequest struct {
	DisplayName   string  `json:"display_name,omitempty"`
	BotToken      string  `json:"bot_token,omitempty"`
	AppToken      string  `json:"app_token,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
	WorkspacePath *string `json:"workspace_path,omitempty"`
	ProfileID     *string `json:"profile_id,omitempty"`
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
	r.HandleFunc("/project/selection", projectSlackConnectionHandler(api)).Methods("GET", "PUT", "POST", "OPTIONS")
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
		ProfileID:     conn.ProfileID,
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
func requireSlackConnectionCreateAccess(r *http.Request, api *StreamingAPI, workspacePath, profileID string) error {
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
	if strings.TrimSpace(profileID) != "" {
		return requireProductSlackScopeOwner(r.Context(), api, profileID, workspacePath)
	}
	return requireSlackConnectionWorkflowOwner(r.Context(), workspacePath)
}

// requireSlackConnectionAccess enforces who may read-for-write, update,
// test, or delete a connection. Admins manage everything; anyone else must
// own the workflow or product project the connection is scoped to.
// Unscoped connections are admin-only.
func requireSlackConnectionAccess(r *http.Request, api *StreamingAPI, conn services.SlackConnection) error {
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
	if strings.TrimSpace(conn.ProfileID) != "" {
		return requireProductSlackScopeOwner(r.Context(), api, conn.ProfileID, conn.WorkspacePath)
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

// requireProductSlackScopeOwner is the product-project mirror of
// requireSlackConnectionWorkflowOwner: admins pass, anyone else must be
// allowed on the product and the scope must sit under their own product
// root. Product workspaces are per-user resources, so a path outside both
// roots names someone else's project. Manifest existence is deliberately
// not required: fixed-workspace products create their manifest on first
// use, and the selection write below does that.
func requireProductSlackScopeOwner(ctx context.Context, api *StreamingAPI, profileID, workspacePath string) error {
	claims := GetUserFromContext(ctx)
	if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" {
		return fmt.Errorf("only an authenticated interactive user may manage Slack connections")
	}
	if userAccessForClaims(claims).Admin {
		return nil
	}
	profileID = strings.TrimSpace(profileID)
	workspacePath = strings.TrimSpace(workspacePath)
	if profileID == "" || workspacePath == "" {
		return fmt.Errorf("product Slack scope is incomplete")
	}
	if api == nil || api.agentProfiles == nil {
		return fmt.Errorf("agent profiles are unavailable; cannot verify product ownership")
	}
	userID := productWorkspaceUserID(ctx)
	profile, err := api.agentProfiles.Resolve(profileID, 0, userID)
	if err != nil {
		return fmt.Errorf("agent profile %q is unavailable: %w", profileID, err)
	}
	if !userAllowedProduct(claims, profile.Product) {
		return fmt.Errorf("only a product owner may manage this Slack connection")
	}
	if !productWorkspaceUnderCallerRoot(profile, userID, workspacePath) {
		return fmt.Errorf("Slack connection scope is outside your product workspace")
	}
	return nil
}

// productWorkspaceUnderCallerRoot reports whether workspacePath sits under
// the caller's own singleton root or keyed-projects runtime root for the
// profile. Paths are cleaned before the prefix comparison so ".." cannot
// escape the root.
func productWorkspaceUnderCallerRoot(profile agentprofiles.Profile, userID, workspacePath string) bool {
	clean := filepath.ToSlash(filepath.Clean("/" + strings.TrimSpace(workspacePath)))
	roots := []string{}
	if root := strings.TrimSpace(profile.Runtime.Workspace.Root); root != "" {
		if cleaned, err := cleanAgentProfileWorkspace(root, userID); err == nil {
			roots = append(roots, cleaned)
		}
	}
	if projectsRoot := strings.TrimSpace(profile.Runtime.Workspace.ProjectsRoot); projectsRoot != "" {
		if cleaned, err := cleanAgentProfileWorkspace(projectsRoot, userID); err == nil {
			roots = append(roots, agentProfileRuntimeWorkspace(userID, cleaned))
		}
	}
	for _, root := range roots {
		prefix := strings.TrimSuffix(filepath.ToSlash(filepath.Clean("/"+root)), "/") + "/"
		if clean == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}

// validateSlackConnectionScope checks an admin-assigned scope names a real
// workflow or product project. Workflow scopes need a workflow manifest;
// product scopes need the project's runtime manifest to exist already
// (creation flows use ensure-style writes instead, so fixed-workspace
// products without a manifest yet are unaffected).
func validateSlackConnectionScope(ctx context.Context, workspacePath, profileID string) error {
	workspacePath = strings.TrimSpace(workspacePath)
	profileID = strings.TrimSpace(profileID)
	if workspacePath == "" {
		return nil
	}
	if profileID == "" {
		if _, exists, err := ReadWorkflowManifest(ctx, workspacePath); err != nil || !exists {
			return fmt.Errorf("slack connection scope names an unknown workflow")
		}
		return nil
	}
	if _, found, err := readProjectRuntimeManifest(ctx, profileID, workspacePath); err != nil || !found {
		return fmt.Errorf("slack connection scope names an unknown product project")
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
		profileID := ""
		if req.ProfileID != nil {
			profileID = strings.TrimSpace(*req.ProfileID)
		}
		if err := requireSlackConnectionCreateAccess(r, api, workspacePath, profileID); err != nil {
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
			ProfileID:     profileID,
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
		if err := requireSlackConnectionAccess(r, api, current); err != nil {
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
		profileID := current.ProfileID
		if req.WorkspacePath != nil || req.ProfileID != nil {
			if !currentUserIsAdmin(r) {
				http.Error(w, "only a platform admin may change a Slack connection's scope", http.StatusForbidden)
				return
			}
			if req.WorkspacePath != nil {
				workspacePath = strings.TrimSpace(*req.WorkspacePath)
			}
			if req.ProfileID != nil {
				profileID = strings.TrimSpace(*req.ProfileID)
			}
			if workspacePath == "" {
				profileID = ""
			} else if err := validateSlackConnectionScope(r.Context(), workspacePath, profileID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		conn, err := svc.UpdateSlackConnection(r.Context(), id, services.SlackConnectionInput{
			DisplayName:   req.DisplayName,
			BotToken:      req.BotToken,
			AppToken:      req.AppToken,
			Enabled:       enabled,
			WorkspacePath: workspacePath,
			ProfileID:     profileID,
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
		if err := requireSlackConnectionAccess(r, api, current); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if ref := slackConnectionWorkflowReference(r.Context(), id); ref != "" {
			http.Error(w, fmt.Sprintf("slack connection is still selected by workflow %q; point the workflow elsewhere first", ref), http.StatusConflict)
			return
		}
		if ref := slackConnectionProjectReference(r.Context(), current, id); ref != "" {
			http.Error(w, fmt.Sprintf("slack connection is still selected by product project %q; point the project elsewhere first", ref), http.StatusConflict)
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

// validateSlackConnectionSelection fails a save that selects an unknown
// Slack connection. Empty (inherit the default) always passes, as does any
// selection when the registry cannot be read: an infra outage must not
// block unrelated edits, and sends fail loudly anyway.
func validateSlackConnectionSelection(connID string) error {
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

// validateWorkflowSlackConnectionID fails a manifest save that selects an
// unknown Slack connection.
func validateWorkflowSlackConnectionID(connID string) error {
	return validateSlackConnectionSelection(connID)
}

// productSlackConnectionID reads capabilities.slack_connection_id from a
// product project's runtime manifest. A missing manifest or field means
// "inherit the platform default", never an error.
func productSlackConnectionID(ctx context.Context, profileID, workspacePath string) (string, error) {
	raw, found, err := readProjectRuntimeManifest(ctx, profileID, workspacePath)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return "", fmt.Errorf("decode product manifest: %w", err)
	}
	capabilities, _ := manifest["capabilities"].(map[string]interface{})
	selected, _ := capabilities["slack_connection_id"].(string)
	return strings.TrimSpace(selected), nil
}

// updateProductSlackConnectionID sets capabilities.slack_connection_id on a
// product project's runtime manifest, creating the manifest when the
// product creates its state on first use. Empty clears the selection.
func updateProductSlackConnectionID(ctx context.Context, profileID, workspacePath, connID string) error {
	connID = strings.TrimSpace(connID)
	if err := validateSlackConnectionSelection(connID); err != nil {
		return err
	}
	raw, manifestPath, err := ensureProjectRuntimeManifest(ctx, profileID, workspacePath)
	if err != nil {
		return err
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return fmt.Errorf("decode product manifest: %w", err)
	}
	capabilities, _ := manifest["capabilities"].(map[string]interface{})
	if capabilities == nil {
		capabilities = map[string]interface{}{}
	}
	if connID == "" {
		delete(capabilities, "slack_connection_id")
	} else {
		capabilities["slack_connection_id"] = connID
	}
	manifest["capabilities"] = capabilities
	manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileToWorkspace(ctx, manifestPath, string(encoded)+"\n")
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

// slackConnectionProjectReference names the product project (label
// preferred) that still selects the connection, or "" when nothing
// references it. Only the connection's own project manifest is checked:
// product workspaces are per-user resources, so unlike workflows there is
// no global manifest listing to scan, and a cross-project selection strands
// sends loudly rather than silently. Lookup failures fail closed.
func slackConnectionProjectReference(ctx context.Context, conn services.SlackConnection, connID string) string {
	profileID := strings.TrimSpace(conn.ProfileID)
	workspacePath := strings.TrimSpace(conn.WorkspacePath)
	if profileID == "" || workspacePath == "" {
		return ""
	}
	raw, found, err := readProjectRuntimeManifest(ctx, profileID, workspacePath)
	if err != nil {
		log.Printf("[SLACK] project discovery failed during connection delete: %v", err)
		return "unknown (project manifest unreadable)"
	}
	if !found {
		return ""
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		log.Printf("[SLACK] project manifest decode failed during connection delete: %v", err)
		return "unknown (project manifest unreadable)"
	}
	capabilities, _ := manifest["capabilities"].(map[string]interface{})
	selected, _ := capabilities["slack_connection_id"].(string)
	if strings.TrimSpace(selected) != connID {
		return ""
	}
	if label, _ := manifest["label"].(string); strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	if id, _ := manifest["id"].(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return workspacePath
}

// projectSlackConnectionHandler reads or writes a product project's Slack
// app selection: the crew mirror of the workflow manifest's
// slack_connection_id. Reads need product access; writes need product
// ownership (or admin).
func projectSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		claims := GetUserFromContext(r.Context())
		if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" {
			http.Error(w, "only an authenticated interactive user may manage project Slack apps", http.StatusForbidden)
			return
		}
		profileID := strings.TrimSpace(r.URL.Query().Get("profile_id"))
		workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
		if profileID == "" || workspacePath == "" {
			http.Error(w, "profile_id and workspace_path are required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if api == nil || api.agentProfiles == nil {
				http.Error(w, "agent profiles are unavailable", http.StatusInternalServerError)
				return
			}
			profile, err := api.agentProfiles.Resolve(profileID, 0, productWorkspaceUserID(r.Context()))
			if err != nil {
				http.Error(w, fmt.Sprintf("agent profile %q is unavailable", profileID), http.StatusBadRequest)
				return
			}
			if !userAllowedProduct(claims, profile.Product) && !userAccessForClaims(claims).Admin {
				http.Error(w, "product access denied", http.StatusForbidden)
				return
			}
			selected, err := productSlackConnectionID(r.Context(), profileID, workspacePath)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to read project Slack selection: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"slack_connection_id": selected})
		case http.MethodPut, http.MethodPost:
			var req struct {
				SlackConnectionID string `json:"slack_connection_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
				return
			}
			if err := requireProductSlackScopeOwner(r.Context(), api, profileID, workspacePath); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			if err := updateProductSlackConnectionID(r.Context(), profileID, workspacePath, req.SlackConnectionID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			selected, err := productSlackConnectionID(r.Context(), profileID, workspacePath)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to read project Slack selection: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"slack_connection_id": selected})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
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
		if err := requireSlackConnectionAccess(r, api, current); err != nil {
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
		// Product route: the project's own selection, else the default.
		// Lookup failures resolve to the default rather than failing the
		// send: explicit selections are validated at save time, and the
		// default path fails loudly on its own when unusable.
		if strings.TrimSpace(route.ProfileID) == "" {
			return ""
		}
		selected, err := productSlackConnectionID(ctx, route.ProfileID, route.WorkspacePath)
		if err != nil {
			log.Printf("[SLACK] project selection lookup failed: %v", err)
			return ""
		}
		return selected
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
