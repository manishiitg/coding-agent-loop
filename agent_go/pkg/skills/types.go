package skills

// SkillFrontmatter represents the YAML frontmatter in SKILL.md
type SkillFrontmatter struct {
	Name         string   `yaml:"name" json:"name"`
	Description  string   `yaml:"description" json:"description"`
	ArgumentHint string   `yaml:"argument-hint,omitempty" json:"argument_hint,omitempty"`
	AllowedTools []string `yaml:"allowed-tools,omitempty" json:"allowed_tools,omitempty"`
	Model        string   `yaml:"model,omitempty" json:"model,omitempty"`
}

// Skill represents a complete skill with parsed content
type Skill struct {
	Frontmatter SkillFrontmatter `json:"frontmatter"`
	Content     string           `json:"content"`      // Markdown content after frontmatter
	FolderName  string           `json:"folder_name"`  // Skill folder name
	FilePath    string           `json:"file_path"`    // Relative path in workspace
	SourceURL   string           `json:"source_url,omitempty"` // Original GitHub URL if imported
}

// ImportSkillRequest represents a request to import a skill from GitHub
type ImportSkillRequest struct {
	GitHubURL string `json:"github_url"` // e.g., https://github.com/user/repo/tree/main/skills/my-skill
}

// ImportSkillResponse represents the response from importing a skill
type ImportSkillResponse struct {
	Success   bool   `json:"success"`
	SkillName string `json:"skill_name,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ValidateSkillRequest represents a request to validate a skill URL
type ValidateSkillRequest struct {
	GitHubURL string `json:"github_url"`
}

// ValidateSkillResponse represents the response from validating a skill URL
type ValidateSkillResponse struct {
	Valid       bool              `json:"valid"`
	Frontmatter *SkillFrontmatter `json:"frontmatter,omitempty"`
	Error       string            `json:"error,omitempty"`
	Files       []string          `json:"files,omitempty"` // List of files in the skill folder
}

// UpdateSkillRequest represents a request to update a skill
type UpdateSkillRequest struct {
	Content string `json:"content"` // Full SKILL.md content (frontmatter + body)
}

// ListSkillsResponse represents the response from listing skills
type ListSkillsResponse struct {
	Skills []Skill `json:"skills"`
	Total  int     `json:"total"`
}

// GitHubFileInfo represents a file in a GitHub repository
type GitHubFileInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	DownloadURL string `json:"download_url,omitempty"`
	URL         string `json:"url,omitempty"` // API URL for directories
}

// SkillsBasePath is the base path for skills in the workspace
const SkillsBasePath = "skills"

// SkillFileName is the required file name for skill definitions
const SkillFileName = "SKILL.md"
