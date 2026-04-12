package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// ChatHistoryRoutes registers the read-only chat history endpoints.
func ChatHistoryRoutes(router *mux.Router, api *StreamingAPI) {
	r := router.PathPrefix("/api/chat-history").Subrouter()
	r.HandleFunc("/sessions", listChatHistoryHandler(api)).Methods("GET")
	r.HandleFunc("/sessions/{session_id}", getChatHistoryConversationHandler(api)).Methods("GET")
}

func listChatHistoryHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		sessions, err := ListChatHistorySessions(userID, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": sessions,
		})
	}
}

func getChatHistoryConversationHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}
		sessionID := mux.Vars(r)["session_id"]

		data, err := ReadChatHistoryConversation(userID, sessionID)
		if err != nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}
