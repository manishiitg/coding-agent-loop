package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CLIConfig holds configuration for the Vercel skills CLI runner
type CLIConfig struct {
	NPXPath    string // path to npx binary
	TimeoutSec int    // command timeout in seconds (default 120)
	mu         sync.Mutex
}

// CLILockEntry matches one entry in skills-lock.json
type CLILockEntry struct {
	Source       string `json:"source"`
	SourceType   string `json:"sourceType"`
	ComputedHash string `json:"computedHash"`
}

// CLILockFile matches the skills-lock.json schema
type CLILockFile struct {
	Version int                     `json:"version"`
	Skills  map[string]CLILockEntry `json:"skills"`
}

// CLIImportResult contains the results of a CLI import operation
type CLIImportResult struct {
	InstalledSkills []string               `json:"installed_skills"`
	LockEntries     map[string]CLILockEntry `json:"lock_entries,omitempty"`
	Errors          []string               `json:"errors,omitempty"`
}

// CLIUpdateInfo represents update availability for one skill
type CLIUpdateInfo struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	HasUpdate bool   `json:"has_update"`
}

// NewCLIConfig creates a new CLI config, checking that npx is available
func NewCLIConfig() (*CLIConfig, error) {
	npxPath, err := exec.LookPath("npx")
	if err != nil {
		// Try common locations
		for _, p := range []string{"/usr/local/bin/npx", "/usr/bin/npx"} {
			if _, statErr := os.Stat(p); statErr == nil {
				npxPath = p
				break
			}
		}
		if npxPath == "" {
			return nil, fmt.Errorf("npx not found: %w. Install Node.js to use skills CLI", err)
		}
	}
	return &CLIConfig{
		NPXPath:    npxPath,
		TimeoutSec: 120,
	}, nil
}

// IsAvailable returns true if the skills CLI is available
func IsAvailable() bool {
	_, err := NewCLIConfig()
	return err == nil
}

// AddSkills installs skills from a source (owner/repo, URL, etc.) into a temp directory,
// then returns the installed skill folders for the caller to copy to workspace.
// Returns the temp directory path (caller must clean up) and installed skill info.
func (c *CLIConfig) AddSkills(ctx context.Context, source string) (tempDir string, result *CLIImportResult, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result = &CLIImportResult{
		LockEntries: make(map[string]CLILockEntry),
	}

	// Create temp directory for CLI operations
	tempDir, err = os.MkdirTemp("", "skills-cli-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Parse source — handle owner/repo@skill-name format
	cliSource := source
	skillFilter := "*"
	if atIdx := strings.LastIndex(source, "@"); atIdx > 0 {
		beforeAt := source[:atIdx]
		afterAt := source[atIdx+1:]
		// Only treat as skill filter if beforeAt looks like owner/repo (has a slash, no colon)
		if strings.Contains(beforeAt, "/") && !strings.Contains(beforeAt, ":") && afterAt != "" {
			cliSource = beforeAt
			skillFilter = afterAt
		}
	}

	// Run npx skills add
	timeout := time.Duration(c.TimeoutSec) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, c.NPXPath, "skills", "add", cliSource,
		"--agent", "universal",
		"--skill", skillFilter,
		"--copy",
		"-y",
	)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Don't clean up tempDir on error — caller needs to see what happened
		log.Printf("[SKILLS CLI] add failed for '%s': %v\nOutput: %s", source, err, string(output))
		return tempDir, nil, fmt.Errorf("skills add failed: %w\nOutput: %s", err, string(output))
	}

	log.Printf("[SKILLS CLI] add succeeded for '%s':\n%s", source, string(output))

	// Find installed skills in .agents/skills/
	agentsSkillsDir := filepath.Join(tempDir, ".agents", "skills")
	entries, err := os.ReadDir(agentsSkillsDir)
	if err != nil {
		return tempDir, result, nil // No skills installed (dir doesn't exist)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMdPath := filepath.Join(agentsSkillsDir, entry.Name(), SkillFileName)
		if _, statErr := os.Stat(skillMdPath); statErr == nil {
			result.InstalledSkills = append(result.InstalledSkills, entry.Name())
		}
	}

	// Read lock file if it exists
	lockPath := filepath.Join(tempDir, "skills-lock.json")
	lockData, err := os.ReadFile(lockPath)
	if err == nil {
		var lockFile CLILockFile
		if jsonErr := json.Unmarshal(lockData, &lockFile); jsonErr == nil {
			result.LockEntries = lockFile.Skills
		}
	}

	return tempDir, result, nil
}

// ImportToWorkspace installs skills from a source and copies them to the workspace via API.
// This is the main entry point for CLI-based skill installation.
func ImportToWorkspace(ctx context.Context, workspaceAPIURL, source string) (*CLIImportResult, error) {
	cli, err := NewCLIConfig()
	if err != nil {
		return nil, err
	}

	tempDir, cliResult, err := cli.AddSkills(ctx, source)
	if tempDir != "" {
		defer os.RemoveAll(tempDir)
	}
	if err != nil {
		return nil, err
	}

	if len(cliResult.InstalledSkills) == 0 {
		return cliResult, fmt.Errorf("no skills found in source: %s", source)
	}

	client := NewWorkspaceAPIClient(workspaceAPIURL)
	agentsSkillsDir := filepath.Join(tempDir, ".agents", "skills")

	// Copy each skill to workspace
	for _, skillName := range cliResult.InstalledSkills {
		srcDir := filepath.Join(agentsSkillsDir, skillName)
		destDir := filepath.Join(SkillsBasePath, skillName)

		// Create skill folder in workspace
		if mkErr := client.CreateFolder(destDir); mkErr != nil {
			if !strings.Contains(mkErr.Error(), "exists") {
				cliResult.Errors = append(cliResult.Errors, fmt.Sprintf("failed to create folder %s: %v", destDir, mkErr))
				continue
			}
		}

		// Walk and copy all files
		walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			relPath, _ := filepath.Rel(srcDir, path)
			if relPath == "." {
				return nil
			}
			destPath := filepath.Join(destDir, relPath)

			if d.IsDir() {
				return client.CreateFolder(destPath)
			}

			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("failed to read %s: %w", path, readErr)
			}
			return client.WriteFile(destPath, string(content))
		})

		if walkErr != nil {
			cliResult.Errors = append(cliResult.Errors, fmt.Sprintf("failed to copy skill %s: %v", skillName, walkErr))
		}
	}

	// Sync lock file to workspace
	if len(cliResult.LockEntries) > 0 {
		if syncErr := SyncLockFile(workspaceAPIURL, cliResult.LockEntries); syncErr != nil {
			log.Printf("[SKILLS CLI] Warning: failed to sync lock file: %v", syncErr)
		}
	}

	return cliResult, nil
}

// ReadLockFile reads skills-lock.json from the workspace
func ReadLockFile(workspaceAPIURL string) (*CLILockFile, error) {
	client := NewWorkspaceAPIClient(workspaceAPIURL)
	content, err := client.ReadFile("skills-lock.json")
	if err != nil {
		return nil, fmt.Errorf("lock file not found: %w", err)
	}

	var lockFile CLILockFile
	if err := json.Unmarshal([]byte(content), &lockFile); err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}
	return &lockFile, nil
}

// SyncLockFile merges new lock entries into the workspace's skills-lock.json
func SyncLockFile(workspaceAPIURL string, newEntries map[string]CLILockEntry) error {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	// Read existing lock file (or create new)
	lockFile := &CLILockFile{
		Version: 1,
		Skills:  make(map[string]CLILockEntry),
	}
	existing, err := ReadLockFile(workspaceAPIURL)
	if err == nil {
		lockFile = existing
	}

	// Merge new entries
	for name, entry := range newEntries {
		lockFile.Skills[name] = entry
	}

	// Write back
	data, err := json.MarshalIndent(lockFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal lock file: %w", err)
	}

	return client.WriteFile("skills-lock.json", string(data))
}

// CheckUpdates checks which installed skills have updates available.
// Compares lock file hashes against fresh fetches from source.
func CheckUpdates(ctx context.Context, workspaceAPIURL string) ([]CLIUpdateInfo, error) {
	lockFile, err := ReadLockFile(workspaceAPIURL)
	if err != nil {
		return nil, fmt.Errorf("no lock file found — only CLI-installed skills can be checked for updates: %w", err)
	}

	if len(lockFile.Skills) == 0 {
		return nil, nil
	}

	cli, err := NewCLIConfig()
	if err != nil {
		return nil, err
	}

	var results []CLIUpdateInfo

	// Group skills by source to minimize fetches
	sourceSkills := make(map[string][]string)
	for name, entry := range lockFile.Skills {
		sourceSkills[entry.Source] = append(sourceSkills[entry.Source], name)
	}

	for source, skillNames := range sourceSkills {
		// Re-fetch from source into temp dir
		tempDir, cliResult, fetchErr := cli.AddSkills(ctx, source)
		if tempDir != "" {
			defer os.RemoveAll(tempDir)
		}
		if fetchErr != nil {
			// Mark all skills from this source as unknown
			for _, name := range skillNames {
				results = append(results, CLIUpdateInfo{
					Name:   name,
					Source: source,
				})
			}
			continue
		}

		// Compare hashes
		for _, name := range skillNames {
			info := CLIUpdateInfo{
				Name:   name,
				Source: source,
			}
			oldEntry := lockFile.Skills[name]
			if newEntry, ok := cliResult.LockEntries[name]; ok {
				info.HasUpdate = newEntry.ComputedHash != oldEntry.ComputedHash
			}
			results = append(results, info)
		}
	}

	return results, nil
}

// UpdateAll re-installs all skills tracked in skills-lock.json from their sources.
func UpdateAll(ctx context.Context, workspaceAPIURL string) (*CLIImportResult, error) {
	lockFile, err := ReadLockFile(workspaceAPIURL)
	if err != nil {
		return nil, fmt.Errorf("no lock file found: %w", err)
	}

	// Collect unique sources
	sources := make(map[string]bool)
	for _, entry := range lockFile.Skills {
		sources[entry.Source] = true
	}

	combined := &CLIImportResult{
		LockEntries: make(map[string]CLILockEntry),
	}

	for source := range sources {
		result, importErr := ImportToWorkspace(ctx, workspaceAPIURL, source)
		if importErr != nil {
			combined.Errors = append(combined.Errors, fmt.Sprintf("failed to update from %s: %v", source, importErr))
			continue
		}
		combined.InstalledSkills = append(combined.InstalledSkills, result.InstalledSkills...)
		for k, v := range result.LockEntries {
			combined.LockEntries[k] = v
		}
	}

	return combined, nil
}

// CLISearchResult represents a skill found via search
type CLISearchResult struct {
	Name     string `json:"name"`     // e.g., "inferen-sh/skills@ai-social-media-content"
	Source   string `json:"source"`   // e.g., "inferen-sh/skills"
	Skill    string `json:"skill"`    // e.g., "ai-social-media-content"
	URL      string `json:"url"`      // e.g., "https://skills.sh/inferen-sh/skills/ai-social-media-content"
	Installs string `json:"installs"` // e.g., "7.9K installs"
}

// FindSkills searches for skills using the CLI
func FindSkills(ctx context.Context, query string) ([]CLISearchResult, error) {
	cli, err := NewCLIConfig()
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(cli.TimeoutSec) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, cli.NPXPath, "skills", "find", query)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("skills find failed: %w", err)
	}

	return parseSearchOutput(string(output)), nil
}

// parseSearchOutput parses the output of `npx skills find` into structured results
func parseSearchOutput(output string) []CLISearchResult {
	var results []CLISearchResult
	lines := strings.Split(output, "\n")

	// Strip ANSI escape codes
	ansiRegex := "\x1b\\[[0-9;]*m"

	for i := 0; i < len(lines); i++ {
		line := stripANSI(lines[i], ansiRegex)
		line = strings.TrimSpace(line)

		// Look for lines matching pattern: "owner/repo@skill-name  N installs"
		if strings.Contains(line, "@") && strings.Contains(line, "installs") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				fullName := parts[0]
				installs := strings.Join(parts[1:], " ")

				// Parse owner/repo@skill
				atIdx := strings.LastIndex(fullName, "@")
				if atIdx > 0 {
					source := fullName[:atIdx]
					skill := fullName[atIdx+1:]

					result := CLISearchResult{
						Name:     fullName,
						Source:   source,
						Skill:    skill,
						Installs: installs,
					}

					// Check next line for URL
					if i+1 < len(lines) {
						nextLine := stripANSI(lines[i+1], ansiRegex)
						nextLine = strings.TrimSpace(nextLine)
						nextLine = strings.TrimPrefix(nextLine, "└ ")
						if strings.HasPrefix(nextLine, "https://") {
							result.URL = nextLine
							i++ // skip URL line
						}
					}

					results = append(results, result)
				}
			}
		}
	}

	return results
}

// stripANSI removes ANSI escape codes from a string
func stripANSI(s string, pattern string) string {
	// Simple implementation - replace common ANSI patterns
	result := s
	for strings.Contains(result, "\x1b[") {
		start := strings.Index(result, "\x1b[")
		end := start + 2
		for end < len(result) && result[end] != 'm' {
			end++
		}
		if end < len(result) {
			result = result[:start] + result[end+1:]
		} else {
			break
		}
	}
	return result
}

// RemoveFromLockFile removes a skill entry from skills-lock.json
func RemoveFromLockFile(workspaceAPIURL, skillName string) error {
	lockFile, err := ReadLockFile(workspaceAPIURL)
	if err != nil {
		return nil // No lock file, nothing to remove
	}

	if _, exists := lockFile.Skills[skillName]; !exists {
		return nil // Not in lock file
	}

	delete(lockFile.Skills, skillName)

	client := NewWorkspaceAPIClient(workspaceAPIURL)
	data, err := json.MarshalIndent(lockFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal lock file: %w", err)
	}
	return client.WriteFile("skills-lock.json", string(data))
}
