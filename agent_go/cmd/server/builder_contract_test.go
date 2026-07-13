package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// knownBuilderInvariantGaps tracks invariants enumerated in builder_contract.go
// that don't yet have an executable certification entry. Listed IDs are
// TOLERATED — they're a public TODO list, not silently ignored. Removing an
// ID from this map without adding the certification entry will fail
// TestAllBuilderInvariantsHaveRegisteredCertification.
//
// History: until this commit, the builder-layer contract lived only in
// markdown (docs/core/coding_agent_builder_e2e_contract.md). Adding rows to
// that doc carried no enforcement, so docs and code drifted independently.
// The IDs below are the rows we extracted from the doc but haven't yet
// linked to a real test file — make this list shorter, not longer, over time.
var knownBuilderInvariantGaps = map[BuilderInvariantID]struct{}{
	InvCWDMatching:                          {},
	InvMCPBridgeSessionScoped:               {},
	InvTmuxLifecyclePolicy:                  {},
	InvTerminalRetentionWindow:              {},
	InvTerminalSelectionStable:              {},
	InvTerminalOwnerReconciliation:          {},
	InvTerminalScrollPreserved:              {},
	InvTerminalDebugIDsCopyable:             {},
	InvProviderVsTerminalCompletionSeparate: {},
	InvChatLaunch:                           {},
	InvTmuxLossContinuation:                 {},
	InvLiteralPromptText:                    {},
	InvStalePromptDraft:                     {},
	InvLiveSteerSameSession:                 {},
	InvCancellationProducesEvent:            {},
	InvCancelDoesNotReusePane:               {},
	InvQueryStepResolution:                  {},
	InvTodoOrchestratorParallel:             {},
	InvBackgroundAgentVisibility:            {},
	InvTerminalCenterStates:                 {},
	InvTerminalDismissAPI:                   {},
	InvHistoryResume:                        {},
	InvUIFormattingSeparation:               {},
	InvTerminalDynamicResize:                {},
	// Now certified (have entries in builderInvariantCertifications):
	//   InvWorkflowDependencyPreflight, InvTerminalCtrlCDelivery,
	//   InvMultiTurnMemory, InvTerminalLifecycleAPI,
	//   InvWorkflowStepCwdMCP, InvNoDuplicateUnifiedCompletion
}

// TestAllBuilderInvariantsHaveRegisteredCertification mirrors the provider-
// layer pattern from multi-llm-provider-go: every invariant ID must either
// have an executable cert entry or be explicitly listed in
// knownBuilderInvariantGaps. Drift in either direction fails the test.
//
// Why: the docs at docs/core/coding_agent_builder_e2e_contract.md describe
// ~25 user-facing invariants. Until this test landed there was no automatic
// way to ensure each had a test backing it — markdown is free-form and
// nobody noticed when a doc row got changed but no test was added.
func TestAllBuilderInvariantsHaveRegisteredCertification(t *testing.T) {
	for _, id := range AllBuilderInvariants() {
		_, hasCert := builderInvariantCertifications[id]
		_, hasGap := knownBuilderInvariantGaps[id]
		if !hasCert && !hasGap {
			t.Errorf("builder invariant %q has neither a certification nor a knownBuilderInvariantGaps allowance — write the e2e test and register it, or add an allowance entry while filing a follow-up task",
				id)
		}
		if hasCert && hasGap {
			t.Errorf("builder invariant %q is both certified AND in knownBuilderInvariantGaps — pick one. If the cert is real, drop the allowance; if the cert is stale, remove the cert.",
				id)
		}
	}
	// Inverse: no certified ID may be left in the gap list (catches "added
	// cert but forgot to remove allowance"). Iterating the cert map already
	// covers this — duplicated above for clarity since the failure message
	// pinpoints the stale entry.
	for id := range builderInvariantCertifications {
		if _, ok := knownBuilderInvariantGaps[id]; ok {
			t.Errorf("knownBuilderInvariantGaps lists %q but the certification is now registered — remove the allowance", id)
		}
	}
}

// TestBuilderInvariantCertificationReferencesExistingFiles asserts every
// registered cert points at a real file in the repo. Catches typos in
// TestFile / TestName + tests that get renamed or deleted without updating
// the cert.
//
// Resolves repo root by walking up from agent_go/cmd/server until it finds
// the repo's outer directory (which contains both agent_go/ and frontend/).
// This lets the test run from any cwd Go test happens to choose.
func TestBuilderInvariantCertificationReferencesExistingFiles(t *testing.T) {
	repoRoot := findRepoRoot(t)
	for id, cert := range builderInvariantCertifications {
		if strings.TrimSpace(cert.TestFile) == "" {
			t.Errorf("invariant %q has empty TestFile", id)
			continue
		}
		if strings.TrimSpace(cert.TestName) == "" {
			t.Errorf("invariant %q has empty TestName", id)
			continue
		}
		absPath := filepath.Join(repoRoot, cert.TestFile)
		raw, err := os.ReadFile(absPath)
		if err != nil {
			t.Errorf("invariant %q references unreadable file %s: %v", id, cert.TestFile, err)
			continue
		}
		// The Cert.TestName may name either a test function (`func TestX(`)
		// OR a production-code function/method that is the proof itself
		// (e.g. validateWorkflowDependencies, sendTerminalKey). Accept
		// either by looking for `func TestName(` anywhere — whitespace
		// between `func` and the name is tolerated.
		needle := "func " + cert.TestName + "("
		needleMethod := ") " + cert.TestName + "("
		if !strings.Contains(string(raw), needle) && !strings.Contains(string(raw), needleMethod) {
			t.Errorf("invariant %q references missing function %s in %s",
				id, cert.TestName, cert.TestFile)
		}
	}
}

// findRepoRoot walks up from this test's working dir until it finds a
// directory containing both agent_go/ and frontend/ (coding-agent-loop's
// repo root). Falls back to t.Fatal if not found within 8 levels — well
// beyond what any reasonable Go test workdir nests.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "agent_go")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "frontend")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate coding-agent-loop repo root from %s", cwd)
	return ""
}
