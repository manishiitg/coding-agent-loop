package subagents

import "strings"

// SubAgentsBasePath is the base path for sub-agent templates in the workspace
const SubAgentsBasePath = "subagents"

// SubAgentFileName is the required file name for sub-agent template definitions
const SubAgentFileName = "SUBAGENT.md"

// SubAgentFrontmatter represents the YAML frontmatter in SUBAGENT.md
type SubAgentFrontmatter struct {
	Name                  string `yaml:"name" json:"name"`
	Description           string `yaml:"description" json:"description"`
	DefaultReasoningLevel string `yaml:"default_reasoning_level,omitempty" json:"default_reasoning_level,omitempty"`
	DefaultToolMode       string `yaml:"default_tool_mode,omitempty" json:"default_tool_mode,omitempty"`
	Skills                string `yaml:"skills,omitempty" json:"skills,omitempty"`   // Comma-separated skill folder names
	Servers               string `yaml:"servers,omitempty" json:"servers,omitempty"` // Comma-separated MCP server names
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
func ParseCSV(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
