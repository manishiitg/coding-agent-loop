package server

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// createPrototypeGitToolDef returns the name, description, and JSON schema params
// for the prototype_git virtual tool.
func createPrototypeGitToolDef() (name, description string, params map[string]interface{}) {
	name = "prototype_git"
	description = "Run any git command inside a prototype project folder. The GitHub PAT is auto-injected for push/pull/fetch/clone — do NOT include credentials in the command."
	params = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project_name": map[string]interface{}{
				"type":        "string",
				"description": "Name of the prototype project (e.g. 'my-app')",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Git command(s) to run. Must start with 'git'. Compound commands allowed: 'git add . && git commit -m \"msg\"'. Do NOT include credentials.",
			},
		},
		"required": []string{"project_name", "command"},
	}
	return
}

// validateGitCommand ensures the command starts with "git", skipping any leading
// env-var tokens (KEY=val). Returns an error for empty or non-git commands.
func validateGitCommand(cmd string) error {
	parts := strings.Fields(strings.TrimSpace(cmd))
	for _, p := range parts {
		if strings.Contains(p, "=") {
			continue // skip env var tokens like GIT_TERMINAL_PROMPT=0
		}
		if p == "git" {
			return nil
		}
		return fmt.Errorf("command must start with 'git', got: %q", p)
	}
	return fmt.Errorf("empty command")
}

// maybeInjectPAT injects the GitHub PAT into the command for remote operations.
// For non-remote commands (no push/fetch/pull/clone) the command is returned unchanged.
// If no GitHub connection is configured or the PAT cannot be read, a no-prompt variant
// is returned so git fails cleanly instead of hanging on a TTY prompt.
func (api *StreamingAPI) maybeInjectPAT(ctx context.Context, userID, projectName, cmd string) string {
	needsPAT := strings.Contains(cmd, "push") ||
		strings.Contains(cmd, "fetch") ||
		strings.Contains(cmd, "pull") ||
		strings.Contains(cmd, "clone")
	if !needsPAT {
		return cmd
	}

	content, err := readPrototypeFile(ctx, prototypeMetaPath(userID, projectName), userID)
	if err != nil || content == "" {
		return "GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=echo " + cmd
	}
	var meta PrototypeProjectMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil || meta.GitHub == nil {
		return "GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=echo " + cmd
	}

	pat, err := api.getProjectPAT(ctx, userID, meta.GitHub.PatSecretName)
	if err != nil || pat == "" {
		return "GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=echo " + cmd
	}

	authURL := injectPAT(meta.GitHub.RepoURL, pat)
	// Wrap in a subshell: set PAT-injected URL → run command → restore clean URL → propagate exit code.
	return fmt.Sprintf(
		"(git remote set-url origin %s 2>/dev/null; GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=echo %s; rc=$?; git remote set-url origin %s 2>/dev/null; exit $rc)",
		shellQuote(authURL), cmd, shellQuote(meta.GitHub.RepoURL),
	)
}

var gitTokenRE = regexp.MustCompile(`(ghp_|github_pat_)[A-Za-z0-9_]+`)

// sanitizeGitTokens strips any PAT-looking tokens from s (defense-in-depth).
func sanitizeGitTokens(s string) string {
	return gitTokenRE.ReplaceAllString(s, "***")
}

// createPrototypeGitExecutor returns the executor function for the prototype_git tool.
func (api *StreamingAPI) createPrototypeGitExecutor(userID string) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		projectName, _ := args["project_name"].(string)
		command, _ := args["command"].(string)

		if projectName == "" {
			return "", fmt.Errorf("project_name is required")
		}

		if err := validateGitCommand(command); err != nil {
			return "", fmt.Errorf("invalid git command: %w", err)
		}

		finalCmd := api.maybeInjectPAT(ctx, userID, projectName, command)

		out, err := runProjectGit(ctx, userID, projectName, finalCmd)
		out = sanitizeGitTokens(out)

		if err != nil {
			return out, fmt.Errorf("git error: %w", err)
		}
		return out, nil
	}
}
