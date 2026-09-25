# Website Growth Loop team and handoffs

## Propose the actual team

Inspect the Automation, accessible Crews, selected skills, checklist state, permissions, and a sample output. Show the owner which Crews can be reused and which must be created. The first manual route needs two distinct Crews: Website Growth Starter for the priority brief and Search Opportunity Mapper for buyer-question research. SEO Analyst produces technical issue evidence; it is a separate optional route and cannot stand in for a buyer-question map. Do not substitute based on a similar name.

After the owner reviews the roster, call Builder `create_crew` for approved missing Crews with stable idempotency keys and `template_id` values `website-growth-starter` and `search-opportunity-mapper`. Do not pass a template's local skill ID as a global `skills` argument. For existing Crews, inspect their capabilities and add a specialist pack through Crew Identity only when the owner chose that extension. Record actual Crew IDs, trigger bindings, and the minimum source access each needs. Do not attach an external account merely because a Playbook recommends it.

| Slot | Agent and output | Minimum first-route input |
| --- | --- | --- |
| Strategist, required | Website Growth Starter → `growth-priority-brief/v1` | Public pages or owner export, offer, audience, market, and useful visitor action. |
| Search, required | Search Opportunity Mapper → `search-opportunity-list/v1` | Validated strategist brief, inspected site pages, and owner/customer questions where available. |
| Technical SEO, optional | SEO Analyst → `seo-issue-list/v1` | Approved crawl scope and key pages; Search Console only for indexation claims. |
| Content, optional | Content Brief Writer → `content-brief/v1` | Owner-approved opportunity and factual product sources. |
| Page, optional | Content Page Builder → `reviewable-page-draft/v1` | Approved content brief, brand guidance, and chosen draft/review destination. |
| Measurement, optional | Traffic & Engagement Analyst → `traffic-readout/v1` | Authorized analytics, event definitions, window, and an actual shipped-change record if measuring a change. |

## Exact first-route artifacts

The strategist returns a JSON `growth-priority-brief/v1` with authorized `site_url`, offer, audience, visitor action, dated page findings, three to five prioritized actions where evidence permits, and open questions. Every priority links to a finding ID. A thin-evidence brief may contain fewer actions and must explain the limitation. See the [worked brief](../examples/growth-priority-brief.json).

The search Crew receives only that brief and approved page/source references. It returns a JSON `search-opportunity-list/v1` with the same site, inspected URLs, dated buyer questions, the question source, intent, answer status (`answered`, `weak_answer`, or `gap`), evidence references, confidence, next action, and reason for ranking. A question may link to a strategist finding. See the [worked search map](../examples/search-opportunity-list.json). An unanswered question with no supporting buyer evidence remains a labeled hypothesis; it does not become a measured search opportunity.

Use the bundled [artifact validator](../scripts/validate_growth_artifact.py) for both artifacts. For a local smoke test:

```bash
python3 scripts/validate_growth_artifact.py brief examples/growth-priority-brief.json
python3 scripts/validate_growth_artifact.py search examples/search-opportunity-list.json --brief examples/growth-priority-brief.json
```

When Builder constructs the Workflow, tell each producing Crew to return **only its JSON object** as the final response. The Crew-step runner saves that response as a run file (by default `response.md`); the validator reads its bytes as JSON regardless of extension. Add a blocking deterministic validator step after each producer and before its consumer. In the installed Workflow, the script is at `skills/agentworks-playbook-website-growth-loop/scripts/validate_growth_artifact.py`; pass the actual producer run-file path, not an example path. Save validation output alongside the artifact and Crew run ID. The general Crew-step runner stores a final response but does not enforce this contract itself. If Builder cannot wire or test the validator, leave the route manual and mark `handoff_contract` blocked. The validator checks structure and internal references. The Builder or a reviewer must also open actual source pages/exports and verify that cited observations are supported. A syntactically valid URL is not evidence of a true claim.

Reject an artifact missing the required source/observation, an action citing an unknown finding, a map using a different site, or a search question citing an unknown strategist finding. Keep the failed producer output for review, show the blocker, and do not invoke the next Crew. Test this with the bundled invalid fixture before marking setup complete. Store both run IDs, artifact paths, validator result paths, and any retry identity in the Workflow run.

## Setup evidence

Complete `SETUP.json` checks only after saving evidence. `goal_owner` records authorized site and decision owner; `metric_policy` records the metric and baseline or baseline-first date; `team_bindings` records the actual two Crew IDs and skill proof; `site_scope` records inspected URLs and dates; `capabilities` records tested source access; `handoff_contract` records both validation results and a blocked invalid case; `plan_review` records the owner's reviewed route and permissions; `test_run` links the real two-Crew manual run; `action_ledger` saves a stable reviewed action and state; `activation_choice` records manual-only or a paused schedule/event with policy. Optional connections can be declined when the first route remains useful.

## First manual route

1. Select a bounded set of pages and one buyer audience. Confirm the site and visitor action.
2. Run the strategist, save its actual artifact, validate it, and inspect its source references.
3. Pass the bounded valid artifact to Search Opportunity Mapper. Validate its map against the strategist brief and inspected pages.
4. Present three to five actions where supported, with owner, evidence, effort, success signal, and open questions. Ask the owner to choose, defer, or reject actions.
5. Save action IDs and review decisions. Keep the first run manual. A new site can start a measurement baseline without claiming a trend.

After this route passes, propose the optional paths in [action and measurement](action-and-measurement.md). Do not call a content brief or page draft a published change.
