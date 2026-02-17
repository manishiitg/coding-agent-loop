package server

import (
	"mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/internal/events"
)

// BotEventSubscriberAdapter bridges EventStore to the BotEventSubscriber interface
// used by the services package (avoiding import cycles).
type BotEventSubscriberAdapter struct {
	eventStore *events.EventStore
}

// NewBotEventSubscriberAdapter creates a new adapter
func NewBotEventSubscriberAdapter(es *events.EventStore) *BotEventSubscriberAdapter {
	return &BotEventSubscriberAdapter{eventStore: es}
}

// SubscribeBot subscribes to events for a session and returns a channel + unsubscribe func
func (a *BotEventSubscriberAdapter) SubscribeBot(sessionID string) (<-chan services.BotEventData, func()) {
	sub := a.eventStore.Subscribe(sessionID, "advanced")

	out := make(chan services.BotEventData, 256)

	// Goroutine to convert Event -> BotEventData
	go func() {
		defer close(out)
		for evt := range sub.Ch {
			bed := services.BotEventData{
				Type:      evt.Type,
				Timestamp: evt.Timestamp,
			}
			if evt.Data != nil {
				bed.Data = evt.Data
			}
			out <- bed
		}
	}()

	unsubscribe := func() {
		a.eventStore.Unsubscribe(sessionID, sub)
	}

	return out, unsubscribe
}
