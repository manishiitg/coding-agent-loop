package server

import (
	"context"
	"log"
	"path"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// Browsers are one per workflow and one per project, never per user. A Crew
// (or any product project) shares its browser with every user who can open
// it, exactly as a workflow does.
func bindConversationBrowserIsolation(sessionID, userID, selectedWorkspace string, _ *resolvedAgentProfile) {
	workspace := normalizeConversationWorkspace(selectedWorkspace)
	if strings.HasPrefix(workspace, "Workflow/") {
		common.BindSessionBrowserIsolationForWorkflow(sessionID, workspace)
		return
	}
	if key := browserProjectKey(userID, selectedWorkspace); key != "" {
		common.BindSessionBrowserIsolationForProject(sessionID, key)
		return
	}
	log.Printf("[BROWSER] session %s has no workspace; using a session-scoped browser", sessionID)
	common.BindSessionBrowserIsolationForSession(sessionID)
}

// browserProjectKey is the owner-qualified physical path of a project
// workspace ("_users/<owner>/..."), so the owner (who may send the logical
// path) and other users (who send the physical path) name the same browser,
// while two owners' same-named projects stay distinct.
func browserProjectKey(userID, workspace string) string {
	clean := strings.Trim(filepath.ToSlash(strings.TrimSpace(workspace)), "/")
	if clean == "" {
		return ""
	}
	// A crew has one browser for its owner and readers. A crew migrated to
	// Crew/<id> keeps the key of the root it came from, so its saved browser
	// profile (logins, cookies) carries over; a new crew uses its Crew root.
	if ref, ok := resolveCrewPath(context.Background(), userID, clean); ok && ref.Shared {
		if legacy := crewPathAliases.legacyRoot(context.Background(), ref.Root); legacy != "" {
			return legacy
		}
		return ref.Root
	}
	if index := strings.Index(clean, "_users/"); index >= 0 && (index == 0 || clean[index-1] == '/') {
		return path.Clean(clean[index:])
	}
	if owner := sanitizeUserIDForPath(userID); owner != "" {
		return path.Join("_users", owner, clean)
	}
	return path.Clean(clean)
}

// browserSessionForWorkspace returns the managed browser session name a
// conversation at workspace uses, matching bindConversationBrowserIsolation.
func browserSessionForWorkspace(userID, workspace string) string {
	normalized := normalizeConversationWorkspace(workspace)
	if strings.HasPrefix(normalized, "Workflow/") {
		return common.PrefixBrowserSessionID(common.WorkflowBrowserSessionNamespace(normalized) + "--browser")
	}
	if key := browserProjectKey(userID, workspace); key != "" {
		return common.PrefixBrowserSessionID(common.ProjectBrowserSessionNamespace(key) + "--browser")
	}
	return ""
}
