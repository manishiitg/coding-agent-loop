package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/reportinteraction"
)

const defaultReportWidgetInstanceKey = "default"

var reportWidgetResponseStoreMu sync.Mutex

var (
	errStaleReportWidgetResponse = errors.New("report interaction changed since it was displayed")
	errReportWidgetConflict      = errors.New("report interaction response conflict")
)

type ReportWidgetResponse struct {
	WorkspacePath     string                   `json:"workspace_path"`
	WidgetID          string                   `json:"widget_id"`
	InstanceKey       string                   `json:"instance_key"`
	Question          string                   `json:"question"`
	ResponseKind      string                   `json:"response_kind"`
	Options           []ReportHumanInputOption `json:"options"`
	AllowFreeText     bool                     `json:"allow_free_text"`
	SubjectID         string                   `json:"subject_id,omitempty"`
	SubjectVersion    string                   `json:"subject_version,omitempty"`
	SubjectHash       string                   `json:"subject_hash,omitempty"`
	Status            string                   `json:"status"`
	SelectedOptionID  string                   `json:"selected_option_id,omitempty"`
	Note              string                   `json:"note,omitempty"`
	AnsweredBy        string                   `json:"answered_by,omitempty"`
	ConsumedBy        string                   `json:"consumed_by,omitempty"`
	OutcomeSummary    string                   `json:"outcome_summary,omitempty"`
	ExecutionKey      string                   `json:"execution_key,omitempty"`
	ExecutionRevision int                      `json:"execution_revision,omitempty"`
	ClaimedBy         string                   `json:"claimed_by,omitempty"`
	ClaimStartedAt    string                   `json:"claim_started_at,omitempty"`
	CompletedAt       string                   `json:"completed_at,omitempty"`
	FailedAt          string                   `json:"failed_at,omitempty"`
	FailureSummary    string                   `json:"failure_summary,omitempty"`
	Revision          int                      `json:"revision"`
	AnsweredAt        string                   `json:"answered_at,omitempty"`
	ConsumedAt        string                   `json:"consumed_at,omitempty"`
	CreatedAt         string                   `json:"created_at"`
	UpdatedAt         string                   `json:"updated_at"`
}

type ReportWidgetResponseAnswerRequest struct {
	WorkspacePath          string `json:"workspace_path"`
	InstanceKey            string `json:"instance_key"`
	SelectedOptionID       string `json:"selected_option_id"`
	Note                   string `json:"note"`
	AnsweredBy             string `json:"answered_by"`
	ExpectedSubjectID      string `json:"expected_subject_id"`
	ExpectedSubjectVersion string `json:"expected_subject_version"`
	ExpectedSubjectHash    string `json:"expected_subject_hash"`
}

type ReportWidgetResponseConsumeRequest struct {
	WorkspacePath    string `json:"workspace_path"`
	InstanceKey      string `json:"instance_key"`
	ExpectedRevision int    `json:"expected_revision"`
	ExecutionKey     string `json:"execution_key"`
	OutcomeSummary   string `json:"outcome_summary"`
	ConsumedBy       string `json:"consumed_by"`
}

type ReportWidgetResponseClaimRequest struct {
	WorkspacePath    string `json:"workspace_path"`
	InstanceKey      string `json:"instance_key"`
	ExpectedRevision int    `json:"expected_revision"`
	ExecutionKey     string `json:"execution_key"`
	ClaimedBy        string `json:"claimed_by"`
}

type ReportWidgetResponseFailRequest struct {
	WorkspacePath    string `json:"workspace_path"`
	InstanceKey      string `json:"instance_key"`
	ExpectedRevision int    `json:"expected_revision"`
	ExecutionKey     string `json:"execution_key"`
	FailureSummary   string `json:"failure_summary"`
	FailedBy         string `json:"failed_by"`
}

type configuredReportInteractionWidget struct {
	ID             string                   `json:"id"`
	Kind           string                   `json:"kind"`
	Title          string                   `json:"title"`
	Question       string                   `json:"question"`
	ResponseKind   string                   `json:"responseKind"`
	Options        []ReportHumanInputOption `json:"options"`
	AllowFreeText  bool                     `json:"allowFreeText"`
	InstanceKey    string                   `json:"instanceKey"`
	SubjectID      string                   `json:"subjectId"`
	SubjectVersion string                   `json:"subjectVersion"`
	SubjectHash    string                   `json:"subjectHash"`
}

type configuredReportPlan struct {
	Sections []struct {
		Entries []struct {
			Widget *configuredReportInteractionWidget `json:"widget"`
			Row    *struct {
				Widgets []configuredReportInteractionWidget `json:"widgets"`
			} `json:"row"`
		} `json:"entries"`
	} `json:"sections"`
}

func ensureReportWidgetResponseSchema(ctx context.Context, db *sql.DB) error {
	return reportinteraction.EnsureSchema(ctx, db)
}

func openReportWidgetResponseDB(ctx context.Context, workspacePath string, create bool) (string, *sql.DB, error) {
	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, create)
	if err != nil || db == nil {
		return normalized, db, err
	}
	// Always run the additive migration. Existing workflow databases may have
	// been created by an older app version and can reach claim/complete without
	// first rendering the report in the upgraded app.
	if err := ensureReportWidgetResponseSchema(ctx, db); err != nil {
		_ = db.Close()
		return "", nil, err
	}
	return normalized, db, nil
}

func configuredReportInteractionWidgetPath(workspacePath string) (string, string, error) {
	normalized, err := normalizeReportHumanInputWorkspacePath(workspacePath)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(getWorkspaceDocsAbsPath())
	if err != nil {
		return "", "", err
	}
	planPath, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(normalized), "reports", "report_plan.json"))
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, planPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("workspace_path escapes workspace docs root")
	}
	return normalized, planPath, nil
}

func loadConfiguredReportInteractionWidget(workspacePath, widgetID string) (string, *configuredReportInteractionWidget, error) {
	normalized, planPath, err := configuredReportInteractionWidgetPath(workspacePath)
	if err != nil {
		return "", nil, err
	}
	widgetID = strings.TrimSpace(widgetID)
	if widgetID == "" || len(widgetID) > 128 || strings.ContainsAny(widgetID, "\x00/\\") {
		return "", nil, fmt.Errorf("widget_id is invalid")
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("report plan not found")
		}
		return "", nil, err
	}
	var plan configuredReportPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return "", nil, fmt.Errorf("invalid report plan: %w", err)
	}
	var found *configuredReportInteractionWidget
	for _, section := range plan.Sections {
		for _, entry := range section.Entries {
			if entry.Widget != nil && entry.Widget.ID == widgetID && strings.EqualFold(entry.Widget.Kind, "interaction") {
				copy := *entry.Widget
				found = &copy
				break
			}
			if entry.Row != nil {
				for _, widget := range entry.Row.Widgets {
					if widget.ID == widgetID && strings.EqualFold(widget.Kind, "interaction") {
						copy := widget
						found = &copy
						break
					}
				}
			}
			if found != nil {
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return "", nil, fmt.Errorf("configured interaction widget %q not found", widgetID)
	}
	if err := normalizeConfiguredReportInteractionWidget(found); err != nil {
		return "", nil, err
	}
	return normalized, found, nil
}

func normalizeConfiguredReportInteractionWidget(widget *configuredReportInteractionWidget) error {
	widget.ID = strings.TrimSpace(widget.ID)
	widget.Question = strings.TrimSpace(widget.Question)
	if widget.Question == "" {
		widget.Question = strings.TrimSpace(widget.Title)
	}
	if widget.Question == "" {
		return fmt.Errorf("interaction widget %q requires question", widget.ID)
	}
	widget.ResponseKind = strings.ToLower(strings.TrimSpace(widget.ResponseKind))
	if widget.ResponseKind == "" {
		if len(widget.Options) > 0 {
			widget.ResponseKind = "choice"
		} else {
			widget.ResponseKind = "text"
		}
	}
	switch widget.ResponseKind {
	case "choice":
	case "text":
		widget.AllowFreeText = true
	case "choice-with-text":
		widget.AllowFreeText = true
	default:
		return fmt.Errorf("interaction widget %q has unsupported responseKind %q", widget.ID, widget.ResponseKind)
	}
	options, err := normalizeReportHumanInputOptions(widget.Options)
	if err != nil {
		return fmt.Errorf("interaction widget %q: %w", widget.ID, err)
	}
	widget.Options = options
	if (widget.ResponseKind == "choice" || widget.ResponseKind == "choice-with-text") && len(widget.Options) == 0 {
		return fmt.Errorf("interaction widget %q requires options for responseKind %q", widget.ID, widget.ResponseKind)
	}
	widget.InstanceKey = strings.TrimSpace(widget.InstanceKey)
	if widget.InstanceKey == "" {
		widget.InstanceKey = defaultReportWidgetInstanceKey
	}
	if len(widget.InstanceKey) > 160 || strings.Contains(widget.InstanceKey, "\x00") {
		return fmt.Errorf("interaction widget %q has invalid instanceKey", widget.ID)
	}
	widget.SubjectID = strings.TrimSpace(widget.SubjectID)
	widget.SubjectVersion = strings.TrimSpace(widget.SubjectVersion)
	widget.SubjectHash = strings.TrimSpace(widget.SubjectHash)
	return nil
}

func reportWidgetSubjectMatches(widget *configuredReportInteractionWidget, subjectID, subjectVersion, subjectHash string) bool {
	return widget.SubjectID == strings.TrimSpace(subjectID) &&
		widget.SubjectVersion == strings.TrimSpace(subjectVersion) &&
		widget.SubjectHash == strings.TrimSpace(subjectHash)
}

func ensureConfiguredReportWidgetResponse(ctx context.Context, db *sql.DB, workspacePath string, widget *configuredReportInteractionWidget) error {
	now := time.Now().UTC().Format(time.RFC3339)
	optionsJSON, _ := json.Marshal(widget.Options)
	_, err := db.ExecContext(ctx, `INSERT INTO report_widget_responses
		(workspace_path, widget_id, instance_key, question, response_kind, options_json, allow_free_text,
		 subject_id, subject_version, subject_hash, status, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?)
		ON CONFLICT(workspace_path, widget_id, instance_key) DO NOTHING`,
		workspacePath, widget.ID, widget.InstanceKey, widget.Question, widget.ResponseKind, string(optionsJSON), boolToInt(widget.AllowFreeText),
		widget.SubjectID, widget.SubjectVersion, widget.SubjectHash, now, now)
	return err
}

func ensureStoredReportWidgetSubjectMatches(response *ReportWidgetResponse, widget *configuredReportInteractionWidget) error {
	if response == nil || reportWidgetSubjectMatches(widget, response.SubjectID, response.SubjectVersion, response.SubjectHash) {
		return nil
	}
	return fmt.Errorf("%w: widget %q reused instanceKey %q for a different subject; configure a new instanceKey to preserve the prior decision",
		errReportWidgetConflict, widget.ID, widget.InstanceKey)
}

func answerReportWidgetResponse(ctx context.Context, workspacePath, widgetID string, req ReportWidgetResponseAnswerRequest) (*ReportWidgetResponse, error) {
	reportWidgetResponseStoreMu.Lock()
	defer reportWidgetResponseStoreMu.Unlock()

	normalized, widget, err := loadConfiguredReportInteractionWidget(workspacePath, widgetID)
	if err != nil {
		return nil, err
	}
	if requested := strings.TrimSpace(req.InstanceKey); requested != "" && requested != widget.InstanceKey {
		return nil, fmt.Errorf("instance_key %q does not match configured widget instance", requested)
	}
	if !reportWidgetSubjectMatches(widget, req.ExpectedSubjectID, req.ExpectedSubjectVersion, req.ExpectedSubjectHash) {
		return nil, fmt.Errorf("%w: refresh the report before answering widget %q", errStaleReportWidgetResponse, widget.ID)
	}
	selected := strings.TrimSpace(req.SelectedOptionID)
	note := strings.TrimSpace(req.Note)
	if selected != "" && !reportHumanInputOptionExists(widget.Options, selected) {
		return nil, fmt.Errorf("selected_option_id %q is not allowed for widget %q", selected, widget.ID)
	}
	switch widget.ResponseKind {
	case "text":
		selected = ""
		if note == "" {
			return nil, fmt.Errorf("note is required")
		}
	case "choice":
		if selected == "" {
			if !widget.AllowFreeText || note == "" {
				return nil, fmt.Errorf("selected_option_id is required")
			}
		}
		if !widget.AllowFreeText {
			note = ""
		}
	case "choice-with-text":
		if selected == "" && note == "" {
			return nil, fmt.Errorf("select an option or provide a note")
		}
	}

	_, db, err := openReportWidgetResponseDB(ctx, normalized, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureConfiguredReportWidgetResponse(ctx, db, normalized, widget); err != nil {
		return nil, err
	}
	existing, err := getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, widget.InstanceKey)
	if err != nil {
		return nil, err
	}
	if err := ensureStoredReportWidgetSubjectMatches(existing, widget); err != nil {
		return nil, err
	}
	if existing != nil && existing.Status == "executing" {
		return nil, fmt.Errorf("%w: response revision %d is currently executing", errReportWidgetConflict, existing.Revision)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	optionsJSON, _ := json.Marshal(widget.Options)
	_, err = db.ExecContext(ctx, `INSERT INTO report_widget_responses
		(workspace_path, widget_id, instance_key, question, response_kind, options_json, allow_free_text,
		 subject_id, subject_version, subject_hash, status, selected_option_id, note, answered_by,
		 revision, answered_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'answered', ?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT(workspace_path, widget_id, instance_key) DO UPDATE SET
		 question=excluded.question,
		 response_kind=excluded.response_kind,
		 options_json=excluded.options_json,
		 allow_free_text=excluded.allow_free_text,
		 subject_id=excluded.subject_id,
		 subject_version=excluded.subject_version,
		 subject_hash=excluded.subject_hash,
		 status='answered',
		 selected_option_id=excluded.selected_option_id,
		 note=excluded.note,
		 answered_by=excluded.answered_by,
		 consumed_by='',
		 outcome_summary='',
		 execution_key='',
		 execution_revision=0,
		 claimed_by='',
		 claim_started_at='',
		 completed_at='',
		 failed_at='',
		 failure_summary='',
		 consumed_at='',
		 revision=report_widget_responses.revision + 1,
		 answered_at=excluded.answered_at,
		 updated_at=excluded.updated_at`,
		normalized, widget.ID, widget.InstanceKey, widget.Question, widget.ResponseKind, string(optionsJSON), boolToInt(widget.AllowFreeText),
		widget.SubjectID, widget.SubjectVersion, widget.SubjectHash, selected, note, strings.TrimSpace(req.AnsweredBy), now, now, now)
	if err != nil {
		return nil, err
	}
	return getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, widget.InstanceKey)
}

func listReportWidgetResponses(ctx context.Context, workspacePath, widgetID, instanceKey, status string) ([]ReportWidgetResponse, error) {
	reportWidgetResponseStoreMu.Lock()
	defer reportWidgetResponseStoreMu.Unlock()

	var configuredWidget *configuredReportInteractionWidget
	if strings.TrimSpace(widgetID) != "" {
		normalized, widget, err := loadConfiguredReportInteractionWidget(workspacePath, widgetID)
		if err != nil {
			return nil, err
		}
		workspacePath = normalized
		if requested := strings.TrimSpace(instanceKey); requested != "" && requested != widget.InstanceKey {
			return nil, fmt.Errorf("instance_key %q does not match configured widget instance", requested)
		}
		if strings.TrimSpace(instanceKey) == "" {
			instanceKey = widget.InstanceKey
		}
		configuredWidget = widget
	}

	// Rendering a configured interaction instantiates its framework-owned store.
	// This makes the table available to later workflow steps even before the user
	// has answered, so "no answer" is an empty result rather than a missing table.
	normalized, db, err := openReportWidgetResponseDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return []ReportWidgetResponse{}, nil
	}
	defer db.Close()
	if configuredWidget != nil {
		if err := ensureConfiguredReportWidgetResponse(ctx, db, normalized, configuredWidget); err != nil {
			return nil, err
		}
		response, err := getReportWidgetResponseByKey(ctx, db, normalized, configuredWidget.ID, configuredWidget.InstanceKey)
		if err != nil {
			return nil, err
		}
		if err := ensureStoredReportWidgetSubjectMatches(response, configuredWidget); err != nil {
			return nil, err
		}
	}
	clauses := []string{"workspace_path = ?"}
	args := []interface{}{normalized}
	if value := strings.TrimSpace(widgetID); value != "" {
		clauses = append(clauses, "widget_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(instanceKey); value != "" {
		clauses = append(clauses, "instance_key = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	query := `SELECT workspace_path, widget_id, instance_key, question, response_kind, options_json,
		allow_free_text, subject_id, subject_version, subject_hash, status, selected_option_id, note,
		answered_by, consumed_by, outcome_summary, execution_key, execution_revision, claimed_by,
		claim_started_at, completed_at, failed_at, failure_summary, revision, answered_at, consumed_at, created_at, updated_at
		FROM report_widget_responses WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY datetime(updated_at) DESC, widget_id, instance_key`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if isReportWidgetResponsesMissingTable(err) {
			return []ReportWidgetResponse{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	responses := []ReportWidgetResponse{}
	for rows.Next() {
		response, err := scanReportWidgetResponse(rows)
		if err != nil {
			return nil, err
		}
		responses = append(responses, *response)
	}
	return responses, rows.Err()
}

func claimReportWidgetResponse(ctx context.Context, workspacePath, widgetID string, req ReportWidgetResponseClaimRequest) (*ReportWidgetResponse, error) {
	reportWidgetResponseStoreMu.Lock()
	defer reportWidgetResponseStoreMu.Unlock()

	normalized, widget, err := loadConfiguredReportInteractionWidget(workspacePath, widgetID)
	if err != nil {
		return nil, err
	}
	instanceKey := strings.TrimSpace(req.InstanceKey)
	if instanceKey == "" {
		instanceKey = widget.InstanceKey
	}
	if instanceKey != widget.InstanceKey {
		return nil, fmt.Errorf("instance_key %q does not match configured widget instance", instanceKey)
	}
	executionKey := strings.TrimSpace(req.ExecutionKey)
	if executionKey == "" {
		return nil, fmt.Errorf("execution_key is required")
	}
	if req.ExpectedRevision <= 0 {
		return nil, fmt.Errorf("expected_revision is required")
	}
	_, db, err := openReportWidgetResponseDB(ctx, normalized, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("response not found")
	}
	defer db.Close()
	response, err := getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, instanceKey)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("response not found")
	}
	if err := ensureStoredReportWidgetSubjectMatches(response, widget); err != nil {
		return nil, err
	}
	if response.Revision != req.ExpectedRevision {
		return nil, fmt.Errorf("%w: expected revision %d, current revision is %d", errReportWidgetConflict, req.ExpectedRevision, response.Revision)
	}
	if (response.Status == "executing" || response.Status == "completed") && response.ExecutionKey == executionKey {
		return response, nil
	}
	if response.Status != "answered" {
		return nil, fmt.Errorf("%w: response must be answered before it can be claimed; current status=%q", errReportWidgetConflict, response.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx, `UPDATE report_widget_responses
		SET status='executing', execution_key=?, execution_revision=?, claimed_by=?, claim_started_at=?,
		    completed_at='', failed_at='', failure_summary='', updated_at=?
		WHERE workspace_path=? AND widget_id=? AND instance_key=? AND status='answered' AND revision=?`,
		executionKey, response.Revision, strings.TrimSpace(req.ClaimedBy), now, now,
		normalized, widget.ID, instanceKey, response.Revision)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, fmt.Errorf("%w: response was claimed by another execution", errReportWidgetConflict)
	}
	return getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, instanceKey)
}

func consumeReportWidgetResponse(ctx context.Context, workspacePath, widgetID string, req ReportWidgetResponseConsumeRequest) (*ReportWidgetResponse, error) {
	reportWidgetResponseStoreMu.Lock()
	defer reportWidgetResponseStoreMu.Unlock()

	normalized, widget, err := loadConfiguredReportInteractionWidget(workspacePath, widgetID)
	if err != nil {
		return nil, err
	}
	instanceKey := strings.TrimSpace(req.InstanceKey)
	if instanceKey == "" {
		instanceKey = widget.InstanceKey
	}
	if instanceKey != widget.InstanceKey {
		return nil, fmt.Errorf("instance_key %q does not match configured widget instance", instanceKey)
	}
	executionKey := strings.TrimSpace(req.ExecutionKey)
	if executionKey == "" {
		return nil, fmt.Errorf("execution_key is required")
	}
	if req.ExpectedRevision <= 0 {
		return nil, fmt.Errorf("expected_revision is required")
	}
	_, db, err := openReportWidgetResponseDB(ctx, normalized, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("response not found")
	}
	defer db.Close()
	response, err := getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, instanceKey)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("response not found")
	}
	if err := ensureStoredReportWidgetSubjectMatches(response, widget); err != nil {
		return nil, err
	}
	if response.Revision != req.ExpectedRevision {
		return nil, fmt.Errorf("%w: expected revision %d, current revision is %d", errReportWidgetConflict, req.ExpectedRevision, response.Revision)
	}
	if response.Status == "completed" && response.ExecutionKey == executionKey {
		return response, nil
	}
	if response.Status != "executing" || response.ExecutionKey != executionKey {
		return nil, fmt.Errorf("%w: only the execution that claimed this response can complete it", errReportWidgetConflict)
	}
	outcome := strings.TrimSpace(req.OutcomeSummary)
	if outcome == "" {
		return nil, fmt.Errorf("outcome_summary is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx, `UPDATE report_widget_responses
		SET status='completed', consumed_by=?, outcome_summary=?, consumed_at=?, completed_at=?, updated_at=?
		WHERE workspace_path=? AND widget_id=? AND instance_key=? AND status='executing'
		  AND revision=? AND execution_key=?`,
		strings.TrimSpace(req.ConsumedBy), outcome, now, now, now, normalized, widget.ID, instanceKey,
		response.Revision, executionKey)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, fmt.Errorf("%w: response completion lost its claim", errReportWidgetConflict)
	}
	return getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, instanceKey)
}

func failReportWidgetResponse(ctx context.Context, workspacePath, widgetID string, req ReportWidgetResponseFailRequest) (*ReportWidgetResponse, error) {
	reportWidgetResponseStoreMu.Lock()
	defer reportWidgetResponseStoreMu.Unlock()

	normalized, widget, err := loadConfiguredReportInteractionWidget(workspacePath, widgetID)
	if err != nil {
		return nil, err
	}
	instanceKey := strings.TrimSpace(req.InstanceKey)
	if instanceKey == "" {
		instanceKey = widget.InstanceKey
	}
	executionKey := strings.TrimSpace(req.ExecutionKey)
	failure := strings.TrimSpace(req.FailureSummary)
	if instanceKey != widget.InstanceKey || executionKey == "" || req.ExpectedRevision <= 0 || failure == "" {
		return nil, fmt.Errorf("instance_key, expected_revision, execution_key, and failure_summary are required")
	}
	_, db, err := openReportWidgetResponseDB(ctx, normalized, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("response not found")
	}
	defer db.Close()
	response, err := getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, instanceKey)
	if err != nil || response == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("response not found")
	}
	if err := ensureStoredReportWidgetSubjectMatches(response, widget); err != nil {
		return nil, err
	}
	if response.Status == "failed" && response.ExecutionKey == executionKey {
		return response, nil
	}
	if response.Status != "executing" || response.ExecutionKey != executionKey || response.Revision != req.ExpectedRevision {
		return nil, fmt.Errorf("%w: only the execution that claimed this revision can fail it", errReportWidgetConflict)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx, `UPDATE report_widget_responses
		SET status='failed', failure_summary=?, failed_at=?, updated_at=?
		WHERE workspace_path=? AND widget_id=? AND instance_key=? AND status='executing'
		  AND revision=? AND execution_key=?`,
		failure, now, now, normalized, widget.ID, instanceKey, response.Revision, executionKey)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, fmt.Errorf("%w: response failure update lost its claim", errReportWidgetConflict)
	}
	return getReportWidgetResponseByKey(ctx, db, normalized, widget.ID, instanceKey)
}

func getReportWidgetResponseByKey(ctx context.Context, db *sql.DB, workspacePath, widgetID, instanceKey string) (*ReportWidgetResponse, error) {
	row := db.QueryRowContext(ctx, `SELECT workspace_path, widget_id, instance_key, question, response_kind, options_json,
		allow_free_text, subject_id, subject_version, subject_hash, status, selected_option_id, note,
		answered_by, consumed_by, outcome_summary, execution_key, execution_revision, claimed_by,
		claim_started_at, completed_at, failed_at, failure_summary, revision, answered_at, consumed_at, created_at, updated_at
		FROM report_widget_responses WHERE workspace_path=? AND widget_id=? AND instance_key=?`,
		workspacePath, widgetID, instanceKey)
	response, err := scanReportWidgetResponse(row)
	if errors.Is(err, sql.ErrNoRows) || isReportWidgetResponsesMissingTable(err) {
		return nil, nil
	}
	return response, err
}

type reportWidgetResponseScanner interface {
	Scan(dest ...interface{}) error
}

func scanReportWidgetResponse(row reportWidgetResponseScanner) (*ReportWidgetResponse, error) {
	var response ReportWidgetResponse
	var optionsJSON string
	var allowFreeText int
	if err := row.Scan(
		&response.WorkspacePath, &response.WidgetID, &response.InstanceKey, &response.Question,
		&response.ResponseKind, &optionsJSON, &allowFreeText, &response.SubjectID, &response.SubjectVersion,
		&response.SubjectHash, &response.Status, &response.SelectedOptionID, &response.Note, &response.AnsweredBy,
		&response.ConsumedBy, &response.OutcomeSummary, &response.ExecutionKey, &response.ExecutionRevision,
		&response.ClaimedBy, &response.ClaimStartedAt, &response.CompletedAt, &response.FailedAt,
		&response.FailureSummary, &response.Revision, &response.AnsweredAt,
		&response.ConsumedAt, &response.CreatedAt, &response.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(optionsJSON), &response.Options)
	if response.Options == nil {
		response.Options = []ReportHumanInputOption{}
	}
	response.AllowFreeText = allowFreeText != 0
	return &response, nil
}

func (api *StreamingAPI) handleListReportWidgetResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	responses, err := listReportWidgetResponses(
		r.Context(),
		r.URL.Query().Get("workspace_path"),
		r.URL.Query().Get("widget_id"),
		r.URL.Query().Get("instance_key"),
		r.URL.Query().Get("status"),
	)
	if err != nil {
		writeReportWidgetResponseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "responses": responses})
}

func (api *StreamingAPI) handleAnswerReportWidgetResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportWidgetResponseAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.AnsweredBy == "" {
		req.AnsweredBy = GetUserIDFromContext(r.Context())
	}
	response, err := answerReportWidgetResponse(r.Context(), req.WorkspacePath, mux.Vars(r)["widget_id"], req)
	if err != nil {
		writeReportWidgetResponseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "response": response})
}

func (api *StreamingAPI) handleClaimReportWidgetResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportWidgetResponseClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.ClaimedBy == "" {
		req.ClaimedBy = GetUserIDFromContext(r.Context())
	}
	response, err := claimReportWidgetResponse(r.Context(), req.WorkspacePath, mux.Vars(r)["widget_id"], req)
	if err != nil {
		writeReportWidgetResponseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "response": response})
}

func (api *StreamingAPI) handleConsumeReportWidgetResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportWidgetResponseConsumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.ConsumedBy == "" {
		req.ConsumedBy = GetUserIDFromContext(r.Context())
	}
	response, err := consumeReportWidgetResponse(r.Context(), req.WorkspacePath, mux.Vars(r)["widget_id"], req)
	if err != nil {
		writeReportWidgetResponseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "response": response})
}

func (api *StreamingAPI) handleFailReportWidgetResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportWidgetResponseFailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.FailedBy == "" {
		req.FailedBy = GetUserIDFromContext(r.Context())
	}
	response, err := failReportWidgetResponse(r.Context(), req.WorkspacePath, mux.Vars(r)["widget_id"], req)
	if err != nil {
		writeReportWidgetResponseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "response": response})
}

func writeReportWidgetResponseError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, errStaleReportWidgetResponse) || errors.Is(err, errReportWidgetConflict) {
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}

func isReportWidgetResponsesMissingTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table: report_widget_responses")
}
