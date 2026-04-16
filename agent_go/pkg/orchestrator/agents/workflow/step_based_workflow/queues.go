package step_based_workflow

import (
	"sync"
)

// Single-writer queues serialize learning agents and KB update agents so concurrent
// step completions don't race on their respective shared files (learnings/_global/ and
// knowledgebase/graph.json). Two independent queues — learning and KB write different
// files, so one-of-each can run in parallel; what's serialized is same-class agents.
//
// Full buffer applies backpressure to the step-completion handler rather than dropping
// jobs, which is the correct failure mode.
const queueBufferSize = 100

type learningJob struct {
	run func()
}

var (
	learningQueue     chan learningJob
	learningQueueOnce sync.Once
)

// enqueueLearningJob sends work to the learning worker (lazy-started on first enqueue).
// Blocks when the buffer fills, to propagate backpressure rather than drop jobs.
func enqueueLearningJob(job func()) {
	learningQueueOnce.Do(func() {
		learningQueue = make(chan learningJob, queueBufferSize)
		go drainLearningQueue()
	})
	learningQueue <- learningJob{run: job}
}

func drainLearningQueue() {
	for job := range learningQueue {
		// Panic guard so one bad job can't kill the worker.
		func() {
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
)

// enqueueKBUpdateJob serializes KB update work to a single worker. Shared by the
// post-step trigger and the reorganize_knowledgebase tool so those two flows can't
// race on graph.json.
func enqueueKBUpdateJob(job func()) {
	kbUpdateQueueOnce.Do(func() {
		kbUpdateQueue = make(chan kbUpdateJob, queueBufferSize)
		go drainKBUpdateQueue()
	})
	kbUpdateQueue <- kbUpdateJob{run: job}
}

func drainKBUpdateQueue() {
	for job := range kbUpdateQueue {
		func() {
			defer func() { _ = recover() }()
			job.run()
		}()
	}
}
