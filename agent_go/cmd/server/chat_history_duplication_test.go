package server

import (
	"fmt"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func chatMessage(role llmtypes.ChatMessageType, text string) llmtypes.MessageContent {
	return llmtypes.MessageContent{Role: role, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: text}}}
}

// RTS 2026-09-25: a builder chat reached 37,882 rows (1,407 unique) and 18
// leading system prompts. Each turn end merged the file with the agent's
// in-memory history; side writers (live input, structured completion) had
// added rows only the file held, so the memory was never a prefix of the file
// and the merge appended the whole history again. The file must grow only by
// the new rows, and keep one (the newest) system prompt.
func TestTurnEndMergeNeverReappendsHistory(t *testing.T) {
	file := []llmtypes.MessageContent{chatMessage(llmtypes.ChatMessageTypeSystem, "prompt @ turn 0")}
	memory := []llmtypes.MessageContent{chatMessage(llmtypes.ChatMessageTypeSystem, "prompt @ turn 0")}
	for turn := 1; turn <= 20; turn++ {
		ask := chatMessage(llmtypes.ChatMessageTypeHuman, fmt.Sprintf("request %d", turn))
		answer := chatMessage(llmtypes.ChatMessageTypeAI, fmt.Sprintf("answer %d", turn))
		// Live input is recorded into the file as soon as it is delivered.
		file = append(file, ask)
		// Every few turns a side writer adds a row memory never sees.
		if turn%3 == 0 {
			file = append(file, chatMessage(llmtypes.ChatMessageTypeAI, fmt.Sprintf("structured completion %d", turn)))
		}
		memory[0] = chatMessage(llmtypes.ChatMessageTypeSystem, fmt.Sprintf("prompt @ turn %d", turn))
		memory = append(memory, ask, answer)
		file = mergeRestoredChatHistory(file, memory)
	}
	want := 1 + 20*2 + 6 // one system prompt, 20 exchanges, 6 side rows
	if len(file) != want {
		t.Fatalf("file has %d rows after 20 turns, want %d", len(file), want)
	}
	systems := 0
	for _, message := range file {
		if message.Role == llmtypes.ChatMessageTypeSystem {
			systems++
		}
	}
	if systems != 1 || file[0].Parts[0].(llmtypes.TextContent).Text != "prompt @ turn 20" {
		t.Fatalf("want exactly the newest system prompt first, got %d system rows starting %v", systems, file[0])
	}
	for i := 1; i <= 20; i++ {
		seen := 0
		for _, message := range file {
			if text, ok := message.Parts[0].(llmtypes.TextContent); ok && text.Text == fmt.Sprintf("request %d", i) {
				seen++
			}
		}
		if seen != 1 {
			t.Fatalf("request %d appears %d times", i, seen)
		}
	}
	// Saving the same memory again must not change the file.
	again := mergeRestoredChatHistory(file, memory)
	if len(again) != len(file) {
		t.Fatalf("repeat save grew the file from %d to %d rows", len(file), len(again))
	}
}

// A native-continuation provider returns only the new exchange: it must be
// appended once, not dropped.
func TestTurnEndMergeAppendsUnrelatedNewExchangeOnce(t *testing.T) {
	file := []llmtypes.MessageContent{chatMessage(llmtypes.ChatMessageTypeHuman, "old"), chatMessage(llmtypes.ChatMessageTypeAI, "old answer")}
	fresh := []llmtypes.MessageContent{chatMessage(llmtypes.ChatMessageTypeHuman, "new"), chatMessage(llmtypes.ChatMessageTypeAI, "new answer")}
	merged := mergeRestoredChatHistory(file, fresh)
	if len(merged) != 4 || merged[2].Parts[0].(llmtypes.TextContent).Text != "new" {
		t.Fatalf("new exchange not appended once: %v", merged)
	}
	if again := mergeRestoredChatHistory(merged, fresh); len(again) != 4 {
		t.Fatalf("repeat save duplicated the new exchange: %d rows", len(again))
	}
}
