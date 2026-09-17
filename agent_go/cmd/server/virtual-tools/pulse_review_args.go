package virtualtools

import (
	"fmt"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

var pulseReviewStatuses = []string{"clean", "issues_found", "fixed", "incomplete", "failed", "skipped"}
var pulseVerificationStatuses = []string{"verified", "monitoring", "unverified", "not_applicable"}

func stringArg(args map[string]interface{}, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
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
