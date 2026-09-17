package pulsestore

import (
	"context"
	"database/sql"
	"fmt"
)

// RefreshCompatibility installs the temporary legacy-to-compact projection
// triggers after a legacy schema owner has created or upgraded its tables.
// It can be removed together with the legacy tables after the rollback window.
func RefreshCompatibility(ctx context.Context, db *sql.DB) error {
	current, err := schemaVersion(ctx, db)
	if err != nil || current < CurrentSchemaVersion {
		return err
	}
	ready, err := compatibilityTriggersCurrent(ctx, db)
	if err != nil || ready {
		return err
	}
	// A deployment scan can encounter a very old database before its legacy
	// schema owner has added every historical column. Re-run the idempotent
	// backfill whenever that owner refreshes its schema so those pre-existing
	// rows are projected as soon as they become readable.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := backfillReviews(ctx, tx); err != nil {
		return fmt.Errorf("refresh compact Pulse reviews: %w", err)
	}
	if err := backfillIssues(ctx, tx); err != nil {
		return fmt.Errorf("refresh compact Pulse issues: %w", err)
	}
	if err := backfillDecisions(ctx, tx); err != nil {
		return fmt.Errorf("refresh compact Pulse decisions: %w", err)
	}
	if err := cleanupRetiredReviewers(ctx, tx); err != nil {
		return err
	}
	if err := validateCompactStore(ctx, tx); err != nil {
		return fmt.Errorf("validate refreshed compact Pulse store: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return installCompatibilityTriggers(ctx, db)
}

func compatibilityTriggersCurrent(ctx context.Context, db *sql.DB) (bool, error) {
	required := []string{}
	for table, triggers := range map[string][]string{
		"pulse_module_state":    {"pulse_compact_module_state_insert", "pulse_compact_module_state_update"},
		"pulse_module_audit":    {"pulse_compact_module_audit_insert", "pulse_compact_module_audit_update"},
		"run_concerns":          {"pulse_compact_run_concern_insert", "pulse_compact_run_concern_update"},
		"pulse_finding_details": {"pulse_compact_finding_detail_insert", "pulse_compact_finding_detail_update"},
	} {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return false, err
		}
		if exists {
			required = append(required, triggers...)
		}
	}
	if events, err := tableExists(ctx, db, "pulse_finding_events"); err != nil {
		return false, err
	} else if inputs, inputErr := tableExists(ctx, db, "report_human_inputs"); inputErr != nil {
		return false, inputErr
	} else if concerns, concernErr := tableExists(ctx, db, "run_concerns"); concernErr != nil {
		return false, concernErr
	} else if events && inputs && concerns {
		required = append(required, "pulse_compact_finding_decision_insert")
	}
	if inputs, err := tableExists(ctx, db, "report_human_inputs"); err != nil {
		return false, err
	} else if inputs {
		required = append(required, "pulse_compact_decision_insert", "pulse_compact_decision_update")
	}
	for _, trigger := range required {
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='trigger' AND name=?)`, trigger).Scan(&exists); err != nil {
			return false, err
		}
		if exists == 0 {
			return false, nil
		}
	}
	return true, nil
}

func installCompatibilityTriggers(ctx context.Context, db *sql.DB) error {
	current, err := schemaVersion(ctx, db)
	if err != nil || current < CurrentSchemaVersion {
		return err
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS pulse_compact_module_state_insert;
		DROP TRIGGER IF EXISTS pulse_compact_module_state_update;
		DROP TRIGGER IF EXISTS pulse_compact_module_audit_insert;
		DROP TRIGGER IF EXISTS pulse_compact_module_audit_update;`); err != nil {
		return fmt.Errorf("replace Pulse reviewer projection triggers: %w", err)
	}
	if ok, err := columnsExist(ctx, db, "pulse_module_state", []string{"module", "last_pulse_run_id", "last_decision", "last_reason", "last_result", "last_result_reason", "next_check_at", "evidence_json", "updated_at"}); err != nil {
		return err
	} else if ok {
		if _, err := db.ExecContext(ctx, pulseModuleStateTriggerSQL); err != nil {
			return fmt.Errorf("install Pulse module-state projection: %w", err)
		}
	}
	if ok, err := columnsExist(ctx, db, "pulse_module_audit", []string{"module", "pulse_run_id", "result", "reason", "evidence_json", "recorded_at"}); err != nil {
		return err
	} else if ok {
		if _, err := db.ExecContext(ctx, pulseModuleAuditTriggersSQL); err != nil {
			return fmt.Errorf("install Pulse review projection: %w", err)
		}
	}
	if ok, err := columnsExist(ctx, db, "run_concerns", []string{"fingerprint", "issue_id", "text", "status", "resolution_note", "first_seen_at", "last_seen_at"}); err != nil {
		return err
	} else if ok {
		if _, err := db.ExecContext(ctx, runConcernTriggersSQL); err != nil {
			return fmt.Errorf("install Pulse issue projection: %w", err)
		}
	}
	if ok, err := columnsExist(ctx, db, "pulse_finding_details", []string{"fingerprint", "issue_kind", "detail_json", "updated_at"}); err != nil {
		return err
	} else if ok {
		if _, err := db.ExecContext(ctx, pulseFindingDetailTriggersSQL); err != nil {
			return fmt.Errorf("install Pulse issue-detail projection: %w", err)
		}
	}
	if eventOK, err := columnsExist(ctx, db, "pulse_finding_events", []string{"fingerprint", "metadata_json"}); err != nil {
		return err
	} else if concernOK, concernErr := columnsExist(ctx, db, "run_concerns", []string{"fingerprint", "issue_id"}); concernErr != nil {
		return concernErr
	} else if inputOK, inputErr := columnsExist(ctx, db, "report_human_inputs", []string{"id", "question", "options_json", "status", "selected_option_id", "note", "created_at", "answered_at"}); inputErr != nil {
		return inputErr
	} else if eventOK && concernOK && inputOK {
		if _, err := db.ExecContext(ctx, pulseFindingDecisionTriggerSQL); err != nil {
			return fmt.Errorf("install Pulse finding-decision projection: %w", err)
		}
	}
	if ok, err := columnsExist(ctx, db, "report_human_inputs", []string{"id", "source", "question", "context", "options_json", "status", "selected_option_id", "note", "created_at", "answered_at", "apply_contract_json"}); err != nil {
		return err
	} else if ok {
		if _, err := db.ExecContext(ctx, pulseDecisionTriggersSQL); err != nil {
			return fmt.Errorf("install Pulse decision projection: %w", err)
		}
	}
	return nil
}

func columnsExist(ctx context.Context, db *sql.DB, table string, required []string) (bool, error) {
	exists, err := tableExists(ctx, db, table)
	if err != nil || !exists {
		return false, err
	}
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, name := range required {
		if !found[name] {
			return false, nil
		}
	}
	return true, nil
}

const pulseModuleStateTriggerSQL = `
CREATE TRIGGER IF NOT EXISTS pulse_compact_module_state_insert
AFTER INSERT ON pulse_module_state WHEN trim(NEW.last_pulse_run_id)<>'' AND NEW.module IN
  ('plan_drift_review','technical_review','architecture_review','strategic_review','workflow_review','llmops_review','strategy_auditor','goal_advisor')
BEGIN
  INSERT INTO pulse_reviews(id,pulse_run_id,reviewer_type,status,summary,evidence_json,next_check_at,created_at,updated_at)
  VALUES ('review:' || NEW.last_pulse_run_id || ':' || CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    NEW.last_pulse_run_id, CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    CASE WHEN lower(NEW.last_result) IN ('done','changed','blocked','failed','skipped') THEN lower(NEW.last_result)
         WHEN lower(NEW.last_decision)='due' THEN 'scheduled' ELSE 'skipped' END,
    CASE WHEN trim(NEW.last_result_reason)<>'' THEN NEW.last_result_reason ELSE NEW.last_reason END,
    CASE WHEN json_valid(NEW.evidence_json) THEN NEW.evidence_json ELSE '[]' END,
    NEW.next_check_at, NEW.updated_at, NEW.updated_at)
  ON CONFLICT(pulse_run_id,reviewer_type) DO UPDATE SET
    status=excluded.status,summary=excluded.summary,evidence_json=excluded.evidence_json,
    next_check_at=excluded.next_check_at,updated_at=excluded.updated_at;
END;
CREATE TRIGGER IF NOT EXISTS pulse_compact_module_state_update
AFTER UPDATE ON pulse_module_state WHEN trim(NEW.last_pulse_run_id)<>'' AND NEW.module IN
  ('plan_drift_review','technical_review','architecture_review','strategic_review','workflow_review','llmops_review','strategy_auditor','goal_advisor')
BEGIN
  INSERT INTO pulse_reviews(id,pulse_run_id,reviewer_type,status,summary,evidence_json,next_check_at,created_at,updated_at)
  VALUES ('review:' || NEW.last_pulse_run_id || ':' || CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    NEW.last_pulse_run_id, CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    CASE WHEN lower(NEW.last_result) IN ('done','changed','blocked','failed','skipped') THEN lower(NEW.last_result)
         WHEN lower(NEW.last_decision)='due' THEN 'scheduled' ELSE 'skipped' END,
    CASE WHEN trim(NEW.last_result_reason)<>'' THEN NEW.last_result_reason ELSE NEW.last_reason END,
    CASE WHEN json_valid(NEW.evidence_json) THEN NEW.evidence_json ELSE '[]' END,
    NEW.next_check_at, NEW.updated_at, NEW.updated_at)
  ON CONFLICT(pulse_run_id,reviewer_type) DO UPDATE SET
    status=excluded.status,summary=excluded.summary,evidence_json=excluded.evidence_json,
    next_check_at=excluded.next_check_at,updated_at=excluded.updated_at;
END;`

const pulseModuleAuditTriggersSQL = `
CREATE TRIGGER IF NOT EXISTS pulse_compact_module_audit_insert
AFTER INSERT ON pulse_module_audit WHEN NEW.module IN
  ('plan_drift_review','technical_review','architecture_review','strategic_review','workflow_review','llmops_review','strategy_auditor','goal_advisor')
BEGIN
  INSERT INTO pulse_reviews(id,pulse_run_id,reviewer_type,status,summary,evidence_json,next_check_at,created_at,updated_at)
  VALUES ('review:' || NEW.pulse_run_id || ':' || CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    NEW.pulse_run_id,CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    CASE lower(NEW.result) WHEN 'timed_out' THEN 'failed' ELSE lower(NEW.result) END,
    NEW.reason,CASE WHEN json_valid(NEW.evidence_json) THEN NEW.evidence_json ELSE '[]' END,'',NEW.recorded_at,NEW.recorded_at)
  ON CONFLICT(pulse_run_id,reviewer_type) DO UPDATE SET
    status=excluded.status,summary=excluded.summary,evidence_json=excluded.evidence_json,updated_at=excluded.updated_at;
END;
CREATE TRIGGER IF NOT EXISTS pulse_compact_module_audit_update
AFTER UPDATE ON pulse_module_audit WHEN NEW.module IN
  ('plan_drift_review','technical_review','architecture_review','strategic_review','workflow_review','llmops_review','strategy_auditor','goal_advisor')
BEGIN
  INSERT INTO pulse_reviews(id,pulse_run_id,reviewer_type,status,summary,evidence_json,next_check_at,created_at,updated_at)
  VALUES ('review:' || NEW.pulse_run_id || ':' || CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    NEW.pulse_run_id,CASE NEW.module
      WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
      WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE NEW.module END,
    CASE lower(NEW.result) WHEN 'timed_out' THEN 'failed' ELSE lower(NEW.result) END,
    NEW.reason,CASE WHEN json_valid(NEW.evidence_json) THEN NEW.evidence_json ELSE '[]' END,'',NEW.recorded_at,NEW.recorded_at)
  ON CONFLICT(pulse_run_id,reviewer_type) DO UPDATE SET
    status=excluded.status,summary=excluded.summary,evidence_json=excluded.evidence_json,updated_at=excluded.updated_at;
END;`

const runConcernTriggersSQL = `
CREATE TRIGGER IF NOT EXISTS pulse_compact_run_concern_insert
AFTER INSERT ON run_concerns
BEGIN
  DELETE FROM pulse_issues
  WHERE dedupe_key=NEW.fingerprint
    AND id<>CASE WHEN trim(NEW.issue_id)<>'' THEN NEW.issue_id ELSE 'PUL-' || upper(substr(NEW.fingerprint,1,8)) END;
  INSERT INTO pulse_issues(id,issue_type,description,evidence_json,dedupe_key,status,action_taken,created_at,updated_at)
  VALUES (CASE WHEN trim(NEW.issue_id)<>'' THEN NEW.issue_id ELSE 'PUL-' || upper(substr(NEW.fingerprint,1,8)) END,
    'workflow_issue',CASE WHEN trim(NEW.text)<>'' THEN NEW.text ELSE 'Pulse issue' END,'[]',NEW.fingerprint,
    CASE WHEN NEW.status IN ('resolved','rejected') THEN 'closed' ELSE 'open' END,
    CASE WHEN NEW.status IN ('resolved','rejected') THEN
      CASE WHEN trim(NEW.resolution_note)<>'' THEN NEW.resolution_note
           WHEN NEW.status='rejected' THEN 'Rejected in the legacy Pulse lifecycle; no additional action was recorded.'
           ELSE 'Resolved in the legacy Pulse lifecycle; no action detail was recorded.' END ELSE '' END,
    CASE WHEN trim(NEW.first_seen_at)<>'' THEN NEW.first_seen_at ELSE NEW.last_seen_at END,
    CASE WHEN trim(NEW.last_seen_at)<>'' THEN NEW.last_seen_at ELSE NEW.first_seen_at END)
  ON CONFLICT(id) DO UPDATE SET description=excluded.description,dedupe_key=excluded.dedupe_key,
    status=excluded.status,action_taken=excluded.action_taken,updated_at=excluded.updated_at;
END;
CREATE TRIGGER IF NOT EXISTS pulse_compact_run_concern_update
AFTER UPDATE ON run_concerns
BEGIN
  DELETE FROM pulse_issues
  WHERE dedupe_key=NEW.fingerprint
    AND id<>CASE WHEN trim(NEW.issue_id)<>'' THEN NEW.issue_id ELSE 'PUL-' || upper(substr(NEW.fingerprint,1,8)) END;
  INSERT INTO pulse_issues(id,issue_type,description,evidence_json,dedupe_key,status,action_taken,created_at,updated_at)
  VALUES (CASE WHEN trim(NEW.issue_id)<>'' THEN NEW.issue_id ELSE 'PUL-' || upper(substr(NEW.fingerprint,1,8)) END,
    'workflow_issue',CASE WHEN trim(NEW.text)<>'' THEN NEW.text ELSE 'Pulse issue' END,'[]',NEW.fingerprint,
    CASE WHEN NEW.status IN ('resolved','rejected') THEN 'closed' ELSE 'open' END,
    CASE WHEN NEW.status IN ('resolved','rejected') THEN
      CASE WHEN trim(NEW.resolution_note)<>'' THEN NEW.resolution_note
           WHEN NEW.status='rejected' THEN 'Rejected in the legacy Pulse lifecycle; no additional action was recorded.'
           ELSE 'Resolved in the legacy Pulse lifecycle; no action detail was recorded.' END ELSE '' END,
    CASE WHEN trim(NEW.first_seen_at)<>'' THEN NEW.first_seen_at ELSE NEW.last_seen_at END,
    CASE WHEN trim(NEW.last_seen_at)<>'' THEN NEW.last_seen_at ELSE NEW.first_seen_at END)
  ON CONFLICT(id) DO UPDATE SET description=excluded.description,dedupe_key=excluded.dedupe_key,
    status=excluded.status,action_taken=excluded.action_taken,updated_at=excluded.updated_at;
END;`

const pulseFindingDetailTriggersSQL = `
CREATE TRIGGER IF NOT EXISTS pulse_compact_finding_detail_insert
AFTER INSERT ON pulse_finding_details
BEGIN
  UPDATE pulse_issues SET
    issue_type=CASE WHEN trim(NEW.issue_kind)<>'' THEN NEW.issue_kind ELSE issue_type END,
    evidence_json=CASE WHEN json_valid(NEW.detail_json) AND json_type(NEW.detail_json,'$.evidence')='array'
                       THEN json_extract(NEW.detail_json,'$.evidence') ELSE evidence_json END,
    updated_at=NEW.updated_at
  WHERE dedupe_key=NEW.fingerprint;
END;
CREATE TRIGGER IF NOT EXISTS pulse_compact_finding_detail_update
AFTER UPDATE ON pulse_finding_details
BEGIN
  UPDATE pulse_issues SET
    issue_type=CASE WHEN trim(NEW.issue_kind)<>'' THEN NEW.issue_kind ELSE issue_type END,
    evidence_json=CASE WHEN json_valid(NEW.detail_json) AND json_type(NEW.detail_json,'$.evidence')='array'
                       THEN json_extract(NEW.detail_json,'$.evidence') ELSE evidence_json END,
    updated_at=NEW.updated_at
  WHERE dedupe_key=NEW.fingerprint;
END;`

const pulseDecisionTriggersSQL = `
CREATE TRIGGER IF NOT EXISTS pulse_compact_decision_insert
AFTER INSERT ON report_human_inputs
WHEN NEW.source IN ('pulse','plan_drift_review','technical_review','architecture_review','strategic_review','strategy_auditor','goal_advisor')
 AND COALESCE(json_extract(NEW.apply_contract_json,'$.issue_id'),'')<>''
 AND EXISTS(SELECT 1 FROM pulse_issues WHERE id=json_extract(NEW.apply_contract_json,'$.issue_id'))
BEGIN
  INSERT INTO pulse_decisions(id,issue_id,question,options_json,status,answer,created_at,answered_at)
  VALUES (NEW.id,json_extract(NEW.apply_contract_json,'$.issue_id'),NEW.question,
    CASE WHEN json_valid(NEW.options_json) THEN NEW.options_json ELSE '[]' END,
    CASE WHEN trim(NEW.answered_at)<>'' OR NEW.status IN ('answered','consumed') THEN 'answered' ELSE 'pending' END,
    trim(NEW.selected_option_id || CASE WHEN trim(NEW.note)<>'' THEN ': ' || NEW.note ELSE '' END),NEW.created_at,NEW.answered_at)
  ON CONFLICT(id) DO UPDATE SET issue_id=excluded.issue_id,question=excluded.question,options_json=excluded.options_json,
    status=excluded.status,answer=excluded.answer,answered_at=excluded.answered_at;
END;
CREATE TRIGGER IF NOT EXISTS pulse_compact_decision_update
AFTER UPDATE ON report_human_inputs
WHEN NEW.source IN ('pulse','plan_drift_review','technical_review','architecture_review','strategic_review','strategy_auditor','goal_advisor')
 AND COALESCE(json_extract(NEW.apply_contract_json,'$.issue_id'),'')<>''
 AND EXISTS(SELECT 1 FROM pulse_issues WHERE id=json_extract(NEW.apply_contract_json,'$.issue_id'))
BEGIN
  INSERT INTO pulse_decisions(id,issue_id,question,options_json,status,answer,created_at,answered_at)
  VALUES (NEW.id,json_extract(NEW.apply_contract_json,'$.issue_id'),NEW.question,
    CASE WHEN json_valid(NEW.options_json) THEN NEW.options_json ELSE '[]' END,
    CASE WHEN trim(NEW.answered_at)<>'' OR NEW.status IN ('answered','consumed') THEN 'answered' ELSE 'pending' END,
    trim(NEW.selected_option_id || CASE WHEN trim(NEW.note)<>'' THEN ': ' || NEW.note ELSE '' END),NEW.created_at,NEW.answered_at)
  ON CONFLICT(id) DO UPDATE SET issue_id=excluded.issue_id,question=excluded.question,options_json=excluded.options_json,
    status=excluded.status,answer=excluded.answer,answered_at=excluded.answered_at;
END;`

const pulseFindingDecisionTriggerSQL = `
CREATE TRIGGER IF NOT EXISTS pulse_compact_finding_decision_insert
AFTER INSERT ON pulse_finding_events
WHEN COALESCE(json_extract(NEW.metadata_json,'$.human_input_id'),'')<>''
BEGIN
  INSERT INTO pulse_decisions(id,issue_id,question,options_json,status,answer,created_at,answered_at)
  SELECT h.id,c.issue_id,h.question,
    CASE WHEN json_valid(h.options_json) THEN h.options_json ELSE '[]' END,
    CASE WHEN trim(h.answered_at)<>'' OR h.status IN ('answered','consumed') THEN 'answered' ELSE 'pending' END,
    trim(h.selected_option_id || CASE WHEN trim(h.note)<>'' THEN ': ' || h.note ELSE '' END),h.created_at,h.answered_at
  FROM report_human_inputs h JOIN run_concerns c ON c.fingerprint=NEW.fingerprint
  WHERE h.id=json_extract(NEW.metadata_json,'$.human_input_id') AND trim(c.issue_id)<>''
    AND EXISTS(SELECT 1 FROM pulse_issues WHERE id=c.issue_id)
  ON CONFLICT(id) DO UPDATE SET issue_id=excluded.issue_id,question=excluded.question,options_json=excluded.options_json,
    status=excluded.status,answer=excluded.answer,answered_at=excluded.answered_at;
END;`
