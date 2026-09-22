package server

import (
	"strings"

	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

// sessionPersistenceClassForRequest classifies the session itself, not an
// individual turn. Automated turns in an interactive chat therefore retain
// the chat's class; event projection decides which of their events are durable.
func sessionPersistenceClassForRequest(req QueryRequest) storeevents.SessionPersistenceClass {
	mode := normalizeAgentMode(req.AgentMode)
	trigger := strings.ToLower(strings.TrimSpace(req.TriggeredBy))
	if strings.TrimSpace(req.ParentSessionID) != "" || strings.TrimSpace(req.SessionKind) != "" {
		return storeevents.SessionPersistenceExecution
	}
	if mode == "workflow" || trigger == "cron" || trigger == "webhook" || strings.Contains(trigger, "schedule") || strings.Contains(trigger, "queue") {
		return storeevents.SessionPersistenceExecution
	}
	if mode == "workflow_phase" || isToolBackedChatMode(mode) {
		return storeevents.SessionPersistenceInteractiveChat
	}
	return storeevents.SessionPersistenceEphemeral
}
