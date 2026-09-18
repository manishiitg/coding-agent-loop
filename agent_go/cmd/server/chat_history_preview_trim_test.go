package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTrimChatHistoryConversationForPreviewKeepsTailAndDropsBulk(t *testing.T) {
	var history []map[string]string
	for i := 0; i < 50; i++ {
		history = append(history, map[string]string{"Role": "ai", "id": string(rune('a' + i%26))})
	}
	doc := map[string]interface{}{
		"session_id":           "s1",
		"conversation_history": history,
		"ui_events":            make([]map[string]string, 200),
		"terminal_snapshots":   []map[string]string{{"content": "pane"}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	out := trimChatHistoryConversationForPreview(raw, 10)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("trimmed document must stay parseable: %v", err)
	}
	if _, ok := got["ui_events"]; ok {
		t.Error("ui_events must be dropped -- it is the bulk of the file and a preview never reads it")
	}
	if _, ok := got["terminal_snapshots"]; ok {
		t.Error("terminal_snapshots must be dropped")
	}
	if _, ok := got["session_id"]; !ok {
		t.Error("unrelated fields must be preserved")
	}

	var trimmed []map[string]string
	if err := json.Unmarshal(got["conversation_history"], &trimmed); err != nil {
		t.Fatal(err)
	}
	if len(trimmed) != 10 {
		t.Fatalf("kept %d messages, want the last 10", len(trimmed))
	}
	// Must be the TAIL: a preview shows how the conversation ended.
	if trimmed[len(trimmed)-1]["id"] != history[len(history)-1]["id"] {
		t.Error("kept the wrong end of the conversation")
	}
}

// A preview that is too large is a performance problem; a preview that fails to
// load is a broken panel. Malformed input must pass through untouched.
func TestTrimChatHistoryConversationForPreviewFallsBackOnBadJSON(t *testing.T) {
	raw := []byte(`{"conversation_history": [ this is not json`)
	if got := string(trimChatHistoryConversationForPreview(raw, 5)); got != string(raw) {
		t.Errorf("malformed input must be returned unchanged, got %q", got)
	}
}

func TestTrimChatHistoryConversationForPreviewKeepsShortConversationsWhole(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"conversation_history": []map[string]string{{"Role": "human"}, {"Role": "ai"}},
	})
	var got map[string][]map[string]string
	if err := json.Unmarshal(trimChatHistoryConversationForPreview(raw, 10), &got); err != nil {
		t.Fatal(err)
	}
	if len(got["conversation_history"]) != 2 {
		t.Errorf("kept %d, want both messages", len(got["conversation_history"]))
	}
}

func TestProjectChatHistoryConversationForResumeKeepsReadableAssistantUpdates(t *testing.T) {
	message := func(role, text string) map[string]interface{} {
		return map[string]interface{}{
			"Role":  role,
			"Parts": []map[string]string{{"Text": text}},
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"session_id": "resume-1",
		"conversation_history": []map[string]interface{}{
			message("system", "large system prompt"),
			message("human", "first question"),
			message("ai", "I will inspect it."),
			message("ai", `[Previous tool call: exec({"input":"secret trace"})]`),
			message("tool", "tool output"),
			message("ai", "First final answer."),
			message("human", "second question"),
			message("ai", "Second final answer."),
		},
		"ui_events":          []map[string]string{{"type": "tool_call_start"}},
		"terminal_snapshots": []map[string]string{{"content": "raw pane"}},
	})

	out := projectChatHistoryConversationForResume(raw, 20)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ui_events"]; ok {
		t.Error("resume projection must not return trace events")
	}
	if _, ok := got["terminal_snapshots"]; ok {
		t.Error("resume projection must not return terminal snapshots")
	}
	var history []struct {
		Role  string `json:"Role"`
		Parts []struct {
			Text string `json:"Text"`
		} `json:"Parts"`
	}
	if err := json.Unmarshal(got["conversation_history"], &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 5 {
		t.Fatalf("got %d projected messages, want both assistant updates in the first turn", len(history))
	}
	texts := make([]string, 0, len(history))
	for _, item := range history {
		texts = append(texts, item.Parts[0].Text)
	}
	want := []string{"first question", "I will inspect it.", "First final answer.", "second question", "Second final answer."}
	for index := range want {
		if texts[index] != want[index] {
			t.Fatalf("message %d = %q, want %q", index, texts[index], want[index])
		}
	}
}

func TestProjectChatHistoryConversationForResumeKeepsAllTextUpdatesInOneToolHeavyTurn(t *testing.T) {
	message := func(role, text string) map[string]interface{} {
		return map[string]interface{}{
			"Role":  role,
			"Parts": []map[string]string{{"Text": text}},
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"conversation_history": []map[string]interface{}{
			message("human", "review this"),
			message("ai", "I will inspect the run."),
			{"Role": "ai", "Parts": []map[string]interface{}{{"Type": "function", "FunctionCall": map[string]string{"Name": "read_file"}}}},
			message("tool", "tool output"),
			message("ai", "The first finding is confirmed."),
			message("ai", "Done — here is the final result."),
		},
	})

	var got struct {
		History []json.RawMessage `json:"conversation_history"`
	}
	if err := json.Unmarshal(projectChatHistoryConversationForResume(raw, 100), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.History) != 4 {
		t.Fatalf("got %d messages, want user plus all three assistant updates", len(got.History))
	}
	texts := make([]string, 0, len(got.History))
	for _, item := range got.History {
		_, text := chatHistoryMessageRoleAndText(item)
		texts = append(texts, text)
	}
	want := []string{"review this", "I will inspect the run.", "The first finding is confirmed.", "Done — here is the final result."}
	for index := range want {
		if texts[index] != want[index] {
			t.Fatalf("message %d = %q, want %q", index, texts[index], want[index])
		}
	}
}

func TestAttachChatHistoryUIEventsForResumeKeepsOnlyFormattedTrace(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"session_id": "schedule-1",
		"conversation_history": []map[string]interface{}{
			{"Role": "human", "Parts": []map[string]string{{"Text": "run it"}}},
		},
		"ui_events": []map[string]interface{}{
			{"id": "prompt", "type": "user_message", "timestamp": "2026-08-21T01:00:00Z"},
			{"id": "tool", "type": "tool_call_start", "timestamp": "2026-08-21T01:00:01Z"},
			{"id": "answer", "type": "unified_completion", "timestamp": "2026-08-21T01:00:02Z"},
			{"id": "stream", "type": "streaming_chunk", "timestamp": "2026-08-21T01:00:03Z"},
			{"id": "prompt", "type": "system_prompt", "timestamp": "2026-08-21T01:00:04Z"},
		},
	})
	projected := projectChatHistoryConversationForResume(raw, 20)
	out := attachChatHistoryUIEventsForResume(projected, chatHistoryUIEvents(raw))
	var got struct {
		Events []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"ui_events"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 3 {
		t.Fatalf("kept %d events, want user/tool/final only: %#v", len(got.Events), got.Events)
	}
	if got.Events[0].Type != "user_message" || got.Events[1].Type != "tool_call_start" || got.Events[2].Type != "unified_completion" {
		t.Fatalf("unexpected restored trace: %#v", got.Events)
	}
}

func TestProjectChatHistoryConversationForResumeLimitsByUserTurn(t *testing.T) {
	message := func(role, text string) map[string]interface{} {
		return map[string]interface{}{"role": role, "parts": []map[string]string{{"text": text}}}
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"conversation_history": []map[string]interface{}{
			message("user", "one"), message("assistant", "answer one"),
			message("user", "two"), message("assistant", "answer two"),
			message("user", "three"), message("assistant", "answer three"),
		},
	})

	var got struct {
		History []json.RawMessage `json:"conversation_history"`
	}
	if err := json.Unmarshal(projectChatHistoryConversationForResume(raw, 2), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.History) != 4 {
		t.Fatalf("got %d messages, want the final two turns", len(got.History))
	}
	_, firstText := chatHistoryMessageRoleAndText(got.History[0])
	if firstText != "two" {
		t.Fatalf("first retained turn = %q, want two", firstText)
	}
}

func TestProjectChatHistoryConversationForResumePageMovesCursorTowardEarlierTurns(t *testing.T) {
	message := func(role, text string) map[string]interface{} {
		return map[string]interface{}{"role": role, "parts": []map[string]string{{"text": text}}}
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"conversation_history": []map[string]interface{}{
			message("user", "one"), message("assistant", "answer one"),
			message("user", "two"), message("assistant", "answer two"),
			message("user", "three"), message("assistant", "answer three"),
		},
	})

	var newest struct {
		History    []json.RawMessage `json:"conversation_history"`
		Pagination struct {
			HasMore    bool `json:"has_more"`
			NextOffset int  `json:"next_offset"`
			StartTurn  int  `json:"start_turn"`
			TotalTurns int  `json:"total_turns"`
		} `json:"history_pagination"`
	}
	if err := json.Unmarshal(projectChatHistoryConversationForResumePage(raw, 2, 0), &newest); err != nil {
		t.Fatal(err)
	}
	if len(newest.History) != 4 {
		t.Fatalf("newest page has %d messages, want two turns", len(newest.History))
	}
	_, text := chatHistoryMessageRoleAndText(newest.History[0])
	if text != "two" {
		t.Fatalf("newest page starts %q, want two", text)
	}
	if !newest.Pagination.HasMore || newest.Pagination.NextOffset != 2 || newest.Pagination.StartTurn != 1 || newest.Pagination.TotalTurns != 3 {
		t.Fatalf("unexpected newest pagination: %+v", newest.Pagination)
	}

	var older struct {
		History    []json.RawMessage `json:"conversation_history"`
		Pagination struct {
			HasMore    bool `json:"has_more"`
			NextOffset int  `json:"next_offset"`
			StartTurn  int  `json:"start_turn"`
		} `json:"history_pagination"`
	}
	if err := json.Unmarshal(projectChatHistoryConversationForResumePage(raw, 2, newest.Pagination.NextOffset), &older); err != nil {
		t.Fatal(err)
	}
	if len(older.History) != 2 {
		t.Fatalf("older page has %d messages, want one turn", len(older.History))
	}
	_, text = chatHistoryMessageRoleAndText(older.History[0])
	if text != "one" {
		t.Fatalf("older page starts %q, want one", text)
	}
	if older.Pagination.HasMore || older.Pagination.NextOffset != 3 || older.Pagination.StartTurn != 0 {
		t.Fatalf("unexpected older pagination: %+v", older.Pagination)
	}
}

func TestProjectChatHistoryConversationForResumePageHidesProviderTaskNotifications(t *testing.T) {
	message := func(role, text string) map[string]interface{} {
		return map[string]interface{}{"role": role, "parts": []map[string]string{{"text": text}}}
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"conversation_history": []map[string]interface{}{
			message("user", "real question"),
			message("assistant", "working"),
			message("human", "<task-notification>\n<task-id>abc</task-id>\n<status>completed</status>\n</task-notification>"),
			message("assistant", "finished answer"),
		},
	})

	var got struct {
		History    []json.RawMessage `json:"conversation_history"`
		Pagination struct {
			TotalTurns int `json:"total_turns"`
		} `json:"history_pagination"`
	}
	if err := json.Unmarshal(projectChatHistoryConversationForResumePage(raw, 10, 0), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pagination.TotalTurns != 1 {
		t.Fatalf("total turns = %d, want 1", got.Pagination.TotalTurns)
	}
	if len(got.History) != 3 {
		t.Fatalf("projected history has %d messages, want user plus two assistant updates", len(got.History))
	}
	for _, item := range got.History {
		_, text := chatHistoryMessageRoleAndText(item)
		if strings.Contains(text, "<task-notification>") {
			t.Fatalf("provider task notification leaked into projected history: %q", text)
		}
	}
}

func TestChatHistoryResumeSnapshotPathIsASeparateSibling(t *testing.T) {
	got := chatHistoryResumeSnapshotPath("Workflow/reviewer/builder/conversation/2026-09-18/session-abc-conversation.json")
	want := "Workflow/reviewer/builder/conversation/2026-09-18/session-abc-resume.json"
	if got != want {
		t.Fatalf("resume snapshot path = %q, want %q", got, want)
	}
}

func TestBoundChatHistoryResumeSnapshotNeverExceedsTwoMiB(t *testing.T) {
	huge := strings.Repeat("x", 3*1024*1024)
	raw, err := json.Marshal(map[string]interface{}{
		"session_id": "large",
		"conversation_history": []map[string]interface{}{
			{"Role": "human", "Parts": []map[string]string{{"Text": "latest question"}}},
			{"Role": "ai", "Parts": []map[string]string{{"Text": huge}}},
		},
		"ui_events": []map[string]string{{"type": "tool_call_end", "data": huge}},
	})
	if err != nil {
		t.Fatal(err)
	}

	bounded := boundChatHistoryResumeSnapshot(raw)
	if len(bounded) > maxChatHistoryResumeSnapshotBytes {
		t.Fatalf("bounded resume snapshot is %d bytes, limit is %d", len(bounded), maxChatHistoryResumeSnapshotBytes)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(bounded, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["ui_events"]; exists {
		t.Fatal("oversized resume snapshot retained tool UI events")
	}
	if !strings.Contains(string(decoded["conversation_history"]), "latest question") {
		t.Fatal("bounded resume snapshot lost the latest user message")
	}
}

func TestPreviewProjectionNeverExceedsTwoMiB(t *testing.T) {
	huge := strings.Repeat("x", 3*1024*1024)
	raw, err := json.Marshal(map[string]interface{}{
		"session_id": "large-preview",
		"conversation_history": []map[string]interface{}{
			{"Role": "human", "Parts": []map[string]string{{"Text": "latest question"}}},
			{"Role": "ai", "Parts": []map[string]string{{"Text": huge}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	preview := boundChatHistoryResumeSnapshot(trimChatHistoryConversationForPreview(raw, 10))
	if len(preview) > maxChatHistoryResumeSnapshotBytes {
		t.Fatalf("bounded preview is %d bytes, limit is %d", len(preview), maxChatHistoryResumeSnapshotBytes)
	}
}
