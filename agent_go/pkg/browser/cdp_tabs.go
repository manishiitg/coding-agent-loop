package browser

import (
	"fmt"
	"strings"
	"sync"
)

var (
	sharedCDPLocks sync.Map // map[string]*sync.Mutex

	cdpTabSelectionsMu sync.RWMutex
	cdpTabSelections   = make(map[string]string)
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
}

func cdpOwnerID(workflowSessionID, agentSessionID, session string) string {
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
	case "list":
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
