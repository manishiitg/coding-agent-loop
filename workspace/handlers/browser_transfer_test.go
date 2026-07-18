package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/manishiitg/coding-agent-loop/workspace/models"
	"github.com/manishiitg/coding-agent-loop/workspace/security"
	"github.com/spf13/viper"
)

func TestExecuteShellBrowserArtifactDestinationIsWorkspaceRelative(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "Downloads"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(security.BrowserArtifactStagingDir(), 0700); err != nil {
		t.Fatal(err)
	}
	staged, err := os.CreateTemp(security.BrowserArtifactStagingDir(), "handler-*.download")
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := staged.Name()
	if _, err := staged.WriteString("download-body"); err != nil {
		t.Fatal(err)
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(stagedPath) })

	oldDocsDir := viper.GetString("docs-dir")
	viper.Set("docs-dir", base)
	t.Cleanup(func() { viper.Set("docs-dir", oldDocsDir) })

	request := models.ExecuteShellRequest{
		Command:          "true",
		WorkingDirectory: "Downloads",
		FolderGuard: &models.FolderGuardConfig{
			Enabled:    true,
			ReadPaths:  []string{"Downloads"},
			WritePaths: []string{"Downloads"},
		},
		ArtifactTransfer: &models.BrowserArtifactTransfer{
			SourcePath:      stagedPath,
			DestinationPath: "Downloads/report.txt",
			Kind:            "download",
			Finalize:        true,
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.ReleaseMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ExecuteShellCommand(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	want := filepath.Join(base, "Downloads", "report.txt")
	got, err := os.ReadFile(want)
	if err != nil || string(got) != "download-body" {
		t.Fatalf("workspace-relative artifact = %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(base, "Downloads", "Downloads", "report.txt")); !os.IsNotExist(err) {
		t.Fatalf("artifact was incorrectly resolved relative to the command working directory: %v", err)
	}
}
