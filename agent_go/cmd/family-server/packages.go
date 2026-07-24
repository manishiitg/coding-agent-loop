package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// This file is the HTTP layer for activities (the activity-folder model). It
// replaces the old package-manifest endpoints: the frontend no longer parses
// subject/topic/type out of file paths — it renders directly from these
// structured objects (each carries its own subject/topic/title from
// activity.json).

type activityItemResp struct {
	Path string `json:"path"` // workspace-relative, for /api/workspace/file|raw
	Name string `json:"name"` // display name (bare filename)
}

type activityResp struct {
	Dir          string             `json:"dir"`
	Title        string             `json:"title"`
	Subject      string             `json:"subject,omitempty"`
	Topic        string             `json:"topic,omitempty"`
	Items        []activityItemResp `json:"items"`
	GuideNote    string             `json:"guide_note,omitempty"`
	TeachingMode string             `json:"teaching_mode,omitempty"`
	Persona      string             `json:"persona,omitempty"`
	CreatedAt    string             `json:"created_at,omitempty"`
	Attempts     []activityItemResp `json:"attempts,omitempty"`
}

// toActivityResp resolves an Activity's bare item filenames into
// workspace-relative {path,name} objects the frontend viewer can open, plus
// whatever the child has saved into the activity's own attempts/ folder.
func toActivityResp(act Activity) activityResp {
	items := make([]activityItemResp, 0, len(act.Items))
	for _, name := range act.Items {
		items = append(items, activityItemResp{
			Path: filepath.ToSlash(filepath.Join(act.Dir, name)),
			Name: name,
		})
	}
	var attempts []activityItemResp
	if abs, ok := resolveWorkspacePath(filepath.Join(act.Dir, "attempts")); ok {
		if entries, err := os.ReadDir(abs); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				attempts = append(attempts, activityItemResp{
					Path: filepath.ToSlash(filepath.Join(act.Dir, "attempts", e.Name())),
					Name: e.Name(),
				})
			}
		}
	}
	return activityResp{
		Dir:          act.Dir,
		Title:        act.Title,
		Subject:      act.Subject,
		Topic:        act.Topic,
		Items:        items,
		GuideNote:    act.GuideNote,
		TeachingMode: act.TeachingMode,
		Persona:      act.Persona,
		CreatedAt:    act.CreatedAt,
		Attempts:     attempts,
	}
}

// GET /api/activities — every activity, structured (parent's Files-tab view).
func handleActivities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	acts := listActivities()
	out := make([]activityResp, 0, len(acts))
	for _, a := range acts {
		out = append(out, toActivityResp(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/child/activity — the ONE activity the child session is bound to,
// structured. Items already exclude the parent-only answer key (it isn't in
// activity.json's items). Returns 204 when nothing is handed off.
func handleChildActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	act, ok := loadCurrentActivity()
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, toActivityResp(act))
}

// activityContainsKey reports whether name looks like an answer key (`*-KEY.md`),
// which must never be listed to the child or included in an activity's items.
func activityContainsKey(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return strings.HasSuffix(base, "-key.md") || strings.HasSuffix(base, "-key.markdown")
}
