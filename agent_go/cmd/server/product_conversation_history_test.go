package server

import (
	"context"
	"strings"
	"testing"
)

// "New chat" keeps the conversation it replaces; a product can make an earlier
// one live again (the current one is then kept in turn) or forget it — but
// never forget the live one.
func TestProductConversationHistoryRotateSwitchForget(t *testing.T) {
	store, _ := memoryProductConversationStore()
	// This scenario mints more identities than the shared fake's fixed list.
	nextID := 0
	store.newID = func() string { nextID++; return "id-" + strings.Repeat("x", nextID) }
	profile := singletonConversationProfile()
	binding, err := resolveProductConversationBinding(context.Background(), "user-1", profile, "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.resolveOrCreate(context.Background(), "user-1", profile, binding, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.rotate(context.Background(), "user-1", profile, binding)
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID == first.SessionID {
		t.Fatal("rotate reused the session")
	}

	current, ok, previous, err := store.history(context.Background(), "user-1", profile, binding)
	if err != nil || !ok || current.SessionID != second.SessionID {
		t.Fatalf("history current = (%+v, %v, %v), want the rotated conversation", current, ok, err)
	}
	if len(previous) != 1 || previous[0].SessionID != first.SessionID {
		t.Fatalf("previous = %+v, want the replaced conversation", previous)
	}

	// Reopen the first: it is live again, the second is kept.
	reopened, err := store.switchTo(context.Background(), "user-1", profile, binding, first.SessionID, false)
	if err != nil || reopened.SessionID != first.SessionID || reopened.ConversationID != first.ConversationID {
		t.Fatalf("switchTo = (%+v, %v), want the first conversation with its identity", reopened, err)
	}
	current, _, previous, _ = store.history(context.Background(), "user-1", profile, binding)
	if current.SessionID != first.SessionID || len(previous) != 1 || previous[0].SessionID != second.SessionID {
		t.Fatalf("after switch: current=%s previous=%+v", current.SessionID, previous)
	}
	// Switching to the live one is a no-op.
	if again, err := store.switchTo(context.Background(), "user-1", profile, binding, first.SessionID, false); err != nil || again.SessionID != first.SessionID {
		t.Fatalf("switch to live = (%+v, %v)", again, err)
	}

	// An unknown session needs verification; verified, it becomes live with a
	// fresh conversation id.
	if _, err := store.switchTo(context.Background(), "user-1", profile, binding, "legacy-session", false); err == nil {
		t.Fatal("unverified unknown session became live")
	}
	legacy, err := store.switchTo(context.Background(), "user-1", profile, binding, "legacy-session", true)
	if err != nil || legacy.SessionID != "legacy-session" || legacy.ConversationID == first.ConversationID {
		t.Fatalf("verified unknown session = (%+v, %v)", legacy, err)
	}
	_, _, previous, _ = store.history(context.Background(), "user-1", profile, binding)
	if len(previous) != 2 || previous[0].SessionID != first.SessionID {
		t.Fatalf("previous after legacy switch = %+v, want first then second", previous)
	}

	// Forget the second; the live one is refused.
	if err := store.forget(context.Background(), "user-1", profile, binding, second.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.forget(context.Background(), "user-1", profile, binding, "legacy-session"); err == nil || !strings.Contains(err.Error(), "live") {
		t.Fatalf("forgetting the live conversation = %v, want refusal", err)
	}
	live, err := store.liveSessionIDs(context.Background(), "user-1", profile.ID)
	if err != nil || !live["legacy-session"] || live[first.SessionID] {
		t.Fatalf("live sessions = %v, want only the live one", live)
	}
	_, _, previous, _ = store.history(context.Background(), "user-1", profile, binding)
	if len(previous) != 1 || previous[0].SessionID != first.SessionID {
		t.Fatalf("previous after forget = %+v, want only the first", previous)
	}
}

func TestConversationTitleComesFromTheFirstMessage(t *testing.T) {
	if got := conversationTitleFrom(&ChatHistorySession{Query: "  How is Myra doing in fractions?\nmore"}, ""); got != "How is Myra doing in fractions?" {
		t.Fatalf("title = %q", got)
	}
	long := strings.Repeat("a", 100)
	if got := conversationTitleFrom(&ChatHistorySession{Query: long}, ""); !strings.HasSuffix(got, "…") || len([]rune(got)) > agentProfileConversationTitleLimit+1 {
		t.Fatalf("long title = %q", got)
	}
	if got := conversationTitleFrom(nil, ""); got != "New chat" {
		t.Fatalf("empty title = %q", got)
	}
	if got := normalizeConversationWorkspace("/_users/default/Chats/SparkQuill/"); got != "Chats/SparkQuill" {
		t.Fatalf("normalized workspace = %q", got)
	}
}
