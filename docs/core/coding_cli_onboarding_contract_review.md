# Coding CLI onboarding: contract review and implementation map

Review date: 2026-09-20. Reviewed snapshots: AgentWorks `e5817f944`,
`multi-llm-provider-go` `6ef98fb`, and `mcpagent` `cb3f8c6`.

Status: **changes required**. This is a review and proposed consolidation plan;
the registration refactor and findings below are not implemented by this document.

## Assessment

Adding a CLI is difficult because registration is distributed across three Go
repositories, frontend behavior, and release tooling. The existing contracts are
substantial: capability declarations, common session types, certification IDs,
resume/transcript registries, and cross-repository tests already exist. Keep them.
The missing piece is a complete, executable onboarding contract that connects a
provider's declaration to every runtime operation and product surface it needs.

The [cross-repository follow-up](#cross-repository-follow-up--2026-09-20) below
adds a deeper builder/agent review, another reproduced resume defect, a latent
lifecycle omission, and a responsibility matrix for both repositories.

Today, adding a capability flag and its adapter test does not ensure that the
application exposes, configures, invokes, restores, or certifies that capability.
Several mappings must be maintained independently. This has already caused the
release-provider list to drift, rather than being only a future maintenance risk.

## Existing authorities

Paths prefixed `SDK/` below are relative to the sibling `multi-llm-provider-go`
checkout; `Agent/` paths are relative to `mcpagent`. Other paths are relative to
this repository. These aliases are documentation notation, not filesystem paths.

| Authority | What it owns |
| --- | --- |
| `SDK/coding_agent_contract.go` | Provider capabilities and default transport; current executable declarations. |
| `SDK/coding_agent_certification.go` | Required proof IDs, priorities, and provider-to-test registrations. |
| `SDK/docs/coding_sdk_tmux_contract.md` | Interactive transport behavior and adapter proof expectations. |
| `SDK/docs/coding_sdk_structured_contract.md` | Non-interactive transport behavior. |
| [Cross-repository contract](../../agent_go/docs/cross_repo_integration_contract.md) | Config, streaming, identity, cost, cancellation, MCP, and canonical turn boundaries. |
| [Builder E2E contract](coding_agent_builder_e2e_contract.md) | Chat, workflows, background work, UI, and history integration. |
| [Durable ACK review](../refactor/durable_ack_p0.md) | Delivery confirmation semantics and separately reproduced open defects. |
| This document | Where to implement a new CLI, review findings, and proposed registration consolidation. |

These documents should reference the executable proof registry rather than
maintaining competing provider and priority lists.

## Confirmed review findings

### R1 — P1: scheduled and “all” live P0 runs omit Muse

Evidence: [workflow](../../.github/workflows/coding-cli-p0.yml), line 205, supplies
`claude-code,codex-cli,cursor-cli,pi-cli` for both scheduled runs and manual
`providers=all`. The [local runner](../../scripts/run-coding-cli-p0.sh) includes
`muse-cli` in its default and `all` expansion and has a Muse execution branch.
The SDK declares Muse as an active tmux provider with registered P0 proofs.

Reproduction: compare the two checked-in provider lists. The difference is
`muse-cli`. Consequently a green scheduled live job does not certify every
active provider. Selecting `muse-cli` explicitly is possible, but the default
workflow never does so. This is a code/configuration finding; no live workflow
run was needed or performed to establish the selection mismatch.

Required change: derive the active release-provider set and test packages from
the SDK registry. Make the workflow pass `all` through unchanged, and have the
runner resolve it dynamically. Add a deterministic assertion that the default
release matrix equals the active release-provider set. Missing authentication
must fail the selected provider's certification rather than remove it from the
matrix. Keep explicit subset runs, but label their evidence as partial.

### R2 — P1: a documented P0 proof is downgraded and omitted by execution policy

Evidence: `SDK/coding_agent_certification.go` describes
`CertStreamNoHistoryReplay` as P0, but
`CodingAgentCertificationPriorityForID` returns P1 for that ID.
`RequiredP0CodingAgentCertificationIDs` adds `structured_streaming` for streaming
providers without also adding `stream_no_history_replay`.
`SDK/coding_agent_contract_test.go` lists the latter as a tolerated gap for
Claude, Codex, Cursor, and Pi.

A small program against the public SDK API produced:

```text
stream_no_history_replay priority: P1
claude-code streaming=true replay_in_P0=false missing_P0=[]
codex-cli streaming=true replay_in_P0=false missing_P0=[]
cursor-cli streaming=true replay_in_P0=false missing_P0=[]
muse-cli streaming=false replay_in_P0=false missing_P0=[]
pi-cli streaming=true replay_in_P0=false missing_P0=[]
```

The registry and P0 checks pass while the promised restart/history proof is
missing. Existing deterministic regression fixtures are useful, but do not
constitute the real CLI restart proof required by this certification's contract.
This finding concerns enforcement; it does not claim all four providers
currently replay history incorrectly.

Required change: make priority and requirement derive from the same proof
definition. To honor the stated P0 contract, add real restart-spanning evidence,
promote the ID in executable policy, require it for streaming providers, and
remove the corresponding gap allowances. Do not merely relabel missing proof as
passed. If the release policy deliberately accepts P1, change the normative
wording and explicitly record that decision instead of retaining contradictory
claims.

### R3 — P2: the bridge integration coverage guard cannot detect missing entries

Evidence: `Agent/agent/coding_agent_options_test.go`,
`TestCodingAgentIntegrationAppenderCoverage`, iterates
`codingAgentIntegrationAppenders` and checks that its existing entries have SDK
contracts. It does not iterate SDK contracts to require the reverse mapping.
`Agent/agent/llm_generation.go`, around line 1135, applies an appender only when
the provider is present, without rejecting a missing required registration there.

Reproduction: a temporary test removed the Codex entry from the in-memory map,
called the existing coverage test, and restored the entry. The coverage test
still passed. The temporary test was removed after this review. Current providers
do have appender entries; this demonstrates an onboarding guard gap, not a claim
that current Codex launches lack their bridge.

Required change: for every SDK contract with `RequiresMCPBridgeConfig`, require
an integration binding, and test both directions. Reject missing bindings before
launch. Extend the same consistency check to persistent lifecycle, retained
delivery, durable ACK, and transcript capabilities rather than relying on
handwritten per-provider cases.

## Why onboarding is spread out

The following are independent wiring points today. This is the current-state
implementation map, not a claim that every provider needs every optional feature.

| Concern | Current locations to inspect/change | Omission risk |
| --- | --- | --- |
| Provider identity, factory, defaults, models | `SDK/providers.go`, `provider_initialization.go`, `provider_catalog.go`, `coding_agent_tier_defaults.go`, adapter model files | Unknown provider, wrong defaults, or unavailable model selection. |
| Adapter implementation | `SDK/pkg/adapters/<provider>/` | Launch, completion, extraction, input, cancellation, cleanup, and native files are CLI-specific. |
| Capabilities and proof inventory | `SDK/coding_agent_contract.go`, `coding_agent_certification.go`, `coding_agent_contract_test.go` | Claims may exceed implementation; missing proof can be hidden behind an incorrect priority. |
| Working directory, owner, persistence, instruction projection | `SDK/coding_agent_option_registry.go` | Required call options do not reach the adapter. Existing drift tests cover several of these mappings. |
| Resume and transcript reads | `SDK/coding_agent_resume_registry.go`, `coding_agent_transcript_registry.go` | Cold restore and native history are unavailable despite successful fresh launch. |
| Live input, control keys, retained final/progress reads | `SDK/coding_agent_live_input.go`, `coding_agent_control_key.go`, `providers.go` | Generic entry points require additional provider branches. |
| Account/auth isolation and security capabilities | `SDK/provider_account.go`, `coding_agent_security.go`, adapter auth code | Provider support for isolated identities or launch policy cannot be assumed from the coding-agent flag. |
| MCP, skills, tool restrictions, transport options | `Agent/agent/coding_agent_integrations.go`, `llm_generation.go`, `Agent/llm/providers.go` | Provider registration alone does not bind the orchestration context. |
| Agent lifecycle and native identity | `Agent/agent/agent.go`, `coding_agent_options.go`, `coding_session.go`, `session_handle.go`, `coding_agent_project_cleanup.go` | Provider-specific fields/switches can miss persistence, restore, or generated-file cleanup. |
| Application classification and persistence | `agent_go/pkg/common/code_execution_providers.go`, `agent_go/cmd/server/coding_agent_modes.go` | The separate CLI set and five-provider boolean tuple must both be extended. |
| Delivery receipt and history projection | `agent_go/cmd/server/live_input_durable.go`, `claude_native_transcript_sync.go` | ACK dispatch defaults to failed for an unhandled provider; native transcript projection has another provider switch. |
| Catalog, install, setup, credentials | `agent_go/cmd/server/llm_provider_manifest.go`, `provider_setup.go`, `provider_connections.go`, `llm_config_handlers.go` | A runnable SDK adapter may be invisible or have incomplete product setup. |
| UI setup and provider typing | `frontend/src/services/api-types.ts`, `components/providers/CodingProvidersPanel.tsx`, `codingProviderGuides.ts`, `GuidedProviderTerminal.tsx`, `stores/useLLMStore.ts` | Manifest-driven behavior coexists with local lists, guides, and fallback metadata. |
| UI runtime semantics | `frontend/src/utils/codingCliTranscriptReconciliation.ts` | A new provider's completion does not trigger reconciliation until added to the frontend set. This is behavioral, not merely branding. |
| Live test selection | `SDK/cmd/coding-agent-p0-tests/main.go`, `SDK/scripts/agentic-p0.sh`, `scripts/run-coding-cli-p0.sh`, `.github/workflows/coding-cli-p0.yml` | Test names are registry-derived, but packages and provider selection are still separately enumerated. |

After using this map, search the existing provider added most recently (Muse in
these snapshots) across source files. Treat results as a completeness audit,
not as instructions to copy all of its exceptions. Generated frontend bundles
are build outputs, not registration locations.

There is also transport documentation drift: the builder contract says workflow
steps use bounded tmux, while `codingAgentUsesStructuredTransportForChat` selects
structured execution for non-interactive work. The SDK still describes
structured transport as a legacy fallback. A new adapter author needs an explicit
per-surface transport requirement; a single default `Transport` value is not enough.

## P0/P1 acceptance contract

Use `RequiredP0CodingAgentCertificationIDs` as the current executable SDK
inventory, with R2 tracked as an unresolved discrepancy. Do not infer a release
pass from the existence of certification records: records identify tests, while
successful live runs supply evidence.

| Gate | Required evidence |
| --- | --- |
| Deterministic integration gate | Bidirectional registration consistency; declared capabilities have callable operations and proof bindings; model/auth/options propagation; event and UI routing. This can run without provider credentials. |
| P0 adapter base | Real fresh launch; runtime system/skill/MCP context; exact cwd; trust/auth handling; MCP reachability and native-tool restrictions; slow-tool false-idle protection; completion and clean final extraction; tmux multi-turn; live/busy input; cancellation; parallel isolation; reply formatting fidelity. |
| P0 capability-dependent | Structured streaming when claimed; independent structured multi-turn when persistent native resume is claimed; stalled-turn diagnosis when claimed; durable ACK when claimed. Restart/no-history-replay must be resolved under R2. |
| P0 application | Real MCP-backed workflow completion and next-step advancement; retained chat through the agent layer; exactly-once delivery/routing; stable turn identity and one canonical completion; persisted final response/history. Adapter-only proof is insufficient. |
| P1 hardening | Lifecycle/retention, cancel-then-reuse, external session loss/recovery, stale drafts, shared-directory config isolation, startup visibility, model/auth/environment variants, status/usage, and provider upgrades, as applicable. Map each requirement to a concrete executable test. |
| Capability publication | Unsupported behavior is explicit. Optional capability claims need their corresponding proof before advertisement; accepted restrictions must remain visible. Muse's named best-effort tool exception must not become the default for new CLIs. |

The existing documents use priorities differently: the cross-repository document
labels MCP propagation P2, the builder requires workflow cwd/MCP under P1, and the
SDK has MCP runtime behavior in P0. These scopes overlap. Proposed rule: each
proof has one stable ID, owner, priority, applicability predicate, and evidence
type; a release runs the union of required proofs across all three layers.
Documentation may explain the proof but should not independently assign priority.

Retain these delivery invariants when building a new adapter: transport acceptance
is distinct from durable confirmation; a queued timeout must remain uncertain
without evidence of rejection; each send needs its own receipt identity; and
identical messages must not share one durable acknowledgement. The open issues
in the durable ACK review show why a happy-path live steer test is insufficient.

## Proposed registration structure

Aim for one SDK provider binding and one application setup binding per new CLI,
plus its adapter implementation and tests. This is a target design, not an API
that exists today.

```text
SDK provider binding
  identity + capabilities + transport support + factory
  options + session/input/control/transcript/receipt operations
  model/auth metadata + proof definitions and test package
                  |
       generic mcpagent orchestration
       session context / bridge / lifecycle / events
                  |
AgentWorks setup binding + SDK-derived manifest
  install/login actions + presentation assets
                  |
       frontend and release test planner
```

1. Consolidate root SDK maps/switches into a typed provider descriptor with
   optional operation groups. Validate that a claimed capability has every
   required operation. Keep CLI flags, transcript formats, prompt parsing, and
   native config files inside each adapter. Compose bindings at the SDK root to
   avoid adapter-to-root import cycles; shared interface types belong in a
   dependency-neutral package such as `llmtypes`.
2. Pass a common launch context through `mcpagent`: account, cwd, owner, native
   session handle, transport, lifecycle, system instructions, MCP bridge,
   skills, tool policy, and cancellation. Translate it at the provider boundary.
   Replace per-provider persistence booleans with a provider-neutral lifecycle
   policy. Explicitly reject requested unsupported transport or capability.
3. Export generic durable ACK and normalized native-transcript operations from
   the SDK so AgentWorks does not import adapter packages or duplicate dispatch.
   Keep retained progress reads' cursor/serialization semantics explicit.
4. Keep product-specific install/login actions in one reviewed server descriptor.
   Expose supported actions and runtime capabilities in the manifest. The frontend
   should render that data; raw command execution must remain server-owned and
   allowlisted. Icons/guidance can remain presentation assets with a generic fallback.
5. Generate the release plan from provider/proof descriptors: provider, transport,
   package, test name, prerequisites, isolation mode, and expected result. Preserve
   Codex's current per-test-process isolation as metadata, not a shell switch.
   Reject missing/skipped required cases and record exact repository revisions,
   CLI version, selected providers, transport, and evidence outcome.

This does not require merging three repositories or removing provider-specific
code. It requires explicit interfaces at their boundaries and one authoritative
registration per responsibility.

## New CLI checklist using today's code

1. Define supported surfaces and transports first: interactive chat, retained
   follow-up, workflow step, and background/sub-agent execution. Specify auth,
   native resume, tool restrictions, token source, and unsupported features.
2. Implement the adapter and typed options under `SDK/pkg/adapters/<provider>`.
   Wire the factory, identity, catalog, defaults, contract, and applicable SDK
   registries/dispatch branches from the implementation map above.
3. Bind the agent layer's launch context, bridge/skills, tool policy, lifecycle,
   native identity, and cleanup. Test missing bridge and unsupported modes as
   explicit errors. Verify registration in both directions.
4. Bind server classification, lifecycle, manifest/setup/auth, durable receipts,
   and native history. Check frontend setup and reconciliation behavior using
   manifest/event fixtures for the new provider.
5. Register real certification tests and required deterministic regression tests.
   Update every current runner/provider/package list until generated discovery
   replaces them. Test the default and explicit subset selections.
6. Run deterministic checks, then authenticated adapter and application P0, then
   applicable P1 upgrade/hardening cases. Record CLI version and exact revisions
   of all three repos. Treat skipped or unavailable required live tests as
   incomplete certification, never as a pass.
7. Publish the provider only after its required evidence passes; record accepted
   restrictions and update setup guidance. Re-run relevant live proofs on CLI
   upgrades. A provider addition should include this completed checklist.

## Suggested implementation order

1. Fix R1, resolve R2, and add the reverse integration guard from R3. These are
   small changes that improve release confidence without changing adapter behavior.
2. Consolidate SDK registration behind existing public wrappers, migrating one
   provider first and comparing existing contract tests before migrating the rest.
3. Move common launch/lifecycle wiring into the agent boundary and remove server
   receipt/transcript switches only after generic operations have equivalent tests.
4. Derive product UI capabilities and release plans from the descriptors. Add a
   synthetic-provider integration fixture: registering its descriptors should
   make it discoverable, routable, and included in the test plan without editing
   generic orchestration, frontend behavior lists, or shell provider switches.

That last fixture is the measurable onboarding goal. A synthetic provider proves
wiring completeness; real CLI P0/P1 remains necessary to prove actual behavior.

## Validation performed

- SDK focused contract/registry tests passed: capability declarations, options,
  resume, transcripts, streaming, proof references, P0 requirements, gap policy,
  and persistent two-transport multi-turn requirements.
- Agent focused tests passed: integration appender coverage, cwd options,
  interactive options, and Muse bridge/transport bindings.
- AgentWorks focused tests passed: CLI classification, coding-agent behavior
  tests selected by `TestCodingAgent.*`, and provider install manifest declarations.
- Public-API probe confirmed R2; checked-in workflow/runner list comparison
  confirmed R1; temporary in-memory missing-appender probe confirmed R3.
- No authenticated live CLI certification, full repository suite, or provider
  upgrade run was performed. Passing the focused tests is not P0 release signoff.
- No production code changed. Temporary probe files were removed. The server test
  link emitted the existing macOS ONNX-library deployment-target warning; tests passed.

## Cross-repository follow-up — 2026-09-20

Reviewed AgentWorks `d7d94ead4` and `mcpagent` `cb3f8c6`, with SDK `6ef98fb`.
The earlier review mapped these repositories; this follow-up traces their actual
construction, option propagation, restore, delivery, and cleanup boundaries.

### R4 — P1: mcpagent can relabel another provider's native resume identity

Location: `Agent/agent/session_handle.go`, `applyAgentSessionHandle`, lines
94–133, especially the assignment to `codingProviderSessionHandle.Provider`.

The method rejects different connection IDs, but does not first reject a
different provider. With the legacy/default empty connection IDs, a Codex agent
can apply a Claude handle. It first imports that handle's session ID and working
directory, then restores the configured provider/model by relabeling the stored
handle as Codex. The native ID remains the Claude ID. The downstream
`codingProviderContinuationHandleForModel` provider check then accepts the
already-relabeled handle.

A temporary regression test constructed a configured Codex agent, applied a
Claude handle with an empty connection ID, and asserted that no Codex
continuation could be obtained. It failed:

```text
foreign native ID accepted for Codex: provider=codex-cli
native=claude-native-id cwd=/old/workspace owner=previous-owner
```

This can give a resumed turn the wrong native conversation identity and working
directory when a consumer switches engines while retaining an old handle. The
probe establishes the wrong continuation options without launching either CLI;
it does not demonstrate cross-provider access to another CLI's conversation.
The method is reached by `NewAgentFromDefinition` with `RuntimeConfig.ResumeHandle`,
`ApplyAgentResumeHandle`, and the store-backed conversation path.

Existing tests separately verify account mismatch, model changes within one
provider, and mismatch rejection for an already-stored handle. They do not
exercise foreign-provider restore followed by continuation. Builder history
code also has provider checks at some higher-level boundaries; those do not
make the reusable agent-layer restore operation safe for every consumer.

Required change: validate provider and connection compatibility before mutating
any agent state. Preserve model changes within the same provider, but never
relabel a native ID to another provider. Return an explicit accepted/rejected
restore outcome so consumers only omit replay history after a successful restore.
Test empty/global/named connections, same-provider model changes, cross-provider
handles, and no partial mutation of owner/cwd on rejection.

### R5 — P2: the builder's shared session helper omits Muse owner cleanup

Location: [agentsession.go](../../agent_go/internal/agentsession/agentsession.go),
`closeInteractiveOwner`, lines 617–627.

The same helper enables `PersistentMuse` when constructing a resumable session,
but its owner-scoped close switch handles only Claude, Codex, Cursor, and Pi.
`CloseAllInteractiveSessions` and `closeOtherInteractiveSessions` remove their
owner bookkeeping and then call that switch. For Muse, the dispatch performs no
close, although the SDK exports `CloseMuseCLIInteractiveSessionForOwner`.

Evidence is the direct source mismatch between the constructor and close paths;
no real Muse process was launched. No production call sites of this shared
helper's constructor/reset API were found in the checked-out repository, so this
is a **latent integration defect**, not evidence that the main server's existing
reset path leaks Muse. The main server has separate terminal/reaper machinery.

Required change: either retire this unused alternate construction path or make
it use the same generic lifecycle operations as the active path. If retained,
require an owner-close binding for every persistent provider and test actual
dispatch with an injectable closer. Do not remove bookkeeping while silently
skipping an unsupported close operation.

### What is already protected

- `TestCodingAgentPersistentInteractiveFlagsCoverTmuxContracts` in the builder
  iterates SDK tmux contracts and requires exactly one persistence flag. A new
  provider omitted from that particular switch is caught. The five-boolean API
  still creates maintenance work, but its current coverage should be preserved.
- `TestNativeTranscriptSyncSupportedProvider` iterates persistent/live-input
  contracts and requires transcript capability plus builder support. This catches
  a missing support-list entry; actual transcript-reader dispatch and UI
  reconciliation are additional boundaries needing their own proof.
- The agent checks steering against actual structured/tmux transport, rather
  than assuming a coding-provider ID always has a live pane. Existing steering
  tests passed for both transport choices.
- `AgentSessionHandle` and the immutable `AgentDefinition` / grouped
  `RuntimeConfig` already provide useful shared types. Strengthen those types
  instead of introducing another parallel configuration object.
- Account mismatch protection and per-user provider connection authorization
  have focused tests that passed. R4 concerns provider compatibility with equal
  connection IDs, not a failure of the tested different-account rejection.

### Required responsibilities in each repository

| Boundary | mcp-agent-builder-go responsibility | mcpagent responsibility | Required acceptance proof |
| --- | --- | --- | --- |
| Discovery and setup | Publish install/auth/model metadata; authorize the chosen connection. | Accept provider/model/account configuration through `RuntimeConfig.Generation`; leave product setup to the builder. | Each published provider is constructible, configured with the selected account, and either supports each advertised setup action or rejects it explicitly. |
| Runtime assembly | `server.go` → `pkg/agentwrapper/llm_agent.go` and workflow `agents/base_agent.go` construct the same definition/runtime contract. | `agent/definition.go` validates immutable instructions/skills/tools and binds runtime services. | Chat, workflow, background, and child agents preserve selected provider/model/account, skills, tools, bridge identity, cwd, and transport through the real constructor. |
| Transport and lifecycle | `coding_agent_modes.go` selects the surface policy; wrapper/workflow configuration carries it. | `coding_session.go` and `coding_agent_options.go` implement actual steering/persistence semantics. | Requested transport equals adapter transport; structured execution cannot accidentally retain/steer a tmux session; persistent chat remains usable after a turn. |
| MCP and native policy | Supply the session-scoped bridge and admitted tool policy. | `coding_agent_integrations.go` translates them to provider options. | Every bridge-required SDK provider has a binding; missing bridge fails before launch; unavailable native-tool containment is explicit. |
| Resume | Persist the opaque handle and current account/provider selection; replay history when restore is rejected. | Validate handle provider/account and apply native identity without changing its ownership. | Same-provider model switch succeeds; provider/account switch rejects stale native identity without changing owner/cwd; cold restart can continue supported sessions. |
| Input and receipts | Own HTTP request/message IDs and durable receipt events. | `Session.Send` / message delivery own turn routing; use SDK receipt operations. | Busy/idle boundary delivers once; identical texts have distinct receipts; late confirmation targets the right message; rejected/uncertain/queued outcomes remain distinct. |
| History | Store product messages/events and render native transcript reconciliation. | Supply normalized current-turn/final/progress data with stable session identity. | Cold restoration and retained follow-ups neither duplicate history nor omit additional replies; each advertised reader is actually callable. |
| Termination | Stop/reap the intended application session and update terminal metadata. | Cancel the active turn, finish its canonical lifecycle once, and release only owned resources via provider operations. | Cancel then reuse, bounded retention, reset, shutdown, and no effect on another session; helper paths must match the primary path or be retired. |
| Certification | Run the generated cross-repo provider/surface matrix and record exact revisions. | Exercise generic constructor, lifecycle, and operation conformance for every declared provider. | Deliberately missing bindings fail deterministic checks; every selected live P0 executes; required P1 evidence is tracked. |

### Additional onboarding gaps exposed by the deeper trace

These are architectural/test gaps rather than claims that all existing provider
routes are currently broken:

1. **Registration in mcpagent is still multidimensional.** A provider can need
   changes in `llm/providers.go`, `CodingRuntimeConfig` and its option conversion
   in `agent/definition.go`, private fields/options in `agent/agent.go`, integration
   appenders, the persistence map/enable switch, native session getters/setters,
   and projected artifact cleanup. A new enum plus SDK contract is insufficient.
   Use generic lifecycle and native-handle state and one operation binding, with
   bidirectional validation. Keep provider-specific serialization at the SDK edge.
2. **Builder has multiple assembly paths.** Main chat passes five persistence
   booleans through `LLMAgentConfig`; workflow assembly constructs its own
   `CodingRuntimeConfig`; the shared `internal/agentsession` helper has another
   provider switch. Cover each supported entry point with one table-driven
   propagation contract, and remove unused alternatives rather than maintaining
   them indefinitely. R5 is a concrete example of divergence.
3. **Durable receipt tests bypass production dispatch.** The current
   `live_input_durable_test.go` cases inject `internalDurableAckHandler`. They
   prove event mapping and outcomes, but would not detect an omitted branch in
   `awaitLiveInputDurable`. Require the SDK capability-to-operation binding in
   addition to these useful server tests; retain a live cross-stack receipt proof.
4. **History support is declared independently at three levels.** SDK transcript
   metadata, the builder's support/reader switches, and the frontend's
   `CODING_CLI_PROVIDERS` set are separate. The existing backend guard does not
   validate the frontend set. Carry a normalized reconciliation capability in the
   manifest or completion event and test it through the UI.

### Extension to the implementation plan

First fix R4's provider-identity validation and add its failed regression case to
the permanent suite. Treat a rejected restore as a distinct result rather than
silently assuming prior context exists. Add the inverse registration checks from
R3 and actual dispatch conformance for receipt/history/lifecycle operations.

Then replace the repeated provider persistence flags with a shared lifecycle
policy inside the existing `CodingRuntimeConfig`. Migrate main chat and workflow
assembly together, with explicit propagation fixtures for both. Audit or retire
`internal/agentsession` before using it for another product.

The onboarding completion criterion applies across both repositories: adding a
synthetic provider binding should exercise constructor → bridge/options →
session restore → delivery → history → cleanup without adding provider-name
branches to generic agent or builder code. Real adapter behavior still requires
authenticated P0/P1 tests; a synthetic provider only certifies the plumbing.

### Follow-up validation

- Existing focused `mcpagent/agent` tests passed for session-ID extraction/resume,
  typed handles, model-preserving restore, account isolation, continuation checks,
  steering, persistence, integration/cwd coverage, and canonical P0 turn lifecycle.
- Existing focused builder tests passed in `internal/agentsession`,
  `pkg/agentwrapper`, and `cmd/server` for definition/bridge configuration,
  additional bridge tools, persistence/transport, native transcript support,
  provider connections, and durable receipt events.
- The added temporary R4 regression failed as expected, then was removed. R5
  is source-verified only; its potential process leak was not exercised live.
- No authenticated CLI runs, full suites, or production code changes. The
  mcpagent working tree remained clean. This follow-up is recorded here to keep
  the onboarding review combined rather than duplicating it in both repositories.
