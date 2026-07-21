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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path or manifest is required"})
		return
	}

	var p, pkgManifest, pkgTitle string
	if manifest := strings.TrimSpace(req.Manifest); manifest != "" {
		pkg, ok := findPackageByManifest(manifest)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package not found"})
			return
		}
		if err := approveForChild(manifest); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		for _, item := range pkg.Items {
			_ = approveForChild(item) // packages are approved in full at creation; this just keeps it idempotent
		}
		pkgManifest, pkgTitle = pkg.Manifest, pkg.Title
		if len(pkg.Items) > 0 {
			p = pkg.Items[0] // so open_file still shows the child something real, not a raw manifest
		} else {
			p = manifest
		}
	} else {
		p = strings.TrimSpace(req.Path)
		if err := approveForChild(p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		pkgManifest, pkgTitle = resolvePackageForPath(p)
	}

	prev := loadCurrentTask()
	// A standalone file (no package) always starts fresh; continuing the same
	// package resumes; anything else (first handoff, or a different package) is
	// also fresh.
	newSession := pkgManifest == "" || pkgManifest != prev.Package
	// Point at the child's own live copy, not the parent's shared/ original —
	// see mirrorToChildActive: this is what lets the child record progress
	// (via their own shell, already scoped to child/) without ever needing
	// write access into shared/.
	p = mirrorToChildActive(p)
	saveCurrentTask(currentTask{Path: p, Package: pkgManifest})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"path":        p,
		"package":     pkgTitle,
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
