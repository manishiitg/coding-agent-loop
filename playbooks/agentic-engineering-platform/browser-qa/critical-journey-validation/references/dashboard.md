# Browser QA dashboard setup

Build the dashboard through AgentWorks' existing workflow Report surface. The builder authors `db/reports/index.html` once; execution steps update durable data. Read `builder-reference/references/reporting-policy.md` and inspect the existing schema/report before editing. Preserve Basic Browser Setup's views and data when extending its workflow.

## 1. Establish the data contract

Document actual tables, columns, keys, enum values, and writer ownership in `db/README.md`. Reuse equivalent existing tables or define a small set for runs, cases, attempts/findings, and artifacts. Suggested names such as `qa_runs` are examples, not pre-existing platform tables.

| Data | Keys and fields the view needs | Writer |
| --- | --- | --- |
| Setup/profile | Application + profile revision, environment, runner/locator/recording readiness, last verified observation | Setup workflow |
| Runs | Unique run ID, application/environment/group, selected mode, profile/journey/source revision, build identity or unknown, lifecycle, QA status, start/end | Prepare/execute/finalize steps |
| Cases/attempts | Run + journey + attempt, required flag, status, assertion expected/actual, original vs diagnostic, duration, classification | Runner and investigation |
| Findings | Run/case, reason, evidence, proposed next action and review state | Investigation |
| Artifacts | Run/case/attempt/phase, source, `video`/`console_log`/`network_log`/`trace`/`screenshot`, durable path or null, format, capture/validation/redaction status, missing reason | Runner/capture owner |

Use explicit run/group keys for queries and idempotent updates. Freeze historical profile/journey/source identities with the run. Do not compute historical results from today's mutable test set. Chronological run data stays in the DB; the KB holds reusable verified knowledge.

## 2. Build a useful first layout

Use existing navigation, or add Overview, Journeys, Findings, and Evidence sections if the report needs them. Avoid adding a separate page for every workflow step.

| Section | Content |
| --- | --- |
| Header | Application, environment, run selector, tested build (or unknown), start/time, lifecycle and QA status |
| Overview | Required cases passed / required expected, failed/blocked/skipped/not-run counts, duration, evidence gaps and next action |
| Journeys | Each journey's expected behavior, case status, failure category, original and diagnostic attempts, evidence links |
| Findings | Reproducible issue, supporting observations, uncertainty, owner/next action, proposed repair without implying it was applied |
| Evidence | Selected attempt video, searchable redacted Console and Network views, screenshots, trace/HAR downloads, capture status and provenance |
| Setup details | Profile revision, runner, locator verification summary, fixture/auth readiness and recording policy |

Compute counts from the declared expected case set, not just rows returned by successful tests. Explicitly show missing cases. If you show a required pass percentage, use required passed / required expected, and show N/A when no valid required set exists. A perfect percentage must not hide an incomplete lifecycle or optional failures.

## 3. Wire the live view

Keep HTML/CSS/JS self-contained. Gate all report data loading with `window.report.ready(...)`, which also handles later refreshes. Inspect the documented query return shape instead of guessing it. Use actual declared columns and supported query arguments; never interpolate untrusted filter values into SQL. Use an in-memory selection over a bounded result set or the documented safe query mechanism.

Preserve the selected historical run on refresh, handle stale async responses, and avoid mixing rows across runs. Render loading, empty, error, running, cancelled/interrupted, blocked, needs-review, incomplete, failed, and passed states explicitly. Use status text as well as color; escape user/application strings with text-safe DOM APIs.

Only `db/` files are report-visible. Put durable evidence under `db/assets/`; a scratch `runs/...` file or a Browser-panel replay URL is not a usable long-lived report link. Show canonical code/KB locations as descriptive source references or use an authorized DB summary snapshot, rather than generate unsupported clickable file links.

## 4. Add evidence inspection

On an explicit recording selection, resolve its durable workspace path using `window.report.mediaUrl(path)` and attach the fresh URL to a native `<video controls playsinline preload="metadata">`. Store paths in the database, never signed/expiring URLs or base64 footage. Obtain a fresh URL after a playback error when the user retries.

Keep the same video element/source while its run/attempt/path identity is unchanged so refresh does not reset playback. Ignore stale async media responses after the user selects a different case. Do not eagerly load every recording. Use `fileUrl`/`openFile` for appropriate downloads according to the bridge contract.

Label original failure versus reproduction video, complete versus partial footage, and unavailable recording with the actual reason. A video belongs to one recorded context/attempt; do not imply it covers the entire suite or has audio unless verified. See [recording lifecycle](regression-workflow.md#browser-recording-and-diagnostics).

Render console records as escaped text with level/time filtering. Render network records as escaped, bounded summaries with method/status/type filters and downloads only when the redacted artifact is permitted. Never execute log content or render captured HTML. Show redaction status and do not expose a failed-redaction artifact. Follow the shared [evidence capture contract](../../references/evidence-capture.md).

## 5. Add only supported user actions

Start with View evidence and a contextual Request investigation / Rerun journey action if the workflow supports it. A button may call `window.report.sendChatMessage(message, { requestId })` from a user click only. Include actual application/run/journey IDs and ask the workflow agent to re-read current state and authorization. Reruns create a new run or diagnostic attempt and never overwrite the original failure. Do not show a Rerun journey action if only whole-suite execution exists.

Show queued as queued; derive running/completed from execution evidence. No automatic run, notification, or remediation on page load/refresh. New approval controls require a separately designed action/decision contract; this dashboard must not silently turn a repair proposal into applied code.

## 6. Validate the result

Run `validate_report_html()` and repair schema/path errors. Then open the actual Report view and inspect it with real fixture records: no runs, pass, failed assertion, blocked setup, missing required case, mixed failure categories, cancellation, missing/partial video, missing console/network logs, redaction failure, and data-query failure. Static HTML validation is not browser verification.

Check that run selection shows the correct cases/build, evidence links open real durable files, videos play after suite teardown, console/network filters are safe, and refresh preserves the selected attempt and active playback. Verify that blocked and incomplete states never appear as green success. Check narrow layouts, readable status text, keyboard access, and visible action errors.
