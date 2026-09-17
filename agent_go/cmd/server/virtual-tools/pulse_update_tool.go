package virtualtools

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

var pulseReviewStatuses = []string{"clean", "issues_found", "fixed", "incomplete", "failed", "skipped"}
var pulseVerificationStatuses = []string{"verified", "monitoring", "unverified", "not_applicable"}

func createPublishPulseUpdateTool() llmtypes.Tool {
	reviewSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"module": map[string]interface{}{
				"type":        "string",
				"minLength":   1,
				"description": "Stable review module name, for example technical_review, architecture_review, strategic_review, or plan_drift_review.",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Short human-readable review name. Defaults to the module name.",
			},
			"status": map[string]interface{}{
				"type": "string",
				"enum": pulseReviewStatuses,
			},
			"summary": map[string]interface{}{
				"type":        "string",
				"minLength":   1,
				"description": "One plain-language sentence saying what the review checked and concluded or changed.",
			},
			"issues_found":  map[string]interface{}{"type": "integer", "minimum": 0},
			"fixes_applied": map[string]interface{}{"type": "integer", "minimum": 0},
			"verification": map[string]interface{}{
				"type": "string",
				"enum": pulseVerificationStatuses,
			},
		},
		"required":             []string{"module", "status", "summary", "issues_found", "fixes_applied", "verification"},
		"additionalProperties": false,
	}
	stringList := func(description string) map[string]interface{} {
		return map[string]interface{}{
			"type":        "array",
			"maxItems":    20,
			"items":       map[string]interface{}{"type": "string", "minLength": 1},
			"description": description,
		}
	}
	summaryFieldsSchema := map[string]interface{}{
		"type": "array", "maxItems": 10,
		"items": map[string]interface{}{
			"type": "object", "additionalProperties": false,
			"properties": map[string]interface{}{"label": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"}},
			"required":   []string{"label", "value"},
		},
	}
	summarySectionsSchema := map[string]interface{}{
		"type": "array", "maxItems": 12,
		"items": map[string]interface{}{
			"type": "object", "additionalProperties": false,
			"properties": map[string]interface{}{"heading": map[string]interface{}{"type": "string"}, "body": map[string]interface{}{"type": "string"}},
			"required":   []string{"heading", "body"},
		},
	}
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "publish_pulse_update",
			Description: "Publish the one structured Pulse review-and-fix digest for the current workflow. Use this after every Pulse pass instead of notify_user(notification_kind=\"pulse_summary\"). It durably records Activity with one typed result for every review module that ran or was explicitly skipped, then uses the configured Pulse channels through the existing notification backend. This is not for workflow run outcomes; send those with notify_user(notification_kind=\"run_summary\").",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type": "string", "minLength": 1, "maxLength": 150,
						"description": "Short Pulse verdict suitable for Activity and notification subject lines.",
					},
					"status": map[string]interface{}{
						"type": "string",
						"enum": []string{"completed", "failed", "blocked", "waiting_for_user", "waiting_for_platform", "monitoring", "informational"},
					},
					"summary": map[string]interface{}{
						"type": "string", "minLength": 1,
						"description": "Concise overall takeaway. Do not repeat every review row here.",
					},
					"reviews": map[string]interface{}{
						"type": "array", "minItems": 1, "maxItems": 20, "items": reviewSchema,
						"description": "One entry per due review module. Include skipped and incomplete modules explicitly; never omit them or call incomplete work clean.",
					},
					"issues_found":       stringList("Material new or reopened issues found in this pass. Empty means none were proven."),
					"fixed_by_pulse":     stringList("Fixes applied by Pulse, including their verification state in ordinary language."),
					"still_pending":      stringList("Current retained issues or blockers that remain open."),
					"decisions_required": stringList("User decisions still required and what each decision unblocks."),
					"operations":         stringList("Backup, publish, review timing, and next-Pulse operational facts."),
					"summary_routes":     notificationRoutesSchema(summaryFieldsSchema, summarySectionsSchema),
				},
				"required":             []string{"title", "status", "summary", "reviews", "issues_found", "fixed_by_pulse", "still_pending", "decisions_required", "operations"},
				"additionalProperties": false,
			}),
		},
	}
}

func handlePublishPulseUpdate(ctx context.Context, args map[string]interface{}) (string, error) {
	title := stringArg(args, "title")
	status := normalizedNotificationSummaryStatus(stringArg(args, "status"))
	summary := stringArg(args, "summary")
	if title == "" || summary == "" {
		return "", fmt.Errorf("title and summary are required")
	}
	if status == "" || status == "no_run" {
		return "", fmt.Errorf("status must describe the Pulse outcome")
	}
	reviews, err := pulseReviewsFromArg(args["reviews"])
	if err != nil {
		return "", err
	}
	if len(reviews) == 0 {
		return "", fmt.Errorf("reviews must contain every due review module")
	}

	sections := make([]interface{}, 0, 5)
	for _, section := range []struct{ key, heading string }{
		{"issues_found", "Issues found this pass"},
		{"fixed_by_pulse", "Fixed by Pulse"},
		{"still_pending", "Still pending"},
		{"decisions_required", "Needs your decision"},
		{"operations", "Operations"},
	} {
		values, err := requiredStringListArg(args, section.key)
		if err != nil {
			return "", err
		}
		if len(values) > 0 {
			sections = append(sections, map[string]interface{}{"heading": section.heading, "body": strings.Join(values, "\n")})
		}
	}

	issues, fixes := 0, 0
	for _, review := range reviews {
		issues += review.IssuesFound
		fixes += review.FixesApplied
	}
	notifyArgs := map[string]interface{}{
		"message_for_user":  renderPulseUpdateMessage(summary, reviews, sections),
		"notification_kind": "pulse_summary",
		"summary_title":     title,
		"summary_status":    status,
		"summary_fields": []interface{}{
			map[string]interface{}{"label": "Reviews", "value": fmt.Sprintf("%d recorded", len(reviews))},
			map[string]interface{}{"label": "Issues found", "value": fmt.Sprintf("%d", issues)},
			map[string]interface{}{"label": "Fixes applied", "value": fmt.Sprintf("%d", fixes)},
		},
		"summary_sections": sections,
		"summary_routes":   args["summary_routes"],
		"pulse_reviews":    pulseReviewsAsArgs(reviews),
		"slack_title":      title,
		"slack_color":      slackColorForSummaryStatus(status),
		"slack_fields": []interface{}{
			map[string]interface{}{"label": "Reviews", "value": fmt.Sprintf("%d recorded", len(reviews))},
			map[string]interface{}{"label": "Issues found", "value": fmt.Sprintf("%d", issues)},
			map[string]interface{}{"label": "Fixes applied", "value": fmt.Sprintf("%d", fixes)},
		},
		"slack_sections": sections,
		"email_subject":  title,
	}
	return handleNotifyUser(ctx, notifyArgs)
}

func renderPulseUpdateMessage(summary string, reviews []services.PulseReviewSummary, sections []interface{}) string {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(summary))
	body.WriteString("\n\nReviews")
	for _, review := range reviews {
		fmt.Fprintf(&body, "\n- %s — %s: %s", review.Label, strings.ReplaceAll(review.Status, "_", " "), review.Summary)
		facts := make([]string, 0, 3)
		if review.IssuesFound > 0 {
			facts = append(facts, fmt.Sprintf("%d issues", review.IssuesFound))
		}
		if review.FixesApplied > 0 {
			facts = append(facts, fmt.Sprintf("%d fixes", review.FixesApplied))
		}
		if review.Verification != "not_applicable" {
			facts = append(facts, strings.ReplaceAll(review.Verification, "_", " "))
		}
		if len(facts) > 0 {
			fmt.Fprintf(&body, " (%s)", strings.Join(facts, ", "))
		}
	}
	for _, raw := range sections {
		section, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		heading, _ := section["heading"].(string)
		text, _ := section["body"].(string)
		if strings.TrimSpace(text) != "" {
			fmt.Fprintf(&body, "\n\n%s\n%s", strings.TrimSpace(heading), strings.TrimSpace(text))
		}
	}
	return body.String()
}

func stringArg(args map[string]interface{}, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func requiredStringListArg(args map[string]interface{}, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	result := make([]string, 0, len(items))
	for i, item := range items {
		value, ok := item.(string)
		value = strings.TrimSpace(value)
		if !ok || value == "" {
			return nil, fmt.Errorf("%s[%d] must be a non-empty string", key, i)
		}
		result = append(result, value)
	}
	return result, nil
}

func pulseReviewsFromArg(raw interface{}) ([]services.PulseReviewSummary, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("reviews must be an array")
	}
	result := make([]services.PulseReviewSummary, 0, len(items))
	for i, rawItem := range items {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("reviews[%d] must be an object", i)
		}
		review := services.PulseReviewSummary{
			Module: stringArg(item, "module"), Label: stringArg(item, "label"),
			Status: stringArg(item, "status"), Summary: stringArg(item, "summary"),
			IssuesFound: intArg(item["issues_found"]), FixesApplied: intArg(item["fixes_applied"]),
			Verification: stringArg(item, "verification"),
		}
		if review.Module == "" || review.Summary == "" {
			return nil, fmt.Errorf("reviews[%d] requires module and summary", i)
		}
		if !containsString(pulseReviewStatuses, review.Status) {
			return nil, fmt.Errorf("reviews[%d] has invalid status", i)
		}
		if !containsString(pulseVerificationStatuses, review.Verification) {
			return nil, fmt.Errorf("reviews[%d] has invalid verification", i)
		}
		if review.IssuesFound < 0 || review.FixesApplied < 0 {
			return nil, fmt.Errorf("reviews[%d] counts cannot be negative", i)
		}
		if review.Label == "" {
			review.Label = strings.ReplaceAll(review.Module, "_", " ")
		}
		result = append(result, review)
	}
	return result, nil
}

func intArg(raw interface{}) int {
	switch value := raw.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pulseReviewsAsArgs(reviews []services.PulseReviewSummary) []interface{} {
	result := make([]interface{}, 0, len(reviews))
	for _, review := range reviews {
		result = append(result, map[string]interface{}{
			"module": review.Module, "label": review.Label, "status": review.Status,
			"summary": review.Summary, "issues_found": review.IssuesFound,
			"fixes_applied": review.FixesApplied, "verification": review.Verification,
		})
	}
	return result
}

func slackColorForSummaryStatus(status string) string {
	switch status {
	case "completed":
		return "success"
	case "failed":
		return "danger"
	case "blocked", "waiting_for_user", "waiting_for_platform", "monitoring":
		return "warning"
	default:
		return "neutral"
	}
}
