package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

func newCDPCleanupTestClient(t *testing.T) (*Client, <-chan ShellExecuteRequest) {
	t.Helper()
	requests := make(chan ShellExecuteRequest, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ShellExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- request
		_ = json.NewEncoder(w).Encode(APIResponse{
			Success: true,
			Data: ShellExecuteResponse{
				Stdout:   `{"success":true}`,
				ExitCode: 0,
			},
		})
	}))
	t.Cleanup(server.Close)
	return NewClient(server.URL), requests
}

func awaitCDPCleanupRequest(t *testing.T, requests <-chan ShellExecuteRequest) ShellExecuteRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delayed CDP tab cleanup")
		return ShellExecuteRequest{}
	}
}

func assertNoCDPCleanupRequest(t *testing.T, requests <-chan ShellExecuteRequest, wait time.Duration) {
	t.Helper()
	select {
	case request := <-requests:
		t.Fatalf("unexpected CDP cleanup request: %s", request.Command)
	case <-time.After(wait):
	}
}

func TestDelayedCDPTabCleanupClosesOnlyWorkflowOwnedTabs(t *testing.T) {
	resetCDPRegistryForTest(t)
	t.Setenv("CDP_HOST", "localhost")
	client, requests := newCDPCleanupTestClient(t)
	const (
		port  = 29222
		owner = "workflow-cleanup-owner"
	)

	setCDPTabAlias(port, owner, "workflow-created", "t7")
	markCDPTabOwned(port, owner, "workflow-created", "t7")
	setCDPTabAlias(port, owner, "user-existing", "t8")

	AcquireCDPTabOwnerLease(owner, []int{port})
	ReleaseCDPTabOwnerLease(owner, []int{port}, client, 10*time.Millisecond)

	request := awaitCDPCleanupRequest(t, requests)
	for _, want := range []string{"--session shared-cdp-29222", "tab close t7", "--cdp http://localhost:29222"} {
		if !strings.Contains(request.Command, want) {
			t.Fatalf("cleanup command %q missing %q", request.Command, want)
		}
	}
	if strings.Contains(request.Command, "t8") {
		t.Fatalf("cleanup must not close a pre-existing user tab: %s", request.Command)
	}
	assertNoCDPCleanupRequest(t, requests, 40*time.Millisecond)
	if tabs := ownedCDPTabsForOwner(port, owner); len(tabs) != 0 {
		t.Fatalf("owned tabs after cleanup = %#v, want none", tabs)
	}
	if got := getCDPTabAlias(port, owner, "user-existing"); got != "t8" {
		t.Fatalf("pre-existing tab alias was changed: got %q", got)
	}
}

func TestDelayedCDPTabCleanupWaitsForLastConcurrentRun(t *testing.T) {
	resetCDPRegistryForTest(t)
	t.Setenv("CDP_HOST", "localhost")
	client, requests := newCDPCleanupTestClient(t)
	const (
		port  = 29223
		owner = "workflow-concurrent-owner"
	)
	markCDPTabOwned(port, owner, "shared-work", "t11")

	AcquireCDPTabOwnerLease(owner, []int{port})
	AcquireCDPTabOwnerLease(owner, []int{port})
	ReleaseCDPTabOwnerLease(owner, []int{port}, client, 10*time.Millisecond)
	assertNoCDPCleanupRequest(t, requests, 50*time.Millisecond)

	ReleaseCDPTabOwnerLease(owner, []int{port}, client, 10*time.Millisecond)
	request := awaitCDPCleanupRequest(t, requests)
	if !strings.Contains(request.Command, "tab close t11") {
		t.Fatalf("cleanup command = %q, want final-run tab close", request.Command)
	}
}

func TestNewCDPTabLeaseCancelsPendingCleanup(t *testing.T) {
	resetCDPRegistryForTest(t)
	t.Setenv("CDP_HOST", "localhost")
	client, requests := newCDPCleanupTestClient(t)
	const (
		port  = 29224
		owner = "workflow-renewed-owner"
	)
	markCDPTabOwned(port, owner, "renewed-work", "t12")

	AcquireCDPTabOwnerLease(owner, []int{port})
	ReleaseCDPTabOwnerLease(owner, []int{port}, client, 80*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	AcquireCDPTabOwnerLease(owner, []int{port})
	assertNoCDPCleanupRequest(t, requests, 100*time.Millisecond)

	ReleaseCDPTabOwnerLease(owner, []int{port}, client, 10*time.Millisecond)
	request := awaitCDPCleanupRequest(t, requests)
	if !strings.Contains(request.Command, "tab close t12") {
		t.Fatalf("cleanup command = %q, want renewed-run tab close", request.Command)
	}
}

func TestAgentBrowserTabNewRegistersCleanupOwnership(t *testing.T) {
	resetCDPRegistryForTest(t)
	t.Setenv("CDP_HOST", "localhost")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(APIResponse{
			Success: true,
			Data: ShellExecuteResponse{
				Stdout:   `{"success":true,"data":{"tabs":[{"active":true,"label":"report","tabId":"t42"}]}}`,
				ExitCode: 0,
			},
		})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "workflow-tab-owner")
	executor := NewExecutor(NewClient(server.URL), WithCdpPort(29225))
	if _, err := executor.HandleAgentBrowser(ctx, map[string]interface{}{
		"command": "tab",
		"args":    []string{"--cdp", "http://localhost:29225", "new", "--label", "report", "https://example.com"},
		"session": "agent-session",
	}); err != nil {
		t.Fatalf("tab new failed: %v", err)
	}

	tabs := ownedCDPTabsForOwner(29225, "workflow-tab-owner")
	if len(tabs) != 1 || tabs[0].Alias != "report" || tabs[0].TabID != "t42" {
		t.Fatalf("registered owned tabs = %#v, want report/t42", tabs)
	}
}
