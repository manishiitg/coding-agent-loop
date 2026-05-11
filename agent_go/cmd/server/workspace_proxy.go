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
	log.Printf("[WORKSPACE PROXY] Proxying /api/wp/* → %s", wsURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWorkflowWorkspaceProxyWrite(r) && !currentUserCanWriteWorkflows(r) {
			writeWorkflowPermissionDenied(w, "write")
			return
		}
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

func isWorkflowWorkspaceProxyWrite(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/wp")
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	path = strings.TrimPrefix(path, "/")

	return strings.HasPrefix(path, "api/documents/Workflow/") ||
		path == "api/documents/Workflow" ||
		strings.HasPrefix(path, "api/folders/Workflow/") ||
		path == "api/folders/Workflow"
}
