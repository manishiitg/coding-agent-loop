package step_based_workflow

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// expandForeach runs a read-only SQL query against db/db.sqlite and renders messageTemplate
// once per result row using Go text/template with the row (an object keyed by column) bound to
// ".". It returns one message string per row — the runtime then feeds each as its own
// conversation turn, so every row is processed deterministically (the loop lives in code, not in
// the LLM, which is the reliability win when a prior step has written rows to the db for this
// step to consume).
//
// sourceSQL is a read-only query (e.g. "SELECT id, name FROM tasks WHERE status='pending'").
// maxIterations > 0 caps the number of rows processed (capping is always logged —
// never silent).
func (hcpo *StepBasedWorkflowOrchestrator) expandForeach(ctx context.Context, sourceSQL, messageTemplate string, maxIterations int) ([]string, error) {
	if strings.TrimSpace(messageTemplate) == "" {
		return nil, fmt.Errorf("foreach: message template is required")
	}
	sql := strings.TrimSpace(sourceSQL)
	if sql == "" {
		return nil, fmt.Errorf("foreach: source_sql is required")
	}

	// Rows come from a read-only SQL query against db/db.sqlite. Each result row
	// (object keyed by column) binds to '.' in the message template below.
	srcLabel := "sql:" + sql
	dbRows, err := hcpo.QueryWorkflowDB(ctx, "db/db.sqlite", sql)
	if err != nil {
		return nil, fmt.Errorf("foreach: query db/db.sqlite: %w", err)
	}
	rows := make([]interface{}, len(dbRows))
	for i, r := range dbRows {
		rows[i] = r
	}

	tmpl, err := template.New("foreach").Option("missingkey=zero").Parse(messageTemplate)
	if err != nil {
		return nil, fmt.Errorf("foreach: invalid message template: %w", err)
	}

	total := len(rows)
	limit := total
	if maxIterations > 0 && maxIterations < total {
		limit = maxIterations
		hcpo.GetLogger().Warn(fmt.Sprintf("🔁 foreach: source %q has %d rows; capping to max_iterations=%d (%d rows NOT processed)", srcLabel, total, maxIterations, total-maxIterations))
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔁 foreach: source %q → %d row(s) to process", srcLabel, limit))

	messages := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, rows[i]); err != nil {
			return nil, fmt.Errorf("foreach: render row %d of %q: %w", i, srcLabel, err)
		}
		messages = append(messages, buf.String())
	}
	return messages, nil
}
