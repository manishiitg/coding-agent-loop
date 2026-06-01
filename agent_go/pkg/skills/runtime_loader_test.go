package skills

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeWorkspaceServer minimally implements the workspace API endpoints
// that runtime_loader.go hits: GET /api/documents?folder=<path> for
// directory listings and GET /api/documents/<path> for file contents.
// Routes are matched by suffix so tests can register fixtures by their
// workspace-relative paths.
//
// Why a real httptest.Server instead of mocking WorkspaceAPIClient:
// LoadAttachable holds a *WorkspaceAPIClient internally and there's no
// dependency-injection seam to swap it. Going through the HTTP layer
// also catches any URL-encoding / response-shape regressions that pure
// in-memory mocks would miss.
func fakeWorkspaceServer(t *testing.T, files map[string]string, listings map[string][]DocumentEntry) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/documents", func(w http.ResponseWriter, r *http.Request) {
		folder := r.URL.Query().Get("folder")
		children, ok := listings[folder]
		if !ok {
			http.Error(w, "no listing fixture for "+folder, http.StatusNotFound)
			return
		}
		// Mirror the unwrap shape the real workspace returns: data is a
		// single-element array wrapping the requested folder, with
		// children inside.
		resp := DocumentsResponse{
			Success: true,
			Data: []DocumentEntry{{
				Filepath: folder,
				Type:     "folder",
				Children: children,
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/documents/")
		content, ok := files[path]
		if !ok {
			http.Error(w, "no file fixture for "+path, http.StatusNotFound)
			return
		}
		// Encode via raw JSON to avoid having to redeclare the
		// anonymous Data struct's exact tag set when it drifts.
		payload := map[string]interface{}{
			"success": true,
			"data":    map[string]interface{}{"content": content},
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	return httptest.NewServer(mux)
}

func TestLoadAttachableBuildsSkillFromWorkspace(t *testing.T) {
	files := map[string]string{
		"skills/pdf-extract/SKILL.md":          "---\nname: pdf-extract\ndescription: Extract text from PDFs\n---\n\n# PDF Extract\n\nUse this skill to extract structured text from PDF files.\n",
		"skills/pdf-extract/scripts/run.py":    "print('hi')\n",
		"skills/pdf-extract/references/api.md": "# API Reference\n\nDetails on the extraction API.\n",
	}
	listings := map[string][]DocumentEntry{
		"skills/pdf-extract": {
			{Filepath: "skills/pdf-extract/SKILL.md", Type: "file"},
			{Filepath: "skills/pdf-extract/scripts", Type: "folder", Children: []DocumentEntry{
				{Filepath: "skills/pdf-extract/scripts/run.py", Type: "file"},
			}},
			{Filepath: "skills/pdf-extract/references", Type: "folder", Children: []DocumentEntry{
				{Filepath: "skills/pdf-extract/references/api.md", Type: "file"},
			}},
		},
	}
	srv := fakeWorkspaceServer(t, files, listings)
	defer srv.Close()

	got := LoadAttachable(srv.URL, []string{"pdf-extract"})
	if len(got) != 1 {
		t.Fatalf("expected 1 skill loaded, got %d", len(got))
	}
	skill := got[0]
	if skill.Name != "pdf-extract" {
		t.Errorf("expected Name=pdf-extract, got %q", skill.Name)
	}
	if skill.Description != "Extract text from PDFs" {
		t.Errorf("expected description from frontmatter, got %q", skill.Description)
	}
	if !strings.Contains(skill.Content, "# PDF Extract") {
		t.Errorf("expected body content, got %q", skill.Content)
	}
	if len(skill.SupportingFiles) != 2 {
		t.Fatalf("expected 2 supporting files (scripts/run.py + references/api.md), got %d: %+v", len(skill.SupportingFiles), skill.SupportingFiles)
	}
	// SKILL.md must NOT appear as a supporting file — its body is in
	// skill.Content. Adapters re-materialize SKILL.md from the
	// structured fields.
	for _, sf := range skill.SupportingFiles {
		if strings.EqualFold(sf.RelPath, "SKILL.md") {
			t.Errorf("SKILL.md leaked into SupportingFiles: %+v", sf)
		}
	}
}

func TestLoadAttachableLoadsAgentBrowserSkill(t *testing.T) {
	got := LoadAttachable("http://unused.example", []string{"agent-browser"})
	if len(got) != 1 {
		t.Fatalf("expected agent-browser skill to load, got %+v", got)
	}
	if got[0].Name != "agent-browser" || !strings.Contains(got[0].Content, "CDP Shared Chrome Rules") {
		t.Errorf("unexpected loaded skill: %+v", got[0])
	}
}

func TestLoadAttachableLoadsPlaywrightSkill(t *testing.T) {
	got := LoadAttachable("http://unused.example", []string{"playwright"})
	if len(got) != 1 {
		t.Fatalf("expected playwright skill to load, got %+v", got)
	}
	if got[0].Name != "playwright" || !strings.Contains(got[0].Content, "Shared Browser Vs Isolated Browser") {
		t.Errorf("unexpected loaded skill: %+v", got[0])
	}
}

func TestLoadAttachableSkipsMissingSkills(t *testing.T) {
	// A skill folder name in the selection that doesn't exist in the
	// workspace must not crash the load — log and skip, return the rest.
	srv := fakeWorkspaceServer(t,
		map[string]string{
			"skills/real/SKILL.md": "---\nname: real\ndescription: A real skill\n---\nbody\n",
		},
		map[string][]DocumentEntry{
			"skills/real": {{Filepath: "skills/real/SKILL.md", Type: "file"}},
		},
	)
	defer srv.Close()

	got := LoadAttachable(srv.URL, []string{"real", "does-not-exist"})
	if len(got) != 1 {
		t.Fatalf("expected 1 skill (real, with missing skipped), got %d", len(got))
	}
	if got[0].Name != "real" {
		t.Errorf("expected real skill, got %q", got[0].Name)
	}
}

func TestLoadAttachableHandlesEmptySelection(t *testing.T) {
	if got := LoadAttachable("http://unused.example", nil); got != nil {
		t.Errorf("expected nil for nil selection, got %+v", got)
	}
	if got := LoadAttachable("http://unused.example", []string{}); got != nil {
		t.Errorf("expected nil for empty selection, got %+v", got)
	}
}
