package step_based_workflow

import "strings"

const (
	CodingAgentTmuxLifecycleCloseOnCompletion = "close_on_completion"
	CodingAgentTmuxLifecycleKeepAlive         = "keep_alive"
)

func normalizeCodingAgentTmuxLifecycle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "close", "close_on_completion", "bounded", "per_turn":
		return CodingAgentTmuxLifecycleCloseOnCompletion
	case "keep_alive", "keepalive", "persistent":
		return CodingAgentTmuxLifecycleKeepAlive
	default:
		return ""
	}
}
