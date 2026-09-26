package services

import "testing"

func TestSameSlackScopePath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"_users/u1/Chats/Work/projects/sde", "Chats/Work/projects/sde", true},
		{"Chats/Work/projects/sde", "_users/u1/Chats/Work/projects/sde/", true},
		{"Workflow/reports", "Workflow/reports", true},
		{"_users/u1/Chats/Work/projects/sde", "_users/u2/Chats/Work/projects/sde", false},
		{"Chats/Work/projects/sde", "Chats/Work/projects/other", false},
		{"_users/u1/Chats/Work/projects/sde", "", false},
	}
	for _, tc := range cases {
		if got := SameSlackScopePath(tc.a, tc.b); got != tc.want {
			t.Errorf("SameSlackScopePath(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestApplyBotThreadFields(t *testing.T) {
	req := map[string]interface{}{"query": "hi"}
	ApplyBotThreadFields(req, "", ThreadID{Platform: "slack", ChannelID: "C1", ThreadTS: "1.2", ConnectionID: "slack_own"})
	for key, want := range map[string]string{"bot_platform": "slack", "bot_channel_id": "C1", "bot_thread_ts": "1.2", "bot_connection_id": "slack_own"} {
		if req[key] != want {
			t.Fatalf("%s = %v, want %q", key, req[key], want)
		}
	}
	if _, set := req["triggered_by"]; set {
		t.Fatal("the trigger belongs to each builder, not the thread fields")
	}
	whatsapp := map[string]interface{}{}
	ApplyBotThreadFields(whatsapp, "whatsapp", ThreadID{ChannelID: "123@s.whatsapp.net"})
	if whatsapp["bot_platform"] != "whatsapp" || whatsapp["bot_connection_id"] != nil || whatsapp["bot_thread_ts"] != nil {
		t.Fatalf("WhatsApp thread fields = %v", whatsapp)
	}
}
