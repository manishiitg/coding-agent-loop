package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

var (
	sharedCDPLocks sync.Map // map[string]*sync.Mutex

	cdpTabSelectionsMu sync.RWMutex
	cdpTabSelections   = make(map[string]string)
	cdpActiveTabs      = make(map[int]string)
	cdpTabAliases      = make(map[string]string)
	cdpOwnedTabs       = make(map[string]cdpOwnedTab)
	cdpRecordingTabs   = make(map[string]cdpRecordingHandoff)
)

type cdpOwnedTab struct {
	Alias string
	TabID string
}

// cdpRecordingHandoff tracks agent-browser's temporary recording context.
// agent-browser cannot add video capture to an existing BrowserContext, so
// `record start` creates a new page (and usually a visible Chrome window). The
// managed adapter must route the owner to that page until recording stops;
// otherwise its normal selected-tab enforcement silently sends actions back to
// the unrecorded original tab.
type cdpRecordingHandoff struct {
	OriginalTab   string
	RecordingTab  string
	NeedsSnapshot bool
}

type newCDPTabRequest struct {
	Label string
	URL   string
}

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
	for key := range cdpOwnedTabs {
		if strings.HasPrefix(key, prefix) {
			delete(cdpOwnedTabs, key)
		}
	}
	for key := range cdpRecordingTabs {
		if strings.HasPrefix(key, prefix) {
			delete(cdpRecordingTabs, key)
		}
	}
	delete(cdpActiveTabs, port)
}

func getCDPRecordingHandoff(port int, ownerID string) (cdpRecordingHandoff, bool) {
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	handoff, ok := cdpRecordingTabs[cdpTabSelectionKey(port, ownerID)]
	return handoff, ok
}

func setCDPRecordingHandoff(port int, ownerID string, handoff cdpRecordingHandoff) {
	handoff.OriginalTab = strings.TrimSpace(handoff.OriginalTab)
	handoff.RecordingTab = strings.TrimSpace(handoff.RecordingTab)
	if handoff.OriginalTab == "" || handoff.RecordingTab == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpRecordingTabs[cdpTabSelectionKey(port, ownerID)] = handoff
	cdpTabSelections[cdpTabSelectionKey(port, ownerID)] = handoff.RecordingTab
	cdpActiveTabs[port] = handoff.RecordingTab
}

func markCDPRecordingSnapshotReady(port int, ownerID string) {
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	key := cdpTabSelectionKey(port, ownerID)
	handoff, ok := cdpRecordingTabs[key]
	if !ok {
		return
	}
	handoff.NeedsSnapshot = false
	cdpRecordingTabs[key] = handoff
}

func clearCDPRecordingHandoff(port int, ownerID string) (cdpRecordingHandoff, bool) {
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	key := cdpTabSelectionKey(port, ownerID)
	handoff, ok := cdpRecordingTabs[key]
	if !ok {
		return cdpRecordingHandoff{}, false
	}
	delete(cdpRecordingTabs, key)
	if handoff.OriginalTab != "" {
		cdpTabSelections[key] = handoff.OriginalTab
	}
	if cdpActiveTabs[port] == handoff.RecordingTab {
		delete(cdpActiveTabs, port)
	}
	return handoff, true
}

// findCDPRecordingTab identifies the fresh page created by agent-browser's
// record start/restart operation. Prefer a newly-created active page, then the
// only new page, then an active page different from the original. The final
// fallback supports agent-browser versions that replace a target while
// retaining their logical tN numbering.
func findCDPRecordingTab(before, after []cdpTabInfo, originalTab string) (string, error) {
	beforeIDs := make(map[string]bool, len(before))
	for _, tab := range before {
		if tabID := strings.TrimSpace(tab.TabID); tabID != "" {
			beforeIDs[tabID] = true
		}
	}

	var newTabs []cdpTabInfo
	for _, tab := range after {
		tabID := strings.TrimSpace(tab.TabID)
		if tabID == "" || beforeIDs[tabID] {
			continue
		}
		newTabs = append(newTabs, tab)
		if tab.Active {
			return tabID, nil
		}
	}
	if len(newTabs) == 1 {
		return strings.TrimSpace(newTabs[0].TabID), nil
	}
	for _, tab := range after {
		tabID := strings.TrimSpace(tab.TabID)
		if tab.Active && tabID != "" && tabID != strings.TrimSpace(originalTab) {
			return tabID, nil
		}
	}
	return "", fmt.Errorf("record start succeeded, but the fresh recording tab could not be identified (original=%q, before=%d tab(s), after=%d tab(s))", originalTab, len(before), len(after))
}

func clearCDPActiveTabForPort(port int) {
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	delete(cdpActiveTabs, port)
}

// countCDPTabAliasesForOwner counts labeled tabs an owner has created in the
// shared CDP browser. Aliases are recorded when a labeled tab is selected or
// created, and removed when the tab is closed, so this approximates the
// owner's live tab count.
func countCDPTabAliasesForOwner(port int, ownerID string) int {
	prefix := fmt.Sprintf("cdp:%d:%s:", port, strings.TrimSpace(ownerID))
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	count := 0
	for key := range cdpTabAliases {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
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
	key := cdpTabAliasKey(port, ownerID, alias)
	delete(cdpTabAliases, key)
	delete(cdpOwnedTabs, key)
}

func markCDPTabOwned(port int, ownerID, alias, tabID string) {
	ownerID = strings.TrimSpace(ownerID)
	alias = strings.TrimSpace(alias)
	tabID = strings.TrimSpace(tabID)
	if ownerID == "" || alias == "" {
		return
	}
	if tabID == "" {
		tabID = alias
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpOwnedTabs[cdpTabAliasKey(port, ownerID, alias)] = cdpOwnedTab{Alias: alias, TabID: tabID}
}

func ownedCDPTabsForOwner(port int, ownerID string) []cdpOwnedTab {
	prefix := fmt.Sprintf("cdp:%d:%s:", port, strings.TrimSpace(ownerID))
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	tabs := make([]cdpOwnedTab, 0)
	for key, tab := range cdpOwnedTabs {
		if strings.HasPrefix(key, prefix) {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

func isCDPTabOwnedByOwner(port int, ownerID, alias, tabID string) bool {
	alias = strings.TrimSpace(alias)
	tabID = strings.TrimSpace(tabID)
	for _, owned := range ownedCDPTabsForOwner(port, ownerID) {
		if (alias != "" && owned.Alias == alias) || (tabID != "" && owned.TabID == tabID) {
			return true
		}
	}
	return false
}

// clearCDPTabStateForOwner removes selection, alias, active-tab, and ownership
// state for one tab. The caller may identify the tab by either its workflow
// label or the agent-browser tab ID returned when it was created.
func clearCDPTabStateForOwner(port int, ownerID, tab string) {
	ownerID = strings.TrimSpace(ownerID)
	tab = strings.TrimSpace(tab)
	if ownerID == "" || tab == "" {
		return
	}
	prefix := fmt.Sprintf("cdp:%d:%s:", port, ownerID)
	selectionKey := cdpTabSelectionKey(port, ownerID)

	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	selected := cdpTabSelections[selectionKey]
	for key, tabID := range cdpTabAliases {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		alias := strings.TrimPrefix(key, prefix)
		if alias != tab && tabID != tab {
			continue
		}
		delete(cdpTabAliases, key)
		delete(cdpOwnedTabs, key)
		if selected == alias || selected == tabID {
			delete(cdpTabSelections, selectionKey)
		}
		if cdpActiveTabs[port] == alias || cdpActiveTabs[port] == tabID {
			delete(cdpActiveTabs, port)
		}
	}
	// Recording contexts are registered for crash-safe delayed cleanup without
	// adding a public alias. Remove any such ownership record by its real tab ID
	// after record stop closes the temporary page.
	for key, owned := range cdpOwnedTabs {
		if strings.HasPrefix(key, prefix) && (owned.Alias == tab || owned.TabID == tab) {
			delete(cdpOwnedTabs, key)
		}
	}
	if selected == tab {
		delete(cdpTabSelections, selectionKey)
	}
	if cdpActiveTabs[port] == tab {
		delete(cdpActiveTabs, port)
	}
	// A tab ID can be used as the alias when agent-browser did not return a
	// separate label mapping. Remove that direct ownership record as well.
	delete(cdpOwnedTabs, cdpTabAliasKey(port, ownerID, tab))
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

func isCDPTabActive(port int, ownerID, tab string) bool {
	tab = strings.TrimSpace(tab)
	if tab == "" {
		return false
	}

	activeTab := getCDPActiveTab(port)
	if activeTab == tab {
		return true
	}

	// Tab selections are usually supplied by agents as stable labels, while
	// agent-browser reports the active tab as its resolved t<N> id.
	return activeTab != "" && activeTab == getCDPTabAlias(port, ownerID, tab)
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
		request, parseErr := parseNewCDPTabRequest(args)
		if parseErr != nil {
			return "", false, parseErr
		}
		return request.Label, false, nil
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

func parseNewCDPTabRequest(args []string) (newCDPTabRequest, error) {
	if len(args) == 0 || args[0] != "new" {
		return newCDPTabRequest{}, fmt.Errorf("tab creation must begin with new")
	}
	var request newCDPTabRequest
	for i := 1; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "--label":
			if i+1 >= len(args) {
				return newCDPTabRequest{}, fmt.Errorf("CDP shared-browser tab creation requires a value after --label")
			}
			i++
			request.Label = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "-"):
			return newCDPTabRequest{}, fmt.Errorf("unsupported tab new option %q", arg)
		case arg != "":
			if request.URL != "" {
				return newCDPTabRequest{}, fmt.Errorf("tab new accepts at most one URL")
			}
			request.URL = arg
		}
	}
	if request.Label == "" {
		return newCDPTabRequest{}, fmt.Errorf("CDP shared-browser tab creation requires --label <label> so later commands can target it")
	}
	if request.URL != "" {
		parsed, err := url.Parse(request.URL)
		if err != nil || strings.TrimSpace(parsed.Scheme) == "" {
			return newCDPTabRequest{}, fmt.Errorf("tab new target %q must be an absolute URL with an explicit scheme such as https://", request.URL)
		}
		if (parsed.Scheme == "http" || parsed.Scheme == "https") && strings.TrimSpace(parsed.Host) == "" {
			return newCDPTabRequest{}, fmt.Errorf("tab new target %q must include a host", request.URL)
		}
	}
	return request, nil
}

func canonicalNewCDPTabArgs(request newCDPTabRequest) []string {
	args := []string{"new", "--label", request.Label}
	if request.URL != "" {
		args = append(args, request.URL)
	}
	return args
}

func isTabListRequest(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return len(args) == 1 && (args[0] == "list" || args[0] == "ls")
}

func selectedCDPTabMessage(port int, ownerID string) string {
	cdpURL := resolveCdpURL(port)
	if tab := getCDPTabSelectionForPrompt(port, ownerID); tab != "" {
		return fmt.Sprintf("Selected CDP tab: %s\nUse the configured CDP endpoint and tab inline on page actions, for example: args=[\"--cdp\", %q, \"tab\", %q, \"-i\"] for snapshot.", tab, cdpURL, tab)
	}
	return fmt.Sprintf("No selected CDP tab for this workflow yet. List tabs and reuse a relevant real tN id first. If none matches, request one stable labeled tab with agent_browser(command=\"tab\", args=[\"--cdp\", %q, \"new\", \"--label\", \"<workflow-label>\", \"https://target.example\"], session=\"<session>\"). The backend performs an exact-URL reuse check before creating and returns the real tN id; use that id inline on later page actions.", cdpURL)
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
		Tabs   []cdpTabInfo `json:"tabs"`
		Active bool         `json:"active"`
		Label  string       `json:"label"`
		TabID  string       `json:"tabId"`
		Title  string       `json:"title"`
		URL    string       `json:"url"`
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
	if strings.TrimSpace(parsed.Data.Label) == tab || strings.TrimSpace(parsed.Data.TabID) == tab {
		return strings.TrimSpace(parsed.Data.TabID)
	}
	for _, candidate := range parsed.Data.Tabs {
		if strings.TrimSpace(candidate.Label) == tab || strings.TrimSpace(candidate.TabID) == tab {
			return strings.TrimSpace(candidate.TabID)
		}
	}
	return ""
}

func findCDPTabByRef(tabs []cdpTabInfo, ref string) (cdpTabInfo, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return cdpTabInfo{}, false
	}
	for _, tab := range tabs {
		if strings.TrimSpace(tab.TabID) == ref || strings.TrimSpace(tab.Label) == ref {
			return tab, true
		}
	}
	return cdpTabInfo{}, false
}

func parseCDPTabs(output string) ([]cdpTabInfo, error) {
	var parsed cdpTabListOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		return nil, err
	}
	tabs := append([]cdpTabInfo(nil), parsed.Data.Tabs...)
	if directID := strings.TrimSpace(parsed.Data.TabID); directID != "" {
		tabs = append(tabs, cdpTabInfo{
			Active: parsed.Data.Active,
			Label:  strings.TrimSpace(parsed.Data.Label),
			TabID:  directID,
			Title:  strings.TrimSpace(parsed.Data.Title),
			URL:    strings.TrimSpace(parsed.Data.URL),
		})
	}
	return tabs, nil
}

func normalizedCDPTabURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String()
}

func displayCDPTabURL(raw string) string {
	normalized := normalizedCDPTabURL(raw)
	parsed, err := url.Parse(normalized)
	if err != nil {
		return normalized
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	return parsed.String()
}

func urlsReferToSameCDPTab(left, right string) bool {
	left = normalizedCDPTabURL(left)
	right = normalizedCDPTabURL(right)
	return left != "" && left == right
}

func findReusableCDPTab(tabs []cdpTabInfo, port int, ownerID string, request newCDPTabRequest) (tab cdpTabInfo, navigate bool, collision *cdpTabInfo, ok bool) {
	for _, candidate := range tabs {
		if strings.TrimSpace(candidate.Label) != request.Label {
			continue
		}
		if urlsReferToSameCDPTab(candidate.URL, request.URL) || isCDPTabOwnedByOwner(port, ownerID, candidate.Label, candidate.TabID) {
			return candidate, request.URL != "" && !urlsReferToSameCDPTab(candidate.URL, request.URL), nil, true
		}
		copy := candidate
		collision = &copy
	}
	if request.URL != "" {
		for _, candidate := range tabs {
			if urlsReferToSameCDPTab(candidate.URL, request.URL) {
				return candidate, false, collision, true
			}
		}
	}
	return cdpTabInfo{}, false, collision, false
}

func findCreatedCDPTab(output, label string) (cdpTabInfo, bool) {
	tabs, err := parseCDPTabs(output)
	if err != nil {
		return cdpTabInfo{}, false
	}
	for _, tab := range tabs {
		if strings.TrimSpace(tab.Label) == strings.TrimSpace(label) {
			return tab, strings.TrimSpace(tab.TabID) != ""
		}
	}
	if len(tabs) == 1 && strings.TrimSpace(tabs[0].TabID) != "" {
		return tabs[0], true
	}
	return cdpTabInfo{}, false
}

func formatCDPTabIdentity(prefix string, tab cdpTabInfo) string {
	parts := []string{strings.TrimSpace(prefix)}
	if label := strings.TrimSpace(tab.Label); label != "" {
		parts = append(parts, fmt.Sprintf("label=%q", label))
	}
	if tabID := strings.TrimSpace(tab.TabID); tabID != "" {
		parts = append(parts, fmt.Sprintf("id=%s", tabID))
	}
	if tabURL := normalizedCDPTabURL(tab.URL); tabURL != "" {
		parts = append(parts, fmt.Sprintf("url=%q", tabURL))
	}
	return strings.Join(parts, " ")
}

func (e *Executor) reuseCDPTabForNew(ctx context.Context, session, cdpURL string, opts *ExecuteOptions, port int, ownerID string, request newCDPTabRequest) (string, bool, error) {
	output, err := e.listCDPTabs(ctx, session, cdpURL, executeOptionsWithTimeout(opts, cdpTabListTimeout))
	if err != nil {
		return "", false, fmt.Errorf("CDP_TAB_REUSE_CHECK_UNAVAILABLE: cannot safely create label %q for %q because the real tab list could not be refreshed: %w", request.Label, request.URL, err)
	}
	tabs, err := parseCDPTabs(output)
	if err != nil {
		return "", false, fmt.Errorf("CDP_TAB_REUSE_CHECK_INVALID: cannot safely create label %q because the real tab list response was invalid: %w", request.Label, err)
	}
	candidate, navigate, collision, ok := findReusableCDPTab(tabs, port, ownerID, request)
	if !ok {
		if collision != nil {
			return "", false, fmt.Errorf("CDP_TAB_LABEL_CONFLICT: label %q already belongs to tab %s at %q. Select that tab explicitly, or choose a different workflow label; the backend will not create a duplicate label or silently navigate a pre-existing user tab", request.Label, collision.TabID, normalizedCDPTabURL(collision.URL))
		}
		return "", false, nil
	}

	tabID := strings.TrimSpace(candidate.TabID)
	if tabID == "" {
		return "", false, nil
	}
	// `agent-browser tab <id>` activates the visible Chrome window on macOS.
	// The real reuse-check above already tells us whether this tab is active, so
	// do not switch it again when no switch is needed.
	if !candidate.Active {
		selectedOutput, err := e.selectCDPTab(ctx, session, tabID, cdpURL, opts)
		if err != nil {
			return "", false, fmt.Errorf("reuse existing CDP tab %s: %w", tabID, err)
		}
		if resolved := findCDPTabID(selectedOutput, tabID); resolved != "" {
			tabID = resolved
		}
	}
	candidate.TabID = tabID

	if navigate {
		openOutput, openErr := e.Client.ExecuteCommand(ctx, []string{
			"--session", session,
			"open", request.URL,
			"--cdp", cdpURL,
			"--json",
		}, opts)
		if openErr != nil {
			return "", false, fmt.Errorf("navigate reused workflow-owned CDP tab %s to %q: %w", tabID, request.URL, openErr)
		}
		candidate.URL = request.URL
		if opened, found := findCreatedCDPTab(openOutput, ""); found && opened.URL != "" {
			candidate.URL = opened.URL
		}
	}

	setCDPTabAlias(port, ownerID, request.Label, tabID)
	setCDPTabSelection(port, ownerID, tabID)
	setCDPActiveTab(port, tabID)
	log.Printf("[BROWSER] CDP: reused tab label=%q id=%q url=%q owner=%q port=%d", request.Label, tabID, candidate.URL, ownerID, port)
	return formatCDPTabIdentity("Reused existing CDP tab", candidate) + "\n" + selectedCDPTabMessage(port, ownerID), true, nil
}

func formatCDPTabListForPrompt(output string) string {
	raw := strings.TrimSpace(output)
	if raw == "" {
		return "(no tabs returned)"
	}

	tabs, err := parseCDPTabs(raw)
	if err != nil {
		return truncatePromptField(raw, 2000)
	}
	if len(tabs) == 0 {
		return "(no tabs found)"
	}

	const maxTabs = 20
	lines := make([]string, 0, min(len(tabs), maxTabs)+1)
	for i, tab := range tabs {
		if i >= maxTabs {
			lines = append(lines, fmt.Sprintf("... %d more tab(s) omitted", len(tabs)-maxTabs))
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
		if tabURL := truncatePromptField(oneLine(displayCDPTabURL(tab.URL)), 180); tabURL != "" {
			line += fmt.Sprintf(" url=%q", tabURL)
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
