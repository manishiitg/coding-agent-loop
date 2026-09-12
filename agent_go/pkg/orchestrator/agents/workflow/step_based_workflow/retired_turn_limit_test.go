package step_based_workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRetiredExecutionTurnLimitIgnoredInSavedConfig(t *testing.T) {
	// Existing workflows remain loadable, but the removed override cannot be
	// carried forward into effective config or saved again on a typed round trip.
	var config AgentConfigs
	if err := json.Unmarshal([]byte(`{"execution_max_turns":1,"knowledgebase_access":"read"}`), &config); err != nil {
		t.Fatal(err)
	}
	if config.KnowledgebaseAccess != "read" {
		t.Fatal("lost supported setting")
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "execution_max_turns") {
		t.Fatal("retired limit survived config round trip")
	}
}
