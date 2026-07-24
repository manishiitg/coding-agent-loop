package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// learningPackage is the OLD shared/packages/*.json manifest shape, kept only
// so the one-time migration (migrate.go) can read pre-redesign manifests. New
// activities use activityManifest (activity.go).
type learningPackage struct {
	Title     string   `json:"title"`
	Items     []string `json:"items,omitempty"`
	GuideNote string   `json:"guide_note,omitempty"`
	CreatedAt string   `json:"created_at"`
}

var packageSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = packageSlugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "activity"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// createLearningActivityTool finalizes an activity: the content-creating skill
// has already made the activity folder `<Subject>/<Topic>/<slug>/` and written
// its files into it (via the shell); this writes the `activity.json` manifest
// that ties them together. The parent then sees it via open_activity(dir). It
// does NOT move files or create content — just records the manifest.
func createLearningActivityTool(childLabel string, recordEvent func(toolEvent)) agentsession.Tool {
	if strings.TrimSpace(childLabel) == "" {
		childLabel = "the child"
	}
	return agentsession.Tool{
		Name: "create_learning_activity",
		Description: "Finalize an activity you've already built. First create the folder <Subject>/<Topic>/<slug>/ and write its " +
			"content files into it (the study material / test HTML, and any answer key as <name>-KEY.md), then call this with that " +
			"folder as `dir` to write its activity.json manifest. `items` are the bare filenames inside the folder, in the order " +
			"the child works through them (do NOT include the answer key). For an instruction-only/dynamic activity (the tutor " +
			"generates questions live), leave `items` empty and put the full activity description in `guide_note`. Set " +
			"`teaching_mode` per the parent's wishes for THIS activity: beginner (tell the answer and keep correcting), graduated " +
			"(give `hints_before_answer` hints, then reveal), or strict (hints only, never reveal). `persona` is the tutor's tone " +
			"for this activity. After this, call open_activity(dir) so the parent sees it on the right with its 'Give to " + childLabel +
			"' button. Neither this nor open_activity hands anything to " + childLabel + " — only the parent tapping that button does; " +
			"never say it's \"sent\" or \"on their screen\".",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"dir":   map[string]interface{}{"type": "string", "description": "the activity folder you created, workspace-relative: <Subject>/<Topic>/<slug>"},
				"title": map[string]interface{}{"type": "string", "description": "short human title, e.g. \"Fractions — Quick Check\""},
				"items": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "bare filenames inside the folder, in order (exclude any *-KEY.md answer key). Empty = instruction-only activity; then guide_note is required.",
				},
				"guide_note":          map[string]interface{}{"type": "string", "description": "pacing/what-to-do-if-stuck, or (for instruction-only) the full activity description"},
				"teaching_mode":       map[string]interface{}{"type": "string", "enum": []string{"beginner", "graduated", "strict"}, "description": "how the tutor handles answers for THIS activity"},
				"hints_before_answer": map[string]interface{}{"type": "integer", "description": "for graduated mode: how many hints before revealing the answer"},
				"persona":             map[string]interface{}{"type": "string", "description": "the tutor's tone/personality for this activity, e.g. \"playful coach\""},
			},
			"required": []string{"dir", "title"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			dir := strings.Trim(strings.TrimSpace(fmt.Sprint(args["dir"])), "/")
			title := strings.TrimSpace(fmt.Sprint(args["title"]))
			if dir == "" || title == "" {
				return "", fmt.Errorf("dir and title are required")
			}
			parts := strings.Split(dir, "/")
			if len(parts) < 3 || !isSubjectDir(parts[0]) {
				return "", fmt.Errorf("dir must be <Subject>/<Topic>/<slug> under a subject folder")
			}
			absDir, ok := resolveWorkspacePath(dir)
			if !ok {
				return "", fmt.Errorf("invalid dir")
			}
			if err := os.MkdirAll(absDir, 0o700); err != nil {
				return "", fmt.Errorf("create activity folder: %w", err)
			}

			var items []string
			if raw, ok := args["items"].([]interface{}); ok {
				for _, it := range raw {
					name := strings.TrimSpace(fmt.Sprint(it))
					if name == "" || it == nil {
						continue
					}
					name = filepath.Base(name) // items are bare filenames within the folder
					if activityContainsKey(name) {
						continue // never list the answer key as an item
					}
					if _, err := os.Stat(filepath.Join(absDir, name)); err != nil {
						return "", fmt.Errorf("item %q not found in the activity folder — write it first", name)
					}
					items = append(items, name)
				}
			}
			guideNote := strings.TrimSpace(fmt.Sprint(args["guide_note"]))
			if guideNote == "<nil>" {
				guideNote = ""
			}
			if len(items) == 0 && guideNote == "" {
				return "", fmt.Errorf("either items (files in the folder) or guide_note (for an instruction-only activity) is required")
			}
			mode := strings.TrimSpace(fmt.Sprint(args["teaching_mode"]))
			switch mode {
			case "beginner", "graduated", "strict", "", "<nil>":
				if mode == "<nil>" {
					mode = ""
				}
			default:
				return "", fmt.Errorf("teaching_mode must be beginner, graduated, or strict")
			}
			hints := 0
			if f, ok := args["hints_before_answer"].(float64); ok {
				hints = int(f)
			}
			persona := strings.TrimSpace(fmt.Sprint(args["persona"]))
			if persona == "<nil>" {
				persona = ""
			}

			m := activityManifest{
				Title:             title,
				Subject:           parts[0],
				Topic:             parts[1],
				Items:             items,
				GuideNote:         guideNote,
				TeachingMode:      mode,
				HintsBeforeAnswer: hints,
				Persona:           persona,
				CreatedAt:         time.Now().UTC().Format(time.RFC3339),
			}
			b, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(absDir, activityManifestName), b, 0o600); err != nil {
				return "", fmt.Errorf("write activity.json: %w", err)
			}

			if recordEvent != nil {
				recordEvent(toolEvent{Tool: "create_learning_activity", Path: dir, Package: title})
			}
			return fmt.Sprintf(`{"status":"ok","dir":%q,"title":%q,"items":%d}`, dir, title, len(items)), nil
		},
	}
}
