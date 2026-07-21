package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// packageInfo mirrors the learningPackage manifest (learning_package_tool.go)
// plus the manifest's own workspace-relative path, for callers that need to
// know WHICH package a file belongs to or list every package that exists.
type packageInfo struct {
	Manifest  string   `json:"manifest"`
	Title     string   `json:"title"`
	Items     []string `json:"items"`
	GuideNote string   `json:"guide_note,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

// listPackages walks shared/packages/ and parses every manifest — the parent
// side's view of what packages exist at all, independent of what's been
// approved for the child (unlike childPackages, which is computed client-side
// from the child's own approved-file tree).
func listPackages() []packageInfo {
	root := filepath.Join(workspaceRoot(), "shared", "packages")
	out := []packageInfo{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var pkg learningPackage
		if err := json.Unmarshal(b, &pkg); err != nil {
			return nil
		}
		rel, err := filepath.Rel(workspaceRoot(), p)
		if err != nil {
			return nil
		}
		out = append(out, packageInfo{
			Manifest:  filepath.ToSlash(rel),
			Title:     pkg.Title,
			Items:     pkg.Items,
			GuideNote: pkg.GuideNote,
			CreatedAt: pkg.CreatedAt,
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// resolvePackageForPath returns the manifest path + title of the package that
// contains path — either path IS a manifest itself, or a manifest under
// shared/packages/ lists it as one of its items. Returns ("", "") if path
// isn't part of any package (a standalone handoff).
func resolvePackageForPath(path string) (manifest string, title string) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	for _, pkg := range listPackages() {
		if pkg.Manifest == path {
			return pkg.Manifest, pkg.Title
		}
		for _, item := range pkg.Items {
			if item == path {
				return pkg.Manifest, pkg.Title
			}
		}
	}
	return "", ""
}

func handlePackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, listPackages())
}
