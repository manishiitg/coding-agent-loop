# Coding Agent Continuation Architecture

This note documents the current coding-agent call path and the proposed
provider-neutral continuation model.

The goal is to make workflow, chat, learning, and background-agent code say only:

```text
continue this agent session with this message
```

Provider-specific details such as tmux, Claude Code `--resume`, Codex thread IDs,
Gemini project directories, prompt-draft cleanup, and compaction prompts should
live below the workflow layer.

## Implementation Status

The vertical slice described below is **largely shipped**. Status as of the
latest sync:

| Layer / Step | Status | Notes |
|---|---|---|
| `llmtypes.CodingProviderSessionHandle` | **Done** | `multi-llm-provider-go/llmtypes/coding_provider_session_handle.go:21` |
| Attach/Extract helpers | **Done** | `AttachCodingProviderSessionHandle`, `ExtractCodingProviderSessionHandleFromResponse` |
| Provider adapters emit handle | **Done** | Claude Code, Codex CLI, Gemini CLI all attach |
| `ContinueCodingAgentSession` | **Done** | `multi-llm-provider-go/coding_agent_continuation.go:49`. Wired for Claude Code, Codex CLI, Gemini CLI. Cursor and Pi return `non_continuable` until certified. |
| Typed continuation errors | **Done** | `CodingAgentContinuationError` with kinds `non_applicable`, `non_continuable`, `stale_handle` |
| `mcpagent.AgentSessionHandle` | **Done** | `mcpagent/agent/session_handle.go:14` |
| `Agent.ContinueAgentSession` | **Done** | `session_handle.go:81`; also `ContinueAgentSessionWithHistory` |
| Auto-routing inside `AskWithHistory` | **Done** | `mcpagent/agent/llm_generation.go:1378` — detects coding-agent handle and routes to `ContinueCodingAgentSession` automatically |
| Capture handle into chat history | **Done** | `ChatHistoryAgentRuntime.AgentSessionHandle` populated via `CurrentAgentSessionHandle()` |
| Restore handle on chat resume | **Done** | `ApplyAgentSessionHandle()` called during restore |
| Remove legacy maps in `server.go` | **Done** | `claudeCodeSessionIDs`, `geminiSessionIDs`, `geminiProjectDirIDs`, and `restoreCodingAgentRuntime()` have been removed. Restore now flows through `seedCodingAgentRuntimeFromRestoredConversation()` and `ApplyAgentSessionHandle()`. |
| Workflow step continuation (Step 5) | **Done for builder workflow runtime** | Workflow steps persist `continuation_state.json` beside step logs with per-phase `AgentSessionHandle`s and phase statuses. Workshop session startup scans durable state and requeues incomplete post-step phases through the normal workshop execution notifier path. |
| Learning / KB review continuation (Step 6) | **Done for builder workflow runtime** | Direct KB review, direct learning, agent-mode learning, and KB update phases record lock-wait/running/completed state. Restart replay resumes or requeues the incomplete phase with the stored phase handle. |
| Kill-tmux recovery E2E test | **Done for Claude Code + Codex CLI chat** | Provider E2E verifies Claude Code and Codex CLI. Builder chat E2E `--run-tmux-loss-resume` has passed live for Claude Code and Codex CLI. |

Day-to-day callers do not need to choose between `AskWithHistory` and
`ContinueAgentSession`. The auto-routing in mcpagent decides based on the
agent's current `CodingProviderSessionHandle`. The explicit
`ContinueAgentSession` is needed only when a caller is *applying* a handle
from external persistence (e.g., chat history restore on builder restart).

## Current Architecture

Normal workflow and chat execution currently goes through `mcpagent`:

```text
mcp-agent-builder-go
  workflow/chat/server code
    -> creates mcpagent.Agent with provider-specific options
    -> calls agent.AskWithHistory(...)
      -> mcpagent runs history/tool loop
        -> mcpagent LLM layer calls multi-llm-provider-go GenerateContent(...)
          -> provider adapter
            -> Claude Code / Codex CLI / Gemini CLI / API
```

So workflow does not directly call the low-level provider adapter for normal
step execution. The problem is that transport configuration and continuation
state still leak upward.

Examples of leaked configuration/state:

```text
mcpagent.WithClaudeCodePersistentInteractiveSession(...)
mcpagent.WithCodexPersistentInteractiveSession(...)
mcpagent.WithGeminiPersistentInteractiveSession(...)
mcpagent.WithForceStructuredCodingAgent(...)
mcpagent.WithCodingAgentWorkingDir(...)

Claude resume id
Codex thread id
Gemini session id + project dir id
tmux session name
terminal owner ids
```

A handle-like struct already exists in the builder:
`ChatHistoryAgentRuntime` in `chat_history_persistence.go` stores
`AgentSessionHandle` plus compatibility fields such as `ExternalSessionID`,
`ProjectDirID`, `ResumeFlag`, `ResumeSupported`, `WorkspacePath`, and
`Provider`. The compatibility fields allow old history files to restore into
the new handle path.

The migration should not move builder-specific history state into
`multi-llm-provider-go`. Instead:

- `CodingProviderSessionHandle` should contain only provider transport state.
- `mcpagent.AgentSessionHandle` should contain agent/session state plus the
  provider handle.
- builder chat/workflow persistence should store the opaque
  `AgentSessionHandle` and can remove or alias `ChatHistoryAgentRuntime` once
  all call sites are migrated.

This makes recovery fragile when:

- a tmux pane disappears
- a provider returns to idle but the workflow is waiting on post-step work
- learning or KB review needs to send another turn after a delay
- a user sends a chat message after the interactive CLI session has been closed
- a workflow step is re-entered and needs to continue the same provider-native
  conversation

## Proposed Architecture

The proposed architecture keeps provider behavior sealed below `mcpagent`.
The primary integration is **auto-routing inside the normal generation path**,
not an explicit continuation call at the workflow/chat site:

```text
mcp-agent-builder-go
  workflow/chat/server code
    -> agent.AskWithHistory(messages)            // unchanged call site
      -> mcpagent inspects a.CodingProviderSessionHandle
         - empty handle → llmInstance.GenerateContent(ctx, messages, opts)
         - present handle → llm.ContinueCodingAgentSession(
                              ctx, llmInstance, handle, latestMessage, opts)
        -> multi-llm-provider-go ContinueCodingAgentSession(providerHandle, message)
          -> provider adapter decides:
             - paste into existing tmux
             - start new tmux with native resume
             - run structured JSON resume
             - fail with typed non-continuable error
```

The explicit `mcpagent.ContinueAgentSession(handle, message)` method still
exists for callers that want to apply a handle from external persistence
(e.g., chat history restore on builder restart). It is a wrapper that calls
`ApplyAgentSessionHandle` + `AskWithHistory` and returns the refreshed handle.
But day-to-day workflow/chat code does not need to choose between
`AskWithHistory` and `ContinueAgentSession` — the routing decision lives
inside mcpagent.

The workflow/backend should store and pass an opaque `AgentSessionHandle`.
It should not know which provider fields are meaningful.

Conceptually:

```go
type AgentSessionHandle struct {
    AgentID      string
    SessionID    string
    OwnerID      string // builder-chosen logical owner, opaque to mcpagent
    Scope        string // chat, workflow_step, background_agent, etc.
    CorrelationID string
    Provider     CodingProviderSessionHandle
}

type CodingProviderSessionHandle struct {
    Provider        string // claude-code, codex-cli, gemini-cli, cursor-cli, etc.
    Transport       string // tmux | structured | api
    NativeSessionID string // Claude resume id, Codex thread id, Gemini session id
    TmuxSession     string // empty for structured and api transports
    WorkingDir      string
    ProjectDirID    string // Gemini-style project/session isolation
    Model           string
    Status          string // active, idle, lost, closed
}
```

The exact type names may differ, but the boundary should remain the same:

```text
builder owns workflow/chat identity and durable work state
mcpagent owns generic agent-session continuation
multi-llm-provider-go owns provider transport mechanics
```

If the builder needs workflow-specific fields such as `WorkflowID`, `StepID`,
`Iteration`, or `GroupName`, those should live in builder persistence next to
the opaque `AgentSessionHandle`, not inside the mcpagent handle itself.

## Current vs Proposed

### Current

```text
workflow/backend:
  "This is Claude Code. Keep tmux alive. Use this working directory.
   Maybe resume with this Claude id. If tmux exists, send input there."
```

Problems:

- continuation state is scattered
- tmux is too close to workflow logic
- terminal state can be mistaken for execution truth
- provider-specific recovery is duplicated or inferred
- new providers must rediscover the same edge cases

### Proposed

```text
workflow/backend:
  "Run agent loop with these messages." (same call site as before)

mcpagent:
  "Agent has a coding-agent handle. Route latest message through
   ContinueCodingAgentSession instead of GenerateContent."

provider:
  "H is Claude Code tmux. The tmux pane is gone. Native resume id exists.
   Start a new tmux using claude --resume, send M, and return updated handle."
```

Benefits:

- workflow code is provider-neutral
- tmux loss is recoverable by the provider adapter
- terminal panes become observations, not source of truth
- continuation behavior can be certified per provider
- Cursor/Pi or future providers implement one contract

## Transport Types

There are three distinct transport types. The agent-level continuation contract
must define behavior for all three, but provider-native coding-agent
continuation only applies to coding-agent transports:

| Transport | Examples | Continuation mechanism |
|---|---|---|
| **tmux** | Claude Code tmux, Codex CLI tmux, Gemini CLI tmux, Cursor CLI | Paste into existing pane if alive; restart with native `--resume` if pane is gone |
| **structured CLI** | Codex CLI `exec resume --json`, Gemini structured mode | Pass resume/thread ID as a flag; no tmux involved |
| **API provider** | OpenAI, Anthropic API, Gemini API | Continue through mcpagent-managed message history; no provider-native `ContinueCodingAgentSession` |

The `CodingProviderSessionHandle.Transport` field encodes which mode is active
for a given session. Provider adapters inspect this field to decide the
continuation path. The workflow layer must not inspect it.

For structured CLI providers the "tmux loss" recovery path does not apply —
structured continuation is already tmux-free and only needs the thread/session
ID in the handle.

Calling the low-level `ContinueCodingAgentSession` on an API-only provider
should return a typed non-applicable error. Calling the higher-level
`mcpagent.ContinueAgentSession` on an API provider may still be valid because
mcpagent can continue using its own conversation history.

This means there are two related, but different, continuation APIs:

| API | Owner | Applies to | Behavior |
|---|---|---|---|
| `ContinueCodingAgentSession` | `multi-llm-provider-go` | Provider-native coding-agent sessions | Resumes/pastes/restarts using provider-native state; API-only providers return typed non-applicable error |
| `ContinueAgentSession` | `mcpagent` | Any resumable agent session | Uses provider-native continuation when available, or mcpagent-managed history for normal API providers |

Builder code should call only the `mcpagent` API. It should never call the
provider-native API directly.

## Layer Responsibilities

### multi-llm-provider-go

Owns the hard transport contract.

Responsibilities:

- define the low-level provider session handle
- expose provider-neutral continuation entry point
- implement provider-specific resume/paste/restart behavior
- emit lifecycle events for continuation
- run provider E2E certification tests

Expected lifecycle events:

```text
resuming
running
completed
failed
lost_tmux
resume_unavailable
stale_handle
```

Provider lifecycle events should describe provider transport state only. Workflow
events such as `waiting_for_learning_lock`, `pre_validation_passed`, and
`kb_review_completed` belong in `mcp-agent-builder-go`, because the provider
does not know why the caller delayed the next continuation turn.

Provider rules:

- If tmux exists and is idle, paste into the existing tmux session.
- If tmux is gone but native resume state exists, create a new provider session
  using native resume.
- If no native resume state exists, return a typed non-continuable error.
- Continuation sends only the latest user message when native resume is active.
- Provider owns prompt-draft cleanup, compaction prompts, final response
  extraction, and completion detection.

### mcpagent

Owns the agent-level session abstraction.

Responsibilities:

- store/update provider handles after `AskWithHistory`
- expose `ContinueAgentSession` or equivalent
- preserve agent-level configuration across continuation
- route continuation to `multi-llm-provider-go`
- keep normal API providers working without coding-agent-specific behavior
- provide agent-level tests for continuation after provider tmux loss

This is the correct boundary for workflow callers. Workflow should talk to
`mcpagent`, not directly to `multi-llm-provider-go`.

### mcp-agent-builder-go

Owns product/workflow intent.

Responsibilities:

- persist the opaque `AgentSessionHandle`
- call `mcpagent` to start or continue an agent session
- stop storing provider-specific resume maps in workflow/server code
- treat terminal panes as UI/debug artifacts
- group old/new terminal panes under the same logical agent execution
- use the same continuation path for:
  - chat turns
  - workflow steps
  - background agents
  - post-validation learning turns
  - KB review turns
  - auto-notification turns

## Hard Invariants

- Workflow code must not construct provider-specific resume flags.
- Workflow code must not depend on tmux session names for continuation.
- Provider adapters must return/update a continuation handle after each turn.
- Continuation must send only the new user message when provider-native resume is
  active.
- One active continuation per handle is allowed; concurrent messages must queue
  or fail clearly.
- Working directory, MCP config, system prompt, model, and tool policy must be
  stable across continuation. If any of these changed between the original turn
  and the continuation, return a typed `StaleHandleError` naming the specific
  field that changed. The caller decides whether to restart or surface the error.
  Do not silently fall back to a fresh session.
- Terminal output is not the source of truth for agent completion.
- If tmux is lost but provider-native resume exists, continuation should recover.
- If recovery is impossible, return a typed error that the UI can display.
- Calling the provider-level `ContinueCodingAgentSession` on a non-coding-agent
  provider must return a typed error immediately. It must not silently succeed
  or no-op.
- Calling the mcpagent-level `ContinueAgentSession` on an API-backed agent may
  continue through mcpagent-managed message history; it should only error when
  the agent/session itself is not continuable.

## Required Tests

### Certification IDs

The existing certification registry at
`multi-llm-provider-go/coding_agent_certification.go` enforces a hard gate:
any capability claim must have a registered certification or
`TestClaudeAndCodexCapabilityClaimsHaveRegisteredCertification` will fail.

New certification IDs that must be added before continuation tests can merge:

| ID | What it proves |
|---|---|
| `CertSessionContinuationAfterTmuxLoss` | Kill tmux between turns, continue from handle, provider recalls codeword |
| `CertNativeResumeOnlyLatestMessage` | Full message history is NOT replayed into the resumed session |
| `CertHandleRoundTrip` | Turn 1 returns a valid handle; turn 2 uses it without any provider-specific field from the caller |
| `CertNonContinuableError` | Missing native resume ID → typed error, not a hang or crash |
| `CertStableWorkingDir` | Continuation returns `StaleHandleError` when working dir changed |

### Existing Test Infrastructure

The `TestCodexCLIRealInteractiveTmuxFullContract` and
`TestCodexCLIStructuredMultiTurnResume` tests already implement the
"remember codeword" pattern with canary tokens, tmux helpers
(`waitForCodexRealActiveSession`, `captureCodexPane`), and MCP server
fixtures. They prove multi-turn session memory but do **not** kill the tmux
session between turns.

Extending those tests for `CertSessionContinuationAfterTmuxLoss` requires
inserting approximately:

```go
exec.Command("tmux", "kill-session", "-t", tmuxSession).Run()
// Then turn 2 uses same handle — provider must restart with --resume
```

For Claude Code, `TestClaudeCodeExperimentalIntegrationNativeResume` must be
verified to actually kill the tmux pane between turns. If it does not, it is
testing multi-turn memory, not recovery.

### Provider-level tests in `multi-llm-provider-go`

1. Start a session and ask the provider to remember a unique codeword.
2. Capture the returned provider session handle.
3. Kill or lose the tmux session.
4. Continue using only the handle and a new message.
5. Verify the provider recalls the codeword.
6. Verify only the latest user message was sent on native resume.
7. Verify continuation lifecycle events were emitted.

### Agent-level tests in `mcpagent`

1. Start an agent session.
2. Continue it through the agent-level handle.
3. Simulate provider tmux loss below the agent layer.
4. Verify `mcpagent` updates the handle after recovery.
5. Verify API-backed agents can still continue through normal mcpagent-managed
   history without provider-native handles.
6. Verify calling the provider-level coding-agent continuation on an API provider
   returns a typed non-applicable error.
7. Verify normal API providers are unaffected.

### Builder-level tests in `mcp-agent-builder-go`

1. Workflow step completes main execution and pre-validation.
2. Post-validation learning or KB review waits long enough for tmux to close.
3. Continuation resumes the same logical agent session.
4. Auto-notification fires after post-step work completes.
5. Terminal UI shows old pane as archived and new pane as current.
6. Durable post-step phase state survives a builder restart:
   `pre_validation_passed -> learning_pending -> kb_pending ->
   notification_pending -> completed`.
7. Terminal state alone cannot complete, retry, or skip pending post-step work.

## Migration Plan

### Phase 1: Claude Code — **DONE**

- Provider handle and continuation API live in
  `multi-llm-provider-go/coding_agent_continuation.go`.
- Claude Code uses `WithResumeSessionID` from the handle; tmux recovery falls
  out of the provider adapter's existing resume path.
- **Done:** explicit kill-tmux-between-turns provider E2E via
  `TestCodingAgentContinuationRealE2EAfterTmuxLoss`.

### Phase 2: mcpagent — **DONE**

- `AgentSessionHandle` and `ContinueAgentSession` exist.
- `AskWithHistory` auto-routes to `ContinueCodingAgentSession` when a
  coding-agent handle is present (`agent/llm_generation.go:1378`). Callers do
  not need to switch APIs.

### Phase 3: Builder Integration — **PARTIAL**

- **Done:** `ChatHistoryAgentRuntime.AgentSessionHandle` is captured and
  restored on chat resume; chat path benefits automatically from the
  auto-routing in mcpagent.
- **Done:** legacy `claudeCodeSessionIDs`, `geminiSessionIDs`,
  `geminiProjectDirIDs` maps and `restoreCodingAgentRuntime()` have been
  removed from `server.go`.
- **Partial:** workflow step / learning / KB review calls that stay inside the
  same live `mcpagent.Agent` now route through continuation automatically.
- **Pending:** durable workflow-step handle storage and post-step phase
  persistence for restart, long lock waits, and delayed learning/KB review.
- **Pending:** terminal grouping of old/new panes under one logical execution.

### Phase 4: Codex CLI — **DONE (alongside Phase 1)**

- Codex CLI is wired in the same `ContinueCodingAgentSession` switch using
  `WithCodexResumeSessionID` + `WithCodexProjectDirID`.
- **Done:** explicit kill-tmux provider E2E via
  `TestCodingAgentContinuationRealE2EAfterTmuxLoss`.

### Phase 5: Gemini / Future Providers — **PARTIAL**

- **Done:** Gemini CLI is wired in the same `ContinueCodingAgentSession`
  switch (`WithGeminiResumeSessionID` + `WithGeminiProjectDirID`).
- **Done:** Cursor CLI and Pi CLI explicitly return `non_continuable`
  from `ContinueCodingAgentSession` until they are certified.
- **Pending:** Gemini structured-mode E2E test for the workflow-step path.

## Non-Goals

- Do not make workflow code understand provider resume internals or inspect the
  `Transport` field of `CodingProviderSessionHandle`.
- Do not use terminal text as the authoritative continuation state.
- Do not replay full message history into coding-agent CLIs when provider-native
  resume is active, unless a specific provider contract requires it.
- Do not add new provider-specific maps to `server.go` as a workaround.
- Do not put builder-specific chat/workflow persistence fields inside
  `CodingProviderSessionHandle`. Migrate `ChatHistoryAgentRuntime` into
  builder/mcpagent session persistence, with provider state nested as the
  provider handle.

## Rough Execution Plan

This is a practical implementation sequence. The point is to harden one vertical
slice first, then generalize.

### Step 0: Freeze the Contract

Owner repo: `multi-llm-provider-go`

Deliverables:

- Add a short contract section to the provider docs defining:
  - provider session handle fields
  - continuation lifecycle events
  - typed errors
  - latest-message-only resume behavior
  - tmux-loss recovery behavior
- Add a small capability flag for providers, covering all three transport types:
  - supports tmux continuation (transport = tmux)
  - supports structured continuation (transport = structured)
  - is API-only / no provider-native continuation (transport = api)
  - supports native resume (within tmux or structured)
  - requires stable working directory

Acceptance criteria:

- Claude Code and Codex CLI both declare their continuation capabilities.
- Structured-only providers can explicitly declare no tmux support.
- API providers can explicitly declare that only mcpagent history continuation is
  supported.
- The contract describes what a new provider must implement before being marked
  production-ready.

### Step 1: Add Provider Handle Types

Owner repo: `multi-llm-provider-go`

Likely areas:

- `llmtypes`
- provider adapter metadata helpers
- provider contract/certification docs

Deliverables:

- Add a serializable `CodingProviderSessionHandle`.
- Add helpers to extract/update the handle from `ContentResponse`.
- Keep old metadata keys temporarily for compatibility.
- Do not reference or import builder-side types such as
  `ChatHistoryAgentRuntime` in `multi-llm-provider-go`. Mapping existing builder
  history fields into the new handle happens in `mcp-agent-builder-go` or
  `mcpagent`, not in the provider package.

Example fields:

```go
type CodingProviderSessionHandle struct {
    Provider        string `json:"provider"`
    Transport       string `json:"transport"`
    NativeSessionID string `json:"native_session_id,omitempty"`
    TmuxSession     string `json:"tmux_session,omitempty"`
    WorkingDir      string `json:"working_dir,omitempty"`
    ProjectDirID    string `json:"project_dir_id,omitempty"`
    Model           string `json:"model,omitempty"`
    Status          string `json:"status,omitempty"`
}
```

Acceptance criteria:

- Claude Code response includes a handle with native resume id and tmux session.
- Codex response includes a handle with thread id and tmux session when relevant.
- Existing callers that read old metadata still work.
- No `multi-llm-provider-go` package imports builder persistence/runtime types.

### Step 2: Implement Claude Code Provider Continuation

Owner repo: `multi-llm-provider-go`

Likely areas:

- Claude Code experimental/tmux adapter
- tmux registry/session management
- prompt wait / resume prompt handling
- real integration tests

Deliverables:

- Add `ContinueCodingAgentSession(ctx, handle, message, options)` or equivalent
  provider-dispatched function.
- Claude path:
  - if tmux exists and idle, paste message into existing session
  - if tmux is gone and native resume id exists, start a new tmux with
    `claude --resume`
  - handle compaction/resume prompts inside adapter
  - return updated handle
- Emit lifecycle events:
  - `resuming`
  - `running`
  - `completed`
  - `failed`
  - `lost_tmux`
  - `resume_unavailable`
  - `stale_handle`

Acceptance criteria:

- Real E2E passes:
  1. ask Claude to remember a codeword
  2. capture handle
  3. kill tmux
  4. continue with only the handle and a new message
  5. verify Claude recalls the codeword
- Test proves old conversation text was not replayed when using native resume.
- Missing native resume id returns typed non-continuable error.

### Step 3: Add mcpagent Agent Session Handle

Owner repo: `mcpagent`

Likely areas:

- agent options/session state
- `AskWithHistory`
- provider response metadata handling
- event emission

Deliverables:

- Add an `AgentSessionHandle` containing the provider handle plus agent-level
  identity/config.
- Store latest handle after each provider call.
- Update `AskWithHistory` (or its result type) to surface the provider handle
  after Turn 1. Currently the session ID is buried in
  `ContentResponse.GenerationInfo.Additional` and extracted by
  `restoreCodingAgentRuntime()` in the builder. The handle must be returned
  directly so callers do not need to re-parse metadata keys.
- Expose:

```go
ContinueAgentSession(ctx, handle, message, options)
```

or an equivalent method on `Agent`.

- `ContinueAgentSession` called on an API-backed agent should continue through
  mcpagent-managed message history when the session is valid.
- `ContinueAgentSession` called on a session that has no durable history and no
  provider-native handle must return a typed non-continuable error immediately.
- Keep normal `AskWithHistory` behavior unchanged for API providers.

Acceptance criteria:

- mcpagent can continue a Claude Code session after provider tmux loss without
  the caller passing Claude-specific resume options.
- The updated provider handle is returned from Turn 1 without requiring metadata
  key parsing in the caller.
- Calling `ContinueAgentSession` on an API-only agent continues through
  mcpagent-managed history.
- Calling low-level `ContinueCodingAgentSession` on an API-only provider returns
  a typed non-applicable error.
- API providers do not regress.

### Step 4: Wire Builder Chat Through mcpagent Continuation

Owner repo: `mcp-agent-builder-go`

Likely areas:

- chat session runtime
- chat history persistence
- live steer routing
- terminal grouping

Deliverables:

- Persist opaque `AgentSessionHandle` in chat runtime/history.
- Add a compatibility mapper from existing `ChatHistoryAgentRuntime` fields into
  the new session model without moving builder-specific fields into the provider
  handle:

  | Existing field | New location |
  |---|---|
  | `ExternalSessionID` | `CodingProviderSessionHandle.NativeSessionID` |
  | `ProjectDirID` | `CodingProviderSessionHandle.ProjectDirID` |
  | `Provider` | `CodingProviderSessionHandle.Provider` |
  | `WorkspacePath` | builder context, and `CodingProviderSessionHandle.WorkingDir` only when it is the provider process cwd |
  | `ResumeSupported` | provider capability / handle status |
  | `ResumeFlag` | temporary compatibility metadata only; remove once provider continuation owns flag construction |

- On a new chat turn for a coding agent:
  - call mcpagent continuation when a handle exists
  - start a fresh agent session when no handle exists
- Remove direct dependence on provider-specific resume maps for the chat path.
- If continuation creates a new tmux session, terminal UI should group it under
  the same logical chat/agent execution and archive the old pane.

Acceptance criteria:

- Start Claude Code chat, ask it to remember a codeword.
- Let/force tmux close.
- Send a new chat message.
- Backend resumes through mcpagent and Claude recalls the codeword.
- UI shows the newer terminal as current and the older pane as previous.

### Step 5: Wire Workflow Step Continuation

Owner repo: `mcp-agent-builder-go`

Status: **Done for builder workflow runtime.** Live workflow execution agents
reuse the same mcpagent continuation path when an `AgentSessionHandle` is
present. Each workflow step also persists a `continuation_state.json` file under
its step log folder. That file records the latest top-level handle, a handle per
phase, and phase states for main execution, pre-validation, direct KB review,
direct learning, agent-mode learning, and KB update agents.

On workshop session startup, once the server execution notifier is attached, the
backend scans `runs/*/logs/*/continuation_state.json`, identifies post-step
phases that are still `pending`, `running`, `waiting_for_lock`, or
`recovery_queued`, and requeues that work through the same workshop registry and
notifier path used by normal runtime jobs. Recovery is provider-agnostic: the
workflow layer reapplies an `AgentSessionHandle`; mcpagent/provider code decides
whether to use native resume or live tmux.

Likely areas:

- workflow execution agent creation
- `executeSingleStep`
- background/todo sub-agent execution
- message-sequence re-entry
- session execution tree

Deliverables:

- Define the persistence location for `AgentSessionHandle` for workflow steps.
  Chat sessions already have `ChatHistoryAgentRuntime` in chat history. Workflow
  steps have no equivalent. The handle must be persisted somewhere durable
  (e.g., a per-execution entry alongside the step state) so that a builder
  process restart does not lose it before the post-step learning turn fires.
  **Current location:** `runs/{iteration}/{group}/logs/{step}/continuation_state.json`.
- Persist post-step phase state separately from the handle. A handle only says
  how to continue an agent session; it does not say which workflow work remains.
  Durable state must record phases such as:
  - main execution completed
  - pre-validation passed/failed
  - KB review pending/completed
  - learning pending/waiting/completed
  - auto-notification pending/sent
- Treat this phase state like a durable work item. On builder restart, the
  backend sees "pre-validation passed, learning/KB still pending" and resumes
  that post-step sequence without relying on an in-memory goroutine, terminal
  pane, or SSE event history.
- Store an agent session handle per logical workflow execution owner:
  - main workflow agent
  - workflow step
  - todo sub-step
  - background agent
- Re-entry or post-step continuation calls mcpagent continuation with the stored
  handle.
- Workflow code no longer decides whether to paste into tmux or resume native
  provider session.

Acceptance criteria:

- A workflow step can complete main execution and pre-validation.
- Post-step work delayed by a lock can continue after tmux is closed.
- Parent `call_sub_agent` receives completion after continuation/post-step work.
- No provider-specific resume flags are assembled inside workflow execution
  code.

**Test gate:** provider kill-tmux continuation is certified in
`multi-llm-provider-go`; builder workflow recovery has unit coverage for durable
state and startup replay selection. Live workflow E2E should still be run before
declaring a new provider certified.

### Step 6: Move Direct Learning and KB Review to Continuation

Owner repo: `mcp-agent-builder-go`

Status: **Done for builder workflow runtime.** Learning and KB review turns that
run through the same live execution agent inherit mcpagent continuation.
Direct-learning lock waits are written to `continuation_state.json` before the
lock is acquired, so a long wait does not erase the provider handle. Agent-mode
learning and KB update agents also persist their own phase handles. Restart
replay requeues any incomplete direct or agent-mode post-step phase and applies
the stored handle before executing the continuation.

Likely areas:

- direct learning turn
- KB review turn
- post-validation execution flow
- learning lock handling

Deliverables:

- Treat learning and KB review as continuation turns on the same logical agent
  session.
- If waiting on a global learning lock outlives the tmux session, continuation
  resumes below mcpagent/provider.
- Emit explicit UI-visible lifecycle events:
  - pre-validation passed
  - waiting for learning lock
  - learning continuation resumed
  - learning completed
  - KB review completed

These are builder/workflow lifecycle events, not provider transport events.

Acceptance criteria:

- A sub-agent that passes pre-validation but waits on learning lock does not
  strand the parent tool call.
- If tmux is gone by the time learning starts, provider continuation recovers.
- Auto-notification fires only after post-step work actually completes.

### Step 7: Implement Codex CLI Provider Continuation

Owner repo: `multi-llm-provider-go`

Deliverables:

- Implement the same handle/continuation contract for Codex CLI.
- Use Codex native thread/session id when tmux is gone.
- Keep final response extraction inside provider adapter.

Acceptance criteria:

- Same real E2E as Claude:
  - remember codeword
  - kill tmux
  - continue from handle
  - verify recall
- Workflow/backend code does not change for Codex after the Claude path is wired.

### Step 8: Gemini Policy

Owner repos:

- `multi-llm-provider-go`
- `mcpagent`
- `mcp-agent-builder-go`

Decision:

- Workflow steps should use Gemini structured JSON mode if that remains policy.
- Gemini tmux continuation is only required if Gemini chat/tmux remains enabled.

Deliverables:

- For structured workflow steps, ensure Gemini handle carries native session id
  and project dir id.
- For tmux chat, implement the same continuation contract or mark it explicitly
  unsupported.

Acceptance criteria:

- Gemini workflow steps do not depend on tmux.
- Gemini structured continuation tests pass if multi-turn workflow continuation
  is required.

### Step 9: Remove Old Provider-Specific State

Owner repo: `mcp-agent-builder-go`

Status: **Done for builder session maps and runtime restore.** Keep the
compatibility readers for old chat-history fields until old history files no
longer need migration.

Deliverables:

- Remove or deprecate backend maps (`claudeCodeSessionIDs`, `geminiSessionIDs`,
  `geminiProjectDirIDs`) in `server.go`. **Done.**
- Replace `restoreCodingAgentRuntime()` with compatibility restore through
  `seedCodingAgentRuntimeFromRestoredConversation()` and
  `ApplyAgentSessionHandle()`. **Done.**
- Remove provider-specific continuation branches from chat/steer/workflow code.
- Keep compatibility migration for existing history files if needed.

Do **not** add new provider maps in `server.go`. Provider-specific native IDs
may still be mirrored into `mcpagent.Agent` fields for adapter compatibility,
but builder persistence should treat `AgentSessionHandle` as the durable source
of truth.

Acceptance criteria:

- New code paths use `AgentSessionHandle`.
- Tests fail if workflow code constructs provider-specific resume options.
- `server.go` does not grow new provider-specific session state.

## Test Gates Before Merge

Minimum gates for the first production-ready slice:

```text
multi-llm-provider-go:
  - Claude Code provider continuation unit tests
  - Claude Code real tmux loss/resume E2E
  - Codex CLI provider continuation unit tests
  - Codex CLI real tmux loss/resume E2E

mcpagent:
  - agent session handle persistence test
  - continue-after-provider-tmux-loss test
  - API-provider history continuation test
  - provider-native API non-applicable error test

mcp-agent-builder-go:
  - chat continuation after tmux close (`coding-agent-chat-e2e --run-tmux-loss-resume`)
  - workflow step post-validation continuation after delay
  - durable post-step phase recovery after builder restart
  - terminal grouping current/previous pane test
```

Current live status: Claude Code and Codex CLI have both passed the builder
chat tmux-loss run. Codex CLI launch now disables startup self-update checks so
the resumed pane does not die while the E2E is waiting for the second turn.

## Suggested First Vertical Slice

Start with Claude Code only:

1. `multi-llm-provider-go`: provider handle + Claude continuation.
2. `mcpagent`: agent handle + continuation method.
3. `mcp-agent-builder-go`: chat continuation only.
4. Add one backend E2E that starts chat, kills tmux, continues, verifies memory.

This proves the boundary without touching every workflow path at once. Once this
works, the same continuation call can be used for workflow steps, learning, KB
review, and Codex.
