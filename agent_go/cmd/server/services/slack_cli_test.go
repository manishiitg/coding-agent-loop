package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlackCLIChildCredentialIsolation(t *testing.T) {
	t.Setenv("AGENTWORKS_TEST_SECRET", "must-not-inherit")
	binary := filepath.Join(t.TempDir(), "slack")
	script := `#!/bin/sh
if [ -n "$AGENTWORKS_TEST_SECRET" ] || [ -n "$SLACK_USER_TOKEN" ]; then exit 9; fi
if [ "$SLACK_BOT_TOKEN" != "test-bot-credential" ]; then exit 8; fi
for arg in "$@"; do case "$arg" in *test-bot-credential*) exit 7;; esac; done
printf '%s\n' '{"ok":true,"text":"test-bot-credential"}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := executeSlackCLI(context.Background(), binary, "test-bot-credential", "conversations.history", map[string]interface{}{"channel": "C123"})
	if err != nil || strings.Contains(out, "test-bot-credential") || !strings.Contains(out, "[redacted]") {
		t.Fatalf("credential isolation failed: %q %v", out, err)
	}
}
func TestSlackCLIMissingScopeGuidance(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "slack")
	os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '{\"ok\":false,\"error\":\"missing_scope\",\"needed\":\"channels:history\"}'\nexit 1\n"), 0700)
	_, err := executeSlackCLI(context.Background(), binary, "test-token", "conversations.history", nil)
	if err == nil || !strings.Contains(err.Error(), "channels:history") || !strings.Contains(err.Error(), "reinstall") {
		t.Fatalf("missing scope guidance: %v", err)
	}
}

// Read methods reject a JSON body (invalid_arguments): flat parameters are
// form-encoded; structured ones (blocks) keep a JSON body.
func TestSlackCLISendsReadParametersAsForm(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "slack")
	record := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + record + "\nprintf '%s\\n' '{\"ok\":true}'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	args := func() string {
		raw, _ := os.ReadFile(record)
		return string(raw)
	}
	if _, err := executeSlackCLI(context.Background(), binary, "xoxb-test-token", "conversations.replies", map[string]interface{}{"channel": "C0AFT3HBWMS", "ts": "1790286994.260769", "limit": float64(50)}); err != nil {
		t.Fatal(err)
	}
	if got := args(); !strings.Contains(got, "--data\nchannel=C0AFT3HBWMS&limit=50&ts=1790286994.260769\n") || strings.Contains(got, "--json") {
		t.Fatalf("read call args = %q", got)
	}
	if _, err := executeSlackCLI(context.Background(), binary, "xoxb-test-token", "chat.postMessage", map[string]interface{}{"channel": "C1", "blocks": []interface{}{map[string]interface{}{"type": "section"}}}); err != nil {
		t.Fatal(err)
	}
	if got := args(); !strings.Contains(got, "--json\n") {
		t.Fatalf("structured call args = %q", got)
	}
}
