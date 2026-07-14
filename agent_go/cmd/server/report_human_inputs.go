package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	_ "modernc.org/sqlite"
)

var reportHumanInputStoreMu sync.Mutex

type ReportHumanInputOption struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ReportHumanInput struct {
	ID               string                   `json:"id"`
	WorkspacePath    string                   `json:"workspace_path"`
	Source           string                   `json:"source"`
	Priority         string                   `json:"priority"`
	Question         string                   `json:"question"`
	Context          string                   `json:"context,omitempty"`
	Options          []ReportHumanInputOption `json:"options"`
	AllowFreeText    bool                     `json:"allow_free_text"`
	Status           string                   `json:"status"`
	SelectedOptionID string                   `json:"selected_option_id,omitempty"`
	Note             string                   `json:"note,omitempty"`
	RunID            string                   `json:"run_id,omitempty"`
	Evidence         string                   `json:"evidence,omitempty"`
	CreatedBy        string                   `json:"created_by,omitempty"`
	AnsweredBy       string                   `json:"answered_by,omitempty"`
	ConsumedBy       string                   `json:"consumed_by,omitempty"`
	OutcomeSummary   string                   `json:"outcome_summary,omitempty"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
	AnsweredAt       string                   `json:"answered_at,omitempty"`
	ConsumedAt       string                   `json:"consumed_at,omitempty"`
	DismissedAt      string                   `json:"dismissed_at,omitempty"`
}

type ReportHumanInputCreateRequest struct {
	WorkspacePath string                   `json:"workspace_path"`
	InputID       string                   `json:"input_id"`
	Source        string                   `json:"source"`
	Priority      string                   `json:"priority"`
	Question      string                   `json:"question"`
	Context       string                   `json:"context"`
	Options       []ReportHumanInputOption `json:"options"`
	AllowFreeText bool                     `json:"allow_free_text"`
	RunID         string                   `json:"run_id"`
	Evidence      string                   `json:"evidence"`
	CreatedBy     string                   `json:"created_by"`
}

type ReportHumanInputAnswerRequest struct {
	WorkspacePath    string `json:"workspace_path"`
	SelectedOptionID string `json:"selected_option_id"`
	Note             string `json:"note"`
	AnsweredBy       string `json:"answered_by"`
}

type ReportHumanInputConsumeRequest struct {
	WorkspacePath  string `json:"workspace_path"`
	OutcomeSummary string `json:"outcome_summary"`
	ConsumedBy     string `json:"consumed_by"`
}

func normalizeReportHumanInputWorkspacePath(workspacePath string) (string, error) {
	cleaned := filepath.ToSlash(filepath.Clean(strings.Trim(strings.TrimSpace(workspacePath), "/")))
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("workspace_path is required")
	}
	if strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "\x00") {
		return "", fmt.Errorf("workspace_path must be workspace-relative")
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return "", fmt.Errorf("workspace_path cannot contain ..")
		}
	}
	return cleaned, nil
}

func reportHumanInputDBPath(workspacePath string) (string, string, error) {
	normalized, err := normalizeReportHumanInputWorkspacePath(workspacePath)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(getWorkspaceDocsAbsPath())
	if err != nil {
		return "", "", err
	}
	dbPath, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(normalized), "db", "db.sqlite"))
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, dbPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("workspace_path escapes workspace docs root")
	}
	return normalized, dbPath, nil
}

func openReportHumanInputDB(ctx context.Context, workspacePath string, create bool) (string, *sql.DB, error) {
	normalized, dbPath, err := reportHumanInputDBPath(workspacePath)
	if err != nil {
		return "", nil, err
	}
	if create {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return "", nil, err
		}
	} else if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return normalized, nil, nil
		}
		return "", nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return "", nil, err
	}
	if create {
		if err := ensureReportHumanInputSchema(ctx, db); err != nil {
			_ = db.Close()
			return "", nil, err
		}
	}
	return normalized, db, nil
}

func ensureReportHumanInputSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS report_human_inputs (
			id TEXT PRIMARY KEY,
			workspace_path TEXT NOT NULL,
			source TEXT NOT NULL,
			priority TEXT NOT NULL DEFAULT 'medium',
			question TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '',
			options_json TEXT NOT NULL DEFAULT '[]',
			allow_free_text INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			selected_option_id TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			run_id TEXT NOT NULL DEFAULT '',
			evidence TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			answered_by TEXT NOT NULL DEFAULT '',
			consumed_by TEXT NOT NULL DEFAULT '',
			outcome_summary TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			answered_at TEXT NOT NULL DEFAULT '',
			consumed_at TEXT NOT NULL DEFAULT '',
			dismissed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_report_human_inputs_status ON report_human_inputs(status, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_report_human_inputs_source ON report_human_inputs(source, status, updated_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func createReportHumanInput(ctx context.Context, workspacePath string, req ReportHumanInputCreateRequest) (*ReportHumanInput, error) {
	reportHumanInputStoreMu.Lock()
	defer reportHumanInputStoreMu.Unlock()

	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	question := strings.TrimSpace(req.Question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	source := normalizeReportHumanInputSource(req.Source)
	priority := normalizeReportHumanInputPriority(req.Priority)
	options, err := normalizeReportHumanInputOptions(req.Options)
	if err != nil {
		return nil, err
	}
	allowFreeText := req.AllowFreeText || len(options) == 0
	id := strings.TrimSpace(req.InputID)
	if id == "" {
		id = newReportHumanInputID(question)
	}
	id = normalizeReportHumanInputID(id)
	if id == "" {
		return nil, fmt.Errorf("input_id is invalid")
	}

	existing, err := getReportHumanInputByID(ctx, db, normalized, id)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status != "pending" {
		return nil, fmt.Errorf("input_id %q already exists with status %q", id, existing.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	optionsJSON, _ := json.Marshal(options)
	input := &ReportHumanInput{
		ID:            id,
		WorkspacePath: normalized,
		Source:        source,
		Priority:      priority,
		Question:      question,
		Context:       strings.TrimSpace(req.Context),
		AllowFreeText: allowFreeText,
		RunID:         strings.TrimSpace(req.RunID),
		Evidence:      strings.TrimSpace(req.Evidence),
		CreatedBy:     strings.TrimSpace(req.CreatedBy),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if existing == nil {
		_, err = db.ExecContext(ctx, `INSERT INTO report_human_inputs
			(id, workspace_path, source, priority, question, context, options_json, allow_free_text, status, run_id, evidence, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?)`,
			input.ID, input.WorkspacePath, input.Source, input.Priority, input.Question, input.Context, string(optionsJSON), boolToInt(input.AllowFreeText),
			input.RunID, input.Evidence, input.CreatedBy, input.CreatedAt, input.UpdatedAt)
	} else {
		_, err = db.ExecContext(ctx, `UPDATE report_human_inputs
			SET source=?, priority=?, question=?, context=?, options_json=?, allow_free_text=?, run_id=?, evidence=?, created_by=?, updated_at=?
			WHERE id=? AND workspace_path=? AND status='pending'`,
			input.Source, input.Priority, input.Question, input.Context, string(optionsJSON), boolToInt(input.AllowFreeText),
			input.RunID, input.Evidence, input.CreatedBy, input.UpdatedAt, input.ID, input.WorkspacePath)
		input.CreatedAt = existing.CreatedAt
	}
	if err != nil {
		return nil, err
	}
	return getReportHumanInputByID(ctx, db, normalized, id)
}

func listReportHumanInputs(ctx context.Context, workspacePath, status, source string) ([]ReportHumanInput, error) {
	reportHumanInputStoreMu.Lock()
	defer reportHumanInputStoreMu.Unlock()

	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return []ReportHumanInput{}, nil
	}
	defer db.Close()

	clauses := []string{"workspace_path = ?"}
	args := []interface{}{normalized}
	if s := strings.TrimSpace(status); s != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, s)
	}
	if s := strings.TrimSpace(source); s != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, normalizeReportHumanInputSource(s))
	}
	query := `SELECT id, workspace_path, source, priority, question, context, options_json, allow_free_text, status,
		selected_option_id, note, run_id, evidence, created_by, answered_by, consumed_by, outcome_summary,
		created_at, updated_at, answered_at, consumed_at, dismissed_at
		FROM report_human_inputs WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY CASE status WHEN 'pending' THEN 0 WHEN 'answered' THEN 1 WHEN 'dismissed' THEN 2 ELSE 3 END,
			datetime(updated_at) DESC, id DESC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if isReportHumanInputsMissingTable(err) {
			return []ReportHumanInput{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var inputs []ReportHumanInput
	for rows.Next() {
		input, err := scanReportHumanInput(rows)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, *input)
	}
	return inputs, rows.Err()
}

func answerReportHumanInput(ctx context.Context, workspacePath, inputID string, req ReportHumanInputAnswerRequest) (*ReportHumanInput, error) {
	reportHumanInputStoreMu.Lock()
	defer reportHumanInputStoreMu.Unlock()

	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("input_id %q not found", inputID)
	}
	defer db.Close()

	input, err := getReportHumanInputByID(ctx, db, normalized, inputID)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("input_id %q not found", inputID)
	}
	if input.Status == "consumed" || input.Status == "dismissed" {
		return nil, fmt.Errorf("input_id %q is %s", inputID, input.Status)
	}

	selected := strings.TrimSpace(req.SelectedOptionID)
	note := strings.TrimSpace(req.Note)
	if selected != "" && !reportHumanInputOptionExists(input.Options, selected) {
		return nil, fmt.Errorf("selected_option_id %q is not valid for input_id %q", selected, inputID)
	}
	if !input.AllowFreeText && len(input.Options) > 0 {
		note = ""
	}
	if selected == "" && note == "" {
		if len(input.Options) > 0 {
			if input.AllowFreeText {
				return nil, fmt.Errorf("select an option or provide a note")
			}
			return nil, fmt.Errorf("selected_option_id is required")
		}
		return nil, fmt.Errorf("note is required for free-text input")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `UPDATE report_human_inputs
		SET status='answered', selected_option_id=?, note=?, answered_by=?, answered_at=?, updated_at=?
		WHERE id=? AND workspace_path=?`,
		selected, note, strings.TrimSpace(req.AnsweredBy), now, now, input.ID, normalized)
	if err != nil {
		return nil, err
	}
	return getReportHumanInputByID(ctx, db, normalized, input.ID)
}

func dismissReportHumanInput(ctx context.Context, workspacePath, inputID, answeredBy string) (*ReportHumanInput, error) {
	reportHumanInputStoreMu.Lock()
	defer reportHumanInputStoreMu.Unlock()

	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("input_id %q not found", inputID)
	}
	defer db.Close()

	input, err := getReportHumanInputByID(ctx, db, normalized, inputID)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("input_id %q not found", inputID)
	}
	if input.Status == "consumed" {
		return nil, fmt.Errorf("input_id %q is already consumed", inputID)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `UPDATE report_human_inputs
		SET status='dismissed', answered_by=?, dismissed_at=?, updated_at=?
		WHERE id=? AND workspace_path=?`,
		strings.TrimSpace(answeredBy), now, now, input.ID, normalized)
	if err != nil {
		return nil, err
	}
	return getReportHumanInputByID(ctx, db, normalized, input.ID)
}

func consumeReportHumanInput(ctx context.Context, workspacePath, inputID string, req ReportHumanInputConsumeRequest) (*ReportHumanInput, error) {
	reportHumanInputStoreMu.Lock()
	defer reportHumanInputStoreMu.Unlock()

	normalized, db, err := openReportHumanInputDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("input_id %q not found", inputID)
	}
	defer db.Close()

	input, err := getReportHumanInputByID(ctx, db, normalized, inputID)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("input_id %q not found", inputID)
	}
	if input.Status != "answered" {
		return nil, fmt.Errorf("input_id %q must be answered before it can be consumed; current status=%q", inputID, input.Status)
	}
	outcome := strings.TrimSpace(req.OutcomeSummary)
	if outcome == "" {
		return nil, fmt.Errorf("outcome_summary is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `UPDATE report_human_inputs
		SET status='consumed', consumed_by=?, outcome_summary=?, consumed_at=?, updated_at=?
		WHERE id=? AND workspace_path=?`,
		strings.TrimSpace(req.ConsumedBy), outcome, now, now, input.ID, normalized)
	if err != nil {
		return nil, err
	}
	return getReportHumanInputByID(ctx, db, normalized, input.ID)
}

func getReportHumanInputByID(ctx context.Context, db *sql.DB, workspacePath, inputID string) (*ReportHumanInput, error) {
	row := db.QueryRowContext(ctx, `SELECT id, workspace_path, source, priority, question, context, options_json, allow_free_text, status,
		selected_option_id, note, run_id, evidence, created_by, answered_by, consumed_by, outcome_summary,
		created_at, updated_at, answered_at, consumed_at, dismissed_at
		FROM report_human_inputs WHERE workspace_path=? AND id=?`, workspacePath, strings.TrimSpace(inputID))
	input, err := scanReportHumanInput(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if isReportHumanInputsMissingTable(err) {
		return nil, nil
	}
	return input, err
}

type reportHumanInputScanner interface {
	Scan(dest ...interface{}) error
}

func scanReportHumanInput(row reportHumanInputScanner) (*ReportHumanInput, error) {
	var input ReportHumanInput
	var optionsJSON string
	var allowFreeText int
	if err := row.Scan(
		&input.ID, &input.WorkspacePath, &input.Source, &input.Priority, &input.Question, &input.Context,
		&optionsJSON, &allowFreeText, &input.Status, &input.SelectedOptionID, &input.Note, &input.RunID,
		&input.Evidence, &input.CreatedBy, &input.AnsweredBy, &input.ConsumedBy, &input.OutcomeSummary,
		&input.CreatedAt, &input.UpdatedAt, &input.AnsweredAt, &input.ConsumedAt, &input.DismissedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(optionsJSON), &input.Options)
	if input.Options == nil {
		input.Options = []ReportHumanInputOption{}
	}
	input.AllowFreeText = allowFreeText != 0
	return &input, nil
}

func createReportHumanInputTools() ([]llmtypes.Tool, map[string]interface{}, map[string]string) {
	createTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_human_input_request",
			Description: "Create or refresh a structured non-blocking question for the user. Pulse and Goal Advisor store workflow questions in that workflow's db/db.sqlite; Chief of Staff may use workspace_path=\"pulse\" for an org-wide question, stored in pulse/db/db.sqlite, or a Workflow/<name> path for a workflow-specific question. The user answers inside Runloop's Pulse/report panel; published static reports should only show the question and tell the user to open Runloop to answer. For Goal Advisor plan-change proposals, use source=\"goal_advisor\", a stable input_id prefixed with \"plan-proposal-\", options approve/reject/defer, and put the exact proposed plan changes, rationale, expected impact, risk, and evidence in context so a later Pulse pass can apply an approved proposal with normal plan tools.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{"type": "string", "description": "Workflow-relative path, for example Workflow/social-media. Chief of Staff may use pulse for an org-wide question. Required; requests are stored in that scope's db/db.sqlite."},
					"input_id":       map[string]interface{}{"type": "string", "description": "Optional stable id. Reuse this for the same still-open question so Pulse refreshes it instead of duplicating it."},
					"source":         map[string]interface{}{"type": "string", "enum": []string{"pulse", "goal_advisor", "chief_of_staff"}, "description": "Who is asking. Defaults to pulse."},
					"priority":       map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}, "description": "How important the answer is. Defaults to medium."},
					"question":       map[string]interface{}{"type": "string", "description": "The exact user-facing question in simple language."},
					"context":        map[string]interface{}{"type": "string", "description": "Short explanation of why this matters and what will happen next. For plan-change proposals, use newline-separated labeled sections exactly like: Proposal:\n...\nExact intended edits if approved:\n(1) ...\n(2) ...\nRationale:\n...\nExpected impact:\n...\nRisk:\n... . Keep evidence paths in the separate evidence field."},
					"options": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":          map[string]interface{}{"type": "string", "description": "Stable machine id, e.g. approve, wait, use_email."},
								"title":       map[string]interface{}{"type": "string", "description": "Short option label shown to the user."},
								"description": map[string]interface{}{"type": "string", "description": "One short sentence explaining the tradeoff."},
							},
							"required": []string{"id", "title"},
						},
						"description": "Optional choice list. Each option needs an id, title, and ideally a short description.",
					},
					"allow_free_text": map[string]interface{}{"type": "boolean", "description": "Allow the user to write a custom answer instead of selecting an option, or add a note alongside an option. If no options are provided, free text is automatically allowed."},
					"run_id":          map[string]interface{}{"type": "string", "description": "Optional schedule/run id connected to the request."},
					"evidence":        map[string]interface{}{"type": "string", "description": "Evidence paths/ids that justify the question."},
				},
				"required": []string{"workspace_path", "question"},
			}),
		},
	}
	consumeTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "mark_human_input_consumed",
			Description: "Mark an answered report human input as consumed after Pulse/Goal Advisor/Chief of Staff has used the answer and recorded the outcome. This keeps history but removes it from the pending-for-agent queue.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path":  map[string]interface{}{"type": "string", "description": "Workflow-relative path, for example Workflow/social-media."},
					"input_id":        map[string]interface{}{"type": "string", "description": "The input id returned by create_human_input_request."},
					"outcome_summary": map[string]interface{}{"type": "string", "description": "What you did with the answer, in one sentence."},
					"consumed_by":     map[string]interface{}{"type": "string", "description": "Optional actor label. Defaults to agent."},
				},
				"required": []string{"workspace_path", "input_id", "outcome_summary"},
			}),
		},
	}
	executors := map[string]interface{}{
		"create_human_input_request": func(ctx context.Context, args map[string]interface{}) (string, error) {
			req, err := reportHumanInputCreateRequestFromToolArgs(args)
			if err != nil {
				return "", err
			}
			if req.CreatedBy == "" {
				req.CreatedBy = "agent"
			}
			input, err := createReportHumanInput(ctx, req.WorkspacePath, req)
			if err != nil {
				return "", err
			}
			return marshalReportHumanInputToolResult("created", input)
		},
		"mark_human_input_consumed": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workspacePath, _ := args["workspace_path"].(string)
			inputID, _ := args["input_id"].(string)
			req := ReportHumanInputConsumeRequest{}
			req.OutcomeSummary, _ = args["outcome_summary"].(string)
			req.ConsumedBy, _ = args["consumed_by"].(string)
			if req.ConsumedBy == "" {
				req.ConsumedBy = "agent"
			}
			input, err := consumeReportHumanInput(ctx, workspacePath, inputID, req)
			if err != nil {
				return "", err
			}
			return marshalReportHumanInputToolResult("consumed", input)
		},
	}
	categories := map[string]string{
		"create_human_input_request": "human_tools",
		"mark_human_input_consumed":  "human_tools",
	}
	return []llmtypes.Tool{createTool, consumeTool}, executors, categories
}

func reportHumanInputCreateRequestFromToolArgs(args map[string]interface{}) (ReportHumanInputCreateRequest, error) {
	var req ReportHumanInputCreateRequest
	req.WorkspacePath, _ = args["workspace_path"].(string)
	req.InputID, _ = args["input_id"].(string)
	req.Source, _ = args["source"].(string)
	req.Priority, _ = args["priority"].(string)
	req.Question, _ = args["question"].(string)
	req.Context, _ = args["context"].(string)
	req.RunID, _ = args["run_id"].(string)
	req.Evidence, _ = args["evidence"].(string)
	req.CreatedBy, _ = args["created_by"].(string)
	req.AllowFreeText, _ = args["allow_free_text"].(bool)
	if raw, ok := args["options"]; ok {
		b, _ := json.Marshal(raw)
		if err := json.Unmarshal(b, &req.Options); err != nil {
			return req, fmt.Errorf("options must be an array of {id,title,description}")
		}
	}
	return req, nil
}

func marshalReportHumanInputToolResult(status string, input *ReportHumanInput) (string, error) {
	payload := map[string]interface{}{
		"status": status,
		"input":  input,
		"note":   "Stored in the workflow-local db/db.sqlite report_human_inputs table.",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (api *StreamingAPI) handleListReportHumanInputs(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath := r.URL.Query().Get("workspace_path")
	inputs, err := listReportHumanInputs(r.Context(), workspacePath, r.URL.Query().Get("status"), r.URL.Query().Get("source"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "inputs": inputs})
}

func (api *StreamingAPI) handleCreateReportHumanInput(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportHumanInputCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.CreatedBy == "" {
		req.CreatedBy = GetUserIDFromContext(r.Context())
	}
	input, err := createReportHumanInput(r.Context(), req.WorkspacePath, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "input": input})
}

func (api *StreamingAPI) handleAnswerReportHumanInput(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportHumanInputAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" {
		req.WorkspacePath = r.URL.Query().Get("workspace_path")
	}
	if req.AnsweredBy == "" {
		req.AnsweredBy = GetUserIDFromContext(r.Context())
	}
	input, err := answerReportHumanInput(r.Context(), req.WorkspacePath, mux.Vars(r)["input_id"], req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "input": input})
}

func (api *StreamingAPI) handleDismissReportHumanInput(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req struct {
		WorkspacePath string `json:"workspace_path"`
		AnsweredBy    string `json:"answered_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.WorkspacePath == "" {
		req.WorkspacePath = r.URL.Query().Get("workspace_path")
	}
	if req.AnsweredBy == "" {
		req.AnsweredBy = GetUserIDFromContext(r.Context())
	}
	input, err := dismissReportHumanInput(r.Context(), req.WorkspacePath, mux.Vars(r)["input_id"], req.AnsweredBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "input": input})
}

func (api *StreamingAPI) handleConsumeReportHumanInput(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req ReportHumanInputConsumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" {
		req.WorkspacePath = r.URL.Query().Get("workspace_path")
	}
	if req.ConsumedBy == "" {
		req.ConsumedBy = GetUserIDFromContext(r.Context())
	}
	input, err := consumeReportHumanInput(r.Context(), req.WorkspacePath, mux.Vars(r)["input_id"], req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "input": input})
}

func formatAnsweredReportHumanInputsForAgent(ctx context.Context, workspacePath string) string {
	inputs, err := listReportHumanInputs(ctx, workspacePath, "answered", "")
	if err != nil || len(inputs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Answered human input requests waiting for this workflow pass:\n")
	for _, input := range inputs {
		answer := reportHumanInputAnswerForAgent(input)
		context := strings.TrimSpace(input.Context)
		if context != "" {
			context = " context=" + strconv.Quote(context)
		}
		b.WriteString(fmt.Sprintf("- input_id=%s source=%s priority=%s question=%q answer=%q answered_at=%s evidence=%q%s\n",
			input.ID, input.Source, input.Priority, input.Question, answer, input.AnsweredAt, input.Evidence, context))
	}
	b.WriteString("If an answered Goal Advisor plan proposal is approved, apply it only with normal plan modification/config/eval/report tools, then call mark_human_input_consumed with the concrete outcome. If it is rejected or deferred, record that outcome and consume it. After consuming any answer, remove or replace the matching visible Human input requested card in builder/improve.html so Pulse no longer shows it as an active question; keep only a short outcome Decision/Note when useful. Do not edit the SQLite table directly.\n")
	return strings.TrimSpace(b.String())
}

// formatAnsweredChiefOfStaffInputsForAgent gathers only Chief of Staff answers
// across the org-level pulse scope and discovered workflow scopes. Unlike a
// workflow Pulse pass, Chief of Staff has no single workspace DB, so each line
// carries the workspace_path required to record the outcome in the same scope.
func formatAnsweredChiefOfStaffInputsForAgent(ctx context.Context, workspacePaths []string) string {
	seen := make(map[string]struct{}, len(workspacePaths))
	inputs := make([]ReportHumanInput, 0)
	for _, workspacePath := range workspacePaths {
		workspacePath = strings.TrimSpace(workspacePath)
		if workspacePath == "" {
			continue
		}
		if _, exists := seen[workspacePath]; exists {
			continue
		}
		seen[workspacePath] = struct{}{}
		workspaceInputs, err := listReportHumanInputs(ctx, workspacePath, "answered", "chief_of_staff")
		if err != nil {
			continue
		}
		inputs = append(inputs, workspaceInputs...)
	}
	if len(inputs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Answered Chief of Staff questions waiting for this run:\n")
	for _, input := range inputs {
		context := strings.TrimSpace(input.Context)
		if context != "" {
			context = " context=" + strconv.Quote(context)
		}
		b.WriteString(fmt.Sprintf("- workspace_path=%s input_id=%s priority=%s question=%q answer=%q answered_at=%s evidence=%q%s\n",
			input.WorkspacePath, input.ID, input.Priority, input.Question, reportHumanInputAnswerForAgent(input), input.AnsweredAt, input.Evidence, context))
	}
	b.WriteString("Use each answer when it is still relevant. Do not mark an answer consumed merely because you read it. After the requested action or a concrete no-action/deferred/stale decision is complete, call mark_human_input_consumed with the same workspace_path and input_id plus a truthful outcome_summary. If the action cannot be completed safely in this run, leave the answer unconsumed so a later Chief of Staff or workflow Pulse pass can handle it.\n")
	return strings.TrimSpace(b.String())
}

func reportHumanInputAnswerForAgent(input ReportHumanInput) string {
	answer := input.Note
	if input.SelectedOptionID == "" {
		return answer
	}
	answer = fmt.Sprintf("option=%s", input.SelectedOptionID)
	if title := reportHumanInputOptionTitle(input.Options, input.SelectedOptionID); title != "" {
		answer += fmt.Sprintf(" (%s)", title)
	}
	if input.Note != "" {
		answer += "; note=" + input.Note
	}
	return answer
}

func normalizeReportHumanInputSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "goal_advisor", "goal-advisor", "goal advisor":
		return "goal_advisor"
	case "chief_of_staff", "chief-of-staff", "chief", "org_pulse", "org-pulse":
		return "chief_of_staff"
	default:
		return "pulse"
	}
}

func normalizeReportHumanInputPriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "low", "high":
		return strings.ToLower(strings.TrimSpace(priority))
	default:
		return "medium"
	}
}

func normalizeReportHumanInputOptions(options []ReportHumanInputOption) ([]ReportHumanInputOption, error) {
	if len(options) == 0 {
		return []ReportHumanInputOption{}, nil
	}
	out := make([]ReportHumanInputOption, 0, len(options))
	seen := map[string]bool{}
	for _, option := range options {
		id := normalizeReportHumanInputID(option.ID)
		title := strings.TrimSpace(option.Title)
		if id == "" || title == "" {
			return nil, fmt.Errorf("each option requires id and title")
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate option id %q", id)
		}
		seen[id] = true
		out = append(out, ReportHumanInputOption{
			ID:          id,
			Title:       title,
			Description: strings.TrimSpace(option.Description),
		})
	}
	return out, nil
}

var reportHumanInputIDRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func normalizeReportHumanInputID(id string) string {
	cleaned := strings.Trim(reportHumanInputIDRe.ReplaceAllString(strings.TrimSpace(id), "-"), "-_")
	if len(cleaned) > 96 {
		cleaned = cleaned[:96]
	}
	return cleaned
}

func newReportHumanInputID(question string) string {
	slug := strings.ToLower(normalizeReportHumanInputID(question))
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-_")
	}
	if slug == "" {
		slug = "question"
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("input-%s-%s", time.Now().UTC().Format("20060102-150405"), slug)
	}
	return fmt.Sprintf("input-%s-%s-%s", time.Now().UTC().Format("20060102-150405"), slug, hex.EncodeToString(buf))
}

func reportHumanInputOptionExists(options []ReportHumanInputOption, id string) bool {
	for _, option := range options {
		if option.ID == id {
			return true
		}
	}
	return false
}

func reportHumanInputOptionTitle(options []ReportHumanInputOption, id string) string {
	for _, option := range options {
		if option.ID == id {
			return option.Title
		}
	}
	return ""
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isReportHumanInputsMissingTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table: report_human_inputs")
}
