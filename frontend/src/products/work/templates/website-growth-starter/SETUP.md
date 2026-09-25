# Website Growth Starter setup

Template: `website-growth-starter` version 1. Progress belongs to this Crew in `templates/website-growth-starter/TEMPLATE_SETUP.json`. The Crew verifies checks in chat; installing the pack never marks setup complete.

## Start without a connection

Provide the public site URL or uploaded site pages, a short description of the offer, target audience, market, and the primary action a good visitor should take. Ask Crew: “Audit this newly launched site and give me the first three to five actions to grow relevant traffic over the next 30 days. Cite the pages and label assumptions.”

The first Website Growth Brief can use public pages and owner context. Google Search Console and analytics make the baseline and follow-up measurable, but a new site may have little data. Do not require either connection to begin or claim measured traffic that is unavailable.

## Optional capabilities

These are suggestions, not installed or activated capabilities.

| Capability | Suggested definition | Before enabling |
| --- | --- | --- |
| Search data | Read an authorized Google Search Console property or export for page/query impressions, clicks, CTR, and indexing evidence. | Owner selects the property and grants this Crew access; test the property and date range. |
| Visitor data | Read an authorized analytics property or export for sources, landing pages, engagement, and conversions. | Owner confirms event definitions and privacy boundaries; test access. |
| Page editing | Prepare a reviewable repository change or CMS draft for approved pages. | Owner grants access, chooses a review/publish path, and verifies a sample edit. |
| Crew schedule | Review last week's site changes, Search Console/analytics signals if available, and the next three actions. | Agree on cadence, timezone, source, and delivery; test a run. |
| Authenticated trigger | Recheck a newly published page after a `page_published` event. Payload: `url` and `published_at`. | Choose an authenticated event source, validate allowed URLs, and test against a real page. |
| Crew function | `site_audit(url)` returns observed issues, page references, and open questions. | Restrict scope to authorized domains and test the typed contract before exposing it. |
| Goal-chasing Automation | Website Growth Loop tracks agreed actions shipped and changes in qualified traffic or conversions. | Create separately after choosing the metric, baseline, data source, cadence, and approval policy. |

Do not copy another Crew's token or connection. No schedule, trigger, function, or Automation is created by this pack.

Useful external references: [Google SEO Starter Guide](https://developers.google.com/search/docs/fundamentals/seo-starter-guide) and [Search Console setup](https://developers.google.com/search/docs/monitor-debug/search-console-start).
