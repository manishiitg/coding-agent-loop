package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/livefeed"
)

func readSSEEvents(t *testing.T, sc *bufio.Scanner, n int) []string {
	t.Helper()
	var got []string
	var event string
	for len(got) < n && sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			got = append(got, event+" "+strings.TrimPrefix(line, "data: "))
		}
	}
	return got
}

func TestLiveFeedResyncsOnConnectThenStreamsCoalescedNotices(t *testing.T) {
	api := &StreamingAPI{}
	srv := httptest.NewServer(http.HandlerFunc(api.handleLiveFeed))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	if got := readSSEEvents(t, sc, 1); len(got) != 1 || got[0] != "resync {}" {
		t.Fatalf("first event = %v, want resync", got)
	}

	for livefeed.Default.Subscribers() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		publishSessionsChanged()
	}
	got := readSSEEvents(t, sc, 1)
	if len(got) != 1 || got[0] != `change {"kind":"sessions"}` {
		t.Fatalf("change event = %v", got)
	}
}

func TestLiveFeedWorkflowRootAndTerminalStatus(t *testing.T) {
	for in, want := range map[string]string{
		"Workflow/trader":                        "Workflow/trader",
		"/Workflow/trader/db/reports/index.html": "Workflow/trader",
		"_users/u/Chats/x":                       "",
		"Workflow":                               "",
	} {
		if got := normalizeLiveFeedWorkflow(in); got != want {
			t.Errorf("normalizeLiveFeedWorkflow(%q) = %q, want %q", in, got, want)
		}
	}
	if !liveFeedReportPath("Workflow/t/db/reports/index.html") || !liveFeedReportPath("Workflow/t/db/db.sqlite") || liveFeedReportPath("Workflow/t/planning/plan.json") {
		t.Error("liveFeedReportPath misclassified a path")
	}
	if !liveFeedTerminalStatus("completed") || !liveFeedTerminalStatus("error") || liveFeedTerminalStatus("running") {
		t.Error("liveFeedTerminalStatus misclassified a status")
	}
}

// Sending a chat message must change only the chat: an interactive session
// or a Builder background execution settling never refreshes the right pane;
// scheduled sessions and workflow runs do.
func TestLiveFeedOnlyRunsRefreshTheRightPane(t *testing.T) {
	if liveFeedScheduledSession("13ce7a15-e4fd-4b9f-8c61-3d27259f3444", "user") {
		t.Error("interactive chat session treated as a run")
	}
	if !liveFeedScheduledSession("schedule-cron--9db4dc39_1790265335110834922", "") || !liveFeedScheduledSession("abc", "cron") {
		t.Error("scheduled session not treated as a run")
	}
	run := &TrackedWorkflowExecution{Source: trackedExecutionSourceWorkflowRun, WorkspacePath: "Workflow/t"}
	builder := &TrackedWorkflowExecution{Source: trackedExecutionSourceWorkshopBackground, WorkspacePath: "Workflow/t"}
	if liveFeedSettledRunPath(run) != "Workflow/t" || liveFeedSettledRunPath(builder) != "" {
		t.Error("only workflow runs should settle the right pane")
	}
}
