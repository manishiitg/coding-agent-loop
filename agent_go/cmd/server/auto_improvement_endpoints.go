package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// =====================================================================
// HTTP endpoints for the auto-improvement framework.
//
// All read-only.
// =====================================================================

// FrameworkHealthResponse is the JSON shape of GET /api/workflow/framework-health.
// One stop shop for "is the framework wired correctly?": soul preconditions.
type FrameworkHealthResponse struct {
	Success           bool   `json:"success"`
	SoulExists        bool   `json:"soul_exists"`
	ObjectiveOK       bool   `json:"objective_ok"`
	SuccessCriteriaOK bool   `json:"success_criteria_ok"`
	Objective         string `json:"objective,omitempty"`
	SuccessCriteria   string `json:"success_criteria,omitempty"`
	Error             string `json:"error,omitempty"`
}

// handleGetFrameworkHealth surfaces the soul.md preconditions used by workflow
// setup and improvement flows.
func (api *StreamingAPI) handleGetFrameworkHealth(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	pre, err := ReadSoulPreconditions(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, FrameworkHealthResponse{Success: false, Error: err.Error()})
		return
	}
	resp := FrameworkHealthResponse{
		Success:           true,
		SoulExists:        pre.SoulExists,
		ObjectiveOK:       pre.ObjectiveOK,
		SuccessCriteriaOK: pre.SuccessCriteriaOK,
		Objective:         pre.Objective,
		SuccessCriteria:   pre.SuccessCriteria,
	}
	writeAIJSON(w, resp)
}

// BuilderDocResponse is the JSON shape of GET /api/workflow/builder-doc.
// It returns document content (or empty if the file does not exist yet).
type BuilderDocResponse struct {
	Success bool   `json:"success"`
	Doc     string `json:"doc"`     // "improve" | "soul" — echoed back
	Path    string `json:"path"`    // workspace-relative path that was read
	Exists  bool   `json:"exists"`  // false if the file does not exist yet
	Content string `json:"content"` // markdown body, "" when !exists
	Error   string `json:"error,omitempty"`
}

type BuilderDocArchiveFile struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

type BuilderDocArchivesResponse struct {
	Success bool                    `json:"success"`
	Files   []BuilderDocArchiveFile `json:"files"`
	Error   string                  `json:"error,omitempty"`
}

// handleGetBuilderDoc serves the contents of builder/improve.html, soul/soul.md,
// and compact dashboard card fragments so the UI can render them inline.
// The "doc" query param picks which file. Read-only.
func (api *StreamingAPI) handleGetBuilderDoc(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	doc := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("doc")))
	requestedPath := strings.TrimSpace(r.URL.Query().Get("path"))
	var rel string
	switch doc {
	case "improve":
		rel = "builder/improve.html"
	case "soul":
		rel = "soul/soul.md"
	case "card-health":
		rel = "builder/card.health.html"
	case "card-progress":
		rel = "builder/card.progress.html"
	case "card-cost":
		rel = "builder/card.cost.html"
	default:
		http.Error(w, "doc must be one of: improve, soul, card-health, card-progress, card-cost", http.StatusBadRequest)
		return
	}
	if requestedPath != "" {
		if doc != "improve" {
			http.Error(w, "path is only supported for improve archive files", http.StatusBadRequest)
			return
		}
		cleanPath := path.Clean(requestedPath)
		if !isBuilderDocArchivePath(doc, cleanPath) {
			http.Error(w, "path must be under the matching builder/*-archive/ folder and end with .md or .html", http.StatusBadRequest)
			return
		}
		rel = cleanPath
	}
	// If the primary HTML file doesn't exist, fall back to the legacy .md file.
	if requestedPath == "" && doc == "improve" {
		htmlFull := path.Join(strings.Trim(workspacePath, "/"), rel)
		_, htmlExists, _ := readFileFromWorkspace(r.Context(), htmlFull)
		if !htmlExists {
			rel = strings.TrimSuffix(rel, ".html") + ".md"
		}
	}
	full := path.Join(strings.Trim(workspacePath, "/"), rel)
	content, exists, err := readFileFromWorkspace(r.Context(), full)
	if err != nil {
		writeAIJSON(w, BuilderDocResponse{Success: false, Doc: doc, Path: rel, Error: err.Error()})
		return
	}
	if !exists {
		writeAIJSON(w, BuilderDocResponse{Success: true, Doc: doc, Path: rel, Exists: false, Content: ""})
		return
	}
	writeAIJSON(w, BuilderDocResponse{Success: true, Doc: doc, Path: rel, Exists: true, Content: content})
}

// BuilderDocStatus reports a builder doc's existence + last-modified time.
type BuilderDocStatus struct {
	Exists       bool   `json:"exists"`
	LastModified string `json:"last_modified,omitempty"` // RFC3339; empty if unknown/absent
	Path         string `json:"path"`
}

// BuilderDocsStatusResponse is the lightweight freshness payload the workflow
// toolbar polls to badge unseen review/improve updates (no doc content).
type BuilderDocsStatusResponse struct {
	Success bool             `json:"success"`
	Improve BuilderDocStatus `json:"improve"`
	Error   string           `json:"error,omitempty"`
}

// handleGetBuilderDocsStatus: GET /api/workflow/builder-docs-status?workspace_path=...
// Returns existence + last-modified for builder/improve.html so the toolbar can show
// a "new since you last looked" dot without downloading the docs repeatedly.
func (api *StreamingAPI) handleGetBuilderDocsStatus(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	writeAIJSON(w, BuilderDocsStatusResponse{
		Success: true,
		Improve: builderDocStatus(r.Context(), workspacePath, "builder/improve.html"),
	})
}

func builderDocStatus(ctx context.Context, workspacePath, rel string) BuilderDocStatus {
	base := strings.Trim(workspacePath, "/")
	if exists, lm := readWorkspaceFileMeta(ctx, path.Join(base, rel)); exists {
		return BuilderDocStatus{Exists: true, LastModified: lm, Path: rel}
	}
	// Legacy fallback: builder/<doc>.md
	mdRel := strings.TrimSuffix(rel, ".html") + ".md"
	if exists, lm := readWorkspaceFileMeta(ctx, path.Join(base, mdRel)); exists {
		return BuilderDocStatus{Exists: true, LastModified: lm, Path: mdRel}
	}
	return BuilderDocStatus{Exists: false, Path: rel}
}

func (api *StreamingAPI) handleGetBuilderDocArchives(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	doc := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("doc")))
	if doc == "" {
		doc = "improve"
	}
	if doc != "improve" {
		http.Error(w, "doc must be improve", http.StatusBadRequest)
		return
	}
	folder := path.Join(strings.Trim(workspacePath, "/"), "builder", doc+"-archive")
	listing, exists, err := listWorkspaceFolder(r.Context(), folder, 2)
	if err != nil {
		writeAIJSON(w, BuilderDocArchivesResponse{Success: false, Files: []BuilderDocArchiveFile{}, Error: err.Error()})
		return
	}
	if !exists {
		writeAIJSON(w, BuilderDocArchivesResponse{Success: true, Files: []BuilderDocArchiveFile{}})
		return
	}
	var paths []string
	collectWorkspaceFilePaths(listing, &paths)
	files := make([]BuilderDocArchiveFile, 0, len(paths))
	workspacePrefix := strings.Trim(workspacePath, "/") + "/"
	for _, p := range paths {
		rel := strings.TrimPrefix(path.Clean(filepath.ToSlash(p)), workspacePrefix)
		if !isBuilderDocArchivePath(doc, rel) {
			continue
		}
		base := path.Base(rel)
		label := strings.TrimSuffix(strings.TrimSuffix(base, ".html"), ".md")
		files = append(files, BuilderDocArchiveFile{Path: rel, Label: label})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path > files[j].Path
	})
	writeAIJSON(w, BuilderDocArchivesResponse{Success: true, Files: files})
}

func isBuilderDocArchivePath(doc, rel string) bool {
	rel = path.Clean(strings.TrimSpace(rel))
	ext := strings.ToLower(path.Ext(rel))
	return doc == "improve" &&
		strings.HasPrefix(rel, "builder/"+doc+"-archive/") &&
		(ext == ".md" || ext == ".html") &&
		!strings.Contains(rel, "..")
}

// --- Shared HTTP helpers ----------------------------------------------------

func setupCORS(w http.ResponseWriter, r *http.Request, method string) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", method+", OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-ID, X-User-ID")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return false
	}
	if r.Method != method {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func requireWorkspacePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return "", false
	}
	cleaned := filepath.Clean(workspacePath)
	if strings.Contains(cleaned, "..") {
		http.Error(w, "Invalid workspace path", http.StatusBadRequest)
		return "", false
	}
	return cleaned, true
}

func writeAIJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
