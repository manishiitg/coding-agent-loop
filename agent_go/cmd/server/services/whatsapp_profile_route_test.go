package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

// An @token that is not a workflow slug is offered to the default product's
// router; a workflow slug still wins, and a token nobody knows is unknown.
func TestResolveFallsBackToTheProductRouterAfterWorkflowSlugs(t *testing.T) {
	svc := &WhatsAppService{routing: WhatsAppRouting{"report": {WorkflowID: "wf-report", WorkspacePath: "Workflow/report"}}}
	svc.SetProfileRouter(func(_ context.Context, token string) (*ProfileRoute, error) {
		switch token {
		case "child":
			return &ProfileRoute{ProfileID: "sparkquill-child", ConversationKey: "fractions", Label: "Myra's tutor"}, nil
		case "report":
			t.Fatal("product router consulted for a workflow slug")
		case "broken":
			return nil, errors.New("No activity yet")
		}
		return nil, nil
	})

	if route := svc.Resolve(context.Background(), "dm", "Report"); route == nil || route.Key != "report" {
		t.Fatalf("@report = %+v, want the workflow route", route)
	} else if _, isWorkflow := route.Value.(*ChannelRoute); !isWorkflow {
		t.Fatalf("@report value = %T, want *ChannelRoute", route.Value)
	}
	route := svc.Resolve(context.Background(), "dm", "Child")
	if route == nil || route.Key != "child" {
		t.Fatalf("@child = %+v, want the profile route", route)
	}
	if profile, ok := route.Value.(*ProfileRoute); !ok || profile.ProfileID != "sparkquill-child" || profile.ConversationKey != "fractions" {
		t.Fatalf("@child value = %+v, want the child profile keyed by the activity", route.Value)
	}
	if route := svc.Resolve(context.Background(), "dm", "nothing"); route != nil {
		t.Fatalf("@nothing = %+v, want unknown", route)
	}
	if route := svc.Resolve(context.Background(), "dm", "broken"); route != nil {
		t.Fatalf("@broken = %+v, want unknown (the product's reason is shown by RouteUnknown)", route)
	}
}

// A message routed to one of the product's profiles starts its own session
// (a different route key from the default chat), and every later turn in it
// carries the same profile route.
func TestProfileRoutedMessageKeepsItsProfileAcrossTurns(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	manager.RegisterConnector(&testBotConnector{})

	child := &ProfileRoute{ProfileID: "sparkquill-child", ConversationKey: "fractions"}
	manager.SetProfileTurnFunc(func(_ context.Context, _ string, msg BotIncomingMessage, _ ThreadID) (map[string]interface{}, string, bool, error) {
		if msg.PresetProfile == nil || msg.PresetProfile.ProfileID != "sparkquill-child" {
			t.Errorf("profile turn built without the @child route: %+v", msg.PresetProfile)
		}
		return map[string]interface{}{"agent_profile_id": "sparkquill-child", "query": msg.Text}, "conv-fractions", true, nil
	})
	started := make(chan string, 1)
	manager.SetStartSessionFunc(func(_ context.Context, _ map[string]interface{}, sessionID, _ string, _ func(*events.AgentEvent)) error {
		started <- sessionID
		return nil
	})

	msg := BotIncomingMessage{Platform: "whatsapp", UserID: "phone", WorkspaceUserID: "user-1", ChannelID: "dm", Text: "2/5 + 1/5?", IsMention: true, PresetProfile: child}
	if key, def := botMessageRouteKey(msg), botMessageRouteKey(BotIncomingMessage{}); key == def || key == "" {
		t.Fatalf("route key for @child = %q, want distinct from the default chat's %q", key, def)
	}
	manager.HandleIncomingMessage(msg)
	select {
	case sessionID := <-started:
		if sessionID != "conv-fractions" {
			t.Fatalf("session = %q, want the child's activity conversation", sessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session never started")
	}

	manager.mu.RLock()
	active := manager.sessions[ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}.Key()]
	manager.mu.RUnlock()
	if active == nil || active.profileRoute == nil || active.profileRoute.ProfileID != "sparkquill-child" {
		t.Fatalf("active session = %+v, want the @child route remembered", active)
	}
	req := manager.turnRequestForActive(active, "is it 3/5?", "user-1", "whatsapp", active.ThreadID)
	if req["agent_profile_id"] != "sparkquill-child" || req["query"] != "is it 3/5?" {
		t.Fatalf("follow-up = %#v, want built as the child's turn", req)
	}
}
