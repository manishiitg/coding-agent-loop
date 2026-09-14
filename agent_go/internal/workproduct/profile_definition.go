package workproduct

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

// BuiltinAgentProfile returns the work profile. Brand new, so only one
// version is ever registered -- same reasoning as every other product's
// own BuiltinAgentProfile.
func BuiltinAgentProfile() agentprofiles.Profile {
	manifest := mustWorkManifest()
	profile := manifest.Profile
	profile.SystemPromptTemplate = renderProductPrompt()
	return profile
}

func BuiltinAgentProfiles() []agentprofiles.Profile {
	return []agentprofiles.Profile{BuiltinAgentProfile()}
}

var registerProductSkillsOnce sync.Once
var registerProductSkillsErr error

var productSkills = []agentprofiles.SkillFileBinding{
	{Name: "work-integrations", Description: "Connect and manage Work MCP servers, secrets, browser access, models, and administrator-authorized server folders.", Path: "skills/work-integrations/SKILL.md"},
	{Name: "work-skills", Description: "Discover, install, import, create, select, and remove reusable skills in Work.", Path: "skills/work-skills/SKILL.md"},
	{Name: "work-schedules-and-bots", Description: "Manage Work's message-only schedules, authenticated webhook triggers, and Slack or WhatsApp project-chat bots.", Path: "skills/work-schedules-and-bots/SKILL.md"},
	{Name: "work-dashboard", Description: "Create and maintain a general-purpose visual dashboard for a Work project.", Path: "skills/work-dashboard/SKILL.md"},
	{Name: "background-work", Description: "Run a bounded task asynchronously and rely on Work's automatic completion notification instead of polling.", Path: "skills/background-work/SKILL.md"},
}

// RegisterProductSkills adds Work's platform-operation contracts. General
// browser, coding, review, and skill-authoring skills still come from the
// existing AgentWorks registry.
func RegisterProductSkills() error {
	registerProductSkillsOnce.Do(func() {
		registerProductSkillsErr = agentprofiles.RegisterEmbeddedSkills(productConfigFiles, productSkills)
	})
	return registerProductSkillsErr
}

type workIdentity struct {
	Icon         string `json:"icon,omitempty"`
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type workProjectManifest struct {
	Identity  *workIdentity `json:"identity,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
}

const (
	workIdentityIconLimit         = 8
	workIdentityNameLimit         = 60
	workIdentityRoleLimit         = 120
	workIdentityInstructionsLimit = 500
)

func normalizeWorkIdentity(identity workIdentity) workIdentity {
	identity.Icon = strings.TrimSpace(identity.Icon)
	identity.Name = strings.TrimSpace(identity.Name)
	identity.Role = strings.TrimSpace(identity.Role)
	identity.Instructions = strings.TrimSpace(identity.Instructions)
	return identity
}

func renderWorkIdentity(identity workIdentity) string {
	identity = normalizeWorkIdentity(identity)
	if identity.Icon == "" && identity.Name == "" && identity.Role == "" && identity.Instructions == "" {
		return ""
	}
	var lines []string
	if identity.Icon != "" {
		lines = append(lines, "Icon: "+identity.Icon)
	}
	if identity.Name != "" {
		lines = append(lines, "Name: "+identity.Name)
	}
	if identity.Role != "" {
		lines = append(lines, "Role: "+identity.Role)
	}
	if identity.Instructions != "" {
		lines = append(lines, "Instructions:\n"+identity.Instructions)
	}
	return strings.Join(lines, "\n")
}

func validateWorkIdentity(identity workIdentity) string {
	limits := []struct {
		label string
		value string
		max   int
	}{
		{label: "icon", value: identity.Icon, max: workIdentityIconLimit},
		{label: "name", value: identity.Name, max: workIdentityNameLimit},
		{label: "role", value: identity.Role, max: workIdentityRoleLimit},
		{label: "instructions", value: identity.Instructions, max: workIdentityInstructionsLimit},
	}
	for _, field := range limits {
		if utf8.RuneCountInString(field.value) > field.max {
			return fmt.Sprintf("The identity %s must be at most %d characters.", field.label, field.max)
		}
	}
	return ""
}

func workIdentityFactory(workspaceAPIURL string) agentprofiles.ToolFactory {
	return func(runtime agentprofiles.ToolRuntimeContext, _ json.RawMessage) (agentprofiles.ToolSpec, error) {
		client := workspace.NewClient(
			workspaceAPIURL,
			workspace.WithUserID(runtime.UserID),
			workspace.WithExtraEnv(map[string]string{"MCP_SESSION_ID": runtime.SessionID}),
		)
		manifestPath := path.Join(runtime.WorkspacePath, "product.json")
		return agentprofiles.ToolSpec{
			Name:     "set_work_identity",
			Category: "work_identity",
			Description: "Set, update, or clear this Work project's agent identity when the user asks. " +
				"A short name, icon, role, and instructions keep the agent consistent across project chats, schedules, bots, and background work; they change presentation and behavior, never permissions. " +
				"Store it in product.json, preserve omitted fields, and never invent an identity.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type": "string", "enum": []string{"set", "clear"},
						"description": "Use set to create or update the identity, or clear to remove it.",
					},
					"icon":         map[string]interface{}{"type": "string", "maxLength": workIdentityIconLimit, "description": "One emoji or short glyph. Omit to preserve; pass empty to remove."},
					"name":         map[string]interface{}{"type": "string", "maxLength": workIdentityNameLimit, "description": "Short display name. Omit to preserve."},
					"role":         map[string]interface{}{"type": "string", "maxLength": workIdentityRoleLimit, "description": "Short role or purpose. Omit to preserve."},
					"instructions": map[string]interface{}{"type": "string", "maxLength": workIdentityInstructionsLimit, "description": "Brief behavior, tone, or working preferences. Omit to preserve."},
				},
				"required": []string{"operation"},
			},
			Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
				read, err := client.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: manifestPath})
				if err != nil {
					return "", fmt.Errorf("read Work project configuration: %w", err)
				}
				var manifest map[string]interface{}
				if err := json.Unmarshal([]byte(read.Content), &manifest); err != nil {
					return "", fmt.Errorf("decode Work project configuration: %w", err)
				}
				operation, _ := args["operation"].(string)
				operation = strings.ToLower(strings.TrimSpace(operation))
				if operation == "clear" {
					delete(manifest, "identity")
				} else if operation == "set" {
					identity := workIdentity{}
					if current, ok := manifest["identity"]; ok {
						encoded, _ := json.Marshal(current)
						_ = json.Unmarshal(encoded, &identity)
					}
					if value, ok := args["name"].(string); ok {
						identity.Name = value
					}
					if value, ok := args["icon"].(string); ok {
						identity.Icon = value
					}
					if value, ok := args["role"].(string); ok {
						identity.Role = value
					}
					if value, ok := args["instructions"].(string); ok {
						identity.Instructions = value
					}
					identity = normalizeWorkIdentity(identity)
					if validationError := validateWorkIdentity(identity); validationError != "" {
						return validationError, nil
					}
					if renderWorkIdentity(identity) == "" {
						return "At least one of icon, name, role, or instructions is required to set the identity.", nil
					}
					manifest["identity"] = identity
				} else {
					return "operation must be set or clear.", nil
				}
				manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)
				encoded, err := json.MarshalIndent(manifest, "", "  ")
				if err != nil {
					return "", fmt.Errorf("encode Work project configuration: %w", err)
				}
				if _, err := client.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{Filepath: manifestPath, Content: string(encoded) + "\n"}); err != nil {
					return "", fmt.Errorf("save Work identity: %w", err)
				}
				if operation == "clear" {
					if runtime.Emit != nil {
						runtime.Emit(map[string]interface{}{"type": "work_identity_updated"})
					}
					return "The project agent identity was removed. Use the base Work identity from now on.", nil
				}
				var saved workIdentity
				encodedIdentity, _ := json.Marshal(manifest["identity"])
				_ = json.Unmarshal(encodedIdentity, &saved)
				if runtime.Emit != nil {
					runtime.Emit(map[string]interface{}{"type": "work_identity_updated"})
				}
				return "The project bot identity is saved. Adopt it immediately in this chat. It will be included automatically in generated provider project instructions on future provider sessions.\n\n" + renderWorkIdentity(saved), nil
			},
		}, nil
	}
}

// RegisterAgentProfileRuntime connects Work's durable chat-configurable
// identity to both the provider prompt and its one product-owned tool.
func RegisterAgentProfileRuntime(registry *agentprofiles.Registry, workspaceAPIURL string) error {
	if err := registry.RegisterToolFactory("work.set-identity", workIdentityFactory(workspaceAPIURL)); err != nil {
		return err
	}
	return registry.RegisterPromptVariables("work", func(ctx context.Context, runtime agentprofiles.RuntimeContext) (map[string]string, error) {
		client := workspace.NewClient(
			workspaceAPIURL,
			workspace.WithUserID(runtime.UserID),
			workspace.WithExtraEnv(map[string]string{"MCP_SESSION_ID": runtime.SessionID}),
		)
		result, err := client.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: path.Join(runtime.WorkspacePath, "product.json")})
		if err != nil {
			return nil, fmt.Errorf("read Work identity: %w", err)
		}
		var manifest workProjectManifest
		if err := json.Unmarshal([]byte(result.Content), &manifest); err != nil {
			return nil, fmt.Errorf("decode Work identity: %w", err)
		}
		identity := ""
		if manifest.Identity != nil {
			identity = renderWorkIdentity(*manifest.Identity)
		}
		// Always return the key because the Work prompt uses missingkey=error.
		return map[string]string{"WORK_IDENTITY": identity}, nil
	})
}
