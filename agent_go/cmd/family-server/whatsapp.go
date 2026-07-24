package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
)

// whatsappSystemPrompt is the parent persona adapted for the WhatsApp channel:
// short, plain-text replies suitable for a phone — no markdown, no HTML, no
// opening files on a screen.
func whatsappSystemPrompt(child *Child, parentLabel string, pulse PulseConfig) string {
	return parentSystemPrompt(child, parentLabel, pulse) +
		"\n\nCHANNEL — WHATSAPP: You are replying to the parent over WhatsApp on their phone. Keep replies SHORT and in plain text: no markdown, no headings, no HTML, and do not talk about opening files on a screen. If the message ends with a parenthetical \"(I sent it to <path>)\", that names the EXACT real path of a file they just sent — never guess a different filename or folder, this path is always correct. If it's an image, call read_image on that exact path directly; if it's a document or PDF, read it with your shell tools (follow the process-file skill) to pull out its content. Then answer in a few lines, warmly and to the point, and never mention files or paths."
}

// handleWhatsAppMessage is the WhatsApp connector core + local simulator endpoint.
// An inbound message (from the real WhatsApp webhook, or the in-app simulator)
// runs one parent agent turn with the WhatsApp channel prompt and returns the
// reply. A production webhook would parse the provider payload, call this, and
// send the reply back through the WhatsApp Business API.
func handleWhatsAppMessage(w http.ResponseWriter, r *http.Request) {
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

	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(workDir, 0o700)

	provider, ok := engineToProvider(s.Engine)
	if !ok {
		reply, err := enginedetect.Chat(r.Context(), s.Engine, "", workDir, whatsappSystemPrompt(s.Child, s.ParentLabel, s.Pulse), req.Messages)
		if err != nil {
			writeJSON(w, http.StatusOK, parentMessageResponse{Error: friendlyTurnError(err)})
			return
		}
		writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
		return
	}

	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), turnTimeout)
	defer cancel()

	cid := req.ConversationID
	if cid == "" {
		cid = "whatsapp"
	}

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:     provider,
		WorkingDir:   workDir,
		SystemPrompt: whatsappSystemPrompt(s.Child, s.ParentLabel, s.Pulse),
		// Stable SessionID reuses the warm tmux within this process; SessionHandle
		// restores the coding agent's `--resume` state across restarts.
		SessionID:                 cid,
		SessionHandle:             loadSessionHandle("parent", cid, provider),
		BridgeRoutingInstructions: bridgeRoutingInstructions(),
		// The ONE canonical parent manifest (parent_tools.go) — this is a
		// parent-scope session (handles are stored under the "parent" scope, and
		// cid is the parent conversation when the caller supplies it), so it must
		// register the same tools as every other parent surface rather than a
		// narrower subset that could define the shared warm session.
		Tools: withLiveStatus("whatsapp:"+req.ConversationID, parentTools(s.Engine, parentChildLabel(s.Child), parentToolSinks{})),
	})
	if err != nil {
		msg := friendlyTurnError(err)
		persistConversation("parent", cid, withReply(req.Messages, msg))
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg})
		return
	}
	defer sess.Close()

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}
	reply, err := sess.Ask(ctx, history)
	if err != nil {
		// Persist the turn even on failure — see chat.go's parent handler for why.
		msg := friendlyTurnError(err)
		persistConversation("parent", cid, withReply(req.Messages, msg))
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg})
		return
	}
	saveSessionHandle("parent", cid, sess.Handle())
	persistConversation("parent", cid, withReply(req.Messages, reply))
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
}
