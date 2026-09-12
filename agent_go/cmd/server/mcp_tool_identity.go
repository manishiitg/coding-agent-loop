package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"
)

// CLI bridge requests have a session bearer, not browser JWT claims. Resolve
// their identity from server-owned session data; never authorize OAuth as the
// generic default user just because the HTTP context has no browser claims.
func (api *StreamingAPI) mcpToolUserID(ctx context.Context) (string, error) {
	userID := ""
	if claims := GetUserFromContext(ctx); claims != nil {
		userID = strings.TrimSpace(claims.UserID)
	}
	if directID, _ := ctx.Value(common.UserIDKey).(string); directID != "" {
		if userID != "" && userID != directID {
			return "", fmt.Errorf("MCP tool user identity mismatch")
		}
		userID = directID
	}
	sessionID := executor.SessionIDFromContext(ctx)
	if sessionID == "" {
		sessionID, _ = ctx.Value(common.ChatSessionIDKey).(string)
	}
	if sessionID != "" {
		owner := ""
		if api.eventStore != nil {
			owner = api.eventStore.GetSessionOwner(sessionID)
		}
		if owner == "" {
			return "", fmt.Errorf("MCP tool session owner is unavailable")
		}
		if userID != "" && userID != owner {
			return "", fmt.Errorf("MCP tool user does not own this session")
		}
		userID = owner
	}
	if userID == "" {
		return "", fmt.Errorf("MCP tool requires an authenticated user")
	}
	return userID, nil
}

// OAuth discovery metadata is deliberately absent from the global status map.
// Read the same per-account cache key used by discovery, without making a live
// connection just to render the server list.
func (api *StreamingAPI) mcpToolStatusForUser(name, userID string, cfg mcpclient.MCPServerConfig) ToolStatus {
	if cfg.OAuth != nil {
		oauth := *cfg.OAuth
		oauth.TokenFile = getUserTokenFilePath(userID, name)
		cfg.OAuth = &oauth
		if !hasOAuthTokenFile(cfg) {
			return ToolStatus{Name: name, Server: name, Status: "not_connected", RequiresOAuth: true}
		}
		if entry, ok := mcpcache.GetCacheManager(api.logger).Get(mcpcache.GenerateUnifiedCacheKey(name, cfg)); ok {
			return api.convertCacheEntryToToolStatus(entry)
		}
		return ToolStatus{Name: name, Server: name, Status: "not_loaded"}
	}
	api.toolStatusMux.RLock()
	defer api.toolStatusMux.RUnlock()
	return api.toolStatus[name]
}
