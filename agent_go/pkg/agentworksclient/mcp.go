package agentworksclient

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type ToolCaller interface {
	Tools(context.Context) ([]Tool, error)
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
}

// MCPInstructions are delivered to every MCP client in the initialize
// response. Skill loading is host-dependent and not guaranteed, so these
// instructions — not an installable skill file — are the reliable channel
// telling the agent how to obtain AgentWorks guidance.
const MCPInstructions = `You are connected to an AgentWorks server. Discover workflow IDs with list_workflows first; IDs are never filesystem paths. ` +
	`Before plan changes, call get_agent_context (with action plan_change) and load only the guidance topics relevant to the task via list_guidance_topics/get_guidance_topic. ` +
	`Plan and file mutations are revision-checked: re-read on revision_conflict and never blind-retry. ` +
	`Every plan mutation returns required_followups; complete them before treating the change as done. ` +
	`For planning work that depends on AgentWorks conventions, prefer builder_chat; direct plan tools are structurally safe but carry no decision process.`

// NewMCPServer discovers schemas from the hosted server. It contains no local
// workflow logic or copied plan schemas. Restart the bridge to refresh tools
// after a server upgrade.
func NewMCPServer(ctx context.Context, client ToolCaller) (*server.MCPServer, error) {
	definitions, err := client.Tools(ctx)
	if err != nil {
		return nil, err
	}
	s := server.NewMCPServer("AgentWorks", "1.0.0", server.WithToolCapabilities(false), server.WithInstructions(MCPInstructions))
	for _, definition := range definitions {
		name := definition.Name
		s.AddTool(mcp.NewToolWithRawSchema(name, definition.Description, definition.InputSchema), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			arguments := request.GetArguments()
			// Preserve integers from the wire instead of round-tripping through
			// float64 (for example numeric external IDs greater than 2^53).
			if len(request.Params.RawArguments) > 0 {
				decoder := json.NewDecoder(bytes.NewReader(request.Params.RawArguments))
				decoder.UseNumber()
				if err := decoder.Decode(&arguments); err != nil || arguments == nil {
					return mcp.NewToolResultError("tool arguments must be a JSON object"), nil
				}
			}
			result, err := client.Call(ctx, name, arguments)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultStructured(json.RawMessage(result), string(result)), nil
		})
	}
	return s, nil
}
