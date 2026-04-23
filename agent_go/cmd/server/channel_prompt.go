package server

// buildChannelFormattingInstructions returns a system-prompt fragment telling
// the agent how to format its replies for a specific chat channel. Both Slack
// and WhatsApp share a minimal "mrkdwn-like" subset — single-asterisk bold,
// underscore italic, tilde strikethrough, triple-backtick code — and explicitly
// do NOT render standard markdown headers, tables, or [text](url) link
// syntax. Injecting this at session start gets the LLM to emit channel-
// native text directly, so the outbound formatter has less normalisation to
// do and less formatting is lost in translation.
//
// Empty string when the platform is unknown / empty — caller can branch on
// that to skip AppendSystemPrompt.
//
// Based on OpenClaw's "WhatsApp Styler" and Slack mrkdwn guidance; see
// docs.openclaw.ai/concepts/markdown-formatting for prior art.
func buildChannelFormattingInstructions(platform string) string {
	switch platform {
	case "slack":
		return `# Output formatting — Slack

Your reply is rendered in Slack (mrkdwn). Use this exact markup:

- Bold: *text* (single asterisks, NOT **text**)
- Italic: _text_ (single underscores)
- Strikethrough: ~text~ (single tildes)
- Inline code: ` + "`text`" + `
- Code block: ` + "```" + `language\ncode\n` + "```" + `
- Bullet list: use "• " (the bullet char) or "- " at line start
- Numbered list: "1. ", "2. ", ... (renders plain but readable)
- Links: paste the URL directly — Slack auto-linkifies. Do NOT use [text](url) markdown, it shows literally.
- Mentions: <@USERID> (if you know the user) — otherwise just say the name.

Do NOT use:
- Markdown headers (#, ##, ###) — Slack shows them as literal "# " text.
- Tables — Slack has no table rendering. Use a bulleted list or paste as a code block with column alignment instead.
- HTML.

Keep individual messages under 3000 characters where possible — Slack caps at ~40k, but long single messages are harder to read on mobile.`

	case "whatsapp":
		return `# Output formatting — WhatsApp

Your reply is sent via WhatsApp. WhatsApp's text formatting is a small subset of markdown. Use ONLY:

- Bold: *text* (single asterisks, NOT **text**)
- Italic: _text_ (single underscores)
- Strikethrough: ~text~ (single tildes)
- Inline code: ` + "`text`" + `
- Code block: ` + "```" + `\ncode\n` + "```" + ` (language hint ignored)
- Bullet list: start lines with "• " (the bullet char) — WhatsApp does NOT style "- " or "*" as bullets.
- Numbered list: "1. ", "2. ", ... (renders plain).
- Links: paste the URL directly — WhatsApp auto-detects them. Do NOT use [text](url); it shows as literal "[text](url)" text.

Do NOT use:
- Markdown headers (#, ##, ###) — WhatsApp renders them as literal "# " text. For section titles, put the title on its own line in *bold* instead.
- Tables — WhatsApp has no table rendering; they become misaligned ASCII. If you need tabular data, format as key: value pairs on separate lines, or a numbered list.
- Nested markup (e.g. bold inside italic) — unreliable.
- HTML.

Keep replies concise — WhatsApp caps individual messages at ~4000 characters, and small phone screens make long walls of text hard to read.`
	}
	return ""
}
