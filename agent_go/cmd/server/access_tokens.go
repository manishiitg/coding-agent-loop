package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/accesstokens"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
)

func openAccessTokens() (*accesstokens.Store, error) {
	root, err := workflowCLIStateRoot()
	if err != nil {
		return nil, err
	}
	docs, err := filepath.Abs(fsutil.WorkspaceDocsRoot())
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(docs, root)
	if err != nil {
		return nil, err
	}
	if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("access token state must be outside workspace documents")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if actualDocs, e := filepath.EvalSymlinks(docs); e == nil {
		docs = actualDocs
	}
	rel, err = filepath.Rel(docs, resolved)
	if err != nil {
		return nil, err
	}
	if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("access token state must be outside workspace documents")
	}
	if err := ValidateConfiguredAuthSecret(); err != nil {
		return nil, err
	}
	// Bind storage to this server authority even if two local instances use
	// the same default state root. Secret rotation invalidates previous PATs.
	authority := sha256.Sum256(GetAuthSecret())
	return accesstokens.Open(filepath.Join(resolved, "auth", hex.EncodeToString(authority[:16])+".sqlite"))
}

// PAT identities are resolved against the current directory, not saved account
// permissions. Unlike legacy browser sessions, directory failures fail closed.
func accessTokenClaims(t accesstokens.Token) (*UserClaims, error) {
	c := &UserClaims{UserID: t.UserID, Username: t.Username, Email: t.Email, Provider: t.Provider, AccessToken: &t}
	if !IsMultiUserMode() {
		if c.UserID != GetDefaultUserID() {
			return nil, accesstokens.ErrInvalid
		}
		return c, nil
	}
	dir, err := readUserDirectoryFile()
	if err != nil {
		return nil, err
	}
	rec := dir.byID(t.UserID)
	if rec == nil || rec.Disabled {
		return nil, accesstokens.ErrInvalid
	}
	c.Username = rec.Username
	c.Email = rec.Email
	return c, nil
}

func authenticateAccessToken(w http.ResponseWriter, r *http.Request, raw string) (*UserClaims, bool) {
	if r.Header.Get("Authorization") == "" {
		externalError(w, 401, "unauthorized", "Access tokens must use the Authorization header.")
		return nil, false
	}
	if !((r.Method == "GET" && r.URL.Path == "/api/external/v1/tools") || (r.Method == "POST" && r.URL.Path == "/api/external/v1/call") || ((r.Method == "GET" || r.Method == "HEAD") && r.URL.Path == "/api/external/v1/files/content")) {
		externalError(w, 403, "forbidden", "Access tokens are valid only for the external tools API.")
		return nil, false
	}
	store, err := openAccessTokens()
	if err != nil {
		externalError(w, 503, "auth_unavailable", "Access token storage is unavailable.")
		return nil, false
	}
	defer store.Close()
	t, err := store.Authenticate(r.Context(), raw, time.Now())
	if err != nil {
		if errors.Is(err, accesstokens.ErrInvalid) {
			externalError(w, 401, "invalid_token", "Access token is invalid, expired, or revoked. Generate a replacement in your account menu.")
		} else {
			externalError(w, 503, "auth_unavailable", "Access token validation is unavailable.")
		}
		return nil, false
	}
	c, err := accessTokenClaims(t)
	if err != nil {
		externalError(w, 401, "invalid_token", "The access token's account is unavailable or disabled.")
		return nil, false
	}
	return c, true
}

func (api *StreamingAPI) handleAccessTokens(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	c := GetUserFromContext(r.Context())
	if c == nil || c.AccessToken != nil || c.Scope != "" {
		externalError(w, 403, "forbidden", "Use your app login to manage access tokens.")
		return
	}
	store, err := openAccessTokens()
	if err != nil {
		externalError(w, 503, "auth_unavailable", "Access token storage is unavailable.")
		return
	}
	defer store.Close()
	switch r.Method {
	case "GET":
		tokens, err := store.List(r.Context(), c.UserID)
		if err != nil {
			externalError(w, 503, "auth_unavailable", "Could not load access tokens.")
			return
		}
		externalJSON(w, map[string]any{"tokens": tokens})
	case "POST":
		var req struct {
			Name          string   `json:"name"`
			Scopes        []string `json:"scopes"`
			WorkflowIDs   []string `json:"workflow_ids"`
			AllWorkflows  bool     `json:"all_workflows"`
			ExpiresInDays int      `json:"expires_in_days"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		d.DisallowUnknownFields()
		if err := d.Decode(&req); err != nil {
			externalError(w, 400, "invalid_arguments", "Invalid access token request.")
			return
		}
		if d.Decode(new(any)) != io.EOF {
			externalError(w, 400, "invalid_arguments", "Expected one JSON object.")
			return
		}
		if req.ExpiresInDays < 1 || req.ExpiresInDays > 90 {
			externalError(w, 400, "invalid_arguments", "Choose an expiry between 1 and 90 days.")
			return
		}
		now := time.Now()
		t := accesstokens.Token{Name: req.Name, UserID: c.UserID, Username: c.Username, Email: c.Email, Provider: c.Provider, Scopes: req.Scopes, WorkflowIDs: req.WorkflowIDs, AllWorkflows: req.AllWorkflows, ExpiresAt: now.Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)}
		if err := accesstokens.Validate(t, now); err != nil {
			externalError(w, 400, "invalid_arguments", err.Error())
			return
		}
		if !t.AllWorkflows {
			workflows, err := DiscoverWorkflowManifests(r.Context())
			if err != nil {
				externalError(w, 502, "workspace_unavailable", "Cannot check workflow access.")
				return
			}
			visible := filterWorkflowManifestsForUser(c, workflows)
			allowed := map[string]bool{}
			for _, wf := range visible {
				if wf.Manifest != nil {
					allowed[wf.Manifest.ID] = true
				}
			}
			for _, id := range t.WorkflowIDs {
				if !allowed[id] {
					externalError(w, 403, "forbidden", "One or more selected workflows are not accessible.")
					return
				}
			}
		}
		token, raw, err := store.Issue(r.Context(), t, now)
		if err != nil {
			externalError(w, 503, "token_creation_failed", "Could not create token; at most 100 active tokens are allowed.")
			return
		}
		w.WriteHeader(http.StatusCreated)
		externalJSON(w, map[string]any{"token": raw, "access_token": token})
	case "DELETE":
		id := mux.Vars(r)["id"]
		if err := store.Revoke(r.Context(), id, c.UserID, time.Now()); err != nil {
			externalError(w, 404, "token_not_found", "Access token not found.")
			return
		}
		api.cancelAccessTokenSessions(id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func externalTokenAllows(c *UserClaims, tool externalTool) bool {
	if c == nil {
		return false
	}
	if c.AccessToken == nil {
		return true
	}
	t := c.AccessToken
	if strings.HasPrefix(tool.Name, "builder_") {
		return t.FullBuilderAccess()
	}
	if tool.plan {
		return t.Allows("plan:write")
	}
	switch tool.Name {
	case "list_files", "read_file", "search_files", "get_file_link":
		return t.Allows("files:read")
	case "write_file", "patch_file":
		return t.Allows("files:write")
	default:
		return t.Allows("workflows:read")
	}
}

// Only PAT-created conversations are controlled by a PAT. A full-access PAT
// cannot inherit a browser conversation (or one owned by another token).
func accessTokenSessionPrefix(c *UserClaims) string { return "pat-" + c.AccessToken.ID + "-" }

type accessTokenSession struct {
	tokenID, sessionID, workspace string
	access                        WorkflowAccessLevel
}
type accessTokenSessionRegistry struct {
	sync.Mutex
	sessions map[string]accessTokenSession
}

func (api *StreamingAPI) cancelAccessTokenSessions(id string) {
	api.accessTokenSessions.Lock()
	sessions := []string{}
	for sid, s := range api.accessTokenSessions.sessions {
		if s.tokenID == id {
			sessions = append(sessions, sid)
			delete(api.accessTokenSessions.sessions, sid)
		}
	}
	api.accessTokenSessions.Unlock()
	for _, sid := range sessions {
		api.cancelSessionRuntimeWork(sid, "access token revoked or expired", runtimePhaseCanceled)
	}
}

func (api *StreamingAPI) watchAccessTokenSession(c *UserClaims, sessionID, workspace string, access WorkflowAccessLevel) {
	api.accessTokenSessions.Lock()
	if api.accessTokenSessions.sessions == nil {
		api.accessTokenSessions.sessions = map[string]accessTokenSession{}
	}
	if _, ok := api.accessTokenSessions.sessions[sessionID]; ok {
		api.accessTokenSessions.Unlock()
		return
	}
	entry := accessTokenSession{c.AccessToken.ID, sessionID, workspace, access}
	api.accessTokenSessions.sessions[sessionID] = entry
	api.accessTokenSessions.Unlock()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			api.accessTokenSessions.Lock()
			_, exists := api.accessTokenSessions.sessions[sessionID]
			api.accessTokenSessions.Unlock()
			if !exists {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			allowed := accessTokenSessionAllowed(ctx, entry)
			cancel()
			if !allowed {
				api.cancelAccessTokenSessions(entry.tokenID)
				return
			}
		}
	}()
}

func accessTokenSessionAllowed(ctx context.Context, entry accessTokenSession) bool {
	store, err := openAccessTokens()
	if err != nil {
		return false
	}
	defer store.Close()
	token, err := store.Active(ctx, entry.tokenID, time.Now())
	if err != nil {
		return false
	}
	claims, err := accessTokenClaims(token)
	if err != nil {
		return false
	}
	level, manifest := workflowAccessForWorkspacePath(ctx, claims, entry.workspace)
	return manifest != nil && level == entry.access
}
