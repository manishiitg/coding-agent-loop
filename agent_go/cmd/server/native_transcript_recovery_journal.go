package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Native adapters do not expose application turn IDs. Retain the demand as
// unresolved even after a bounded reconciliation window; a restart retries it
// without guessing that a matching text constitutes a delivery acknowledgement.
type nativeTranscriptRecoveryDemand struct {
	UserID        string    `json:"user_id"`
	SessionID     string    `json:"session_id"`
	WorkspacePath string    `json:"workspace_path"`
	RequestedAt   time.Time `json:"requested_at"`
	State         string    `json:"state"`
}

func persistNativeTranscriptRecoveryDemand(d nativeTranscriptRecoveryDemand) error {
	if strings.TrimSpace(d.UserID) == "" || strings.TrimSpace(d.SessionID) == "" || strings.TrimSpace(d.WorkspacePath) == "" {
		return fmt.Errorf("missing native recovery identity")
	}
	root, err := workflowCLIStateRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "chat-native-recovery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(d.UserID + "\x00" + d.SessionID + "\x00" + d.WorkspacePath))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	if directory, err := os.Open(dir); err == nil {
		defer directory.Close()
		return directory.Sync()
	}
	return nil
}

// One scanner retries late native flushes throughout the server lifetime. A
// fixed worker pool bounds concurrency even when many historical demands exist.
func (api *StreamingAPI) resumePendingNativeTranscriptRecovery() context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go runPeriodicNativeRecovery(ctx, 30*time.Second, api.replayPendingNativeTranscriptRecovery)
	return cancel
}

func runPeriodicNativeRecovery(ctx context.Context, interval time.Duration, replay func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		replay(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (api *StreamingAPI) replayPendingNativeTranscriptRecovery(ctx context.Context) {
	root, err := workflowCLIStateRoot()
	if err != nil {
		log.Printf("[CHAT_HISTORY] Native recovery scan: %v", err)
		return
	}
	paths, err := filepath.Glob(filepath.Join(root, "chat-native-recovery", "*.json"))
	if err != nil {
		return
	}
	demands := make([]nativeTranscriptRecoveryDemand, 0, len(paths))
	for _, path := range paths {
		if ctx.Err() != nil {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var demand nativeTranscriptRecoveryDemand
		if json.Unmarshal(raw, &demand) != nil || demand.UserID == "" || demand.SessionID == "" || demand.WorkspacePath == "" || demand.State != "unresolved" {
			continue
		}
		demands = append(demands, demand)
	}
	runNativeRecoveryBatch(ctx, demands, func(ctx context.Context, d nativeTranscriptRecoveryDemand) {
		key := nativeTranscriptRecoveryKey(d.UserID, d.SessionID, d.WorkspacePath)
		api.nativeTranscriptSyncMu.Lock()
		if api.nativeTranscriptSyncInFlight[key] {
			api.nativeTranscriptSyncMu.Unlock()
			return
		}
		if api.nativeTranscriptSyncInFlight == nil {
			api.nativeTranscriptSyncInFlight = map[string]bool{}
		}
		if api.nativeTranscriptSyncPending == nil {
			api.nativeTranscriptSyncPending = map[string]bool{}
		}
		api.nativeTranscriptSyncInFlight[key] = true
		api.nativeTranscriptSyncMu.Unlock()
		api.runNativeTranscriptSyncWorker(key, func() {
			if ctx.Err() != nil {
				return
			}
			attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			api.syncWorkflowBuilderConversationFromNativeTranscript(attemptCtx, d.UserID, d.SessionID, d.WorkspacePath)
		})
	})
}

func runNativeRecoveryBatch(ctx context.Context, demands []nativeTranscriptRecoveryDemand, reconcile func(context.Context, nativeTranscriptRecoveryDemand)) {
	jobs := make(chan nativeTranscriptRecoveryDemand)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for d := range jobs {
				if ctx.Err() != nil {
					return
				}
				reconcile(ctx, d)
			}
		}()
	}
dispatch:
	for _, d := range demands {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- d:
		}
	}
	close(jobs)
	workers.Wait()
}

func nativeTranscriptRecoveryKey(owner, session, workspace string) string {
	return owner + "\x00" + session + "\x00" + normalizeConversationWorkspace(workspace)
}
