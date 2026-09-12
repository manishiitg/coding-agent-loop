package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalMetricToolsRoundTripMultipleOutcomesAndNotifications(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	ws := "Workflow/metrics"
	if err := os.MkdirAll(filepath.Join(root, ws, "db"), 0755); err != nil {
		t.Fatal(err)
	}
	_, executors, _ := createGoalMetricTools()
	call := func(name string, args map[string]interface{}) (string, error) {
		args["workspace_path"] = ws
		return executors[name].(func(context.Context, map[string]interface{}) (string, error))(context.Background(), args)
	}
	metric := func(id, goal string) map[string]interface{} {
		return map[string]interface{}{"id": id, "criterion_id": id, "name": id, "role": "primary", "goal_id": goal, "goal_name": goal, "unit": "ms", "direction": "decrease", "definition": "p50", "source": "samples", "window": "daily", "collection_frequency": "daily", "freshness_hours": 48}
	}
	latency := metric("voice", "performance")
	cost := metric("cost", "spend")
	english := metric("English", "performance")
	english["role"] = "supporting"
	delete(english, "goal_id")
	delete(english, "goal_name")
	english["supports"] = []string{"voice"}
	english["support_kind"] = "breakdown"
	english["dimensions"] = map[string]string{"language": "English"}
	if _, err := call("configure_goal_metrics", map[string]interface{}{"metrics": []interface{}{latency, cost, english}}); err != nil {
		t.Fatal(err)
	}
	if _, err := call("record_goal_observations", map[string]interface{}{"observations": []interface{}{map[string]interface{}{"metric": "voice", "criterion_id": "voice", "unit": "ms", "run_id": "run-1", "observed_at": "2026-09-12T00:00:00Z", "value": 120, "evidence": []string{"source"}}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := call("get_goal_metrics", map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Metrics []struct {
			ID       string   `json:"id"`
			Supports []string `json:"supports"`
		}
		Progress []json.RawMessage
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Metrics) != 3 || len(result.Progress) != 3 {
		t.Fatal("tool dropped outcomes", raw)
	}
	sections, err := loadGoalProgressNotificationSections(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 2 {
		t.Fatal("notification dropped primary", sections)
	}
	for _, section := range sections {
		if strings.Contains(section.Heading, "voice") && !strings.Contains(section.Body, "English") {
			t.Fatal("missing supporting breakdown", section)
		}
		if strings.Contains(section.Heading, "cost") && strings.Contains(section.Body, "English") {
			t.Fatal("supporting breakdown attached to wrong goal", section)
		}
	}
}
