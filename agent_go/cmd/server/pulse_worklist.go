package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	htmlpkg "golang.org/x/net/html"
)

const (
	pulseModuleBugReview           = "bug_review"
	pulseModuleArtifactReview      = "artifact_review"
	pulseModuleReportHealth        = "report_health"
	pulseModuleEvalHealth          = "eval_health"
	pulseModuleLearningHealth      = "learning_health"
	pulseModuleKnowledgebaseHealth = "knowledgebase_health"
	pulseModuleDBHealth            = "db_health"
	pulseModuleCostLLMTime         = "cost_llm_time"
	pulseModuleLLMOpsReview        = "llm_ops_review"
	pulseModuleGoalAdvisor         = "goal_advisor"
)

var pulseModuleOrder = []string{
	pulseModuleBugReview,
	pulseModuleArtifactReview,
	pulseModuleReportHealth,
	pulseModuleEvalHealth,
	pulseModuleLearningHealth,
	pulseModuleKnowledgebaseHealth,
	pulseModuleDBHealth,
	pulseModuleCostLLMTime,
	pulseModuleLLMOpsReview,
	pulseModuleGoalAdvisor,
}

var validPulseModules = map[string]bool{
	pulseModuleBugReview:           true,
	pulseModuleArtifactReview:      true,
	pulseModuleReportHealth:        true,
	pulseModuleEvalHealth:          true,
	pulseModuleLearningHealth:      true,
	pulseModuleKnowledgebaseHealth: true,
	pulseModuleDBHealth:            true,
	pulseModuleCostLLMTime:         true,
	pulseModuleLLMOpsReview:        true,
	pulseModuleGoalAdvisor:         true,
}

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

func ensurePulseModuleStateSchema(ctx context.Context, db *sql.DB) error {
	if err := migratePulseModuleStateSchema(ctx, db); err != nil {
		return err
	}
	stmts := []string{
		pulseModuleStateSchema,
		pulseFinalCommandStateSchema,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensurePulseModuleStateColumns(ctx, db); err != nil {
		return err
	}
	stmts = []string{
		`CREATE INDEX IF NOT EXISTS idx_pulse_module_state_run ON pulse_module_state(last_pulse_run_id, last_decision)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
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

func recordPulseWorklist(ctx context.Context, workspacePath, pulseRunID string, decisions []PulseWorklistDecision) ([]PulseModuleState, error) {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return nil, fmt.Errorf("pulse_run_id is required")
	}
	if err := validatePulseWorklistDecisions(decisions); err != nil {
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
	states := make([]PulseModuleState, 0, len(decisions))
	for _, decision := range decisions {
		module := normalizePulseModule(decision.Module)
		if !validPulseModules[module] {
			return nil, fmt.Errorf("module %q is not valid", decision.Module)
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

func recordTrustedPulseWorklistOnce(ctx context.Context, workspacePath, pulseRunID string, decisions []PulseWorklistDecision) ([]PulseModuleState, error) {
	if err := validatePulseWorklistDecisions(decisions); err != nil {
		return nil, err
	}

	pulseWorklistRecordMu.Lock()
	defer pulseWorklistRecordMu.Unlock()
	// Revalidate at the serialized write boundary. A session may have been
	// revoked after the tool call began but before argument parsing finished.
	if err := validateTrustedPulseToolRunID(ctx, pulseRunID); err != nil {
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
	return recordPulseWorklist(ctx, workspacePath, pulseRunID, decisions)
}

func validatePulseWorklistDecisions(decisions []PulseWorklistDecision) error {
	if len(decisions) == 0 {
		return fmt.Errorf("decisions are required")
	}
	if len(decisions) != len(pulseModuleOrder) {
		return fmt.Errorf("decisions must include exactly one entry for each Pulse module; got %d want %d", len(decisions), len(pulseModuleOrder))
	}
	seen := map[string]bool{}
	for _, decision := range decisions {
		module := normalizePulseModule(decision.Module)
		if !validPulseModules[module] {
			return fmt.Errorf("module %q is not valid", decision.Module)
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
	for _, module := range pulseModuleOrder {
		if !seen[module] {
			return fmt.Errorf("decisions missing required module %q", module)
		}
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

func validatePulseGateCompletion(ctx context.Context, workspacePath, pulseRunID, previousHTML string, previousExists bool) error {
	worklist, exists, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return fmt.Errorf("read Pulse Gate worklist: %w", err)
	}
	if !exists || !pulseWorklistIsComplete(worklist) {
		return fmt.Errorf("Pulse Gate did not record a complete worklist for pulse_run_id %q", pulseRunID)
	}

	htmlPath := strings.TrimSuffix(workspacePath, "/") + "/builder/improve.html"
	html, htmlExists, err := readFileFromWorkspace(ctx, htmlPath)
	if err != nil {
		return fmt.Errorf("read Pulse Gate handoff: %w", err)
	}
	if !htmlExists || strings.TrimSpace(html) == "" {
		return fmt.Errorf("Pulse Gate did not write %s", htmlPath)
	}
	if previousExists && html == previousHTML {
		return fmt.Errorf("Pulse Gate left %s unchanged", htmlPath)
	}
	if !pulseGateHandoffContainsRunID(html, pulseRunID) {
		return fmt.Errorf("Pulse Gate handoff does not contain current pulse_run_id %q", pulseRunID)
	}
	return nil
}

func pulseGateHandoffContainsRunID(html, pulseRunID string) bool {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return false
	}
	tokenizer := htmlpkg.NewTokenizer(strings.NewReader(html))
	depth := 0
	for {
		switch tokenizer.Next() {
		case htmlpkg.ErrorToken:
			return false
		case htmlpkg.StartTagToken, htmlpkg.SelfClosingTagToken:
			token := tokenizer.Token()
			if depth == 0 {
				if !htmlTokenHasID(token, "pulse-agent-handoff") {
					continue
				}
				for _, attr := range token.Attr {
					if attr.Key == "data-pulse-run-id" && strings.TrimSpace(attr.Val) == pulseRunID {
						return true
					}
				}
				if token.Type == htmlpkg.StartTagToken {
					depth = 1
				}
				continue
			}
			if token.Type == htmlpkg.StartTagToken {
				depth++
			}
		case htmlpkg.TextToken:
			if depth > 0 && strings.Contains(tokenizer.Token().Data, pulseRunID) {
				return true
			}
		case htmlpkg.EndTagToken:
			if depth > 0 {
				depth--
			}
		}
	}
}

func markPulseModuleResult(ctx context.Context, workspacePath, module, pulseRunID, result, reason string, evidence []string) (*PulseModuleState, error) {
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return nil, fmt.Errorf("module %q is not valid", module)
	}
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return nil, fmt.Errorf("pulse_run_id is required")
	}
	result = strings.TrimSpace(strings.ToLower(result))
	if result == "" {
		return nil, fmt.Errorf("result is required")
	}
	switch result {
	case "done", "changed", "blocked", "failed", "skipped", "timed_out":
	default:
		return nil, fmt.Errorf("result must be one of done, changed, blocked, failed, skipped, timed_out")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	evidence = normalizePulseEvidence(evidence)
	evidenceJSON, _ := json.Marshal(evidence)
	_, err = db.ExecContext(ctx, `INSERT INTO pulse_module_state (
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
	state, err := getPulseModuleStateByModule(ctx, db, normalized, module)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func markPulseModuleResultFromAgent(ctx context.Context, workspacePath, module, pulseRunID, result, reason string, evidence []string) (*PulseModuleState, error) {
	module = normalizePulseModule(module)
	if !validPulseModules[module] {
		return nil, fmt.Errorf("module %q is not valid", module)
	}
	pulseRunID = strings.TrimSpace(pulseRunID)
	result = strings.TrimSpace(strings.ToLower(result))
	switch result {
	case "done", "changed", "blocked", "failed", "skipped":
	default:
		return nil, fmt.Errorf("result must be one of done, changed, blocked, failed, skipped")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	evidenceJSON, _ := json.Marshal(normalizePulseEvidence(evidence))
	resultExec, err := db.ExecContext(ctx, `UPDATE pulse_module_state SET
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
		existing, readErr := getPulseModuleStateByModule(ctx, db, normalized, module)
		if readErr != nil {
			return nil, fmt.Errorf("Pulse module %q is not an unresolved due module for run %q", module, pulseRunID)
		}
		if existing.LastPulseRunID == pulseRunID && existing.LastResult == result {
			return existing, nil
		}
		return nil, fmt.Errorf("Pulse module %q for run %q is already terminal or belongs to another run", module, pulseRunID)
	}
	return getPulseModuleStateByModule(ctx, db, normalized, module)
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"modules":  states,
		"commands": commands,
	})
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

type pulseModuleScanner interface {
	Scan(dest ...interface{}) error
}

func getPulseModuleStateByModule(ctx context.Context, db *sql.DB, workspacePath, module string) (*PulseModuleState, error) {
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
	recordTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "record_pulse_worklist",
			Description: "Record the dynamic Pulse worklist for this run in the workflow's db/db.sqlite. Pulse Gate must call this exactly once after deciding which modules are due or skipped. The decisions array must contain exactly one entry for each Pulse module: bug_review, artifact_review, learning_health, knowledgebase_health, db_health, eval_health, report_health, cost_llm_time, llm_ops_review, and goal_advisor. Every skipped module must include next_check_at, next_check_after_run_id, or a positive cooldown_runs value. The scheduler reads this table and only sends prompts for due modules.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Scheduler-provided Pulse run id. Use exactly the id in the prompt."},
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
								"cooldown_runs":           map[string]interface{}{"type": "integer", "minimum": 0, "description": "Optional number of future runs to skip unless new evidence overrides it."},
							},
							"required": []string{"module", "due", "reason"},
						},
					},
				},
				"required": []string{"workspace_path", "pulse_run_id", "decisions"},
			}),
		},
	}
	stateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_pulse_module_state",
			Description: "Read the workflow-local Pulse module state from db/db.sqlite so Pulse Gate can decide what is due this run. Use this before record_pulse_worklist.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
				},
				"required": []string{"workspace_path"},
			}),
		},
	}
	resultTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "mark_pulse_module_result",
			Description: "Mark a selected Pulse module as done, changed, blocked, failed, or skipped after its module review and Pulse Fixer work complete. Scheduler timeouts are recorded by the scheduler and cannot be overwritten by an agent.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Scheduler-provided Pulse run id."},
					"module":         map[string]interface{}{"type": "string", "enum": moduleEnum},
					"result":         map[string]interface{}{"type": "string", "enum": []string{"done", "changed", "blocked", "failed", "skipped"}},
					"reason":         map[string]interface{}{"type": "string", "description": "One-sentence result summary."},
					"evidence":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				},
				"required": []string{"workspace_path", "pulse_run_id", "module", "result", "reason"},
			}),
		},
	}
	finalCommandTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "mark_pulse_final_command_result",
			Description: "Record the live or final status of one Pulse final command in the workflow-local db/db.sqlite. The combined Pulse finalizer must mark each command running before work and then done, skipped, blocked, or failed immediately after the command finishes.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, e.g. Workflow/social-media."},
					"pulse_run_id":   map[string]interface{}{"type": "string", "description": "Scheduler-provided Pulse run id."},
					"command":        map[string]interface{}{"type": "string", "enum": pulseFinalCommandOrder},
					"status":         map[string]interface{}{"type": "string", "enum": []string{"running", "done", "skipped", "blocked", "failed"}},
					"reason":         map[string]interface{}{"type": "string", "description": "Short factual status or outcome."},
				},
				"required": []string{"workspace_path", "pulse_run_id", "command", "status", "reason"},
			}),
		},
	}

	executors := map[string]interface{}{
		"record_pulse_worklist": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			pulseRunID, _ := args["pulse_run_id"].(string)
			if err := validateTrustedPulseToolRunID(ctx, pulseRunID); err != nil {
				return "", err
			}
			decisions, err := pulseWorklistDecisionsFromArgs(args["decisions"])
			if err != nil {
				return "", err
			}
			states, err := recordTrustedPulseWorklistOnce(ctx, workspacePath, pulseRunID, decisions)
			if err != nil {
				return "", err
			}
			payload, _ := json.Marshal(map[string]interface{}{"status": "recorded", "modules": states})
			return string(payload), nil
		},
		"get_pulse_module_state": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			states, err := getPulseModuleStates(ctx, workspacePath)
			if err != nil {
				return "", err
			}
			payload, _ := json.Marshal(map[string]interface{}{"modules": states})
			return string(payload), nil
		},
		"mark_pulse_module_result": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			pulseRunID, _ := args["pulse_run_id"].(string)
			module, _ := args["module"].(string)
			result, _ := args["result"].(string)
			reason, _ := args["reason"].(string)
			if err := validateTrustedPulseToolRunID(ctx, pulseRunID); err != nil {
				return "", err
			}
			state, err := markPulseModuleResultFromAgent(ctx, workspacePath, module, pulseRunID, result, reason, stringSliceFromToolArg(args["evidence"]))
			if err != nil {
				return "", err
			}
			payload, _ := json.Marshal(map[string]interface{}{"status": "updated", "module": state})
			return string(payload), nil
		},
		"mark_pulse_final_command_result": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			pulseRunID, _ := args["pulse_run_id"].(string)
			command, _ := args["command"].(string)
			status, _ := args["status"].(string)
			reason, _ := args["reason"].(string)
			if err := validateTrustedPulseToolRunID(ctx, pulseRunID); err != nil {
				return "", err
			}
			state, err := markPulseFinalCommandStateFromAgent(ctx, workspacePath, command, pulseRunID, status, reason)
			if err != nil {
				return "", err
			}
			payload, _ := json.Marshal(map[string]interface{}{"status": "updated", "command": state})
			return string(payload), nil
		},
	}
	categories := map[string]string{
		"record_pulse_worklist":           "workflow",
		"get_pulse_module_state":          "workflow",
		"mark_pulse_module_result":        "workflow",
		"mark_pulse_final_command_result": "workflow",
	}
	return []llmtypes.Tool{recordTool, stateTool, resultTool, finalCommandTool}, executors, categories
}

func pulseWorklistDecisionsFromArgs(raw interface{}) ([]PulseWorklistDecision, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("decisions must be an array")
	}
	out := make([]PulseWorklistDecision, 0, len(arr))
	allowed := map[string]bool{
		"module": true, "due": true, "reason": true, "evidence": true,
		"next_check_at": true, "next_check_after_run_id": true, "cooldown_runs": true,
	}
	for index, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("decisions[%d] must be an object", index)
		}
		for key := range m {
			if !allowed[key] {
				return nil, fmt.Errorf("decisions[%d] contains unknown field %q; use the required boolean field due", index, key)
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
		return "", fmt.Errorf("decisions[%d].%s is required and must be a non-empty string", index, key)
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
		return "", fmt.Errorf("decisions[%d].%s must be a string", index, key)
	}
	return value, nil
}

func strictStringSliceToolArg(raw interface{}) ([]string, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an array of strings")
	}
	out := make([]string, 0, len(arr))
	for index, item := range arr {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("item %d must be a string", index)
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
			return 0, fmt.Errorf("must be an integer")
		}
		return integer, nil
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}

func stringSliceFromToolArg(raw interface{}) []string {
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
	module = strings.ToLower(strings.TrimSpace(module))
	module = strings.ReplaceAll(module, "-", "_")
	switch module {
	case "artifact", "artifact_drift":
		return pulseModuleArtifactReview
	case "report", "reporting", "report_repair":
		return pulseModuleReportHealth
	case "eval", "evaluation", "evaluation_health", "eval_repair":
		return pulseModuleEvalHealth
	case "learnings", "learning", "learning_policy":
		return pulseModuleLearningHealth
	case "kb", "knowledgebase":
		return pulseModuleKnowledgebaseHealth
	case "db", "database":
		return pulseModuleDBHealth
	case "cost", "llm_cost", "cost_time":
		return pulseModuleCostLLMTime
	case "advisor":
		return pulseModuleGoalAdvisor
	default:
		return module
	}
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
