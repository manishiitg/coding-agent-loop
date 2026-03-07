package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// CheckGWSAuthStatus runs `gws auth status` and returns the parsed JSON.
// GET /api/gws-auth-status
func CheckGWSAuthStatus(c *gin.Context) {
	cmd := exec.Command("gws", "auth", "status")
	output, err := cmd.Output()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"configured": false,
			"error":      fmt.Sprintf("gws not found or auth error: %v", err),
		})
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"configured": false,
			"error":      fmt.Sprintf("failed to parse gws output: %v", err),
		})
		return
	}
	result["configured"] = true
	c.JSON(http.StatusOK, result)
}

// gwsGitHubFileInfo represents a file entry from the GitHub Contents API
type gwsGitHubFileInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	DownloadURL string `json:"download_url,omitempty"`
}

// SyncGWSSkills fetches all gws-* skills from the googleworkspace/cli GitHub repo
// and writes them directly into workspace-docs/skills/.
// POST /api/gws-sync-skills
func SyncGWSSkills(c *gin.Context) {
	docsDir := viper.GetString("docs-dir")

	// List all dirs in the skills/ folder of the googleworkspace/cli repo
	entries, err := fetchGitHubContents("googleworkspace", "cli", "main", "skills")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"synced": 0,
			"error":  fmt.Sprintf("failed to list GitHub skills: %v", err),
		})
		return
	}

	// Only sync skills for the 6 configured services + shared (prerequisite)
	configured := []string{"drive", "gmail", "calendar", "docs", "sheets", "slides", "shared"}

	var synced int
	var failed []gin.H

	for _, entry := range entries {
		if entry.Type != "dir" || !strings.HasPrefix(entry.Name, "gws-") {
			continue
		}
		skillService := strings.TrimPrefix(entry.Name, "gws-")
		matched := false
		for _, svc := range configured {
			if skillService == svc || strings.HasPrefix(skillService, svc+"-") {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		skillPath := filepath.Join(docsDir, "skills", entry.Name)
		log.Printf("[GWS_SYNC] Importing skill: %s -> %s", entry.Name, skillPath)

		if err := os.MkdirAll(skillPath, 0755); err != nil {
			failed = append(failed, gin.H{"name": entry.Name, "error": err.Error()})
			continue
		}

		if err := downloadGitHubFolder("googleworkspace", "cli", "main", "skills/"+entry.Name, skillPath); err != nil {
			failed = append(failed, gin.H{"name": entry.Name, "error": err.Error()})
			continue
		}
		synced++
	}

	c.JSON(http.StatusOK, gin.H{
		"synced": synced,
		"failed": failed,
	})
}

// fetchGitHubContents fetches the contents of a GitHub folder via the Contents API
func fetchGitHubContents(owner, repo, branch, path string) ([]gwsGitHubFileInfo, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		owner, repo, url.PathEscape(path), branch)

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub contents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var files []gwsGitHubFileInfo
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}
	return files, nil
}

// downloadGitHubFolder recursively downloads a GitHub folder to a local directory
func downloadGitHubFolder(owner, repo, branch, ghPath, destDir string) error {
	entries, err := fetchGitHubContents(owner, repo, branch, ghPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		destPath := filepath.Join(destDir, entry.Name)
		if entry.Type == "dir" {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create dir %s: %w", destPath, err)
			}
			if err := downloadGitHubFolder(owner, repo, branch, ghPath+"/"+entry.Name, destPath); err != nil {
				return err
			}
		} else if entry.DownloadURL != "" {
			resp, err := http.Get(entry.DownloadURL)
			if err != nil {
				return fmt.Errorf("failed to download %s: %w", entry.Name, err)
			}
			content, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", entry.Name, err)
			}
			if err := os.WriteFile(destPath, content, 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", destPath, err)
			}
		}
	}
	return nil
}
