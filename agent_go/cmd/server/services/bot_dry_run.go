package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

// ErrBotDryRunAdmitted is returned by a dry run's session start when the turn
// passed every check. runSession ends such a session quietly.
var ErrBotDryRunAdmitted = errors.New("bot dry run: turn admitted")

// BotDryRunOutcome is what a real message would have produced.
type BotDryRunOutcome struct {
	// Admitted: the turn passed routing, preparation, revalidation and access,
	// and would have started.
	Admitted bool `json:"admitted"`
	// Reason is the refusal, when the turn was not admitted.
	Reason string `json:"reason,omitempty"`
	// Replies are the messages the bot would have posted in the thread
	// (e.g. the user-facing refusal).
	Replies []string `json:"replies,omitempty"`
	// Destination describes where the turn would run.
	Destination string `json:"destination,omitempty"`
	// Request is the turn request the bot built (admitted or refused at the
	// query boundary); nil when it was refused earlier.
	Request map[string]interface{} `json:"-"`
	UserID  string                 `json:"-"`
}

// dryRunConnector wraps the real platform connector: capabilities, formatter
// and reads pass through; every write (messages, updates, reactions,
// listening) is recorded or dropped, so nothing reaches the platform.
type dryRunConnector struct {
	BotConnector
	mu      sync.Mutex
	replies []string
	sent    chan struct{}
}

func (c *dryRunConnector) record(text string) (string, error) {
	c.mu.Lock()
	c.replies = append(c.replies, text)
	c.mu.Unlock()
	select {
	case c.sent <- struct{}{}:
	default:
	}
	return fmt.Sprintf("dryrun-%d", time.Now().UnixNano()), nil
}

func (c *dryRunConnector) StartListening(context.Context) error { return nil }
func (c *dryRunConnector) StopListening()                       {}
func (c *dryRunConnector) SendThreadMessage(_ context.Context, _ ThreadID, message string) (string, error) {
	return c.record(message)
}
func (c *dryRunConnector) SendThreadMessageWithBlocks(_ context.Context, _ ThreadID, message string, _ []MessageBlock) (string, error) {
	return c.record(message)
}
func (c *dryRunConnector) UpdateMessage(context.Context, ThreadID, string, string) error { return nil }
func (c *dryRunConnector) AddReaction(context.Context, string, string, string) error     { return nil }
func (c *dryRunConnector) RemoveReaction(context.Context, string, string, string) error  { return nil }
func (c *dryRunConnector) SetMessageHandler(BotMessageHandler)                           {}
func (c *dryRunConnector) SetInteractionHandler(BotInteractionHandler)                   {}

func (c *dryRunConnector) recorded() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.replies...)
}

// RunDryRun sends msg through a manager wired exactly like m (every hook
// copied) whose only connector records instead of sending and whose session
// start is admit. admit receives the request the bot built and returns
// ErrBotDryRunAdmitted when the turn would start, or the refusal. It returns
// once the turn is admitted, refused, or timeout passes.
func (m *BotConversationManager) RunDryRun(ctx context.Context, msg BotIncomingMessage, admit SessionStartFunc, timeout time.Duration) (BotDryRunOutcome, error) {
	base := m.GetConnector(msg.Platform)
	if base == nil {
		return BotDryRunOutcome{}, fmt.Errorf("no %s connector is registered", msg.Platform)
	}
	connector := &dryRunConnector{BotConnector: base, sent: make(chan struct{}, 1)}
	type admission struct {
		req    map[string]interface{}
		userID string
		err    error
	}
	admitted := make(chan admission, 1)
	dry := &BotConversationManager{
		connectors:       map[string]BotConnector{msg.Platform: connector},
		sessions:         map[string]*activeBotSession{},
		chatStore:        m.chatStore,
		profileTurn:      m.profileTurn,
		workflowTurn:     m.workflowTurn,
		runningWorkflows: m.runningWorkflows,
		workflowAccess:   m.workflowAccess,
		resumeTarget:     m.resumeTarget,
		resumeList:       m.resumeList,
		mcpConfigPath:    m.mcpConfigPath,
		workspaceURL:     m.workspaceURL,
		// No event subscriber, follow-up or progressive reader: the turn
		// never runs, so there is nothing to stream.
	}
	dry.startSession = func(sessionCtx context.Context, req map[string]interface{}, sessionID, userID string, cb func(*events.AgentEvent)) error {
		err := admit(sessionCtx, req, sessionID, userID, cb)
		admitted <- admission{req: req, userID: userID, err: err}
		return err
	}
	msg.IsMention = true
	go dry.HandleIncomingMessage(msg)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case result := <-admitted:
			outcome := BotDryRunOutcome{Request: result.req, UserID: result.userID, Destination: dryRunDestination(result.req)}
			if errors.Is(result.err, ErrBotDryRunAdmitted) {
				outcome.Admitted = true
			} else if result.err != nil {
				outcome.Reason = result.err.Error()
			}
			// A refusal's user-facing message is posted right after start
			// returns; give it a moment to land.
			if !outcome.Admitted {
				select {
				case <-connector.sent:
				case <-time.After(2 * time.Second):
				}
			}
			outcome.Replies = connector.recorded()
			return outcome, nil
		case <-connector.sent:
			// Refused before a turn was built (no route, no access, ...):
			// the bot answered without starting a session. Wait briefly in
			// case a session start follows the acknowledgement.
			select {
			case result := <-admitted:
				admitted <- result
				continue
			case <-time.After(500 * time.Millisecond):
			}
			replies := connector.recorded()
			return BotDryRunOutcome{Reason: strings.Join(replies, "\n"), Replies: replies}, nil
		case <-timer.C:
			return BotDryRunOutcome{Reason: "no turn started and the bot did not answer (the message was ignored)", Replies: connector.recorded()}, nil
		case <-ctx.Done():
			return BotDryRunOutcome{}, ctx.Err()
		}
	}
}

func dryRunDestination(req map[string]interface{}) string {
	if req == nil {
		return ""
	}
	str := func(key string) string { value, _ := req[key].(string); return strings.TrimSpace(value) }
	if profile := str("agent_profile_id"); profile != "" {
		return fmt.Sprintf("crew %s (%s)", str("agent_profile_conversation_key"), str("selected_folder"))
	}
	if workflow := str("preset_query_id"); workflow != "" {
		return "workflow " + workflow
	}
	return "default chat"
}
