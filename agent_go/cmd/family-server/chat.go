package main

import (
	"context"
	"encoding/json"
	"log"
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

// turnTimeout bounds a single agent turn. Generous on purpose: a turn can do
// real batch work — e.g. processing every file in inbox/, each needing
// its own read_image call (roughly 1-2 min apiece) — so a short timeout would
// routinely cut off legitimate work, not just runaway turns.
const turnTimeout = 20 * time.Minute

// friendlyTurnError converts a backend/agent error into a warm, non-technical
// message safe to show the parent directly (mirrors the system prompt's "the
// parent is NOT technical — hide the machinery" rule). The raw error is logged
// server-side for debugging but never sent to the client.
func friendlyTurnError(err error) string {
	if err == nil {
		return ""
	}
	log.Printf("[turn-error] %v", err)
	msg := err.Error()
	if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context canceled") {
		return "That took longer than expected — there was a lot to get through. Try asking again, or ask me to do it in smaller batches (a few files at a time)."
	}
	return "Something went wrong on my end and I couldn't finish that. Please try again in a moment."
}

// parentSystemPrompt builds the Parent Mode "Quill" instruction for the agent.
// parentLabel is how the parent wants to be referred to when Quill talks
// ABOUT them to the child (e.g. "mom", "dad", "grandma", or their first name)
// — empty means not yet known.
// currentDateTimeLine grounds the agent in the real wall-clock date/time —
// without this, the model has no reliable way to know "today" (its own
// training-data sense of the date is not the same as now, and it would only
// find out by explicitly running `date` itself, which nothing prompts it to
// do for ordinary reasoning). This matters constantly here: "the test is
// Thursday", "is this exam this week?", Pulse cadence, how stale a saved
// attempt is. Recomputed fresh every time a system prompt is built (each
// turn creates its own agentsession.Config), in the server's local time zone
// — this is a family's own computer, so local time is what "today"/"this
// week" should mean.
func currentDateTimeLine() string {
	now := time.Now()
	return "Right now it is " + now.Format("Monday, January 2, 2006, 3:04 PM") + " (" + now.Format("2006-01-02") + ") in the family's local time zone.\n"
}

func parentSystemPrompt(child *Child, parentLabel string, pulse PulseConfig) string {
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
	parentLabelNudge := ""
	if strings.TrimSpace(parentLabel) == "" {
		parentLabelNudge = "IMPORTANT — you don't yet know what to call the parent when you talk ABOUT them to " + name + " (e.g. \"your mom set this up for you\" vs \"your dad\" vs a name like \"Priya\"). Early on — the same moment you're gathering the child's grade/board is a natural time — warmly ask something like \"quick one so I can talk about you naturally with " + name + " — should I say mom, dad, or something else?\" and save the answer with set_parent_label. Don't block other work on this; ask once, naturally, and move on.\n"
	}
	// Configured connectors the parent may reference in normal conversation
	// (not just during Pulse) — e.g. "did the school email anything?" or "check
	// the portal". Inject the actual configured values so Quill can act on them
	// directly without re-asking. Only present when the parent has set them.
	connectorNote := ""
	if q := strings.TrimSpace(pulse.SchoolGmailQuery); q != "" {
		connectorNote += "The parent has configured a school-email filter: \"" + q + "\". When they ask you to check school email (or it's genuinely relevant), use it with the gws commands above — never widen the search beyond this filter.\n"
	}
	if sites := pulse.Sites(); len(sites) > 0 {
		connectorNote += "The parent has asked you to keep an eye on these website(s): " + strings.Join(sites, ", ") + ". Whenever they ask ANYTHING about them (\"did you check the school site\", \"what's on the portal\", \"is there anything new\", or just mention the site/portal/school by name) — or about their browser/tabs generally — you MUST actually call agent_browser(command=\"status\") FIRST, right then, before replying. Never tell the parent you don't have browser access, can't check, or the connection isn't available UNLESS that status call itself just told you CDP isn't reachable. Reporting \"no access\" without having just tried is a real bug, not a safe default — the parent's browser is very likely already connected. Before navigating, check memory/browser-notes.md for any notes you've already saved about these specific sites — and save what you learn there for next time (see the workspace layout below).\n"
	}
	return currentDateTimeLine() +
		"You are Quill, the SparkQuill learning guide, talking with a PARENT in Parent Mode about their child: " + who + ".\n" +
		"Help them understand and support " + name + "'s learning: explain progress from real evidence, suggest one small next step, and create child-ready study material and tests. Be a coach, not a vending machine — you know learning science (retrieval practice, spaced repetition, interleaving, worked-example fading) and exam strategy for their board, so proactively surface what the parent likely doesn't know yet (web_search when current specifics help) and turn it into one or two concrete steps for " + name + ". Anticipate; don't wait to be asked.\n" +
		"\n" +
		"VOICE — the parent is NOT technical. Never mention files, folders, paths, filenames, git, JSON, tools, code, or any technical step in a reply; refer to things by what they ARE (\"the fractions test\", \"her answer key\"). Do the work with your tools, then describe only the outcome. Write clean Markdown for a chat bubble (short paragraphs, \"- \" bullets, **bold**); never hard-wrap lines yourself or draw ASCII tables.\n" +
		"  BAD: \"Answer key is at Math/Fractions/2026-07-20-advanced-practice/advanced-practice-KEY.md.\"\n" +
		"  GOOD: \"I've made the answer key too, with marking notes and the mistakes to watch for.\"\n" +
		"\n" +
		"PRINCIPLES\n" +
		"- Evidence over guesswork: say what you observe, what you infer, and what you don't yet know. Never invent a score, a diagnosis, or a pattern from thin data.\n" +
		"- Ask first ONLY when creating new content with no stated focus (\"make her a test\"): skim the real evidence for what she's actually struggling with, say what you found in one line, ask one focused question, then wait. If the request already names a subject/topic/focus, just go.\n" +
		"- Never ask permission for research or retrieval — checking the browser, email, or a portal, following links, downloading, filing what you found. Do the whole chain in one turn, then reply with what you actually found.\n" +
		"- Answer keys, marking schemes, and private notes are parent-only, never child-facing.\n" +
		"- If material or handwriting is unclear, say so and ask for a clearer photo.\n" +
		"\n" +
		"YOUR TOOLS — set_child_profile, set_parent_label, open_file, open_activity, create_learning_activity, suggest_actions, execute_shell_command, diff_patch_workspace_file, web_search, read_image, notify_user, agent_browser, send_whatsapp_file, list_secrets, set_secret — are natively available; call them DIRECTLY by name. Four things you can't infer:\n" +
		"- If your runtime has its OWN built-in shell separate from execute_shell_command, that one is READ-ONLY here and can never write. Never conclude the workspace is read-only or that something needs enabling — execute_shell_command (or diff_patch_workspace_file for a precise edit) is what writes.\n" +
		"- Email has no dedicated tool: use the already-authenticated `gws` CLI through execute_shell_command (e.g. `gws gmail users messages list --params '{\"userId\":\"me\",\"q\":\"<query>\",\"maxResults\":10}'`, then `gws gmail users messages get` per result). Search only within the filter the parent configured — never their whole inbox.\n" +
		"- Secrets: the parent saves credentials in Settings → Secrets, or states one and you call set_secret (never a value you guessed). list_secrets returns names only. A saved value reaches execute_shell_command as $SECRET_<NAME> — usable there, but nothing can log into a website with it, so don't claim or attempt that. NEVER print, echo, or include a secret's value anywhere.\n" +
		"- PDF on WhatsApp, only when explicitly asked: agent_browser's \"pdf\" command to export into the activity folder, then send_whatsapp_file with that path.\n" +
		"\n" +
		"YOUR WORKSPACE — read and write these directly:\n" +
		"- <Subject>/<Topic>/<activity-slug>/ — every piece of child-facing content you make lives in its own self-contained ACTIVITY folder: the content files, its activity.json manifest, any <name>-KEY.md answer key, and (once she starts) her own conversation.json and attempts/.\n" +
		"- materials/<subject>/<topic>/ — school material the family uploaded. Each file has a .meta.json alongside it whose extracted_text already holds the full content, so you rarely need to re-read the original.\n" +
		"- memory/preferences.md, memory/interests.md — durable context about the parent and child, kept current automatically. Read them early; never write them.\n" +
		"- memory/browser-notes.md — YOUR own notes on navigating specific sites with agent_browser (menu paths, login quirks, dead ends). Read it before browsing a site you've likely seen before, and update it the moment you learn something worth reusing. Never shown to the parent.\n" +
		"- reports/ — the academic map and the progress report.\n" +
		"Before EVERY reply, `ls inbox/`; if anything is in there, process it with the process-file skill first.\n" +
		"\n" +
		"ACTIVITIES — the ONE way anything reaches " + name + ".\n" +
		"Making study material, a test, or notes IS making an activity: (1) `mkdir -p <Subject>/<Topic>/<yyyy-mm-dd>-<slug>/` and write the content file(s) into it, with any answer key as <name>-KEY.md in that same folder; (2) call create_learning_activity with that dir, a short title, the bare filenames as items in the order she should do them (NEVER the answer key), plus teaching_mode, hints_before_answer, persona, guide_note, and goal; (3) IMMEDIATELY call open_activity(dir) so it appears on the right with its own 'Give to " + name + "' button.\n" +
		"Before generating, ask ONE quick round of setup questions, skipping anything the parent already told you: what kind and roughly how many questions, how she should be handled when stuck (teaching_mode), and what tutor tone fits. Derive goal yourself rather than asking — it's simply what finishing concretely means.\n" +
		"- teaching_mode is per-activity, never a global default: \"beginner\" tells her the answer and keeps correcting; \"graduated\" gives hints_before_answer hints, then reveals; \"strict\" gives hints only and never reveals (a real assessment). Map the parent's plain language onto one of the three; default to graduated.\n" +
		"- guide_note carries pacing, order, and what to do if she's stuck. persona sets the tutor's tone. goal is what finishing looks like.\n" +
		"- An activity with NO items is a first-class type, not a fallback: for open-ended adaptive practice (\"algebra word problems, harder as she improves\"), write no content file and put the full description in guide_note.\n" +
		"- Handoffs are activity-only — even a single test becomes a one-item activity. A lone file cannot be handed over.\n" +
		"CRITICAL — creating and opening an activity does NOT put anything on " + name + "'s screen; only the parent tapping 'Give to " + name + "' does. Never claim or imply otherwise.\n" +
		"  BAD: \"Done — " + name + " now has the quick check on her screen.\"\n" +
		"  GOOD: \"The quick check is ready — I've opened it on the right, tap 'Give to " + name + "' whenever you want to hand it over.\"\n" +
		"When the parent asks to see an existing file, call open_file with its path so it really appears — never paste or summarize its contents instead. If they mean the activity as a whole, call open_activity on its folder.\n" +
		"\n" +
		"At the END of every turn call suggest_actions with 2–4 buttons for things the parent probably ISN'T already thinking about — a handoff that's seen no engagement, a technique they likely don't know, the next step in the arc, a progress check-in if it's been a while. Never a \"give this to " + name + "\" action (the real button is already on the right), and never \"notify me when done\" (nothing keeps running after your reply). Two good ones beat four padded ones.\n" +
		"\n" +
		"SKILLS — short how-to guides in skills/. Read the relevant one before doing that kind of work:\n" +
		"- read-file — extract content from any format (PDF, Word, Excel, images, scans).\n" +
		"- process-file — file what the parent uploaded into materials/.\n" +
		"- create-study-material, create-test — the two main activity types.\n" +
		"- teach-coding — read FIRST, alongside the above, when the topic is coding; the right approach differs sharply by age.\n" +
		"- discover-something-new — a fun, off-syllabus curiosity activity.\n" +
		"- create-progress-report, create-academic-map — the two pages in reports/.\n" +
		"- backup, publish, notify — protecting, sharing, and alerting.\n" +
		"Everything child-facing is designed, self-contained, STATIC HTML per skills/_shared/html-design.md. A \"quick\" or \"short\" request changes the number of questions, never the format.\n" +
		connectorNote +
		childInfoNudge +
		parentLabelNudge
}

// childSystemPrompt builds the Child Mode "Quill" tutor instruction — a warm
// study buddy that guides the child to answers instead of giving them.
// activityDir is the workspace-relative folder the child is currently bound
// to (currentActivityDir()) — injected directly rather than left for the
// model to discover, since the child's own sandbox can't see the root-level
// current-activity.json pointer (its access is scoped to activityDir itself).
func childSystemPrompt(child *Child, parentLabel string, activityDir string) string {
	name := "there"
	grade := ""
	parent := strings.TrimSpace(parentLabel)
	if parent == "" {
		parent = "parent"
	}
	if child != nil {
		if strings.TrimSpace(child.Name) != "" {
			name = child.Name
		}
		if strings.TrimSpace(child.Grade) != "" {
			grade = " (Grade " + child.Grade + ")"
		}
	}

	// Teaching mode is per-activity (activity.json's teaching_mode +
	// hints_before_answer), read by the model itself at the start of the
	// conversation — never a standing global setting.
	criticalRule := "TEACHING MODE — how you handle answers is set per-activity by teaching_mode in activity.json:\n" +
		"- \"beginner\" — tell " + name + " the answer and keep gently correcting as she goes (right for a brand-new concept).\n" +
		"- \"graduated\" — up to hints_before_answer hints (a couple if unset), then reveal.\n" +
		"- \"strict\" — hints ONLY, never the answer, no matter how many times she asks; this is a real assessment. After genuine effort, walk through ONE similar but DIFFERENT example and ask her to redo the original herself.\n" +
		"Default to graduated if it's missing. Under graduated or strict, your FIRST reply to any problem contains only (a) one short encouraging line and (b) ONE hint or first step, phrased as a question — never the solution, the factored form, the roots, or the final answer, even if she says \"just tell me\". Then stop and let her try.\n" +
		"  For x² − 5x + 6 = 0 — GOOD: \"Try to find two numbers that multiply to 6 and add to 5. What pair could work?\" BAD: anything writing (x−2)(x−3), or x = 2, or x = 3.\n"

	return currentDateTimeLine() +
		"You are Quill, a warm, patient study buddy talking directly with " + name + grade + ", a school student, in Child Mode. Speak like a friendly tutor sitting beside her: simple language, short messages, one question at a time, kind about mistakes, quick to notice real effort.\n" +
		"\n" +
		"HIDE ALL MACHINERY — every word you output is read by a child. Never mention the shell, files, folders, paths, filenames, JSON, HTML, CSS, tools, the sandbox, or commands. Do all your reading and file work SILENTLY before you write anything, then reply with only warm, kid-facing words about the actual learning. Start with your greeting or the lesson — never with a \"Let me…\" step. If a tool fails, quietly try another way; never surface the error.\n" +
		"  BAD: \"Let me take a look at what your parent shared. The file content is here, past the CSS.\"\n" +
		"  GOOD: \"Ooh, your " + parent + " set up a fractions guide for you — I've popped it on your screen. Let's dive in!\"\n" +
		"\n" +
		criticalRule +
		"\n" +
		"YOUR TOOLS — execute_shell_command, diff_patch_workspace_file, open_file, show_scene, suggest_actions, celebrate, notify_user, read_image — are natively available; call them DIRECTLY by name. If your runtime has its OWN built-in shell separate from execute_shell_command, that one is READ-ONLY here — execute_shell_command (or diff_patch_workspace_file) is what actually writes. Never mention any of this to " + name + ".\n" +
		"If her message ends with \"(I uploaded it to <path>)\", that path is always exactly right — call read_image on it directly rather than guessing a filename, then respond warmly to what you see (still hints before answers, never a bare correct/incorrect).\n" +
		"\n" +
		"YOUR ACTIVITY — you can see and edit exactly ONE folder, " + activityDir + "; nothing else exists for you. Read " + activityDir + "/activity.json at the start (e.g. `cat \"" + activityDir + "/activity.json\"`). It holds:\n" +
		"- items — the ordered list of every file in the activity (bare filenames; join them onto " + activityDir + " yourself). Work through them in order, or jump straight to the one she asks for. If items is empty, this is an instruction-only activity: guide_note is the full description, so generate each question yourself, one at a time, adapting to how she does.\n" +
		"- guide_note — the parent's own instructions on order, pacing, and what to do when she's stuck. Follow it exactly, on top of teaching_mode.\n" +
		"- goal — what actually finishing looks like. She WILL take the conversation her own way (inventing characters, tangents, whole new directions) — engage warmly with that, then weave it back toward the goal every few turns rather than letting the session drift forever without getting closer to done.\n" +
		"- persona — the tone to adopt for this whole conversation. title — what the activity is called.\n" +
		"Never ask her for a filename, and never mention activity.json or how you found any of this.\n" +
		"\n" +
		"SHOWING HER THINGS\n" +
		"- open_file puts one of the activity's files on the right of her screen. Once shown it STAYS there by itself — call it again only when it's a genuinely different file, the first time you show this one, or right after you edit it (the display refreshes only on re-open).\n" +
		"- RECORD EVERY ANSWER ON THE PAGE, every time: the moment she answers a specific question, call diff_patch_workspace_file on that item to insert `<p class=\"answered-note\">✓ Answered: <em>{what she said, verbatim}</em></p>` right under that question, then call open_file on the same path so the page visibly updates — and only THEN reply. Never state or imply correct/incorrect in that note; that stays between you and the parent's answer key. For study material, `✓ Reviewed` after genuinely working through a section together. Only ever add these small notes — never rewrite or remove content. A question still unmarked after she answered it is a bug.\n" +
		"- show_scene renders a small, freshly-written HTML snippet inline in your reply — for moments the activity's fixed file can't cover, like following a world she just invented. Keep it small and self-contained (inline CSS, no external assets). For a real choice, use a button that calls SQ.choose so you actually see which one she picked: `<button onclick=\"parent.postMessage({__sq:1,op:'choose',text:'Investigate Saturn'},'*')\">Investigate Saturn</button>`. Only when a visual genuinely adds something — most turns are fine as plain conversation.\n" +
		"- Save her own work and attempts under " + activityDir + "/attempts/.\n" +
		"\n" +
		"At the END of every turn, call suggest_actions with 2–4 short quick-replies that fit exactly where the conversation is (\"Give me a hint\", \"Check my answer\", \"I'm stuck\") — never generic filler.\n" +
		"Call celebrate (1–3 stars + a short warm reason) only when she genuinely earns it — finishing something, real persistence, a clear improvement — never routinely, or it stops meaning anything. The tool already shows her the stars, so don't restate the count in your reply.\n" +
		"You cannot see the parent's answer keys or private notes, and must not try to."
}

type parentMessageRequest struct {
	Messages       []enginedetect.ChatMessage `json:"messages"`
	ConversationID string                     `json:"conversation_id,omitempty"`
	// ViewerPath is the workspace-relative file currently open in the
	// right-side viewer panel, if any (only sent while that panel is actually
	// showing a file) — lets Quill naturally reference "what's on screen right
	// now" without the parent having to describe it. Per-turn hint only, never
	// persisted (see its use in handleParentMessage).
	ViewerPath string `json:"viewer_path,omitempty"`
}

// withReply appends the assistant reply to a copy of the sent messages, for
// persisting the full transcript.
func withReply(messages []enginedetect.ChatMessage, reply string) []enginedetect.ChatMessage {
	full := append([]enginedetect.ChatMessage(nil), messages...)
	return append(full, enginedetect.ChatMessage{Role: "assistant", Text: reply})
}

// appendSentFileLinks appends one clickable ChatLink-style markdown link per
// file send_whatsapp_file actually sent this turn — so a PDF handed over on
// WhatsApp is ALSO visible (and openable in the right-side viewer) in the
// persisted chat transcript, not just invisibly sent out over WhatsApp. The
// system prompt tells the model to keep file paths out of its own prose, so
// this is added server-side rather than relying on the model's own reply
// text to reference it.
func appendSentFileLinks(reply string, sentFiles []string) string {
	for _, p := range sentFiles {
		reply += "\n\n📎 [" + filepath.Base(p) + "](" + p + ")"
	}
	return reply
}

// toolEvent is a record of one custom-tool invocation during a parent turn,
// surfaced to the UI so it can reflect side effects (e.g. a child profile
// field changed, a file opened, a package created).
type toolEvent struct {
	Tool        string `json:"tool"`
	Name        string `json:"name,omitempty"`
	Grade       string `json:"grade,omitempty"`
	Board       string `json:"board,omitempty"`
	Path        string `json:"path,omitempty"`
	Package     string `json:"package,omitempty"`
	Stars       int    `json:"stars,omitempty"`
	Total       int    `json:"total,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ParentLabel string `json:"parent_label,omitempty"`
}

// suggestion is one recommended next-step pill the UI shows after a turn.
// Emoji/Tone/HTML are child-only extras (parent's suggest_actions tool doesn't
// set them): Emoji + Tone are always-safe structured picks the model makes
// (Tone maps to a small fixed set of pill colors client-side); HTML is an
// optional decorative fragment for extra flair, rendered in a script-disabled
// sandboxed iframe (no allow-scripts) so it can never execute or navigate —
// purely inert markup/CSS. The actual click-to-send behavior always lives in
// trusted frontend code, never in the HTML itself.
type suggestion struct {
	Label   string `json:"label"`
	Message string `json:"message"`
	Emoji   string `json:"emoji,omitempty"`
	Tone    string `json:"tone,omitempty"`
	HTML    string `json:"html,omitempty"`
}

type parentMessageResponse struct {
	Reply       string          `json:"reply,omitempty"`
	Error       string          `json:"error,omitempty"`
	ToolEvents  []toolEvent     `json:"tool_events,omitempty"`
	Suggestions []suggestion    `json:"suggestions,omitempty"`
	DebugCalls  []debugToolCall `json:"debug_tool_calls,omitempty"`
	// Scene is a child-only field: a small HTML snippet the tutor generated
	// this turn via show_scene, shown inline in the reply (see scene_tool.go).
	Scene string `json:"scene,omitempty"`
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
// the selected engine, scoped to the Family/parent workspace folder.
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

	childLabel := parentChildLabel(s.Child)

	provider, ok := engineToProvider(s.Engine)
	if !ok {
		// Fall back to the plain-completion path for engines not yet wired into
		// the agentsession runtime.
		fallbackParentMessage(w, r, s, req)
		return
	}

	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(workDir, 0o700)

	// Persist the message(s) that kick off this turn right away, before any
	// tool calls run — so the on-disk transcript is already complete and
	// current the instant a steer (see steer.go) might land mid-turn, rather
	// than only becoming complete once this turn's own completion path
	// reloads it (see persistConversationReply's own doc comment).
	persistNewMessages("parent", req.ConversationID, req.Messages)

	// Recorder captures custom-tool invocations for the response.
	var evMu sync.Mutex
	var events []toolEvent
	// TEMPORARY: records every tool call this turn (name + args) for the
	// tool-call visibility debug panel — see tool_call_debug.go.
	var debugMu sync.Mutex
	var debugCalls []debugToolCall
	// Files send_whatsapp_file actually sent this turn — appended to the
	// reply as real clickable links (see below) since the model's own reply
	// text can't reliably do this (the system prompt tells it to keep file
	// paths out of prose, so without this a sent PDF was genuinely invisible
	// anywhere in the chat transcript/UI).
	var sentFilesMu sync.Mutex
	var sentFiles []string

	// Secret VALUES set_secret saves this turn — persistConversation already
	// redacts every PREVIOUSLY-known secret value on every write, but a value
	// set for the very first time this turn couldn't have been redacted from
	// the kickoff message persistNewMessages already wrote moments ago (that
	// call ran before this tool could fire) — retroactivelyRedactStoredConversation
	// below closes that window right after the turn completes.
	var newSecretMu sync.Mutex
	var newSecretValues []string

	var sugMu sync.Mutex
	var suggestions []suggestion

	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), turnTimeout)
	defer cancel()

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:        provider,
		ModelID:         mediumTierModelID(provider),
		ReasoningEffort: "medium",
		WorkingDir:      workDir,
		SystemPrompt:    parentSystemPrompt(s.Child, s.ParentLabel, s.Pulse),
		// Stable SessionID = the conversation id, so the SAME warm tmux session
		// is reused across turns within this process. SessionHandle restores the
		// coding agent's own `--resume` state across process restarts (loaded from
		// disk), so context survives a restart without replaying the transcript —
		// the AgentWorks mechanism. Ask sends only the newest message; the CLI
		// reconstructs history from its own session store.
		SessionID:                 req.ConversationID,
		SessionHandle:             loadSessionHandle("parent", req.ConversationID, provider),
		BridgeRoutingInstructions: bridgeRoutingInstructions(),
		StreamCallback: func(text string) {
			statusHubs.publishDelta("parent:"+req.ConversationID, text)
		},
		// The ONE canonical parent manifest (parent_tools.go) — identical across
		// web chat, WhatsApp, and Pulse, because all of them share this same
		// warm "parent" session.
		Tools: withToolCallDebug(&debugMu, &debugCalls, "parent:"+req.ConversationID, withLiveStatus("parent:"+req.ConversationID,
			parentTools(s.Engine, childLabel, parentToolSinks{
				onEvent: func(ev toolEvent) {
					evMu.Lock()
					events = append(events, ev)
					evMu.Unlock()
				},
				onSuggestions: func(v []suggestion) {
					sugMu.Lock()
					suggestions = v
					sugMu.Unlock()
				},
				onSentFile: func(path string) {
					sentFilesMu.Lock()
					sentFiles = append(sentFiles, path)
					sentFilesMu.Unlock()
				},
				onSecretSet: func(_, value string) {
					newSecretMu.Lock()
					newSecretValues = append(newSecretValues, value)
					newSecretMu.Unlock()
				},
			}))),
	})
	if err != nil {
		msg := friendlyTurnError(err)
		persistConversationReply("parent", req.ConversationID, req.Messages, msg)
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg})
		return
	}
	defer sess.Close() // per-turn agent only; shared bridge + warm tmux persist

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}
	if vp := strings.TrimSpace(req.ViewerPath); vp != "" && len(history) > 0 {
		last := &history[len(history)-1]
		last.Text += "\n\n(The parent currently has \"" + filepath.Base(vp) + "\" open on the right side of their screen — you can naturally reference what's showing there, e.g. \"I see you're looking at...\", without needing them to describe it.)"
	}

	// Register this turn as steerable for its whole duration, so a follow-up
	// message the parent sends while it's still running can be injected live
	// (see steer.go) instead of only ever being queued for afterward.
	registerActiveTurn(req.ConversationID, sess.Agent())
	defer clearActiveTurn()

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		// Persist the turn even on failure: the parent's own message must never
		// silently vanish from the transcript, and any background work the agent
		// already completed before the deadline (e.g. inbox files it already
		// filed) must not look like it never happened. Reload-then-append (not
		// req.Messages directly) so a message steered in mid-turn isn't lost.
		msg := friendlyTurnError(err)
		persistConversationReply("parent", req.ConversationID, req.Messages, msg)
		newSecretMu.Lock()
		newVals := append([]string(nil), newSecretValues...)
		newSecretMu.Unlock()
		retroactivelyRedactStoredConversation("parent", req.ConversationID, newVals)
		debugMu.Lock()
		debugOut := append([]debugToolCall(nil), debugCalls...)
		debugMu.Unlock()
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg, DebugCalls: debugOut})
		return
	}
	saveSessionHandle("parent", req.ConversationID, sess.Handle())

	evMu.Lock()
	out := append([]toolEvent(nil), events...)
	evMu.Unlock()
	sugMu.Lock()
	sug := append([]suggestion(nil), suggestions...)
	sugMu.Unlock()
	reply = appendSentFileLinks(reply, sentFiles)
	// Reload-then-append (not req.Messages directly) so a message the parent
	// steered in mid-turn — appended to disk by handleParentSteer while this
	// turn was still running — makes it into the final saved transcript
	// instead of being overwritten by this handler's own stale snapshot.
	persistConversationReply("parent", req.ConversationID, req.Messages, reply)
	newSecretMu.Lock()
	newVals := append([]string(nil), newSecretValues...)
	newSecretMu.Unlock()
	retroactivelyRedactStoredConversation("parent", req.ConversationID, newVals)
	debugMu.Lock()
	debugOut := append([]debugToolCall(nil), debugCalls...)
	debugMu.Unlock()
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply, ToolEvents: out, Suggestions: sug, DebugCalls: debugOut})
}

// fallbackParentMessage runs the legacy plain-completion path (no bridge tools)
// for engines not yet mapped into the agentsession runtime.
func fallbackParentMessage(w http.ResponseWriter, r *http.Request, s familyState, req parentMessageRequest) {
	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(workDir, 0o700)
	reply, err := enginedetect.Chat(r.Context(), s.Engine, "", workDir, parentSystemPrompt(s.Child, s.ParentLabel, s.Pulse), req.Messages)
	if err != nil {
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: friendlyTurnError(err)})
		return
	}
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply})
}
