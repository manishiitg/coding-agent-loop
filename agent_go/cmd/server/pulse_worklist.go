package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/loopclosure"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/pulseintake"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/pulsemodules"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	// pulseModuleTechnicalReview is one durable review identity with distinct
	// agent-selected lenses for correctness, stores, runtime operations,
	// orchestration shape, model/tier fitness, cost, and execution efficiency.
	// Engineering and Operations remain useful lens names, not separate queues.
	pulseModuleTechnicalReview = pulsemodules.TechnicalReviewID
	// Legacy constants remain available to migration tests and readers. Runtime
	// worklists and new writes use pulseModuleTechnicalReview exclusively.
	pulseModuleWorkflowReview = pulsemodules.LegacyWorkflowReviewID
	pulseModuleLLMOpsReview   = pulsemodules.LegacyLLMOpsReviewID
	// pulseModuleStrategicReview owns both hidden-mechanism review of the
	// current strategy and conditional discovery of materially different
	// approaches. Those are sequence turns, not independent modules.
	pulseModuleStrategicReview = pulsemodules.StrategicReviewID
	// pulseModulePlanDriftReview is event-triggered, not time-cadenced: it is
	// due whenever any step has no drift_review record, or one flagged
	// needs_review==true. See validatePlanDriftRouting below.
	pulseModulePlanDriftReview = pulsemodules.PlanDriftReviewID
)

// Derived from the canonical registry — see pkg/pulsemodules. Do not restate
// the module set here; a hand-maintained second copy is exactly what caused
// the 2026-07-29 desync.
var pulseModuleOrder = pulsemodules.IDs()

// Focus catalogs define durable coverage identities, not review decisions.
// Gate and reviewers still use evidence and judgment to choose what is due.
var pulseReviewFocusCatalog = map[string][]string{
	pulseModuleTechnicalReview: {
		"execution_health", "validation_contract_health", "plan_orchestration_integrity", "store_integrity",
		"report_quality_truth", "evaluation_quality_truth", "model_cost_fitness",
	},
	pulseModuleStrategicReview: {
		"goal_measurement_validity", "strategy_effectiveness", "feedback_loops_bias",
		"concentration_saturation", "alternative_headroom", "experiment_impact",
	},
}

var validPulseModules = func() map[string]bool {
	m := make(map[string]bool, len(pulsemodules.All))
	for _, id := range pulsemodules.IDs() {
		m[id] = true
	}
	return m
}()

// pulseModuleList renders the closed module set for rejection messages. A
// rejection that names the bad value but not the set it had to come from
// leaves the caller guessing, which is how the same invalid value gets retried.
func pulseModuleList() string {
	return strings.Join(pulseModuleOrder, ", ")
}

func pulseModuleExample() string {
	if len(pulseModuleOrder) > 0 {
		return pulseModuleOrder[0]
	}
	return pulseModuleTechnicalReview
}

// pulseModuleResultValues is the module-audit result set shared by the accept
// check and its rejection message.
var pulseModuleResultValues = []string{"done", "changed", "blocked", "failed", "skipped"}

// pulseSchedulerModuleResultValues additionally accepts the scheduler-only
// timed_out result, which agents cannot report for themselves.
var pulseSchedulerModuleResultValues = append(append([]string(nil), pulseModuleResultValues...), "timed_out")

const pulseModuleStateSchema = `CREATE TABLE IF NOT EXISTS pulse_module_state (
	workspace_path TEXT NOT NULL,
	module TEXT NOT NULL,
	last_pulse_run_id TEXT NOT NULL DEFAULT '',
	last_checked_at TEXT NOT NULL DEFAULT '',
	last_ran_at TEXT NOT NULL DEFAULT '',
	last_decision TEXT NOT NULL DEFAULT '',
	last_reason TEXT NOT NULL DEFAULT '',
	last_gate_decision TEXT NOT NULL DEFAULT '',
	last_result TEXT NOT NULL DEFAULT '',
	last_result_reason TEXT NOT NULL DEFAULT '',
	next_check_at TEXT NOT NULL DEFAULT '',
	next_check_after_run_id TEXT NOT NULL DEFAULT '',
	cooldown_runs INTEGER NOT NULL DEFAULT 0,
	evidence_json TEXT NOT NULL DEFAULT '[]',
	updated_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, module)
)`

const pulseModuleAuditSchema = `CREATE TABLE IF NOT EXISTS pulse_module_audit (
	workspace_path TEXT NOT NULL,
	module TEXT NOT NULL,
	pulse_run_id TEXT NOT NULL,
	result TEXT NOT NULL,
	reason TEXT NOT NULL,
	evidence_json TEXT NOT NULL DEFAULT '[]',
	changed_files_json TEXT NOT NULL DEFAULT '[]',
	verification_json TEXT NOT NULL DEFAULT '[]',
	before_refs_json TEXT NOT NULL DEFAULT '[]',
	after_refs_json TEXT NOT NULL DEFAULT '[]',
	recorded_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, module, pulse_run_id)
)`

// pulse_review_recovery is the scheduler-owned handoff between an interrupted
// reviewer and the next Gate. A checkpoint alone is only working memory: if a
// child exits before it writes record_pulse_result, later Gates previously had
// no durable fact requiring them to resume it.
const pulseReviewRecoverySchema = `CREATE TABLE IF NOT EXISTS pulse_review_recovery (
	workspace_path TEXT NOT NULL,
	module TEXT NOT NULL,
	source_pulse_run_id TEXT NOT NULL,
	checkpoint_path TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'incomplete',
	reason TEXT NOT NULL,
	detected_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, module)
)`

type PulseReviewRecovery struct {
	WorkspacePath    string `json:"workspace_path"`
	Module           string `json:"module"`
	SourcePulseRunID string `json:"source_pulse_run_id"`
	CheckpointPath   string `json:"checkpoint_path"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	DetectedAt       string `json:"detected_at"`
	UpdatedAt        string `json:"updated_at"`
}

// pulseRunModeSchema stores the Gate's agent-selected shape for one Pulse pass.
// It is deliberately separate from per-module cadence: mode explains *how* the
// selected work should run, while module rows explain *which* perspectives are due.
const pulseRunModeSchema = `CREATE TABLE IF NOT EXISTS pulse_run_mode (
	workspace_path TEXT NOT NULL,
	pulse_run_id TEXT NOT NULL,
	mode TEXT NOT NULL,
	reason TEXT NOT NULL,
	recorded_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, pulse_run_id)
)`

// Pulse review focus is deliberately separate from module cadence and reviewer
// receipts. Module state answers whether Technical Review is due; focus state
// answers which bounded deep lenses were examined inside that module. A review
// may select more than one lens when distinct routes or evidence boundaries
// justify the extra work; route scope is retained so a small route does not
// make a materially different large route look reviewed.
// The Markdown checkpoint remains per-run working memory only.
const pulseReviewFocusStateSchema = `CREATE TABLE IF NOT EXISTS pulse_review_focus_state (
	workspace_path TEXT NOT NULL,
	module TEXT NOT NULL,
	focus_key TEXT NOT NULL,
	last_pulse_run_id TEXT NOT NULL DEFAULT '',
	last_reviewed_at TEXT NOT NULL DEFAULT '',
	last_verdict TEXT NOT NULL DEFAULT '',
	last_selection_reason TEXT NOT NULL DEFAULT '',
	last_route_scope TEXT NOT NULL DEFAULT '',
	next_check_at TEXT NOT NULL DEFAULT '',
	next_check_reason TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, module, focus_key)
)`

const pulseReviewFocusHistorySchema = `CREATE TABLE IF NOT EXISTS pulse_review_focus_history (
	_id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_path TEXT NOT NULL,
	module TEXT NOT NULL,
	pulse_run_id TEXT NOT NULL,
	focus_key TEXT NOT NULL,
	priority_class TEXT NOT NULL,
	selection_reason TEXT NOT NULL,
	route_scope TEXT NOT NULL DEFAULT '',
	verdict TEXT NOT NULL DEFAULT '',
	evidence_json TEXT NOT NULL DEFAULT '[]',
	issue_ids_json TEXT NOT NULL DEFAULT '[]',
	deferred_focuses_json TEXT NOT NULL DEFAULT '[]',
	recorded_at TEXT NOT NULL
)`

const (
	pulseRunModeBacklogDrain = "backlog_drain"
	pulseRunModeDiscovery    = "discovery"
	pulseRunModeStrategy     = "strategy"
	pulseRunModeObserve      = "observe"
)

var pulseRunModeValues = []string{
	pulseRunModeBacklogDrain,
	pulseRunModeDiscovery,
	pulseRunModeStrategy,
	pulseRunModeObserve,
}

const pulseShadowSignalObservationSchema = `CREATE TABLE IF NOT EXISTS pulse_shadow_signal_observation (
	workspace_path TEXT NOT NULL,
	pulse_run_id TEXT NOT NULL,
	detector TEXT NOT NULL,
	detector_version TEXT NOT NULL,
	observed_at TEXT NOT NULL,
	coverage_status TEXT NOT NULL,
	coverage_reason TEXT NOT NULL DEFAULT '',
	signals_json TEXT NOT NULL DEFAULT '[]',
	gate_decisions_json TEXT NOT NULL DEFAULT '[]',
	recorded_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, pulse_run_id, detector)
)`

type PulseModuleState struct {
	WorkspacePath       string   `json:"workspace_path"`
	Module              string   `json:"module"`
	LastPulseRunID      string   `json:"last_pulse_run_id,omitempty"`
	LastCheckedAt       string   `json:"last_checked_at,omitempty"`
	LastRanAt           string   `json:"last_ran_at,omitempty"`
	LastDecision        string   `json:"last_decision,omitempty"`
	LastReason          string   `json:"last_reason,omitempty"`
	LastGateDecision    string   `json:"last_gate_decision,omitempty"`
	LastResult          string   `json:"last_result,omitempty"`
	LastResultReason    string   `json:"last_result_reason,omitempty"`
	NextCheckAt         string   `json:"next_check_at,omitempty"`
	NextCheckAfterRunID string   `json:"next_check_after_run_id,omitempty"`
	CooldownRuns        int      `json:"cooldown_runs,omitempty"`
	Evidence            []string `json:"evidence,omitempty"`
	UpdatedAt           string   `json:"updated_at,omitempty"`
}

type PulseWorklistDecision struct {
	Module              string   `json:"module"`
	Due                 bool     `json:"due"`
	Reason              string   `json:"reason"`
	Evidence            []string `json:"evidence"`
	NextCheckAt         string   `json:"next_check_at"`
	NextCheckAfterRunID string   `json:"next_check_after_run_id"`
	CooldownRuns        int      `json:"cooldown_runs"`
}

// PulseRunMode is the durable, human-readable Gate decision for a single pass.
// Go validates the finite vocabulary but never selects a mode.
type PulseRunMode struct {
	WorkspacePath string `json:"workspace_path"`
	PulseRunID    string `json:"pulse_run_id"`
	Mode          string `json:"mode"`
	Reason        string `json:"reason"`
	RecordedAt    string `json:"recorded_at"`
}

// PulseReviewFocus is compact, durable coverage state. Times are explicit UTC
// RFC3339 timestamps; never infer lifecycle time from the opaque pulse_run_id.
type PulseReviewFocus struct {
	WorkspacePath       string   `json:"workspace_path"`
	Module              string   `json:"module"`
	FocusKey            string   `json:"focus_key"`
	LastPulseRunID      string   `json:"last_pulse_run_id,omitempty"`
	LastReviewedAt      string   `json:"last_reviewed_at,omitempty"`
	LastVerdict         string   `json:"last_verdict,omitempty"`
	LastSelectionReason string   `json:"last_selection_reason,omitempty"`
	RouteScope          string   `json:"route_scope,omitempty"`
	NextCheckAt         string   `json:"next_check_at,omitempty"`
	NextCheckReason     string   `json:"next_check_reason,omitempty"`
	UpdatedAt           string   `json:"updated_at"`
	ReviewCount         int      `json:"review_count"`
	RouteReviewCount    int      `json:"route_review_count,omitempty"`
	DeferredFocuses     []string `json:"deferred_focuses,omitempty"`
	IssueIDs            []string `json:"issue_ids,omitempty"`
	Evidence            []string `json:"evidence,omitempty"`
}

type PulseModuleAudit struct {
	WorkspacePath string   `json:"workspace_path"`
	Module        string   `json:"module"`
	PulseRunID    string   `json:"pulse_run_id"`
	Result        string   `json:"result"`
	Reason        string   `json:"reason"`
	Evidence      []string `json:"evidence,omitempty"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	Verification  []string `json:"verification,omitempty"`
	BeforeRefs    []string `json:"before_refs,omitempty"`
	AfterRefs     []string `json:"after_refs,omitempty"`
	RecordedAt    string   `json:"recorded_at"`
}

type PulseModuleAuditInput struct {
	ChangedFiles []string
	Verification []string
	BeforeRefs   []string
	AfterRefs    []string
}

// PulseShadowSignalObservation is retained experimental evidence. It is
// deliberately not returned by get_pulse_state(view="module"), so the live Gate cannot
// condition its worklist on a shadow detector before Stage B approval.
type PulseShadowSignalObservation struct {
	WorkspacePath   string                  `json:"workspace_path"`
	PulseRunID      string                  `json:"pulse_run_id"`
	Detector        string                  `json:"detector"`
	DetectorVersion string                  `json:"detector_version"`
	ObservedAt      string                  `json:"observed_at"`
	CoverageStatus  string                  `json:"coverage_status"`
	CoverageReason  string                  `json:"coverage_reason,omitempty"`
	Signals         []loopclosure.Finding   `json:"signals"`
	GateDecisions   []PulseWorklistDecision `json:"gate_decisions"`
	RecordedAt      string                  `json:"recorded_at"`
}

func ensurePulseModuleStateSchema(ctx context.Context, db *sql.DB) error {
	if err := migratePulseModuleStateSchema(ctx, db); err != nil {
		return err
	}
	stmts := []string{
		pulseModuleStateSchema,
		pulseModuleAuditSchema,
		`CREATE TABLE IF NOT EXISTS pulse_module_result_history (
			_id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_path TEXT NOT NULL, module TEXT NOT NULL, pulse_run_id TEXT NOT NULL,
			previous_state_json TEXT NOT NULL, previous_audit_json TEXT NOT NULL,
			replaced_at TEXT NOT NULL
		)`,
		pulseReviewRecoverySchema,
		pulseRunModeSchema,
		pulseReviewFocusStateSchema,
		pulseReviewFocusHistorySchema,
		pulseShadowSignalObservationSchema,
		pulseFinalCommandStateSchema,
		backgroundAgentLogSchema,
		pulseFastRequestSchema,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensurePulseModuleStateColumns(ctx, db); err != nil {
		return err
	}
	if err := ensurePulseReviewRecoveryColumns(ctx, db); err != nil {
		return err
	}
	if err := ensurePulseReviewFocusColumns(ctx, db); err != nil {
		return err
	}
	if err := ensureBackgroundAgentLogColumns(ctx, db); err != nil {
		return err
	}
	if err := migrateMergedStrategicReviewRows(ctx, db); err != nil {
		return err
	}
	if err := migrateMergedTechnicalReviewRows(ctx, db); err != nil {
		return err
	}
	if err := migratePulseReviewFocusCatalog(ctx, db); err != nil {
		return err
	}
	stmts = []string{
		`CREATE INDEX IF NOT EXISTS idx_pulse_module_state_run ON pulse_module_state(last_pulse_run_id, last_decision)`,
		`CREATE INDEX IF NOT EXISTS idx_pulse_module_audit_recorded ON pulse_module_audit(workspace_path, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_pulse_run_mode_recorded ON pulse_run_mode(workspace_path, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_pulse_review_focus_history_module ON pulse_review_focus_history(workspace_path, module, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_pulse_shadow_signal_observed ON pulse_shadow_signal_observation(workspace_path, observed_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_background_agent_log_session ON background_agent_log(workspace_path, session_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pulse_fast_request_status ON pulse_fast_request(status, requested_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func ensurePulseReviewRecoveryColumns(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(pulse_review_recovery)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasStatus := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		hasStatus = hasStatus || name == "status"
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasStatus {
		_, err := db.ExecContext(ctx, `ALTER TABLE pulse_review_recovery ADD COLUMN status TEXT NOT NULL DEFAULT 'incomplete'`)
		return err
	}
	return nil
}

// migratePulseReviewFocusCatalog preserves the useful coverage history from
// the original fine-grained catalog while moving the live agenda to the six
// operator-facing review areas introduced by PLAT-163. Merged lenses retain
// their newest state and historical counts. The old combined report/evaluation
// lens is intentionally not copied into both successors: doing so would claim
// two reviews happened when only one broad review did. Its history remains
// readable, while both new lenses start due. Safety history is also retained,
// but safety is not an independently selectable focus in the current catalog.
func migratePulseReviewFocusCatalog(ctx context.Context, db *sql.DB) error {
	merged := map[string][]string{
		"execution_health": {
			"execution_correctness", "execution_efficiency", "tool_runtime_reliability", "schedule_capacity_recovery",
		},
		"plan_orchestration_integrity": {"plan_contract_integrity", "orchestration_fitness"},
		"model_cost_fitness":           {"model_tier_fitness", "cost_attribution"},
	}
	legacyStateKeys := []string{
		"execution_correctness", "execution_efficiency", "tool_runtime_reliability", "schedule_capacity_recovery",
		"plan_contract_integrity", "orchestration_fitness", "model_tier_fitness", "cost_attribution",
		"report_eval_truth", "safety_permissions",
	}
	// Probe before opening a write transaction: this runs on every open of the
	// module-state db, including pure reads, and on an already-migrated
	// database every statement below is a no-op that would still take the
	// write lock (see sqliteRowsExist).
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(legacyStateKeys)), ",")
	probe := `SELECT 1 FROM pulse_review_focus_state WHERE module=? AND focus_key IN (` + placeholders + `)
		UNION ALL SELECT 1 FROM pulse_review_focus_history WHERE module=? AND (focus_key IN (` + placeholders + `)`
	probeArgs := make([]interface{}, 0, 3*len(legacyStateKeys)+2)
	probeArgs = append(probeArgs, pulseModuleTechnicalReview)
	for _, key := range legacyStateKeys {
		probeArgs = append(probeArgs, key)
	}
	probeArgs = append(probeArgs, pulseModuleTechnicalReview)
	for _, key := range legacyStateKeys {
		probeArgs = append(probeArgs, key)
	}
	for _, key := range legacyStateKeys {
		probe += ` OR deferred_focuses_json LIKE '%"' || ? || '"%'`
		probeArgs = append(probeArgs, key)
	}
	probe += `)`
	if found, err := sqliteRowsExist(ctx, db, probe, probeArgs...); err != nil || !found {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for canonical, legacyKeys := range merged {
		for _, legacy := range legacyKeys {
			if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_review_focus_state (
				workspace_path,module,focus_key,last_pulse_run_id,last_reviewed_at,last_verdict,
				last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at)
				SELECT workspace_path,module,?,last_pulse_run_id,last_reviewed_at,last_verdict,
				last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at
				FROM pulse_review_focus_state WHERE module=? AND focus_key=?
				ON CONFLICT(workspace_path,module,focus_key) DO UPDATE SET
					last_pulse_run_id=excluded.last_pulse_run_id,
					last_reviewed_at=excluded.last_reviewed_at,
					last_verdict=excluded.last_verdict,
					last_selection_reason=excluded.last_selection_reason,
					last_route_scope=excluded.last_route_scope,
					next_check_at=excluded.next_check_at,
					next_check_reason=excluded.next_check_reason,
					updated_at=excluded.updated_at
				WHERE excluded.updated_at > pulse_review_focus_state.updated_at`, canonical, pulseModuleTechnicalReview, legacy); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE pulse_review_focus_history SET focus_key=? WHERE module=? AND focus_key=?`, canonical, pulseModuleTechnicalReview, legacy); err != nil {
				return err
			}
		}
	}
	for _, legacy := range legacyStateKeys {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pulse_review_focus_state WHERE module=? AND focus_key=?`, pulseModuleTechnicalReview, legacy); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT _id,deferred_focuses_json FROM pulse_review_focus_history WHERE module=?`, pulseModuleTechnicalReview)
	if err != nil {
		return err
	}
	type deferredRow struct {
		id      int64
		encoded string
	}
	var deferredRows []deferredRow
	for rows.Next() {
		var row deferredRow
		if err := rows.Scan(&row.id, &row.encoded); err != nil {
			_ = rows.Close()
			return err
		}
		deferredRows = append(deferredRows, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range deferredRows {
		var keys []string
		_ = json.Unmarshal([]byte(row.encoded), &keys)
		encoded, _ := json.Marshal(canonicalPulseDeferredFocuses(keys))
		if string(encoded) == row.encoded {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pulse_review_focus_history SET deferred_focuses_json=? WHERE _id=?`, string(encoded), row.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func canonicalPulseDeferredFocuses(keys []string) []string {
	aliases := map[string][]string{
		"execution_correctness":      {"execution_health"},
		"execution_efficiency":       {"execution_health"},
		"tool_runtime_reliability":   {"execution_health"},
		"schedule_capacity_recovery": {"execution_health"},
		"plan_contract_integrity":    {"plan_orchestration_integrity"},
		"orchestration_fitness":      {"plan_orchestration_integrity"},
		"model_tier_fitness":         {"model_cost_fitness"},
		"cost_attribution":           {"model_cost_fitness"},
		"report_eval_truth":          {"report_quality_truth", "evaluation_quality_truth"},
		"safety_permissions":         {},
	}
	seen := map[string]bool{}
	out := []string{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		mapped, legacy := aliases[key]
		if !legacy {
			mapped = []string{key}
		}
		for _, candidate := range mapped {
			if candidate == "" || seen[candidate] {
				continue
			}
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}

// migrateMergedTechnicalReviewRows collapses the retired Engineering and
// Operations lanes into one live Technical Review identity. The newest module
// state wins. Audit receipts from the same Pulse run are merged conservatively:
// the newest receipt becomes the canonical summary while the legacy evidence
// remains available through finding and focus history migration.
func migrateMergedTechnicalReviewRows(ctx context.Context, db *sql.DB) error {
	return migrateMergedModuleRows(ctx, db, pulsemodules.TechnicalReviewID,
		pulsemodules.LegacyWorkflowReviewID, pulsemodules.LegacyLLMOpsReviewID)
}

// migrateMergedStrategicReviewRows collapses the two retired advisor lanes
// into the one live Strategic Review identity. The newest state wins when an
// old workflow database has both rows; audit history preserves one receipt per
// Pulse run, which is the new module contract.
func migrateMergedStrategicReviewRows(ctx context.Context, db *sql.DB) error {
	return migrateMergedModuleRows(ctx, db, pulsemodules.StrategicReviewID,
		pulsemodules.LegacyStrategyAuditorID, pulsemodules.LegacyGoalAdvisorID)
}

func migrateMergedModuleRows(ctx context.Context, db *sql.DB, canonical, legacyFirst, legacySecond string) error {
	// Probe before opening a write transaction; see sqliteRowsExist.
	found, err := sqliteRowsExist(ctx, db, `SELECT 1 FROM pulse_module_state WHERE module IN (?, ?)
		UNION ALL SELECT 1 FROM pulse_module_audit WHERE module IN (?, ?)
		UNION ALL SELECT 1 FROM pulse_review_focus_state WHERE module IN (?, ?)
		UNION ALL SELECT 1 FROM pulse_review_focus_history WHERE module IN (?, ?)`,
		legacyFirst, legacySecond, legacyFirst, legacySecond, legacyFirst, legacySecond, legacyFirst, legacySecond)
	if err != nil || !found {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_module_state (
		workspace_path, module, last_pulse_run_id, last_checked_at, last_ran_at,
		last_decision, last_reason, last_gate_decision, last_result, last_result_reason,
		next_check_at, next_check_after_run_id, cooldown_runs, evidence_json, updated_at)
		SELECT workspace_path, ?, last_pulse_run_id, last_checked_at, last_ran_at,
		last_decision, last_reason, last_gate_decision, last_result, last_result_reason,
		next_check_at, next_check_after_run_id, cooldown_runs, evidence_json, updated_at
		FROM pulse_module_state old
		WHERE module IN (?, ?)
		AND updated_at=(SELECT MAX(updated_at) FROM pulse_module_state newer
			WHERE newer.workspace_path=old.workspace_path AND newer.module IN (?, ?))
		ORDER BY updated_at DESC
		ON CONFLICT(workspace_path, module) DO UPDATE SET
			last_pulse_run_id=excluded.last_pulse_run_id,
			last_checked_at=excluded.last_checked_at,
			last_ran_at=excluded.last_ran_at,
			last_decision=excluded.last_decision,
			last_reason=excluded.last_reason,
			last_gate_decision=excluded.last_gate_decision,
			last_result=excluded.last_result,
			last_result_reason=excluded.last_result_reason,
			next_check_at=excluded.next_check_at,
			next_check_after_run_id=excluded.next_check_after_run_id,
			cooldown_runs=excluded.cooldown_runs,
			evidence_json=excluded.evidence_json,
			updated_at=excluded.updated_at
		WHERE excluded.updated_at > pulse_module_state.updated_at`, canonical, legacyFirst, legacySecond, legacyFirst, legacySecond); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pulse_module_state WHERE module IN (?, ?)`, legacyFirst, legacySecond); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_module_audit (
		workspace_path, module, pulse_run_id, result, reason, evidence_json,
		changed_files_json, verification_json, before_refs_json, after_refs_json, recorded_at)
		SELECT workspace_path, ?, pulse_run_id, result, reason, evidence_json,
		changed_files_json, verification_json, before_refs_json, after_refs_json, recorded_at
		FROM pulse_module_audit WHERE module IN (?, ?) ORDER BY recorded_at DESC
		ON CONFLICT(workspace_path,module,pulse_run_id) DO UPDATE SET
			result=excluded.result,
			reason=excluded.reason,
			evidence_json=excluded.evidence_json,
			changed_files_json=excluded.changed_files_json,
			verification_json=excluded.verification_json,
			before_refs_json=excluded.before_refs_json,
			after_refs_json=excluded.after_refs_json,
			recorded_at=excluded.recorded_at
		WHERE excluded.recorded_at > pulse_module_audit.recorded_at`,
		canonical, legacyFirst, legacySecond); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pulse_module_audit WHERE module IN (?, ?)`, legacyFirst, legacySecond); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_review_focus_state (
		workspace_path,module,focus_key,last_pulse_run_id,last_reviewed_at,last_verdict,
		last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at)
		SELECT workspace_path,?,focus_key,last_pulse_run_id,last_reviewed_at,last_verdict,
		last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at
		FROM pulse_review_focus_state WHERE module IN (?, ?)
		ORDER BY updated_at ASC
		ON CONFLICT(workspace_path,module,focus_key) DO UPDATE SET
			last_pulse_run_id=excluded.last_pulse_run_id,
			last_reviewed_at=excluded.last_reviewed_at,
			last_verdict=excluded.last_verdict,
			last_selection_reason=excluded.last_selection_reason,
			last_route_scope=excluded.last_route_scope,
			next_check_at=excluded.next_check_at,
			next_check_reason=excluded.next_check_reason,
			updated_at=excluded.updated_at
		WHERE excluded.updated_at > pulse_review_focus_state.updated_at`, canonical, legacyFirst, legacySecond); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pulse_review_focus_state WHERE module IN (?, ?)`, legacyFirst, legacySecond); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pulse_review_focus_history SET module=? WHERE module IN (?, ?)`, canonical, legacyFirst, legacySecond); err != nil {
		return err
	}
	return tx.Commit()
}

func migratePulseModuleStateSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(pulse_module_state)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	pk := map[string]int{}
	hasTable := false
	for rows.Next() {
		hasTable = true
		var cid, notNull, pkIndex int
		var name, colType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pkIndex); err != nil {
			return err
		}
		if pkIndex > 0 {
			pk[name] = pkIndex
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasTable {
		return nil
	}
	if pk["workspace_path"] > 0 && pk["module"] > 0 {
		return nil
	}
	if pk["module"] == 0 {
		return nil
	}

	legacyTable := fmt.Sprintf("pulse_module_state_legacy_%d", time.Now().UnixNano())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE pulse_module_state RENAME TO %s`, legacyTable)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_pulse_module_state_run`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, pulseModuleStateSchema); err != nil {
		return err
	}
	insert := fmt.Sprintf(`INSERT OR REPLACE INTO pulse_module_state (
			workspace_path, module, last_pulse_run_id, last_checked_at, last_ran_at,
			last_decision, last_reason, last_gate_decision, last_result, last_result_reason,
			next_check_at, next_check_after_run_id, cooldown_runs, evidence_json, updated_at
		)
		SELECT workspace_path, module, last_pulse_run_id, last_checked_at, last_ran_at,
			last_decision, last_reason,
			CASE WHEN last_decision IN ('due', 'skipped') THEN last_decision ELSE '' END,
			CASE WHEN last_decision IN ('done', 'changed', 'blocked', 'failed') THEN last_decision ELSE '' END,
			CASE WHEN last_decision IN ('done', 'changed', 'blocked', 'failed') THEN last_reason ELSE '' END,
			next_check_at, next_check_after_run_id,
			cooldown_runs, evidence_json, updated_at
		FROM %s`, legacyTable)
	if _, err := tx.ExecContext(ctx, insert); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s`, legacyTable)); err != nil {
		return err
	}
	return tx.Commit()
}

func ensurePulseModuleStateColumns(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(pulse_module_state)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pkIndex int
		var name, colType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pkIndex); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, col := range []string{"last_gate_decision", "last_result", "last_result_reason"} {
		if cols[col] {
			continue
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE pulse_module_state ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, col)); err != nil {
			return err
		}
	}
	return nil
}

func ensurePulseReviewFocusColumns(ctx context.Context, db *sql.DB) error {
	for _, table := range []struct {
		name   string
		column string
	}{
		{name: "pulse_review_focus_state", column: "last_route_scope"},
		{name: "pulse_review_focus_history", column: "route_scope"},
	} {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table.name))
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notNull, pkIndex int
			var name, colType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pkIndex); err != nil {
				_ = rows.Close()
				return err
			}
			found = found || name == table.column
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if found {
			continue
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, table.name, table.column)); err != nil {
			return err
		}
	}
	return nil
}

func openPulseModuleStateDB(ctx context.Context, workspacePath string, create bool) (string, *sql.DB, error) {
	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, create)
	if err != nil || db == nil {
		return normalized, db, err
	}
	if create {
		if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
			_ = db.Close()
			return "", nil, err
		}
	}
	return normalized, db, nil
}

// Strategic focuses describe the investigation after reasoning; the starter
// catalog must not prevent a useful domain-specific question from being saved.
// Technical focus identities remain closed because they select maintenance packs.
func validPulseReviewFocusKey(module, key string) bool {
	if slices.Contains(pulseReviewFocusCatalog[module], key) {
		return true
	}
	if module != pulseModuleStrategicReview || len(key) == 0 || len(key) > 64 || key[0] < 'a' || key[0] > 'z' || strings.HasSuffix(key, "_") || strings.Contains(key, "__") {
		return false
	}
	for _, c := range key {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func pulseReviewFocusKeyHint(module string) string {
	if module == pulseModuleStrategicReview {
		return "reuse a fitting strategic focus or supply a descriptive lowercase snake_case key (maximum 64 characters)"
	}
	return "choose one of: " + strings.Join(pulseReviewFocusCatalog[module], ", ")
}

func recordPulseReviewFocus(ctx context.Context, workspacePath, pulseRunID, module, focusKey, routeScope, priorityClass, selectionReason, verdict, nextCheckAt, nextCheckReason string, evidence, issueIDs, deferred []string) (*PulseReviewFocus, error) {
	pulseRunID, module, focusKey = strings.TrimSpace(pulseRunID), strings.TrimSpace(module), strings.TrimSpace(focusKey)
	routeScope = strings.TrimSpace(routeScope)
	if pulseRunID == "" || module == "" || focusKey == "" || strings.TrimSpace(priorityClass) == "" || strings.TrimSpace(selectionReason) == "" {
		return nil, fmt.Errorf("pulse_run_id, module, focus_key, priority_class, and selection_reason are required")
	}
	if !validPulseModules[module] {
		return nil, fmt.Errorf("module %q is not valid; choose one of: %s", module, pulseModuleList())
	}
	if !validPulseReviewFocusKey(module, focusKey) {
		return nil, fmt.Errorf("focus_key %q is not valid for %s; %s", focusKey, module, pulseReviewFocusKeyHint(module))
	}
	for _, deferredKey := range deferred {
		deferredKey = strings.TrimSpace(deferredKey)
		if deferredKey != "" && !validPulseReviewFocusKey(module, deferredKey) {
			return nil, fmt.Errorf("deferred focus %q is not valid for %s; %s", deferredKey, module, pulseReviewFocusKeyHint(module))
		}
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	evidenceJSON, _ := json.Marshal(evidence)
	issuesJSON, _ := json.Marshal(issueIDs)
	deferredJSON, _ := json.Marshal(deferred)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_review_focus_history
		(workspace_path,module,pulse_run_id,focus_key,route_scope,priority_class,selection_reason,verdict,evidence_json,issue_ids_json,deferred_focuses_json,recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, normalized, module, pulseRunID, focusKey, routeScope, strings.TrimSpace(priorityClass), strings.TrimSpace(selectionReason), strings.TrimSpace(verdict), string(evidenceJSON), string(issuesJSON), string(deferredJSON), now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_review_focus_state
		(workspace_path,module,focus_key,last_pulse_run_id,last_reviewed_at,last_verdict,last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(workspace_path,module,focus_key) DO UPDATE SET
		last_pulse_run_id=excluded.last_pulse_run_id,last_reviewed_at=excluded.last_reviewed_at,last_verdict=excluded.last_verdict,
		last_selection_reason=excluded.last_selection_reason,last_route_scope=excluded.last_route_scope,next_check_at=excluded.next_check_at,next_check_reason=excluded.next_check_reason,updated_at=excluded.updated_at`,
		normalized, module, focusKey, pulseRunID, now, strings.TrimSpace(verdict), strings.TrimSpace(selectionReason), routeScope, strings.TrimSpace(nextCheckAt), strings.TrimSpace(nextCheckReason), now); err != nil {
		return nil, err
	}
	// A deferred lens is not merely prose: seed it as never-reviewed coverage so
	// the next compact agenda can surface it without scanning Markdown history.
	for _, deferredKey := range deferred {
		deferredKey = strings.TrimSpace(deferredKey)
		if deferredKey == "" || deferredKey == focusKey {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_review_focus_state
			(workspace_path,module,focus_key,last_pulse_run_id,last_reviewed_at,last_verdict,last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_path,module,focus_key) DO NOTHING`,
			normalized, module, deferredKey, "", "", "", "", "", "", "", now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PulseReviewFocus{WorkspacePath: normalized, Module: module, FocusKey: focusKey, LastPulseRunID: pulseRunID, LastReviewedAt: now, LastVerdict: strings.TrimSpace(verdict), LastSelectionReason: strings.TrimSpace(selectionReason), RouteScope: routeScope, NextCheckAt: strings.TrimSpace(nextCheckAt), NextCheckReason: strings.TrimSpace(nextCheckReason), UpdatedAt: now, ReviewCount: 1, RouteReviewCount: 1, DeferredFocuses: deferred}, nil
}

func getPulseReviewFocusAgenda(ctx context.Context, workspacePath, module, routeScope string, limit int) ([]PulseReviewFocus, error) {
	if !validPulseModules[strings.TrimSpace(module)] {
		return nil, fmt.Errorf("module %q is not valid; choose one of: %s", module, pulseModuleList())
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return []PulseReviewFocus{}, err
	}
	defer db.Close()
	routeScope = strings.TrimSpace(routeScope)
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, focusKey := range pulseReviewFocusCatalog[module] {
		if _, err := db.ExecContext(ctx, `INSERT INTO pulse_review_focus_state
			(workspace_path,module,focus_key,last_pulse_run_id,last_reviewed_at,last_verdict,last_selection_reason,last_route_scope,next_check_at,next_check_reason,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_path,module,focus_key) DO NOTHING`,
			normalized, module, focusKey, "", "", "", "", "", "", "", now); err != nil {
			return nil, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT s.workspace_path,s.module,s.focus_key,s.last_pulse_run_id,s.last_reviewed_at,s.last_verdict,s.last_selection_reason,s.last_route_scope,s.next_check_at,s.next_check_reason,s.updated_at,
		(SELECT COUNT(*) FROM pulse_review_focus_history hc WHERE hc.workspace_path=s.workspace_path AND hc.module=s.module AND hc.focus_key=s.focus_key),
		(SELECT COUNT(*) FROM pulse_review_focus_history hr WHERE hr.workspace_path=s.workspace_path AND hr.module=s.module AND hr.focus_key=s.focus_key AND (?='' OR hr.route_scope=?)),
		COALESCE((SELECT deferred_focuses_json FROM pulse_review_focus_history h WHERE h.workspace_path=s.workspace_path AND h.module=s.module AND h.focus_key=s.focus_key ORDER BY h._id DESC LIMIT 1),'[]')
		FROM pulse_review_focus_state s WHERE s.workspace_path=? AND s.module=?
		ORDER BY CASE
			WHEN s.last_reviewed_at='' THEN 0
			WHEN s.next_check_at<>'' AND s.next_check_at<=? THEN 1
			ELSE 2
		END,
		(SELECT COUNT(*) FROM pulse_review_focus_history hr WHERE hr.workspace_path=s.workspace_path AND hr.module=s.module AND hr.focus_key=s.focus_key AND (?='' OR hr.route_scope=?)) ASC,
		s.last_reviewed_at ASC, s.focus_key ASC LIMIT ?`, routeScope, routeScope, normalized, module, now, routeScope, routeScope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PulseReviewFocus{}
	for rows.Next() {
		var focus PulseReviewFocus
		var deferredJSON string
		if err := rows.Scan(&focus.WorkspacePath, &focus.Module, &focus.FocusKey, &focus.LastPulseRunID, &focus.LastReviewedAt, &focus.LastVerdict, &focus.LastSelectionReason, &focus.RouteScope, &focus.NextCheckAt, &focus.NextCheckReason, &focus.UpdatedAt, &focus.ReviewCount, &focus.RouteReviewCount, &deferredJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(deferredJSON), &focus.DeferredFocuses)
		if focus.DeferredFocuses == nil {
			focus.DeferredFocuses = []string{}
		}
		out = append(out, focus)
	}
	return out, rows.Err()
}

func getPulseReviewFocusStates(ctx context.Context, workspacePath string) ([]PulseReviewFocus, error) {
	out := []PulseReviewFocus{}
	for _, module := range []string{pulseModuleTechnicalReview, pulseModuleStrategicReview} {
		focuses, err := getPulseReviewFocusAgenda(ctx, workspacePath, module, "", 50)
		if err != nil {
			return nil, err
		}
		out = append(out, focuses...)
	}
	return out, nil
}

func getPulseReviewFocusSelections(ctx context.Context, workspacePath string, limit int) ([]PulseReviewFocus, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return []PulseReviewFocus{}, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT h.workspace_path,h.module,h.focus_key,h.pulse_run_id,h.recorded_at,h.verdict,h.selection_reason,h.route_scope,h.deferred_focuses_json,h.issue_ids_json,
		(SELECT COUNT(*) FROM pulse_review_focus_history hc WHERE hc.workspace_path=h.workspace_path AND hc.module=h.module AND hc.focus_key=h.focus_key),
		(SELECT COUNT(*) FROM pulse_review_focus_history hr WHERE hr.workspace_path=h.workspace_path AND hr.module=h.module AND hr.focus_key=h.focus_key AND hr.route_scope=h.route_scope)
		FROM pulse_review_focus_history h WHERE h.workspace_path=? ORDER BY h._id DESC LIMIT ?`, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PulseReviewFocus{}
	for rows.Next() {
		var focus PulseReviewFocus
		var deferredJSON, issueIDsJSON string
		if err := rows.Scan(&focus.WorkspacePath, &focus.Module, &focus.FocusKey, &focus.LastPulseRunID, &focus.LastReviewedAt, &focus.LastVerdict, &focus.LastSelectionReason, &focus.RouteScope, &deferredJSON, &issueIDsJSON, &focus.ReviewCount, &focus.RouteReviewCount); err != nil {
			return nil, err
		}
		focus.UpdatedAt = focus.LastReviewedAt
		_ = json.Unmarshal([]byte(deferredJSON), &focus.DeferredFocuses)
		if focus.DeferredFocuses == nil {
			focus.DeferredFocuses = []string{}
		}
		_ = json.Unmarshal([]byte(issueIDsJSON), &focus.IssueIDs)
		if focus.IssueIDs == nil {
			focus.IssueIDs = []string{}
		}
		out = append(out, focus)
	}
	return out, rows.Err()
}

func recordPulseWorklist(ctx context.Context, workspacePath, pulseRunID string, decisions []PulseWorklistDecision) ([]PulseModuleState, error) {
	return recordPulseWorklistWithMode(ctx, workspacePath, pulseRunID, pulseRunModeDiscovery, "Direct worklist call; Gate mode was not supplied.", decisions)
}

func recordPulseWorklistWithMode(ctx context.Context, workspacePath, pulseRunID, mode, modeReason string, decisions []PulseWorklistDecision) ([]PulseModuleState, error) {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return nil, fmt.Errorf("pulse_run_id is required: pass the scheduler-provided Pulse run id exactly as it appears in the prompt")
	}
	mode, modeReason, err := normalizePulseRunMode(mode, modeReason)
	if err != nil {
		return nil, err
	}
	// Recovery is a backend fact, not another input for Gate to remember. Apply
	// it before validating the worklist so an otherwise valid Gate skip cannot
	// suppress an interrupted review behind a cooldown.
	decisions, err = forcePendingPulseReviewRecoveries(ctx, workspacePath, decisions)
	if err != nil {
		return nil, err
	}
	if err := validatePulseWorklistDecisions(decisions); err != nil {
		return nil, err
	}
	if err := validateDeterministicIntakeRouting(ctx, workspacePath, decisions); err != nil {
		return nil, err
	}
	if err := validatePlanDriftRouting(ctx, workspacePath, decisions); err != nil {
		return nil, err
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_run_mode (workspace_path, pulse_run_id, mode, reason, recorded_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_path, pulse_run_id) DO UPDATE SET
			mode=excluded.mode, reason=excluded.reason, recorded_at=excluded.recorded_at`,
		normalized, pulseRunID, mode, modeReason, now); err != nil {
		return nil, err
	}
	states := make([]PulseModuleState, 0, len(decisions))
	for _, decision := range decisions {
		module := normalizePulseModule(decision.Module)
		if !validPulseModules[module] {
			return nil, fmt.Errorf("module %q is not a valid Pulse module. Must be one of: %s", decision.Module, pulseModuleList())
		}
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			return nil, fmt.Errorf("reason is required for module %q", module)
		}
		lastDecision := "skipped"
		if decision.Due {
			lastDecision = "due"
		}
		evidence := normalizePulseEvidence(decision.Evidence)
		evidenceJSON, _ := json.Marshal(evidence)
		state := PulseModuleState{
			WorkspacePath:       normalized,
			Module:              module,
			LastPulseRunID:      pulseRunID,
			LastCheckedAt:       now,
			LastDecision:        lastDecision,
			LastReason:          reason,
			LastGateDecision:    lastDecision,
			NextCheckAt:         strings.TrimSpace(decision.NextCheckAt),
			NextCheckAfterRunID: strings.TrimSpace(decision.NextCheckAfterRunID),
			CooldownRuns:        decision.CooldownRuns,
			Evidence:            evidence,
			UpdatedAt:           now,
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO pulse_module_state (
				module, workspace_path, last_pulse_run_id, last_checked_at, last_decision,
				last_reason, last_gate_decision, last_result, last_result_reason,
				next_check_at, next_check_after_run_id, cooldown_runs,
				evidence_json, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, ?, ?)
			ON CONFLICT(workspace_path, module) DO UPDATE SET
				last_pulse_run_id=excluded.last_pulse_run_id,
				last_checked_at=excluded.last_checked_at,
				last_decision=excluded.last_decision,
				last_reason=excluded.last_reason,
				last_gate_decision=excluded.last_gate_decision,
				last_result='',
				last_result_reason='',
				next_check_at=excluded.next_check_at,
				next_check_after_run_id=excluded.next_check_after_run_id,
				cooldown_runs=excluded.cooldown_runs,
				evidence_json=excluded.evidence_json,
				updated_at=excluded.updated_at`,
			module, normalized, pulseRunID, now, lastDecision, reason,
			lastDecision,
			state.NextCheckAt, state.NextCheckAfterRunID, state.CooldownRuns,
			string(evidenceJSON), now,
		)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return states, nil
}

func pendingPulseReviewRecoveries(ctx context.Context, workspacePath string) ([]PulseReviewRecovery, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return []PulseReviewRecovery{}, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT workspace_path, module, source_pulse_run_id, checkpoint_path, status, reason, detected_at, updated_at
		FROM pulse_review_recovery WHERE workspace_path=? AND status='incomplete' ORDER BY detected_at`, normalized)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recoveries []PulseReviewRecovery
	for rows.Next() {
		var recovery PulseReviewRecovery
		if err := rows.Scan(&recovery.WorkspacePath, &recovery.Module, &recovery.SourcePulseRunID, &recovery.CheckpointPath, &recovery.Status, &recovery.Reason, &recovery.DetectedAt, &recovery.UpdatedAt); err != nil {
			return nil, err
		}
		recovery.Module = normalizePulseModule(recovery.Module)
		recoveries = append(recoveries, recovery)
	}
	return recoveries, rows.Err()
}

func forcePendingPulseReviewRecoveries(ctx context.Context, workspacePath string, decisions []PulseWorklistDecision) ([]PulseWorklistDecision, error) {
	recoveries, err := pendingPulseReviewRecoveries(ctx, workspacePath)
	if err != nil || len(recoveries) == 0 {
		return decisions, err
	}
	// Plan Drift is the single selected review when deterministic drift is due.
	// Keep recovery durable and schedule it in the next eligible pass rather
	// than running two independent review modes together.
	for _, decision := range decisions {
		if normalizePulseModule(decision.Module) == pulseModulePlanDriftReview && decision.Due {
			return decisions, nil
		}
	}
	forced := append([]PulseWorklistDecision(nil), decisions...)
	for _, recovery := range recoveries {
		for index := range forced {
			if normalizePulseModule(forced[index].Module) != recovery.Module {
				continue
			}
			forced[index].Due = true
			forced[index].CooldownRuns = 0
			forced[index].NextCheckAt = ""
			forced[index].NextCheckAfterRunID = ""
			forced[index].Reason = fmt.Sprintf("Mandatory recovery: %s", recovery.Reason)
			forced[index].Evidence = append(normalizePulseEvidence(forced[index].Evidence), "pulse_review_recovery:"+recovery.SourcePulseRunID)
			break
		}
	}
	return forced, nil
}

func markPulseReviewRecovery(ctx context.Context, workspacePath, module, sourcePulseRunID, checkpointPath, reason string) error {
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return fmt.Errorf("module %q is not valid for review recovery", module)
	}
	if strings.TrimSpace(sourcePulseRunID) == "" || strings.TrimSpace(checkpointPath) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("source_pulse_run_id, checkpoint_path, and reason are required for review recovery")
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx, `INSERT INTO pulse_review_recovery (
		workspace_path, module, source_pulse_run_id, checkpoint_path, status, reason, detected_at, updated_at
	) VALUES (?, ?, ?, ?, 'incomplete', ?, ?, ?)
	ON CONFLICT(workspace_path, module) DO UPDATE SET
		source_pulse_run_id=excluded.source_pulse_run_id, checkpoint_path=excluded.checkpoint_path,
		status='incomplete', reason=excluded.reason, updated_at=excluded.updated_at`,
		normalized, module, strings.TrimSpace(sourcePulseRunID), strings.TrimSpace(checkpointPath), strings.TrimSpace(reason), now, now)
	return err
}

// beginPulseReviewRecovery records ownership before a child is dispatched. A
// server restart can then turn this running attempt into an incomplete
// recovery obligation even if the scheduler never receives the child result.
func beginPulseReviewRecovery(ctx context.Context, workspacePath, module, pulseRunID, checkpointPath string) error {
	module = normalizePulseModule(module)
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx, `INSERT INTO pulse_review_recovery (
		workspace_path, module, source_pulse_run_id, checkpoint_path, status, reason, detected_at, updated_at
	) VALUES (?, ?, ?, ?, 'running', 'review executor dispatched; awaiting terminal module result', ?, ?)
	ON CONFLICT(workspace_path, module) DO NOTHING`, normalized, module, strings.TrimSpace(pulseRunID), strings.TrimSpace(checkpointPath), now, now)
	return err
}

func finalizeRunningPulseReviewRecoveries(ctx context.Context, workspacePath, reason string) (int64, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return 0, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return 0, err
	}
	result, err := db.ExecContext(ctx, `UPDATE pulse_review_recovery SET status='incomplete', reason=?, updated_at=?
		WHERE workspace_path=? AND status='running'`, strings.TrimSpace(reason), time.Now().UTC().Format(time.RFC3339Nano), normalized)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func clearPulseReviewRecoveryTx(ctx context.Context, tx *sql.Tx, workspacePath, module string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM pulse_review_recovery WHERE workspace_path=? AND module=?`, workspacePath, module)
	return err
}

// forcePulseModuleDueForLateRepairDebt amends exactly one module's persisted
// worklist decision to due=true, mid-pass, after Gate has already recorded
// its own worklist for this pulse_run_id. Gate's own decision is made before
// plan_drift_review runs, so real workflow-owned repair debt plan_drift_review
// discovers this same pass (a rare fixer_handoff route, or a check it could
// not safely fix itself) can leave technical_review sitting at Gate's earlier
// due=false even though actionable work now exists in the backlog — every
// downstream reader (the review-fix turn's own get_pulse_state read,
// pulseWorklistModulesDue, validatePulseDueModuleResultsFor) treats
// pulse_module_state.last_decision as the single source of truth for "is this
// module due," with no live recomputation from backlog content. This is a
// narrow, scheduler-internal correction to that one row — not a second
// record_pulse_worklist call, which Gate's own contract expects to run only
// once per pass. last_result/last_result_reason are cleared to an empty string to match
// exactly what a fresh due decision looks like, so validatePulseDueModuleResultsFor
// correctly requires a terminal result for this module before the pass ends.
func forcePulseModuleDueForLateRepairDebt(ctx context.Context, workspacePath, pulseRunID, module, reason string) error {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return fmt.Errorf("pulse_run_id is required")
	}
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return fmt.Errorf("module %q is not a valid Pulse module", module)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("reason is required")
	}
	return writePulseModuleDueRow(ctx, workspacePath, pulseRunID, module, reason)
}

// writePulseModuleDueRow is the shared single-row INSERT/ON CONFLICT write
// both forcePulseModuleDueForLateRepairDebt and
// recordPulseModuleDueForManualReview use. Callers must validate their
// arguments and any collision guard before calling this.
func writePulseModuleDueRow(ctx context.Context, workspacePath, pulseRunID, module, reason string) error {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `INSERT INTO pulse_module_state (
			module, workspace_path, last_pulse_run_id, last_checked_at, last_decision,
			last_reason, last_gate_decision, last_result, last_result_reason,
			next_check_at, next_check_after_run_id, cooldown_runs,
			evidence_json, updated_at
		) VALUES (?, ?, ?, ?, 'due', ?, 'due', '', '', '', '', 0, '[]', ?)
		ON CONFLICT(workspace_path, module) DO UPDATE SET
			last_pulse_run_id=excluded.last_pulse_run_id,
			last_checked_at=excluded.last_checked_at,
			last_decision='due',
			last_reason=excluded.last_reason,
			last_result='',
			last_result_reason='',
			updated_at=excluded.updated_at`,
		module, normalized, pulseRunID, now, reason, now,
	)
	return err
}

// recordPulseModuleDueForManualReview lets a manual, non-Gate-scheduled
// review (e.g. /review-artifact-drift's Part 1, which runs plan_drift_review's
// real procedure on demand rather than waiting for the next Pulse pass)
// establish its own due claim for exactly the module it is about to persist a
// terminal record_pulse_result for. record_pulse_result's own write is
// due-gated: it only accepts a result when pulse_module_state already shows
// due=true for the SAME pulse_run_id, which a manual invocation never has —
// Gate never ran for it, so without this call every manual completion would
// hit "already terminal or belongs to another run."
//
// pulse_module_state is one row per (workspace_path, module), not scoped per
// pulse_run_id, so an unconditional overwrite here would be unsafe: if a real
// scheduled Pulse pass is CURRENTLY mid-flight for this exact module (Gate
// already recorded it due, its reviewer turn has not yet persisted a result),
// blindly overwriting last_pulse_run_id would silently sever that in-flight
// pass's own later receipt validation (validatePulseDueModuleResultsFor,
// pulseWorklistModulesDue) from ever finding its row again. Refuse instead —
// the manual reviewer should report that a Pulse pass is already reviewing
// this module and wait, rather than corrupt it.
func recordPulseModuleDueForManualReview(ctx context.Context, workspacePath, pulseRunID, module, reason string) error {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return fmt.Errorf("pulse_run_id is required")
	}
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return fmt.Errorf("module %q is not a valid Pulse module", module)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("reason is required")
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	existing, stateErr := getPulseModuleStateByModule(ctx, db, normalized, module)
	db.Close()
	if stateErr != nil && !errors.Is(stateErr, sql.ErrNoRows) {
		return stateErr
	}
	if existing != nil &&
		strings.EqualFold(strings.TrimSpace(existing.LastDecision), "due") &&
		strings.TrimSpace(existing.LastResult) == "" &&
		strings.TrimSpace(existing.LastPulseRunID) != pulseRunID {
		return fmt.Errorf(
			"a Pulse pass (run %q) is currently reviewing %s and has not yet recorded a result — wait for it to finish before running this manual review, rather than risk corrupting its in-progress state",
			existing.LastPulseRunID, module,
		)
	}
	return writePulseModuleDueRow(ctx, workspacePath, pulseRunID, module, reason)
}

// validateDeterministicIntakeRouting enforces missing plan-dependency receipts
// only. Runtime signals remain visible to Gate, but a failed child call alone
// cannot prove an unresolved step outcome or reserve the only review slot.
// Gate assesses recovery, impact, prior reviews, and fresh comparable evidence.
func validateDeterministicIntakeRouting(ctx context.Context, workspacePath string, decisions []PulseWorklistDecision) error {
	// Pulse deliberately runs one review mode per pass. When deterministic plan
	// drift is already selected, retain runtime evidence for the next Gate
	// rather than forcing a concurrent Technical Review.
	for _, decision := range decisions {
		if normalizePulseModule(decision.Module) == pulseModulePlanDriftReview && decision.Due {
			return nil
		}
	}
	reasons := []string{}
	planBacklog := step_based_workflow.CollectPlanChangeBacklog(workspacePath)
	planResult := step_based_workflow.BuildPlanChangeDependencyIntake(planBacklog)
	if planResult.Failed {
		reasons = append(reasons, fmt.Sprintf("%d current-contract plan change(s) lack complete dependency coverage", planResult.FailureCount))
	}
	if len(reasons) == 0 {
		return nil
	}
	for _, decision := range decisions {
		if normalizePulseModule(decision.Module) == pulseModuleTechnicalReview && decision.Due {
			return nil
		}
	}
	return fmt.Errorf("technical_review must be due because deterministic intake failed: %s. Route these facts to an agentic Technical Review; they are not automatic Pulse issues and do not authorize a Fixer", strings.Join(reasons, "; "))
}

// validatePlanDriftRouting is plan_drift_review's due-ness enforcement.
// Unlike technical_review's cadence-plus-evidence judgment
// call, plan_drift_review's due condition is a plain fact: does any step lack
// a drift_review record, or carry one flagged needs_review==true. There is no
// cadence math and no judgment for Gate to exercise here — if the fact is
// true, the module must be marked due, or the worklist is rejected.
func validatePlanDriftRouting(ctx context.Context, workspacePath string, decisions []PulseWorklistDecision) error {
	candidates, err := step_based_workflow.CollectPlanDriftCandidates(ctx, workspacePath)
	if err != nil {
		// An unreadable or malformed plan.json/step_config.json makes the real
		// candidate set unknowable — that must never silently read as "nothing
		// is due." Require either plan_drift_review (if the agent already
		// suspects drift) or technical_review (to investigate the read/parse
		// failure itself, which is a platform/data-integrity issue in its own
		// right) to be due, exactly like a failed deterministic intake signal.
		for _, decision := range decisions {
			module := normalizePulseModule(decision.Module)
			if (module == pulseModulePlanDriftReview || module == pulseModuleTechnicalReview) && decision.Due {
				return nil
			}
		}
		return fmt.Errorf("plan_drift_review or technical_review must be due: the plan drift candidate scan itself failed and the real candidate set is unknown: %w", err)
	}
	if len(candidates) == 0 {
		return nil
	}
	for _, decision := range decisions {
		if normalizePulseModule(decision.Module) == pulseModulePlanDriftReview && decision.Due {
			return nil
		}
	}
	stepIDs := make([]string, 0, len(candidates))
	for _, c := range candidates {
		stepIDs = append(stepIDs, c.StepID)
	}
	return fmt.Errorf("plan_drift_review must be due: %d step(s) have no drift_review record or are flagged needs_review (%s). This is a plain fact, not a judgment call — mark it due", len(candidates), strings.Join(stepIDs, ", "))
}

func recordPulseWorklistOnce(ctx context.Context, workspacePath, pulseRunID string, decisions []PulseWorklistDecision) ([]PulseModuleState, error) {
	return recordPulseWorklistOnceAfter(ctx, workspacePath, pulseRunID, pulseRunModeDiscovery, "Direct worklist call; Gate mode was not supplied.", decisions, nil)
}

func recordPulseWorklistOnceWithShadowAndMode(ctx context.Context, workspacePath, pulseRunID, mode, modeReason string, decisions []PulseWorklistDecision, shadowResult loopclosure.Result) ([]PulseModuleState, error) {
	return recordPulseWorklistOnceAfter(ctx, workspacePath, pulseRunID, mode, modeReason, decisions, func() {
		// Shadow instrumentation cannot block or modify live scheduling.
		// Coverage failures are retained in shadowResult rather than converted
		// to an empty, apparently-clean signal set.
		if err := recordPulseShadowSignalObservation(ctx, workspacePath, pulseRunID, shadowResult, decisions); err != nil {
			log.Printf("[PULSE] record shadow loop-closure observation for %s: %v", workspacePath, err)
		}
	})
}

func recordPulseWorklistOnceAfter(ctx context.Context, workspacePath, pulseRunID, mode, modeReason string, decisions []PulseWorklistDecision, afterRecord func()) ([]PulseModuleState, error) {
	if _, _, err := normalizePulseRunMode(mode, modeReason); err != nil {
		return nil, err
	}
	// Apply recovery before this early structural validation too. Otherwise an
	// interrupted module that Gate skipped without a future boundary would be
	// rejected before the backend got its chance to make it mandatory due.
	var err error
	decisions, err = forcePendingPulseReviewRecoveries(ctx, workspacePath, decisions)
	if err != nil {
		return nil, err
	}
	if err := validatePulseWorklistDecisions(decisions); err != nil {
		return nil, err
	}

	pulseWorklistRecordMu.Lock()
	defer pulseWorklistRecordMu.Unlock()
	// Revalidate at the serialized write boundary. A session may have been
	// revoked after the tool call began but before argument parsing finished.
	if err := validatePulseToolRunID(ctx, pulseRunID); err != nil {
		return nil, err
	}

	existing, exists, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return nil, err
	}
	if exists && pulseWorklistIsComplete(existing) {
		states := make([]PulseModuleState, 0, len(pulseModuleOrder))
		for _, module := range pulseModuleOrder {
			states = append(states, existing[module])
		}
		return states, nil
	}
	states, err := recordPulseWorklistWithMode(ctx, workspacePath, pulseRunID, mode, modeReason, decisions)
	if err != nil {
		return nil, err
	}
	if afterRecord != nil {
		afterRecord()
	}
	return states, nil
}

func normalizePulseRunMode(mode, reason string) (string, string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	reason = strings.TrimSpace(reason)
	valid := false
	for _, value := range pulseRunModeValues {
		if mode == value {
			valid = true
			break
		}
	}
	if !valid {
		return "", "", fmt.Errorf("mode %q is not valid; choose one of: %s", mode, strings.Join(pulseRunModeValues, ", "))
	}
	if reason == "" {
		return "", "", fmt.Errorf("mode_reason is required: explain why this pass is %q", mode)
	}
	return mode, reason, nil
}

func validatePulseWorklistDecisions(decisions []PulseWorklistDecision) error {
	if len(decisions) == 0 {
		return fmt.Errorf("decisions are required: exactly one entry for each Pulse module (%s), each with module, due, and reason", pulseModuleList())
	}
	if len(decisions) != len(pulseModuleOrder) {
		return fmt.Errorf("decisions must include exactly one entry for each Pulse module; got %d, want %d covering: %s",
			len(decisions), len(pulseModuleOrder), pulseModuleList())
	}
	seen := map[string]bool{}
	for _, decision := range decisions {
		module := normalizePulseModule(decision.Module)
		if !validPulseModules[module] {
			return fmt.Errorf("module %q is not a valid Pulse module. Must be one of: %s", decision.Module, pulseModuleList())
		}
		if seen[module] {
			return fmt.Errorf("module %q appears more than once", module)
		}
		seen[module] = true
		if strings.TrimSpace(decision.Reason) == "" {
			return fmt.Errorf("reason is required for module %q", module)
		}
		if decision.CooldownRuns < 0 {
			return fmt.Errorf("cooldown_runs must be non-negative for module %q", module)
		}
		nextCheckAt := strings.TrimSpace(decision.NextCheckAt)
		if nextCheckAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, nextCheckAt); err != nil {
				if _, dateErr := time.Parse("2006-01-02", nextCheckAt); dateErr != nil {
					return fmt.Errorf("next_check_at must be RFC3339 or YYYY-MM-DD for module %q", module)
				}
			}
		}
		if !decision.Due && nextCheckAt == "" && strings.TrimSpace(decision.NextCheckAfterRunID) == "" && decision.CooldownRuns == 0 {
			return fmt.Errorf("skipped module %q must include next_check_at, next_check_after_run_id, or cooldown_runs", module)
		}
	}
	missing := []string{}
	for _, module := range pulseModuleOrder {
		if !seen[module] {
			missing = append(missing, module)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("decisions is missing required module(s) %s; every Pulse module needs its own entry and the complete set is: %s",
			strings.Join(missing, ", "), pulseModuleList())
	}
	var planDriftDue, technicalDue, strategicDue bool
	for _, decision := range decisions {
		switch normalizePulseModule(decision.Module) {
		case pulseModulePlanDriftReview:
			planDriftDue = decision.Due
		case pulseModuleTechnicalReview:
			technicalDue = decision.Due
		case pulseModuleStrategicReview:
			strategicDue = decision.Due
		}
	}
	if planDriftDue && (technicalDue || strategicDue) {
		return fmt.Errorf("plan_drift_review is due, so technical_review and strategic_review must both be skipped this pass; Pulse selects one review mode at a time")
	}
	if technicalDue && strategicDue {
		return fmt.Errorf("technical_review and strategic_review cannot both be due in one pass; choose the single perspective with the strongest current evidence")
	}
	return nil
}

func pulseWorklistIsComplete(worklist map[string]PulseModuleState) bool {
	if len(worklist) != len(pulseModuleOrder) {
		return false
	}
	for _, module := range pulseModuleOrder {
		if _, ok := worklist[module]; !ok {
			return false
		}
	}
	return true
}

// validatePulseGateCompletion validates only Gate's durable control-plane
// output. Gate does not own any presentation artifact; coupling routing to a
// UI write made a presentation failure discard an otherwise complete worklist.
func validatePulseGateCompletion(ctx context.Context, workspacePath, pulseRunID string) error {
	worklist, exists, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return fmt.Errorf("read Pulse Gate worklist: %w", err)
	}
	if !exists || !pulseWorklistIsComplete(worklist) {
		return fmt.Errorf("Pulse Gate did not record a complete worklist for pulse_run_id %q", pulseRunID)
	}
	return nil
}

func markPulseModuleResult(ctx context.Context, workspacePath, module, pulseRunID, result, reason string, evidence []string) (*PulseModuleState, error) {
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return nil, fmt.Errorf("module %q is not a valid Pulse module. Must be one of: %s", module, pulseModuleList())
	}
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return nil, fmt.Errorf("pulse_run_id is required: pass the scheduler-provided Pulse run id exactly as it appears in the prompt")
	}
	result = strings.TrimSpace(strings.ToLower(result))
	if result == "" {
		return nil, fmt.Errorf("result is required. Must be one of: %s", strings.Join(pulseSchedulerModuleResultValues, ", "))
	}
	switch result {
	case "done", "changed", "blocked", "failed", "skipped", "timed_out":
	default:
		return nil, fmt.Errorf("result %q is not valid. Must be one of: %s", result, strings.Join(pulseSchedulerModuleResultValues, ", "))
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required: one sentence stating the module's outcome")
	}

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	evidence = normalizePulseEvidence(evidence)
	evidenceJSON, _ := json.Marshal(evidence)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO pulse_module_state (
			module, workspace_path, last_pulse_run_id, last_checked_at, last_ran_at,
			last_result, last_result_reason, evidence_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_path, module) DO UPDATE SET
			last_pulse_run_id=excluded.last_pulse_run_id,
			last_ran_at=excluded.last_ran_at,
			last_result=excluded.last_result,
			last_result_reason=excluded.last_result_reason,
			evidence_json=excluded.evidence_json,
			updated_at=excluded.updated_at`,
		module, normalized, pulseRunID, now, now, result, reason, string(evidenceJSON), now,
	)
	if err != nil {
		return nil, err
	}
	if err := recordPulseModuleAudit(ctx, tx, normalized, module, pulseRunID, result, reason, evidence, PulseModuleAuditInput{}, now); err != nil {
		return nil, err
	}
	if err := clearPulseReviewRecoveryTx(ctx, tx, normalized, module); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	state, err := getPulseModuleStateByModule(ctx, db, normalized, module)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func markPulseModuleResultFromAgent(ctx context.Context, workspacePath, module, pulseRunID, result, reason string, evidence []string) (*PulseModuleState, error) {
	return markPulseModuleResultFromAgentWithAudit(ctx, workspacePath, module, pulseRunID, result, reason, evidence, PulseModuleAuditInput{})
}

func markPulseModuleResultFromAgentWithAudit(ctx context.Context, workspacePath, module, pulseRunID, result, reason string, evidence []string, audit PulseModuleAuditInput) (*PulseModuleState, error) {
	return markPulseModuleResultFromAgentWithAuditAndFindings(
		ctx, workspacePath, module, pulseRunID, result, reason, evidence, audit, nil,
	)
}

func markPulseModuleResultFromAgentWithAuditAndFindings(
	ctx context.Context,
	workspacePath, module, pulseRunID, result, reason string,
	evidence []string,
	audit PulseModuleAuditInput,
	dispositions []step_based_workflow.PulseFindingDisposition,
) (*PulseModuleState, error) {
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return nil, fmt.Errorf("module %q is not a valid Pulse module. Must be one of: %s", module, pulseModuleList())
	}
	pulseRunID = strings.TrimSpace(pulseRunID)
	result = strings.TrimSpace(strings.ToLower(result))
	switch result {
	case "done", "changed", "blocked", "failed", "skipped":
	default:
		return nil, fmt.Errorf("result %q is not valid. Must be one of: %s. Use changed only when files were modified, and pair it with changed_files, verification, and finding_dispositions",
			result, strings.Join(pulseModuleResultValues, ", "))
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required: one sentence stating the module's outcome, including the exact technical failure when result is blocked or failed")
	}

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	evidence = normalizePulseEvidence(evidence)
	evidenceJSON, _ := json.Marshal(evidence)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	resultExec, err := tx.ExecContext(ctx, `UPDATE pulse_module_state SET
			last_ran_at = ?, last_result = ?, last_result_reason = ?, evidence_json = ?, updated_at = ?
		WHERE workspace_path = ? AND module = ? AND last_pulse_run_id = ?
			AND last_decision = 'due' AND last_result = ''`,
		now, result, reason, string(evidenceJSON), now, normalized, module, pulseRunID)
	if err != nil {
		return nil, err
	}
	if changed, err := resultExec.RowsAffected(); err != nil {
		return nil, err
	} else if changed == 0 {
		existing, readErr := getPulseModuleStateByModule(ctx, tx, normalized, module)
		if readErr != nil {
			return nil, fmt.Errorf("Pulse module %q is not an unresolved due module for run %q", module, pulseRunID)
		}
		// Accept same-result replays, an evidenced repair after review, and a
		// verified correction of a premature failure. Never silently update only
		// the audit while returning the old (possibly failed) live module state.
		correction := (existing.LastResult == "failed" || existing.LastResult == "blocked" || existing.LastResult == "timed_out") &&
			(result == "done" || result == "changed") && len(normalizePulseEvidence(audit.Verification)) > 0 &&
			(len(evidence) > 0 || len(dispositions) > 0)
		supplement := existing.LastResult == "done" && result == "changed" && len(dispositions) > 0
		if existing.LastPulseRunID != pulseRunID || existing.LastDecision != "due" ||
			(existing.LastResult != result && !correction && !supplement) {
			return nil, fmt.Errorf("Pulse module %q for run %q is already terminal or belongs to another run; correcting failed/blocked/timed_out to done/changed requires verification and evidence or finding_dispositions", module, pulseRunID)
		}
		previous, err := json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_module_result_history
			(workspace_path, module, pulse_run_id, previous_state_json, previous_audit_json, replaced_at)
			SELECT ?, ?, ?, ?, COALESCE((SELECT json_object(
			'result', result, 'reason', reason, 'evidence_json', evidence_json,
			'changed_files_json', changed_files_json, 'verification_json', verification_json,
			'before_refs_json', before_refs_json, 'after_refs_json', after_refs_json, 'recorded_at', recorded_at)
			FROM pulse_module_audit WHERE workspace_path=? AND module=? AND pulse_run_id=?), '{}'), ?`,
			normalized, module, pulseRunID, string(previous), normalized, module, pulseRunID, now); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pulse_module_state SET last_ran_at=?, last_result=?,
			last_result_reason=?, evidence_json=?, updated_at=? WHERE workspace_path=? AND module=? AND last_pulse_run_id=?`,
			now, result, reason, string(evidenceJSON), now, normalized, module, pulseRunID); err != nil {
			return nil, err
		}
	}
	if err := recordPulseModuleAudit(ctx, tx, normalized, module, pulseRunID, result, reason, evidence, audit, now); err != nil {
		return nil, err
	}
	if err := step_based_workflow.RecordPulseFindingDispositionsTx(
		ctx, tx, module, pulseRunID, dispositions, now,
	); err != nil {
		return nil, err
	}
	if err := clearPulseReviewRecoveryTx(ctx, tx, normalized, module); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getPulseModuleStateByModule(ctx, db, normalized, module)
}

type pulseModuleAuditExecer interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func recordPulseModuleAudit(ctx context.Context, execer pulseModuleAuditExecer, workspacePath, module, pulseRunID, result, reason string, evidence []string, audit PulseModuleAuditInput, recordedAt string) error {
	marshal := func(values []string) string {
		encoded, _ := json.Marshal(normalizePulseEvidence(values))
		return string(encoded)
	}
	_, err := execer.ExecContext(ctx, `INSERT INTO pulse_module_audit (
			workspace_path, module, pulse_run_id, result, reason, evidence_json,
			changed_files_json, verification_json, before_refs_json, after_refs_json, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_path, module, pulse_run_id) DO UPDATE SET
			result=excluded.result,
			reason=excluded.reason,
			evidence_json=excluded.evidence_json,
			changed_files_json=excluded.changed_files_json,
			verification_json=excluded.verification_json,
			before_refs_json=excluded.before_refs_json,
			after_refs_json=excluded.after_refs_json,
			recorded_at=excluded.recorded_at`,
		workspacePath, module, pulseRunID, result, reason, marshal(evidence),
		marshal(audit.ChangedFiles), marshal(audit.Verification), marshal(audit.BeforeRefs), marshal(audit.AfterRefs), recordedAt)
	return err
}

// latestPulseReviewReceipts supplies an actual review baseline independently
// of current-pass state, whose result is cleared whenever Gate records a skip.
// Three bounded lookups; no new store or full historical transcript is needed.
func latestPulseReviewReceipts(ctx context.Context, workspacePath string) ([]PulseModuleAudit, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	receipts := []PulseModuleAudit{}
	if db == nil {
		return receipts, nil
	}
	defer db.Close()
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pulse_module_audit'`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return receipts, nil
	}
	for _, module := range pulseModuleOrder {
		receipt := PulseModuleAudit{WorkspacePath: normalized, Module: module}
		var evidenceJSON string
		err := db.QueryRowContext(ctx, `SELECT pulse_run_id, result, reason, evidence_json, recorded_at
			FROM pulse_module_audit WHERE workspace_path=? AND module=?
			AND result IN ('done','changed','failed','blocked','timed_out')
			ORDER BY recorded_at DESC, rowid DESC LIMIT 1`, normalized, module).Scan(
			&receipt.PulseRunID, &receipt.Result, &receipt.Reason, &evidenceJSON, &receipt.RecordedAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &receipt.Evidence); err != nil {
			return nil, fmt.Errorf("decode %s review receipt evidence: %w", module, err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

const pulseShadowDetectorLoopClosure = "loop_closure"

func recordPulseShadowSignalObservation(ctx context.Context, workspacePath, pulseRunID string, result loopclosure.Result, decisions []PulseWorklistDecision) error {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return fmt.Errorf("pulse_run_id is required: pass the scheduler-provided Pulse run id exactly as it appears in the prompt")
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	defer db.Close()

	signalsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		return err
	}
	decisionsJSON, err := json.Marshal(decisions)
	if err != nil {
		return err
	}
	recordedAt := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `INSERT INTO pulse_shadow_signal_observation (
			workspace_path, pulse_run_id, detector, detector_version, observed_at,
			coverage_status, coverage_reason, signals_json, gate_decisions_json, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_path, pulse_run_id, detector) DO NOTHING`,
		normalized, pulseRunID, pulseShadowDetectorLoopClosure, result.DetectorVersion,
		result.ObservedAt, result.CoverageStatus, result.CoverageReason,
		string(signalsJSON), string(decisionsJSON), recordedAt)
	return err
}

func getPulseShadowSignalObservations(ctx context.Context, workspacePath string, limit int) ([]PulseShadowSignalObservation, error) {
	if limit <= 0 {
		limit = 20
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return []PulseShadowSignalObservation{}, nil
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT workspace_path, pulse_run_id, detector,
			detector_version, observed_at, coverage_status, coverage_reason,
			signals_json, gate_decisions_json, recorded_at
		FROM pulse_shadow_signal_observation
		WHERE workspace_path = ?
		ORDER BY observed_at DESC
		LIMIT ?`, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PulseShadowSignalObservation{}
	for rows.Next() {
		var observation PulseShadowSignalObservation
		var signalsJSON, decisionsJSON string
		if err := rows.Scan(
			&observation.WorkspacePath,
			&observation.PulseRunID,
			&observation.Detector,
			&observation.DetectorVersion,
			&observation.ObservedAt,
			&observation.CoverageStatus,
			&observation.CoverageReason,
			&signalsJSON,
			&decisionsJSON,
			&observation.RecordedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(signalsJSON), &observation.Signals); err != nil {
			return nil, fmt.Errorf("decode shadow signals for %s: %w", observation.PulseRunID, err)
		}
		if err := json.Unmarshal([]byte(decisionsJSON), &observation.GateDecisions); err != nil {
			return nil, fmt.Errorf("decode shadow Gate decisions for %s: %w", observation.PulseRunID, err)
		}
		if observation.Signals == nil {
			observation.Signals = []loopclosure.Finding{}
		}
		if observation.GateDecisions == nil {
			observation.GateDecisions = []PulseWorklistDecision{}
		}
		out = append(out, observation)
	}
	return out, rows.Err()
}

func getPulseModuleStates(ctx context.Context, workspacePath string) ([]PulseModuleState, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return []PulseModuleState{}, nil
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT module, workspace_path, last_pulse_run_id, last_checked_at, last_ran_at,
		last_decision, last_reason, last_gate_decision, last_result, last_result_reason,
		next_check_at, next_check_after_run_id, cooldown_runs, evidence_json, updated_at
		FROM pulse_module_state WHERE workspace_path = ? ORDER BY module`, normalized)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []PulseModuleState
	for rows.Next() {
		state, err := scanPulseModuleState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, *state)
	}
	return states, rows.Err()
}

func (api *StreamingAPI) handleGetPulseModuleState(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	states, err := getPulseModuleStates(r.Context(), r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	commands, err := getPulseFinalCommandStates(r.Context(), r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	gateMode, err := getLatestPulseRunMode(r.Context(), r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	focusHistory, err := getPulseReviewFocusStates(r.Context(), r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	focusSelections, err := getPulseReviewFocusSelections(r.Context(), r.URL.Query().Get("workspace_path"), 250)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	shadowObservations, shadowErr := getPulseShadowSignalObservations(r.Context(), r.URL.Query().Get("workspace_path"), 20)
	shadowCoverage := map[string]string{}
	if shadowErr != nil {
		log.Printf("[PULSE] shadow signal observations unavailable: %v", shadowErr)
		shadowObservations = []PulseShadowSignalObservation{}
		shadowCoverage = map[string]string{"status": "unavailable", "reason": shadowErr.Error()}
	} else if len(shadowObservations) == 0 {
		shadowCoverage = map[string]string{"status": "not_instrumented", "reason": "no shadow observations recorded yet"}
	} else {
		shadowCoverage["status"] = shadowObservations[0].CoverageStatus
		if shadowObservations[0].CoverageReason != "" {
			shadowCoverage["reason"] = shadowObservations[0].CoverageReason
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":                    true,
		"modules":                    states,
		"commands":                   commands,
		"gate_mode":                  gateMode,
		"review_focus_history":       focusHistory,
		"review_focus_selections":    focusSelections,
		"shadow_signal_observations": shadowObservations,
		"shadow_signal_coverage":     shadowCoverage,
	})
}

func (api *StreamingAPI) handleGetPulseFindings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath, err := normalizeReportHumanInputWorkspacePath(r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	module := normalizePulseModule(r.URL.Query().Get("module"))
	if module != "" && !validPulseModules[module] {
		http.Error(w, fmt.Sprintf("module %q is not valid", module), http.StatusBadRequest)
		return
	}
	findings, err := step_based_workflow.LoadPulseFindingLifecycles(
		r.Context(), workspacePath, module, -1,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"findings": findings,
		"total":    len(findings),
	})
}

func (api *StreamingAPI) handleGetPulseReviews(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath, err := normalizeReportHumanInputWorkspacePath(r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	module := normalizePulseModule(r.URL.Query().Get("module"))
	if module != "" && !validPulseModules[module] {
		http.Error(w, fmt.Sprintf("module %q is not valid", module), http.StatusBadRequest)
		return
	}
	receipts, err := step_based_workflow.LoadPulseReviewReceipts(r.Context(), workspacePath, module, -1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	coverage, audits, err := loadPulseReviewActivity(r.Context(), workspacePath, module)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reports, err := listPulseReviewReports(workspacePath, module)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "reviews": receipts, "total": len(receipts), "coverage": coverage, "audits": audits, "reports": reports})
}

func (api *StreamingAPI) handleGetPulseAgentMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath, err := normalizeReportHumanInputWorkspacePath(r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	module := strings.TrimSpace(r.URL.Query().Get("module"))
	if module != "" {
		module = normalizePulseModule(module)
		if !validPulseModules[module] {
			http.Error(w, fmt.Sprintf("module %q is not valid", module), http.StatusBadRequest)
			return
		}
	}
	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	if role != "" && role != "reviewer" && role != "fixer" {
		http.Error(w, "role must be reviewer or fixer", http.StatusBadRequest)
		return
	}
	metrics, err := step_based_workflow.LoadPulseAgentMetrics(
		r.Context(), workspacePath, r.URL.Query().Get("pulse_run_id"), module, role, -1,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"metrics": metrics,
		"total":   len(metrics),
	})
}

func (api *StreamingAPI) handleGetPulseImpact(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath, err := normalizeReportHumanInputWorkspacePath(r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ledger, err := step_based_workflow.LoadPulseImpactLedger(r.Context(), workspacePath, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "impact": ledger})
}

func (api *StreamingAPI) handleGetPulseContext(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath, err := normalizeReportHumanInputWorkspacePath(r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	records, err := step_based_workflow.LoadPulseContextRecords(r.Context(), workspacePath, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "records": records, "total": len(records)})
}

func (api *StreamingAPI) handleGetPulseEvalResults(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath, err := normalizeReportHumanInputWorkspacePath(r.URL.Query().Get("workspace_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	results, err := step_based_workflow.LoadEvalResults(r.Context(), workspacePath, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "results": results})
}

func getPulseWorklistForRun(ctx context.Context, workspacePath, pulseRunID string) (map[string]PulseModuleState, bool, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil {
		return nil, false, err
	}
	if db == nil {
		return map[string]PulseModuleState{}, false, nil
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, false, err
	}
	rows, err := db.QueryContext(ctx, `SELECT module, workspace_path, last_pulse_run_id, last_checked_at, last_ran_at,
		last_decision, last_reason, last_gate_decision, last_result, last_result_reason,
		next_check_at, next_check_after_run_id, cooldown_runs, evidence_json, updated_at
		FROM pulse_module_state WHERE workspace_path = ? AND last_pulse_run_id = ?`, normalized, strings.TrimSpace(pulseRunID))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := map[string]PulseModuleState{}
	for rows.Next() {
		state, err := scanPulseModuleState(rows)
		if err != nil {
			return nil, false, err
		}
		out[state.Module] = *state
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return out, len(out) > 0, nil
}

func getPulseRunMode(ctx context.Context, workspacePath, pulseRunID string) (*PulseRunMode, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return nil, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	mode := &PulseRunMode{}
	err = db.QueryRowContext(ctx, `SELECT workspace_path, pulse_run_id, mode, reason, recorded_at
		FROM pulse_run_mode WHERE workspace_path = ? AND pulse_run_id = ?`, normalized, strings.TrimSpace(pulseRunID)).Scan(
		&mode.WorkspacePath, &mode.PulseRunID, &mode.Mode, &mode.Reason, &mode.RecordedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mode, nil
}

func getLatestPulseRunMode(ctx context.Context, workspacePath string) (*PulseRunMode, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return nil, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	mode := &PulseRunMode{}
	err = db.QueryRowContext(ctx, `SELECT workspace_path, pulse_run_id, mode, reason, recorded_at
		FROM pulse_run_mode WHERE workspace_path = ? ORDER BY recorded_at DESC LIMIT 1`, normalized).Scan(
		&mode.WorkspacePath, &mode.PulseRunID, &mode.Mode, &mode.Reason, &mode.RecordedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mode, nil
}

type pulseModuleScanner interface {
	Scan(dest ...interface{}) error
}

func getPulseModuleStateByModule(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, workspacePath, module string) (*PulseModuleState, error) {
	row := db.QueryRowContext(ctx, `SELECT module, workspace_path, last_pulse_run_id, last_checked_at, last_ran_at,
		last_decision, last_reason, last_gate_decision, last_result, last_result_reason,
		next_check_at, next_check_after_run_id, cooldown_runs, evidence_json, updated_at
		FROM pulse_module_state WHERE workspace_path = ? AND module = ?`, workspacePath, module)
	return scanPulseModuleState(row)
}

func scanPulseModuleState(row pulseModuleScanner) (*PulseModuleState, error) {
	var state PulseModuleState
	var evidenceJSON string
	if err := row.Scan(&state.Module, &state.WorkspacePath, &state.LastPulseRunID, &state.LastCheckedAt, &state.LastRanAt,
		&state.LastDecision, &state.LastReason, &state.LastGateDecision, &state.LastResult, &state.LastResultReason,
		&state.NextCheckAt, &state.NextCheckAfterRunID, &state.CooldownRuns,
		&evidenceJSON, &state.UpdatedAt); err != nil {
		return nil, err
	}
	if state.LastGateDecision == "" {
		state.LastGateDecision = state.LastDecision
	}
	if strings.TrimSpace(evidenceJSON) != "" {
		_ = json.Unmarshal([]byte(evidenceJSON), &state.Evidence)
	}
	if state.Evidence == nil {
		state.Evidence = []string{}
	}
	return &state, nil
}

func createPulseWorklistTools() ([]llmtypes.Tool, map[string]interface{}, map[string]string) {
	moduleEnum := append([]string(nil), pulseModuleOrder...)
	reviewIdentityProperties := map[string]interface{}{
		"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
		"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Use \"current\" for this active Pulse conversation."},
		"module":         map[string]interface{}{"type": "string", "enum": moduleEnum},
	}
	findingProperties := map[string]interface{}{}
	for key, value := range reviewIdentityProperties {
		findingProperties[key] = value
	}
	for key, value := range map[string]interface{}{
		"issue_id":          map[string]interface{}{"type": "string", "description": "Optional visible PUL issue_id from the active or closed issue index returned by get_pulse_state(view=\"backlog\", detail=\"compact\"). Supply it when this is new evidence for an existing root cause; a closed match is reopened. Omit only for a genuinely new root issue."},
		"step_id":           map[string]interface{}{"type": "string", "description": "The plan step this finding is about, e.g. the step's id field from plan.json. Required for a plan_drift_review fail-status check's linked finding (record_plan_drift_review verifies the finding is filed against this exact step). Omit only for a genuinely module-wide finding that is not about one specific step; updating an existing issue by issue_id keeps that issue's original step identity regardless of what is passed here."},
		"concern":           map[string]interface{}{"type": "string", "description": "Current concise root-cause statement. It may be reworded when issue_id is supplied; wording never creates a new identity by itself."},
		"issue_kind":        map[string]interface{}{"type": "string", "enum": []string{"workflow_issue", "harness_issue"}},
		"classification":    map[string]interface{}{"type": "string"},
		"severity":          map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high", "critical"}},
		"summary":           map[string]interface{}{"type": "string"},
		"impact":            map[string]interface{}{"type": "string"},
		"evidence":          map[string]interface{}{"type": "array", "minItems": 1, "items": map[string]interface{}{"type": "string"}},
		"recommended_route": map[string]interface{}{"type": "string", "enum": []string{"decision_required", "evidence_wait", "fixer_handoff"}},
		"human_input_id":    map[string]interface{}{"type": "string", "description": "Required with recommended_route=decision_required. Create the pending reviewer-owned decision first and pass its exact id so the finding is atomically linked as awaiting_user."},
		"next_check":        map[string]interface{}{"type": "string"},
		"workaround":        map[string]interface{}{"type": "string"},
		"target_key":        map[string]interface{}{"type": "string"},
		"reproduction": map[string]interface{}{
			"type": "object", "additionalProperties": false,
			"properties": map[string]interface{}{
				"safe": map[string]interface{}{"type": "boolean"}, "setup": map[string]interface{}{"type": "string"},
				"action": map[string]interface{}{"type": "string"}, "expected": map[string]interface{}{"type": "string"},
				"observed": map[string]interface{}{"type": "string"}, "limitations": map[string]interface{}{"type": "string"},
			},
		},
	} {
		findingProperties[key] = value
	}
	recordFindingTool := llmtypes.Tool{Type: "function", Function: &llmtypes.FunctionDefinition{
		Name:        "record_pulse_finding",
		Description: "Persist one complete Pulse reviewer finding directly to the SQLite lifecycle. First inspect the compact active and closed issue index and reason about semantic sameness; request targeted full detail only for candidate issue_ids. For an existing root cause, including a closed one, supply its issue_id and update/reopen it; do not create a second issue because wording, evidence paths, or symptoms differ. Omit issue_id only for a genuinely distinct root cause with a different repair or owner. Do not encode findings in the final response. Backup, publish, and notify waiting for their ordered finalizer stage are not findings; report only a real terminal failure after that command ran.",
		Parameters:  llmtypes.NewParameters(map[string]interface{}{"type": "object", "additionalProperties": false, "properties": findingProperties, "required": []string{"workspace_path", "pulse_run_id", "module", "concern", "issue_kind", "classification", "severity", "summary", "impact", "evidence"}}),
	}}
	recordFocusTool := llmtypes.Tool{Type: "function", Function: &llmtypes.FunctionDefinition{
		Name:        "record_pulse_review_focus",
		Description: "Persist one deep focus selected for this technical or strategic review. Call once for every focus actually investigated, then complete the module review. The reviewer agentically chooses the smallest sufficient set from route size, distinct evidence boundaries, unresolved risk, prior coverage, and marginal value; there is no fixed focus count. Strategic categories are optional coverage labels, not a reasoning checklist; a new strategic lens may be recorded when none fits. This is not a findings store and does not replace the run-scoped Markdown checkpoint.",
		Parameters: llmtypes.NewParameters(map[string]interface{}{"type": "object", "additionalProperties": false, "properties": map[string]interface{}{
			"workspace_path": reviewIdentityProperties["workspace_path"], "pulse_run_id": reviewIdentityProperties["pulse_run_id"], "module": reviewIdentityProperties["module"],
			"focus_key":        map[string]interface{}{"type": "string", "maxLength": 64, "pattern": "^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$", "description": "Technical Review must use one of: " + strings.Join(pulseReviewFocusCatalog[pulseModuleTechnicalReview], ", ") + ". Strategic Review may reuse a fitting category or record a descriptive lowercase snake_case key (maximum 64 characters); explain new lenses in selection_reason and reuse them later."},
			"route_scope":      map[string]interface{}{"type": "string", "description": "Canonical route/group/sub-workflow this focus covered. Leave empty only when the evidence and conclusion are genuinely workflow-wide."},
			"priority_class":   map[string]interface{}{"type": "string", "enum": []string{"critical_regression", "matured_verification", "answered_decision", "new_or_changed", "overdue", "oldest_remaining"}},
			"selection_reason": map[string]interface{}{"type": "string"}, "verdict": map[string]interface{}{"type": "string"},
			"evidence":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"issue_ids":         map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string", "pattern": "^PUL-[A-Za-z0-9]+$"}},
			"deferred_focuses":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string", "maxLength": 64, "pattern": "^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$"}},
			"next_check_at":     map[string]interface{}{"type": "string", "description": "Optional explicit RFC3339 UTC timestamp or date; do not encode this in pulse_run_id."},
			"next_check_reason": map[string]interface{}{"type": "string"},
		}, "required": []string{"workspace_path", "pulse_run_id", "module", "focus_key", "priority_class", "selection_reason"}}),
	}}
	mergeIssuesTool := llmtypes.Tool{Type: "function", Function: &llmtypes.FunctionDefinition{
		Name:        "merge_pulse_issues",
		Description: "Merge symptom-level duplicate Pulse issues into one canonical root-cause issue after a semantic backlog review. This never deletes history: it retires each duplicate from the active queue, links it to the canonical PUL issue, and preserves all attempts, events, and verification records. Use only when the issues share the same causal defect and compatible repair/verification boundary; never merge merely because they involve the same file or module.",
		Parameters: llmtypes.NewParameters(map[string]interface{}{"type": "object", "additionalProperties": false, "properties": map[string]interface{}{
			"workspace_path":      map[string]interface{}{"type": "string"},
			"canonical_issue_id":  map[string]interface{}{"type": "string", "description": "The one visible PUL issue that remains active."},
			"duplicate_issue_ids": map[string]interface{}{"type": "array", "minItems": 1, "items": map[string]interface{}{"type": "string"}, "description": "Visible PUL issue IDs to retire into the canonical issue."},
			"reason":              map[string]interface{}{"type": "string", "description": "One sentence naming the shared root cause and why one repair covers the duplicates."},
		}, "required": []string{"workspace_path", "canonical_issue_id", "duplicate_issue_ids", "reason"}}),
	}}
	reconcileTool := llmtypes.Tool{Type: "function", Function: &llmtypes.FunctionDefinition{
		Name: "record_pulse_migration_reconciliation",
		Description: "Run one idempotent Pulse-register migration for a workflow-contract upgrade. Never edits the workflow plan or schedules. Pass scope to choose which:\n" +
			"scope=\"lifecycle\": the close-on-applied compatibility migration. Closes every active issue with a recorded applied repair and changed files, moves legacy unfixed waiting rows back to the active register, retires merged aliases, and preserves all history. Use for the workflow-contract v1.0.32 and v1.0.33 upgrades.\n" +
			"scope=\"actionable_backlog\": the actionable-backlog migration. First applies the same close-on-applied lifecycle reconciliation as scope=\"lifecycle\", then retires untyped legacy observations that were never promoted to canonical findings and hands typed harness issues to the platform register. Preserves all evidence and leaves human decisions and evidence waits visible but outside the workflow repair target. Use for the workflow-contract v1.0.34 upgrade.",
		Parameters: llmtypes.NewParameters(map[string]interface{}{"type": "object", "additionalProperties": false, "properties": map[string]interface{}{
			"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
			"scope":          map[string]interface{}{"type": "string", "enum": []string{"lifecycle", "actionable_backlog"}, "description": "Which migration to run. actionable_backlog is a superset of lifecycle -- it already applies the lifecycle step first."},
		}, "required": []string{"workspace_path", "scope"}}),
	}}
	recordTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "record_pulse_worklist",
			Description: fmt.Sprintf("Record the dynamic Pulse worklist for this run in the workflow's db/db.sqlite. Pulse Gate must call this exactly once after choosing the agent-owned pass mode and deciding which perspectives are due or skipped. backlog_drain verifies and repairs retained issues without broad discovery; discovery investigates materially new evidence; strategy is for selected product/goal work; observe runs no reviewer or fixer. Go validates the declared mode but never selects it. The decisions array must contain exactly one entry for each current Pulse module: %s. This is a small scheduling receipt, not a review-plan payload: in each decision use only module, due, reason, evidence, next_check_at, next_check_after_run_id, and cooldown_runs. Never send focuses, route_scope, issue_ids, deferred_focuses, decision, or other review-plan fields; unknown fields reject the whole worklist. State scope and lower-priority deferrals in reason/evidence and the next-check boundary. The later reviewer independently selects and persists actual focus coverage after it has inspected evidence. Do not pass retired module names. Every skipped module must include next_check_at, next_check_after_run_id, or a positive cooldown_runs value.", strings.Join(pulseModuleOrder, ", ")),
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Scheduler-provided Pulse run id. Use exactly the id in the prompt."},
					"mode":           map[string]interface{}{"type": "string", "enum": pulseRunModeValues, "description": "Agent-selected Pulse pass shape."},
					"mode_reason":    map[string]interface{}{"type": "string", "description": "Evidence why this mode is the cheapest sufficient next action."},
					"decisions": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]interface{}{
								"module":                  map[string]interface{}{"type": "string", "enum": moduleEnum},
								"due":                     map[string]interface{}{"type": "boolean"},
								"reason":                  map[string]interface{}{"type": "string", "description": "Plain-language reason with the evidence basis."},
								"evidence":                map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								"next_check_at":           map[string]interface{}{"type": "string", "description": "Optional RFC3339 timestamp or YYYY-MM-DD date for the next normal check."},
								"next_check_after_run_id": map[string]interface{}{"type": "string", "description": "Optional run id/folder after which to check again."},
								"cooldown_runs":           map[string]interface{}{"type": "integer", "minimum": 0, "description": "Optional evidence-collection wait in relevant workflow runs, not chat turns or tool calls. Explain comparable route/group scope and the baseline review in reason/evidence. Reassess progress without blindly restarting the wait at every Gate check; new materially harmful evidence can override it."},
							},
							"required": []string{"module", "due", "reason"},
						},
					},
				},
				"required": []string{"workspace_path", "pulse_run_id", "mode", "mode_reason", "decisions"},
			}),
		},
	}
	stateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name: "get_pulse_state",
			Description: fmt.Sprintf("Read Pulse state from the workflow's db/db.sqlite. One read tool with four views; typed SQLite state is the only source of truth.\n"+
				"view=\"module\": per-module cadence and results so Pulse Gate can decide what is due, plus the complete active concern backlog, externally owned suppressed concerns, plan-change backlog, reviewer history, impact ledger, and read-only loop-closure facts. Read this before record_pulse_worklist. Loop-closure findings are evidence Gate may weigh; they do not mandate a module or authorize mutation. A concern with a high seen_count has been reported on that many runs and should weigh heavily.\n"+
				"view=\"backlog\": a compact active issue/observation index plus the closed canonical issue index by default. Read it once to select or semantically reuse public PUL ids. To inspect lifecycle evidence, call again with detail=\"full\" and 1-20 exact issue_ids; broad full-history reads are rejected. Optional module filter.\n"+
				"view=\"review\": one compact reviewer receipt as JSON with validated structured verifications. Requires the stored pulse-run receipt id and module; reviewer prose and Markdown are not stored.\n"+
				"view=\"focus_agenda\": the compact durable deep-review coverage agenda for technical_review or strategic_review — every canonical focus, global and route-specific review counts, last verdict, due boundary, and deferred history. Requires module. Pass route_scope when reviewing one route/group/sub-workflow. Read this after view=\"module\"/\"backlog\" and before agentically choosing the smallest sufficient focus set; it is not blind round-robin.\n"+
				"Close a real finding only through a verified finding_disposition on record_pulse_result; resolve_run_concern is limited to acknowledgment or rejection. Modules: %s.", pulseModuleList()),
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"view":           map[string]interface{}{"type": "string", "enum": pulseStateViewValues, "description": "Which Pulse state to read: module cadence, the finding backlog, one saved review, or the focus coverage agenda."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Optional current Pulse run id. With view=module, returns that Gate's persisted pass mode."},
					"module":         map[string]interface{}{"type": "string", "description": "Optional owning-module filter for view=\"backlog\" (omit for the complete backlog). Required for view=\"review\" and view=\"focus_agenda\". Ignored for view=\"module\"."},
					"review_run_id":  map[string]interface{}{"type": "string", "description": "Required for view=\"review\": the review run id from the reviewer's completion notification."},
					"detail":         map[string]interface{}{"type": "string", "enum": []string{"compact", "full"}, "description": "For view=\"backlog\" only. compact (default) returns active issues/observations and the closed canonical issue index for semantic reuse. full requires issue_ids and returns complete lifecycle evidence only for those ids."},
					"issue_ids": map[string]interface{}{
						"type": "array", "minItems": 1, "maxItems": 20, "uniqueItems": true,
						"items":       map[string]interface{}{"type": "string", "pattern": "^PUL-[A-Za-z0-9]+$"},
						"description": "For view=\"backlog\" with detail=\"full\": 1-20 exact public PUL ids selected from the compact issues or observations index.",
					},
					"route_scope": map[string]interface{}{"type": "string", "description": "For view=\"focus_agenda\" only. Optional canonical route/group/sub-workflow scope. Use the route id or stable group label from plan/run evidence; leave empty only for genuinely workflow-wide evidence."},
					"limit":       map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "For view=\"focus_agenda\" only."},
				},
				"required": []string{"workspace_path", "view"},
			}),
		},
	}
	resultTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name: "record_pulse_result",
			Description: fmt.Sprintf("Record one Pulse outcome in the workflow's db/db.sqlite. Pass exactly one of module or command.\n"+
				"module (%s): the terminal result of a selected Pulse module after its review and Fixer work complete — done, changed, blocked, failed, or skipped. This writes the module audit and per-finding lifecycle atomically. For result=changed, changed_files, verification, and finding_dispositions are required.\n"+
				"Never declare a review failed merely because a checkpoint or receipt is absent while its child executor is active. Wait for terminal child evidence. An evidenced same-run correction from failed/blocked/timed_out to done/changed requires verification plus evidence or finding_dispositions; changed still requires its full repair proof. Accepted corrections update live state and audit together and retain prior receipts in history.\n"+
				"command (%s): the live or final status of one Pulse final command — running, done, skipped, blocked, or failed. The combined Pulse finalizer marks each command running before work and then terminal immediately after it finishes.\n"+
				"Fix attempts are opened by the backend from the disposition itself; there is no separate attempt tool and no attempt_id to carry. A fixed_verified finding needs changed_files plus only passed immediate checks. changed_unverified means a repair was successfully applied but no stronger proof was available; it closes immediately, and a later normal workflow observation semantically linked to the same issue_id reopens it. Pulse does not schedule a separate verification run. queued_for_engineering means a safe workflow repair exists but was deliberately not attempted in this pass; it requires next_check naming the next Engineering/Pulse pass and remains in Gate's active queue. external_action_required permanently removes a diagnosed real finding from Pulse's active queue and requires external_owner, reason_code, and reopen_condition; use it only when workflow tools cannot act. A failed immediate check keeps the concern open. awaiting_run is only for a real finding where no fix was applied and required evidence does not exist yet; it requires next_check naming that evidence. blocked means there is genuinely no safe action at all; never use it merely because work was deferred, deprioritized, or not selected in this pass. awaiting_user requires human_input_id naming a still-pending create_human_input_request. For strategic_review, proposal_only is accepted only with a concrete next_check evidence boundary; an actionable recommendation must use awaiting_user linked to a pending strategic_review decision. before_refs and after_refs are paired audit references. issue_id is the sole public identity; inspect existing issue text and history and reuse the issue_id for the same semantic root cause.",
				strings.Join(pulseModuleOrder, ", "), strings.Join(pulseFinalCommandOrder, ", ")),
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Scheduler-provided Pulse run id."},
					"module":         map[string]interface{}{"type": "string", "enum": moduleEnum, "description": "The Pulse module this result belongs to. Pass this or command, never both."},
					"command":        map[string]interface{}{"type": "string", "enum": pulseFinalCommandOrder, "description": "The Pulse final command this status belongs to. Pass this or module, never both."},
					"result":         map[string]interface{}{"type": "string", "enum": pulseResultValues, "description": "For a module: done, changed, blocked, failed, or skipped. For a command: running, done, skipped, blocked, or failed."},
					"reason":         map[string]interface{}{"type": "string", "description": "One-sentence result summary."},
					"evidence":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Module results only."},
					"changed_files":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Exact workspace-relative files changed by this module. Required when result=changed; otherwise omit."},
					"verification":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Agent-reported checks performed and their observed outcomes. Required when result=changed; otherwise include only when useful. These records are audit evidence, not backend-executed proof."},
					"before_refs":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional agent-supplied pre-change hashes, versions, or cursors. When present, provide an equal-length after_refs audit pair."},
					"after_refs":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional agent-supplied post-change hashes, versions, or cursors paired positionally with before_refs."},
					"finding_dispositions": map[string]interface{}{
						"type":        "array",
						"description": "Per-finding outcome. Required for changed modules and whenever a reviewer returned trackable findings.",
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]interface{}{
								"issue_id":         map[string]interface{}{"type": "string", "description": "The visible issue_id (for example PUL-DBA2B19E) from get_pulse_state(view=\"backlog\", detail=\"compact\"). This is the only finding identity to send; the backend resolves the internal fingerprint."},
								"disposition":      map[string]interface{}{"type": "string", "enum": []string{"fixed_verified", "verified_no_change", "changed_unverified", "proposal_only", "awaiting_user", "queued_for_engineering", "awaiting_run", "blocked", "external_action_required", "failed", "rejected"}},
								"summary":          map[string]interface{}{"type": "string"},
								"changed_files":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								"before_refs":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								"after_refs":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								"next_check":       map[string]interface{}{"type": "string"},
								"external_owner":   map[string]interface{}{"type": "string", "enum": []string{"platform", "user", "vendor", "workflow_owner"}, "description": "Required for external_action_required."},
								"reason_code":      map[string]interface{}{"type": "string", "description": "Required stable reason such as missing_platform_tool, permission_boundary, vendor_issue, policy, or accepted_risk."},
								"reopen_condition": map[string]interface{}{"type": "string", "description": "Required evidence/capability/user boundary that makes this actionable again."},
								"human_input_id":   map[string]interface{}{"type": "string", "description": "Required for awaiting_user: the id returned by create_human_input_request for the decision this finding is waiting on. The request must exist and still be pending, so the operator has something real to answer."},
								"verification": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": false,
										"properties": map[string]interface{}{
											"check":    map[string]interface{}{"type": "string"},
											"verdict":  map[string]interface{}{"type": "string", "enum": []string{"passed", "failed", "inconclusive"}},
											"expected": map[string]interface{}{"type": "string"},
											"observed": map[string]interface{}{"type": "string"},
											"evidence": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
										},
										"required": []string{"check", "verdict"},
									},
								},
							},
							"required": []string{"issue_id", "disposition", "summary"},
						},
					},
				},
				"required": []string{"workspace_path", "pulse_run_id", "result", "reason"},
			}),
		},
	}
	impactTool, impactExecutor := createRecordPulseImpactTool()

	executors := map[string]interface{}{
		"record_pulse_finding": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			pulseRunID, _ := args["pulse_run_id"].(string)
			pulseRunID = pulseRunIDForSession(ctx, pulseRunID)
			module, _ := args["module"].(string)
			if err := validatePulseToolRunID(ctx, pulseRunID); err != nil {
				return "", err
			}
			reproduction := step_based_workflow.PulseFindingReproduction{}
			if raw, ok := args["reproduction"].(map[string]interface{}); ok {
				reproduction.Safe, _ = raw["safe"].(bool)
				reproduction.Setup, _ = raw["setup"].(string)
				reproduction.Action, _ = raw["action"].(string)
				reproduction.Expected, _ = raw["expected"].(string)
				reproduction.Observed, _ = raw["observed"].(string)
				reproduction.Limitations, _ = raw["limitations"].(string)
			}
			input := step_based_workflow.PulseReviewFindingInput{IssueID: stringToolArg(args, "issue_id"), StepID: stringToolArg(args, "step_id"), Concern: stringToolArg(args, "concern"), Module: module, HumanInputID: stringToolArg(args, "human_input_id"), PulseFindingDetails: step_based_workflow.PulseFindingDetails{
				TargetKey: stringToolArg(args, "target_key"), IssueKind: stringToolArg(args, "issue_kind"),
				RecommendedRoute: stringToolArg(args, "recommended_route"), NextCheck: stringToolArg(args, "next_check"), Classification: stringToolArg(args, "classification"),
				Severity: stringToolArg(args, "severity"), Summary: stringToolArg(args, "summary"), Impact: stringToolArg(args, "impact"), Workaround: stringToolArg(args, "workaround"),
				Evidence: stringSliceFromToolArg(args["evidence"]), Reproduction: reproduction,
			}}
			reviewRunID := pulseReviewRunIDForSession(ctx, pulseRunID)
			record, err := step_based_workflow.RecordPulseReviewFinding(ctx, workspacePath, pulseRunID, reviewRunID, input)
			if err != nil {
				return "", err
			}
			encoded, _ := json.Marshal(record)
			return string(encoded), nil
		},
		"merge_pulse_issues": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			merged, err := step_based_workflow.MergePulseFindingIssues(ctx, workspacePath,
				stringToolArg(args, "canonical_issue_id"), stringSliceFromToolArg(args["duplicate_issue_ids"]), stringToolArg(args, "reason"))
			if err != nil {
				return "", err
			}
			payload, _ := json.Marshal(map[string]interface{}{"status": "merged", "merged_count": merged, "canonical_issue_id": stringToolArg(args, "canonical_issue_id")})
			return string(payload), nil
		},
		"record_pulse_review_focus": func(ctx context.Context, args map[string]interface{}) (string, error) {
			pulseRunID := pulseRunIDForSession(ctx, stringToolArg(args, "pulse_run_id"))
			if err := validatePulseToolRunID(ctx, pulseRunID); err != nil {
				return "", err
			}
			focus, err := recordPulseReviewFocus(ctx, stringToolArg(args, "workspace_path"), pulseRunID, stringToolArg(args, "module"), stringToolArg(args, "focus_key"), stringToolArg(args, "route_scope"), stringToolArg(args, "priority_class"), stringToolArg(args, "selection_reason"), stringToolArg(args, "verdict"), stringToolArg(args, "next_check_at"), stringToolArg(args, "next_check_reason"), stringSliceFromToolArg(args["evidence"]), stringSliceFromToolArg(args["issue_ids"]), stringSliceFromToolArg(args["deferred_focuses"]))
			if err != nil {
				return "", err
			}
			encoded, _ := json.Marshal(focus)
			return string(encoded), nil
		},
		"record_pulse_worklist": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			pulseRunID, _ := args["pulse_run_id"].(string)
			pulseRunID = pulseRunIDForSession(ctx, pulseRunID)
			if err := validatePulseToolRunID(ctx, pulseRunID); err != nil {
				return "", err
			}
			decisions, err := pulseWorklistDecisionsFromArgs(args["decisions"])
			if err != nil {
				return "", err
			}
			normalized, err := normalizeReportHumanInputWorkspacePath(workspacePath)
			if err != nil {
				return "", err
			}
			// Capture the deterministic result before writing this Gate pass so
			// answer_not_applied is evaluated against the preceding completed
			// pass. The result is retained only for shadow comparison and is
			// never returned to the Gate that made these decisions.
			shadowResult := loopclosure.Check(ctx, normalized, time.Now().UTC())
			mode := stringToolArg(args, "mode")
			modeReason := stringToolArg(args, "mode_reason")
			states, err := recordPulseWorklistOnceWithShadowAndMode(ctx, normalized, pulseRunID, mode, modeReason, decisions, shadowResult)
			if err != nil {
				return "", err
			}
			payload, _ := json.Marshal(map[string]interface{}{"status": "recorded", "mode": mode, "mode_reason": modeReason, "modules": states})
			return string(payload), nil
		},
		"get_pulse_state": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			view, _ := args["view"].(string)
			view = strings.ToLower(strings.TrimSpace(view))
			if strings.TrimSpace(workspacePath) == "" {
				// Every view opens the workflow's own db/db.sqlite. Without the
				// path there is no database to read, and the empty-result answer
				// reads as "nothing is tracked" rather than "you addressed nothing".
				return "", fmt.Errorf("get_pulse_state requires workspace_path: the workflow-relative path, e.g. Workflow/social-media")
			}
			switch view {
			case pulseStateViewModule:
				return readPulseModuleStateView(ctx, workspacePath, stringToolArg(args, "pulse_run_id"))
			case pulseStateViewBacklog:
				module, _ := args["module"].(string)
				return readPulseBacklogViewWithOptions(ctx, workspacePath, module, stringToolArg(args, "detail"), stringSliceFromToolArg(args["issue_ids"]))
			case pulseStateViewReview:
				module, _ := args["module"].(string)
				reviewRunID, _ := args["review_run_id"].(string)
				return readPulseReviewView(ctx, workspacePath, reviewRunID, module)
			case pulseStateViewFocusAgenda:
				module, _ := args["module"].(string)
				return readPulseFocusAgendaView(ctx, workspacePath, module, stringToolArg(args, "route_scope"), intToolArg(args, "limit"))
			default:
				return "", fmt.Errorf("view %q is not a valid Pulse state view. Must be one of: %s. Use %q for module cadence and the active concern backlog, %q for the durable finding backlog, %q for one saved reviewer result (which also needs review_run_id and module), and %q for the deep-review focus coverage agenda (which also needs module)",
					view, strings.Join(pulseStateViewValues, ", "),
					pulseStateViewModule, pulseStateViewBacklog, pulseStateViewReview, pulseStateViewFocusAgenda)
			}
		},
		"record_pulse_result": func(ctx context.Context, args map[string]interface{}) (string, error) {
			return recordPulseResultFromToolArgs(ctx, args)
		},
		"record_pulse_migration_reconciliation": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath := stringToolArg(args, "workspace_path")
			switch scope := strings.TrimSpace(stringToolArg(args, "scope")); scope {
			case "lifecycle":
				result, err := step_based_workflow.ReconcilePulseFindingLifecycle(ctx, workspacePath)
				if err != nil {
					return "", err
				}
				payload, _ := json.Marshal(result)
				return string(payload), nil
			case "actionable_backlog":
				result, err := step_based_workflow.ReconcilePulseActionableBacklog(ctx, workspacePath)
				if err != nil {
					return "", err
				}
				payload, _ := json.Marshal(result)
				return string(payload), nil
			default:
				return "", fmt.Errorf("scope %q is not valid; must be \"lifecycle\" or \"actionable_backlog\"", scope)
			}
		},
		"record_pulse_impact": impactExecutor,
	}
	categories := map[string]string{
		"record_pulse_finding":                  "workflow",
		"merge_pulse_issues":                    "workflow",
		"record_pulse_review_focus":             "workflow",
		"record_pulse_worklist":                 "workflow",
		"get_pulse_state":                       "workflow",
		"record_pulse_result":                   "workflow",
		"record_pulse_impact":                   "workflow",
		"resolve_run_concern":                   "workflow",
		"record_pulse_migration_reconciliation": "workflow",
		"record_pulse_module_due":               "workflow",
	}
	declareDueTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "record_pulse_module_due",
			Description: "Establish this conversation's OWN due claim for one Pulse module, so its later record_pulse_result call is accepted. Only for a manual, non-Gate-scheduled review that runs a canonical module's real procedure on demand — for example /review-artifact-drift's Part 1 running plan_drift_review outside the normal Pulse schedule. record_pulse_result's write is due-gated: it only accepts a result when the durable worklist already shows this module due for this exact pulse_run_id, which Gate normally records; a manual invocation has no such Gate-recorded row without calling this first. Refused if a real scheduled Pulse pass is currently mid-flight for this exact module (already due, no result recorded yet, under a different pulse_run_id) — wait for it to finish rather than risk corrupting its state. Never call this for a module Gate itself would decide; it exists only for a manual on-demand invocation of a module's real procedure.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Use \"current\" for this active conversation."},
					"module":         map[string]interface{}{"type": "string", "enum": moduleEnum},
					"reason":         map[string]interface{}{"type": "string", "description": "Why this manual invocation is running the module's real procedure now."},
				},
				"required": []string{"workspace_path", "pulse_run_id", "module", "reason"},
			}),
		},
	}
	executors["record_pulse_module_due"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		pulseRunID := pulseRunIDForSession(ctx, stringToolArg(args, "pulse_run_id"))
		if err := validatePulseToolRunID(ctx, pulseRunID); err != nil {
			return "", err
		}
		workspacePath := stringToolArg(args, "workspace_path")
		module := stringToolArg(args, "module")
		reason := stringToolArg(args, "reason")
		if err := recordPulseModuleDueForManualReview(ctx, workspacePath, pulseRunID, module, reason); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s is now due for this conversation (run %s); record_pulse_result(module=%q, ...) will be accepted.", normalizePulseModule(module), pulseRunID, normalizePulseModule(module)), nil
	}
	resolveConcernTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "resolve_run_concern",
			Description: "Acknowledge or reject a Pulse issue returned by get_pulse_state(view=\"backlog\"). Use rejected only when evidence shows it is not a problem; rejected concerns stay closed if the same behavior recurs. Use acknowledged when it is real but deliberately deferred. This tool cannot close a real bug as resolved: use record_pulse_result with a verified finding_disposition so changed files and test evidence are retained. Absence is never evidence of a fix.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"issue_id":       map[string]interface{}{"type": "string", "description": "The visible issue_id from get_pulse_state(view=\"backlog\", detail=\"compact\")."},
					"status":         map[string]interface{}{"type": "string", "enum": []string{"acknowledged", "rejected"}},
					"note":           map[string]interface{}{"type": "string", "description": "Short justification recorded with the judgement."},
				},
				"required": []string{"workspace_path", "issue_id", "status"},
			}),
		},
	}
	executors["resolve_run_concern"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		workspacePath, _ := args["workspace_path"].(string)
		issueID := stringToolArg(args, "issue_id")
		status, _ := args["status"].(string)
		note, _ := args["note"].(string)
		if strings.EqualFold(strings.TrimSpace(status), step_based_workflow.ConcernStatusResolved) {
			return "", fmt.Errorf("resolve_run_concern cannot close a real finding; use record_pulse_result with verified finding_dispositions")
		}
		finding, err := step_based_workflow.ResolvePulseFindingIssueID(ctx, workspacePath, issueID)
		if err != nil {
			return "", err
		}
		if err := step_based_workflow.ResolveRunConcern(ctx, workspacePath, finding.Fingerprint, status, "pulse", note); err != nil {
			return "", err
		}
		return fmt.Sprintf("Issue %s marked %s.", step_based_workflow.NewPulseIssue(finding).ID, status), nil
	}

	return []llmtypes.Tool{recordFindingTool, recordFocusTool, mergeIssuesTool, reconcileTool, recordTool, stateTool, resultTool, impactTool, resolveConcernTool, declareDueTool}, executors, categories
}

func stringToolArg(args map[string]interface{}, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func intToolArg(args map[string]interface{}, key string) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

// The three reads get_pulse_state merges. One tool with a named view rather
// than three tools whose names shared no rule: the agent that invented
// close_pulse_fix_attempt and resolve_human_input was guessing across a surface
// larger than the number of concepts in it.
const (
	pulseStateViewModule      = "module"
	pulseStateViewBacklog     = "backlog"
	pulseStateViewReview      = "review"
	pulseStateViewFocusAgenda = "focus_agenda"
)

// pulseStateViewValues is the closed view set shared by the schema enum, the
// accept check, and the rejection message.
var pulseStateViewValues = []string{pulseStateViewBacklog, pulseStateViewModule, pulseStateViewReview, pulseStateViewFocusAgenda}

// pulseResultValues is the union of the module and final-command result sets.
// Each target validates its own subset and names it on rejection; the schema
// enum is the union so a valid value is never rejected by the transport before
// the executor can explain which subset applies.
var pulseResultValues = []string{"running", "done", "changed", "skipped", "blocked", "failed"}

// readPulseFocusAgendaView wraps getPulseReviewFocusAgenda in the same
// (string, error) JSON-encoded shape the other get_pulse_state views use.
// Folded in as a fourth view (was the standalone get_pulse_review_focus_agenda
// tool) since it is read-only and already fit get_pulse_state's existing
// "one tool, multiple views" shape.
func readPulseFocusAgendaView(ctx context.Context, workspacePath, module, routeScope string, limit int) (string, error) {
	focuses, err := getPulseReviewFocusAgenda(ctx, workspacePath, module, routeScope, limit)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(map[string]interface{}{"focuses": focuses})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func readPulseModuleStateView(ctx context.Context, workspacePath, pulseRunID string) (string, error) {
	states, err := getPulseModuleStates(ctx, workspacePath)
	if err != nil {
		return "", err
	}
	// Workflow observations and accepted Pulse issues share one durable ledger,
	// but they are different queues. Gate needs both: issues are lifecycle work;
	// observations are evidence it may ask a reviewer to classify. Flattening
	// them made raw CONCERNS lines look like 100 confirmed bugs and sent Fixer
	// into broad rediscovery loops.
	lifecycles, concernsErr := step_based_workflow.LoadPulseFindingLifecycles(ctx, workspacePath, "", -1)
	if concernsErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): open concerns unavailable for %s: %v", workspacePath, concernsErr)
	}
	issues, observations := step_based_workflow.SplitPulseFindingLifecycles(lifecycles)
	issues = activePulseFindingLifecycles(issues)
	observations = activePulseFindingLifecycles(observations)
	suppressedConcerns, suppressedErr := step_based_workflow.LoadExternallyOwnedRunConcerns(ctx, workspacePath)
	if suppressedErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): suppressed concerns unavailable for %s: %v", workspacePath, suppressedErr)
	}
	// Plan changes whose knock-on effects have not been traced yet. The
	// changelog already records every plan-mod call and artifact_review
	// already stamps the ones that have been reconciled; nothing counted
	// the remainder, so Gate had to derive it from the files each run.
	planBacklog := step_based_workflow.CollectPlanChangeBacklog(workspacePath)
	planDependencyIntake := step_based_workflow.BuildPlanChangeDependencyIntake(planBacklog)
	// Steps with no drift_review record, or one flagged needs_review==true,
	// with the deterministic Group 1/2 checks Go could run for each already
	// computed — evidence for both Gate's forced due-decision
	// (validatePlanDriftRouting) and the plan_drift_review reviewer turn
	// itself, so it starts from pre-computed results instead of re-deriving
	// them.
	planDriftCandidates, planDriftErr := step_based_workflow.CollectPlanDriftCandidates(ctx, workspacePath)
	planDriftErrorText := ""
	if planDriftErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): plan drift candidate scan failed for %s: %v", workspacePath, planDriftErr)
		planDriftErrorText = planDriftErr.Error()
	}
	// What each reviewer has actually been finding. Without this the choice
	// between modules is a guess: nothing distinguished a module that keeps
	// surfacing real breakage from one that returns clean every time.
	reviewHistory, historyErr := step_based_workflow.LoadModuleReviewHistory(ctx, workspacePath, 3)
	if historyErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): review history unavailable for %s: %v", workspacePath, historyErr)
	}
	// This detector has already been validated against fleet state and
	// is supplied as a read-only fact feed, like open_concerns and
	// plan_change_backlog. Gate may weigh it but receives no mandatory
	// routing rule. A separate pre-decision snapshot is still retained
	// when Gate records its worklist so its handling remains auditable.
	loopClosure := loopclosure.Check(ctx, workspacePath, time.Now().UTC())
	// Runtime receipts are emitted by the execution engine, but a completed
	// outer run does not prove every child call succeeded. This collector turns
	// only explicit status disagreements and structured failures into compact
	// evidence for Gate; it never promotes them directly to Pulse issues.
	runtimeIntake := pulseintake.CheckRuntime(workspacePath, time.Now().UTC())
	impactLedger, impactErr := step_based_workflow.LoadPulseImpactLedger(ctx, workspacePath, 100)
	if impactErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): impact ledger unavailable for %s: %v", workspacePath, impactErr)
	}
	focusHistory, focusErr := getPulseReviewFocusStates(ctx, workspacePath)
	if focusErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): focus history unavailable for %s: %v", workspacePath, focusErr)
		focusHistory = []PulseReviewFocus{}
	}
	focusSelections, selectionErr := getPulseReviewFocusSelections(ctx, workspacePath, 50)
	if selectionErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): focus selections unavailable for %s: %v", workspacePath, selectionErr)
		focusSelections = []PulseReviewFocus{}
	}
	pendingRecoveries, recoveryErr := pendingPulseReviewRecoveries(ctx, workspacePath)
	if recoveryErr != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): review recovery state unavailable for %s: %v", workspacePath, recoveryErr)
		pendingRecoveries = []PulseReviewRecovery{}
	}
	lastReviews, lastReviewsErr := latestPulseReviewReceipts(ctx, workspacePath)
	lastReviewsError := ""
	if lastReviewsErr != nil {
		lastReviewsError = lastReviewsErr.Error()
		lastReviews = []PulseModuleAudit{}
	}
	var runMode *PulseRunMode
	if strings.TrimSpace(pulseRunID) != "" {
		var modeErr error
		runMode, modeErr = getPulseRunMode(ctx, workspacePath, pulseRunID)
		if modeErr != nil {
			return "", fmt.Errorf("read persisted Pulse mode: %w", modeErr)
		}
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"modules":                    states,
		"last_review_receipts":       lastReviews,
		"last_review_receipts_error": lastReviewsError,
		"last_review_receipts_note":  "Latest actual terminal review per module, excluding skips. These survive subsequent Gate checks; use recorded_at, result, reason, and evidence as the baseline for accumulating comparable workflow runs. A failed/blocked/timed_out receipt is not a completed assessment; inspect pending review recovery. Missing receipts or read errors mean unknown history, not a clean review. Current modules.last_result may be empty after a skip.",
		// Backward-compatible aliases now intentionally mean canonical issues.
		"open_concerns":              pulseLifecycleAgentProjection(issues, "issue_id"),
		"open_concern_count":         len(issues),
		"canonical_issues":           pulseLifecycleAgentProjection(issues, "issue_id"),
		"canonical_issue_count":      len(issues),
		"concerns_note":              "Complete active canonical issue backlog. These have been accepted by a reviewer or entered lifecycle work; they are not raw step emissions.",
		"workflow_observations":      []map[string]interface{}{},
		"workflow_observation_count": len(observations),
		"workflow_observations_note": "Historical observation rows are retained for audit only and are no longer an active Pulse intake. Technical Review reads retained run artifacts and deterministic receipts directly; do not load or classify these rows as a queue.",
		"pulse_store_navigation": map[string]string{
			"issues":                "Canonical repair roots. Reuse the existing public PUL issue_id for the same semantic root cause.",
			"closed_issues":         "Previously handled roots. Matching new evidence reopens the same root rather than creating a duplicate.",
			"workflow_observations": "Historical audit evidence only. It is not an active reviewer queue; Technical Review reads retained run artifacts and deterministic receipts directly.",
		},
		"suppressed_concerns":            pulseConcernAgentProjection(suppressedConcerns),
		"suppressed_concern_count":       len(suppressedConcerns),
		"suppressed_concerns_note":       "Diagnosed real findings owned outside this workflow. Do not report an unchanged fingerprint as a new finding or spend active review effort on it. A materially changed target/evidence creates or reopens an active finding; the recorded reopen condition explains the boundary.",
		"plan_change_backlog":            planBacklog,
		"loop_closure":                   loopClosure,
		"loop_closure_note":              "Read-only deterministic evidence. Gate may weigh verified findings alongside other facts, but they do not mandate a module or authorize mutation. coverage_status must be verified before an empty findings list means clean.",
		"deterministic_intake":           map[string]interface{}{"runtime": runtimeIntake, "plan_change_dependencies": planDependencyIntake},
		"plan_drift_candidates":          planDriftCandidates,
		"plan_drift_candidates_error":    planDriftErrorText,
		"plan_drift_candidates_note":     "Steps with no drift_review record, one flagged needs_review==true, or one below the contract version required for its step_type (evidence from any prior review is preserved on the step's own record in step_config.json, not duplicated here), each with Go-precomputed Check 1/2/4/9 results (report query compatibility, validation_schema db[] rules from step_config.json only, scripted-code queries, db/README.md contract). Checks 5 (validation_schema file rules) and 13 (orphaned tables) are not precomputed here; the plan_drift_review reviewer checks those directly. Each supported step type requires one reference-backed agentic check: scripted_best_practices, message_sequence_best_practices, orchestrator_best_practices (legacy orchestrator_best_practices accepted), routing_best_practices, or branch_best_practices; record_plan_drift_review rejects a step without its matching check. The reviewer also records step_prompt_quality for every due real step using step-description.md; categories describe affected compatibility and prompt contracts, not a whole-architecture audit. A small edit may reuse still-applicable prior evidence. Applied fixes close without a future-run verification queue. A failed precomputed check is still evidence, not a filed finding — the reviewer turn records the merged result via record_plan_drift_review and files a Pulse finding for anything unresolved. A candidate with step_id==\"" + step_based_workflow.WorkflowDriftReviewStepID + "\" is not a real plan step: it means one or more steps were deleted since the last workflow-level audit, and dependent-artifact fallout from that deletion needs tracing — see plan-drift-review.md's workflow-level deletion audit section; clear it the same way, via record_plan_drift_review(step_id=\"" + step_based_workflow.WorkflowDriftReviewStepID + "\", ...). A non-empty plan_drift_candidates_error means this scan itself failed (unreadable/malformed plan.json or step_config.json) — the candidate list above is empty because it is unknown, not because nothing is due; validatePlanDriftRouting requires plan_drift_review or technical_review due in that case.",
		"deterministic_intake_note":      "Read-only typed evidence, not automatic Pulse issues or Fixer authorization. Runtime errors do not force Technical Review: Gate must assess step concerns, required outcomes, recovery, material impact, prior review dispositions, and new comparable runs since last_ran_at. A completed status alone does not prove recovery; missing concerns or incomplete coverage do not prove health. Skip with a concrete evidence/recheck boundary when another review has no useful new evidence; a new critical failure must not wait for sample accumulation. Failed plan_change_dependencies still requires Technical Review unless Plan Drift is selected. coverage_status must be verified before an empty findings list means clean.",
		"module_review_history":          reviewHistory,
		"review_history_note":            "What each reviewer concluded the last few times it ran, most recently run first. A module absent from this list has not run in the retained window at all. Use it to justify each skip: a module that keeps returning real findings is a poor candidate for another cooldown, and one that has come back clean repeatedly is a good one. A verdict here is the reviewer's conclusion, which is not the same as whether anything was then fixed.",
		"review_focus_history":           focusHistory,
		"review_focus_history_note":      "Compact durable deep-review coverage. Never-reviewed and overdue focus keys are candidates after urgent regressions, reproduced technical defects or matured strategic experiments, and answered unapplied decisions. Applied technical fixes stay closed without a verification backlog. Route-specific counts prevent one sub-workflow from standing in for another. The agent chooses semantically; this is not blind round-robin.",
		"review_focus_selections":        focusSelections,
		"review_focus_selections_note":   "Recent focus selections, one row per investigated focus and route scope. Multiple rows for one Pulse run are valid when the reviewer found distinct evidence and decision value; their count is agent-chosen, not a platform quota.",
		"pending_review_recoveries":      pendingRecoveries,
		"pending_review_recoveries_note": "Backend-owned interrupted-review recovery. A listed module is mandatory due regardless of cooldown. Its executor must continue from checkpoint_path and persist one terminal module result; do not restart from scratch or infer missing findings.",
		"impact_ledger":                  impactLedger,
		"impact_ledger_note":             "Durable intervention, per-run success-criterion observation, and append-only before/after assessment history. Reliability or measurement work is not direct goal progress; inconclusive is correct until a comparable evidence window matures.",
		"context_records":                loadPulseContextRecordsForState(ctx, workspacePath),
		"context_records_note":           "User-confirmed workflow rules captured through capture_context. The context file is the runtime source; these immutable records show who captured what and when.",
		"gate_mode":                      runMode,
		"gate_mode_note":                 "The Gate-selected pass shape for the supplied pulse_run_id. Go records it but does not choose it; the following message sequence must follow it.",
	})
	return string(payload), nil
}

func loadPulseContextRecordsForState(ctx context.Context, workspacePath string) []step_based_workflow.PulseContextRecord {
	records, err := step_based_workflow.LoadPulseContextRecords(ctx, workspacePath, 100)
	if err != nil {
		log.Printf("[PULSE] get_pulse_state(view=module): context records unavailable for %s: %v", workspacePath, err)
		return []step_based_workflow.PulseContextRecord{}
	}
	return records
}

const maxPulseBacklogDetailIDs = 20

// readPulseBacklogView is retained as the compact-default entry point used by
// focused tests and internal callers. Agent tool calls flow through the options
// variant so a full lifecycle response is impossible without explicit ids.
func readPulseBacklogView(ctx context.Context, workspacePath, module string) (string, error) {
	return readPulseBacklogViewWithOptions(ctx, workspacePath, module, "compact", nil)
}

func readPulseBacklogViewWithOptions(ctx context.Context, workspacePath, module, detail string, issueIDs []string) (string, error) {
	module = normalizePulseModule(module)
	if module != "" && !validPulseModules[module] {
		return "", fmt.Errorf("module %q is not a valid Pulse module. Must be one of: %s. Omit module entirely to load the complete backlog", module, pulseModuleList())
	}
	detail = strings.ToLower(strings.TrimSpace(detail))
	if detail == "" {
		detail = "compact"
	}
	if detail != "compact" && detail != "full" {
		return "", fmt.Errorf("detail %q is not valid for get_pulse_state(view=\"backlog\"); use compact or full", detail)
	}
	issueIDs = normalizePulseIssueIDs(issueIDs)
	if detail == "compact" && len(issueIDs) > 0 {
		return "", fmt.Errorf("issue_ids is only valid with detail=\"full\"; read the compact index without ids first")
	}
	if detail == "full" {
		if len(issueIDs) == 0 {
			return "", fmt.Errorf("detail=\"full\" requires 1-%d issue_ids selected from the compact backlog index; broad full-history reads are intentionally disabled", maxPulseBacklogDetailIDs)
		}
		if len(issueIDs) > maxPulseBacklogDetailIDs {
			return "", fmt.Errorf("detail=\"full\" accepts at most %d issue_ids, got %d", maxPulseBacklogDetailIDs, len(issueIDs))
		}
	}
	findings, err := step_based_workflow.LoadPulseFindingLifecycles(ctx, workspacePath, module, -1)
	if err != nil {
		return "", err
	}
	issues, observations := step_based_workflow.SplitPulseFindingLifecycles(findings)
	if detail == "full" {
		selected, missing, ambiguous := selectPulseLifecyclesByIssueID(findings, issueIDs)
		if len(missing) > 0 {
			return "", fmt.Errorf("unknown issue_ids: %s; refresh get_pulse_state(view=\"backlog\", detail=\"compact\") and use exact ids", strings.Join(missing, ", "))
		}
		if len(ambiguous) > 0 {
			return "", fmt.Errorf("ambiguous issue_ids: %s; these public prefixes match multiple lifecycle rows and require backend identity repair before mutation", strings.Join(ambiguous, ", "))
		}
		payload, marshalErr := json.Marshal(map[string]interface{}{
			"detail":  "full",
			"records": pulseBacklogAgentProjection(selected),
			"total":   len(selected),
			"note":    "Complete lifecycle evidence for only the requested public PUL ids. No unrequested issue or observation history was loaded into the agent response.",
		})
		if marshalErr != nil {
			return "", marshalErr
		}
		return string(payload), nil
	}

	activeIssues := activePulseFindingLifecycles(issues)
	activeObservations := activePulseFindingLifecycles(observations)
	closedIssues := make([]step_based_workflow.PulseFindingLifecycle, 0, len(issues)-len(activeIssues))
	for _, finding := range issues {
		if !pulseFindingLifecycleActive(finding) {
			closedIssues = append(closedIssues, finding)
		}
	}
	payload, marshalErr := json.Marshal(map[string]interface{}{
		"detail":            "compact",
		"issues":            pulseLifecycleAgentProjection(activeIssues, "issue_id"),
		"closed_issues":     pulseLifecycleAgentProjection(closedIssues, "issue_id"),
		"observations":      []map[string]interface{}{},
		"total":             len(activeIssues),
		"issue_total":       len(activeIssues),
		"observation_total": len(activeObservations),
		"summary":           pulseBacklogSummary(issues, observations),
		"note":              "Compact issue register: compare new evidence with active and closed issue text, reuse the matching issue_id, and request detail=\"full\" only for candidates. Historical observations are retained for audit but are not supplied as an active review queue; Technical Review reads retained run artifacts and deterministic receipts directly.",
		"navigation": map[string]string{
			"issues":        "Canonical repair roots. Start here and reuse the same PUL issue_id for the same semantic root cause.",
			"closed_issues": "Previously handled roots. Matching new evidence reopens the same root rather than creating a duplicate.",
			"observations":  "Historical audit evidence only. It is not an active reviewer queue; Technical Review reads retained run artifacts and deterministic receipts directly.",
		},
	})
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(payload), nil
}

func normalizePulseIssueIDs(issueIDs []string) []string {
	seen := make(map[string]bool, len(issueIDs))
	normalized := make([]string, 0, len(issueIDs))
	for _, issueID := range issueIDs {
		issueID = strings.ToUpper(strings.TrimSpace(issueID))
		if issueID == "" || seen[issueID] {
			continue
		}
		seen[issueID] = true
		normalized = append(normalized, issueID)
	}
	return normalized
}

func selectPulseLifecyclesByIssueID(findings []step_based_workflow.PulseFindingLifecycle, issueIDs []string) ([]step_based_workflow.PulseFindingLifecycle, []string, []string) {
	byID := make(map[string][]step_based_workflow.PulseFindingLifecycle, len(findings))
	for _, finding := range findings {
		issueID := strings.ToUpper(step_based_workflow.NewPulseIssue(finding).ID)
		byID[issueID] = append(byID[issueID], finding)
	}
	selected := make([]step_based_workflow.PulseFindingLifecycle, 0, len(issueIDs))
	missing := make([]string, 0)
	ambiguous := make([]string, 0)
	for _, issueID := range issueIDs {
		matches := byID[issueID]
		if len(matches) == 0 {
			missing = append(missing, issueID)
			continue
		}
		if len(matches) > 1 {
			ambiguous = append(ambiguous, issueID)
			continue
		}
		selected = append(selected, matches[0])
	}
	return selected, missing, ambiguous
}

// pulseBacklogSummary is intentionally derived from durable lifecycle rows,
// not a model receipt. It lets a reviewer and the consolidation command report
// whether the active backlog actually moved rather than celebrating activity
// while the number of unresolved root causes rises.
func pulseBacklogSummary(issues, observations []step_based_workflow.PulseFindingLifecycle) map[string]interface{} {
	byStatus := map[string]int{}
	active, merged := 0, 0
	for _, finding := range issues {
		status := strings.TrimSpace(finding.Status)
		if status == "" {
			status = "unknown"
		}
		byStatus[status]++
		if finding.Details != nil && strings.TrimSpace(finding.Details.MergedIntoIssueID) != "" {
			merged++
		}
		switch status {
		case step_based_workflow.ConcernStatusResolved, step_based_workflow.ConcernStatusRejected, step_based_workflow.ConcernStatusExternalActionRequired:
		default:
			active++
		}
	}
	observationByStatus := map[string]int{}
	activeObservations := 0
	for _, observation := range observations {
		status := strings.TrimSpace(observation.Status)
		if status == "" {
			status = "unknown"
		}
		observationByStatus[status]++
		if pulseFindingLifecycleActive(observation) {
			activeObservations++
		}
	}
	return map[string]interface{}{
		"active_count": active, "terminal_count": len(issues) - active,
		"merged_duplicate_count": merged, "by_status": byStatus,
		"active_observation_count":   activeObservations,
		"terminal_observation_count": len(observations) - activeObservations,
		"observations_by_status":     observationByStatus,
	}
}

func pulseFindingLifecycleActive(finding step_based_workflow.PulseFindingLifecycle) bool {
	switch strings.TrimSpace(finding.Status) {
	case step_based_workflow.ConcernStatusResolved, step_based_workflow.ConcernStatusRejected, step_based_workflow.ConcernStatusExternalActionRequired:
		return false
	default:
		return true
	}
}

func activePulseFindingLifecycles(findings []step_based_workflow.PulseFindingLifecycle) []step_based_workflow.PulseFindingLifecycle {
	active := make([]step_based_workflow.PulseFindingLifecycle, 0, len(findings))
	for _, finding := range findings {
		if pulseFindingLifecycleActive(finding) {
			active = append(active, finding)
		}
	}
	return active
}

func pulseLifecycleAgentProjection(findings []step_based_workflow.PulseFindingLifecycle, idKey string) []map[string]interface{} {
	projected := make([]map[string]interface{}, 0, len(findings))
	for _, finding := range findings {
		issue := step_based_workflow.NewPulseIssue(finding)
		card := map[string]interface{}{
			idKey: issue.ID, "kind": finding.Kind, "step_id": finding.StepID,
			"phase": finding.Phase, "group_name": finding.GroupName, "summary": issue.Title,
			"first_seen_run": finding.FirstSeenRun, "first_seen_at": finding.FirstSeenAt,
			"last_seen_run": finding.LastSeenRun, "last_seen_at": finding.LastSeenAt,
			"seen_count": finding.SeenCount, "status": finding.Status, "priority": issue.Priority,
			"module": issue.Module, "repair_eligible": finding.Kind == step_based_workflow.PulseFindingKindIssue && issue.Status == "backlog",
		}
		if finding.Details != nil {
			card["target_key"] = finding.Details.TargetKey
			card["next_check"] = finding.Details.NextCheck
		}
		if len(finding.Verification) > 0 {
			latest := finding.Verification[0]
			card["latest_verification"] = map[string]interface{}{
				"verdict": latest.Verdict, "check": latest.Check, "verified_at": latest.At,
			}
		}
		projected = append(projected, card)
	}
	return projected
}

// pulseConcernAgentProjection keeps the Gate's issue feed on the same public
// identity contract as the detailed backlog. A raw fingerprint is a database
// join key, not an instruction for an LLM to copy into a later write.
func pulseConcernAgentProjection(concerns []step_based_workflow.RunConcern) []map[string]interface{} {
	projected := make([]map[string]interface{}, 0, len(concerns))
	for _, concern := range concerns {
		issue := step_based_workflow.NewPulseIssue(step_based_workflow.PulseFindingLifecycle{
			Fingerprint: concern.Fingerprint, IssueID: concern.IssueID, StepID: concern.StepID, Phase: concern.Phase,
			GroupName: concern.GroupName, Text: concern.Text, FirstSeenRun: concern.FirstSeenRun,
			FirstSeenAt: concern.FirstSeenAt, LastSeenRun: concern.LastSeenRun, LastSeenAt: concern.LastSeenAt,
			SeenCount: concern.SeenCount, Status: concern.Status,
		})
		projected = append(projected, map[string]interface{}{
			"issue_id": issue.ID, "step_id": concern.StepID, "phase": concern.Phase,
			"group_name": concern.GroupName, "text": concern.Text, "first_seen_run": concern.FirstSeenRun,
			"first_seen_at": concern.FirstSeenAt, "last_seen_run": concern.LastSeenRun,
			"last_seen_at": concern.LastSeenAt, "seen_count": concern.SeenCount, "status": concern.Status,
			"resolution_note": concern.ResolutionNote,
		})
	}
	return projected
}

// pulseBacklogAgentProjection removes lifecycle implementation keys from the
// coding-agent tool response. The REST/UI projection can retain them as stable
// React keys while the agent receives only the one public PUL issue identity.
func pulseBacklogAgentProjection(findings []step_based_workflow.PulseFindingLifecycle) interface{} {
	raw, err := json.Marshal(findings)
	if err != nil {
		return []interface{}{}
	}
	var projected interface{}
	if err := json.Unmarshal(raw, &projected); err != nil {
		return []interface{}{}
	}
	var redact func(interface{})
	redact = func(value interface{}) {
		switch typed := value.(type) {
		case map[string]interface{}:
			delete(typed, "fingerprint")
			delete(typed, "attempt_id")
			delete(typed, "finding_id")
			for _, nested := range typed {
				redact(nested)
			}
		case []interface{}:
			for _, nested := range typed {
				redact(nested)
			}
		}
	}
	redact(projected)
	return projected
}

func readPulseReviewView(ctx context.Context, workspacePath, reviewRunID, module string) (string, error) {
	reviewRunID = strings.TrimSpace(reviewRunID)
	module = strings.TrimSpace(module)
	if reviewRunID == "" || module == "" {
		return "", fmt.Errorf("get_pulse_state(view=%q) requires both review_run_id and module (got %s); use the pair exactly as reported by the call_generic_agent completion notification",
			pulseStateViewReview,
			pulseArgArrivalReport("review_run_id", reviewRunID, "module", module))
	}
	if err := step_based_workflow.ValidatePulseReviewIdentity(reviewRunID, module); err != nil {
		return "", err
	}
	receipt, err := step_based_workflow.LoadPulseReviewReceiptForRun(ctx, workspacePath, reviewRunID, module)
	if errors.Is(err, sql.ErrNoRows) {
		// Absence is the expected answer here, not a fault. The review stage
		// prompt (scheduler.go) tells a reviewer to reconcile "any already-saved
		// SQLite result before discovery" so it does not launch a duplicate — on
		// a fresh run that pre-check must miss, because the caller is the thing
		// that will write the row.
		//
		// The previous wording offered "or this identity pair is wrong" as a
		// co-equal explanation. ValidatePulseReviewIdentity ran immediately
		// above and passed, so the code had already disproved that: the format
		// is valid and the module is in the canonical registry. Naming it anyway
		// sent reviewers hunting for a different id — 8 of these on 2026-08-04
		// across 3 sessions, on identities that were correct and seconds old.
		//
		// So: state the normal case first, and do not offer a cause this
		// function has already ruled out.
		return "", fmt.Errorf(
			"no Pulse review is saved for review_run_id=%q module=%q yet. This identity is well-formed and the module is valid — "+
				"do not look for a different review_run_id. "+
				"If you are the reviewer for this run, nothing is saved until you save it: this is the expected result of the "+
				"pre-discovery check, so proceed with discovery and record your result. "+
				"If you are waiting on another stage's reviewer, it has not finished — resume from its completion notification "+
				"rather than polling",
			reviewRunID, module,
		)
	}
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]interface{}{
		"module":             receipt.Module,
		"review_run_id":      receipt.ReviewRunID,
		"pulse_run_id":       receipt.PulseRunID,
		"status":             receipt.Status,
		"verdict":            receipt.Verdict,
		"finding_count":      receipt.FindingCount,
		"verification_count": receipt.VerificationCount,
		"verifications":      receipt.Verifications,
		"note":               "Review findings are stored only in the structured lifecycle backlog; load view=module or view=backlog for actionable issues.",
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// pulseArgArrivalReport names what actually arrived for each of a pair of
// required arguments. "requires both" alone does not say which one was missing.
func pulseArgArrivalReport(nameA, valueA, nameB, valueB string) string {
	state := func(value string) string {
		if strings.TrimSpace(value) == "" {
			return "missing"
		}
		return "set"
	}
	return fmt.Sprintf("%s=%s, %s=%s", nameA, state(valueA), nameB, state(valueB))
}

// recordPulseResultFromToolArgs writes one Pulse outcome. Exactly one of module
// or command selects which durable record is being written; the two used to be
// separate tools whose names differed by more than the concept did.
func recordPulseResultFromToolArgs(ctx context.Context, args map[string]interface{}) (string, error) {
	workspacePath, _ := args["workspace_path"].(string)
	pulseRunID, _ := args["pulse_run_id"].(string)
	pulseRunID = pulseRunIDForSession(ctx, pulseRunID)
	module, _ := args["module"].(string)
	command, _ := args["command"].(string)
	result, _ := args["result"].(string)
	reason, _ := args["reason"].(string)
	module = strings.TrimSpace(module)
	command = strings.TrimSpace(command)
	if (module == "") == (command == "") {
		return "", fmt.Errorf("record_pulse_result requires exactly one of module or command (got %s). Pass module (one of: %s) to record a Pulse module's terminal result, or command (one of: %s) to record a Pulse final command's status",
			pulseArgArrivalReport("module", module, "command", command),
			pulseModuleList(), strings.Join(pulseFinalCommandOrder, ", "))
	}
	if err := validatePulseToolRunID(ctx, pulseRunID); err != nil {
		return "", err
	}
	if command != "" {
		normalizedResult := strings.ToLower(strings.TrimSpace(result))
		if !pulseValueIsOneOf(normalizedResult, pulseFinalCommandResultValues) {
			return "", fmt.Errorf("result %q is not valid for command %q. Must be one of: %s. Mark a command running before its work and terminal immediately after it finishes",
				result, command, strings.Join(pulseFinalCommandResultValues, ", "))
		}
		state, err := markPulseFinalCommandStateFromAgent(ctx, workspacePath, command, pulseRunID, normalizedResult, reason)
		if err != nil {
			return "", err
		}
		payload, _ := json.Marshal(map[string]interface{}{"status": "updated", "command": state})
		return string(payload), nil
	}

	if !pulseValueIsOneOf(strings.ToLower(strings.TrimSpace(result)), pulseModuleResultValues) {
		return "", fmt.Errorf("result %q is not valid for module %q. Must be one of: %s. Use changed only when files were modified, and pair it with changed_files, verification, and finding_dispositions",
			result, module, strings.Join(pulseModuleResultValues, ", "))
	}
	audit := PulseModuleAuditInput{
		ChangedFiles: stringSliceFromToolArg(args["changed_files"]),
		Verification: stringSliceFromToolArg(args["verification"]),
		BeforeRefs:   stringSliceFromToolArg(args["before_refs"]),
		AfterRefs:    stringSliceFromToolArg(args["after_refs"]),
	}
	if len(audit.BeforeRefs) != len(audit.AfterRefs) {
		return "", fmt.Errorf("before_refs and after_refs must be equal-length positional audit pairs (got before_refs=%d, after_refs=%d); supply the matching after_ref for each before_ref, or omit both arrays",
			len(audit.BeforeRefs), len(audit.AfterRefs))
	}
	dispositions, err := pulseFindingDispositionsFromToolArg(args["finding_dispositions"])
	if err != nil {
		return "", err
	}
	dispositions, err = step_based_workflow.ResolvePulseFindingDispositionIssueIDs(ctx, workspacePath, dispositions)
	if err != nil {
		return "", err
	}
	reviewerVerifications, err := step_based_workflow.LoadPulseReviewVerificationsForPulseRun(
		ctx, workspacePath, pulseRunID, module,
	)
	if err != nil {
		return "", fmt.Errorf("load structured reviewer verifications: %w", err)
	}
	if err := validateReviewerVerificationDispositions(reviewerVerifications, dispositions); err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimSpace(result), "changed") {
		// One rejection per missing array taught the caller the contract one
		// failed write at a time. Name the whole required set and what
		// actually arrived, so a single retry can satisfy all three.
		changedRequirement := fmt.Sprintf("result=changed requires changed_files, verification, and finding_dispositions together (got changed_files=%d items, verification=%d items, finding_dispositions=%d items)",
			len(audit.ChangedFiles), len(audit.Verification), len(dispositions))
		if len(audit.ChangedFiles) == 0 {
			return "", fmt.Errorf("changed_files is required when result=changed; %s. changed_files lists the exact workspace-relative files this module changed", changedRequirement)
		}
		if len(audit.Verification) == 0 {
			return "", fmt.Errorf("verification is required when result=changed; %s. verification lists the checks performed and their observed outcomes", changedRequirement)
		}
		if len(dispositions) == 0 {
			return "", fmt.Errorf("finding_dispositions is required when result=changed; %s. %s", changedRequirement, pulseFindingDispositionsShape)
		}
	}
	state, err := markPulseModuleResultFromAgentWithAuditAndFindings(
		ctx, workspacePath, module, pulseRunID, result, reason,
		stringSliceFromToolArg(args["evidence"]), audit, dispositions,
	)
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]interface{}{"status": "updated", "module": state})
	return string(payload), nil
}

func pulseValueIsOneOf(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// pulseFindingDispositionsShape is appended to decode failures. A raw
// json.Unmarshal message names a Go struct field the agent has never seen and
// says nothing about the shape that would have worked, so a mistyped payload
// cannot be corrected from the rejection alone.
const pulseFindingDispositionsShape = `finding_dispositions takes an array of disposition objects, not an object or a string: ` +
	`[{"issue_id": "<backlog issue_id>", "disposition": "fixed_verified", "summary": "<one sentence>", ` +
	`"changed_files": ["path/to/file"], ` +
	`"verification": [{"check": "<what was run>", "verdict": "passed", "expected": "<expected>", "observed": "<observed>"}]}]`

type pulseFindingDispositionToolArg struct {
	IssueID         string                                         `json:"issue_id"`
	Disposition     string                                         `json:"disposition"`
	Summary         string                                         `json:"summary"`
	ChangedFiles    []string                                       `json:"changed_files,omitempty"`
	BeforeRefs      []string                                       `json:"before_refs,omitempty"`
	AfterRefs       []string                                       `json:"after_refs,omitempty"`
	NextCheck       string                                         `json:"next_check,omitempty"`
	ExternalOwner   string                                         `json:"external_owner,omitempty"`
	ReasonCode      string                                         `json:"reason_code,omitempty"`
	ReopenCondition string                                         `json:"reopen_condition,omitempty"`
	HumanInputID    string                                         `json:"human_input_id,omitempty"`
	Verification    []step_based_workflow.PulseFindingVerification `json:"verification,omitempty"`
}

func pulseFindingDispositionsFromToolArg(raw interface{}) ([]step_based_workflow.PulseFindingDisposition, error) {
	if raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode finding_dispositions (%s): %w", pulseFindingDispositionsShape, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var toolArgs []pulseFindingDispositionToolArg
	if err := decoder.Decode(&toolArgs); err != nil {
		return nil, fmt.Errorf("decode finding_dispositions (%s): %w", pulseFindingDispositionsShape, err)
	}
	dispositions := make([]step_based_workflow.PulseFindingDisposition, len(toolArgs))
	for index, input := range toolArgs {
		dispositions[index] = step_based_workflow.NormalizePulseFindingDisposition(step_based_workflow.PulseFindingDisposition{
			FindingID:       input.IssueID,
			Disposition:     input.Disposition,
			Summary:         input.Summary,
			ChangedFiles:    input.ChangedFiles,
			BeforeRefs:      input.BeforeRefs,
			AfterRefs:       input.AfterRefs,
			NextCheck:       input.NextCheck,
			ExternalOwner:   input.ExternalOwner,
			ReasonCode:      input.ReasonCode,
			ReopenCondition: input.ReopenCondition,
			HumanInputID:    input.HumanInputID,
			Verification:    input.Verification,
		})
	}
	return dispositions, nil
}

// validateReviewerVerificationDispositions reports every mismatch between the
// saved reviewer verdicts and the submitted dispositions in one pass, not one
// mismatch per call.
//
// It used to return on the first mismatch within the first review. On
// 2026-08-04 finding PUL-70B1057E took three separate record_pulse_result
// rejections to get through this single function alone — wrong disposition
// value, then a verification proof that didn't match the reviewer's evidence,
// then a next_check that didn't match the reviewer's boundary text — because
// each rejection only revealed the next check once the previous one was fixed.
// All three checks read independent fields of the same matched disposition, so
// none of them needs the others to have passed first; they can all run and all
// report in one message. validateFindingDisposition got the same fix for the
// structural checks that follow this one in the same call.
func validateReviewerVerificationDispositions(
	reviews []step_based_workflow.PulseReviewVerificationResult,
	dispositions []step_based_workflow.PulseFindingDisposition,
) error {
	var messages []string
	for _, review := range reviews {
		var matched *step_based_workflow.PulseFindingDisposition
		for index := range dispositions {
			candidate := &dispositions[index]
			// The reviewer names the attempt it checked, but the fixer no longer
			// carries an attempt_id: the backend resolves the module's own open
			// attempt for this finding. Match on the identity the fixer does hold,
			// and only compare attempt_id when one was supplied anyway.
			if candidate.FindingID != review.FindingID || candidate.Fingerprint != review.Fingerprint {
				continue
			}
			if candidate.AttemptID != "" && candidate.AttemptID != review.AttemptID {
				continue
			}
			matched = candidate
			break
		}
		if matched == nil {
			messages = append(messages, fmt.Sprintf("reviewer verification for issue %q requires a matching issue_id disposition before the module can be terminal", review.FindingID))
			continue
		}

		var findingProblems []string
		wantDisposition := map[string]string{
			step_based_workflow.VerificationPassed:       step_based_workflow.FindingDispositionFixedVerified,
			step_based_workflow.VerificationFailed:       step_based_workflow.FindingDispositionFailed,
			step_based_workflow.VerificationInconclusive: step_based_workflow.FindingDispositionChangedUnverified,
		}[review.Verdict]
		if matched.Disposition != wantDisposition {
			findingProblems = append(findingProblems, fmt.Sprintf("reviewer verification %s requires disposition %q, got %q", review.Verdict, wantDisposition, matched.Disposition))
		}
		verificationMatched := false
		for _, proof := range matched.Verification {
			if proof.Verdict == review.Verdict && strings.TrimSpace(proof.Expected) == review.Expected && strings.TrimSpace(proof.Observed) == review.Observed {
				verificationMatched = true
				break
			}
		}
		if !verificationMatched {
			findingProblems = append(findingProblems, fmt.Sprintf("disposition must carry the reviewer's structured %s proof with identical expected and observed evidence", review.Verdict))
		}
		if review.Verdict == step_based_workflow.VerificationInconclusive && strings.TrimSpace(matched.NextCheck) != review.NextCheck {
			findingProblems = append(findingProblems, fmt.Sprintf("inconclusive disposition next_check must match the reviewer boundary %q", review.NextCheck))
		}
		if len(findingProblems) > 0 {
			messages = append(messages, step_based_workflow.FormatPulseDispositionProblems(review.FindingID, findingProblems).Error())
		}
	}
	if len(messages) == 0 {
		return nil
	}
	if len(messages) == 1 {
		return errors.New(messages[0])
	}
	return fmt.Errorf("%d findings failed reviewer-verification cross-check:\n%s", len(messages), strings.Join(messages, "\n"))
}

// pulseDecisionFields is the complete decision contract in schema order. It
// backs both the unknown-field check and the message that reports it, so a
// field can never be silently accepted without appearing in the list an agent
// is shown.
var pulseDecisionFields = []string{
	"module", "due", "reason", "evidence",
	"next_check_at", "next_check_after_run_id", "cooldown_runs",
}

// pulseDecisionFieldAliases maps the wrong names agents actually reach for onto
// the field they meant. "decision" carrying the due/skip verdict is the observed
// case; without the pointer the rejection only says a field is unknown, which
// does not tell the caller which of the seven it should have used.
var pulseDecisionFieldAliases = map[string]string{
	"decision": "due", "decisions": "due", "is_due": "due", "status": "due",
	"verdict": "due", "state": "due", "rationale": "reason", "justification": "reason",
	"why": "reason", "notes": "reason", "note": "reason", "proof": "evidence",
	"cooldown": "cooldown_runs", "next_check": "next_check_at",
}

func pulseDecisionFieldSuggestion(key string) string {
	if suggestion, ok := pulseDecisionFieldAliases[strings.ToLower(strings.TrimSpace(key))]; ok {
		return suggestion
	}
	// A near-miss spelling of a real field is still recoverable, but only if the
	// rejection says which field it was close to.
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	for _, field := range pulseDecisionFields {
		if normalized == field || strings.HasPrefix(field, normalized) || strings.HasPrefix(normalized, field) {
			return field
		}
	}
	return ""
}

func pulseWorklistDecisionsFromArgs(raw interface{}) ([]PulseWorklistDecision, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("decisions must be an array with one object per Pulse module: [{\"module\": %q, \"due\": true, \"reason\": \"<why>\"}]; module, due (boolean), and reason are required on every entry",
			pulseModuleExample())
	}
	out := make([]PulseWorklistDecision, 0, len(arr))
	allowed := map[string]bool{}
	for _, field := range pulseDecisionFields {
		allowed[field] = true
	}
	for index, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("decisions[%d] must be an object shaped {\"module\": %q, \"due\": true, \"reason\": \"<why>\"}, got %T",
				index, pulseModuleExample(), item)
		}
		for key := range m {
			if !allowed[key] {
				message := fmt.Sprintf("decisions[%d] contains unknown field %q. Allowed fields: %s; module, due, and reason are required",
					index, key, strings.Join(pulseDecisionFields, ", "))
				if suggestion := pulseDecisionFieldSuggestion(key); suggestion != "" {
					if suggestion == "due" {
						message += fmt.Sprintf("; use the required boolean field %s", suggestion)
					} else {
						message += fmt.Sprintf("; did you mean %q?", suggestion)
					}
				}
				return nil, fmt.Errorf("%s", message)
			}
		}
		decision := PulseWorklistDecision{}
		var err error
		if decision.Module, err = requiredStringToolArg(m, "module", index); err != nil {
			return nil, err
		}
		if decision.Due, ok = m["due"].(bool); !ok {
			return nil, fmt.Errorf("decisions[%d].due is required and must be boolean", index)
		}
		if decision.Reason, err = requiredStringToolArg(m, "reason", index); err != nil {
			return nil, err
		}
		if rawEvidence, exists := m["evidence"]; exists {
			if decision.Evidence, err = strictStringSliceToolArg(rawEvidence); err != nil {
				return nil, fmt.Errorf("decisions[%d].evidence: %w", index, err)
			}
		}
		if decision.NextCheckAt, err = optionalStringToolArg(m, "next_check_at", index); err != nil {
			return nil, err
		}
		if decision.NextCheckAfterRunID, err = optionalStringToolArg(m, "next_check_after_run_id", index); err != nil {
			return nil, err
		}
		if rawCooldown, exists := m["cooldown_runs"]; exists {
			if decision.CooldownRuns, err = strictIntToolArg(rawCooldown); err != nil {
				return nil, fmt.Errorf("decisions[%d].cooldown_runs: %w", index, err)
			}
		}
		out = append(out, decision)
	}
	return out, nil
}

func requiredStringToolArg(m map[string]interface{}, key string, index int) (string, error) {
	value, ok := m[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("decisions[%d].%s is required and must be a non-empty string (got %s); every decision entry needs module, due, and reason",
			index, key, pulseArgTypeName(m[key]))
	}
	return value, nil
}

func optionalStringToolArg(m map[string]interface{}, key string, index int) (string, error) {
	raw, exists := m[key]
	if !exists {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("decisions[%d].%s must be a string (got %s)", index, key, pulseArgTypeName(raw))
	}
	return value, nil
}

// pulseArgTypeName describes what actually arrived for a tool argument. "must
// be a string" alone does not say whether the caller sent a number, an object,
// or nothing at all.
func pulseArgTypeName(raw interface{}) string {
	switch value := raw.(type) {
	case nil:
		return "nothing"
	case string:
		if strings.TrimSpace(value) == "" {
			return "an empty string"
		}
		return "a string"
	case bool:
		return "a boolean"
	case float64, int, int64:
		return "a number"
	case []interface{}:
		return "an array"
	case map[string]interface{}:
		return "an object"
	default:
		return fmt.Sprintf("%T", raw)
	}
}

func strictStringSliceToolArg(raw interface{}) ([]string, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an array of strings, got %s", pulseArgTypeName(raw))
	}
	out := make([]string, 0, len(arr))
	for index, item := range arr {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("item %d must be a string, got %s", index, pulseArgTypeName(item))
		}
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out, nil
}

func strictIntToolArg(raw interface{}) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		integer := int(value)
		if float64(integer) != value {
			return 0, fmt.Errorf("must be a whole number of runs, got the fractional value %v", value)
		}
		return integer, nil
	default:
		return 0, fmt.Errorf("must be a non-negative integer number of runs, got %s", pulseArgTypeName(raw))
	}
}

func stringSliceFromToolArg(raw interface{}) []string {
	if values, ok := raw.([]string); ok {
		return normalizePulseEvidence(values)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func normalizePulseModule(module string) string {
	return pulsemodules.Normalize(module)
}

func normalizePulseEvidence(evidence []string) []string {
	out := make([]string, 0, len(evidence))
	seen := map[string]bool{}
	for _, item := range evidence {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
