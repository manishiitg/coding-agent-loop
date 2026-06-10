package browser

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func resetCDPRegistryForTest(t *testing.T) {
	t.Helper()
	cdpOwnersMu.Lock()
	cdpOwners = make(map[int]map[string]time.Time)
	cdpOwnersMu.Unlock()
	t.Cleanup(func() {
		cdpOwnersMu.Lock()
		cdpOwners = make(map[int]map[string]time.Time)
		cdpOwnersMu.Unlock()
	})
}

func TestGuardCDPResetSoleOwnerAllowed(t *testing.T) {
	resetCDPRegistryForTest(t)
	touchCDPOwner(9222, "workflow-a")
	if err := guardCDPReset(9222, "workflow-a"); err != nil {
		t.Fatalf("sole owner should be allowed to reset, got: %v", err)
	}
}

func TestGuardCDPResetBlockedByOtherActiveOwner(t *testing.T) {
	resetCDPRegistryForTest(t)
	touchCDPOwner(9222, "workflow-a")
	touchCDPOwner(9222, "workflow-b")
	err := guardCDPReset(9222, "workflow-a")
	if err == nil {
		t.Fatal("reset should be refused while another workflow is active on the port")
	}
	if !strings.Contains(err.Error(), "workflow-b") {
		t.Fatalf("error should name the other active owner, got: %v", err)
	}
}

func TestGuardCDPResetIgnoresStaleOwners(t *testing.T) {
	resetCDPRegistryForTest(t)
	touchCDPOwner(9222, "workflow-a")
	touchCDPOwner(9222, "workflow-stale")
	cdpOwnersMu.Lock()
	cdpOwners[9222]["workflow-stale"] = time.Now().Add(-cdpOwnerActiveWindow - time.Minute)
	cdpOwnersMu.Unlock()
	if err := guardCDPReset(9222, "workflow-a"); err != nil {
		t.Fatalf("stale owners should not block reset, got: %v", err)
	}
	// Stale entry should have been pruned.
	if others := otherActiveCDPOwners(9222, "workflow-a"); len(others) != 0 {
		t.Fatalf("expected stale owner pruned, got: %v", others)
	}
}

func TestGuardCDPResetIsolatedPerPort(t *testing.T) {
	resetCDPRegistryForTest(t)
	touchCDPOwner(9222, "workflow-a")
	touchCDPOwner(9223, "workflow-b")
	if err := guardCDPReset(9222, "workflow-a"); err != nil {
		t.Fatalf("owner on a different port should not block reset, got: %v", err)
	}
}

func TestRemoveCDPOwner(t *testing.T) {
	resetCDPRegistryForTest(t)
	touchCDPOwner(9222, "workflow-a")
	touchCDPOwner(9222, "workflow-b")
	removeCDPOwner(9222, "workflow-b")
	if err := guardCDPReset(9222, "workflow-a"); err != nil {
		t.Fatalf("removed owner should not block reset, got: %v", err)
	}
}

func TestGuardCDPTabCreationEnforcesLimit(t *testing.T) {
	resetCDPRegistryForTest(t)
	cdpTabSelectionsMu.Lock()
	cdpTabAliases = make(map[string]string)
	cdpTabSelectionsMu.Unlock()
	t.Cleanup(func() {
		cdpTabSelectionsMu.Lock()
		cdpTabAliases = make(map[string]string)
		cdpTabSelectionsMu.Unlock()
	})

	for i := 0; i < MaxCDPTabsPerOwner; i++ {
		setCDPTabAlias(9222, "workflow-a", fmt.Sprintf("label-%d", i), fmt.Sprintf("t%d", i))
	}
	err := guardCDPTabCreation(9222, "workflow-a")
	if err == nil {
		t.Fatalf("tab creation should be blocked at the per-owner limit (%d)", MaxCDPTabsPerOwner)
	}
	if !strings.Contains(err.Error(), "close") {
		t.Fatalf("error should tell the agent how to recover, got: %v", err)
	}

	// A different owner on the same port is unaffected.
	if err := guardCDPTabCreation(9222, "workflow-b"); err != nil {
		t.Fatalf("other owner should not be limited, got: %v", err)
	}
	// Closing a tab (alias cleared) frees a slot.
	clearCDPTabAlias(9222, "workflow-a", "label-0")
	if err := guardCDPTabCreation(9222, "workflow-a"); err != nil {
		t.Fatalf("freeing a tab slot should allow creation again, got: %v", err)
	}
}

func TestActiveCDPOwnersSnapshot(t *testing.T) {
	resetCDPRegistryForTest(t)
	touchCDPOwner(9222, "workflow-b")
	touchCDPOwner(9222, "workflow-a")
	snapshot := ActiveCDPOwnersSnapshot()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 owners in snapshot, got %d", len(snapshot))
	}
	if snapshot[0]["owner"] != "workflow-a" || snapshot[1]["owner"] != "workflow-b" {
		t.Fatalf("snapshot should be sorted by owner, got: %v", snapshot)
	}
	if snapshot[0]["cdp_port"] != "9222" {
		t.Fatalf("snapshot should include the port, got: %v", snapshot[0])
	}
}
