package instructions

// GetGWSQuickStartInstructions returns inline instructions for using Google Workspace via the gws CLI.
// Appended to the agent's system prompt when GWS access is enabled.
func GetGWSQuickStartInstructions() string {
	return `## Google Workspace

You have access to Google Workspace services (Drive, Gmail, Calendar, Docs, Sheets, Slides) via the ` + "`gws`" + ` CLI tool.

Read the skill files for usage instructions:
- All services: ` + "`execute_shell_command(command=\"ls skills/gws-*/SKILL.md\")`" + `
- Specific service (e.g. Drive): ` + "`execute_shell_command(command=\"cat skills/gws-drive/SKILL.md\")`" + ``
}
