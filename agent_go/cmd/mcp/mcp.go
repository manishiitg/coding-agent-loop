package mcp

import (
	"github.com/spf13/cobra"
)

// MCPCmd represents the mcp command
var MCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP client operations",
	Long:  "Connect to MCP servers and show capabilities",
}

func init() {
	MCPCmd.AddCommand(connectCmd)
	MCPCmd.AddCommand(testAllCmd)
}
