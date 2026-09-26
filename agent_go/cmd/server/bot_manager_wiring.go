package server

import (
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

// wireBotManager installs every server hook on a bot manager: session start
// and follow-up, resume, workflow access, running workflows, the workflow and
// crew-conversation turn builders, progressive text and the Slack dedicated
// route. Startup and the bot dry-run tests share it, so a test exercises the
// production wiring rather than a copy.
func (api *StreamingAPI) wireBotManager(m *services.BotConversationManager) {
	m.SetStartSessionFunc(api.startSessionInternal)
	m.SetFollowUpFunc(api.sendFollowUpInternal)
	m.SetResumeTargetFunc(api.resolveBotResumeTarget)
	m.SetResumeListFunc(api.listBotResumeTargets)
	m.SetWorkflowAccessFunc(api.checkBotWorkflowAccess)
	m.SetRunningWorkflowsFunc(api.botRunningWorkflows)
	// Product/profile routes are shared by Slack and WhatsApp; Slack profile
	// routes need the handler even when WhatsApp is disabled.
	m.SetProfileTurnFunc(api.botProfileTurn)
	m.SetWorkflowTurnFunc(api.botWorkflowTurn)
	// Progressive text: while a bot turn runs, forward each completed
	// assistant reply as its own message rather than waiting for the whole
	// turn to finish — reads the same durable conversation_history the UI's
	// own chat-restore path already trusts, kept current as the turn runs.
	m.SetChatHistoryReader(botProgressiveChatHistoryReader)
	services.SetDedicatedSlackRouteFunc(api.dedicatedSlackRoute)
	// A 1:1 Slack DM runs as the one enabled account its sender's email
	// maps to (slack_dm.go).
	services.SetSlackDMUserResolver(slackDMUserForEmail)
}

// botRunningWorkflows lists a user's running workflows for bot status replies.
func (api *StreamingAPI) botRunningWorkflows(userID string) []services.BotRunningWorkflow {
	running := api.listRunningWorkflowExecutions(userID)
	out := make([]services.BotRunningWorkflow, 0, len(running))
	for _, wf := range running {
		label := strings.TrimSpace(wf.PresetName)
		if label == "" && wf.WorkspacePath != "" {
			label = workflowNameFromWorkspacePath(wf.WorkspacePath)
		}
		out = append(out, services.BotRunningWorkflow{
			WorkflowLabel:    label,
			WorkspacePath:    wf.WorkspacePath,
			Status:           wf.Status,
			CurrentStepTitle: wf.CurrentStepTitle,
			PhaseName:        wf.PhaseName,
			Title:            wf.Title,
			SessionID:        wf.SessionID,
			StartedAt:        wf.StartedAt,
		})
	}
	return out
}
