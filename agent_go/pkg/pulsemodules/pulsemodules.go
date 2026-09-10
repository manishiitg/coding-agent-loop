// Package pulsemodules is the single canonical registry of Pulse review
// module identity.
//
// Why this package exists, and why it is a dependency-free leaf:
//
// Module identity was previously restated in nine independently maintained
// places — scheduler constants, an order slice, a validity map, an alias
// normalizer, a scheduler step-label switch, a tool enum, that tool's prose
// description, a reviewer artifact-path whitelist, and two frontend files.
// Nothing asserted they agreed.
//
// A 2026-07-29 refactor merged several maintenance lenses without updating all
// consumers. Two production failures followed: a missing ToolCategories entry
// blocked every todo_task orchestrator, and a missing reviewer-whitelist entry
// silently discarded a whole review result. Both built cleanly and passed the
// full test suite.
//
// The registry is a leaf package with no imports because both cmd/server and
// pkg/orchestrator/.../step_based_workflow must consume it, and server already
// imports step_based_workflow — so the shared truth cannot live in either.
package pulsemodules

import "strings"

// Module is one scheduled Pulse review module.
type Module struct {
	// ID is the canonical identifier used in db state, tool payloads,
	// reviewer artifact paths, and HTML data-module attributes.
	ID string
	// Label is the plain-language name shown to operators.
	Label string
	// StepLabel is the scheduler's per-stage label for this module.
	StepLabel string
	// Aliases are shorthand or superseded spellings that normalize to ID.
	// These are accepted as input; they are never emitted.
	Aliases []string
}

// Canonical module IDs. Consumers that need compile-time constants must alias
// these values rather than restating their string literals.
const (
	TechnicalReviewID    = "technical_review"
	ArchitectureReviewID = "architecture_review"
	StrategicReviewID    = "strategic_review"
	PlanDriftReviewID    = "plan_drift_review"

	// Legacy review IDs are accepted only at persistence/read boundaries so
	// existing workflow databases can be migrated into the canonical review
	// identities. New worklists, receipts, findings, and UI projections must
	// never emit them.
	LegacyWorkflowReviewID  = "workflow_review"
	LegacyLLMOpsReviewID    = "llm_ops_review"
	LegacyStrategyAuditorID = "strategy_auditor"
	LegacyGoalAdvisorID     = "goal_advisor"
)

// HTML-only classifications. They are not scheduled review modules.
const (
	PseudoRunSummaryID = "run_summary"
)

// All is the canonical, ordered module set. Order is the Pulse worklist order
// and is part of the contract — the scheduler and UI both rely on it.
var All = []Module{
	{
		// Technical Review is one retained reviewer sequence with an agent-chosen
		// deep lens. Engineering correctness, store integrity, runtime operations,
		// model/tier fitness, orchestration shape, and execution efficiency are
		// lenses within this module rather than competing durable identities.
		ID:        TechnicalReviewID,
		Label:     "Technical review",
		StepLabel: "technical-review",
		Aliases: []string{
			"technical", "engineering", "engineering_review", "correctness",
			"correctness_review", "ops", "operations",
			LegacyWorkflowReviewID, LegacyLLMOpsReviewID,
		},
	},
	{
		ID:        ArchitectureReviewID,
		Label:     "Architecture review",
		StepLabel: "architecture-review",
		Aliases:   []string{"architecture", "workflow_improvement"},
	},
	{
		// Strategic Review owns both causal criticism of the current strategy
		// and, when Gate evidence warrants it, independent discovery of a
		// materially different approach. Keeping those as turns in one sequence
		// lets the critic compare alternatives against the same evidence without
		// creating two competing durable module identities.
		ID:        StrategicReviewID,
		Label:     "Strategic review",
		StepLabel: "strategic-review",
		Aliases: []string{
			"strategy", "strategy_review", "plan_effectiveness", "advisor",
			LegacyStrategyAuditorID, LegacyGoalAdvisorID,
		},
	},
	{
		// Plan Drift Review is event-triggered rather than time-cadenced: it is
		// due whenever any step has no step_config.json drift_review record, or
		// one flagged needs_review==true (flagged by the same hook that flags
		// description_reviewed stale on any persisted plan-step field change),
		// not on a fixed interval. Evidence from a prior review is preserved on
		// the flag, not discarded. See validatePlanDriftRouting in
		// pulse_worklist.go for the deterministic force-due enforcement,
		// mirroring validateDeterministicIntakeRouting's treatment of
		// technical_review.
		ID:        PlanDriftReviewID,
		Label:     "Plan drift review",
		StepLabel: "plan-drift-review",
		Aliases:   []string{"drift_review", "plan_drift"},
	},
}

// PseudoIDs are data-module values that appear in builder/improve.html but are
// not scheduled review modules. run_summary covers Gate and run rows; fixes
// and decisions belong to their actual Technical or Strategic
// Review source rather than a synthetic "Pulse fixer" lane.
var PseudoIDs = []string{PseudoRunSummaryID}

// IDs returns the canonical module IDs in worklist order.
func IDs() []string {
	out := make([]string, 0, len(All))
	for _, m := range All {
		out = append(out, m.ID)
	}
	return out
}

// IsValid reports whether id is a currently scheduled canonical module.
func IsValid(id string) bool {
	for _, m := range All {
		if m.ID == id {
			return true
		}
	}
	return false
}

// Normalize maps current shorthand, loosely-cased spellings, and retired
// review identities onto a canonical ID. Callers accept the old
// values for migration only; all output uses the canonical ID.
func Normalize(module string) string {
	module = strings.ToLower(strings.TrimSpace(module))
	module = strings.ReplaceAll(module, "-", "_")
	for _, m := range All {
		if m.ID == module {
			return m.ID
		}
		for _, a := range m.Aliases {
			if a == module {
				return m.ID
			}
		}
	}
	return module
}

// ForStepLabel maps a scheduler stage label to its canonical module ID, or ""
// when the label is not a module stage (for example "gate" or "finalize").
func ForStepLabel(label string) string {
	for _, m := range All {
		if m.StepLabel == label {
			return m.ID
		}
	}
	return ""
}

// AcceptedForReviewReceipts is the current module set accepted for durable
// reviewer receipts.
func AcceptedForReviewReceipts() []string {
	return IDs()
}
