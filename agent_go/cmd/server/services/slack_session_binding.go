package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// One file per thread avoids unrelated channels overwriting each other's
// pointers. RouteKey is checked on restore, so repointing cannot adopt history.
func slackBindingPath(thread ThreadID) string {
	sum := sha256.Sum256([]byte(thread.Key()))
	return fmt.Sprintf("config/slack-threads/%x.json", sum)
}

func (s *SlackService) LoadBotSessionBinding(ctx context.Context, thread ThreadID, routeKey string) (BotSessionBinding, bool, error) {
	raw, found, err := readWorkspaceFile(ctx, workspaceAPIURL(), slackBindingPath(thread))
	if err != nil || !found {
		return BotSessionBinding{}, false, err
	}
	var binding BotSessionBinding
	if err := json.Unmarshal([]byte(raw), &binding); err != nil {
		return binding, false, err
	}
	return binding, binding.SessionID != "" && binding.RouteKey == routeKey, nil
}
func (s *SlackService) SaveBotSessionBinding(ctx context.Context, thread ThreadID, binding BotSessionBinding) error {
	raw, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	return writeWorkspaceFile(ctx, workspaceAPIURL(), slackBindingPath(thread), string(raw))
}
func (s *SlackService) ClearBotSessionBinding(ctx context.Context, thread ThreadID) error {
	return s.SaveBotSessionBinding(ctx, thread, BotSessionBinding{})
}
