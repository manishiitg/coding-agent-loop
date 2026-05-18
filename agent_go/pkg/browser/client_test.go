package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExecuteCommandHostDockerFallbackDefaultsToLocalhost(t *testing.T) {
	var got ShellExecuteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(APIResponse{
			Success: true,
			Data: ShellExecuteResponse{
				Stdout:   "ok",
				ExitCode: 0,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.ExecuteCommand(context.Background(), []string{
		"--session", "shared-cdp-9222",
		"tab", "list",
		"--cdp", "http://host.docker.internal:9222",
		"--json",
	}, &ExecuteOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}

	if !strings.Contains(got.Command, `if [ -z "$HOST_IP" ]; then HOST_IP=localhost; fi;`) {
		t.Fatalf("command missing empty HOST_IP fallback: %s", got.Command)
	}
	if !strings.Contains(got.Command, "http://${HOST_IP}:9222") {
		t.Fatalf("command did not replace host.docker.internal with HOST_IP: %s", got.Command)
	}
	if strings.Contains(got.Command, `|| echo 'localhost') &&`) {
		t.Fatalf("command uses pipeline fallback that leaves HOST_IP empty on native macOS: %s", got.Command)
	}
}

func TestCDPRuntimeStartupErrorRecognized(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "auto launch", err: errString("Auto-launch failed: Invalid CDP URL: empty host"), want: true},
		{name: "invalid cdp", err: errString("Invalid CDP URL: empty host"), want: true},
		{name: "timeout", err: errString("command timed out after 30s"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCDPRuntimeStartupError(tt.err); got != tt.want {
				t.Fatalf("isCDPRuntimeStartupError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRetryCDPTimeout(t *testing.T) {
	if !shouldRetryCDPTimeout("snapshot") {
		t.Fatalf("snapshot timeouts should retry")
	}
	if shouldRetryCDPTimeout("wait") {
		t.Fatalf("wait timeouts should not retry the same wait condition")
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}
