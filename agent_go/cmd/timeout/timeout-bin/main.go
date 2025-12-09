package main

import (
	"os"

	"mcp-agent-builder-go/agent_go/cmd/timeout"
)

func main() {
	if err := timeout.TimeoutCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
