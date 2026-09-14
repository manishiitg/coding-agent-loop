package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGetFileLinkToolCreatesAuthenticatedFileAndFolderLinks(t *testing.T) {
	f := newExternalToolsFixture(t)
	t.Setenv("PUBLIC_URL", "https://confida.example/")
	reg := &recordingRegistrar{}
	if err := f.api.registerShareLinkTools(reg, "owner", "Workflow/invoices"); err != nil {
		t.Fatal(err)
	}
	tool, ok := reg.tools["get_file_link"]
	if !ok || !strings.Contains(tool.desc, "grants no access") {
		t.Fatalf("get_file_link registration = %+v", tool)
	}

	for _, tc := range []struct {
		path string
		kind string
	}{
		{path: "docs/process.md", kind: "file"},
		{path: "docs", kind: "folder"},
	} {
		out, err := tool.exec(context.Background(), map[string]interface{}{"path": tc.path})
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if result["kind"] != tc.kind || result["path"] != tc.path {
			t.Fatalf("%s metadata = %#v", tc.path, result)
		}
		preview, err := url.Parse(result["preview_url"].(string))
		if err != nil || preview.Path != "/"+tc.kind {
			t.Fatalf("%s preview = %v err=%v", tc.path, preview, err)
		}
		if result["url"] != result["preview_url"] {
			t.Fatalf("%s primary URL and compatibility alias differ: %#v", tc.path, result)
		}
		decoded, err := base64.StdEncoding.DecodeString(preview.Query().Get("path"))
		if err != nil || string(decoded) != "Workflow/invoices/"+tc.path {
			t.Fatalf("%s encoded path = %q err=%v", tc.path, decoded, err)
		}
		if strings.Contains(preview.RawQuery, "token") {
			t.Fatal("share URL contains a credential")
		}
	}
}

func TestGetFileLinkToolScopesWorkLinksToProjectOwner(t *testing.T) {
	f := newExternalToolsFixture(t)
	t.Setenv("PUBLIC_URL", "https://confida.example")
	const userID = "work-user"
	f.write(t, "_users/"+userID+"/Chats/Work/projects/demo/output/report.txt", "ready")
	reg := &recordingRegistrar{}
	if err := f.api.registerWorkShareLinkTool(reg, userID, "Chats/Work/projects/demo"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path string
		kind string
	}{
		{path: "output/report.txt", kind: "file"},
		{path: "output", kind: "folder"},
	} {
		out, err := reg.tools["get_file_link"].exec(context.Background(), map[string]interface{}{"path": tc.path})
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		preview, err := url.Parse(result["preview_url"].(string))
		if err != nil || preview.Path != "/"+tc.kind || preview.Query().Get("uid") != userID {
			t.Fatalf("Work preview = %v err=%v", preview, err)
		}
		if result["url"] != result["preview_url"] {
			t.Fatalf("Work primary URL and compatibility alias differ: %#v", result)
		}
		decoded, err := base64.StdEncoding.DecodeString(preview.Query().Get("path"))
		if err != nil || string(decoded) != "Chats/Work/projects/demo/"+tc.path {
			t.Fatalf("Work encoded path = %q err=%v", decoded, err)
		}
		if tc.kind == "file" {
			request := adminRequest("GET", preview.RequestURI(), "", &UserClaims{UserID: userID}, nil)
			response := httptest.NewRecorder()
			f.api.handlePublicFile(response, request)
			if response.Code != 200 || response.Body.String() != "ready" {
				t.Fatalf("owner could not open Work link: %d %s", response.Code, response.Body.String())
			}
			request = adminRequest("GET", preview.RequestURI(), "", &UserClaims{UserID: "other-user"}, nil)
			response = httptest.NewRecorder()
			f.api.handlePublicFile(response, request)
			if response.Code != 403 {
				t.Fatalf("another user opened a personal Work link: %d", response.Code)
			}
		}
	}

	if _, err := reg.tools["get_file_link"].exec(context.Background(), map[string]interface{}{"path": "../outside.txt"}); err == nil {
		t.Fatal("Work share tool accepted traversal")
	}
	if err := f.api.registerWorkShareLinkTool(&recordingRegistrar{}, userID, "Chats/another-project"); err == nil {
		t.Fatal("Work share tool accepted a non-Work project root")
	}
}

func TestGetFileLinkToolRejectsUnauthorizedPrivateAndMissingPaths(t *testing.T) {
	f := newExternalToolsFixture(t)
	t.Setenv("PUBLIC_URL", "https://confida.example")

	denied := &recordingRegistrar{}
	if err := f.api.registerShareLinkTools(denied, "outsider", "Workflow/invoices"); err != nil {
		t.Fatal(err)
	}
	if _, err := denied.tools["get_file_link"].exec(context.Background(), map[string]interface{}{"path": "docs/process.md"}); err == nil {
		t.Fatal("outsider created a workflow link")
	}

	allowed := &recordingRegistrar{}
	if err := f.api.registerShareLinkTools(allowed, "owner", "Workflow/invoices"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret", "Workflow/invoices/docs/process.md", "secrets/token", "docs/missing.pdf", "."} {
		if _, err := allowed.tools["get_file_link"].exec(context.Background(), map[string]interface{}{"path": p}); err == nil {
			t.Fatalf("unsafe or missing path accepted: %s", p)
		}
	}
}
