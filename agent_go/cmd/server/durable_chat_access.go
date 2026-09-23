package server

import "net/http"

// durableChatReadAllowed authorizes a durable read of an inactive chat with
// the same rule the chat-history list uses, so a session that is listed can be
// opened and vice versa: the author (legacy "default" ownership only counts on
// a single-user install), or a workflow reader for a workflow bot conversation.
func durableChatReadAllowed(r *http.Request, sessionID, ownerID, workspacePath string) bool {
	viewerID := GetUserIDFromContext(r.Context())
	platformAdmin := userAccessForClaims(GetUserFromContext(r.Context())).Admin
	if chatHistoryVisibleTo(ownerID, viewerID, platformAdmin) {
		return true
	}
	_, workflowScoped, allowed := chatHistoryWorkspaceAccess(r, workspacePath)
	if !workflowScoped || !allowed {
		return false
	}
	return isWorkflowBotHistory(ChatHistorySession{
		SessionID:   sessionID,
		UserID:      ownerID,
		BotPlatform: botPlatformFromSessionID(sessionID),
	})
}
