package main

import "sync"

// warmedConversations tracks which conversation IDs have completed at least
// one turn IN THIS PROCESS. agentsession's warm-resume assumes a stable
// SessionID means an actual live tmux/CLI session already holds full context,
// and only sends the newest message to it (see agentsession.Session.Ask) —
// correct once true, but false the moment the process restarts: the frontend
// keeps using the same conversation_id it always did, but the underlying
// warm session died with the old process. Without this check, the first turn
// after any restart would silently go out with zero prior context even
// though the parent/child never asked for a new conversation.
//
// resumableSessionID only returns the id (enabling warm-resume) once this
// process has actually completed a turn for it — otherwise it returns "",
// which makes agentsession start a genuinely fresh session and replay the
// FULL history the caller already has (req.Messages), rather than trimming
// to just the latest message against a session that no longer exists.
var (
	warmedMu sync.Mutex
	warmed   = map[string]bool{}
)

func resumableSessionID(id string) string {
	if id == "" {
		return ""
	}
	warmedMu.Lock()
	defer warmedMu.Unlock()
	if warmed[id] {
		return id
	}
	return ""
}

func markConversationWarm(id string) {
	if id == "" {
		return
	}
	warmedMu.Lock()
	warmed[id] = true
	warmedMu.Unlock()
}
