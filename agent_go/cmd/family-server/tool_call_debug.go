package main

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// debugToolCall is one raw tool invocation captured for the TEMPORARY tool-call
// visibility feature in the UI — lets the parent/child see exactly what the
// agent called each turn while codex-cli tool-calling reliability is under
// active investigation. Delete this file (and its LearningApp.tsx rendering)
// once that's no longer needed.
type debugToolCall struct {
	Tool string `json:"tool"`
	Args string `json:"args,omitempty"`
}

// withToolCallDebug wraps every tool's Handler to record its name plus a short
// args summary before running it — regardless of whether the tool also has a
// withLiveStatus label — AND publish it live on conversationID's SSE status
// stream (see statusHub.publishToolCall) so the UI shows each call the moment
// it happens instead of only once the whole turn finishes (a turn with many
// tool calls could otherwise look totally silent for minutes, then dump every
// debug bubble at once right at the end). Purely observational: never changes
// a tool's behavior, arguments, or return value.
func withToolCallDebug(mu *sync.Mutex, calls *[]debugToolCall, conversationID string, tools []agentsession.Tool) []agentsession.Tool {
	out := make([]agentsession.Tool, len(tools))
	for i, t := range tools {
		name := t.Name
		orig := t.Handler
		t.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
			argSummary := summarizeToolArgs(args)
			mu.Lock()
			*calls = append(*calls, debugToolCall{Tool: name, Args: argSummary})
			mu.Unlock()
			statusHubs.publishToolCall(conversationID, name, argSummary)
			return orig(ctx, args)
		}
		out[i] = t
	}
	return out
}

// summarizeToolArgs renders a tool call's arguments as compact JSON, truncated
// so a large payload (e.g. a shell command's full script) doesn't blow up the
// debug panel.
func summarizeToolArgs(args map[string]interface{}) string {
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	s := string(b)
	const maxLen = 200
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}
