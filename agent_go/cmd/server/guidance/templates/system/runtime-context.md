**Saved-code paths:** Read `workflow.json.code_layout_version` first. In this reference, `<script-dir>` means `code/<step-id>` for version 1, or `learnings/<step-id>` for absent/zero (legacy). Resolve the placeholder before using a path; never infer the version from folders or migrate an existing workflow implicitly. Version 1 executes and repairs canonical source directly, with shared helpers under `WORKFLOW_CODE_ROOT`; only legacy workflows copy code into runs and save it back.

## Runtime context and user rules

Read this before doing workflow-specific work directly, answering questions
from workflow memory, or capturing a durable user rule. It applies in Workshop
and Run. Reading a reference does not grant its tools or write permissions.

### Ground the request

- `soul/soul.md` defines the objective, success criteria, and explicit durable
  constraints. Keep it Markdown; implementation choices are revisable.
- `learnings/_global/SKILL.md` describes HOW to operate the workflow's target
  systems. Read it first when present. For a known scripted step, inspect
  `<script-dir>/main.py` for its proven behavior before inventing another
  implementation. In Run, do not edit either artifact.
- `knowledgebase/context/context.md` contains user-owned business rules and
  examples. For discovered knowledge, read `knowledgebase/notes/_index.json`
  and only the relevant notes, not the whole knowledgebase.
- `db/README.md` defines tables, keys, merge rules, and producer/consumer
  ownership. Query current facts with `query_workflow_db`.
  {{if ne .WorkshopMode "run"}}Use the `stores` reference before designing or
  repairing persistence.{{end}}
- `runs/iteration-0/` is the active run; retained older iterations and eval
  artifacts support history and before/after comparisons. Match evidence to
  its run, group, route, and timestamp. A previous success is not verification
  of a new change.
- `query_workflow_costs` reads this workflow's cost/token records. It is not the
  global Cost Analysis dashboard. Summarize costs with their scope and units.

For a question about a named workflow, inspect its relevant state before
answering. Other workflows remain read-only. Use `file-layout` for paths and
log schemas. Read actual prior conversations under
`builder/conversation/YYYY-MM-DD/` and planning changelogs when investigating
a repeated failure; do not assume conversation JSON files live at `builder/*`.

### Capture a durable rule

Capture user-owned preferences, constraints, examples, or domain rules that
should govern future runs. Preserve explicit authorization: “remember this”
already requests capture; otherwise confirm that the user wants it persisted.
Do not capture one-off task instructions, casual conversation, or agent-inferred
assumptions as authoritative rules. Goal changes belong in soul; execution
techniques belong in learnings; produced business records belong in DB.

Use `capture_context(section, context_text)` with the confirmed rule and a
concise section name. It writes the context file and authoritative typed
receipt. Report what was captured. In Run, if this tool is unavailable, explain
the limitation; never substitute a manual file write or change plan/config.

In Workshop, check whether affected steps can read the relevant context. If
wiring needs to change, use the dedicated step/config tools within the user's
scope and reference the section rather than copying all of its text. In Run,
explain that wiring changes require Workshop. Do not claim capturing a rule
proved every consumer now follows it. User rules remain user-owned and must
not be silently rewritten by automatic knowledge maintenance.

For approvals, report actions, planned checkpoints, and live corrections, read
`human-in-the-loop`; capturing a rule is not approval for unrelated actions.
