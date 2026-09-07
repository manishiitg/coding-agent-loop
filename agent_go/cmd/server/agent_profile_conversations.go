package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gorilla/mux"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// A product's earlier conversations. The registry remembers every
// conversation a "new chat" or a switch moved out of a slot; chat_history
// holds the transcripts and a first line to title them by. Chats rotated
// before the registry kept history are found by their workspace, so a
// product's list is complete either way.

// AgentProfileConversationSummary is one conversation of a slot as a client
// lists it: identity, when, and a title from its first message.
type AgentProfileConversationSummary struct {
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	MessageCount   int    `json:"message_count,omitempty"`
	Current        bool   `json:"current"`
}

type AgentProfileConversationsResponse struct {
	Current  *AgentProfileConversationSummary  `json:"current,omitempty"`
	Previous []AgentProfileConversationSummary `json:"previous"`
}

const agentProfileConversationTitleLimit = 72

// conversationTitleFrom titles a chat by its first message, trimmed to one
// line; a chat with no message yet is "New chat".
func conversationTitleFrom(session *ChatHistorySession, fallback string) string {
	if session != nil {
		first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(session.Query), "\n", 2)[0])
		if first != "" {
			if runes := []rune(first); len(runes) > agentProfileConversationTitleLimit {
				first = strings.TrimSpace(string(runes[:agentProfileConversationTitleLimit])) + "…"
			}
			return first
		}
	}
	if fallback = strings.TrimSpace(fallback); fallback != "" {
		return fallback
	}
	return "New chat"
}

// normalizeConversationWorkspace compares workspaces the way chat_history
// and the registry each record them: with or without the per-user prefix.
func normalizeConversationWorkspace(workspacePath string) string {
	clean := strings.Trim(strings.TrimSpace(strings.ReplaceAll(workspacePath, "\\", "/")), "/")
	if strings.HasPrefix(clean, "_users/") {
		if rest := strings.SplitN(clean, "/", 3); len(rest) == 3 {
			clean = rest[2]
		}
	}
	return clean
}

func chatHistorySessionWorkspace(session ChatHistorySession) string {
	if session.Runtime != nil && strings.TrimSpace(session.Runtime.WorkspacePath) != "" {
		return normalizeConversationWorkspace(session.Runtime.WorkspacePath)
	}
	return normalizeConversationWorkspace(session.WorkspacePath)
}

// chatHistorySessionsByID indexes the user's chat history for titling and
// for finding chats the registry never listed.
func chatHistorySessionsByID(userID string) map[string]ChatHistorySession {
	sessions, err := ListChatHistorySessions(userID, 2000, 0, "")
	if err != nil {
		return map[string]ChatHistorySession{}
	}
	byID := make(map[string]ChatHistorySession, len(sessions))
	for _, session := range sessions {
		byID[session.SessionID] = session
	}
	return byID
}

func summarizeProductConversation(record ProductConversationRecord, session *ChatHistorySession, current bool) AgentProfileConversationSummary {
	summary := AgentProfileConversationSummary{
		SessionID:      record.SessionID,
		ConversationID: record.ConversationID,
		Title:          conversationTitleFrom(session, ""),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
		Current:        current,
	}
	if session != nil {
		summary.MessageCount = session.MessageCount
		if strings.TrimSpace(session.CreatedAt) != "" {
			summary.CreatedAt = session.CreatedAt
		}
		if strings.TrimSpace(session.UpdatedAt) != "" {
			summary.UpdatedAt = session.UpdatedAt
		}
	}
	return summary
}

func (api *StreamingAPI) agentProfileConversationSlot(w http.ResponseWriter, r *http.Request, conversationKey string) (string, profileAndBinding, bool) {
	if api.agentProfiles == nil {
		writeAgentProfileError(w, http.StatusServiceUnavailable, "agent profiles are unavailable")
		return "", profileAndBinding{}, false
	}
	userID := productWorkspaceUserID(r.Context())
	profile, err := api.agentProfiles.Resolve(strings.TrimSpace(mux.Vars(r)["id"]), 0, userID)
	if err != nil {
		writeAgentProfileError(w, http.StatusNotFound, "agent profile not found")
		return "", profileAndBinding{}, false
	}
	binding, err := resolveProductConversationBinding(r.Context(), userID, profile, conversationKey)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return "", profileAndBinding{}, false
	}
	return userID, profileAndBinding{profile: profile, binding: binding}, true
}

type profileAndBinding struct {
	profile agentprofiles.Profile
	binding productConversationBinding
}

// handleListAgentProfileConversations lists a slot's live conversation and
// its earlier ones, newest first: GET /api/agent-profiles/{id}/conversations
// ?conversation_key=….
func (api *StreamingAPI) handleListAgentProfileConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	userID, slot, ok := api.agentProfileConversationSlot(w, r, r.URL.Query().Get("conversation_key"))
	if !ok {
		return
	}
	store := defaultProductConversationRegistryStore()
	current, hasCurrent, previous, err := store.history(r.Context(), userID, slot.profile, slot.binding)
	if err != nil {
		writeAgentProfileError(w, http.StatusInternalServerError, err.Error())
		return
	}
	live, err := store.liveSessionIDs(r.Context(), userID, slot.profile.ID)
	if err != nil {
		writeAgentProfileError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sessions := chatHistorySessionsByID(userID)
	lookup := func(sessionID string) *ChatHistorySession {
		if session, ok := sessions[sessionID]; ok {
			return &session
		}
		return nil
	}

	response := AgentProfileConversationsResponse{Previous: []AgentProfileConversationSummary{}}
	if hasCurrent {
		summary := summarizeProductConversation(current, lookup(current.SessionID), true)
		response.Current = &summary
	}
	listed := map[string]bool{}
	for _, record := range previous {
		if listed[record.SessionID] || live[record.SessionID] {
			continue
		}
		listed[record.SessionID] = true
		response.Previous = append(response.Previous, summarizeProductConversation(record, lookup(record.SessionID), false))
	}
	// Chats rotated before the registry kept history: same workspace as this
	// slot, not live anywhere, not listed yet.
	workspace := normalizeConversationWorkspace(slot.binding.WorkspacePath)
	for sessionID, session := range sessions {
		if listed[sessionID] || live[sessionID] || (hasCurrent && sessionID == current.SessionID) {
			continue
		}
		if workspace == "" || chatHistorySessionWorkspace(session) != workspace {
			continue
		}
		listed[sessionID] = true
		found := session
		response.Previous = append(response.Previous, summarizeProductConversation(ProductConversationRecord{SessionID: sessionID, ConversationKey: slot.binding.ConversationKey, ProfileID: slot.profile.ID}, &found, false))
	}
	sort.SliceStable(response.Previous, func(i, j int) bool {
		return response.Previous[i].UpdatedAt > response.Previous[j].UpdatedAt
	})
	writeAgentProfileJSON(w, http.StatusOK, response)
}

// handleSwitchAgentProfileConversation makes an earlier conversation the
// slot's live one: POST /api/agent-profiles/{id}/conversation/switch
// {"conversation_key": …, "session_id": …}. The client then reopens the
// slot the same way it does after "new chat".
func (api *StreamingAPI) handleSwitchAgentProfileConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentProfileRequestBytes))
	decoder.DisallowUnknownFields()
	var input AgentProfileConversationRequest
	if err := decoder.Decode(&input); err != nil {
		writeAgentProfileError(w, http.StatusBadRequest, "invalid product conversation request: "+err.Error())
		return
	}
	if strings.TrimSpace(input.SessionID) == "" {
		writeAgentProfileError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	userID, slot, ok := api.agentProfileConversationSlot(w, r, input.ConversationKey)
	if !ok {
		return
	}
	// A chat the registry never listed is accepted when chat_history shows it
	// belongs to this slot's workspace.
	verified := false
	if session, found := chatHistorySessionsByID(userID)[strings.TrimSpace(input.SessionID)]; found {
		verified = chatHistorySessionWorkspace(session) == normalizeConversationWorkspace(slot.binding.WorkspacePath)
	}
	conversation, err := defaultProductConversationRegistryStore().switchTo(r.Context(), userID, slot.profile, slot.binding, input.SessionID, verified)
	if err != nil {
		writeAgentProfileError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeAgentProfileJSON(w, http.StatusOK, AgentProfileConversationResponse{
		ConversationID:  conversation.ConversationID,
		ConversationKey: conversation.ConversationKey,
		SessionID:       conversation.SessionID,
	})
}

// handleDeleteAgentProfileConversation forgets an earlier conversation and
// deletes its transcript: DELETE /api/agent-profiles/{id}/conversations/
// {session_id}?conversation_key=…. The live conversation is refused.
func (api *StreamingAPI) handleDeleteAgentProfileConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sessionID := strings.TrimSpace(mux.Vars(r)["session_id"])
	if sessionID == "" {
		writeAgentProfileError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	userID, slot, ok := api.agentProfileConversationSlot(w, r, r.URL.Query().Get("conversation_key"))
	if !ok {
		return
	}
	store := defaultProductConversationRegistryStore()
	live, err := store.liveSessionIDs(r.Context(), userID, slot.profile.ID)
	if err != nil {
		writeAgentProfileError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if live[sessionID] {
		writeAgentProfileError(w, http.StatusConflict, "this is a live conversation — start a new chat before deleting it")
		return
	}
	// Only this slot's chats can go: listed by the registry, or in its
	// workspace per chat_history.
	owned := false
	for _, record := range func() []ProductConversationRecord { _, _, previous, _ := store.history(r.Context(), userID, slot.profile, slot.binding); return previous }() {
		if record.SessionID == sessionID {
			owned = true
		}
	}
	if !owned {
		if session, found := chatHistorySessionsByID(userID)[sessionID]; found {
			owned = chatHistorySessionWorkspace(session) == normalizeConversationWorkspace(slot.binding.WorkspacePath)
		}
	}
	if !owned {
		writeAgentProfileError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err := store.forget(r.Context(), userID, slot.profile, slot.binding, sessionID); err != nil {
		writeAgentProfileError(w, http.StatusConflict, err.Error())
		return
	}
	result, err := DeleteChatHistorySession(userID, sessionID, "")
	if err != nil {
		writeAgentProfileError(w, http.StatusInternalServerError, "delete conversation transcript: "+err.Error())
		return
	}
	writeAgentProfileJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted_paths": result.DeletedPaths})
}
