package workflowtypes

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	FolderAccessReadOnly  = "read_only"
	FolderAccessReadWrite = "read_write"
)

// WorkflowFolderGrant is an owner-approved, host-local filesystem capability.
// Agents refer to Alias; they never establish authority by supplying Path.
type WorkflowFolderGrant struct {
	ID        string `json:"id"`
	Alias     string `json:"alias"`
	Path      string `json:"path"`
	Access    string `json:"access"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// WorkflowFolderAccessRequest is an agent-authored proposal awaiting trusted
// user approval. RequestedPath may retain an exact absolute path supplied by
// the user, but it is not an authority grant until the user approves it.
type WorkflowFolderAccessRequest struct {
	ID            string `json:"id"`
	Alias         string `json:"alias"`
	RequestedPath string `json:"requested_path,omitempty"`
	Access        string `json:"access"`
	Reason        string `json:"reason"`
	RequestedAt   string `json:"requested_at"`
}

func (g WorkflowFolderGrant) CanWrite() bool {
	return strings.TrimSpace(g.Access) == FolderAccessReadWrite
}

var folderAliasEnvUnsafe = regexp.MustCompile(`[^A-Z0-9]+`)

// CanonicalizeFolderPath cleans a host path, resolves symlinks, and
// requires an existing directory. Every folder-grant write path funnels
// through here so stored grants are always canonical.
func CanonicalizeFolderPath(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return "", fmt.Errorf("folder path is unavailable: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("folder path must reference an existing directory")
	}
	return filepath.Clean(canonical), nil
}

// FolderAliasEnvKey converts a folder alias to the <PREFIX>_<KEY>
// environment suffix shared by every folder-grant consumer.
func FolderAliasEnvKey(alias string) string {
	return strings.Trim(folderAliasEnvUnsafe.ReplaceAllString(strings.ToUpper(strings.TrimSpace(alias)), "_"), "_")
}

// FolderGrantAvailable reports whether a stored grant path still resolves
// to a live directory. Enforcement trusts stored canonical paths (they
// were canonicalized at write time); this is for UI availability display.
func FolderGrantAvailable(path string) bool {
	canonical, err := filepath.EvalSymlinks(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return false
	}
	info, err := os.Stat(canonical)
	return err == nil && info.IsDir()
}

// NormalizeFolderGrants canonicalizes requested grants, trims their
// fields, and preserves CreatedAt for grants that already existed. The
// label prefixes per-grant errors (for example "folder_access"). The
// two-step path check keeps the exact historical error strings.
func NormalizeFolderGrants(requested, previous []WorkflowFolderGrant, label, now string) ([]WorkflowFolderGrant, error) {
	previousByID := make(map[string]WorkflowFolderGrant, len(previous))
	for _, grant := range previous {
		previousByID[grant.ID] = grant
	}
	normalized := make([]WorkflowFolderGrant, 0, len(requested))
	for i, grant := range requested {
		canonical, err := filepath.EvalSymlinks(filepath.Clean(strings.TrimSpace(grant.Path)))
		if err != nil {
			return nil, fmt.Errorf("%s[%d] is unavailable: %w", label, i, err)
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%s[%d] must reference an existing directory", label, i)
		}
		grant.Path = filepath.Clean(canonical)
		grant.ID = strings.TrimSpace(grant.ID)
		grant.Alias = strings.TrimSpace(grant.Alias)
		grant.Access = strings.TrimSpace(grant.Access)
		grant.Reason = strings.TrimSpace(grant.Reason)
		if prior, exists := previousByID[grant.ID]; exists && strings.TrimSpace(prior.CreatedAt) != "" {
			grant.CreatedAt = prior.CreatedAt
		} else {
			grant.CreatedAt = now
		}
		grant.UpdatedAt = now
		normalized = append(normalized, grant)
	}
	return normalized, nil
}

// ResolveFolderGrants turns stored grants into guard inputs: every grant
// is readable, read_write grants are also writable, read-only grants are
// listed separately, and each alias becomes a <envPrefix>_<KEY>
// environment variable. Stored paths are trusted (canonicalized at write
// time); use FolderGrantAvailable to report liveness separately.
func ResolveFolderGrants(grants []WorkflowFolderGrant, envPrefix string) (read, write, readOnly []string, env map[string]string) {
	env = map[string]string{}
	for _, grant := range grants {
		path := filepath.Clean(strings.TrimSpace(grant.Path))
		read = append(read, path)
		if grant.CanWrite() {
			write = append(write, path)
		} else {
			readOnly = append(readOnly, path)
		}
		if key := FolderAliasEnvKey(grant.Alias); key != "" {
			env[envPrefix+key] = path
		}
	}
	return deduplicateStrings(read), deduplicateStrings(write), deduplicateStrings(readOnly), env
}

// FolderGrantsPrompt renders the "### Attached Folders" system prompt
// section listing resolved grants. The emptyHint covers the no-grants
// case, which differs per product surface.
func FolderGrantsPrompt(grants []WorkflowFolderGrant, envPrefix, emptyHint string) string {
	var sb strings.Builder
	sb.WriteString("\n### Attached Folders\n")
	if len(grants) == 0 {
		sb.WriteString(emptyHint)
		if !strings.HasSuffix(emptyHint, "\n") {
			sb.WriteString("\n")
		}
		return sb.String()
	}
	for _, grant := range grants {
		sb.WriteString(fmt.Sprintf("- **%s** (`%s`) — %s — `$%s%s`\n", strings.TrimSpace(grant.Alias), strings.TrimSpace(grant.ID), strings.TrimSpace(grant.Access), envPrefix, FolderAliasEnvKey(grant.Alias)))
	}
	return sb.String()
}

func deduplicateStrings(strs []string) []string {
	seen := make(map[string]bool, len(strs))
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
