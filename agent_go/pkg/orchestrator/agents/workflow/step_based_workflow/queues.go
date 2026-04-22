package step_based_workflow

import (
	"context"
	"log"
	"sync"
	"time"
)

// Single-writer queues serialize learning agents and KB update agents so concurrent
// step completions don't race on their respective shared files (learnings/_global/ and
// knowledgebase/notes/). Two independent queues — learning and KB write different
// files, so one-of-each can run in parallel; what's serialized is same-class agents.
//
// Full buffer applies backpressure to the step-completion handler rather than dropping
// jobs, which is the correct failure mode.
//
// Per-queue WaitGroups let workflow-completion callers block until pending jobs drain
// (step-level flow stays non-blocking — only whole-workflow / scheduled runs wait).
const queueBufferSize = 100

type learningJob struct {
	run func()
}

var (
	learningQueue     chan learningJob
	learningQueueOnce sync.Once
	learningWG        sync.WaitGroup
)

// enqueueLearningJob sends work to the learning worker (lazy-started on first enqueue).
// Blocks when the buffer fills, to propagate backpressure rather than drop jobs.
func enqueueLearningJob(job func()) {
	learningQueueOnce.Do(func() {
		learningQueue = make(chan learningJob, queueBufferSize)
		go drainLearningQueue()
	})
	learningWG.Add(1)
	learningQueue <- learningJob{run: job}
}

func drainLearningQueue() {
	for job := range learningQueue {
		// Panic guard so one bad job can't kill the worker.
		func() {
			defer learningWG.Done()
			defer func() { _ = recover() }()
			job.run()
		}()
	}
}

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

// WaitForBackgroundJobs blocks until both queues drain or the context is canceled
// or the optional timeout elapses. Returns an error only when the wait is cut short.
//
// Intended for whole-workflow completion points (e.g. end of run_full_workflow) where
// the caller wants "workflow done" to mean "side-effects persisted". Per-step flow
// should NOT call this — it would serialize steps against their predecessors' learning.
//
// Pass 0 timeout for "wait indefinitely (respect context only)".
func WaitForBackgroundJobs(ctx context.Context, timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		learningWG.Wait()
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
		log.Printf("[BG_WAIT] context canceled while waiting for background learning/KB queues: %v", ctx.Err())
		return ctx.Err()
	case <-timeoutCh:
		log.Printf("[BG_WAIT] timeout (%s) elapsed waiting for background learning/KB queues to drain — returning with jobs still in flight", timeout)
		return context.DeadlineExceeded
	}
}
