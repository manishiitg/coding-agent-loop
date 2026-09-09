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

// NewMCPServer discovers schemas from the hosted server. It contains no local
// workflow logic or copied plan schemas. Restart the bridge to refresh tools
// after a server upgrade.
func NewMCPServer(ctx context.Context, client ToolCaller) (*server.MCPServer, error) {
	definitions, err := client.Tools(ctx)
	if err != nil {
		return nil, err
	}
	s := server.NewMCPServer("AgentWorks", "1.0.0", server.WithToolCapabilities(false))
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
