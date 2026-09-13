# Release and PR quality gate

## Bind the exact release identity

Define one gate key from repository/provider, change or PR identifier, commit SHA, deployment/build identifier, target environment, and policy revision. Verify the tested application exposes or can be mapped to that exact identity. If identity is unknown or mismatched, the gate is `needs_review` or `fail` according to approved policy; it never passes using repository HEAD or a previous run.

The policy predeclares required suite or security-assessment IDs, route selections, variable groups, expected cases/checks, allowed result age, evidence requirements, blocking classifications or severities, timeout/cancellation handling, rerun rules, and human-review conditions. Include Browser Security only when its scope and policy apply to the release.

## Adapt the plan

Use a deterministic preparation step to validate trigger payload and identity, initialize the gate and expected suite/group set, and reject duplicate/incompatible dispatch. Execute existing saved routes; do not copy their browser tests into the gate. Persist each child run reference and terminal state.

Use deterministic finalization to compare expected versus observed suite/group keys, verify build/source identity and evidence, and derive `pass`, `fail`, or `needs_review` from the frozen policy. A human branch may resolve only policy-defined review cases. Reruns create new attempts linked to the same gate and do not delete earlier results.

Configure authenticated webhook/API triggers through the supported AgentWorks Setup surface. A schedule is appropriate for time-based deployment checks; PR/change events use the supported API-trigger contract. GitHub or another SCM/CI integration is optional and must be explicitly selected and authorized.

## Persist, report, and deliver

Store gate/policy identity, trigger receipt, expected suite/groups, child execution references, results, evidence completeness, derived verdict, human decisions, and external delivery receipts. Record queued, attempted, delivered, rejected, and failed delivery separately.

The report shows exact change/build identity, policy revision, current gate attempt, required suite completeness, failures and classifications, video/console/network evidence, reviews, reruns, timeline, and actual publication state. External status text links to this durable result without exposing secrets or unsafe logs.

## Acceptance cases

Verify all-required pass, one blocking failure, optional failure, missing suite, cancelled/timed-out suite, stale result, build mismatch, duplicate trigger, rerun, evidence-required-but-missing, human defer/reject/approve, unauthorized status destination, delivery failure, and successful receipt. Confirm no false pass from empty results or a prior commit.
