package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Notes are optional reasoning, not a second finding ledger or completion signal.
const pulseReviewNotesSchema = `CREATE TABLE IF NOT EXISTS pulse_review_notes (
	workspace_path TEXT NOT NULL, module TEXT NOT NULL, pulse_run_id TEXT NOT NULL,
	content TEXT NOT NULL, updated_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, module, pulse_run_id)
)`

type PulseReviewNote struct {
	Module     string   `json:"module"`
	PulseRunID string   `json:"pulse_run_id"`
	Content    string   `json:"content"`
	Conclusion string   `json:"conclusion,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
	Result     string   `json:"result"`
	UpdatedAt  string   `json:"updated_at"`
}

func savePulseReviewNoteTx(ctx context.Context, tx pulseModuleAuditExecer, workspace, module, run, content, at string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO pulse_review_notes (workspace_path,module,pulse_run_id,content,updated_at)
		VALUES (?,?,?,?,?) ON CONFLICT(workspace_path,module,pulse_run_id) DO UPDATE SET content=excluded.content,updated_at=excluded.updated_at`, workspace, module, run, strings.TrimSpace(content), at)
	return err
}

func savePulseWorkingNote(ctx context.Context, workspace, module, run, content string) error {
	module = normalizePulseModule(module)
	if !validPulseModules[module] || strings.TrimSpace(content) == "" {
		return fmt.Errorf("working note requires a valid module and nonempty review_note or reason")
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspace, true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := getPulseModuleStateByModule(ctx, tx, normalized, module)
	if err != nil || state.LastPulseRunID != run || state.LastDecision != "due" || state.LastResult != "" {
		return fmt.Errorf("working note requires an unresolved due module for this exact Pulse run")
	}
	if err := savePulseReviewNoteTx(ctx, tx, normalized, module, run, content, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// Bounded, module-filtered continuity read. An unfinished note never means done.
func loadPulseReviewNotes(ctx context.Context, workspace, module, run string, limit int) ([]PulseReviewNote, error) {
	out := []PulseReviewNote{}
	module = normalizePulseModule(module)
	if module != "" && !validPulseModules[module] {
		return nil, fmt.Errorf("invalid Pulse module %q", module)
	}
	if limit <= 0 {
		limit = 3
	}
	if limit > 50 {
		limit = 50
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspace, false)
	if err != nil || db == nil {
		return out, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `WITH identities AS (
        SELECT workspace_path,module,pulse_run_id FROM pulse_review_notes
        UNION SELECT workspace_path,module,pulse_run_id FROM pulse_module_audit WHERE result<>'skipped'
    ) SELECT i.module,i.pulse_run_id,COALESCE(n.content,''),COALESCE(a.reason,''),COALESCE(a.evidence_json,'[]'),COALESCE(a.result,'incomplete'),COALESCE(a.recorded_at,n.updated_at)
        FROM identities i
        LEFT JOIN pulse_review_notes n ON n.workspace_path=i.workspace_path AND n.module=i.module AND n.pulse_run_id=i.pulse_run_id
        LEFT JOIN pulse_module_audit a ON a.workspace_path=i.workspace_path AND a.module=i.module AND a.pulse_run_id=i.pulse_run_id
        WHERE i.workspace_path=? AND (?='' OR i.module=?) AND (?='' OR i.pulse_run_id=?)
        ORDER BY COALESCE(a.recorded_at,n.updated_at) DESC,i.pulse_run_id DESC LIMIT ?`, normalized, module, module, run, run, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var note PulseReviewNote
		var evidence string
		if err := rows.Scan(&note.Module, &note.PulseRunID, &note.Content, &note.Conclusion, &evidence, &note.Result, &note.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evidence), &note.Evidence)
		out = append(out, note)
	}
	return out, rows.Err()
}

func readPulseReviewNotesView(ctx context.Context, workspace, module, run string, limit int) (string, error) {
	notes, err := loadPulseReviewNotes(ctx, workspace, module, run, limit)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]interface{}{"notes": notes, "note": "Concise reasoning only. Result=incomplete means no terminal result was saved; this may still be an active review. Use existing findings and decisions as lifecycle authority. Empty notes are normal for older reviews; do not reconstruct or backfill them."})
	return string(data), err
}
