package server

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentworksproduct"
)

type workflowChatPolicy struct {
	Mode         string
	Origin       string
	Capabilities map[string]bool
}

// Resolve origin from server-maintained session provenance as well as the
// request. A missing field on a resumed turn must never promote a schedule/child.
func resolveWorkflowChatPolicy(mode, session string, req QueryRequest, active *ActiveSessionInfo, readOnly bool) workflowChatPolicy {
	origin := "interactive"
	switch {
	case req.ParentSessionID != "" || req.SessionKind != "" || active != nil && (active.ParentSessionID != "" || active.SessionKind != ""):
		origin = "child"
	case req.BotPlatform != "" || active != nil && active.BotPlatform != "":
		origin = "bot"
	case req.IsAutoNotification || strings.EqualFold(strings.TrimSpace(req.TriggeredBy), "auto_notification"):
		origin = "notification"
	case req.PulseLifecycleTurn:
		origin = "pulse"
	case !req.UserInteractiveContinuation && (isScheduledSessionIdentity(session, req.TriggeredBy) || active != nil && isScheduledSessionIdentity(session, active.TriggeredBy)):
		origin = "scheduled"
	}
	normalized := "run"
	switch strings.TrimSpace(mode) {
	case "", "workshop", "builder", "optimizer":
		normalized = "builder"
	}
	if strings.TrimSpace(req.AgentMode) == "workflow" || origin == "scheduled" || origin == "notification" {
		normalized = "run"
	}
	return workflowChatPolicy{Mode: normalized, Origin: origin, Capabilities: agentworksproduct.ChatCapabilities(normalized, origin, readOnly)}
}

func (p workflowChatPolicy) allows(capability string) bool { return p.Capabilities[capability] }

func (p workflowChatPolicy) sessionKey() string {
	names := make([]string, 0, len(p.Capabilities))
	for name := range p.Capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(p.Mode + "|" + p.Origin + "|" + strings.Join(names, ",")))
	return fmt.Sprintf("%x", sum[:16])
}

func (api *StreamingAPI) chatPolicySessionKey(p workflowChatPolicy) string {
	h := sha256.New()
	h.Write([]byte(p.sessionKey()))
	h.Write([]byte(agentworksproduct.ChatDefinitionKey(p.Mode)))
	// Never log config contents or secret values. A changed install/auth/config
	// causes the existing durable-history reconnect path on the next user turn.
	for _, path := range []string{api.mcpConfigPath, api.getUserConfigPath()} {
		data, err := os.ReadFile(path)
		if err == nil {
			h.Write(data)
		}
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func chatPolicyRequiresReconnect(codingProvider bool, previous, current string, known bool, saved *ChatHistoryAgentRuntime) bool {
	if !codingProvider {
		return false
	}
	if known {
		return previous != current
	}
	if saved == nil {
		return false
	}
	hasResume := saved.ExternalSessionID != "" || saved.AgentSessionHandle != nil && !saved.AgentSessionHandle.Empty()
	return hasResume && saved.ChatPolicyKey != current
}

// Registration is shared by normal chat and workflow-phase construction, before
// finalization/catalog publication. Other products retain their manifest gate.
func (api *StreamingAPI) registerMCPToolsForChat(registrar definitionToolRegistrar, policy workflowChatPolicy, disabled func(string) bool) error {
	if !policy.allows("mcp_management") {
		return nil
	}
	return api.registerMultiAgentMCPServerTools(registrar, disabled)
}
