package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type PulseReviewReport struct {
	Module     string `json:"module"`
	PulseRunID string `json:"pulse_run_id"`
	Path       string `json:"path"`
	UpdatedAt  string `json:"updated_at"`
}

// Coverage must outlive the recent activity window. In particular, a newer
// store review of a different scope must not erase the last learnings review.
func loadPulseReviewActivity(ctx context.Context, workspacePath, module string) ([]PulseReviewFocus, []PulseModuleAudit, error) {
	coverage, audits := []PulseReviewFocus{}, []PulseModuleAudit{}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return coverage, audits, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT module,focus_key,pulse_run_id,recorded_at,verdict,selection_reason,route_scope,evidence_json,issue_ids_json
		FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY module,focus_key,route_scope ORDER BY recorded_at DESC,_id DESC) AS rank
		FROM pulse_review_focus_history WHERE workspace_path=? AND (?='' OR module=?)) WHERE rank=1 ORDER BY recorded_at DESC`, normalized, module, module)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		focus := PulseReviewFocus{WorkspacePath: normalized}
		var evidence, issues string
		if err := rows.Scan(&focus.Module, &focus.FocusKey, &focus.LastPulseRunID, &focus.LastReviewedAt, &focus.LastVerdict, &focus.LastSelectionReason, &focus.RouteScope, &evidence, &issues); err != nil {
			rows.Close()
			return nil, nil, err
		}
		focus.UpdatedAt = focus.LastReviewedAt
		_ = json.Unmarshal([]byte(evidence), &focus.Evidence)
		_ = json.Unmarshal([]byte(issues), &focus.IssueIDs)
		coverage = append(coverage, focus)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = db.QueryContext(ctx, `SELECT module,pulse_run_id,result,reason,evidence_json,changed_files_json,verification_json,before_refs_json,after_refs_json,recorded_at
		FROM (SELECT *, ROW_NUMBER() OVER (ORDER BY recorded_at DESC,rowid DESC) AS recent,
        ROW_NUMBER() OVER (PARTITION BY module ORDER BY recorded_at DESC,rowid DESC) AS latest
        FROM pulse_module_audit WHERE workspace_path=? AND (?='' OR module=?) AND result<>'skipped')
        WHERE recent<=100 OR latest=1 ORDER BY recorded_at DESC`, normalized, module, module)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		audit := PulseModuleAudit{WorkspacePath: normalized}
		var evidence, files, verification, before, after string
		if err := rows.Scan(&audit.Module, &audit.PulseRunID, &audit.Result, &audit.Reason, &evidence, &files, &verification, &before, &after, &audit.RecordedAt); err != nil {
			return nil, nil, err
		}
		for raw, target := range map[*string]*[]string{&evidence: &audit.Evidence, &files: &audit.ChangedFiles, &verification: &audit.Verification, &before: &audit.BeforeRefs, &after: &audit.AfterRefs} {
			_ = json.Unmarshal([]byte(*raw), target)
		}
		audits = append(audits, audit)
	}
	return coverage, audits, rows.Err()
}

// Return only files that actually exist, including checkpoints without a
// completed audit. A report's modified time is not a completed-review time.
func listPulseReviewReports(workspacePath, module string) ([]PulseReviewReport, error) {
	normalized, dbPath, err := reportHumanInputDBPath(workspacePath)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(filepath.Dir(filepath.Dir(dbPath)), "runs", "pulse")
	runs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []PulseReviewReport{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := map[string]string{"technical-review.md": pulseModuleTechnicalReview, "architecture-review.md": pulseModuleArchitectureReview, "strategic-review.md": pulseModuleStrategicReview, "plan-drift-review.md": "plan_drift_review"}
	out := []PulseReviewReport{}
	for _, run := range runs {
		if !run.IsDir() {
			continue
		}
		for name, owner := range files {
			if module != "" && module != owner {
				continue
			}
			info, err := os.Lstat(filepath.Join(root, run.Name(), name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			out = append(out, PulseReviewReport{Module: owner, PulseRunID: run.Name(), Path: filepath.ToSlash(filepath.Join(normalized, "runs", "pulse", run.Name(), name)), UpdatedAt: info.ModTime().UTC().Format(time.RFC3339)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].Path < out[j].Path
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}
