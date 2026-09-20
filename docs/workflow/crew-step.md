# Crew workflow step

**Status:** Proposed

## Summary

A workflow can use a Crew project as an executable step. The step invokes one
of the Crew's public or platform-internal triggers, waits for that trigger run
to finish, and saves the Crew's final text response in the calling workflow's
current iteration folder. Later workflow steps can read the Crew's persistent
project files through the platform's existing read-only attached-folder
mechanism.

This feature makes a maintained Crew available as a reusable specialist inside
many workflows. It does not turn Crew projects into workflows or add iteration
folders to Crew.

## Why use a Crew instead of an ordinary step?

An ordinary workflow step belongs to one pipeline. Its inputs, execution, and
outputs are scoped to one workflow run. A Crew is a long-lived project that a
person and multiple automations can cultivate over time.

A Crew can retain:

- human-curated memory and instructions;
- selected and configured skills;
- persistent files and attached folders;
- a project database and dashboard;
- main-chat conversation history;
- separate persistent trigger-conversation history;
- schedules, triggers, and bot routes.

Use an ordinary step when the work belongs to the current run and should be
reproducible from that run's inputs. Use a Crew when the responsibility,
knowledge, or human stewardship continues across runs and workflows.

Calling a Crew does not provide a more capable model by itself. Its advantage
is the persistent, human-maintained specialist context.

## Product model

The workflow Builder adds a step with `type: "crew"`. The step identifies an
accessible Crew project and one of that Crew's triggers. The trigger owns its
saved base instruction and conversation destination. A platform-internal
trigger can be created while the step is configured and is scoped to the
calling workflow. The step supplies the workflow-specific instruction and
runtime input.

The Builder must understand that a Crew is a persistent project, not merely an
LLM call. Builder guidance and tools must cover Crew identity, memory, skills,
files, attached folders, database, dashboard, triggers, schedules, bots, and
conversation destinations.

## Plan representation

An illustrative `plan.json` entry is:

```json
{
  "type": "crew",
  "id": "review-with-rts",
  "title": "Review with RTS Crew",
  "description": "Ask the maintained RTS reviewer to review this pull request.",
  "crew_profile_id": "work",
  "crew_project_id": "rts-pr-reviewer",
  "trigger_id": "5f16e1fa-7ddd-4ee5-8311-63b34745ad46",
  "instruction": "Read your memory and review skills. Review the pull request supplied in the workflow input. Update your dashboard with the outcome. Return a concise result; if the supporting report is large, save it under reports/ and include its Crew-relative path in the response.",
  "context_dependencies": [
    "prepare-review/pr.json"
  ],
  "context_output": "response.md",
  "timeout_seconds": 1800,
  "next_step_id": "publish-review"
}
```

The exact schema should use stable project and trigger IDs. Names, icons, and
paths are display metadata and must not be execution identities.

### Required fields

| Field | Purpose |
| --- | --- |
| `id` | Stable workflow step identity. |
| `title` | Human-readable node title. |
| `crew_profile_id` | Product profile; initially `work`. |
| `crew_project_id` | Stable target Crew project ID. |
| `trigger_id` | Existing trigger to invoke. |
| `instruction` | Workflow-specific instruction rendered at run time. |

### Optional fields

| Field | Purpose |
| --- | --- |
| `context_dependencies` | Workflow outputs made available as trigger input. |
| `context_output` | Response filename; defaults to `response.md`. |
| `timeout_seconds` | Maximum wait for the Crew run. |
| `next_step_id` | Explicit successor using normal workflow navigation. |

## Conversation destinations

The selected Crew trigger has one of the existing run destinations.

### Main Crew chat (`crew_chat`)

The invocation continues in the same persistent conversation used by the
person and the Crew. Choose it when the instruction depends on discussion in
that main conversation.

### Trigger conversation (`isolated`)

The invocation continues in the trigger's own persistent automation
conversation. It retains earlier deliveries for that trigger but does not mix
them into the main user conversation. Choose it when the task should build on
previous automated runs.

`isolated` does **not** mean a fresh conversation for every invocation. Both
destinations retain context; they retain different histories. Avoiding visible
interruption is a secondary consideration. The primary choice is which history
the work requires.

The step should normally respect the selected trigger's configured
destination. A workflow needing both histories should use two clearly named
triggers instead of silently overriding one trigger's behavior.

## Runtime contract

Executing a Crew step consists of four core operations:

1. Invoke the selected Crew trigger with the rendered instruction, declared
   workflow inputs, and a stable idempotency key.
2. Poll or await the trigger run until it succeeds, fails, is stopped, or
   reaches the configured timeout.
3. Read the exact final assistant response recorded for that trigger run.
4. Save the text response in the calling workflow's current iteration folder.

For example, with variable groups enabled:

```text
Workflow/example/runs/iteration-0/production/execution/review-with-rts/response.md
```

Active runs execute in `iteration-0` (older numbered folders are archives);
without groups the step folder sits directly under `runs/iteration-0/execution/`.

The execution record should also retain operational metadata—Crew project ID,
trigger ID, Crew run ID, session ID, status, timestamps, and error details—in
the workflow's existing progress/run metadata. The Crew does not have to emit
JSON for this metadata.

Token spend is attributed to both sides: the Crew run records its caller
(workflow ID, run ID, step ID) so Crew-side cost history shows what each
caller spent, and the workflow run's cost breakdown includes the crew step's
tokens so the run shows its true cost. It is one spend visible in two places,
each side labeled with the other — never silently absorbed by either side.

### Trigger request

The delivery payload should include at least:

```json
{
  "source": "workflow_step",
  "workflow_id": "release-pipeline",
  "workflow_run_id": "iteration-0",
  "workflow_step_id": "review-with-rts",
  "group": "production",
  "instruction": "Rendered workflow-specific instruction",
  "inputs": {
    "pr": {
      "number": 87,
      "repository": "course_designer"
    }
  }
}
```

`group` is the calling workflow's variable group, for correlation only.
Payload data cannot override Crew permissions, tools, trigger ownership,
conversation destination, or folder guards.

### Internal and external dispatch

An external caller invokes a public Crew trigger through its authenticated HTTP
endpoint. A workflow running inside the same deployment should use a
platform-internal trigger and reuse the same trigger dispatcher directly,
rather than sending an HTTP request to itself or placing trigger secrets in
`plan.json`.

Internal dispatch must produce the same trigger delivery, conversation,
history, run status, final response, and idempotency behavior as the hardened
public endpoint (see Crew trigger hardening).

### Platform-internal triggers

A platform-internal trigger is a first-class trigger binding for communication
between a Workflow and a Crew on the same deployment. It has no public URL and
issues no bearer or GitHub secret. Its authority comes from an explicit caller
binding checked by the platform on every invocation.

A binding is not a separate object: it is `kind: "internal"` plus a caller on
an ordinary trigger, stored in the same trigger list as public triggers. The
target is implicit — whichever manifest or schedule list holds the trigger —
so deleting or disabling the trigger automatically retires the binding and
orphaned permissions are impossible.

An illustrative Crew-side binding is:

```json
{
  "kind": "internal",
  "id": "5f16e1fa-7ddd-4ee5-8311-63b34745ad46",
  "name": "Release workflow reviewer",
  "enabled": true,
  "message": "Review the pull request supplied in the delivery payload.",
  "caller": {
    "type": "workflow",
    "id": "release-pipeline"
  },
  "run_destination": "isolated"
}
```

The reverse binding is a workflow schedule entry with `kind: "internal"` and
`caller.type="crew"`. Bindings are directional and grant invocation only. They
do not grant write access to the target's files or management access to its
configuration.

Both sides manage bindings through their existing trigger tools, extended
with `kind: "internal"` (no secret issued, no public path, caller required):

- Workflow Builder creates an internal Crew trigger while adding a Crew step.
- Crew creates an internal workflow trigger when asked to invoke an attached
  workflow.
- Either side can list, disable, re-enable, or remove bindings it is authorized
  to manage; existing trigger UIs show internal triggers with an internal
  badge and no URL.

Creating a binding requires trigger-edit rights on the target side plus read
access on the caller side, reusing the ownership checks each side already
enforces. Invocation rechecks both resources, their attachment relationship,
and the binding's enabled state. The public HTTP trigger endpoints skip
internal triggers entirely: they are invokable only through internal dispatch.
Copying either resource never silently copies cross-project authority.

## Files and attached folders

Crew has one persistent project workspace. It does not have workflow iteration
folders, and this feature must not add them.

Before a Crew step can run, the target Crew project must be attached to the
workflow using the existing attached-folder mechanism. The attachment is
read-only. The attachment is the stable namespace through which downstream
steps read Crew files; it is not itself the permission. Folder grants store a
fixed path and never recheck project access, so the workflow must verify Crew
access before the run starts and again before each crew step executes, using
the same permission check as trigger invocation. If access is gone — revoked,
unshared, moved, or deleted — the run fails fast with an actionable error
before any step reads through the stale grant.

If the Crew returns:

```text
The detailed review is in reports/pr-87.md.
```

and the attachment alias is `rts-reviewer`, a downstream step can read:

```text
rts-reviewer/reports/pr-87.md
```

The workflow can read the file but cannot modify it. The file remains owned by
the Crew and may change later because Crew storage is persistent and mutable.

The first version does not infer an `output_files` list from natural-language
responses and does not automatically copy Crew files into the workflow run.
Small results may exist entirely in `response.md`. For large results, the
Builder can instruct the Crew to save files at a known Crew-relative location
and include those paths in its response.

If immutable file snapshots become necessary later, they should be an explicit
workflow feature with declared paths. They must not be introduced by parsing
arbitrary response prose.

## Crew invoking a workflow

The integration is bidirectional. A Crew can invoke an attached AgentWorks
workflow through a selected workflow trigger. This is useful when a persistent,
human-maintained specialist needs a deterministic pipeline for testing,
rendering, deployment, extraction, or another bounded operation.

The runtime flow is:

1. The Crew selects an attached workflow and an enabled trigger.
2. It creates or uses a platform-internal trigger binding scoped to that Crew.
3. It sends a JSON payload with a stable idempotency key.
4. The workflow executes in its own `iteration-<n>-hook` folder.
5. The Crew polls or awaits the trigger run until it becomes terminal.
6. The Crew reads the returned status, inline step outputs, progress, and
   artifact references.
7. The Crew can summarize the result, update its persistent files or database,
   and update its dashboard.

The workflow trigger already defines its route selections, optional single-step
target, variable groups, input mode, and execution contract. The Crew payload
cannot override configuration that the trigger did not explicitly expose.

An illustrative invocation is:

```json
{
  "workflow_id": "course-designer-tests",
  "trigger_id": "smoke-test-trigger",
  "payload": {
    "pull_request": 87,
    "environment": "dev"
  },
  "wait": true,
  "timeout_seconds": 1800
}
```

The completed result can contain the workflow's normal structured outputs:

```json
{
  "status": "completed",
  "run_id": "iteration-68-hook",
  "terminal": true,
  "steps": [
    {
      "step_id": "test",
      "outputs": {
        "result.json": {
          "passed": 42,
          "failed": 0
        }
      }
    }
  ],
  "artifacts": []
}
```

Small JSON and text outputs can be read from the polling response. Large files
remain workflow run artifacts and use the workflow trigger's existing signed
download contract. When the workflow is attached read-only to the Crew, the
Crew can also inspect the completed hook iteration through that attachment.

The conceptual distinction is:

- Workflow to Crew calls a persistent specialist and receives a natural text
  response.
- Crew to Workflow calls a deterministic pipeline and receives structured run
  status, step outputs, and artifacts.

## Builder behavior

The Builder should be able to:

1. List Crews accessible to the current workflow owner, including icon, name,
   identity, and stable ID.
2. Inspect a Crew's triggers, saved instruction summary, enabled state, and
   conversation destination.
3. Attach the selected Crew to the workflow as read-only after an explicit
   configuration action.
4. Create, add, update, and remove Crew steps.
5. Explain whether the selected trigger uses main Crew history or its own
   persistent trigger history.
6. Validate that the Crew attachment and trigger still exist before saving the
   plan.

The Builder tools are:

- `list_accessible_workflows` (existing discovery: workflows and Crew
  projects with identity)
- `manage_crew_trigger` (create/list/update/delete Crew triggers, extended
  with `kind: internal`; internal bindings default to the calling workflow)
- `manage_crew_attachment` (attach/list/detach Crews read-only)
- `add_step` / `update_step` / `delete_plan_steps` with `type: crew` (plan
  editing, with crew field schemas)

The Crew runtime has the corresponding workflow tools:

- `list_attached_workflows`
- `list_workflow_triggers`
- `manage_workflow_webhook` (the existing workflow trigger tool, extended
  with `kind: internal`)
- `run_workflow_trigger`
- `get_workflow_trigger_run`

The Builder should help write instructions that state:

- which Crew memory and skills to consult;
- what runtime input to process;
- whether to update Crew files, database, or dashboard;
- when a short text response is sufficient;
- where to save large outputs and how to reference them in the response.

## Canvas and configuration UI

The Crew node should show the Crew icon and name, the selected trigger, its
conversation destination, and the response output.

Example:

```text
🚀 RTS PR Reviewer
Trigger: Review pull request
Conversation: Trigger history
Output: response.md
```

The configuration view should contain:

- Crew selector with icon, name, identity, and ID;
- trigger selector;
- clear retained-context explanation;
- workflow-specific instruction editor;
- context dependency selection;
- response filename;
- timeout;
- next step;
- read-only attachment status and action.

The UI should not imply that Crew files are copied into the workflow run.

Run-time observability lives in the execution views, not the plan. The crew
step's run entry must link its recorded Crew run ID to the exact Crew turn —
transcript and timing one click away. The execution logs gain a crew category
with the polling timeline (queued, turn started, answer recorded) and the
final response, like other step types have.

## Access and security

- Never store a plaintext trigger secret in the workflow plan.
- Platform-internal triggers never issue a public endpoint or plaintext secret.
- Every internal invocation must match the binding's exact caller and target.
- Check Crew and trigger access when the step is configured and again when it
  executes.
- Require the target Crew to be attached read-only before execution.
- Re-verify Crew access before the run starts and before each crew step
  executes; a revoked, moved, or deleted Crew fails the run fast, before any
  step reads through its attachment.
- Keep the workflow's write paths separate from the Crew attachment.
- Reject deleted or disabled triggers with an actionable step error.
- A copied workflow must not inherit authority to call a Crew its new owner
  cannot access.
- Resolve all referenced Crew paths inside the attachment root; traversal and
  absolute-path escape are forbidden.

## Reliability

### Idempotency

Derive the trigger delivery identity from the workflow execution identity,
group, step ID, and step attempt. Retrying the same attempt must recover the
existing Crew delivery instead of creating duplicate work. A deliberate new
step attempt receives a new delivery identity.

### Concurrency

A persistent Crew conversation must process one turn at a time. When the
selected conversation is occupied, the delivery is accepted and queued as the
next turn; the step shows a queued state until the turn starts. It must not
interleave two turns in the same conversation. Mid-turn steering is for
corrections to the current job, not for new deliveries: every delivery gets
its own turn and its own recorded final response.

### Cancellation

Out of scope for the first version. Stopping the workflow ends the step's
wait; the accepted Crew delivery runs to completion on its own. Crew-side
cancellation may be added later.

### Failure

The workflow step fails when the trigger is inaccessible, disabled, deleted,
or completes with an execution error, or when the delivery is still queued or
running beyond the timeout. The step error should include the Crew and trigger
names plus the recorded run ID when one exists.

## Trigger status API

The public Crew trigger currently accepts a delivery and records the final
response. A complete external call-and-poll contract should expose a status
resource equivalent to:

```http
POST /api/hooks/product/{trigger_id}
GET  /api/hooks/product/{trigger_id}/runs/{run_id}
```

```json
{
  "run_id": "...",
  "status": "completed",
  "terminal": true,
  "final_response": "Review complete. Details are in reports/pr-87.md.",
  "error": ""
}
```

The internal workflow dispatcher may await the same service directly, but the
observable run contract should remain consistent with the public API.

## Crew trigger hardening

Crew triggers were built as fire-and-forget deliveries into a chat, while
workflow triggers were built for programmatic callers (idempotent recovery,
pollable status). A workflow stepper is a programmatic caller: it must
distinguish *queued* from *running* from *lost*, and it must reattach after
its own retries. The gaps below must be closed in the shared trigger layer,
so both the public endpoint and internal dispatch offer the same
accept → poll → terminal contract. Restart survival is explicitly out of
scope: the system does not promise it anywhere else either.

### Idempotent redelivery

Delivery dedupe is currently a process-local map that returns no run status,
and run appends have no duplicate check, so a repeat can fork history under
one run ID. Dedupe must consult the run store (check-then-append under the
existing run-file lock), and a duplicate delivery must return the existing run
ID with its current status, as workflow triggers do. No restart promises: the
guarantee holds while the server is alive.

### Accept-and-queue instead of busy

When the trigger conversation is occupied, the run is currently rejected
before any run record is written, while the public endpoint has already
returned 202 — a poller waits on a run ID that will never exist. There must
be no busy rejection at all: the delivery is accepted, recorded with a
`queued` status, and executed as the next turn in that conversation. The
runtime's mid-turn steering is for corrections to the running job; queued
deliveries each get their own turn and their own recorded final response.

### Conversation-level mutual exclusion

The run lock is keyed per trigger, but every `crew_chat` trigger of a project
shares one main conversation with the other triggers and the user.
One-turn-at-a-time holds for `isolated` triggers only. Main-chat dispatch
needs a conversation-level lock so two turns can never interleave in the same
conversation.

### Status vocabulary

Crew runs record `running`, `success`, `error`, and `stopped` (plus `partial`
and `interrupted`); accept-and-queue adds a real `queued` state for accepted
deliveries still waiting their turn. The step's displayed states map onto
this vocabulary, with `timed-out` owned by the waiting stepper rather than
the Crew run.

## Implementation outline

Steps 1–10 are implemented. Stopping runs is out of scope: there is no
stop tool, and an accepted Crew delivery runs to completion on its own.

1. Harden the shared Crew trigger layer: idempotent redelivery that returns
   the existing run with its current status, accept-and-queue with a real
   `queued` run state, and conversation-level mutual exclusion for main-chat
   triggers.
2. Extend both sides' trigger models with `kind: internal` plus a caller,
   reusing existing trigger storage, ownership checks, and management tools;
   public HTTP trigger endpoints skip internal triggers.
3. Add the Crew trigger status response and durable final-response lookup.
4. Extract internal Crew-trigger and workflow-trigger dispatch interfaces from
   their HTTP handlers so callers can use them without secrets or loopback
   HTTP. Internal dispatch must surface disabled and deleted outcomes
   synchronously, with the same delivery, conversation, history, run status,
   final response, and idempotency behavior as the hardened public endpoint.
5. Add `CrewPlanStep` parsing, validation, marshaling, plan editing tools, and
   workflow execution dispatch.
6. Save the final text to the step's current iteration execution directory and
   connect it to normal context-output handling.
7. Add Builder discovery and mutation tools for Crews, triggers, and read-only
   attachment setup.
8. Add Crew tools to discover, create, invoke, and poll internal workflow
   trigger runs.
9. Add the Crew canvas node and configuration UI.
10. Update Builder and Crew reference material and product guidance so each
    side understands internal triggers, and so the Builder can choose between
    a Crew and an ordinary step and between the two retained conversation
    histories.

## Acceptance criteria

- A Builder can select an accessible Crew and one of its enabled triggers and
  add it as a workflow step.
- Workflow Builder can create and manage a Workflow-to-Crew internal trigger;
  no public URL or secret is created.
- Crew can create and manage a Crew-to-Workflow internal trigger for an
  attached workflow, invoke it, and poll it.
- The Builder establishes or verifies a read-only Crew attachment.
- A workflow run invokes the trigger exactly once for one step attempt and can
  recover safely after a retry.
- A duplicate Crew delivery returns the existing run ID and its current
  status; no second run is launched.
- An occupied Crew conversation accepts the delivery and queues it as the
  next turn; the step shows queued until the turn starts.
- Two turns never interleave in one Crew conversation, including the shared
  main chat.
- The step displays queued, running, completed, failed, and timed-out
  states, mapped onto the recorded Crew run statuses (a stopped run fails
  the step; its status stays visible in the execution logs).
- The Crew receives the rendered instruction and declared workflow input.
- Main-chat triggers retain the main Crew conversation; isolated triggers
  retain their own prior trigger-run conversation.
- The exact final Crew text is saved as `response.md` (or the configured
  `context_output`) inside the calling workflow iteration.
- Downstream steps can read Crew-relative files through the read-only
  attachment and cannot write to them.
- No trigger secret appears in `plan.json`, logs, frontend state, or step
  outputs.
- Internal trigger invocation is rejected when caller identity, target
  identity, attachment, permissions, or enabled state no longer matches.
- A workflow run whose Crew access was lost after configuration fails fast
  before any step executes; no step reads through the stale attachment.
- Crew-step token spend appears in both the workflow run's cost breakdown and
  the Crew project's cost history, each side labeled with the other.
- A crew step's run entry links to its exact Crew turn, and the execution logs
  include a crew category with the polling timeline and final response.
- Crew projects remain persistent projects and do not gain iteration folders.
