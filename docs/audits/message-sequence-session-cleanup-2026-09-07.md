# Message-sequence runtime cleanup collision — 2026-09-07

## Observed incident

Workflow: `social-media`; step: `execute-required-saas-connect-post`;
item: `verify-saas-discovery-and-execution`.

The retained server log (`agent_go/logs/server_debug.log`, 2026-09-06 IST)
shows two overlapping executions using the same runtime identity:
`msgseq-iteration-0-default-default-step-11-execute-required-saas-connect-post`.

- 23:28:23: the first execution registers its working directory and guard.
- 23:30:18: its replacement registers the same runtime identity and directory.
- 23:31:38: the cancelled first execution finishes unwinding, closes its agent,
  and reaches message-sequence cleanup. Its execution ID ends in
  `1788717503412367000`.
- 23:31:44: the replacement's shell requests begin failing with missing cwd.
  Repeated requests continue failing through 23:32:28.
- 23:32:38: the replacement execution, ending in `1788717618256685000`, fails.

The cwd was not absent at creation: it was registered repeatedly before the
failure. The configuration error reports the symptom; shared session ownership
explains why it disappeared. The cleanup function clears shell configuration
and releases CLI resources by that identity. The failure is reproduced using
the production cleanup function in a local unit test.

## Fix

Allocate a unique UUID suffix for each new message-sequence runtime. Keep the
readable run/group/step prefix for diagnostics. Reuse the ID only from the
sequence's live in-memory runtime across its turns. Do not reclaim a serialized
runtime ID when reconstructing conversation history after execution/restart.

The old execution now releases only its own shell guard, cwd, environment,
managed DB capability, and CLI runtime. No workspace-root fallback or additional
file/DB permission was introduced. Existing conversation/history metadata and
cost recording paths are unchanged; `runtime_session_id` records the new owner.

## Verification

- Before the change, `TestMessageSequenceOldRuntimeCleanupPreservesReplacement`
  failed with the replacement shell config becoming nil.
- After the change, message-sequence, working-directory, and direct-learning
  tests passed.
- Regression coverage verifies late cleanup preserves all replacement shell
  configuration, including blocked paths and DB capability; the completed
  runtime is still cleaned up.
- Resume coverage verifies saved history cannot reclaim the previous runtime,
  while subsequent turns reuse the live replacement runtime.
- Focused race-detector tests for runtime ownership, resume, stable turn identity,
  and working-directory propagation passed. `git diff --check` passed.

## Limits

No AgentWorks server was started/restarted and no live workflow was retried.
The fix is local source pending deployment. This does not prove that no public
X action occurred earlier in either run. That requires authoritative action
receipts or external read-back. Nor does it serialize all shared workflow
artifact writes: this patch isolates runtime/session ownership, not execution
of the same business action in parallel. Repeated agent shell retries cannot
repair missing server-side session state.
