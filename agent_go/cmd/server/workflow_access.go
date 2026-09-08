package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
)

// Per-workflow ownership and sharing (phase 3 of
// docs/design/user_accounts_and_workflow_sharing.md).
//
// A workflow carries an `access` block in its manifest: owners, who may edit,
// run, share and delete it, and readers, who get exactly the PLAT-262
// read-only session (chat, run, watch, inspect; no mutating tools, no shell
// writes). The creator is the first owner. There is deliberately no editor
// tier — to let someone edit, make them an owner.
//
// Resolution order for a request against a workflow:
//   1. an account admin is owner of everything;
//   2. a manifest with an ownership record (an access block, or a created_by
//      stamp) answers from its lists, except that a read-only ACCOUNT is
//      never more than a reader even when listed as owner;
//   3. a legacy manifest with neither keeps today's behavior: the account's
//      own tier (members write, read-only accounts read), so an existing
//      deployment sees no change until someone claims or shares a workflow.

// WorkflowAccess is the manifest's `access` block.
type WorkflowAccess struct {
	Owners  []string `json:"owners"`
	Readers []string `json:"readers"`
}

// WorkflowAccessNone is "may not see this workflow at all".
const WorkflowAccessNone WorkflowAccessLevel = "none"

func (m *WorkflowManifest) effectiveOwners() []string {
	if m == nil {
		return nil
	}
	if m.Access != nil && len(m.Access.Owners) > 0 {
		return m.Access.Owners
	}
	if strings.TrimSpace(m.CreatedBy) != "" {
		return []string{m.CreatedBy}
	}
	return nil
}

func (m *WorkflowManifest) effectiveReaders() []string {
	if m == nil || m.Access == nil {
		return nil
	}
	return m.Access.Readers
}

// hasOwnershipRecord reports whether anyone has ever been recorded as owning
// this workflow; without one the legacy account-tier answer applies.
func (m *WorkflowManifest) hasOwnershipRecord() bool {
	return len(m.effectiveOwners()) > 0
}

func containsID(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// workflowAccessForManifest is the one answer to "what may this user do with
// this workflow".
func workflowAccessForManifest(claims *UserClaims, m *WorkflowManifest) WorkflowAccessLevel {
	account := workflowAccessForClaims(claims)
	if claims == nil {
		return account
	}
	if userAccessForClaims(claims).Admin {
		return WorkflowAccessOwner
	}
	if m == nil || !m.hasOwnershipRecord() {
		return account
	}
	if containsID(m.effectiveOwners(), claims.UserID) {
		if !userAccessForClaims(claims).CanEdit {
			return WorkflowAccessRead
		}
		return WorkflowAccessOwner
	}
	if containsID(m.effectiveReaders(), claims.UserID) {
		return WorkflowAccessRead
	}
	return WorkflowAccessNone
}

// workflowAccessForWorkspacePath reads the manifest at workspacePath and
// resolves access. A folder without a manifest (a workflow being created)
// answers with the account tier and a nil manifest.
func workflowAccessForWorkspacePath(ctx context.Context, claims *UserClaims, workspacePath string) (WorkflowAccessLevel, *WorkflowManifest) {
	manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !exists {
		return workflowAccessForManifest(claims, nil), nil
	}
	return workflowAccessForManifest(claims, manifest), manifest
}

func currentUserWorkflowAccess(r *http.Request, workspacePath string) WorkflowAccessLevel {
	level, _ := workflowAccessForWorkspacePath(r.Context(), GetUserFromContext(r.Context()), workspacePath)
	return level
}

// requireWorkflowOwner writes a 403 and returns false unless the caller may
// edit the workflow at workspacePath.
func requireWorkflowOwner(w http.ResponseWriter, r *http.Request, workspacePath string) bool {
	level := currentUserWorkflowAccess(r, workspacePath)
	if level == WorkflowAccessOwner || level == WorkflowAccessWrite {
		return true
	}
	writeWorkflowPermissionDenied(w, "owner")
	return false
}

// requireWorkflowVisible writes a 403 and returns false unless the caller
// may at least see the workflow at workspacePath.
func requireWorkflowVisible(w http.ResponseWriter, r *http.Request, workspacePath string) bool {
	if currentUserWorkflowAccess(r, workspacePath) != WorkflowAccessNone {
		return true
	}
	writeWorkflowPermissionDenied(w, "read")
	return false
}

// workflowFolderFromWorkspaceProxyPath extracts "Workflow/<folder>" from a
// workspace-API path such as "api/documents/Workflow/<folder>/plan.json" or
// "api/folders/Workflow/<folder>". Empty when the path is the Workflow root
// itself (creating a new folder).
func workflowFolderFromWorkspaceProxyPath(path string) string {
	for _, prefix := range []string{"api/documents/Workflow/", "api/folders/Workflow/"} {
		if strings.HasPrefix(path, prefix) {
			rest := strings.TrimPrefix(path, prefix)
			folder := rest
			if i := strings.Index(rest, "/"); i >= 0 {
				folder = rest[:i]
			}
			if folder != "" {
				return "Workflow/" + folder
			}
		}
	}
	return ""
}

// ---- share API ---------------------------------------------------------

type workflowAccessUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
}

type workflowAccessResponse struct {
	WorkspacePath string               `json:"workspace_path"`
	Owners        []workflowAccessUser `json:"owners"`
	Readers       []workflowAccessUser `json:"readers"`
	MyAccess      WorkflowAccessLevel  `json:"my_access"`
	// Legacy is true when nothing has been recorded yet and the workflow is
	// open to every member; saving any grant ends that.
	Legacy bool `json:"legacy"`
}

func describeUsers(ids []string, dir *userDirectory) []workflowAccessUser {
	out := make([]workflowAccessUser, 0, len(ids))
	for _, id := range ids {
		u := workflowAccessUser{ID: id, Username: id}
		if dir != nil {
			if rec := dir.byID(id); rec != nil {
				u.Username, u.Email = rec.Username, rec.Email
			}
		}
		out = append(out, u)
	}
	return out
}

// GET /api/workflow/access?workspace_path=Workflow/<folder>
func (api *StreamingAPI) handleGetWorkflowAccess(w http.ResponseWriter, r *http.Request) {
	workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
	if workspacePath == "" {
		writeUsersError(w, http.StatusBadRequest, "workspace_path is required")
		return
	}
	claims := GetUserFromContext(r.Context())
	level, manifest := workflowAccessForWorkspacePath(r.Context(), claims, workspacePath)
	if manifest == nil {
		writeUsersError(w, http.StatusNotFound, "no workflow at that path")
		return
	}
	if level == WorkflowAccessNone {
		writeWorkflowPermissionDenied(w, "read")
		return
	}
	dir, _ := loadUserDirectory()
	writeUsersJSON(w, http.StatusOK, workflowAccessResponse{
		WorkspacePath: workspacePath,
		Owners:        describeUsers(manifest.effectiveOwners(), dir),
		Readers:       describeUsers(manifest.effectiveReaders(), dir),
		MyAccess:      level,
		Legacy:        !manifest.hasOwnershipRecord(),
	})
}

// PUT /api/workflow/access {workspace_path, owners:[...], readers:[...]}
// Entries may be user ids, usernames, or emails; they are stored as ids. The
// caller must be an owner (or admin) and cannot remove the last owner.
func (api *StreamingAPI) handleSetWorkflowAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspacePath string   `json:"workspace_path"`
		Owners        []string `json:"owners"`
		Readers       []string `json:"readers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeUsersError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.WorkspacePath = strings.TrimSpace(req.WorkspacePath)
	if req.WorkspacePath == "" {
		writeUsersError(w, http.StatusBadRequest, "workspace_path is required")
		return
	}
	claims := GetUserFromContext(r.Context())
	level, manifest := workflowAccessForWorkspacePath(r.Context(), claims, req.WorkspacePath)
	if manifest == nil {
		writeUsersError(w, http.StatusNotFound, "no workflow at that path")
		return
	}
	if level != WorkflowAccessOwner && level != WorkflowAccessWrite {
		writeWorkflowPermissionDenied(w, "owner")
		return
	}
	dir, err := loadUserDirectory()
	if err != nil {
		writeUsersError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resolve := func(entries []string) ([]string, string) {
		seen := map[string]bool{}
		var ids []string
		for _, raw := range entries {
			key := strings.TrimSpace(raw)
			if key == "" {
				continue
			}
			rec := dir.find(key, key, key)
			if rec == nil {
				return nil, key
			}
			if !seen[rec.ID] {
				seen[rec.ID] = true
				ids = append(ids, rec.ID)
			}
		}
		sort.Strings(ids)
		return ids, ""
	}
	owners, unknown := resolve(req.Owners)
	if unknown != "" {
		writeUsersError(w, http.StatusBadRequest, "unknown user: "+unknown)
		return
	}
	readers, unknown := resolve(req.Readers)
	if unknown != "" {
		writeUsersError(w, http.StatusBadRequest, "unknown user: "+unknown)
		return
	}
	if len(owners) == 0 {
		writeUsersError(w, http.StatusBadRequest, "a workflow needs at least one owner")
		return
	}
	// A user is either an owner or a reader, never both.
	filtered := readers[:0]
	for _, id := range readers {
		if !containsID(owners, id) {
			filtered = append(filtered, id)
		}
	}
	manifest.Access = &WorkflowAccess{Owners: owners, Readers: filtered}
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		writeUsersError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("[ACCESS] %s set access on %s: owners=%v readers=%v", GetUserIDFromContext(r.Context()), req.WorkspacePath, owners, filtered)
	writeUsersJSON(w, http.StatusOK, workflowAccessResponse{
		WorkspacePath: req.WorkspacePath,
		Owners:        describeUsers(owners, dir),
		Readers:       describeUsers(filtered, dir),
		MyAccess:      workflowAccessForManifest(claims, manifest),
	})
}

// GET /api/users/directory — the picker behind sharing: every enabled
// account's id, username and email, nothing else. Any signed-in user may
// call it (a member needs it to share their own workflow).
func (api *StreamingAPI) handleUserDirectory(w http.ResponseWriter, r *http.Request) {
	dir, err := loadUserDirectory()
	if err != nil {
		writeUsersError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]workflowAccessUser, 0, len(dir.Users))
	for _, u := range dir.Users {
		if u.Disabled {
			continue
		}
		out = append(out, workflowAccessUser{ID: u.ID, Username: u.Username, Email: u.Email})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	writeUsersJSON(w, http.StatusOK, map[string]any{"users": out})
}

// ---- phase 4: session visibility ----------------------------------------

// sessionVisibleTo replaces the old "empty owner means everyone" rule. A
// session that recorded no owner (system-started: scheduler, bots) is visible
// to the machine's single user, or to admins in multi-user mode — never to
// every signed-in account.
func sessionVisibleTo(ownerID string, claims *UserClaims) bool {
	currentUserID := GetDefaultUserID()
	if claims != nil && claims.UserID != "" {
		currentUserID = claims.UserID
	}
	if ownerID == currentUserID {
		return true
	}
	if ownerID != "" {
		return false
	}
	if !IsMultiUserMode() {
		return true
	}
	return userAccessForClaims(claims).Admin
}
