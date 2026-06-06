## human_input — Asking the User a Question Mid-Workflow

`human_input` is the step type that **blocks the workflow and asks the
user a question**, returning their response to drive subsequent steps.
Load this skill when adding or editing a human_input step, deciding
between input types, or pairing it with a `routing` step for
user-driven branching.

Use `add_human_input_step` / `update_human_input_step` to manage these
in the plan.

## When to use human_input

- The next decision genuinely **needs user judgment** (e.g., picking
  between approaches, confirming a destructive action, providing
  context the agent can't infer).
- The workflow has an **approval gate** (e.g., "proceed with sending?").
- The user must **provide data** the agent can't otherwise discover
  (e.g., a PAN, a confirmation code from email).

**Don't use human_input for:**

- Data the agent can derive from variables, prior steps, or tools.
- Validation that can be done deterministically (use `validation_schema`
  + retry).
- "Status checks" the user doesn't actually need to see — those become
  noise and break unattended runs.

## Input types

Set the `input_type` on the step:

- **`text`** — free-form text response. Use when the answer space is
  open (e.g., "What's the company name?", "Paste the OTP from email").
- **`yesno`** — boolean response. Use for confirmation gates inside a
  running workflow (e.g., "Send the report now?").
- **`multiple_choice`** — one selection from a fixed list. Use when the
  running workflow must ask for one enumerable answer (e.g., "Which
  environment?" -> ["staging", "production"]).

## Branching from human_input

Do not add `human_input` just to ask which fixed branch to run when the
user already told the builder in chat. In that common case, model the
choice as a deterministic `routing` step and have the builder/caller
pass `route_selections` when starting the workflow.

Use `human_input` only when the running workflow truly has to stop and
ask a human because the answer is not already available at launch. If
that in-run answer branches a small fixed menu, `option_routes` is fine.
If the branch must be visible as an explicit deterministic router, map
the answer to `route_selection.json` and route after it.

```
routing(
  id="pick-job",
  routes=[
    {route_id: "lead-verification", next_step_id: "ask-target"},
    {route_id: "withdrawals-cleanup", next_step_id: "withdrawals-check"}
  ]
)

run_full_workflow(
  group_name="aayush",
  route_selections={"pick-job": "lead-verification"},
  human_inputs={"ask-target": "Mar26"}
)
```

The `routing` skill has the full deterministic route-structure rules
for workflows that need an explicit router node.

## Schedule / unattended runs

Schedules run **unattended** — human_input steps in a workflow that's
scheduled cannot wait for a real human. Two strategies:

1. **Pre-supply responses via `human_inputs` arg**: `run_full_workflow(group_name, human_inputs={"step-id": "yes"})`.
   The schedule's message provides the response upfront. Required for
   any human_input step in a scheduled run, or the schedule fails with
   "missing human_input responses".
2. **Restructure the workflow** to remove the human_input — e.g.,
   default to a safe choice for scheduled runs and only ask
   interactively.

When you add a human_input step, decide from context whether the
workflow may be scheduled. If yes, plan for response preparation and
state the assumption briefly instead of asking unless the schedule
policy is blocking.

## Validation

`human_input` accepts any value the user types (for `text`) or any
selection from the configured list (for `multiple_choice` / `yesno`).
Validation typically happens on the **downstream** step that consumes
the response — that step's `validation_schema` checks the response
makes sense for its purpose.

If you need the user's response itself to match a format (e.g., a
valid PAN, an email, a number in a range), add explicit validation
guidance in the question prompt AND validate downstream rather than
relying on input-level validation (the human can always type anything
into a text field).

## Anti-patterns

- **Question-as-status-update**: "Do you want me to continue?" without
  a real branch — just continue.
- **Open-ended questions in scheduled runs**: schedules can't answer.
- **Multiple human_inputs in sequence**: feels chatty. Either gather
  context upfront with variables, or use a single message_sequence
  step that has a conversation.
- **Asking twice**: do not add a `human_input` branch picker when the
  builder can infer the route from the user's launch request and pass
  `route_selections`.

## Tools

- **`add_human_input_step(step_id, prompt, input_type, options?, context_output, ...)`** — add the step. `options` is required for `multiple_choice`.
- **`update_human_input_step(step_id, prompt?, input_type?, options?, ...)`** — edit.
- **`execute_step(step_id, group_name, human_input="<response>")`** — in workshop mode, test by passing the response directly via `human_input`. Skips the actual prompt UX.

For the full signatures + parameters see
`get_reference_doc(kind="workflow-tools")`.
