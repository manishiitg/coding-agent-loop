package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// =====================================================================
// JSONL helpers — read/append for legacy append-only framework files. The
// workspace API does not expose a streaming append, so we read-modify-write
// with a sequence-aware id allocator.
// =====================================================================

// readJSONLLines returns each non-empty line of a JSONL file. Returns ([], false, nil)
// if the file does not exist.
func readJSONLLines(ctx context.Context, filePath string) ([]string, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, filePath)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	if strings.TrimSpace(content) == "" {
		return []string{}, true, nil
	}
	rawLines := strings.Split(content, "\n")
	out := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[Binary file:") {
			continue
		}
		out = append(out, trimmed)
	}
	return out, true, nil
}

// appendJSONLRecord serializes record as JSON and appends it as a new line to
// filePath. Atomic via writeFileToWorkspace temp-file + rename pattern provided
// by the workspace API. Returns the resulting line count.
func appendJSONLRecord(ctx context.Context, filePath string, record interface{}) (int, error) {
	lines, _, err := readJSONLLines(ctx, filePath)
	if err != nil {
		return 0, fmt.Errorf("read jsonl %s: %w", filePath, err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("marshal record for %s: %w", filePath, err)
	}
	lines = append(lines, string(encoded))
	body := strings.Join(lines, "\n") + "\n"
	if err := writeFileToWorkspace(ctx, filePath, body); err != nil {
		return 0, fmt.Errorf("write jsonl %s: %w", filePath, err)
	}
	return len(lines), nil
}

// readJSONLRecords streams JSONL lines and decodes each into a fresh decoded
// value of type T (T must be JSON-decodable). Lines that fail to parse are
// skipped (logged via stderr is the caller's choice). Use this for read-only
// queries against history.jsonl etc.
func readJSONLRecords[T any](ctx context.Context, filePath string) ([]T, error) {
	lines, _, err := readJSONLLines(ctx, filePath)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(lines))
	for _, line := range lines {
		var rec T
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}
