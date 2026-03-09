package instructions

// GetGWSQuickStartInstructions returns inline instructions for using Google Workspace via the gws CLI.
// Appended to the agent's system prompt when GWS access is enabled.
func GetGWSQuickStartInstructions() string {
	return `## Google Workspace CLI (Quick Start)

You have access to Google Workspace services via the ` + "`gws`" + ` CLI tool. Run commands using ` + "`execute_shell_command`" + `.

### Common Commands

**Drive:**
- List files: gws drive files list --params '{"pageSize":10}'
- Search: gws drive files list --params '{"q":"name contains \"report\""}'
- Download: gws drive files export <fileId> --mimeType 'text/plain' > file.txt

**Gmail:**
- List messages: gws gmail messages list --params '{"maxResults":5}'
- Read message: gws gmail messages get <messageId> --params '{"format":"full"}'
- Send email: gws gmail messages send --body '{"raw":"<base64-encoded-email>"}'

**Calendar:**
- List events: gws calendar events list <calendarId> --params '{"maxResults":10,"timeMin":"2024-01-01T00:00:00Z"}'
- Create event: gws calendar events insert <calendarId> --body '{"summary":"Meeting","start":{"dateTime":"..."},"end":{"dateTime":"..."}}'

**Sheets:**
- Read range: gws sheets spreadsheets.values get <spreadsheetId> --range 'Sheet1!A1:D10'
- Write range: gws sheets spreadsheets.values update <spreadsheetId> --range 'Sheet1!A1' --params '{"valueInputOption":"USER_ENTERED"}' --body '{"values":[["a","b"],["c","d"]]}'

**Docs:**
- Get document: gws docs documents get <documentId>

**Slides:**
- Get presentation: gws slides presentations get <presentationId>

### Tips
- Use ` + "`--params`" + ` for query parameters and ` + "`--body`" + ` for request body (JSON strings)
- All IDs are Google resource IDs (long alphanumeric strings)
- For detailed usage per service, read the gws-* skill files: execute_shell_command(command="cat skills/gws-drive/SKILL.md")`
}
