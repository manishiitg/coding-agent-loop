package productschedule

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Source says what started a run.
type Source string

const (
	SourceScheduled Source = "scheduled"
	SourceManual    Source = "manual"
)

// ErrSkip may be returned from Host.Begin to skip a run quietly (for
// example, when another session holds the shared workspace).
var ErrSkip = errors.New("productschedule: run skipped")

// ErrAlreadyRunning is returned by RunNow while a run is in progress.
var ErrAlreadyRunning = errors.New("productschedule: a run is already in progress")

// Turn is one message of a run, resolved by the host at run time (a product
// may fill placeholders or drop messages that do not apply today).
type Turn struct {
	// Label is the short user-facing line shown for this turn.
	Label string
	// Message is the full text sent to the agent.
	Message string
}

// Run is one execution of a schedule, created by the host per run so it can
// hold whatever spans the turns (a conversation, a lock, ...).
type Run interface {
	// Turns returns the ordered turns of this run.
	Turns() []Turn
	// Send executes turn i and returns its reply. The host persists the
	// reply itself. An error stops the run.
	Send(ctx context.Context, i int, turn Turn) (string, error)
	// Finish is always called exactly once, with nil when every turn ran.
	Finish(ctx context.Context, err error)
}

// Host is what a standalone product gives the Runner.
type Host struct {
	// Name is used in log lines, e.g. "pulse".
	Name string
	// Schedule is read on every tick so config changes apply live.
	Schedule func() Schedule
	// LastRun is when the schedule last ran successfully (zero = never).
	LastRun func() time.Time
	// SinceInteractive is how long ago the user last used the product.
	// Nil means "always quiet".
	SinceInteractive func() time.Duration
	// Begin creates the run. Return ErrSkip to skip quietly.
	Begin func(ctx context.Context, source Source) (Run, error)
	// Timeout bounds one whole run. Zero means no timeout.
	Timeout time.Duration
	// Tick is how often the runner re-evaluates. Zero means five minutes.
	Tick time.Duration
}

// StepStatus is the state of one turn within a run.
type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

// Step is the per-turn status exposed through Status.
type Step struct {
	Label      string     `json:"label"`
	Status     StepStatus `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Reply      string     `json:"reply,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// RunStatus describes one run, in progress or finished.
type RunStatus struct {
	Source     Source     `json:"source"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Steps      []Step     `json:"steps"`
	Error      string     `json:"error,omitempty"`
	Skipped    bool       `json:"skipped,omitempty"`
}

// Status is a snapshot of the runner.
type Status struct {
	Running bool       `json:"running"`
	Current *RunStatus `json:"current,omitempty"`
	Last    *RunStatus `json:"last,omitempty"`
	// LastDecision is the most recent scheduled-tick verdict.
	LastDecision string `json:"last_decision,omitempty"`
}

// Runner executes one schedule for a standalone product.
type Runner struct {
	host        Host
	activatedAt time.Time

	mu           sync.Mutex
	running      bool
	current      *RunStatus
	last         *RunStatus
	lastDecision string
}

// NewRunner validates the host and returns a stopped runner.
func NewRunner(host Host) (*Runner, error) {
	if host.Begin == nil {
		return nil, fmt.Errorf("productschedule: Begin is required")
	}
	if host.Schedule == nil {
		return nil, fmt.Errorf("productschedule: Schedule is required")
	}
	if host.Name == "" {
		host.Name = "schedule"
	}
	if host.LastRun == nil {
		host.LastRun = func() time.Time { return time.Time{} }
	}
	if host.Tick <= 0 {
		host.Tick = 5 * time.Minute
	}
	return &Runner{host: host, activatedAt: time.Now().UTC()}, nil
}

func (r *Runner) logf(format string, args ...interface{}) {
	log.Printf("["+r.host.Name+"] "+format, args...)
}

// Start runs the scheduled loop until ctx is done.
func (r *Runner) Start(ctx context.Context) {
	ticker := time.NewTicker(r.host.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.tick(ctx, now)
		}
	}
}

func (r *Runner) tick(ctx context.Context, now time.Time) {
	schedule := r.host.Schedule()
	activatedAt := r.activatedAt
	if createdAt, err := time.Parse(time.RFC3339, schedule.CreatedAt); err == nil {
		activatedAt = createdAt
	}
	in := Inputs{Now: now, ActivatedAt: activatedAt, LastRun: r.host.LastRun(), SinceInteractive: 365 * 24 * time.Hour}
	if r.host.SinceInteractive != nil {
		in.SinceInteractive = r.host.SinceInteractive()
	}
	d := Decide(schedule, in)
	r.mu.Lock()
	r.lastDecision = d.Reason
	r.mu.Unlock()
	if !d.Run {
		if d.Deferred {
			r.logf("deferring: %s", d.Reason)
		}
		return
	}
	if d.Reason != "due" {
		r.logf("%s", d.Reason)
	}
	if err := r.RunNow(ctx, SourceScheduled); err != nil && !errors.Is(err, ErrSkip) && !errors.Is(err, ErrAlreadyRunning) {
		r.logf("scheduled run failed: %v", err)
	}
}

// RunNow runs the schedule once, synchronously. Manual runs ignore Enabled.
func (r *Runner) RunNow(ctx context.Context, source Source) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrAlreadyRunning
	}
	r.running = true
	status := &RunStatus{Source: source, StartedAt: time.Now()}
	r.current = status
	r.mu.Unlock()

	finish := func(err error) {
		now := time.Now()
		r.mu.Lock()
		status.FinishedAt = &now
		if err != nil {
			if errors.Is(err, ErrSkip) {
				status.Skipped = true
			}
			status.Error = err.Error()
		}
		r.last = status
		r.current = nil
		r.running = false
		r.mu.Unlock()
	}

	if r.host.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.host.Timeout)
		defer cancel()
	}
	run, err := r.host.Begin(ctx, source)
	if err != nil {
		finish(err)
		if errors.Is(err, ErrSkip) {
			r.logf("skipped: %v", err)
		}
		return err
	}
	turns := run.Turns()
	r.mu.Lock()
	for _, t := range turns {
		status.Steps = append(status.Steps, Step{Label: t.Label, Status: StepPending})
	}
	r.mu.Unlock()

	var runErr error
	for i, t := range turns {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		r.setStep(status, i, func(st *Step) {
			now := time.Now()
			st.Status, st.StartedAt = StepRunning, &now
		})
		reply, err := run.Send(ctx, i, t)
		r.setStep(status, i, func(st *Step) {
			now := time.Now()
			st.FinishedAt = &now
			if err != nil {
				st.Status, st.Error = StepFailed, err.Error()
				return
			}
			st.Status, st.Reply = StepDone, reply
		})
		if err != nil {
			runErr = fmt.Errorf("%q failed: %w", t.Label, err)
			break
		}
	}
	if runErr != nil {
		r.mu.Lock()
		for i := range status.Steps {
			if status.Steps[i].Status == StepPending {
				status.Steps[i].Status = StepSkipped
			}
		}
		r.mu.Unlock()
	}
	run.Finish(ctx, runErr)
	finish(runErr)
	if runErr == nil {
		r.logf("run complete (%s): %d turn(s)", source, len(turns))
	}
	return runErr
}

func (r *Runner) setStep(status *RunStatus, i int, fn func(*Step)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= 0 && i < len(status.Steps) {
		fn(&status.Steps[i])
	}
}

// Status returns a copy of the runner state.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{Running: r.running, Current: cloneRun(r.current), Last: cloneRun(r.last), LastDecision: r.lastDecision}
}

func cloneRun(c *RunStatus) *RunStatus {
	if c == nil {
		return nil
	}
	out := *c
	out.Steps = append([]Step(nil), c.Steps...)
	return &out
}
