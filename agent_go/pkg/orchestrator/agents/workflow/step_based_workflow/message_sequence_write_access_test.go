package step_based_workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageSequenceWriteAccess_RejectsPerFilePaths(t *testing.T) {
	var w MessageSequenceWriteAccess
	err := json.Unmarshal([]byte(`{"db": true, "paths": ["db/session_health.json"]}`), &w)
	if err == nil {
		t.Fatal("expected error for per-file paths in write_access, got nil")
	}
	if !strings.Contains(err.Error(), "per-file scoping") {
		t.Fatalf("error should explain per-file scoping is unsupported, got: %v", err)
	}
}

func TestMessageSequenceWriteAccess_FolderBooleansOK(t *testing.T) {
	var w MessageSequenceWriteAccess
	if err := json.Unmarshal([]byte(`{"db": true, "knowledgebase": true}`), &w); err != nil {
		t.Fatalf("folder-level booleans should unmarshal cleanly, got: %v", err)
	}
	if !w.DB || !w.Knowledgebase || w.Learnings {
		t.Fatalf("unexpected decoded write_access: %+v", w)
	}
}

func TestMessageSequenceWriteAccess_EmptyOK(t *testing.T) {
	var w MessageSequenceWriteAccess
	if err := json.Unmarshal([]byte(`{}`), &w); err != nil {
		t.Fatalf("empty write_access should unmarshal cleanly, got: %v", err)
	}
	if w != (MessageSequenceWriteAccess{}) {
		t.Fatalf("empty write_access should be zero value, got: %+v", w)
	}
}

func msgSeqStep(items ...MessageSequenceItem) *MessageSequencePlanStep {
	return &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{
			ID:          "step-seq",
			Title:       "Sequence",
			Description: "do work",
		},
		Items: items,
	}
}

func TestMessageSequenceItemWriteIntent(t *testing.T) {
	tests := []struct {
		name    string
		item    MessageSequenceItem
		wantDB  bool
		wantKB  bool
	}{
		{
			name:   "write verb + db path in message",
			item:   MessageSequenceItem{ID: "a", Type: "user_message", Message: "Build the queue and write it to db/action_queue.json"},
			wantDB: true,
		},
		{
			name:   "append to db",
			item:   MessageSequenceItem{ID: "a", Type: "user_message", Message: "Append the health snapshot to db/session_health.json"},
			wantDB: true,
		},
		{
			name:   "read-only db mention does not count",
			item:   MessageSequenceItem{ID: "a", Type: "user_message", Message: "Read db/action_queue.json and summarize it"},
			wantDB: false,
		},
		{
			name:   "write target elsewhere, db only read",
			item:   MessageSequenceItem{ID: "a", Type: "user_message", Message: "Compare against db/baseline.json, then write your findings to the report"},
			wantDB: false,
		},
		{
			name:   "output_files into db is definitive",
			item:   MessageSequenceItem{ID: "a", Type: "code", ScriptPath: "s.py", OutputFiles: []string{"db/action_queue.json"}},
			wantDB: true,
		},
		{
			name:   "kb notes write in message",
			item:   MessageSequenceItem{ID: "a", Type: "user_message", Message: "Save the finding to knowledgebase/notes/foo.md"},
			wantKB: true,
		},
		{
			name: "no write intent",
			item: MessageSequenceItem{ID: "a", Type: "user_message", Message: "Think about the plan and outline next steps"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDB, gotKB := messageSequenceItemWriteIntent(tc.item)
			if gotDB != tc.wantDB || gotKB != tc.wantKB {
				t.Fatalf("messageSequenceItemWriteIntent(%q) = (db=%v, kb=%v), want (db=%v, kb=%v)", tc.item.Message, gotDB, gotKB, tc.wantDB, tc.wantKB)
			}
		})
	}
}

func TestValidateMessageSequence_DBWriteWithoutAccessRejected(t *testing.T) {
	step := msgSeqStep(MessageSequenceItem{
		ID:      "build-queue",
		Type:    "user_message",
		Message: "Build the action queue and write it to db/action_queue.json",
	})
	err := validateMessageSequenceStepFieldsTyped(step)
	if err == nil {
		t.Fatal("expected validation error for db write without write_access, got nil")
	}
	if !strings.Contains(err.Error(), "db write access") {
		t.Fatalf("error should explain the missing db grant, got: %v", err)
	}
}

func TestValidateMessageSequence_DBWriteWithExplicitAccessOK(t *testing.T) {
	step := msgSeqStep(MessageSequenceItem{
		ID:          "build-queue",
		Type:        "user_message",
		Message:     "Build the action queue and write it to db/action_queue.json",
		WriteAccess: MessageSequenceWriteAccess{DB: true},
	})
	if err := validateMessageSequenceStepFieldsTyped(step); err != nil {
		t.Fatalf("expected no error with explicit db write_access, got: %v", err)
	}
}

func TestValidateMessageSequence_DBWriteWithKindDBOK(t *testing.T) {
	step := msgSeqStep(MessageSequenceItem{
		ID:      "build-queue",
		Type:    "user_message",
		Kind:    "db",
		Message: "Build the action queue and write it to db/action_queue.json",
	})
	if err := validateMessageSequenceStepFieldsTyped(step); err != nil {
		t.Fatalf("expected no error with kind=db, got: %v", err)
	}
}

func TestMessageSequenceItemReportedFailure(t *testing.T) {
	tests := []struct {
		name       string
		summary    string
		wantFailed bool
		wantReason string
	}{
		{name: "failed with reason", summary: "did the work\nSTATUS: FAILED — cannot write db/x.json: no db write access", wantFailed: true, wantReason: "cannot write db/x.json: no db write access"},
		{name: "failed no space", summary: "STATUS:FAILED - blocked by folder guard", wantFailed: true, wantReason: "blocked by folder guard"},
		{name: "completed is not failed", summary: "all done\nSTATUS: COMPLETED", wantFailed: false},
		{name: "prose mention of failed is not the marker", summary: "the previous attempt failed but I recovered", wantFailed: false},
		{name: "no status marker", summary: "wrote the queue and validated it", wantFailed: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, failed := messageSequenceItemReportedFailure(tc.summary)
			if failed != tc.wantFailed {
				t.Fatalf("failed=%v, want %v (summary=%q)", failed, tc.wantFailed, tc.summary)
			}
			if tc.wantFailed && reason != tc.wantReason {
				t.Fatalf("reason=%q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestValidateMessageSequence_ReadOnlyDBMentionOK(t *testing.T) {
	step := msgSeqStep(MessageSequenceItem{
		ID:      "review",
		Type:    "user_message",
		Message: "Read db/action_queue.json and double-check the entries are valid",
	})
	if err := validateMessageSequenceStepFieldsTyped(step); err != nil {
		t.Fatalf("read-only db mention should not require write access, got: %v", err)
	}
}
