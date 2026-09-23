package events

import (
	"encoding/json"
	"time"
)

// Retention never deletes a conversation. Old bulky rows are moved into their
// private artifact and replaced in place by a summary, so history stays
// complete while the journal stays small.

const compactedArtifactSummaryBytes = 2 * 1024

// DurableEventJournalCompactor is implemented by journals that support
// in-place compaction of old rows.
type DurableEventJournalCompactor interface {
	CompactionCandidates(olderThan time.Time, minBytes int) ([]string, error)
	CompactSession(sessionID string, olderThan time.Time, minBytes int) (int, error)
	UsedBytes() (int64, error)
	Reclaim() (needsFullVacuum bool, err error)
}

// CompactionCandidates lists sessions holding compactable rows, oldest first.
func (j *SQLiteEventJournal) CompactionCandidates(olderThan time.Time, minBytes int) ([]string, error) {
	if j == nil || j.db == nil {
		return nil, nil
	}
	rows, err := j.db.Query(`SELECT session_id FROM structured_chat_events
		WHERE created_at < ? AND length(payload) > ?
		GROUP BY session_id ORDER BY MIN(created_at)`, olderThan.UTC().Format(time.RFC3339Nano), minBytes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		sessions = append(sessions, sessionID)
	}
	return sessions, rows.Err()
}

// CompactSession summarizes one session's old, large rows in place (same
// event id and sequence). User messages and existing summaries are never
// touched, so the pass is idempotent. The caller must hold the session's
// append lock so a concurrent delete cannot orphan a freshly written artifact.
func (j *SQLiteEventJournal) CompactSession(sessionID string, olderThan time.Time, minBytes int) (int, error) {
	if j == nil || j.db == nil || sessionID == "" {
		return 0, nil
	}
	type candidate struct {
		sequence int64
		payload  []byte
	}
	rows, err := j.db.Query(`SELECT sequence, payload FROM structured_chat_events
		WHERE session_id = ? AND created_at < ? AND length(payload) > ? ORDER BY sequence`,
		sessionID, olderThan.UTC().Format(time.RFC3339Nano), minBytes)
	if err != nil {
		return 0, err
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.sequence, &c.payload); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	compacted := 0
	for _, c := range candidates {
		var event Event
		if json.Unmarshal(c.payload, &event) != nil || event.ID == "" {
			continue
		}
		if event.Type == "user_message" || IsChatArtifactSummary(event) {
			continue
		}
		summary := summarizeChatArtifactEvent(event, ChatArtifactID(event.ID), len(c.payload), compactedArtifactSummaryBytes)
		encoded, err := json.Marshal(summary)
		if err != nil || len(encoded) >= len(c.payload) {
			continue
		}
		if _, err := j.writeChatArtifact(sessionID, event.ID, c.payload); err != nil {
			return compacted, err
		}
		// Compare-and-swap on the old payload: a concurrent rewrite wins.
		result, err := j.db.Exec(`UPDATE structured_chat_events SET payload = ?
			WHERE session_id = ? AND sequence = ? AND payload = ?`, encoded, sessionID, c.sequence, c.payload)
		if err != nil {
			return compacted, err
		}
		if affected, _ := result.RowsAffected(); affected == 1 {
			compacted++
		}
	}
	return compacted, nil
}

// UsedBytes is the logical size of the journal (pages in use). Unlike the file
// size it drops as soon as rows are compacted, before space is reclaimed.
func (j *SQLiteEventJournal) UsedBytes() (int64, error) {
	if j == nil || j.db == nil {
		return 0, nil
	}
	var pageCount, freePages, pageSize int64
	for query, target := range map[string]*int64{
		`PRAGMA page_count`:     &pageCount,
		`PRAGMA freelist_count`: &freePages,
		`PRAGMA page_size`:      &pageSize,
	} {
		if err := j.db.QueryRow(query).Scan(target); err != nil {
			return 0, err
		}
	}
	return (pageCount - freePages) * pageSize, nil
}

// Reclaim checkpoints the WAL and returns free pages to the filesystem when
// the database was created with incremental auto-vacuum. Older databases need
// a one-time offline VACUUM, which is reported rather than run here.
func (j *SQLiteEventJournal) Reclaim() (bool, error) {
	if j == nil || j.db == nil {
		return false, nil
	}
	if _, err := j.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return false, err
	}
	var mode int
	if err := j.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return false, err
	}
	if mode != 2 {
		return true, nil
	}
	if _, err := j.db.Exec(`PRAGMA incremental_vacuum`); err != nil {
		return false, err
	}
	_, err := j.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return false, err
}
