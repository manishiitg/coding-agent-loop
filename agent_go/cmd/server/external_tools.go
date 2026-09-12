package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	planops "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type externalTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	mutates     bool
	plan        bool
	validator   *jsonschema.Schema
}

var externalCatalogOnce sync.Once
var externalCatalog []externalTool
var externalCatalogErr error

func externalString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description, "minLength": 1}
}
func externalInteger(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}
func externalTools() ([]externalTool, error) {
	externalCatalogOnce.Do(func() {
		add := func(name, description string, write, scoped bool, props map[string]any, required ...string) {
			if props == nil {
				props = map[string]any{}
			}
			if scoped {
				props["workflow_id"] = externalString("Workflow ID returned by list_workflows. Never a filesystem path.")
				required = append(required, "workflow_id")
			}
			req := make([]any, len(required))
			for i, r := range required {
				req[i] = r
			}
			externalCatalog = append(externalCatalog, externalTool{Name: name, Description: description, mutates: write, InputSchema: map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}})
		}
		page := func() map[string]any {
			return map[string]any{"limit": externalInteger(1, 200), "offset": externalInteger(0, 10000)}
		}
		p := page()
		p["query"] = externalString("Filter workflow labels and IDs.")
		add("list_workflows", "List workflows visible to the signed-in user.", false, false, p)
		add("get_workflow", "Read one workflow manifest and the caller's access level.", false, true, nil)
		for _, name := range []string{"list_files", "search_files"} {
			p = page()
			p["path"] = externalString("Workflow-relative directory; defaults to root.")
			p["depth"] = externalInteger(1, 8)
			required := []string{}
			if name == "search_files" {
				p["query"] = externalString("Case-insensitive literal text to find.")
				required = append(required, "query")
			}
			add(name, "Browse or search workflow files. Private paths and symbolic links are excluded. Results are bounded and paginated.", false, true, p, required...)
		}
		add("get_file_link", "Get an existing asset’s browser preview URL, authenticated download URL, size and content type. Works for large PDFs, images and videos. Links never contain credentials; recipients need workflow access.", false, true, map[string]any{"path": externalString("Workflow-relative file path.")}, "path")
		add("read_file", "Read a workflow file up to 2 MiB, with a revision. Binary content is base64. Missing files return revision 'missing'.", false, true, map[string]any{"path": externalString("Workflow-relative file path.")}, "path")
		for _, name := range []string{"write_file", "patch_file"} {
			p = map[string]any{"path": externalString("Workflow-relative file path."), "expected_revision": externalString("Revision returned by read_file; use 'missing' to create.")}
			field := "content"
			if name == "patch_file" {
				field = "diff"
			}
			p[field] = map[string]any{"type": "string", "description": "UTF-8 content, or a unified diff for patch_file.", "maxLength": wf.MaxFileBytes}
			add(name, "Edit an ordinary workflow file with revision checking. Plan/config, run state, ownership and private files are protected; use typed tools for plan changes.", true, true, p, "path", "expected_revision", field)
		}
		add("get_plan", "Read the plan and configuration with a combined revision required by typed plan mutations.", false, true, nil)
		add("list_runs", "List saved run folders and their metadata files. Use get_run for a chosen run.", false, true, page())
		for _, name := range []string{"get_run", "get_logs"} {
			p = page()
			p["run_folder"] = externalString("Run directory relative to runs/, e.g. iteration-0/group-name.")
			add(name, "Inspect a saved run's files or log files; use read_file to retrieve selected content.", false, true, p, "run_folder")
		}
		add("builder_chat", "Send a message to the existing Workflow Builder runtime. Returns a session ID; poll builder_status. Starts an LLM turn with normal builder capabilities, which may include execution. Omit session_id to resume your latest conversation.", false, true, map[string]any{"message": externalString("Message for Workflow Builder."), "session_id": externalString("Your existing builder session ID."), "provider": externalString("Optional provider override."), "model_id": externalString("Optional model override.")}, "message")
		add("builder_status", "Read bounded builder progress/events and requests for input. Return the cursor as since_index on the next call.", false, true, map[string]any{"session_id": externalString("Your builder session ID."), "since_index": externalInteger(-1, 2147483647), "limit": externalInteger(1, 200)}, "session_id")
		add("builder_reply_input", "Answer a pending human-input request in your builder session. Use request_id from builder_status pending_inputs; a chat message does not answer a blocked tool request.", false, true, map[string]any{"session_id": externalString("Your builder session ID."), "request_id": externalString("Pending input unique_id from builder_status."), "response": externalString("Response to the pending input request.")}, "session_id", "request_id", "response")
		add("builder_cancel", "Cancel your active builder turn using existing session cancellation.", false, true, map[string]any{"session_id": externalString("Your builder session ID.")}, "session_id")
		for _, definition := range planops.ExternalPlanToolDefinitions() {
			var schema map[string]any
			if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
				externalCatalogErr = err
				return
			}
			props, _ := schema["properties"].(map[string]any)
			if props == nil {
				props = map[string]any{}
				schema["properties"] = props
			}
			props["workflow_id"] = externalString("Workflow ID from list_workflows.")
			props["expected_revision"] = externalString("Combined revision returned by get_plan. A stale revision is rejected.")
			required, _ := schema["required"].([]any)
			schema["required"] = append(required, "workflow_id", "expected_revision")
			schema["additionalProperties"] = false
			externalCatalog = append(externalCatalog, externalTool{Name: definition.Name, Description: definition.Description, InputSchema: schema, mutates: true, plan: true})
		}
		for i := range externalCatalog {
			tool := &externalCatalog[i]
			compiler := jsonschema.NewCompiler()
			uri := "https://agentworks.invalid/schemas/" + tool.Name
			if err := compiler.AddResource(uri, tool.InputSchema); err != nil {
				externalCatalogErr = err
				return
			}
			validator, err := compiler.Compile(uri)
			if err != nil {
				externalCatalogErr = err
				return
			}
			tool.validator = validator
		}
	})
	return externalCatalog, externalCatalogErr
}
func externalError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func externalJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (api *StreamingAPI) handleExternalTools(w http.ResponseWriter, r *http.Request) {
	if GetUserFromContext(r.Context()) == nil {
		externalError(w, 401, "unauthorized", "Sign in to AgentWorks.")
		return
	}
	catalog, err := externalTools()
	if err != nil {
		externalError(w, 500, "schema_error", err.Error())
		return
	}
	allowed := make([]externalTool, 0, len(catalog))
	for _, tool := range catalog {
		if externalTokenAllows(GetUserFromContext(r.Context()), tool) {
			allowed = append(allowed, tool)
		}
	}
	externalJSON(w, map[string]any{"tools": allowed})
}
func (api *StreamingAPI) handleExternalCall(w http.ResponseWriter, r *http.Request) {
	if GetUserFromContext(r.Context()) == nil {
		externalError(w, 401, "unauthorized", "Sign in to AgentWorks.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&call); err != nil {
		externalError(w, 400, "invalid_arguments", err.Error())
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		externalError(w, 400, "invalid_arguments", "Expected one JSON object.")
		return
	}
	if call.Arguments == nil {
		call.Arguments = map[string]any{}
	}
	catalog, err := externalTools()
	if err != nil {
		externalError(w, 500, "schema_error", err.Error())
		return
	}
	var tool *externalTool
	for i := range catalog {
		if catalog[i].Name == call.Name {
			tool = &catalog[i]
			break
		}
	}
	if tool == nil {
		externalError(w, 404, "unknown_tool", "Tool is not exposed by this API.")
		return
	}
	if !externalTokenAllows(GetUserFromContext(r.Context()), *tool) {
		externalError(w, 403, "insufficient_scope", "This access token does not allow this operation.")
		return
	}
	if err = tool.validator.Validate(call.Arguments); err != nil {
		externalError(w, 400, "invalid_arguments", err.Error())
		return
	}
	discovered, err := DiscoverWorkflowManifests(r.Context())
	if err != nil {
		externalError(w, 502, "workspace_unavailable", err.Error())
		return
	}
	visible := filterWorkflowManifestsForUser(GetUserFromContext(r.Context()), discovered)
	if token := GetUserFromContext(r.Context()).AccessToken; token != nil {
		filtered := make([]DiscoveredWorkflow, 0, len(visible))
		for _, workflow := range visible {
			if workflow.Manifest != nil && token.AllowsWorkflow(workflow.Manifest.ID) {
				filtered = append(filtered, workflow)
			}
		}
		visible = filtered
	}
	args := call.Arguments
	if tool.Name == "list_workflows" {
		matches := make([]DiscoveredWorkflow, 0)
		query := strings.ToLower(externalArg(args, "query"))
		for _, item := range visible {
			if item.Manifest != nil && (query == "" || strings.Contains(strings.ToLower(item.Manifest.Label+" "+item.Manifest.ID), query)) {
				matches = append(matches, item)
			}
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].Manifest.ID < matches[j].Manifest.ID })
		start := min(externalInt(args, "offset", 0), len(matches))
		end := min(start+externalInt(args, "limit", 100), len(matches))
		externalJSON(w, map[string]any{"workflows": matches[start:end], "total": len(matches), "next_offset": end, "has_more": end < len(matches)})
		return
	}
	var selected *DiscoveredWorkflow
	for i := range visible {
		if visible[i].Manifest != nil && visible[i].Manifest.ID == externalArg(args, "workflow_id") {
			if selected != nil {
				externalError(w, 409, "ambiguous_workflow", "Duplicate workflow IDs must be resolved in AgentWorks.")
				return
			}
			selected = &visible[i]
		}
	}
	if selected == nil {
		externalError(w, 404, "workflow_not_found", "Workflow does not exist or is not accessible.")
		return
	}
	access := workflowAccessForManifest(GetUserFromContext(r.Context()), selected.Manifest)
	if tool.mutates && access != WorkflowAccessOwner && access != WorkflowAccessWrite {
		externalError(w, 403, "forbidden", "Workflow write access is required.")
		return
	}
	// Conversation access follows the builder runtime: workflow readers may
	// chat with its existing read-only tool policy, and control their own turns.
	if strings.HasPrefix(tool.Name, "builder_") {
		api.externalBuilderCall(w, r, tool.Name, args, *selected)
		return
	}
	if tool.Name == "get_file_link" {
		api.externalAssetLink(w, r, *selected, externalArg(args, "path"))
		return
	}
	if tool.Name == "get_workflow" {
		externalJSON(w, selected)
		return
	}
	lock := externalWorkflowLock(selected.WorkspacePath)
	lock.Lock()
	defer lock.Unlock()
	if tool.mutates && api.externalWorkflowBusy(selected.WorkspacePath) {
		externalError(w, 409, "workflow_busy", "A workflow run or builder turn is active; retry after it finishes.")
		return
	}
	if tool.Name == "get_plan" || tool.plan {
		api.externalPlanCall(w, r, *tool, args, *selected)
		return
	}
	api.externalFileCall(w, r, tool.Name, args, *selected)
}

var externalWorkflowLocks [64]sync.Mutex

func externalWorkflowLock(root string) *sync.Mutex {
	sum := 0
	for _, c := range root {
		sum = (sum*31 + int(c)) % len(externalWorkflowLocks)
	}
	return &externalWorkflowLocks[sum]
}
func (api *StreamingAPI) externalWorkflowBusy(root string) bool {
	api.activeSessionsMux.RLock()
	for _, s := range api.activeSessions {
		if s != nil && s.WorkspacePath == root && (s.Status == "running" || s.Status == "paused") {
			api.activeSessionsMux.RUnlock()
			return true
		}
	}
	api.activeSessionsMux.RUnlock()
	api.trackedWorkflowExecutionsMux.RLock()
	defer api.trackedWorkflowExecutionsMux.RUnlock()
	for _, e := range api.trackedWorkflowExecutions {
		if e != nil && e.WorkspacePath == root && e.Status == trackedExecutionStatusRunning {
			return true
		}
	}
	return false
}
func externalArg(args map[string]any, name string) string { s, _ := args[name].(string); return s }
func externalInt(args map[string]any, name string, fallback int) int {
	if n, ok := args[name].(float64); ok {
		return int(n)
	}
	return fallback
}
func externalFileRequest(ctx context.Context, req wf.Request) (wf.Result, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return wf.Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, getWorkspaceAPIURL()+"/api/workflow-files", bytes.NewReader(data))
	if err != nil {
		return wf.Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Workspace-Token", os.Getenv("WORKSPACE_API_TOKEN"))
	response, err := workspaceHTTPClient.Do(request)
	if err != nil {
		return wf.Result{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return wf.Result{}, err
	}
	if response.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = "Workspace service request failed"
		}
		return wf.Result{}, &externalUpstreamError{response.StatusCode, e.Error}
	}
	var result wf.Result
	err = json.Unmarshal(raw, &result)
	return result, err
}

type externalUpstreamError struct {
	status  int
	message string
}

func (e *externalUpstreamError) Error() string { return e.message }
func externalFailure(w http.ResponseWriter, err error) {
	status := 502
	code := "workspace_error"
	var upstream *externalUpstreamError
	if errors.As(err, &upstream) {
		status = upstream.status
		switch status {
		case 409:
			code = "revision_conflict"
		case 403:
			code = "protected_path"
		case 400:
			code = "invalid_arguments"
		case 404:
			code = "not_found"
		case 413:
			code = "too_large"
		}
	}
	externalError(w, status, code, err.Error())
}
func (api *StreamingAPI) externalFileCall(w http.ResponseWriter, r *http.Request, name string, args map[string]any, workflow DiscoveredWorkflow) {
	req := wf.Request{Root: workflow.WorkspacePath, Path: externalArg(args, "path"), Query: externalArg(args, "query"), Content: externalArg(args, "content"), Diff: externalArg(args, "diff"), ExpectedRevision: externalArg(args, "expected_revision"), Offset: externalInt(args, "offset", 0), Limit: externalInt(args, "limit", 100), Depth: externalInt(args, "depth", 4)}
	switch name {
	case "read_file":
		req.Operation = "read"
	case "list_files":
		req.Operation = "list"
	case "search_files":
		req.Operation = "search"
	case "write_file":
		req.Operation = "write"
	case "patch_file":
		req.Operation = "patch"
	case "list_runs":
		req.Operation = "list"
		req.Path = "runs"
		req.Depth = 2
	case "get_run", "get_logs":
		folder, e := wf.CleanRelative(externalArg(args, "run_folder"))
		if e != nil {
			externalError(w, 400, "invalid_arguments", e.Error())
			return
		}
		req.Operation = "list"
		req.Path = path.Join("runs", folder)
		req.Depth = 4
		if name == "get_logs" {
			req.Path = path.Join(req.Path, "logs")
		}
	}
	result, err := externalFileRequest(r.Context(), req)
	if err != nil {
		externalFailure(w, err)
		return
	}
	if name == "write_file" || name == "patch_file" {
		log.Printf("[EXTERNAL_API] user=%s tool=%s workflow=%s path=%s revision=%s", GetUserIDFromContext(r.Context()), name, workflow.Manifest.ID, req.Path, result.Revision)
	}
	// File responses include the existing Share file viewer URL.
	if result.Exists {
		raw, _ := json.Marshal(result)
		var linked map[string]any
		_ = json.Unmarshal(raw, &linked)
		linked["workflow_id"] = workflow.Manifest.ID
		linked["preview_url"] = sharedAssetURL(r, path.Join(workflow.WorkspacePath, result.Path))
		externalJSON(w, linked)
		return
	}
	externalJSON(w, result)
}

var externalPlanPaths = []string{"planning/plan.json", "planning/step_config.json", "evaluation/evaluation_plan.json", "evaluation/step_config.json"}

type externalPlanTransaction struct {
	ctx    context.Context
	root   string
	checks map[string]string
	files  map[string]wf.File
	writes map[string]string
	err    error
}

func newExternalPlanTransaction(ctx context.Context, root string) *externalPlanTransaction {
	return &externalPlanTransaction{ctx: ctx, root: root, checks: map[string]string{}, files: map[string]wf.File{}, writes: map[string]string{}}
}
func (tx *externalPlanTransaction) relative(p string) (string, error) {
	p = strings.TrimPrefix(p, tx.root+"/")
	clean, e := wf.CleanRelative(p)
	if e != nil || clean == "." || strings.HasPrefix(clean, "Workflow/") {
		return "", fmt.Errorf("plan tool requested a path outside its workflow")
	}
	if wf.Private(clean) {
		return "", fmt.Errorf("plan tool requested a private path")
	}
	return clean, nil
}
func (tx *externalPlanTransaction) load(p string) (wf.File, error) {
	p, e := tx.relative(p)
	if e != nil {
		return wf.File{}, e
	}
	if f, ok := tx.files[p]; ok {
		return f, nil
	}
	result, e := externalFileRequest(tx.ctx, wf.Request{Root: tx.root, Operation: "read", Path: p, Managed: true})
	if e != nil {
		return wf.File{}, e
	}
	tx.files[p] = result.File
	tx.checks[p] = result.Revision
	return result.File, nil
}
func (tx *externalPlanTransaction) read(ctx context.Context, p string) (string, error) {
	p, e := tx.relative(p)
	if e != nil {
		return "", e
	}
	if content, ok := tx.writes[p]; ok {
		return content, nil
	}
	f, e := tx.load(p)
	if e != nil {
		return "", e
	}
	if !f.Exists {
		return "", fmt.Errorf("%w: %s", os.ErrNotExist, p)
	}
	if f.Encoding == "base64" {
		return "", fmt.Errorf("plan tool requested a binary file")
	}
	return f.Content, nil
}
func (tx *externalPlanTransaction) write(ctx context.Context, p, content string) error {
	p, e := tx.relative(p)
	if e == nil {
		_, e = tx.load(p)
	}
	if e != nil {
		tx.err = errors.Join(tx.err, e)
		return e
	}
	tx.writes[p] = content
	return nil
}
func (tx *externalPlanTransaction) snapshot() (map[string]any, string, error) {
	out := map[string]any{}
	var revisions strings.Builder
	for _, p := range externalPlanPaths {
		f, e := tx.load(p)
		if e != nil {
			return nil, "", e
		}
		revisions.WriteString(p + ":" + f.Revision + "\n")
		var value any
		if f.Exists {
			if e = json.Unmarshal([]byte(f.Content), &value); e != nil {
				return nil, "", fmt.Errorf("invalid JSON in %s: %w", p, e)
			}
		}
		out[p] = value
	}
	return out, wf.Revision([]byte(revisions.String())), nil
}
func (api *StreamingAPI) externalPlanCall(w http.ResponseWriter, r *http.Request, tool externalTool, args map[string]any, workflow DiscoveredWorkflow) {
	ctx := context.WithValue(r.Context(), common.ChatSessionIDKey, "external-"+GetUserIDFromContext(r.Context())+"-"+uuid.NewString())
	tx := newExternalPlanTransaction(ctx, workflow.WorkspacePath)
	artifacts, revision, err := tx.snapshot()
	if err != nil {
		externalFailure(w, err)
		return
	}
	if tool.Name == "get_plan" {
		externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "revision": revision, "plan": artifacts["planning/plan.json"], "artifacts": artifacts})
		return
	}
	if revision != externalArg(args, "expected_revision") {
		externalError(w, 409, "revision_conflict", "Plan/configuration changed. Call get_plan and review the current state before retrying.")
		return
	}
	native := map[string]any{}
	for k, v := range args {
		if k != "workflow_id" && k != "expected_revision" {
			native[k] = v
		}
	}
	ctx = planops.WithExternalPlanSelectedServers(ctx, workflow.Manifest.Capabilities.SelectedServers)
	result, err := planops.ExecuteExternalPlanTool(ctx, tool.Name, native, workflow.WorkspacePath, createServerLogger(), tx.read, tx.write, func(context.Context, string, string) error {
		return fmt.Errorf("file moves are not supported by this plan transaction")
	})
	if err != nil {
		externalError(w, 400, "plan_validation_failed", err.Error())
		return
	}
	if tx.err != nil {
		externalFailure(w, tx.err)
		return
	}
	if _, err = externalFileRequest(ctx, wf.Request{Root: workflow.WorkspacePath, Operation: "commit", Managed: true, Checks: tx.checks, Writes: tx.writes}); err != nil {
		externalFailure(w, err)
		return
	}
	for p, content := range tx.writes {
		tx.files[p] = wf.File{Path: p, Exists: true, Content: content, Encoding: "utf-8", Revision: wf.Revision([]byte(content))}
	}
	_, newRevision, err := tx.snapshot()
	if err != nil {
		externalFailure(w, err)
		return
	}
	log.Printf("[EXTERNAL_API] user=%s tool=%s workflow=%s old_revision=%s revision=%s", GetUserIDFromContext(r.Context()), tool.Name, workflow.Manifest.ID, revision, newRevision)
	externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "message": result, "revision": newRevision, "changed_files": len(tx.writes)})
}
