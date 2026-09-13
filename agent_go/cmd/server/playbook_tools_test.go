package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchPlaybooksFindsIntentAndReportsInstalledStatus(t *testing.T) {
	items, err := loadPlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	matches := searchPlaybooks(items, []InstalledPlaybook{{ID: "browser-performance-validation", Version: "0.2.0", Status: "draft"}}, "browser performance budgets", 3)
	if len(matches) == 0 || matches[0].ID != "browser-performance-validation" {
		t.Fatalf("matches = %+v", matches)
	}
	if !matches[0].Installed || matches[0].InstalledStatus != "draft" || matches[0].InstalledVersion != "0.2.0" || !matches[0].UpdateAvailable || len(matches[0].Changelog) == 0 || len(matches[0].Outputs) == 0 || len(matches[0].SetupAreas) == 0 || len(matches[0].PulseFocus) != 3 {
		t.Fatalf("incomplete result = %+v", matches[0])
	}
}

func TestPlaybookSearchToolIsRecommendationOnly(t *testing.T) {
	api := &StreamingAPI{}
	reg := &recordingRegistrar{}
	if err := api.registerPlaybookSearchTool(reg, ""); err != nil {
		t.Fatal(err)
	}
	tool, ok := reg.tools["search_playbooks"]
	if !ok || !strings.Contains(tool.desc, "never installs") || !strings.Contains(tool.desc, "open_workspace_view") {
		t.Fatalf("tool = %+v", tool)
	}
	out, err := tool.exec(context.Background(), map[string]interface{}{"query": "cost anomalies", "limit": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Matches []playbookSearchResult `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Matches) == 0 || payload.Matches[0].ID != "cost-anomaly-to-verified-savings" {
		t.Fatalf("out = %s", out)
	}
}
