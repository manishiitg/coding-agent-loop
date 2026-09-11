package agentprofiles

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/productdeps"
)

// ProductManifest is a product.yaml. A product declares one profile under
// `profile:` (its prompt under the top-level `prompt:`), and may declare
// more under `profiles:`, each carrying its own `prompt:`. Every profile of
// a product shares the product's dependencies, branding and workflows; what
// differs per profile is the prompt, tools, skills, commands and runtime
// policy (including the sandbox), which is exactly what a parent and a
// child profile of one product need.
// ChatCapabilityPolicy declares host capabilities by chat mode and caller origin.
// Effective capabilities are their intersection, further restricted for read-only users.
type ChatCapabilityPolicy struct {
	Modes    map[string][]string `yaml:"modes"`
	Origins  map[string][]string `yaml:"origins"`
	ReadOnly []string            `yaml:"read_only"`
}

type ProductManifest struct {
	ChatPolicy    *ChatCapabilityPolicy `yaml:"chat_policy,omitempty"`
	SchemaVersion int                   `yaml:"schema_version"`
	Dependencies  productdeps.Manifest  `yaml:"dependencies"`
	// Prompt is the primary profile's prompt source.
	Prompt PromptSource `yaml:"prompt"`
	// Profile is the primary profile (the one the product surface opens).
	Profile Profile `yaml:"profile"`
	// Profiles are further profiles of the same product.
	Profiles []Profile `yaml:"profiles,omitempty"`
	Branding struct {
		Icon    string `yaml:"icon"`
		Favicon string `yaml:"favicon"`
	} `yaml:"branding"`
	UI struct {
		Surface       string `yaml:"surface"`
		FilesPanel    bool   `yaml:"files_panel"`
		WorkflowPanel bool   `yaml:"workflow_panel"`
		Secrets       bool   `yaml:"secrets"`
		Streaming     string `yaml:"streaming"`
	} `yaml:"ui"`
	Workflows struct {
		Enabled        []string `yaml:"enabled"`
		SelectedSkills []string `yaml:"selected_skills"`
		BrowserMode    string   `yaml:"browser_mode"`
	} `yaml:"workflows"`
}

// LoadProductManifest decodes product.yaml from a product's embedded files
// with unknown keys rejected, validates the dependency manifest, resolves
// every profile's command prompt files, and checks that the profiles are
// distinct and each has a prompt.
func LoadProductManifest(fsys fs.FS, path string) (ProductManifest, error) {
	var manifest ProductManifest
	contents, err := fs.ReadFile(fsys, path)
	if err != nil {
		return manifest, fmt.Errorf("read product manifest: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("parse product manifest: %w", err)
	}
	if manifest.SchemaVersion != 2 {
		return manifest, fmt.Errorf("invalid product manifest: schema_version must be 2")
	}
	if err := productdeps.Validate(manifest.Dependencies); err != nil {
		return manifest, fmt.Errorf("invalid dependencies: %w", err)
	}
	if strings.TrimSpace(manifest.Profile.ID) == "" {
		return manifest, fmt.Errorf("invalid product manifest: profile.id is required")
	}
	if strings.TrimSpace(manifest.Profile.Prompt.File) == "" {
		manifest.Profile.Prompt = manifest.Prompt
	}
	seen := map[string]struct{}{manifest.Profile.ID: {}}
	for i := range manifest.Profiles {
		extra := &manifest.Profiles[i]
		if strings.TrimSpace(extra.ID) == "" {
			return manifest, fmt.Errorf("invalid product manifest: profiles[%d].id is required", i)
		}
		if _, dup := seen[extra.ID]; dup {
			return manifest, fmt.Errorf("invalid product manifest: duplicate profile id %q", extra.ID)
		}
		seen[extra.ID] = struct{}{}
		if strings.TrimSpace(extra.Prompt.File) == "" {
			return manifest, fmt.Errorf("invalid product manifest: profile %q needs its own prompt.file", extra.ID)
		}
	}
	for _, profile := range manifest.AllProfiles() {
		if strings.TrimSpace(profile.Prompt.File) == "" {
			return manifest, fmt.Errorf("invalid product manifest: profile %q has no prompt file", profile.ID)
		}
		if _, err := fs.Stat(fsys, profile.Prompt.File); err != nil {
			return manifest, fmt.Errorf("profile %q prompt file %q: %w", profile.ID, profile.Prompt.File, err)
		}
		if err := ResolveCommandPrompts(fsys, profile.Commands); err != nil {
			return manifest, fmt.Errorf("profile %q: %w", profile.ID, err)
		}
	}
	return manifest, nil
}

// AllProfiles returns the primary profile followed by the extra ones.
func (m ProductManifest) AllProfiles() []Profile {
	out := make([]Profile, 0, 1+len(m.Profiles))
	out = append(out, m.Profile)
	out = append(out, m.Profiles...)
	return out
}

// FindProfile returns the declared profile with the given id.
func (m ProductManifest) FindProfile(id string) (Profile, bool) {
	for _, p := range m.AllProfiles() {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

// RenderPrompt reads a profile's prompt file and applies its manifest
// variables plus any extra ones (a product passes Go-template placeholders
// here when its prompt is rendered again per turn, see RenderPrompt).
func (m ProductManifest) RenderPrompt(fsys fs.FS, profile Profile, extra map[string]string) (string, error) {
	file := strings.TrimSpace(profile.Prompt.File)
	if file == "" {
		return "", fmt.Errorf("profile %q has no prompt file", profile.ID)
	}
	contents, err := fs.ReadFile(fsys, file)
	if err != nil {
		return "", fmt.Errorf("read prompt %q: %w", file, err)
	}
	values := map[string]string{}
	for name, value := range profile.Prompt.Variables {
		values[name] = value
	}
	for name, value := range extra {
		values[name] = value
	}
	return RenderPromptTemplate(string(contents), values), nil
}

// BuiltinProfiles returns every declared profile with its system prompt
// rendered, ready to register. Command prompts were already resolved by
// LoadProductManifest.
func (m ProductManifest) BuiltinProfiles(fsys fs.FS, extra map[string]string) ([]Profile, error) {
	profiles := m.AllProfiles()
	for i := range profiles {
		prompt, err := m.RenderPrompt(fsys, profiles[i], extra)
		if err != nil {
			return nil, err
		}
		profiles[i].SystemPromptTemplate = prompt
	}
	return profiles, nil
}
