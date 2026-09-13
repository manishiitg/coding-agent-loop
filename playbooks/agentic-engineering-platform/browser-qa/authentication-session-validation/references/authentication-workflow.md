# Authentication and session workflow

## Scope the scenario set

Start from approved application behavior. Give every scenario a stable ID, actor role, authentication method, initial session state, action, observable expected state, expectation source, side-effect risk, cleanup, and evidence requirement.

Cover applicable behavior rather than forcing a universal checklist:

- valid and invalid sign-in;
- logout and protected-page access afterward;
- refresh, navigation, and multi-tab session continuity;
- expiry, revocation, or invalid-session recovery;
- MFA challenge, enrollment, or recovery only with safe owned channels;
- password/account recovery only when messages and state changes are authorized;
- remember-device or concurrent-session behavior when the product defines it.

Use fresh contexts for unauthenticated cases. Use separate owned identities where concurrent or role behavior could contaminate results. Do not use a previous interactive session as proof the saved suite can authenticate.

## Adapt the plan

A small workflow usually needs a scripted runner, an attention branch only when investigation differs, and one message sequence to classify failures. Keep account preparation and cleanup inside the runner when they share its security and retry boundary. Split them only when a different credential, permission, or independently recoverable state requires it.

The runner initializes expected cases, resolves selected secret references at runtime, executes canonical tests, finalizes video/console/network evidence, performs cleanup, and persists terminal results. It may emit `clean` or `attention` for a branch. The investigator reads original evidence and approved expectations; it does not change authentication policy or application code.

## Persist and report

Declare auth scenarios, runs, cases, attempts, and artifacts in `db/README.md`. Results include actor role without identity secrets, initial/final state, expected/observed behavior, build/source revision, cleanup, classification, and evidence references. Store non-secret verified behavior in the application KB; keep tokens, cookies, storage state, MFA values, and passwords out of DB, KB, artifacts, and reports.

The report shows scenario and actor coverage, session-state transitions, failures, evidence, cleanup, and unresolved unsafe-to-test behavior. It must distinguish an unavailable channel/account from an application failure.

## Acceptance cases

Verify at least: successful login; invalid credentials; logout followed by direct protected navigation; expired/invalid session when safely reproducible; unavailable MFA/recovery channel; account lockout limit reached before execution; missing required case; missing evidence; cleanup failure; and two actor roles without result/evidence mixing.
