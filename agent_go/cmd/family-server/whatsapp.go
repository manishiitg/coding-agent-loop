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

// whatsappSystemPrompt is the parent persona adapted for the WhatsApp channel:
// short, plain-text replies suitable for a phone — no markdown, no HTML, no
// opening files on a screen.
func whatsappSystemPrompt(child *Child) string {
	return parentSystemPrompt(child) +
		"\n\nCHANNEL — WHATSAPP: You are replying to the parent over WhatsApp on their phone. Keep replies SHORT and in plain text: no markdown, no headings, no HTML, and do not talk about opening files on a screen. If they send a photo of homework, use read_image. Answer in a few lines, warmly and to the point."
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
		reply, err := enginedetect.Chat(r.Context(), s.Engine, "", workDir, whatsappSystemPrompt(s.Child), req.Messages)
		if err != nil {
			writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
		return
	}

	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:     provider,
		WorkingDir:   workDir,
		SystemPrompt: whatsappSystemPrompt(s.Child),
		SessionID:    req.ConversationID, // warm-resume the WhatsApp thread
		Tools:        []agentsession.Tool{webSearchTool(), readImageTool(s.Engine), notifyTool(), shellTool()},
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
	cid := req.ConversationID
	if cid == "" {
		cid = "whatsapp"
	}
	persistConversation("parent", cid, withReply(req.Messages, reply))
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
}
