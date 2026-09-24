package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// dismissDuplicateHumanInput never answers a question. Both requests must still
// be pending, identical in decision semantics, and the discarded ID unlinked.
func dismissDuplicateHumanInput(ctx context.Context, workspacePath, inputID, keepID, reason, sessionID string) (*ReportHumanInput, error) {
	defer publishHumanInputsChanged(workspacePath) // live feed: header count + decisions pane
	inputID, keepID, reason = strings.TrimSpace(inputID), strings.TrimSpace(keepID), strings.TrimSpace(reason)
	if inputID == "" || keepID == "" || inputID == keepID || reason == "" {
		return nil, fmt.Errorf("distinct input_id, keep_input_id, and reason are required")
	}
	reportHumanInputStoreMu.Lock()
	defer reportHumanInputStoreMu.Unlock()
	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("workflow decision database not found")
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var matches int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM report_human_inputs a JOIN report_human_inputs b ON b.workspace_path=a.workspace_path
	 WHERE a.workspace_path=? AND a.id=? AND b.id=? AND a.status='pending' AND b.status='pending'
	 AND a.source=b.source AND a.question=b.question AND a.context=b.context AND a.options_json=b.options_json
	 AND a.allow_free_text=b.allow_free_text AND a.apply_contract_json=b.apply_contract_json
	 AND a.run_id=b.run_id AND a.evidence=b.evidence`, normalized, inputID, keepID).Scan(&matches)
	if err != nil {
		return nil, err
	}
	if matches != 1 {
		return nil, fmt.Errorf("requests must both be pending and have identical question, context, options, source, run, evidence, and approval contract")
	}
	for _, spec := range []struct{ table, column string }{{"pulse_finding_events", "metadata_json"}, {"pulse_finding_details", "detail_json"}} {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", spec.table).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		var invalid int
		invalidQuery := fmt.Sprintf("SELECT count(*) FROM %s WHERE trim(coalesce(%s,''))<>'' AND NOT json_valid(%s)", spec.table, spec.column, spec.column)
		if err := tx.QueryRowContext(ctx, invalidQuery).Scan(&invalid); err != nil {
			return nil, err
		}
		if invalid != 0 {
			return nil, fmt.Errorf("cannot safely check decision links: malformed finding metadata")
		}
		// These identifiers are constants, never caller-controlled.
		query := fmt.Sprintf("SELECT count(*) FROM %s r, json_tree(CASE WHEN json_valid(r.%s) THEN r.%s ELSE '{}' END) j WHERE j.key='human_input_id' AND j.value=?", spec.table, spec.column, spec.column)
		var links int
		if err := tx.QueryRowContext(ctx, query, inputID).Scan(&links); err != nil {
			return nil, err
		}
		if links != 0 {
			return nil, fmt.Errorf("input_id %q is linked to a finding; keep linked decisions", inputID)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, `UPDATE report_human_inputs SET status='dismissed', dismissed_at=?, updated_at=? WHERE workspace_path=? AND id=? AND status='pending'`, now, now, normalized, inputID)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, fmt.Errorf("decision changed concurrently")
	}
	details, _ := json.Marshal(map[string]string{"keep_input_id": keepID, "reason": reason})
	if err := writeReportHumanInputEvent(ctx, tx, normalized, reportHumanInputEvent{InputID: inputID, EventType: "duplicate_dismissed", Status: "dismissed", ActorID: "agent", ActorKind: "agent", Channel: "agent_tool", SessionID: sessionID, Details: string(details), CreatedAt: now}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getReportHumanInputByID(ctx, db, normalized, inputID)
}
