package browser

import "testing"

import "mcp-agent-builder-go/agent_go/pkg/common"

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
