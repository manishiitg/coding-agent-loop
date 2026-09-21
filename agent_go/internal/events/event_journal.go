package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DurableEventJournal is the persistence boundary for the canonical structured
// chat stream. Append assigns a monotonic per-session sequence atomically and
// reports whether event_id was new; replaying the same event is idempotent.
type DurableEventJournal interface {
	Append(sessionID string, event Event) (persisted Event, inserted bool, err error)
	LoadTail(sessionID string, limit int) ([]Event, error)
	Close() error
}

type SQLiteEventJournal struct {
	db   *sql.DB
	path string
}

type EventJournalStats struct {
	Path      string
	SizeBytes int64
	Events    int64
}

type DurableEventJournalStats interface {
	Stats() (EventJournalStats, error)
}

func OpenSQLiteEventJournal(path string) (*SQLiteEventJournal, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("structured event journal path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS structured_chat_events (
			session_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			event_id TEXT NOT NULL,
			payload BLOB NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (session_id, sequence),
			UNIQUE (session_id, event_id)
		)`,
		`CREATE TABLE IF NOT EXISTS structured_chat_sequences (
			session_id TEXT PRIMARY KEY,
			last_sequence INTEGER NOT NULL
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize structured event journal: %w", err)
		}
	}
	_ = os.Chmod(path, 0o600)
	return &SQLiteEventJournal{db: db, path: path}, nil
}

func (j *SQLiteEventJournal) Append(sessionID string, event Event) (Event, bool, error) {
	if j == nil || j.db == nil || sessionID == "" || event.ID == "" {
		return Event{}, false, fmt.Errorf("structured event journal requires session_id and event_id")
	}
	tx, err := j.db.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return Event{}, false, err
	}
	defer tx.Rollback()
	var existing []byte
	if err := tx.QueryRow(`SELECT payload FROM structured_chat_events WHERE session_id = ? AND event_id = ?`, sessionID, event.ID).Scan(&existing); err == nil {
		var persisted Event
		if json.Unmarshal(existing, &persisted) != nil {
			return Event{}, false, fmt.Errorf("decode existing structured event")
		}
		return persisted, false, nil
	} else if err != sql.ErrNoRows {
		return Event{}, false, err
	}

	var last int64
	if err := tx.QueryRow(`SELECT last_sequence FROM structured_chat_sequences WHERE session_id = ?`, sessionID).Scan(&last); err != nil && err != sql.ErrNoRows {
		return Event{}, false, err
	}
	next := last + 1
	// Live-only streaming events consume in-memory sequence numbers without
	// paying a durable transaction. Preserve their gap in the next semantic
	// boundary so restart cursors remain monotonic across the full live stream.
	if event.Sequence > next {
		next = event.Sequence
	}
	event.Sequence = next
	event.SessionID = sessionID
	payload, err := json.Marshal(event)
	if err != nil {
		return Event{}, false, err
	}
	if _, err := tx.Exec(`INSERT INTO structured_chat_events(session_id, sequence, event_id, payload, created_at) VALUES(?, ?, ?, ?, ?)`,
		sessionID, next, event.ID, payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Event{}, false, err
	}
	if _, err := tx.Exec(`INSERT INTO structured_chat_sequences(session_id, last_sequence) VALUES(?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_sequence = excluded.last_sequence`, sessionID, next); err != nil {
		return Event{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, false, err
	}
	return event, true, nil
}

func (j *SQLiteEventJournal) Stats() (EventJournalStats, error) {
	if j == nil || j.db == nil {
		return EventJournalStats{}, fmt.Errorf("structured event journal is closed")
	}
	stats := EventJournalStats{Path: j.path}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(j.path + suffix)
		if err == nil {
			stats.SizeBytes += info.Size()
		} else if !os.IsNotExist(err) {
			return EventJournalStats{}, err
		}
	}
	if err := j.db.QueryRow(`SELECT COUNT(*) FROM structured_chat_events`).Scan(&stats.Events); err != nil {
		return EventJournalStats{}, err
	}
	return stats, nil
}

func (j *SQLiteEventJournal) LoadTail(sessionID string, limit int) ([]Event, error) {
	if j == nil || j.db == nil || sessionID == "" {
		return []Event{}, nil
	}
	if limit <= 0 {
		limit = 1500
	}
	rows, err := j.db.Query(`SELECT payload FROM structured_chat_events WHERE session_id = ? ORDER BY sequence DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reversed := make([]Event, 0, limit)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var event Event
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, err
		}
		reversed = append(reversed, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]Event, len(reversed))
	for index := range reversed {
		result[len(reversed)-1-index] = reversed[index]
	}
	return result, nil
}

func (j *SQLiteEventJournal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}
