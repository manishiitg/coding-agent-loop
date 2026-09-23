package events

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// JournalMaintenanceConfig bounds the chat journal without deleting history:
// rows older than CompactAfter and larger than MinBytes move to artifacts, and
// above MaxBytes the age threshold steps down, oldest rows first.
type JournalMaintenanceConfig struct {
	CompactAfter time.Duration
	MinBytes     int
	MaxBytes     int64
	InitialDelay time.Duration
	Interval     time.Duration
}

func LoadJournalMaintenanceConfig() JournalMaintenanceConfig {
	cfg := JournalMaintenanceConfig{
		CompactAfter: 30 * 24 * time.Hour,
		MinBytes:     8 * 1024,
		MaxBytes:     2 << 30,
		InitialDelay: 2 * time.Minute,
		Interval:     24 * time.Hour,
	}
	if days, ok := positiveEnvInt("CHAT_JOURNAL_COMPACT_AFTER_DAYS"); ok {
		cfg.CompactAfter = time.Duration(days) * 24 * time.Hour
	}
	if minBytes, ok := positiveEnvInt("CHAT_JOURNAL_COMPACT_MIN_BYTES"); ok {
		cfg.MinBytes = int(minBytes)
	}
	if maxBytes, ok := positiveEnvInt("CHAT_JOURNAL_MAX_BYTES"); ok {
		cfg.MaxBytes = maxBytes
	}
	return cfg
}

func positiveEnvInt(name string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	return value, err == nil && value > 0
}

type JournalMaintenanceReport struct {
	Compacted       int
	UsedBytes       int64
	OverCap         bool
	NeedsFullVacuum bool
}

// sizeGuardAges are the progressively younger thresholds used once the
// journal is over its cap, always compacting the oldest rows first.
var sizeGuardAges = []time.Duration{14 * 24 * time.Hour, 7 * 24 * time.Hour, 24 * time.Hour, 0}

// RunDurableJournalMaintenance compacts old bulk and enforces the size cap.
// Each session is compacted under its append lock, so appends, deletes and
// compaction of one chat never interleave.
func (es *EventStore) RunDurableJournalMaintenance(cfg JournalMaintenanceConfig, now time.Time) (JournalMaintenanceReport, error) {
	var report JournalMaintenanceReport
	es.mu.RLock()
	journal, ok := es.durableJournal.(DurableEventJournalCompactor)
	es.mu.RUnlock()
	if !ok || journal == nil {
		return report, nil
	}
	compact := func(age time.Duration) error {
		cutoff := now.Add(-age)
		sessions, err := journal.CompactionCandidates(cutoff, cfg.MinBytes)
		if err != nil {
			return err
		}
		for _, sessionID := range sessions {
			unlock := es.lockSessionAppend(sessionID)
			count, err := journal.CompactSession(sessionID, cutoff, cfg.MinBytes)
			unlock()
			report.Compacted += count
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err := compact(cfg.CompactAfter); err != nil {
		return report, err
	}
	used, err := journal.UsedBytes()
	if err != nil {
		return report, err
	}
	if cfg.MaxBytes > 0 && used > cfg.MaxBytes {
		report.OverCap = true
		log.Printf("[EventStore] ALERT structured journal over size cap used_bytes=%d cap_bytes=%d; compacting oldest rows first", used, cfg.MaxBytes)
		for _, age := range sizeGuardAges {
			if age >= cfg.CompactAfter {
				continue
			}
			if err := compact(age); err != nil {
				return report, err
			}
			if used, err = journal.UsedBytes(); err != nil {
				return report, err
			}
			if used <= cfg.MaxBytes {
				break
			}
		}
		if used > cfg.MaxBytes {
			log.Printf("[EventStore] ALERT structured journal still over size cap after compaction used_bytes=%d cap_bytes=%d", used, cfg.MaxBytes)
		}
	}
	needsVacuum, err := journal.Reclaim()
	if err != nil {
		return report, err
	}
	report.UsedBytes = used
	report.NeedsFullVacuum = needsVacuum
	return report, nil
}

func (es *EventStore) runJournalMaintenanceLoop(cfg JournalMaintenanceConfig) {
	timer := time.NewTimer(cfg.InitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			report, err := es.RunDurableJournalMaintenance(cfg, time.Now())
			if err != nil {
				log.Printf("[EventStore] structured journal maintenance failed: %v", err)
			} else {
				log.Printf("[EventStore] structured journal maintenance compacted=%d used_bytes=%d over_cap=%t needs_one_time_vacuum=%t",
					report.Compacted, report.UsedBytes, report.OverCap, report.NeedsFullVacuum)
			}
			timer.Reset(cfg.Interval)
		case <-es.stopCh:
			return
		}
	}
}

// ReadDurableChatArtifact returns the complete event behind a summarized row.
// Authorization is the caller's job and must match durable reads.
func (es *EventStore) ReadDurableChatArtifact(sessionID, artifactID string) ([]byte, error) {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return nil, ErrChatArtifactNotFound
	}
	es.mu.RLock()
	journal, ok := es.durableJournal.(interface {
		ReadChatArtifact(sessionID, artifactID string) ([]byte, error)
	})
	es.mu.RUnlock()
	if !ok || journal == nil {
		return nil, ErrChatArtifactNotFound
	}
	return journal.ReadChatArtifact(sessionID, artifactID)
}
