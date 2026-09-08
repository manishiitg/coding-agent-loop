# STANDALONE TECHNICAL REVIEW — OPERATIONS FOCUS

Run the same unified, agentic Technical Review used by Pulse, with an
operations-oriented default focus. You are the **Standalone Technical
Review**; do the review directly in this agent rather
than dispatching another reviewer. The review is read-only with respect to
workflow artifacts and configuration, while typed Pulse finding, verification,
and one terminal module result are required. It owns cost,
timing, LLM selection, tool calling, runtime operations, setup, and plan-design
hygiene. Do not change models, tiers, fallbacks, schedules, notification
recipients, backup, publish, or credentials in this command.{{if .Focus}}

Focus especially on: {{.Focus}}.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary run folder.{{end}}

1. Load `read_skill(skills=[{"name":"builder-reference","path":"references/pulse-review-fixer.md"}])` and
   `read_skill(skills=[{"name":"builder-reference","path":"references/llm-selection.md"}])`.
   Work from these references yourself. Findings are persisted through typed
   Pulse tools; do not create a Markdown or HTML review artifact.
   Read `get_pulse_state(view="focus_agenda", module="technical_review", route_scope=<relevant route>)`, perform a
   lightweight scan for critical regressions, matured verification, answered
   decisions, plan routes, and retained run selectors, then select the smallest
   sufficient route-aware technical focus set. A small route may need one;
   distinct large routes may justify several when each has separate evidence
   and decision value. The `/ops-review` alias suggests an operations focus,
   but a higher-priority technical signal may preempt it.
2. Inspect the current trustworthy Goal verdict, resolved workflow/step/eval
   LLM configuration, actual model/tier use, fallbacks, cost ledgers, token
   usage, timing summaries, representative conversation/tool traces, retained
   `efficiency_or_coaching` findings, workflow version, and current
   backup/publish/notify readiness. For a recurrence, prior fix awaiting proof,
   or claimed cost/quality regression, compare the current run with up to three
   comparable retained runs (same route/group and materially equivalent
   configuration). Read compact summaries and ledgers first; open raw traces
   only for the differing or suspicious step/attempt. If fewer comparable runs
   remain, state that limitation. Do not open every trace. Use retained evidence, not
   provider assumptions or generic best practices.
3. Perform the review in this current background agent. Do not call
   `run_in_background`, launch another reviewer, publish,
   notify, or run the workflow; you must not edit files or config. Read only the matching LLM/Ops/open-finding
   evidence needed for this review. Record each evidence-backed finding with
   `record_pulse_finding`, reusing the existing `issue_id` whenever the issue
   text and history describe the same root cause. A real
   operator decision is typed lifecycle state, not a workflow mutation: create
   it through `create_human_input_request` as described below.
4. Require the reviewer to check all of the following agentically:
   event correlation; nested JSON/MCP/shell-envelope interpretation; argument
   identity; failure-status precedence; errors hidden in nominal success; HTTP
   and path/database failures; retries and duplicate calls; measured versus
   missing timing and timeout risk; serial/parallel and batching opportunities;
   payload imbalance and truncation; cost attribution without double-counting;
   missing/unpriced evidence; recurrence across runs; whether tool results were
   interpreted and used correctly; and whether the chosen tool, source,
   workspace, run, group, table, endpoint, IDs, filters, time window, and
   destination were semantically correct. A zero duration is unmeasured, not
   instant. A zero exit code containing explicit error evidence is suspicious,
   not clean. Distinguish proven failure, review candidate, and evidence gap.
   Use judgment to decide necessity, impact, and the recommendation; do not
   assume a deterministic Go detector has already classified the trace.
5. Reconcile raw ledgers before judging cost. For current ledgers, the
   immutable `execution_id` (or `evaluation_id`) is the record identity;
   `date + scope + group_folder` locates its shard, while `run_folder` and
   `archived_run_folder` are display metadata only. Never merge or compare
   records merely because they share an `iteration-0/...` path: that path is
   reused after rotation. Use `run_folders` only as a legacy fallback when no
   ID-keyed record exists. Within each execution record and model, treat
   `by_model` as authoritative and `by_step_and_model` as included attribution;
   never add both. Report a positive remainder as unattributed/orchestrator, do
   not double-count an explicit `workflow_orchestrator` row, and report
   overflow, missing buckets, and unpriced calls instead of estimating.
6. Inventory exact model pins in explicit workflow roles and
   planning/evaluation step config. Call `list_provider_models` once per pinned
   provider and compare against its catalog and `default_tier_models`; never
   infer recency from model names. Provider-profile defaults update automatically
   and are not stale pins.
7. Judge structural fitness against the plan-design checklist, which names this
   module as its owner. Load
   `read_skill(skills=[{"name":"workflow-commands","path":"references/design-plan.md"}])`
   and apply **PART 3 — STEP-TYPE FITNESS** to the current plan. (It lives in
   `workflow-commands`, not `builder-reference`; `builder-reference` carries the
   different, authoring-time `plan-design.md`.) That checklist's
   own contract makes this a read-only use: return findings to the parent and
   edit no workspace file; the Pulse Fixer remains the only writer. Attribute
   these findings to `technical_review`. Cite the checklist rather than restating
   it. This is the one review that can judge step shape from *behaviour* instead
   of description, because it is the only one holding per-step cost, tool-call
   counts, and full tool-call traces:
   - **Scripted candidates.** A step whose real tool-call sequence is
     deterministic and identical in shape across runs is a scripted candidate
     even when its description reads as agentic. Ground the finding in the trace
     evidence, never the description alone. Judgment, synthesis, adaptive
     discovery, and browser/UI work stay agentic; do not propose scripting them
     to save cost.
   - **Container necessity.** For every cost- or time-material `orchestrator`,
     `routing`, or `message_sequence` container, inspect the parent and its
     owned children as one execution unit. Read the targeted plan definition and
     representative parent/child traces, then state the actual runtime decision
     the parent makes. A fully prescribed child set and order is a structural
     review candidate: do not accept "coordination" as value when the plan has
     already enumerated the work. Preserve the container when evidence shows
     genuine runtime selection, conditional fan-out, adaptive retry/recovery,
     concurrency, a user or approval boundary, or synthesis that cannot live in
     declared dependencies and a final aggregation step. Treat repeated
     source/schema/data discovery across the parent, retries, and children as
     evidence of a weak handoff; an independent clean-room verification read is
     not automatically waste. Use agent judgment, not numeric thresholds or a
     Go-authored classifier, and recommend only—this read-only review must never
     rewrite the plan itself.
   - **Sequence shape.** Establish where a step's time actually goes before
     recommending anything, measuring four things separately rather than
     collapsing them into one: **model turns** (a recorded tool call is not
     automatically its own round-trip — code-execution mode and batching can
     issue many underlying calls inside a single model action), **tool-call
     count**, **payload size** (a large result is not just slow to transfer; it
     enlarges the context every later turn re-reads, so it can cost more than
     the call that produced it), and **parallelism** (serial calls that could
     have run concurrently). Name which of the four dominates, with the
     measurement, and recommend against that one — batching a step whose cost is
     really payload growth, or trimming payloads when the real cost is serial
     round-trips, both spend effort for nothing. Propose merging adjacent steps
     only when they genuinely share context, and splitting only at a boundary
     the checklist recognises (credentials/security, independent outputs or
     retries, clean-room independence, human/routing boundaries, context
     contamination). "It is long" is not a boundary.
   - **Schedule execution model.** Inspect enabled schedules as runtime entry
     points. Route-backed schedules should select planned work via group_names
     and route_selections. Direct message sequences are also valid when they
     carry `direct_messages_reason`; measure their prompt/call cost and name the
     missing step-level lifecycle, but do not call them defective merely for
     being long. Recommend a route only after verifying matching side effects,
     approval boundary, failure behavior, and reuse (especially draft-only
     versus publish routes). Artifact Review owns topology drift; record an Ops
     finding only for measured cost, latency, or runtime impact.
   - **A schedule with no recent runs is not evidence of a scheduler defect
     by itself.** `get_schedule_runs` returns both actual runs AND any recent
     occurrences the scheduler deliberately did not run (global pause, another
     schedule already owning the workflow, a queued dependency), each with its
     real reason — call it and read that list before concluding a schedule
     "silently skipped" or theorizing about a missing misfire-recovery
     mechanism. Live case: four Technical Review passes across three weeks
     built an increasingly detailed theory that the platform scheduler has "no
     durable missed-slot recovery" — the scheduler was in fact evaluating and
     correctly skipping every single occurrence on time, the whole gap was one
     multi-day global pause, and the answer was sitting in this same tool the
     whole time, unread. Only escalate a scheduler-code finding once this list
     is checked and does not explain the gap.
   - **A null `lcp_ms` on a shared-CDP browser is not evidence of a capture
     bug by itself.** CDP mode connects every concurrent workflow session to
     one real, shared Chrome instance. Largest Contentful Paint is withheld
     by the browser's own spec-mandated behavior whenever
     `document.visibilityState` is not `"visible"` at capture time — no
     page-side script or web-vitals library can work around that. Live-tested
     directly: `agent_browser`'s own tab-switch command does correctly bring
     the target tab to the foreground when used in isolation, so a null
     reading is not the tool silently failing to switch. The gap is
     concurrency: another session sharing the same CDP port can switch its
     own tab to the foreground between this session's switch and its actual
     capture, and creating a dedicated labeled tab does not prevent that race
     — it only fixes tab *identity* (PLAT-181's concern), not tab
     *foreground*. Treat a null `lcp_ms` under concurrent CDP sharing as a
     known, structural limitation of the current shared-browser architecture
     (see PLAT-181 for the sibling ownership-collision issue on the same
     root cause), not a new platform bug to keep re-filing every cycle. A
     real fix requires giving performance-sensitive steps a dedicated,
     non-shared CDP browser instance — a deliberately out-of-scope
     architecture change, not a quick patch.
   - **Reflection yield.** `reflection:<step-id>` is attributed separately from
     `execution_only:<step-id>` in the cost ledger, and `reflection-timing.json`
     sits beside the execution timing files. **A single reflection turn that
     recorded no learning is not waste** — a turn that examined the run and
     correctly concluded there was nothing new to record is the mechanism
     working, and treating it as overhead would penalise honesty and push the
     next turn toward inventing a learning to look productive. Judge the pattern
     instead: only where several comparable runs (same route, materially
     equivalent configuration) each show meaningful reflection cost together
     with consistently absent contribution — `wrote_learnings`/`wrote_kb` false,
     corroborated by `has_new_learning` in
     `learnings/<step-id>/.learning_metadata.json` `detection_history` — is
     there a finding. Then the recommendation is to sharpen the objective, or
     drop the step to `learnings_access: "read"` when it should consume but not
     contribute; not to make the turn faster. State the number of runs the
     judgment rests on.
   Any recommendation here still obeys item 8: state current state, exact
   suggestion, expected benefit, risk, and evidence. A step-type change carries
   real risk — scripting a step that is not actually deterministic breaks it —
   so where the trace does not settle it, say so and leave it agentic.

### Prompt-contract health

Call
`read_skill(skills=[{"name":"builder-reference","path":"references/step-description.md"}])`
before this check. Its
prompt-engineering contract is the review rubric, including for short prompts;
the size report below is only a triage signal and cannot establish quality.

Before making a prompt-bloat judgment, call `get_plan_prompt_health` once.
It reports authored description sizes and long verbatim duplicate paragraphs
without injecting the full plan into this review. Inspect only the exact
affected step definitions, validation schemas, and referenced shared contracts
afterward. Do not equate a long prompt with a defect: browser/UI work, adaptive
research, and safety-critical judgment can legitimately need substantial
context.

When the report crosses a triage boundary (a step over 20k characters, 30% or
more of described steps over 5k, or 10k or more repeated description
characters), read the flagged steps' full descriptions yourself — the tool
only measures size and exact-text duplication, it has no sense of content
quality, so this reading is where the actual judgment happens. For each
flagged step, ask two separate questions, since either can be true without
the other:

- **Wrong home.** Does the description carry durable HOW-to-operate
  knowledge (selectors, API quirks, timing, auth flows) that belongs in
  `learnings/_global/SKILL.md`; business/domain facts that belong in the
  knowledgebase; produced data that belongs in `db/db.sqlite`; or shared
  database, browser, validation, or policy prose duplicated elsewhere that
  belongs in a versioned reference or deterministic script? A step should
  reference that store (per its `learnings_access`/`knowledgebase_access`),
  not restate its contents.
- **Not concise.** Independent of duplication or wrong-home content, is the
  step's own instruction itself wordier than the judgment it asks for
  requires — restating context the model doesn't need, hedging with
  redundant qualifiers, or spelling out a procedure that could be stated as
  a precise, unambiguous outcome instead? A step can be long because its
  task is genuinely complex (safety-critical judgment, adaptive research,
  browser/UI work) without being imprecise; the question is whether the
  same precision survives a tighter rewrite, not whether it can be made
  short.

Apply a third question to `validation_schema`, not only to steps that
crossed the description-size triage boundary — a schema can be
over-specified even when its description is short, and `validation_schema`
now renders into the executing agent's prompt on every attempt (not only on
retry), so its size is a live prompt cost, not just an authoring artifact.
`get_plan_prompt_health` gives no schema-size signal, so run a cheap
`jq`-style scan of `planning/plan.json` for schemas with an unusually high
check count relative to the step's actual output, then read only those
schemas in full — this stays a lightweight scan, not a full-plan read:

- **Over-specified schema.** Is every `json_checks`/`files`/`db` entry
  load-bearing — does it catch a real failure a downstream step, evaluator,
  or user actually depends on — or does it re-check structure the model
  gets right by construction, assert on free-form/optional content where
  cosmetic variation isn't actually wrong, or simply mirror the entire
  output document field-by-field? A schema this large is not automatically
  wrong (a genuinely multi-file, multi-field contract earns its size), but
  each check should survive "what real failure does this catch" — if the
  honest answer is "none, it's just thorough," that check is bloat and a
  source of spurious retries, not rigor.

File **one canonical workflow-level finding**, not one finding per large
step. Its evidence must name the measured totals and the exact affected
steps, and say which of the three questions above applies to each one (they
are not the same defect and can call for different fixes). Its
recommendation must name the proposed fix per step: which store it should
move to and what the step should reference instead, a tighter rewrite that
preserves the same precision, or — when the size is genuinely earned by the
task — no change.

A migration spanning several steps, shared references, public-action behavior,
or output ownership is `decision_required`: create a stable
`technical-decision-prompt-contract-consolidation-...` request with
approve/reject/defer options and the phased extraction order, preserved
boundaries, expected benefit, risk, and verification plan. A small local
deduplication that preserves step inputs, outputs, validation, routes, and
side-effect order may use `fixer_handoff`. The Fixer changes one bounded
contract at a time and requires a post-change producing run; it never bulk
shortens prose merely to meet a numeric target.

### Execution-health diagnosis

When Gate selected `execution_health`, make this a bounded causal review,
not a broad list of expensive calls. Read the current `planning/plan.json` and
the smallest comparable timing/cost/trace evidence needed for the affected
steps. Produce one compact diagnosis that names:

When a Gate `deterministic_intake.runtime` signal selected this focus, begin
with its exact `run_folder`, `step_id`, and timing artifact. It proves only a
status fact; determine whether the failed child call was essential, recovered,
already represented by a canonical issue, or makes the claimed result
unreliable. Do not create a separate finding solely because the signal exists.

- the one to three exact step or message-sequence IDs responsible for the
  material delay, with measured turn time, input/context cost, tool-call count,
  retry count, or retained-result size as applicable;
- which mechanism dominates: repeated context reconstruction, payload carried
  into later turns, duplicated discovery/validation, unnecessary sequence or
  container ownership, or unavoidable external/browser work;
- the safety, approval, clean-room, credential, or adaptive-decision boundary
  that must remain; and
- the smallest structural migration that removes only the demonstrated waste,
  plus the evidence boundary for measuring its result over comparable future
  runs.

Do not call a large result or a long turn waste merely because it is large or
long. Do not infer an input-token count from output size, and do not replace an
agentic decision with a scripted calculation unless the representative trace
proves that the work is deterministic. The expected benefit is a hypothesis
to measure, never a promised cost saving.

If that migration changes plan topology, step type, route ownership, retry
semantics, public-action ordering, or a safety boundary, it is not a normal
Fixer handoff. Create one stable `ops-decision-execution-efficiency-...`
human-input request with approve/reject/defer options, the exact migration,
the preserved boundary, expected benefit, alternative, risk, and measured
evidence. File the canonical finding as `decision_required` linked to that
request. A safe local prompt/output-size correction that preserves topology
may use `fixer_handoff` instead.
8. Require a compact result grouped by `cost`, `time`,
   `tool/runtime reliability`, `quality`, and `setup`. Every recommendation
   needs current state, exact suggestion, expected benefit, risk, and evidence.
   Separate evidence gaps from true optimization opportunities. If a material goal criterion is below target,
   forbid tier/model downgrades for outcome-bearing, reasoning, diagnostic,
   recovery, eval, and verification steps. A downgrade is eligible only for a
   deterministic non-bottleneck step with representative evidence proving
   quality-equivalent output and no downstream outcome loss; label it as an
   approval-required reversible trial. Missing evidence means keep the tier.
   For an unavailable/deprecated pin or a materially useful newer provider-owned
   default, recommend either clearing the pin to inherit its tier or one exact
   supported replacement. Label it user approval required and include current
   model, affected roles/steps, capability/cost/reasoning comparison, expected
   benefit, and risk. A newer model is not automatically better.
   Return a non-HTML packet with `module=technical_review`, `verdict`, `next_check`,
   and ordered findings. Every finding includes no invented identifier,
   severity, plain-language summary, exact evidence, bounded
   `recommended_fix`, verification, and `user_judgment_required` with reason.
   A shared harness/runtime/bridge/tool-API defect must be classified as
   `issue_kind=harness_issue` and be persisted with `record_pulse_finding`
   under the injected typed review contract, including expected versus observed and
   a minimal safe reproduction or an explicit limitation. Wrong workflow
   arguments, paths, credentials, IDs, and data remain workflow findings. A
   harness issue is platform-owned, not a user-decision request, unless the
   remaining question is genuinely product policy.
9. Before filing any finding with `recommended_route="decision_required"`,
   prove that the goal does not already settle the choice and that the tradeoff
   materially affects reliability, cost, quality, real users, or workflow
   meaning. Create or refresh one stable
   `create_human_input_request(source="technical_review", input_id="technical-decision-...", options=[approve,reject,defer])`
   with the exact proposed change, expected benefit, alternative, risk, and
   evidence. Then pass that returned id as `human_input_id` on
   `record_pulse_finding`; the typed write links the finding as `awaiting_user`.
   Never emit `decision_required` without this question. Safe technical work
   uses `fixer_handoff`, and missing future evidence uses `evidence_wait`.
10. Reconcile your findings against the actual artifacts, call
   `record_pulse_review_focus(module="technical_review", ...)` once per focus
   actually investigated, including `route_scope`, selection reason, evidence,
   and deferred focuses,
   then call `record_pulse_result` exactly once with
   `module="technical_review"`, `result="done"`, a concise evidence-grounded
   reason, and its evidence. That module result is the completion boundary;
   returning prose without it leaves the background work incomplete. Do not
   apply recommendations in this read-only command; creating and linking a
   durable decision is allowed.

Include reflection-turn cost as a first-class cost line. Each contributing step
runs one post-completion reflection turn, and it is not free: a measured Social
Media run spent **20.1% of all LLM time** there, with short steps at 30–55% of
their own runtime. Judge yield, not just spend — `learnings/<step-id>/.learning_metadata.json`
records `has_new_learning` per run in `detection_history`. A step whose recent
turns produce nothing is paying for an objective that no longer earns it;
recommend sharpening the objective or dropping the step to
`learnings_access="read"`. A step producing real technique is working as
intended however long it takes.

Finish with a short executive summary followed by every evidence-backed
recommendation in severity order. Identify which exact changes require user
approval before `/engineering-review` can apply them. Do not truncate the result to a
Top 3.

Write every `execution_tier` and `execution_llm` recommendation so that it can
be **stored verbatim as the change's reason**. The Fixer that applies it must
supply `execution_tier_reason` / `execution_llm_reason`, and the tool rejects
the change without one. A scripted↔agentic recommendation is a step-type
change (`change_step_type(step_id, target_type, reason)`, reasoned in the plan
changelog), not a config field — write it to the same standard. Your recommendation text is that reason — state current
state, the exact suggestion, the measured evidence, and the risk in a form that
survives the handoff into `planning/step_config.json`, which is what the next
reviewer reads. A recommendation too vague to store is too vague to apply.
