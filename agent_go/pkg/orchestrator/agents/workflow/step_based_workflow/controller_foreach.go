package step_based_workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// expandForeach reads a JSON array from a workspace file (typically under db/) and renders
// messageTemplate once per row using Go text/template with the row bound to ".". It returns
// one message string per row — the runtime then feeds each as its own conversation turn, so
// every row is processed deterministically (the loop lives in code, not in the LLM, which is
// the reliability win when a prior step has written rows to the db for this step to consume).
//
// source is a workspace-relative path (e.g. "db/tasks.json"). sourcePath optionally selects a
// nested array via a dot path (e.g. "result.items"); empty means the file's top-level value is
// the array. maxIterations > 0 caps the number of rows processed (capping is always logged —
// never silent).
func (hcpo *StepBasedWorkflowOrchestrator) expandForeach(ctx context.Context, source, sourcePath, messageTemplate string, maxIterations int) ([]string, error) {
	src := strings.TrimSpace(source)
	if src == "" {
		return nil, fmt.Errorf("foreach: source is required")
	}
	if strings.TrimSpace(messageTemplate) == "" {
		return nil, fmt.Errorf("foreach: message template is required")
	}

	content, err := hcpo.ReadWorkspaceFile(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("foreach: read source %q: %w", src, err)
	}

	var root interface{}
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		return nil, fmt.Errorf("foreach: parse source %q as JSON: %w", src, err)
	}

	// Navigate to the array via the optional dot path.
	node := root
	if sp := strings.TrimSpace(sourcePath); sp != "" {
		for _, key := range strings.Split(sp, ".") {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			obj, ok := node.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("foreach: source_path %q does not resolve to an array in %q (segment %q is not an object)", sp, src, key)
			}
			node = obj[key]
		}
	}

	rows, ok := node.([]interface{})
	if !ok {
		return nil, fmt.Errorf("foreach: source %q (path %q) is not a JSON array", src, sourcePath)
	}

	tmpl, err := template.New("foreach").Option("missingkey=zero").Parse(messageTemplate)
	if err != nil {
		return nil, fmt.Errorf("foreach: invalid message template: %w", err)
	}

	total := len(rows)
	limit := total
	if maxIterations > 0 && maxIterations < total {
		limit = maxIterations
		hcpo.GetLogger().Warn(fmt.Sprintf("🔁 foreach: source %q has %d rows; capping to max_iterations=%d (%d rows NOT processed)", src, total, maxIterations, total-maxIterations))
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔁 foreach: source %q → %d row(s) to process", src, limit))

	messages := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, rows[i]); err != nil {
			return nil, fmt.Errorf("foreach: render row %d of %q: %w", i, src, err)
		}
		messages = append(messages, buf.String())
	}
	return messages, nil
}
