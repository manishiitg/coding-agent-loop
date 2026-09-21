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

// readOnlyForRequest decides whether a turn runs with read-only treatment.
// A read-only workflow identity is always read-only; PinRunMode additionally
// lets a caller voluntarily take read-only treatment for one turn. The pin
// is downgrade-only — it keeps the Run-mode tool surface while withholding
// authoring, and grants nothing — so external execution-only callers use it
// to run without authoring, whatever their workflow access.
func readOnlyForRequest(access WorkflowAccessLevel, req QueryRequest) bool {
	return access == WorkflowAccessRead || req.PinRunMode
}

// Resolve the complete execution profile from access and server-maintained
// provenance. There is deliberately no caller-supplied mode input: workflow
// access is the authority boundary (writable => Builder, read-only => Run),
// while origin can only narrow the resulting capability set. A missing field
// on a resumed turn must never promote a schedule/child.
func resolveWorkflowChatPolicy(session string, req QueryRequest, active *ActiveSessionInfo, readOnly bool) workflowChatPolicy {
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
	// Conversational workflow authority is access-derived. Legacy/client mode
	// fields cannot widen or reduce the authenticated principal. The direct
	// headless workflow executor is not a conversation and remains Run.
	normalized := "builder"
	if readOnly || strings.TrimSpace(req.AgentMode) == "workflow" {
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

// Persist the same access-derived conversational mode used for tool admission.
// Old clients and restored chats may still submit the mode from before an
// access change; it must not select the CLI directory or reconnect metadata.
func normalizeWorkflowConversationMode(req *QueryRequest, readOnly bool) {
	if req == nil || req.AgentProfileID != "" || req.AgentMode != "workflow_phase" || req.PhaseID != "workflow-builder" {
		return
	}
	if req.ExecutionOptions == nil {
		req.ExecutionOptions = &ExecutionOptions{}
	} else {
		options := *req.ExecutionOptions
		req.ExecutionOptions = &options
	}
	req.ExecutionOptions.WorkshopMode = "workshop"
	if resolveWorkflowChatPolicy("", *req, nil, readOnly).Mode == "run" {
		req.ExecutionOptions.WorkshopMode = "run"
	}
}
