package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// External (MCP / agentworks CLI) access to Crews. Every tool resolves the
// Crew through the same server-wide Crew listing the app uses, then applies
// the token's Crew bound (crew_ids / all_crews). Files use the shared-Crew
// reader view, so private areas (builder/ transcripts, db/, manifests) are
// never exposed, whoever owns the Crew.

var externalCrewTools = map[string]bool{
	"list_crews": true, "get_crew": true, "list_crew_files": true, "read_crew_file": true, "list_crew_functions": true,
}

func isExternalCrewTool(name string) bool { return externalCrewTools[name] }

// externalCrewsVisible lists the Crews this connection may use.
func (api *StreamingAPI) externalCrewsVisible(ctx context.Context, claims *UserClaims, query string) ([]map[string]interface{}, error) {
	crews, err := listAccessibleCrewProjects(ctx, claims.UserID, strings.ToLower(strings.TrimSpace(query)))
	if err != nil {
		return nil, err
	}
	if claims.AccessToken == nil {
		return crews, nil
	}
	visible := make([]map[string]interface{}, 0, len(crews))
	for _, crew := range crews {
		if claims.AccessToken.AllowsCrew(fmt.Sprint(crew["id"])) {
			visible = append(visible, crew)
		}
	}
	return visible, nil
}

// externalCrewResolve returns the Crew binding for crew_id when this
// connection may use it. Unknown and out-of-bound Crews are one not-found.
func (api *StreamingAPI) externalCrewResolve(ctx context.Context, claims *UserClaims, crewID string) (crewProjectBinding, productProjectManifest, map[string]interface{}, bool) {
	crewID = strings.TrimSpace(crewID)
	if crewID == "" || api == nil || api.agentProfiles == nil {
		return crewProjectBinding{}, productProjectManifest{}, nil, false
	}
	crews, err := api.externalCrewsVisible(ctx, claims, "")
	if err != nil {
		return crewProjectBinding{}, productProjectManifest{}, nil, false
	}
	var summary map[string]interface{}
	for _, crew := range crews {
		if fmt.Sprint(crew["id"]) == crewID {
			summary = crew
			break
		}
	}
	if summary == nil {
		return crewProjectBinding{}, productProjectManifest{}, nil, false
	}
	profile, err := api.agentProfiles.Resolve("work", 0, claims.UserID)
	if err != nil || !userAllowedProduct(claims, profile.Product) {
		return crewProjectBinding{}, productProjectManifest{}, nil, false
	}
	crew, err := resolveCrewProjectBinding(ctx, claims.UserID, profile, crewID, "")
	if err != nil {
		return crewProjectBinding{}, productProjectManifest{}, nil, false
	}
	manifest, err := readCrewProjectManifests(ctx, profile.ID, crew.Binding.WorkspacePath)
	if err != nil || strings.TrimSpace(manifest.ID) != crewID {
		return crewProjectBinding{}, productProjectManifest{}, nil, false
	}
	return crew, manifest, summary, true
}

func externalCrewFunctionSummaries(ctx context.Context, crew crewProjectBinding, manifest productProjectManifest, label string) []map[string]interface{} {
	target := triggerTarget{Kind: triggerCallerCrew, Path: crew.Binding.WorkspacePath, Label: label, CrewID: manifest.ID, CrewProfile: "work", CrewOwner: crew.OwnerID}
	functions, err := readCrewFunctions(ctx, target)
	if err != nil {
		functions = nil
	}
	out := []map[string]interface{}{}
	for _, fn := range withDefaultAskFunction(functions) {
		out = append(out, map[string]interface{}{
			"name": fn.Name, "description": fn.Description,
			"input_schema": fn.InputSchema, "result_schema": fn.ResultSchema,
		})
	}
	return out
}

func (api *StreamingAPI) externalCrewCall(w http.ResponseWriter, r *http.Request, name string, args map[string]any) {
	ctx := r.Context()
	claims := GetUserFromContext(ctx)
	str := func(key string) string {
		value, _ := args[key].(string)
		return strings.TrimSpace(value)
	}
	if name == "list_crews" {
		crews, err := api.externalCrewsVisible(ctx, claims, str("query"))
		if err != nil {
			externalError(w, 502, "workspace_unavailable", "Cannot list Crews.")
			return
		}
		items := make([]map[string]interface{}, 0, len(crews))
		for _, crew := range crews {
			items = append(items, map[string]interface{}{
				"crew_id": crew["id"], "name": crew["name"], "identity": crew["identity_name"],
				"owner": crew["owner"], "access": crew["access"],
			})
		}
		externalJSON(w, map[string]any{"crews": items})
		return
	}
	crew, manifest, summary, ok := api.externalCrewResolve(ctx, claims, str("crew_id"))
	if !ok {
		externalError(w, 404, "not_found", "Crew not found or not allowed for this connection.")
		return
	}
	label := fmt.Sprint(summary["name"])
	switch name {
	case "get_crew":
		out := map[string]any{
			"crew_id": manifest.ID, "name": label, "identity": summary["identity_name"],
			"owner": summary["owner"], "description": strings.TrimSpace(manifest.Description),
			"functions": externalCrewFunctionSummaries(ctx, crew, manifest, label),
		}
		if llm := manifest.Capabilities.LLMConfig; llm != nil {
			out["model"] = map[string]any{"mode": llm.Mode, "provider": llm.Provider}
		}
		externalJSON(w, out)
	case "list_crew_functions":
		externalJSON(w, map[string]any{"crew_id": manifest.ID, "functions": externalCrewFunctionSummaries(ctx, crew, manifest, label)})
	case "list_crew_files":
		entries := []sharedProjectFileEntry{}
		truncated := false
		if listing, exists, err := listWorkspaceFolder(ctx, crew.Binding.WorkspacePath, sharedProjectFileTreeDepth); err == nil && exists {
			entries, truncated = flattenSharedProjectFiles(crew.Binding.WorkspacePath, listing)
		}
		externalJSON(w, map[string]any{"crew_id": manifest.ID, "files": entries, "truncated": truncated})
	case "read_crew_file":
		full, confined := confineSharedProjectPath(crew.Binding.WorkspacePath, str("path"))
		if !confined {
			externalError(w, 404, "not_found", "File not found or private.")
			return
		}
		raw, found, err := readFileFromWorkspace(ctx, full)
		if err != nil || !found {
			externalError(w, 404, "not_found", "File not found or private.")
			return
		}
		if isSharedProjectBinaryContent(raw) {
			externalError(w, 415, "unsupported", "File is not readable as text.")
			return
		}
		truncated := false
		if len(raw) > sharedProjectFileContentCap {
			raw, truncated = raw[:sharedProjectFileContentCap], true
		}
		externalJSON(w, map[string]any{"crew_id": manifest.ID, "path": str("path"), "content": raw, "truncated": truncated})
	default:
		externalError(w, 404, "unknown_tool", "Tool is not exposed by this API.")
	}
}
