package virtualtools

import "context"

// SubAgentSpec is the typed contract for spawning a delegated sub-agent via
// the delegate() tool. It replaces the previous set of individual context
// keys (delegation_depth, reasoning_level, agent_template, delegation_servers,
// delegation_skills, share_browser, background_agent_id) that together formed
// an implicit, untraceable spec. One struct under one key means the full
// contract is visible at every set/read site and testable in isolation.
//
// Note: call_sub_agent (the workflow-orchestrator route path) has its own
// separate keys in sub_agent_tools.go and is not covered by this spec.
type SubAgentSpec struct {
	// Depth is the delegation depth of the agent this context belongs to
	// (0 = root). handleDelegate refuses to go past MaxDelegationDepth.
	Depth int

	// ReasoningLevel selects the delegation tier model: "high", "medium",
	// "low", or a custom tier name. Empty means the parent request's model.
	ReasoningLevel string

	// AgentTemplate is the sub-agent template folder name whose instructions
	// are injected into the sub-agent's system prompt.
	AgentTemplate string

	// Servers restricts the sub-agent to specific MCP servers. Empty means
	// inherit all of the parent's servers.
	Servers []string

	// Skills is the explicit-pass skill list: sub-agents inherit NO skills
	// from the parent; the parent must enumerate every skill the sub-agent
	// needs in its delegate(skills=[...]) call.
	Skills []string

	// ShareBrowser controls browser session isolation. true (the default)
	// shares the parent's browser session; false gives the sub-agent an
	// isolated session ID.
	ShareBrowser bool

	// BackgroundAgentID links a background delegation to its registry entry
	// so delegation events and nested spawns can reference the parent agent.
	BackgroundAgentID string
}

type subAgentSpecKeyType struct{}

var subAgentSpecKey subAgentSpecKeyType

// DefaultSubAgentSpec returns the spec for a context with no delegation
// configured: root depth, shared browser, no overrides.
func DefaultSubAgentSpec() SubAgentSpec {
	return SubAgentSpec{ShareBrowser: true}
}

// WithSubAgentSpec returns a context carrying the spec.
func WithSubAgentSpec(ctx context.Context, spec SubAgentSpec) context.Context {
	return context.WithValue(ctx, subAgentSpecKey, spec)
}

// SubAgentSpecFromContext returns the spec carried by ctx, or
// DefaultSubAgentSpec when none is set.
func SubAgentSpecFromContext(ctx context.Context) SubAgentSpec {
	if spec, ok := ctx.Value(subAgentSpecKey).(SubAgentSpec); ok {
		return spec
	}
	return DefaultSubAgentSpec()
}

// WithBackgroundAgentID returns a context whose SubAgentSpec carries the given
// background agent ID, preserving any other spec fields already set. Used by
// background/step spawners that only need to link child events to a parent
// execution.
func WithBackgroundAgentID(ctx context.Context, agentID string) context.Context {
	spec := SubAgentSpecFromContext(ctx)
	spec.BackgroundAgentID = agentID
	return WithSubAgentSpec(ctx, spec)
}
