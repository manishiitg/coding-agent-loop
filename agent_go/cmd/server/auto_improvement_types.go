package server

import "time"

// =====================================================================
// Auto-Improvement Framework — shared types
// Schemas: schemas/auto-improvement.schema.json
// Doc:     docs/workflow/auto_improvement_framework.md
// =====================================================================

// OversightMode — per-workflow oversight policy. Hard gate: drives auto-vs-
// human-approval flow on hypothesis acceptance and verdict commitment.
type OversightMode string

const (
	OversightManual     OversightMode = "manual"
	OversightSupervised OversightMode = "supervised"
	OversightAutonomous OversightMode = "autonomous"
)

// Workflow typology and plan stability are NOT enums anymore. They live as
// prose in builder/improve.md under the "Workflow Profile" section. The agent
// reads improve.md on every improvement turn and adjusts behavior; the
// framework no longer hard-gates on a workflow_type value. Three reasons:
//
//   1. Real workflows mix axes the enum couldn't express (Twitter is both
//      exploratory AND deterministic in dual-mode).
//   2. The hard gates that did matter (allow-list filtering of plan-mod tools)
//      added kernel-level enforcement nobody actually needed — the agent
//      respects guidance in improve.md.
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

// DecisionLogMutability — controls whether decision-log entries can be edited.
type DecisionLogMutability string

const (
	DecisionLogAppendOnly       DecisionLogMutability = "append_only"
	DecisionLogAppendOnlyStrict DecisionLogMutability = "append_only_strict"
)

// DecisionSource — who emitted this decision.
type DecisionSource string

const (
	DecisionSourceAgent  DecisionSource = "agent"
	DecisionSourceUser   DecisionSource = "user"
	DecisionSourceSystem DecisionSource = "system"
)

// DecisionEntry is one line of builder/decisions.jsonl. Append-only.
//
// Type-3 rule captures (formerly stored in context/clarifications.jsonl) now
// land here too with `Source: user` + `Trigger: capture-context` + the
// rule-specific fields below (RuleAdded, RuleSection, ExamplePaths). One
// audit log, one place to read.
type DecisionEntry struct {
	Ts                 string         `json:"ts"`
	ID                 string         `json:"id"`
	Source             DecisionSource `json:"source"`
	Trigger            string         `json:"trigger"`
	Rationale          string         `json:"rationale,omitempty"`
	AppliedChanges     []string       `json:"applied_changes"`
	TargetMetrics      []string       `json:"target_metrics,omitempty"`
	LinkedExperimentID string         `json:"linked_experiment_id,omitempty"`
	RegulationRef      string         `json:"regulation_ref,omitempty"`
	EvidencePaths      []string       `json:"evidence_paths,omitempty"`
	Supersedes         string         `json:"supersedes,omitempty"`
	EditedAt           string         `json:"edited_at,omitempty"`
	EditedBy           string         `json:"edited_by,omitempty"`
	// Rule-capture fields. Populated when Source=user + Trigger=capture-context.
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

// MetricSourceType — where a metric value comes from each run.
type MetricSourceType string

const (
	MetricSourceEvalStep            MetricSourceType = "eval_step"
	MetricSourceTelemetry           MetricSourceType = "telemetry"
	MetricSourceExternal            MetricSourceType = "external"
	MetricSourceDelayedGroundTruth  MetricSourceType = "delayed_ground_truth"
	MetricSourceLineage             MetricSourceType = "lineage"
	MetricSourceSchemaCheck         MetricSourceType = "schema_check"
)

// MetricSource describes how a metric's value is resolved per run.
type MetricSource struct {
	Type      MetricSourceType `json:"type"`
	ID        string           `json:"id,omitempty"`
	Field     string           `json:"field,omitempty"`
	JoinedVia string           `json:"joined_via,omitempty"`
}

// Metric is one entry in metrics.json::metrics[].
type Metric struct {
	ID              string          `json:"id"`
	Label           string          `json:"label,omitempty"`
	Unit            string          `json:"unit"`
	Direction       MetricDirection `json:"direction"`
	Mode            MetricMode      `json:"mode"`
	Target          *float64        `json:"target,omitempty"`
	Floor           *float64        `json:"floor,omitempty"`
	Ceiling         *float64        `json:"ceiling,omitempty"`
	Source          MetricSource    `json:"source"`
	EvaluableAtLag  string          `json:"evaluable_at_lag,omitempty"`
	Parent          string          `json:"parent,omitempty"`
	Version         int             `json:"version,omitempty"`
	// LinkedSuccessCriteria traces this metric to one or more entries in the
	// plan's success_criteria. Closes the Goodhart loop: an experiment moves a
	// metric, the metric operationalizes a criterion, the criterion is the
	// outcome the workflow is meant to achieve. Empty means the metric is
	// auxiliary (telemetry like cost/duration), not tied to a user-facing
	// outcome — surfaced as a warning in the UI but not blocked.
	LinkedSuccessCriteria []string `json:"linked_success_criteria,omitempty"`
}

// MetricArchiveEntry preserves prior versions of amended metrics.
type MetricArchiveEntry struct {
	ID             string    `json:"id"`
	Version        int       `json:"version"`
	ArchivedAt     string    `json:"archived_at"`
	ArchivedReason string    `json:"archived_reason"`
	Definition     Metric    `json:"definition"`
}

// MetricsFile is the shape of <workflow>/metrics.json.
//
// `ActiveMode` is the runtime state for dual-mode workflows (e.g. Twitter
// explore/exploit cycles). When the workflow's improve.md declares dual mode,
// the active value lives here so steps can branch on it via the variable
// resolver. Workflows that don't declare dual mode leave this empty.
type MetricsFile struct {
	Metrics    []Metric             `json:"metrics"`
	Archive    []MetricArchiveEntry `json:"archive,omitempty"`
	ActiveMode string               `json:"active_mode,omitempty"`
}

// ExperimentStatus — the experiment state machine.
type ExperimentStatus string

const (
	ExpStatusProposed                  ExperimentStatus = "proposed"
	ExpStatusAwaitingApproval          ExperimentStatus = "awaiting-approval"
	ExpStatusMeasuring                 ExperimentStatus = "measuring"
	ExpStatusEvaluating                ExperimentStatus = "evaluating"
	ExpStatusAwaitingConclusionApproval ExperimentStatus = "awaiting-conclusion-approval"
	ExpStatusConcluded                 ExperimentStatus = "concluded"
	ExpStatusAborted                   ExperimentStatus = "aborted"
)

// Verdict — outcome of an experiment.
type Verdict string

const (
	VerdictKept         Verdict = "kept"
	VerdictReverted     Verdict = "reverted"
	VerdictInconclusive Verdict = "inconclusive"
	VerdictExtend       Verdict = "extend"
)

// ExpectedDirection — direction of the predicted metric movement.
type ExpectedDirection string

const (
	DirectionIncrease ExpectedDirection = "increase"
	DirectionDecrease ExpectedDirection = "decrease"
	DirectionMaintain ExpectedDirection = "maintain"
)

// InterventionOperation — kind of file write the intervention applies.
type InterventionOperation string

const (
	OpAppend  InterventionOperation = "append"
	OpReplace InterventionOperation = "replace"
	OpPatch   InterventionOperation = "patch"
	OpCreate  InterventionOperation = "create"
)

// InterventionChange describes a single file write that the experiment performs.
type InterventionChange struct {
	Path      string                `json:"path"`
	Operation InterventionOperation `json:"operation"`
	Content   string                `json:"content"`
}

// WorldStateSnapshot captures runtime state at a point in time. Used for drift detection.
type WorldStateSnapshot struct {
	CapturedAt       string            `json:"captured_at"`
	ModelVersions    map[string]string `json:"model_versions,omitempty"`
	MCPVersions      map[string]string `json:"mcp_versions,omitempty"`
	SourceDataHashes map[string]string `json:"source_data_hashes,omitempty"`
}

// ExperimentBaseline is the pre-intervention reference window.
type ExperimentBaseline struct {
	Window       string                 `json:"window"`
	Values       map[string][]float64   `json:"values"`
	Mean         map[string]float64     `json:"mean"`
	Std          map[string]float64     `json:"std,omitempty"`
	Insufficient bool                   `json:"insufficient,omitempty"`
}

// ExperimentIntervention captures what was changed and how to revert.
type ExperimentIntervention struct {
	Trigger             string   `json:"trigger"`
	AppliedChanges      []string `json:"applied_changes"`
	RevertableDiffPath  string   `json:"revertable_diff_path"`
}

// ExperimentMeasurement holds the post-intervention data window.
//
// ContributedRuns is the dedupe list. RecordMeasurement appends a run folder
// here after pulling values from it; subsequent calls for the same run folder
// (e.g. when eval scoring is re-run on the same iteration) skip — values
// already counted, no double-counting.
type ExperimentMeasurement struct {
	TargetRuns      int                  `json:"target_runs"`
	CompletedRuns   int                  `json:"completed_runs"`
	Values          map[string][]float64 `json:"values"`
	ContributedRuns []string             `json:"contributed_runs,omitempty"`
}

// ExperimentWorldState pairs the start/end snapshots.
type ExperimentWorldState struct {
	StartedAt    *WorldStateSnapshot `json:"started_at,omitempty"`
	ConcludedAt  *WorldStateSnapshot `json:"concluded_at,omitempty"`
}

// ExperimentEvidence is the numeric evidence produced by compute_verdict.
//
// PerMetricVerdict carries the per-metric verdict before they were combined
// into the overall verdict, so the evaluator (and the UI) can show "outcome
// kept; cost regressed" without re-deriving from the raw numbers.
//
// CostWarning is set when the overall verdict is `kept` because the outcome
// metric(s) were kept, but at least one telemetry SLO (cost_per_run /
// run_duration_seconds / any unanchored metric) regressed. The change still
// stays applied — success criteria are paramount — but the evaluator's
// rationale should call out the cost or runtime hit so the operator can
// decide whether to revisit.
type ExperimentEvidence struct {
	PostMean          map[string]float64 `json:"post_mean,omitempty"`
	MagnitudeObserved map[string]float64 `json:"magnitude_observed,omitempty"`
	PerRunBeatPct     map[string]float64 `json:"per_run_beat_pct,omitempty"`
	PerMetricVerdict  map[string]Verdict `json:"per_metric_verdict,omitempty"`
	DriftFlagged      bool               `json:"drift_flagged,omitempty"`
	CostWarning       bool               `json:"cost_warning,omitempty"`
}

// ExperimentConclusion is filled in at the end of an experiment.
type ExperimentConclusion struct {
	Verdict           Verdict             `json:"verdict,omitempty"`
	Rationale         string              `json:"rationale,omitempty"`
	Evidence          *ExperimentEvidence `json:"evidence,omitempty"`
	VerdictOverridden bool                `json:"verdict_overridden,omitempty"`
	OverrideReason    string              `json:"override_reason,omitempty"`
}

// ExperimentApprovals tracks who approved which gate.
type ExperimentApprovals struct {
	HypothesisApprovedBy  string `json:"hypothesis_approved_by,omitempty"`
	HypothesisApprovedAt  string `json:"hypothesis_approved_at,omitempty"`
	ConclusionApprovedBy  string `json:"conclusion_approved_by,omitempty"`
	ConclusionApprovedAt  string `json:"conclusion_approved_at,omitempty"`
}

// ExperimentRecord is the durable record stored in active.json (in progress) or history.jsonl (concluded).
type ExperimentRecord struct {
	ID                 string                  `json:"id"`
	Status             ExperimentStatus        `json:"status"`
	Hypothesis         string                  `json:"hypothesis"`
	TargetMetrics      []string                `json:"target_metrics"`
	ExpectedDirection  ExpectedDirection       `json:"expected_direction"`
	ExpectedMagnitude  float64                 `json:"expected_magnitude"`
	Baseline           ExperimentBaseline      `json:"baseline"`
	Intervention       ExperimentIntervention  `json:"intervention"`
	Measurement        ExperimentMeasurement   `json:"measurement"`
	WorldState         ExperimentWorldState    `json:"world_state"`
	StartedAt          string                  `json:"started_at"`
	ConcludedAt        string                  `json:"concluded_at,omitempty"`
	Conclusion         *ExperimentConclusion   `json:"conclusion"`
	Approvals          ExperimentApprovals     `json:"approvals"`
	LinkedDecisions    []string                `json:"linked_decisions,omitempty"`
}

// ExperimentsActiveFile is the shape of experiments/active.json (a list of in-flight experiments).
type ExperimentsActiveFile struct {
	Experiments []ExperimentRecord `json:"experiments"`
	UpdatedAt   string             `json:"updated_at"`
}

// VerdictThresholds are the heuristics used by compute_verdict.
type VerdictThresholds struct {
	KeptMagnitudePct        float64 `json:"kept_magnitude_pct,omitempty"`
	KeptPerRunBeatPct       float64 `json:"kept_per_run_beat_pct,omitempty"`
	RevertedPerRunBeatPct   float64 `json:"reverted_per_run_beat_pct,omitempty"`
	NoiseBandStdMultiplier  float64 `json:"noise_band_std_multiplier,omitempty"`
}

// PinnedHypothesis records hypotheses the optimizer must not retry.
type PinnedHypothesis struct {
	Text     string `json:"text"`
	Reason   string `json:"reason"`
	PinnedAt string `json:"pinned_at"`
	PinnedBy string `json:"pinned_by,omitempty"`
}

// ExperimentsConfig is experiments/config.json.
type ExperimentsConfig struct {
	DefaultMeasurementRuns      int                `json:"default_measurement_runs"`
	MinRuns                     int                `json:"min_runs,omitempty"`
	MaxRuns                     int                `json:"max_runs,omitempty"`
	BaselineWindow              int                `json:"baseline_window"`
	AllowedInterventionPaths    []string           `json:"allowed_intervention_paths"`
	ForbiddenInterventionPaths  []string           `json:"forbidden_intervention_paths,omitempty"`
	VerdictThresholds           *VerdictThresholds `json:"verdict_thresholds,omitempty"`
	CooldownHours               float64            `json:"cooldown_hours,omitempty"`
	MaxConcurrentExperiments    int                `json:"max_concurrent_experiments,omitempty"`
	HighRiskPaths               []string           `json:"high_risk_paths,omitempty"`
	PinnedHypotheses            []PinnedHypothesis `json:"pinned_hypotheses,omitempty"`
	FocusMetrics                []string           `json:"focus_metrics,omitempty"`
}

// DefaultExperimentsConfig returns sensible defaults for a new workflow.
func DefaultExperimentsConfig() ExperimentsConfig {
	return ExperimentsConfig{
		DefaultMeasurementRuns: 5,
		MinRuns:                3,
		MaxRuns:                30,
		BaselineWindow:         5,
		AllowedInterventionPaths: []string{
			"knowledgebase/rules/",
			"knowledgebase/rules/rules.md",
			"knowledgebase/rules/examples/",
			"evaluation/",
			"evaluation/evaluation_plan.json",
			"evaluation/step_config.json",
			"planning/step_config.json",
			"planning/metrics.json",
		},
		ForbiddenInterventionPaths: []string{
			".env",
			".git/",
			"workflow.json",
		},
		VerdictThresholds: &VerdictThresholds{
			KeptMagnitudePct:       0.5,
			KeptPerRunBeatPct:      0.7,
			RevertedPerRunBeatPct:  0.5,
			NoiseBandStdMultiplier: 1.0,
		},
		CooldownHours:            24,
		MaxConcurrentExperiments: 1,
	}
}

// nowUTC returns the current time in ISO-8601 UTC string form.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
