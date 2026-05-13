package server

import (
	"context"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"sort"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ChatHistorySession is the metadata returned by the list endpoint.
type ChatHistorySession struct {
	SessionID        string `json:"session_id"`
	AgentMode        string `json:"agent_mode"`
	Status           string `json:"status"`
	Query            string `json:"query,omitempty"`
	UserID           string `json:"user_id"`
	ConversationPath string `json:"conversation_path"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	MessageCount     int    `json:"message_count"`
}

// chatHistoryRoot returns the workspace-relative path to a user's chat_history root.
func chatHistoryRoot(userID string) string {
	return fmt.Sprintf("_users/%s/chat_history", sanitizeUserIDForPath(userID))
}

// persistChatConversation saves the conversation in the same format as the
// workflow builder: a single conversation.json with the full message history.
// Called inline (not async) right after finalHistory is captured.
func (api *StreamingAPI) persistChatConversation(sessionID, agentMode, userID string, history []llmtypes.MessageContent) {
	if len(history) == 0 {
		return
	}
	if userID == "" {
		userID = "default"
	}
	logCtx := newServerLogContext("", "", agentMode, userID, "", sessionID)

	convData := map[string]interface{}{
		"session_id":           sessionID,
		"agent_mode":           agentMode,
		"conversation_history": history,
		"updated_at":           time.Now().Format(time.RFC3339),
	}

	convJSON, err := json.MarshalIndent(convData, "", "  ")
	if err != nil {
		logfWithContext(logCtx, "[CHAT_HISTORY] Failed to marshal conversation for %s: %v", sessionID, err)
		return
	}

	convPath := pathpkg.Join(chatHistoryRoot(userID), sessionID, "conversation.json")
	if err := writeRawFileToWorkspace(context.Background(), convPath, string(convJSON)); err != nil {
		logfWithContext(logCtx, "[CHAT_HISTORY] Failed to write %s: %v", convPath, err)
		return
	}

	logfWithContext(logCtx, "[CHAT_HISTORY] Saved conversation (%d messages) to %s", len(history), convPath)
}

// ListChatHistorySessions returns persisted session metadata for a user, newest first.
func ListChatHistorySessions(userID string, limit, offset int) ([]ChatHistorySession, error) {
	if userID == "" {
		userID = "default"
	}
	root := chatHistoryRoot(userID)

	sessionIDs, err := listWorkspaceChildFolderNames(context.Background(), root)
	if err != nil {
		return nil, err
	}

	var sessions []ChatHistorySession
	for _, sessionID := range sessionIDs {
		convPath := pathpkg.Join(root, sessionID, "conversation.json")
		data, exists, err := readFileFromWorkspace(context.Background(), convPath)
		if err != nil || !exists {
			continue
		}

		var raw struct {
			SessionID string                    `json:"session_id"`
			AgentMode string                    `json:"agent_mode"`
			History   []llmtypes.MessageContent `json:"conversation_history"`
			UpdatedAt string                    `json:"updated_at"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}

		// Extract first user query from history
		query := ""
		for _, msg := range raw.History {
			if msg.Role == "human" {
				for _, part := range msg.Parts {
					switch v := part.(type) {
					case llmtypes.TextContent:
						if v.Text != "" {
							query = v.Text
						}
					case map[string]interface{}:
						if t, ok := v["Text"].(string); ok && t != "" {
							query = t
						}
					}
					if query != "" {
						break
					}
				}
				if query != "" {
					break
				}
			}
		}
		if len(query) > 200 {
			query = query[:200] + "..."
		}

		sessions = append(sessions, ChatHistorySession{
			SessionID:        raw.SessionID,
			AgentMode:        raw.AgentMode,
			Query:            query,
			UserID:           userID,
			ConversationPath: convPath,
			UpdatedAt:        raw.UpdatedAt,
			MessageCount:     len(raw.History),
		})
	}

	// Sort by UpdatedAt descending
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	// Apply pagination
	if offset >= len(sessions) {
		return []ChatHistorySession{}, nil
	}
	sessions = sessions[offset:]
	if limit > 0 && limit < len(sessions) {
		sessions = sessions[:limit]
	}

	return sessions, nil
}

// ReadChatHistoryConversation reads the persisted conversation.json for a session.
func ReadChatHistoryConversation(userID, sessionID string) (json.RawMessage, error) {
	if userID == "" {
		userID = "default"
	}
	convPath := pathpkg.Join(chatHistoryRoot(userID), sessionID, "conversation.json")
	data, exists, err := readFileFromWorkspace(context.Background(), convPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("conversation not found")
	}
	return json.RawMessage(data), nil
}
