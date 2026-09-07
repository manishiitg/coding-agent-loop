# Session identity and tool lifecycle review — 2026-09-07

## Result

The unique message-sequence runtime ID change addresses the reproduced old-run
cleanup collision. The three additional defects recorded below are now fixed
in local source, **not deployed**. The original review used temporary isolated
probes; the fix adds permanent regression tests. No AgentWorks server, coding
CLI, or live workflow was started. Existing uncommitted changes were preserved.

## Implemented follow-up

- Runtime allocation acquires an owner before registering shell/DB capabilities.
  Failed setup immediately retires that allocation; retry gets a fresh ID.
  Persisted history never grants authority to clean up a previous live session.
  Both common agent factory entrypoints close agents when initialization fails.
- Final sequence cleanup marks its own session stopped, closes the agent and
  provider session, clears shell/notification state, and calls
  `mcpagent.CloseSession` to close MCP connections and remove its isolated
  workspace. Duplicate cleanup is harmless. Shared browser/group sessions
  retain their own lifecycle, and ordinary turns retain their live runtime.
- Default and explicit tool lists share one capability-derived DB/cost tool
  construction path. Read-write step authority consistently includes query,
  mutation, and migration, subject to the owning workflow's available tool
  pool. A custom list cannot add tools withheld by that pool.
- Creation verifies that the agent uses the allocated MCP identity and restores
  the current item's guard after constructing the full-step tool snapshot.

Permanent tests cover failed setup/retry, both common factory failure paths,
owned versus shared MCP connection cleanup, repeated cleanup, restored history,
late old-run cleanup, and default/explicit tool selection with a restricted
parent pool. Focused orchestrator and message-sequence tests pass both normally
and with Go's race detector:

```sh
go test ./pkg/orchestrator ./pkg/orchestrator/agents/workflow/step_based_workflow -run 'Test(AgentFactoriesClose|MessageSequence|PrepareCustomTools|DedicatedWorkflowStepSessions|WorkflowStepWorkingDir|PrepareDirectLearningTurn)' -count=1
go test -race ./pkg/orchestrator ./pkg/orchestrator/agents/workflow/step_based_workflow -run 'Test(AgentFactoriesClose|MessageSequence|PrepareCustomTools|DedicatedWorkflowStepSessions|WorkflowStepWorkingDir|PrepareDirectLearningTurn)' -count=1
```

Commands run from `agent_go` using the local Go workspace dependencies.
`git diff --check` also passes. No live provider or deployment verification is
claimed by these tests.

Stopped-session markers use the dependency's existing process-lifetime registry
to prevent late reconnects. This fix releases connections and runtime resources;
it does not add expiration of those small identity tombstones.

## Original confirmed findings (before the follow-up)

### P2 — Completed runtimes do not close MCP connection sessions

Source: `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_message_sequence.go:1307`.

`closeMessageSequenceRuntime` calls `Agent.Close`, releases provider runtime
state, clears shell state, and removes the isolated directory. It does not call
`mcpagent.CloseSession` for the owned runtime. The dependency explicitly keeps
connections alive on `Agent.Close`; `SessionConnectionRegistry.CloseSession` is
the connection cleanup operation. Workflow/group teardown registers and closes
group IDs, not these distinct per-sequence IDs.

Reproduction: register a lazy MCP server configuration under a fresh sequence
runtime ID, invoke production sequence cleanup, then check `HasSession(id)`.
The completed session remains registered. The same lifecycle also owns actual
connections when tools connect. Unique IDs prevent replacement collisions but
make per-execution accumulation more apparent on a long-running server.

Fix direction: fully retire the owned MCP session at true sequence termination,
after in-flight work has settled. Do not retire the shared browser/group owner or
close a sequence's connection registry between its turns. Ensure late callbacks
cannot recreate a deliberately stopped connection.

### P2 — Agent creation failure leaves trusted shell configuration registered

Sources: `controller_message_sequence.go:1213`, `:1239`, and `:1302`.

The factory writes cwd, folder guards, runtime environment, DB capability, and
notification inheritance before creating the agent. If creation fails,
`session.runtime` is never assigned. The deferred cleanup returns immediately
when runtime is nil, so it cannot reclaim the already allocated identity.

Reproduction: call `getMessageSequenceRuntime` with a temporary workspace and
no valid LLM configuration. It returns the intended configuration error before
launching a provider. Calling production cleanup still leaves the shell config
and managed DB grant registered. This is an ownership/resource leak; the probe
does not demonstrate that an external caller can use the stale grant.

Fix direction: give session allocation an explicit owner before partial setup,
or use an error rollback that clears everything allocated by that attempt.
Do not use serialized `RuntimeSessionID` as unconditional cleanup authority:
old persisted history is diagnostic metadata, not ownership of a live runtime.
The common agent factory should also close an agent that initialized partially
before returning an error.

### P2 — Tool construction differs between default and explicit lists

Source: `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go:914`.

For the same managed DB read-write authority, the explicit-list path adds
`query_workflow_db`, `mutate_workflow_db`, and `apply_workflow_db_migration`.
The default-list path adds only the first two. A step can therefore gain the
migration tool merely by choosing an explicit shell-only custom-tool list,
or lose it by reverting to defaults. Existing tests exercise the explicit
branch only. This contradicts the capability-derived tool contract stated in
the code and creates confusing unavailable-tool failures.

Reproduction: use one synthetic workspace tool pool containing all four tools;
compare `prepareCustomTools(nil)` with an explicit
`workspace_advanced:execute_shell_command` list. Migration is absent from the
first and present in the second. No DB operation was performed.

Fix direction: build mandatory capability-derived tools once, then merge them
with either selection path. Test both paths with the same authority matrix;
choose migration authorization explicitly rather than as a side effect of
whether a tool list was supplied.

## How identity and tools currently connect

1. A sequence allocates a unique runtime owner ID. Live turns reuse its
   in-memory runtime; restored history receives a new runtime owner.
2. The controller registers a cwd, folder guard, runtime environment, logical
   DB access, and browser-session mapping under that ID.
3. Agent config receives the same `MCPSessionID`. The browser mapping deliberately
   points at the shared workflow browser; it does not merge shell identities.
4. Custom tools are selected from the available workflow pool. The base factory
   constructs tool definitions with folder guards and captures executors before
   the immutable agent is initialized. Profile/transport rules govern which
   tools the CLI bridge exposes directly and which it discovers/calls over HTTP.
5. Both BaseOrchestratorAgent execution and BaseAgent continuations put the
   configured step ID into trusted `ChatSessionIDKey`. The shell wrapper also
   pins this identity and its bridge URL against stale caller extra_env values.
6. Workspace and DB tools resolve trusted context before client fallback.
   Current session guards take precedence over frozen context guards. Missing
   DB session state is rejected; the workflow shell rejects an implicit
   workspace-root cwd fallback.
7. Cleanup spans several registries; allocation and final retirement now use
   the same explicit owner, including when setup fails.

## Validation

Three temporary tests asserted the desired cleanup/tool invariants. All three
failed at the specific checks above; no network/provider operations were needed.
The temporary probe file was removed in a finally block. Existing focused
runtime/session/tool tests in step_based_workflow and DB tests in virtual-tools
passed. The workspace package compiled but the selected name filter matched no
workspace tests; no new workspace-runtime coverage is claimed.
`git diff --check` passed.

This is not a complete multi-tenant security audit, a proof that overlapping
business actions are serialized, or a live end-to-end provider test. It does not
claim that UUIDs alone are authorization. Server-side grants and tool admission
remain the authority.
