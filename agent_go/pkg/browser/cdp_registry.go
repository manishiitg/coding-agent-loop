package browser

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// CDP mode connects every workflow to the user's single real Chrome, so the
// headless SessionTracker (one Chrome per session, evictable) doesn't apply.
// This registry is the CDP-mode counterpart: it records which owners
// (workflow/agent sessions) are actively using each CDP port so that
// destructive operations (reset) and unbounded growth (tab creation) can be
// arbitrated across concurrent workflows instead of silently interfering —
// the failure mode where one workflow's reset killed the shared daemon and
// wiped every other workflow's tab state.

var (
	// MaxCDPTabsPerOwner caps how many labeled tabs a single workflow/agent
	// may keep in the shared Chrome. Overridden by MAX_CDP_TABS_PER_OWNER.
	MaxCDPTabsPerOwner = 4

	// cdpOwnerActiveWindow is how recently an owner must have issued a CDP
	// command to count as "active" when guarding destructive operations.
	cdpOwnerActiveWindow = 10 * time.Minute

	cdpOwnersMu sync.Mutex
	cdpOwners   = make(map[int]map[string]time.Time) // port → ownerID → lastUsed
)

func init() {
	if v, err := strconv.Atoi(os.Getenv("MAX_CDP_TABS_PER_OWNER")); err == nil && v > 0 {
		MaxCDPTabsPerOwner = v
		log.Printf("[CDP_REGISTRY] Per-owner tab limit set from env: %d", v)
	}
}

// touchCDPOwner records that an owner issued a command against a CDP port.
func touchCDPOwner(port int, ownerID string) {
	if ownerID == "" {
		return
	}
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	owners := cdpOwners[port]
	if owners == nil {
		owners = make(map[string]time.Time)
		cdpOwners[port] = owners
	}
	if _, exists := owners[ownerID]; !exists {
		log.Printf("[CDP_REGISTRY] New owner %q on CDP port %d (owners: %d)", ownerID, port, len(owners)+1)
	}
	owners[ownerID] = time.Now()
}

// removeCDPOwner drops an owner from a port's registry.
func removeCDPOwner(port int, ownerID string) {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	if owners := cdpOwners[port]; owners != nil {
		delete(owners, ownerID)
		if len(owners) == 0 {
			delete(cdpOwners, port)
		}
	}
}

// otherActiveCDPOwners returns owners (excluding ownerID) that used the port
// within cdpOwnerActiveWindow. Stale entries are pruned as a side effect.
func otherActiveCDPOwners(port int, ownerID string) []string {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	owners := cdpOwners[port]
	if owners == nil {
		return nil
	}
	var active []string
	for owner, lastUsed := range owners {
		if time.Since(lastUsed) > cdpOwnerActiveWindow {
			delete(owners, owner)
			continue
		}
		if owner != ownerID {
			active = append(active, owner)
		}
	}
	if len(owners) == 0 {
		delete(cdpOwners, port)
	}
	sort.Strings(active)
	return active
}

// ActiveCDPOwnersSnapshot returns all tracked CDP owners for observability
// (exposed alongside SessionTracker.ActiveSessions in the debug endpoint).
func ActiveCDPOwnersSnapshot() []map[string]string {
	cdpOwnersMu.Lock()
	defer cdpOwnersMu.Unlock()
	var result []map[string]string
	for port, owners := range cdpOwners {
		for owner, lastUsed := range owners {
			result = append(result, map[string]string{
				"cdp_port": strconv.Itoa(port),
				"owner":    owner,
				"idle":     time.Since(lastUsed).Round(time.Second).String(),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i]["cdp_port"] != result[j]["cdp_port"] {
			return result[i]["cdp_port"] < result[j]["cdp_port"]
		}
		return result[i]["owner"] < result[j]["owner"]
	})
	return result
}

// guardCDPReset returns an error when a reset on the shared CDP browser would
// disrupt other active workflows. Reset kills the shared agent-browser daemon
// and clears every owner's tab selections on the port, so it is only allowed
// when the requesting owner is the sole active user.
func guardCDPReset(port int, ownerID string) error {
	others := otherActiveCDPOwners(port, ownerID)
	if len(others) == 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to reset shared CDP browser on port %d: %d other workflow(s) used it in the last %s (%v). "+
			"A reset kills the shared browser daemon and clears their tab state. "+
			"Instead, close your own tab with agent_browser(command=\"tab\", args=[\"close\", \"<your-tab-label>\"]) "+
			"or create a fresh labeled tab with agent_browser(command=\"tab\", args=[\"new\", \"--label\", \"<label>\", \"<url>\"]).",
		port, len(others), cdpOwnerActiveWindow, others)
}

// guardCDPTabCreation returns an error when an owner already has
// MaxCDPTabsPerOwner labeled tabs in the shared Chrome.
func guardCDPTabCreation(port int, ownerID string) error {
	count := countCDPTabAliasesForOwner(port, ownerID)
	if count < MaxCDPTabsPerOwner {
		return nil
	}
	return fmt.Errorf(
		"cannot create another tab in the shared CDP browser: this workflow already has %d labeled tab(s) (max %d). "+
			"Reuse an existing tab by label, or close one first with agent_browser(command=\"tab\", args=[\"close\", \"<label>\"]).",
		count, MaxCDPTabsPerOwner)
}
