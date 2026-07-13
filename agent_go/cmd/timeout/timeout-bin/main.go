package main

import (
	"os"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/timeout"
)

func main() {
	if err := timeout.TimeoutCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
