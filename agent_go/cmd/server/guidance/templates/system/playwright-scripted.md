## Direct Playwright in scripted steps

Use this reference for deterministic browser suites saved as a scripted step's `main.py`.
The script may import and drive Python Playwright directly. `agent_browser` is the tool for
interactive agent browsing; it does not replace a self-contained repeatable test harness.

### Runtime and dependencies

- Install Python packages once into `$WORKFLOW_CODE_DEPS`; never run `pip install` on every test run.
- Resolve the browser from `AGENT_BROWSER_EXECUTABLE_PATH` when supplied. Otherwise use a Playwright-installed browser from the workflow's persistent dependency/cache directory. Never hardcode a host-specific binary or browser version.
- Preserve the platform's short private `TMPDIR`, `TMP`, and `TEMP`. Chromium profile/socket paths have a small Unix length limit; never put the profile under a deep step path or use a process-shared fixed profile.
- Treat `VAR_*`, `SECRET_*`, `STEP_OUTPUT_DIR`, `DB_PATH`, `WORKFLOW_CODE_ROOT`, and `WORKFLOW_CODE_DEPS` as injected runtime inputs. Required values use `os.environ[...]`; do not rediscover the workflow root from directory depth.

### Isolation and ownership

- The suite owns `sync_playwright()`, each browser process, context, page, and cleanup.
- Start a fresh browser process and fresh context for every case. Do not add a `shared_browser` flag or reuse one page/context across a suite. A wedged page must not block every later case.
- Browser isolation does not imply application-session destruction. If the tested service has a server-side session, reconnect to it deliberately in the next case. Persist Playwright `storage_state` only when authentication reuse is intentional; never share live page objects.
- Close context, browser, and Playwright in `finally`. Clean up an external paid/GPU session once, in a dedicated final case or an idempotent suite finalizer.

### Bounded execution

- Set explicit Playwright action and navigation timeouts. Also give every case a wall-clock watchdog; Playwright action timeouts alone do not cover every protocol/browser hang.
- A case timeout must stop that case's browser process group, finalize its evidence/status, and let the harness either continue safely or fail the suite. The outer workspace timeout is a last-resort harness kill, not the normal case timeout.
- Use bounded infrastructure retries only with a brand-new browser. Do not retry assertion failures, and record every attempt.

### Progress, results, and provenance

- Create the suite run record first. Before launching each case, persist a `running` case record and print a flushed progress line naming the case and operation.
- Print progress before/after browser launch, context/page creation, navigation, authentication, and every major test phase. Stdout/stderr are the repair agent's primary live evidence.
- In `finally`, move each case to a terminal status and close the suite record. A later reconciliation pass may mark only an expired-heartbeat record as `harness_timeout`; an LLM must not manually rewrite a fresh running row.
- At run start, SHA-256 hash `main.py` and every workflow-owned helper imported by the suite. Store the manifest with the run summary/evidence. “main.py was unchanged” is not proof that the executed code was unchanged.
- Store screenshots, video, console/network logs, current URL, last completed operation, and the exception/traceback on failure. Redact secrets. Durable evidence belongs under `db/assets/` with a DB/report reference; `$STEP_OUTPUT_DIR` is run-volatile.

### Failure classification

Classify the terminal cause precisely: `assertion_failure`, `setup_failure`,
`browser_crash`, `case_timeout`, `harness_timeout`, or `cancelled`. A process that emitted
progress and was later killed did run; never report that the workspace “refused to start” it.
Do not weaken assertions or rewrite product code because of an infrastructure failure. Before
claiming code was unchanged, compare the stored source manifest, including shared helpers.

### Verification

Run syntax/import and dependency/browser preflight checks first. Validate the harness against a
small local fixture that exercises pass, assertion failure, and timeout finalization. Use
`execute_step(fast_path_only=true)` for the real selected group only when a live run is authorized;
do not launch an expensive or externally mutating suite merely to prove the harness compiles.
