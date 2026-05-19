// Package guidance owns the canonical guided-flow text for every workflow
// slash command in the workshop UI. The text lives as embedded markdown
// templates so it can be rendered with focus/iteration/run_folder params and
// returned to the agent — the agent then follows the rendered prose verbatim.
// Focus is the conversation-derived instruction or context that caused the
// command, not just a narrow keyword. Slash-command wrappers should pass the
// user's surrounding/preceding request into focus when available.
//
// The same prose is reachable from three contexts:
//
//  1. A user typed a slash command. The slash command's frontend onSubmit
//     collapses to one line: "Call get_workflow_command_guidance(kind=<name>,
//     ...) and follow the returned instructions."
//  2. A user described the same intent in chat ("help me improve this
//     workflow"). The agent recognizes the intent and calls the tool.
//  3. A scheduled fire (e.g. /auto-improve's recurring optimizer
//     message) calls the tool to get the same canonical flow.
//
// One source of truth, three callers.
package guidance

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

//go:embed templates/builder/*.md templates/review/*.md templates/improve/*.md templates/report/*.md templates/kb/*.md templates/learning/*.md templates/db/*.md
var templatesFS embed.FS

// kindMeta captures everything we know about a guided flow at registration
// time: which file holds its template, which workshop modes are allowed to
// invoke it, and a one-line description used in the tool-list enum.
type kindMeta struct {
	Group       string // builder | review | improve | report | kb | learning | db
	Description string // shown to the agent in the kind enum
	Modes       []string
}

// allKinds is the canonical registry of guided flows. Adding a new kind:
//
//  1. Drop a markdown file in templates/<group>/<kind>.md.
//  2. Add an entry here with description + allowed modes.
//  3. Update the slash command's onSubmit to call this tool with the new kind.
//
// Modes match interactive_workshop_manager.go's switch ("builder",
// "optimizer", "run", "reporting"). The tool refuses kinds not allowed in
// the caller's mode and tells the agent to suggest a mode switch.
var allKinds = map[string]kindMeta{
	// Builder-mode audits
	"design-flow":       {Group: "builder", Description: "Inspect context dependency / handoff design between steps", Modes: []string{"builder"}},
	"ready-to-optimize": {Group: "builder", Description: "Pre-optimizer readiness checklist (objective, success criteria, runs, validation, etc.)", Modes: []string{"builder"}},

	// Reviews — recommend, don't apply; appends to builder/review.md
	"review-plan":           {Group: "review", Description: "Comprehensive workflow audit: plan, step descriptions, learnings, KB, db/*.json, reports, variables, and eval wiring", Modes: []string{"builder", "optimizer", "run"}},
	"review-speed":          {Group: "review", Description: "Latency analysis with safe-speedup recommendations", Modes: []string{"optimizer"}},
	"review-cost":           {Group: "review", Description: "Cost analysis with safe-reduction recommendations", Modes: []string{"optimizer"}},
	"review-code":           {Group: "review", Description: "Saved main.py vs current step descriptions drift check", Modes: []string{"optimizer"}},
	"review-artifact-drift": {Group: "review", Description: "Audit plan changelog entries against dependent artifacts: learnings, main.py, KB, db, reports, and eval wiring", Modes: []string{"builder", "optimizer"}},

	// Knowledgebase maintenance — applies targeted or cross-step KB cleanup
	"improve-knowledge": {Group: "kb", Description: "Improve knowledgebase/notes with targeted cleanup or cross-step consolidation", Modes: []string{"builder", "optimizer"}},

	// Learning maintenance — applies targeted/cross-step cleanup to learnings/_global
	"improve-learnings": {Group: "learning", Description: "Improve learnings/_global with targeted cleanup or current-plan consolidation", Modes: []string{"builder", "optimizer"}},

	// DB maintenance — applies guarded schema/contract cleanup to db/*.json
	"improve-data": {Group: "db", Description: "Improve db/*.json contracts, schemas, and report compatibility", Modes: []string{"builder", "optimizer"}},

	// Improvements — metric-driven harden/replan flows
	"define-success":     {Group: "improve", Description: "One-time bootstrap of optimization success criteria (Workflow Profile + metrics)", Modes: []string{"optimizer"}},
	"improve-workflow":   {Group: "improve", Description: "Unified metric-driven workflow improvement: harden or replan from run/eval evidence", Modes: []string{"optimizer"}},
	"improve-evaluation": {Group: "improve", Description: "Evaluation plan changes and metric-source health checks", Modes: []string{"optimizer"}},
	"auto-improve":       {Group: "improve", Description: "Set up recurring run + optimizer schedules", Modes: []string{"optimizer"}},
	"improve-report":     {Group: "report", Description: "Report layout / color / density improvements", Modes: []string{"builder", "optimizer", "reporting"}},
}

// tmplData is the typed context passed to every guidance template. Focus is
// the conversation-derived instruction/context for this command. New fields
// require updating the markdown templates that consume them.
type tmplData struct {
	Focus        string
	Iteration    string
	RunFolder    string
	WorkshopMode string
}

const pathDisciplineGuidance = `PATH DISCIPLINE
Use absolute workspace paths for shell commands when the prompt or env exposes them (` + "`AbsWorkspacePath`" + `, ` + "`VAR_WORKSPACE_PATH`" + `, ` + "`STEP_OUTPUT_DIR`" + `, ` + "`STEP_EXECUTION_DIR`" + `). For file tools that expect workspace paths, use workflow-root-qualified paths. If the current workflow path is ` + "`Workflow/<name>`" + `, read ` + "`Workflow/<name>/runs/...`" + `, ` + "`Workflow/<name>/evaluation/...`" + `, ` + "`Workflow/<name>/planning/...`" + `, and ` + "`Workflow/<name>/db/...`" + ` rather than bare ` + "`runs/...`" + `, ` + "`evaluation/...`" + `, ` + "`planning/...`" + `, or ` + "`db/...`" + `. Bare examples in this guidance are shorthand for paths under the current workflow root. Do not use host paths outside workspace-docs.

`

// renderKind loads templates/<group>/<kind>.md, renders it with the supplied
// params, and returns the rendered text. Returns an error if the kind isn't
// known or its template is malformed.
func renderKind(kind string, data tmplData) (string, error) {
	meta, ok := allKinds[kind]
	if !ok {
		return "", fmt.Errorf("unknown kind %q", kind)
	}
	rel := path.Join("templates", meta.Group, kind+".md")
	body, err := templatesFS.ReadFile(rel)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", rel, err)
	}
	tmpl, err := template.New(kind).Parse(string(body))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", rel, err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", rel, err)
	}
	return pathDisciplineGuidance + strings.TrimRight(buf.String(), "\n") + "\n", nil
}

// modeAllowed reports whether a kind can be invoked from a given workshop
// mode. The caller passes their current mode (builder / optimizer / run /
// reporting); the kind's allow-list is checked.
func modeAllowed(kind, mode string) bool {
	meta, ok := allKinds[kind]
	if !ok {
		return false
	}
	for _, m := range meta.Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// kindEnum returns sorted kind names — used to populate the tool schema's
// enum and for diagnostic error messages.
func kindEnum() []string {
	out := make([]string, 0, len(allKinds))
	for k := range allKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// kindEnumWithDescriptions formats the kind list for the tool description so
// the agent can see, in one place, every guided flow available to it.
func kindEnumWithDescriptions() string {
	type row struct {
		k     string
		d     string
		modes []string
	}
	rows := make([]row, 0, len(allKinds))
	for k, v := range allKinds {
		rows = append(rows, row{k: k, d: v.Description, modes: v.Modes})
	}
	sort.Slice(rows, func(i, j int) bool {
		gi, gj := allKinds[rows[i].k].Group, allKinds[rows[j].k].Group
		if gi != gj {
			return gi < gj
		}
		return rows[i].k < rows[j].k
	})
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %s — %s [modes: %s]\n", r.k, r.d, strings.Join(r.modes, ", "))
	}
	return b.String()
}

// RegisterGuidanceTool exposes get_workflow_command_guidance to the agent.
// The tool returns the rendered prompt for any kind in allKinds. Mode is
// validated against the kind's allow-list — calling a kind from the wrong
// mode returns an error message instructing the agent to suggest a mode
// switch.
func RegisterGuidanceTool(agent *mcpagent.Agent, currentMode string, logger loggerv2.Logger) {
	desc := "Get the canonical guided-flow text for any workflow command. " +
		"Call this tool — and follow the returned instructions verbatim — when (1) the user invokes a slash command " +
		"like /improve-workflow or /review-plan (the slash command will name the kind to pass; pass the surrounding " +
		"conversation/request into focus when available), (2) the user describes " +
		"the same intent in plain chat (\"help me improve this workflow\", \"review whether the goal is being met\", " +
		"\"improve the eval plan\") — recognize the intent and pick the matching kind, or (3) you're running on a " +
		"schedule and the message names a kind. The returned text is your instructions for this turn — do not paraphrase " +
		"or skip steps. Available kinds:\n" + kindEnumWithDescriptions() +
		"Mode validation: each kind is gated to specific workshop modes. If the user's request matches a kind not allowed " +
		"in the current mode, tell the user the mode they need to switch to instead of calling the tool."

	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind": map[string]interface{}{
				"type":        "string",
				"enum":        kindEnum(),
				"description": "The guided-flow to render. See the tool description for the full list of kinds and their per-mode availability.",
			},
			"focus": map[string]interface{}{
				"type":        "string",
				"description": "Optional but strongly recommended. The conversation-derived instruction/context for this command: include the user's recent request, constraints, examples, or focus area that led to the slash command. This is how a slash command carries 'based on the conversation that just happened' into the canonical guidance.",
			},
			"iteration": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Run iteration to use as evidence (e.g. \"iteration-3\"). When set, templates that take an iteration use it as the starting evidence set.",
			},
			"run_folder": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Full run folder path (e.g. \"iteration-3/group-a\"). Used by review-speed / review-cost / improve-evaluation-style flows that anchor on a specific run.",
			},
		},
		"required": []string{"kind"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		kind, _ := args["kind"].(string)
		focus, _ := args["focus"].(string)
		iteration, _ := args["iteration"].(string)
		runFolder, _ := args["run_folder"].(string)

		if _, ok := allKinds[kind]; !ok {
			return fmt.Sprintf("error: unknown kind %q. Valid kinds: %s", kind, strings.Join(kindEnum(), ", ")), nil
		}
		if currentMode != "" && !modeAllowed(kind, currentMode) {
			meta := allKinds[kind]
			return fmt.Sprintf(
				"error: kind %q is not available in mode %q. It runs in: %s. Tell the user they need to switch workshop mode before this command can run.",
				kind, currentMode, strings.Join(meta.Modes, ", "),
			), nil
		}

		text, err := renderKind(kind, tmplData{
			Focus:        strings.TrimSpace(focus),
			Iteration:    strings.TrimSpace(iteration),
			RunFolder:    strings.TrimSpace(runFolder),
			WorkshopMode: strings.TrimSpace(currentMode),
		})
		if err != nil {
			return fmt.Sprintf("error rendering guidance for %q: %v", kind, err), nil
		}
		// Wrap the rendered guidance in a JSON envelope so the agent sees a
		// stable shape; the actual prose is the `guidance` field.
		envelope, _ := json.MarshalIndent(map[string]interface{}{
			"kind":     kind,
			"guidance": text,
		}, "", "  ")
		return string(envelope), nil
	}

	if err := agent.RegisterCustomTool("get_workflow_command_guidance", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register get_workflow_command_guidance: %v", err))
		}
	}
}
