package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

// workspaceProxyHandler creates an http.Handler that reverse-proxies to the workspace API.
// It strips the /api/wp prefix so /api/wp/api/documents → WORKSPACE_API_URL/api/documents.
// Auth is enforced by the router's AuthMiddleware (applied to all /api/* routes).
func workspaceProxyHandler() http.Handler {
	wsURL := os.Getenv("WORKSPACE_API_URL")
	if wsURL == "" {
		wsURL = "http://localhost:8080"
	}

	target, err := url.Parse(wsURL)
	if err != nil {
		log.Printf("[WORKSPACE PROXY] Invalid WORKSPACE_API_URL: %v", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "workspace proxy misconfigured", http.StatusBadGateway)
		})
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// The agent server's own CORS middleware answers the browser; the
	// workspace server adds its own permissive headers too, and a response
	// carrying two Access-Control-Allow-Origin values is rejected by every
	// browser. Strip the upstream's so only ours remain.
	proxy.ModifyResponse = func(resp *http.Response) error {
		for name := range resp.Header {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				resp.Header.Del(name)
			}
		}
		return nil
	}
	log.Printf("[WORKSPACE PROXY] Proxying /api/wp/* → %s", wsURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A browser may only address its own private tree. The workspace
		// server resolves explicit _users/<id>/... paths verbatim with no
		// identity of its own, so the proxy — the one hop that knows the
		// JWT claims — refuses any cross-user addressing in the URL, in
		// query params, and in JSON body path fields. Crew Run mode
		// depends on this: reader file access goes through the mediated
		// crew endpoints, never through raw cross-user proxy reads, and
		// no proxied call may write another user's tree.
		if claims := GetUserFromContext(r.Context()); claims != nil {
			if blocked, reason := workspaceProxyCrossUserBlock(r, claims.UserID); blocked {
				log.Printf("[WORKSPACE PROXY] Denied cross-user workspace access for %q: %s", claims.UserID, reason)
				http.Error(w, "cross-user workspace access denied", http.StatusForbidden)
				return
			}
		}
		// Keep the retired workflow-files path closed, and never expose the
		// internal shared-assets endpoint through the generic proxy.
		if internalPath := path.Clean("/" + workspaceProxyRelativePath(r)); internalPath == "/api/workflow-files" || internalPath == "/api/shared-assets" {
			http.NotFound(w, r)
			return
		}
		// Live browser access must go through workflow ownership and input gating.
		if strings.HasPrefix(workspaceProxyRelativePath(r), "api/browser/live/") {
			http.NotFound(w, r)
			return
		}
		if isWorkflowWorkspaceProxyWrite(r) {
			if !currentUserCanWriteWorkflows(r) {
				writeWorkflowPermissionDenied(w, "write")
				return
			}
			// Inside an existing workflow's folder only its owners may
			// write; a new folder under Workflow/ is creation, covered by
			// the account tier above.
			if folder := workflowFolderFromWorkspaceProxyPath(workspaceProxyRelativePath(r)); folder != "" {
				if level, manifest := workflowAccessForWorkspacePath(r.Context(), GetUserFromContext(r.Context()), folder); manifest != nil && level != WorkflowAccessOwner && level != WorkflowAccessWrite {
					writeWorkflowPermissionDenied(w, "owner")
					return
				}
			}
		}
		// The workspace API scopes per-user paths by X-User-ID and has no
		// auth of its own; it must carry the identity this server verified,
		// never whatever the browser put in the header.
		r.Header.Set("X-User-ID", GetUserIDFromContext(r.Context()))
		// Strip /api/wp prefix: /api/wp/api/documents → /api/documents
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/wp")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.URL.RawPath = ""
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	})
}

// workspaceProxyRelativePath is the decoded path below /api/wp, without a
// leading slash: "api/documents/Workflow/<folder>/plan.json".
func workspaceProxyRelativePath(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api/wp")
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return strings.TrimPrefix(path, "/")
}

// workspaceProxyCrossUserBlock reports whether a proxied browser request
// addresses another user's private tree. The check mirrors the workspace
// server's own resolution: explicit _users/<id>/... paths resolve
// verbatim, while logical per-user paths resolve under the stamped
// X-User-ID and are always the caller's own.
func workspaceProxyCrossUserBlock(r *http.Request, callerID string) (bool, string) {
	own := sanitizeUserIDForPath(callerID)
	if workspaceProxyURLIsOtherUser(workspaceProxyRelativePath(r), own) {
		return true, "url path"
	}
	query := r.URL.Query()
	for _, key := range []string{"folder", "pattern", "db_path", "path", "filepath", "file_path", "source_path", "destination_path"} {
		for _, value := range query[key] {
			if workspaceProxyPathIsOtherUser(value, own) {
				return true, "query param " + key
			}
		}
	}
	if blocked, reason := workspaceProxyBodyIsOtherUser(r, own); blocked {
		return true, reason
	}
	return false, ""
}

// workspaceProxyRoutePathPrefixes are workspace routes whose trailing path
// parameter is a workspace path. The check strips the route before testing
// the workspace path itself, mirroring how the workspace server resolves
// it: a leading _users/<id>/ addresses that user's tree verbatim, while
// logical per-user paths resolve under the stamped X-User-ID.
var workspaceProxyRoutePathPrefixes = []string{
	"api/documents/",
	"api/folders/",
	"api/versions/",
	"api/restore/",
}

func workspaceProxyURLIsOtherUser(rel, own string) bool {
	remainder := strings.Trim(rel, "/")
	for _, prefix := range workspaceProxyRoutePathPrefixes {
		if after, ok := strings.CutPrefix(remainder, prefix); ok {
			remainder = after
			break
		}
	}
	return workspaceProxyPathIsOtherUser(remainder, own)
}

// workspaceProxyPathIsOtherUser matches values that address another
// user's tree: the bare _users root, or _users/<segment>/... with a
// segment that is not the caller's own.
func workspaceProxyPathIsOtherUser(raw, own string) bool {
	clean := strings.Trim(path.Clean("/"+strings.TrimSpace(raw)), "/")
	if clean == "_users" {
		return true
	}
	if !strings.HasPrefix(clean, "_users/") {
		return false
	}
	segment := strings.SplitN(strings.TrimPrefix(clean, "_users/"), "/", 2)[0]
	return segment != "" && segment != own
}

// workspaceProxyBodyPathFields are the JSON field names that carry
// workspace paths (folder creation, move/copy, database paths, guard
// paths, shell working directories). Command and content fields are
// deliberately absent: only path-typed fields are inspected, so patch
// text or shell commands mentioning _users/ never false-positive.
var workspaceProxyBodyPathFields = map[string]bool{
	"folder": true, "folder_path": true, "filepath": true, "file_path": true,
	"path": true, "source_path": true, "destination_path": true,
	"source": true, "destination": true, "db_path": true,
	"workspace_path": true,
	"working_directory": true, "working_dir": true,
	"read_paths": true, "write_paths": true, "blocked_paths": true, "blocked_write_paths": true,
}

// workspaceProxyMultipartPathFields are the multipart form fields that
// carry workspace paths (file upload destination, backup import target).
var workspaceProxyMultipartPathFields = map[string]bool{
	"folder_path": true, "workspace_path": true,
}

const workspaceProxyBodyInspectCap = 8 << 20

// workspaceProxyMultipartScanCap bounds how much of a multipart body is
// buffered while scanning its path fields. Field parts come before file
// content, so small text fields are always seen; the scanned prefix is
// replayed ahead of the untouched remainder, so allowed uploads stream
// through byte-identical.
const workspaceProxyMultipartScanCap = 4 << 20

// workspaceProxyBodyIsOtherUser scans a request body for cross-user path
// fields, restoring the body for the upstream request. JSON bodies are
// decoded; multipart bodies are scanned part by part. Anything else — and
// anything unreadable — is left to the URL and query checks.
func workspaceProxyBodyIsOtherUser(r *http.Request, own string) (bool, string) {
	if r.Body == nil || r.ContentLength == 0 {
		return false, ""
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "json") {
		return workspaceProxyJSONBodyIsOtherUser(r, own)
	}
	if strings.HasPrefix(contentType, "multipart/") {
		return workspaceProxyMultipartBodyIsOtherUser(r, own)
	}
	return false, ""
}

func workspaceProxyJSONBodyIsOtherUser(r *http.Request, own string) (bool, string) {
	if r.ContentLength < 0 || r.ContentLength > workspaceProxyBodyInspectCap {
		return false, ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, workspaceProxyBodyInspectCap+1))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	if err != nil || len(raw) > workspaceProxyBodyInspectCap {
		return false, ""
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false, ""
	}
	if workspaceProxyJSONAddressesOtherUser(decoded, own) {
		return true, "request body path"
	}
	return false, ""
}

func workspaceProxyMultipartBodyIsOtherUser(r *http.Request, own string) (bool, string) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		return false, ""
	}
	recorded := &bytes.Buffer{}
	// Tee the stream so the scanned prefix replays ahead of the untouched
	// remainder: allowed requests forward byte-identical.
	partReader := multipart.NewReader(io.TeeReader(r.Body, recorded), params["boundary"])
	blocked := false
	for recorded.Len() <= workspaceProxyMultipartScanCap {
		part, err := partReader.NextPart()
		if err != nil {
			break
		}
		if part.FileName() == "" && workspaceProxyMultipartPathFields[part.FormName()] {
			value, err := io.ReadAll(io.LimitReader(part, workspaceProxyMultipartScanCap+1))
			if err != nil {
				break
			}
			if workspaceProxyPathIsOtherUser(string(value), own) {
				blocked = true
				break
			}
			continue
		}
		// Drain anything else (file content, unrelated fields) under the
		// same budget; the drain is recorded for replay.
		budget := int64(workspaceProxyMultipartScanCap - recorded.Len() + 1)
		if budget < 0 {
			budget = 0
		}
		if _, err := io.CopyN(io.Discard, part, budget); err != nil && err != io.EOF {
			break
		}
	}
	if blocked {
		return true, "multipart form path"
	}
	r.Body = io.NopCloser(io.MultiReader(recorded, r.Body))
	return false, ""
}

func workspaceProxyJSONAddressesOtherUser(node any, own string) bool {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if !workspaceProxyBodyPathFields[key] {
				if workspaceProxyJSONAddressesOtherUser(child, own) {
					return true
				}
				continue
			}
			switch held := child.(type) {
			case string:
				if workspaceProxyPathIsOtherUser(held, own) {
					return true
				}
			case []any:
				for _, entry := range held {
					if text, ok := entry.(string); ok && workspaceProxyPathIsOtherUser(text, own) {
						return true
					}
				}
			}
		}
	case []any:
		for _, entry := range value {
			if workspaceProxyJSONAddressesOtherUser(entry, own) {
				return true
			}
		}
	}
	return false
}

func isWorkflowWorkspaceProxyWrite(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}

	path := workspaceProxyRelativePath(r)

	return strings.HasPrefix(path, "api/documents/Workflow/") ||
		path == "api/documents/Workflow" ||
		strings.HasPrefix(path, "api/folders/Workflow/") ||
		path == "api/folders/Workflow"
}
