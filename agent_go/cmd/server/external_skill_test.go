package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExternalSkillRequiresIdentity(t *testing.T) {
	api := &StreamingAPI{}
	for path, handler := range map[string]func(http.ResponseWriter, *http.Request){
		"/api/external/v1/skill.md":  api.handleExternalSkillMD,
		"/api/external/v1/skill.zip": api.handleExternalSkillZIP,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", path, w.Code)
		}
	}
}

func TestExternalSkillMDAndZIP(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://skills.test")
	api := &StreamingAPI{}
	claims := &UserClaims{UserID: "owner", Username: "owner"}

	mdRec := httptest.NewRecorder()
	api.handleExternalSkillMD(mdRec, adminRequest(http.MethodGet, "/api/external/v1/skill.md", "", claims, nil))
	if mdRec.Code != http.StatusOK {
		t.Fatalf("skill.md status %d: %s", mdRec.Code, mdRec.Body.String())
	}
	if ctype := mdRec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/markdown") {
		t.Fatalf("skill.md content type %q", ctype)
	}
	markdown := mdRec.Body.String()
	for _, want := range []string{
		"---\nname: agentworks\n",
		"description: " + hostedSkillDescription,
		"https://skills.test",
		"get_agent_context",
		"list_workflows",
		"run_status",
		"authoring is not exposed",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("skill.md missing %q", want)
		}
	}
	if strings.Contains(markdown, "aw_pat_") || strings.Contains(markdown, "agentworks login") || strings.Contains(markdown, "claude mcp add") {
		t.Fatal("hosted skill must not carry credentials or local-CLI setup")
	}
	if len(hostedSkillDescription) > 1024 || strings.ContainsAny(hostedSkillDescription, "<>") {
		t.Fatal("skill frontmatter description violates upload-scanner constraints")
	}

	zipRec := httptest.NewRecorder()
	api.handleExternalSkillZIP(zipRec, adminRequest(http.MethodGet, "/api/external/v1/skill.zip", "", claims, nil))
	if zipRec.Code != http.StatusOK {
		t.Fatalf("skill.zip status %d: %s", zipRec.Code, zipRec.Body.String())
	}
	if ctype := zipRec.Header().Get("Content-Type"); ctype != "application/zip" {
		t.Fatalf("skill.zip content type %q", ctype)
	}
	archive, err := zip.NewReader(bytes.NewReader(zipRec.Body.Bytes()), int64(zipRec.Body.Len()))
	if err != nil {
		t.Fatalf("unzip: %v", err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != "agentworks/SKILL.md" {
		names := make([]string, 0, len(archive.File))
		for _, f := range archive.File {
			names = append(names, f.Name)
		}
		t.Fatalf("zip entries %v, want [agentworks/SKILL.md]", names)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	zipped, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	if string(zipped) != markdown {
		t.Fatal("zipped SKILL.md differs from served skill.md")
	}
}
