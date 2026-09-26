package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Per-conversation tabs in a shared managed headless browser.
//
// A Crew (or workflow) keeps ONE persistent agent-browser session, so logins,
// cookies and the profile are shared by every conversation that uses it: the
// Crew's main chat and every caller's `ask` conversation resolve to the same
// session name (common.ResolveBrowserSessionID). agent-browser has a single
// current tab per session, so without routing, one conversation's open/click
// moved the page another conversation was working on.
//
// This file applies the per-owner tab process built for the shared CDP Chrome
// (cdp_tabs.go / cdp_registry.go) to a managed session: the owner is the
// conversation (ctx ChatSessionIDKey), the scope is "session:<name>", and the
// per-command tab selection runs without --cdp. Each owner gets and keeps its
// own tab; every page command first selects that tab, and both calls run
// under the caller's AcquireBrowserAutomation(session) hold so select-then-
// command is atomic between conversations.

var (
	// sessionTabOwners records the conversations using each managed session:
	// session → owner → lastUsed. Guarded by cdpOwnersMu (same registry lock as
	// the CDP per-port owners).
	sessionTabOwners = make(map[string]map[string]time.Time)

	// sessionTabHandoff remembers the tab the last owner of a session left
	// selected when it ended while no other owner was using the browser, so the
	// next conversation (for example the next step of a sequential workflow)
	// continues on that page instead of a blank one. Guarded by
	// cdpTabSelectionsMu.
	sessionTabHandoff = make(map[string]string)

	sessionTabLabelCounter atomic.Int64
)

// sessionOwnerTabIdleRelease is how long a conversation may leave its tab
// untouched before the idle reaper closes it (only while other conversations
// keep the browser alive; an entirely idle browser is reaped whole).
var sessionOwnerTabIdleRelease = time.Hour

// tabTarget is how one agent-browser tab helper call reaches its browser: the
// session name, launch flags for a managed session that may need to start,
// and the --cdp endpoint only in CDP mode.
type tabTarget struct {
	session    string
	cdpURL     string
	launchArgs []string
}

func cdpTabTarget(session, cdpURL string) tabTarget {
	return tabTarget{session: session, cdpURL: cdpURL}
}

func headlessTabTarget(session string) tabTarget {
	return tabTarget{session: session, launchArgs: HeadlessLaunchArgsForSession(session)}
}

// command builds [launch flags...] --session <s> <args...> [--cdp <url>] --json.
// For a CDP target this is byte-for-byte the historical helper argv.
func (t tabTarget) command(args ...string) []string {
	out := make([]string, 0, len(t.launchArgs)+len(args)+6)
	out = append(out, t.launchArgs...)
	out = append(out, "--session", t.session)
	out = append(out, args...)
	if t.cdpURL != "" {
		out = append(out, "--cdp", t.cdpURL)
	}
	return append(out, "--json")
}

// sessionTabScope is the tab-state scope of one managed session. Names are
// escaped so a ':' inside them cannot alias another scope's keys.
func sessionTabScope(session string) tabScope {
	return tabScope("session:" + url.QueryEscape(strings.TrimSpace(session)))
}

// sessionTabOwnerKey escapes a conversation ID for use inside tab-state keys.
// Conversation IDs such as "work:project:<crew>" contain ':', which would make
// one owner's key prefix match another owner's keys.
func sessionTabOwnerKey(ownerID string) string {
	return url.QueryEscape(strings.TrimSpace(ownerID))
}

// sessionTabOwnerID is the conversation that owns a tab: the agent/chat
// session, falling back to the workflow session.
func sessionTabOwnerID(agentSessionID, workflowSessionID string) string {
	if owner := strings.TrimSpace(agentSessionID); owner != "" {
		return owner
	}
	return strings.TrimSpace(workflowSessionID)
}

// sessionOwnerTabsApply reports whether per-conversation tab routing applies.
// The server-wide shared-profile browser is excluded: its automation lock is a
// no-op, so select-then-command could not be made atomic there.
func sessionOwnerTabsApply(session, ownerID string) bool {
	if strings.TrimSpace(session) == "" || strings.TrimSpace(ownerID) == "" {
		return false
	}
	return !(SharedBrowserEnabled() && session == SharedSessionName)
}

func touchSessionTabOwner(session, ownerID string) {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	owners := sessionTabOwners[session]
	if owners == nil {
		owners = make(map[string]time.Time)
		sessionTabOwners[session] = owners
	}
	if _, exists := owners[ownerID]; !exists {
		log.Printf("[BROWSER_TABS] New conversation %q on browser %q (conversations: %d)", ownerID, session, len(owners)+1)
	}
	owners[ownerID] = time.Now()
}

func removeSessionTabOwner(session, ownerID string) {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	if owners := sessionTabOwners[session]; owners != nil {
		delete(owners, ownerID)
		if len(owners) == 0 {
			delete(sessionTabOwners, session)
		}
	}
}

// otherActiveSessionTabOwners returns the other conversations that used the
// session within cdpOwnerActiveWindow (the same window guardCDPReset uses).
func otherActiveSessionTabOwners(session, ownerID string) []string {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	owners := sessionTabOwners[session]
	if owners == nil {
		return nil
	}
	return otherActiveOwnersLocked(owners, ownerID, false)
}

// otherSessionTabOwners returns every other registered conversation, active
// or not, sorted.
func otherSessionTabOwners(session, ownerID string) []string {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	var others []string
	for owner := range sessionTabOwners[session] {
		if owner != ownerID {
			others = append(others, owner)
		}
	}
	sort.Strings(others)
	return others
}

// SessionHasOtherTabOwners reports whether conversations other than ownerID
// still hold tabs in the managed session, so the browser must stay open.
func SessionHasOtherTabOwners(session, ownerID string) bool {
	return len(otherSessionTabOwners(session, ownerID)) > 0
}

// sessionTabOwnerOf returns which registered conversation owns tabID, or "".
func sessionTabOwnerOf(session, tabID string) string {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return ""
	}
	scope := sessionTabScope(session)
	for _, owner := range otherSessionTabOwners(session, "") {
		if isScopedTabOwnedByOwner(scope, sessionTabOwnerKey(owner), tabID, tabID) {
			return owner
		}
	}
	return ""
}

// clearSessionTabScope forgets every conversation's tab state for a session.
// Called whenever the session's browser runtime is killed (reset, reaper,
// eviction, dead-session recovery): agent-browser renumbers tabs in a fresh
// browser, so remembered tab IDs would point at the wrong pages.
func clearSessionTabScope(session string) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	clearScopedTabsLocked(sessionTabScope(session))
	delete(sessionTabHandoff, session)
	cdpTabSelectionsMu.Unlock()
	cdpOwnersMu.Lock()
	delete(sessionTabOwners, session)
	cdpOwnersMu.Unlock()
}

func dropSessionTabOwnerState(session, ownerID string) {
	scope := sessionTabScope(session)
	prefix := scope.ownerPrefix(sessionTabOwnerKey(ownerID))
	cdpTabSelectionsMu.Lock()
	delete(cdpTabSelections, scope.ownerKey(sessionTabOwnerKey(ownerID)))
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
	cdpTabSelectionsMu.Unlock()
	removeSessionTabOwner(session, ownerID)
}

// sessionTabOutcome is what per-conversation routing decided for a command.
type sessionTabOutcome struct {
	// handled means routing produced the final tool result itself (tab
	// commands, or a close scoped to this conversation's tab).
	handled bool
	output  string
	// note is appended to the command's result (for example: the previous tab
	// disappeared and a fresh one was opened).
	note string
	// tab is the tab the command was routed to.
	tab string
}

// routeSessionOwnerTab gives the calling conversation its own tab in a shared
// managed session. The caller must hold AcquireBrowserAutomation(session).
func (e *Executor) routeSessionOwnerTab(ctx context.Context, session, ownerID, command string, args []string, opts *ExecuteOptions) (sessionTabOutcome, error) {
	if !sessionOwnerTabsApply(session, ownerID) || isBrowserDocumentationCommand(command) || command == "capture" || command == "status" {
		return sessionTabOutcome{}, nil
	}
	target := headlessTabTarget(session)

	switch command {
	case "reset":
		// Reset kills the whole browser: only allowed when no other
		// conversation is actively using it (mirrors guardCDPReset).
		if others := otherActiveSessionTabOwners(session, ownerID); len(others) > 0 {
			return sessionTabOutcome{}, fmt.Errorf(
				"refusing to reset shared browser %q: %d other conversation(s) used it in the last %s (%v). "+
					"A reset kills the shared browser and every conversation's tab. "+
					"Instead, close only this conversation's tab with agent_browser(command=\"tab\", args=[\"close\"]); the next command opens a fresh one",
				session, len(others), cdpOwnerActiveWindow, others)
		}
		clearSessionTabScope(session)
		return sessionTabOutcome{}, nil
	case "close", "quit", "exit":
		if !SessionHasOtherTabOwners(session, ownerID) {
			// Sole user: keep the historical whole-browser close.
			clearSessionTabScope(session)
			return sessionTabOutcome{}, nil
		}
		closed := e.closeSessionOwnerTabs(ctx, target, session, ownerID, opts)
		dropSessionTabOwnerState(session, ownerID)
		message := fmt.Sprintf("Closed this conversation's tab(s) %v. The shared browser stays open for %d other conversation(s); its sign-ins are kept. The next browser command opens a fresh tab.", closed, len(otherSessionTabOwners(session, ownerID)))
		return sessionTabOutcome{handled: true, output: fmt.Sprintf(`{"success":true,"message":%q}`, message)}, nil
	case "tab":
		touchSessionTabOwner(session, ownerID)
		return e.handleSessionOwnerTabCommand(ctx, target, session, ownerID, args, opts)
	}

	touchSessionTabOwner(session, ownerID)
	tab, note, err := e.selectSessionOwnerTab(ctx, target, session, ownerID, command, opts)
	if err != nil {
		return sessionTabOutcome{}, err
	}
	return sessionTabOutcome{note: note, tab: tab}, nil
}

// selectSessionOwnerTab makes the owner's tab the session's current tab,
// acquiring one first if the owner has none, and transparently replacing a tab
// that has disappeared.
func (e *Executor) selectSessionOwnerTab(ctx context.Context, target tabTarget, session, ownerID, command string, opts *ExecuteOptions) (string, string, error) {
	scope := sessionTabScope(session)
	owner := sessionTabOwnerKey(ownerID)
	note := ""

	tab := getScopedTabSelection(scope, owner)
	if tab == "" {
		acquired, err := e.acquireSessionOwnerTab(ctx, target, session, ownerID, opts)
		if err != nil {
			return "", "", err
		}
		tab = acquired
	}

	output, err := e.Client.ExecuteCommand(ctx, target.command("tab", tab), opts)
	if err != nil {
		listed, listErr := e.Client.ExecuteCommand(ctx, target.command("tab"), executeOptionsWithTimeout(opts, cdpTabListTimeout))
		if listErr != nil {
			return "", "", fmt.Errorf("failed to select this conversation's browser tab %q before %q: %w", tab, command, err)
		}
		tabs, parseErr := parseCDPTabs(listed)
		if parseErr != nil {
			return "", "", fmt.Errorf("failed to select this conversation's browser tab %q before %q: %w (tab list unreadable: %w)", tab, command, err, parseErr)
		}
		if _, stillThere := findCDPTabByRef(tabs, tab); stillThere {
			return "", "", fmt.Errorf("failed to select this conversation's browser tab %q before %q: %w", tab, command, err)
		}
		// The page closed itself or the tab crashed: give the conversation a
		// fresh tab rather than failing or borrowing someone else's.
		previous := tab
		cdpTabSelectionsMu.Lock()
		clearScopedTabStateLocked(scope, owner, previous)
		cdpTabSelectionsMu.Unlock()
		tab, err = e.createSessionOwnerTab(ctx, target, session, ownerID, opts)
		if err != nil {
			return "", "", err
		}
		if output, err = e.Client.ExecuteCommand(ctx, target.command("tab", tab), opts); err != nil {
			return "", "", fmt.Errorf("failed to select this conversation's fresh browser tab %q before %q: %w", tab, command, err)
		}
		note = fmt.Sprintf("AGENTWORKS_BROWSER_TAB: this conversation's previous tab %q no longer exists (closed by the page or crashed), so a fresh tab %q was opened in the same browser. Sign-ins are kept, but the page is new: navigate again and take a fresh snapshot; old element refs are invalid.", previous, tab)
		log.Printf("[BROWSER_TABS] Conversation %q lost tab %q on %q; replaced with %q", ownerID, previous, session, tab)
	}
	if resolved := findCDPTabID(output, tab); resolved != "" && resolved != tab {
		tab = resolved
	}
	setScopedTabSelection(scope, owner, tab)
	log.Printf("[BROWSER_TABS] browser=%q conversation=%q selected tab %q before %q", session, ownerID, tab, command)
	return tab, note, nil
}

// acquireSessionOwnerTab gives a conversation its first tab. When nobody else
// holds a tab in this browser it adopts the page left by the previous
// conversation, or the browser's only tab (a fresh launch's blank page, or the
// page a restarted server's browser still shows) so tabs are not leaked.
// Otherwise it opens a new tab.
func (e *Executor) acquireSessionOwnerTab(ctx context.Context, target tabTarget, session, ownerID string, opts *ExecuteOptions) (string, error) {
	scope := sessionTabScope(session)
	owner := sessionTabOwnerKey(ownerID)
	if !SessionHasOtherTabOwners(session, ownerID) {
		listed, err := e.Client.ExecuteCommand(ctx, target.command("tab"), executeOptionsWithTimeout(opts, cdpTabListTimeout))
		if err != nil {
			return "", fmt.Errorf("failed to list tabs of browser %q before giving this conversation its tab: %w", session, err)
		}
		tabs, err := parseCDPTabs(listed)
		if err != nil {
			return "", fmt.Errorf("failed to read tab list of browser %q: %w", session, err)
		}
		adopt := ""
		cdpTabSelectionsMu.Lock()
		handoff := sessionTabHandoff[session]
		delete(sessionTabHandoff, session)
		cdpTabSelectionsMu.Unlock()
		if handoff != "" {
			if found, ok := findCDPTabByRef(tabs, handoff); ok {
				adopt = strings.TrimSpace(found.TabID)
			}
		}
		if adopt == "" && len(tabs) == 1 {
			adopt = strings.TrimSpace(tabs[0].TabID)
		}
		if adopt != "" {
			markScopedTabOwned(scope, owner, adopt, adopt)
			setScopedTabSelection(scope, owner, adopt)
			log.Printf("[BROWSER_TABS] Conversation %q adopted tab %q on %q", ownerID, adopt, session)
			return adopt, nil
		}
	}
	return e.createSessionOwnerTab(ctx, target, session, ownerID, opts)
}

func sessionOwnerTabLabel(ownerID string) string {
	sum := sha256.Sum256([]byte(ownerID))
	return fmt.Sprintf("conv-%s-%d", hex.EncodeToString(sum[:4]), sessionTabLabelCounter.Add(1))
}

func (e *Executor) createSessionOwnerTab(ctx context.Context, target tabTarget, session, ownerID string, opts *ExecuteOptions) (string, error) {
	scope := sessionTabScope(session)
	owner := sessionTabOwnerKey(ownerID)
	label := sessionOwnerTabLabel(ownerID)
	output, err := e.Client.ExecuteCommand(ctx, target.command("tab", "new", "--label", label), opts)
	if err != nil {
		return "", fmt.Errorf("failed to open a tab for this conversation in browser %q: %w", session, err)
	}
	tabID := ""
	if created, found := findCreatedCDPTab(output, label); found {
		tabID = strings.TrimSpace(created.TabID)
	}
	if tabID == "" {
		if listed, listErr := e.Client.ExecuteCommand(ctx, target.command("tab"), executeOptionsWithTimeout(opts, cdpTabListTimeout)); listErr == nil {
			tabID = findCDPTabID(listed, label)
		}
	}
	if tabID == "" {
		// Fall back to the label: agent-browser accepts it wherever a tab id is.
		tabID = label
	}
	setScopedTabAlias(scope, owner, label, tabID)
	markScopedTabOwned(scope, owner, label, tabID)
	setScopedTabSelection(scope, owner, tabID)
	captureChromePID(target.session)
	log.Printf("[BROWSER_TABS] Opened tab %q (label %q) for conversation %q on %q", tabID, label, ownerID, session)
	return tabID, nil
}

// handleSessionOwnerTabCommand runs an explicit `tab` command for a
// conversation: listing is shared, but creating/selecting records the tab as
// this conversation's, and another active conversation's tab can be neither
// selected nor closed.
func (e *Executor) handleSessionOwnerTabCommand(ctx context.Context, target tabTarget, session, ownerID string, args []string, opts *ExecuteOptions) (sessionTabOutcome, error) {
	scope := sessionTabScope(session)
	owner := sessionTabOwnerKey(ownerID)
	resolve := func(ref string) string {
		if id := getScopedTabAlias(scope, owner, ref); id != "" {
			return id
		}
		return ref
	}
	refuseForeign := func(ref, action string) error {
		if other := sessionTabOwnerOf(session, resolve(ref)); other != "" && other != ownerID {
			return fmt.Errorf("refusing to %s tab %q: it belongs to another conversation (%s) sharing this browser. Use this conversation's own tab (agent_browser(command=\"tab\") lists them) or open one with agent_browser(command=\"tab\", args=[\"new\", \"--label\", \"<label>\", \"<url>\"])", action, ref, other)
		}
		return nil
	}
	yourTab := func() string {
		if tab := getScopedTabSelection(scope, owner); tab != "" {
			return fmt.Sprintf("\n\nAGENTWORKS_BROWSER_TAB: this conversation's tab is %q; other tabs belong to other conversations sharing this browser (same sign-ins). Page commands are routed to this conversation's tab automatically.", tab)
		}
		return ""
	}

	switch {
	case isTabListRequest(args):
		output, err := e.Client.ExecuteCommand(ctx, target.command("tab"), opts)
		if err != nil {
			return sessionTabOutcome{}, err
		}
		return sessionTabOutcome{handled: true, output: strings.TrimSpace(output) + yourTab()}, nil

	case args[0] == "new":
		request, err := parseNewCDPTabRequest(args)
		if err != nil {
			return sessionTabOutcome{}, err
		}
		output, err := e.Client.ExecuteCommand(ctx, target.command(append([]string{"tab"}, canonicalNewCDPTabArgs(request)...)...), opts)
		if err != nil {
			return sessionTabOutcome{}, err
		}
		tabID := ""
		if created, found := findCreatedCDPTab(output, request.Label); found {
			tabID = strings.TrimSpace(created.TabID)
		}
		if tabID == "" {
			tabID = request.Label
		}
		setScopedTabAlias(scope, owner, request.Label, tabID)
		markScopedTabOwned(scope, owner, request.Label, tabID)
		setScopedTabSelection(scope, owner, tabID)
		captureChromePID(target.session)
		return sessionTabOutcome{handled: true, output: strings.TrimSpace(output) + yourTab(), tab: tabID}, nil

	case args[0] == "close":
		ref := ""
		if len(args) > 1 {
			ref = strings.TrimSpace(args[1])
		}
		if ref == "" {
			ref = getScopedTabSelection(scope, owner)
			if ref == "" {
				return sessionTabOutcome{handled: true, output: `{"success":true,"message":"this conversation has no tab open in the shared browser"}`}, nil
			}
		}
		if err := refuseForeign(ref, "close"); err != nil {
			return sessionTabOutcome{}, err
		}
		resolved := resolve(ref)
		output, err := e.Client.ExecuteCommand(ctx, target.command("tab", "close", resolved), opts)
		if err != nil {
			return sessionTabOutcome{}, err
		}
		cdpTabSelectionsMu.Lock()
		clearScopedTabStateLocked(scope, owner, ref)
		clearScopedTabStateLocked(scope, owner, resolved)
		cdpTabSelectionsMu.Unlock()
		return sessionTabOutcome{handled: true, output: strings.TrimSpace(output) + "\n\nAGENTWORKS_BROWSER_TAB: closed. The next page command opens a fresh tab for this conversation if it has none left selected."}, nil

	default:
		ref := strings.TrimSpace(args[0])
		if strings.HasPrefix(ref, "-") {
			output, err := e.Client.ExecuteCommand(ctx, target.command(append([]string{"tab"}, args...)...), opts)
			if err != nil {
				return sessionTabOutcome{}, err
			}
			return sessionTabOutcome{handled: true, output: output}, nil
		}
		if err := refuseForeign(ref, "select"); err != nil {
			return sessionTabOutcome{}, err
		}
		output, err := e.Client.ExecuteCommand(ctx, target.command("tab", resolve(ref)), opts)
		if err != nil {
			return sessionTabOutcome{}, err
		}
		tabID := findCDPTabID(output, ref)
		if tabID == "" {
			tabID = resolve(ref)
		}
		setScopedTabAlias(scope, owner, ref, tabID)
		markScopedTabOwned(scope, owner, ref, tabID)
		setScopedTabSelection(scope, owner, tabID)
		return sessionTabOutcome{handled: true, output: strings.TrimSpace(output) + yourTab(), tab: tabID}, nil
	}
}

// closeSessionOwnerTabs closes every tab a conversation owns (best effort) and
// returns the tab IDs it closed.
func (e *Executor) closeSessionOwnerTabs(ctx context.Context, target tabTarget, session, ownerID string, opts *ExecuteOptions) []string {
	return closeSessionOwnerTabsWith(ctx, e.Client, target, session, ownerID, opts)
}

func closeSessionOwnerTabsWith(ctx context.Context, client *Client, target tabTarget, session, ownerID string, opts *ExecuteOptions) []string {
	closed := []string{}
	if client == nil {
		return closed
	}
	seen := map[string]bool{}
	for _, owned := range ownedScopedTabsForOwner(sessionTabScope(session), sessionTabOwnerKey(ownerID)) {
		tabID := strings.TrimSpace(owned.TabID)
		if tabID == "" || seen[tabID] {
			continue
		}
		seen[tabID] = true
		if output, err := client.ExecuteCommand(ctx, target.command("tab", "close", tabID), opts); err != nil {
			log.Printf("[BROWSER_TABS] Could not close tab %q of conversation %q on %q: %v (%s)", tabID, ownerID, session, err, strings.TrimSpace(output))
			continue
		}
		closed = append(closed, tabID)
	}
	sort.Strings(closed)
	return closed
}

// ReleaseSessionTabOwner is called when a conversation ends (stop, clear,
// completion) or goes idle. While other conversations still use a browser, the
// ending conversation's tabs are closed so they do not accumulate. When it was
// the browser's only user, its tabs are kept and its selected page is handed
// to whichever conversation uses the browser next.
func ReleaseSessionTabOwner(ownerID string, client *Client) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return
	}
	cdpOwnersMu.Lock()
	var sessions []string
	for session, owners := range sessionTabOwners {
		if _, ok := owners[ownerID]; ok {
			sessions = append(sessions, session)
		}
	}
	cdpOwnersMu.Unlock()
	sort.Strings(sessions)
	for _, session := range sessions {
		releaseSessionTabOwner(session, ownerID, client)
	}
}

func releaseSessionTabOwner(session, ownerID string, client *Client) {
	scope := sessionTabScope(session)
	owner := sessionTabOwnerKey(ownerID)
	if !SessionHasOtherTabOwners(session, ownerID) {
		if selected := getScopedTabSelection(scope, owner); selected != "" {
			cdpTabSelectionsMu.Lock()
			sessionTabHandoff[session] = selected
			cdpTabSelectionsMu.Unlock()
		}
		dropSessionTabOwnerState(session, ownerID)
		log.Printf("[BROWSER_TABS] Conversation %q released browser %q (last user; tab kept for the next conversation)", ownerID, session)
		return
	}
	if client != nil && len(ownedScopedTabsForOwner(scope, owner)) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		release, err := AcquireBrowserAutomation(ctx, session)
		if err == nil {
			closed := closeSessionOwnerTabsWith(ctx, client, headlessTabTarget(session), session, ownerID, &ExecuteOptions{Timeout: 10 * time.Second})
			release()
			log.Printf("[BROWSER_TABS] Conversation %q released browser %q; closed its tab(s) %v", ownerID, session, closed)
		} else {
			log.Printf("[BROWSER_TABS] Conversation %q released browser %q without closing its tabs (browser busy: %v)", ownerID, session, err)
		}
		cancel()
	}
	dropSessionTabOwnerState(session, ownerID)
}

// releaseIdleSessionTabOwners closes the tabs of conversations that have not
// used a still-running shared browser for idleAfter. Called by the idle reaper.
func releaseIdleSessionTabOwners(idleAfter time.Duration, client *Client) {
	type idleOwner struct{ session, owner string }
	var idle []idleOwner
	cdpOwnersMu.Lock()
	for session, owners := range sessionTabOwners {
		for owner, lastUsed := range owners {
			if time.Since(lastUsed) > idleAfter {
				idle = append(idle, idleOwner{session, owner})
			}
		}
	}
	cdpOwnersMu.Unlock()
	for _, item := range idle {
		log.Printf("[BROWSER_REAPER] Releasing idle conversation %q from browser %q (idle > %s)", item.owner, item.session, idleAfter)
		releaseSessionTabOwner(item.session, item.owner, client)
	}
}

func defaultWorkspaceBrowserClient() *Client {
	workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
	if workspaceAPIURL == "" {
		workspaceAPIURL = "http://127.0.0.1:8081"
	}
	return NewClient(workspaceAPIURL)
}
