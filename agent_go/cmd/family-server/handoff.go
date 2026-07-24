package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// currentTask is the pointer file at child/current-task.json — it names the
// exact file (or first item of a package) the parent most recently handed off,
// plus which package (if any) that file belongs to. The Package field is what
// lets handleHandoff decide resume-vs-new-session: continuing the SAME package
// keeps the child's ongoing conversation going, anything else (a different
// package, or a standalone file with no package at all) starts a fresh one —
// a child shouldn't have last week's algebra chat context bleeding into
// today's new reading assignment.
type currentTask struct {
	Path    string `json:"path"`
	Package string `json:"package,omitempty"`
	// Title and GuideNote carry the learning package's own info INTO the child's
	// readable space (child/current-task.json), so the tutor reliably has the
	// custom instructions without needing to read shared/packages/ (which the
	// child sandbox restricts). GuideNote is the parent's pacing/what-to-do-if-
	// stuck note for this bundle; empty for a plain single-file handoff.
	Title     string `json:"title,omitempty"`
	GuideNote string `json:"guide_note,omitempty"`
	// Items is the FULL ordered list of the package's real shared/ file paths.
	// This is what gives the tutor context of the whole bundle — not just the
	// first file — and IS the child sandbox's allow-list for this activity
	// (see currentActivityItems/childCanSee/childCanWrite) — the child reads
	// and annotates these exact files directly, there is no separate copy.
	// Empty for a single-file / instruction-only handoff.
	Items []string `json:"items,omitempty"`
}

func currentTaskPath() (string, bool) { return resolveWorkspacePath("child/current-task.json") }

func loadCurrentTask() currentTask {
	abs, ok := currentTaskPath()
	if !ok {
		return currentTask{}
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return currentTask{}
	}
	var ct currentTask
	_ = json.Unmarshal(b, &ct)
	return ct
}

func saveCurrentTask(ct currentTask) {
	abs, ok := currentTaskPath()
	if !ok {
		return
	}
	_ = os.MkdirAll(filepath.Dir(abs), 0o700)
	b, _ := json.Marshal(ct)
	_ = os.WriteFile(abs, b, 0o600)
}

// POST /api/parent/handoff — the "Give to <child>" button (a single file) or
// the "Open child learning space" button (an entire package, via manifest).
// It does the one real state change a handoff needs: approve the material for
// the child so it's on their screen and readable by their sandbox. The
// frontend then switches into child mode and lets Quill (the child agent)
// open the actual file and greet the child about it — nothing the child reads
// is templated here in code.
func handleHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path     string `json:"path"`
		Manifest string `json:"manifest"`
		// Resume, when the frontend sends it, is the PARENT'S explicit answer to
		// "continue Myra's existing chat, or start fresh?" (asked only when the
		// package genuinely matches what's already active — see
		// startPackageHandoff in LearningApp.tsx) and overrides the same-package
		// heuristic below entirely. Nil when the frontend didn't ask (a
		// standalone file, or a different/first-ever package) — those cases fall
		// back to the heuristic.
		Resume *bool `json:"resume,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path or manifest is required"})
		return
	}

	var p, pkgManifest, pkgTitle, pkgGuide string
	var childItems []string
	if manifest := strings.TrimSpace(req.Manifest); manifest != "" {
		pkg, ok := findPackageByManifest(manifest)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package not found"})
			return
		}
		// The activity's real shared/ item paths ARE the child's sandbox scope
		// for this session (recorded in child/current-task.json below); the
		// child reads and annotates them directly, no copy involved.
		childItems = append(childItems, pkg.Items...)
		pkgManifest, pkgTitle, pkgGuide = pkg.Manifest, pkg.Title, pkg.GuideNote
		if len(pkg.Items) > 0 {
			p = pkg.Items[0] // so open_file still shows the child something real, not a raw manifest
		} else {
			p = manifest
		}
	} else {
		p = strings.TrimSpace(req.Path)
		if err := validateSharedFile(p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		pkgManifest, pkgTitle = resolvePackageForPath(p)
		// A single file that belongs to a package still carries that package's
		// guide_note, so the tutor follows the same pacing/instructions.
		if pkgManifest != "" {
			if pkg, ok := findPackageByManifest(pkgManifest); ok {
				pkgGuide = pkg.GuideNote
			}
		}
	}

	prev := loadCurrentTask()
	// A standalone file (no package) always starts fresh; continuing the same
	// package resumes; anything else (first handoff, or a different package) is
	// also fresh. The parent's explicit Resume answer overrides this outright.
	var newSession bool
	if req.Resume != nil {
		newSession = !*req.Resume
	} else {
		newSession = pkgManifest == "" || pkgManifest != prev.Package
	}
	saveCurrentTask(currentTask{Path: p, Package: pkgManifest, Title: pkgTitle, GuideNote: pkgGuide, Items: childItems})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"path":        p,
		"package":     pkgTitle,
		"guide_note":  pkgGuide,
		"new_session": newSession,
	})
}

func findPackageByManifest(manifest string) (packageInfo, bool) {
	manifest = strings.TrimSpace(manifest)
	for _, pkg := range listPackages() {
		if pkg.Manifest == manifest {
			return pkg, true
		}
	}
	return packageInfo{}, false
}
