package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
	"github.com/spf13/cobra"
)

var migrateDurableChatsCmd = &cobra.Command{
	Use:   "migrate-chat-events",
	Short: "Import legacy interactive chat JSON into the canonical SQLite log",
	RunE: func(cmd *cobra.Command, _ []string) error {
		docsRoot, _ := cmd.Flags().GetString("docs-root")
		stateRoot, _ := cmd.Flags().GetString("state-root")
		if strings.TrimSpace(docsRoot) == "" {
			docsRoot = fsutil.WorkspaceDocsRoot()
		}
		if strings.TrimSpace(stateRoot) == "" {
			var err error
			stateRoot, err = workflowCLIStateRoot()
			if err != nil {
				return err
			}
		}
		force, _ := cmd.Flags().GetBool("force")
		return migrateDurableChatsFromDisk(filepath.Clean(docsRoot), filepath.Clean(stateRoot), force)
	},
}

func init() {
	migrateDurableChatsCmd.Flags().String("docs-root", "", "absolute workspace documents root")
	migrateDurableChatsCmd.Flags().String("state-root", "", "absolute AgentWorks state root")
	migrateDurableChatsCmd.Flags().Bool("force", false, "rerun even when the one-time marker exists")
}

func migrateDurableChatsFromDisk(docsRoot, stateRoot string, force bool) error {
	if !filepath.IsAbs(docsRoot) || !filepath.IsAbs(stateRoot) {
		return fmt.Errorf("docs-root and state-root must be absolute")
	}
	markerPath := filepath.Join(stateRoot, "migrations", "chat-events-v2.done")
	if !force {
		if _, err := os.Stat(markerPath); err == nil {
			fmt.Printf("durable chat migration already complete: marker=%s\n", markerPath)
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	journal, err := internalevents.OpenSQLiteEventJournal(filepath.Join(stateRoot, "structured-chat-events-v2.sqlite"))
	if err != nil {
		return err
	}
	store := internalevents.NewEventStore(1)
	store.SetDurableJournal(journal)
	defer store.Stop()

	type candidate struct {
		path    string
		raw     []byte
		updated time.Time
		ownerID string
	}
	candidates := make(map[string]candidate)
	var skipped int
	err = filepath.WalkDir(docsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "session-") || !strings.HasSuffix(entry.Name(), "-conversation.json") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var header struct {
			SessionID string `json:"session_id"`
			UpdatedAt string `json:"updated_at"`
			UserID    string `json:"user_id"`
		}
		if jsonErr := json.Unmarshal(raw, &header); jsonErr != nil {
			return fmt.Errorf("decode %s: %w", path, jsonErr)
		}
		sessionID := strings.TrimSpace(header.SessionID)
		if sessionID == "" || strings.HasPrefix(sessionID, "schedule-") || strings.HasPrefix(sessionID, "sched_") || strings.HasPrefix(sessionID, "bot-") {
			skipped++
			return nil
		}
		updated, _ := time.Parse(time.RFC3339Nano, header.UpdatedAt)
		if updated.IsZero() {
			if info, infoErr := entry.Info(); infoErr == nil {
				updated = info.ModTime()
			}
		}
		if previous, exists := candidates[sessionID]; !exists || updated.After(previous.updated) {
			ownerID := strings.TrimSpace(header.UserID)
			if ownerID == "" {
				ownerID = legacyChatOwnerFromPath(path)
			}
			candidates[sessionID] = candidate{path: path, raw: raw, updated: updated, ownerID: ownerID}
		}
		return nil
	})
	if err != nil {
		return err
	}

	sessionIDs := make([]string, 0, len(candidates))
	for sessionID := range candidates {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	var migrated int
	for _, sessionID := range sessionIDs {
		candidate := candidates[sessionID]
		complete, stateErr := store.DurableChatMigrationComplete(sessionID)
		if stateErr != nil {
			return stateErr
		}
		if complete && !force {
			skipped++
			continue
		}
		if err := store.SetSessionPersistenceClass(sessionID, internalevents.SessionPersistenceInteractiveChat); err != nil {
			return err
		}
		store.SetSessionOwner(sessionID, candidate.ownerID)
		events, decodeErr := decodeLegacyChatEvents(candidate.raw, sessionID)
		if decodeErr != nil {
			return fmt.Errorf("migrate %s: %w", candidate.path, decodeErr)
		}
		if importErr := store.ImportDurableChatEvents(sessionID, events); importErr != nil {
			return fmt.Errorf("migrate %s: %w", candidate.path, importErr)
		}
		migrated++
	}
	if err := journal.Checkpoint(); err != nil {
		return fmt.Errorf("checkpoint durable chat journal: %w", err)
	}
	fmt.Printf("durable chat migration complete: scanned=%d migrated=%d skipped=%d database=%s\n", len(sessionIDs), migrated, skipped, filepath.Join(stateRoot, "structured-chat-events-v2.sqlite"))
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(markerPath), ".chat-events-v2.*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := fmt.Fprintf(temporary, "completed_at=%s\nscanned=%d\nmigrated=%d\n", time.Now().UTC().Format(time.RFC3339), len(sessionIDs), migrated); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, markerPath)
}

func legacyChatOwnerFromPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index, part := range parts {
		if (part == "_users" || part == "users") && index+1 < len(parts) && strings.TrimSpace(parts[index+1]) != "" {
			return parts[index+1]
		}
	}
	return "default"
}
