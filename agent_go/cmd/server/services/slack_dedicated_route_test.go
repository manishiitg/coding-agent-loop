package services

import (
	"context"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/mcpagent/events"
)

func installDedicatedSlackRoutes(t *testing.T, routes map[string]*ChannelRoute) {
	t.Helper()
	SetDedicatedSlackRouteFunc(func(_ context.Context, connectionID string) (*ChannelRoute, bool) {
		route, dedicated := routes[connectionID]
		return route, dedicated
	})
	t.Cleanup(func() { SetDedicatedSlackRouteFunc(nil) })
}

// A scoped app answers for its own workflow in any channel; a shared app
// follows the channel route; a scoped app whose workflow is gone is refused
// rather than answering as the channel's workflow.
func TestResolveSlackRouteDedicatedAppIgnoresChannelRoute(t *testing.T) {
	own := &ChannelRoute{WorkflowID: "wf-v3", WorkspacePath: "Workflow/testingv3", BotGrant: "run"}
	installDedicatedSlackRoutes(t, map[string]*ChannelRoute{"app-v3": own, "app-gone": nil})
	channelRoute := &ChannelRoute{WorkflowID: "wf-testing", WorkspacePath: "Workflow/testing", BotGrant: "run"}
	channel := func() *ChannelRoute { return channelRoute }

	if got := ResolveSlackRoute(context.Background(), "app-v3", channel); got != own {
		t.Fatalf("dedicated app routed to %+v, want its own workflow", got)
	}
	if got := ResolveSlackRoute(context.Background(), "shared", channel); got != channelRoute {
		t.Fatalf("shared app routed to %+v, want the channel route", got)
	}
	if got := ResolveSlackRoute(context.Background(), "", channel); got != channelRoute {
		t.Fatalf("default listener routed to %+v, want the channel route", got)
	}
	got := ResolveSlackRoute(context.Background(), "app-gone", channel)
	if got == nil || !IsRevokedSlackRoute(*got) {
		t.Fatalf("unresolvable dedicated app routed to %+v, want the revoked sentinel", got)
	}
}

// With two bots holding sessions in one thread, a plain reply reaches
// neither; the user tags the bot they mean.
func TestPlainReplyInMultiBotThreadIsIgnored(t *testing.T) {
	routeA := &ChannelRoute{WorkflowID: "wf-a", WorkspacePath: "Workflow/a", BotGrant: "run", WorkshopMode: "run"}
	routeB := &ChannelRoute{WorkflowID: "wf-b", WorkspacePath: "Workflow/b", BotGrant: "run", WorkshopMode: "run"}
	installDedicatedSlackRoutes(t, map[string]*ChannelRoute{"app-a": routeA, "app-b": routeB})
	manager := NewBotConversationManager(nil, "", "")
	manager.RegisterConnector(&testBotConnector{name: "slack", supportsThreads: true})
	manager.SetWorkflowAccessFunc(func(_ context.Context, userID, _ string, _ ChannelRoute) (string, bool, error) {
		return userID, true, nil
	})
	followUps := make(chan string, 2)
	manager.SetFollowUpFunc(func(_ context.Context, _ map[string]interface{}, sessionID string, _ string) error {
		followUps <- sessionID
		return nil
	})
	manager.SetStartSessionFunc(func(_ context.Context, _ map[string]interface{}, sessionID string, _ string, _ func(event *events.AgentEvent)) error {
		followUps <- sessionID
		return nil
	})

	threadA := ThreadID{Platform: "slack", ChannelID: "C1", ThreadTS: "1.0", ConnectionID: "app-a"}
	threadB := threadA
	threadB.ConnectionID = "app-b"
	manager.mu.Lock()
	for thread, route := range map[ThreadID]*ChannelRoute{threadA: routeA, threadB: routeB} {
		manager.sessions[thread.Key()] = &activeBotSession{
			SessionID:     "session-" + thread.ConnectionID,
			UserID:        "bot-route",
			Status:        chathistory.BotSessionStatusCompleted,
			Platform:      "slack",
			ThreadID:      thread,
			PresetQueryID: route.WorkflowID,
			WorkspacePath: route.WorkspacePath,
			RouteKey:      botRouteKey(route),
			LastActivity:  time.Now(),
			builderDone:   true,
		}
	}
	manager.mu.Unlock()

	if !manager.threadHasOtherBot(threadA, "") {
		t.Fatal("second app's session in the thread not detected")
	}
	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform:     "slack",
		UserID:       "U1",
		UserEmail:    "alice@example.com",
		ChannelID:    "C1",
		ThreadTS:     "1.0",
		ConnectionID: "app-a",
		Text:         "plain follow-up",
	})
	select {
	case sessionID := <-followUps:
		t.Fatalf("plain reply in a two-bot thread reached %s", sessionID)
	case <-time.After(200 * time.Millisecond):
	}

	manager.mu.Lock()
	delete(manager.sessions, threadB.Key())
	manager.mu.Unlock()
	if manager.threadHasOtherBot(threadA, "") {
		t.Fatal("single-bot thread reported as multi-bot")
	}
	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform:     "slack",
		UserID:       "U1",
		UserEmail:    "alice@example.com",
		ChannelID:    "C1",
		ThreadTS:     "1.0",
		ConnectionID: "app-a",
		Text:         "plain follow-up",
	})
	select {
	case sessionID := <-followUps:
		if sessionID != "session-app-a" {
			t.Fatalf("single-bot plain reply reached %s, want session-app-a", sessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("plain reply in a single-bot thread was dropped")
	}
}
