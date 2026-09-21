package chathistory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBotConnectorWorkflowIDMigration(t *testing.T) {
	var cfg BotConnectorConfig
	if err := json.Unmarshal([]byte(`{"id":"slack","default_preset_id":"wf-old"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultWorkflowID != "wf-old" {
		t.Fatalf("DefaultWorkflowID = %q", cfg.DefaultWorkflowID)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"default_workflow_id":"wf-old"`) || strings.Contains(string(encoded), `"default_preset_id"`) {
		t.Fatalf("noncanonical bot config: %s", encoded)
	}
}
