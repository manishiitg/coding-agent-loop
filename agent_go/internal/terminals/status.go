package terminals

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	ansiEscapePattern     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	callingToolPattern    = regexp.MustCompile(`(?i)\b(?:Calling|Called)\s+([a-zA-Z0-9_.-]+)(?:\s+(\d+)\s+times?)?`)
	geminiToolPattern     = regexp.MustCompile(`^[\s│╎╏]*[✓•]\s+([a-zA-Z0-9_.-]+)\s*\(`)
	codexToolPattern      = regexp.MustCompile(`^[\s│╎╏]*[•-]?\s*Called\s+([a-zA-Z0-9_.-]+)\b`)
	longSeparatorPattern  = regexp.MustCompile(`^[\s─━═-]{16,}$`)
	boxDrawingOnlyPattern = regexp.MustCompile(`^[\s│┃║┌┐└┘├┤┬┴┼╭╮╰╯─━═╎╏]+$`)
	whitespacePattern     = regexp.MustCompile(`\s+`)
	leadingMarkerPattern  = regexp.MustCompile(`^[\s│┃║╎╏>›❯•*-]+`)
	trailingStatusPattern = regexp.MustCompile(`\s*(?:\?|for shortcuts|Shift\+Tab.*|esc to interrupt.*)$`)
)

// DeriveStatus extracts a compact progress summary from a terminal screen.
// It intentionally returns no assistant preview when the screen is ambiguous.
func DeriveStatus(content string, metadata map[string]interface{}) Status {
	provider := providerLabel(content, metadata)
	preview := assistantPreview(content, provider)
	toolName, toolCount := latestToolSummary(content)

	toolSummary := ""
	if toolName != "" {
		if toolCount > 1 {
			toolSummary = toolName + " x" + strconv.Itoa(toolCount)
		} else {
			toolSummary = toolName
		}
	}

	statusText := preview
	if statusText == "" {
		if provider != "" {
			statusText = provider + " is working"
		} else {
			statusText = "Agent is working"
		}
	}

	return Status{
		ProviderLabel:    provider,
		StatusText:       statusText,
		AssistantPreview: preview,
		ToolSummary:      toolSummary,
		ToolName:         toolName,
		ToolCount:        toolCount,
	}
}

func providerLabel(content string, metadata map[string]interface{}) string {
	if provider := firstNonEmpty(stringValue(metadata, "provider"), stringValue(metadata, "provider_id")); provider != "" {
		switch strings.ToLower(provider) {
		case "claude-code", "claudecode":
			return "Claude Code"
		case "gemini-cli", "geminicli":
			return "Gemini CLI"
		case "codex-cli", "codexcli":
			return "Codex CLI"
		default:
			return provider
		}
	}

	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "claude code"):
		return "Claude Code"
	case strings.Contains(lower, "gemini cli"):
		return "Gemini CLI"
	case strings.Contains(lower, "openai codex") || strings.Contains(lower, ">_ openai codex"):
		return "Codex CLI"
	default:
		return ""
	}
}

func assistantPreview(content, provider string) string {
	switch provider {
	case "Claude Code":
		return markerPreview(content, "⏺")
	case "Gemini CLI":
		return markerPreview(content, "✦")
	default:
		return ""
	}
}

func markerPreview(content, marker string) string {
	lines := cleanedLines(content)
	best := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		markerIndex := strings.Index(line, marker)
		if markerIndex < 0 {
			continue
		}
		candidateParts := []string{strings.TrimSpace(line[markerIndex+len(marker):])}
		for j := i + 1; j < len(lines) && len(candidateParts) < 4; j++ {
			next := strings.TrimSpace(lines[j])
			if strings.Contains(next, marker) || isNoisyTerminalLine(next) {
				break
			}
			if next != "" {
				candidateParts = append(candidateParts, next)
			}
		}
		candidate := cleanPreview(strings.Join(candidateParts, " "))
		if candidate != "" {
			best = candidate
		}
	}
	return best
}

func latestToolSummary(content string) (string, int) {
	lines := cleanedLines(content)
	counts := map[string]int{}
	latestTool := ""
	latestCount := 0

	for _, line := range lines {
		tool, count := toolFromLine(line)
		if tool == "" {
			continue
		}
		if count <= 0 {
			counts[tool]++
			count = counts[tool]
		} else if count > counts[tool] {
			counts[tool] = count
		}
		latestTool = tool
		latestCount = count
	}

	return latestTool, latestCount
}

func toolFromLine(line string) (string, int) {
	line = strings.TrimSpace(line)
	for _, pattern := range []*regexp.Regexp{callingToolPattern, geminiToolPattern, codexToolPattern} {
		match := pattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		tool := strings.Trim(match[1], " .,:;")
		if isIgnoredToolName(tool) {
			continue
		}
		count := 0
		if len(match) > 2 && match[2] != "" {
			if parsed, err := strconv.Atoi(match[2]); err == nil {
				count = parsed
			}
		}
		return tool, count
	}
	return "", 0
}

func isIgnoredToolName(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "", "working", "thinking", "vibing":
		return true
	default:
		return false
	}
}

func cleanedLines(content string) []string {
	rawLines := strings.Split(ansiEscapePattern.ReplaceAllString(content, ""), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.Trim(line, "│┃║╎╏"))
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func cleanPreview(value string) string {
	value = strings.TrimSpace(value)
	value = leadingMarkerPattern.ReplaceAllString(value, "")
	value = trailingStatusPattern.ReplaceAllString(value, "")
	value = whitespacePattern.ReplaceAllString(value, " ")
	value = strings.TrimSpace(value)
	if value == "" || isNoisyTerminalLine(value) {
		return ""
	}
	if len(value) > 180 {
		value = strings.TrimSpace(value[:180]) + "..."
	}
	return value
}

func isNoisyTerminalLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || boxDrawingOnlyPattern.MatchString(line) || longSeparatorPattern.MatchString(line) {
		return true
	}
	lower := strings.ToLower(line)
	noisyPrefixes := []string{
		"calling ",
		"called ",
		"working ",
		"press ",
		"shift+tab",
		"workspace ",
		"sandbox ",
		"model:",
		"directory:",
		"claude code v",
		"gemini cli v",
		"openai codex",
		"tips for getting started",
		"what's new",
		"authenticated with",
		"question id:",
		"raw operator question:",
	}
	for _, prefix := range noisyPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	noisyContains := []string{
		"ctrl+o",
		"esc to interrupt",
		"for shortcuts",
		"tokens",
		"thought for",
		"thinking with",
		"still thinking",
		"type your message",
		"mcp server",
	}
	for _, needle := range noisyContains {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func terminalStateFromContent(content string, active bool) string {
	lower := strings.ToLower(strings.Join(cleanedLines(content), "\n"))
	switch {
	case strings.Contains(lower, "status: failed"),
		strings.Contains(lower, "pre-validation failed"),
		strings.Contains(lower, "llm generation error"),
		strings.Contains(lower, "conversation error"),
		strings.Contains(lower, "agent error:"),
		strings.Contains(lower, "failed to "),
		strings.Contains(lower, " error details:"):
		return "failed"
	case strings.Contains(lower, "status: completed"),
		strings.Contains(lower, "completed successfully"),
		strings.Contains(lower, "status: complete"):
		return "completed"
	case active:
		return "running"
	default:
		return "completed"
	}
}
