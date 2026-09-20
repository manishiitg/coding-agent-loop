package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

// CreateCrewRequest is the validated input for server-side Crew creation from
// Builder chat: identity, starter brief, capability selections, wiring, and
// the idempotency scope.
type CreateCrewRequest struct {
	UserID       string
	WorkflowPath string
	ProfileID    string
	Title        string
	Description  string
	Icon         string
	Purpose      string
	Instructions string
	Skills       []string
	Servers      []string
	// Secrets and GlobalSecrets are references to existing authorized secret
	// records by name. Values are never accepted: the schema has no
	// value-bearing fields, so plaintext cannot enter by construction.
	Secrets       []string
	GlobalSecrets []string
	// Alias defaults to the slugified crew title (suffixed when reserved).
	Alias string
	// TriggerName defaults to "<title> trigger"; TriggerMessage falls back to
	// Instructions, then Purpose, and is required after fallback.
	TriggerName    string
	TriggerMessage string
	// StepID defaults to "crew-<slug>"; StepTitle defaults to the crew title;
	// StepInstruction is required: the workflow-specific instruction is the
	// Builder's core input for the step it adds next.
	StepID              string
	StepTitle           string
	StepInstruction     string
	ContextDependencies []string
	// IdempotencyKey scopes one Builder proposal. Retrying with the same key
	// and title returns the existing crew instead of minting a duplicate; a
	// changed proposal must use a new key.
	IdempotencyKey string
}

// CreatedCrewStepConfig is the exact crew-step configuration the Builder
// passes to add_step(type="crew") as the immediate follow-up, adding only
// placement (insert_after_step_id) and reason. The step is deliberately not
// written here: plan mutations flow through the gated tool path so schedule
// collision and phase policies apply.
type CreatedCrewStepConfig struct {
	StepID              string   `json:"step_id"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	CrewProfileID       string   `json:"crew_profile_id"`
	CrewProjectID       string   `json:"crew_project_id"`
	TriggerID           string   `json:"trigger_id"`
	Instruction         string   `json:"instruction"`
	ContextDependencies []string `json:"context_dependencies"`
}

// CreatedCrew is the durable result of CreateCrewProject. Duplicate reports
// an idempotent re-entry: the returned crew already existed under the key.
type CreatedCrew struct {
	CrewID          string
	Title           string
	WorkspacePath   string
	ManifestPath    string
	SessionID       string
	TriggerID       string
	AttachmentAlias string
	Step            CreatedCrewStepConfig
	Duplicate       bool
}

// crewCreationNamespace derives stable project IDs from idempotency keys so
// retried proposals resolve to the same crew without a stored receipt.
var crewCreationNamespace = uuid.MustParse("7c9e2f4a-1b3d-4e5f-8a6b-9c0d1e2f3a4b")

// CreateCrewProject creates a Crew project server-side, mirroring the UI
// creation layout byte-for-byte (product.json identity, workflow.json runtime
// manifest, code/ folder) so UI-created and Builder-created crews are
// indistinguishable. The creating workflow is pre-seeded in the crew's
// workflow_context_paths (always mounted read-only). First chat open binds
// the session and lazily initializes the database, exactly as for UI crews.
func (s *ProductScheduleService) CreateCrewProject(ctx context.Context, req CreateCrewRequest) (CreatedCrew, error) {
	userID := strings.TrimSpace(req.UserID)
	workflowPath := strings.Trim(strings.TrimSpace(req.WorkflowPath), "/")
	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = "work"
	}
	title := strings.TrimSpace(req.Title)
	key := strings.TrimSpace(req.IdempotencyKey)
	if userID == "" {
		return CreatedCrew{}, fmt.Errorf("crew creation requires a user")
	}
	if err := validateCrewCreationWorkflowPath(workflowPath); err != nil {
		return CreatedCrew{}, err
	}
	if title == "" || len([]rune(title)) > 60 {
		return CreatedCrew{}, fmt.Errorf("crew title must be 1-60 characters")
	}
	if len([]rune(strings.TrimSpace(req.Description))) > 2000 {
		return CreatedCrew{}, fmt.Errorf("crew description must be at most 2000 characters")
	}
	if len([]rune(strings.TrimSpace(req.Icon))) > 8 {
		return CreatedCrew{}, fmt.Errorf("crew icon must be at most 8 characters")
	}
	if len([]rune(strings.TrimSpace(req.Purpose))) > 2000 {
		return CreatedCrew{}, fmt.Errorf("crew purpose must be at most 2000 characters")
	}
	if len([]rune(strings.TrimSpace(req.Instructions))) > 8000 {
		return CreatedCrew{}, fmt.Errorf("crew instructions must be at most 8000 characters")
	}
	if key == "" || len(key) > 128 {
		return CreatedCrew{}, fmt.Errorf("crew creation requires an idempotency key of 1-128 characters")
	}
	if profileID != "work" {
		return CreatedCrew{}, fmt.Errorf("crew creation currently supports only the work profile")
	}
	if _, err := authorizeWorkflowContextPaths(ctx, []string{workflowPath}); err != nil {
		return CreatedCrew{}, fmt.Errorf("creating workflow is unavailable or access denied: %w", err)
	}
	if _, exists, err := ReadWorkflowManifest(ctx, workflowPath); err != nil || !exists {
		return CreatedCrew{}, fmt.Errorf("creating workflow is unavailable or access denied")
	}
	if s == nil || s.registry == nil {
		return CreatedCrew{}, fmt.Errorf("product profiles are unavailable")
	}
	profile, err := s.registry.Resolve(profileID, 0, userID)
	if err != nil {
		return CreatedCrew{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(profile.Runtime.Conversation.Mode), agentprofiles.ConversationModeKeyed) {
		return CreatedCrew{}, fmt.Errorf("profile %q does not support crew projects", profileID)
	}
	projectsRoot, err := cleanAgentProfileWorkspace(profile.Runtime.Workspace.ProjectsRoot, userID)
	if err != nil {
		return CreatedCrew{}, fmt.Errorf("invalid product projects root: %w", err)
	}
	runtimeRoot := agentProfileRuntimeWorkspace(userID, projectsRoot)
	skills, err := validateCrewCreationSkills(req.Skills)
	if err != nil {
		return CreatedCrew{}, err
	}
	servers, err := validateCrewCreationNames("server", req.Servers)
	if err != nil {
		return CreatedCrew{}, err
	}
	secrets, err := validateCrewCreationNames("secret", req.Secrets)
	if err != nil {
		return CreatedCrew{}, err
	}
	globalSecrets, err := validateCrewCreationNames("global secret", req.GlobalSecrets)
	if err != nil {
		return CreatedCrew{}, err
	}
	wiring, err := validateCrewCreationWiring(req, title)
	if err != nil {
		return CreatedCrew{}, err
	}

	slug := slugifyCrewTitle(title)
	crewID := uuid.NewSHA1(crewCreationNamespace, []byte("crew-creation:"+key)).String()
	suffix := crewCreationSuffix(key)
	workspacePath := filepath.ToSlash(filepath.Join(runtimeRoot, slug+"-"+suffix))
	created := CreatedCrew{}
	if existing, ok, err := readCreatedCrew(ctx, workspacePath, crewID); err != nil {
		return CreatedCrew{}, err
	} else if ok {
		// Idempotent re-entry adopts the existing crew and continues
		// through the phases below, so a retry after a partial failure
		// still converges instead of stranding the crew half-built.
		existing.Duplicate = true
		created = existing
	} else {
		if projectExistsAtPath(ctx, workspacePath) {
			// Path collision with an unrelated crew (same slug, same
			// suffix): fall back to random identity rather than adopting it.
			crewID = uuid.NewString()
			workspacePath = filepath.ToSlash(filepath.Join(runtimeRoot, slug+"-"+uuid.NewString()[:8]))
		}
		created, err = writeCrewCreationManifests(ctx, userID, profile, profileID, workflowPath, title, req.Description, req.Icon, crewID, workspacePath)
		if err != nil {
			return CreatedCrew{}, err
		}
	}

	if err := applyCrewCreationStarter(ctx, created.WorkspacePath, title, workflowPath, req.Purpose, req.Instructions); err != nil {
		return CreatedCrew{}, err
	}
	if err := applyCrewCreationSelections(ctx, profileID, created.WorkspacePath, skills, servers, secrets, globalSecrets); err != nil {
		return CreatedCrew{}, err
	}
	if err := applyCrewCreationWiring(ctx, s, userID, profileID, workflowPath, created.CrewID, wiring, &created); err != nil {
		return CreatedCrew{}, err
	}
	return created, nil
}

// crewCreationWiring is the validated wiring resolved from a creation request.
type crewCreationWiring struct {
	alias           string
	triggerName     string
	triggerMessage  string
	stepID          string
	stepTitle       string
	stepInstruction string
	contextDeps     []string
}

func validateCrewCreationWiring(req CreateCrewRequest, title string) (crewCreationWiring, error) {
	out := crewCreationWiring{}
	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		alias = slugifyCrewTitle(title)
		if err := workflowtypes.ValidateCrewAttachmentAlias(alias); err != nil {
			alias += "-crew"
		}
	}
	if err := workflowtypes.ValidateCrewAttachmentAlias(alias); err != nil {
		return out, err
	}
	out.alias = alias
	out.triggerName = strings.TrimSpace(req.TriggerName)
	if out.triggerName == "" {
		out.triggerName = title + " trigger"
	}
	if len([]rune(out.triggerName)) > 120 {
		return out, fmt.Errorf("trigger name must be at most 120 characters")
	}
	out.triggerMessage = strings.TrimSpace(req.TriggerMessage)
	if out.triggerMessage == "" {
		out.triggerMessage = strings.TrimSpace(req.Instructions)
	}
	if out.triggerMessage == "" {
		out.triggerMessage = strings.TrimSpace(req.Purpose)
	}
	if out.triggerMessage == "" {
		return out, fmt.Errorf("trigger message, instructions, or purpose is required")
	}
	if len([]rune(out.triggerMessage)) > 8000 {
		return out, fmt.Errorf("trigger message must be at most 8000 characters")
	}
	out.stepID = strings.TrimSpace(req.StepID)
	if out.stepID == "" {
		out.stepID = "crew-" + slugifyCrewTitle(title)
	}
	if len(out.stepID) > 120 {
		return out, fmt.Errorf("step id must be at most 120 characters")
	}
	out.stepTitle = strings.TrimSpace(req.StepTitle)
	if out.stepTitle == "" {
		out.stepTitle = title
	}
	if len([]rune(out.stepTitle)) > 120 {
		return out, fmt.Errorf("step title must be at most 120 characters")
	}
	out.stepInstruction = strings.TrimSpace(req.StepInstruction)
	if out.stepInstruction == "" {
		return out, fmt.Errorf("step instruction is required")
	}
	if len([]rune(out.stepInstruction)) > 8000 {
		return out, fmt.Errorf("step instruction must be at most 8000 characters")
	}
	deps, err := validateCrewCreationNames("context dependency", req.ContextDependencies)
	if err != nil {
		return out, err
	}
	out.contextDeps = deps
	return out, nil
}

// applyCrewCreationWiring creates the crew's workflow-bound trigger, attaches
// the crew read-only, and builds the step configuration for the Builder's
// follow-up add_step call. Trigger and attachment adoption make re-entry
// converge: matching records are reused, never duplicated.
func applyCrewCreationWiring(ctx context.Context, svc *ProductScheduleService, userID, profileID, workflowPath, crewID string, wiring crewCreationWiring, created *CreatedCrew) error {
	workflowID, err := boundWorkflowID(ctx, workflowPath)
	if err != nil {
		return err
	}
	triggerID, err := ensureCrewCreationTrigger(ctx, svc, userID, profileID, crewID, workflowID, wiring.triggerName, wiring.triggerMessage)
	if err != nil {
		return err
	}
	if _, _, err := attachCrewProjectToWorkflow(ctx, svc, userID, workflowPath, profileID, crewID, wiring.alias); err != nil {
		return err
	}
	created.TriggerID = triggerID
	created.AttachmentAlias = wiring.alias
	created.Step = CreatedCrewStepConfig{
		StepID:              wiring.stepID,
		Title:               wiring.stepTitle,
		Description:         strings.TrimSpace(created.Title) + " via the " + wiring.alias + " crew attachment",
		CrewProfileID:       profileID,
		CrewProjectID:       crewID,
		TriggerID:           triggerID,
		Instruction:         wiring.stepInstruction,
		ContextDependencies: wiring.contextDeps,
	}
	return nil
}

// ensureCrewCreationTrigger returns the crew's internal trigger for the
// creating workflow, creating one (isolated, enabled, caller-bound) when the
// same-named binding does not already exist.
func ensureCrewCreationTrigger(ctx context.Context, svc *ProductScheduleService, userID, profileID, projectID, workflowID, name, message string) (string, error) {
	if svc == nil {
		return "", fmt.Errorf("product schedules are unavailable")
	}
	triggers, err := svc.projectWebhookConfigs(ctx, userID, profileID, projectID)
	if err != nil {
		return "", err
	}
	for _, trigger := range triggers {
		if !trigger.IsInternal() || trigger.Caller == nil {
			continue
		}
		if strings.TrimSpace(trigger.Caller.Type) != triggerCallerWorkflow || strings.TrimSpace(trigger.Caller.ID) != workflowID {
			continue
		}
		if strings.TrimSpace(trigger.Name) == name {
			return trigger.ID, nil
		}
	}
	response, _, err := svc.saveProductWebhookConfig(ctx, userID, productWebhookRequest{
		ProfileID: profileID, ProjectID: projectID, Name: name, Message: message,
		Enabled: true, RunDestination: runDestinationIsolated,
		Kind:   triggerKindInternal,
		Caller: &triggerCaller{Type: triggerCallerWorkflow, ID: workflowID},
	}, "")
	if err != nil {
		return "", err
	}
	return response.ID, nil
}

// writeCrewCreationManifests writes a fresh crew's runtime manifest, identity
// manifest, and code folder, mirroring the UI creation layout.
func writeCrewCreationManifests(ctx context.Context, userID string, profile agentprofiles.Profile, profileID, workflowPath, title, description, icon, crewID, workspacePath string) (CreatedCrew, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sessionID := profileID + ":project:" + crewID
	description = strings.TrimSpace(description)
	icon = strings.TrimSpace(icon)
	if icon == "" {
		icon = strings.ToUpper(string([]rune(title)[:1]))
		if icon == "" {
			icon = "C"
		}
	}
	capabilities := map[string]interface{}{
		"selected_servers":             []string{},
		"selected_tools":               []string{},
		"selected_skills":              []string{},
		"selected_secrets":             []string{},
		"selected_global_secret_names": []string{},
		"browser_mode":                 "auto",
		"use_code_execution_mode":      false,
	}
	if llmConfig := defaultCrewLLMConfig(profile); llmConfig != nil {
		capabilities["llm_config"] = llmConfig
	}
	runtimeManifest := map[string]interface{}{
		"schema_version":         1,
		"id":                     crewID,
		"label":                  title,
		"capabilities":           capabilities,
		"workflow_context_paths": []string{workflowPath},
		"schedules":              []string{},
		"triggers":               []string{},
		"created_at":             now,
		"updated_at":             now,
	}
	manifest := map[string]interface{}{
		"schema_version": 1,
		"product":        profileID,
		"id":             crewID,
		"title":          title,
		"description":    description,
		"session_id":     sessionID,
		"created_at":     now,
		"updated_at":     now,
		"identity":       map[string]interface{}{"name": title, "icon": icon},
	}
	if err := writeCrewCreationManifest(ctx, filepath.ToSlash(filepath.Join(workspacePath, "workflow.json")), runtimeManifest); err != nil {
		return CreatedCrew{}, err
	}
	manifestPath := filepath.ToSlash(filepath.Join(workspacePath, "product.json"))
	if err := writeCrewCreationManifest(ctx, manifestPath, manifest); err != nil {
		return CreatedCrew{}, err
	}
	client := workspace.NewClient(getWorkspaceAPIURL(), workspace.WithUserID(userID))
	if err := client.CreateFolder(ctx, filepath.ToSlash(filepath.Join(workspacePath, "code"))); err != nil {
		return CreatedCrew{}, fmt.Errorf("initialize crew code folder: %w", err)
	}
	return CreatedCrew{CrewID: crewID, Title: title, WorkspacePath: workspacePath, ManifestPath: manifestPath, SessionID: sessionID}, nil
}

// defaultCrewLLMConfig mirrors the UI creation default: the profile's
// default provider option, else its first option. A nil return omits
// llm_config and the profile default applies at chat time.
func defaultCrewLLMConfig(profile agentprofiles.Profile) map[string]interface{} {
	options := profile.Runtime.ProviderOptions
	if len(options) == 0 {
		return nil
	}
	selected := options[0]
	for _, option := range options {
		if option.Default {
			selected = option
			break
		}
	}
	provider := strings.TrimSpace(selected.Provider)
	modelID := strings.TrimSpace(selected.ModelID)
	if provider == "" || modelID == "" {
		return nil
	}
	builder := map[string]interface{}{"provider": provider, "model_id": modelID}
	effort := ""
	if raw, ok := selected.Options["reasoning_effort"].(string); ok && strings.TrimSpace(raw) != "" {
		effort = strings.TrimSpace(raw)
	} else if len(selected.ReasoningEfforts) > 0 {
		effort = strings.TrimSpace(selected.ReasoningEfforts[0])
	}
	if effort != "" {
		builder["options"] = map[string]interface{}{"reasoning_effort": effort}
	}
	return map[string]interface{}{"schema_version": 2, "mode": "explicit", "builder_llm": builder}
}

// applyCrewCreationStarter seeds the crew's project-root MEMORY.md with the
// Builder's brief when one was supplied. Crews are instructed to read
// MEMORY.md before reporting project information unknown, so this is the
// channel that actually reaches the specialist; overwriting keeps retries
// idempotent.
func applyCrewCreationStarter(ctx context.Context, workspacePath, title, workflowPath, purpose, instructions string) error {
	purpose = strings.TrimSpace(purpose)
	instructions = strings.TrimSpace(instructions)
	if purpose == "" && instructions == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("# " + strings.TrimSpace(title) + "\n\n")
	b.WriteString("## " + time.Now().UTC().Format("2006-01-02") + " — Crew brief\n\n")
	b.WriteString("Created by Workflow Builder for workflow `" + workflowPath + "`.\n\n")
	if purpose != "" {
		b.WriteString("Purpose: " + purpose + "\n\n")
	}
	if instructions != "" {
		b.WriteString("Starter instructions from the Builder:\n\n" + instructions + "\n")
	}
	if err := writeRawFileToWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "MEMORY.md")), b.String()); err != nil {
		return fmt.Errorf("write crew starter brief: %w", err)
	}
	return nil
}

// applyCrewCreationSelections records capability selections through the
// existing product writers with union merges, so re-entry converges without
// duplicating entries. Empty lists leave the manifest defaults untouched.
func applyCrewCreationSelections(ctx context.Context, profileID, workspacePath string, skills, servers, secrets, globalSecrets []string) error {
	merge := func(add []string) func([]string) []string {
		return func(current []string) []string {
			seen := make(map[string]struct{}, len(current)+len(add))
			out := make([]string, 0, len(current)+len(add))
			for _, name := range append(append([]string{}, current...), add...) {
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				out = append(out, name)
			}
			return out
		}
	}
	if len(skills) > 0 {
		if err := updateProductSelectedSkills(ctx, profileID, workspacePath, merge(skills)); err != nil {
			return fmt.Errorf("select crew skills: %w", err)
		}
	}
	if len(servers) > 0 {
		if err := updateProductSelectedServers(ctx, profileID, workspacePath, merge(servers)); err != nil {
			return fmt.Errorf("select crew servers: %w", err)
		}
	}
	if len(secrets) > 0 {
		if err := updateProductSelectedSecrets(ctx, profileID, workspacePath, merge(secrets)); err != nil {
			return fmt.Errorf("select crew secrets: %w", err)
		}
	}
	if len(globalSecrets) > 0 {
		if err := updateProductSelectedGlobalSecrets(ctx, profileID, workspacePath, merge(globalSecrets)); err != nil {
			return fmt.Errorf("select crew global secrets: %w", err)
		}
	}
	return nil
}

// validateCrewCreationSkills trims, dedupes, and verifies each skill is
// installed. Builders must list or install skills before proposing them;
// hallucinating a skill name fails fast here instead of recording a broken
// reference. A transient read failure also rejects, safely: creation is
// retryable under the same key.
func validateCrewCreationSkills(names []string) ([]string, error) {
	checked, err := validateCrewCreationNames("skill", names)
	if err != nil {
		return nil, err
	}
	for _, name := range checked {
		if _, err := skills.GetSkill(getWorkspaceAPIURL(), name); err != nil {
			return nil, fmt.Errorf("crew skill %q is not installed; install it first or drop it: %w", name, err)
		}
	}
	return checked, nil
}

// validateCrewCreationNames trims, dedupes, and shape-checks capability
// references. Names are references only: non-empty, bounded, and free of
// path separators and control characters. Server and secret availability is
// a crew-UI concern surfaced through the pending-connections flow, not a
// creation-time rejection.
func validateCrewCreationNames(kind string, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || len(name) > 128 || strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") {
			return nil, fmt.Errorf("invalid crew %s reference %q", kind, raw)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

// validateCrewCreationWorkflowPath mirrors the workflow_context_paths rule:
// the creating workflow must be addressable as Workflow/<folder>, since its
// path is seeded into the crew manifest verbatim.
func validateCrewCreationWorkflowPath(workflowPath string) error {
	parts := strings.Split(workflowPath, "/")
	if len(parts) != 2 || parts[0] != "Workflow" || parts[1] == "" || parts[1] == "." || parts[1] == ".." || strings.ContainsAny(workflowPath, "\\\x00") {
		return fmt.Errorf("creating workflow must be a Workflow/<folder> path")
	}
	return nil
}

// slugifyCrewTitle mirrors the UI slugifyTitle for ASCII titles: lowercase,
// non-alphanumeric runs become dashes, trimmed and capped at 48 characters.
// It deliberately skips Unicode NFKD folding (no new dependency for a
// cosmetic segment; the id suffix carries uniqueness).
func slugifyCrewTitle(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !ok {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		b.WriteRune(r)
		lastDash = false
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		return "workspace"
	}
	return slug
}

// crewCreationSuffix derives the stable 8-hex directory suffix from the
// idempotency key.
func crewCreationSuffix(key string) string {
	sum := sha256.Sum256([]byte("crew-creation-path:" + key))
	return hex.EncodeToString(sum[:])[:8]
}

// readCreatedCrew returns the existing crew when the candidate path already
// holds the crew minted under this key (verified by project ID, never by
// path alone).
func readCreatedCrew(ctx context.Context, workspacePath, crewID string) (CreatedCrew, bool, error) {
	raw, found, err := readFileFromWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "product.json")))
	if err != nil || !found {
		return CreatedCrew{}, false, err
	}
	var manifest productProjectManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return CreatedCrew{}, false, nil
	}
	if strings.TrimSpace(manifest.ID) != crewID {
		return CreatedCrew{}, false, nil
	}
	return CreatedCrew{
		CrewID:        strings.TrimSpace(manifest.ID),
		Title:         strings.TrimSpace(manifest.Title),
		WorkspacePath: workspacePath,
		ManifestPath:  filepath.ToSlash(filepath.Join(workspacePath, "product.json")),
		SessionID:     strings.TrimSpace(manifest.SessionID),
	}, true, nil
}

// projectExistsAtPath reports whether any crew already occupies a path,
// regardless of which key minted it.
func projectExistsAtPath(ctx context.Context, workspacePath string) bool {
	_, found, err := readFileFromWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "product.json")))
	return err == nil && found
}

func writeCrewCreationManifest(ctx context.Context, path string, manifest map[string]interface{}) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeRawFileToWorkspace(ctx, path, string(encoded)+"\n"); err != nil {
		return fmt.Errorf("write crew manifest %s: %w", path, err)
	}
	return nil
}
