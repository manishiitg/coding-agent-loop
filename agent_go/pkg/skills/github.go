package skills

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// GitHubURLInfo contains parsed information from a GitHub URL
type GitHubURLInfo struct {
	Owner  string
	Repo   string
	Branch string
	Path   string
}

// ParseGitHubURL parses a GitHub folder URL into its components
func ParseGitHubURL(rawURL string) (*GitHubURLInfo, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Host != "github.com" {
		return nil, fmt.Errorf("not a GitHub URL (host: %s)", parsed.Host)
	}

	// Parse path: /owner/repo/tree/branch/path/to/folder
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[2] != "tree" {
		return nil, fmt.Errorf("invalid GitHub URL format, expected: https://github.com/owner/repo/tree/branch/path")
	}

	owner := pathParts[0]
	repo := pathParts[1]
	branch := pathParts[3]
	folderPath := ""
	if len(pathParts) > 4 {
		folderPath = strings.Join(pathParts[4:], "/")
	}

	return &GitHubURLInfo{
		Owner:  owner,
		Repo:   repo,
		Branch: branch,
		Path:   folderPath,
	}, nil
}

// FetchGitHubFolderContents fetches the contents of a GitHub folder
func FetchGitHubFolderContents(info *GitHubURLInfo) ([]GitHubFileInfo, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		info.Owner, info.Repo, url.PathEscape(info.Path), info.Branch)

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub contents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var files []GitHubFileInfo
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return files, nil
}

// FetchGitHubFileContent fetches the content of a single file from GitHub
func FetchGitHubFileContent(downloadURL string) (string, error) {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch file: status %d, body: %s", resp.StatusCode, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read file content: %w", err)
	}

	return string(content), nil
}

// ValidateGitHubSkill validates a skill at a GitHub URL without importing it
func ValidateGitHubSkill(gitHubURL string) (*ValidateSkillResponse, error) {
	info, err := ParseGitHubURL(gitHubURL)
	if err != nil {
		return &ValidateSkillResponse{Valid: false, Error: err.Error()}, nil
	}

	files, err := FetchGitHubFolderContents(info)
	if err != nil {
		return &ValidateSkillResponse{Valid: false, Error: fmt.Sprintf("failed to fetch folder: %v", err)}, nil
	}

	var skillFile *GitHubFileInfo
	var fileNames []string
	for i := range files {
		fileNames = append(fileNames, files[i].Name)
		if files[i].Name == SkillFileName {
			skillFile = &files[i]
		}
	}

	if skillFile == nil {
		return &ValidateSkillResponse{Valid: false, Error: fmt.Sprintf("no %s found", SkillFileName), Files: fileNames}, nil
	}

	content, err := FetchGitHubFileContent(skillFile.DownloadURL)
	if err != nil {
		return &ValidateSkillResponse{Valid: false, Error: fmt.Sprintf("failed to fetch %s: %v", SkillFileName, err), Files: fileNames}, nil
	}

	frontmatter, _, err := ValidateSkillContent(content)
	if err != nil {
		return &ValidateSkillResponse{Valid: false, Error: fmt.Sprintf("invalid %s: %v", SkillFileName, err), Files: fileNames}, nil
	}

	return &ValidateSkillResponse{Valid: true, Frontmatter: frontmatter, Files: fileNames}, nil
}

// ImportGitHubSkill imports a skill from a GitHub URL to the workspace
func ImportGitHubSkill(workspaceAPIURL, gitHubURL string) (*ImportSkillResponse, error) {
	validation, err := ValidateGitHubSkill(gitHubURL)
	if err != nil {
		return &ImportSkillResponse{Success: false, Error: err.Error()}, nil
	}
	if !validation.Valid {
		return &ImportSkillResponse{Success: false, Error: validation.Error}, nil
	}

	info, err := ParseGitHubURL(gitHubURL)
	if err != nil {
		return &ImportSkillResponse{Success: false, Error: err.Error()}, nil
	}

	skillName := validation.Frontmatter.Name
	if skillName == "" {
		skillName = path.Base(info.Path)
	}
	skillName = sanitizeFolderName(skillName)

	client := NewWorkspaceAPIClient(workspaceAPIURL)
	skillFolderPath := path.Join(SkillsBasePath, skillName)
	
	if err := client.CreateFolder(skillFolderPath); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return &ImportSkillResponse{Success: false, Error: fmt.Sprintf("failed to create folder: %v", err)}, nil
		}
	}

	if err := downloadGitHubFolder(client, info, skillFolderPath); err != nil {
		return &ImportSkillResponse{Success: false, Error: fmt.Sprintf("failed to download: %v", err)}, nil
	}

	return &ImportSkillResponse{Success: true, SkillName: skillName}, nil
}

func downloadGitHubFolder(client *WorkspaceAPIClient, info *GitHubURLInfo, destPath string) error {
	files, err := FetchGitHubFolderContents(info)
	if err != nil {
		return err
	}

	for _, file := range files {
		destFilePath := path.Join(destPath, file.Name)

		if file.Type == "dir" {
			if err := client.CreateFolder(destFilePath); err != nil {
				if !strings.Contains(err.Error(), "exists") {
					return fmt.Errorf("failed to create folder %s: %w", destFilePath, err)
				}
			}
			subInfo := &GitHubURLInfo{Owner: info.Owner, Repo: info.Repo, Branch: info.Branch, Path: file.Path}
			if err := downloadGitHubFolder(client, subInfo, destFilePath); err != nil {
				return err
			}
		} else if file.Type == "file" {
			content, err := FetchGitHubFileContent(file.DownloadURL)
			if err != nil {
				return fmt.Errorf("failed to download %s: %w", file.Name, err)
			}
			if err := client.WriteFile(destFilePath, content); err != nil {
				return fmt.Errorf("failed to write %s: %w", destFilePath, err)
			}
		}
	}
	return nil
}

func sanitizeFolderName(name string) string {
	name = strings.ReplaceAll(name, " ", "-")
	reg := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	name = reg.ReplaceAllString(name, "")
	name = strings.ToLower(name)
	if name == "" {
		name = "skill"
	}
	return name
}
