package mcpagent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// BridgeToolDef is the serialized tool definition for the MCP bridge binary.
type BridgeToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Server      string          `json:"server,omitempty"` // MCP server name (empty for custom/virtual)
	Type        string          `json:"type"`             // "mcp", "custom", or "virtual"
}

// bridgeTools is the explicit list of tools exposed through the Claude Code MCP
// bridge. Claude Code discovers all other MCP tools via get_api_spec and calls
// them through execute_shell_command.
var bridgeTools = []struct {
	name     string
	toolType string // "custom" or "virtual"
}{
	{"execute_shell_command", "custom"},
	{"agent_browser", "custom"},
	{"get_api_spec", "virtual"},
}

// BuildBridgeMCPConfig creates an MCP config that launches the mcpbridge binary
// as a stdio server, forwarding tool calls to the HTTP API endpoints.
// This is used by Claude Code to access MCP tools natively via the bridge.
//
// The bridge exposes exactly 3 tools: execute_shell_command, agent_browser, and
// get_api_spec. All other MCP tools are discovered via get_api_spec and called
// through HTTP API endpoints using execute_shell_command.
func (a *Agent) BuildBridgeMCPConfig() (string, error) {
	logger := getLogger(a)

	// 1. Resolve bridge binary path
	bridgePath := os.Getenv("MCP_BRIDGE_BINARY")
	if bridgePath == "" {
		var err error
		bridgePath, err = exec.LookPath("mcpbridge")
		if err != nil {
			// Fallback: check ~/go/bin/mcpbridge (common Go install location)
			if home, hErr := os.UserHomeDir(); hErr == nil {
				candidate := home + "/go/bin/mcpbridge"
				if _, sErr := os.Stat(candidate); sErr == nil {
					bridgePath = candidate
				}
			}
			if bridgePath == "" {
				return "", fmt.Errorf("mcpbridge binary not found in PATH or ~/go/bin/ (set MCP_BRIDGE_BINARY env var): %w", err)
			}
		}
	}

	// 2. Collect the 3 bridge tools by name
	var toolDefs []BridgeToolDef
	for _, want := range bridgeTools {
		def := a.lookupBridgeTool(want.name, want.toolType, logger)
		if def != nil {
			toolDefs = append(toolDefs, *def)
		} else {
			logger.Warn("Bridge tool not found — skipping",
				loggerv2.String("tool", want.name),
				loggerv2.String("type", want.toolType))
		}
	}

	// 3. Resolve API URL and token for the bridge process
	// The bridge binary runs on the host (not in Docker), so it needs a host-reachable URL.
	// MCP_BRIDGE_API_URL overrides MCP_API_URL for this purpose.
	apiURL := os.Getenv("MCP_BRIDGE_API_URL")
	if apiURL == "" {
		apiURL = a.APIBaseURL
	}
	if apiURL == "" {
		apiURL = os.Getenv("MCP_API_URL")
	}
	apiToken := a.APIToken
	if apiToken == "" {
		apiToken = os.Getenv("MCP_API_TOKEN")
	}
	if apiURL == "" {
		return "", fmt.Errorf("API base URL not configured (set APIBaseURL or MCP_API_URL)")
	}
	if apiToken == "" {
		return "", fmt.Errorf("API token not configured (set APIToken or MCP_API_TOKEN)")
	}

	// Embed session_id in the API URL path for the bridge.
	// The server registers session-scoped routes at /s/{session_id}/tools/...
	// so all bridge tool calls automatically include the session context.
	if a.SessionID != "" {
		apiURL = apiURL + "/s/" + a.SessionID
		logger.Info("Bridge API URL includes session prefix",
			loggerv2.String("session_id", a.SessionID))
	}

	// 4. Build MCP config JSON
	toolsJSON, err := json.Marshal(toolDefs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool definitions: %w", err)
	}

	config := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"api-bridge": map[string]interface{}{
				"command": bridgePath,
				"args":    []string{},
				"env": map[string]string{
					"MCP_API_URL":   apiURL,
					"MCP_API_TOKEN": apiToken,
					"MCP_TOOLS":     string(toolsJSON),
				},
				"trust": true, // Auto-trust: bypass confirmation dialogs for non-interactive usage
			},
		},
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	logger.Info("Built bridge MCP config",
		loggerv2.Int("tool_count", len(toolDefs)),
		loggerv2.String("bridge_path", bridgePath))

	return string(configJSON), nil
}

// lookupBridgeTool finds a tool by name from the agent's registered tools.
// "custom" tools are in a.customTools, "virtual" tools are in a.Tools.
func (a *Agent) lookupBridgeTool(name, toolType string, logger loggerv2.Logger) *BridgeToolDef {
	switch toolType {
	case "custom":
		ct, ok := a.customTools[name]
		if !ok {
			return nil
		}
		description := ""
		var parameters interface{}
		if ct.Definition.Function != nil {
			description = ct.Definition.Function.Description
			parameters = ct.Definition.Function.Parameters
		}
		return marshalBridgeToolDef(name, description, parameters, toolType, logger)

	case "virtual":
		for _, tool := range a.Tools {
			if tool.Function != nil && tool.Function.Name == name {
				return marshalBridgeToolDef(name, tool.Function.Description, tool.Function.Parameters, toolType, logger)
			}
		}
		return nil

	default:
		return nil
	}
}

// marshalBridgeToolDef creates a BridgeToolDef, marshalling parameters to JSON.
func marshalBridgeToolDef(name, description string, parameters interface{}, toolType string, logger loggerv2.Logger) *BridgeToolDef {
	var inputSchema json.RawMessage
	if parameters != nil {
		schemaBytes, err := json.Marshal(parameters)
		if err != nil {
			logger.Warn("Failed to marshal tool parameters, skipping",
				loggerv2.String("tool", name), loggerv2.Error(err))
			return nil
		}
		inputSchema = schemaBytes
	}
	return &BridgeToolDef{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Type:        toolType,
	}
}
