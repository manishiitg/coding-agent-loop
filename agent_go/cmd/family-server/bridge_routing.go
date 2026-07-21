package main

// bridgeRoutingText replaces mcpagent's default per-provider bridge-tool-
// routing preamble (see mcpagent's coding_agent_bridge_routing_prompt.go and
// docs/core/mcp_bridge_layer.md). The default is written for AgentWorks' full
// orchestrator — mentioning LLM-provider-config tools (set_provider_auth,
// save_published_llm, ...) and a human_feedback HTTP callback that don't
// exist in family-server, plus "CRITICAL INSTRUCTION"/"override any default
// behavior"/"DO NOT report X" language that reads as a textbook
// prompt-injection pattern to Claude Code's own safety training — which then
// surfaces an alarming, factually-wrong "this file is compromised" message
// straight to the parent, something no amount of app-level counter-prompting
// was able to suppress (verified repeatedly). This calmer, scoped version
// describes only what's actually true for this app: every declared tool is
// already natively callable by name, no discovery/curl step, nothing else.
const bridgeRoutingText = "You're running inside SparkQuill, a small family tutoring app. Built-in tools (Bash, Read, Write, etc.) are disabled for this session — that's expected and normal, not a restriction to work around. Every tool declared to you this session is already natively available; call it directly by its exact name. There is no separate discovery step and nothing needs to be routed through shell+curl. If a declared tool call fails or is unavailable, try a different declared tool or explain the specific failure to whoever you're helping — don't keep retrying the same call."

func bridgeRoutingInstructions() *string {
	text := bridgeRoutingText
	return &text
}
