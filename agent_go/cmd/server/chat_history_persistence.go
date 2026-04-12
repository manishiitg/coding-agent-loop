package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/fsutil"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ChatHistorySession is the metadata returned by the list endpoint.
type ChatHistorySession struct {
	SessionID   string `json:"session_id"`
	AgentMode   string `json:"agent_mode"`
	Status      string `json:"status"`
	Query       string `json:"query,omitempty"`
	UserID      string `json:"user_id"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	MessageCount int   `json:"message_count"`
}

// chatHistoryDir returns the absolute path to a user's chat_history root.
func chatHistoryRoot(userID string) string {
	return filepath.Join(
		fsutil.WorkspaceDocsRoot(),
		fmt.Sprintf("_users/%s/chat_history", sanitizeUserIDForPath(userID)),
	)
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

	dir := filepath.Join(chatHistoryRoot(userID), sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[CHAT_HISTORY] Failed to create dir %s: %v", dir, err)
		return
	}

	convData := map[string]interface{}{
		"session_id":           sessionID,
		"agent_mode":           agentMode,
		"conversation_history": history,
		"updated_at":           time.Now().Format(time.RFC3339),
	}

	convJSON, err := json.MarshalIndent(convData, "", "  ")
	if err != nil {
		log.Printf("[CHAT_HISTORY] Failed to marshal conversation for %s: %v", sessionID, err)
		return
	}

	convPath := filepath.Join(dir, "conversation.json")
	if err := os.WriteFile(convPath, convJSON, 0644); err != nil {
		log.Printf("[CHAT_HISTORY] Failed to write %s: %v", convPath, err)
		return
	}

	log.Printf("[CHAT_HISTORY] Saved conversation (%d messages) to %s", len(history), convPath)
}

// ListChatHistorySessions returns persisted session metadata for a user, newest first.
func ListChatHistorySessions(userID string, limit, offset int) ([]ChatHistorySession, error) {
	if userID == "" {
		userID = "default"
	}
	root := chatHistoryRoot(userID)

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []ChatHistorySession{}, nil
		}
		return nil, err
	}

	var sessions []ChatHistorySession
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		convPath := filepath.Join(root, e.Name(), "conversation.json")
		data, err := os.ReadFile(convPath)
		if err != nil {
			continue
		}

		var raw struct {
			SessionID   string                     `json:"session_id"`
			AgentMode   string                     `json:"agent_mode"`
			History     []llmtypes.MessageContent  `json:"conversation_history"`
			UpdatedAt   string                     `json:"updated_at"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
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
			SessionID:    raw.SessionID,
			AgentMode:    raw.AgentMode,
			Query:        query,
			UserID:       userID,
			UpdatedAt:    raw.UpdatedAt,
			MessageCount: len(raw.History),
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
	convPath := filepath.Join(chatHistoryRoot(userID), sessionID, "conversation.json")
	data, err := os.ReadFile(convPath)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
