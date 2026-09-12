package step_based_workflow

import (
	"strings"
	"testing"
)

func TestWorkshopBrowserSessionIDIsNamespacedPerUserChat(t *testing.T) {
	manish := workshopBrowserSessionID("user-manish--chat-one", "Workflow/testing", "research")
	shubham := workshopBrowserSessionID("user-shubham--chat-two", "Workflow/testing", "research")

	if manish == shubham {
		t.Fatalf("different users received the same workflow browser: %q", manish)
	}
	if !strings.HasPrefix(manish, "user-manish--chat-one--workflow-browser-") {
		t.Fatalf("workflow browser lost its authenticated namespace: %q", manish)
	}
	if !strings.HasSuffix(manish, "-research") {
		t.Fatalf("workflow browser lost its group label: %q", manish)
	}
}

func TestWorkshopBrowserSessionIDIsStableWithinUserChatAndGroup(t *testing.T) {
	first := workshopBrowserSessionID("user-one--chat-one", "Workflow/testing", "research")
	second := workshopBrowserSessionID("user-one--chat-one", "Workflow/testing", "research")
	if first != second {
		t.Fatalf("same user/chat/group was not stable: %q != %q", first, second)
	}
}
