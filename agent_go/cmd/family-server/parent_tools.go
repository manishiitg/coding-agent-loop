package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// parent_tools.go owns the ONE canonical Parent-Mode tool manifest.
//
// Why this file exists: web chat (chat.go), the WhatsApp bot (whatsapp_bot.go),
// direct WhatsApp (whatsapp.go), and Pulse (pulse.go) all run against the SAME
// persistent parent session — every one of them passes
// SessionID = parentConversationID ("parent"), which is deliberate (one
// continuous parent↔Quill conversation, one warm tmux session, resumable via
// the stored session handle). But each surface used to hand that shared session
// a DIFFERENT tool list: 16 tools from web chat, 6 from the WhatsApp bot, 4
// from direct WhatsApp, and 2-7 from Pulse depending on which check ran.
//
// That is a real bug, not just inconsistency. The coding-agent CLI is launched
// once for the warm session and learns its tool set at launch, so WHICHEVER
// SURFACE HAPPENED TO START THE SESSION silently decided what tools the other
// surfaces got for the rest of that session's life. A parent asking the web UI
// to make an activity could hit a session started by a Pulse browser check,
// where create_learning_activity was never registered — and the failure mode is
// the model claiming it can't do something it normally can, which reads as a
// model problem rather than a wiring one. Handler registration is also
// session-scoped, so a surface's handlers could stay registered after its turn.
//
// The fix is that every parent-scope surface asks for the same manifest here.
// Per-surface DIFFERENCES IN BEHAVIOR stay where they belong — in the system
// prompt / per-check instructions (e.g. a Pulse browser check is told to focus
// only on the saved sites), which is the agentic control this codebase prefers
// over hard tool gating that the shared session can't honor anyway.
//
// Child Mode (child.go) is deliberately NOT part of this: it is a genuinely
// different session (SessionID = the activity dir, not "parent"), a different
// prompt, and a legitimately different, narrower tool set.

// parentToolSinks collects the per-turn recorders parent tools write into.
// Every field is optional — a nil sink is a no-op. This is what lets a surface
// that has nowhere to render suggestions (WhatsApp, Pulse) still expose the
// SAME tool manifest as the web UI: it registers the identical tool and simply
// discards that particular signal, instead of omitting the tool and corrupting
// the shared session's capabilities for everyone else.
type parentToolSinks struct {
	onEvent       func(toolEvent)
	onSuggestions func([]suggestion)
	onSentFile    func(path string)
	onSecretSet   func(name, value string)
}

func (s parentToolSinks) event(ev toolEvent) {
	if s.onEvent != nil {
		s.onEvent(ev)
	}
}

func (s parentToolSinks) suggestions(v []suggestion) {
	if s.onSuggestions != nil {
		s.onSuggestions(v)
	}
}

func (s parentToolSinks) sentFile(path string) {
	if s.onSentFile != nil {
		s.onSentFile(path)
	}
}

func (s parentToolSinks) secretSet(name, value string) {
	if s.onSecretSet != nil {
		s.onSecretSet(name, value)
	}
}

// parentChildLabel is how tool descriptions refer to the child. Shared so every
// surface builds the identical manifest — a differing label would otherwise
// make the "same" tool differ by description text between surfaces.
func parentChildLabel(child *Child) string {
	if child != nil && strings.TrimSpace(child.Name) != "" {
		return child.Name
	}
	return "the child"
}

// parentTools returns the canonical Parent-Mode manifest. Every parent-scope
// surface MUST use this, unmodified, so the shared warm session has one stable
// set of capabilities regardless of which surface started it.
func parentTools(engine, childLabel string, sinks parentToolSinks) []agentsession.Tool {
	return []agentsession.Tool{
		setChildProfileTool(sinks),
		setParentLabelTool(sinks),
		openFileTool(sinks),
		openActivityTool(sinks),
		createLearningActivityTool(childLabel, sinks.event),
		suggestActionsTool(sinks),
		webSearchTool(),
		readImageTool(engine),
		notifyTool(),
		shellTool(),
		diffPatchWorkspaceFileTool(),
		agentBrowserTool(),
		sendWhatsAppFileTool(sinks.sentFile),
		listSecretsTool(),
		setSecretTool(sinks.secretSet),
	}
}

func setChildProfileTool(sinks parentToolSinks) agentsession.Tool {
	return agentsession.Tool{
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
			sinks.event(toolEvent{Tool: "set_child_profile", Name: saved.Name, Grade: saved.Grade, Board: saved.Board})
			return fmt.Sprintf(`{"status":"ok","name":%q,"grade":%q,"board":%q}`, saved.Name, saved.Grade, saved.Board), nil
		},
	}
}

func setParentLabelTool(sinks parentToolSinks) agentsession.Tool {
	return agentsession.Tool{
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
			sinks.event(toolEvent{Tool: "set_parent_label", ParentLabel: label})
			return fmt.Sprintf(`{"status":"ok","label":%q}`, label), nil
		},
	}
}

func openFileTool(sinks parentToolSinks) agentsession.Tool {
	return agentsession.Tool{
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
			sinks.event(toolEvent{Tool: "open_file", Path: p})
			return fmt.Sprintf(`{"status":"ok","opened":%q}`, p), nil
		},
	}
}

func openActivityTool(sinks parentToolSinks) agentsession.Tool {
	return agentsession.Tool{
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
			sinks.event(toolEvent{Tool: "open_activity", Path: dir})
			return fmt.Sprintf(`{"status":"ok","opened":%q}`, dir), nil
		},
	}
}

func suggestActionsTool(sinks parentToolSinks) agentsession.Tool {
	return agentsession.Tool{
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
			sinks.suggestions(out)
			return fmt.Sprintf(`{"status":"ok","count":%d}`, len(out)), nil
		},
	}
}
