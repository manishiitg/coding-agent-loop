package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/manishiitg/mcpagent/mcpclient"
)

// withTempHome points ~ at a temp dir so tool preferences never touch the real
// user profile.
func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestToolPrefsRoundTrip(t *testing.T) {
	withTempHome(t)

	// Nothing saved means nothing disabled: every tool is on by default.
	if got := loadToolPrefs("u1"); len(got.Disabled) != 0 {
		t.Errorf("fresh prefs = %v, want empty", got.Disabled)
	}

	prefs := &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}
	if err := saveToolPrefs("u1", prefs); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := loadToolPrefs("u1")
	if len(loaded.Disabled["notion"]) != 1 || loaded.Disabled["notion"][0] != "delete_page" {
		t.Errorf("loaded = %v, want notion:[delete_page]", loaded.Disabled)
	}

	// Preferences are per user.
	if len(loadToolPrefs("u2").Disabled) != 0 {
		t.Error("one user's tool switches must not leak into another's")
	}
}

func TestToolPrefsFileIsPrivate(t *testing.T) {
	withTempHome(t)

	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"x"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(toolPrefsPath("u1"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestLoadToolPrefsSurvivesCorruptFile(t *testing.T) {
	withTempHome(t)

	path := toolPrefsPath("u1")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A damaged file must fall back to "everything enabled" rather than panic
	// or silently disable tools.
	if got := loadToolPrefs("u1"); got == nil || len(got.Disabled) != 0 {
		t.Errorf("corrupt prefs = %v, want an empty set", got)
	}
}

func TestSplitToolEntry(t *testing.T) {
	tests := []struct {
		entry      string
		wantServer string
		wantTool   string
		wantOK     bool
	}{
		{"notion:search", "notion", "search", true},
		{"notion:*", "notion", "*", true},
		// Server names may contain spaces; only the first colon separates.
		{"Parallel Search MCP:web_search", "Parallel Search MCP", "web_search", true},
		{"noseparator", "noseparator", "", false},
	}

	for _, tc := range tests {
		server, tool, ok := splitToolEntry(tc.entry)
		if server != tc.wantServer || tool != tc.wantTool || ok != tc.wantOK {
			t.Errorf("splitToolEntry(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.entry, server, tool, ok, tc.wantServer, tc.wantTool, tc.wantOK)
		}
	}
}

// apiWithTools builds an API whose discovery cache already knows some servers.
func apiWithTools(t *testing.T, tools map[string][]string) *StreamingAPI {
	t.Helper()

	api := &StreamingAPI{toolStatus: map[string]ToolStatus{}}
	for server, names := range tools {
		details := make([]mcpclient.ToolDetail, 0, len(names))
		for _, n := range names {
			details = append(details, mcpclient.ToolDetail{Name: n})
		}
		api.toolStatus[server] = ToolStatus{Name: server, Server: server, Tools: details}
	}
	return api
}

func TestApplyDisabledToolsNoPreferencesIsAPassThrough(t *testing.T) {
	withTempHome(t)
	api := apiWithTools(t, map[string][]string{"notion": {"search", "delete_page"}})

	// With nothing switched off the selection must be returned untouched, so
	// the agent's "empty means everything" behaviour is preserved.
	if got := api.applyDisabledTools("u1", nil, nil); got != nil {
		t.Errorf("got %v, want nil (no filtering)", got)
	}

	in := []string{"notion:search"}
	got := api.applyDisabledTools("u1", nil, in)
	if len(got) != 1 || got[0] != "notion:search" {
		t.Errorf("got %v, want the input unchanged", got)
	}
}

func TestApplyDisabledToolsSubtractsFromExplicitSelection(t *testing.T) {
	withTempHome(t)
	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	api := apiWithTools(t, map[string][]string{"notion": {"search", "delete_page"}})

	got := api.applyDisabledTools("u1", nil, []string{"notion:search", "notion:delete_page"})
	if len(got) != 1 || got[0] != "notion:search" {
		t.Errorf("got %v, want only notion:search", got)
	}
}

func TestApplyDisabledToolsExpandsWideOpenSelection(t *testing.T) {
	withTempHome(t)
	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	api := apiWithTools(t, map[string][]string{
		"notion": {"search", "read_page", "delete_page"},
		"github": {"list_issues"},
	})

	// An empty selection means "everything", which the agent reads as "no
	// filtering" — so switching one tool off must become an explicit allow-list.
	got := api.applyDisabledTools("u1", nil, nil)
	sort.Strings(got)

	want := []string{"github:*", "notion:read_page", "notion:search"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestApplyDisabledToolsKeepsUntouchedServersWideOpen(t *testing.T) {
	withTempHome(t)
	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	api := apiWithTools(t, map[string][]string{
		"notion": {"search", "delete_page"},
		"github": {"list_issues", "create_issue"},
	})

	got := api.applyDisabledTools("u1", nil, nil)

	// github has nothing switched off, so it must stay a wildcard rather than
	// being pinned to whatever the cache happened to hold at this moment.
	found := false
	for _, e := range got {
		if e == "github:*" {
			found = true
		}
		if e == "github:list_issues" {
			t.Error("a server with no disabled tools must not be pinned to a snapshot")
		}
	}
	if !found {
		t.Errorf("got %v, want github:* to be present", got)
	}
}

func TestApplyDisabledToolsFallsBackToWildcardWhenToolsUnknown(t *testing.T) {
	withTempHome(t)
	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Cache knows the server exists but holds no tool list for it.
	api := &StreamingAPI{toolStatus: map[string]ToolStatus{"notion": {Name: "notion"}}}

	got := api.applyDisabledTools("u1", []string{"notion"}, nil)

	// Without a tool list there is nothing to subtract. Losing the whole
	// connection would be far worse than honouring one stale switch.
	if len(got) != 1 || got[0] != "notion:*" {
		t.Errorf("got %v, want [notion:*]", got)
	}
}

func TestApplyDisabledToolsWithNothingDiscoveredLeavesFilteringOff(t *testing.T) {
	withTempHome(t)
	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	api := &StreamingAPI{toolStatus: map[string]ToolStatus{}}

	// Nothing is known yet. Returning an empty allow-list here would hand the
	// agent zero tools, so filtering must stay off instead.
	if got := api.applyDisabledTools("u1", nil, nil); got != nil {
		t.Errorf("got %v, want nil so the agent keeps all tools", got)
	}
}

func TestApplyDisabledToolsPreservesWildcardEntries(t *testing.T) {
	withTempHome(t)
	if err := saveToolPrefs("u1", &toolPrefs{Disabled: map[string][]string{"notion": {"delete_page"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	api := apiWithTools(t, map[string][]string{"notion": {"search", "delete_page"}})

	// A caller that explicitly asked for "notion:*" gets it back; expanding it
	// here would silently change what that caller requested.
	got := api.applyDisabledTools("u1", nil, []string{"notion:*"})
	if len(got) != 1 || got[0] != "notion:*" {
		t.Errorf("got %v, want [notion:*]", got)
	}
}

func TestSetConnectionToolsPersistsAndClears(t *testing.T) {
	withTempHome(t)
	api, router := newTestAPI(t)
	router.HandleFunc("/api/connections/{id}/tools", api.handleSetConnectionTools).Methods("PUT")
	_ = api

	rec, _ := doJSON(t, router, "PUT", "/api/connections/notion/tools", `{"disabled":["delete_page","archive_page"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
	}

	saved := loadToolPrefs(GetDefaultUserID()).Disabled["notion"]
	if len(saved) != 2 {
		t.Fatalf("saved = %v, want 2 disabled tools", saved)
	}
	// Stored sorted so the file does not churn on every save.
	if saved[0] != "archive_page" || saved[1] != "delete_page" {
		t.Errorf("saved = %v, want it sorted", saved)
	}

	// Re-enabling everything must drop the entry rather than leave an empty list.
	if rec, _ := doJSON(t, router, "PUT", "/api/connections/notion/tools", `{"disabled":[]}`); rec.Code != http.StatusOK {
		t.Fatalf("clear failed: %s", rec.Body.String())
	}
	if _, ok := loadToolPrefs(GetDefaultUserID()).Disabled["notion"]; ok {
		t.Error("clearing every switch must remove the server entry entirely")
	}
}

func TestSetConnectionToolsLeavesOtherConnectionsAlone(t *testing.T) {
	withTempHome(t)
	api, router := newTestAPI(t)
	router.HandleFunc("/api/connections/{id}/tools", api.handleSetConnectionTools).Methods("PUT")
	_ = api

	if rec, _ := doJSON(t, router, "PUT", "/api/connections/notion/tools", `{"disabled":["delete_page"]}`); rec.Code != http.StatusOK {
		t.Fatalf("first save failed: %s", rec.Body.String())
	}
	if rec, _ := doJSON(t, router, "PUT", "/api/connections/github/tools", `{"disabled":["create_issue"]}`); rec.Code != http.StatusOK {
		t.Fatalf("second save failed: %s", rec.Body.String())
	}

	prefs := loadToolPrefs(GetDefaultUserID())
	if len(prefs.Disabled["notion"]) != 1 {
		t.Errorf("notion switches were lost: %v", prefs.Disabled)
	}
	if len(prefs.Disabled["github"]) != 1 {
		t.Errorf("github switches were lost: %v", prefs.Disabled)
	}
}

func TestSetConnectionToolsRejectsBadBody(t *testing.T) {
	withTempHome(t)
	api, router := newTestAPI(t)
	router.HandleFunc("/api/connections/{id}/tools", api.handleSetConnectionTools).Methods("PUT")
	_ = api

	rec, body := doJSON(t, router, "PUT", "/api/connections/notion/tools", `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj == nil || errObj["title"] == "" {
		t.Error("a bad body must still produce a friendly error")
	}
}

func TestGetConnectionToolsMarksDisabledOnesAndGroups(t *testing.T) {
	withTempHome(t)
	api, router := newTestAPI(t)
	router.HandleFunc("/api/connections/{id}/tools", api.handleGetConnectionTools).Methods("GET")

	// Seed the grouping cache so the handler does not open a live connection.
	toolGroupCacheMu.Lock()
	toolGroupCache["notion"] = []ConnectionTool{
		{Name: "search", Description: "Search pages", ReadOnly: true, Source: "annotation"},
		{Name: "delete_page", Description: "Delete a page", ReadOnly: false, Source: "annotation"},
	}
	toolGroupCacheMu.Unlock()
	t.Cleanup(func() { forgetServerTools("notion") })

	if err := saveToolPrefs(GetDefaultUserID(), &toolPrefs{
		Disabled: map[string][]string{"notion": {"delete_page"}},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	rec, _ := doJSON(t, router, "GET", "/api/connections/notion/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
	}

	var resp connectionToolsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || resp.EnabledCount != 1 {
		t.Errorf("total=%d enabled=%d, want 2 and 1", resp.Total, resp.EnabledCount)
	}
	if !resp.GroupsFromAnnotations {
		t.Error("every tool declared an annotation, so grouping must be reported as authoritative")
	}

	byName := map[string]ConnectionTool{}
	for _, tool := range resp.Tools {
		byName[tool.Name] = tool
	}
	if !byName["search"].Enabled {
		t.Error("search must be enabled")
	}
	if byName["delete_page"].Enabled {
		t.Error("delete_page must be reported as disabled")
	}
	if !byName["search"].ReadOnly || byName["delete_page"].ReadOnly {
		t.Error("groups must follow the server's annotations")
	}
}

func TestGetConnectionToolsFlagsInferredGrouping(t *testing.T) {
	withTempHome(t)
	api, router := newTestAPI(t)
	router.HandleFunc("/api/connections/{id}/tools", api.handleGetConnectionTools).Methods("GET")

	toolGroupCacheMu.Lock()
	toolGroupCache["notion"] = []ConnectionTool{
		{Name: "list_pages", ReadOnly: true, Source: "inferred"},
	}
	toolGroupCacheMu.Unlock()
	t.Cleanup(func() { forgetServerTools("notion") })

	rec, _ := doJSON(t, router, "GET", "/api/connections/notion/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp connectionToolsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// A guessed grouping must never be presented as if the server declared it.
	if resp.GroupsFromAnnotations {
		t.Error("inferred grouping must not be reported as authoritative")
	}
}

func TestForgetServerToolsClearsCache(t *testing.T) {
	toolGroupCacheMu.Lock()
	toolGroupCache["notion"] = []ConnectionTool{{Name: "search"}}
	toolGroupCacheMu.Unlock()

	forgetServerTools("notion")

	toolGroupCacheMu.RLock()
	_, ok := toolGroupCache["notion"]
	toolGroupCacheMu.RUnlock()
	if ok {
		// A disconnect or removal must not leave a stale tool list behind.
		t.Error("cached tools must be dropped")
	}
}

func TestInferReadOnly(t *testing.T) {
	readOnly := []string{
		"get_email", "list-labels", "search_threads", "notion-fetch",
		"read_page", "downloadAttachment", "view", "query_database",
	}
	for _, name := range readOnly {
		if !inferReadOnly(name) {
			t.Errorf("inferReadOnly(%q) = false, want true", name)
		}
	}

	write := []string{
		"create_page", "delete-message", "update_row", "send_email",
		"notion-create-pages", "archive_thread", "move_file", "trash_message",
	}
	for _, name := range write {
		if inferReadOnly(name) {
			t.Errorf("inferReadOnly(%q) = true, want false", name)
		}
	}

	// A write verb anywhere in the name wins, so a mutating tool is never
	// filed under read-only just because it also mentions "search".
	if inferReadOnly("update_search_index") {
		t.Error("update_search_index must be treated as a write tool")
	}

	// Unrecognised names fall to the safer side.
	if inferReadOnly("frobnicate") {
		t.Error("an unknown verb must be treated as a write tool")
	}
}

func TestDisplayTitle(t *testing.T) {
	// A server that supplies its own title always wins.
	if got := displayTitle("gmail", "get_email_message", "Get email message"); got != "Get email message" {
		t.Errorf("got %q, want the server's own title", got)
	}

	tests := []struct {
		server string
		tool   string
		want   string
	}{
		// The server name is redundant on its own page, so it is dropped.
		{"notion", "notion-create-pages", "Create Pages"},
		{"notion", "notion-search", "Search"},
		{"gmail", "get_email_message", "Get Email Message"},
		{"gmail", "listDraftEmails", "List Draft Emails"},
		// A name that merely starts with similar letters is not a prefix match.
		{"notion", "notionalvalue_get", "Notionalvalue Get"},
		// Nothing left after dropping the prefix means the prefix is kept.
		{"notion", "notion", "Notion"},
	}

	for _, tc := range tests {
		if got := displayTitle(tc.server, tc.tool, ""); got != tc.want {
			t.Errorf("displayTitle(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}

func TestSplitToolName(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"get_email_message", []string{"get", "email", "message"}},
		{"notion-create-pages", []string{"notion", "create", "pages"}},
		// camelCase is common in MCP servers and must split too.
		{"downloadAttachment", []string{"download", "attachment"}},
		{"listDraftEmails", []string{"list", "draft", "emails"}},
		// A run of capitals stays one word until a lowercase resumes.
		{"HTTPRequest", []string{"http", "request"}},
		{"search", []string{"search"}},
	}

	for _, tc := range tests {
		got := splitToolName(tc.name)
		if len(got) != len(tc.want) {
			t.Errorf("splitToolName(%q) = %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("splitToolName(%q) = %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}
