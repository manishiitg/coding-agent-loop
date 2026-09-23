package server

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	pkgevents "github.com/manishiitg/mcpagent/events"
)

// referenceMergeLegacyChatTrace is the pre-optimization algorithm, kept
// verbatim so the indexed version is proven to produce identical output.
func referenceMergeLegacyChatTrace(history, trace []internalevents.Event, sessionID string) []internalevents.Event {
	if len(trace) == 0 {
		return history
	}
	anchors := make(map[int]int)
	nextTrace := len(trace) - 1
	for historyIndex := len(history) - 1; historyIndex >= 0; historyIndex-- {
		role, text := legacyEventCarrier(history[historyIndex])
		if role == "" || text == "" {
			continue
		}
		for traceIndex := nextTrace; traceIndex >= 0; traceIndex-- {
			traceRole, traceText := legacyEventCarrier(trace[traceIndex])
			if role == traceRole && text == traceText {
				anchors[traceIndex] = historyIndex
				nextTrace = traceIndex - 1
				break
			}
		}
	}
	imported := make([]internalevents.Event, 0, len(history)+len(trace))
	historyCursor := 0
	if len(anchors) == 0 {
		if len(history) > 0 && history[len(history)-1].Type == "streaming_chunk" {
			imported = append(imported, history[:len(history)-1]...)
			historyCursor = len(history) - 1
		} else {
			imported = append(imported, history...)
			historyCursor = len(history)
		}
	} else {
		firstAnchor := len(history)
		for _, historyIndex := range anchors {
			if historyIndex < firstAnchor {
				firstAnchor = historyIndex
			}
		}
		imported = append(imported, history[:firstAnchor]...)
		historyCursor = firstAnchor
	}
	for traceIndex, event := range trace {
		if anchor, ok := anchors[traceIndex]; ok {
			for historyCursor <= anchor {
				imported = append(imported, history[historyCursor])
				historyCursor++
			}
			continue
		}
		role, text := legacyEventCarrier(event)
		if role != "" && text != "" {
			found := false
			for _, candidate := range history {
				candidateRole, candidateText := legacyEventCarrier(candidate)
				if candidateRole == role && candidateText == text {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		if event.ID == "" {
			event.ID = legacyChatEventID(sessionID, traceIndex, "trace:"+event.Type, text)
		}
		imported = append(imported, event)
	}
	imported = append(imported, history[historyCursor:]...)
	return imported
}

func legacyTestUser(id, text string) internalevents.Event {
	return internalevents.Event{ID: id, Type: "user_message", Data: &pkgevents.AgentEvent{Type: "user_message",
		Data: internalevents.NewGenericEventData("user_message", map[string]interface{}{"content": text})}}
}

func legacyTestAssistant(id, text string) internalevents.Event {
	return internalevents.Event{ID: id, Type: "streaming_chunk", Data: &pkgevents.AgentEvent{Type: "streaming_chunk",
		Data: &pkgevents.StreamingChunkEvent{Source: "transcript", Content: text}}}
}

func legacyTestTool(id string) internalevents.Event {
	return internalevents.Event{ID: id, Type: "tool_call_start", Data: &pkgevents.AgentEvent{Type: "tool_call_start",
		Data: internalevents.NewGenericEventData("tool_call_start", map[string]interface{}{"tool_name": "exec"})}}
}

// Repeated texts ("continue", "ok") exercise the anchoring order, and missing
// IDs exercise the derived-ID path.
func randomLegacySession(rng *rand.Rand, turns int) ([]internalevents.Event, []internalevents.Event) {
	vocabulary := []string{"continue", "ok", "run the tests", "ship it", "why?", "done"}
	var history, trace []internalevents.Event
	for turn := 0; turn < turns; turn++ {
		question := vocabulary[rng.Intn(len(vocabulary))]
		answer := fmt.Sprintf("answer %d", rng.Intn(turns/2+1))
		history = append(history, legacyTestUser(fmt.Sprintf("h-u-%d", turn), question), legacyTestAssistant(fmt.Sprintf("h-a-%d", turn), answer))
		if rng.Intn(3) == 0 {
			continue // Turn absent from the bounded trace.
		}
		if rng.Intn(2) == 0 {
			trace = append(trace, legacyTestUser(fmt.Sprintf("t-u-%d", turn), question))
		}
		for tool := rng.Intn(3); tool > 0; tool-- {
			id := fmt.Sprintf("t-tool-%d-%d", turn, tool)
			if rng.Intn(4) == 0 {
				id = ""
			}
			trace = append(trace, legacyTestTool(id))
		}
		if rng.Intn(2) == 0 {
			trace = append(trace, legacyTestAssistant(fmt.Sprintf("t-a-%d", turn), answer))
		}
		if rng.Intn(5) == 0 {
			trace = append(trace, legacyTestUser(fmt.Sprintf("t-orphan-%d", turn), "trace only message"))
		}
	}
	return history, trace
}

func TestMergeLegacyChatTraceMatchesReferenceAlgorithm(t *testing.T) {
	rng := rand.New(rand.NewSource(352))
	for iteration := 0; iteration < 300; iteration++ {
		history, trace := randomLegacySession(rng, 1+rng.Intn(25))
		want := referenceMergeLegacyChatTrace(history, trace, "chat")
		got := mergeLegacyChatTrace(history, trace, "chat")
		if len(got) != len(want) {
			t.Fatalf("iteration %d: %d events, want %d", iteration, len(got), len(want))
		}
		for index := range want {
			if got[index].ID != want[index].ID || got[index].Type != want[index].Type {
				t.Fatalf("iteration %d event %d = %s/%s, want %s/%s", iteration, index, got[index].Type, got[index].ID, want[index].Type, want[index].ID)
			}
		}
	}
}

func TestMergeLegacyChatTraceLargeSessionIsNotQuadratic(t *testing.T) {
	// Worst case for the old scan: no trace carrier matches history, so every
	// history entry decoded every trace entry.
	var history, trace []internalevents.Event
	for index := 0; index < 3000; index++ {
		history = append(history, legacyTestUser(fmt.Sprintf("u-%d", index), fmt.Sprintf("question %d", index)),
			legacyTestAssistant(fmt.Sprintf("a-%d", index), fmt.Sprintf("answer %d", index)))
	}
	for index := 0; index < 3000; index++ {
		trace = append(trace, legacyTestTool(fmt.Sprintf("tool-%d", index)),
			legacyTestAssistant(fmt.Sprintf("trace-a-%d", index), fmt.Sprintf("unmatched %d", index)))
	}
	started := time.Now()
	got := mergeLegacyChatTrace(history, trace, "big")
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("merging 6k history x 6k trace events took %s", elapsed)
	}
	if len(got) != len(history)+len(trace) {
		t.Fatalf("merged %d events, want %d", len(got), len(history)+len(trace))
	}
}

func TestMigrateDurableChatsFromDiskSkipsBadFilesAndStillCompletes(t *testing.T) {
	docsRoot := t.TempDir()
	stateRoot := t.TempDir()
	dir := filepath.Join(docsRoot, "Workflow", "demo", "builder")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("session-truncated-conversation.json", `{"session_id":"truncated","conversation_history":[`)
	write("session-good-conversation.json", `{"session_id":"good","updated_at":"2026-01-02T00:00:00Z","conversation_history":[{"Role":"human","Parts":[{"Text":"hi"}]}],"ui_events":[{"type":"tool_call_start","data":{"type":"tool_call_start","data":{"tool_name":"exec"}}}]}`)
	unreadable := filepath.Join(docsRoot, "Workflow", "locked")
	if err := os.MkdirAll(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	if err := migrateDurableChatsFromDisk(docsRoot, stateRoot, false); err != nil {
		t.Fatalf("one malformed file or unreadable directory must not abort the run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "migrations", "chat-events-v2.done")); err != nil {
		t.Fatalf("marker not written after a completed scan: %v", err)
	}
	journal, err := internalevents.OpenSQLiteEventJournal(filepath.Join(stateRoot, "structured-chat-events-v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	page, err := journal.ReadPage("good", internalevents.DurableEventPageOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 {
		t.Fatal("the valid session was not imported")
	}
	for _, event := range page.Events {
		if event.ID == "" {
			t.Fatalf("imported event without a stable ID: %+v", event)
		}
	}
}

func TestMigrateDurableChatsFromDiskMissingDocsRootWritesNoMarker(t *testing.T) {
	stateRoot := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-mounted")
	err := migrateDurableChatsFromDisk(missing, stateRoot, false)
	if err == nil {
		t.Fatal("missing docs root reported success")
	}
	if _, statErr := os.Stat(filepath.Join(stateRoot, "migrations", "chat-events-v2.done")); !os.IsNotExist(statErr) {
		t.Fatalf("marker written for an unscanned docs root: %v", statErr)
	}
}
