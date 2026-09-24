package server

import "testing"

// Definition/config drift must keep the native coding-agent conversation;
// only a role change (mode, origin, capabilities) may replace it.
func TestChatPolicyRoleRequiresReconnect(t *testing.T) {
	builder := workflowChatPolicy{Mode: "builder", Origin: "interactive", Capabilities: map[string]bool{"authoring": true}}
	run := workflowChatPolicy{Mode: "run", Origin: "interactive", Capabilities: map[string]bool{}}
	resumable := func(role string) *ChatHistoryAgentRuntime {
		return &ChatHistoryAgentRuntime{ExternalSessionID: "native-1", ChatPolicyKey: "full-key-before-deploy", ChatPolicyRoleKey: role}
	}

	if chatPolicyRoleRequiresReconnect(true, builder.sessionKey(), builder.sessionKey(), true, nil) {
		t.Fatal("same role in this process must not reconnect")
	}
	if !chatPolicyRoleRequiresReconnect(true, builder.sessionKey(), run.sessionKey(), true, nil) {
		t.Fatal("builder -> run in this process must reconnect")
	}
	// After a restart/deploy the full key differs (new chat definition), but
	// the saved role key still matches: resume the same session.
	if chatPolicyRoleRequiresReconnect(true, "", builder.sessionKey(), false, resumable(builder.sessionKey())) {
		t.Fatal("definition drift with an unchanged role must resume")
	}
	if !chatPolicyRoleRequiresReconnect(true, "", run.sessionKey(), false, resumable(builder.sessionKey())) {
		t.Fatal("a saved builder session restored as run must reconnect")
	}
	if chatPolicyRoleRequiresReconnect(true, "", builder.sessionKey(), false, resumable("")) {
		t.Fatal("a legacy runtime without a role key must resume (mode is compared separately)")
	}
	if chatPolicyRoleRequiresReconnect(false, builder.sessionKey(), run.sessionKey(), true, nil) {
		t.Fatal("non-coding providers never reconnect")
	}
}

func TestCodingProviderReloadsInstructionsOnResume(t *testing.T) {
	for _, provider := range []string{"claude-code", "codex-cli", "cursor-cli", "pi-cli"} {
		if !codingProviderReloadsInstructionsOnResume(provider) {
			t.Fatalf("%s should keep its session across prompt changes", provider)
		}
	}
	if codingProviderReloadsInstructionsOnResume("muse-cli") || codingProviderReloadsInstructionsOnResume(" Muse-CLI ") {
		t.Fatal("muse-cli cannot take new instructions into a resumed session")
	}
}
