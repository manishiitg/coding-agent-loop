package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
)

// POST /api/child/message — run one turn of Child Mode tutoring through the
// selected engine. Same agentic runtime as the parent, but with the child tutor
// prompt and the sandboxed child shell (shared/ + child/ only, never parent/).
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

	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(filepath.Join(workDir, "child", "attempts"), 0o700)

	provider, ok := engineToProvider(s.Engine)
	if !ok {
		// Plain-completion fallback (no tools) for engines not yet mapped.
		reply, err := enginedetect.Chat(r.Context(), s.Engine, "", workDir, childSystemPrompt(s.Child), req.Messages)
		if err != nil {
			writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
		return
	}

	// Serialize on the shared agent-turn lock (parent + child share global MCP env).
	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:     provider,
		WorkingDir:   workDir,
		SystemPrompt: childSystemPrompt(s.Child),
		Tools:        []agentsession.Tool{childShellTool(), notifyTool()},
	})
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}
	defer sess.Close()

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}
	persistConversation("child", req.ConversationID, withReply(req.Messages, reply))
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
}
