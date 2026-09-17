package services

import (
	"context"
	"github.com/slack-go/slack/slackevents"
	"strings"
)

// SlackTrigger is configured by the route owner, never by message payloads.
type SlackTrigger struct {
	Type            string            `json:"type"` // human_message or trusted_app
	AppID           string            `json:"app_id,omitempty"`
	BotID           string            `json:"bot_id,omitempty"`
	Contains        string            `json:"contains,omitempty"`
	RouteSelections map[string]string `json:"route_selections,omitempty"`
	StepID          string            `json:"step_id,omitempty"`
	GroupNames      []string          `json:"group_names,omitempty"`
}
type SlackTriggerHandler func(context.Context, string, *slackevents.MessageEvent) error

func (s *SlackService) SetTriggerHandler(handler SlackTriggerHandler) { s.triggerHandler = handler }
func SlackTriggerMatches(trigger *SlackTrigger, event *slackevents.MessageEvent, ownUserID string) bool {
	if trigger == nil || event == nil || event.TimeStamp == "" || event.User == ownUserID && ownUserID != "" || event.ThreadTimeStamp != "" && event.ThreadTimeStamp != event.TimeStamp {
		return false
	}
	if event.SubType != "" && event.SubType != "bot_message" {
		return false
	}
	switch trigger.Type {
	case "human_message":
		if event.BotID != "" || event.User == "" {
			return false
		}
	case "trusted_app":
		if event.BotID == "" || trigger.BotID == "" && trigger.AppID == "" {
			return false
		}
		if trigger.BotID != "" && event.BotID != trigger.BotID {
			return false
		}
		appID := ""
		if event.Message != nil && event.Message.BotProfile != nil {
			appID = event.Message.BotProfile.AppID
		}
		if trigger.AppID != "" && appID != trigger.AppID {
			return false
		}
	default:
		return false
	}
	return trigger.Contains == "" || strings.Contains(event.Text, trigger.Contains)
}
