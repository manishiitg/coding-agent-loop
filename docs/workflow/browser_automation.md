# Browser Automation: Durable Selectors & Discovery

This doc describes the browser-specific rules injected into execution, learning, harden, and review prompts when a workflow has a browser MCP available (`playwright`, `camofox`, `agent-browser` skill, or a CDP port).

## The problem

Browser automation agents interact with elements through two channels:

1. **Accessibility tree** — via `browser_snapshot`, which returns a YAML tree of roles + accessible names + current state (disabled, expanded, selected). Each element gets an ephemeral `ref` (`@e1`, `e68`) usable only within that snapshot.
2. **Direct DOM** — via `browser_evaluate` for reads, and the usual `browser_click` / `browser_type` / `browser_navigate` for writes.

Two failure modes this doc addresses:

- **Refs bleed into saved scripts.** If a learn-code step's `main.py` bakes `click @e3`, the next run's snapshot assigns `e3` to a different element and the script fails. Refs are session-local.
- **The a11y tree misses elements.** Custom `<div>` buttons with onclick, dropdowns inside React portals, autocomplete options, form inputs lacking role/label — none show up in the snapshot. Agents flying blind either guess selectors from memory or give up.

## The core rule

**Selectors persisted to main.py must be DETERMINISTIC across future runs.** A deterministic selector resolves to the same element on every replay — across browser restarts, page rebuilds, deploys that rename auto-generated classes (`css-8xy3zb`), React key changes, re-hydration. Anything that depends on session state (refs) or on unstable DOM shape (nth-child chains, auto-gen class names) is NOT deterministic and will silently match the wrong element tomorrow.

Refs (`@e1`, `e68`, `"ref": "abc123"`) are session-local identifiers the browser tools assign per snapshot. They are valid only for the immediate next tool call in the same session. **Any ref hardcoded into main.py is a bug.**

Two equally valid paths to a deterministic selector:

- **Path A — snapshot + act.** `browser_snapshot` gives you role + accessible name + widget state. Pick a locator from the priority list, then act via individual tool calls (`browser_click`, `browser_type`, `browser_select`, `browser_navigate`) OR via Playwright code (`browser_run_code` with `page.getByRole(...)`, `page.locator(...)`). Tool-call style is a debugability preference, not a durability dimension.
- **Path B — DOM probe via eval.** Run the canonical read-only probe below via `browser_evaluate` (Playwright/Camoufox) or `agent-browser eval` to get a structured inventory. Use when the a11y snapshot misses elements (custom `<div>` buttons, portal/popover children, form inputs the tree skips).

Both paths terminate in a durable locator expressed in the persisted script. `browser_run_code` using Playwright's locator API (`page.getByRole('button', { name: 'Continue' }).click()`) is durable and is the right shape for chained multi-step interactions. `browser_evaluate` for inspection is allowed; `browser_evaluate` that hand-rolls `document.querySelector` for an action and then writes that selector into the script is fine IF the selector is in the durability tier below — avoid structural CSS chains that break on DOM rearrangement.

## Durable-selector priority

When writing locators, agents must pick the highest-priority hook that uniquely identifies the element:

1. **`data-testid`** / `data-test` / `data-cy` / `data-qa` — ideal, rare on production sites.
2. **Hand-written semantic `id` or `name`** — e.g. `#panAdhaarUserId`, `#loginPasswordField`. **Skip auto-generated ids**: `radix-_rN_`, `mat-mdc-*`, React `:rNN:`, UUID-shaped `8-4-4-4-12` — all rotate across rebuilds.
3. **`aria-label`** — very durable when present.
4. **Role + accessible name** — `page.get_by_role("button", name="Sign in")`.
5. **`get_by_label(...)`** / `get_by_placeholder(...)` / `get_by_text(...)`.
6. **Structural CSS / XPath with nth-child chains** — last resort; flag in learnings.

This priority mirrors Playwright's own recommendation but is explicitly enforced in prompts so code-gen doesn't default to fragile nth-child paths.

## The DOM probe

When the a11y snapshot is insufficient (custom divs, portal dropdowns, missing form inputs), the agent runs a **read-only DOM probe** via `browser_evaluate`. The probe is a canonical JS snippet embedded in the prompt — agents copy it verbatim so results stay comparable across runs.

### What the probe returns

```jsonc
{
  "url": "https://example.com/page",
  "framework": "angular-material" | "radix" | "headlessui" | "react" | "unknown",
  "stableHookInventory": {
    "data-testid": 0, "aria-label": 38, "id": 80, "name": 5, "role": 73, ...
  },
  "popoverItems": [
    // Visible children of floating/portal containers, auto-detected
    { "source": "popover", "tag": "div", "text": "Option A",
      "attrs": { "role": "option", "aria-label": "..." },
      "cssPath": "[aria-label=\"Option A\"]" }
  ],
  "actionableItems": [
    // Cursor-pointer / onclick / interactive-role elements body-wide
    { "source": "actionable", "tag": "button", "text": "Continue",
      "attrs": { "aria-label": "Continue", "type": "submit" },
      "cssPath": "[aria-label=\"Continue\"]" }
  ],
  "counts": { "popover": 3, "actionable": 42 }
}
```

### Why each field matters

- **`stableHookInventory`** tells the agent which hook strategy applies site-wide: "38 aria-labels, 0 testids → use aria-label + role+name across this workflow". Record this once per site in learnings.
- **`framework`** drives known-bad-id filters (Radix → skip `_rN_`, Angular Material → skip `mat-mdc-*`).
- **`popoverItems`** captures floating/portal content (React portals, Radix Popover, Headless UI menus) that the a11y tree often misses.
- **`actionableItems`** catches custom `<div>` buttons and form inputs the a11y tree skips because the tag itself implies interactive.
- **`cssPath`** is pre-filtered against auto-generated id patterns. If non-null, use it directly in main.py. If null, fall back to `role+name` from the a11y snapshot.

### When to run the probe

- After first navigation to a new site (for the stable-hook inventory).
- When a specific element isn't in `browser_snapshot` output but should be clickable.
- When debugging a selector failure — the probe's current DOM view is ground truth.

### When NOT to run the probe

- As a substitute for `browser_snapshot`. The snapshot is faster, lighter, and carries widget state (disabled/expanded). Probe the DOM only when the snapshot is insufficient.
- For actions. The probe is strictly read-only. Writes go through `browser_click`, `browser_type`, `browser_select`, `browser_navigate`.

### Multi-backend invocation

The probe JS body is identical across backends. Only the wrapper differs:

| Backend | Invocation |
|---|---|
| Playwright MCP | `call_mcp('playwright', 'browser_evaluate', {'function': '<JS>'})` |
| Camoufox MCP | same as Playwright — `browser_evaluate` tool with `function` param |
| agent-browser CLI | `agent-browser eval "<JS>"` — returns JSON on stdout; pipe to a file if large |

## Site-access resilience

Production sites often block Playwright-launched browsers. When `browser_navigate` returns "Permission Denied" / a blank page / a native `alert()` freeze, the agent should:

1. **Stop launching a fresh browser** — the Playwright fingerprint is the problem.
2. **CDP-attach to an already-running Chrome** with a real user profile:
   - `agent-browser --cdp <port>` / `--auto-connect`
   - Playwright's `connect_over_cdp("http://localhost:9222")` in `main.py`.
3. **Register a dialog handler** before interacting if the page shows native alerts.
4. **Document the access preamble in learnings** so future steps detect-and-switch automatically.

Cloudflare-style interstitials may also be bypassable by using an alternate subdomain (e.g. `prod.gcp.example.com` vs `prod.example.com`). Record these in the site-profile learning.

## Ephemeral refs — explicit ban

Refs like `@e1`, `e68`, `"ref": "abc123"` appear in snapshot output and are usable **only for the immediate next tool call in the same session**. They are:

- **Not stable across snapshots** — a fresh snapshot reassigns them.
- **Not stable across sessions** — every new browser spawn rebuilds the ref map.
- **Never safe to save** to main.py, learnings, or any other artifact.

The validator `reviewMainPyScript` at `controller_learn_code.go` runs a regex check (`Check 10`) that rejects any saved main.py containing `['"]@e\d+['"]` or `{"ref": "abc..."}` when the script calls browser tools. This catches the failure mode at save time before it costs you a broken run.

## What learnings must capture for browser steps

When `learnings_access="read-write"` and the step used a browser tool, the post-step learning agent MUST produce the following content in `learnings/_global/SKILL.md` (typically split across `references/site-profile.md` and `references/selectors.md`):

1. **Site access preconditions** — CDP required, Cloudflare interstitial on apex, dialog handler needed, failure signatures so future steps can detect-and-switch.
2. **Stable-hook inventory** — once per site: counts of testids / aria-labels / ids, framework, recommended locator strategy.
3. **Per-action intents, not raw selectors** — record semantic identity plus 1–2 alternates:
   ```
   Step [login.fill_user_id]
     intent: {by: "id", value: "panAdhaarUserId"}
     alt:    {by: "placeholder", value: "PAN/ Aadhaar/ Other User ID"}
     alt:    {by: "role+name_contains", role: "textbox", name: "User ID"}
     notes:  Continue button stays disabled until input has a value.
   ```
4. **Behavioral quirks** — multi-step flows (User ID → Continue → Password), cross-domain redirects (e-Filing → TRACES), disabled-until-valid gates, OTP/captcha branches, phantom controls (e.g. a `#btn` that looks like Proceed but does nothing — the real action is a link below).
5. **Known-bad selector patterns** — explicit per-site "do NOT use" list (Radix auto-ids, mat-mdc-*, whatever bit this workflow).

Intents (not raw selectors) are the core contract: future fix loops and harden runs re-derive the selector from the intent against the current snapshot/probe output. This is why intents are 1–2 alternates per action rather than a single locator.

## Which agents see these rules

The browser-authoring block (`BuildBrowserAuthoringRules`) is injected into every prompt that authors, patches, or reviews main.py — **provided the workflow has a browser MCP** (`HasBrowserCapability()` returns true):

| Agent | Reads the block? | Why |
|---|---|---|
| Execution agent (learn-code mode) | ✅ | Writes main.py; must use durable locators |
| Execution agent (code-exec-only mode) | ✅ | Even throwaway scripts shouldn't bake refs — same discipline |
| Learning agent | ✅ | Needs to know what learnings SHOULD contain for browser steps |
| Harden agent | ✅ | Patches main.py during eval-driven fixes; must enforce the contract |
| Review agent | ✅ | Detects drift in main.py; must know what "drift" means |
| Todo-task orchestrator | ✅ | Same execution semantics as regular steps |

Non-browser workflows (workflow config has no browser server/skill/CDP) skip the entire block — saves ~60 lines of prompt tokens per step.

## Code locations

- **`prompt_sections.go:BuildBrowserAuthoringRules()`** — the full browser-rules block including the canonical DOM probe JS. Single source of truth.
- **`prompt_sections.go:browserAuthoringRulesIfBrowserEnabled()`** — helper for call sites that have direct access to the orchestrator (workshop manager).
- **`prompt_sections.go:BrowserAuthoringRulesFromTemplateVars()`** — helper for call sites that only have templateVars (harden, review, execution-only).
- **`controller.go:HasBrowserCapability()`** — the gate. Checks explicit non-`none` browserMode, CDP port, registered servers (`playwright`/`camofox`), `agent-browser` skill. Never use `GetBrowserMode() != ""` as a proxy — empty means auto-detect, not disabled.
- **`execution_only_agent.go`** — template has `{{.BrowserAuthoringRules}}` slot; populated by `BrowserAuthoringRulesFromTemplateVars(templateVars)`.
- **`interactive_workshop_manager.go`** — workshop/harden/review call sites set `HasBrowserAccess` from `iwm.controller.HasBrowserCapability()`.
- **`learning_agent.go`** — `{{if .HasBrowserAccess}}...{{end}}` block describes the required learnings shape for browser steps.
- **`controller_learn_code.go:reviewMainPyScript` Check 10** — regex-rejects ephemeral refs in saved main.py when the script imports browser tool calls.

## What's intentionally NOT done

- **No new `browser_inspect` MCP tool.** The probe is JS the agent passes to existing `browser_evaluate`. Rationale: keeps tool surface lean; probe logic stays in one prompt file. Promote to a real tool only if prompt-embedded proves unreliable in practice.
- **No self-healing runtime.** When a saved main.py selector rots, the current flow is: run fails → execution agent sees error → patches main.py. A future "self-healing" pass could read the intent from learnings, re-derive the selector from the current snapshot, and patch automatically without invoking the execution agent. Designed but deferred until we have real failure-pattern data.
- **No iframe probe traversal.** The probe runs in the top document only. Sites with iframes (TRACES embedded forms, Stripe checkout) need per-frame probe invocation. Deferred.

## Related docs

- [learning_architecture.md](learning_architecture.md) — how learnings get written + auto-locked per description hash.
- [step_config_format_specification.md](step_config_format_specification.md) — `learnings_access` field governing read/write of global SKILL.md.
- [learn_code_flow.md](learn_code_flow.md) — the full learn-code lifecycle (fast path, fix loop, save-back).
