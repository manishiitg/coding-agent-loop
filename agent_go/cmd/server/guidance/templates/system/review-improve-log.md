## Review / improve logging conventions

Canonical conventions shared by every `/review-*` and `/improve-*` skill (and chat-driven fixes that touch them). Load this once; the individual skills point here instead of restating it.

### The two logs

- **`builder/review.html`** — findings from `/review-*` (recommendations; REVIEW = recommend, do NOT apply).
- **`builder/improve.html`** — applied/proposed changes from `/improve-*` and chat fixes (the auditable decision trail).

Both are **self-contained HTML files, not Markdown**. Call `get_reference_doc(kind="html-output")` for the style baseline. Use `.badge.fail` for CRITICAL, `.badge.warn` for WARNING, `.badge.pass` for resolved. **Always read the existing file first** and carry forward unresolved findings rather than overwriting.

### One-time `.md → .html` migration (still live)

Legacy `builder/review.md` / `builder/improve.md` may still exist in some workspaces. When you open either log, if the `.md` version exists: read it in full, extract every unresolved `F-…` / `I-…` finding, any important context, and any structured `improve-decision` blocks into the corresponding `.html`, then delete the `.md` with `execute_shell_command`. Migrate both files (review and improve) when both exist. Do this once per workspace, before writing the new HTML, so nothing is lost; after the `.md` is gone there is nothing to migrate.

### Finding / decision IDs

- **`F-YYYY-MM-DD-NNN`** — a review finding (in `builder/review.html`).
- **`I-YYYY-MM-DD-NNN`** — an improve proposal/decision (in `builder/improve.html`).

`YYYY-MM-DD` is today; `NNN` is a 3-digit sequence that restarts at `001` each day. Scan the target file for today's highest existing sequence and continue from there — **never reuse an id**. Format a finding line so later close-out edits can target it exactly:

```
- [F-YYYY-MM-DD-NNN] <severity>: <step-id, db table, or "structural"> — <finding>
```

(In Run mode, where the log isn't written, assign temporary `F-…` ids in the chat response only — do not scan or write `builder/review.html`.)

### Improve decision block + close-out

When an `/improve-*` skill (or a chat fix) applies or proposes a change, append a fenced decision block to `builder/improve.html` via `diff_patch_workspace_file`:

```improve-decision
{"id":"I-YYYY-MM-DD-NNN","ts":"<ISO-8601 UTC>","source":"agent","trigger":"<skill-name>","applied_changes":["<path>"],"rationale":"<one-line>","linked_review_finding":["F-…"]}
```

`linked_review_finding` is omitted when no matching review finding exists. When a change resolves a finding already in `builder/review.html`, also append, on its own line immediately after that finding:

```
**[RESOLVED YYYY-MM-DD — <one-line how it was fixed>]**
```

This shared schema is what makes review/improve activity auditable and machine-parseable — keep the field names stable.
