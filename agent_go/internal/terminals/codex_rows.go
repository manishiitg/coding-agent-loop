package terminals

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	codexCompletedLSLinePattern    = regexp.MustCompile(`^(total [0-9]+|[-dlcbsp][rwx-]{9}[@+]? +[0-9]+ +\S+)`)
	completedAgyShellEchoSuffix    = regexp.MustCompile(`\s\d+(?:\.\d+)?(?:ms|s)\s*$`)
	completedAgyFoundCountLine     = regexp.MustCompile(`^found\s+[\d,]+\s+(files?|matches?|results?|symbols?)\b`)
	completedAgyMultiToolLine      = regexp.MustCompile(`^(?:read|grepped|globbed|listed|searched)[,\s].*\b\d+\s+(?:files?|greps?|globs?|matches?|results?|symbols?|reads?|lists?|searches?)\b`)
	completedAgyEarlierHiddenLine  = regexp.MustCompile(`^(?:…|\.\.\.)\s*\d+\s+earlier\s+(?:items?|tools?|results?)\b`)
	completedAgyReadFileLine       = regexp.MustCompile(`^read\s+(?:\.\.\.|/|~)\S*\s+(?:lines?\s+\d+(?:-\d+)?|.*\.\w{1,8}\b)`)
	completedAgyMCPToolCardLine    = regexp.MustCompile(`^[\w.-]+/[\w.-]+\(`)
	completedAgyToolCountLine      = regexp.MustCompile(`^[+-]\s*\d+\s+tools?\b`)
	completedOpenCodeToolCallLine  = regexp.MustCompile(`(?i)^(read|write|edit|bash|grep|glob|list|tool|tool_use|todowrite|task)\s*\(`)
	completedOpenCodeToolStateLine = regexp.MustCompile(`(?i)^(running|completed|failed|errored)\s+(read|write|edit|bash|grep|glob|tool)\b`)
)

// RowsForCompletedTmuxSnapshot extracts a renderable assistant row from a
// completed direct CLI tmux pane. Live panes still render through xterm; this
// only upgrades the inactive final answer so markdown survives the terminal
// fallback path.
func RowsForCompletedTmuxSnapshot(snapshot Snapshot) []Row {
	if snapshot.Active || strings.TrimSpace(snapshot.Content) == "" {
		return nil
	}
	var text string
	switch completedTmuxSnapshotProvider(snapshot) {
	case "codex":
		text = completedCodexTmuxAssistantText(snapshot.Content)
	case "agy":
		text = completedAgyTmuxAssistantText(snapshot.Content)
	case "opencode":
		text = completedOpenCodeTmuxAssistantText(snapshot.Content)
	default:
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []Row{{Kind: "asst", Text: text}}
}

func completedTmuxSnapshotProvider(snapshot Snapshot) string {
	label := strings.ToLower(strings.TrimSpace(snapshot.Status.ProviderLabel))
	content := strings.ToLower(snapshot.Content)
	switch {
	case strings.Contains(label, "codex") || providerLabel(snapshot.Content, nil) == "Codex CLI":
		return "codex"
	case strings.Contains(label, "agy") ||
		strings.Contains(label, "antigravity") ||
		strings.Contains(content, "antigravity cli") ||
		strings.Contains(content, "agy agent"):
		return "agy"
	case strings.Contains(label, "opencode") ||
		strings.Contains(label, "open code") ||
		strings.Contains(content, "opencode"):
		return "opencode"
	default:
		return ""
	}
}

func completedCodexTmuxAssistantText(content string) string {
	lines := completedCodexPaneLines(content)
	if text := completedCodexFramedAnswer(lines); text != "" {
		return text
	}
	return completedCodexLastAssistantSegment(lines)
}

func completedCodexPaneLines(content string) []string {
	rawLines := strings.Split(ansiEscapePattern.ReplaceAllString(content, ""), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		line := strings.TrimRight(raw, " \t\r")
		line = strings.TrimSpace(strings.Trim(line, "│┃║╎╏"))
		lines = append(lines, line)
	}
	return lines
}

func completedCodexFramedAnswer(lines []string) string {
	rules := make([]int, 0, 4)
	for i, line := range lines {
		if completedCodexHorizontalRuleLine(line) {
			rules = append(rules, i)
		}
	}
	if len(rules) >= 2 {
		for i := len(rules) - 1; i > 0; i-- {
			if text := cleanCompletedCodexAnswerLines(lines[rules[i-1]+1:rules[i]], false); text != "" {
				return text
			}
		}
	}
	if len(rules) == 1 {
		return cleanCompletedCodexAnswerLines(lines[rules[0]+1:], true)
	}
	return ""
}

func completedCodexLastAssistantSegment(lines []string) string {
	segments := make([][]string, 0, 4)
	current := make([]string, 0, 8)
	flush := func() {
		if text := cleanCompletedCodexAnswerLines(current, false); text != "" {
			segments = append(segments, strings.Split(text, "\n"))
		}
		current = current[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(current) > 0 {
				current = append(current, "")
			}
			continue
		}
		if completedCodexChromeOrToolLine(trimmed) {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	if len(segments) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(segments[len(segments)-1], "\n"))
}

func cleanCompletedCodexAnswerLines(lines []string, stopAtPrompt bool) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		if stopAtPrompt && completedCodexPromptLine(trimmed) {
			break
		}
		if completedCodexChromeOrToolLine(trimmed) {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if completedCodexLikelyToolReplay(out) {
		return ""
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func completedCodexChromeOrToolLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	unbulleted := strings.TrimSpace(strings.TrimPrefix(trimmed, "• "))
	unbulletedLower := strings.ToLower(unbulleted)
	if trimmed == "" {
		return true
	}
	if completedCodexPromptLine(trimmed) ||
		completedCodexHorizontalRuleLine(trimmed) ||
		boxDrawingOnlyPattern.MatchString(trimmed) ||
		strings.Contains(lower, "openai codex") ||
		strings.Contains(lower, "chatgpt.com/codex") ||
		strings.Contains(lower, "esc to interrupt") ||
		strings.Contains(lower, "ctrl+o") ||
		strings.Contains(lower, "shift+tab") ||
		strings.Contains(lower, "type your message") ||
		strings.Contains(lower, "use /skills") ||
		strings.Contains(unbulletedLower, "worked for") ||
		strings.HasPrefix(unbulletedLower, "status: completed") ||
		strings.HasPrefix(unbulletedLower, "status: complete") ||
		strings.HasPrefix(unbulletedLower, "working (") ||
		strings.HasPrefix(unbulletedLower, "updated plan") ||
		strings.HasPrefix(lower, "searching the web") ||
		strings.HasPrefix(lower, "searched http") ||
		strings.HasPrefix(lower, "spawned ") ||
		strings.HasPrefix(lower, "waiting for ") ||
		strings.HasPrefix(lower, "finished waiting") ||
		strings.HasPrefix(trimmed, "└") ||
		strings.HasPrefix(trimmed, "├") ||
		strings.HasPrefix(trimmed, "│") ||
		completedCodexToolStatusLine(trimmed) ||
		completedCodexToolReplayLine(trimmed) {
		return true
	}
	if strings.Contains(lower, "tokens") &&
		(strings.Contains(lower, "↑") || strings.Contains(lower, "↓") || strings.Contains(lower, "thought for") || strings.Contains(lower, "thinking with")) {
		return true
	}
	return false
}

func completedCodexPromptLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return trimmed == "›" ||
		trimmed == ">" ||
		trimmed == "❯" ||
		strings.HasPrefix(trimmed, "› ") ||
		strings.HasPrefix(trimmed, "❯ ") ||
		strings.HasPrefix(lower, "gpt-") && strings.Contains(lower, "·")
}

func completedCodexHorizontalRuleLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	dashes := 0
	other := 0
	for _, r := range trimmed {
		switch r {
		case '─', '━', '╌', '╍', '-':
			dashes++
		case ' ':
		default:
			other++
		}
	}
	return dashes >= 20 && other == 0
}

func completedCodexToolStatusLine(line string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "• "))
	lower := strings.ToLower(trimmed)
	if trimmed == "Called" || trimmed == "Calling" {
		return true
	}
	if !strings.HasPrefix(lower, "called ") && !strings.HasPrefix(lower, "calling ") {
		return false
	}
	rest := strings.TrimSpace(trimmed[strings.Index(trimmed, " ")+1:])
	return strings.Contains(rest, "(") ||
		strings.Contains(rest, "{") ||
		strings.Contains(rest, ".") ||
		strings.Contains(rest, "_") ||
		strings.Contains(strings.ToLower(rest), "api-bridge")
}

func completedCodexToolReplayLine(line string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(line, "• "))
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, `"stdout"`) ||
		strings.Contains(lower, `"stderr"`) ||
		strings.Contains(lower, `"exit_code"`) ||
		strings.Contains(lower, `"execution_time_ms"`) ||
		strings.Contains(lower, "curl -ss") ||
		strings.Contains(lower, "requests.post") ||
		strings.Contains(lower, "json.dumps") ||
		strings.Contains(lower, "python3 - <<") ||
		strings.Contains(lower, "cat <<json") ||
		strings.Contains(lower, "find . -maxdepth") ||
		strings.Contains(lower, "ls -l") ||
		strings.Contains(lower, "allowed folders") ||
		strings.Contains(lower, "cannot read from") ||
		strings.Contains(lower, "operation not permitted") ||
		strings.Contains(lower, "mcp_api_token") ||
		strings.Contains(lower, "authorization: bearer") ||
		strings.Contains(lower, "/tools/custom/") ||
		strings.Contains(lower, "/tools/mcp/") ||
		strings.HasPrefix(trimmed, "./") ||
		strings.HasPrefix(trimmed, "../") ||
		codexCompletedLSLinePattern.MatchString(trimmed)
}

func completedCodexLikelyToolReplay(lines []string) bool {
	nonEmpty := 0
	toolish := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		nonEmpty++
		if completedCodexToolReplayLine(trimmed) || completedCodexToolStatusLine(trimmed) {
			toolish++
		}
	}
	return nonEmpty > 0 && toolish == nonEmpty
}

func completedAgyTmuxAssistantText(content string) string {
	lines := strings.Split(ansiEscapePattern.ReplaceAllString(content, ""), "\n")
	out := make([]string, 0, len(lines))
	skipThoughtTitle := false
	for _, line := range lines {
		trimmed := completedAgyNormalizePaneLine(line)
		if draft, ok := completedAgyPromptLineDraft(trimmed); ok {
			if strings.TrimSpace(draft) != "" && !completedAgyPromptPlaceholder(draft) {
				out = out[:0]
				skipThoughtTitle = false
				continue
			}
			break
		}
		if completedAgyPromptBoundaryLine(trimmed) {
			break
		}
		if skipThoughtTitle && trimmed != "" {
			skipThoughtTitle = false
			continue
		}
		if completedAgyUserTurnHeader(trimmed) {
			out = out[:0]
			continue
		}
		if trimmed == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		if completedAgyThoughtStatusLine(trimmed) {
			out = out[:0]
			skipThoughtTitle = true
			continue
		}
		if completedAgyToolStatusLine(trimmed) {
			out = out[:0]
			continue
		}
		if completedAgyLineStartsWithSpinner(trimmed) ||
			completedAgyTUILine(trimmed) ||
			completedAgyBoxDrawingLine(trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func completedAgyNormalizePaneLine(line string) string {
	line = strings.TrimSpace(ansiEscapePattern.ReplaceAllString(line, ""))
	line = strings.TrimPrefix(line, "│")
	line = strings.TrimSuffix(line, "│")
	line = strings.TrimSpace(line)
	line = strings.TrimSpace(strings.TrimPrefix(line, "Assistant:"))
	return line
}

func completedAgyPromptLineDraft(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{">", "›", "❯", "→"} {
		if trimmed == prefix {
			return "", true
		}
		if strings.HasPrefix(trimmed, prefix+" ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)), true
		}
	}
	return "", false
}

func completedAgyPromptPlaceholder(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return lower == "" ||
		strings.Contains(lower, "type your message") ||
		strings.Contains(lower, "what can i help") ||
		strings.Contains(lower, "add a follow-up") ||
		strings.Contains(lower, "ask (shift+tab")
}

func completedAgyPromptBoundaryLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "→") ||
		trimmed == ">" ||
		trimmed == "›" ||
		trimmed == "❯" ||
		strings.Contains(lower, "ask (shift+tab") ||
		strings.HasPrefix(lower, "type your message") ||
		strings.Contains(lower, "what can i help") ||
		strings.Contains(lower, "add a follow-up") ||
		strings.Contains(lower, "agy agent") && strings.Contains(lower, "workspace")
}

func completedAgyUserTurnHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "User:") && !strings.HasPrefix(trimmed, "user:") {
		return false
	}
	if len(trimmed) == len("User:") {
		return true
	}
	next := trimmed[len("User:")]
	return next == ' ' || next == '\t'
}

func completedAgyThoughtStatusLine(line string) bool {
	trimmed := strings.TrimLeft(strings.TrimSpace(line), "▸▾ ")
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "thought for ") ||
		strings.HasPrefix(lower, "thought process")
}

func completedAgyToolStatusLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	nativeToolLine := strings.TrimSpace(strings.TrimLeft(trimmed, "●○◦* "))
	nativeToolLower := strings.ToLower(nativeToolLine)
	for _, prefix := range []string{
		"bash(", "generateimage(", "listpermissions(", "read(", "write(",
		"edit(", "grep(", "glob(",
	} {
		if strings.HasPrefix(nativeToolLower, prefix) {
			return true
		}
	}
	if completedAgyMCPToolCardLine.MatchString(nativeToolLower) {
		return true
	}
	if strings.HasPrefix(lower, "thinking") ||
		strings.HasPrefix(lower, "working") ||
		strings.HasPrefix(lower, "running") ||
		strings.HasPrefix(lower, "reading") ||
		strings.HasPrefix(lower, "editing") ||
		strings.HasPrefix(lower, "writing") ||
		strings.HasPrefix(lower, "searching") ||
		strings.HasPrefix(lower, "applying") ||
		strings.HasPrefix(lower, "calling ") ||
		strings.HasPrefix(lower, "called ") ||
		strings.HasPrefix(lower, "executing") ||
		strings.HasPrefix(lower, "globbed ") ||
		strings.HasPrefix(lower, "listed ") ||
		strings.HasPrefix(lower, "grepped ") ||
		strings.Contains(lower, "mcp") && strings.Contains(lower, "tool") ||
		strings.Contains(lower, "shell(") ||
		strings.Contains(lower, `"stdout"`) ||
		strings.Contains(lower, `"stderr"`) ||
		strings.Contains(lower, `"exit_code"`) {
		return true
	}
	if completedAgyToolCountLine.MatchString(lower) ||
		strings.HasPrefix(trimmed, "-H ") ||
		strings.HasPrefix(trimmed, "-d ") ||
		completedAgyFoundCountLine.MatchString(lower) ||
		completedAgyMultiToolLine.MatchString(lower) ||
		completedAgyEarlierHiddenLine.MatchString(lower) ||
		completedAgyReadFileLine.MatchString(lower) {
		return true
	}
	if strings.HasPrefix(trimmed, "$ ") && completedAgyShellEchoSuffix.MatchString(lower) {
		return true
	}
	return strings.Contains(lower, "truncated") &&
		(strings.Contains(lower, "more lines") || strings.Contains(lower, "more line"))
}

func completedAgyTUILine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	if trimmed == "" {
		return true
	}
	return strings.Contains(lower, "ctrl+") ||
		strings.HasSuffix(lower, "collapse)") ||
		strings.HasSuffix(lower, "to collapse)") ||
		strings.Contains(lower, "esc to") ||
		strings.Contains(lower, "press enter") ||
		strings.Contains(lower, "run everything") ||
		strings.Contains(lower, "ask (shift+tab") ||
		strings.HasPrefix(lower, "v20") ||
		strings.Contains(lower, "try composer") ||
		strings.Contains(lower, "composer") && strings.Contains(lower, "fast") ||
		strings.Contains(trimmed, " · ") ||
		strings.HasPrefix(trimmed, "→ ") ||
		strings.Contains(lower, "agy agent") ||
		strings.Contains(lower, "agy") && strings.Contains(lower, "model") ||
		strings.Contains(lower, "workspace:") ||
		strings.Contains(lower, "mode:") ||
		strings.Contains(lower, "approval") ||
		strings.Contains(lower, "permission") ||
		strings.Contains(lower, "pasted text") ||
		strings.HasPrefix(lower, "use /") ||
		strings.HasPrefix(lower, "add a follow-up") ||
		strings.HasPrefix(lower, "auto-run") ||
		strings.HasPrefix(lower, "user:")
}

func completedAgyBoxDrawingLine(line string) bool {
	if line == "" {
		return true
	}
	if strings.Contains(line, "│") || strings.Contains(line, "|") {
		return false
	}
	for _, r := range line {
		if strings.ContainsRune("─━▀▄▁▂▃▅▆▇█▌▐▝▜▗▟▘▛▙▚▞▖╭╮╰╯│┌┐└┘├┤┬┴┼╞╪╡╘╧╛╔╗╚╝═║╠╣╦╩╬╌╍╎╏┄┅┆┇┈┉┊┋ ", r) {
			continue
		}
		return false
	}
	return true
}

func completedAgyLineStartsWithSpinner(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(trimmed)
	return strings.ContainsRune("⠁⠃⠇⠧⠷⠿⠻⠹⠸⠼⠴⠦⠖⠒⠐⣾⣽⣻⢿⡿⣟⣯⣷", first)
}

func completedOpenCodeTmuxAssistantText(content string) string {
	return completedGenericLastAssistantSegment(completedCodexPaneLines(content), completedOpenCodeChromeOrToolLine)
}

func completedGenericLastAssistantSegment(lines []string, isChrome func(string) bool) string {
	segments := make([]string, 0, 4)
	current := make([]string, 0, 8)
	flush := func() {
		if text := cleanCompletedGenericAnswerLines(current, isChrome); text != "" {
			segments = append(segments, text)
		}
		current = current[:0]
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(current) > 0 {
				current = append(current, "")
			}
			continue
		}
		if isChrome(trimmed) {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	if len(segments) == 0 {
		return ""
	}
	return strings.TrimSpace(segments[len(segments)-1])
}

func cleanCompletedGenericAnswerLines(lines []string, isChrome func(string) bool) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		if isChrome(trimmed) {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func completedOpenCodeChromeOrToolLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	unbulleted := strings.TrimSpace(strings.TrimLeft(trimmed, "•●○◦*- "))
	unbulletedLower := strings.ToLower(unbulleted)
	if trimmed == "" {
		return true
	}
	if completedCodexHorizontalRuleLine(trimmed) ||
		boxDrawingOnlyPattern.MatchString(trimmed) ||
		completedCodexPromptLine(trimmed) ||
		completedOpenCodeToolCallLine.MatchString(unbulleted) ||
		completedOpenCodeToolStateLine.MatchString(unbulleted) ||
		lower == "opencode" ||
		strings.Contains(lower, "opencode cli") ||
		strings.Contains(lower, "ctrl+") ||
		strings.Contains(lower, "esc to") ||
		strings.Contains(lower, "type your message") ||
		strings.HasPrefix(unbulletedLower, "status: completed") ||
		strings.HasPrefix(unbulletedLower, "status: complete") ||
		strings.HasPrefix(unbulletedLower, "working") ||
		strings.HasPrefix(unbulletedLower, "thinking") ||
		strings.HasPrefix(unbulletedLower, "calling ") ||
		strings.HasPrefix(unbulletedLower, "called ") ||
		strings.Contains(unbulletedLower, `"stdout"`) ||
		strings.Contains(unbulletedLower, `"stderr"`) ||
		strings.Contains(unbulletedLower, `"exit_code"`) ||
		strings.Contains(unbulletedLower, "authorization: bearer") ||
		strings.Contains(unbulletedLower, "/tools/custom/") ||
		strings.Contains(unbulletedLower, "/tools/mcp/") {
		return true
	}
	return strings.Contains(lower, "tokens") &&
		(strings.Contains(lower, "thought") || strings.Contains(lower, "thinking") || strings.Contains(lower, "input") || strings.Contains(lower, "output"))
}
