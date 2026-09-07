package server

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
)

type resolvedAgentProfile struct {
	Definition agentprofiles.Profile
	Prompt     string
	// APIKeys carries the project-scoped credential this resolver loaded from the
	// encrypted per-user/workspace store. It is returned on the resolver's own
	// result rather than handed back through req.LLMConfig so the query path can
	// never take a credential from the request body: LLMConfig is deserialized
	// from client JSON, and every field of ProviderAPIKeys except
	// ClaudeCodeOAuthToken is JSON-visible. nil when no profile is bound.
	APIKeys *llm.ProviderAPIKeys
}

// cleanAgentProfileWorkspace validates the client-supplied selected_folder for
// a profile-backed turn. It is the authorization gate for that path: every
// agentProfileRuntimeWorkspace() call in the query path runs only after a
// profile resolved successfully, so rejecting here keeps an unauthorized path
// from ever reaching a folder guard or CLI working directory.
//
// Blocking traversal is not sufficient on its own. agentProfileRuntimeWorkspace
// only re-scopes paths that start with "Chats" into the caller's own
// _users/<id>/Chats space and returns anything else verbatim, so an explicit
// "_users/<someone-else>/..." never gets rewritten and would be handed to the
// agent as its read/write root. userID is therefore required, and a foreign
// _users/ prefix is refused rather than silently rewritten -- a caller aiming
// at another user's workspace is not a path to normalize, it is a request to
// deny.
func cleanAgentProfileWorkspace(raw, userID string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("selected_folder is required when agent_profile_id is set")
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("selected_folder must be workspace-relative")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("selected_folder must stay inside the workspace")
	}
	if clean == "_users" || strings.HasPrefix(clean, "_users/") {
		owner := strings.TrimPrefix(clean, "_users")
		owner = strings.TrimPrefix(owner, "/")
		if idx := strings.Index(owner, "/"); idx >= 0 {
			owner = owner[:idx]
		}
		if owner == "" || owner != sanitizeUserIDForPath(userID) {
			return "", fmt.Errorf("selected_folder must stay inside your own workspace")
		}
	}
	return clean, nil
}

func appendUniqueStrings(current []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(current)+len(additions))
	out := make([]string, 0, len(current)+len(additions))
	for _, values := range [][]string{current, additions} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

// isGlobalScopedProfile reports whether the resolved profile declared
// agentprofiles.ProfileScopeGlobal -- any future global product profile
// profile with no single project workspace. A global-scoped profile keeps
// the same chat-wide grants (including the pulse/ write grant) and
// workspace description a profile-less turn already has; only a
// project-scoped profile narrows to one project root. Every call site that
// narrows behavior on bare `resolvedProfile != nil` must also check this, or
// it silently narrows a global-scoped profile the same way it narrows a
// project-scoped one.
func isGlobalScopedProfile(p *resolvedAgentProfile) bool {
	return p != nil && p.Definition.EffectiveScope() == agentprofiles.ProfileScopeGlobal
}

func agentProfileRuntimeWorkspace(userID, workspacePath string) string {
	workspacePath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(workspacePath)))
	if workspacePath == "Chats" || strings.HasPrefix(workspacePath, "Chats/") {
		suffix := strings.TrimPrefix(strings.TrimPrefix(workspacePath, "Chats"), "/")
		return filepath.ToSlash(filepath.Join(perUserChatsFolderFor(userID), suffix))
	}
	return workspacePath
}

// providerOptionRuntimeOptions returns a copy of the runtime options the
// profile declares for the (provider, model) binding a turn resolved to —
// nil when the binding is not one of the profile's provider_options or
// declares none. These become LLMModel.Options, which the agent runtime
// already turns into provider call options (reasoning_effort and friends).
func providerOptionRuntimeOptions(runtime agentprofiles.RuntimePolicy, provider, modelID string) map[string]interface{} {
	for _, option := range runtime.ProviderOptions {
		if !strings.EqualFold(strings.TrimSpace(option.Provider), strings.TrimSpace(provider)) || !strings.EqualFold(strings.TrimSpace(option.ModelID), strings.TrimSpace(modelID)) {
			continue
		}
		if len(option.Options) == 0 {
			return nil
		}
		out := make(map[string]interface{}, len(option.Options))
		for k, v := range option.Options {
			out[k] = v
		}
		return out
	}
	return nil
}

func resolveProfileRuntimeModel(runtime agentprofiles.RuntimePolicy, requestedProvider, requestedModelID string) (string, string) {
	provider, modelID := strings.TrimSpace(runtime.Provider), strings.TrimSpace(runtime.ModelID)
	for _, option := range runtime.ProviderOptions {
		if option.Default {
			provider, modelID = strings.TrimSpace(option.Provider), strings.TrimSpace(option.ModelID)
			break
		}
	}
	requestedProvider = strings.TrimSpace(requestedProvider)
	requestedModelID = strings.TrimSpace(requestedModelID)
	for _, option := range runtime.ProviderOptions {
		if !strings.EqualFold(requestedProvider, strings.TrimSpace(option.Provider)) {
			continue
		}
		if strings.EqualFold(requestedModelID, strings.TrimSpace(option.ModelID)) {
			return strings.TrimSpace(option.Provider), strings.TrimSpace(option.ModelID)
		}
		// option.ModelID is only this engine's own default model — Models is
		// the full curated list a turn may request within it (ProviderOption's
		// own doc comment: "narrows ... the server's accepted model_id to
		// exactly these ids"). Missing this fell through to the profile's
		// unrelated default provider option below, discarding both the
		// requested provider AND model. Caught live: switching SparkQuill's
		// composer from Luna to Terra (both gpt-5.6, both codex-cli) silently
		// ran the next turn on claude-code with no prior conversation context.
		for _, id := range option.Models {
			if strings.EqualFold(requestedModelID, strings.TrimSpace(id)) {
				return strings.TrimSpace(option.Provider), requestedModelID
			}
		}
	}
	return provider, modelID
}

// lookupAgentProfileDefinition returns the profile a request is bound to
// WITHOUT running its runtime initializer and without mutating the request.
//
// resolveAgentProfileForQuery is not a resolver: it calls
// agentProfiles.Initialize, which for a product like Video Studio seeds the
// workspace, writes a plan refresh, initializes the workflow DB, and runs
// productdeps.Ensure (which can shell out to `npx skills add`). It also rewrites
// the request's provider, model, skills, and secrets. That is correct once per
// turn and wrong for a caller that only needs to read the declared surface —
// delegation ran the whole initializer again for every sub-agent.
//
// Use this wherever the profile is being consulted rather than entered. It
// keeps the same authorization boundary: Resolve is what enforces ownership of
// a non-built-in profile.
func (api *StreamingAPI) lookupAgentProfileDefinition(ctx context.Context, req *QueryRequest, userID string) (*resolvedAgentProfile, error) {
	profileID := strings.TrimSpace(req.AgentProfileID)
	if profileID == "" {
		return nil, nil
	}
	if api.agentProfiles == nil {
		return nil, fmt.Errorf("agent profiles are unavailable")
	}
	profile, err := api.agentProfiles.Resolve(profileID, req.AgentProfileVersion, userID)
	if err != nil {
		return nil, err
	}
	if !userAllowedProduct(GetUserFromContext(ctx), profile.Product) {
		return nil, fmt.Errorf("you don't have access to the %q product", profile.Product)
	}
	return &resolvedAgentProfile{Definition: profile}, nil
}

func (api *StreamingAPI) resolveAgentProfileForQuery(ctx context.Context, req *QueryRequest, userID, sessionID string) (*resolvedAgentProfile, error) {
	profileID := strings.TrimSpace(req.AgentProfileID)
	if profileID == "" {
		if req.AgentProfileVersion != 0 || strings.TrimSpace(req.AgentProfileContext.ProjectTitle) != "" || strings.TrimSpace(req.AgentProfileContext.WorkspaceDescription) != "" {
			return nil, fmt.Errorf("agent_profile_id is required when agent profile fields are provided")
		}
		return nil, nil
	}
	if api.agentProfiles == nil {
		return nil, fmt.Errorf("agent profiles are unavailable")
	}
	if req.AgentMode != "multi-agent" {
		return nil, fmt.Errorf("agent profiles currently require agent_mode=multi-agent")
	}

	// Resolve before validating workspace/project fields: a global-scoped
	// global profile has no single project workspace, so whether
	// those fields are required at all depends on what this profile declares.
	profile, err := api.agentProfiles.Resolve(profileID, req.AgentProfileVersion, userID)
	if err != nil {
		return nil, err
	}
	if !userAllowedProduct(GetUserFromContext(ctx), profile.Product) {
		return nil, fmt.Errorf("you don't have access to the %q product", profile.Product)
	}
	isGlobalScope := profile.EffectiveScope() == agentprofiles.ProfileScopeGlobal

	selectedFolder := strings.TrimSpace(req.SelectedFolder)
	if isGlobalScope && selectedFolder == "" {
		// "Chats" is the same alias agentProfileRuntimeWorkspace already
		// rewrites to perUserChatsFolderFor(userID) -- the exact folder a
		// profile-less multi-agent turn already uses today.
		selectedFolder = "Chats"
	}
	workspacePath, err := cleanAgentProfileWorkspace(selectedFolder, userID)
	if err != nil {
		return nil, err
	}
	req.SelectedFolder = workspacePath

	promptContext := req.AgentProfileContext
	promptContext.ProjectTitle = strings.TrimSpace(promptContext.ProjectTitle)
	if promptContext.ProjectTitle == "" {
		if isGlobalScope {
			// A global profile has no per-turn project; its own declared Name
			// is the only sensible constant "title" for its prompt context.
			promptContext.ProjectTitle = profile.Name
		} else {
			return nil, fmt.Errorf("agent_profile_context.project_title is required")
		}
	}
	if strings.TrimSpace(promptContext.LocalDateTime) == "" {
		now := time.Now()
		_, offsetSeconds := now.Zone()
		offsetSign := "+"
		if offsetSeconds < 0 {
			offsetSign = "-"
			offsetSeconds = -offsetSeconds
		}
		promptContext.LocalDateTime = fmt.Sprintf("%s (UTC%s%02d:%02d)", now.Format("Monday, 2 January 2006 at 3:04 PM MST"), offsetSign, offsetSeconds/3600, (offsetSeconds%3600)/60)
	}
	// Product prompt variables are computed here from trusted state; never
	// from the request body.
	promptContext.Product = nil
	if productVars, err := api.agentProfiles.PromptVariables(ctx, profile.ID, agentprofiles.RuntimeContext{
		UserID: userID, SessionID: sessionID, WorkspacePath: workspacePath,
	}); err != nil {
		return nil, fmt.Errorf("agent profile %q prompt variables: %w", profile.ID, err)
	} else if len(productVars) > 0 {
		promptContext.Product = productVars
	}
	rendered, err := agentprofiles.RenderPrompt(profile, promptContext)
	if err != nil {
		return nil, err
	}
	if err := api.agentProfiles.Initialize(ctx, profile.ID, agentprofiles.RuntimeContext{
		UserID: userID, SessionID: sessionID, WorkspacePath: workspacePath,
	}); err != nil {
		return nil, fmt.Errorf("initialize agent profile %q: %w", profile.ID, err)
	}
	req.AgentProfileID = profile.ID
	req.AgentProfileVersion = profile.Version
	req.AgentProfileContext = promptContext
	req.SelectedSkills = appendUniqueStrings(req.SelectedSkills, profile.Skills...)
	if profile.Runtime.Capabilities.Secrets == agentprofiles.CapabilityDisabled {
		// Product profiles may explicitly opt out of the shared secret runtime.
		// Clear both user and global selections after saved chat configuration has
		// been applied, so a profile cannot inherit credentials accidentally.
		req.DecryptedSecrets = nil
		noGlobalSecrets := []string{}
		req.SelectedGlobalSecrets = &noGlobalSecrets
	} else if !isGlobalScope && api.chatStore != nil && userID != "" {
		// A product project owns its workflow-scoped secrets. Attach their names
		// automatically for every direct-chat turn so native coding-agent tools
		// receive SECRET_<NAME> without the model ever seeing a value. User-wide
		// secrets remain opt-in through the existing selected-secret mechanism;
		// a project secret with the same name deliberately resolves to the
		// project value. Skipped for a global-scoped profile: there is no
		// single project secret bucket to attach, and req.DecryptedSecrets /
		// req.SelectedGlobalSecrets are left exactly as the client sent them --
		// identical to today's profile-less behavior.
		stored, secretErr := api.chatStore.ListWorkflowSecrets(ctx, userID, workspacePath)
		if secretErr != nil {
			log.Printf("[SECRETS] Failed to list product workspace secrets for %s (%s): %v", userID, workspacePath, secretErr)
		} else {
			selectedNames := make([]string, 0, len(req.DecryptedSecrets)+len(stored))
			for _, secret := range req.DecryptedSecrets {
				selectedNames = appendUniqueStrings(selectedNames, secret.Name)
			}
			for _, secret := range stored {
				selectedNames = appendUniqueStrings(selectedNames, secret.Name)
			}
			if len(selectedNames) > 0 {
				req.DecryptedSecrets = api.loadSelectedSecrets(ctx, userID, workspacePath, selectedNames)
			}
		}
	}
	browserRequirement := profile.Runtime.Capabilities.Browser
	if browserRequirement == agentprofiles.CapabilityRequired || browserRequirement == agentprofiles.CapabilityPreferred || browserRequirement == agentprofiles.CapabilityOptional {
		// Agent profiles declare browser capability once. The generic chat
		// runtime then registers AgentWorks' managed agent_browser tool and
		// attaches its shared built-in skill; product code must not duplicate
		// either implementation.
		browserEnabled := true
		req.EnableBrowserAccess = &browserEnabled
		if strings.TrimSpace(req.BrowserMode) == "" || strings.EqualFold(strings.TrimSpace(req.BrowserMode), "none") {
			req.BrowserMode = "auto"
		}
	}
	var resolvedKeys *llm.ProviderAPIKeys
	requestHasExplicitModel := strings.TrimSpace(req.Provider) != "" && strings.TrimSpace(req.ModelID) != ""
	if provider, modelID := resolveProfileRuntimeModel(profile.Runtime, req.Provider, req.ModelID); provider != "" && modelID != "" {
		// A profile-owned model binding is authoritative over the user's global
		// AgentWorks chat selection for a project-scoped product (Video Studio):
		// the whole point is a curated, pinned choice. A global-scoped profile
		// global profile is meant to feel like a profile-less chat -- any
		// published LLM the user already picked wins; the declared binding is
		// only the starting default for a brand-new chat with no selection yet.
		if isGlobalScope && requestHasExplicitModel {
			provider, modelID = req.Provider, req.ModelID
		}
		req.Provider = provider
		req.ModelID = modelID
		llmOptions := providerOptionRuntimeOptions(profile.Runtime, provider, modelID)
		if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
			// providerOptionRuntimeOptions returns a fresh copy per call — safe
			// to mutate here without touching the profile's own declared options.
			if llmOptions == nil {
				llmOptions = map[string]interface{}{}
			}
			llmOptions["reasoning_effort"] = effort
		}
		req.LLMConfig = &orchestrator.LLMConfig{Primary: orchestrator.LLMModel{Provider: provider, ModelID: modelID, Options: llmOptions}}
		req.LLMConfigSource = llmConfigSourceAgentProfile
		if strings.EqualFold(strings.TrimSpace(profile.Runtime.CredentialScope), agentprofiles.CredentialScopeGlobal) {
			// Some products intentionally use the server-wide coding-agent login.
			// Do not layer a previously saved project credential when the profile
			// makes that choice, or the effective account would disagree with the UI.
			resolvedKeys = MergedProviderAPIKeys(ctx)
		} else if api.chatStore != nil {
			// Product workspaces use the same encrypted per-project credential store
			// as AgentWorks workflows (Claude Code's setup token, Cursor's API key).
			// It stays scoped to this user/workspace and is injected only into the
			// provider runtime, never into a prompt or tool. resolveEffectiveAPIKeys
			// always checks both supported providers and is a no-op for whichever
			// one isn't in use, so this call needs no gate on the resolved provider
			// name — that gate existed before and silently excluded cursor-cli.
			keys, credentialErr := api.resolveEffectiveAPIKeys(ctx, userID, workspacePath, nil)
			if credentialErr != nil {
				return nil, fmt.Errorf("load agent profile %s credential: %w", provider, credentialErr)
			}
			// Returned on resolvedAgentProfile, NOT written back onto req.LLMConfig.
			// The query path reads the resolver's result, so a credential can never
			// originate from the request body.
			resolvedKeys = keys
		}
	}
	if strings.TrimSpace(req.SessionTitle) == "" {
		req.SessionTitle = promptContext.ProjectTitle
	}
	return &resolvedAgentProfile{Definition: profile, Prompt: rendered, APIKeys: resolvedKeys}, nil
}

func profileRuntimeEventType(event any) string {
	if typed, ok := event.(unifiedevents.EventData); ok {
		if eventType := strings.TrimSpace(string(typed.GetEventType())); eventType != "" {
			return eventType
		}
	}
	if payload, ok := event.(map[string]interface{}); ok {
		if eventType, _ := payload["type"].(string); strings.TrimSpace(eventType) != "" {
			return strings.TrimSpace(eventType)
		}
	}
	return "agent_profile_event"
}

// emitAgentProfileEvent records an agent-profile-emitted event for this
// session's stream.
//
// event should be a value implementing unifiedevents.EventData -- a real
// struct from a schema-gen-registered event package (e.g.
// orchestrator_events.PresentationUpdatedEvent), not a hand-built
// map[string]interface{}. A typed value is used directly as the
// AgentEvent.Data payload, so it serializes at the same nesting depth as
// every other typed event (tool_call_end, llm_generation_end, ...) and gets
// a real generated TypeScript interface via cmd/schema-gen instead of an
// `unknown`-typed blob the frontend has to defensively unwrap.
//
// The map[string]interface{} path still exists underneath for a caller that
// has no registered event type yet, but it comes at a real cost: schema-gen
// has no way to generate a shape for it, so consumers get no compile-time
// guarantee about what is inside, and it wraps one JSON level deeper
// (GenericEventData's own "data" field) than a typed event does. Prefer
// registering a real type (see docs/design/agent_tool_surface_single_source.md
// for why "declared once, consumed everywhere" beats "reconstructed per
// consumer").
func (api *StreamingAPI) emitAgentProfileEvent(sessionID string, event any) {
	if api.eventStore == nil {
		return
	}
	eventType := profileRuntimeEventType(event)
	now := time.Now()

	var data unifiedevents.EventData
	if typed, ok := event.(unifiedevents.EventData); ok {
		stampEventData(typed, now)
		data = typed
	} else {
		payload := map[string]interface{}{"event": event}
		if untyped, ok := event.(map[string]interface{}); ok {
			payload = untyped
		}
		data = &unifiedevents.GenericEventData{Data: payload}
	}

	api.eventStore.AddEvent(sessionID, internalevents.Event{
		ID:        fmt.Sprintf("profile_%s_%d", strings.ReplaceAll(eventType, ".", "_"), now.UnixNano()),
		Type:      eventType,
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type: unifiedevents.EventType(eventType), Timestamp: now,
			Data: data,
		},
	})
}

// stampEventData gives a product tool's event a real timestamp when its
// embedded BaseEventData was left at zero. A zero inner timestamp sorted the
// event to the year 0001 when a persisted trace was merged back into a
// restored conversation, so a product's suggestion pills vanished after a
// reload because the event landed before the user's message instead of after
// its reply.
func stampEventData(data unifiedevents.EventData, now time.Time) {
	v := reflect.ValueOf(data)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	base := v.Elem().FieldByName("BaseEventData")
	if !base.IsValid() {
		return
	}
	ts := base.FieldByName("Timestamp")
	if !ts.IsValid() || !ts.CanSet() {
		return
	}
	if current, ok := ts.Interface().(time.Time); ok && current.IsZero() {
		ts.Set(reflect.ValueOf(now))
	}
}

func (api *StreamingAPI) registerAgentProfileTools(registrar definitionToolRegistrar, resolved *resolvedAgentProfile, userID, sessionID, workspacePath string) error {
	if resolved == nil {
		return nil
	}
	for _, binding := range resolved.Definition.Tools {
		tool, err := api.agentProfiles.BuildTool(binding, agentprofiles.ToolRuntimeContext{
			UserID: userID, SessionID: sessionID, WorkspacePath: workspacePath,
			Emit:         func(event any) { api.emitAgentProfileEvent(sessionID, event) },
			Presentation: binding.Presentation,
			Interaction:  binding.Interaction,
			Product:      resolved.Definition.Product,
		})
		if err != nil {
			return err
		}
		category := strings.TrimSpace(tool.Category)
		if category == "" {
			category = "agent_profile_tools"
		}
		if err := registrar.RegisterCustomTool(tool.Name, tool.Description, tool.Parameters, tool.Execute, category); err != nil {
			return fmt.Errorf("register profile tool %q: %w", tool.Name, err)
		}
	}
	return nil
}

func profileDisablesVirtualTool(profile *resolvedAgentProfile, toolName string) bool {
	if profile == nil {
		return false
	}
	for _, disabled := range profile.Definition.ToolPolicy.Disabled {
		if strings.EqualFold(strings.TrimSpace(disabled), strings.TrimSpace(toolName)) {
			return true
		}
	}
	return false
}

// agentProfileReadOnlyFolders is what a project-scoped profile may read but
// not write beyond its own workspace: the platform default (skills,
// subagents, Downloads, plus the workflow read-only set) unless the
// profile's sandbox policy names its own list. Entries are normalized to
// the trailing-slash form the folder guard compares with.
// agentProfileChatHistoryGrants is the account chat_history/ folder a product
// profile's shell may read and write — the platform default, so an assistant
// can recall earlier chats — or nothing when the profile's sandbox says
// chat_history: none. The server's own persistence of the profile's log
// does not go through the shell and is unaffected.
func agentProfileChatHistoryGrants(sandbox agentprofiles.SandboxPolicy, perUserChatHistory string) []string {
	if sandbox.ChatHistoryDenied() || strings.TrimSpace(perUserChatHistory) == "" {
		return nil
	}
	return []string{perUserChatHistory}
}

func agentProfileReadOnlyFolders(sandbox agentprofiles.SandboxPolicy, workflowReadOnlyFolders []string) []string {
	if sandbox.ReadOnly == nil {
		return append([]string{"skills/", "subagents/", "Downloads/"}, workflowReadOnlyFolders...)
	}
	out := make([]string, 0, len(sandbox.ReadOnly))
	for _, folder := range sandbox.ReadOnly {
		clean := strings.Trim(strings.TrimSpace(folder), "/")
		if clean == "" {
			continue
		}
		out = append(out, clean+"/")
	}
	return out
}
