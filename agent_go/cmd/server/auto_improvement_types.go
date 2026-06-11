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
	TargetMetrics       []string       `json:"target_metrics,omitempty"`
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

// MetricDirection — whether higher or lower values are better.
type MetricDirection string

const (
	HigherBetter MetricDirection = "higher_better"
	LowerBetter  MetricDirection = "lower_better"
)

// MetricMode — drive-toward (target) vs stay-above-floor/below-ceiling (slo).
type MetricMode string

const (
	MetricModeTarget MetricMode = "target"
	MetricModeSLO    MetricMode = "slo"
)

// MetricRole describes how strongly a metric should steer improvement.
// Primary metrics are the small set the workflow is truly optimizing for;
// secondary metrics explain, guard, or diagnose movement in primary metrics.
type MetricRole string

const (
	MetricRolePrimary   MetricRole = "primary"
	MetricRoleSecondary MetricRole = "secondary"
)

// MetricSourceType — where a metric value comes from each run.
type MetricSourceType string

const (
	MetricSourceEvalStep  MetricSourceType = "eval_step"
	MetricSourceTelemetry MetricSourceType = "telemetry"
)

// MetricSource describes how a metric's value is resolved per run.
type MetricSource struct {
	Type  MetricSourceType `json:"type"`
	ID    string           `json:"id,omitempty"`
	Field string           `json:"field,omitempty"`
}

// Metric is one active entry in metrics.json::metrics[].
type Metric struct {
	ID        string          `json:"id"`
	Label     string          `json:"label,omitempty"`
	Role      MetricRole      `json:"role,omitempty"`
	Category  string          `json:"category,omitempty"`
	Unit      string          `json:"unit"`
	Direction MetricDirection `json:"direction"`
	Mode      MetricMode      `json:"mode"`
	Target    *float64        `json:"target,omitempty"`
	Floor     *float64        `json:"floor,omitempty"`
	Ceiling   *float64        `json:"ceiling,omitempty"`
	Source    MetricSource    `json:"source"`
	// SuccessCriteria is the soul.md success-criteria text this metric operationalizes.
	// Optional for backward compatibility; UI surfaces a warning when it is missing.
	SuccessCriteria string `json:"success_criteria,omitempty"`
	Parent          string `json:"parent,omitempty"`
	Version         int    `json:"version,omitempty"`
}

// MetricArchiveEntry preserves prior definitions when an active metric is
// amended or retired. The historical metric rows remain in metrics_history;
// this archive explains what those older rows meant.
type MetricArchiveEntry struct {
	ID             string `json:"id"`
	Version        int    `json:"version"`
	ArchivedAt     string `json:"archived_at"`
	ArchivedReason string `json:"archived_reason"`
	Definition     Metric `json:"definition"`
}

// MetricsFile is the shape of <workflow>/planning/metrics.json.
type MetricsFile struct {
	Metrics    []Metric             `json:"metrics"`
	Archive    []MetricArchiveEntry `json:"archive,omitempty"`
	ActiveMode string               `json:"active_mode,omitempty"`
}

// nowUTC returns the current time in ISO-8601 UTC string form.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
