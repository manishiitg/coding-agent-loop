package terminalleases

import (
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

func TestObserveSeparatesBoundedProcessDeadlineFromSnapshotDeadline(t *testing.T) {
	now := time.Now()
	snapshotDeadline := now.Add(30 * time.Minute)
	registry := NewRegistry("instance-1", 1234, now.Add(-time.Minute))
	snapshot := terminals.Snapshot{
		TerminalID:    "terminal-1",
		TmuxSession:   "mlp-codex-cli-int-1",
		SessionID:     "session-1",
		ExecutionKind: "workflow_step",
		Active:        false,
		ClosesAt:      &snapshotDeadline,
		UpdatedAt:     now,
	}

	lease, acquired := registry.Observe(snapshot, now)
	if !acquired {
		t.Fatal("expected first observation to acquire the lease")
	}
	if lease.Policy != PolicyBounded || lease.ProcessState != ProcessClosing {
		t.Fatalf("policy/state = %q/%q, want bounded/closing", lease.Policy, lease.ProcessState)
	}
	if lease.ProcessDeadline == nil || !lease.ProcessDeadline.Equal(now.Add(DefaultBoundedProcessGrace)) {
		t.Fatalf("process deadline = %v, want %v", lease.ProcessDeadline, now.Add(DefaultBoundedProcessGrace))
	}
	if lease.SnapshotDeadline == nil || !lease.SnapshotDeadline.Equal(snapshotDeadline) {
		t.Fatalf("snapshot deadline = %v, want %v", lease.SnapshotDeadline, snapshotDeadline)
	}
}

func TestDismissedSnapshotDoesNotDeleteLiveLease(t *testing.T) {
	now := time.Now()
	registry := NewRegistry("instance-1", 1234, now)
	registry.Observe(terminals.Snapshot{
		TerminalID:    "terminal-1",
		TmuxSession:   "mlp-pi-cli-int-1",
		SessionID:     "session-1",
		ExecutionKind: "main_agent",
		Active:        true,
		UpdatedAt:     now,
	}, now)

	if !registry.MarkSnapshotDismissed("terminal-1") {
		t.Fatal("expected lease to be marked dismissed")
	}
	lease, ok := registry.GetByTerminal("terminal-1")
	if !ok {
		t.Fatal("dismissal deleted the process lease")
	}
	if !lease.SnapshotDismissed || lease.ProcessState != ProcessLive {
		t.Fatalf("dismissed/state = %v/%q, want true/live", lease.SnapshotDismissed, lease.ProcessState)
	}
}

func TestPersistentMainAgentDoesNotReceiveBoundedDeadline(t *testing.T) {
	now := time.Now()
	registry := NewRegistry("instance-1", 1234, now)
	lease, _ := registry.Observe(terminals.Snapshot{
		TerminalID:    "terminal-1",
		TmuxSession:   "mlp-claude-code-1",
		SessionID:     "session-1",
		OwnerID:       "main:session-1",
		ExecutionKind: "main_agent",
		Active:        false,
		UpdatedAt:     now,
	}, now)

	if lease.Policy != PolicyPersistent {
		t.Fatalf("policy = %q, want persistent", lease.Policy)
	}
	if lease.ProcessDeadline != nil {
		t.Fatalf("persistent lease received bounded deadline %v", lease.ProcessDeadline)
	}
}

func TestClosedLeasePrunesOnlyAfterSnapshotDeadline(t *testing.T) {
	now := time.Now()
	snapshotDeadline := now.Add(time.Minute)
	registry := NewRegistry("instance-1", 1234, now)
	registry.Observe(terminals.Snapshot{
		TerminalID:  "terminal-1",
		TmuxSession: "mlp-agy-cli-int-1",
		SessionID:   "session-1",
		Active:      false,
		ClosesAt:    &snapshotDeadline,
		UpdatedAt:   now,
	}, now)
	registry.MarkClosed("mlp-agy-cli-int-1", "completed", now)

	if got := registry.Prune(now.Add(30 * time.Second)); got != 0 {
		t.Fatalf("early prune = %d, want 0", got)
	}
	if got := registry.Prune(now.Add(2 * time.Minute)); got != 1 {
		t.Fatalf("expired prune = %d, want 1", got)
	}
}

func TestClosedLeaseWithoutSnapshotDeadlineGetsBoundedRetention(t *testing.T) {
	now := time.Now()
	registry := NewRegistry("instance-1", 1234, now)
	registry.Observe(terminals.Snapshot{
		TerminalID:    "terminal-1",
		TmuxSession:   "mlp-codex-cli-int-1",
		SessionID:     "session-1",
		ExecutionKind: "main_agent",
		Active:        true,
		UpdatedAt:     now,
	}, now)

	lease, ok := registry.MarkClosed("mlp-codex-cli-int-1", "completed", now)
	if !ok {
		t.Fatal("expected lease to close")
	}
	wantDeadline := now.Add(DefaultClosedLeaseRetention)
	if lease.SnapshotDeadline == nil || !lease.SnapshotDeadline.Equal(wantDeadline) {
		t.Fatalf("snapshot deadline = %v, want %v", lease.SnapshotDeadline, wantDeadline)
	}
	if got := registry.Prune(wantDeadline.Add(time.Second)); got != 1 {
		t.Fatalf("expired prune = %d, want 1", got)
	}
}
