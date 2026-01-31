package skills

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// WorkspaceAPIClient provides methods to interact with the workspace API
type WorkspaceAPIClient struct {
	BaseURL string
	Client  *http.Client
}

// NewWorkspaceAPIClient creates a new workspace API client
func NewWorkspaceAPIClient(baseURL string) *WorkspaceAPIClient {
	return &WorkspaceAPIClient{
		BaseURL: baseURL,
		Client:  &http.Client{},
	}
}

// DocumentsResponse represents the response from listing documents
type DocumentsResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    []DocumentEntry `json:"data"`
}

// DocumentEntry represents a file or folder in the workspace
type DocumentEntry struct {
	Filepath     string          `json:"filepath"`
	Type         string          `json:"type"` // "file" or "folder"
	LastModified string          `json:"last_modified,omitempty"`
	Children     []DocumentEntry `json:"children,omitempty"`
}

// DocumentContentResponse represents the response from reading a document
type DocumentContentResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Content      string `json:"content"`
		LastModified string `json:"last_modified"`
	} `json:"data"`
	Message string `json:"message"`
}

// ListFiles lists files in a folder via workspace API
func (c *WorkspaceAPIClient) ListFiles(folderPath string) ([]DocumentEntry, error) {
	reqURL := fmt.Sprintf("%s/api/documents?folder=%s", c.BaseURL, url.QueryEscape(folderPath))

	resp, err := c.Client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list files: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result DocumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("API returned error: %s", result.Message)
	}

	return result.Data, nil
}

// ReadFile reads a file's content via workspace API
func (c *WorkspaceAPIClient) ReadFile(filePath string) (string, error) {
	reqURL := fmt.Sprintf("%s/api/documents/%s", c.BaseURL, url.PathEscape(filePath))

	resp, err := c.Client.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to read file: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result DocumentContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("API returned error: %s", result.Message)
	}

	return result.Data.Content, nil
}

// WriteFile writes content to a file via workspace API
func (c *WorkspaceAPIClient) WriteFile(filePath, content string) error {
	reqURL := fmt.Sprintf("%s/api/documents/%s", c.BaseURL, url.PathEscape(filePath))

	body := map[string]string{
		"content": content,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to write file: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreateFolder creates a folder via workspace API
func (c *WorkspaceAPIClient) CreateFolder(folderPath string) error {
	reqURL := fmt.Sprintf("%s/api/folders", c.BaseURL)

	body := map[string]string{
		"folder_path": folderPath,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create folder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create folder: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteFolder deletes a folder via workspace API
func (c *WorkspaceAPIClient) DeleteFolder(folderPath string) error {
	reqURL := fmt.Sprintf("%s/api/folders/%s?confirm=true", c.BaseURL, url.PathEscape(folderPath))

	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete folder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete folder: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DiscoverSkills discovers all skills in the workspace, including those in skills/custom/
func DiscoverSkills(workspaceAPIURL string) ([]Skill, error) {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	// List all folders in the skills directory
	entries, err := client.ListFiles(SkillsBasePath)
	if err != nil {
		// If skills folder doesn't exist or workspace API is unreachable, return empty list
		return []Skill{}, nil
	}

	var skills []Skill

	// Helper to process a potential skill folder
	processSkillFolder := func(entry DocumentEntry, prefix string) {
		folderName := entry.Filepath
		if prefix != "" {
			// For nested skills, construct relative folder name
			// entry.Filepath is full path like "skills/custom/my-skill"
			// we want "custom/my-skill"
			parts := strings.Split(entry.Filepath, "/")
			if len(parts) >= 2 {
				// Take the last N parts based on prefix depth
				// But simpler: just strip "skills/" prefix
				relPath := strings.TrimPrefix(entry.Filepath, SkillsBasePath+"/")
				folderName = relPath
			}
		} else {
			folderName = path.Base(entry.Filepath)
		}

		// Try to read SKILL.md from this folder
		skillFilePath := path.Join(entry.Filepath, SkillFileName)
		content, err := client.ReadFile(skillFilePath)
		if err != nil {
			// Skip folders without SKILL.md
			return
		}

		// Parse the skill
		skill, err := ParseSkillFromContent(content, folderName, skillFilePath)
		if err != nil {
			// Log but skip invalid skills
			return
		}

		skills = append(skills, *skill)
	}

	// Process each entry in skills/
	for _, entry := range entries {
		if entry.Type != "folder" {
			continue
		}

		folderName := path.Base(entry.Filepath)

		// Check for "custom" folder
		if folderName == "custom" {
			// List contents of skills/custom
			customEntries, err := client.ListFiles(entry.Filepath)
			if err == nil {
				for _, customEntry := range customEntries {
					if customEntry.Type == "folder" {
						processSkillFolder(customEntry, "custom")
					}
				}
			}
			continue
		}

		// Process standard skill folder
		processSkillFolder(entry, "")
	}

	return skills, nil
}

// GetSkill retrieves a specific skill by folder name
func GetSkill(workspaceAPIURL, folderName string) (*Skill, error) {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	skillFilePath := path.Join(SkillsBasePath, folderName, SkillFileName)
	content, err := client.ReadFile(skillFilePath)
	if err != nil {
		return nil, fmt.Errorf("skill not found: %w", err)
	}

	skill, err := ParseSkillFromContent(content, folderName, skillFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse skill: %w", err)
	}

	return skill, nil
}

// DeleteSkill deletes a skill folder
func DeleteSkill(workspaceAPIURL, folderName string) error {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	skillFolderPath := path.Join(SkillsBasePath, folderName)
	if err := client.DeleteFolder(skillFolderPath); err != nil {
		return fmt.Errorf("failed to delete skill: %w", err)
	}

	return nil
}

// UpdateSkill updates a skill's SKILL.md content
func UpdateSkill(workspaceAPIURL, folderName, content string) (*Skill, error) {
	// Validate the content first
	frontmatter, body, err := ValidateSkillContent(content)
	if err != nil {
		return nil, fmt.Errorf("invalid skill content: %w", err)
	}

	client := NewWorkspaceAPIClient(workspaceAPIURL)

	skillFilePath := path.Join(SkillsBasePath, folderName, SkillFileName)
	if err := client.WriteFile(skillFilePath, content); err != nil {
		return nil, fmt.Errorf("failed to write skill: %w", err)
	}

	return &Skill{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    skillFilePath,
	}, nil
}
