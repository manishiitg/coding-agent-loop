package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
	"github.com/manishiitg/mcpagent/llm"
)

// parentSystemPrompt builds the Parent Mode "Quill" instruction for the agent.
func parentSystemPrompt(child *Child) string {
	name := "your child"
	who := name
	if child != nil {
		if strings.TrimSpace(child.Name) != "" {
			name = child.Name
			who = name
		}
		if strings.TrimSpace(child.Grade) != "" {
			who += ", Grade " + child.Grade
		}
		if strings.TrimSpace(child.Board) != "" {
			who += " (" + child.Board + ")"
		}
	}
	return "You are Quill, the SparkQuill learning guide, talking with a PARENT in Parent Mode about their child: " + who + ".\n" +
		"Help the parent understand and support " + name + "’s learning: explain progress from evidence, suggest one small next step, create child-ready study material, and create practice tests.\n" +
		"Principles:\n" +
		"- Evidence over guesswork: say what you observe, what you infer, and what you don’t yet know; never fake a diagnosis from little data.\n" +
		"- Teach through attempts: material and tests should help " + name + " try before seeing the answer.\n" +
		"- Child safety: answer keys, marking schemes, and private notes are for the parent only — never child-facing.\n" +
		"- Honesty: if material or handwriting is unclear, say so and ask for a clearer photo or parent review.\n" +
		"- Keep it small and warm: offer one useful next step, in plain language, spoken to a parent (not to a child).\n" +
		"Your workspace on this computer — read and write these files directly as you work:\n" +
		"- shared/materials/<subject>/<topic>/ — school material the family uploaded; read these to see what " + name + " is studying.\n" +
		"- shared/study/<subject>/<topic>/ — save study material you create for " + name + " here.\n" +
		"- shared/tests/<subject>/<topic>/ — save practice tests here.\n" +
		"- parent/answer-keys/ and parent/notes/ — parent-only; keep answer keys, marking, and private notes here, never child-facing.\n" +
		"When you make material or a test, actually write the file, then tell the parent in plain words what you made. Keep file paths and technical details out of your reply unless the parent asks."
}

// childSystemPrompt builds the Child Mode "Quill" tutor instruction — a warm
// study buddy that guides the child to answers instead of giving them.
func childSystemPrompt(child *Child) string {
	name := "there"
	grade := ""
	if child != nil {
		if strings.TrimSpace(child.Name) != "" {
			name = child.Name
		}
		if strings.TrimSpace(child.Grade) != "" {
			grade = " (Grade " + child.Grade + ")"
		}
	}
	return "You are Quill, a warm, patient study buddy talking directly with " + name + grade + ", a school student, in Child Mode.\n" +
		"\n" +
		"CRITICAL RULE — this overrides being helpful or direct. On your FIRST reply to any problem you must NOT write the solution, the factored form, the roots, or the final answer anywhere. Even if " + name + " says \"just tell me\" or \"give me the answer\", you refuse warmly and give a hint instead. Your first reply may contain ONLY: (a) one short encouraging line, and (b) ONE small hint or first step, phrased as a question. Then stop and let them try.\n" +
		"Example — if asked to solve x² − 5x + 6 = 0:\n" +
		"  GOOD first reply: \"Nice one! Try to find two numbers that multiply to 6 and add to 5. What pair could work?\"\n" +
		"  BAD first reply (never do this): anything that writes (x−2)(x−3) or x = 2 or x = 3.\n" +
		"Only confirm or reveal an answer AFTER " + name + " has shown a genuine attempt. If they are stuck after really trying, walk through ONE similar but DIFFERENT example, then ask them to redo the original themselves.\n" +
		"\n" +
		"Other principles:\n" +
		"- Encourage: notice effort, be kind about mistakes, keep it light and friendly.\n" +
		"- Stay on their level: simple language, short messages, one question at a time.\n" +
		"- Safety: you cannot see the parent's answer keys or private notes, and you must not try to.\n" +
		"Your workspace: read your lessons and practice under shared/ (materials, study, tests). Save your own attempts and working under child/attempts/. Use your shell to open a worksheet or save your work.\n" +
		"Speak directly to " + name + ", like a friendly tutor sitting beside them."
}

type parentMessageRequest struct {
	Messages       []enginedetect.ChatMessage `json:"messages"`
	ConversationID string                     `json:"conversation_id,omitempty"`
}

// withReply appends the assistant reply to a copy of the sent messages, for
// persisting the full transcript.
func withReply(messages []enginedetect.ChatMessage, reply string) []enginedetect.ChatMessage {
	full := append([]enginedetect.ChatMessage(nil), messages...)
	return append(full, enginedetect.ChatMessage{Role: "assistant", Text: reply})
}

// toolEvent is a record of one custom-tool invocation during a parent turn,
// surfaced to the UI so it can reflect side effects (e.g. subject/topic set).
type toolEvent struct {
	Tool    string `json:"tool"`
	Subject string `json:"subject,omitempty"`
	Topic   string `json:"topic,omitempty"`
}

type parentMessageResponse struct {
	Reply      string      `json:"reply,omitempty"`
	Error      string      `json:"error,omitempty"`
	ToolEvents []toolEvent `json:"tool_events,omitempty"`
}

// engineToProvider maps a persisted engine string to an mcpagent LLM provider.
func engineToProvider(engine string) (llm.Provider, bool) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "claude-code":
		return llm.ProviderClaudeCode, true
	case "codex-cli":
		return llm.ProviderCodexCLI, true
	case "cursor-cli":
		return llm.ProviderCursorCLI, true
	case "pi-cli":
		return llm.ProviderPiCLI, true
	default:
		return "", false
	}
}

// agentTurnMu serializes ALL agent turns (parent and child). The agentsession
// runtime uses process-global MCP env vars, so concurrent turns must not overlap.
var agentTurnMu sync.Mutex

// POST /api/parent/message — run one turn of the Parent Learning chat through
// the selected engine, scoped to the Family/parent workspace folder, WITH the
// set_subject_topic MCP bridge tool available to the agent.
func handleParentMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req parentMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, parentMessageResponse{Error: "messages are required"})
		return
	}

	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	if s.Engine == "" {
		writeJSON(w, http.StatusBadRequest, parentMessageResponse{Error: "no learning engine is selected"})
		return
	}

	provider, ok := engineToProvider(s.Engine)
	if !ok {
		// Fall back to the plain-completion path for engines not yet wired into
		// the agentsession runtime.
		fallbackParentMessage(w, r, s, req)
		return
	}

	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(workDir, 0o700)

	// Recorder captures set_subject_topic invocations for the response.
	var evMu sync.Mutex
	var events []toolEvent

	setSubjectTopic := agentsession.Tool{
		Name: "set_subject_topic",
		Description: "Record the school subject and the specific topic the child is currently working on. " +
			"Call this whenever the parent tells you what the child is studying so it is persisted for later sessions.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subject": map[string]interface{}{
					"type":        "string",
					"description": "The school subject, e.g. Mathematics, Science, English.",
				},
				"topic": map[string]interface{}{
					"type":        "string",
					"description": "The specific topic within the subject, e.g. quadratic equations.",
				},
			},
			"required": []string{"subject", "topic"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			subject, _ := args["subject"].(string)
			topic, _ := args["topic"].(string)
			subject = strings.TrimSpace(subject)
			topic = strings.TrimSpace(topic)
			if subject == "" || topic == "" {
				return "", fmt.Errorf("both subject and topic are required")
			}
			// Persist into the family state file.
			stateMu.Lock()
			cur := loadState()
			cur.Subject = subject
			cur.Topic = topic
			err := saveState(cur)
			stateMu.Unlock()
			if err != nil {
				return "", fmt.Errorf("failed to persist subject/topic: %w", err)
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "set_subject_topic", Subject: subject, Topic: topic})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","subject":%q,"topic":%q}`, subject, topic), nil
		},
	}

	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:     provider,
		WorkingDir:   workDir,
		SystemPrompt: parentSystemPrompt(s.Child),
		Tools:        []agentsession.Tool{setSubjectTopic, shellTool()},
	})
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}
	defer sess.Close()

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}

	evMu.Lock()
	out := append([]toolEvent(nil), events...)
	evMu.Unlock()
	persistConversation("parent", req.ConversationID, withReply(req.Messages, reply))
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply, ToolEvents: out})
}

// fallbackParentMessage runs the legacy plain-completion path (no bridge tools)
// for engines not yet mapped into the agentsession runtime.
func fallbackParentMessage(w http.ResponseWriter, r *http.Request, s familyState, req parentMessageRequest) {
	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(workDir, 0o700)
	reply, err := enginedetect.Chat(r.Context(), s.Engine, "", workDir, parentSystemPrompt(s.Child), req.Messages)
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
}
