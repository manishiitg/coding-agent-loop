package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
)

type WorkflowAccessLevel string

const (
	WorkflowAccessRead  WorkflowAccessLevel = "read"
	WorkflowAccessWrite WorkflowAccessLevel = "write"
	WorkflowAccessOwner WorkflowAccessLevel = "owner"
)

type WorkflowPermissionInfo struct {
	WorkflowAccess             WorkflowAccessLevel `json:"workflow_access"`
	CanRunWorkflows            bool                `json:"can_run_workflows"`
	CanWriteWorkflows          bool                `json:"can_write_workflows"`
	CanManageWorkflowAccess    bool                `json:"can_manage_workflow_access"`
	WorkflowPermissionSource   string              `json:"workflow_permission_source,omitempty"`
	WorkflowPermissionsEnabled bool                `json:"workflow_permissions_enabled"`
}

type workflowPermissionConfig struct {
	configured bool
	entries    map[string]WorkflowAccessLevel
}

func loadWorkflowPermissionConfig() workflowPermissionConfig {
	cfg := workflowPermissionConfig{entries: make(map[string]WorkflowAccessLevel)}

	parseList := func(envName string, level WorkflowAccessLevel) {
		raw := os.Getenv(envName)
		if strings.TrimSpace(raw) == "" {
			return
		}
		cfg.configured = true
		for _, item := range splitPermissionList(raw) {
			cfg.entries[normalizeWorkflowPermissionKey(item)] = level
		}
	}

	parseList("WORKFLOW_READ_USERS", WorkflowAccessRead)
	parseList("WORKFLOW_WRITE_USERS", WorkflowAccessWrite)
	parseList("WORKFLOW_OWNER_USERS", WorkflowAccessOwner)

	if raw := strings.TrimSpace(os.Getenv("WORKFLOW_USER_PERMISSIONS")); raw != "" {
		cfg.configured = true
		for _, item := range splitPermissionList(raw) {
			key, level, ok := parseWorkflowPermissionEntry(item)
			if ok {
				cfg.entries[key] = level
			}
		}
	}

	// File-backed grants override env. Failing to read the file is non-fatal —
	// we fall back to env-only behavior (logged elsewhere if needed).
	if persisted, err := readWorkflowUserPermissionsFile(); err == nil {
		for k, v := range persisted {
			cfg.configured = true
			cfg.entries[k] = v
		}
	}

	return cfg
}

func splitPermissionList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t'
	})
	items := make([]string, 0, len(fields))
	for _, field := range fields {
		if item := strings.TrimSpace(field); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func parseWorkflowPermissionEntry(entry string) (string, WorkflowAccessLevel, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", "", false
	}

	separator := strings.LastIndex(entry, "=")
	if separator < 0 {
		separator = strings.LastIndex(entry, ":")
	}
	if separator <= 0 || separator >= len(entry)-1 {
		return "", "", false
	}

	key := normalizeWorkflowPermissionKey(entry[:separator])
	level, ok := parseWorkflowAccessLevel(entry[separator+1:])
	return key, level, key != "" && ok
}

func parseWorkflowAccessLevel(raw string) (WorkflowAccessLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "owner", "admin", "manage", "manager":
		return WorkflowAccessOwner, true
	case "write", "writer", "edit", "editor", "builder", "optimizer":
		return WorkflowAccessWrite, true
	case "read", "reader", "run", "runner", "view", "viewer":
		return WorkflowAccessRead, true
	default:
		return "", false
	}
}

func normalizeWorkflowPermissionKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func workflowAccessForClaims(claims *UserClaims) WorkflowAccessLevel {
	if claims == nil {
		if loadWorkflowPermissionConfig().configured {
			return WorkflowAccessRead
		}
		return WorkflowAccessOwner
	}
	return workflowAccessForIdentity(claims.UserID, claims.Username, claims.Email)
}

func workflowAccessForIdentity(userID, username, email string) WorkflowAccessLevel {
	cfg := loadWorkflowPermissionConfig()
	if !cfg.configured {
		return WorkflowAccessOwner
	}

	for _, key := range []string{userID, username, email} {
		normalized := normalizeWorkflowPermissionKey(key)
		if normalized == "" {
			continue
		}
		if access, ok := cfg.entries[normalized]; ok {
			return access
		}
	}

	return WorkflowAccessRead
}

func workflowPermissionInfoForClaims(claims *UserClaims) WorkflowPermissionInfo {
	return workflowPermissionInfo(workflowAccessForClaims(claims))
}

func workflowPermissionInfo(access WorkflowAccessLevel) WorkflowPermissionInfo {
	cfg := loadWorkflowPermissionConfig()
	canWrite := access == WorkflowAccessWrite || access == WorkflowAccessOwner
	canManage := access == WorkflowAccessOwner
	return WorkflowPermissionInfo{
		WorkflowAccess:             access,
		CanRunWorkflows:            access == WorkflowAccessRead || canWrite,
		CanWriteWorkflows:          canWrite,
		CanManageWorkflowAccess:    canManage,
		WorkflowPermissionsEnabled: cfg.configured,
	}
}

func userInfoWithWorkflowPermissions(info UserInfo) UserInfo {
	access := workflowAccessForIdentity(info.ID, info.Username, info.Email)
	perms := workflowPermissionInfo(access)
	info.WorkflowAccess = string(perms.WorkflowAccess)
	info.CanRunWorkflows = perms.CanRunWorkflows
	info.CanWriteWorkflows = perms.CanWriteWorkflows
	info.CanManageWorkflowAccess = perms.CanManageWorkflowAccess
	return info
}

func workflowPermissionResponseFields(perms WorkflowPermissionInfo) map[string]interface{} {
	return map[string]interface{}{
		"workflow_access":              perms.WorkflowAccess,
		"can_run_workflows":            perms.CanRunWorkflows,
		"can_write_workflows":          perms.CanWriteWorkflows,
		"can_manage_workflow_access":   perms.CanManageWorkflowAccess,
		"workflow_permissions_enabled": perms.WorkflowPermissionsEnabled,
	}
}

func currentUserCanWriteWorkflows(r *http.Request) bool {
	return workflowPermissionInfoForClaims(GetUserFromContext(r.Context())).CanWriteWorkflows
}

func currentUserCanManageWorkflowAccess(r *http.Request) bool {
	return workflowPermissionInfoForClaims(GetUserFromContext(r.Context())).CanManageWorkflowAccess
}

func requireWorkflowWriteAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" || currentUserCanWriteWorkflows(r) {
			next(w, r)
			return
		}
		writeWorkflowPermissionDenied(w, "write")
	}
}

func requireWorkflowOwnerAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" || currentUserCanManageWorkflowAccess(r) {
			next(w, r)
			return
		}
		writeWorkflowPermissionDenied(w, "owner")
	}
}

func writeWorkflowPermissionDenied(w http.ResponseWriter, required string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":           "workflow permission denied",
		"required_access": required,
	})
}

func enforceWorkflowQueryAccess(r *http.Request, req *QueryRequest) bool {
	if req == nil {
		return true
	}
	if currentUserCanWriteWorkflows(r) {
		return true
	}

	workspaceMode := ""
	if req.ExecutionOptions != nil {
		workspaceMode = strings.ToLower(strings.TrimSpace(req.ExecutionOptions.WorkshopMode))
	}
	if workspaceMode == "builder" || workspaceMode == "optimizer" || workspaceMode == "reporting" {
		return false
	}

	if req.AgentMode != "workflow_phase" {
		return true
	}

	if workspaceMode == "run" || workspaceMode == "runner" {
		return true
	}

	return false
}

func (api *StreamingAPI) handleListAuthUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !currentUserCanManageWorkflowAccess(r) {
		writeWorkflowPermissionDenied(w, "owner")
		return
	}

	type authUserResponse struct {
		ID                      string `json:"id"`
		Username                string `json:"username"`
		Email                   string `json:"email,omitempty"`
		Provider                string `json:"provider"`
		WorkflowAccess          string `json:"workflow_access"`
		CanRunWorkflows         bool   `json:"can_run_workflows"`
		CanWriteWorkflows       bool   `json:"can_write_workflows"`
		CanManageWorkflowAccess bool   `json:"can_manage_workflow_access"`
	}

	users := GetHardcodedUsers()
	names := make([]string, 0, len(users))
	for name := range users {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]authUserResponse, 0, len(names))
	for _, name := range names {
		user := users[name]
		access := workflowAccessForIdentity(user.UserID, user.Username, "")
		perms := workflowPermissionInfo(access)
		out = append(out, authUserResponse{
			ID:                      user.UserID,
			Username:                user.Username,
			Provider:                "simple",
			WorkflowAccess:          string(perms.WorkflowAccess),
			CanRunWorkflows:         perms.CanRunWorkflows,
			CanWriteWorkflows:       perms.CanWriteWorkflows,
			CanManageWorkflowAccess: perms.CanManageWorkflowAccess,
		})
	}

	if len(out) == 0 {
		if claims := GetUserFromContext(r.Context()); claims != nil {
			perms := workflowPermissionInfoForClaims(claims)
			out = append(out, authUserResponse{
				ID:                      claims.UserID,
				Username:                claims.Username,
				Email:                   claims.Email,
				Provider:                claims.Provider,
				WorkflowAccess:          string(perms.WorkflowAccess),
				CanRunWorkflows:         perms.CanRunWorkflows,
				CanWriteWorkflows:       perms.CanWriteWorkflows,
				CanManageWorkflowAccess: perms.CanManageWorkflowAccess,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users": out,
		"total": len(out),
	})
}

type workflowUserPermissionResponse struct {
	UserKey        string `json:"user_key"`
	WorkflowAccess string `json:"workflow_access"`
}

func (api *StreamingAPI) handleListWorkflowUserPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	entries, err := listWorkflowUserPermissions()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read permissions: %v", err), http.StatusInternalServerError)
		return
	}
	out := make([]workflowUserPermissionResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, workflowUserPermissionResponse{
			UserKey:        e.UserKey,
			WorkflowAccess: string(e.Access),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"permissions": out,
		"total":       len(out),
	})
}

type workflowUserPermissionUpsertRequest struct {
	UserKey        string `json:"user_key"`
	WorkflowAccess string `json:"workflow_access"`
}

func (api *StreamingAPI) handleUpsertWorkflowUserPermission(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req workflowUserPermissionUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}
	key := normalizeWorkflowPermissionKey(req.UserKey)
	if key == "" {
		http.Error(w, "user_key is required", http.StatusBadRequest)
		return
	}
	level, ok := parseWorkflowAccessLevel(req.WorkflowAccess)
	if !ok {
		http.Error(w, "workflow_access must be one of read|write|owner", http.StatusBadRequest)
		return
	}
	if err := upsertWorkflowUserPermission(key, level); err != nil {
		http.Error(w, fmt.Sprintf("failed to save permission: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(workflowUserPermissionResponse{
		UserKey:        key,
		WorkflowAccess: string(level),
	})
}

func (api *StreamingAPI) handleDeleteWorkflowUserPermission(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	key := normalizeWorkflowPermissionKey(r.URL.Query().Get("user_key"))
	if key == "" {
		http.Error(w, "user_key query param is required", http.StatusBadRequest)
		return
	}
	if err := deleteWorkflowUserPermission(key); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete permission: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
