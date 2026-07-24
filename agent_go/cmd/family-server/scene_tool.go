package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// sceneMaxLen bounds a single show_scene snippet — small and inline, not a
// full page (that's what an activity's own generated file is for).
const sceneMaxLen = 20000

// childShowSceneTool lets the child tutor render a small, freshly-generated
// HTML visual INLINE in its reply — a story beat, a diagram, a "guess before
// you peek" moment with real choices — instead of only ever pointing at the
// activity's one original static file. Unlike that file, a scene is generated
// fresh every time it's called, so it can match whatever's actually being
// discussed right now (a tangent the child took the conversation into, not
// just what was anticipated when the activity was first created).
func childShowSceneTool(recordScene func(html string)) agentsession.Tool {
	return agentsession.Tool{
		Name: "show_scene",
		Description: "Show a small, self-contained HTML visual INLINE in this reply — a story beat, a diagram, a 'guess before you peek' " +
			"moment, a mini interactive scene. Generate it fresh to match exactly what's happening in the conversation right now — " +
			"not limited to whatever is in the activity's original file, so it can follow the child's own tangents naturally. " +
			"Keep it SMALL (a few lines/cards — not a full page) and self-contained (inline CSS only, no external assets, follow " +
			"skills/_shared/html-design.md's visual style). To offer a real choice, use a button that calls SQ.choose so you actually " +
			"see and respond to whichever one she picks — never a `<details>` reveal or a button that does nothing further: " +
			"`<button onclick=\"parent.postMessage({__sq:1,op:'choose',text:'Investigate Saturn'},'*')\">Investigate Saturn</button>`. " +
			"Call this when a visual moment genuinely helps, not every single turn — plain conversation is fine most of the time.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"html": map[string]interface{}{"type": "string", "description": "the small, self-contained HTML snippet to show inline"},
			},
			"required": []string{"html"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			html := strings.TrimSpace(fmt.Sprint(args["html"]))
			if html == "" {
				return "", fmt.Errorf("html is required")
			}
			if len(html) > sceneMaxLen {
				return "", fmt.Errorf("that's too large for an inline scene (max %d chars) — keep it small, or put full content in the activity's own file instead", sceneMaxLen)
			}
			if recordScene != nil {
				recordScene(html)
			}
			return `{"status":"ok"}`, nil
		},
	}
}
