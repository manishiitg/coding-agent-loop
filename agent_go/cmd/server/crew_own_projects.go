package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

// The owner's own crews. Crews live at the shared Crew/ root, which the raw
// workspace proxy never lists (it would show every owner's crews), so the
// Crew UI lists and creates its crews through these endpoints. Editing an
// owned crew's files still goes through the proxy, which admits the owner.

type ownCrewProject struct {
	WorkspacePath string  `json:"workspace_path"`
	Product       string  `json:"product_json"`
	Runtime       *string `json:"runtime_json,omitempty"`
	LastModified  string  `json:"last_modified,omitempty"`
}

// GET /api/agent-profiles/{id}/my-projects: the caller's own crews with their
// raw manifests, so the client parses them exactly as before.
func (api *StreamingAPI) handleListOwnCrewProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	claims, profile, ok := api.ownCrewProfile(w, r)
	if !ok {
		return
	}
	catalog, err := listCrewCatalog(r.Context(), defaultProductProjectStore())
	if err != nil {
		writeAgentProfileError(w, http.StatusBadGateway, "could not list crews")
		return
	}
	self := sanitizeUserIDForPath(claims.UserID)
	projects := []ownCrewProject{}
	for _, entry := range catalog {
		if entry.OwnerID != self {
			continue
		}
		raw, found, err := readFileFromWorkspace(r.Context(), entry.ManifestPath)
		if err != nil || !found {
			continue
		}
		project := ownCrewProject{WorkspacePath: entry.Root, Product: raw}
		if runtimeRaw, runtimeFound, err := readFileFromWorkspace(r.Context(), projectRuntimeManifestPath(profile.ID, entry.Root)); err == nil && runtimeFound {
			project.Runtime = &runtimeRaw
		}
		projects = append(projects, project)
	}
	writeAgentProfileJSON(w, http.StatusOK, map[string]interface{}{"projects": projects})
}

var ownCrewSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// POST /api/agent-profiles/{id}/my-projects {title, description, identity, llm_config}
// creates a crew at Crew/<slug>-<id8> owned by the caller.
func (api *StreamingAPI) handleCreateOwnCrewProject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	claims, profile, ok := api.ownCrewProfile(w, r)
	if !ok {
		return
	}
	var body struct {
		Title       string                         `json:"title"`
		Description string                         `json:"description"`
		Identity    *workIdentityInput             `json:"identity,omitempty"`
		LLMConfig   *workflowtypes.PresetLLMConfig `json:"llm_config,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(body.Title)
	description := strings.TrimSpace(body.Description)
	if title == "" || utf8.RuneCountInString(title) > 200 || utf8.RuneCountInString(description) > 1000 {
		writeAgentProfileError(w, http.StatusBadRequest, "a title (at most 200 characters) is required; the description is at most 1000")
		return
	}
	created, err := createOwnCrewProject(r.Context(), claims.UserID, profile, title, description, body.Identity, body.LLMConfig, time.Now().UTC())
	if err != nil {
		writeAgentProfileError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAgentProfileJSON(w, http.StatusCreated, created)
}

type workIdentityInput struct {
	Name string `json:"name,omitempty"`
	Icon string `json:"icon,omitempty"`
}

func createOwnCrewProject(ctx context.Context, userID string, profile agentprofiles.Profile, title, description string, identity *workIdentityInput, llmConfig *workflowtypes.PresetLLMConfig, now time.Time) (map[string]interface{}, error) {
	id := uuid.NewString()
	slug := strings.Trim(ownCrewSlugPattern.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "crew"
	}
	workspacePath := path.Join(crewSharedRootName, slug+"-"+id[:8])
	stamp := now.Format(time.RFC3339)
	capabilities := map[string]interface{}{
		"selected_servers":             []string{},
		"selected_tools":               []string{},
		"selected_skills":              []string{},
		"selected_secrets":             []string{},
		"selected_global_secret_names": []string{},
		"browser_mode":                 "auto",
		"use_code_execution_mode":      false,
	}
	if llmConfig != nil {
		capabilities["llm_config"] = llmConfig
	}
	runtimeManifest := map[string]interface{}{
		"schema_version": 1, "id": id, "label": title, "capabilities": capabilities,
		"workflow_context_paths": []string{}, "schedules": []interface{}{}, "triggers": []interface{}{},
		"created_at": stamp, "updated_at": stamp,
	}
	sessionID := profile.ID + ":project:" + id
	productManifest := map[string]interface{}{
		"schema_version": 1, "product": profile.ID, "id": id, "owner_id": userID,
		"title": title, "description": description, "session_id": sessionID,
		"created_at": stamp, "updated_at": stamp,
	}
	if identity != nil && (strings.TrimSpace(identity.Name) != "" || strings.TrimSpace(identity.Icon) != "") {
		productManifest["identity"] = map[string]string{"name": strings.TrimSpace(identity.Name), "icon": strings.TrimSpace(identity.Icon)}
	}
	// Runtime manifest first: a crew is listed by its product.json, so it
	// never appears half-created.
	for _, file := range []struct {
		name  string
		value map[string]interface{}
	}{{"workflow.json", runtimeManifest}, {"product.json", productManifest}} {
		if err := writeCrewCreationManifest(ctx, path.Join(workspacePath, file.name), file.value); err != nil {
			return nil, fmt.Errorf("create crew %s: %w", file.name, err)
		}
	}
	crewOwners.mu.Lock()
	delete(crewOwners.entries, workspacePath)
	crewOwners.mu.Unlock()
	return map[string]interface{}{
		"id": id, "workspace_path": workspacePath, "session_id": sessionID,
		"product_json": mustMarshalIndent(productManifest), "runtime_json": mustMarshalIndent(runtimeManifest),
	}, nil
}

func mustMarshalIndent(value interface{}) string {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	return string(encoded) + "\n"
}

func (api *StreamingAPI) ownCrewProfile(w http.ResponseWriter, r *http.Request) (*UserClaims, agentprofiles.Profile, bool) {
	claims := GetUserFromContext(r.Context())
	profileID := strings.TrimSpace(mux.Vars(r)["id"])
	if claims == nil || strings.TrimSpace(claims.UserID) == "" || claims.Provider == "bot_route" || api == nil || api.agentProfiles == nil || !strings.EqualFold(profileID, "work") {
		writeAgentProfileError(w, http.StatusNotFound, "crews not found")
		return nil, agentprofiles.Profile{}, false
	}
	profile, err := api.agentProfiles.Resolve(profileID, 0, claims.UserID)
	if err != nil || !userAllowedProduct(claims, profile.Product) {
		writeAgentProfileError(w, http.StatusNotFound, "crews not found")
		return nil, agentprofiles.Profile{}, false
	}
	return claims, profile, true
}
