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
	LastAttemptAt time.Time `json:"last_attempt_at,omitempty"`
	NextAttemptAt time.Time `json:"next_attempt_at,omitempty"`
	AttemptCount  int       `json:"attempt_count,omitempty"`
	State         string    `json:"state"`
}

const (
	nativeTranscriptRecoveryMaxAge      = 24 * time.Hour
	nativeTranscriptRecoveryMaxAttempts = 7
	nativeTranscriptRecoveryWorkers     = 1
)

var nativeTranscriptRecoveryBackoff = [...]time.Duration{
	time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
}

func persistNativeTranscriptRecoveryDemand(d nativeTranscriptRecoveryDemand) error {
	if strings.TrimSpace(d.UserID) == "" || strings.TrimSpace(d.SessionID) == "" || strings.TrimSpace(d.WorkspacePath) == "" {
		return fmt.Errorf("missing native recovery identity")
	}
	if d.RequestedAt.IsZero() {
		d.RequestedAt = time.Now().UTC()
	}
	if strings.TrimSpace(d.State) == "" {
		d.State = "unresolved"
	}
	path, err := nativeTranscriptRecoveryDemandPath(d)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
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

func nativeTranscriptRecoveryDemandPath(d nativeTranscriptRecoveryDemand) (string, error) {
	root, err := workflowCLIStateRoot()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(d.UserID + "\x00" + d.SessionID + "\x00" + d.WorkspacePath))
	return filepath.Join(root, "chat-native-recovery", hex.EncodeToString(sum[:])+".json"), nil
}

// A completion can create a fresh demand while an older periodic attempt is
// still reconciling. Never let that older attempt replace the newer request's
// retry clock when it records its result.
func persistNativeTranscriptRecoveryAttempt(original, advanced nativeTranscriptRecoveryDemand) error {
	path, err := nativeTranscriptRecoveryDemandPath(original)
	if err != nil {
		return err
	}
	if raw, readErr := os.ReadFile(path); readErr == nil {
		var current nativeTranscriptRecoveryDemand
		if json.Unmarshal(raw, &current) == nil && current.RequestedAt.After(original.RequestedAt) {
			return nil
		}
	}
	return persistNativeTranscriptRecoveryDemand(advanced)
}

func nativeTranscriptRecoveryDue(d nativeTranscriptRecoveryDemand, now time.Time) bool {
	if d.State != "unresolved" || d.AttemptCount >= nativeTranscriptRecoveryMaxAttempts {
		return false
	}
	if d.RequestedAt.IsZero() || now.Sub(d.RequestedAt) >= nativeTranscriptRecoveryMaxAge {
		return false
	}
	return d.NextAttemptAt.IsZero() || !now.Before(d.NextAttemptAt)
}

func advanceNativeTranscriptRecoveryDemand(d nativeTranscriptRecoveryDemand, now time.Time, supported bool) nativeTranscriptRecoveryDemand {
	now = now.UTC()
	d.LastAttemptAt = now
	d.AttemptCount++
	if !supported {
		d.State = "unsupported"
		d.NextAttemptAt = time.Time{}
		return d
	}
	if d.AttemptCount >= nativeTranscriptRecoveryMaxAttempts || now.Sub(d.RequestedAt) >= nativeTranscriptRecoveryMaxAge {
		d.State = "exhausted"
		d.NextAttemptAt = time.Time{}
		return d
	}
	delayIndex := d.AttemptCount - 1
	if delayIndex >= len(nativeTranscriptRecoveryBackoff) {
		delayIndex = len(nativeTranscriptRecoveryBackoff) - 1
	}
	d.NextAttemptAt = now.Add(nativeTranscriptRecoveryBackoff[delayIndex])
	return d
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
	now := time.Now().UTC()
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
		if !nativeTranscriptRecoveryDue(demand, now) {
			if demand.AttemptCount >= nativeTranscriptRecoveryMaxAttempts || (!demand.RequestedAt.IsZero() && now.Sub(demand.RequestedAt) >= nativeTranscriptRecoveryMaxAge) {
				demand.State = "exhausted"
				demand.NextAttemptAt = time.Time{}
				_ = persistNativeTranscriptRecoveryDemand(demand)
			}
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
			_, supported := api.syncWorkflowBuilderConversationFromNativeTranscript(attemptCtx, d.UserID, d.SessionID, d.WorkspacePath)
			if err := persistNativeTranscriptRecoveryAttempt(d, advanceNativeTranscriptRecoveryDemand(d, time.Now(), supported)); err != nil {
				log.Printf("[CHAT_HISTORY] Native recovery retry state could not be persisted session=%s: %v", d.SessionID, err)
			}
		})
	})
}

func runNativeRecoveryBatch(ctx context.Context, demands []nativeTranscriptRecoveryDemand, reconcile func(context.Context, nativeTranscriptRecoveryDemand)) {
	jobs := make(chan nativeTranscriptRecoveryDemand)
	var workers sync.WaitGroup
	for i := 0; i < nativeTranscriptRecoveryWorkers; i++ {
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
