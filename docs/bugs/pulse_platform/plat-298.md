[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-298 — Separate workflow code from learnings and unify scripted execution, testing and repair

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | Implemented locally — focused verification passed; production acceptance pending |
| Last synchronized | 2026-09-07 |
| Priority | P1 reliability |

## Problem and observed evidence

Cursor encountered repeated infrastructure and authoring failures while building
simple browser tests for `Workflow/automationtesting`:

- A builder shell lacked `VAR_BASE_URL`, while the real scripted runner received
  it. Replacing the workspace environment map could discard resolved variables.
- A script imported `rts_pw_lib` from `learnings/_global/scripts` by counting
  `__file__.parents`. Moving the entry point from learnings to an execution copy
  changed the directory depth and caused `ModuleNotFoundError`.
- Guidance promised pip installation, but server Python had no pip module.
- Builder and repair guidance disagreed on whether to edit the saved source or
  the execution copy. Generic shell dry runs did not share the step environment.
- Bash `pipefail` was used under sh; a browser URL contained literal `${VAR_BASE_URL}`.
- Validation-only failures omitted captured script output from repair context.
  After the import repair, the public navigation assertion failed on `/intro`
  while the login-form assertion passed. This requires investigation of the
  expected page behavior, not automatic weakening of the test.

## Agreed product decisions

1. New workflows store executable source in `code/`; learnings contain reusable
   documentation and observations. Existing workflows retain their current
   source locations and execution behavior. No automatic migration.
2. Persist an explicit layout version at workflow creation. Missing version means
   legacy. Do not choose a layout by detecting whether a folder happens to exist.
3. Steps have read/write access to the entire **own workflow's** `code/` tree,
   including shared helpers. Access is not restricted to a step subfolder.
   Preserve user authorization, workflow isolation and existing code-lock rules.
4. For the new layout, execute directly from the canonical source. No execution
   snapshot/copy requirement in this ticket. Repair edits canonical code and
   retries; do not copy an old execution bundle back over repaired source.
5. Builder tests, scheduled execution, manual execution and repair retries use
   one runner and the same resolved execution contract. Builder editing rights
   can be broader, but its test execution must use the step's permissions.
6. Builder and repair agents must produce and maintain documented, commented code.

Example layout:

```text
code/
  shared/
    __init__.py
    browser.py
    utils.py
  public_tests/
    main.py
  login_tests/
    main.py
learnings/
  public_tests/
    SKILL.md
runs/
  .../execution/public_tests/   # outputs and diagnostics, not source
```

Use actual persisted step IDs for source folders; shared packages use valid
Python identifiers. Define one explicit absolute code root and make it the
import root so `from shared.utils import ...` works through every entry point.
Do not infer the root from DB_PATH or parent-directory counts. Keep source
location distinct from per-run output location.

## Implementation scope

### Layout and compatibility

- Add a versioned layout field in persisted workflow configuration; stamp it in
  all new-workflow creation paths. Specify clone/import behavior explicitly:
  preserve the source workflow's version, with absent versions remaining legacy.
- Centralize source-path resolution. Update script discovery, initial authoring,
  execution, repair, review tools, metadata/run statistics, evaluation runs,
  file views, prompt projection and any source-save paths through that resolver.
- Keep legacy copying and helper behavior compatible. New-layout execution must
  not invoke legacy copy-back logic. Choose and document metadata locations so
  code locking and run-history tracking survive the layout change.

### One execution contract

- Build one resolved execution specification containing interpreter, entry point,
  working directory, import root, group-specific VAR_*, secrets, argv inputs,
  MCP session binding, DB capability, output paths, permissions and validation.
- Use that specification for builder `execute_step`, schedules, manual runs and
  repair self-tests. Native coding-agent bridge calls must retain the trusted
  step session and its environment rather than inheriting stale parent routing.
- Repair missing-variable propagation at its origin. Do not require the agent
  to hardcode values, manually export configuration or read variables.json as
  a workaround. Preserve variables through environment replacement and refresh.
- Do not give builder shells raw DB access simply to mimic a scripted runtime.
  The runner supplies the appropriate DB contract for the selected step.
- Add workflow-scoped code read/write grants to builder and step file/shell
  paths, including code-edit tools. Preserve traversal/symlink boundaries and
  existing authorization checks. Code access must not grant unrelated stores.

### Dependencies and preflight

- Provision Python pip and venv in Linux installation guidance; verify them as
  the service user. Account for externally managed Python without disabling
  host-wide package protections.
- Select a supported interpreter/dependency mechanism once and use it in every
  runner path. A package installed in a venv must be visible to the interpreter
  that actually runs the saved script. Keep installation state persistent.
- Before side effects, check entry-point existence, syntax, required configuration
  and supported imports in the actual runtime. Do not execute user modules just
  to validate them; avoid rejecting optional/conditional dependencies.
- Support nested shared packages. Scope preflight so an unrelated unfinished
  step does not prevent another step from running.
- Document POSIX vs explicit Bash commands and literal tool JSON arguments.
  Use the managed browser integration consistently rather than inventing another
  browser stack for simple tests.

### Repair and diagnostics

- Preserve execution errors, captured stdout/stderr and validation failures in
  repair context, including exit-0 validation failures; avoid duplicate output.
- Persist full available diagnostics and expose a file reference if prompt/tool
  output is truncated. Do not claim captured output equals all backend logs.
- Distinguish harness rejection, script exceptions, deliberate refusals and
  assertion failures. Preserve terminal-refusal and locked-code behavior.
- Require evidence before changing an assertion. A test finding a product bug
  must not be rewritten merely to make the workflow pass.
- Repairs can update shared code as authorized. Record changed paths and what
  was verified; changes to shared helpers can affect subsequent steps.

### Documentation contract

- Entry-point module docstrings explain purpose, required configuration names,
  input arguments, outputs/side effects and supported execution method.
- Shared helpers and non-obvious functions describe inputs, returns and failures.
- Comments explain reasons for selectors, assertions, retries, ordering and
  workarounds; avoid narrating obvious code or inventing causes.
- Builder and repair update affected documentation with the implementation,
  remove stale comments, and keep repair history in reports rather than source.
- Render layout-specific instructions: never tell a new-layout workflow to edit
  learnings source, or a legacy workflow to use a code path it does not have.

## Acceptance criteria

- [ ] A new workflow stores and runs `code/<step-id>/main.py` directly and imports
  a nested shared helper without path guessing or manually exported variables.
- [ ] The same step tested from builder and run by schedule uses matching cwd,
  interpreter, import paths, env names/values, input arguments and permissions
  for the same selected group. Tests compare secret values without logging them.
- [ ] A repair can edit the entry point and a shared helper, retry through the
  shared runner and leave the repaired source for subsequent runs.
- [ ] The step can read/write its workflow code tree but not another workflow's
  code; read-only user restrictions and locked-code semantics remain enforced.
- [ ] Missing variables survive environment-map replacement, initialization and
  selected-group refresh tests; no stale prior-group values reach execution.
- [ ] Legacy workflows without a version continue running and repairing without
  moving their files. Merely creating code/ does not switch their layout.
- [ ] Import/clone and evaluation paths have explicit compatibility tests.
- [ ] Missing module/configuration and syntax failures are actionable; preflight
  does not execute module side effects or require optional imports unnecessarily.
- [ ] Exit-0 validation failure includes the script's diagnostic output in repair
  context; harness/refusal failures retain their existing semantics.
- [ ] Linux checklist validates pip/venv as the service identity, and a dependency
  smoke test runs through the actual selected interpreter.
- [ ] Both builder and repair receive the same documentation requirements and
  correct layout-specific paths. Demonstrate an initial authoring and repair run.

## Implementation and verification (2026-09-07)

- New manifests stamp `code_layout_version: 1`; manifest updates preserve the
  prior version and unknown versions are rejected. Imported/cloned manifests
  preserve their version. Legacy workflows remain legacy.
- A common resolver selects source, working directory and persistent Python
  import/dependency roots. Builder `execute_step`, normal execution and canonical
  repair retries use `execScriptedScript`; evaluation loads the same layout.
- New-layout source is edited and executed directly, with no copy-back. Repair
  cannot pass solely because the agent wrote valid-looking output files. After
  exhausting repair attempts, canonical execution reports failure instead of
  silently replacing the script with agentic work.
- Whole-workflow code read/write grants apply to unlocked steps. Locked steps
  receive code read access. Existing user/Folder Guard boundaries remain in
  force. Code root does not grant access to another workflow.
- Metadata resides beside canonical source. Run history is initialized for new
  scripts; entry-point hashes track revisions. Shared helper changes are live,
  not versioned snapshots. Existing per-run entry-point repair diffs remain.
- Builder/repair guidance documents layout selection, shared helpers, comments,
  dependencies and controller-based tests. Review discovery and saved-code UI
  use the versioned source. Legacy guidance is explicitly labeled.
- Local tests pass for legacy/version-1 and evaluation source resolution,
  imported/cloned manifest version preservation, code grants/locks, runtime
  environment, preflight, repair diagnostics and canonical prompt behavior.
  A real Python subprocess test uses the runner command and generated import
  environment, imports a nested helper, repairs it in place, reruns successfully,
  and writes output without an execution source copy. Frontend type-check passes.
- Production deployment and a live builder/schedule/LLM-repair acceptance run
  are still pending. These local tests do not claim to prove every live bridge
  provider or selected-group scenario. The full workflow package also has a
  separate existing learning-prompt size-budget failure (7174 vs 7100 chars).

### Persistent storage

RTS workspace docs are `/data/video-studio/docs` on encrypted 100 GiB gp3 EBS
`vol-0a3e0156144c12a51`, mounted via `/dev/nvme0n1p1` on the root filesystem.
`DeleteOnTermination=false` was verified. Source, helpers, dependencies, durable
assets and retained run documents stay beneath the workspace docs root, outside
release directories. Restart/deploy does not erase them. Instance replacement
requires reattaching/mounting the retained volume; backups were not verified.
Run retention still applies to old run folders; permanent report media belongs
under `db/assets/`. The Linux checklist now requires these storage checks.

### Prerequisite history

- Follow-up skill consistency audit: updated independently loaded plan-design,
  file-layout, tools, step-config, stores, runtime-context, message-sequence,
  learning/review and optimization references to resolve saved source from the
  manifest version. Corrected legacy-only copy-back instructions and dependency
  guidance; the orchestrator prompt now uses the same rule. Added an embedded
  reference audit test. Changes remain local, not deployed.

- Main already contains variable environment backfill commit `6500cdbe4`;
  regression tests passed locally. Verify deployed revision and remaining bridge
  paths before declaring the production environment issue closed.
- On 2026-09-07, pip and venv were installed on the RTS server and verified as
  `video-studio` (Python 3.12 / pip 24.0).
- Local changes include the Linux checklist, limited adjacent-module preflight
  and repair-context diagnostic preservation. No push or deployment was performed
  as part of this implementation turn.

## Current tradeoff

Direct source execution is the agreed initial design. Concurrent edits to shared
helpers may affect overlapping executions; this ticket does not introduce
snapshots, new scheduling restrictions or a concurrency redesign. Do not silently
add snapshot execution while implementing these requirements.
