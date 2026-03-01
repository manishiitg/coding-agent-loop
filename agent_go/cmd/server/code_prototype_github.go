package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// GitHub helpers
// ---------------------------------------------------------------------------

// runProjectGit runs a git command inside Projects/{projectName} via the
// workspace execute API, merging stdout+stderr so callers get full git output.
func runProjectGit(ctx context.Context, userID, projectName, gitCmd string) (string, error) {
	return runWorkspaceShell(ctx, getWorkspaceAPIURL(), userID, gitCmd, "Projects/"+projectName)
}

// getProjectPAT retrieves and decrypts the stored GitHub PAT for a project.
func (api *StreamingAPI) getProjectPAT(ctx context.Context, userID, secretName string) (string, error) {
	secrets, err := api.chatDB.ListUserSecrets(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("list secrets: %w", err)
	}
	for _, s := range secrets {
		if s.Name == secretName {
			return decryptSecretValue(s.EncryptedValue, userID)
		}
	}
	return "", fmt.Errorf("GitHub token not found — please reconnect")
}

// injectPAT rewrites https://github.com/... → https://{pat}@github.com/...
func injectPAT(repoURL, pat string) string {
	return strings.Replace(repoURL, "https://", "https://"+pat+"@", 1)
}

// sanitizeGitOutput removes the PAT from git output before returning to the client.
func sanitizeGitOutput(s, pat string) string {
	if pat == "" {
		return s
	}
	return strings.ReplaceAll(s, pat, "***")
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// branchNameFromLabel turns a user label into a git branch name.
// "new dark theme" → "experiment-new-dark-theme-1706876400"
func branchNameFromLabel(label string) string {
	slug := nonAlnum.ReplaceAllString(strings.ToLower(label), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "experiment"
	}
	return fmt.Sprintf("experiment-%s-%d", slug, time.Now().Unix())
}

// branchLabelFromName recovers a human-readable label from a branch name.
// "experiment-new-dark-theme-1706876400" → "new dark theme"
func branchLabelFromName(branch string) string {
	s := strings.TrimPrefix(branch, "experiment-")
	parts := strings.Split(s, "-")
	// Strip trailing unix timestamp (≥8 all-digit segment)
	if n := len(parts); n > 1 {
		last := parts[n-1]
		allDigits := true
		for _, c := range last {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(last) >= 8 {
			parts = parts[:n-1]
		}
	}
	return strings.Join(parts, " ")
}

// gitAutoSave commits any dirty state with the given message (no-op if clean).
func gitAutoSave(ctx context.Context, userID, projectName, message string) {
	cmd := fmt.Sprintf(`git add . && (git diff --cached --quiet || git commit -m %s)`, shellQuote(message))
	runProjectGit(ctx, userID, projectName, cmd) //nolint:errcheck
}

// gitCurrentBranch returns the current branch name, defaulting to "main".
func gitCurrentBranch(ctx context.Context, userID, projectName string) string {
	out, _ := runProjectGit(ctx, userID, projectName,
		"git rev-parse --abbrev-ref HEAD 2>/dev/null || echo main")
	b := strings.TrimSpace(out)
	if b == "" || strings.Contains(b, "fatal") {
		return "main"
	}
	return b
}

// updateProjectMeta is a helper that reads, modifies, and re-writes .prototype.json.
func updateProjectMeta(ctx context.Context, userID, name string, fn func(*PrototypeProjectMeta)) error {
	content, err := readPrototypeFile(ctx, prototypeMetaPath(userID, name), userID)
	if err != nil || content == "" {
		return fmt.Errorf("project not found")
	}
	var meta PrototypeProjectMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return err
	}
	fn(&meta)
	b, _ := json.MarshalIndent(meta, "", "  ")
	return writePrototypeFile(ctx, prototypeMetaPath(userID, name), string(b), userID)
}

// ---------------------------------------------------------------------------
// Route handlers
// ---------------------------------------------------------------------------

// POST /api/code-prototype/projects/{name}/github/connect
// Body: {"repo_url": "https://github.com/...", "pat": "ghp_..."}
func (api *StreamingAPI) handleGitHubConnect(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	var req struct {
		RepoURL string `json:"repo_url"`
		PAT     string `json:"pat"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RepoURL == "" || req.PAT == "" {
		http.Error(w, "repo_url and pat are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Encrypt and persist the PAT in the user secrets DB.
	secretName := "GITHUB_PAT_" + name
	encrypted, err := encryptSecretValue(req.PAT, userID)
	if err != nil {
		http.Error(w, "encrypt PAT: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := api.chatDB.UpsertUserSecret(ctx, userID, secretName, encrypted); err != nil {
		http.Error(w, "store PAT: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Persist connection info in .prototype.json.
	if err := updateProjectMeta(ctx, userID, name, func(m *PrototypeProjectMeta) {
		m.GitHub = &PrototypeGitHub{RepoURL: req.RepoURL, PatSecretName: secretName}
	}); err != nil {
		http.Error(w, "update meta: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialise git repo, configure identity, set remote.
	initCmd := fmt.Sprintf(
		`git init -b main 2>/dev/null || git init && `+
			`git config --local user.email "prototype@mcpagent.io" && `+
			`git config --local user.name "MCP Agent" && `+
			`git remote remove origin 2>/dev/null; git remote add origin %s`,
		req.RepoURL,
	)
	if out, err := runProjectGit(ctx, userID, name, initCmd); err != nil {
		log.Printf("[GITHUB] init error for %s: %v — %s", name, err, out)
	}

	// Initial commit if there are unstaged files.
	gitAutoSave(ctx, userID, name, "Initial commit")

	log.Printf("[GITHUB] connected %s → %s", name, req.RepoURL)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"current_branch": "main"})
}

// DELETE /api/code-prototype/projects/{name}/github
func (api *StreamingAPI) handleGitHubDisconnect(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	content, _ := readPrototypeFile(ctx, prototypeMetaPath(userID, name), userID)
	var meta PrototypeProjectMeta
	json.Unmarshal([]byte(content), &meta) //nolint:errcheck

	if meta.GitHub != nil && meta.GitHub.PatSecretName != "" {
		api.chatDB.DeleteUserSecret(ctx, userID, meta.GitHub.PatSecretName) //nolint:errcheck
	}

	updateProjectMeta(ctx, userID, name, func(m *PrototypeProjectMeta) { m.GitHub = nil }) //nolint:errcheck
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/code-prototype/projects/{name}/github/status
func (api *StreamingAPI) handleGitHubStatus(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	content, _ := readPrototypeFile(ctx, prototypeMetaPath(userID, name), userID)
	var meta PrototypeProjectMeta
	json.Unmarshal([]byte(content), &meta) //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	if meta.GitHub == nil {
		json.NewEncoder(w).Encode(map[string]bool{"connected": false})
		return
	}

	branch := gitCurrentBranch(ctx, userID, name)
	isExp := strings.HasPrefix(branch, "experiment-")
	expLabel := ""
	if isExp {
		expLabel = branchLabelFromName(branch)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected":        true,
		"repo_url":         meta.GitHub.RepoURL,
		"current_branch":   branch,
		"is_experiment":    isExp,
		"experiment_label": expLabel,
	})
}

// POST /api/code-prototype/projects/{name}/github/checkpoint
// Body: {"message": "Added counter button"}
func (api *StreamingAPI) handleGitHubSaveCheckpoint(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	var req struct {
		Message string `json:"message"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.Message == "" {
		req.Message = "Checkpoint"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := fmt.Sprintf(
		`git add . && (git diff --cached --quiet && echo "nothing-to-commit" || git commit -m %s)`,
		shellQuote(req.Message),
	)
	out, err := runProjectGit(ctx, userID, name, cmd)
	if strings.Contains(out, "nothing-to-commit") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "nothing to save"})
		return
	}
	if err != nil {
		http.Error(w, "checkpoint failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	hashOut, _ := runProjectGit(ctx, userID, name, "git rev-parse --short HEAD")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"hash":    strings.TrimSpace(hashOut),
		"message": req.Message,
	})
}

// GET /api/code-prototype/projects/{name}/github/history
func (api *StreamingAPI) handleGitHubHistory(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	out, _ := runProjectGit(ctx, userID, name,
		`git log --format="%H|%s|%ar" -20 2>/dev/null || echo ""`)

	type entry struct {
		Hash      string `json:"hash"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
	}
	var entries []entry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		entries = append(entries, entry{Hash: parts[0], Message: parts[1], Timestamp: parts[2]})
	}
	if entries == nil {
		entries = []entry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// POST /api/code-prototype/projects/{name}/github/restore
// Body: {"hash": "abc1234", "label": "Added counter button"}
func (api *StreamingAPI) handleGitHubRestore(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	var req struct {
		Hash  string `json:"hash"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Hash == "" {
		http.Error(w, "hash is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Auto-save current state before restoring.
	gitAutoSave(ctx, userID, name, "Auto-save before restore")

	cmd := fmt.Sprintf(
		`git checkout %s -- . && git add . && (git diff --cached --quiet || git commit -m %s)`,
		req.Hash,
		shellQuote("Restored to: "+req.Label),
	)
	if _, err := runProjectGit(ctx, userID, name, cmd); err != nil {
		http.Error(w, "restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// POST /api/code-prototype/projects/{name}/github/publish
func (api *StreamingAPI) handleGitHubPublish(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	content, _ := readPrototypeFile(ctx, prototypeMetaPath(userID, name), userID)
	var meta PrototypeProjectMeta
	json.Unmarshal([]byte(content), &meta) //nolint:errcheck
	if meta.GitHub == nil {
		http.Error(w, "GitHub not connected", http.StatusBadRequest)
		return
	}

	pat, err := api.getProjectPAT(ctx, userID, meta.GitHub.PatSecretName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	branch := gitCurrentBranch(ctx, userID, name)
	remoteURL := injectPAT(meta.GitHub.RepoURL, pat)
	// GIT_TERMINAL_PROMPT=0 prevents git from hanging waiting for a TTY credential prompt.
	// GIT_ASKPASS=echo returns empty string for any credential queries.
	cmd := fmt.Sprintf(
		"GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=echo git push %s HEAD:%s -u --force 2>&1",
		remoteURL, branch,
	)
	log.Printf("[GITHUB] pushing %s → %s (branch: %s)", name, meta.GitHub.RepoURL, branch)
	out, err := runProjectGit(ctx, userID, name, cmd)
	out = sanitizeGitOutput(out, pat)

	if err != nil {
		errMsg := sanitizeGitOutput(err.Error(), pat)
		log.Printf("[GITHUB] publish error for %s: %s | output: %s", name, errMsg, out)
		http.Error(w, "publish failed: "+errMsg+"\n"+out, http.StatusInternalServerError)
		return
	}
	log.Printf("[GITHUB] published %s → branch %s | %s", name, branch, out)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"branch": branch, "output": out})
}

// GET /api/code-prototype/projects/{name}/github/experiments
func (api *StreamingAPI) handleGitHubListExperiments(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	out, _ := runProjectGit(ctx, userID, name,
		`git branch --format="%(refname:short)|%(creatordate:relative)" 2>/dev/null || echo ""`)

	type entry struct {
		Branch    string `json:"branch"`
		Label     string `json:"label"`
		Timestamp string `json:"timestamp"`
	}
	var experiments []entry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 2)
		branch := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(branch, "experiment-") {
			continue
		}
		ts := ""
		if len(parts) == 2 {
			ts = strings.TrimSpace(parts[1])
		}
		experiments = append(experiments, entry{
			Branch:    branch,
			Label:     branchLabelFromName(branch),
			Timestamp: ts,
		})
	}
	if experiments == nil {
		experiments = []entry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(experiments)
}

// POST /api/code-prototype/projects/{name}/github/experiments
// Body: {"label": "dark theme"}
func (api *StreamingAPI) handleGitHubStartExperiment(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	var req struct {
		Label string `json:"label"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
	if req.Label == "" {
		req.Label = "experiment"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	gitAutoSave(ctx, userID, name, "Checkpoint before experiment: "+req.Label)

	branchName := branchNameFromLabel(req.Label)
	if out, err := runProjectGit(ctx, userID, name, "git checkout -b "+branchName); err != nil {
		log.Printf("[GITHUB] start experiment error: %v — %s", err, out)
		http.Error(w, "failed to start experiment: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[GITHUB] experiment started: %s (branch: %s)", name, branchName)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"branch": branchName, "label": req.Label})
}

// POST /api/code-prototype/projects/{name}/github/experiments/keep
// Saves current experiment, merges to main, deletes the experiment branch.
func (api *StreamingAPI) handleGitHubKeepExperiment(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	branch := gitCurrentBranch(ctx, userID, name)
	if !strings.HasPrefix(branch, "experiment-") {
		http.Error(w, "not on an experiment branch", http.StatusBadRequest)
		return
	}
	label := branchLabelFromName(branch)

	gitAutoSave(ctx, userID, name, "Final save: "+label)

	cmd := fmt.Sprintf(
		`git checkout main && git merge %s --no-ff -m %s && git branch -d %s`,
		branch,
		shellQuote("Applied experiment: "+label),
		branch,
	)
	if out, err := runProjectGit(ctx, userID, name, cmd); err != nil {
		log.Printf("[GITHUB] keep experiment error: %v — %s", err, out)
		http.Error(w, "keep failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[GITHUB] experiment kept: %s (%s → main)", name, branch)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// DELETE /api/code-prototype/projects/{name}/github/experiments/current
// Discards the current experiment branch, returning to main.
func (api *StreamingAPI) handleGitHubDiscardExperiment(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := mux.Vars(r)["name"]

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	branch := gitCurrentBranch(ctx, userID, name)
	if !strings.HasPrefix(branch, "experiment-") {
		http.Error(w, "not on an experiment branch", http.StatusBadRequest)
		return
	}

	cmd := fmt.Sprintf("git checkout main && git branch -D %s", branch)
	if out, err := runProjectGit(ctx, userID, name, cmd); err != nil {
		log.Printf("[GITHUB] discard experiment error: %v — %s", err, out)
		http.Error(w, "discard failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[GITHUB] experiment discarded: %s (%s)", name, branch)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
