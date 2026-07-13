package server

import (
	"log"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
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
	log.Printf("[BOT_EVENT_ADAPTER] SubscribeBot called for session %s", sessionID)
	sub := a.eventStore.Subscribe(sessionID)

	out := make(chan services.BotEventData, 256)

	// Goroutine to convert Event -> BotEventData
	go func() {
		defer close(out)
		count := 0
		for evt := range sub.Ch {
			count++
			log.Printf("[BOT_EVENT_ADAPTER] Event %d from EventStore: type=%s session=%s", count, evt.Type, sessionID)
			bed := services.BotEventData{
				Type:      evt.Type,
				Timestamp: evt.Timestamp,
			}
			if evt.Data != nil {
				bed.Data = evt.Data
			}
			out <- bed
		}
		log.Printf("[BOT_EVENT_ADAPTER] EventStore channel closed for session %s after %d events", sessionID, count)
	}()

	unsubscribe := func() {
		log.Printf("[BOT_EVENT_ADAPTER] Unsubscribing from session %s", sessionID)
		a.eventStore.Unsubscribe(sessionID, sub)
	}

	return out, unsubscribe
}
