package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/presentations"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

const maxAgentProfileRequestBytes = 2 << 20

// AgentProfileChatRequest is the intentionally small public contract for a
// product-owned chat. Prompt, model, tools, skills, permissions and workspace
// all come from the registered profile rather than the browser. Engine is the
// one exception, and only within the profile's own declared bounds: it may
// name one of profile.Runtime.ProviderOptions[].ID, letting the client choose
// among a product-curated set of coding-agent runtimes (see ProviderOption's
// doc comment) — never an arbitrary provider or model.
type AgentProfileChatRequest struct {
	Message         string `json:"message"`
	ConversationKey string `json:"conversation_key,omitempty"`
	Engine          string `json:"engine,omitempty"`
	// ModelID picks a model within the engine's provider: one the platform's
	// model catalog lists for that provider (or, when the engine declares its
	// own Models list, one of those). Empty keeps the option's own model.
	ModelID string `json:"model_id,omitempty"`
	// ReasoningEffort picks a level from the engine's declared
	// ReasoningEfforts. Empty keeps whatever the engine's own Options declare.
	ReasoningEffort      string   `json:"reasoning_effort,omitempty"`
	EnabledServers       []string `json:"enabled_servers,omitempty"`
	SelectedSkills       []string `json:"selected_skills,omitempty"`
	WorkflowContextPaths []string `json:"workflow_context_paths,omitempty"`
}

type AgentProfileConversationRequest struct {
	ConversationKey string `json:"conversation_key,omitempty"`
	// SessionID names an earlier conversation of the slot to make live again
	// (POST …/conversation/switch); the other conversation routes ignore it.
	SessionID string `json:"session_id,omitempty"`
}

type AgentProfileConversationResponse struct {
	ConversationID  string `json:"conversation_id"`
	ConversationKey string `json:"conversation_key"`
	SessionID       string `json:"session_id"`
}

// AgentProfilePresentationDeleteRequest is deliberately narrow: the browser
// can request deletion of one presented Video Studio asset, but cannot supply
// an arbitrary workspace root or a SQL statement. The server resolves the
// project from its durable manifest before touching either the files or the
// presentation row.
type AgentProfilePresentationDeleteRequest struct {
	ConversationKey string `json:"conversation_key"`
	Kind            string `json:"kind"`
}

func queryRequestForAgentProfileChat(profile agentprofiles.Profile, input AgentProfileChatRequest, conversation ProductConversationRecord) (QueryRequest, error) {
	if strings.TrimSpace(conversation.SessionID) == "" || strings.TrimSpace(conversation.WorkspacePath) == "" {
		return QueryRequest{}, fmt.Errorf("product conversation has no runtime binding")
	}
	req := QueryRequest{
		Query:                       input.Message,
		SessionTitle:                firstNonEmptyTrimmed(conversation.Title, profile.Name),
		AgentMode:                   "multi-agent",
		AgentProfileID:              profile.ID,
		AgentProfileVersion:         profile.Version,
		AgentProfileConversationKey: conversation.ConversationKey,
		// The registry has already verified that this durable session belongs to
		// the signed-in user and selected product resource. Carry that trusted
		// identity into the shared runner so a reopened product chat can restore
		// its provider-native session, or replay its bounded saved transcript when
		// native resume is unavailable. The narrow product API still gives the
		// browser no way to nominate an arbitrary restore path or session.
		RestoredConversationSessionID: conversation.SessionID,
		AgentProfileContext: agentprofiles.PromptContext{
			ProjectTitle:         firstNonEmptyTrimmed(conversation.Title, profile.Name),
			WorkspaceDescription: conversation.Description,
		},
		SelectedFolder: conversation.WorkspacePath,
	}
	// Work is the general coding product: unlike fixed-purpose profiles, it
	// exposes AgentWorks' existing per-chat MCP and skill selection. The
	// profile resolver still adds its built-in skills and enforces its tool and
	// workspace policy. Other products retain their deliberately narrow input.
	if profile.Runtime.Capabilities.MCPSelection != "" && profile.Runtime.Capabilities.MCPSelection != agentprofiles.CapabilityDisabled {
		if strings.TrimSpace(conversation.ResourceID) != "" {
			req.EnabledServers = appendUniqueStrings(nil, conversation.ProjectSelectedServers...)
			if len(req.EnabledServers) == 0 {
				// Preserve an explicit project-level "none" through the shared
				// query path. This also differs from legacy registry records that
				// silently mounted every connected account MCP, forcing one clean
				// CLI relaunch during migration.
				req.EnabledServers = []string{"NO_SERVERS"}
			}
		} else {
			req.EnabledServers = appendUniqueStrings(nil, input.EnabledServers...)
		}
	} else if len(input.EnabledServers) > 0 {
		return QueryRequest{}, fmt.Errorf("profile %q does not accept user-selected MCP servers", profile.ID)
	}
	if profile.Runtime.Capabilities.SkillSelection != "" && profile.Runtime.Capabilities.SkillSelection != agentprofiles.CapabilityDisabled {
		if strings.TrimSpace(conversation.ResourceID) != "" {
			req.SelectedSkills = appendUniqueStrings(nil, conversation.ProjectSelectedSkills...)
		} else {
			req.SelectedSkills = appendUniqueStrings(nil, input.SelectedSkills...)
		}
	} else if len(input.SelectedSkills) > 0 {
		return QueryRequest{}, fmt.Errorf("profile %q does not accept user-selected skills", profile.ID)
	}
	if profile.Runtime.Capabilities.WorkflowReferences != "" && profile.Runtime.Capabilities.WorkflowReferences != agentprofiles.CapabilityDisabled {
		req.WorkflowContextPaths = appendUniqueStrings(nil, conversation.ProjectWorkflowContextPaths...)
		req.WorkflowContextPaths = appendUniqueStrings(req.WorkflowContextPaths, input.WorkflowContextPaths...)
	} else if len(input.WorkflowContextPaths) > 0 {
		return QueryRequest{}, fmt.Errorf("profile %q does not accept workflow references", profile.ID)
	}
	// Project schedules, bots and restored clients may not send a browser tab's
	// engine fields. Reuse the same capabilities.llm_config stored in
	// workflow.json that the Work UI uses, just as AgentWorks turns resolve their
	// runtime from workflow.json.
	if strings.TrimSpace(input.Engine) == "" && conversation.ProjectLLMConfig != nil {
		builder := presetPrimaryLLMForChat(conversation.ProjectLLMConfig)
		provider := ""
		modelID := ""
		reasoningEffort := ""
		if builder != nil {
			provider = strings.TrimSpace(builder.Provider)
			modelID = strings.TrimSpace(builder.ModelID)
			if effort, ok := builder.Options["reasoning_effort"].(string); ok {
				reasoningEffort = strings.TrimSpace(effort)
			}
		}
		if provider != "" {
			for _, option := range profile.Runtime.ProviderOptions {
				if !strings.EqualFold(strings.TrimSpace(option.Provider), provider) {
					continue
				}
				input.Engine = option.ID
				input.ModelID = modelID
				input.ReasoningEffort = reasoningEffort
				break
			}
			if strings.TrimSpace(input.Engine) == "" {
				return QueryRequest{}, fmt.Errorf("project LLM provider %q is not offered by profile %q", provider, profile.ID)
			}
		}
	}
	// Projects created before capabilities.llm_config was introduced fall back
	// to the conversation's last actual runtime, never the profile default. The
	// Work UI backfills this value into workflow.json when the project is opened.
	if strings.TrimSpace(input.Engine) == "" && conversation.ProjectLLMConfig == nil && strings.TrimSpace(conversation.Provider) != "" {
		for _, option := range profile.Runtime.ProviderOptions {
			if !strings.EqualFold(strings.TrimSpace(option.Provider), strings.TrimSpace(conversation.Provider)) {
				continue
			}
			input.Engine = option.ID
			input.ModelID = conversation.ModelID
			input.ReasoningEffort = conversation.ReasoningEffort
			break
		}
	}
	if engine := strings.TrimSpace(input.Engine); engine != "" {
		option, ok := findProviderOptionByID(profile.Runtime.ProviderOptions, engine)
		if !ok {
			return QueryRequest{}, fmt.Errorf("engine %q is not offered by this profile", engine)
		}
		req.Provider = option.Provider
		req.ModelID = option.ModelID
		if modelID := strings.TrimSpace(input.ModelID); modelID != "" {
			if !providerOptionOffersModel(option, modelID) {
				return QueryRequest{}, fmt.Errorf("model %q is not offered for engine %q", modelID, engine)
			}
			req.ModelID = modelID
		}
		if effort := strings.TrimSpace(input.ReasoningEffort); effort != "" {
			if !reasoningEffortOffered(option, effort) {
				return QueryRequest{}, fmt.Errorf("reasoning effort %q is not offered for engine %q", effort, engine)
			}
			req.ReasoningEffort = effort
		}
	}
	return req, nil
}

// providerOptionOffersModel reports whether modelID may be chosen for this
// engine: one of its own curated Models when it declares any, otherwise one
// the platform's model catalog lists for its provider (the same catalog the
// composer's switcher is filled from when the engine declares no curation).
func providerOptionOffersModel(option agentprofiles.ProviderOption, modelID string) bool {
	if len(option.Models) > 0 {
		for _, id := range option.Models {
			if strings.EqualFold(strings.TrimSpace(id), modelID) {
				return true
			}
		}
		return false
	}
	return providerOffersModel(option.Provider, modelID)
}

// reasoningEffortOffered reports whether effort is one of the engine's
// declared ReasoningEfforts. An engine that declares none offers no control
// at all — nothing to compare against, so nothing is accepted.
func reasoningEffortOffered(option agentprofiles.ProviderOption, effort string) bool {
	for _, level := range option.ReasoningEfforts {
		if strings.EqualFold(strings.TrimSpace(level), effort) {
			return true
		}
	}
	return false
}

// providerOffersModel reports whether the platform's model catalog lists
// modelID under provider — the same catalog the composer's switcher is
// filled from, so a client can only send back what it was offered. A
// provider the catalog does not know at all accepts any id: nothing to
// check against.
func providerOffersModel(provider, modelID string) bool {
	known := false
	for _, model := range allProviderModelMetadata() {
		if model == nil || !strings.EqualFold(strings.TrimSpace(model.Provider), strings.TrimSpace(provider)) {
			continue
		}
		known = true
		if strings.EqualFold(strings.TrimSpace(model.ModelID), modelID) {
			return true
		}
	}
	return !known
}

func findProviderOptionByID(options []agentprofiles.ProviderOption, id string) (agentprofiles.ProviderOption, bool) {
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option.ID), id) {
			return option, true
		}
	}
	return agentprofiles.ProviderOption{}, false
}

func AgentProfileRoutes(router *mux.Router, registry *agentprofiles.Registry) {
	router.HandleFunc("/agent-profiles", listAgentProfilesHandler(registry)).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/agent-profiles/validate", validateAgentProfileHandler()).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/agent-profiles/{id}", getAgentProfileHandler(registry)).Methods(http.MethodGet, http.MethodOptions)
}

func cleanPresentedAssetPath(raw string, kind string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("asset path must be project-relative")
	}
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") {
		return "", fmt.Errorf("asset path must stay inside the project")
	}
	extension := strings.ToLower(filepath.Ext(path))
	switch kind {
	case "media.video":
		if extension != ".mp4" && extension != ".mov" && extension != ".webm" && extension != ".mkv" {
			return "", fmt.Errorf("video deletion requires a video file")
		}
	case "media.character":
		if extension != ".png" && extension != ".jpg" && extension != ".jpeg" && extension != ".webp" && extension != ".md" {
			return "", fmt.Errorf("character deletion only permits its reference image and spec")
		}
	default:
		return "", fmt.Errorf("this presentation kind cannot be deleted from the product UI")
	}
	return path, nil
}

// handleAgentProfilePresentationDelete deletes a user-confirmed, generated
// presentation. The browser provides only the presentation ID, kind, and
// paths already rendered from that row; project ownership and the workspace
// root are always determined server-side from the signed-in user's project.
func (api *StreamingAPI) handleAgentProfilePresentationDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentProfileRequestBytes))
	decoder.DisallowUnknownFields()
	var input AgentProfilePresentationDeleteRequest
	if err := decoder.Decode(&input); err != nil {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid presentation deletion request: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid presentation deletion request: expected one JSON object")
		return
	}
	if api.agentProfiles == nil {
		writeAgentProfileError(w, http.StatusServiceUnavailable, "agent profiles are unavailable")
		return
	}
	profile, err := api.agentProfiles.Resolve(strings.TrimSpace(mux.Vars(r)["id"]), 0, GetUserIDFromContext(r.Context()))
	if err != nil {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	if !userAllowedProduct(GetUserFromContext(r.Context()), profile.Product) {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	if profile.ID != "video-studio" {
		writeAgentProfileError(w, http.StatusMethodNotAllowed, "this product does not support deleting presentations")
		return
	}
	presentationID := strings.TrimSpace(mux.Vars(r)["presentationID"])
	if presentationID == "" {
		writeAgentProfileError(w, http.StatusBadRequest, "presentation id is required")
		return
	}
	userID := productWorkspaceUserID(r.Context())
	binding, err := resolveProductConversationBinding(r.Context(), userID, profile, strings.TrimSpace(input.ConversationKey))
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	kind := strings.TrimSpace(input.Kind)
	client := workspace.NewClient(getWorkspaceAPIURL(), workspace.WithUserID(userID))
	rows, err := client.QueryAuthorizedWorkflowDB(r.Context(), workspace.QueryWorkflowDBParams{
		DBPath: presentations.DatabasePath(binding.WorkspacePath),
		SQL:    "SELECT payload_json FROM ui_presentations WHERE id = ? AND kind = ?",
		Params: []interface{}{presentationID, kind},
	})
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, "load presentation for deletion: "+err.Error())
		return
	}
	if len(rows.Rows) != 1 {
		writeAgentProfileError(w, http.StatusNotFound, "presentation was not found in this project")
		return
	}
	payloadJSON, _ := rows.Rows[0]["payload_json"].(string)
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, "stored presentation payload is invalid")
		return
	}
	rawPaths := []string{}
	switch kind {
	case "media.video":
		rawPaths = append(rawPaths, stringPayloadValue(payload, "path"))
	case "media.character":
		rawPaths = append(rawPaths, stringPayloadValue(payload, "image_path"), stringPayloadValue(payload, "spec_path"))
	default:
		writeAgentProfileError(w, http.StatusBadRequest, "this presentation kind cannot be deleted from the product UI")
		return
	}
	paths := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		path, pathErr := cleanPresentedAssetPath(rawPath, kind)
		if pathErr != nil {
			writeAgentProfileError(w, http.StatusUnprocessableEntity, "stored presentation source is invalid: "+pathErr.Error())
			return
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if _, err := client.DeleteWorkspaceFile(r.Context(), workspace.DeleteWorkspaceFileParams{Filepath: filepath.ToSlash(filepath.Join(binding.WorkspacePath, path))}); err != nil {
			// A stale presentation is exactly the case where the user most needs
			// this control: its generated file may already have been removed from
			// the Files panel. Still remove the durable card in that case rather
			// than trapping them behind a failed delete button.
			if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "does not exist") {
				continue
			}
			writeAgentProfileError(w, http.StatusUnprocessableEntity, "delete presented asset: "+err.Error())
			return
		}
	}
	if err := presentations.Delete(r.Context(), client, binding.WorkspacePath, presentationID, kind); err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeAgentProfileJSON(w, http.StatusOK, map[string]interface{}{"deleted": true, "presentation_id": presentationID})
}

func stringPayloadValue(payload map[string]interface{}, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func (api *StreamingAPI) handleAgentProfileChatQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentProfileRequestBytes))
	decoder.DisallowUnknownFields()
	var input AgentProfileChatRequest
	if err := decoder.Decode(&input); err != nil {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid profile chat request: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid profile chat request: expected one JSON object")
		return
	}
	if strings.TrimSpace(input.Message) == "" {
		writeAgentProfileError(w, http.StatusBadRequest, "message is required")
		return
	}
	if api.agentProfiles == nil {
		writeAgentProfileError(w, http.StatusServiceUnavailable, "agent profiles are unavailable")
		return
	}

	profileID := strings.TrimSpace(mux.Vars(r)["id"])
	profile, err := api.agentProfiles.Resolve(profileID, 0, GetUserIDFromContext(r.Context()))
	if err != nil {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	if !userAllowedProduct(GetUserFromContext(r.Context()), profile.Product) {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	// A person is using the product right now: the quiet rule must hold any
	// due schedule back. Scheduled runs enter through Run, never here, so a
	// check-in's own turns are not mistaken for family activity.
	productInteractions.Note(r.Context(), GetUserIDFromContext(r.Context()), profile.Product)
	conversation, err := api.resolveAgentProfileConversation(r, profile, input.ConversationKey)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	query, err := queryRequestForAgentProfileChat(profile, input, conversation)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// Runtime changes keep the AgentWorks conversation identity but must close
	// the retained coding CLI. Its provider/model flags are read only at launch;
	// the shared runner relaunches it with the new project configuration below.
	if strings.TrimSpace(query.Provider) != "" {
		_, restartNeeded, err := defaultProductConversationRegistryStore().bindRuntimeConfiguration(
			r.Context(), productWorkspaceUserID(r.Context()), profile, conversation.ConversationKey,
			query.Provider, query.ModelID, query.ReasoningEffort, query.EnabledServers, query.SelectedSkills,
			query.WorkflowContextPaths,
		)
		if err != nil {
			writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if restartNeeded {
			closeAllCodingCLIInteractiveSessionsForOwner(conversation.SessionID, "product chat: coding agent, model, MCP or skill configuration changed")
		}
	}
	encoded, err := json.Marshal(query)
	if err != nil {
		writeAgentProfileError(w, http.StatusInternalServerError, "encode profile chat request")
		return
	}

	// Preserve authentication, cancellation and X-Session-ID, but replace the
	// broad browser-authored QueryRequest with the server-authored profile turn.
	// This is the migration seam: product clients now have a stable minimal API;
	// the shared turn runner can be extracted from handleQuery behind this seam
	// without another frontend migration.
	// The rootless product gateway identifies its loopback caller as the
	// product (for example "video-studio"), while a single-user deployment's
	// durable workspace remains owned by DEFAULT_USER_ID. Keep that gateway
	// identity out of the shared query path so the folder guard, project
	// initializer, history and registry all address the same project files.
	forwarded := r.Clone(productWorkspaceContext(r.Context()))
	forwarded.Body = io.NopCloser(bytes.NewReader(encoded))
	forwarded.ContentLength = int64(len(encoded))
	forwarded.Header = r.Header.Clone()
	forwarded.Header.Set("Content-Type", "application/json")
	forwarded.Header.Set("X-Session-ID", conversation.SessionID)
	api.handleQuery(w, forwarded)
}

func (api *StreamingAPI) handleResolveAgentProfileConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentProfileRequestBytes))
	decoder.DisallowUnknownFields()
	var input AgentProfileConversationRequest
	if err := decoder.Decode(&input); err != nil {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid product conversation request: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid product conversation request: expected one JSON object")
		return
	}
	if api.agentProfiles == nil {
		writeAgentProfileError(w, http.StatusServiceUnavailable, "agent profiles are unavailable")
		return
	}
	profile, err := api.agentProfiles.Resolve(strings.TrimSpace(mux.Vars(r)["id"]), 0, GetUserIDFromContext(r.Context()))
	if err != nil {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	if !userAllowedProduct(GetUserFromContext(r.Context()), profile.Product) {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	// Opening the product's conversation is what the app does on launch: the
	// family is here, so a due check-in waits for a quiet moment.
	productInteractions.Note(r.Context(), GetUserIDFromContext(r.Context()), profile.Product)
	conversation, err := api.resolveAgentProfileConversation(r, profile, input.ConversationKey)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeAgentProfileJSON(w, http.StatusOK, AgentProfileConversationResponse{
		ConversationID:  conversation.ConversationID,
		ConversationKey: conversation.ConversationKey,
		SessionID:       conversation.SessionID,
	})
}

// handleRotateAgentProfileConversation is the server-owned implementation of
// “New chat” for products. A browser cannot safely rotate a product session by
// inventing an ID because keyed products bind their chat to a durable project
// manifest; the server updates that binding and the registry together.
func (api *StreamingAPI) handleRotateAgentProfileConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentProfileRequestBytes))
	decoder.DisallowUnknownFields()
	var input AgentProfileConversationRequest
	if err := decoder.Decode(&input); err != nil {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid product conversation request: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid product conversation request: expected one JSON object")
		return
	}
	if api.agentProfiles == nil {
		writeAgentProfileError(w, http.StatusServiceUnavailable, "agent profiles are unavailable")
		return
	}
	userID := productWorkspaceUserID(r.Context())
	profile, err := api.agentProfiles.Resolve(strings.TrimSpace(mux.Vars(r)["id"]), 0, userID)
	if err != nil {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	if !userAllowedProduct(GetUserFromContext(r.Context()), profile.Product) {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	binding, err := resolveProductConversationBinding(r.Context(), userID, profile, input.ConversationKey)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	conversation, err := defaultProductConversationRegistryStore().rotate(r.Context(), userID, profile, binding)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeAgentProfileJSON(w, http.StatusOK, AgentProfileConversationResponse{
		ConversationID:  conversation.ConversationID,
		ConversationKey: conversation.ConversationKey,
		SessionID:       conversation.SessionID,
	})
}

func (api *StreamingAPI) resolveAgentProfileConversation(r *http.Request, profile agentprofiles.Profile, requestedKey string) (ProductConversationRecord, error) {
	userID := productWorkspaceUserID(r.Context())
	binding, err := resolveProductConversationBinding(r.Context(), userID, profile, requestedKey)
	if err != nil {
		return ProductConversationRecord{}, err
	}
	client := workspace.NewClient(getWorkspaceAPIURL(), workspace.WithUserID(userID))
	// Work projects have a conventional source-code home. Creating it here is
	// idempotent and also upgrades projects created before the convention was
	// introduced. The frontend creates it eagerly so it is visible immediately;
	// this server-side guard keeps non-UI callers consistent.
	if profile.ID == "work" {
		if err := client.CreateFolder(r.Context(), filepath.ToSlash(filepath.Join(binding.WorkspacePath, "code"))); err != nil {
			return ProductConversationRecord{}, fmt.Errorf("initialize product code folder: %w", err)
		}
	}
	// Database is a product feature, so its physical managed SQLite location is
	// part of enabling that feature too. Create the valid empty database through
	// the trusted server-to-workspace channel when a project is first opened;
	// repeat calls are idempotent and older projects are upgraded automatically.
	if agentprofiles.HasFeature(profile, "database") {
		if _, err := client.InitializeWorkflowDB(r.Context(), workspace.InitializeWorkflowDBParams{
			DBPath: filepath.ToSlash(filepath.Join(binding.WorkspacePath, "db", "db.sqlite")),
		}); err != nil {
			return ProductConversationRecord{}, fmt.Errorf("initialize product database: %w", err)
		}
	}
	preferredSessionID := ""
	if binding.AuthoritativeSessionID == "" {
		candidate := strings.TrimSpace(r.Header.Get("X-Session-ID"))
		if candidate != "" && api.canUseSessionIDForQuery(r, candidate) {
			if active, ok := api.getActiveSession(candidate); ok && sessionVisibleTo(active.UserID, GetUserFromContext(r.Context())) {
				preferredSessionID = candidate
			} else if _, found, findErr := FindChatHistoryConversationPathForSession(userID, candidate, ""); findErr != nil {
				return ProductConversationRecord{}, fmt.Errorf("find existing product conversation: %w", findErr)
			} else if found {
				preferredSessionID = candidate
			}
		}
	}
	record, err := defaultProductConversationRegistryStore().resolveOrCreate(r.Context(), userID, profile, binding, preferredSessionID)
	if err != nil {
		return ProductConversationRecord{}, err
	}
	if !api.canUseSessionIDForQuery(r, record.SessionID) {
		return ProductConversationRecord{}, fmt.Errorf("product conversation session belongs to another user")
	}
	return record, nil
}

// productWorkspaceUserID preserves true per-user isolation where it is
// enabled. A dedicated single-user product deployment is different: its
// gateway uses an internal product principal for the reverse-proxy JWT, but
// all durable project data belongs to the configured default owner.
func productWorkspaceUserID(ctx context.Context) string {
	if !IsMultiUserMode() {
		return GetDefaultUserID()
	}
	return GetUserIDFromContext(ctx)
}

// productWorkspaceContext makes the downstream shared /query path use the
// same owner chosen by productWorkspaceUserID. It deliberately leaves every
// other claim intact (including access level), changing only the workspace
// namespace in the single-user gateway case.
func productWorkspaceContext(ctx context.Context) context.Context {
	if IsMultiUserMode() {
		return ctx
	}
	claims := GetUserFromContext(ctx)
	if claims == nil {
		return ctx
	}
	copy := *claims
	copy.UserID = GetDefaultUserID()
	return context.WithValue(ctx, UserContextKey, &copy)
}

func listAgentProfilesHandler(registry *agentprofiles.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		userID := GetUserIDFromContext(r.Context())
		profiles := registry.List(userID)
		claims := GetUserFromContext(r.Context())
		visible := profiles[:0]
		for _, profile := range profiles {
			if userAllowedProduct(claims, profile.Product) {
				visible = append(visible, profile)
			}
		}
		writeAgentProfileJSON(w, http.StatusOK, map[string]interface{}{
			"profiles": visible,
		})
	}
}

func getAgentProfileHandler(registry *agentprofiles.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		version := 0
		if rawVersion := strings.TrimSpace(r.URL.Query().Get("version")); rawVersion != "" {
			parsed, err := strconv.Atoi(rawVersion)
			if err != nil || parsed < 1 {
				writeAgentProfileError(w, http.StatusBadRequest, "version must be a positive integer")
				return
			}
			version = parsed
		}
		profile, err := registry.Resolve(mux.Vars(r)["id"], version, GetUserIDFromContext(r.Context()))
		if err != nil {
			writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
			return
		}
		if !userAllowedProduct(GetUserFromContext(r.Context()), profile.Product) {
			writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
			return
		}
		writeAgentProfileJSON(w, http.StatusOK, profile)
	}
}

func validateAgentProfileHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentProfileRequestBytes))
		decoder.DisallowUnknownFields()
		var profile agentprofiles.Profile
		if err := decoder.Decode(&profile); err != nil {
			writeAgentProfileError(w, http.StatusBadRequest, "invalid agent profile: "+err.Error())
			return
		}
		// Validation is the user-profile contract. A client cannot claim built-in
		// authority or choose a different owner through this endpoint.
		profile.BuiltIn = false
		profile.OwnerID = GetUserIDFromContext(r.Context())
		if err := agentprofiles.Validate(profile); err != nil {
			writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeAgentProfileJSON(w, http.StatusOK, map[string]interface{}{
			"valid":   true,
			"profile": profile,
		})
	}
}

func writeAgentProfileError(w http.ResponseWriter, status int, message string) {
	writeAgentProfileJSON(w, status, map[string]string{"error": message})
}

func writeAgentProfileJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
