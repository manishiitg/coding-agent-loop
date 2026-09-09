package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func TestBrowserRecordingBundleAndIsolation(t *testing.T) {
	root := t.TempDir()
	socket := t.TempDir()
	bin := t.TempDir()
	old := viper.GetString("docs-dir")
	viper.Set("docs-dir", root)
	defer viper.Set("docs-dir", old)
	t.Setenv("AGENT_BROWSER_SOCKET_DIR", socket)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.MkdirAll(filepath.Join(root, "Workflow", "one"), 0700)
	os.MkdirAll(filepath.Join(root, "Workflow", "two"), 0700)
	os.WriteFile(filepath.Join(socket, "capture-test.stream"), []byte("12345"), 0600)
	script := `#!/bin/sh
shift 2
if [ "$1 $2" = "record stop" ] && [ -f "$AGENT_BROWSER_SOCKET_DIR/fail-stop" ]; then echo stop-failed; exit 1; fi
case "$1 $2 $3" in
 "record start "*) printf video > "$3" ;;
 "network har stop") printf '{"log":{"entries":[]}}' > "$4" ;;
esac
printf '{"success":true,"data":{"messages":["test console"]}}'
`
	os.WriteFile(filepath.Join(bin, "agent-browser"), []byte(script), 0700)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/:session", BrowserRecording)
	call := func(action, workspace string) (int, browserCapture) {
		payload, _ := json.Marshal(map[string]string{"action": action, "workspace_path": workspace})
		req := httptest.NewRequest("POST", "/capture-test", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var capture browserCapture
		json.Unmarshal(response.Body.Bytes(), &capture)
		if response.Code != 200 && workspace == "Workflow/one" {
			t.Fatalf("%s: %s", action, response.Body.String())
		}
		return response.Code, capture
	}
	_, started := call("start", "Workflow/one")
	if !started.Recording {
		t.Fatal("not recording")
	}
	_, same := call("start", "Workflow/one")
	if same.Directory != started.Directory {
		t.Fatal("duplicate start forked capture")
	}
	if code, _ := call("stop", "Workflow/two"); code != 409 {
		t.Fatalf("cross-workflow stop: %d", code)
	}
	if code, _ := call("start", "../../outside"); code != 400 {
		t.Fatalf("path traversal: %d", code)
	}
	os.WriteFile(filepath.Join(socket, "fail-stop"), []byte("1"), 0600)
	_, partial := call("stop", "Workflow/one")
	if !partial.Recording || !partial.VideoActive || partial.HARActive {
		t.Fatalf("failed stop must stay retryable: %+v", partial)
	}
	os.Remove(filepath.Join(socket, "fail-stop"))
	_, stopped := call("stop", "Workflow/one")
	if stopped.Recording || len(stopped.Errors) > 0 {
		t.Fatalf("stop failed: %+v", stopped)
	}
	archive, err := zip.OpenReader(filepath.Join(root, stopped.Directory, "capture.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	files := map[string]bool{}
	for _, file := range archive.File {
		files[file.Name] = true
	}
	for _, name := range []string{"video.webm", "network.har", "console.json", "errors.json", "manifest.json"} {
		if !files[name] {
			t.Errorf("missing %s", name)
		}
	}
	_, again := call("stop", "Workflow/one")
	if again.Directory != stopped.Directory {
		t.Fatal("stop must be idempotent")
	}
}
