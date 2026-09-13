# Browser security testing

## Coverage

Use the verified Browser QA foundation when available for routes, actors, stable locators, fixtures, and evidence capture. Security independently evaluates bypass, exposure, and exploitability. Applicable allowlisted checks may include:

- cookie attributes, session invalidation, rotation, fixation resistance, and browser-storage exposure;
- sensitive values in URLs, redirects, console output, network records, cache, or client bundles;
- content, frame, referrer, transport, browser-permission, and cross-origin policies;
- mixed content, unexpected third-party resources, unsafe navigation, opener behavior, and clickjacking;
- bounded output-encoding, injection, request-forgery, redirect, upload/download, and client-side trust checks;
- direct navigation, object/action authorization, cross-role, ownership, and tenant isolation.

Each check specifies origin/path, actor, initial state, expected control/source, passive or active mode, safe payload/fixture reference, allowed side effects, rate/attempt limit, cleanup, evidence, and stop conditions. Mark non-applicable checks rather than using generic payload catalogs.

## AgentWorks steps

Use a scripted preparation step to enforce scope and initialize expected checks. Run passive inspection in a clean managed-browser context and persist sanitized headers, cookie metadata, storage key/type summaries, origins, redirects, console/error summaries, and network metadata.

Place active checks behind the rules-of-engagement decision. Use scripted Playwright scenarios for repeatability, isolated actors/fixtures, bounded attempts, cleanup, and exact build identity. Use a message sequence after collection to validate meaning, correlate duplicates, and identify missing evidence. Finalization remains deterministic.

## Evidence

Store allowed artifacts under a restricted security asset path scoped by run/check/finding. Retain redacted video, console/network logs, selected response/header/storage summaries, screenshots, traces, and safe reproduction output. Never retain credentials, cookies, tokens, MFA material, unrestricted bodies/DOM dumps, or working exploit material beyond the approved evidence need. Redaction failure makes the artifact unavailable.

## Remediation retest

After an approved fix is deployed, run the original check against the resulting build with the same actor/state and safe case, then test likely bypass variants permitted by scope. Rerun affected authentication, permission, and critical-journey routes. Preserve the original evidence and link the retest; test self-healing cannot edit product controls or expectations to clear the finding.

## Acceptance cases

Exercise passive clean result, confirmed finding, scanner-only false positive, third-party/out-of-scope navigation, actor/build mismatch, prohibited active check, approval defer/reject/approve, rate stop, cleanup failure, missing/redaction-failed evidence, duplicate finding, retest pass/fail, and release-gate consumption.
