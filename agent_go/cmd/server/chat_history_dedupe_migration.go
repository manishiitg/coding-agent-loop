package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
	"github.com/spf13/cobra"
)

// Chat history dedupe (one-time migration). Until c630fc680 the turn-end merge
// re-appended a chat's whole in-memory history onto its saved file whenever a
// side writer had added a row, so saved histories grew by whole copies: on RTS
// every chat over 5 MB was 54-98% duplicates (37,882 rows, 1,407 unique;
// 166,969 rows, 1,802 unique). This removes the copies:
//
//   - tool calls and tool results are identified by their call ID; each ID
//     keeps its fullest version (a transcript catch-up joins the assistant's
//     text to the call) at the place the ID first appeared;
//   - other rows are dropped only when the same row already followed the same
//     preceding row, so a message legitimately sent twice survives;
//   - only the newest system prompt is kept, first.
//
// Files are streamed twice rather than decoded whole, backed up under the
// state root before being replaced, and locked against live chat writes.

const (
	chatDedupeMarkerName = "chat-history-dedupe-v1.done"
	chatDedupeMinBytes   = 256 << 10
)

var dedupeChatHistoryCmd = &cobra.Command{
	Use:   "dedupe-chat-history",
	Short: "Remove duplicated copies from saved chat histories (one-time; backs up originals)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		docsRoot, _ := cmd.Flags().GetString("docs-root")
		stateRoot, _ := cmd.Flags().GetString("state-root")
		if strings.TrimSpace(docsRoot) == "" {
			docsRoot = fsutil.WorkspaceDocsRoot()
		}
		if strings.TrimSpace(stateRoot) == "" {
			var err error
			if stateRoot, err = workflowCLIStateRoot(); err != nil {
				return err
			}
		}
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		docsRoot, _ = filepath.Abs(docsRoot)
		stateRoot, _ = filepath.Abs(stateRoot)
		_, err := dedupeChatHistories(docsRoot, stateRoot, force, dryRun)
		return err
	},
}

func init() {
	dedupeChatHistoryCmd.Flags().String("docs-root", "", "absolute workspace documents root")
	dedupeChatHistoryCmd.Flags().String("state-root", "", "absolute AgentWorks state root")
	dedupeChatHistoryCmd.Flags().Bool("force", false, "rerun even when the one-time marker exists")
	dedupeChatHistoryCmd.Flags().Bool("dry-run", false, "report what would change without writing")
}

// dedupeChatHistoriesAtStartup runs the migration once, in the background so
// a large archive never delays the listener. Failures are logged; without the
// marker the next start retries.
func dedupeChatHistoriesAtStartup(stateRoot string) {
	docsRoot, err := filepath.Abs(fsutil.WorkspaceDocsRoot())
	if err != nil {
		log.Printf("[CHAT_DEDUPE] cannot resolve workspace docs root: %v", err)
		return
	}
	go func() {
		if _, err := dedupeChatHistories(docsRoot, filepath.Clean(stateRoot), false, false); err != nil {
			log.Printf("[CHAT_DEDUPE] failed; will retry on next start: %v", err)
		}
	}()
}

type chatDedupeStats struct {
	Scanned, Rewritten, Failed int
	RowsBefore, RowsAfter      int
	BytesBefore, BytesAfter    int64
}

func dedupeChatHistories(docsRoot, stateRoot string, force, dryRun bool) (chatDedupeStats, error) {
	var stats chatDedupeStats
	markerPath := filepath.Join(stateRoot, "migrations", chatDedupeMarkerName)
	if !force && !dryRun {
		if _, err := os.Stat(markerPath); err == nil {
			return stats, nil
		}
	}
	if info, err := os.Stat(docsRoot); err != nil || !info.IsDir() {
		return stats, fmt.Errorf("workspace docs root does not exist: %s", docsRoot)
	}
	var paths []string
	_ = filepath.WalkDir(docsRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "-conversation.json") {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Size() >= chatDedupeMinBytes {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	backupRoot := filepath.Join(stateRoot, "migrations", "chat-history-dedupe-v1-backup")
	for _, path := range paths {
		stats.Scanned++
		rel, err := filepath.Rel(docsRoot, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		result, err := dedupeChatHistoryFile(path, filepath.Join(backupRoot, rel), rel, dryRun)
		if err != nil {
			stats.Failed++
			log.Printf("[CHAT_DEDUPE] %s: %v", rel, err)
			continue
		}
		stats.RowsBefore += result.rowsBefore
		stats.RowsAfter += result.rowsAfter
		stats.BytesBefore += result.bytesBefore
		stats.BytesAfter += result.bytesAfter
		if result.rewritten {
			stats.Rewritten++
			log.Printf("[CHAT_DEDUPE] %s: %d -> %d rows, %.1f -> %.1f MB", rel, result.rowsBefore, result.rowsAfter, float64(result.bytesBefore)/1e6, float64(result.bytesAfter)/1e6)
		}
	}
	summary := fmt.Sprintf("scanned=%d rewritten=%d failed=%d rows=%d->%d bytes=%.1fMB->%.1fMB dry_run=%v",
		stats.Scanned, stats.Rewritten, stats.Failed, stats.RowsBefore, stats.RowsAfter, float64(stats.BytesBefore)/1e6, float64(stats.BytesAfter)/1e6, dryRun)
	log.Printf("[CHAT_DEDUPE] complete: %s", summary)
	if dryRun {
		return stats, nil
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		return stats, err
	}
	return stats, os.WriteFile(markerPath, []byte("completed_at="+time.Now().UTC().Format(time.RFC3339)+"\n"+summary+"\n"), 0o600)
}

type chatDedupeFileResult struct {
	rowsBefore, rowsAfter   int
	bytesBefore, bytesAfter int64
	rewritten               bool
}

type chatDedupeRow struct {
	key      string
	system   bool
	callIDs  []string
	parts    int
	size     int
}

func dedupeChatHistoryFile(path, backupPath, lockKey string, dryRun bool) (chatDedupeFileResult, error) {
	var result chatDedupeFileResult
	lock := chatConversationMutex(lockKey)
	lock.Lock()
	defer lock.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	result.bytesBefore = info.Size()

	// Pass 1: one small record per row.
	var rows []chatDedupeRow
	err = streamChatConversation(path, nil, func(index int, raw json.RawMessage) error {
		row, err := describeChatDedupeRow(raw)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return result, err
	}
	result.rowsBefore = len(rows)
	keep := planChatDedupe(rows)
	result.rowsAfter = len(keep)
	if len(keep) >= len(rows) || len(rows) == 0 {
		result.rowsAfter = len(rows)
		result.bytesAfter = result.bytesBefore
		return result, nil
	}
	if dryRun {
		result.bytesAfter = result.bytesBefore * int64(len(keep)) / int64(len(rows))
		return result, nil
	}

	// Pass 2: collect the kept rows (small by construction), then rewrite the
	// file with every other top-level field copied as-is.
	kept := make(map[int]json.RawMessage, len(keep))
	wanted := make(map[int]bool, len(keep))
	for _, index := range keep {
		wanted[index] = true
	}
	var fields []chatDedupeField
	err = streamChatConversation(path, &fields, func(index int, raw json.RawMessage) error {
		if wanted[index] {
			kept[index] = append(json.RawMessage(nil), raw...)
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return result, err
	}
	if err := copyChatDedupeBackup(path, backupPath); err != nil {
		return result, fmt.Errorf("backup: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".dedupe-*.json")
	if err != nil {
		return result, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	writer := bufio.NewWriterSize(temporary, 1<<20)
	if err := writeDedupedChatConversation(writer, fields, keep, kept); err != nil {
		temporary.Close()
		return result, err
	}
	if err := writer.Flush(); err != nil {
		temporary.Close()
		return result, err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return result, err
	}
	if err := temporary.Close(); err != nil {
		return result, err
	}
	if after, err := os.Stat(temporaryPath); err == nil {
		result.bytesAfter = after.Size()
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return result, err
	}
	result.rewritten = true
	return result, nil
}

func describeChatDedupeRow(raw json.RawMessage) (chatDedupeRow, error) {
	var message struct {
		Role  string                   `json:"Role"`
		Parts []map[string]interface{} `json:"Parts"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return chatDedupeRow{}, err
	}
	key, err := chatSnapshotMessageKey(raw)
	if err != nil {
		return chatDedupeRow{}, err
	}
	sum := sha256.Sum256([]byte(key))
	row := chatDedupeRow{key: hex.EncodeToString(sum[:16]), system: message.Role == "system", parts: len(message.Parts), size: len(raw)}
	for _, part := range message.Parts {
		if id, _ := part["ToolCallID"].(string); id != "" {
			row.callIDs = append(row.callIDs, "result:"+id)
		} else if id, _ := part["ID"].(string); id != "" && part["FunctionCall"] != nil {
			row.callIDs = append(row.callIDs, "call:"+id)
		}
	}
	return row, nil
}

// planChatDedupe returns the indices to keep, in output order.
func planChatDedupe(rows []chatDedupeRow) []int {
	// The fullest version of each call ID, and where that ID first appeared.
	best := map[string]int{}
	first := map[string]int{}
	for i, row := range rows {
		for _, id := range row.callIDs {
			if _, ok := first[id]; !ok {
				first[id] = i
			}
			if current, ok := best[id]; !ok || row.parts > rows[current].parts || (row.parts == rows[current].parts && row.size > rows[current].size) {
				best[id] = i
			}
		}
	}
	type slot struct{ position, index int }
	var slots []slot
	emitted := map[int]bool{}
	seenPairs := map[[2]string]bool{}
	// Every adjacent pair seen so far, of any row kind: the start of a
	// re-appended copy follows the previous copy's last row (a new pair) but
	// is followed by the same row as the first time.
	allPairs := map[[2]string]bool{}
	seenKeys := map[string]bool{}
	lastSystem := -1
	previous := ""
	for i, row := range rows {
		startsCopy := seenKeys[row.key] && i+1 < len(rows) && allPairs[[2]string{row.key, rows[i+1].key}]
		allPairs[[2]string{previous, row.key}] = true
		seenKeys[row.key] = true
		switch {
		case row.system:
			lastSystem = i
		case len(row.callIDs) > 0:
			for _, id := range row.callIDs {
				chosen := best[id]
				if chosen == i && !emitted[i] {
					emitted[i] = true
					position := i
					for _, other := range row.callIDs {
						if first[other] < position {
							position = first[other]
						}
					}
					slots = append(slots, slot{position, i})
				}
			}
		default:
			pair := [2]string{previous, row.key}
			if !seenPairs[pair] && !startsCopy {
				seenPairs[pair] = true
				slots = append(slots, slot{i, i})
			}
		}
		previous = row.key
	}
	sort.SliceStable(slots, func(a, b int) bool { return slots[a].position < slots[b].position })
	keep := make([]int, 0, len(slots)+1)
	if lastSystem >= 0 {
		keep = append(keep, lastSystem)
	}
	for _, s := range slots {
		keep = append(keep, s.index)
	}
	return keep
}

type chatDedupeField struct {
	name  string
	value json.RawMessage // nil for conversation_history
}

// streamChatConversation walks a conversation record, calling onRow for each
// conversation_history element; when fields is non-nil it also collects the
// other top-level fields in order.
func streamChatConversation(path string, fields *[]chatDedupeField, onRow func(int, json.RawMessage) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReaderSize(file, 1<<20))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return errors.New("not a JSON object")
	}
	sawHistory := false
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, _ := token.(string)
		if name != "conversation_history" {
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			if fields != nil {
				*fields = append(*fields, chatDedupeField{name: name, value: value})
			}
			continue
		}
		sawHistory = true
		if fields != nil {
			*fields = append(*fields, chatDedupeField{name: name})
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
			if token == nil {
				continue // null history
			}
			return errors.New("conversation_history is not an array")
		}
		for index := 0; decoder.More(); index++ {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return err
			}
			if err := onRow(index, raw); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
	}
	if !sawHistory {
		return errors.New("no conversation_history")
	}
	return nil
}

func writeDedupedChatConversation(w io.Writer, fields []chatDedupeField, keep []int, kept map[int]json.RawMessage) error {
	write := func(s string) error { _, err := io.WriteString(w, s); return err }
	if err := write("{"); err != nil {
		return err
	}
	for i, field := range fields {
		if i > 0 {
			if err := write(","); err != nil {
				return err
			}
		}
		name, _ := json.Marshal(field.name)
		if err := write(string(name) + ":"); err != nil {
			return err
		}
		switch {
		case field.value == nil:
			if err := write("["); err != nil {
				return err
			}
			for j, index := range keep {
				if j > 0 {
					if err := write(","); err != nil {
						return err
					}
				}
				if _, err := w.Write(kept[index]); err != nil {
					return err
				}
			}
			if err := write("]"); err != nil {
				return err
			}
		case field.name == "revision":
			// Readers treat the revision as the record's version; a rewrite is
			// a new version.
			var revision int64
			if json.Unmarshal(field.value, &revision) == nil {
				if err := write(fmt.Sprint(revision + 1)); err != nil {
					return err
				}
				continue
			}
			if _, err := w.Write(field.value); err != nil {
				return err
			}
		default:
			if _, err := w.Write(field.value); err != nil {
				return err
			}
		}
	}
	return write("}")
}

func copyChatDedupeBackup(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return nil // an earlier run already saved the original
	}
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		os.Remove(to)
		return err
	}
	return target.Close()
}
