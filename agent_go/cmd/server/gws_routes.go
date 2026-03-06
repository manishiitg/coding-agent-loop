package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/skills"
)

// handleGWSAuthStatus runs `gws auth status` and returns the parsed JSON.
// Returns { configured: false, error: "..." } if gws is not installed or auth not set up.
func (api *StreamingAPI) handleGWSAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	cmd := exec.Command("gws", "auth", "status")
	output, err := cmd.Output()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"configured": false,
			"error":      fmt.Sprintf("gws not found or auth error: %v", err),
		})
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"configured": false,
			"error":      fmt.Sprintf("failed to parse gws output: %v", err),
		})
		return
	}
	result["configured"] = true
	json.NewEncoder(w).Encode(result)
}

type gwsSyncResult struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// handleGWSSyncSkills fetches all gws-* skills from the googleworkspace/cli GitHub repo
// and imports them into the workspace skills directory.
func (api *StreamingAPI) handleGWSSyncSkills(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	workspaceAPIURL := getWorkspaceAPIURL()

	// List all dirs in the skills/ folder of the googleworkspace/cli repo
	repoInfo := &skills.GitHubURLInfo{
		Owner:  "googleworkspace",
		Repo:   "cli",
		Branch: "main",
		Path:   "skills",
	}
	entries, err := skills.FetchGitHubFolderContents(repoInfo)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"synced": 0,
			"error":  fmt.Sprintf("failed to list GitHub skills: %v", err),
		})
		return
	}

	var synced int
	var failed []gwsSyncResult

	// Only sync skills for the 6 configured services + shared (prerequisite)
	configured := []string{"drive", "gmail", "calendar", "docs", "sheets", "slides", "shared"}

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
		githubURL := fmt.Sprintf("https://github.com/googleworkspace/cli/tree/main/skills/%s", entry.Name)
		log.Printf("[GWS_SYNC] Importing skill: %s", entry.Name)
		result, err := skills.ImportGitHubSkill(workspaceAPIURL, githubURL, "")
		if err != nil {
			failed = append(failed, gwsSyncResult{Name: entry.Name, Error: err.Error()})
			continue
		}
		if !result.Success {
			failed = append(failed, gwsSyncResult{Name: entry.Name, Error: result.Error})
			continue
		}
		synced++
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"synced": synced,
		"failed": failed,
	})
}
