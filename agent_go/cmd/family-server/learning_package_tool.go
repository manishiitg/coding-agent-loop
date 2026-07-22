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

// learningPackage is the manifest a "create_learning_package" call writes under
// shared/packages/. It either bundles an ordered set of already-created files
// (notes, study material, a basic test, an advanced test, ...) into one thing
// the parent hands off in a single approval, or — when Items is empty — is a
// purely instruction-driven package: GuideNote is then the whole activity
// description (what to generate, how to adapt difficulty, when to stop), and
// the tutor generates fresh content live in the conversation instead of
// pointing the child at a fixed file. This is intentionally file-based,
// mirroring every other piece of family state — no database, no separate API.
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
		s = "package"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// createLearningPackageTool bundles several already-created shared/ files
// (e.g. notes + study material + a basic test + an advanced test) into one
// package and approves the manifest AND every item for the child in a single
// call — the parent hands off the whole thing at once instead of one file at
// a time. The child's system prompt (childSystemPrompt) tells the tutor to
// check shared/packages/ for approved manifests and follow their order/note.
func createLearningPackageTool(recordEvent func(toolEvent)) agentsession.Tool {
	return agentsession.Tool{
		Name: "create_learning_package",
		Description: "Bundle pre-made files (notes, study material, a basic test, an advanced test, etc.) into one package for the " +
			"child, in the order they should do them — OR create an instruction-only package with no files at all, just a guide_note " +
			"describing an open-ended, dynamically-generated activity (e.g. \"Give Myra one algebra word problem at a time. Start " +
			"medium difficulty, go harder after two correct in a row, easier after a miss. Keep going until she wants to stop.\" or " +
			"\"Run adaptive GMAT-style quant practice: one question at a time, adjust difficulty from her answers, never repeat a " +
			"question.\"). Use items when there's real pre-made material to hand off; use guide_note alone when the child should get " +
			"freshly-generated, adapting questions each session instead of fixed material. This creates the package, approves all its " +
			"items, and adds the real 'Give to <child>' handoff button to your reply — you do NOT need to call approve_for_child " +
			"separately. CRITICAL, exactly like approve_for_child: this does NOT put anything on the child's screen or start a session " +
			"— only the parent physically tapping that button hands it over. So NEVER tell the parent the package is \"on its way\", " +
			"\"sent\", or \"on the child's screen\"; say it's ready and to tap 'Give to <child>' below when they want to hand it over.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "short human title for the package, e.g. \"Quadratic Equations — Full Practice Set\" or \"Adaptive Algebra Drills\"",
				},
				"items": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "workspace-relative paths under shared/, in the order the child should go through them. Omit or leave empty for an instruction-only (dynamically-generated) package — then guide_note is required instead.",
				},
				"guide_note": map[string]interface{}{
					"type":        "string",
					"description": "instructions for the tutor: pacing/order/what to do if stuck when items are given, or the full activity description (what to generate, how to adapt difficulty, when to stop) when there are no items",
				},
			},
			"required": []string{"title"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			title, _ := args["title"].(string)
			title = strings.TrimSpace(title)
			if title == "" {
				return "", fmt.Errorf("title is required")
			}
			rawItems, _ := args["items"].([]interface{})
			var items []string
			for _, it := range rawItems {
				p, ok := it.(string)
				p = strings.TrimSpace(p)
				if !ok || p == "" {
					continue
				}
				items = append(items, p)
			}
			guideNote, _ := args["guide_note"].(string)
			guideNote = strings.TrimSpace(guideNote)
			if len(items) == 0 && guideNote == "" {
				return "", fmt.Errorf("either items (pre-made files) or guide_note (instructions for a dynamic activity) is required")
			}

			for _, p := range items {
				if err := approveForChild(p); err != nil {
					return "", fmt.Errorf("item %q: %w", p, err)
				}
			}

			pkg := learningPackage{
				Title:     title,
				Items:     items,
				GuideNote: guideNote,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			manifestRel := filepath.ToSlash(filepath.Join("shared", "packages", time.Now().UTC().Format("2006-01-02")+"-"+slugify(title)+".json"))
			abs, ok := resolveWorkspacePath(manifestRel)
			if !ok {
				return "", fmt.Errorf("invalid manifest path")
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
				return "", fmt.Errorf("failed to create packages folder: %w", err)
			}
			b, err := json.MarshalIndent(pkg, "", "  ")
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, b, 0o600); err != nil {
				return "", fmt.Errorf("failed to write package manifest: %w", err)
			}
			if err := approveForChild(manifestRel); err != nil {
				return "", fmt.Errorf("failed to hand off package manifest: %w", err)
			}

			if recordEvent != nil {
				recordEvent(toolEvent{Tool: "create_learning_package", Path: manifestRel, Package: title})
			}
			return fmt.Sprintf(`{"status":"ok","package":%q,"manifest":%q,"items":%d}`, title, manifestRel, len(items)), nil
		},
	}
}
