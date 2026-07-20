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
	var missing []string
	if child == nil || strings.TrimSpace(child.Name) == "" {
		missing = append(missing, "name")
	}
	if child == nil || strings.TrimSpace(child.Grade) == "" {
		missing = append(missing, "grade")
	}
	if child == nil || strings.TrimSpace(child.Board) == "" {
		missing = append(missing, "board (e.g. CBSE, ICSE, State Board)")
	}
	childInfoNudge := ""
	if len(missing) > 0 {
		childInfoNudge = "IMPORTANT — you do not yet know the child's " + strings.Join(missing, ", ") +
			". Early in the conversation, warmly ask the parent for these, then save them with the set_child_profile tool. You need them to tailor material to the right grade and board.\n"
	}
	return "You are Quill, the SparkQuill learning guide, talking with a PARENT in Parent Mode about their child: " + who + ".\n" +
		"Help the parent understand and support " + name + "’s learning: explain progress from evidence, suggest one small next step, create child-ready study material, and create practice tests.\n" +
		"FORMAT — write replies as clean, simple Markdown for a chat bubble: short paragraphs, \"- \" bullets, \"1.\" numbered lists, and **bold** for emphasis. Do NOT hard-wrap lines yourself (let the app wrap), and NEVER draw ASCII tables or box characters — the app renders your Markdown into a nice bubble.\n" +
		"IMPORTANT — the parent is NOT technical. In your replies NEVER mention files, folders, paths, git, commits, JSON, tools, code, or technical steps — hide all the machinery. Speak in plain, warm, everyday language a busy parent understands. For example, say “I've safely saved a backup of everything” — not how or where it was stored. Do the technical work with your tools, but describe it simply.\n" +
		"Be a COACH, not just an assistant — stay one step ahead of the parent. You know global best practices in education and learning science (retrieval practice, spaced repetition, interleaving, active recall, worked-example fading, growth mindset) and exam strategy for the child’s school board. Proactively surface things the parent may not know yet: better ways to help " + name + " learn, common pitfalls at this level, and what strong students do. Use the web_search tool to bring in current best practices, board/exam patterns, and quality resources when useful — then translate them into one or two concrete, doable steps for " + name + " specifically. Anticipate; don’t wait to be asked.\n" +
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
		"Before you create study material, a test, a progress report, or the academic map, you MUST read the matching skill file in skills/ (e.g. `cat skills/create-test/SKILL.md`) and follow it exactly. Always output designed, self-contained, INTERACTIVE HTML (per skills/_shared/html-design.md) — never plain text/markdown — because " + name + " uses it on screen; tests must record the child’s answers with the SQ helper.\n" +
		"When you make material or a test, actually write the file, then call the open_file tool with its path so it opens on the right side for the parent, and tell them in plain words what you made. Keep file paths and technical details out of your reply unless the parent asks.\n" +
		"At the END of every turn, call the suggest_actions tool with 2–4 recommended next steps for the parent (short button label + the message to send if clicked) based on the conversation — e.g. update the progress report, create a practice test on this topic, make study material, or open a specific file.\n" +
		"You have skills — short how-to guides — in the skills/ folder. Read the relevant one and follow it exactly:\n" +
		"- skills/process-file/SKILL.md — process files the parent uploaded.\n" +
		"- skills/create-study-material/SKILL.md — make study notes and worked examples for " + name + ".\n" +
		"- skills/create-test/SKILL.md — make a practice test plus a separate parent-only answer key.\n" +
		"- skills/create-progress-report/SKILL.md — build an HTML progress report in shared/reports/ that appears in the left menu for both parent and child.\n" +
		"- skills/create-academic-map/SKILL.md — (re)build the HTML academic map at shared/academic-map.html from the real materials.\n" +
		"- skills/backup/SKILL.md — back up the workspace (local git checkpoint, a private GitHub repo, or an object store like Cloudflare R2 / S3).\n" +
		"- skills/publish/SKILL.md — publish a report or the academic map to a shareable destination (first publish is attended).\n" +
		"- skills/notify/SKILL.md — notify the parent (via notify_user) at moments worth their attention.\n" +
		"At the START of every conversation, run `ls shared/inbox/`; if it contains any files, process them with the process-file skill before doing anything else.\n" +
		childInfoNudge
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
	Name    string `json:"name,omitempty"`
	Grade   string `json:"grade,omitempty"`
	Board   string `json:"board,omitempty"`
	Path    string `json:"path,omitempty"`
}

// suggestion is one recommended next-step pill the UI shows after a turn.
type suggestion struct {
	Label   string `json:"label"`
	Message string `json:"message"`
}

type parentMessageResponse struct {
	Reply       string       `json:"reply,omitempty"`
	Error       string       `json:"error,omitempty"`
	ToolEvents  []toolEvent  `json:"tool_events,omitempty"`
	Suggestions []suggestion `json:"suggestions,omitempty"`
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

	setChildProfile := agentsession.Tool{
		Name: "set_child_profile",
		Description: "Save or update the child's profile — name, grade, and school board — once the parent tells you. " +
			"Call this whenever you learn any of these so future sessions and material are tailored to the right level.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":  map[string]interface{}{"type": "string", "description": "the child's name"},
				"grade": map[string]interface{}{"type": "string", "description": "the child's grade/class, e.g. 10"},
				"board": map[string]interface{}{"type": "string", "description": "the school board, e.g. CBSE, ICSE, State Board"},
			},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			nm, _ := args["name"].(string)
			gr, _ := args["grade"].(string)
			bd, _ := args["board"].(string)
			nm, gr, bd = strings.TrimSpace(nm), strings.TrimSpace(gr), strings.TrimSpace(bd)
			if nm == "" && gr == "" && bd == "" {
				return "", fmt.Errorf("provide at least one of name, grade, board")
			}
			stateMu.Lock()
			cur := loadState()
			if cur.Child == nil {
				cur.Child = &Child{Language: "en", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
			}
			if nm != "" {
				cur.Child.Name = nm
			}
			if gr != "" {
				cur.Child.Grade = gr
			}
			if bd != "" {
				cur.Child.Board = bd
			}
			err := saveState(cur)
			saved := cur.Child
			stateMu.Unlock()
			if err != nil {
				return "", fmt.Errorf("failed to save child profile: %w", err)
			}
			seedWorkspace(saved) // keep parent/child-profile.json (read by skills) in sync
			evMu.Lock()
			events = append(events, toolEvent{Tool: "set_child_profile", Name: saved.Name, Grade: saved.Grade, Board: saved.Board})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","name":%q,"grade":%q,"board":%q}`, saved.Name, saved.Grade, saved.Board), nil
		},
	}

	var sugMu sync.Mutex
	var suggestions []suggestion
	suggestActions := agentsession.Tool{
		Name: "suggest_actions",
		Description: "Offer the parent 2–4 recommended next steps as clickable buttons, based on the conversation so far. " +
			"Call this at the END of your turn. Each action has a short button label and the exact message that will be " +
			"sent as if the parent typed it when they click (e.g. update the progress report, create a practice test on " +
			"this topic, make study material, open a file).",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"actions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"label":   map[string]interface{}{"type": "string", "description": "short button text, 2–4 words"},
							"message": map[string]interface{}{"type": "string", "description": "the message sent as the parent when clicked"},
						},
						"required": []string{"label", "message"},
					},
				},
			},
			"required": []string{"actions"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			raw, _ := args["actions"].([]interface{})
			out := []suggestion{}
			for _, it := range raw {
				m, ok := it.(map[string]interface{})
				if !ok {
					continue
				}
				label, _ := m["label"].(string)
				msg, _ := m["message"].(string)
				label, msg = strings.TrimSpace(label), strings.TrimSpace(msg)
				if label == "" || msg == "" {
					continue
				}
				out = append(out, suggestion{Label: label, Message: msg})
				if len(out) >= 4 {
					break
				}
			}
			sugMu.Lock()
			suggestions = out
			sugMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","count":%d}`, len(out)), nil
		},
	}

	openFile := agentsession.Tool{
		Name: "open_file",
		Description: "Show a workspace file to the parent on the right side of the screen. Call this right after you " +
			"create or update a file the parent should see (study material, a test, a progress report, the academic map) " +
			"so it opens for them immediately. Pass the workspace-relative path.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "workspace-relative path to the file to display"},
			},
			"required": []string{"path"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			p, _ := args["path"].(string)
			p = strings.TrimSpace(p)
			if _, ok := resolveWorkspacePath(p); !ok {
				return "", fmt.Errorf("invalid path")
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "open_file", Path: p})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","opened":%q}`, p), nil
		},
	}

	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sess, cached, err := agentsession.Acquire(ctx, agentsession.Config{
		Provider:     provider,
		WorkingDir:   workDir,
		SystemPrompt: parentSystemPrompt(s.Child),
		SessionID:    req.ConversationID, // warm-resume the same conversation
		Tools:        []agentsession.Tool{setSubjectTopic, setChildProfile, openFile, suggestActions, webSearchTool(), readImageTool(s.Engine), notifyTool(), shellTool()},
	})
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}
	if !cached {
		defer sess.Close() // uncached one-off session — cache owns cached ones
	}

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}
	// Deterministically trigger inbox processing by embedding the directive in the
	// turn the agent directly acts on — coding agents reliably follow the user
	// message, not a proactive system-prompt line. The persisted transcript keeps
	// the parent's original message (this only affects what we send the agent).
	if inbox := inboxFiles(); len(inbox) > 0 && len(history) > 0 {
		history[len(history)-1].Text += "\n\n[Before replying: there are unprocessed uploads in shared/inbox/ (" +
			strings.Join(inbox, ", ") + "). Process each one now by following skills/process-file/SKILL.md — read it, classify, " +
			"move it into shared/materials/<subject>/<topic>/, and write a .meta.json — then continue with your reply.]"
	}

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: err.Error()})
		return
	}

	evMu.Lock()
	out := append([]toolEvent(nil), events...)
	evMu.Unlock()
	sugMu.Lock()
	sug := append([]suggestion(nil), suggestions...)
	sugMu.Unlock()
	persistConversation("parent", req.ConversationID, withReply(req.Messages, reply))
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply, ToolEvents: out, Suggestions: sug})
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
