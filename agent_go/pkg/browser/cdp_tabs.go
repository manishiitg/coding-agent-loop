package browser

import (
	"fmt"
	"strings"
	"sync"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

var (
	sharedCDPLocks sync.Map // map[string]*sync.Mutex

	cdpTabSelectionsMu sync.RWMutex
	cdpTabSelections   = make(map[string]string)
	cdpActiveTabs      = make(map[int]string)
)

func sharedCDPSessionName(port int) string {
	return fmt.Sprintf("shared-cdp-%d", port)
}

func sharedCDPLock(port int) *sync.Mutex {
	key := fmt.Sprintf("cdp:%d", port)
	actual, _ := sharedCDPLocks.LoadOrStore(key, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func cdpTabSelectionKey(port int, ownerID string) string {
	return fmt.Sprintf("cdp:%d:%s", port, strings.TrimSpace(ownerID))
}

func getCDPTabSelection(port int, ownerID string) string {
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	return cdpTabSelections[cdpTabSelectionKey(port, ownerID)]
}

func setCDPTabSelection(port int, ownerID, tab string) {
	tab = strings.TrimSpace(tab)
	if tab == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpTabSelections[cdpTabSelectionKey(port, ownerID)] = tab
}

func clearCDPTabSelection(port int, ownerID string) {
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	delete(cdpTabSelections, cdpTabSelectionKey(port, ownerID))
}

func clearCDPTabSelectionsForPort(port int) {
	prefix := fmt.Sprintf("cdp:%d:", port)
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	for key := range cdpTabSelections {
		if strings.HasPrefix(key, prefix) {
			delete(cdpTabSelections, key)
		}
	}
	delete(cdpActiveTabs, port)
}

func getCDPActiveTab(port int) string {
	cdpTabSelectionsMu.RLock()
	defer cdpTabSelectionsMu.RUnlock()
	return cdpActiveTabs[port]
}

func setCDPActiveTab(port int, tab string) {
	tab = strings.TrimSpace(tab)
	if tab == "" {
		return
	}
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	cdpActiveTabs[port] = tab
}

func clearCDPActiveTab(port int, tab string) {
	tab = strings.TrimSpace(tab)
	cdpTabSelectionsMu.Lock()
	defer cdpTabSelectionsMu.Unlock()
	if tab == "" || cdpActiveTabs[port] == tab {
		delete(cdpActiveTabs, port)
	}
}

func cdpOwnerID(workflowSessionID, agentSessionID, session string) string {
	for _, candidate := range []string{agentSessionID, workflowSessionID} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if resolved := common.ResolveBrowserSessionID(candidate, "default"); resolved != "" && resolved != "default" {
			return resolved
		}
	}
	for _, candidate := range []string{workflowSessionID, agentSessionID, session} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return "default"
}

func parseTabSelection(args []string) (tab string, clear bool, err error) {
	if len(args) == 0 {
		return "", false, nil
	}

	switch args[0] {
	case "list", "ls":
		return "", false, nil
	case "new":
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "--label" {
				label := strings.TrimSpace(args[i+1])
				if label == "" {
					return "", false, fmt.Errorf("CDP shared-browser tab creation requires a non-empty --label")
				}
				return label, false, nil
			}
		}
		return "", false, fmt.Errorf("CDP shared-browser tab creation requires --label <label> so later commands can target it")
	case "close":
		if len(args) > 1 {
			return strings.TrimSpace(args[1]), true, nil
		}
		return "", false, nil
	default:
		if strings.HasPrefix(args[0], "-") {
			return "", false, nil
		}
		return strings.TrimSpace(args[0]), false, nil
	}
}

func extractInlineCDPTab(args []string) (tab string, cleaned []string, err error) {
	cleaned = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "tab", "--tab":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", nil, fmt.Errorf("CDP shared-browser command requires %s <tab-id-or-label>", args[i])
			}
			if tab != "" {
				return "", nil, fmt.Errorf("CDP shared-browser command includes multiple tab selections; include exactly one")
			}
			tab = strings.TrimSpace(args[i+1])
			i++
		default:
			cleaned = append(cleaned, args[i])
		}
	}
	return tab, cleaned, nil
}

func stringArgs(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, arg := range v {
			if s, ok := arg.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return nil
	}
}

func stripCDPArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--cdp" {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}
