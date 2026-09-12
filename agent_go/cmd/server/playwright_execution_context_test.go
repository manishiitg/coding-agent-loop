package server

import "testing"

func TestPlaywrightExecutionContext(t *testing.T) {
	for _, tc := range []struct {
		name, session string
		req           QueryRequest
		active        *ActiveSessionInfo
		want          string
	}{
		{name: "builder", want: "builder"},
		{name: "cron", req: QueryRequest{TriggeredBy: "cron"}, want: "schedule"},
		{name: "webhook", req: QueryRequest{TriggeredBy: "webhook"}, want: "webhook"},
		{name: "bot", req: QueryRequest{BotPlatform: "slack"}, want: "bot"},
		{name: "resumed webhook", active: &ActiveSessionInfo{TriggeredBy: "webhook"}, want: "webhook"},
		{name: "resumed bot", active: &ActiveSessionInfo{BotPlatform: "slack"}, want: "bot"},
		{name: "scheduled child", session: "schedule-run", req: QueryRequest{ParentSessionID: "parent"}, want: "schedule"},
		{name: "pulse", req: QueryRequest{PulseLifecycleTurn: true}, want: "pulse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := playwrightExecutionContext(tc.session, tc.req, tc.active); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestPlaywrightChildInheritsUnattendedContext(t *testing.T) {
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		"bot": {BotPlatform: "slack"}, "child": {ParentSessionID: "bot"},
	}}
	env := map[string]string{"AGENTWORKS_EXECUTION_CONTEXT": "builder"}
	api.setPlaywrightExecutionContext(env, "child", QueryRequest{})
	if env["AGENTWORKS_EXECUTION_CONTEXT"] != "bot" {
		t.Fatal(env)
	}
	api.setPlaywrightExecutionContext(env, "fresh-builder", QueryRequest{})
	if env["AGENTWORKS_EXECUTION_CONTEXT"] != "builder" {
		t.Fatal("stale context", env)
	}
}
