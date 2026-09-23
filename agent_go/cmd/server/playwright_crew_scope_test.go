package server

import "testing"

// Crew Playwright tests are filed under the owner-qualified crew path so the
// owner's logical path and the physical path name the same crew, and another
// user's same-named crew never sees them.
func TestPlaywrightCrewScopeAndVisibility(t *testing.T) {
	api := &StreamingAPI{}
	alice := &UserClaims{UserID: "alice"}
	bob := &UserClaims{UserID: "bob"}
	scope := playwrightScope("alice", "Chats/Work/projects/qa/")
	if scope != "_users/alice/Chats/Work/projects/qa" {
		t.Fatalf("crew scope = %q", scope)
	}
	if got := playwrightScope("alice", "_users/alice/Chats/Work/projects/qa"); got != scope {
		t.Fatalf("physical crew path scope = %q, want %q", got, scope)
	}
	if !api.playwrightVisible(alice, "Chats/Work/projects/qa", "alice", scope) {
		t.Fatal("crew owner must see the crew's Playwright test via the logical path")
	}
	if !api.playwrightVisible(alice, "_users/alice/Chats/Work/projects/qa", "alice", scope) {
		t.Fatal("crew owner must see the crew's Playwright test via the physical path")
	}
	if api.playwrightVisible(bob, "Chats/Work/projects/qa", "alice", scope) {
		t.Fatal("another user's same-named crew must not see it")
	}
	if api.playwrightVisible(alice, "Chats/Work/projects/other", "alice", scope) {
		t.Fatal("a different crew must not see it")
	}
	// Workflows are unchanged: private to the user who ran the test.
	if playwrightScope("alice", "Workflow/test/") != "Workflow/test" ||
		!api.playwrightVisible(alice, "Workflow/test", "alice", "Workflow/test") ||
		api.playwrightVisible(bob, "Workflow/test", "alice", "Workflow/test") {
		t.Fatal("workflow Playwright visibility changed")
	}
}
