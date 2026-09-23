package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/musecli"
)

func (api *StreamingAPI) handleMuseQuestionAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	sessionID := strings.TrimSpace(mux.Vars(r)["session_id"])
	if sessionID == "" || !api.canAccessTerminalSession(r, sessionID) {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	var req struct {
		PromptID string                   `json:"prompt_id"`
		Answers  []musecli.QuestionAnswer `json:"answers"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		http.Error(w, "Invalid question answer", http.StatusBadRequest)
		return
	}
	if req.PromptID == "" || len(req.Answers) == 0 || len(req.Answers) > 12 {
		http.Error(w, "Prompt and answers are required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := musecli.SubmitQuestionAnswers(ctx, sessionID, req.PromptID, req.Answers); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "prompt_id": req.PromptID})
}
