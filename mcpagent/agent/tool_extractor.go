package mcpagent

import (
	"fmt"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// SummarizeTools returns a human-readable summary string of the available tools.
// The summary lists each tool's name, description, and function signature (parameters).
func SummarizeTools(tools []llmtypes.Tool) string {
	if len(tools) == 0 {
		return "No tools available"
	}

	var sb strings.Builder
	for i, t := range tools {
		if t.Function == nil {
			continue
		}
		name := t.Function.Name
		desc := strings.TrimSpace(t.Function.Description)
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		sb.WriteString(fmt.Sprintf("Tool: %s\n  Description: %s\n", name, desc))
		params, required := extractParamsAndRequired(t.Function.Parameters)
		if len(params) == 0 {
			sb.WriteString("  Parameters: none\n")
		} else {
			sb.WriteString("  Parameters:\n")
			for _, p := range params {
				req := "optional"
				if required[p.name] {
					req = "required"
				}
				typeStr := p.typ
				sb.WriteString(fmt.Sprintf("    - %s (%s, %s)\n", p.name, typeStr, req))
			}
		}
		if i < len(tools)-1 {
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

type paramInfo struct {
	name string
	typ  string
}

// extractParamsAndRequired parses the parameters (JSON Schema) and returns paramInfo and required set.
func extractParamsAndRequired(params any) ([]paramInfo, map[string]bool) {
	m, ok := params.(map[string]any)
	if !ok {
		return nil, nil
	}
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return nil, nil
	}
	requiredSet := map[string]bool{}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}
	var paramsList []paramInfo
	for k, v := range props {
		ptype := "unknown"
		if vmap, ok := v.(map[string]any); ok {
			if t, ok := vmap["type"].(string); ok {
				ptype = t
			}
		}
		paramsList = append(paramsList, paramInfo{name: k, typ: ptype})
	}
	return paramsList, requiredSet
}
