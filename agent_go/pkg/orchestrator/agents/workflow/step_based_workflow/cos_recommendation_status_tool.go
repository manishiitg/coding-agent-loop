package step_based_workflow

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

const chiefOfStaffRecommendationStatusToolName = "mark_cos_recommendation_status"

var validChiefOfStaffRecommendationStatuses = map[string]bool{
	"proposed":            true,
	"accepted":            true,
	"queued_goal_advisor": true,
	"in_progress":         true,
	"needs_evidence":      true,
	"done":                true,
	"dismissed":           true,
	"blocked":             true,
}

// RegisterChiefOfStaffRecommendationStatusTool registers the workflow-side reply
// tool for Org Pulse recommendation cards in builder/improve.html.
func RegisterChiefOfStaffRecommendationStatusTool(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) error {
	if mcpAgent == nil {
		return fmt.Errorf("nil mcp agent")
	}
	return mcpAgent.RegisterCustomTool(
		chiefOfStaffRecommendationStatusToolName,
		"Mark a Chief of Staff / Org Pulse recommendation card in builder/improve.html with workflow-side status. Use this instead of hand-editing data-status attributes after Workflow Pulse or Goal Advisor accepts, queues, completes, dismisses, blocks, or requests more evidence for a recommendation.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"rec_id": map[string]interface{}{
					"type":        "string",
					"description": "Stable recommendation id from data-cos-rec-id or data-rec-id on the card.",
				},
				"status": map[string]interface{}{
					"type": "string",
					"enum": []interface{}{
						"proposed",
						"accepted",
						"queued_goal_advisor",
						"in_progress",
						"needs_evidence",
						"done",
						"dismissed",
						"blocked",
					},
					"description": "Workflow-side lifecycle status.",
				},
				"note": map[string]interface{}{
					"type":        "string",
					"description": "Short reason for the status change. Keep it one sentence.",
				},
				"evidence": map[string]interface{}{
					"type":        "string",
					"description": "Optional evidence path, run id, decision card id, or blocker path supporting the status.",
				},
				"updated_at": map[string]interface{}{
					"type":        "string",
					"description": "Optional RFC3339 UTC timestamp. Defaults to now.",
				},
				"updated_by": map[string]interface{}{
					"type":        "string",
					"description": "Optional actor label. Defaults to workflow-pulse.",
				},
			},
			"required": []string{"rec_id", "status", "note"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return markChiefOfStaffRecommendationStatus(ctx, workspacePath, args, logger, readFile, writeFile)
		},
		"workflow",
	)
}

func markChiefOfStaffRecommendationStatus(
	ctx context.Context,
	workspacePath string,
	args map[string]interface{},
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) (string, error) {
	recID := strings.TrimSpace(asString(args["rec_id"]))
	if recID == "" {
		return "rec_id is required", nil
	}
	status := strings.TrimSpace(asString(args["status"]))
	if !validChiefOfStaffRecommendationStatuses[status] {
		return `status must be one of "proposed", "accepted", "queued_goal_advisor", "in_progress", "needs_evidence", "done", "dismissed", or "blocked"`, nil
	}
	note := strings.TrimSpace(asString(args["note"]))
	if note == "" {
		return "note is required", nil
	}
	updatedAt := strings.TrimSpace(asString(args["updated_at"]))
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	updatedBy := strings.TrimSpace(asString(args["updated_by"]))
	if updatedBy == "" {
		updatedBy = "workflow-pulse"
	}
	evidence := strings.TrimSpace(asString(args["evidence"]))

	workspacePath = strings.Trim(strings.TrimSpace(workspacePath), "/")
	if workspacePath == "" {
		return "workspace path is missing", nil
	}
	improvePath := workspacePath + "/builder/improve.html"
	content, err := readFile(ctx, improvePath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", improvePath, err)
	}

	updated, changed := updateChiefOfStaffRecommendationStatusHTML(content, recID, status, note, evidence, updatedAt, updatedBy)
	if !changed {
		return fmt.Sprintf("No Chief of Staff recommendation card with rec_id %q found in %s. Expected data-cos-rec-id or data-rec-id on the card.", recID, improvePath), nil
	}
	if err := writeFile(ctx, improvePath, updated); err != nil {
		return "", fmt.Errorf("write %s: %w", improvePath, err)
	}
	logger.Info(fmt.Sprintf("Marked Chief of Staff recommendation %s as %s in %s", recID, status, improvePath))
	return fmt.Sprintf("Marked Chief of Staff recommendation %s as %s in %s", recID, status, improvePath), nil
}

func updateChiefOfStaffRecommendationStatusHTML(content, recID, status, note, evidence, updatedAt, updatedBy string) (string, bool) {
	if strings.TrimSpace(recID) == "" {
		return content, false
	}
	quotedID := regexp.QuoteMeta(recID)
	cardRe := regexp.MustCompile(`(?is)<(article|div)\b[^>]*(?:data-cos-rec-id|data-rec-id)\s*=\s*["']` + quotedID + `["'][^>]*>`)
	loc := cardRe.FindStringIndex(content)
	if loc == nil {
		return content, false
	}

	openTag := content[loc[0]:loc[1]]
	openTag = upsertHTMLAttribute(openTag, "data-status", status)
	openTag = upsertHTMLAttribute(openTag, "data-status-updated-at", updatedAt)
	openTag = upsertHTMLAttribute(openTag, "data-status-updated-by", updatedBy)
	openTag = upsertHTMLAttribute(openTag, "data-status-note", note)
	if strings.TrimSpace(evidence) != "" {
		openTag = upsertHTMLAttribute(openTag, "data-status-evidence", evidence)
	}

	return content[:loc[0]] + openTag + content[loc[1]:], true
}

func upsertHTMLAttribute(openTag, name, value string) string {
	escaped := html.EscapeString(value)
	attrRe := regexp.MustCompile(`(?is)\s+` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*')`)
	replacement := fmt.Sprintf(` %s="%s"`, name, escaped)
	if attrRe.MatchString(openTag) {
		return attrRe.ReplaceAllString(openTag, replacement)
	}
	insertAt := strings.LastIndex(openTag, ">")
	if insertAt < 0 {
		return openTag
	}
	return openTag[:insertAt] + replacement + openTag[insertAt:]
}
