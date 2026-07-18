package browser

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultCDPTabCleanupDelay keeps workflow-created tabs available briefly for
// inspection/reuse after a run, then closes them without terminating Chrome or
// touching tabs that were not created by the workflow.
const DefaultCDPTabCleanupDelay = time.Hour

const cdpTabCleanupRetryDelay = 10 * time.Minute

type cdpTabCleanupLease struct {
	active     int
	generation uint64
	timer      *time.Timer
}

var (
	cdpTabCleanupMu     sync.Mutex
	cdpTabCleanupLeases = make(map[string]*cdpTabCleanupLease)
)

func cdpTabCleanupKey(port int, ownerID string) string {
	return fmt.Sprintf("cdp:%d:%s", port, strings.TrimSpace(ownerID))
}

// AcquireCDPTabOwnerLease marks a workflow/browser owner as active. Starting a
// new or concurrent run cancels any delayed cleanup left by an earlier run so
// its timer cannot close tabs while the owner is in use.
func AcquireCDPTabOwnerLease(ownerID string, ports []int) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return
	}
	for _, port := range normalizeCDPPorts(ports) {
		key := cdpTabCleanupKey(port, ownerID)
		cdpTabCleanupMu.Lock()
		lease := cdpTabCleanupLeases[key]
		if lease == nil {
			lease = &cdpTabCleanupLease{}
			cdpTabCleanupLeases[key] = lease
		}
		lease.active++
		lease.generation++
		if lease.timer != nil {
			lease.timer.Stop()
			lease.timer = nil
			log.Printf("[CDP_CLEANUP] Canceled pending tab cleanup for active owner=%q port=%d", ownerID, port)
		}
		active := lease.active
		cdpTabCleanupMu.Unlock()
		log.Printf("[CDP_CLEANUP] Acquired owner lease owner=%q port=%d active=%d", ownerID, port, active)
	}
}

// ReleaseCDPTabOwnerLease releases one active run. The final release schedules
// workflow-created tabs for cleanup after delay. Tabs selected from the user's
// existing Chrome session are never included in the owned-tab registry.
func ReleaseCDPTabOwnerLease(ownerID string, ports []int, client *Client, delay time.Duration) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return
	}
	if delay < 0 {
		delay = 0
	}
	for _, port := range normalizeCDPPorts(ports) {
		key := cdpTabCleanupKey(port, ownerID)
		cdpTabCleanupMu.Lock()
		lease := cdpTabCleanupLeases[key]
		if lease == nil {
			lease = &cdpTabCleanupLease{}
			cdpTabCleanupLeases[key] = lease
		}
		if lease.active > 0 {
			lease.active--
		}
		lease.generation++
		generation := lease.generation
		active := lease.active
		if active == 0 {
			if len(ownedCDPTabsForOwner(port, ownerID)) > 0 {
				scheduleCDPTabCleanupLocked(key, ownerID, port, client, delay, generation)
			} else {
				if lease.timer != nil {
					lease.timer.Stop()
				}
				delete(cdpTabCleanupLeases, key)
			}
		}
		cdpTabCleanupMu.Unlock()
		if active > 0 {
			log.Printf("[CDP_CLEANUP] Released owner lease owner=%q port=%d; %d concurrent run(s) still active", ownerID, port, active)
		} else if len(ownedCDPTabsForOwner(port, ownerID)) > 0 {
			log.Printf("[CDP_CLEANUP] Scheduled workflow-created tab cleanup owner=%q port=%d after %s", ownerID, port, delay)
		}
	}
}

func scheduleCDPTabCleanupLocked(key, ownerID string, port int, client *Client, delay time.Duration, generation uint64) {
	lease := cdpTabCleanupLeases[key]
	if lease == nil {
		return
	}
	if lease.timer != nil {
		lease.timer.Stop()
	}
	lease.timer = time.AfterFunc(delay, func() {
		runScheduledCDPTabCleanup(key, ownerID, port, client, generation)
	})
}

func runScheduledCDPTabCleanup(key, ownerID string, port int, client *Client, generation uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()

	// Serialize cleanup with normal CDP actions on this port. This prevents a
	// tab-close command from racing a page action in another workflow.
	unlock, err := acquireSharedCDPLock(ctx, port)
	if err != nil {
		log.Printf("[CDP_CLEANUP] Could not acquire port lock owner=%q port=%d: %v", ownerID, port, err)
		rescheduleFailedCDPTabCleanup(key, ownerID, port, client, generation)
		return
	}
	defer unlock()

	cdpTabCleanupMu.Lock()
	lease := cdpTabCleanupLeases[key]
	if lease == nil || lease.active > 0 || lease.generation != generation {
		cdpTabCleanupMu.Unlock()
		return
	}
	lease.timer = nil
	cdpTabCleanupMu.Unlock()

	remaining := closeOwnedCDPTabs(ctx, client, port, ownerID)

	cdpTabCleanupMu.Lock()
	defer cdpTabCleanupMu.Unlock()
	lease = cdpTabCleanupLeases[key]
	if lease == nil || lease.active > 0 || lease.generation != generation {
		return
	}
	if remaining > 0 {
		lease.generation++
		nextGeneration := lease.generation
		scheduleCDPTabCleanupLocked(key, ownerID, port, client, cdpTabCleanupRetryDelay, nextGeneration)
		log.Printf("[CDP_CLEANUP] %d tab(s) remain for owner=%q port=%d; retrying after %s", remaining, ownerID, port, cdpTabCleanupRetryDelay)
		return
	}
	delete(cdpTabCleanupLeases, key)
	removeCDPOwner(port, ownerID)
	clearCDPTabSelection(port, ownerID)
	log.Printf("[CDP_CLEANUP] Completed delayed tab cleanup owner=%q port=%d", ownerID, port)
}

func rescheduleFailedCDPTabCleanup(key, ownerID string, port int, client *Client, generation uint64) {
	cdpTabCleanupMu.Lock()
	defer cdpTabCleanupMu.Unlock()
	lease := cdpTabCleanupLeases[key]
	if lease == nil || lease.active > 0 || lease.generation != generation {
		return
	}
	lease.generation++
	nextGeneration := lease.generation
	scheduleCDPTabCleanupLocked(key, ownerID, port, client, cdpTabCleanupRetryDelay, nextGeneration)
}

func closeOwnedCDPTabs(ctx context.Context, client *Client, port int, ownerID string) int {
	tabs := ownedCDPTabsForOwner(port, ownerID)
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].Alias < tabs[j].Alias })
	if len(tabs) == 0 {
		return 0
	}
	if client == nil {
		log.Printf("[CDP_CLEANUP] Browser client unavailable; retaining %d owned tab(s) owner=%q port=%d", len(tabs), ownerID, port)
		return len(tabs)
	}

	for _, tab := range tabs {
		target := strings.TrimSpace(tab.TabID)
		if target == "" {
			target = tab.Alias
		}
		_, err := client.ExecuteCommand(ctx, []string{
			"--session", sharedCDPSessionName(port),
			"tab", "close", target,
			"--cdp", resolveCdpURL(port),
			"--json",
		}, &ExecuteOptions{Timeout: 10 * time.Second})
		if err != nil && !isMissingCDPTabError(err) {
			log.Printf("[CDP_CLEANUP] Failed to close owned tab label=%q id=%q owner=%q port=%d: %v", tab.Alias, tab.TabID, ownerID, port, err)
			continue
		}
		clearCDPTabStateForOwner(port, ownerID, target)
		log.Printf("[CDP_CLEANUP] Closed workflow-created tab label=%q id=%q owner=%q port=%d", tab.Alias, tab.TabID, ownerID, port)
	}
	return len(ownedCDPTabsForOwner(port, ownerID))
}

func isMissingCDPTabError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"tab not found", "no such tab", "no tab with label", "unknown tab", "does not exist", "not selectable"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func resetCDPTabCleanupForTest() {
	cdpTabCleanupMu.Lock()
	defer cdpTabCleanupMu.Unlock()
	for _, lease := range cdpTabCleanupLeases {
		if lease.timer != nil {
			lease.timer.Stop()
		}
	}
	cdpTabCleanupLeases = make(map[string]*cdpTabCleanupLease)
}
