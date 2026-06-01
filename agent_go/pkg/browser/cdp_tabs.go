package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

var (
	sharedCDPLocks sync.Map // map[string]*sync.Mutex

	cdpTabSelectionsMu sync.RWMutex
	cdpTabSelections   = make(map[string]string)
	cdpActiveTabs      = make(map[int]string)
	cdpTabAliases      = make(map[string]string)
)

func sharedCDPSessionName(port int) string {
	return fmt.Sprintf("shared-cdp-%d", port)
}

func sharedCDPLock(port int) *sync.Mutex {
	key := fmt.Sprintf("cdp:%d", port)
	actual, _ := sharedCDPLocks.LoadOrStore(key, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func acquireSharedCDPLock(ctx context.Context, port int) (func(), error) {
	local := sharedCDPLock(port)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for !local.TryLock() {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for local CDP lock on port %d: %w", port, ctx.Err())
		case <-ticker.C:
		}
	}

	lockPath := sharedCDPFileLockPath(port)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		local.Unlock()
		return nil, fmt.Errorf("open shared CDP lock %s: %w", lockPath, err)
	}

	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			lockFile.Close()
			local.Unlock()
			return nil, fmt.Errorf("acquire shared CDP lock %s: %w", lockPath, err)
		}
		select {
		case <-ctx.Done():
			lockFile.Close()
			local.Unlock()
			return nil, fmt.Errorf("timed out waiting for shared CDP lock on port %d: %w", port, ctx.Err())
		case <-ticker.C:
		}
	}

	return func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		local.Unlock()
	}, nil
}

func sharedCDPFileLockPath(port int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("mcp-agent-builder-cdp-%d.lock", port))
}

func cdpTabSelectionKey(port int, ownerID string) string {
	return fmt.Sprintf("cdp:%d:%s", port, strings.TrimSpace(ownerID))
}

func cdpTabAliasKey(port int, ownerID, alias string) string {
	return fmt.Sprintf("cdp:%d:%s:%s", port, strings.TrimSpace(ownerID), strings.TrimSpace(alias))
}

func getCDPTabSelection(port int, ownerID string) string {
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	return cdpTabSelections[cdpTabSelectionKey(port, ownerID)]
}

func setCDPTabSelection(port int, ownerID, tab string) {
	tab = strings.TrimSpace(tab)
	if tab == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpTabSelections[cdpTabSelectionKey(port, ownerID)] = tab
}

func clearCDPTabSelection(port int, ownerID string) {
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	delete(cdpTabSelections, cdpTabSelectionKey(port, ownerID))
}

func clearCDPTabSelectionsForPort(port int) {
	prefix := fmt.Sprintf("cdp:%d:", port)
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	for key := range cdpTabSelections {
		if strings.HasPrefix(key, prefix) {
			delete(cdpTabSelections, key)
		}
	}
	for key := range cdpTabAliases {
		if strings.HasPrefix(key, prefix) {
			delete(cdpTabAliases, key)
		}
	}
	delete(cdpActiveTabs, port)
}

func clearCDPActiveTabForPort(port int) {
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	delete(cdpActiveTabs, port)
}

func getCDPTabAlias(port int, ownerID, alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" || isCDPTabID(alias) {
		return ""
	}
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	return cdpTabAliases[cdpTabAliasKey(port, ownerID, alias)]
}

func getCDPTabSelectionForPrompt(port int, ownerID string) string {
	tab := getCDPTabSelection(port, ownerID)
	if alias := getCDPTabAlias(port, ownerID, tab); alias != "" {
		return alias
	}
	return tab
}

func setCDPTabAlias(port int, ownerID, alias, tabID string) {
	alias = strings.TrimSpace(alias)
	tabID = strings.TrimSpace(tabID)
	if alias == "" || tabID == "" || alias == tabID || isCDPTabID(alias) {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpTabAliases[cdpTabAliasKey(port, ownerID, alias)] = tabID
}

func clearCDPTabAlias(port int, ownerID, alias string) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	delete(cdpTabAliases, cdpTabAliasKey(port, ownerID, alias))
}

func getCDPActiveTab(port int) string {
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	return cdpActiveTabs[port]
}

func setCDPActiveTab(port int, tab string) {
	tab = strings.TrimSpace(tab)
	if tab == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpActiveTabs[port] = tab
}

func clearCDPActiveTab(port int, tab string) {
	tab = strings.TrimSpace(tab)
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	if tab == "" || cdpActiveTabs[port] == tab {
		delete(cdpActiveTabs, port)
	}
}

func cdpOwnerID(workflowSessionID, agentSessionID, session string) string {
	for _, candidate := range []string{agentSessionID, workflowSessionID} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if resolved := common.ResolveBrowserSessionID(candidate, "default"); resolved != "" && resolved != "default" {
			return resolved
		}
	}
	for _, candidate := range []string{workflowSessionID, agentSessionID, session} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return "default"
}

func parseTabSelection(args []string) (tab string, clear bool, err error) {
	if len(args) == 0 {
		return "", false, nil
	}

	switch args[0] {
	case "list", "ls":
		return "", false, nil
	case "new":
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "--label" {
				label := strings.TrimSpace(args[i+1])
				if label == "" {
					return "", false, fmt.Errorf("CDP shared-browser tab creation requires a non-empty --label")
				}
				return label, false, nil
			}
		}
		return "", false, fmt.Errorf("CDP shared-browser tab creation requires --label <label> so later commands can target it")
	case "close":
		if len(args) > 1 {
			return strings.TrimSpace(args[1]), true, nil
		}
		return "", false, nil
	default:
		if strings.HasPrefix(args[0], "-") {
			return "", false, nil
		}
		return strings.TrimSpace(args[0]), false, nil
	}
}

func isTabListRequest(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return len(args) == 1 && (args[0] == "list" || args[0] == "ls")
}

func selectedCDPTabMessage(port int, ownerID string) string {
	if tab := getCDPTabSelectionForPrompt(port, ownerID); tab != "" {
		return fmt.Sprintf("Selected CDP tab: %s\nUse it inline on page actions, for example: args=[\"tab\", %q, \"-i\"] for snapshot.", tab, tab)
	}
	return "No selected CDP tab for this workflow yet. Create a stable labeled tab instead of listing every browser tab, for example: agent_browser(command=\"tab\", args=[\"new\", \"--label\", \"<workflow-label>\", \"https://target.example\"], session=\"<session>\"). Then use that label or returned tab id inline on page actions."
}

func fallbackCDPTabListMessage(port int, ownerID string, err error) string {
	message := "Could not refresh the real CDP tab list within the short timeout; falling back to remembered workflow tab state."
	if err != nil {
		message += " Last tab-list error: " + truncatePromptField(oneLine(err.Error()), 240)
	}
	return message + "\n\n" + selectedCDPTabMessage(port, ownerID)
}

type cdpTabListOutput struct {
	Data struct {
		Tabs []cdpTabInfo `json:"tabs"`
	} `json:"data"`
}

type cdpTabInfo struct {
	Active bool   `json:"active"`
	Label  string `json:"label"`
	TabID  string `json:"tabId"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

func isCDPTabID(tab string) bool {
	tab = strings.TrimSpace(tab)
	if len(tab) < 2 || tab[0] != 't' {
		return false
	}
	_, err := strconv.Atoi(tab[1:])
	return err == nil
}

func findCDPTabID(output, tab string) string {
	tab = strings.TrimSpace(tab)
	if tab == "" {
		return ""
	}
	if isCDPTabID(tab) {
		return tab
	}
	var parsed cdpTabListOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return ""
	}
	for _, candidate := range parsed.Data.Tabs {
		if strings.TrimSpace(candidate.Label) == tab || strings.TrimSpace(candidate.TabID) == tab {
			return strings.TrimSpace(candidate.TabID)
		}
	}
	return ""
}

func formatCDPTabListForPrompt(output string) string {
	raw := strings.TrimSpace(output)
	if raw == "" {
		return "(no tabs returned)"
	}

	var parsed cdpTabListOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return truncatePromptField(raw, 2000)
	}
	if len(parsed.Data.Tabs) == 0 {
		return "(no tabs found)"
	}

	const maxTabs = 20
	lines := make([]string, 0, min(len(parsed.Data.Tabs), maxTabs)+1)
	for i, tab := range parsed.Data.Tabs {
		if i >= maxTabs {
			lines = append(lines, fmt.Sprintf("... %d more tab(s) omitted", len(parsed.Data.Tabs)-maxTabs))
			break
		}

		tabID := oneLine(tab.TabID)
		if tabID == "" {
			tabID = fmt.Sprintf("tab-%d", i+1)
		}

		status := ""
		if tab.Active {
			status = " active"
		}

		line := fmt.Sprintf("- %s%s", tabID, status)
		if label := truncatePromptField(oneLine(tab.Label), 50); label != "" {
			line += fmt.Sprintf(" label=%q", label)
		}
		if title := truncatePromptField(oneLine(tab.Title), 90); title != "" {
			line += fmt.Sprintf(" title=%q", title)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncatePromptField(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 1 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "..."
}

func stripRedundantTabCommandArg(command string, args []string) []string {
	if command != "tab" || len(args) == 0 {
		return args
	}
	cleaned := append([]string(nil), args...)
	for len(cleaned) > 1 && cleaned[0] == "tab" {
		cleaned = cleaned[1:]
	}
	return cleaned
}

func normalizeAgentBrowserCommandArgs(command string, args []string) []string {
	cleaned := stripRedundantCommandArg(command, args)
	if command == "wait" {
		cleaned = normalizeWaitDurationArgs(cleaned)
	}
	return cleaned
}

func normalizeOpenCommandArgs(command string, args []string) (tab string, cleaned []string, ok bool, err error) {
	cleaned = stripRedundantCommandArg(command, args)
	tab, cleaned, ok, err = stripInlineTabFromOpenArgs(cleaned)
	if err != nil || ok {
		return tab, cleaned, ok, err
	}
	return "", cleaned, false, nil
}

func stripRedundantCommandArg(command string, args []string) []string {
	command = strings.TrimSpace(command)
	if command == "" || len(args) <= 1 {
		return args
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), command) {
		return append([]string(nil), args[1:]...)
	}
	return args
}

func normalizeWaitDurationArgs(args []string) []string {
	if len(args) != 1 {
		return args
	}
	raw := strings.TrimSpace(args[0])
	if raw == "" {
		return args
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return args
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return args
	}
	return []string{fmt.Sprintf("%d", d.Milliseconds())}
}

func extractInlineCDPTab(args []string) (tab string, cleaned []string, err error) {
	cleaned = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "tab", "--tab":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", nil, fmt.Errorf("CDP shared-browser command requires %s <tab-id-or-label>", args[i])
			}
			if tab != "" {
				return "", nil, fmt.Errorf("CDP shared-browser command includes multiple tab selections; include exactly one")
			}
			tab = strings.TrimSpace(args[i+1])
			i++
		default:
			cleaned = append(cleaned, args[i])
		}
	}
	return tab, cleaned, nil
}

func stripInlineTabFromOpenArgs(args []string) (tab string, cleaned []string, ok bool, err error) {
	if len(args) == 0 {
		return "", args, false, nil
	}
	if args[0] != "tab" && args[0] != "--tab" {
		return "", args, false, nil
	}
	if len(args) < 3 || strings.TrimSpace(args[1]) == "" {
		return "", nil, false, fmt.Errorf("open command uses %s but is missing <tab-id-or-label> and URL", args[0])
	}
	return strings.TrimSpace(args[1]), append([]string(nil), args[2:]...), true, nil
}

func stringArgs(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, arg := range v {
			if s, ok := arg.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return nil
	}
}

func stripCDPArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--cdp" {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}
