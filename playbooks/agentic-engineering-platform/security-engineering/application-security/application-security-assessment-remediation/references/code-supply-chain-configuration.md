# Code, supply-chain, secret, and configuration testing

## Source and build contract

Resolve the canonical repository, commit/ref, modules, languages/build systems, lockfiles/manifests, generated/vendored exclusions, artifact/image digest, deployment environment/revision, IaC/state ownership, and relevant configuration sources. Record tool, ruleset/database, configuration, suppression/baseline, and retrieval timestamps. Do not compare or close findings across mismatched revisions.

## Source analysis

Run customer-selected SAST and semantic checks only for applicable languages and components. Preserve tool-native rule/location/dataflow plus a stable source fingerprint. Validate whether the path is reachable or security-relevant in the built/deployed configuration, whether framework controls alter the result, and whether evidence contradicts exploitability. Do not copy sensitive source into broadly visible reports.

## Dependencies and artifacts

Resolve dependencies from lockfiles and built artifacts/images where possible. Record package/component, version, ecosystem, dependency path, direct/transitive status, runtime/build/dev scope, affected-version source, advisory/database revision, reachability evidence, available fix, compatibility constraints, and deployment identity. A package name match without version/build evidence remains an observation.

Evaluate base images, actions/plugins, build tools, provenance/signatures, and untrusted registries when in scope. Separate vulnerable dependency, malicious/compromised component, license/policy concern, and unsupported/end-of-life status.

## Secrets

Use scanners that redact matched values. Persist rule, file/object identity, location, secret type, validity-check status, exposure history, and rotation/revocation state without the secret. Verification must not send credentials to an unapproved service. A live credential triggers the customer's containment process; removing it from the latest commit alone is insufficient if history/artifacts remain exposed.

## IaC and deployed configuration

Compare IaC, policy, container, cloud, gateway, and application configuration with customer controls. Link the exact resource and canonical module/state. When authorized, compare declared configuration to deployed state and classify drift separately. Validate contextual exceptions such as compensating controls, private reachability, inheritance, or managed defaults.

## Remediation checks

Prepare the smallest source, dependency, image, IaC, or policy change. Run relevant formatter/build/unit/security/policy tests, dependency resolution, image rebuild, IaC plan, and affected integration/regression checks. Record breaking-change and transitive impacts. Closure requires the fixed commit/artifact/configuration to deploy and the exact finding to retest; a merged PR is an intermediate state.

## Acceptance cases

Exercise tool false positive, unreachable path, confirmed flow, generated-source exclusion, stale advisory database, ambiguous package identity, vulnerable dev-only dependency, transitive upgrade conflict, leaked-secret redaction and rotation, IaC drift, accepted compensating control, fix build failure, deployed revision mismatch, and verified closure.
