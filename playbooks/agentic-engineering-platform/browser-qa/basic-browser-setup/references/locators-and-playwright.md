# Locator discovery and repeatable Playwright setup

Basic setup includes finding and saving verified locators for the agreed smoke journey. Discover additional controls as regression coverage grows; a full application selector crawl is not a setup prerequisite.

## Discover and verify

Use managed `agent_browser` exploration and its current command guide to inspect actual page states. Identify the intended element, its frame or component scope, the actor role, and the state in which it exists.

Prefer user-facing semantics such as role plus accessible name for controls, associated labels for fields, or an established test-ID contract. Scope repeated controls to the intended form, dialog, row, or frame. Stable semantic CSS hooks can be a fallback when verified. Avoid generated IDs, hashed classes, brittle DOM-position chains, and unexplained first/nth matches. If an ordinal is the actual business requirement, document it explicitly.

Before accepting a candidate, verify that it resolves uniquely in the intended context, is actionable when needed, and produces the expected behavior. Recheck after relevant navigation/refresh or rerender and from a fresh context with reconstructed preconditions. Persist observed verification and any untested variants; stability is evidence-backed, never guaranteed forever.

Browser snapshot refs such as `@e1`, coordinates, and live element handles are not reusable locators. Save a locating recipe or a Playwright locator factory that resolves against the current page. If there is no reliable identifying contract, report the ambiguity and propose a scoped test-ID or accessibility improvement instead of guessing. Adding application attributes is a reviewed product-code change, not an automatic setup action.

## Save one canonical executable definition

Keep locators in the customer's existing page objects or helper modules where possible. For a new small suite, a simple helper module is sufficient; do not build a separate selector framework.

Example only, to be derived and verified against the real application:

```ts
export const loginControls = (page) => ({
  email: page.getByLabel('Email', { exact: true }),
  password: page.getByLabel('Password', { exact: true }),
  submit: page.getByRole('button', { name: 'Sign in', exact: true }),
});
```

The smoke and regression tests import these same helpers. Do not copy executable selector strings into several JSON files and tests. The application profile points to canonical code and the browser QA knowledgebase note. The note documents verified locator expressions, their purpose/scope, and source references; it is not a second executable selector registry.

For each important locator, save a stable key, expression/strategy, semantic purpose, source symbol/path, page/frame scope, expected state/role, last verified build or observation time, and supporting evidence in the application's knowledgebase note. Keep it synchronized with verified helper changes; a new selector database is not required. Workflow learnings can link to that canonical note. Never include secret values or private page dumps as locator documentation. See [KB persistence and access wiring](setup-and-handoff.md#knowledgebase-persistence).

## Configure Playwright and prove the smoke test

1. Inspect existing test configuration, language, package pins, fixtures, auth setup, and browser installations. Reuse them; otherwise create a minimal suite in an authorized canonical test/code directory resolved through the current builder's code-layout contract.
2. Configure the base URL, scoped secret references, authentication fixture, deterministic test-data preparation/cleanup, selected browser/viewport, and bounded action/test/run timeouts. Use condition-based waits and Playwright assertions rather than fixed sleeps. Persist dependency versions and the actual working directory/run invocation.
3. Implement the agreed smoke scenario with shared verified locator helpers and explicit expected-outcome assertions. Locating an element successfully is not proof that the business behavior is correct.
4. Follow `builder-reference/references/playwright-scripted.md` for AgentWorks' suite integration, environment injection, lifecycle, and evidence. Check the actual dependency/fixture endpoints before installing; private AgentWorks packages are not public npm/PyPI packages. Live viewing is optional and does not replace test-result evidence.
5. Execute the saved suite from a fresh context, not just the discovery session. Capture the assertion result and failures. Reconstruct auth and fixtures on rerun so saved selectors do not accidentally depend on the interactive browser's state.
6. Keep canonical test source in the authorized repository/workflow code location. Retain evidence and source revision/hash references durably. Record missing browser/dependencies, ambiguous selectors, or failed assertions as unresolved setup, not `ready`.

## Browser recording and evidence

Configure the saved runner to collect attempt-scoped video, console logs, and network logs according to the customer retention policy. Screenshots and traces are additional evidence types, not substitutes for required sources. Playwright owns capture for the repeatable smoke context; managed `agent_browser` capture owns only an explicitly started diagnostic session.

Finalize captures during cleanup, redact sensitive console/network content, copy permitted files into `db/assets/browser-qa/...`, and persist one metadata row per expected artifact. A path is not enough: validate file format, attempt time range, redaction, and—when video is retained—that it opens and shows the intended run. Follow the shared [evidence capture contract](../../references/evidence-capture.md).

## Handoff and maintenance

The profile references the runner, config, smoke specs, canonical locator helpers, evidence policy, knowledgebase note, and verification record. Critical Journey Validation reads that note, resolves the sources, reuses existing helpers, adds verified locators only for new journey coverage, and records the exact source revision used. Update the note after verified additions or repairs.

If a locator stops matching, preserve the failure and propose a change to the canonical helper. Reverify its affected journeys before accepting the repair. Do not silently change assertion meaning, click a different matching control, or redefine expected text just to obtain a pass.

References: [Playwright locators](https://playwright.dev/docs/locators) and [Playwright best practices](https://playwright.dev/docs/best-practices). AgentWorks-specific execution details remain in its versioned builder references.
