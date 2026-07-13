package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// TestValidatePathAgainstGuard_BlockedWritePaths verifies the write-only deny
// semantic on the Go-side path validator:
//
//   - A path under BlockedWritePaths is allowed to READ.
//   - The same path is denied for WRITE.
//   - Paths not under BlockedWritePaths are unaffected.
//   - BlockedPaths (the hard deny) still denies both reads and writes.
//
// This is the client-side counterpart to the isolator's kernel-level enforcement
// tested in workspace/security/isolator_test.go. Both surfaces must deny the same
// writes for the semantic to be consistent — a Go-client check that was lenient
// here would let raw Go-level file API calls bypass the block even though shell
// commands would still hit the kernel sandbox.
func TestValidatePathAgainstGuard_BlockedWritePaths(t *testing.T) {
	guard := &FolderGuardConfig{
		Enabled:           true,
		WritePaths:        []string{"Workflow/test-ops"},
		BlockedWritePaths: []string{"Workflow/test-ops/planning"},
	}

	cases := []struct {
		name      string
		path      string
		isWrite   bool
		wantError string // substring; empty = expect success
	}{
		{
			name:    "read of blocked-write path is allowed",
			path:    "Workflow/test-ops/planning/plan.json",
			isWrite: false,
		},
		{
			name:    "read of nested file under blocked-write path is allowed",
			path:    "Workflow/test-ops/planning/nested/deep.json",
			isWrite: false,
		},
		{
			name:      "write to blocked-write path is denied",
			path:      "Workflow/test-ops/planning/plan.json",
			isWrite:   true,
			wantError: "blocked for writes",
		},
		{
			name:      "write to nested file under blocked-write path is denied",
			path:      "Workflow/test-ops/planning/nested/deep.json",
			isWrite:   true,
			wantError: "blocked for writes",
		},
		{
			name:    "write to sibling under same workflow root is allowed",
			path:    "Workflow/test-ops/reports/report_plan.md",
			isWrite: true,
		},
		{
			name:    "read from sibling is allowed",
			path:    "Workflow/test-ops/reports/report_plan.md",
			isWrite: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePathAgainstGuard(guard, tc.path, tc.isWrite)
			switch {
			case tc.wantError == "" && err != nil:
				t.Fatalf("expected success for path=%q isWrite=%v, got error: %v", tc.path, tc.isWrite, err)
			case tc.wantError != "" && err == nil:
				t.Fatalf("expected error containing %q for path=%q isWrite=%v, got nil", tc.wantError, tc.path, tc.isWrite)
			case tc.wantError != "" && err != nil && !strings.Contains(err.Error(), tc.wantError):
				t.Fatalf("expected error containing %q, got: %v", tc.wantError, err)
			}
		})
	}
}

func TestValidatePathAgainstGuardEmptyCapabilitiesFailClosed(t *testing.T) {
	guard := &FolderGuardConfig{Enabled: true}
	if err := validatePathAgainstGuard(guard, "Workflow/demo/report.html", false); err == nil || !strings.Contains(err.Error(), "no workspace read paths") {
		t.Fatalf("empty read capability should fail closed, got %v", err)
	}
	if err := validatePathAgainstGuard(guard, "Workflow/demo/report.html", true); err == nil || !strings.Contains(err.Error(), "no workspace write paths") {
		t.Fatalf("empty write capability should fail closed, got %v", err)
	}
}

func TestResolveEffectiveFolderGuardPreservesReadOnlyAndDenyPaths(t *testing.T) {
	sessionID := "read-only-session-guard"
	SetSessionFolderGuard(sessionID, []string{"Workflow/demo"}, nil)
	SetSessionFolderGuardBlockedPaths(sessionID, []string{"Workflow/demo/secrets"})
	SetSessionFolderGuardBlockedWritePaths(sessionID, []string{"Workflow/demo/planning"})
	defer ClearSessionShellConfig(sessionID)

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	guard := NewClient("http://unused").resolveEffectiveFolderGuard(ctx)
	if guard == nil || len(guard.ReadPaths) != 1 || len(guard.WritePaths) != 0 {
		t.Fatalf("read-only guard was not preserved: %#v", guard)
	}
	if len(guard.BlockedPaths) != 1 || len(guard.BlockedWritePaths) != 1 {
		t.Fatalf("deny paths were dropped: %#v", guard)
	}
	if err := validatePathAgainstGuard(guard, "Workflow/demo/report.html", false); err != nil {
		t.Fatalf("granted read should pass: %v", err)
	}
	if err := validatePathAgainstGuard(guard, "Workflow/demo/report.html", true); err == nil {
		t.Fatal("read-only session unexpectedly gained write access")
	}
}

// TestValidatePathAgainstGuard_BlockedPathsStillDeniesBoth asserts that the
// pre-existing BlockedPaths semantic — "deny both reads and writes" — is
// unchanged by the addition of BlockedWritePaths. These are two independent
// primitives and must not interfere.
func TestValidatePathAgainstGuard_BlockedPathsStillDeniesBoth(t *testing.T) {
	guard := &FolderGuardConfig{
		Enabled:      true,
		WritePaths:   []string{"Workflow/test-ops"},
		BlockedPaths: []string{"Workflow/test-ops/secrets"},
	}

	for _, isWrite := range []bool{true, false} {
		name := "read"
		if isWrite {
			name = "write"
		}
		t.Run(name+"_of_blocked_path_is_denied", func(t *testing.T) {
			err := validatePathAgainstGuard(guard, "Workflow/test-ops/secrets/token.txt", isWrite)
			if err == nil {
				t.Fatalf("expected error for %s of blocked path, got nil", name)
			}
			if !strings.Contains(err.Error(), "is blocked") {
				t.Fatalf("expected 'is blocked' error, got: %v", err)
			}
		})
	}
}

func TestValidatePathAgainstGuard_ExactFileWritePathIsNotPrefix(t *testing.T) {
	guard := &FolderGuardConfig{
		Enabled:    true,
		WritePaths: []string{"Workflow/rtslatency/builder/improve.html"},
	}

	cases := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name: "exact improve log is writable",
			path: "Workflow/rtslatency/builder/improve.html",
		},
		{
			name:      "sibling builder file is not writable",
			path:      "Workflow/rtslatency/builder/other.html",
			wantError: true,
		},
		{
			name:      "file path is not treated as writable directory prefix",
			path:      "Workflow/rtslatency/builder/improve.html/child.html",
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePathAgainstGuard(guard, tc.path, true)
			if tc.wantError && err == nil {
				t.Fatalf("expected write to %q to be blocked", tc.path)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("expected write to %q to be allowed, got: %v", tc.path, err)
			}
		})
	}
}
