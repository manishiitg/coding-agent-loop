package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
)

// POST /api/child/message — run one turn of Child Mode tutoring through the
// selected engine. Same agentic runtime as the parent, but with the child tutor
// prompt and the sandboxed child shell — scoped to exactly the current
// activity folder (see child_workspace.go / shell_tool.go).
func handleChildMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req parentMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, parentMessageResponse{Error: "messages are required"})
		return
	}

	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	if s.Engine == "" {
		writeJSON(w, http.StatusBadRequest, parentMessageResponse{Error: "no learning engine is selected"})
		return
	}
	if s.Child == nil {
		writeJSON(w, http.StatusBadRequest, parentMessageResponse{Error: "setup is not complete"})
		return
	}
	// The child's conversation lives INSIDE the current activity folder
	// (conversation.json) — one per activity, resumed whenever that same
	// activity is reopened. There is no child session without one.
	activityDir := currentActivityDir()
	if activityDir == "" {
		writeJSON(w, http.StatusBadRequest, parentMessageResponse{Error: "no activity has been handed off yet"})
		return
	}

	workDir := filepath.Join(familyDataDir(), "workspace")

	provider, ok := engineToProvider(s.Engine)
	if !ok {
		// Plain-completion fallback (no tools) for engines not yet mapped.
		reply, err := enginedetect.Chat(r.Context(), s.Engine, "", workDir, childSystemPrompt(s.Child, s.ParentLabel, activityDir), req.Messages)
		if err != nil {
			writeJSON(w, http.StatusOK, parentMessageResponse{Error: friendlyTurnError(err)})
			return
		}
		writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
		return
	}

	// Recorder captures open_file invocations so the child UI can show the file
	// on the right, mirroring the parent flow (chat.go).
	var evMu sync.Mutex
	var events []toolEvent
	// TEMPORARY: records every tool call this turn — see tool_call_debug.go.
	var debugMu sync.Mutex
	var debugCalls []debugToolCall
	// Recorder for show_scene — at most one scene per turn is shown (the
	// latest call wins if the model calls it more than once).
	var sceneMu sync.Mutex
	var scene string
	childOpenFile := agentsession.Tool{
		Name: "open_file",
		Description: "Show a lesson, worksheet, or one of your own saved pages to " + childDisplayName(s.Child) +
			" on the right side of their screen. Call this when you want them to look at a specific study sheet, " +
			"practice test, or their own work while you talk about it. Pass the workspace-relative path.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "workspace-relative path to the file to display"},
			},
			"required": []string{"path"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			p, _ := args["path"].(string)
			p = strings.TrimSpace(p)
			if !childCanSee(p) {
				return "", fmt.Errorf("that file isn't available on the child's screen")
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "open_file", Path: p})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","opened":%q}`, p), nil
		},
	}

	// Same suggest_actions tool as the parent (chat.go) — already in mcpagent's
	// bridgeTools allowlist under that name, so no bridge change is needed here.
	var sugMu sync.Mutex
	var suggestions []suggestion
	childSuggestActions := agentsession.Tool{
		Name: "suggest_actions",
		Description: "Offer " + childDisplayName(s.Child) + " 2–4 quick-reply buttons based on the conversation so far — " +
			"call this at the END of every turn. Each has a short, simple button label (2–4 words, e.g. \"Give me a hint\", " +
			"\"Check my answer\", \"I'm stuck\") and the exact message sent as if " + childDisplayName(s.Child) + " typed it when clicked. " +
			"Make them colorful and fun: pick a fitting emoji and a tone for each (hint, success, fun, celebrate, stuck, or neutral) " +
			"— these drive the button's color. You may also add a tiny optional decorative html snippet (plain inline-styled text/spans " +
			"only, e.g. <span style=\"color:#e0a51c\">✨ nice!</span>) for extra flair — it's shown for decoration only, never clickable itself.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"actions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"label":   map[string]interface{}{"type": "string", "description": "short button text, 2–4 words"},
							"message": map[string]interface{}{"type": "string", "description": "the message sent as the child when clicked"},
							"emoji":   map[string]interface{}{"type": "string", "description": "one emoji that fits this action, e.g. 💡"},
							"tone": map[string]interface{}{
								"type":        "string",
								"description": "which color this button should be",
								"enum":        []string{"hint", "success", "fun", "celebrate", "stuck", "neutral"},
							},
							"html": map[string]interface{}{"type": "string", "description": "optional tiny decorative HTML/inline-CSS fragment shown alongside the label, purely decorative"},
						},
						"required": []string{"label", "message"},
					},
				},
			},
			"required": []string{"actions"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			raw, _ := args["actions"].([]interface{})
			allowedTones := map[string]bool{"hint": true, "success": true, "fun": true, "celebrate": true, "stuck": true, "neutral": true}
			out := []suggestion{}
			for _, it := range raw {
				m, ok := it.(map[string]interface{})
				if !ok {
					continue
				}
				label, _ := m["label"].(string)
				msg, _ := m["message"].(string)
				emoji, _ := m["emoji"].(string)
				tone, _ := m["tone"].(string)
				htmlSnippet, _ := m["html"].(string)
				label, msg = strings.TrimSpace(label), strings.TrimSpace(msg)
				emoji, tone = strings.TrimSpace(emoji), strings.TrimSpace(tone)
				htmlSnippet = strings.TrimSpace(htmlSnippet)
				if label == "" || msg == "" {
					continue
				}
				if !allowedTones[tone] {
					tone = "neutral"
				}
				if len(htmlSnippet) > 400 {
					htmlSnippet = "" // decorative only — drop anything unreasonably large rather than truncate mid-tag
				}
				out = append(out, suggestion{Label: label, Message: msg, Emoji: emoji, Tone: tone, HTML: htmlSnippet})
				if len(out) >= 4 {
					break
				}
			}
			sugMu.Lock()
			suggestions = out
			sugMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","count":%d}`, len(out)), nil
		},
	}

	celebrate := agentsession.Tool{
		Name: "celebrate",
		Description: "Award " + childDisplayName(s.Child) + " 1-3 stars for genuine effort or progress, right now, in the moment — " +
			"finishing a test, working through something hard, a nice improvement, real persistence. This is shown live in the chat " +
			"as it happens; it is not tracked as a running total anywhere. Call this in the moment it happens, not routinely. Never " +
			"for just showing up or a single easy answer — save it for effort that actually deserves it, or it stops meaning anything.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"stars":  map[string]interface{}{"type": "integer", "description": "how many stars, 1 to 3"},
				"reason": map[string]interface{}{"type": "string", "description": "one short, warm sentence about what earned it"},
			},
			"required": []string{"stars", "reason"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			starsF, _ := args["stars"].(float64)
			stars := int(starsF)
			if stars < 1 {
				stars = 1
			}
			if stars > 3 {
				stars = 3
			}
			reason, _ := args["reason"].(string)
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return "", fmt.Errorf("reason is required")
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "celebrate", Stars: stars, Reason: reason})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","stars_awarded":%d}`, stars), nil
		},
	}

	// Serialize on the shared agent-turn lock (parent + child share global MCP env).
	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), turnTimeout)
	defer cancel()

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider: provider,
		// CHILD Mode uses the fast tier (Claude Code -> haiku; codex-cli ->
		// gpt-5.6-terra, see lowTierModelID) for short, one-at-a-time tutoring
		// turns, paired with LOW reasoning effort — latency matters far more
		// than deep reasoning for this kind of short back-and-forth turn.
		ModelID:         lowTierModelID(provider),
		ReasoningEffort: "low",
		WorkingDir:      workDir,
		SystemPrompt:    childSystemPrompt(s.Child, s.ParentLabel, activityDir),
		// Stable SessionID reuses the warm tmux within this process; SessionHandle
		// restores the coding agent's `--resume` state across restarts (loaded from
		// disk) so context survives a restart without replaying the transcript.
		SessionID:                 activityDir,
		SessionHandle:             loadSessionHandle("child", activityDir, provider),
		BridgeRoutingInstructions: bridgeRoutingInstructions(),
		StreamCallback: func(text string) {
			statusHubs.publishDelta("child:"+activityDir, text)
		},
		Tools: withToolCallDebug(&debugMu, &debugCalls, "child:"+activityDir, withLiveStatus("child:"+activityDir, []agentsession.Tool{
			childShellTool(), childOpenFile, childSuggestActions, celebrate, notifyTool(), childDiffPatchWorkspaceFileTool(), childReadImageTool(s.Engine),
			childShowSceneTool(func(html string) {
				sceneMu.Lock()
				scene = html
				sceneMu.Unlock()
			}),
		})),
	})
	if err != nil {
		msg := friendlyTurnError(err)
		persistConversation("child", activityDir, withReply(req.Messages, msg))
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg})
		return
	}
	defer sess.Close()

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}
	if suffix := pendingChildUploadSuffix(); suffix != "" && len(history) > 0 {
		history[len(history)-1].Text += suffix
	}

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		// Persist the turn even on failure — see chat.go's parent handler for why.
		msg := friendlyTurnError(err)
		persistConversation("child", activityDir, withReply(req.Messages, msg))
		debugMu.Lock()
		debugOut := append([]debugToolCall(nil), debugCalls...)
		debugMu.Unlock()
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg, DebugCalls: debugOut})
		return
	}
	saveSessionHandle("child", activityDir, sess.Handle())
	evMu.Lock()
	evs := events
	evMu.Unlock()
	sugMu.Lock()
	sug := suggestions
	sugMu.Unlock()
	debugMu.Lock()
	debugOut := append([]debugToolCall(nil), debugCalls...)
	debugMu.Unlock()
	sceneMu.Lock()
	sceneOut := scene
	sceneMu.Unlock()

	toSave := withReply(req.Messages, reply)
	if cel := findCelebrateEvent(evs); cel != nil {
		// Persist the celebration alongside the reply so a reloaded transcript
		// replays the star moment exactly where it happened, not just the text.
		toSave = append(toSave, enginedetect.ChatMessage{Role: "tool", Tool: "celebrate", Stars: cel.Stars, Reason: cel.Reason})
	}
	if sceneOut != "" {
		// Persist the scene alongside the reply so reloading mid-conversation
		// replays it exactly where it was shown, not just the reply text.
		toSave = append(toSave, enginedetect.ChatMessage{Role: "tool", Tool: "scene", HTML: sceneOut})
	}
	persistConversation("child", activityDir, toSave)

	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply, ToolEvents: evs, Suggestions: sug, DebugCalls: debugOut, Scene: sceneOut})
}

func findCelebrateEvent(evs []toolEvent) *toolEvent {
	for i := range evs {
		if evs[i].Tool == "celebrate" {
			return &evs[i]
		}
	}
	return nil
}
