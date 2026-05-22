package step_based_workflow

import (
	"context"
	"log"
	"sync"
	"time"
)

// Single-writer queue for KB update agents so concurrent step completions don't
// race on knowledgebase/notes/ files. The learning-agent queue was retired
// along with the post-step learning agent itself — direct-mode learning runs
// inline in the step agent's own turn and doesn't need its own worker.
//
// Full buffer applies backpressure to the step-completion handler rather than
// dropping jobs, which is the correct failure mode.
//
// A WaitGroup lets workflow-completion callers block until pending KB jobs
// drain (step-level flow stays non-blocking — only whole-workflow / scheduled
// runs wait).
const queueBufferSize = 100

type kbUpdateJob struct {
	run func()
}

var (
	kbUpdateQueue     chan kbUpdateJob
	kbUpdateQueueOnce sync.Once
	kbUpdateWG        sync.WaitGroup
)

// enqueueKBUpdateJob serializes KB update work to a single worker. Shared by the
// post-step trigger and the reorganize_knowledgebase tool so those two flows can't
// race on notes/ files.
func enqueueKBUpdateJob(job func()) {
	kbUpdateQueueOnce.Do(func() {
		kbUpdateQueue = make(chan kbUpdateJob, queueBufferSize)
		go drainKBUpdateQueue()
	})
	kbUpdateWG.Add(1)
	kbUpdateQueue <- kbUpdateJob{run: job}
}

func drainKBUpdateQueue() {
	for job := range kbUpdateQueue {
		func() {
			defer kbUpdateWG.Done()
			defer func() { _ = recover() }()
			job.run()
		}()
	}
}

// WaitForBackgroundJobs blocks until the KB queue drains or the context is
// canceled or the optional timeout elapses. Returns an error only when the
// wait is cut short.
//
// Intended for whole-workflow completion points (e.g. end of run_full_workflow)
// where the caller wants "workflow done" to mean "side-effects persisted".
// Per-step flow should NOT call this — it would serialize steps against their
// predecessors' KB writes.
//
// Pass 0 timeout for "wait indefinitely (respect context only)".
func WaitForBackgroundJobs(ctx context.Context, timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		kbUpdateWG.Wait()
		close(done)
	}()

	var timeoutCh <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timeoutCh = t.C
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		log.Printf("[BG_WAIT] context canceled while waiting for KB update queue: %v", ctx.Err())
		return ctx.Err()
	case <-timeoutCh:
		log.Printf("[BG_WAIT] timeout (%s) elapsed waiting for KB update queue to drain — returning with jobs still in flight", timeout)
		return context.DeadlineExceeded
	}
}
