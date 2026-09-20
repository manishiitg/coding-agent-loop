package workflowtypes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CrewAttachment exposes one Crew project's workspace to a workflow under a
// stable alias, read-only. Attachments live in workflow.json next to the
// host-local folder_access grants, but unlike those grants they reference
// workspace paths (resolved once at attach time), never host directories.
// Downstream steps read crew files as "<alias>/<crew-relative path>".
type CrewAttachment struct {
	ID                string `json:"id"`
	Alias             string `json:"alias"`
	CrewProfileID     string `json:"crew_profile_id"`
	CrewProjectID     string `json:"crew_project_id"`
	CrewWorkspacePath string `json:"crew_workspace_path"`
	CreatedAt         string `json:"created_at,omitempty"`
}

// crewAttachmentAliasPattern keeps aliases usable as a path segment and as
// an environment suffix. Aliases are matched exactly (case-sensitive).
var crewAttachmentAliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-_]{0,63}$`)

// ReservedCrewAttachmentAliases are top-level workflow names an attachment
// alias must never shadow; reads under them always resolve inside the
// workflow. The read path uses the same list to skip the manifest lookup
// for ordinary paths.
var ReservedCrewAttachmentAliases = []string{
	"runs", "planning", "skills", "notes", "knowledgebase", "db",
	"variables", "logs", "workflow.json",
}

// ValidateCrewAttachmentAlias rejects empty, malformed, and shadowing aliases.
// The reserved list is checked before the charset pattern so reserved names
// always get the specific shadowing error, even ones like workflow.json
// that could never be a valid alias anyway.
func ValidateCrewAttachmentAlias(alias string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return errCrewAttachmentAlias("alias is required")
	}
	for _, reserved := range ReservedCrewAttachmentAliases {
		if alias == reserved {
			return errCrewAttachmentAlias("alias must not shadow the workflow path " + reserved)
		}
	}
	if !crewAttachmentAliasPattern.MatchString(alias) {
		return errCrewAttachmentAlias("alias must be 1-64 lowercase letters, digits, dashes, or underscores")
	}
	return nil
}

type crewAttachmentAliasError struct{ msg string }

func errCrewAttachmentAlias(msg string) error { return crewAttachmentAliasError{msg} }

func (e crewAttachmentAliasError) Error() string { return "invalid crew attachment alias: " + e.msg }

// ReadCrewAttachments loads the crew attachments declared in a workflow's
// manifest from the local docs root. It reports found=false when no
// manifest exists there; a manifest without attachments returns an empty
// list with found=true.
func ReadCrewAttachments(docsRoot, workspacePath string) ([]CrewAttachment, bool) {
	manifestPath := filepath.Join(docsRoot, filepath.FromSlash(strings.TrimSpace(workspacePath)), "workflow.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, false
	}
	var manifest struct {
		CrewAttachments []CrewAttachment `json:"crew_attachments"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return nil, false
	}
	return manifest.CrewAttachments, true
}

// ResolveCrewAttachmentPath rewrites an "<alias>/..." read path to the
// attached Crew workspace path. It returns ok=false when the first segment
// is not an attached alias, and refuses to resolve outside the attached
// root (fail closed on ".." escapes).
func ResolveCrewAttachmentPath(attachments []CrewAttachment, filePath string) (resolved string, ok bool) {
	trimmed := strings.Trim(strings.TrimSpace(filePath), "/")
	if trimmed == "" {
		return "", false
	}
	segment := trimmed
	if idx := strings.IndexByte(trimmed, '/'); idx >= 0 {
		segment = trimmed[:idx]
	}
	for _, attachment := range attachments {
		if attachment.Alias != segment {
			continue
		}
		root := strings.Trim(strings.TrimSpace(attachment.CrewWorkspacePath), "/")
		if root == "" {
			return "", false
		}
		rest := strings.TrimPrefix(trimmed, segment)
		rest = strings.Trim(rest, "/")
		if rest == "" {
			return root, true
		}
		candidate := filepath.ToSlash(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(rest)))
		if candidate != root && !strings.HasPrefix(candidate, root+"/") {
			return "", false
		}
		return candidate, true
	}
	return "", false
}

// IsReservedCrewAttachmentAlias reports whether a first path segment can
// never be an attachment alias.
func IsReservedCrewAttachmentAlias(segment string) bool {
	for _, reserved := range ReservedCrewAttachmentAliases {
		if segment == reserved {
			return true
		}
	}
	return false
}
