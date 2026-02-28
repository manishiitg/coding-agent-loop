package subagents

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// SubAgentsBasePath is the base path for sub-agent templates in the workspace
const SubAgentsBasePath = "subagents"

// SubAgentFileName is the required file name for sub-agent template definitions
const SubAgentFileName = "SUBAGENT.md"

// FlexString is a string type that can be unmarshaled from either a YAML/JSON string
// or an array of strings (joined with commas). This supports both formats:
//
//	skills: "skill1, skill2"         (string)
//	skills: ["skill1", "skill2"]     (array)
type FlexString string

func (f *FlexString) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.SequenceNode {
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		*f = FlexString(strings.Join(items, ", "))
		return nil
	}
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	*f = FlexString(s)
	return nil
}

func (f *FlexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	var items []string
	if err := json.Unmarshal(data, &items); err == nil {
		*f = FlexString(strings.Join(items, ", "))
		return nil
	}
	return nil
}

func (f FlexString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(f))
}

func (f FlexString) String() string {
	return string(f)
}

// SubAgentFrontmatter represents the YAML frontmatter in SUBAGENT.md
type SubAgentFrontmatter struct {
	Name                  string     `yaml:"name" json:"name"`
	Description           string     `yaml:"description" json:"description"`
	DefaultReasoningLevel string     `yaml:"default_reasoning_level,omitempty" json:"default_reasoning_level,omitempty"`
	DefaultToolMode       string     `yaml:"default_tool_mode,omitempty" json:"default_tool_mode,omitempty"`
}

// SubAgent represents a complete sub-agent template with parsed content
type SubAgent struct {
	Frontmatter SubAgentFrontmatter `json:"frontmatter"`
	Content     string              `json:"content"`     // Markdown content after frontmatter
	FolderName  string              `json:"folder_name"` // Sub-agent folder name
	FilePath    string              `json:"file_path"`   // Relative path in workspace
}

// UpdateSubAgentRequest represents a request to update a sub-agent template
type UpdateSubAgentRequest struct {
	Content string `json:"content"` // Full SUBAGENT.md content (frontmatter + body)
}

// ListSubAgentsResponse represents the response from listing sub-agent templates
type ListSubAgentsResponse struct {
	SubAgents []SubAgent `json:"subagents"`
	Total     int        `json:"total"`
}

// ParseCSV splits a comma-separated string into trimmed values, filtering empty strings
func ParseCSV(s FlexString) []string {
	if string(s) == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(string(s), ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// ImportSubAgentRequest represents a request to import a subagent from GitHub
type ImportSubAgentRequest struct {
	GitHubURL   string `json:"github_url"`              // e.g., https://github.com/user/repo/tree/main/subagents/my-agent
	GitHubToken string `json:"github_token,omitempty"`  // Optional PAT for private repos
}

// ImportSubAgentResponse represents the response from importing a subagent
type ImportSubAgentResponse struct {
	Success      bool   `json:"success"`
	SubAgentName string `json:"subagent_name,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ValidateSubAgentRequest represents a request to validate a subagent URL
type ValidateSubAgentRequest struct {
	GitHubURL   string `json:"github_url"`
	GitHubToken string `json:"github_token,omitempty"` // Optional PAT for private repos
}

// ValidateSubAgentResponse represents the response from validating a subagent URL
type ValidateSubAgentResponse struct {
	Valid       bool                 `json:"valid"`
	Frontmatter *SubAgentFrontmatter `json:"frontmatter,omitempty"`
	Error       string               `json:"error,omitempty"`
	Files       []string             `json:"files,omitempty"` // List of files in the subagent folder
	Exists      bool                 `json:"exists"`          // True if a subagent with this name already exists
}

