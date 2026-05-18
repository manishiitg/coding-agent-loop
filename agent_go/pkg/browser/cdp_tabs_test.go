package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

func TestParseTabSelection(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantTab   string
		wantClear bool
		wantErr   bool
	}{
		{name: "list tabs", args: nil},
		{name: "select existing tab", args: []string{"t1"}, wantTab: "t1"},
		{name: "select labeled tab", args: []string{"daily-post"}, wantTab: "daily-post"},
		{name: "new labeled tab", args: []string{"new", "--label", "daily-post", "https://example.com"}, wantTab: "daily-post"},
		{name: "new tab requires label", args: []string{"new", "https://example.com"}, wantErr: true},
		{name: "close selected tab", args: []string{"close", "daily-post"}, wantTab: "daily-post", wantClear: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTab, gotClear, err := parseTabSelection(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotTab != tt.wantTab || gotClear != tt.wantClear {
				t.Fatalf("parseTabSelection() = (%q, %v), want (%q, %v)", gotTab, gotClear, tt.wantTab, tt.wantClear)
			}
		})
	}
}

func TestStripRedundantTabCommandArg(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{name: "tab command repeated before new", command: "tab", args: []string{"tab", "new", "--label", "daily-post", "https://example.com"}, want: []string{"new", "--label", "daily-post", "https://example.com"}},
		{name: "tab command repeated before select", command: "tab", args: []string{"tab", "daily-post"}, want: []string{"daily-post"}},
		{name: "multiple repeated tab tokens", command: "tab", args: []string{"tab", "tab", "new", "--label", "daily-post"}, want: []string{"new", "--label", "daily-post"}},
		{name: "single tab token remains selectable", command: "tab", args: []string{"tab"}, want: []string{"tab"}},
		{name: "other command unchanged", command: "snapshot", args: []string{"tab", "daily-post", "-i"}, want: []string{"tab", "daily-post", "-i"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripRedundantTabCommandArg(tt.command, tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeAgentBrowserCommandArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{name: "wait command repeated with duration", command: "wait", args: []string{"wait", "6s"}, want: []string{"6000"}},
		{name: "snapshot command repeated", command: "snapshot", args: []string{"snapshot", "-i"}, want: []string{"-i"}},
		{name: "wait text option unchanged", command: "wait", args: []string{"--text", "Welcome"}, want: []string{"--text", "Welcome"}},
		{name: "single wait token unchanged", command: "wait", args: []string{"wait"}, want: []string{"wait"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAgentBrowserCommandArgs(tt.command, tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMissingCDPPageActionTabErrorShowsWaitRetry(t *testing.T) {
	err := missingCDPPageActionTabError("wait", []string{"wait", "6s"}, "tabs")
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		`agent_browser(command="wait", args=["tab","<tab-id-or-label>","6000"])`,
		"Do not put the command name inside args",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
	}
}

func TestCDPTabAliasCache(t *testing.T) {
	port := 20922
	owner := "owner-for-alias-test"
	clearCDPTabSelectionsForPort(port)
	t.Cleanup(func() { clearCDPTabSelectionsForPort(port) })

	output := `{"success":true,"data":{"tabs":[{"active":true,"label":"upwork_proposal","tabId":"t12","title":"Submit a Proposal","url":"https://www.upwork.com/nx/proposals/job/~02/apply/"}]},"error":null}`
	if got := findCDPTabID(output, "upwork_proposal"); got != "t12" {
		t.Fatalf("findCDPTabID() = %q, want t12", got)
	}
	if got := findCDPTabID(output, "t12"); got != "t12" {
		t.Fatalf("findCDPTabID(tab id) = %q, want t12", got)
	}

	setCDPTabAlias(port, owner, "upwork_proposal", "t12")
	setCDPTabSelection(port, owner, "upwork_proposal")
	setCDPActiveTab(port, "t12")
	clearCDPActiveTabForPort(port)
	if got := getCDPActiveTab(port); got != "" {
		t.Fatalf("active tab after daemon reset clear = %q, want empty", got)
	}
	if got := getCDPTabAlias(port, owner, "upwork_proposal"); got != "t12" {
		t.Fatalf("getCDPTabAlias() = %q, want t12", got)
	}
	if got := getCDPTabSelection(port, owner); got != "upwork_proposal" {
		t.Fatalf("selection after daemon reset clear = %q, want upwork_proposal", got)
	}
	if got := getCDPTabAlias(port, owner, "t12"); got != "" {
		t.Fatalf("tab ids should not resolve as aliases, got %q", got)
	}

	clearCDPTabAlias(port, owner, "upwork_proposal")
	if got := getCDPTabAlias(port, owner, "upwork_proposal"); got != "" {
		t.Fatalf("alias after clear = %q, want empty", got)
	}
}

func TestStripCDPArgs(t *testing.T) {
	got := stripCDPArgs([]string{"--cdp", "http://localhost:9222", "new", "--label", "daily-post"})
	want := []string{"new", "--label", "daily-post"}
	if len(got) != len(want) {
		t.Fatalf("stripCDPArgs() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stripCDPArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractInlineCDPTab(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantTab     string
		wantCleaned []string
		wantErr     bool
	}{
		{name: "tab prefix", args: []string{"tab", "t1", "-i"}, wantTab: "t1", wantCleaned: []string{"-i"}},
		{name: "tab flag", args: []string{"--tab", "upwork", "https://example.com"}, wantTab: "upwork", wantCleaned: []string{"https://example.com"}},
		{name: "missing tab", args: []string{"-i"}, wantCleaned: []string{"-i"}},
		{name: "tab missing value", args: []string{"tab"}, wantErr: true},
		{name: "multiple tabs", args: []string{"tab", "t1", "--tab", "t2"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTab, gotCleaned, err := extractInlineCDPTab(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotTab != tt.wantTab {
				t.Fatalf("tab = %q, want %q", gotTab, tt.wantTab)
			}
			if len(gotCleaned) != len(tt.wantCleaned) {
				t.Fatalf("cleaned len = %d, want %d (%v)", len(gotCleaned), len(tt.wantCleaned), gotCleaned)
			}
			for i := range tt.wantCleaned {
				if gotCleaned[i] != tt.wantCleaned[i] {
					t.Fatalf("cleaned[%d] = %q, want %q", i, gotCleaned[i], tt.wantCleaned[i])
				}
			}
		})
	}
}

func TestStripInlineTabFromOpenArgs(t *testing.T) {
	tab, cleaned, ok, err := stripInlineTabFromOpenArgs([]string{"tab", "t1", "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || tab != "t1" || len(cleaned) != 1 || cleaned[0] != "https://example.com" {
		t.Fatalf("stripInlineTabFromOpenArgs() = (%q, %v, %v), want t1 URL true", tab, cleaned, ok)
	}

	_, cleaned, ok, err = stripInlineTabFromOpenArgs([]string{"https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || len(cleaned) != 1 || cleaned[0] != "https://example.com" {
		t.Fatalf("expected URL-only open args to stay unchanged, got cleaned=%v ok=%v", cleaned, ok)
	}

	if _, _, _, err := stripInlineTabFromOpenArgs([]string{"tab", "t1"}); err == nil {
		t.Fatalf("expected malformed tab-prefixed open args to fail")
	}
}

func TestExtractCDPArg(t *testing.T) {
	info, cleaned, err := extractCDPArg([]string{"--cdp", "http://localhost:9222", "tab", "t1", "-i"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.found || info.url != "http://localhost:9222" || info.port != 9222 {
		t.Fatalf("cdp info = %+v, want found localhost:9222 port 9222", info)
	}
	want := []string{"tab", "t1", "-i"}
	if len(cleaned) != len(want) {
		t.Fatalf("cleaned len = %d, want %d (%v)", len(cleaned), len(want), cleaned)
	}
	for i := range want {
		if cleaned[i] != want[i] {
			t.Fatalf("cleaned[%d] = %q, want %q", i, cleaned[i], want[i])
		}
	}

	info, cleaned, err = extractCDPArg([]string{"https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.found || len(cleaned) != 1 || cleaned[0] != "https://example.com" {
		t.Fatalf("expected no cdp arg and unchanged args, got info=%+v cleaned=%v", info, cleaned)
	}
}

func TestCDPOwnerIDUsesStableBrowserSessionOverride(t *testing.T) {
	agentSession := "agent-session-for-cdp-owner-test"
	workflowSession := "workflow-session-for-cdp-owner-test"
	browserSession := "workflow-browser-stable-owner"
	common.SetSessionBrowserSessionID(agentSession, browserSession)
	defer common.ClearSessionShellConfig(agentSession)

	got := cdpOwnerID(workflowSession, agentSession, "shared-cdp-9222")
	if got != browserSession {
		t.Fatalf("cdpOwnerID() = %q, want %q", got, browserSession)
	}
}

func TestCDPActiveTabTracksPortSelection(t *testing.T) {
	port := 9922
	clearCDPTabSelectionsForPort(port)
	t.Cleanup(func() { clearCDPTabSelectionsForPort(port) })

	if got := getCDPActiveTab(port); got != "" {
		t.Fatalf("active tab = %q, want empty", got)
	}

	setCDPActiveTab(port, "workflow-tab")
	if got := getCDPActiveTab(port); got != "workflow-tab" {
		t.Fatalf("active tab = %q, want workflow-tab", got)
	}

	clearCDPActiveTab(port, "other-tab")
	if got := getCDPActiveTab(port); got != "workflow-tab" {
		t.Fatalf("active tab = %q, want workflow-tab after clearing unrelated tab", got)
	}

	clearCDPActiveTab(port, "workflow-tab")
	if got := getCDPActiveTab(port); got != "" {
		t.Fatalf("active tab = %q, want empty after clearing active tab", got)
	}
}

func TestAcquireSharedCDPLockHonorsContext(t *testing.T) {
	port := 19922
	unlock, err := acquireSharedCDPLock(context.Background(), port)
	if err != nil {
		t.Fatalf("first acquireSharedCDPLock() error = %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if secondUnlock, err := acquireSharedCDPLock(ctx, port); err == nil {
		secondUnlock()
		t.Fatalf("second acquireSharedCDPLock() unexpectedly succeeded while lock was held")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquireSharedCDPLock() error = %v, want context deadline", err)
	}
}
