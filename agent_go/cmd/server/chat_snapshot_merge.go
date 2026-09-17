package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// mergeChatConversationSnapshots merges two snapshots, not a provider's text
// transcript. Keep raw message identities: projecting tool rows to role/text
// turns every tool-only row into the same key and corrupts later alignments.
// The result contains both input sequences in order, so replaying either
// snapshot is idempotent. Repeated equal rows keep their occurrence count.
func mergeChatConversationSnapshots(canonical, incoming map[string]interface{}) ([]json.RawMessage, error) {
	old, ok := builderConversationRawHistory(canonical)
	if !ok {
		return nil, fmt.Errorf("invalid canonical conversation history")
	}
	next, ok := builderConversationRawHistory(incoming)
	if !ok {
		return nil, fmt.Errorf("invalid incoming conversation history")
	}
	positions := make(map[string][]int, len(old))
	for i, raw := range old {
		key, err := chatSnapshotMessageKey(raw)
		if err != nil {
			return nil, err
		}
		positions[key] = append(positions[key], i)
	}
	merged := make([]json.RawMessage, 0, len(old)+len(next))
	pending := make([]json.RawMessage, 0)
	cursor := 0
	for _, raw := range next {
		key, err := chatSnapshotMessageKey(raw)
		if err != nil {
			return nil, err
		}
		candidates := positions[key]
		index := sort.SearchInts(candidates, cursor)
		if index == len(candidates) {
			pending = append(pending, raw)
			continue
		}
		match := candidates[index]
		// Canonical-only answers precede newly submitted rows in the same gap.
		merged = append(merged, old[cursor:match]...)
		merged = append(merged, pending...)
		pending = pending[:0]
		merged = append(merged, old[match])
		cursor = match + 1
	}
	merged = append(merged, old[cursor:]...)
	merged = append(merged, pending...)
	return merged, nil
}

func chatSnapshotMessageKey(raw json.RawMessage) (string, error) {
	// Match structured message content, keeping full tool IDs/arguments/results.
	// Top-level trace enrichment is not a new message; retain the canonical row
	// including that enrichment when the runtime snapshot omits it.
	var value map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if role, ok := value["Role"]; ok {
		if parts, exists := value["Parts"]; exists {
			value = map[string]interface{}{"Role": role, "Parts": parts}
		}
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
