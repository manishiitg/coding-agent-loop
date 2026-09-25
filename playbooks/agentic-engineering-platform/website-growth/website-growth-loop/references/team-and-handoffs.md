# Website Growth Loop team and handoffs

## Team proposal before mutation

Inspect accessible Crew projects and current Workflow steps. For each slot, show the existing Crew that can satisfy it or a proposed new specialist, including its Agent Playbook, required inputs, skills, account access, and first output. The first manual route needs at least two distinct Crews. Do not create a duplicate because names resemble roles; inspect identity, selected skills, checklist status, and actual output capability. Use the owner's edits to the proposed roster.

Once the owner reviews the concrete roster, call Builder `create_crew` for each missing Crew with a stable idempotency key and the matching `template_id`: `website-growth-starter` for strategist; `search-opportunity-mapper` or `seo-analyst` for search; `content-brief-writer` and `traffic-engagement-analyst` only if those optional slots were accepted. The template applies its local skill, setup guide, and checklist to the new Crew. Do not pass the template's local skill ID as a global `skills` argument. For an existing Crew, inspect its selected skills and add the specialist template through Crew Identity only when the owner chose to extend that Crew.

| Slot | Agent | Minimum output | First-run access |
| --- | --- | --- | --- |
| Strategist, required | Website Growth Starter | `growth-priority-brief/v1` | Public site or page export, offer, audience, desired visitor action. |
| Search, required | Search Opportunity Mapper or SEO Analyst | `search-opportunity-list/v1` | Site pages and strategist brief; Search Console is optional. |
| Content, optional | Content Brief Writer | `content-brief/v1` | Approved opportunity, site context, factual source material. |
| Measurement, optional | Traffic & Engagement Analyst | `traffic-readout/v1` | Authorized analytics and event definitions. If data is unavailable, keep baseline first and defer this slot. |

## Handoff contracts

`growth-priority-brief/v1` contains `site_url`, `audience`, `offer`, `visitor_action`, `findings[]` with source URLs, `priorities[]` with evidence and owner, `unknowns[]`, and `created_at`. The search specialist receives that bounded artifact, not the strategist's entire workspace.

`search-opportunity-list/v1` contains `site_url`, `buyer_questions[]` with intent, existing page or gap, evidence URLs, confidence, and proposed next action. If content is enabled, an approved opportunity is passed to Content Brief Writer as a separate reviewed handoff.

`content-brief/v1` contains the buyer need, angle, page outline, source-linked claims, internal-link suggestions, questions to verify, and success signal. A brief is not a published page.

`traffic-readout/v1` contains source property/export, date window, metric definitions, numerator and denominator, comparable baseline, observed changes, and limitations. Do not compare incompatible windows or call a planned action a measured result.

The Workflow validates required fields and source references after each Crew step. Invalid or missing artifacts stop the route with a visible blocker; the next Crew is not invoked. Save producer/consumer Crew run IDs and artifact refs in the Workflow run. Retry with the same execution identity and inspect the existing result before repeating a Crew invocation.

## Setup checks

1. `goal_owner`: named owner, authorized site, offer, audience, market, and visitor action.
2. `metric_policy`: agreed useful traffic or conversion metric, source, and baseline or explicit baseline-first start date.
3. `team_bindings`: at least two distinct authorized Crew IDs with the expected skills and first-result capability.
4. `site_scope`: inspected pages or owner export, crawl boundary, and source dates.
5. `capabilities`: each selected skill and account is available to the Crew that needs it; no token is copied.
6. `handoff_contract`: strategist output validates and the search Crew accepts the bounded input.
7. `plan_review`: owner sees actual team, steps, budget, permissions, and approval boundaries.
8. `test_run`: one manual run has both Crew run IDs, valid artifacts, and a reviewable combined result.
9. `activation_choice`: manual-only or paused schedule/event with explicit owner cadence, timezone, concurrency, retries, budget, and notifications.

Completing a check requires saved evidence, not just a chat statement. An optional connection may be skipped only if the first result and chosen metric can still be honestly delivered.

## First route

1. Confirm shared context and choose manual run scope: a bounded page set and one buyer audience.
2. Run strategist Crew and validate its priority brief.
3. Pass the brief to search Crew and validate its opportunity list.
4. Produce a combined action review with three to five priorities and named open questions. No publication or outreach.
5. Save dashboard status and evidence links. Establish a measurement start date if the site is too new for a baseline.

After this route passes, propose optional content and measurement steps and a weekly cadence. Use actual data availability and customer decisions to determine whether those additions belong in the same Automation.
