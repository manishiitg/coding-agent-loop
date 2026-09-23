package browser

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RecordAction remembers the last page-changing command a tracked browser
// session ran, for the live Browser panel. Read-only commands (snapshot,
// get, screenshot, ...) keep the previous action. Untracked sessions are
// ignored, like TouchExisting.
func (t *SessionTracker) RecordAction(browserSession, command string, args []string) {
	action := describeBrowserAction(command, args)
	if action == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if info, ok := t.sessions[browserSession]; ok {
		info.lastAction = action
		info.lastActionAt = time.Now()
	}
}

// describeBrowserAction renders an agent-browser command as a short
// human-readable sentence. It never includes typed text, selected values,
// scripts, file paths, or URL query strings: those can carry secrets.
func describeBrowserAction(command string, args []string) string {
	arg := func(i int) string {
		if i < len(args) {
			return strings.TrimSpace(args[i])
		}
		return ""
	}
	target := func(i int) string {
		if value := arg(i); value != "" && !strings.HasPrefix(value, "-") {
			return fmt.Sprintf("%q", truncateActionLabel(value))
		}
		return "an element"
	}
	switch command {
	case "open", "goto", "navigate":
		if page := redactActionURL(arg(0)); page != "" {
			return "Opened " + page
		}
		return "Opened a page"
	case "click":
		return "Clicked " + target(0)
	case "dblclick":
		return "Double-clicked " + target(0)
	case "fill", "type":
		return "Typed into " + target(0)
	case "press", "key":
		if key := arg(0); key != "" && len(key) <= 20 {
			return "Pressed " + key
		}
		return "Pressed a key"
	case "hover":
		return "Hovered " + target(0)
	case "select":
		return "Selected an option in " + target(0)
	case "check":
		return "Checked " + target(0)
	case "uncheck":
		return "Unchecked " + target(0)
	case "upload":
		return "Uploaded a file to " + target(0)
	case "scroll":
		return "Scrolled the page"
	case "back":
		return "Went back"
	case "forward":
		return "Went forward"
	case "reload":
		return "Reloaded the page"
	case "eval":
		return "Ran a script on the page"
	case "find":
		// find <locator> <value> <action> [--name <label>]
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--name" {
				return fmt.Sprintf("Used %q", truncateActionLabel(args[i+1]))
			}
		}
		return "Used " + target(1)
	case "tab":
		if arg(0) == "new" {
			return "Opened a new tab"
		}
		if arg(0) == "close" {
			return "Closed a tab"
		}
		return "Switched tabs"
	}
	return ""
}

func truncateActionLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if runes := []rune(value); len(runes) > 60 {
		return string(runes[:57]) + "..."
	}
	return value
}

// redactActionURL keeps scheme, host and path; drops credentials, query and
// fragment.
func redactActionURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	page := parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
	if runes := []rune(page); len(runes) > 80 {
		page = string(runes[:77]) + "..."
	}
	return page
}
