# Role and permission workflow

## Define the permission contract

Create an approved matrix whose stable key combines actor role, tenant/ownership state, resource, action, access path, and expected decision. Record the policy source. Test only meaningful cells; avoid multiplying roles and actions that have identical, explicitly confirmed policy.

Each cell defines:

- actor role and owned test identity reference;
- own, other-user, other-tenant, or global resource state;
- list/view/create/edit/delete/export/admin action;
- navigation path such as visible UI, copied direct URL, or deep link;
- allow, deny, hidden, disabled, redirect, or not-applicable expectation;
- safe fixture and cleanup behavior;
- required evidence and related critical journey.

UI visibility is one observation. Where the application exposes a browser action that reaches a protected backend operation, verify the resulting denial or absence of unauthorized data without bypassing the product through unrelated raw requests.

## Adapt the plan

Use a scripted runner to initialize the expected matrix, create isolated fixtures, execute cells under the correct selected secrets, finalize evidence, clean fixtures, and persist every terminal outcome. Batch cells that share credentials and fixture lifecycle; split security contexts that must remain isolated. A deterministic completeness step compares planned and observed keys.

Use one message sequence to inspect mismatches against the approved policy and classify application defect, test/config defect, environment blocker, ambiguous policy, or unknown. It may propose follow-up work but does not change roles or permissions.

## Persist and report

Store policy revisions, matrix cells, runs, attempts, findings, and artifact rows. Preserve role labels but exclude identity secrets. Freeze the tested build, application profile, matrix revision, and expectation source with every run.

The report shows coverage by role/resource/action, allowed and denied outcomes, direct-navigation results, cross-tenant checks, missing cells, evidence, and cleanup. Never turn not-applicable or not-run cells into passes.

## Acceptance cases

Verify an allowed action, a denied action, a hidden/disabled control, direct protected navigation, cross-tenant isolation, missing role credentials, ambiguous expected policy, unauthorized destructive action excluded from scope, omitted matrix cell, cleanup failure, and concurrent actors without session or evidence crossover.
