package terminals

import "testing"

func TestParseRowsKeepsMultilineAutoNotificationTogether(t *testing.T) {
	content := "$ gemini --output-format stream-json model=auto msgs=1\n" +
		"> user: [AUTO-NOTIFICATION] Agent 'full-workflow' completed.\n" +
		"  Result: Todo planning complete.\n" +
		"  ### Final Run Summary\n" +
		"  * All steps completed.\n" +
		"[done · 1s · 10 in · 2 out]"

	rows := ParseRows(content)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3: %#v", len(rows), rows)
	}
	if rows[1].Kind != "user" {
		t.Fatalf("row[1].Kind = %q, want user", rows[1].Kind)
	}
	wantText := "[AUTO-NOTIFICATION] Agent 'full-workflow' completed.\n" +
		"Result: Todo planning complete.\n" +
		"### Final Run Summary\n" +
		"* All steps completed."
	if rows[1].Text != wantText {
		t.Fatalf("auto text = %q, want %q", rows[1].Text, wantText)
	}
}

func TestParseRowsSplitsAutoNotificationAssistantReply(t *testing.T) {
	content := "$ gemini --output-format stream-json model=auto msgs=1\n" +
		"> user: [AUTO-NOTIFICATION] Background agent 'full-workflow' started.\n" +
		"  Ack briefly; completion will arrive as a separate AUTO-NOTIFICATION. Do NOT call tools.\n" +
		"  ⚠️ Gemini model is experiencing high demand (503). Retrying automatically, please wait...\n" +
		"  Acknowledged. I will wait for the completion notification.\n" +
		"[done · 1s · 10 in · 2 out]"

	rows := ParseRows(content)
	if len(rows) != 5 {
		t.Fatalf("row count = %d, want 5: %#v", len(rows), rows)
	}
	if rows[1].Kind != "user" {
		t.Fatalf("row[1].Kind = %q, want user", rows[1].Kind)
	}
	wantUser := "[AUTO-NOTIFICATION] Background agent 'full-workflow' started.\n" +
		"Ack briefly; completion will arrive as a separate AUTO-NOTIFICATION. Do NOT call tools."
	if rows[1].Text != wantUser {
		t.Fatalf("auto text = %q, want %q", rows[1].Text, wantUser)
	}
	if rows[2].Kind != "plain" {
		t.Fatalf("row[2].Kind = %q, want plain", rows[2].Kind)
	}
	if rows[3].Kind != "asst" {
		t.Fatalf("row[3].Kind = %q, want asst", rows[3].Kind)
	}
	wantAssistant := "Acknowledged. I will wait for the completion notification."
	if rows[3].Text != wantAssistant {
		t.Fatalf("assistant text = %q, want %q", rows[3].Text, wantAssistant)
	}
	if rows[4].Kind != "done" {
		t.Fatalf("row[4].Kind = %q, want done", rows[4].Kind)
	}
}

func TestParseRowsKeepsMultilineToolResultTogether(t *testing.T) {
	content := "$ codex exec --json\n" +
		"→ tool: mcp_api_bridge_get_api_spec({})\n" +
		"✓ result mcp_api_bridge_get_api_spec: auth: Bearer $MCP_API_TOKEN\n" +
		"POST /tools/custom/call_generic_agent\n" +
		"\n" +
		"instructions: string (required)\n" +
		"[done · 1s · 10 in · 2 out]"

	rows := ParseRows(content)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3: %#v", len(rows), rows)
	}
	if rows[1].Kind != "tool" {
		t.Fatalf("row[1].Kind = %q, want tool", rows[1].Kind)
	}
	wantResult := "auth: Bearer $MCP_API_TOKEN\nPOST /tools/custom/call_generic_agent\n\ninstructions: string (required)"
	if rows[1].Result != wantResult {
		t.Fatalf("tool result = %q, want %q", rows[1].Result, wantResult)
	}
	if rows[2].Kind != "done" {
		t.Fatalf("row[2].Kind = %q, want done", rows[2].Kind)
	}
}

func TestStatusWithRowsCountsVisibleToolCalls(t *testing.T) {
	rows := []Row{
		{Kind: "banner", Text: "vertex.generateContent"},
		{Kind: "tool", Name: "execute_shell_command", Args: `{"command":"one"}`},
		{Kind: "tool", Name: "execute_shell_command", Result: "ok", ResultPrefix: "✓"},
		{Kind: "tool", Name: "execute_shell_command", Args: `{"command":"two"}`},
		{Kind: "tool", Name: "execute_shell_command", Result: "ok", ResultPrefix: "✓"},
		{Kind: "asst", Text: "STATUS: COMPLETED"},
	}

	status := StatusWithRows(Status{ToolName: "old", ToolCount: 1, ToolSummary: "old"}, rows)
	if status.ToolCount != 2 {
		t.Fatalf("tool count = %d, want 2", status.ToolCount)
	}
	if status.ToolName != "execute_shell_command" {
		t.Fatalf("tool name = %q, want execute_shell_command", status.ToolName)
	}
	if status.ToolSummary != "execute_shell_command x2" {
		t.Fatalf("tool summary = %q", status.ToolSummary)
	}
}
