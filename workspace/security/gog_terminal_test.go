package security

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Explicitly opt in when validating a connected local account. This performs
// a Gmail read through the actual sandbox and real gog binary; it never sends.
func TestGogLiveTerminalRead(t *testing.T) {
	account := os.Getenv("AGENTWORKS_GOG_LIVE_ACCOUNT")
	if account == "" {
		t.Skip("set AGENTWORKS_GOG_LIVE_ACCOUNT for a live read-only smoke test")
	}
	t.Setenv("NATIVE_WORKSPACE", "true")
	docs := t.TempDir()
	iso := &Isolator{BaseDir: docs, WorkDir: docs, ReadPaths: []string{docs}, WritePaths: []string{docs}}
	cmd, cleanup, err := iso.ExecuteIsolated(context.Background(), `gog --account "$AGENTWORKS_GOG_LIVE_ACCOUNT" --readonly --no-input gmail labels list --json`, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	cmd.Env = append(cmd.Env, "AGENTWORKS_GOG_LIVE_ACCOUNT="+account)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sandboxed gog read failed: %v", err)
	}
	var result struct {
		Labels []struct {
			ID string `json:"id"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	for _, label := range result.Labels {
		if label.ID == "INBOX" {
			return
		}
	}
	t.Fatal("Gmail read did not return the system INBOX label")
}

func TestGogTerminalStoreOutsideWorkspace(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox integration; Linux policy covered separately")
	}
	base := t.TempDir()
	docs := filepath.Join(base, "workspace-docs")
	home := filepath.Join(base, "private", "gog store")
	if err := os.MkdirAll(docs, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "credential-fixture"), []byte("fixture-only"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOG_HOME", home)
	t.Setenv("NATIVE_WORKSPACE", "true")
	t.Setenv("AGENTWORKS_GOG_TERMINAL_ACCESS", "")
	for _, mode := range []string{"builder", "workflow"} {
		t.Run(mode, func(t *testing.T) {
			iso := &Isolator{BaseDir: docs, WorkDir: docs, ReadPaths: []string{docs}, WritePaths: []string{docs}}
			// A script, a pipe and a write stand in for the real CLI's token read,
			// parsing and refresh. The actual credential files are never touched.
			cmd, cleanup, err := iso.ExecuteIsolated(context.Background(), `cat "$GOG_HOME/credential-fixture" | tr a-z A-Z; printf refreshed > "$GOG_HOME/refreshed"`, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "FIXTURE-ONLY") {
				t.Fatalf("terminal failed: %s: %v", out, err)
			}
			if _, err := os.Stat(filepath.Join(home, "refreshed")); err != nil {
				t.Fatal(err)
			}
			if len(iso.WritePaths) != 1 {
				t.Fatal("shared policy mutated")
			}
		})
	}
	iso := &Isolator{BaseDir: docs, WorkDir: docs, ReadPaths: []string{docs}, WritePaths: []string{docs}, StrictAllowlist: true}
	cmd, cleanup, err := iso.ExecuteIsolated(context.Background(), `test -z "$GOG_HOME"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict profile received home: %s: %v", out, err)
	}
}
