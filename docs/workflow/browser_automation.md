# Browser Automation in Workflows

Browser workflows use the `agent-browser` skill and the managed
`agent_browser` tool. Set `capabilities.browser_mode` to `auto`, `headless`, or
`cdp`; use `none` when browsing is not required.

## Authoring sequence

1. Load the installed command guide with
   `agent_browser(command="skills", args=["get", "core"])`.
2. Open or select the workflow's labeled tab.
3. Take an interactive snapshot.
4. Act with a live ref or a durable selector.
5. Re-snapshot and verify the expected state.
6. Save stable site knowledge to the workflow's learnings.

## Persisted scripts

Snapshot refs such as `@e1` are valid only for the current page state. A saved
script must either parse a fresh ref from a new snapshot or use a stable hook.
Prefer selectors in this order:

1. `data-testid`, `data-test`, `data-cy`, or `data-qa`;
2. a hand-written semantic `id` or `name`;
3. `aria-label`;
4. role plus accessible name resolved from a fresh snapshot;
5. stable label, placeholder, or visible text;
6. structural CSS or XPath only as a documented last resort.

Avoid generated framework IDs, hashed class names, and `nth-child` chains.
When the accessibility snapshot is incomplete, use a read-only `eval` probe to
inventory stable DOM attributes before choosing a selector.

## CDP workflows

CDP mode attaches to a visible Chrome and can reuse existing login state. Keep a
stable labeled tab for each workflow or account. A single shared CDP browser is
the normal concurrency model; configure multiple CDP ports only when one
workflow genuinely needs independent Chrome profiles, such as testing two
logged-in accounts on the same site.

## Debugging

Use `network`, `console`, `errors`, screenshots, HAR capture, recording, and
tracing through the same managed tool. This preserves the workflow's tab lock
and session identity. Do not use raw CDP calls or shell-launched browser actions.

See [the core browser reference](../core/browser.md) for setup, isolation,
artifact handling, and operational limits.
