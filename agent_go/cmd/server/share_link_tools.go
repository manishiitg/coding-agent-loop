package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
)

// registerShareLinkTools exposes the same authenticated preview links as the
// workspace UI. A link identifies an artifact; it never grants access.
func (api *StreamingAPI) registerShareLinkTools(reg definitionToolRegistrar, userID, workspace string) error {
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	if err := reg.RegisterCustomTool("get_file_link", "Create an authenticated AgentWorks preview link for an existing non-dashboard file or folder in the active workflow. Pass a canonical workflow-relative path such as runs/latest/report.pdf or runs/latest; the server validates current workflow access, existence, protected-path rules, and whether the target is a file or folder. A request for db/reports/index.html is rejected because dashboards must use get_report_link and the dedicated report runtime. Present the returned url value verbatim; never manually build, rewrite, or Base64-encode a /file or /folder URL. Inspect shareable and warning in the result: when PUBLIC_URL is localhost or another loopback host, the URL is a same-machine preview only and must not be described as shareable. The URL contains no credential and grants no access: every recipient must sign in and already have access to this workflow. This is not publishing and cannot create anonymous links or share arbitrary web URLs.", map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"path"},
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "Canonical path relative to the active workflow root. Do not prefix it with Workflow/<id>."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = context.WithValue(ctx, UserContextKey, &UserClaims{UserID: userID})
		if userAccessForClaims(&UserClaims{UserID: userID}).Disabled {
			return "", fmt.Errorf("access denied: disabled account")
		}
		if _, err := authorizeWorkflowContextPaths(ctx, []string{workspace}); err != nil {
			return "", fmt.Errorf("workflow is unavailable or access denied")
		}

		raw, _ := args["path"].(string)
		relative := strings.TrimSpace(raw)
		clean, err := wf.CleanRelative(relative)
		if err != nil || clean == "." || clean != relative || strings.HasPrefix(clean, "Workflow/") {
			return "", fmt.Errorf("path must be a canonical path relative to the active workflow")
		}
		if wf.Private(clean) {
			return "", fmt.Errorf("private workspace files are not shareable")
		}
		if clean == "db/reports/index.html" {
			return "", fmt.Errorf("db/reports/index.html is a live dashboard; use get_report_link so it opens in the dedicated report runtime")
		}

		return createSecureShareLink(ctx, workspace, workspace, clean, "", "Recipient must sign in to AgentWorks and already have access to this workflow. The link contains no credential and grants no access.")
	}, "workflow_files"); err != nil {
		return err
	}

	return reg.RegisterCustomTool("get_report_link", "Create an authenticated AgentWorks dashboard link for the active workflow's db/reports/index.html. The server validates current workflow access and confirms that the report exists. Present the returned url value verbatim; never manually build, rewrite, or Base64-encode a /report URL. Inspect shareable and warning in the result: when PUBLIC_URL is localhost or another loopback host, the URL is a same-machine preview only and must not be described as shareable. The dedicated report viewer preserves the dashboard's styling, tabs, live data API, file actions, and refresh behavior. The URL contains no credential and grants no access: every recipient must sign in and already have access to this workflow. This is secure internal sharing, not anonymous public publishing.", map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]interface{}{},
	}, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		ctx = context.WithValue(ctx, UserContextKey, &UserClaims{UserID: userID})
		if userAccessForClaims(&UserClaims{UserID: userID}).Disabled {
			return "", fmt.Errorf("access denied: disabled account")
		}
		if _, err := authorizeWorkflowContextPaths(ctx, []string{workspace}); err != nil {
			return "", fmt.Errorf("workflow is unavailable or access denied")
		}
		return createSecureReportLink(ctx, workspace, workspace, "", "Recipient must sign in to AgentWorks and already have access to this workflow. The link contains no credential and grants no access.")
	}, "workflow_files")
}

// registerWorkShareLinkTool exposes share links for the active Work project.
// Work projects are personal today, so uid binds the URL to the same signed-in
// account instead of pretending that the URL grants another user access.
func (api *StreamingAPI) registerWorkShareLinkTool(reg definitionToolRegistrar, userID, workspace string) error {
	cleanWorkspace, err := cleanAgentProfileWorkspace(workspace, userID)
	if err != nil || cleanWorkspace != workspace || !isActiveWorkProjectWorkspace(userID, cleanWorkspace) {
		return fmt.Errorf("Work share links require an active Work project")
	}
	canonicalWorkspace := canonicalChatHistoryWorkspacePath(userID, cleanWorkspace)
	physicalRoot := agentProfileRuntimeWorkspace(userID, canonicalWorkspace)
	if err := reg.RegisterCustomTool("get_file_link", "Create an authenticated preview link for an existing file or folder in the active Work project. Pass a canonical project-relative path; the server validates existence, protected-path rules, and whether the target is a file or folder. Present the returned url value verbatim; never manually build, rewrite, or Base64-encode a /file or /folder URL. Inspect shareable and warning in the result: when PUBLIC_URL is localhost or another loopback host, the URL is a same-machine preview only and must not be described as shareable. The URL contains no credential and grants no access. Work projects are personal: the link can currently be opened only by the same signed-in Work account. This is not public publishing and cannot share arbitrary web URLs or files outside this project.", map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"path"},
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "Canonical path relative to the active Work project."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, _ := args["path"].(string)
		relative := strings.TrimSpace(raw)
		clean, err := wf.CleanRelative(relative)
		if err != nil || clean == "." || clean != relative {
			return "", fmt.Errorf("path must be a canonical path relative to the active Work project")
		}
		if wf.Private(clean) {
			return "", fmt.Errorf("private workspace files are not shareable")
		}
		if clean == "db/reports/index.html" {
			return "", fmt.Errorf("db/reports/index.html is a live dashboard; use get_report_link so it opens in the dedicated report runtime")
		}
		return createSecureShareLink(ctx, physicalRoot, canonicalWorkspace, clean, userID, "The link contains no credential and grants no access. It can currently be opened only by this same signed-in Work account.")
	}, "work_files"); err != nil {
		return err
	}

	return reg.RegisterCustomTool("get_report_link", "Create an authenticated AgentWorks dashboard link for the active Work project's db/reports/index.html. The server validates that the dashboard exists and emits the canonical project URL even when this is a resumed chat with an expanded _users path. Present the returned url value verbatim; never manually build, rewrite, or Base64-encode a /report URL. Inspect shareable and warning in the result: when PUBLIC_URL is localhost or another loopback host, the URL is a same-machine preview only and must not be described as shareable. The dedicated report viewer preserves the dashboard's styling, tabs, live project data, file actions, and refresh behavior. The URL contains no credential and grants no access. Work projects are personal: it can be opened only by the same signed-in Work account.", map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]interface{}{},
	}, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		return createSecureReportLink(ctx, physicalRoot, canonicalWorkspace, userID, "The link contains no credential and grants no access. It can currently be opened only by this same signed-in Work account.")
	}, "work_files")
}

func createSecureReportLink(ctx context.Context, metadataRoot, linkRoot, userID, authentication string) (string, error) {
	const reportPath = "db/reports/index.html"
	metadata, err := sharedAssetMetadata(ctx, metadataRoot, reportPath)
	if err != nil {
		return "", fmt.Errorf("project report is unavailable: %w", err)
	}
	if kind, _ := metadata["type"].(string); kind != "file" {
		return "", fmt.Errorf("project report is not a file")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/")
	if publicURL == "" {
		return "", fmt.Errorf("PUBLIC_URL is not configured on this server; cannot create a report link")
	}
	previewURL := sharedAssetPublicURLForUser(publicURL, "report", linkRoot, userID)
	result := map[string]interface{}{
		"kind":           "report",
		"path":           reportPath,
		"url":            previewURL,
		"preview_url":    previewURL,
		"authentication": authentication,
	}
	addShareabilityMetadata(result, publicURL)
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("cannot encode report link metadata: %w", err)
	}
	return string(encoded), nil
}

func createSecureShareLink(ctx context.Context, metadataRoot, linkRoot, relative, userID, authentication string) (string, error) {
	metadata, err := sharedAssetMetadata(ctx, metadataRoot, relative)
	if err != nil {
		return "", err
	}
	kind, _ := metadata["type"].(string)
	if kind != "file" && kind != "folder" {
		return "", fmt.Errorf("asset service returned an unsupported target type")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/")
	if publicURL == "" {
		return "", fmt.Errorf("PUBLIC_URL is not configured on this server; cannot create a share link")
	}
	metadata["path"] = relative
	metadata["kind"] = kind
	previewURL := sharedAssetPublicURLForUser(publicURL, kind, path.Join(linkRoot, relative), userID)
	metadata["url"] = previewURL
	metadata["preview_url"] = previewURL
	metadata["authentication"] = authentication
	addShareabilityMetadata(metadata, publicURL)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("cannot encode share link metadata: %w", err)
	}
	return string(encoded), nil
}

func addShareabilityMetadata(result map[string]interface{}, publicURL string) {
	parsed, err := url.Parse(publicURL)
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	ip := net.ParseIP(hostname)
	localOnly := err != nil || hostname == "" || hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || (ip != nil && ip.IsLoopback())
	result["shareable"] = !localOnly
	if localOnly {
		result["scope"] = "local_machine"
		result["warning"] = "PUBLIC_URL uses localhost or a loopback address. This URL works only from the machine running AgentWorks and is not shareable with another user or device. Configure a reachable deployment PUBLIC_URL before offering a shareable link."
		return
	}
	result["scope"] = "deployment"
}

func sharedAssetMetadata(ctx context.Context, root, relative string) (map[string]any, error) {
	body, err := json.Marshal(map[string]string{"root": root, "path": relative, "operation": "stat"})
	if err != nil {
		return nil, err
	}
	req, err := newSharedAssetMetadataRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	response, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asset service unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("asset is unavailable or not shareable (status %d)", response.StatusCode)
	}
	var metadata map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("asset service returned invalid metadata")
	}
	return metadata, nil
}

func newSharedAssetMetadataRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", getWorkspaceAPIURL()+"/api/shared-assets", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-Token", os.Getenv("WORKSPACE_API_TOKEN"))
	req.Header.Set("X-Asset-Method", "POST")
	return req, nil
}

func sharedAssetPublicURL(baseURL, kind, full string) string {
	return sharedAssetPublicURLForUser(baseURL, kind, full, "")
}

func sharedAssetPublicURLForUser(baseURL, kind, full, userID string) string {
	q := url.Values{"path": {base64.StdEncoding.EncodeToString([]byte(full))}}
	if strings.TrimSpace(userID) != "" {
		q.Set("uid", userID)
	}
	return strings.TrimRight(baseURL, "/") + "/" + kind + "?" + q.Encode()
}
