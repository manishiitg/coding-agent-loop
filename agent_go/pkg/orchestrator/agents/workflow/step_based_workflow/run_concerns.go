package step_based_workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/pulsemodules"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/pulsestore"
	"github.com/manishiitg/coding-agent-loop/workspace/sqliteopen"

	_ "modernc.org/sqlite"
)

// Durable record of the `CONCERNS:` lines steps emit.
//
// Why this exists: a concern used to live only as a substring inside the step's
// completion summary. Pulse Gate grepped that text for the literal marker, and
// if the Gate's cooldown said the matching module wasn't due, the concern was
// gone. Nothing could answer "has this been reported before?" — pulse_module_state
// keeps only `last_*` fields, so run N overwrites run N-1, and pulse_module_audit
// (the table that was meant to hold history) is empty in most workflows because
// it depends on an agent remembering to call a tool.
//
// That last point is the design constraint here: these rows are written by Go at
// the moment the completion summary is composed, never by an agent calling a
// tool. There is no call for an agent to skip, so the table cannot quietly end up
// empty the way pulse_module_audit did.

const runConcernsSchema = `CREATE TABLE IF NOT EXISTS run_concerns (
	fingerprint TEXT PRIMARY KEY,
	issue_id TEXT NOT NULL DEFAULT '',
	step_id TEXT NOT NULL DEFAULT '',
	phase TEXT NOT NULL DEFAULT '',
	group_name TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL DEFAULT '',
	first_seen_run TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL DEFAULT '',
	last_seen_run TEXT NOT NULL DEFAULT '',
	last_seen_at TEXT NOT NULL DEFAULT '',
	seen_count INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'open',
	resolved_at TEXT NOT NULL DEFAULT '',
	resolved_by TEXT NOT NULL DEFAULT '',
	resolution_note TEXT NOT NULL DEFAULT '',
	first_seen_platform_version TEXT NOT NULL DEFAULT ''
)`

// Phases a concern can be raised from. The step body and its two closing turns
// are separate sources: knowing a contradiction came from the learnings turn
// rather than the task itself is what tells a reviewer where to look.
const (
	ConcernPhaseExecution       = "execution"
	ConcernPhaseLearnings       = "learnings"
	ConcernPhaseKBReview        = "kb-review"
	ConcernPhaseMessageSequence = "message-sequence"
	// ConcernPhaseReview covers Pulse reviewer artifacts. The "step" for these is
	// the module name (bug_review, db_health, ...) rather than a plan step.
	ConcernPhaseReview = "review"
	// ConcernPhasePreValidation is filed by Go itself (SavePreValidationLog),
	// not by an agent — there is no step turn to emit a CONCERNS: line, since a
	// failing gate produces a repair turn, not a completion summary. Written at
	// the moment a validation_schema check fails so a step whose schema demands
	// an undocumented field, or rejects a legitimate null, surfaces as evidence
	// even on a run where it eventually passed after repair and left no other
	// trace. See ParseConcernLines and RecordRunConcerns.
	ConcernPhasePreValidation = "prevalidation"
)

const (
	ConcernStatusOpen                   = "open"
	ConcernStatusAcknowledged           = "acknowledged"
	ConcernStatusExternalActionRequired = "external_action_required"
	ConcernStatusResolved               = "resolved"
	ConcernStatusRejected               = "rejected"
)

const concernLinePrefix = "CONCERNS:"

// RunConcern is one deduplicated concern with its recurrence history.
type RunConcern struct {
	Fingerprint    string `json:"-"`
	IssueID        string `json:"issue_id"`
	StepID         string `json:"step_id"`
	Phase          string `json:"phase"`
	GroupName      string `json:"group_name,omitempty"`
	Text           string `json:"text"`
	FirstSeenRun   string `json:"first_seen_run,omitempty"`
	FirstSeenAt    string `json:"first_seen_at,omitempty"`
	LastSeenRun    string `json:"last_seen_run,omitempty"`
	LastSeenAt     string `json:"last_seen_at,omitempty"`
	SeenCount      int    `json:"seen_count"`
	Status         string `json:"status"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	ResolvedBy     string `json:"resolved_by,omitempty"`
	ResolutionNote string `json:"resolution_note,omitempty"`
}

// newPulseIssueID creates the durable public address for a newly discovered
// concern. It must never be derived from the concern wording: wording changes
// are reviewed semantically and may reopen or merge an existing issue, while
// the issue ID itself remains stable.
func newPulseIssueID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return fmt.Sprintf("PUL-%X", bytes)
	}
	// crypto/rand failure is exceptionally rare. The timestamp fallback keeps
	// recording concerns available rather than making a completed step fail.
	return fmt.Sprintf("PUL-%X", time.Now().UTC().UnixNano())
}

// ParseConcernLines pulls the `CONCERNS:` payloads out of a summary blob.
//
// The marker is matched case-insensitively at the start of a trimmed line, which
// is the shape the prompts ask for and the shape the message-sequence aggregator
// already assumed. Everything after the prefix on that line is the concern.
//
// PLAT-211: a legacy review/advisor summary can render the marker as a
// Markdown inline-code span -- "`CONCERNS: foo`" instead of a bare
// "CONCERNS: foo" line. That backtick is real content the model wrote, not
// whitespace strings.TrimSpace touches, so the bare prefix check silently
// dropped it: no error, no warning, the review still recorded as completed,
// and the finding just never reached run_concerns at all. Confirmed live on
// HDFC-Personal-Accounts: six such findings vanished with no trace until a
// later stage's fingerprint lookup came up empty. stripMarkdownCodeSpan
// removes one matching pair of backtick-run delimiters wrapping the WHOLE
// line before the prefix check runs.
func ParseConcernLines(summary string) []string {
	var out []string
	for _, line := range strings.Split(summary, "\n") {
		trimmed := stripMarkdownCodeSpan(strings.TrimSpace(line))
		if !strings.HasPrefix(strings.ToUpper(trimmed), concernLinePrefix) {
			continue
		}
		body := strings.TrimSpace(trimmed[len(concernLinePrefix):])
		// A bare "CONCERNS:" with no payload carries no information.
		if body == "" {
			continue
		}
		out = append(out, body)
	}
	return out
}

// stripMarkdownCodeSpan removes a single matching pair of backtick-run
// delimiters that wrap an ENTIRE line, e.g. "`CONCERNS: foo`" -> "CONCERNS: foo".
// Markdown inline code spans open and close with the same run length of one
// or more backticks (a longer run lets the span contain a shorter run of
// backticks as literal text). Only a matching open/close run at the very
// start and end of the line is stripped, so a line that merely mentions a
// backtick mid-sentence, or opens a span it never closes, is left untouched.
func stripMarkdownCodeSpan(line string) string {
	if !strings.HasPrefix(line, "`") {
		return line
	}
	openLen := 0
	for openLen < len(line) && line[openLen] == '`' {
		openLen++
	}
	closeDelim := strings.Repeat("`", openLen)
	if len(line) < openLen*2 || !strings.HasSuffix(line, closeDelim) {
		return line
	}
	inner := line[openLen : len(line)-openLen]
	if strings.Contains(inner, closeDelim) {
		// The delimiter recurs inside the span -- not actually the closing
		// fence for THIS span (e.g. "`a` CONCERNS: `b`" is two spans, not one
		// wrapping the whole line). Leave it alone rather than guess.
		return line
	}
	return strings.TrimSpace(inner)
}

// concernFingerprint identifies "the same concern raised again".
//
// Normalization is deliberately conservative: lowercase and collapse whitespace,
// but do NOT strip digits. Stripping them would merge "12 items stale" with
// "3 items stale" — the same underlying issue at a different magnitude, where the
// magnitude is the interesting part. Under-merging shows up as visible
// near-duplicates and is recoverable; over-merging silently hides a changing
// problem behind one row.
func concernFingerprint(stepID, text string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	sum := sha256.Sum256([]byte(strings.TrimSpace(stepID) + "\x00" + normalized))
	return hex.EncodeToString(sum[:])[:16]
}

// preValidationConcernFingerprint identifies the step's output-contract gate,
// not an individual JSON path. The detailed failed checks remain in the concern
// text and the per-run pre_validation.json evidence, while one lifecycle row
// represents the repair the Fixer must make.
func preValidationConcernFingerprint(stepID string) string {
	return concernFingerprint(stepID, "prevalidation:step-output-contract")
}

// existingCanonicalReviewFingerprint preserves the identity of an exact finding
// in the same current review lane.
func existingCanonicalReviewFingerprint(ctx context.Context, db pulseFindingLifecycleDB, module, text string) string {
	wanted := strings.ToLower(strings.Join(strings.Fields(text), " "))
	args := []interface{}{ConcernPhaseReview, module}
	rows, err := db.QueryContext(ctx, `SELECT fingerprint, text FROM run_concerns
		WHERE phase=? AND step_id=?`, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var fingerprint, existingText string
		if rows.Scan(&fingerprint, &existingText) == nil &&
			strings.ToLower(strings.Join(strings.Fields(existingText), " ")) == wanted {
			return fingerprint
		}
	}
	return ""
}

func runConcernsDBPath(workspacePath string) string {
	return filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(strings.Trim(strings.TrimSpace(workspacePath), "/")), "db", "db.sqlite")
}

// openRunConcernsDB opens the workflow's shared db.sqlite -- the same file
// report_human_inputs.go, workspace/handlers/query.go, and
// openPlanDriftQueryOnlyDB all open, concurrently, from multiple processes
// (this server plus scheduled background Pulse jobs). Built on the shared
// sqliteopen.DSN helper (journal_mode=WAL and busy_timeout embedded in the
// DSN) rather than a bare path + a one-time runtime PRAGMA call: this was
// the last, and most-used, holdout still opening this file the old way.
// Confirmed live on the Dominion deployment 2026-08-31: a standalone
// reproduction of a query through openPlanDriftQueryOnlyDB's already-WAL-DSN
// connection was fast in isolation, but hung specifically while this
// function's live, non-WAL-DSN connections from the running server were
// concurrently active against the same file -- consistent with
// modernc.org/sqlite's own WAL/locking negotiation being connection-DSN-
// driven, not purely governed by the file's on-disk journal_mode header.
func openRunConcernsDB(ctx context.Context, workspacePath string, create bool) (*sql.DB, error) {
	_ = ctx // busy_timeout is now DSN-embedded (see sqliteopen.DSN), not a runtime PRAGMA on ctx
	dbPath := runConcernsDBPath(workspacePath)
	if create {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteopen.DSN(dbPath))
	if err != nil {
		return nil, err
	}
	if _, err := pulsestore.Ensure(ctx, db, dbPath); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate compact Pulse store: %w", err)
	}
	return db, nil
}

// RecordRunConcerns extracts every concern from summary and upserts it.
//
// Best-effort by contract: a step that did its work must not fail because its
// concern could not be filed. Callers log and continue.
func RecordRunConcerns(ctx context.Context, workspacePath, runFolder, groupName, stepID, phase, summary string) (int, error) {
	if phase == ConcernPhaseReview && pulsemodules.IsValid(stepID) {
		stepID = pulsemodules.Normalize(stepID)
	}
	lines := ParseConcernLines(summary)
	if len(lines) == 0 {
		return 0, nil
	}
	if phase == ConcernPhaseReview {
		if err := validatePulseAdvisorFindingRoutes(stepID, summary); err != nil {
			return 0, err
		}
	}
	db, err := openRunConcernsDB(ctx, workspacePath, true)
	if err != nil || db == nil {
		return 0, err
	}
	defer db.Close()
	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		return 0, err
	}
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	fingerprints := pulseFindingFingerprintsByConcern(summary, stepID)
	for concern, fingerprint := range fingerprints {
		if canonical := canonicalFingerprintForMergedIssue(ctx, db, fingerprint); canonical != "" {
			fingerprints[concern] = canonical
		}
	}
	recorded, err := recordRunConcernLinesAtWithFingerprints(
		ctx, db, runFolder, groupName, stepID, phase, lines,
		observedAt, fingerprints,
	)
	if err != nil {
		return recorded, err
	}
	if err := recordPulseFindingDetailsAt(
		ctx, db, workspacePath, runFolder, stepID, summary, observedAt, lines, fingerprints,
	); err != nil {
		return recorded, err
	}
	return recorded, nil
}

func recordRunConcernLinesAtWithFingerprints(
	ctx context.Context,
	db pulseFindingLifecycleDB,
	runFolder, groupName, stepID, phase string,
	lines []string,
	observedAt string,
	fingerprints map[string]string,
) (int, error) {
	if strings.TrimSpace(observedAt) == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	recorded := 0
	for _, text := range lines {
		issueID := newPulseIssueID()
		normalizedText := strings.ToLower(strings.Join(strings.Fields(text), " "))
		fp := fingerprints[normalizedText]
		if phase == ConcernPhasePreValidation {
			fp = preValidationConcernFingerprint(stepID)
		} else if fp == "" {
			fp = concernFingerprint(stepID, text)
		}
		if phase == ConcernPhaseReview && pulsemodules.IsValid(stepID) {
			if historical := existingCanonicalReviewFingerprint(ctx, db, stepID, text); historical != "" {
				fp = historical
			}
		}
		if canonical := canonicalFingerprintForMergedIssue(ctx, db, fp); canonical != "" {
			fp = canonical
		}
		previousStatus := ""
		previousStepID := ""
		if err := db.QueryRowContext(ctx, `SELECT status, step_id FROM run_concerns WHERE fingerprint=?`, fp).Scan(&previousStatus, &previousStepID); err != nil && err != sql.ErrNoRows {
			return recorded, err
		}
		// A recurring write's step_id may only UPGRADE a placeholder to a real
		// attribution, never move an already-real one. Before per-caller
		// step_id arguments existed, every finding's row got a canonical
		// module name (e.g. "plan_drift_review") as its step_id fallback; that
		// is not a real step identity, and a legacy or module-wide row stuck
		// with one would otherwise be a permanent dead end for any caller that
		// needs exact-step attribution (e.g. plan_drift_review's own
		// verifyStepDriftCheckFindingsExist) — even though reusing the same
		// fingerprint/issue for the same semantic root cause is exactly what
		// callers are told to do. A previousStepID that is NOT a canonical
		// module name is already real and must never silently move to a
		// different step just because a later call passes a different one.
		finalStepID := stepID
		if previousStepID != "" && !pulsemodules.IsValid(previousStepID) {
			finalStepID = previousStepID
		}
		// A concern that recurs after being marked resolved reopens: the fix did
		// not hold, and that is strictly more important than the original report.
		// A concern recurring while awaiting verification is the failed
		// verification signal itself, so it also returns to open.
		// A concern still recurring after the run it was waiting for means that
		// run happened and did not resolve it, so awaiting_run reopens too.
		// "rejected" is deliberately sticky — someone judged it a non-issue, and
		// recurrence is not new evidence against that judgement.
		// first_seen_platform_version is written on INSERT only; the ON CONFLICT
		// branch deliberately leaves it alone. It answers "what was this first
		// observed against", so a recurrence must not overwrite it with a newer
		// revision — that would erase exactly the signal a staleness sweep reads.
		_, err := db.ExecContext(ctx, `INSERT INTO run_concerns
			(fingerprint, issue_id, step_id, phase, group_name, text, first_seen_run, first_seen_at, last_seen_run, last_seen_at, seen_count, status, first_seen_platform_version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(fingerprint) DO UPDATE SET
				step_id = excluded.step_id,
				text = excluded.text,
				phase = excluded.phase,
				group_name = excluded.group_name,
				last_seen_run = excluded.last_seen_run,
				last_seen_at = excluded.last_seen_at,
				seen_count = run_concerns.seen_count + CASE
					WHEN excluded.phase = 'prevalidation' AND run_concerns.last_seen_run = excluded.last_seen_run THEN 0
					ELSE 1
				END,
				status = CASE WHEN run_concerns.status IN (?, ?, ?) THEN ? ELSE run_concerns.status END`,
			fp, issueID, finalStepID, phase, groupName, text, runFolder, observedAt, runFolder, observedAt, ConcernStatusOpen, PlatformVersion(),
			ConcernStatusResolved, ConcernStatusAwaitingVerification, ConcernStatusAwaitingRun, ConcernStatusOpen)
		if err != nil {
			return recorded, err
		}
		eventType := "observed_again"
		switch previousStatus {
		case "":
			eventType = "filed"
		case ConcernStatusResolved, ConcernStatusAwaitingVerification, ConcernStatusAwaitingRun:
			eventType = "reopened"
		}
		metadata, _ := json.Marshal(map[string]string{
			"previous_status": previousStatus,
			"phase":           phase,
			"step_id":         stepID,
		})
		if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_events
			(fingerprint, pulse_run_id, event_type, summary, metadata_json, recorded_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(fingerprint, pulse_run_id, attempt_id, event_type) DO NOTHING`,
			fp, runFolder, eventType, text, string(metadata), observedAt); err != nil {
			return recorded, err
		}
		recorded++
	}
	return recorded, nil
}

// canonicalFingerprintForMergedIssue keeps retired semantic aliases retired.
// Merge history stores the canonical public issue_id; a later workflow
// occurrence of the old symptom must append to and reopen the canonical issue,
// not recreate the duplicate queue item.
func canonicalFingerprintForMergedIssue(ctx context.Context, db pulseFindingLifecycleDB, fingerprint string) string {
	var issueID string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(json_extract(detail_json, '$.merged_into_issue_id'), '')
		FROM pulse_finding_details WHERE fingerprint=?`, fingerprint).Scan(&issueID)
	if err != nil || strings.TrimSpace(issueID) == "" {
		return ""
	}
	var canonical string
	err = db.QueryRowContext(ctx, `SELECT fingerprint FROM run_concerns
		WHERE upper(issue_id)=upper(?)
		ORDER BY first_seen_at ASC LIMIT 1`, strings.TrimSpace(issueID)).Scan(&canonical)
	if err != nil || canonical == fingerprint {
		return ""
	}
	return canonical
}

// LoadOpenRunConcerns returns concerns still needing Pulse attention. A negative
// limit returns the complete active backlog; zero preserves the bounded default
// for callers that only need a preview.
func LoadOpenRunConcerns(ctx context.Context, workspacePath string, limit int) ([]RunConcern, error) {
	db, err := openRunConcernsDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return nil, err
	}
	defer db.Close()
	if limit == 0 {
		limit = 50
	}
	// Rank by how many active concerns a step has before ranking by how often
	// any one of them recurred. Prevalidation is already one root concern per
	// step; this clustering remains useful for distinct execution/review concerns
	// that share a step and should be read together.
	query := `SELECT c.fingerprint, c.issue_id, c.step_id, c.phase, c.group_name, c.text,
			c.first_seen_run, c.first_seen_at, c.last_seen_run, c.last_seen_at, c.seen_count, c.status
		FROM run_concerns c
		JOIN (
			SELECT step_id, COUNT(*) AS active_count, MAX(seen_count) AS peak_seen
			FROM run_concerns
			WHERE status IN (?, ?, ?, ?, ?, ?)
			GROUP BY step_id
		) cluster ON cluster.step_id = c.step_id
		WHERE c.status IN (?, ?, ?, ?, ?, ?)
		ORDER BY cluster.active_count DESC, cluster.peak_seen DESC, c.step_id ASC,
			c.seen_count DESC, c.first_seen_at ASC, c.last_seen_at DESC`
	args := []interface{}{
		ConcernStatusOpen, ConcernStatusAcknowledged, ConcernStatusFixing, ConcernStatusAwaitingVerification, ConcernStatusAwaitingRun, ConcernStatusQueuedForEngineering,
		ConcernStatusOpen, ConcernStatusAcknowledged, ConcernStatusFixing, ConcernStatusAwaitingVerification, ConcernStatusAwaitingRun, ConcernStatusQueuedForEngineering,
	}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		// Absent table just means no concern has been raised yet.
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var out []RunConcern
	for rows.Next() {
		var c RunConcern
		if err := rows.Scan(&c.Fingerprint, &c.IssueID, &c.StepID, &c.Phase, &c.GroupName, &c.Text,
			&c.FirstSeenRun, &c.FirstSeenAt, &c.LastSeenRun, &c.LastSeenAt, &c.SeenCount, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadExternallyOwnedRunConcerns returns findings Pulse has diagnosed as real
// but cannot act on in this workflow. Reviewers receive these as a suppression
// list; recurrence updates their audit history without putting them back in the
// active Pulse queue.
func LoadExternallyOwnedRunConcerns(ctx context.Context, workspacePath string) ([]RunConcern, error) {
	db, err := openRunConcernsDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT fingerprint, issue_id, step_id, phase, group_name, text,
			first_seen_run, first_seen_at, last_seen_run, last_seen_at, seen_count, status,
			resolved_at, resolved_by, resolution_note
		FROM run_concerns
		WHERE status=?
		ORDER BY last_seen_at DESC, first_seen_at ASC`, ConcernStatusExternalActionRequired)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []RunConcern
	for rows.Next() {
		var concern RunConcern
		if err := rows.Scan(
			&concern.Fingerprint, &concern.IssueID, &concern.StepID, &concern.Phase, &concern.GroupName,
			&concern.Text, &concern.FirstSeenRun, &concern.FirstSeenAt,
			&concern.LastSeenRun, &concern.LastSeenAt, &concern.SeenCount,
			&concern.Status, &concern.ResolvedAt, &concern.ResolvedBy,
			&concern.ResolutionNote,
		); err != nil {
			return nil, err
		}
		out = append(out, concern)
	}
	return out, rows.Err()
}

// ResolveRunConcern records a terminal judgement on a concern.
//
// Absence never resolves a concern: a report that stops appearing may just mean
// the step did not run, or took a different path. Auto-closing on absence would
// quietly delete real findings, so closing one is always an explicit act.
func ResolveRunConcern(ctx context.Context, workspacePath, fingerprint, status, resolvedBy, note string) error {
	switch status {
	case ConcernStatusAcknowledged, ConcernStatusResolved, ConcernStatusRejected:
	default:
		return fmt.Errorf("invalid concern status %q", status)
	}
	db, err := openRunConcernsDB(ctx, workspacePath, false)
	if err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("no workflow database at %s", runConcernsDBPath(workspacePath))
	}
	defer db.Close()

	res, err := db.ExecContext(ctx, `UPDATE run_concerns
		SET status = ?, resolved_at = ?, resolved_by = ?, resolution_note = ?
		WHERE fingerprint = ?`,
		status, time.Now().UTC().Format(time.RFC3339), resolvedBy, note, fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no concern with fingerprint %q", fingerprint)
	}
	return nil
}

// LoadPriorPreValidationFailures returns this step's still-open prevalidation
// concerns, newest first, so the next run can be told what its last output got
// wrong.
//
// Why this exists. A scripted step that fails gets its own error handed back:
// controller_execution.go renders ScriptedPriorScript and ScriptedPriorError
// into the prompt, and the LLM repairs the script. An agentic step had no
// equivalent. It receives ValidationSchema — so it knows the shape it must
// produce — but prevalidation runs *after* it, and nothing carried the verdict
// forward. The step therefore read an identical prompt every run and wrote the
// same wrong shape every run.
//
// tectonicusadaytrading's deliver-briefing is the worked example: five concerns
// filed 2026-07-29, still open at seen_count 3 on 2026-07-30, all of the form
// "$.delivery_status must exist but was not found: unknown key". A stable
// contract mismatch that recurred purely because the only party able to fix it
// was never told.
//
// Reads run_concerns rather than the prevalidation log because these rows are
// already durable, already deduplicated by fingerprint, and already carry
// seen_count — recurrence is the strongest signal that the step is not learning,
// and it is free here.
func LoadPriorPreValidationFailures(ctx context.Context, workspacePath, stepID string, limit int) ([]RunConcern, error) {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return nil, nil
	}
	db, err := openRunConcernsDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return nil, err
	}
	defer db.Close()
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.QueryContext(ctx, `SELECT fingerprint, issue_id, step_id, phase, group_name, text,
			first_seen_run, first_seen_at, last_seen_run, last_seen_at, seen_count, status,
			resolved_at, resolved_by, resolution_note
		FROM run_concerns
		WHERE step_id=? AND phase=? AND status NOT IN (?, ?, ?)
		ORDER BY seen_count DESC, last_seen_at DESC
		LIMIT ?`,
		stepID, ConcernPhasePreValidation,
		ConcernStatusResolved, ConcernStatusRejected, ConcernStatusExternalActionRequired,
		limit)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []RunConcern
	for rows.Next() {
		var concern RunConcern
		if err := rows.Scan(
			&concern.Fingerprint, &concern.IssueID, &concern.StepID, &concern.Phase, &concern.GroupName,
			&concern.Text, &concern.FirstSeenRun, &concern.FirstSeenAt,
			&concern.LastSeenRun, &concern.LastSeenAt, &concern.SeenCount,
			&concern.Status, &concern.ResolvedAt, &concern.ResolvedBy,
			&concern.ResolutionNote,
		); err != nil {
			return nil, err
		}
		out = append(out, concern)
	}
	return out, rows.Err()
}

// FormatPriorPreValidationFailures renders concerns for a step prompt. Returns
// "" when there is nothing to say, so the caller can leave the template variable
// empty and the block disappears entirely rather than rendering an empty header.
func FormatPriorPreValidationFailures(concerns []RunConcern) string {
	if len(concerns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("A previous run of this step produced output that failed its own validation schema. ")
	b.WriteString("These are unresolved. Produce output that satisfies them this time; if the schema itself is wrong, say so explicitly in your summary as a CONCERNS: line rather than writing output you know will fail.\n")
	for _, concern := range concerns {
		text := strings.TrimSpace(concern.Text)
		if text == "" {
			continue
		}
		// seen_count is the part that matters most: "failed once" is noise, and
		// "failed on three consecutive runs" means the step has been reading its
		// schema and getting it wrong repeatedly.
		if concern.SeenCount > 1 {
			fmt.Fprintf(&b, "- %s (seen on %d runs)\n", text, concern.SeenCount)
			continue
		}
		fmt.Fprintf(&b, "- %s\n", text)
	}
	return strings.TrimRight(b.String(), "\n")
}
