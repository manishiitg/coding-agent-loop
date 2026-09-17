package services

import (
	"context"
	"encoding/json"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtrigger"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackHistoryBoundedChannelAndThreads(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		if r.FormValue("channel") != "CSOURCE" || r.FormValue("oldest") != "1000.000001" || r.FormValue("latest") != "1060.000001" {
			t.Errorf("wrong bounds %v", r.Form)
		}
		switch r.URL.Path {
		case "/conversations.history":
			w.Write([]byte(`{"ok":true,"has_more":true,"messages":[{"ts":"1060.000002","text":"future"},{"ts":"1059.000001","text":"incident","reply_count":2},{"ts":"1058.000001","text":"other"}]}`))
		case "/conversations.replies":
			w.Write([]byte(`{"ok":true,"messages":[{"ts":"1059.000001","text":"incident"},{"ts":"1059.000002","text":"reply"},{"ts":"1060.000003","text":"future reply"}]}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	s := &SlackService{client: slack.New("test", slack.OptionAPIURL(srv.URL+"/"))}
	since, _ := SlackMessageTime("1000.000001")
	before, _ := SlackMessageTime("1060.000001")
	result, err := s.ReadChannelHistory(context.Background(), "CSOURCE", ChannelHistoryRequest{Limit: 3, Since: since, Before: before, IncludeThreads: true})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(result.Messages) != 2 || len(result.Threads["1059.000001"]) != 1 || !result.Truncated {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if _, err = s.ReadChannelHistory(context.Background(), "CSOURCE", ChannelHistoryRequest{Limit: 101, Since: since, Before: before}); err == nil || calls != 2 {
		t.Fatal("invalid bounds reached API")
	}
}
func TestSlackHistoryByteBudget(t *testing.T) {
	r := &ChannelHistoryResult{ChannelID: "C", Threads: map[string][]ChannelHistoryMessage{}}
	for i := 0; i < 100; i++ {
		if !appendHistoryMessage(r, ChannelHistoryMessage{MessageID: "1", Text: strings.Repeat("x", 8000)}) {
			break
		}
	}
	raw, _ := json.Marshal(r)
	if len(raw) > 64*1024 || !r.Truncated {
		t.Fatalf("bytes=%d truncated=%v", len(raw), r.Truncated)
	}
}
func TestSlackHistoryFailureHasNoRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
	}))
	defer srv.Close()
	s := &SlackService{client: slack.New("test", slack.OptionAPIURL(srv.URL+"/"))}
	_, err := s.ReadChannelHistory(context.Background(), "C", ChannelHistoryRequest{Limit: 1, Since: time.Unix(1, 0), Before: time.Unix(2, 0)})
	if err == nil || !strings.Contains(err.Error(), "missing_scope") || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
func TestSlackTimestampAndRichTrustedMatch(t *testing.T) {
	when, err := SlackMessageTime("1000.123456")
	if err != nil || when.Nanosecond() != 123456000 || channelHistoryTimestamp(when) != "1000.123456" {
		t.Fatal("timestamp precision lost")
	}
	for _, v := range []string{"NaN", "+Inf", "0.1", "1000.bad", "1000.1234567890"} {
		if _, err := SlackMessageTime(v); err == nil {
			t.Errorf("accepted %s", v)
		}
	}
	var e slackevents.MessageEvent
	if err = json.Unmarshal([]byte(`{"type":"message","subtype":"bot_message","bot_id":"BSENTRY","channel":"C","ts":"1000.123456","text":"alert","attachments":[{"title":"Production Incident"}]}`), &e); err != nil {
		t.Fatal(err)
	}
	trigger := &SlackTrigger{Type: "trusted_app", BotID: "BSENTRY", Match: &workflowtrigger.Match{All: []workflowtrigger.Condition{{Source: "message.attachments.0.title", Operator: "contains", Value: "incident", CaseInsensitive: true}}}}
	if !SlackTriggerMatches(trigger, &e, "UOWN") {
		raw, _ := json.Marshal(e)
		t.Fatalf("rejected: %s", raw)
	}
	e.BotID = "BOTHER"
	if SlackTriggerMatches(trigger, &e, "UOWN") {
		t.Fatal("untrusted source accepted")
	}
}
