package terminals

import (
	"regexp"
	"strconv"
	"strings"
)

// Row is a typed view of a synthetic terminal transcript. Content remains the
// durable/debug representation; rows let the UI render without guessing from
// indentation and newline placement.
type Row struct {
	Kind         string `json:"kind"`
	Text         string `json:"text,omitempty"`
	Name         string `json:"name,omitempty"`
	Args         string `json:"args,omitempty"`
	Result       string `json:"result,omitempty"`
	ResultPrefix string `json:"result_prefix,omitempty"`
}

var (
	fullToolResultRowPattern  = regexp.MustCompile(`^([✓✗])\s+result\s+([^:]+):\s*(.*)$`)
	shortToolResultRowPattern = regexp.MustCompile(`^([✓✗])\s+(\S+)\s+\(([^)]+)\)\s*(.*)$`)
	toolStartRowPattern       = regexp.MustCompile(`^tool:\s*([^(]+)\((.*)\)$`)
)

// ParseRows turns the synthetic terminal text protocol into typed rows. It is
// intentionally conservative: unknown lines stay plain, and multiline user,
// assistant, and tool-result blocks remain attached to their owning row.
func ParseRows(content string) []Row {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	rows := make([]Row, 0, len(lines))
	activeToolResultIndex := -1
	activeTextRowIndex := -1

	for _, line := range lines {
		if match := fullToolResultRowPattern.FindStringSubmatch(line); match != nil {
			activeTextRowIndex = -1
			for i := len(rows) - 1; i >= 0; i-- {
				if rows[i].Kind == "tool" && rows[i].Name == strings.TrimSpace(match[2]) && rows[i].Result == "" {
					rows[i].Result = match[3]
					rows[i].ResultPrefix = match[1]
					activeToolResultIndex = i
					break
				}
			}
			continue
		}
		if match := shortToolResultRowPattern.FindStringSubmatch(line); match != nil {
			activeTextRowIndex = -1
			for i := len(rows) - 1; i >= 0; i-- {
				if rows[i].Kind == "tool" && rows[i].Name == match[2] && rows[i].Result == "" {
					if match[4] != "" {
						rows[i].Result = match[3] + " · " + match[4]
					} else {
						rows[i].Result = match[3]
					}
					rows[i].ResultPrefix = match[1]
					activeToolResultIndex = i
					break
				}
			}
			continue
		}

		if activeToolResultIndex >= 0 && !isRowBoundary(line) {
			rows[activeToolResultIndex].Result = appendRowText(rows[activeToolResultIndex].Result, line)
			continue
		}
		activeToolResultIndex = -1

		if activeTextRowIndex >= 0 && !isRowBoundary(line) {
			if rows[activeTextRowIndex].Kind == "user" || rows[activeTextRowIndex].Kind == "asst" {
				continuation := strings.TrimPrefix(line, "  ")
				if rows[activeTextRowIndex].Kind == "user" && isAutoNotificationText(rows[activeTextRowIndex].Text) {
					if isAutoNotificationStatusLine(continuation) {
						rows = append(rows, Row{Kind: "plain", Text: continuation})
						continue
					}
					if isAutoNotificationAssistantLine(continuation) {
						rows = append(rows, Row{Kind: "asst", Text: continuation})
						activeTextRowIndex = len(rows) - 1
						continue
					}
				}
				rows[activeTextRowIndex].Text = appendRowText(rows[activeTextRowIndex].Text, continuation)
				continue
			}
		}

		row := classifyRow(line)
		if row.Kind == "asst" && len(rows) > 0 && rows[len(rows)-1].Kind == "asst" {
			rows[len(rows)-1].Text = appendRowText(rows[len(rows)-1].Text, row.Text)
			activeTextRowIndex = len(rows) - 1
			continue
		}
		if row.Kind == "asst" && strings.HasPrefix(line, "  ") {
			rows = append(rows, Row{Kind: "plain", Text: line})
			activeTextRowIndex = -1
			continue
		}
		if row.Kind == "user" || row.Kind == "asst" {
			activeTextRowIndex = len(rows)
		} else {
			activeTextRowIndex = -1
		}
		rows = append(rows, row)
	}
	return rows
}

func classifyRow(line string) Row {
	switch {
	case strings.HasPrefix(line, "$ "):
		return Row{Kind: "banner", Text: strings.TrimPrefix(line, "$ ")}
	case strings.HasPrefix(line, "↳ "):
		return Row{Kind: "context", Text: strings.TrimPrefix(line, "↳ ")}
	case strings.HasPrefix(line, "> user: "):
		return Row{Kind: "user", Text: strings.TrimPrefix(line, "> user: ")}
	case strings.HasPrefix(line, "< asst: "):
		return Row{Kind: "asst", Text: strings.TrimPrefix(line, "< asst: ")}
	case strings.HasPrefix(line, "  "):
		return Row{Kind: "asst", Text: strings.TrimPrefix(line, "  ")}
	case strings.HasPrefix(line, "[image "), strings.HasPrefix(line, "[document "):
		return Row{Kind: "attachment", Text: line}
	case strings.HasPrefix(line, "[done"):
		return Row{Kind: "done", Text: line}
	case strings.HasPrefix(line, "[error]"):
		return Row{Kind: "error", Text: strings.TrimSpace(strings.TrimPrefix(line, "[error]"))}
	case strings.HasPrefix(line, "→ "):
		rest := strings.TrimPrefix(line, "→ ")
		if match := toolStartRowPattern.FindStringSubmatch(rest); match != nil {
			return Row{Kind: "tool", Name: strings.TrimSpace(match[1]), Args: match[2]}
		}
		if idx := strings.IndexByte(rest, ' '); idx > 0 {
			return Row{Kind: "tool", Name: rest[:idx], Args: rest[idx+1:]}
		}
		return Row{Kind: "tool", Name: rest}
	default:
		return Row{Kind: "plain", Text: line}
	}
}

func isRowBoundary(line string) bool {
	return strings.HasPrefix(line, "$ ") ||
		strings.HasPrefix(line, "↳ ") ||
		strings.HasPrefix(line, "> user: ") ||
		strings.HasPrefix(line, "< asst: ") ||
		strings.HasPrefix(line, "[image ") ||
		strings.HasPrefix(line, "[document ") ||
		strings.HasPrefix(line, "[done") ||
		strings.HasPrefix(line, "[error]") ||
		strings.HasPrefix(line, "→ ") ||
		fullToolResultRowPattern.MatchString(line) ||
		shortToolResultRowPattern.MatchString(line)
}

func appendRowText(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "\n" + next
}

func StatusWithRows(status Status, rows []Row) Status {
	toolName, toolCount := ToolSummaryFromRows(rows)
	status.ToolName = toolName
	status.ToolCount = toolCount
	if toolName == "" {
		status.ToolSummary = ""
		return status
	}
	if toolCount > 1 {
		status.ToolSummary = toolName + " x" + strconv.Itoa(toolCount)
	} else {
		status.ToolSummary = toolName
	}
	return status
}

func ToolSummaryFromRows(rows []Row) (string, int) {
	startKeys := map[string]int{}
	resultKeys := map[string]int{}
	latest := ""
	for _, row := range rows {
		if row.Kind != "tool" {
			continue
		}
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = "tool"
		}
		if row.Args != "" {
			startKeys[name+"\x00"+row.Args] = 1
			latest = name
			continue
		}
		if row.Result != "" {
			resultKeys[name+"\x00"+row.Result] = 1
			if latest == "" {
				latest = name
			}
		}
	}
	total := len(startKeys)
	if total == 0 {
		total = len(resultKeys)
	}
	return latest, total
}

func isAutoNotificationText(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "[AUTO-NOTIFICATION]")
}

func isAutoNotificationStatusLine(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "⚠️")
}

func isAutoNotificationAssistantLine(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "Acknowledged.") ||
		strings.HasPrefix(trimmed, "Got it.") ||
		strings.HasPrefix(trimmed, "Understood.")
}
