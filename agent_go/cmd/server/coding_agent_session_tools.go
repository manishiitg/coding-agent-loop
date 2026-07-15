package server

import (
	"context"
	"encoding/json"
	"fmt"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
)

// codingAgentToolExecutors is the executor-map shape used across the coding-agent
// tool factories and folder-guard wrappers.
type codingAgentToolExecutors = map[string]func(ctx context.Context, args map[string]interface{}) (string, error)

// codingToolGuard wraps a set of executors with a folder-access guard. It is
// supplied by the caller as a closure so each path keeps its own guard policy:
// the fresh query handler passes its rich grants (resolvedGrants /
// fileContextWriteFolders), the restore path passes a conservative
// workspace-only guard. The shared registrars never compute guards themselves —
// that keeps the fresh path's write-isolation entirely caller-owned and
// un-weakenable by this code.
type codingToolGuard func(execs codingAgentToolExecutors) codingAgentToolExecutors

// codingToolDecorator optionally rewrites a tool's description and/or executor at
// registration time (e.g. the fresh path's mode-specific description enhancement
// and image-gen runtime override). Return the (possibly unchanged) description
// and executor. nil means "register as-is".
type codingToolDecorator func(toolName, description string, exec func(ctx context.Context, args map[string]interface{}) (string, error)) (string, func(ctx context.Context, args map[string]interface{}) (string, error))

// registerCustomToolFunc matches (*mcpagent.Agent).RegisterCustomTool. Taking it
// as a parameter (rather than the concrete agent) keeps registerCodingToolGroup
// unit-testable without constructing a real agent.
type registerCustomToolFunc = func(name string, description string, parameters map[string]interface{}, exec func(ctx context.Context, args map[string]interface{}) (string, error), category string) error

// registerCodingToolGroup is the single, shared "register these virtual tools" loop
// — the marshal-params → RegisterCustomTool boilerplate that was previously
// copy-pasted across the fresh and restore paths. Tool set, category resolution,
// guard, and any per-tool decoration are all supplied by the caller, so the two
// paths share the mechanism while keeping their legitimately-different inputs.
func registerCodingToolGroup(
	register registerCustomToolFunc,
	tools []llmtypes.Tool,
	execs codingAgentToolExecutors,
	categoryFor func(toolName string) string,
	decorate codingToolDecorator,
) error {
	if register == nil {
		return nil
	}
	for _, tool := range tools {
		if tool.Function == nil {
			continue
		}
		name := tool.Function.Name
		exec, ok := execs[name]
		if !ok {
			continue
		}
		description := tool.Function.Description
		if decorate != nil {
			description, exec = decorate(name, description, exec)
		}
		category := categoryFor(name)
		if category == "" {
			return fmt.Errorf("coding tool %q has no category", name)
		}
		var params map[string]interface{}
		if tool.Function.Parameters != nil {
			if b, err := json.Marshal(tool.Function.Parameters); err == nil {
				_ = json.Unmarshal(b, &params)
			}
		}
		if params == nil {
			params = map[string]interface{}{}
		}
		if err := register(name, description, params, exec, category); err != nil {
			return fmt.Errorf("register coding tool %q: %w", name, err)
		}
	}
	return nil
}

// registerCodingBrowserTools registers the workspace browser tools on a coding
// agent. The fresh query handler and the restore path both call this so
// browser-tool exposure can't drift between them; only the guard closure and the
// caller's gating (fresh: enableBrowserAccess; resume: browser_mode headless/cdp)
// differ.
func registerCodingBrowserTools(ag *mcpagent.Agent, sessionID string, cdpPorts []int, guard codingToolGuard) error {
	if ag == nil {
		return nil
	}
	execs := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, cdpPorts...)
	if guard != nil {
		execs = guard(execs)
	}
	category := virtualtools.GetWorkspaceBrowserToolCategory()
	return registerCodingToolGroup(ag.RegisterCustomTool, virtualtools.CreateWorkspaceBrowserTools(), execs, func(string) string { return category }, nil)
}
