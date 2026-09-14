[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-321 — Add a simple explicit human-approved parallel-schedule opt-in

| Coordination | Value |
|---|---|
| Assigned agent | unassigned |
| Ticket state | `design confirmed; implementation not started` |
| Last synchronized | `2026-09-14` |

- **Priority:** P1 throughput after PLAT-320.
- **Depends on:** [PLAT-320](plat-320.md).
- **Owner:** schedule schema/tools/UI, human approval, scheduler admission,
  collision/dependency behavior, generated guidance and audit history.

## Decision

Keep the concurrency contract simple:

1. Workflow-producing schedules are sequential by default.
2. Do not build an agent-authored resource-claim system. A declared file or
   resource list cannot prove safety because actual writes and side effects can
   be dynamic or omitted.
3. A schedule may opt into parallel execution only through an explicit platform
   field and only after a human approves a fixed risk disclosure.
4. Both the already-running producing schedule and the incoming producing
   schedule must have the parallel opt-in. Otherwise the workflow-wide lock
   remains authoritative.
5. `after_schedule_ids` always wins: a dependent occurrence waits even when
   both schedules allow parallel execution.
6. Two occurrences of the same schedule do not overlap. Collision policy still
   queues, coalesces, retries or skips the later occurrence.

PLAT-320's immutable `iteration-N-sched` folders prevent two scheduled runs
from sharing their run-output tree. They do **not** isolate the rest of the
workflow. Before approval, the agent and UI must state plainly that parallel
runs can concurrently mutate the workflow database, knowledge base, learnings,
reports/files, planning state and browser/CDP state, and can duplicate or race
external actions. The opt-in accepts that risk; it does not describe parallel
execution as safe.

## Proposed schedule contract

Use one explicit mode rather than resource declarations:

```json
{
  "concurrency_mode": "sequential"
}
```

Allowed values are `sequential` (default) and `parallel`. Enabling `parallel`
must persist authenticated approval provenance and the risk-disclosure version;
the backend supplies identity/timestamps rather than trusting agent-authored
audit fields. Create/update tools must reject `parallel` when the approval step
has not completed.

The exact approval transport may reuse the platform's existing human-input
lifecycle or a UI confirmation, but it must produce one durable approval record
that both paths consume. Do not create separate, inconsistent chat and UI
approval rules.

## Scheduler admission

When schedule B fires while schedule A owns the workflow:

- if B depends on A, B waits;
- if A and B both have approved `parallel`, B may start;
- otherwise B follows its existing `collision_policy` against the workflow-wide
  busy state.

Manual **Run now** uses the saved schedule's mode and approval. Pulse-only and
maintenance occurrences remain non-producing evidence reviewers under their
existing rules. Interactive Builder/manual workflow execution remains exclusive
with producing schedules unless a separate policy explicitly changes it.

## Agent and human awareness

Generated AgentWorks system instructions, the Builder schedule reference,
Pulse Gate, Architecture Review, Technical Review and Fixer must all agree:

- sequential is the default;
- dependencies mean waiting, not overlap;
- resource claims are not used;
- the agent must show the fixed risk disclosure before requesting parallel;
- only explicit human approval can enable the persisted opt-in; and
- separate `-sched` folders do not prevent shared-state overwrites or duplicate
  external actions.

Guidance must not advertise the parallel field until the runtime schema and
approval path are implemented. Before then, agents preserve the existing lock.

## Acceptance criteria

- Existing and newly created schedules default to `sequential` without a
  migration prompt.
- An agent cannot enable `parallel` without a durable human approval record.
- The UI shows the same warning and writes the same approval record as chat.
- A sequential schedule blocks overlap with a parallel schedule in either
  direction.
- Two approved parallel schedules can overlap only after PLAT-320 gives both
  different immutable `-sched` identities.
- `after_schedule_ids` blocks overlap even when both schedules are parallel.
- The same schedule cannot overlap itself.
- Removing approval or changing back to sequential immediately prevents new
  overlap without interrupting runs already in progress.
- Schedule history records concurrency mode and approval provenance used at
  admission time.
- Generated prompt/skill tests assert the fixed risk language and never claim
  that resource declarations make execution safe.
- End-to-end tests cover every pairwise mode combination, dependency override,
  Run now, collision policies, restart, stop and simultaneous fire admission.
