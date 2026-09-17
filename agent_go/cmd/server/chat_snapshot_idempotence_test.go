package server

import (
	"context"
	"encoding/json"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSnapshotRepeatedPartialToolTailDoesNotDuplicateConversation(t *testing.T) {
	const path = "Workflow/test/builder/conversation/repeated.json"
	human := `{"Role":"human","Parts":[{"Text":"run once"}]}`
	callA := `{"Role":"ai","Parts":[{"ToolCall":{"ID":"call-a","FunctionCall":{"Name":"first","Arguments":"{}"}}}]}`
	callB := `{"Role":"ai","Parts":[{"ToolCall":{"ID":"call-b","FunctionCall":{"Name":"second","Arguments":"{}"}}}]}`
	answer := `{"Role":"ai","Parts":[{"Text":"done"}]}`
	record := func(rows ...string) string {
		history := make([]json.RawMessage, len(rows))
		for i, row := range rows {
			history[i] = json.RawMessage(row)
		}
		raw, _ := json.Marshal(map[string]interface{}{"session_id": "chat", "user_id": "alice", "conversation_history": history})
		return string(raw)
	}
	original := record(human, callA, callB, answer)
	incoming := record(human, callB, answer)
	workspace := &mockWorkspaceAPI{files: map[string]string{path: original}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	var want map[string]interface{}
	_ = json.Unmarshal([]byte(original), &want)
	for i := 0; i < 4; i++ {
		if err := persistRawConversationSnapshot(context.Background(), path, incoming); err != nil {
			t.Fatal(err)
		}
		var got map[string]interface{}
		_ = json.Unmarshal([]byte(workspace.files[path]), &got)
		if !reflect.DeepEqual(got["conversation_history"], want["conversation_history"]) {
			t.Fatalf("save %d duplicated/corrupted a tool tail: %s", i, workspace.files[path])
		}
	}
}

func TestSnapshotDistinctToolRowsAreNotAliasedByEmptyText(t *testing.T) {
	previous := map[string]interface{}{"conversation_history": []json.RawMessage{
		json.RawMessage(`{"Role":"human","Parts":[{"Text":"run"}]}`),
		json.RawMessage(`{"Role":"ai","Parts":[{"ID":"tool-a","Type":"function","FunctionCall":{"Name":"a","Arguments":"{}"}}]}`),
	}}
	incoming := map[string]interface{}{"conversation_history": []json.RawMessage{
		json.RawMessage(`{"Role":"human","Parts":[{"Text":"run"}]}`),
		json.RawMessage(`{"Role":"ai","Parts":[{"ID":"tool-b","Type":"function","FunctionCall":{"Name":"b","Arguments":"{}"}}]}`),
	}}
	result, err := mergeChatConversationSnapshots(previous, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("distinct tool IDs collapsed: %s", result)
	}
	replay, err := mergeChatConversationSnapshots(map[string]interface{}{"conversation_history": result}, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, replay) {
		t.Fatal("repeated snapshot changed merged raw identities")
	}
}

func TestSnapshotLegitimateRepeatedMessagesKeepMultiplicity(t *testing.T) {
	h := json.RawMessage(`{"Role":"human","Parts":[{"Text":"again"}]}`)
	a := json.RawMessage(`{"Role":"ai","Parts":[{"Text":"done"}]}`)
	old := map[string]interface{}{"conversation_history": []json.RawMessage{h, a}}
	next := map[string]interface{}{"conversation_history": []json.RawMessage{h, a, h, a}}
	got, err := mergeChatConversationSnapshots(old, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("legitimate repeat erased: %s", got)
	}
	for i := 0; i < 3; i++ {
		got, err = mergeChatConversationSnapshots(map[string]interface{}{"conversation_history": got}, next)
		if err != nil || len(got) != 4 {
			t.Fatalf("replay grew: %d %v", len(got), err)
		}
	}
}

func TestSnapshotRetainedTailNeverPrecedesOlderCanonicalToolTurn(t *testing.T) {
	const path = "Workflow/test/builder/conversation/retained-tail.json"
	row := func(role, text string) json.RawMessage {
		raw, _ := json.Marshal(map[string]interface{}{"Role": role, "Parts": []map[string]string{{"Text": text}}})
		return raw
	}
	tool := func(id string) json.RawMessage {
		raw, _ := json.Marshal(map[string]interface{}{"Role": "ai", "Parts": []map[string]interface{}{{"ID": id, "Type": "function", "FunctionCall": map[string]string{"Name": "inspect", "Arguments": "{}"}}}})
		return raw
	}
	old := []json.RawMessage{row("human", "old request"), tool("old"), row("ai", "old answer"), row("human", "recent request"), tool("recent"), row("ai", "recent answer")}
	next := []json.RawMessage{row("human", "recent request"), tool("recent"), row("ai", "recent answer"), row("human", "new request"), tool("new"), row("ai", "new answer")}
	record := func(history []json.RawMessage) string {
		raw, _ := json.Marshal(map[string]interface{}{"session_id": "chat", "user_id": "alice", "conversation_history": history})
		return string(raw)
	}
	workspace := &mockWorkspaceAPI{files: map[string]string{path: record(old)}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	want := append(append([]json.RawMessage{}, old...), next[3:]...)
	for i := 0; i < 3; i++ {
		if err := persistRawConversationSnapshot(context.Background(), path, record(next)); err != nil {
			t.Fatal(err)
		}
		var got map[string]interface{}
		_ = json.Unmarshal([]byte(workspace.files[path]), &got)
		history, _ := builderConversationRawHistory(got)
		if len(history) != len(want) {
			t.Fatalf("save %d duplicated prior human/answer: got %d want %d", i, len(history), len(want))
		}
		for j := range want {
			g, _ := chatSnapshotMessageKey(history[j])
			w, _ := chatSnapshotMessageKey(want[j])
			if g != w {
				t.Fatalf("save %d row %d reordered/corrupted", i, j)
			}
		}
	}
}

func TestSnapshotFullHistoryWriterRetainedTailIsIdempotent(t *testing.T) {
	const path = "Workflow/test/builder/conversation/full-tail.json"
	var old, next []llmtypes.MessageContent
	if err := json.Unmarshal([]byte(`[{"Role":"human","Parts":[{"Text":"old"}]},{"Role":"ai","Parts":[{"ID":"old-tool","Type":"function","FunctionCall":{"Name":"inspect","Arguments":"{}"}}]},{"Role":"ai","Parts":[{"Text":"old answer"}]},{"Role":"human","Parts":[{"Text":"recent"}]},{"Role":"ai","Parts":[{"ID":"recent-tool","Type":"function","FunctionCall":{"Name":"inspect","Arguments":"{}"}}]},{"Role":"ai","Parts":[{"Text":"recent answer"}]}]`), &old); err != nil {
		t.Fatal(err)
	}
	next = append(next, old[3:]...)
	next = append(next, llmtypes.MessageContent{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "new request"}}})
	raw, _ := json.Marshal(map[string]interface{}{"session_id": "chat", "user_id": "alice", "conversation_history": old})
	workspace := &mockWorkspaceAPI{files: map[string]string{path: string(raw)}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	for i := 0; i < 3; i++ {
		(&StreamingAPI{}).persistChatConversationToPathWithTerminalSession("chat", "", "simple", "alice", next, nil, nil, path)
		var got builderConversationLog
		if err := json.Unmarshal([]byte(workspace.files[path]), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.ConversationHistory) != 7 {
			t.Fatalf("full save %d grew to %d rows", i, len(got.ConversationHistory))
		}
		if got.ConversationHistory[0].Parts[0].Text != "old" || got.ConversationHistory[3].Parts[0].Text != "recent" || got.ConversationHistory[6].Parts[0].Text != "new request" {
			t.Fatal("full writer reordered conversation")
		}
	}
}

func TestSnapshotEnrichmentDoesNotDuplicateMessage(t *testing.T) {
	old := map[string]interface{}{"conversation_history": []json.RawMessage{json.RawMessage(`{"Role":"human","Parts":[{"Text":"hello"}],"trace":"keep-me"}`)}}
	next := map[string]interface{}{"conversation_history": []json.RawMessage{json.RawMessage(`{"Role":"human","Parts":[{"Text":"hello"}]}`)}}
	got, err := mergeChatConversationSnapshots(old, next)
	if err != nil || len(got) != 1 {
		t.Fatalf("enrichment duplicated message: %d %v", len(got), err)
	}
	var row map[string]interface{}
	if err := json.Unmarshal(got[0], &row); err != nil || row["trace"] != "keep-me" {
		t.Fatal("canonical enrichment lost")
	}
}

func TestSnapshotNumericToolPayloadsRemainDistinct(t *testing.T) {
	a, _ := chatSnapshotMessageKey(json.RawMessage(`{"Role":"tool","Parts":[{"Content":9007199254740992}]}`))
	b, _ := chatSnapshotMessageKey(json.RawMessage(`{"Role":"tool","Parts":[{"Content":9007199254740993}]}`))
	if a == b {
		t.Fatal("distinct large tool values were rounded into the same key")
	}
}
