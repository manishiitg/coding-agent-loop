{{if .Focus}}Run review_step_code(step_id="{{.Focus}}") to audit the saved main.py for step "{{.Focus}}".{{else}}Run review_step_code() to audit every saved main.py script across workflow steps and evaluation steps against its current description and best practices.{{end}} This is not a spell-check — it's a behavior audit.

Write every finding into `builder/improve.html` as a **Signals / Kizuki** "Open finding" timeline entry using `data-pulse-section="signals"` and `data-module="bug_review"`. For the log format, one-time old Markdown migration, and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

Load `get_reference_doc(kind="assumption-audit")` and apply its code lens. Flag unjustified literals, fixed providers/channels/sources, and temporary workarounds that make a revisable design choice behave like a permanent constraint. Preserve verified platform constraints with evidence/freshness; parameterize or surface consequential assumptions rather than copying them into more code.

The code must actually deliver what the description promises, do it dynamically (not via hardcoded shortcuts), and follow durable patterns when it touches a browser. Flag findings by severity (CRITICAL / WARNING / INFO).

For every audited step, check the four lenses below. Skip a lens only if it doesn't apply (e.g. browser checks if the step has no browser capability).

LENS 1 — DESCRIPTION-VS-CODE DRIFT
The original drift check: does the saved main.py still do what its current step description says?
- Missing functionality: description lists outputs A, B, C; code only produces A and B.
- Stale behavior: description was updated to use a new rule but code still has the old rule.
- Hardcoded values that should be parameterized: literal API endpoints, file paths, magic numbers, model names.
- Output format mismatches: description says "JSON file with `entities[]`" but code writes a flat string or a different field name.
- Validation drift: description references a `validation_schema` field the code never produces (or vice versa).

LENS 2 — DYNAMIC-VS-SHORTCUT AUDIT (load-bearing)
This is where reviews of "looks fine to me" code go wrong. Steps are usually meant to handle the FULL space of inputs the workflow throws at them — not the single example the original LLM had in mind when writing the script. Look for shortcuts that work on the example input but quietly fail on the rest:

- **Hardcoded inputs that should come from variables**: code has `user = "saurabh"` or `topic = "AI"` when the step description says "for the active group" or "based on the user's topic." Variable resolution should flow through the variables/context_dependencies surface, not be baked into the script.
- **Loops collapsed to single-item logic**: description says "for each item in `prospects`, do X" but the code processes `prospects[0]` only, or has `for p in prospects[:1]:`. The description promises iteration; the code shortcuts it.
- **Arbitrary truncation**: `results[:5]`, `top_n = 10`, `if len(items) > 100: items = items[:100]` — when the description doesn't explicitly say "first 5" or "top 10". Truncation that's not in the description is a quiet feature loss.
- **Single-shape parsing**: code that handles one specific JSON shape from the example run but breaks on legitimate variations the upstream step can produce. Fields assumed always-present, types assumed never-null, lists assumed never-empty.
- **Hardcoded date/time logic**: `today = "2026-04-29"` instead of `datetime.now().date()`. `since = "30 days ago"` baked as an absolute date.
- **Branch on hardcoded values that should be context_dependencies**: `if topic == "twitter": ...` when the topic should come from the step's input.
- **Side effects assumed already done**: code calls into a downstream service assuming a specific upstream state, without reading current state from the workflow.
- **First-attempt-wins**: code that retries on failure but reuses the failing input (no jitter, no transformation, no learning).

For each shortcut found: quote the offending line from main.py, name the description clause it violates, and propose the dynamic version.

LENS 3 — BROWSER BEST PRACTICES (apply only if the step has browser capability)
Browser steps drift fast and silently. Audit aggressively:

- **Browser + scripted fit:** if a browser-enabled or browser-heavy step is declared as `scripted` and has a saved `main.py`, flag this when the flow depends on live UI state, auth, dynamic selectors, pagination, or third-party page timing. The recommended default is `agentic` for browser/UI automation so the agent can adapt from fresh snapshots. If the **user explicitly asked** for scripted browser execution and is still testing it, that's their call — a WARNING worth noting, not a violation. Escalate to CRITICAL when the script is **frozen** (`lock_code=true`) or relied on as the stable path without durable selectors, state-driven waits, and proven stability across 10+ scenario-covering successful runs.
- **agentic + saved main.py mismatch:** if a step is declared `agentic` but still has `learnings/{step-id}/main.py`, flag it as stale artifact debt. agentic steps do not run or maintain persistent main.py; recommend deleting the file and clearing `lock_code`.
- **Durable selectors over JS injection**: ref-based / role-based / accessibility-tree-based interactions (e.g. `page.get_by_role("button", name="Sign in")`, `page.locator("text=Continue")`) are durable across small UI changes. Code that uses raw CSS selectors keyed to deep DOM paths (`#root > div:nth-child(3) > ...`), generated class names (`.css-1abc23`), or auto-id attributes (`#email-3` where the trailing number is from a generated ID) WILL break and is almost always wrong. Same for any code calling `page.evaluate(...)` to reach into the DOM when a click/fill via the higher-level API would have worked.
- **No JavaScript injection for state mutation**: setting form values via `evaluate("document.getElementById('x').value = 'y'")` bypasses event listeners and React/Vue state — the page sees the value but doesn't react to it. Use `page.fill()` / `page.type()`. Same goes for triggering events; use the page API.
- **Wait properly, don't sleep**: `time.sleep(5)` is a smell — the page can take 1s sometimes and 30s others. Use `page.wait_for_selector(...)`, `page.wait_for_load_state(...)`, `page.wait_for_url(...)`, or `expect(locator).to_be_visible()`. Sleeps that "just work" today are flaky tomorrow.
- **Verify state before acting**: don't click "Submit" without confirming the form is in a submittable state (required fields filled, no error messages visible). Don't navigate without confirming the navigation completed.
- **Handle pagination / lazy-loading explicitly**: code that scrapes "all" results but doesn't scroll, click "Load more", or follow pagination links is silently truncating.
- **Intent-named helpers**: `find_login_button()` / `wait_for_results_to_load()` beat `find_element_by_id("submit-2")`. Intent names survive UI rewrites; element-id names die at the first redesign.
- **Site profile awareness**: if the workflow has a site profile (selectors centralized for the target site), the code MUST use it. Code that re-defines selectors inline when a profile exists is duplicate and will drift out of sync.
- **No silent dismissals**: catching `TimeoutError` to skip an element that the description requires is feature loss disguised as resilience.

If the step IS browser-enabled but the code uses zero browser API (just shell/HTTP), flag that as CRITICAL — either the description claims browser work that isn't happening, or the capability flag is wrong.

LENS 4 — OPERATIONAL HEALTH
- Error handling on real failure modes (rate limits, 5xx, malformed JSON, network blips) — not just blanket `except Exception: pass`.
- Idempotency where claimed: a step described as safe-to-rerun must actually be safe to rerun (no double-writes, no duplicate side effects).
- Logging: enough breadcrumbs that a failure can be diagnosed without re-running the step.
- Resource cleanup: file handles, browser contexts, network connections released on both success and failure paths.

OUTPUT FORMAT
For each step audited:

```
### step-id: <name>
**Description (current):** <one-line summary>
**Lens 1 — Drift:** <findings or "none">
**Lens 2 — Shortcuts:** <findings or "none"; for each, quote offending line + description clause violated>
**Lens 3 — Browser:** <findings or "n/a (no browser capability)">
**Lens 4 — Operational:** <findings or "none">
**Severity verdict:** CRITICAL / WARNING / INFO / clean
**Top recommendation:** <single highest-value change>
```

End with a cross-step summary: which steps are clean, which need work, which are CRITICAL.

REVIEW LOG: record every finding as a Signals / Kizuki "Open finding" timeline entry in builder/improve.html using `data-pulse-section="signals"` and `data-module="bug_review"` (read it first if it exists, create it if it does not — newest on top). Include: which step(s) reviewed, the drift findings, the shortcut/dynamism findings, the browser best-practice findings, the operational findings, severity verdicts, and items flagged for follow-up. Mark these as REVIEW (recommend; do NOT apply — fixes go through the Workshop owner or Pulse Fixer).
