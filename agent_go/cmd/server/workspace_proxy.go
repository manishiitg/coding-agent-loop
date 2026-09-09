package server

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
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
