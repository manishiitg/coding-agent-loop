package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/mcpagent/mcpclient"
)

// Per-connection tool permissions. A connection brings a whole toolset with it;
// this lets a user keep the connection while switching individual tools off, so
// agents never see the ones they turned off.
//
// Only the OFF switches are stored. Anything absent is enabled, which makes
// "everything on" the default and keeps newly added tools working without the
// user having to revisit this screen.

// toolPrefs is the on-disk shape: server name -> disabled tool names.
type toolPrefs struct {
	Disabled map[string][]string `json:"disabled"`
}

var toolPrefsMu sync.Mutex

func toolPrefsPath(userID string) string {
	return expandPath(fmt.Sprintf("~/.config/mcpagent/tool-prefs/%s.json", userID))
}

func loadToolPrefs(userID string) *toolPrefs {
	prefs := &toolPrefs{Disabled: map[string][]string{}}

	data, err := os.ReadFile(toolPrefsPath(userID))
	if err != nil {
		return prefs // no preferences saved yet: everything is enabled
	}
	if err := json.Unmarshal(data, prefs); err != nil {
		return &toolPrefs{Disabled: map[string][]string{}}
	}
	if prefs.Disabled == nil {
		prefs.Disabled = map[string][]string{}
	}
	return prefs
}

func saveToolPrefs(userID string, prefs *toolPrefs) error {
	path := toolPrefsPath(userID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create tool preferences directory: %w", err)
	}

	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// disabledToolSet returns the disabled tools for one server as a lookup.
func disabledToolSet(userID, serverName string) map[string]bool {
	set := map[string]bool{}
	for _, name := range loadToolPrefs(userID).Disabled[serverName] {
		set[name] = true
	}
	return set
}

// ConnectionTool is one tool of a connection, with its on/off state and which
// group it belongs to.
type ConnectionTool struct {
	Name string `json:"name"`
	// Title is what the row shows. The raw name stays available so a tool can
	// still be identified exactly.
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	// ReadOnly separates tools that only observe from ones that change or
	// delete things, so a user can act on the risky half in one move.
	ReadOnly bool `json:"read_only"`
	// Source is "annotation" when the server declared readOnlyHint itself, and
	// "inferred" when it did not and the name had to be read instead. The UI
	// says so rather than presenting a guess as fact.
	Source string `json:"source"`
}

type connectionToolsResponse struct {
	ServerName   string           `json:"server_name"`
	Tools        []ConnectionTool `json:"tools"`
	Total        int              `json:"total"`
	EnabledCount int              `json:"enabled_count"`
	// True when every tool's group came from the server's own annotations.
	GroupsFromAnnotations bool `json:"groups_from_annotations"`
}

// readOnlyVerbs and writeVerbs back the fallback used only when a server omits
// MCP tool annotations. Checked as name prefixes and separated words, never as
// bare substrings, so "update_search_index" is not read as read-only.
var readOnlyVerbs = []string{
	"get", "list", "search", "read", "fetch", "view", "query", "find",
	"describe", "show", "download", "export", "check", "count",
}

var writeVerbs = []string{
	"create", "update", "delete", "remove", "write", "append", "insert",
	"archive", "move", "send", "post", "patch", "put", "set", "add",
	"upload", "rename", "duplicate", "merge", "close", "trash", "restore",
}

// inferReadOnly guesses a tool's group from its name. Used only when the server
// declares no annotations. Unknown names are treated as write tools: putting a
// mutating tool in the read-only group is the more damaging mistake.
func inferReadOnly(name string) bool {
	words := splitToolName(name)
	for _, w := range words {
		for _, v := range writeVerbs {
			if w == v {
				return false
			}
		}
	}
	for _, w := range words {
		for _, v := range readOnlyVerbs {
			if w == v {
				return true
			}
		}
	}
	return false
}

// splitToolName breaks a tool name into lowercase words. Separators and
// camelCase boundaries both count, since MCP servers use snake_case,
// kebab-case and camelCase interchangeably ("downloadAttachment" has to yield
// "download" for the fallback to see it at all).
func splitToolName(name string) []string {
	var words []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			words = append(words, strings.ToLower(current.String()))
			current.Reset()
		}
	}

	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ' || r == ':' || r == '/':
			flush()
		case r >= 'A' && r <= 'Z':
			// A capital starts a new word, except inside a run of capitals
			// ("HTTPRequest" -> "http", "request").
			if i > 0 {
				prev := runes[i-1]
				nextIsLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') || nextIsLower {
					flush()
				}
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return words
}

// displayTitle produces the label shown next to a tool.
//
// The server's own title annotation wins when it sets one. Most do not, so the
// fallback rewrites the raw name into something readable: the server's own name
// is dropped as a prefix, since the whole screen is already about that one
// connection, and the rest becomes a sentence.
func displayTitle(serverName, toolName, annotationTitle string) string {
	if annotationTitle != "" {
		return annotationTitle
	}

	words := splitToolName(toolName)
	serverWords := splitToolName(serverName)

	// Drop a leading repeat of the server name ("notion-create-pages").
	for len(serverWords) > 0 && len(words) > len(serverWords) {
		match := true
		for i, sw := range serverWords {
			if words[i] != sw {
				match = false
				break
			}
		}
		if !match {
			break
		}
		words = words[len(serverWords):]
		break
	}

	if len(words) == 0 {
		return toolName
	}

	// Title case every word, so the label reads as a name rather than a
	// sentence fragment: "notion-create-pages" -> "Create Pages".
	titled := make([]string, 0, len(words))
	for _, w := range words {
		r := []rune(w)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		titled = append(titled, string(r))
	}
	return strings.Join(titled, " ")
}

type setConnectionToolsRequest struct {
	// Only the OFF switches travel; everything omitted is enabled.
	Disabled []string `json:"disabled"`
}

// toolGroupCache remembers each server's tool grouping. Grouping needs the MCP
// annotations, which the shared discovery cache does not carry, so it comes
// from a live ListTools; caching it keeps reopening this screen cheap.
var (
	toolGroupCache   = map[string][]ConnectionTool{}
	toolGroupCacheMu sync.RWMutex
)

// serverToolDetails returns a server's tools along with the group each belongs
// to.
//
// This connects and calls ListTools rather than reading the shared discovery
// cache, because that cache stores mcpclient.ToolDetail, which drops the MCP
// readOnlyHint/destructiveHint annotations. Those annotations are what make the
// read-only/write split trustworthy instead of a guess about names.
func (api *StreamingAPI) serverToolDetails(ctx context.Context, serverName string) ([]ConnectionTool, error) {
	toolGroupCacheMu.RLock()
	cached, ok := toolGroupCache[serverName]
	toolGroupCacheMu.RUnlock()
	if ok {
		return cached, nil
	}

	config, err := mcpclient.LoadMergedConfig(api.mcpConfigPath, api.logger)
	if err != nil {
		return nil, err
	}
	serverConfig, err := config.GetServer(serverName)
	if err != nil {
		return nil, fmt.Errorf("server not found: %s", serverName)
	}
	if serverConfig.OAuth != nil {
		serverConfig.OAuth.TokenFile = getUserTokenFilePath(GetUserIDFromContext(ctx), serverName)
	}

	client := mcpclient.New(serverConfig, api.logger)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	defer client.Close()

	listed, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	tools := make([]ConnectionTool, 0, len(listed))
	for _, t := range listed {
		tool := ConnectionTool{
			Name:        t.Name,
			Title:       displayTitle(serverName, t.Name, t.Annotations.Title),
			Description: t.Description,
		}
		if hint := t.Annotations.ReadOnlyHint; hint != nil {
			tool.ReadOnly = *hint
			tool.Source = "annotation"
		} else {
			tool.ReadOnly = inferReadOnly(t.Name)
			tool.Source = "inferred"
		}
		tools = append(tools, tool)
	}

	toolGroupCacheMu.Lock()
	toolGroupCache[serverName] = tools
	toolGroupCacheMu.Unlock()

	return tools, nil
}

// forgetServerTools drops a server's cached grouping, so a reconnect or a
// removal cannot leave a stale tool list behind.
func forgetServerTools(serverName string) {
	toolGroupCacheMu.Lock()
	delete(toolGroupCache, serverName)
	toolGroupCacheMu.Unlock()
}

// handleGetConnectionTools handles GET /api/connections/{id}/tools
func (api *StreamingAPI) handleGetConnectionTools(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	serverName := api.resolveServerName(id)
	displayName := serverName
	if entry, err := api.findCatalogEntry(id); err == nil {
		displayName = entry.Name
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	tools, err := api.serverToolDetails(ctx, serverName)
	if err != nil {
		writeFriendlyError(w, http.StatusBadGateway, friendlyError(displayName, 0, err.Error()))
		return
	}

	disabled := disabledToolSet(GetUserIDFromContext(r.Context()), serverName)

	resp := connectionToolsResponse{
		ServerName:            serverName,
		Tools:                 []ConnectionTool{},
		GroupsFromAnnotations: true,
	}
	for _, t := range tools {
		if t.Source != "annotation" {
			resp.GroupsFromAnnotations = false
		}
		t.Enabled = !disabled[t.Name]
		if t.Enabled {
			resp.EnabledCount++
		}
		resp.Tools = append(resp.Tools, t)
	}
	sort.Slice(resp.Tools, func(i, j int) bool { return resp.Tools[i].Name < resp.Tools[j].Name })
	resp.Total = len(resp.Tools)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSetConnectionTools handles PUT /api/connections/{id}/tools
func (api *StreamingAPI) handleSetConnectionTools(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	serverName := api.resolveServerName(id)
	userID := GetUserIDFromContext(r.Context())

	var req setConnectionToolsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFriendlyError(w, http.StatusBadRequest, &FriendlyError{
			Code:    "bad_request",
			Title:   "Could not save tool settings",
			Message: "The request could not be read. Try again.",
			Action:  "retry",
			Raw:     err.Error(),
		})
		return
	}

	// Read-modify-write under a lock: two connections' settings live in one file.
	toolPrefsMu.Lock()
	prefs := loadToolPrefs(userID)
	if len(req.Disabled) == 0 {
		delete(prefs.Disabled, serverName)
	} else {
		disabled := append([]string(nil), req.Disabled...)
		sort.Strings(disabled)
		prefs.Disabled[serverName] = disabled
	}
	err := saveToolPrefs(userID, prefs)
	toolPrefsMu.Unlock()

	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to save tool preferences for %s: %v", serverName, err), err)
		writeFriendlyError(w, http.StatusInternalServerError, friendlyError(serverName, http.StatusInternalServerError, err.Error()))
		return
	}

	api.logger.Info(fmt.Sprintf("Tool preferences saved for %s: %d disabled", serverName, len(req.Disabled)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":       "saved",
		"server_name":  serverName,
		"disabled":     req.Disabled,
		"disabled_qty": len(req.Disabled),
	})
}

// applyDisabledTools folds a user's per-connection tool switches into the
// selection an agent runs with.
//
// The agent's filter treats an empty selection as "no filtering", so switching
// one tool off has to be expressed as an explicit allow-list. A server whose
// tools cannot be enumerated is passed through as "server:*" rather than being
// dropped — losing a whole connection would be far worse than honouring one
// stale switch.
func (api *StreamingAPI) applyDisabledTools(userID string, selectedServers, selectedTools []string) []string {
	prefs := loadToolPrefs(userID)
	if len(prefs.Disabled) == 0 {
		return selectedTools
	}

	disabledFor := func(server string) map[string]bool {
		set := map[string]bool{}
		for _, name := range prefs.Disabled[server] {
			set[name] = true
		}
		return set
	}

	// An explicit selection is already an allow-list: just subtract.
	if len(selectedTools) > 0 {
		kept := make([]string, 0, len(selectedTools))
		for _, entry := range selectedTools {
			server, tool, found := splitToolEntry(entry)
			if !found || tool == "*" {
				kept = append(kept, entry)
				continue
			}
			if !disabledFor(server)[tool] {
				kept = append(kept, entry)
			}
		}
		return kept
	}

	// No selection means every tool. Expand only the servers that actually have
	// something switched off; the rest stay wide open via "server:*".
	servers := selectedServers
	if len(servers) == 0 {
		api.toolStatusMux.RLock()
		for name := range api.toolStatus {
			servers = append(servers, name)
		}
		api.toolStatusMux.RUnlock()
	}
	if len(servers) == 0 {
		return selectedTools // nothing known to expand; leave filtering off
	}
	sort.Strings(servers)

	expanded := make([]string, 0, len(servers))
	for _, server := range servers {
		off := disabledFor(server)
		if len(off) == 0 {
			expanded = append(expanded, server+":*")
			continue
		}

		api.toolStatusMux.RLock()
		status, ok := api.toolStatus[server]
		api.toolStatusMux.RUnlock()
		if !ok || len(status.Tools) == 0 {
			// Cannot enumerate, so cannot subtract safely.
			expanded = append(expanded, server+":*")
			continue
		}

		for _, t := range status.Tools {
			if !off[t.Name] {
				expanded = append(expanded, server+":"+t.Name)
			}
		}
	}
	return expanded
}

// splitToolEntry splits a "server:tool" selection entry. Server names can
// contain spaces but not colons, so the first colon is the separator.
func splitToolEntry(entry string) (server, tool string, ok bool) {
	for i := 0; i < len(entry); i++ {
		if entry[i] == ':' {
			return entry[:i], entry[i+1:], true
		}
	}
	return entry, "", false
}
