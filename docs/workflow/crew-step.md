# Crew workflow step

**Status:** Proposed

## Summary

A workflow can use a Crew project as an executable step. The step invokes one
of the Crew's authenticated triggers, waits for that trigger run to finish, and
saves the Crew's final text response in the calling workflow's current
iteration folder. Later workflow steps can read the Crew's persistent project
files through the platform's existing read-only attached-folder mechanism.

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
saved base instruction and conversation destination. The step supplies the
workflow-specific instruction and runtime input.

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

For example:

```text
Workflow/example/runs/iteration-12/execution/review-with-rts/response.md
```

The execution record should also retain operational metadata—Crew project ID,
trigger ID, Crew run ID, session ID, status, timestamps, and error details—in
the workflow's existing progress/run metadata. The Crew does not have to emit
JSON for this metadata.

### Trigger request

The delivery payload should include at least:

```json
{
  "source": "workflow_step",
  "workflow_id": "release-pipeline",
  "workflow_run_id": "iteration-12",
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

Payload data cannot override Crew permissions, tools, trigger ownership,
conversation destination, or folder guards.

### Internal and external dispatch

An external caller invokes a Crew trigger through its authenticated HTTP
endpoint. A workflow running inside the same deployment should reuse the same
trigger dispatcher internally rather than sending an HTTP request to itself or
placing trigger secrets in `plan.json`.

Internal dispatch must produce the same trigger delivery, conversation,
history, run status, final response, and idempotency behavior as the public
endpoint.

## Files and attached folders

Crew has one persistent project workspace. It does not have workflow iteration
folders, and this feature must not add them.

Before a Crew step can run, the target Crew project must be attached to the
workflow using the existing attached-folder mechanism. The attachment is
read-only. The attachment is both the access grant and the stable namespace
through which downstream steps read Crew files.

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

Suggested Builder tools are:

- `list_crews`
- `list_crew_triggers`
- `attach_crew_to_workflow`
- `add_crew_step`
- `update_crew_step`
- `delete_crew_step`

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

## Access and security

- Never store a plaintext trigger secret in the workflow plan.
- Check Crew and trigger access when the step is configured and again when it
  executes.
- Require the target Crew to be attached read-only before execution.
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
selected conversation is busy, the step should show a waiting state and retry
until capacity is available or its timeout expires. It must not interleave two
turns in the same conversation.

### Cancellation

Stopping the workflow should cancel its wait and request cancellation of the
owned Crew delivery when possible. Cancellation must be scoped by Crew run ID;
it must never stop an unrelated user or automation turn.

### Failure

The workflow step fails when the trigger is inaccessible, disabled, deleted,
busy beyond the timeout, stopped, or completes with an execution error. The
step error should include the Crew and trigger names plus the recorded run ID
when one exists.

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

## Implementation outline

1. Add the Crew trigger status response and durable final-response lookup.
2. Extract an internal Crew-trigger dispatch interface from the HTTP handler so
   workflow execution can call it without secrets or loopback HTTP.
3. Add `CrewPlanStep` parsing, validation, marshaling, plan editing tools, and
   workflow execution dispatch.
4. Save the final text to the step's current iteration execution directory and
   connect it to normal context-output handling.
5. Add Builder discovery and mutation tools for Crews, triggers, and read-only
   attachment setup.
6. Add the Crew canvas node and configuration UI.
7. Update Builder reference material and product guidance so the Builder can
   choose between a Crew and an ordinary step and between the two retained
   conversation histories.

## Acceptance criteria

- A Builder can select an accessible Crew and one of its enabled triggers and
  add it as a workflow step.
- The Builder establishes or verifies a read-only Crew attachment.
- A workflow run invokes the trigger exactly once for one step attempt and can
  recover safely after a retry.
- The step displays queued, running, completed, failed, stopped, and timed-out
  states.
- The Crew receives the rendered instruction and declared workflow input.
- Main-chat triggers retain the main Crew conversation; isolated triggers
  retain their own prior trigger-run conversation.
- The exact final Crew text is saved as `response.md` (or the configured
  `context_output`) inside the calling workflow iteration.
- Downstream steps can read Crew-relative files through the read-only
  attachment and cannot write to them.
- No trigger secret appears in `plan.json`, logs, frontend state, or step
  outputs.
- Crew projects remain persistent projects and do not gain iteration folders.

