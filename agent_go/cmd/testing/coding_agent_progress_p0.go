package testing

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Observe the same session working-set SSE endpoint as ChatArea. Polling omits
// streaming chunks and cannot prove that narration reached the live chat.
type progressP0Stream struct {
	events chan map[string]interface{}
	errors chan error
	close  func()
}

func (c *codingAgentChatE2EClient) openProgressP0Stream(ctx context.Context, sessionID string, since int) (*progressP0Stream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	endpoint := fmt.Sprintf("%s/api/sessions/%s/events/stream?since=%d&working_set=session", c.baseURL, url.PathEscape(sessionID), since)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Session-ID", sessionID)
	req.Header.Set("Accept", "text/event-stream")
	// The ordinary JSON client has a 30s timeout; an SSE response owns the turn.
	client := &http.Client{Transport: c.http.Transport}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("progress SSE HTTP %d", resp.StatusCode)
	}
	stream := &progressP0Stream{events: make(chan map[string]interface{}, 2048), errors: make(chan error, 1)}
	stream.close = func() { cancel(); resp.Body.Close() }
	go func() {
		defer resp.Body.Close()
		defer close(stream.events)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 8*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var message struct {
				Events []map[string]interface{} `json:"events"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &message); err != nil {
				stream.errors <- fmt.Errorf("decode progress SSE: %w", err)
				return
			}
			for _, event := range message.Events {
				select {
				case stream.events <- event:
				case <-streamCtx.Done():
					return
				}
			}
		}
		if streamCtx.Err() == nil {
			err := scanner.Err()
			if err == nil {
				err = io.EOF
			}
			stream.errors <- fmt.Errorf("progress SSE closed before completion: %w", err)
		}
	}()
	return stream, nil
}

func (s *progressP0Stream) until(ctx context.Context, match func(map[string]interface{}) bool) ([]map[string]interface{}, error) {
	var events []map[string]interface{}
	for {
		select {
		case <-ctx.Done():
			return events, ctx.Err()
		case event, ok := <-s.events:
			if !ok {
				select {
				case err := <-s.errors:
					return events, err
				default:
					return events, fmt.Errorf("progress SSE closed")
				}
			}
			events = append(events, event)
			if match(event) {
				return events, nil
			}
		}
	}
}

func progressP0Completion(event map[string]interface{}) bool {
	return fmt.Sprint(event["type"]) == "unified_completion"
}

// Tokens must occur exactly once in whole assistant transcript messages, in
// requested order, before the canonical final. Tool output, terminal pixels,
// user echoes, deltas and final-answer-only tokens cannot satisfy this proof.
func assertProgressP0Narration(events []map[string]interface{}, tokens []string) error {
	counts := make([]int, len(tokens))
	next := 0
	completed := false
	for _, event := range events {
		if progressP0Completion(event) {
			completed = true
			continue
		}
		if fmt.Sprint(event["type"]) != "streaming_chunk" || eventPayloadString(event, "source") != "transcript" {
			continue
		}
		if progressP0Flag(event, "is_delta") || progressP0Flag(event, "is_tool_call") {
			continue
		}
		content := eventPayloadString(event, "content")
		messageMarkers := 0
		for i, token := range tokens {
			occurrences := strings.Count(content, token)
			if occurrences == 0 {
				continue
			}
			messageMarkers++
			if messageMarkers > 1 {
				return fmt.Errorf("separate narration messages were merged into one chunk")
			}
			if completed {
				return fmt.Errorf("narration %s arrived after completion", token)
			}
			counts[i] += occurrences
			if counts[i] != 1 {
				return fmt.Errorf("narration %s was duplicated", token)
			}
			if i != next {
				return fmt.Errorf("narration %s arrived out of order", token)
			}
			next++
		}
	}
	if !completed {
		return fmt.Errorf("progress stream has no canonical completion")
	}
	for i, n := range counts {
		if n != 1 {
			return fmt.Errorf("missing live narration %s before completion", tokens[i])
		}
	}
	return nil
}

// Exercise retained Send, not GenerateContent: launch a slow MCP call, steer
// while it is running, and require narration from both sides of that steer.
func (c *codingAgentChatE2EClient) runCursorRetainedProgressP0(ctx context.Context, sessionID, model string) error {
	before, _, err := c.getEvents(ctx, sessionID)
	if err != nil {
		return err
	}
	stream, err := c.openProgressP0Stream(ctx, sessionID, before.LastProcessedIndex)
	if err != nil {
		return err
	}
	defer stream.close()
	markerA := "PROGRESS_BEFORE_STEER_" + uuid.NewString()
	markerB := "PROGRESS_AFTER_STEER_" + uuid.NewString()
	finalMarker := "STEER_FINISHED_" + uuid.NewString()
	query := fmt.Sprintf("This is a narration regression test. First send a standalone progress message containing %s. Then call api-bridge execute_shell_command with command `sleep 8; printf FIRST_TOOL_DONE`. After it returns, send a second standalone progress message containing %s, then call execute_shell_command with command `sleep 2; printf SECOND_TOOL_DONE`. Finally reply only BASE_FINISHED. If a follow-up changes the final reply, still complete both tool calls and both progress messages in this order.", markerA, markerB)
	ack, _, err := c.startQueryWithResponse(ctx, sessionID, "cursor-cli", model, query)
	if err != nil {
		return err
	}
	if ack.DeliveryStatus != "sent_to_cli" || ack.DeliverySource != "mcpagent_session" {
		return fmt.Errorf("progress P0 bypassed retained Session: %+v", ack)
	}
	first, err := stream.until(ctx, func(e map[string]interface{}) bool {
		return progressP0Completion(e) || (fmt.Sprint(e["type"]) == "tool_call_start" && eventPayloadString(e, "tool_name") == "execute_shell_command")
	})
	if err != nil {
		return err
	}
	if progressP0Completion(first[len(first)-1]) {
		return fmt.Errorf("completed before slow tool started")
	}
	followup := fmt.Sprintf("Keep doing the two requested tool calls and both progress messages. Change only your final reply to exactly %s.", finalMarker)
	ack, _, err = c.startQueryWithResponse(ctx, sessionID, "cursor-cli", model, followup)
	if err != nil {
		return err
	}
	if ack.DeliveryStatus != "sent_to_cli" || ack.DeliverySource != "mcpagent_session" {
		return fmt.Errorf("busy follow-up was not accepted by retained Session: %+v", ack)
	}
	rest, err := stream.until(ctx, progressP0Completion)
	if err != nil {
		return err
	}
	all := append(first, rest...)
	if err := assertProgressP0Narration(all, []string{markerA, markerB}); err != nil {
		return err
	}
	if final := extractUnifiedCompletionFinal(all); strings.TrimSpace(final) != finalMarker {
		return fmt.Errorf("steer not processed: final=%q, want %q", final, finalMarker)
	}
	ends := 0
	for _, event := range all {
		if fmt.Sprint(event["type"]) == "tool_call_end" && eventPayloadString(event, "tool_name") == "execute_shell_command" {
			ends++
		}
	}
	if ends != 2 {
		return fmt.Errorf("completed with %d tool receipts, want 2", ends)
	}
	if err := assertOneRetainedCompletion(all, finalMarker); err != nil {
		return err
	}
	return c.assertRetainedTmuxLive(ctx, sessionID)
}

// Drain through completion on the connection opened before retained delivery.
func assertRetainedProgressStream(ctx context.Context, stream *progressP0Stream, token string) error {
	if stream == nil {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	events, err := stream.until(timeoutCtx, progressP0Completion)
	if err != nil {
		return err
	}
	return assertProgressP0Narration(events, []string{token})
}

func progressP0Flag(event map[string]interface{}, key string) bool {
	for _, payload := range eventPayloadCandidates(event) {
		if value, ok := payload[key].(bool); ok && value {
			return true
		}
	}
	return false
}
