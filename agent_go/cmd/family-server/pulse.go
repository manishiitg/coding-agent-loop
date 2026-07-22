package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
	"github.com/manishiitg/mcpagent/llm"
)

// appendPulseTurn appends a Pulse check-in as a real, visible two-message
// turn — a parent-facing trigger line (what the check-in looked at) followed
// by Quill's reply — both tagged Source:"pulse" so the UI marks them as an
// automated check-in. Nothing about Pulse is hidden: the parent sees the
// check-in happen and its result, exactly like a normal chat turn. The
// trigger persisted here is the CLEAN parent-facing line (pulseCheck.trigger),
// NOT the raw technical instruction sent to the model — showing the parent
// file paths/skill names would break the "parent is not technical, hide the
// machinery" rule the whole app follows.
func appendPulseTurn(messages []enginedetect.ChatMessage, trigger, reply string) []enginedetect.ChatMessage {
	full := append([]enginedetect.ChatMessage(nil), messages...)
	full = append(full, enginedetect.ChatMessage{Role: "user", Text: trigger, Source: "pulse"})
	full = append(full, enginedetect.ChatMessage{Role: "assistant", Text: reply, Source: "pulse"})
	return full
}

// pulseCheck is one focused Pulse sub-check. Pulse runs each enabled check as
// its OWN agent turn producing its own visible message (a parent-facing
// trigger divider + Quill's reply), rather than one combined message doing
// everything at once — so the parent sees distinct, legible check-ins
// (learning, school portal, school email, memory) instead of one dense blob.
type pulseCheck struct {
	trigger     string // clean parent-facing divider line (no file paths)
	instruction string // full technical instruction sent to the model
	tools       func(engine string) []agentsession.Tool
}

// pulseReplyRules is the shared tail every check's instruction ends with, so
// each focused turn produces one short, honest, parent-appropriate message.
const pulseReplyRules = " If nothing here has meaningfully changed since your last check-in visible earlier in this conversation, " +
	"say so briefly and warmly — do not manufacture busywork or repeat something you already told the parent. " +
	"Write ONE short, warm message as your reply, following your usual rules (parent is not technical, no files/paths/JSON, plain language)."

// pulseChecks returns the ordered set of check-ins to run this cycle — the
// learning review and the memory update always run; the school portal and
// school email checks only run when the parent has configured them.
func pulseChecks(s familyState) []pulseCheck {
	who := "the child"
	if s.Child != nil && strings.TrimSpace(s.Child.Name) != "" {
		who = s.Child.Name
	}
	engine := s.Engine

	checks := []pulseCheck{{
		trigger: "Automated check-in — reviewing recent learning activity",
		instruction: "This is an automated Pulse check-in — the parent did not just ask you anything; you're reviewing on your own " +
			"initiative, focused ONLY on " + who + "'s recent learning activity this turn (ignore email and the school portal — those are " +
			"separate check-ins). Look at what's actually changed since your last check (recent conversations, child/attempts/, test results, " +
			"uploaded materials, child/conversations/). If there's real new evidence, rebuild shared/academic-map.html and/or " +
			"shared/reports/progress.html per their skill files (skills/create-academic-map/SKILL.md, skills/create-progress-report/SKILL.md) — " +
			"both are fully-regenerated current-state snapshots, never a dated log; replace the whole file with a fresh picture. If a clear gap " +
			"or opportunity stands out (a weak topic, something " + who + " hasn't practiced in a while, a natural next step), you may prepare " +
			"study material or a test (skills/create-study-material/SKILL.md, skills/create-test/SKILL.md) — but do NOT call approve_for_child; " +
			"nothing gets handed to " + who + " without the parent explicitly asking, so just mention what you made." + pulseReplyRules,
		tools: func(engine string) []agentsession.Tool {
			return []agentsession.Tool{
				webSearchTool(), readImageTool(engine), generateImageTool(), notifyTool(),
				shellTool(), diffPatchWorkspaceFileTool(), createLearningPackageTool(func(toolEvent) {}),
			}
		},
	}}

	if sites := s.Pulse.Sites(); len(sites) > 0 {
		list := strings.Join(sites, ", ")
		checks = append(checks, pulseCheck{
			trigger: "Automated check-in — checking your saved websites",
			instruction: "This is an automated Pulse check-in, focused ONLY on the website(s) the parent asked you to keep an eye on: " + list + ". " +
				"Use agent_browser (it reuses the parent's own signed-in browser). FIRST run tab list to see all open tabs — the parent's browser " +
				"usually has many, and the active one is often unrelated (a work site, etc.); find the tab(s) that match the site(s) above and switch " +
				"to each, or open the URL yourself if it isn't already a tab. NEVER read or act on an unrelated tab just because it's in front. " +
				"Then explore each of the parent's site(s) THOROUGHLY. These can be a school portal, a class website, or any third-party site — a school portal for instance " +
				"usually has a lot: assignments/homework, due dates, uploaded books/materials/handouts, grades or graded work, teacher " +
				"announcements/notices, timetable or calendar changes, messages, attendance. Don't stop at the first page: navigate into the main " +
				"sections (snapshot the page, follow the obvious links) and gather as much concrete detail as you can — specific item names, due " +
				"dates, topics, anything new or relevant to " + who + ". When a site has actual resource FILES worth keeping — worksheets, notes, " +
				"PDFs, images, handouts, question papers — download them: clicking a download link puts the file in the parent's Downloads folder on " +
				"this computer, so then use execute_shell_command to copy it into shared/materials/<subject>/<topic>/ (e.g. " +
				"`cp ~/Downloads/<file> shared/materials/...`) so it becomes part of " + who + "'s workspace. Then INGEST what you saved the same way an " +
				"uploaded file is handled: for an image call read_image to see what it actually is; for a PDF or document, read/convert it with your " +
				"shell tools to pull out the real content; follow skills/process-file/SKILL.md to file it properly (right subject/topic, a short " +
				"summary). The goal is that a useful resource on a site ends up usable INSIDE SparkQuill, not just noticed. Then tell the parent " +
				"plainly what's actually new across the site(s) and what matters for " + who + " — be specific (names, dates, what you pulled in), not " +
				"vague. If a site needs a login you can't get past, say it needs them to sign in first (via the Browser connector) rather than " +
				"guessing." + pulseReplyRules,
			tools: func(engine string) []agentsession.Tool {
				return []agentsession.Tool{agentBrowserTool(), readImageTool(engine), shellTool(), diffPatchWorkspaceFileTool(), notifyTool()}
			},
		})
	}

	if q := strings.TrimSpace(s.Pulse.SchoolGmailQuery); q != "" {
		checks = append(checks, pulseCheck{
			trigger: "Automated check-in — checking school email",
			instruction: "This is an automated Pulse check-in, focused ONLY on school email. The parent configured this filter: \"" + q + "\". " +
				"Use execute_shell_command with the gws CLI (see your system instructions for the exact command shape) to check for anything " +
				"new WITHIN that filter — never widen it to their whole inbox. If there's a relevant new email (a notice, a deadline, something " +
				"about " + who + "), summarize it plainly for the parent." + pulseReplyRules,
			tools: func(engine string) []agentsession.Tool {
				return []agentsession.Tool{shellTool(), notifyTool()}
			},
		})
	}

	checks = append(checks, pulseCheck{
		trigger: "Automated check-in — updating what I remember about your preferences",
		instruction: "This is an automated Pulse check-in, focused ONLY on your working memory of the parent's preferences. Read " +
			"skills/update-preferences/SKILL.md and follow it: check parent/preferences.md against what the parent has actually said across " +
			"parent/conversations/, and update it in place if there's something durable worth remembering (exam dates, scheduling/behavioral " +
			"preferences, content preferences) that isn't already captured. Tell the parent in one short line what (if anything) you noted." + pulseReplyRules,
		tools: func(engine string) []agentsession.Tool {
			return []agentsession.Tool{shellTool(), diffPatchWorkspaceFileTool(), notifyTool()}
		},
	})

	_ = engine
	return checks
}

// Pulse is SparkQuill's version of AgentWorks' Pulse feature (see the design
// discussion this was built from): a periodic, opt-in check-in that reviews
// recent learning activity and keeps shared/academic-map.html and
// shared/reports/progress.html current, proposing new study material where a
// gap shows up. Deliberately much simpler than AgentWorks' multi-module
// Gate/Reviewer/Fixer machinery (that exists because ONE workflow run can
// touch ten disparate concern types with no natural home in its own output
// files) — here there's exactly one output that matters and it already has a
// durable, dated, human-readable home. No separate Pulse log file either
// (see the "why do we need improve.html" conversation): findings are written
// straight into the existing academic map/progress report, and the
// parent-facing narrative goes into their own ongoing chat — nothing needs a
// second log that just restates what's already visible elsewhere.
//
// Crucially, Pulse does NOT run in its own session/thread: it checks in on the
// single parent conversation (parentConversationID) that web chat, WhatsApp, and
// Pulse all share — so a check-in reads like Quill following up in the same
// conversation the parent already has open, not a separate channel they'd have
// to remember to check.
const pulseTickInterval = 5 * time.Minute

// runPulseOnce runs one Pulse cycle. When force is false (the periodic
// ticker's normal call), it's a no-op unless Pulse is actually enabled —
// when force is true (a manual "run now" trigger), it runs regardless of the
// enabled toggle, since testing it shouldn't require turning on the
// recurring schedule first.
//
// A cycle runs each check in pulseChecks(s) as its OWN sequential agent turn,
// persisting each as its own visible message before moving to the next — so
// the parent sees distinct check-ins, and if the process dies mid-cycle the
// checks that already completed are still saved.
func runPulseOnce(ctx context.Context, force bool) error {
	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	if !force && !s.Pulse.Enabled {
		return nil
	}
	if s.Engine == "" {
		return fmt.Errorf("no learning engine selected")
	}
	if s.Child == nil {
		return fmt.Errorf("no child profile set up yet")
	}
	provider, ok := engineToProvider(s.Engine)
	if !ok {
		return fmt.Errorf("engine %q has no provider mapping", s.Engine)
	}

	// Pulse checks in on the SINGLE parent conversation (same file + warm tmux
	// session as the web chat and WhatsApp) — one unified thread, not a separate
	// Pulse channel.
	convID := parentConversationID
	existing, _ := loadStoredConversation("parent", convID)

	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	messages := existing.Messages
	var replies []string
	for _, c := range pulseChecks(s) {
		if err := ctx.Err(); err != nil {
			return err
		}
		reply, err := runPulseCheckTurn(ctx, provider, s, convID, messages, c)
		if err != nil {
			return fmt.Errorf("%q check failed: %w", c.trigger, err)
		}
		// Persist each check as its own visible turn immediately, so a later
		// check failing (or the process dying) doesn't lose the ones already done.
		messages = appendPulseTurn(messages, c.trigger, reply)
		persistConversation("parent", convID, messages)
		replies = append(replies, reply)
	}

	// ONE consolidated notification for the whole cycle — the full per-check
	// detail already lives in the chat; this is the single "ping the parent's
	// phone/email" digest, following AgentWorks' Pulse pattern (one notify at
	// the end, not one per check — per-check pushes would spam WhatsApp/email).
	// deliverNotification fans out to desktop + WhatsApp + Gmail, whichever are
	// set up.
	childName := s.Child.Name
	if childName == "" {
		childName = "your child"
	}
	digest := strings.TrimSpace(strings.Join(replies, "\n\n"))
	if digest == "" {
		digest = "I checked in on " + childName + "'s learning — nothing new to flag right now."
	}
	res := deliverNotification(context.Background(), "SparkQuill — check-in on "+childName, digest)
	log.Printf("[pulse] notification: status=%s delivered=%v failed=%v", res.Status, res.Delivered, res.Failed)

	stateMu.Lock()
	cur := loadState()
	cur.Pulse.LastRunAt = time.Now().UTC().Format(time.RFC3339)
	_ = saveState(cur)
	stateMu.Unlock()
	return nil
}

// runPulseCheckTurn runs one focused check as a single agent turn over the
// accumulated conversation so far (so it sees prior checks this cycle and
// won't repeat them), returning Quill's reply. The technical instruction is
// sent to the model but never persisted — only the clean trigger + reply are
// (see appendPulseTurn).
func runPulseCheckTurn(ctx context.Context, provider llm.Provider, s familyState, convID string, messages []enginedetect.ChatMessage, c pulseCheck) (string, error) {
	history := make([]agentsession.Message, 0, len(messages)+1)
	for _, m := range messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}
	history = append(history, agentsession.Message{Role: "user", Text: c.instruction})

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:                  provider,
		ModelID:                   mediumTierModelID(provider),
		WorkingDir:                filepath.Join(familyDataDir(), "workspace"),
		SystemPrompt:              parentSystemPrompt(s.Child, s.ParentLabel, s.Pulse),
		SessionID:                 convID,
		SessionHandle:             loadSessionHandle("parent", convID),
		BridgeRoutingInstructions: bridgeRoutingInstructions(),
		Tools:                     withLiveStatus("pulse:"+convID, c.tools(s.Engine)),
	})
	if err != nil {
		return "", fmt.Errorf("session setup failed: %w", err)
	}
	defer sess.Close()

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		return "", err
	}
	saveSessionHandle("parent", convID, sess.Handle())
	return reply, nil
}

// startPulseTicker runs the periodic check forever until ctx is canceled.
// A plain wall-clock ticker is enough at this scale — no cron parser needed,
// since the only knob is "every N hours," configured via /api/pulse/config.
func startPulseTicker(ctx context.Context) {
	ticker := time.NewTicker(pulseTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stateMu.Lock()
			s := loadState()
			stateMu.Unlock()
			if !s.Pulse.Enabled {
				continue
			}
			due := true
			if s.Pulse.LastRunAt != "" {
				if last, err := time.Parse(time.RFC3339, s.Pulse.LastRunAt); err == nil {
					due = time.Since(last) >= s.Pulse.cadence()
				}
			}
			if !due {
				continue
			}
			runCtx, cancel := context.WithTimeout(context.Background(), turnTimeout)
			if err := runPulseOnce(runCtx, false); err != nil {
				log.Printf("[pulse] scheduled run failed: %v", err)
			}
			cancel()
		}
	}
}

// --- HTTP routes ---------------------------------------------------------

type pulseConfigResponse struct {
	Enabled          bool     `json:"enabled"`
	CadenceHours     int      `json:"cadence_hours"`
	LastRunAt        string   `json:"last_run_at,omitempty"`
	SchoolGmailQuery string   `json:"school_gmail_query,omitempty"`
	WatchSites       []string `json:"watch_sites,omitempty"`
	NotifyEmails     []string `json:"notify_emails,omitempty"`
}

func pulseConfigResponseFrom(p PulseConfig) pulseConfigResponse {
	hours := p.CadenceHours
	if hours <= 0 {
		hours = 24
	}
	return pulseConfigResponse{Enabled: p.Enabled, CadenceHours: hours, LastRunAt: p.LastRunAt, SchoolGmailQuery: p.SchoolGmailQuery, WatchSites: p.Sites(), NotifyEmails: p.NotifyEmails}
}

// pulseRunMu prevents two manual "run now" triggers overlapping — a real
// Pulse turn already serializes on agentTurnMu once it starts, but this stops
// a second HTTP call from spawning a redundant goroutine that would just
// block, and lets the handler tell the parent plainly "already running"
// instead of silently queuing.
var pulseRunMu sync.Mutex
var pulseRunning bool

// POST /api/pulse/run — manual "run now" trigger (e.g. from the Pulse
// popover, to test it without waiting for the ticker or turning on the
// recurring schedule). Runs in the background; the caller polls
// GET /api/pulse/config and watches last_run_at change to know it's done.
func handlePulseRunNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pulseRunMu.Lock()
	if pulseRunning {
		pulseRunMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a Pulse check-in is already running"})
		return
	}
	pulseRunning = true
	pulseRunMu.Unlock()

	go func() {
		defer func() {
			pulseRunMu.Lock()
			pulseRunning = false
			pulseRunMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
		defer cancel()
		if err := runPulseOnce(ctx, true); err != nil {
			log.Printf("[pulse] manual run failed: %v", err)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// GET /api/pulse/config
func handleGetPulseConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	writeJSON(w, http.StatusOK, pulseConfigResponseFrom(s.Pulse))
}

type setPulseConfigRequest struct {
	Enabled          *bool     `json:"enabled,omitempty"`
	CadenceHours     *int      `json:"cadence_hours,omitempty"`
	SchoolGmailQuery *string   `json:"school_gmail_query,omitempty"`
	WatchSites       *[]string `json:"watch_sites,omitempty"`
	NotifyEmails     *[]string `json:"notify_emails,omitempty"`
}

// POST /api/pulse/config — partial update; only provided fields change.
func handleSetPulseConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req setPulseConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	stateMu.Lock()
	s := loadState()
	if req.Enabled != nil {
		s.Pulse.Enabled = *req.Enabled
	}
	if req.CadenceHours != nil && *req.CadenceHours > 0 {
		s.Pulse.CadenceHours = *req.CadenceHours
	}
	if req.SchoolGmailQuery != nil {
		s.Pulse.SchoolGmailQuery = strings.TrimSpace(*req.SchoolGmailQuery)
	}
	if req.WatchSites != nil {
		cleaned := make([]string, 0, len(*req.WatchSites))
		for _, u := range *req.WatchSites {
			if u = strings.TrimSpace(u); u != "" {
				cleaned = append(cleaned, u)
			}
		}
		s.Pulse.WatchSites = cleaned
		s.Pulse.SchoolPortalURL = "" // fully replaced by the generic list; drop the legacy single value
	}
	if req.NotifyEmails != nil {
		cleaned := make([]string, 0, len(*req.NotifyEmails))
		for _, e := range *req.NotifyEmails {
			if e = strings.TrimSpace(e); e != "" {
				cleaned = append(cleaned, e)
			}
		}
		s.Pulse.NotifyEmails = cleaned
	}
	err := saveState(s)
	stateMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pulseConfigResponseFrom(s.Pulse))
}
