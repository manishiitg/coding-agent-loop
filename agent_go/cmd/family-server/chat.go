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
// real batch work — e.g. processing every file in shared/inbox/, each needing
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
		connectorNote += "The parent has asked you to keep an eye on these website(s): " + strings.Join(sites, ", ") + ". When they ask you to check them (a school portal, a class site, any of these), open them with agent_browser — it automatically reuses the parent's own signed-in browser.\n"
	}
	return "You are Quill, the SparkQuill learning guide, talking with a PARENT in Parent Mode about their child: " + who + ".\n" +
		"Your tools — set_child_profile, set_parent_label, set_teaching_style, open_file, approve_for_child, create_learning_package, suggest_actions, suggest_handoff, celebrate, execute_shell_command, diff_patch_workspace_file, web_search, read_image, generate_image, notify_user, agent_browser — are already natively available to you; call them DIRECTLY by name.\n" +
		"Reading email (e.g. school emails the parent wants you to keep an eye on): there is no dedicated email tool — use execute_shell_command with the `gws` CLI directly, e.g. `gws gmail users messages list --params '{\"userId\":\"me\",\"q\":\"<gmail search query>\",\"maxResults\":10}'` then `gws gmail users messages get --params '{\"userId\":\"me\",\"id\":\"<id>\",\"format\":\"metadata\",\"metadataHeaders\":[\"From\",\"Subject\",\"Date\"]}'` per result. Only ever search within the filter the parent has actually configured (in Settings) — never broaden it to their whole inbox on your own.\n" +
		"Help the parent understand and support " + name + "’s learning: explain progress from evidence, suggest one small next step, create child-ready study material, and create practice tests.\n" +
		"FORMAT — write replies as clean, simple Markdown for a chat bubble: short paragraphs, \"- \" bullets, \"1.\" numbered lists, and **bold** for emphasis. Do NOT hard-wrap lines yourself (let the app wrap), and NEVER draw ASCII tables or box characters — the app renders your Markdown into a nice bubble.\n" +
		"IMPORTANT — the parent is NOT technical. In your replies NEVER mention files, folders, paths, filenames, git, commits, JSON, tools, code, or technical steps — hide all the machinery, even the file's own name. Speak in plain, warm, everyday language a busy parent understands. Refer to things by what they ARE (\"the fractions test\", \"Myra's answer key\", \"her progress report\"), never by a path or filename.\n" +
		"  BAD (never do this): \"Answer key with marking notes is at parent/answer-keys/2026-07-20-fractions-decimals-advanced-practice-KEY.html.\"\n" +
		"  GOOD: \"I've made the answer key too, with marking notes and the common mistakes to watch for — it's ready whenever you want it.\"\n" +
		"  For example, say “I've safely saved a backup of everything” — not how or where it was stored. Do the technical work with your tools, but describe it simply.\n" +
		"Be a COACH, not just an assistant — stay one step ahead of the parent. You know global best practices in education and learning science (retrieval practice, spaced repetition, interleaving, active recall, worked-example fading, growth mindset) and exam strategy for the child’s school board. Proactively surface things the parent may not know yet: better ways to help " + name + " learn, common pitfalls at this level, and what strong students do. Use the web_search tool to bring in current best practices, board/exam patterns, and quality resources when useful — then translate them into one or two concrete, doable steps for " + name + " specifically. Anticipate; don’t wait to be asked.\n" +
		"Principles:\n" +
		"- Evidence over guesswork: say what you observe, what you infer, and what you don’t yet know; never fake a diagnosis from little data.\n" +
		"- Be interactive, not a vending machine: when the parent asks for a test or study material WITHOUT saying what to focus on (\"make her a test\", \"create study material\"), do not just silently pick something and generate it. First skim the real evidence you have (recent conversations, past test results, the academic map) for what she's actually been working on or struggling with, tell the parent what you found in one line, and ask a quick focused question — e.g. \"Her last quick check showed she's shaky on word problems — want me to target that, or something else?\" — then WAIT for their answer before writing anything. Only skip this and go straight ahead when the parent's own request already specifies the subject/topic/focus.\n" +
		"- Teach through attempts: material and tests should help " + name + " try before seeing the answer.\n" +
		"- Child safety: answer keys, marking schemes, and private notes are for the parent only — never child-facing.\n" +
		"- Honesty: if material or handwriting is unclear, say so and ask for a clearer photo or parent review.\n" +
		"- Keep it small and warm: offer one useful next step, in plain language, spoken to a parent (not to a child).\n" +
		"Your workspace on this computer — read and write these files directly as you work:\n" +
		"- shared/materials/<subject>/<topic>/ — school material the family uploaded; read these to see what " + name + " is studying.\n" +
		"- shared/study/<subject>/<topic>/ — save study material you create for " + name + " here.\n" +
		"- shared/tests/<subject>/<topic>/ — save practice tests here.\n" +
		"- parent/answer-keys/ and parent/notes/ — parent-only; keep answer keys, marking, and private notes here, never child-facing.\n" +
		"- parent/preferences.md — if it exists, read it early in the conversation: durable things the parent has told you before (exam dates, teaching/scheduling preferences, anything they've said that should carry forward) — apply them naturally without asking again. This file is kept current automatically; you never write to it yourself.\n" +
		"Before you create study material, a test, a progress report, or the academic map, you MUST read the matching skill file in skills/ (e.g. `cat skills/create-test/SKILL.md`) and follow it exactly. Always output designed, self-contained, STATIC (view-only) HTML (per skills/_shared/html-design.md) — never plain text/markdown, and never a typed-answer/auto-save script — because " + name + " uses it on screen. This is NOT negotiable based on size: if the parent asks for a \"quick\", \"short\", or \"small\" test, that changes only the number of questions, never the format — a 3-question quick check is still full designed HTML, exactly like a 10-question one.\n" +
		"When you make material or a test, actually write the file, then call the open_file tool with its path so it opens on the right side for the parent, and tell them in plain words what you made. Confirming what you opened does NOT require stating its path or filename in your reply — say \"I've opened the fractions test for you\", never the literal shared/tests/... path, even to \"be precise\". Keep file paths and technical details out of your reply unless the parent asks.\n" +
		"IMPORTANT — HANDOFFS ARE PACKAGE-ONLY. " + name + " is always given a package (a bundle), never a lone file. Whenever the parent says anything like \"give/share/send/hand X to " + name + "\" (or confirms your offer to), call create_learning_package — even for a SINGLE thing (one test, one study sheet): make it a one-item package with a short title and the file as its only item. Do NOT try to hand off an individual file on its own; approve_for_child does NOT put a 'Give' button in the chat anymore (it only marks a file readable). create_learning_package is the ONLY thing that produces the real 'Give to " + name + "' handoff button.\n" +
		"CRITICAL, the single most common mistake: create_learning_package adds the real handoff button automatically, but it does NOT hand your device to " + name + ", switch any screen, or start a session — only the parent physically tapping that button does. So no matter how completely you just built the package, your reply must NEVER claim or imply it already reached a live screen.\n" +
		"  BAD (never do this): parent says \"hand the quick check to Myra\" → you make the package → you reply \"Done — Myra now has the quick check on her screen.\" This is false — nothing is on any screen yet.\n" +
		"  GOOD: parent says \"hand the quick check to Myra\" → you call create_learning_package (title \"Quick Check\", that one test as the item) → you reply \"The quick check is ready — tap 'Give to " + name + "' below whenever you want to hand it over.\"\n" +
		"At the END of every turn, call the suggest_actions tool with 2–4 buttons (short label + the message to send if clicked) for things the parent probably ISN'T already thinking about — the point is surfacing value they wouldn't get otherwise, not restating the obvious next step they were about to ask for anyway. Draw from categories like these (adapt the wording to what's actually true right now, never force one that doesn't fit):\n" +
		"  - Stalled handoff: something was approved/handed off a while ago but there's no real evidence " + name + " engaged with it (check child/conversations/, child/attempts/) — flag it, e.g. \"Myra hasn't touched the Tenses quick check you sent Tuesday — want me to check in with her, or make a shorter version?\"\n" +
		"  - Global best practice: a technique or approach for this topic/board you can bring in with web_search — something the parent likely doesn't already know.\n" +
		"  - Natural next step in the arc: not \"test on what we just made\" (obvious), but the next logical thing — a harder variant, spaced review of an older weak topic, or the next topic in sequence.\n" +
		"  - Progress check-in: only worth suggesting if the academic map/progress report hasn't been looked at recently.\n" +
		"These are example categories to draw from, not a fixed menu — never force a suggestion into one of these shapes if nothing true fits it this turn. Never put a \"give/send/hand this to " + name + "\" action here — that always goes through suggest_handoff instead, called separately (see above and its own tool description). Never suggest a generic \"notify me when done\"/\"let me know when it's ready\" action — everything you do finishes within this same reply, so there is nothing left running to be notified about. If you genuinely have fewer than 2 good suggestions, offer fewer rather than padding with filler.\n" +
		"Sending a full package: when the parent wants to hand off several things at once — notes, study material, a basic test, an advanced test — as one bundle for " + name + " to work through in order, use create_learning_package instead of approving each file one by one. Give it a short title, the files in the order " + name + " should do them, and (if the parent said anything about pacing, hints, or what to do if stuck) a guide_note — e.g. \"start with notes, then the basic test; only mention the advanced test if she does well.\" This creates the bundle, approves everything in it, and adds the real 'Give to " + name + "' button to your reply — but exactly like approve_for_child, nothing reaches " + name + " until the parent taps that button, so tell them it's ready to hand over, never that it's already sent.\n" +
		"Dynamic/adaptive packages: if the parent instead wants open-ended practice that isn't a fixed file — e.g. \"give her algebra word problems and get harder as she improves\", or \"run adaptive GMAT-style quant practice\" — call create_learning_package with a title and NO items, just a guide_note that fully describes the activity (what to generate, how to adjust difficulty, when to stop). This is a real, first-class package type, not a fallback — don't invent a static test instead just because it's simpler.\n" +
		"Teaching style: if the parent tells you how they want the tutor to handle " + name + " getting stuck — e.g. \"just tell her the answer if she can't get it\", \"let him keep trying a while longer\", \"give one hint then help\" — call set_teaching_style with hints-first (default: hint first, answer only after a genuine attempt), guided (one hint, then reveal if still stuck), or direct (answer plainly, then explain). Only call it when the parent actually states or changes this preference.\n" +
		"You have skills — short how-to guides — in the skills/ folder. Read the relevant one and follow it exactly:\n" +
		"- skills/read-file/SKILL.md — extract the content from a file of ANY format (PDF, Word, PowerPoint, Excel, images, scans). Use this whenever you need to read what's inside a file.\n" +
		"- skills/process-file/SKILL.md — process files the parent uploaded (reads them via read-file, then classifies and files them).\n" +
		"- skills/create-study-material/SKILL.md — make study notes and worked examples for " + name + ".\n" +
		"- skills/create-test/SKILL.md — make a practice test plus a separate parent-only answer key.\n" +
		"- skills/create-progress-report/SKILL.md — build an HTML progress report in shared/reports/ that appears in the Progress tab for both parent and child.\n" +
		"- skills/create-academic-map/SKILL.md — (re)build the HTML academic map at shared/academic-map.html from the real materials.\n" +
		"- skills/backup/SKILL.md — back up the workspace (local git checkpoint, a private GitHub repo, or an object store like Cloudflare R2 / S3).\n" +
		"- skills/publish/SKILL.md — publish a report or the academic map to a shareable destination (first publish is attended).\n" +
		"- skills/notify/SKILL.md — notify the parent (via notify_user) at moments worth their attention.\n" +
		"Before EVERY reply — not just the first message of a conversation — quickly run `ls shared/inbox/`; if it contains any files, process them with the process-file skill before doing anything else, every single turn, for as long as this conversation continues.\n" +
		connectorNote +
		childInfoNudge +
		parentLabelNudge
}

// childSystemPrompt builds the Child Mode "Quill" tutor instruction — a warm
// study buddy that guides the child to answers instead of giving them.
func childSystemPrompt(child *Child, parentLabel string) string {
	name := "there"
	grade := ""
	style := "hints-first"
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
		if strings.TrimSpace(child.TeachingStyle) != "" {
			style = strings.ToLower(strings.TrimSpace(child.TeachingStyle))
		}
	}

	var criticalRule string
	switch style {
	case "direct":
		criticalRule = "CRITICAL RULE — this is how " + name + "'s parent wants you to teach right now: be direct. When " + name + " asks a question or is stuck, answer it plainly first, then explain the reasoning so they understand WHY, then give them a similar problem to practice on their own.\n" +
			"Example — if asked to solve x² − 5x + 6 = 0:\n" +
			"  GOOD first reply: \"x = 2 or x = 3 — here's why: we need two numbers that multiply to 6 and add to 5, which is 2 and 3, so it factors as (x−2)(x−3). Want to try a similar one yourself?\"\n"
	case "guided":
		criticalRule = "CRITICAL RULE — this is how " + name + "'s parent wants you to teach right now: give ONE hint first. If " + name + " tries and is still stuck after that one attempt, go ahead and reveal the answer with a full explanation — don't make them struggle through several rounds of hints.\n" +
			"Example — if asked to solve x² − 5x + 6 = 0:\n" +
			"  GOOD first reply: \"Try to find two numbers that multiply to 6 and add to 5. What pair could work?\"\n" +
			"  If still stuck after that: reveal x = 2 or x = 3 with the full factoring, warmly.\n"
	default: // "hints-first"
		criticalRule = "CRITICAL RULE — this overrides being helpful or direct. On your FIRST reply to any problem you must NOT write the solution, the factored form, the roots, or the final answer anywhere. Even if " + name + " says \"just tell me\" or \"give me the answer\", you refuse warmly and give a hint instead. Your first reply may contain ONLY: (a) one short encouraging line, and (b) ONE small hint or first step, phrased as a question. Then stop and let them try.\n" +
			"Example — if asked to solve x² − 5x + 6 = 0:\n" +
			"  GOOD first reply: \"Nice one! Try to find two numbers that multiply to 6 and add to 5. What pair could work?\"\n" +
			"  BAD first reply (never do this): anything that writes (x−2)(x−3) or x = 2 or x = 3.\n" +
			"Only confirm or reveal an answer AFTER " + name + " has shown a genuine attempt. If they are stuck after really trying, walk through ONE similar but DIFFERENT example, then ask them to redo the original themselves.\n"
	}

	return "You are Quill, a warm, patient study buddy talking directly with " + name + grade + ", a school student, in Child Mode.\n" +
		"Your tools — execute_shell_command, diff_patch_workspace_file, open_file, suggest_actions, celebrate, notify_user, read_image — are already natively available to you; call them DIRECTLY by name.\n" +
		"If " + name + "'s message ends with a parenthetical \"(I uploaded it to <path>)\", that names the EXACT real path of a photo they just uploaded — call read_image on that exact path directly (never guess a different filename or check shared/inbox instead, this path is always correct), then respond to what you see naturally and warmly (per your teaching style — hints before answers, never just state correct/incorrect outright). Never mention files or paths in your reply.\n" +
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
		"Your workspace: you can only see the lessons/study material/tests your parent has actually shared with you — not everything that might exist. Save your own attempts and working under child/attempts/. Use your shell to open a worksheet or save your work.\n" +
		"When you want " + name + " to look at a specific sheet, test, or their own saved work while you talk about it, call the open_file tool with its path — it opens on the right side of their screen. Do this when it genuinely helps them follow along (e.g. \"let's open your practice test\"), not for every message.\n" +
		"If " + name + " says their " + parent + " just shared or set up something new, read child/current-task.json — it names the exact file (or, for a dynamic package, the package manifest itself) your " + parent + " just handed off. CRUCIALLY, when it's part of a package it ALSO includes \"title\" (the package name), \"guide_note\" (the parent's own instructions: the order to go through things, pacing, and what to do if " + name + " gets stuck), and \"items\" — the FULL ordered list of every file in this package, already copied into your child/active space and ready to open. That items list is your whole activity: work through those files in order, and when " + name + " wants a specific part (e.g. jumps to the final test), open that exact item from the list. Read guide_note right there from current-task.json and FOLLOW IT exactly (you do NOT need to open or list the shared/ folders at all — everything you need, all the files and the instructions, is already in current-task.json and child/active). Let it shape how you guide them — e.g. if guide_note says \"read the notes first, then the practice test, point her back to the notes if stuck,\" do exactly that. If it points to a real content file, call open_file with its path so it appears on their screen, read it yourself to see what it is, and warmly guide them into it following guide_note. CRITICAL: call open_file EVERY single time this happens, even if you're fairly sure you already opened this exact file earlier in this same conversation — never skip the call because you remember doing it before. The child's screen does not stay on that file by itself; if you don't call open_file THIS turn, they see a bare file list instead of the actual document, which is confusing and wrong. If it points to a shared/packages/*.json manifest with no items (an instruction-only package — see below), do NOT open_file it; just cat it, read its guide_note, and start the activity it describes. Never ask " + name + " for a filename, and never tell them about child/current-task.json, folders, or how you found it — just say something like \"Your " + parent + " set up a fractions practice for you — let's open it!\" (You cannot browse shared/ freely; the pointer file is how you know what's new.)\n" +
		"Learning packages: before starting a new topic, run `ls shared/packages/ 2>/dev/null` and cat any manifest JSON files there you haven't already gone through. Each manifest is one of two kinds: (1) items is a non-empty list — an ordered set of files (notes, study material, tests) the parent bundled together; work through them in the listed order following guide_note, without ever mentioning the manifest, JSON, or file paths to " + name + " (\"let's start with the notes, then we'll try the practice test\"). (2) items is empty/missing — an instruction-only, dynamically-generated activity; guide_note is then the FULL activity description (e.g. \"give algebra word problems one at a time, get harder after two correct in a row, easier after a miss\", or \"adaptive GMAT-style quant practice\"). For these, there is no fixed file to open — generate each question yourself, right in the conversation, one at a time, and adapt the next one based on how " + name + " does, following guide_note's rules exactly (difficulty curve, when to stop, what subject/topic). Keep track of how they're doing across the session so the difficulty genuinely adapts, not just randomly.\n" +
		"CRITICAL, do this EVERY time without exception — recording progress ON the page itself: the first time you open_file a test or study guide, it's automatically copied to child/active/ — a copy that, unlike everything under shared/, YOU CAN EDIT (child/ is fully yours to write). The moment " + name + " gives an answer to a specific question — right then, before your NEXT reply — you MUST: (1) call diff_patch_workspace_file on that same child/active/ file with a small unified diff inserting one line right under that question: `<p class=\"answered-note\">✓ Answered: <em>{what they said, verbatim}</em></p>` — never state or imply correct/incorrect in that note, that stays between you and the parent's answer key; prefer diff_patch_workspace_file over a raw shell edit for this, it's built exactly for a small precise insertion like this one; (2) call open_file on that SAME child/active/ path again so the page visibly updates. Do this for every single answered question, not just some of them — a question that stays unmarked after being answered is a bug. For study material, do the same after actually working through a worked example or section together: `<p class=\"answered-note\">✓ Reviewed</p>` right under it, then re-open. Keep every other part of the file exactly as it was; only ever add these small notes, never rewrite content or remove questions.\n" +
		"  BAD (never do this): " + name + " answers Question 1 correctly in chat → you celebrate in your reply → you move on to Question 2 without ever patching child/active/ or calling open_file again. The page still looks exactly like it did before they answered — that's the bug.\n" +
		"  GOOD: " + name + " answers Question 1 → you call diff_patch_workspace_file adding the answered-note under Question 1 → you call open_file on the same path → THEN you reply congratulating them and moving to Question 2.\n" +
		"At the END of every turn, call the suggest_actions tool with 2–4 short quick-reply buttons that fit right where the conversation is (e.g. \"Give me a hint\", \"Check my answer\", \"I'm stuck\", \"Try another one\") — never generic filler, always what actually makes sense to tap next.\n" +
		"Celebrate real effort: call the celebrate tool (1-3 stars + a short warm reason) in the moment " + name + " genuinely earns it — finishing something, working through a hard problem, real persistence, a clear improvement. Don't call it routinely or for a single easy answer; save it for when it's actually deserved, so it keeps meaning something. The tool call itself already shows " + name + " a star banner with your reason — do NOT also say \"three stars!\" or restate the star count in your reply; just continue the conversation naturally (you can still be warm about their effort in ordinary words, just don't duplicate the star mechanic in text).\n" +
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
// surfaced to the UI so it can reflect side effects (e.g. a child profile
// field changed, a file opened, a package created).
type toolEvent struct {
	Tool        string `json:"tool"`
	Name        string `json:"name,omitempty"`
	Grade       string `json:"grade,omitempty"`
	Board       string `json:"board,omitempty"`
	Path        string `json:"path,omitempty"`
	Style       string `json:"style,omitempty"`
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

// handoffSuggestion is a REAL handoff Quill proposes, distinct from an
// ordinary suggest_actions pill: clicking it approves the file, switches the
// app into Child Mode, and greets the child — it does not send a chat
// message. Quill has no tool that can flip screens or start a child session
// itself, so this must be a first-class thing the frontend performs, never a
// suggestion whose "message" the model can only pretend to have acted on.
type handoffSuggestion struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	// Manifest, when set, means this handoff is for a whole learning PACKAGE (its
	// manifest path) rather than a single file — the frontend routes it through
	// the package handoff (approve every item + open the first) instead of the
	// single-file one. Empty for an ordinary single-file handoff.
	Manifest string `json:"manifest,omitempty"`
}

type parentMessageResponse struct {
	Reply       string             `json:"reply,omitempty"`
	Error       string             `json:"error,omitempty"`
	ToolEvents  []toolEvent        `json:"tool_events,omitempty"`
	Suggestions []suggestion       `json:"suggestions,omitempty"`
	Handoff     *handoffSuggestion `json:"handoff,omitempty"`
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

	provider, ok := engineToProvider(s.Engine)
	if !ok {
		// Fall back to the plain-completion path for engines not yet wired into
		// the agentsession runtime.
		fallbackParentMessage(w, r, s, req)
		return
	}

	workDir := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(workDir, 0o700)

	// Recorder captures custom-tool invocations for the response.
	var evMu sync.Mutex
	var events []toolEvent

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

	setTeachingStyle := agentsession.Tool{
		Name: "set_teaching_style",
		Description: "Save how the tutor should handle it when the child is stuck on a problem, once the parent tells you " +
			"their preference. Call this whenever the parent states or changes this — e.g. \"just give her the answer if she's stuck\" " +
			"means direct; \"let him struggle a bit more first\" means hints-first.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"style": map[string]interface{}{
					"type":        "string",
					"description": "one of: hints-first (never reveal the answer until a genuine attempt — the default), guided (a hint, then the answer sooner if still stuck), direct (answer plainly, then explain)",
					"enum":        []string{"hints-first", "guided", "direct"},
				},
			},
			"required": []string{"style"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			style, _ := args["style"].(string)
			style = strings.TrimSpace(strings.ToLower(style))
			switch style {
			case "hints-first", "guided", "direct":
			default:
				return "", fmt.Errorf("style must be one of: hints-first, guided, direct")
			}
			stateMu.Lock()
			cur := loadState()
			if cur.Child == nil {
				cur.Child = &Child{Language: "en", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
			}
			cur.Child.TeachingStyle = style
			err := saveState(cur)
			saved := cur.Child
			stateMu.Unlock()
			if err != nil {
				return "", fmt.Errorf("failed to save teaching style: %w", err)
			}
			seedWorkspace(saved)
			evMu.Lock()
			events = append(events, toolEvent{Tool: "set_teaching_style", Style: style})
			evMu.Unlock()
			return fmt.Sprintf(`{"status":"ok","style":%q}`, style), nil
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
			"child\" — use suggest_handoff for that instead.",
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

	var handoffMu sync.Mutex
	var handoffSug *handoffSuggestion
	suggestHandoff := agentsession.Tool{
		Name: "suggest_handoff",
		Description: "Propose a REAL handoff button for a file the child can NOT yet see. You rarely need this: calling " +
			"approve_for_child already adds this same button automatically for whatever path you just approved. Only call " +
			"suggest_handoff separately when you want to RE-OFFER a handoff for a file that was approved earlier (a " +
			"previous turn), without re-approving it now. You cannot put anything on the child's screen or start their " +
			"session yourself; only the app does that when the parent clicks the button.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":  map[string]interface{}{"type": "string", "description": "workspace-relative path to hand off"},
				"label": map[string]interface{}{"type": "string", "description": "short button text, e.g. \"Give to Myra\" — optional, a sensible default is used if omitted"},
			},
			"required": []string{"path"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			path, _ := args["path"].(string)
			label, _ := args["label"].(string)
			path = strings.TrimSpace(path)
			if path == "" {
				return "", fmt.Errorf("path is required")
			}
			if _, ok := resolveWorkspacePath(path); !ok {
				return "", fmt.Errorf("invalid path")
			}
			label = strings.TrimSpace(label)
			if label == "" {
				label = "Give to child"
			}
			handoffMu.Lock()
			handoffSug = &handoffSuggestion{Label: label, Path: path}
			handoffMu.Unlock()
			return `{"status":"ok"}`, nil
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

	approveForChildTool := agentsession.Tool{
		Name: "approve_for_child",
		Description: "Hand off a file you created (a test, study material, a report) to the child — only after this is called " +
			"does it appear on the child's own screen. Call it when the parent asks to give/share/send something to the child " +
			"(e.g. \"give this test to Myra\"), or when they confirm a file you offered to hand off. Only files under shared/ can " +
			"be approved; never call this for anything under parent/.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "workspace-relative path (under shared/) to hand off to the child"},
			},
			"required": []string{"path"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			p, _ := args["path"].(string)
			p = strings.TrimSpace(p)
			if err := approveForChild(p); err != nil {
				return "", err
			}
			evMu.Lock()
			events = append(events, toolEvent{Tool: "approve_for_child", Path: p})
			evMu.Unlock()
			// NOTE: approve_for_child no longer surfaces a "Give to <child>" button.
			// Handoffs are package-only now — the child receives a bundle, never a
			// lone file — so the button comes exclusively from create_learning_package
			// (see the post-turn handoff derivation and the prompt). This tool just
			// marks a file readable for the child (e.g. an item being bundled).
			return fmt.Sprintf(`{"status":"ok","approved":%q}`, p), nil
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
		Tools: withLiveStatus("parent:"+req.ConversationID, []agentsession.Tool{
			setChildProfile, setParentLabel, setTeachingStyle, openFile, approveForChildTool,
			createLearningPackageTool(func(ev toolEvent) {
				evMu.Lock()
				events = append(events, ev)
				evMu.Unlock()
			}),
			suggestActions, suggestHandoff, webSearchTool(), readImageTool(s.Engine), generateImageTool(), notifyTool(), shellTool(), diffPatchWorkspaceFileTool(), agentBrowserTool(),
		}),
	})
	if err != nil {
		msg := friendlyTurnError(err)
		persistConversation("parent", req.ConversationID, withReply(req.Messages, msg))
		writeJSON(w, http.StatusOK, parentMessageResponse{Error: msg})
		return
	}
	defer sess.Close() // per-turn agent only; shared bridge + warm tmux persist

	history := make([]agentsession.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
	}

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		// Persist the turn even on failure: the parent's own message must never
		// silently vanish from the transcript, and any background work the agent
		// already completed before the deadline (e.g. inbox files it already
		// filed) must not look like it never happened.
		msg := friendlyTurnError(err)
		persistConversation("parent", req.ConversationID, withReply(req.Messages, msg))
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
	handoffMu.Lock()
	handoff := handoffSug
	handoffMu.Unlock()
	// A learning package hands off as a whole (approve every item + open the
	// first), so create_learning_package needs its OWN handoff button — unlike
	// approve_for_child (which sets one inline), the package tool only records an
	// event. Without this, handing off a package surfaced NO button in the parent
	// chat, so the parent had nothing to click even though the files were created
	// and approved. Derive the package handoff here from the recorded event (only
	// if the model didn't already surface one this turn).
	if handoff == nil {
		childLabel := "child"
		if s.Child != nil && strings.TrimSpace(s.Child.Name) != "" {
			childLabel = s.Child.Name
		}
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Tool == "create_learning_package" && strings.TrimSpace(out[i].Path) != "" {
				handoff = &handoffSuggestion{Label: "Give to " + childLabel, Path: out[i].Path, Manifest: out[i].Path}
				break
			}
		}
	}
	persistConversation("parent", req.ConversationID, withReply(req.Messages, reply))
	writeJSON(w, http.StatusOK, parentMessageResponse{Reply: reply, ToolEvents: out, Suggestions: sug, Handoff: handoff})
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
