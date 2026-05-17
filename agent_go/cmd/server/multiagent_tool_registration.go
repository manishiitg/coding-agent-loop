package server

// isPreRegisteredMultiAgentTool reports tools that multi-agent chat wires with
// session-specific wrappers before the generic custom-tool registration pass.
func isPreRegisteredMultiAgentTool(toolName string) bool {
	switch toolName {
	case
		"delegate",
		"query_agent",
		"terminate_agent",
		"list_agents",
		"run_workflow",
		"run_step",
		"stop_workflow_run",
		"list_all_schedules",
		"list_workflow_schedules",
		"create_workflow_schedule",
		"create_calendar_workflow_schedule",
		"update_workflow_schedule",
		"delete_workflow_schedule",
		"trigger_workflow_schedule",
		"get_workflow_schedule_runs":
		return true
	default:
		return false
	}
}
