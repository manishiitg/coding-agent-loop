package events

// ForwardEventPage is a bounded page for external clients. Cursors are absolute
// event indices, including filtered events. CursorReset tells a client that its
// cursor is outside the retained log (for example, after a server restart).
type ForwardEventPage struct {
	GetEventsResult
	CursorReset         bool
	FirstAvailableIndex int
}

// GetForwardEventPage returns at most limit non-streaming events in order. It
// scans the retained log in place instead of allocating a filtered full copy.
// Unlike the legacy UI polling path, an initial request is bounded as well.
func (es *EventStore) GetForwardEventPage(sessionID string, sinceIndex, limit int) ForwardEventPage {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	es.mu.RLock()
	defer es.mu.RUnlock()
	rows, exists := es.events[sessionID]
	base := es.sessionStartIndices[sessionID]
	page := ForwardEventPage{
		GetEventsResult:     GetEventsResult{Events: []Event{}, Exists: exists, TotalCount: len(rows), LastProcessedIndex: base - 1},
		FirstAvailableIndex: base,
	}
	last := base + len(rows) - 1
	if !exists || sinceIndex > last {
		page.CursorReset = sinceIndex > last
		page.LastProcessedIndex = last
		return page
	}
	if sinceIndex < base-1 {
		page.CursorReset = true
	}
	start := 0
	if sinceIndex >= base {
		start = sinceIndex - base + 1
	}
	page.LastProcessedIndex = base + start - 1
	for i := start; i < len(rows); i++ {
		if shouldReturnEvent(rows[i], false) {
			if len(page.Events) == limit {
				page.HasMore = true
				break
			}
			page.Events = append(page.Events, rows[i])
		}
		page.LastProcessedIndex = base + i
	}
	return page
}
