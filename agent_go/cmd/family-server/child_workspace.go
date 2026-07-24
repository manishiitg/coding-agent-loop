package main

import (
	"strings"
)

// In the activity-folder model the child is sandboxed to exactly ONE activity
// folder — the current one (current-activity.json, see activity.go). It reads
// and writes that whole folder directly (its content files, activity.json, its
// conversation, its attempts) and can reach nothing else. So the child access
// checks collapse to a single question: "is this path inside the current
// activity folder?" — no per-item enumeration, no per-file exemptions, no
// approved-for-child list. `childCanSee`, `childCanWrite`, and childShellTool's
// Read/WritePaths (shell_tool.go) all derive from `currentActivityDir()`.

// withinCurrentActivity reports whether a workspace-relative path resolves and
// sits inside the current activity folder (the folder itself, or any file
// under it). The single boundary the child agent is held to.
func withinCurrentActivity(rel string) bool {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	if rel == "" {
		return false
	}
	if _, ok := resolveWorkspacePath(rel); !ok {
		return false
	}
	dir := currentActivityDir()
	if dir == "" {
		return false
	}
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// childCanSee — the child may open on their screen anything inside the current
// activity folder. (The answer key `*-KEY.md` is physically in the folder and
// technically openable by the sandbox, but is kept out of the child's view by
// the tutor prompt + by being absent from activity.json items / the child UI —
// see the plan.)
func childCanSee(rel string) bool { return withinCurrentActivity(rel) }

// childCanWrite — the child agent may write anywhere in the current activity
// folder (this is how the tutor records "✓ Answered" progress notes straight
// onto the real file — see childSystemPrompt). Must stay in sync with
// childShellTool's WritePaths.
func childCanWrite(rel string) bool { return withinCurrentActivity(rel) }

// childDisplayName is the child's first name for prompt wording, or a neutral
// fallback when no profile exists yet.
func childDisplayName(child *Child) string {
	if child != nil && strings.TrimSpace(child.Name) != "" {
		return child.Name
	}
	return "your student"
}
