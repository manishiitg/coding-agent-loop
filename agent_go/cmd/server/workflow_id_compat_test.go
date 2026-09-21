package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkflowIDDecodesOldKeyButWritesCanonicalKey(t *testing.T) {
	for _, target := range []interface{}{&QueryRequest{}, &WorkflowRequest{}, &WorkflowUpdateRequest{}} {
		if err := json.Unmarshal([]byte(`{"preset_query_id":"wf-old"}`), target); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(target)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"workflow_id":"wf-old"`) || strings.Contains(string(encoded), `"preset_query_id"`) {
			t.Fatalf("noncanonical workflow ID encoding: %s", encoded)
		}
	}
}

func TestWorkflowIDCanonicalKeyWins(t *testing.T) {
	var req QueryRequest
	if err := json.Unmarshal([]byte(`{"workflow_id":"wf-new","preset_query_id":"wf-old"}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.WorkflowID != "wf-new" {
		t.Fatalf("WorkflowID = %q, want wf-new", req.WorkflowID)
	}
}
