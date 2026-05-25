# Cross-Repo Integration Contract — Canonical Source

> **This is the canonical contract for the 3-repo LLM pipeline.** It supersedes
> the per-repo copies in `multi-llm-provider-go/docs/` and `mcpagent/docs/`,
> which now point here.

```
mcp-agent-builder-go (HTTP API + frontend)      ← YOU ARE HERE
  → mcpagent (orchestrator + agent loop)
    → multi-llm-provider-go (adapter + real CLI)
```

The adapter-level contracts in `multi-llm-provider-go/docs/` prove the
bottom layer (per-provider request/response shape, streaming chunks, etc.).
This document defines what must additionally hold **across the full stack**:
config propagation, event flow, cost tracking, debug visibility, error
handling, cancellation.

---

## Table of Contents

1. [Boundaries](#boundaries)
2. [Integration Contract Areas (IC-1 through IC-10)](#integration-contract-areas)
3. [Cost Tracking Contract](#cost-tracking-contract)
4. [Inspector Debug Contract](#inspector-debug-contract)
5. [Test Matrix](#test-matrix)
6. [Provider Coverage](#provider-coverage)
7. [Priority](#priority)
8. [Existing Coverage](#existing-coverage)
9. [Linked Sub-Repo Contracts](#linked-sub-repo-contracts)

---

## Boundaries

### Boundary 1: mcp-agent-builder-go → mcpagent

The Go API server builds an `LLMAgentConfig` from the HTTP request and passes it
to `agent.NewLLMAgentWrapperWithTrace()`. The wrapper calls
`llm.InitializeLLM()` with an `llm.Config`.

**Config fields that must propagate:**

| Field | Source | Destination |
|---|---|---|
| Provider | `req.LLMConfig.Primary.Provider` | `llm.Config.Provider` |
| ModelID | `req.LLMConfig.Primary.ModelID` | `llm.Config.ModelID` |
| Temperature | `req.Temperature` | `llm.Config.Temperature` |
| API Keys | `config/provider-api-keys.json` | `llm.Config.APIKeys` |
| Fallbacks | `req.LLMConfig.Fallbacks[]` | `llm.Config.FallbackModels` |
| Working Dir | `agentConfig.CodingAgentWorkingDir` | agent option → adapter option |
| Session ID | HTTP `X-Session-ID` | `agentConfig.SessionID` → `WithSessionID()` |
| Transport | `agentConfig.ClaudeCodeTransport` | `llm.Config.ClaudeCodeTransport` |
| **Inspector enabled** | session toggle | scoped `llmtypes.WithInspectorSink` |

### Boundary 2: mcpagent → multi-llm-provider-go

The agent's `executeLLM()` builds provider-specific options and calls
`llmInstance.GenerateContent(ctx, messages, opts...)`.

**Provider-specific option mapping** (in `mcpagent/agent/llm_generation.go`):

| Provider | Adapter options passed |
|---|---|
| claude-code | `WithMCPConfig`, `WithResumeSessionID`, `WithClaudeCodeTools`, `WithClaudeCodeSettings`, `WithMaxTurns`, `WithClaudeCodeEffort` |
| gemini-cli | `WithGeminiProjectSettings`, `WithGeminiAdminPolicyPath`, `WithGeminiWorkingDir`, `WithGeminiProjectDirID`, `WithGeminiResumeSessionID` |
| codex-cli | `WithCodexDisableShellTool`, `WithCodexApprovalPolicy`, `WithCodexConfigOverrides`, `WithCodexResumeSessionID` |
| cursor-cli | `WithCursorMCPConfig`, `WithCursorApproveMCPs`, `WithCursorForce` |
| agy-cli | `WithAgyMCPConfig`, `WithAgyDangerouslySkipPermissions`, `WithAgyWorkingDir`, `WithAgyResumeSessionID`, persistent tmux session options |
| opencode-cli | `opencodecli.WithMCPConfig`, `opencodecli.WithWorkingDir`, `opencodecli.WithOpenCodeModel`, `opencodecli.WithResumeSessionID`, `opencodecli.WithAgent`, `opencodecli.WithWriteProjectInstructionFile` (writes AGENTS.md + `tools` deny block into `opencode.jsonc`) |

**Universal options (all providers):**
- `WithReasoningEffort()`
- `WithThinkingLevel()`
- `WithThinkingBudget()`
- `WithStreamingChan()`
- `WithStopSequences()`, `WithTopP()`, `WithTopK()`
- **`WithInspectorSink()`** (opt-in; nil = no-op)

### Boundary 3: multi-llm-provider-go → CLI / API

Each adapter launches the provider CLI or hits the provider's HTTP API,
parses the response stream, and returns `*llmtypes.ContentResponse`.

This boundary is fully covered by adapter-level e2e tests — see the
sub-repo contracts referenced at the end of this document.

---

## Integration Contract Areas

### IC-1: Config Propagation

Every config field set in the HTTP request must arrive at the adapter's
`GenerateContent` call with the correct value.

**Proof required:** provider→adapter routing, modelID→`--model`,
Temperature, API key, Working dir, Fallback models all propagate.

**Risk areas:** provider enum mapping, cross-provider fallback parsing,
`CodingAgentWorkingDir` not wired through for all providers.

### IC-2: Streaming Chunk Flow

Every `StreamChunk` emitted by the adapter must arrive at the SSE event
stream sent to the frontend.

**Required mappings:**
- `StreamChunkTypeContent` → `StreamingChunkEvent` → SSE `event: chunk`
- `StreamChunkTypeToolCallStart` → `ToolCallStartEvent` → SSE `event: tool_start`
- `StreamChunkTypeToolCallEnd` → `ToolCallEndEvent` → SSE `event: tool_end`
- `StreamChunkTypeReasoning` → forwarded (where applicable)
- `StreamChunkTypeTerminal` → terminal-pane preview (tmux CLIs only)
- Stream channel closed → `StreamingEndEvent` → SSE `event: stream_end`

#### Production-config routing matrix (suppressEvents=true)

The HTTP server wraps the orchestrator's Agent with
`WithGenerationStreamingEvents(false)` (`suppressEvents=true`). Under
that config the routing is NOT uniform — terminal and tool events
bypass the gate. The full matrix:

| Chunk type | StreamingCallback fires | StreamingChunkEvent | ToolCallStart/EndEvent |
|---|---|---|---|
| `Content` | yes | **no** (suppressed) | n/a |
| `Terminal` | yes | **yes** (bypass) | n/a |
| `ToolCallStart` | no | no | **yes** (bypass) |
| `ToolCallEnd` | yes | no | **yes** (bypass) |

The terminal bypass exists so tmux-backed coding-agent panels stay
populated in production. Removing the bypass (e.g. a refactor that
puts terminal chunks under the same `if !suppressEvents` as content)
will blank the terminal pane for every tmux step — and the wrapper
unit suite catches this.

**Test class for this matrix:**
- `agent/llm_generation_streaming_test.go::TestStreamingManagerChunkRoutingMatrixProductionConfig` — pins all 4 chunk types under `suppressEvents=true` in one table test.
- `agent/llm_generation_streaming_test.go::TestStreamingManagerEmitsTerminalChunkEventEvenWhenSuppressEventsTrue` — focused regression for the terminal bypass.
- `agent/llm_generation_streaming_test.go::TestStreamingManagerSuppressesContentChunksWhenSuppressEventsTrue` — pins the content-suppress half of the gate.

**Risk areas:** chunk channel buffer (256) overflow silently drops; goroutine
must drain on early return; tool-call chunks accumulate in `sm.CLIToolCalls`
for multi-turn history. Any change to the production wrapper's
`SuppressGenerationStreamingEvents` flag must run the routing matrix
test above first — it's the cheapest signal that the gate semantics
shifted.

### IC-3: Token Usage & Cost

Token counts from the adapter response must reach the cost ledger and be
available via `GET /api/cost/summary`. **See [Cost Tracking Contract](#cost-tracking-contract) for the detailed contract.**

### IC-4: Session ID & Resume

Session IDs from the adapter response must be stored and passed back on
subsequent turns to resume the conversation.

**Provider-specific session-id keys** (in `GenerationInfo.Additional`):
- `claude_code_session_id`, `gemini_session_id`, `codex_thread_id`,
  `cursor_session_id`, `agy_session_id`, `opencode_session_id`

**Risk:** key drift → mcpagent reads wrong key → resume silently starts a
fresh session.

### IC-5: Model Metadata (Effective Model)

The actual model used (which can drift from the requested alias for
auto-routing/composer/etc.) must be available downstream.

| Provider | Effective-model key on Additional |
|---|---|
| Claude Code | `claude_code_model` |
| Gemini CLI | `gemini_effective_model` (tmux) / `gemini_model` (structured) |
| Codex CLI | `codex_effective_model` |
| Cursor CLI | `cursor_model` |
| OpenCode CLI | `opencode_effective_model` (adapter synthesizes from `--model` flag; cross-checked against `opencode export <id>` info.model.id) |
| Direct API adapters | `cost_model_id` (the canonical key all adapters emit) |

**Risk:** CLI doesn't report → fallback to requested model ID, accept it
may be wrong.

### IC-6: Fallback Chain

When primary model fails, agent must try fallback models in order. Cross-
provider fallback (e.g. `openai/gpt-5.5` → `anthropic/claude-opus-4-7`)
must instantiate a different adapter, not just swap model IDs.

### IC-7: Error Propagation

Adapter errors must reach the frontend as meaningful events. Auth, rate-
limit, and quota failures must classify into distinct error types so the
UI can show actionable messages (not generic "request failed").

### IC-8: Cancellation Propagation

User cancel (SSE disconnect / explicit cancel API) → context cancel →
adapter kills process group → channel drained → `StreamingEndEvent` with
partial content. No goroutine leaks.

### IC-9: Multi-Turn Tool Context

Tool calls from previous turns must be included in the conversation
history for subsequent turns. CLI providers receive tool context as text
(via `convertToolCallsToTextForCLI()`).

### IC-10: MCP Bridge Propagation

`WithMCPConfig(json)` → Claude Code `--mcp-config <file>`
`WithCursorMCPConfig(json)` → Cursor `.cursor/mcp.json`
`WithAgyMCPConfig(json)` → Antigravity `.agents/mcp_config.json`
`WithAgyResumeSessionID(id)` → Antigravity `agy --conversation <id>`
`opencodecli.WithMCPConfig(json)` → OpenCode `opencode.jsonc` `mcp` block
(adapter translates the standard `{"mcpServers":{name:{command:"<exe>",args:[...]}}}`
shape into opencode 1.15.4's required `{"mcp":{name:{command:["<exe>",...]}}}` —
single-array command, no separate `args` field)
MCP tool calls work through the bridge end-to-end.

---

## Cost Tracking Contract

This section is the canonical reference for how USD cost and token
counts flow from a real provider call to the cost dashboard.

### Pipeline

```
real provider call
  → adapter emits cost_usd_estimated + cost_model_id on GenerationInfo.Additional
  → mcpagent builds TokenUsageEvent (copies Additional generically into GenerationInfo)
  → ContextAwareEventBridge intercepts, injects step context + effective model
  → calls TokenPersister with StepTokenData + ModelTokenData (ModelID = effective)
  → costObserver.HandleEvent for the per-session ledger
  → GET /api/cost/summary aggregates by_date, by_model
```

### Cost source per provider

| Provider | Cost source | Field |
|---|---|---|
| Claude Code (structured / --print) | Provider-blessed | `cost_usd` (= `total_cost_usd`) |
| Claude Code (tmux) | Computed | `cost_usd_estimated` |
| Anthropic API | Computed | `cost_usd_estimated` |
| OpenAI API | Computed | `cost_usd_estimated` |
| Vertex (Gemini API) | Computed | `cost_usd_estimated` |
| Codex CLI (structured + tmux) | Computed | `cost_usd_estimated` |
| Gemini CLI (structured) | Computed | `cost_usd_estimated` |
| Cursor CLI (structured) | Computed | `cost_usd_estimated` |
| Cursor CLI (tmux) | **No tokens available** (transcript lacks usage) | — |
| Antigravity CLI (tmux) | Estimated tokens only | `cost_usd_estimated` when rates are available |
| Bedrock / Azure / Z.AI / Kimi / MiniMax | Computed | `cost_usd_estimated` |

### IC-area test coverage (cross-stack)

| IC | Area | Test File | Count |
|----|------|-----------|-------|
| IC-3 | Token usage & cost | `cmd/server/cost_routes_test.go` | 20 subtests |
| IC-7 | SSE error serialization | `cmd/server/sse_test.go` | 4 tests |
| IC-8 | Cancellation (subscriber) | `internal/events/event_store_test.go` | 19 subtests |
| IC-8 | Cancellation (shutdown) | `cmd/server/shutdown_cleanup_test.go` | 2 tests |
| IC-10 | Coding CLI chat contract: multi-turn, literal `@` prompt text, MCP bridge tool completion, live steer, and terminal pane de-dupe | `cmd/testing/coding_agent_chat_e2e.go` | opt-in `coding-agent-chat-e2e` live command |
| IC-10 | Coding CLI → MCP bridge → `agent_browser` CDP mode | `cmd/testing/agent_browse_e2e.go` | opt-in `agent-browse-e2e` live command |
| IC-10 | Parallel coding CLIs → shared `agent_browser` CDP lock | `cmd/testing/agent_browse_stress_e2e.go` | opt-in `agent-browse-stress-e2e` live command |
| IC-10 | Direct mcpbridge-compatible API → shared `agent_browser` CDP lock | `cmd/testing/agent_browse_api_stress_e2e.go` | opt-in `agent-browse-api-stress-e2e` live command |

**Important:** for subscription-billed CLIs (Cursor, Codex Pro), the computed
cost is a **shadow cost** — what the same workload would cost via the
underlying per-token API. Not the user's actual flat-plan bill. The
ledger `Entry.CostUSDSource` field flags this: `"provider"` vs `"estimated"`.

### Required Additional fields per adapter

Every API/CLI adapter that participates in the cost contract MUST emit on
`GenerationInfo.Additional` after a successful call:

- `cost_usd_estimated` (float64) — API-equivalent USD from tokens × rate
- `cost_model_id` (string) — model used for the rate lookup
- Provider-specific effective model key (see IC-5 above)

Claude Code (structured) additionally emits `cost_usd` with the
provider-blessed value.

### Ledger Entry shape

```go
costledger.Entry{
    Timestamp, SessionID, UserID, AgentMode, Component, CorrelationID,
    Provider, ModelID,
    EffectiveModelID,           // from Additional[cost_model_id] (NEW)
    PromptTokens, CompletionTokens, ReasoningTokens,
    CacheReadTokens, CacheWriteTokens,
    TotalCostUSD,
    CostUSDSource,              // "provider" | "estimated" (NEW)
}
```

### Workflow rollup keys

`TokenUsageFile.ByModel[modelID]` is keyed by the **effective model**
(not the requested alias). The bridge resolves this via
`effectiveModelIDFromTokenEvent()` reading the canonical keys in priority
order (see `base_orchestrator_tokens_helpers.go`).

### Cost contract tests

| Layer | Test |
|---|---|
| Adapter unit | `pkg/adapters/utils/cost_test.go` (rate-table math) |
| Adapter real-API | `pkg/adapters/<provider>/<provider>_real_test.go` — `TestXxxRealCostEstimateOnPlainText` |
| Bridge | `pkg/orchestrator/workflow_cost_e2e_real_test.go` — real Anthropic call → bridge with step pushed → persister sees right buckets |
| Ledger | `cmd/server/cost_ledger_e2e_real_test.go` — real call → costObserver → ledger.Summarize() |
| HTTP | `cmd/server/cost_http_e2e_real_test.go` — real call → ledger → `GET /api/cost/summary` |

---

## Inspector Debug Contract

This section is the canonical reference for the opt-in debug panel.

### Design

Inspector events ride a **separate sink** (`llmtypes.InspectorSink`)
attached via `WithInspectorSink()`. They do NOT flow through the chat
content stream. The panel is opt-in: when the sink is nil, adapters
skip emission entirely (single nil-compare, zero alloc).

### Phases (closed set — adding one = breaking change)

| Phase | Emitted | Required metadata |
|---|---|---|
| `request` | once, before HTTP dispatch | `message_count`, plus provider-specific request envelope |
| `event` | per provider stream event | `event_name`; optional `delta_text_length`, `tokens_so_far` |
| `tool_call` | per tool selection | `tool_name`; optional `tool_call_id`, `args_length` |
| `completion` | once, on success | `prompt_tokens`, `completion_tokens`, `stop_reason`, `duration_ms` |
| `error` | once, on failure | `error`, `phase` (where in the call it failed) |

### Pipeline

```
real provider call with InspectorSink wired
  → adapter's InspectorEmitter fires at every phase boundary
  → ScopedInspectorSink injects StepContext (sessionID, step_id, phase, ...)
  → inspector.Store.Sink() forwards into the per-session ring buffer
  → GET /api/inspector/<sessionID>?since=<cursor> serves the timeline
```

### Step attribution

A `StepContext` is attached at the orchestrator layer via a wrapping
`ScopedInspectorSink`. Adapters never touch step context. Chaining is
supported: inner scope wins per-field.

**Required when in-workflow:** `step_id`, `step_type`, `phase`,
`execution_owner_id` (for parallel-batch disambiguation).
**Highly recommended:** `step_name`, `step_index`, `step_started_at`,
`attempt`, `call_purpose`, `agent_name`.

### Stable interface enforcement

The matrix test (`multi-llm-provider-go/inspector_contract_matrix_test.go`)
runs the same assertion against every adapter that registers a factory:

- phase ordering: `request` first, `completion` last (no `error`)
- at least one `event` phase
- monotonic `Seq` per call, restarts at 1
- `Provider` + `Model` set on every event
- required-metadata keys present on `request` and `completion`

**Adding a new adapter to the inspector** = add one line to
`inspectorContractFactories`. If it doesn't honor the contract, the
matrix test fails loudly with which provider broke.

### HTTP surface

| Endpoint | Behavior |
|---|---|
| `GET /api/inspector` | List session IDs currently tracked |
| `GET /api/inspector/{session_id}?since=<n>&max=<m>` | Timeline since cursor |
| `DELETE /api/inspector/{session_id}` | Drop session ring |

`503` when store not initialised; `404` when session unknown.

### Inspector contract tests

| Layer | Test |
|---|---|
| Types + emitter unit | `multi-llm-provider-go/llmtypes/inspector_emitter_test.go` |
| Per-adapter regression | `multi-llm-provider-go/pkg/adapters/anthropic/anthropic_inspector_real_test.go` (more as adapters get wired) |
| Cross-provider matrix | `multi-llm-provider-go/inspector_contract_matrix_test.go` |
| Cross-stack | `mcp-agent-builder-go/agent_go/cmd/server/inspector_e2e_real_test.go` |
| Store unit | `mcp-agent-builder-go/agent_go/internal/inspector/store_test.go` |

---

## Test Matrix

| # | Area | Boundary | Test Location |
|---|---|---|---|
| IC-1 | Config propagation | B1 + B2 | mcpagent |
| IC-2 | Streaming chunk flow | B2 + B1 | mcpagent + mcp-agent-builder-go |
| IC-3 | Token usage & cost | B3 → B1 | adapter + bridge + ledger + HTTP |
| IC-4 | Session ID & resume | B3 → B2 → B3 | mcpagent |
| IC-5 | Model metadata | B3 → B1 | mcpagent + cost ledger (effective) |
| IC-6 | Fallback chain | B2 | mcpagent |
| IC-7 | Error propagation | B3 → B1 | mcpagent + mcp-agent-builder-go |
| IC-8 | Cancellation propagation | B1 → B3 | mcp-agent-builder-go |
| IC-9 | Multi-turn tool context | B2 → B3 | mcpagent |
| IC-10 | MCP bridge propagation | B1 → B3 | mcpagent |
| Cost | Cost emission + bucketing | B3 → B2 → B1 | adapter + bridge + ledger + HTTP |
| Inspector | Debug-event contract | B3 → B2 → B1 (separate sink) | adapter + store + HTTP |

---

## Provider Coverage

Each contract area should be verified for all supported providers.

**Coding agents (tmux transport):**
- claude-code, codex-cli, cursor-cli
- agy-cli is implemented for explicit local/contract runs, but remains hidden
  from published provider lists until its MCP bridge E2E is certified.

**Coding agents (structured JSON transport):**
- gemini-cli, opencode-cli

**API providers (direct HTTP):**
- anthropic, openai, vertex (Gemini API), bedrock, azure, z-ai, kimi, minimax, openrouter

---

## Priority

**P0 (blocks production):**
- IC-2 Streaming chunk flow
- IC-4 Session ID & resume
- IC-8 Cancellation propagation

**P1 (degrades quality):**
- IC-1 Config propagation
- IC-3 Token usage & cost
- IC-7 Error propagation
- **Cost contract** (billing accuracy)

**P2 (important but not urgent):**
- IC-5 Model metadata
- IC-6 Fallback chain
- IC-9 Multi-turn tool context
- IC-10 MCP bridge propagation
- **Inspector contract** (debug-only, opt-in)

---

## Existing Coverage

**Adapter-level (multi-llm-provider-go):**

| Adapter | Real-API tests | Inspector |
|---|---|---|
| anthropic | 14 | ✅ wired |
| openai | 11 | ⏳ pending |
| vertex | 10 | ⏳ pending |
| kimi | 1 (skips without direct moonshot key) | — |
| claudecode / codexcli / geminicli / cursorcli / agycli | structured + tmux transcript/contract tests as supported by each provider | ⏳ pending |
| bedrock / azure / minimax / zai | code-only, no creds | — |

Cross-adapter matrix test: `inspector_contract_matrix_test.go` (currently anthropic only).

**Agent-level (mcpagent):**
- IC-1: `agent/coding_agent_options_test.go` — 6 tests
- IC-2: `agent/llm_generation_streaming_test.go` — 13 tests
- IC-4: `agent/session_resume_integration_test.go` — 18 subtests
- IC-5: `agent/llm_generation_streaming_test.go` — 2 tests
- IC-6: `agent/fallback_parsing_test.go` — 17 subtests
- IC-7: `agent/error_classification_test.go` — 44 subtests
- IC-9: `agent/cli_tool_history_test.go` — 3 tests
- IC-10: `agent/coding_agents_bridge_test.go` — 7 tests

**API-level (mcp-agent-builder-go):**
- IC-3 + Cost: `cmd/server/cost_routes_test.go` (20 subtests), `cost_routes_extract_test.go`, `cost_ledger_e2e_real_test.go`, `cost_http_e2e_real_test.go`, `pkg/orchestrator/workflow_cost_e2e_real_test.go`
- IC-7: `cmd/server/sse_test.go` — 4 tests
- IC-8: `internal/events/event_store_test.go` (19 subtests), `cmd/server/shutdown_cleanup_test.go` (2 tests)
- IC-10 cross-stack: `cmd/testing/coding_agent_chat_e2e.go`, `cmd/testing/agent_browse_*_e2e.go`
- Validate-key cross-stack: `cmd/server/validate_<provider>_real_test.go` (anthropic, openai, vertex)
- Inspector: `cmd/server/inspector_e2e_real_test.go` + `internal/inspector/store_test.go`

---

## Linked Sub-Repo Contracts

These docs live in their respective sub-repos but the **canonical contract is this file**.
Update those sub-repo docs only when the per-repo specifics drift; cross-repo
behavior lives here.

### multi-llm-provider-go

| Document | What it covers |
|---|---|
| `docs/api_provider_test_contract.md` | The 24-area test contract for API adapters (plain text, streaming, tool use, sampling, image, PDF, thinking, JSON, prompt caching, auth, rate limit, ...) |
| `docs/coding_sdk_structured_contract.md` | Per-provider structured (`--print`/JSON) transport contract |
| `docs/coding_sdk_tmux_contract.md` | Per-provider tmux interactive transport contract |
| `docs/CODEX_CLI_CODING_AGENT_CONTRACT.md` | Codex CLI-specific behavior |
| `docs/GEMINI_CLI_CODING_AGENT_CONTRACT.md` | Gemini CLI-specific behavior |

### mcpagent

| Document | What it covers |
|---|---|
| `docs/token-usage-tracking.md` | Token accounting in the agent loop |
| `docs/folder_guard.md` | Workspace isolation contract |
| `docs/llm_resilience.md` | Retry, timeout, classification |
| `docs/smart_routing.md` | Smart-routing model selection |
| `docs/oauth.md` | OAuth flow for provider auth |
| `docs/mcp_cache_system.md` | MCP server connection caching |
| `docs/tool_search_mode.md` | Tool-search agent mode |
| `docs/tool_use_agent.md` | Agentic tool-use loop |

### mcp-agent-builder-go (this repo)

| Document | What it covers |
|---|---|
| `docs/learn_code_flow.md` | Learn-code workflow specifics |
| **`docs/cross_repo_integration_contract.md`** | **This file (canonical)** |

---

## Notes for Future Contributors

- **When adding a new adapter** — register it in:
  1. `inspectorContractFactories` (matrix test in `multi-llm-provider-go`)
  2. The cost contract table above (provider + cost source)
  3. The provider coverage list above
- **When changing the cost ledger schema** — update the [Cost Tracking Contract](#cost-tracking-contract) ledger Entry shape AND the workflow rollup keys.
- **When adding an inspector phase** — that's a breaking change. Bump a contract version, update the assertion in the matrix test, update the Phases table above.
- **When the per-repo doc drifts** — update *this* file too, even if the change is small. Out-of-band updates create confusion later.
