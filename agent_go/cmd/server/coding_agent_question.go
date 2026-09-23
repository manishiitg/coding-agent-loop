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

func (api *StreamingAPI) handleCodingAgentQuestionAnswer(w http.ResponseWriter, r *http.Request) {
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
		Provider string `json:"provider"`
		PromptID string `json:"prompt_id"`
		Answers  []struct {
			ID             string   `json:"id"`
			SelectedLabels []string `json:"selected_labels"`
			SelectedLabel  string   `json:"selected_label"` // Legacy Muse client.
		} `json:"answers"`
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
	provider := strings.TrimSpace(req.Provider)
	if provider == "" && strings.HasSuffix(r.URL.Path, "/muse-question/answer") {
		provider = "muse-cli"
	}
	if provider != "muse-cli" {
		http.Error(w, "Question choice delivery is not available for this provider", http.StatusNotImplemented)
		return
	}
	answers := make([]musecli.QuestionAnswer, 0, len(req.Answers))
	for _, answer := range req.Answers {
		labels := answer.SelectedLabels
		if len(labels) == 0 && answer.SelectedLabel != "" {
			labels = []string{answer.SelectedLabel}
		}
		if answer.ID == "" || len(labels) != 1 || labels[0] == "" {
			http.Error(w, "Muse requires one selected option per question", http.StatusBadRequest)
			return
		}
		answers = append(answers, musecli.QuestionAnswer{ID: answer.ID, SelectedLabel: labels[0]})
	}
	if err := musecli.SubmitQuestionAnswers(ctx, sessionID, req.PromptID, answers); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "prompt_id": req.PromptID})
}
