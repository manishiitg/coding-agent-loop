package step_based_workflow

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPulseReviewResultPathRequiresDatedRunAndSeparateModuleFile(t *testing.T) {
	path, err := pulseReviewResultPath("2026-07-21T00-08-44.123Z_pulse-run-1", "bug_review")
	if err != nil {
		t.Fatalf("pulseReviewResultPath: %v", err)
	}
	if want := "pulse/reviews/2026-07-21T00-08-44.123Z_pulse-run-1/bug_review.md"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	for _, tc := range []struct {
		runID  string
		module string
	}{
		{"pulse-run-1", "bug_review"},
		{"2026-07-21T00-08-44.123Z_../escape", "bug_review"},
		{"2026-07-21T00-08-44.123Z_pulse-run-1", "unknown"},
	} {
		if _, err := pulseReviewResultPath(tc.runID, tc.module); err == nil {
			t.Fatalf("pulseReviewResultPath(%q, %q) unexpectedly succeeded", tc.runID, tc.module)
		}
	}
}

func TestPulseReviewResultMarkdownCarriesIdentityAndFindings(t *testing.T) {
	completedAt := time.Date(2026, 7, 21, 0, 8, 44, 123000000, time.UTC)
	body := pulseReviewResultMarkdown("pulse-run-1", "2026-07-21T00-08-44.123Z_pulse-run-1", "eval_health", "completed", "Verdict: clean", completedAt)
	for _, want := range []string{
		"Pulse run: `pulse-run-1`",
		"Review run: `2026-07-21T00-08-44.123Z_pulse-run-1`",
		"Module: `eval_health`",
		"Status: `completed`",
		"Verdict: clean",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("result markdown missing %q:\n%s", want, body)
		}
	}
}

func TestPulseReviewerSlotsEnforceMaximumTwo(t *testing.T) {
	slots := make(chan struct{}, pulseReviewerMaxConcurrency)
	release := make(chan struct{})
	var active atomic.Int32
	var peak atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := acquirePulseReviewerSlot(context.Background(), slots); err != nil {
				t.Errorf("acquirePulseReviewerSlot: %v", err)
				return
			}
			current := active.Add(1)
			for {
				seen := peak.Load()
				if current <= seen || peak.CompareAndSwap(seen, current) {
					break
				}
			}
			<-release
			active.Add(-1)
			<-slots
		}()
	}

	deadline := time.Now().Add(time.Second)
	for peak.Load() < pulseReviewerMaxConcurrency && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := peak.Load(); got != pulseReviewerMaxConcurrency {
		t.Fatalf("peak concurrency = %d, want %d", got, pulseReviewerMaxConcurrency)
	}
	close(release)
	wg.Wait()
	if got := peak.Load(); got != pulseReviewerMaxConcurrency {
		t.Fatalf("final peak concurrency = %d, want %d", got, pulseReviewerMaxConcurrency)
	}
}
