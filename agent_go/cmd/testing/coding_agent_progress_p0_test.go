package testing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	stdtesting "testing"
	"time"
)

func progressFixture(kind, source, text string) map[string]interface{} {
	return map[string]interface{}{"type": kind, "data": map[string]interface{}{"data": map[string]interface{}{"source": source, "content": text}}}
}
func TestProgressP0RequiresEveryNarrationBeforeCompletion(t *stdtesting.T) {
	a := progressFixture("streaming_chunk", "transcript", "FIRST")
	b := progressFixture("streaming_chunk", "transcript", "SECOND")
	done := progressFixture("unified_completion", "", "FIRST SECOND")
	delta := progressFixture("streaming_chunk", "transcript", "FIRST")
	delta["data"].(map[string]interface{})["data"].(map[string]interface{})["is_delta"] = true
	for _, tc := range []struct {
		name   string
		events []map[string]interface{}
		valid  bool
	}{
		{"complete", []map[string]interface{}{a, b, done}, true},
		{"missing first", []map[string]interface{}{b, done}, false},
		{"missing second", []map[string]interface{}{a, done}, false},
		{"late", []map[string]interface{}{a, done, b}, false},
		{"duplicate", []map[string]interface{}{a, a, b, done}, false},
		{"duplicate in chunk", []map[string]interface{}{progressFixture("streaming_chunk", "transcript", "FIRST FIRST"), b, done}, false},
		{"merged narration", []map[string]interface{}{progressFixture("streaming_chunk", "transcript", "FIRST SECOND"), done}, false},
		{"final only", []map[string]interface{}{done}, false},
		{"terminal echo", []map[string]interface{}{progressFixture("streaming_chunk", "terminal", "FIRST"), b, done}, false},
		{"tool result", []map[string]interface{}{progressFixture("tool_call_end", "transcript", "FIRST"), b, done}, false},
		{"delta", []map[string]interface{}{delta, b, done}, false},
		{"unfinished", []map[string]interface{}{a, b}, false},
	} {
		t.Run(tc.name, func(t *stdtesting.T) {
			if err := assertProgressP0Narration(tc.events, []string{"FIRST", "SECOND"}); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
func TestProgressP0ReadsAuthenticatedChatSSE(t *stdtesting.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions/session-test/events/stream" || r.URL.Query().Get("working_set") != "session" || r.URL.Query().Get("since") != "42" {
			t.Errorf("wrong endpoint %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			t.Error("missing auth")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: status\ndata: {\"session_status\":\"running\"}\n\n")
		fmt.Fprint(w, "event: event\ndata: {\"events\":[{\"type\":\"streaming_chunk\",\"data\":{\"data\":{\"source\":\"transcript\",\"content\":\"FIRST\"}}},{\"type\":\"unified_completion\"}]}\n\n")
	}))
	defer server.Close()
	c := &codingAgentChatE2EClient{baseURL: server.URL, token: "test-jwt", http: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := c.openProgressP0Stream(ctx, "session-test", 42)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.close()
	if err := assertRetainedProgressStream(ctx, stream, "FIRST"); err != nil {
		t.Fatal(err)
	}
}
