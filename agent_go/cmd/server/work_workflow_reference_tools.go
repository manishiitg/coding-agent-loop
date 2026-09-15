package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

func updateWorkSessionWorkflowGuard(sessionID string, add []string, remove ...string) {
	cfg := common.GetSessionShellConfig(sessionID)
	if cfg == nil {
		return
	}
	removeSet := map[string]bool{}
	for _, path := range remove {
		removeSet[strings.TrimSuffix(strings.TrimSpace(path), "/")] = true
	}
	reads := make([]string, 0, len(cfg.ReadPaths)+len(add))
	for _, path := range cfg.ReadPaths {
		if !removeSet[strings.TrimSuffix(strings.TrimSpace(path), "/")] {
			reads = append(reads, path)
		}
	}
	reads = appendUniqueStrings(reads, add...)
	common.SetSessionFolderGuard(sessionID, reads, cfg.WritePaths)
}

// registerWorkWorkflowReferenceTools lets the Work assistant discover the
// same authorized workflow set as the AgentWorks picker and persist an exact
// read-only reference in workflow.json. Names are never accepted for mutation:
// the model must first list, disambiguate, and use the returned workspace path.
func (api *StreamingAPI) registerWorkWorkflowReferenceTools(registrar definitionToolRegistrar, userID, sessionID, workspacePath string) error {
	register := func(name, description string, parameters map[string]interface{}, execute func(context.Context, map[string]interface{}) (string, error)) error {
		return registrar.RegisterCustomTool(name, description, parameters, execute, "work_workflow_reference_tools")
	}
	claims := &UserClaims{UserID: strings.TrimSpace(userID)}
	if record := directoryUserFor(userID, "", ""); record != nil {
		claims.Username = record.Username
		claims.Email = record.Email
	}
	withClaims := func(ctx context.Context) context.Context {
		copy := *claims
		return context.WithValue(ctx, UserContextKey, &copy)
	}

	if err := register("list_accessible_workflows", "Search AgentWorks workflows the current user may read. Use this before suggesting or attaching a workflow; an empty query lists all accessible workflows.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Optional case-insensitive name, id, or path search."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		discovered, err := DiscoverWorkflowManifests(ctx)
		if err != nil {
			return "", err
		}
		visible := filterWorkflowManifestsForUser(claims, discovered)
		attached, err := readWorkWorkflowReferences(ctx, workspacePath)
		if err != nil {
			return "", err
		}
		attachedSet := make(map[string]bool, len(attached))
		for _, path := range attached {
			attachedSet[path] = true
		}
		query, _ := args["query"].(string)
		query = strings.ToLower(strings.TrimSpace(query))
		items := make([]map[string]interface{}, 0, len(visible))
		for _, workflow := range visible {
			label := strings.TrimSpace(workflow.Manifest.Label)
			id := strings.TrimSpace(workflow.Manifest.ID)
			path := strings.TrimSuffix(strings.TrimSpace(workflow.WorkspacePath), "/")
			haystack := strings.ToLower(strings.Join([]string{label, id, path}, "\n"))
			if query != "" && !strings.Contains(haystack, query) {
				continue
			}
			items = append(items, map[string]interface{}{
				"id": id, "label": label, "workspace_path": path,
				"access": string(workflow.MyAccess), "attached": attachedSet[path],
			})
		}
		sort.Slice(items, func(i, j int) bool {
			left := strings.ToLower(fmt.Sprint(items[i]["label"], "\n", items[i]["workspace_path"]))
			right := strings.ToLower(fmt.Sprint(items[j]["label"], "\n", items[j]["workspace_path"]))
			return left < right
		})
		encoded, err := json.MarshalIndent(map[string]interface{}{"workflows": items}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	if err := register("attach_workflow_reference", "Attach one accessible AgentWorks workflow to this Work project as durable read-only context. Pass only an exact workspace_path returned by list_accessible_workflows, and call only after the user explicitly asks to attach it.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workspace_path": map[string]interface{}{"type": "string", "description": "Exact Workflow/<folder> path returned by list_accessible_workflows."},
		},
		"required": []string{"workspace_path"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		path, _ := args["workspace_path"].(string)
		authorized, err := authorizeWorkflowContextPaths(ctx, []string{path})
		if err != nil || len(authorized) != 1 {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("workflow reference is unavailable")
		}
		paths, err := updateWorkWorkflowReferences(ctx, workspacePath, func(existing []string) ([]string, error) {
			return appendUniqueStrings(existing, authorized[0]), nil
		})
		if err != nil {
			return "", err
		}
		updateWorkSessionWorkflowGuard(sessionID, paths)
		api.emitAgentProfileEvent(sessionID, map[string]interface{}{"type": "work_workflow_references_updated", "workflow_context_paths": paths})
		encoded, err := json.MarshalIndent(map[string]interface{}{
			"workflow_context_paths": paths,
			"note":                   "The workflow is saved as read-only project context and file access is active now.",
		}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	return register("detach_workflow_reference", "Detach one durable AgentWorks workflow reference from this Work project. Pass the exact saved workspace_path.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workspace_path": map[string]interface{}{"type": "string"},
		},
		"required": []string{"workspace_path"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		path, _ := args["workspace_path"].(string)
		path = strings.TrimSuffix(strings.TrimSpace(path), "/")
		paths, err := updateWorkWorkflowReferences(ctx, workspacePath, func(existing []string) ([]string, error) {
			kept := make([]string, 0, len(existing))
			found := false
			for _, current := range existing {
				if current == path {
					found = true
					continue
				}
				kept = append(kept, current)
			}
			if !found {
				return nil, fmt.Errorf("workflow reference %q is not attached", path)
			}
			return kept, nil
		})
		if err != nil {
			return "", err
		}
		updateWorkSessionWorkflowGuard(sessionID, paths, path)
		api.emitAgentProfileEvent(sessionID, map[string]interface{}{"type": "work_workflow_references_updated", "workflow_context_paths": paths})
		return "Workflow reference detached. Access is revoked now.", nil
	})
}

func readWorkWorkflowReferences(ctx context.Context, workspacePath string) ([]string, error) {
	productRaw, productExists, err := readFileFromWorkspace(ctx, strings.TrimSuffix(strings.TrimSpace(workspacePath), "/")+"/product.json")
	if err != nil {
		return nil, err
	}
	if !productExists {
		return nil, fmt.Errorf("Work product manifest does not exist")
	}
	var productManifest productProjectManifest
	if err := json.Unmarshal([]byte(productRaw), &productManifest); err != nil {
		return nil, fmt.Errorf("decode Work product manifest: %w", err)
	}
	if strings.TrimSpace(productManifest.Product) != "work" {
		return nil, fmt.Errorf("workspace is not a Work project")
	}
	raw, _, err := readProjectRuntimeManifest(ctx, "work", workspacePath)
	if err != nil {
		return nil, err
	}
	var manifest productProjectManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil, fmt.Errorf("decode Work runtime manifest: %w", err)
	}
	return canonicalRuntimeSelection(firstNonEmptyStrings(manifest.WorkflowContextPaths, manifest.Capabilities.WorkflowContextPaths)), nil
}

func updateWorkWorkflowReferences(ctx context.Context, workspacePath string, mutate func([]string) ([]string, error)) ([]string, error) {
	manifestPath := projectRuntimeManifestPath("work", workspacePath)
	mutex := productConversationRegistryMutex(manifestPath)
	mutex.Lock()
	defer mutex.Unlock()

	raw, _, err := ensureProjectRuntimeManifest(ctx, "work", workspacePath)
	if err != nil {
		return nil, err
	}
	var typed productProjectManifest
	if err := json.Unmarshal([]byte(raw), &typed); err != nil {
		return nil, fmt.Errorf("decode Work product manifest: %w", err)
	}
	next, err := mutate(canonicalRuntimeSelection(firstNonEmptyStrings(typed.WorkflowContextPaths, typed.Capabilities.WorkflowContextPaths)))
	if err != nil {
		return nil, err
	}
	next = canonicalRuntimeSelection(next)

	var document map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return nil, fmt.Errorf("decode Work product manifest fields: %w", err)
	}
	document["workflow_context_paths"] = next
	document["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Work product manifest: %w", err)
	}
	if err := writeFileToWorkspace(ctx, manifestPath, string(encoded)+"\n"); err != nil {
		return nil, err
	}
	return next, nil
}
