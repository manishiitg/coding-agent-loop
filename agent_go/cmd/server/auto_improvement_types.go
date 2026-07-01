package server

import "time"

// =====================================================================
// Auto-Improvement Framework — shared types
// Schemas: schemas/auto-improvement.schema.json
// Doc:     docs/workflow/auto_improvement_framework.md
// =====================================================================

// OversightMode — per-workflow oversight policy for high-risk framework
// changes.
type OversightMode string

const (
	OversightManual     OversightMode = "manual"
	OversightSupervised OversightMode = "supervised"
	OversightAutonomous OversightMode = "autonomous"
)

// Workflow typology and plan stability are NOT enums anymore. They live as
// prose in builder/improve.html under the "Workflow Profile" section. The agent
// reads improve.html on every improvement turn and adjusts behavior; the
// framework no longer hard-gates on a workflow_type value. Three reasons:
//
//   1. Real workflows mix axes the enum couldn't express (Twitter is both
//      exploratory AND deterministic in dual-mode).
//   2. The hard gates that did matter (allow-list filtering of plan-mod tools)
//      added kernel-level enforcement nobody actually needed — the agent
//      respects guidance in improve.html.
//   3. Prose captures nuance enums can't ("mostly stable but new tactics
//      monthly", "frozen except during compliance reviews").

// PlanStability is retained as a type alias for the field in WorkflowManifest
// during the deprecation window — the framework no longer reads it. New code
// must NOT introduce new readers. Setting the field has no behavioral effect.
type PlanStability string

const (
	PlanStabilityMutable PlanStability = "mutable"
	PlanStabilityRatchet PlanStability = "ratchet"
	PlanStabilityFrozen  PlanStability = "frozen"
)

// DecisionSource — who emitted this decision.
type DecisionSource string

const (
	DecisionSourceAgent  DecisionSource = "agent"
	DecisionSourceUser   DecisionSource = "user"
	DecisionSourceSystem DecisionSource = "system"
)

// DecisionEntry is one structured fenced block in builder/improve.html. Append-only.
//
// User context captures land here too with `Source: user` +
// `Trigger: capture-context` + the context-specific fields below (RuleAdded,
// RuleSection, ExamplePaths). One improve.html ledger, one place to read. Agents
// append these through capture_context when the user confirms durable context
// in chat.
type DecisionEntry struct {
	Ts                  string         `json:"ts"`
	ID                  string         `json:"id"`
	Source              DecisionSource `json:"source"`
	Trigger             string         `json:"trigger"`
	Rationale           string         `json:"rationale,omitempty"`
	AppliedChanges      []string       `json:"applied_changes"`
	LinkedReviewFinding []string       `json:"linked_review_finding,omitempty"`
	LinkedImproveEntry  []string       `json:"linked_improve_entry,omitempty"`
	RegulationRef       string         `json:"regulation_ref,omitempty"`
	EvidencePaths       []string       `json:"evidence_paths,omitempty"`
	Supersedes          string         `json:"supersedes,omitempty"`
	EditedAt            string         `json:"edited_at,omitempty"`
	EditedBy            string         `json:"edited_by,omitempty"`
	// Context-capture fields. Populated when Source=user + Trigger=capture-context.
	RuleAdded    string   `json:"rule_added,omitempty"`
	RuleSection  string   `json:"rule_section,omitempty"`
	ExamplePaths []string `json:"example_paths,omitempty"`
}

// nowUTC returns the current time in ISO-8601 UTC string form.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
