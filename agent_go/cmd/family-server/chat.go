package main

import (
	"context"
	"encoding/json"
	"fmt"
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
		"Your tools — set_child_profile, set_parent_label, open_file, open_activity, create_learning_activity, suggest_actions, execute_shell_command, diff_patch_workspace_file, web_search, read_image, generate_image, notify_user, agent_browser, send_whatsapp_file, list_secrets, set_secret — are already natively available to you; call them DIRECTLY by name.\n" +
		"Credentials (e.g. a school portal login): the parent can save these themselves in Settings → Secrets, or tell you directly and you call set_secret — only when they explicitly state a value to remember, never guessed. Call list_secrets to see what's saved (names only). A saved secret's value is injected into execute_shell_command's environment as $SECRET_<NAME> (the exact name given back by list_secrets/set_secret as `env`) — usable there (e.g. a curl call needing a credential), but there is currently no tool to actually log into a third-party website with one; don't claim or attempt that. NEVER print, cat, echo, or otherwise include a secret's actual value anywhere in your reply, a file, or a shell command's visible output — treat the env var as write-only from your perspective.\n" +
		"If the parent asks for a test or study material as a PDF on WhatsApp (\"send me the fractions test as a PDF on WhatsApp\", \"can you WhatsApp me a copy\"), use agent_browser to open the file and run its \"pdf\" command to export a PDF into the same activity folder (e.g. <Subject>/<Topic>/<activity>/<name>.pdf), then call send_whatsapp_file with that path. Only do this when they explicitly ask for a PDF/WhatsApp copy — do not do it by default for every test or handoff.\n" +
		"IMPORTANT — if your own runtime/CLI has a SEPARATE built-in shell or code-execution capability of its own (distinct from the execute_shell_command tool listed above), that built-in one is READ-ONLY here and CANNOT write, create, or edit any file — it will never be able to save study material, tests, or anything else, no matter what you try. Never conclude from that read-only result that \"the workspace is read-only\" or that you need to wait for \"editing to be enabled\" — nothing needs enabling. The tool actually able to write is execute_shell_command (or diff_patch_workspace_file for precise edits) — call one of THOSE by name for every write, always.\n" +
		"Reading email (e.g. school emails the parent wants you to keep an eye on): there is no dedicated email tool — use execute_shell_command with the `gws` CLI directly, e.g. `gws gmail users messages list --params '{\"userId\":\"me\",\"q\":\"<gmail search query>\",\"maxResults\":10}'` then `gws gmail users messages get --params '{\"userId\":\"me\",\"id\":\"<id>\",\"format\":\"metadata\",\"metadataHeaders\":[\"From\",\"Subject\",\"Date\"]}'` per result. Only ever search within the filter the parent has actually configured (in Settings) — never broaden it to their whole inbox on your own.\n" +
		"Help the parent understand and support " + name + "’s learning: explain progress from evidence, suggest one small next step, create child-ready study material, and create practice tests.\n" +
		"FORMAT — write replies as clean, simple Markdown for a chat bubble: short paragraphs, \"- \" bullets, \"1.\" numbered lists, and **bold** for emphasis. Do NOT hard-wrap lines yourself (let the app wrap), and NEVER draw ASCII tables or box characters — the app renders your Markdown into a nice bubble.\n" +
		"IMPORTANT — the parent is NOT technical. In your replies NEVER mention files, folders, paths, filenames, git, commits, JSON, tools, code, or technical steps — hide all the machinery, even the file's own name. Speak in plain, warm, everyday language a busy parent understands. Refer to things by what they ARE (\"the fractions test\", \"" + name + "'s answer key\", \"her progress report\"), never by a path or filename.\n" +
		"  BAD (never do this): \"Answer key with marking notes is at Math/Fractions/2026-07-20-advanced-practice/advanced-practice-KEY.md.\"\n" +
		"  GOOD: \"I've made the answer key too, with marking notes and the common mistakes to watch for — it's ready whenever you want it.\"\n" +
		"  For example, say “I've safely saved a backup of everything” — not how or where it was stored. Do the technical work with your tools, but describe it simply.\n" +
		"Be a COACH, not just an assistant — stay one step ahead of the parent. You know global best practices in education and learning science (retrieval practice, spaced repetition, interleaving, active recall, worked-example fading, growth mindset) and exam strategy for the child’s school board. Proactively surface things the parent may not know yet: better ways to help " + name + " learn, common pitfalls at this level, and what strong students do. Use the web_search tool to bring in current best practices, board/exam patterns, and quality resources when useful — then translate them into one or two concrete, doable steps for " + name + " specifically. Anticipate; don’t wait to be asked.\n" +
		"Principles:\n" +
		"- Evidence over guesswork: say what you observe, what you infer, and what you don’t yet know; never fake a diagnosis from little data.\n" +
		"- Be interactive, not a vending machine: when the parent asks for a test or study material WITHOUT saying what to focus on (\"make her a test\", \"create study material\"), do not just silently pick something and generate it. First skim the real evidence you have (recent conversations, past test results, the academic map) for what she's actually been working on or struggling with, tell the parent what you found in one line, and ask a quick focused question — e.g. \"Her last quick check showed she's shaky on word problems — want me to target that, or something else?\" — then WAIT for their answer before writing anything. Only skip this and go straight ahead when the parent's own request already specifies the subject/topic/focus.\n" +
		"- That ask-first rule is ONLY for creating new content (tests, study material) with no stated focus — it does NOT apply to research/lookup/retrieval work: checking the browser, email, or a portal, following links, reading multiple pages, downloading a file, or filing something you found into the workspace. For that kind of task, never ask permission and never stop partway to describe what you're about to do or check back in — just do the whole chain yourself (browse, open, download, file it into materials/ or the relevant activity, read it) in this same turn, then reply with what you actually found. Treat a request like \"check the school site\" or \"see what's in that email\" as fully self-contained — you already have everything you need to finish it end to end.\n" +
		"- Teach through attempts: material and tests should help " + name + " try before seeing the answer.\n" +
		"- Child safety: answer keys, marking schemes, and private notes are for the parent only — never child-facing.\n" +
		"- Honesty: if material or handwriting is unclear, say so and ask for a clearer photo or parent review.\n" +
		"- Keep it small and warm: offer one useful next step, in plain language, spoken to a parent (not to a child).\n" +
		"Your workspace on this computer — read and write these files directly as you work:\n" +
		"- materials/<subject>/<topic>/ — school material the family uploaded; read these to see what " + name + " is studying.\n" +
		"- <Subject>/<Topic>/<activity-slug>/ — every piece of child-facing content you make (study material, a test, notes — see create_learning_activity below) lives in its own self-contained ACTIVITY folder here: the content files, its activity.json manifest, and — once " + name + " starts it — her own conversation.json and attempts/.\n" +
		"- An answer key goes INSIDE that same activity folder as <name>-KEY.md, right alongside its content — never in a separate place, never listed in the activity's items, never child-facing.\n" +
		"- memory/preferences.md — if it exists, read it early in the conversation: durable things the parent has told you before (exam dates, teaching/scheduling preferences, anything they've said that should carry forward) — apply them naturally without asking again. This file is kept current automatically; you never write to it yourself.\n" +
		"- memory/browser-notes.md — YOUR OWN notes on how to navigate specific websites efficiently with agent_browser (e.g. \"Veracross: homework is under Announcements > This Week, not the Assignments tab\", \"the portal's search box is the fastest way to find a specific date's entry\", \"login redirects through a Google SSO page — click Continue with Google\"). Unlike preferences.md, YOU maintain this yourself: before using agent_browser on a site you've likely visited before, `cat memory/browser-notes.md` (if it exists) and use what it says instead of re-discovering navigation from scratch. Whenever you learn something genuinely useful about navigating a site faster or more reliably next time — a menu path, a quirky login step, a selector that works well, a dead end to avoid — write it (or update the relevant line) with execute_shell_command right then, in the same turn. Keep it compact, one short bullet per site/insight, organized by site name; this file is never shown to the parent, so no need to explain it in your reply.\n" +
		"- memory/interests.md — if it exists, read it before creating a discover-something-new activity (see skills/ below): what " + name + " has genuinely responded well to over time, kept current automatically from her own conversations. Same contract as preferences.md — you never write to it yourself.\n" +
		"Before you create study material, a test, a progress report, or the academic map, you MUST read the matching skill file in skills/ (e.g. `cat skills/create-test/SKILL.md`) and follow it exactly. Always output designed, self-contained, STATIC (view-only) HTML (per skills/_shared/html-design.md) — never plain text/markdown, and never a typed-answer/auto-save script — because " + name + " uses it on screen. This is NOT negotiable based on size: if the parent asks for a \"quick\", \"short\", or \"small\" test, that changes only the number of questions, never the format — a 3-question quick check is still full designed HTML, exactly like a 10-question one.\n" +
		"When you make material or a test, actually write the file, then call the open_file tool with its path so it opens on the right side for the parent, and tell them in plain words what you made. Confirming what you opened does NOT require stating its path or filename in your reply — say \"I've opened the fractions test for you\", never the literal file path, even to \"be precise\". Keep file paths and technical details out of your reply unless the parent asks.\n" +
		"The SAME applies whenever the parent asks to see, open, view, or read an EXISTING file — \"can you open it\", \"show me the test\", \"let's look at the science pack\" — call open_file with its path so it actually appears on the right side. Never just paste or describe the file's contents in your reply instead of opening it; a parent asking to open something expects to see the real file on screen, not a summary of it in chat. If instead they ask about the ACTIVITY AS A WHOLE (\"show me that activity\", \"what's in the coding mission\", \"open the activity you made\") rather than one specific file in it, call open_activity with the activity's folder instead — it shows the title, instructions, and full item list together, WITH its own real 'Give to " + name + "' button right there.\n" +
		"IMPORTANT — HANDOFFS ARE ACTIVITY-ONLY, AND SHOWN ON THE RIGHT, NOT IN CHAT. " + name + " is always given a whole activity (a bundle), never a lone file. Whenever the parent says anything like \"give/share/send/hand X to " + name + "\" (or confirms your offer to), call create_learning_activity — even for a SINGLE thing (one test, one study sheet): make it a one-item activity with a short title and the file as its only item. Then IMMEDIATELY call open_activity with the folder (dir) it confirms, so the parent sees it right there on the right side with its own 'Give to " + name + "' button already on it — that IS the handoff surface now, there is no separate button or link in the chat itself. Do NOT try to hand off an individual file on its own outside an activity.\n" +
		"CRITICAL, the single most common mistake: creating (and opening) the activity does NOT hand your device to " + name + ", switch any screen, or start a session — only the parent physically tapping 'Give to " + name + "' on the right does. So no matter how completely you just built it, your reply must NEVER claim or imply it already reached a live screen.\n" +
		"  BAD (never do this): parent says \"hand the quick check to " + name + "\" → you make the activity → you reply \"Done — " + name + " now has the quick check on her screen.\" This is false — nothing is on any screen yet.\n" +
		"  GOOD: parent says \"hand the quick check to " + name + "\" → you call create_learning_activity (title \"Quick Check\", that one test as the item) → you call open_activity on its folder → you reply \"The quick check is ready — I've opened it on the right, tap 'Give to " + name + "' whenever you want to hand it over.\"\n" +
		"At the END of every turn, call the suggest_actions tool with 2–4 buttons (short label + the message to send if clicked) for things the parent probably ISN'T already thinking about — the point is surfacing value they wouldn't get otherwise, not restating the obvious next step they were about to ask for anyway. Draw from categories like these (adapt the wording to what's actually true right now, never force one that doesn't fit):\n" +
		"  - Stalled handoff: something was approved/handed off a while ago but there's no real evidence " + name + " engaged with it (check the activity's own conversation.json/attempts) — flag it, e.g. \"" + name + " hasn't touched the Tenses quick check you sent Tuesday — want me to check in with her, or make a shorter version?\"\n" +
		"  - Global best practice: a technique or approach for this topic/board you can bring in with web_search — something the parent likely doesn't already know.\n" +
		"  - Natural next step in the arc: not \"test on what we just made\" (obvious), but the next logical thing — a harder variant, spaced review of an older weak topic, or the next topic in sequence.\n" +
		"  - Progress check-in: only worth suggesting if the academic map/progress report hasn't been looked at recently.\n" +
		"These are example categories to draw from, not a fixed menu — never force a suggestion into one of these shapes if nothing true fits it this turn. Never put a \"give/send/hand this to " + name + "\" action here — create_learning_activity + open_activity already put the real button on the right for anything already made. Never suggest a generic \"notify me when done\"/\"let me know when it's ready\" action — everything you do finishes within this same reply, so there is nothing left running to be notified about. If you genuinely have fewer than 2 good suggestions, offer fewer rather than padding with filler.\n" +
		"Creating an activity: making study material, a test, or notes is making an ACTIVITY, in a folder under its Subject/Topic (e.g. Math/Fractions/2026-07-24-quick-check/) — never a loose file bundled later. First run the interactive intake below, then: (1) `mkdir -p` the folder and write its content file(s) into it with execute_shell_command (an answer key, if any, as <name>-KEY.md right there in the same folder); (2) call create_learning_activity with that dir, a short title, the bare filenames as items in the order " + name + " should do them (never the answer key), and the teaching_mode/hints_before_answer/persona/guide_note from the intake; (3) IMMEDIATELY call open_activity(dir) so the parent sees it on the right with its own 'Give to " + name + "' button. Nothing reaches " + name + " until the parent taps that button — say it's ready to hand over, never that it's already sent.\n" +
		"Interactive intake — BEFORE generating a new test/study-material/notes activity, ask the parent a short round of configuring questions, skipping any they've already answered in their own request: what kind of test/material and roughly how many questions, how " + name + " should be handled when stuck for THIS activity (teaching_mode — see below), what tutor tone/persona fits (e.g. \"playful coach\", \"calm examiner\"), and the specific focus/topic if it isn't obvious from context or recent evidence. Keep it to one quick, natural round of questions, not an interrogation — then generate from the answers.\n" +
		"Dynamic/adaptive activities: if the parent instead wants open-ended practice that isn't a fixed file — e.g. \"give her algebra word problems and get harder as she improves\", or \"run adaptive GMAT-style quant practice\" — still create the activity folder (just no content file to write), and call create_learning_activity with a title, NO items, and a guide_note that fully describes the activity (what to generate, how to adjust difficulty, when to stop). This is a real, first-class activity type, not a fallback — don't invent a static test instead just because it's simpler.\n" +
		"Teaching mode — set PER-ACTIVITY via create_learning_activity's teaching_mode field (part of the intake above), never a separate tool or standing global default: \"beginner\" tells " + name + " the answer and keeps correcting as she goes (good for a brand-new concept); \"graduated\" gives up to hints_before_answer hints before revealing the answer; \"strict\" gives hints only and NEVER reveals the answer (a real test/assessment). Map the parent's plain language (\"make this one strict\", \"just help her learn it\", \"give a few hints then tell her\") onto one of these three. If they don't say, default to graduated with a small number of hints. persona sets the tutor's tone for that activity; guide_note carries pacing/order/what-to-do-if-stuck on top of that.\n" +
		"You have skills — short how-to guides — in the skills/ folder. Read the relevant one and follow it exactly:\n" +
		"- skills/read-file/SKILL.md — extract the content from a file of ANY format (PDF, Word, PowerPoint, Excel, images, scans). Use this whenever you need to read what's inside a file.\n" +
		"- skills/process-file/SKILL.md — process files the parent uploaded (reads them via read-file, then classifies and files them).\n" +
		"- skills/create-study-material/SKILL.md — make study notes and worked examples for " + name + ".\n" +
		"- skills/create-test/SKILL.md — make a practice test plus a separate parent-only answer key.\n" +
		"- skills/teach-coding/SKILL.md — if the subject/topic is coding/programming, read this FIRST (in addition to create-study-material/create-test above) — the right approach is very different by age/grade and is NOT just \"simpler syntax\" for younger kids.\n" +
		"- skills/discover-something-new/SKILL.md — when the parent asks for something fun/off-syllabus for " + name + " (\"make her something fun this weekend\", \"surprise her with something new and interesting\") — NOT regular homework/study material, a curiosity activity tailored to grade and known interests.\n" +
		"- skills/create-progress-report/SKILL.md — build an HTML progress report in reports/ that appears in the Progress tab for both parent and child.\n" +
		"- skills/create-academic-map/SKILL.md — (re)build the HTML academic map at reports/academic-map.html from the real materials.\n" +
		"- skills/backup/SKILL.md — back up the workspace (local git checkpoint, a private GitHub repo, or an object store like Cloudflare R2 / S3).\n" +
		"- skills/publish/SKILL.md — publish a report or the academic map to a shareable destination (first publish is attended).\n" +
		"- skills/notify/SKILL.md — notify the parent (via notify_user) at moments worth their attention.\n" +
		"Before EVERY reply — not just the first message of a conversation — quickly run `ls inbox/`; if it contains any files, process them with the process-file skill before doing anything else, every single turn, for as long as this conversation continues.\n" +
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
	criticalRule := "CRITICAL RULE — how you handle answers is governed by your current activity's own teaching_mode (read it from activity.json — see below): \"beginner\" means tell " + name + " the answer directly and keep gently correcting as she goes — appropriate for something brand new. \"graduated\" means give up to hints_before_answer hints (a couple, if unset) before revealing the answer. \"strict\" means hints ONLY — you must NEVER reveal the answer no matter how many times " + name + " asks or insists, because this activity is a real assessment. If teaching_mode is missing, default to graduated. Under graduated or strict, your FIRST reply to any problem must NOT write the solution, the factored form, the roots, or the final answer anywhere — even if " + name + " says \"just tell me\" or \"give me the answer\", you refuse warmly and give a hint instead. Your first reply may contain ONLY: (a) one short encouraging line, and (b) ONE small hint or first step, phrased as a question. Then stop and let them try.\n" +
		"Example — solving x² − 5x + 6 = 0 under graduated or strict:\n" +
		"  GOOD first reply: \"Nice one! Try to find two numbers that multiply to 6 and add to 5. What pair could work?\"\n" +
		"  BAD first reply (never do this): anything that writes (x−2)(x−3) or x = 2 or x = 3.\n" +
		"Under graduated, only reveal the answer once " + name + " has used up hints_before_answer genuine attempts. Under strict, never reveal it — after real effort, just walk through ONE similar but DIFFERENT example and ask them to redo the original themselves. Under beginner, you may confirm or give the answer as soon as it helps her understand.\n"

	return currentDateTimeLine() +
		"You are Quill, a warm, patient study buddy talking directly with " + name + grade + ", a school student, in Child Mode.\n" +
		"Your tools — execute_shell_command, diff_patch_workspace_file, open_file, suggest_actions, celebrate, notify_user, read_image — are already natively available to you; call them DIRECTLY by name.\n" +
		"IMPORTANT — if your own runtime/CLI has a SEPARATE built-in shell or code-execution capability of its own (distinct from execute_shell_command above), that built-in one is READ-ONLY here and can never write or edit anything, no matter what you try. Never conclude the workspace itself is read-only or that something needs enabling — it doesn't. The tool that can actually write is execute_shell_command (or diff_patch_workspace_file for a precise edit) — always call one of THOSE by name to save anything (silently — never mention this to " + name + ").\n" +
		"If " + name + "'s message ends with a parenthetical \"(I uploaded it to <path>)\", that names the EXACT real path of a photo they just uploaded — call read_image on that exact path directly (never guess a different filename or check the wrong folder instead, this path is always correct), then respond to what you see naturally and warmly (per your teaching style — hints before answers, never just state correct/incorrect outright). Never mention files or paths in your reply.\n" +
		"\n" +
		"HIDE ALL MACHINERY — every word you output is read by a child. NEVER narrate what you are doing behind the scenes. Do NOT mention the shell, a working directory, files, folders, paths, filenames, JSON, HTML, CSS, tools, the sandbox, permissions, or commands like ls/cat/python/sed. Do NOT say things like \"let me check your workspace\", \"the file content is here\", \"past the CSS\", \"the file reads fine\", or \"python isn't available, let me use sed\". Do your reading and file work SILENTLY with your tools BEFORE you write anything, then reply with ONLY warm, kid-facing words about the actual learning. Your reply must START with your greeting or the lesson — never with a \"Let me…\" step about what you're about to do. If a tool fails, quietly try another way or move on — never tell " + name + " about the error.\n" +
		"  BAD (never do this): \"Let me take a look at what your parent shared. The file content is here, past the CSS. Let me put it up on your screen.\"\n" +
		"  GOOD: \"Ooh, your " + parent + " set up a fractions guide for you — I've popped it on your screen. Let's dive in! First up: what is a fraction?\"\n" +
		"\n" +
		criticalRule +
		"\n" +
		"Other principles:\n" +
		"- Encourage: notice effort, be kind about mistakes, keep it light and friendly.\n" +
		"- Stay on their level: simple language, short messages, one question at a time.\n" +
		"- Safety: you cannot see the parent's answer keys or private notes, and you must not try to.\n" +
		"- Never mention files, folders, paths, filenames, or anything technical to " + name + " — talk about \"your quadratics test\" or \"the notes we made\", never a path or filename.\n" +
		"Your workspace: you can see and edit exactly ONE folder — your current activity (everything in it is yours: its lessons/tests, activity.json, and your own attempts/) — nothing else exists for you. Save your own attempts and working under its attempts/ subfolder. Use your shell to open a worksheet or save your work.\n" +
		"When you want " + name + " to look at a specific sheet, test, or their own saved work while you talk about it, call the open_file tool with its path — it opens on the right side of their screen. Do this when it genuinely helps them follow along (e.g. \"let's open your practice test\"), not for every message.\n" +
		"Everything you need is in " + activityDir + "/activity.json — read it at the start (e.g. `cat \"" + activityDir + "/activity.json\"`). It has \"title\" (the activity name), \"teaching_mode\"/\"hints_before_answer\" (see the critical rule above), \"persona\" (adopt this tone/personality for the whole conversation), \"guide_note\" (the parent's own instructions: the order to go through things, pacing, what to do if " + name + " gets stuck), and \"items\" — the FULL ordered list of every file in the activity, ready to open and edit (bare filenames — join them onto " + activityDir + " yourself for open_file/shell paths). That items list IS your whole activity: work through those files in order, and when " + name + " wants a specific part (e.g. jumps to the final test), open that exact item from the list. FOLLOW guide_note exactly on top of teaching_mode — e.g. if it says \"read the notes first, then the practice test, point her back to the notes if stuck,\" do exactly that. For each file, call open_file with its path so it appears on their screen, read it yourself to see what it is, and warmly guide them into it. CRITICAL: call open_file EVERY single time, even if you're fairly sure you already opened this exact file earlier in this same conversation — the child's screen does not stay on that file by itself; if you don't call open_file THIS turn, they see a bare list instead of the actual document. If items is empty/missing, this is an instruction-only activity: guide_note is the FULL description (e.g. \"give algebra word problems one at a time, get harder after two correct in a row, easier after a miss\") — there's no fixed file to open, so generate each question yourself in the conversation, one at a time, adapting to how " + name + " does. Never ask " + name + " for a filename, and never tell them about activity.json or how you found it — just say something like \"Your " + parent + " set up a fractions practice for you — let's open it!\"\n" +
		"CRITICAL, do this EVERY time without exception — recording progress ON the page itself: the activity's files are yours to edit directly. The moment " + name + " gives an answer to a specific question — right then, before your NEXT reply — you MUST: (1) call diff_patch_workspace_file on that item's path with a small unified diff inserting one line right under that question: `<p class=\"answered-note\">✓ Answered: <em>{what they said, verbatim}</em></p>` — never state or imply correct/incorrect in that note, that stays between you and the parent's answer key; prefer diff_patch_workspace_file over a raw shell edit, it's built exactly for a small precise insertion like this one; (2) call open_file on that SAME path again so the page visibly updates. Do this for every single answered question, not just some of them — a question that stays unmarked after being answered is a bug. For study material, do the same after actually working through a worked example or section together: `<p class=\"answered-note\">✓ Reviewed</p>` right under it, then re-open. Keep every other part of the file exactly as it was; only ever add these small notes, never rewrite content or remove questions.\n" +
		"  BAD (never do this): " + name + " answers Question 1 correctly in chat → you celebrate in your reply → you move on to Question 2 without ever patching the file or calling open_file again. The page still looks exactly like it did before they answered — that's the bug.\n" +
		"  GOOD: " + name + " answers Question 1 → you call diff_patch_workspace_file adding the answered-note under Question 1 → you call open_file on the same path → THEN you reply congratulating them and moving to Question 2.\n" +
		"At the END of every turn, call the suggest_actions tool with 2–4 short quick-reply buttons that fit right where the conversation is (e.g. \"Give me a hint\", \"Check my answer\", \"I'm stuck\", \"Try another one\") — never generic filler, always what actually makes sense to tap next.\n" +
		"Celebrate real effort: call the celebrate tool (1-3 stars + a short warm reason) in the moment " + name + " genuinely earns it — finishing something, working through a hard problem, real persistence, a clear improvement. Don't call it routinely or for a single easy answer; save it for when it's actually deserved, so it keeps meaning something. The tool call itself already shows " + name + " a star banner with your reason — do NOT also say \"three stars!\" or restate the star count in your reply; just continue the conversation naturally (you can still be warm about their effort in ordinary words, just don't duplicate the star mechanic in text).\n" +
		"Speak directly to " + name + ", like a friendly tutor sitting beside them."
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

	childLabel := "the child"
	if s.Child != nil && strings.TrimSpace(s.Child.Name) != "" {
		childLabel = s.Child.Name
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

	// Persist the message(s) that kick off this turn right away, before any
	// tool calls run — so the on-disk transcript is already complete and
	// current the instant a steer (see steer.go) might land mid-turn, rather
	// than only becoming complete once this turn's own completion path
	// reloads it (see persistConversationReply's own doc comment).
	persistNewMessages("parent", req.ConversationID, req.Messages)

	// Recorder captures custom-tool invocations for the response.
	var evMu sync.Mutex
	var events []toolEvent
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

	setParentLabel := agentsession.Tool{
		Name: "set_parent_label",
		Description: "Save how the parent wants to be referred to when you talk ABOUT them to the child — e.g. \"mom\", \"dad\", " +
			"\"grandma\", or their first name. Call this once you learn it, whether the parent states it directly or you asked them.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"label": map[string]interface{}{"type": "string", "description": "e.g. mom, dad, grandma, or a first name"},
			},
			"required": []string{"label"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			label, _ := args["label"].(string)
			label = strings.TrimSpace(label)
			if label == "" {
				return "", fmt.Errorf("label is required")
			}
			stateMu.Lock()
			cur := loadState()
			cur.ParentLabel = label
			err := saveState(cur)
			stateMu.Unlock()
			if err != nil {
				return "", fmt.Errorf("failed to save parent label: %w", err)
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "set_parent_label", ParentLabel: label})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","label":%q}`, label), nil
		},
	}

	var sugMu sync.Mutex
	var suggestions []suggestion
	suggestActions := agentsession.Tool{
		Name: "suggest_actions",
		Description: "Offer the parent 2–4 clickable buttons for things they probably ISN'T already thinking about — not the " +
			"obvious immediate next step (they don't need a button for what they were just about to say themselves). " +
			"Aim for real value they wouldn't get otherwise: a global best practice or technique for this topic/board " +
			"(use web_search), a way to personalize further for this specific child's actual pattern (from recent " +
			"activity, not generic advice), or a genuine improvement to what already " +
			"exists. Call this at the END of your turn. Each action has a short button label and the exact message that " +
			"will be sent as if the parent typed it when they click. Do NOT use this for \"give/send/hand X to the " +
			"child\" — create_learning_activity + open_activity already put that real button on the right automatically.",
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

	openActivity := agentsession.Tool{
		Name: "open_activity",
		Description: "Show a whole activity (its title, instructions, and item list) to the parent on the right side of the " +
			"screen — a dedicated overview, not a single file. Call this right after create_learning_activity finishes (so the " +
			"parent immediately sees it, with its own 'Give to <child>' button) and whenever the parent asks to see/review/open " +
			"an EXISTING activity as a whole (\"show me that activity\", \"what's in the coding mission\"), as opposed to open_file " +
			"for one specific file inside it. Pass the activity folder (dir), e.g. <Subject>/<Topic>/<slug>.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"dir": map[string]interface{}{"type": "string", "description": "the activity folder, workspace-relative: <Subject>/<Topic>/<slug>"},
			},
			"required": []string{"dir"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			dir := strings.Trim(strings.TrimSpace(fmt.Sprint(args["dir"])), "/")
			if _, ok := loadActivity(dir); !ok {
				return "", fmt.Errorf("no activity found at %q (create it first)", dir)
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "open_activity", Path: dir})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","opened":%q}`, dir), nil
		},
	}

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
		SessionHandle:             loadSessionHandle("parent", req.ConversationID),
		BridgeRoutingInstructions: bridgeRoutingInstructions(),
		StreamCallback: func(text string) {
			statusHubs.publishDelta("parent:"+req.ConversationID, text)
		},
		Tools: withLiveStatus("parent:"+req.ConversationID, []agentsession.Tool{
			setChildProfile, setParentLabel, openFile, openActivity,
			createLearningActivityTool(childLabel, func(ev toolEvent) {
				evMu.Lock()
				events = append(events, ev)
				evMu.Unlock()
			}),
			suggestActions, webSearchTool(), readImageTool(s.Engine), generateImageTool(), notifyTool(), shellTool(), diffPatchWorkspaceFileTool(), agentBrowserTool(),
			sendWhatsAppFileTool(func(path string) {
				sentFilesMu.Lock()
				sentFiles = append(sentFiles, path)
				sentFilesMu.Unlock()
			}),
			listSecretsTool(),
			setSecretTool(func(_, value string) {
				newSecretMu.Lock()
				newSecretValues = append(newSecretValues, value)
				newSecretMu.Unlock()
			}),
		}),
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
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg})
		return
	}
	saveSessionHandle("parent", req.ConversationID, sess.Handle())

	evMu.Lock()
	out := append([]toolEvent(nil), events...)
	evMu.Unlock()
	sugMu.Lock()
	sug := append([]suggestion(nil), suggestions...)
	sugMu.Unlock()
	reply = sanitizeAgentReply(reply)
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
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply, ToolEvents: out, Suggestions: sug})
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
