# Bug: Workshop background agents and batch groups render as orphans at root

## Symptom

In Tree view, workshop iterations and their batch groups render as
disconnected siblings at the root level instead of nesting under the
work that contains them.

Example screenshot showed:

```
Nested Calc Task [Test Group]                         workshop background  completed
full-workflow [Test Group / iteration-0]              workshop background  failed
full-workflow [Test Group / iteration-0]              workshop background  completed
┌─────────────────────────────────────────────────────────────┐
│ Group TEST GROUP                                  Completed │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│ Full Workflow Execution                    completed (3m4s) │
│   └─ Execution Regression Router -> Math Solver Probe       │
│   └─ Execution Regression Router -> Text Processor Probe    │
│   └─ Execution Regression Router -> Nested Manager Probe    │
│       └─ Nested Manager -> Nested Calc Probe                │
│       └─ Nested Manager -> Nested Word Probe                │
│       └─ nested-manager                                     │
└─────────────────────────────────────────────────────────────┘
```

The subtree under "Full Workflow Execution" nests correctly. Everything
above it — the three `workshop background` rows and the two boxed
owner events — sits at root level even though they're conceptually
parent/child of each other.

## What the tree should look like

```
full-workflow [Test Group / iteration-0]   workshop background  failed (attempt 1)
full-workflow [Test Group / iteration-0]   workshop background  completed (attempt 2)
  └─ Full Workflow Execution               completed (3m4s)
       └─ Group TEST GROUP                 Completed
            └─ Execution Regression Router -> Math Solver Probe
            └─ Execution Regression Router -> Text Processor Probe
            └─ Execution Regression Router -> Nested Manager Probe
                  └─ Nested Manager -> Nested Calc Probe
                  └─ Nested Manager -> Nested Word Probe
                  └─ nested-manager
       └─ Nested Calc Task [Test Group]    (if spawned inside the iteration)
```

## Root cause

Three independent gaps in the execution-id propagation contract:

1. **`BatchGroupStart` events have no unique execution ID.** They flow
   through `ContextAwareEventBridge.HandleEvent` and inherit whatever
   `currentStepID` / step context the bridge happens to hold at emit
   time. So the "Group TEST GROUP" owner event ends up with the same
   `execution_id` as the workshop iteration it lives inside — the
   frontend sees them as peers, not parent/child.
   - Emitter: `controller_batch_execution.go:697 emitBatchGroupStartEvent`
   - Same issue affects `BatchExecutionStart` ("Full Workflow Execution").

2. **Workshop background iterations called directly from chat have no
   parent.** `currentWorkshopParentExecutionID(ctx)` reads
   `BackgroundAgentIDKey` from the context. For top-level tool calls
   it's empty, so the iteration's `parent_execution_id` defaults to
   `main:<sessionID>` in `addEventDerivedExecutionNodes`. The frontend
   treats `main:*` as root-like (`isRootLikeExecutionId`), so the row
   lands at the top level. This is *correct* for genuinely top-level
   work, but it means we can't visually distinguish "this is an
   iteration of the Test Group" from "this is an independent
   invocation."

3. **Retries spawn fresh background-agent IDs.** When iteration-0
   fails and re-runs, the retry gets a new `background_agent_id`.
   There's no "attempts" grouping in `EventHierarchy.tsx`, so failed
   and completed render as siblings instead of collapsing into one
   row with "2 attempts (1 failed)".

(3) is independent of (1)/(2) and can be fixed alone if needed.

## Fix sketch

### Backend (proper fix for the nesting)

1. Synthesize a stable execution ID for each batch group:
   `workshop-group:<groupName>:<iteration>` (or include a run ID).
2. In `emitBatchGroupStartEvent`, attach this ID as the event's
   own `execution_id` and set its `parent_execution_id` to the
   workshop iteration's exec ID (or to the workspace path / preset
   query ID when there's no enclosing iteration).
3. Push this ID into the context as `BackgroundAgentIDKey` (or a
   dedicated `BatchGroupIDKey`) before `runExecutionPhase` so every
   downstream event emitted inside the group inherits it as
   `parent_execution_id`.
4. Pop on group end so siblings in the next group don't inherit it.
5. Apply the same pattern to `BatchExecutionStart` ("Full Workflow
   Execution") so it nests under the workshop iteration too.

Touch points:
- `controller_batch_execution.go:emitBatchGroupStartEvent`,
  `emitBatchGroupEndEvent`, `emitBatchExecutionStartEvent`,
  `emitBatchExecutionEndEvent`
- `context_aware_bridge.go` — extend bridge state to track the
  current batch-group exec ID and inject it into event metadata
  alongside `step_*` keys
- `interactive_workshop_manager.go` — verify the existing
  `ctx = context.WithValue(execCtx, BackgroundAgentIDKey, execID)`
  chain still works after the batch group injects its own ID

### Frontend (retry collapse, independent)

In `EventHierarchy.tsx`, when building the flat list, detect
consecutive `background_agent_started` rows with the same `name` and
same parent and merge them into a single owner row showing attempt
counts and the final status. Don't drop the failed-attempt summary
entirely — it's the most diagnostic signal in a test run.

## Out of scope / why not just patch the frontend

I considered a frontend-only rule that infers parent links by
matching `[Test Group / iteration-0]` substring patterns in event
names. Rejected: that's display-string parsing and would silently
break the moment the naming convention changes. The execution-id
contract is the right place to fix it.

## Files referenced

- `agent_go/cmd/server/server.go:6978` — `workshopExecutionBgNotifier.OnExecutionStart`
- `agent_go/cmd/server/server.go:7344` — `emitBackgroundAgentEvent`
- `agent_go/cmd/server/session_execution_tree.go:308` — `addEventDerivedExecutionNodes`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_batch_execution.go:697` — `emitBatchGroupStartEvent`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go:3692` — typical `OnExecutionStart` call site
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/execution_parent.go:10` — `currentWorkshopParentExecutionID`
- `agent_go/pkg/orchestrator/context_aware_bridge.go` — metadata enrichment
- `frontend/src/components/events/EventHierarchy.tsx:151` — `getEventParentExecutionId`
- `frontend/src/components/events/EventHierarchy.tsx:196` — `isRootLikeExecutionId`
- `frontend/src/components/events/EventHierarchy.tsx:990-1056` — execution-tree → owner-event materialization
