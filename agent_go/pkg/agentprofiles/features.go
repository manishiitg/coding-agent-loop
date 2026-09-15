package agentprofiles

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FeatureBinding enables one shared platform feature for a product profile.
// The scalar YAML form is intentionally the common case:
//
//	features: [files, browser, mcp]
//
// The object form leaves room for feature-specific configuration without
// adding another top-level product manifest field for every feature:
//
//   - id: schedules
//     options: {mode: message_only}
type FeatureBinding struct {
	ID      string            `json:"id" yaml:"id"`
	Enabled *bool             `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Options map[string]string `json:"options,omitempty" yaml:"options,omitempty"`
}

func (b FeatureBinding) IsEnabled() bool { return b.Enabled == nil || *b.Enabled }

// UnmarshalYAML accepts either a short scalar feature id or the expanded
// object form while retaining strict unknown-field checks.
func (b *FeatureBinding) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		b.ID = strings.TrimSpace(node.Value)
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			switch node.Content[i].Value {
			case "id":
				if err := node.Content[i+1].Decode(&b.ID); err != nil {
					return err
				}
			case "enabled":
				if err := node.Content[i+1].Decode(&b.Enabled); err != nil {
					return err
				}
			case "options":
				if err := node.Content[i+1].Decode(&b.Options); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown feature field %q", node.Content[i].Value)
			}
		}
		b.ID = strings.TrimSpace(b.ID)
		return nil
	default:
		return fmt.Errorf("feature must be an id or object")
	}
}

// ResolvedFeature is the load-bearing, client-visible result of a feature
// declaration. PromptExtension is appended to (never substituted for) the
// product's base system prompt.
type ResolvedFeature struct {
	ID              string                           `json:"id"`
	Dependencies    []string                         `json:"dependencies,omitempty"`
	Tools           []string                         `json:"tools,omitempty"`
	Skills          []string                         `json:"skills,omitempty"`
	PromptExtension string                           `json:"prompt_extension,omitempty"`
	UIPanels        []string                         `json:"ui_panels,omitempty"`
	Capabilities    map[string]CapabilityRequirement `json:"capabilities,omitempty"`
	Options         map[string]string                `json:"options,omitempty"`
}

type featureDefinition struct {
	Dependencies    []string
	Tools           []string
	Skills          []string
	PromptExtension string
	UIPanels        []string
	Capabilities    map[string]CapabilityRequirement
}

// featureCatalog is the single backend contract for the platform features
// shared by product profiles. Existing tools and UI components are referenced;
// the bundle layer does not fork their implementations.
var featureCatalog = map[string]featureDefinition{
	"live-chat": {
		Capabilities: map[string]CapabilityRequirement{
			"live_input": CapabilityPreferred, "warm_session": CapabilityPreferred,
			"new_conversation": CapabilityPreferred,
		},
		PromptExtension: "Live chat is enabled. Accept steering while a turn is running and keep each durable project conversation resumable.",
	},
	"coding": {
		Tools:           []string{"diff_patch_workspace_file", "execute_shell_command", "read_image", "search_web_llm"},
		Skills:          []string{"code-reviewer"},
		PromptExtension: "Coding tools are enabled. Inspect existing files before editing, keep changes scoped to the request, and validate in proportion to risk.",
	},
	"files": {
		Tools:           []string{"diff_patch_workspace_file", "read_image"},
		UIPanels:        []string{"files"},
		PromptExtension: "Files are enabled. Treat the current project folder as the durable source of truth and preserve unrelated user files.",
	},
	"browser": {
		Tools:           []string{"agent_browser"},
		Skills:          []string{"agent-browser"},
		UIPanels:        []string{"browser"},
		Capabilities:    map[string]CapabilityRequirement{"browser": CapabilityPreferred},
		PromptExtension: "Managed browser access is enabled. When browser work is needed, read the attached `agent-browser` skill before acting; live browser status is authoritative.",
	},
	"secrets": {
		Tools:           []string{"list_secrets", "set_workflow_secret", "delete_workflow_secret", "set_user_secret", "delete_user_secret"},
		Skills:          []string{"work-integrations"},
		UIPanels:        []string{"secrets"},
		Capabilities:    map[string]CapabilityRequirement{"secrets": CapabilityPreferred},
		PromptExtension: "Secret management is enabled. Read the attached `work-integrations` skill before managing secrets. Use dedicated secret tools, refer to credentials only by name, and never print or store secret values in project files.",
	},
	"mcp": {
		Tools:           []string{"list_mcp_servers", "search_mcp_catalog", "install_mcp_server", "add_mcp_server", "remove_mcp_server", "get_mcp_server_logs", "trigger_mcp_discovery"},
		Skills:          []string{"work-integrations"},
		UIPanels:        []string{"mcp"},
		Capabilities:    map[string]CapabilityRequirement{"mcp_selection": CapabilityPreferred},
		PromptExtension: "MCP integrations are enabled. Read the attached `work-integrations` skill before managing MCP servers. Distinguish account installation from project selection and use only connected, explicitly selected servers.",
	},
	"skills": {
		Tools:           []string{"list_skills", "search_skills", "install_skill", "import_skill", "uninstall_skill"},
		Skills:          []string{"work-skills"},
		UIPanels:        []string{"skills"},
		Capabilities:    map[string]CapabilityRequirement{"skill_selection": CapabilityPreferred},
		PromptExtension: "Reusable skills are enabled. Read the attached `work-skills` skill before managing skills. Discover and select an existing skill when possible. When the user explicitly asks to preserve or improve a repeated procedure, the normal Work agent may create or update a focused skill under `skills/custom/`; do not switch its identity to Skill Builder.",
	},
	"attached-folders": {
		Dependencies:    []string{"files"},
		Tools:           []string{"list_work_folders", "attach_work_folder", "detach_work_folder"},
		Skills:          []string{"work-integrations", "work-workflow-files"},
		UIPanels:        []string{"folders"},
		PromptExtension: "Administrator-authorized attached folders are enabled. Read `work-integrations` before managing grants and `work-workflow-files` before reading attached content. Use the least access required and inspect the current grants before changing them.",
	},
	"workflow-references": {
		Tools:           []string{"list_accessible_workflows", "attach_workflow_reference", "detach_workflow_reference"},
		Skills:          []string{"work-workflow-files"},
		Capabilities:    map[string]CapabilityRequirement{"workflow_references": CapabilityPreferred},
		PromptExtension: "Read-only AgentWorks workflow references are enabled. Read the attached `work-workflow-files` skill before discovering, managing, or reading them. A # selection applies to one message; a workflow linked under Attached folders is durable for the project. Treat both as context only, and never edit or execute the referenced workflow from this product.",
	},
	"terminal": {
		Capabilities:    map[string]CapabilityRequirement{"raw_terminal": CapabilityPreferred},
		PromptExtension: "The selected coding CLI's native terminal is enabled inside the project sandbox. Terminal access does not widen project, secret, or user authorization.",
	},
	"voice": {
		Capabilities: map[string]CapabilityRequirement{"voice": CapabilityPreferred},
	},
	"models": {
		Skills:          []string{"work-integrations"},
		UIPanels:        []string{"models"},
		PromptExtension: "Project model selection is enabled. Read the attached `work-integrations` skill before changing provider or model configuration. Use the configured provider and model for this conversation; never claim another runtime based on model self-identification.",
	},
	"schedules": {
		Tools:           []string{"list_project_schedules", "create_project_schedule", "update_project_schedule", "delete_project_schedule", "trigger_project_schedule"},
		Skills:          []string{"work-schedules-and-bots"},
		UIPanels:        []string{"schedules"},
		PromptExtension: "Project schedules are enabled in message-only mode. Read the attached `work-schedules-and-bots` skill before managing schedules. A schedule sends one message to the project chat; it is not a workflow execution.",
	},
	"triggers": {
		Dependencies:    []string{"schedules"},
		Tools:           []string{"list_project_triggers", "create_project_trigger", "update_project_trigger", "delete_project_trigger"},
		Skills:          []string{"work-schedules-and-bots"},
		UIPanels:        []string{"triggers"},
		PromptExtension: "Authenticated webhook triggers are enabled. Read the attached `work-schedules-and-bots` skill before managing triggers. A trigger stores one instruction in product.json and sends it to the project chat with the authenticated delivery payload; it does not run workflow routes.",
	},
	"bots": {
		Tools:           []string{"google_workspace_cli", "list_gmail_connections", "update_gmail_connection_grants"},
		Skills:          []string{"work-schedules-and-bots"},
		UIPanels:        []string{"bots"},
		Capabilities:    map[string]CapabilityRequirement{"whatsapp": CapabilityPreferred},
		PromptExtension: "Slack and WhatsApp project-chat bots plus connected Gmail/Google Workspace accounts are enabled through the shared connector infrastructure. Read `work-schedules-and-bots` before using them. For Google Workspace, check installed skills and install `https://github.com/openclaw/gogcli` with `install_skill` only when its current CLI guidance is needed; then load `gog` and the relevant `gog-*` service skill with `read_skill`. Invoke that syntax through `google_workspace_cli` without the `gog` binary or account/auth flags because the server supplies those securely. Check existing Gmail connections before searching for an MCP server; mailbox reads require an observed Gmail read grant.",
	},
	"database": {
		Tools:           []string{"query_workflow_db", "mutate_workflow_db", "apply_workflow_db_migration", "create_workflow_database_snapshot"},
		Skills:          []string{"work-dashboard"},
		UIPanels:        []string{"database"},
		PromptExtension: "A project-scoped managed database is enabled. Read the attached `work-dashboard` skill before using it. Use the database tools and idempotent migrations; never edit SQLite, WAL, or SHM files directly.",
	},
	"dashboard": {
		Dependencies:    []string{"database", "files"},
		Tools:           []string{"validate_report_html", "preview_report"},
		Skills:          []string{"work-dashboard"},
		UIPanels:        []string{"dashboard"},
		PromptExtension: "A visual Dashboard is enabled. Read the attached `work-dashboard` skill before creating or changing it. It may contain multiple HTML views under db/reports/ with optional views.json metadata; the shared toolbar handles navigation. The viewer locally supports opt-in daisyUI. Use the managed data contract and validate every changed view.",
	},
	"costs": {
		UIPanels: []string{"costs"},
	},
	"background-work": {
		Tools:           []string{"run_in_background", "query_agent", "list_agents", "terminate_agent"},
		Skills:          []string{"background-work"},
		PromptExtension: "Background work is enabled. Read the attached `background-work` skill before delegating. Delegate only bounded independent tasks, rely on automatic completion notifications, and do not poll unless the user asks for status.",
	},
	"workspace-ui": {
		Tools:           []string{"open_workspace_view", "refresh_workspace_view", "list_ui_capabilities", "get_ui_state", "perform_ui_action", "get_ui_action_result"},
		PromptExtension: "The interactive Work chat can present its right-side workspace views. Use open_workspace_view after creating or discussing something the user should inspect, and trust only an applied browser acknowledgement.",
	},
}

// ResolveFeatures expands a profile's feature declarations into its existing
// flattened fields. This compatibility projection lets all current backend and
// frontend consumers keep working while the feature list becomes the source of
// truth. Calling it more than once is idempotent.
func ResolveFeatures(profile *Profile) error {
	if profile == nil || len(profile.Features) == 0 {
		return nil
	}
	requested := make(map[string]FeatureBinding, len(profile.Features))
	order := make([]string, 0, len(profile.Features))
	for _, binding := range profile.Features {
		id := strings.TrimSpace(binding.ID)
		if id == "" {
			return fmt.Errorf("feature id is required")
		}
		if _, ok := featureCatalog[id]; !ok {
			return fmt.Errorf("unknown feature %q", id)
		}
		if _, duplicate := requested[id]; duplicate {
			return fmt.Errorf("duplicate feature %q", id)
		}
		requested[id] = binding
		if binding.IsEnabled() {
			order = append(order, id)
		}
	}

	resolved := make([]ResolvedFeature, 0, len(order))
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("feature dependency cycle at %q", id)
		}
		definition, ok := featureCatalog[id]
		if !ok {
			return fmt.Errorf("unknown feature dependency %q", id)
		}
		visiting[id] = true
		for _, dependency := range definition.Dependencies {
			if binding, explicitlySet := requested[dependency]; explicitlySet && !binding.IsEnabled() {
				return fmt.Errorf("feature %q requires disabled feature %q", id, dependency)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		binding := requested[id]
		resolved = append(resolved, ResolvedFeature{
			ID: id, Dependencies: cloneStrings(definition.Dependencies), Tools: cloneStrings(definition.Tools),
			Skills: cloneStrings(definition.Skills), PromptExtension: definition.PromptExtension,
			UIPanels: cloneStrings(definition.UIPanels), Capabilities: cloneCapabilities(definition.Capabilities),
			Options: cloneStringMap(binding.Options),
		})
		return nil
	}
	for _, id := range order {
		if err := visit(id); err != nil {
			return err
		}
	}

	profile.ResolvedFeatures = resolved
	for _, feature := range resolved {
		profile.Skills = appendUnique(profile.Skills, feature.Skills...)
		profile.ToolPolicy.Enabled = appendUnique(profile.ToolPolicy.Enabled, feature.Tools...)
		for name, requirement := range feature.Capabilities {
			setFeatureCapability(&profile.Runtime.Capabilities, name, requirement)
		}
		for _, panel := range feature.UIPanels {
			setLegacyUIPanel(&profile.UIPanels, panel)
		}
	}
	return nil
}

// FeaturePromptExtensions returns the ordered prompt additions for a resolved
// profile. The server feeds these only to AddInstructions after it has applied
// the product's base prompt.
func FeaturePromptExtensions(profile Profile) []string {
	result := make([]string, 0, len(profile.ResolvedFeatures))
	for _, feature := range profile.ResolvedFeatures {
		if text := strings.TrimSpace(feature.PromptExtension); text != "" {
			result = append(result, "## Feature: "+feature.ID+"\n\n"+text)
		}
	}
	return result
}

// HasFeature reports whether featureID is present in the resolved feature set.
// Runtime/tool registration uses this rather than product IDs, so enabling a
// bundle is what activates its backend contribution.
func HasFeature(profile Profile, featureID string) bool {
	featureID = strings.TrimSpace(featureID)
	for _, feature := range profile.ResolvedFeatures {
		if feature.ID == featureID {
			return true
		}
	}
	return false
}

// AvailableFeatures is useful to admin/product tooling and keeps catalog
// ordering deterministic.
func AvailableFeatures() []string {
	ids := make([]string, 0, len(featureCatalog))
	for id := range featureCatalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func setFeatureCapability(c *RuntimeCapabilities, name string, requirement CapabilityRequirement) {
	switch name {
	case "live_input":
		c.LiveInput = requirement
	case "raw_terminal":
		c.RawTerminal = requirement
	case "warm_session":
		c.WarmSession = requirement
	case "workflow_execution":
		c.WorkflowExecution = requirement
	case "browser":
		c.Browser = requirement
	case "secrets":
		c.Secrets = requirement
	case "mcp_selection":
		c.MCPSelection = requirement
	case "skill_selection":
		c.SkillSelection = requirement
	case "workflow_references":
		c.WorkflowReferences = requirement
	case "voice":
		c.Voice = requirement
	case "new_conversation":
		c.NewConversation = requirement
	case "whatsapp":
		c.WhatsApp = requirement
	}
}

func setLegacyUIPanel(p *UIPanels, name string) {
	switch name {
	case "files":
		p.Files = true
	case "secrets":
		p.Secrets = true
	case "schedules":
		p.Schedules = true
	}
}

func appendUnique(current []string, additions ...string) []string {
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

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneCapabilities(values map[string]CapabilityRequirement) map[string]CapabilityRequirement {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]CapabilityRequirement, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
