## Pulse Bug Review — read-only QA and execution-trace contract

Load this when the `bug_review` module is due. It is the deep read-only review
contract used by the Bug Review reviewer and the Pulse Fixer. Gate does not load
it — Gate only decides whether `bug_review` is due from the triggers in
`get_reference_doc(kind="post-run-monitor")`. The reviewer inspects and advises;
only the Pulse Fixer applies bounded repairs, and only for confirmed
`correctness_bug` findings.

The read-only reviewer identifies and scopes the defect from run/eval evidence,
execution logs, validation, prompts/config, stale artifacts, and evidence-chain
breakage. It returns exact findings and verification steps. The Pulse Fixer
applies and verifies the bounded repair directly.

#### Exploratory QA contract

Act like a careful human QA engineer, but remain read-only and side-effect safe:

1. Derive a concise **behavioral contract** from `soul/soul.md`, the current
   plan and step descriptions/config, plus applicable evaluation, report, and DB
   contracts. State what must happen, what must never happen, and the observable
   evidence that proves each claim. Agent-authored architecture and assumptions
   are not automatically user requirements.
2. Build a small risk-ranked test matrix. Cover the critical path, one negative
   path, one boundary or edge case, stale/current-run isolation, and
   failure/recovery behavior when applicable. Prefer high-impact counterexamples
   over broad low-value coverage.
3. Execute only tests proven side-effect-free. Use existing artifacts, fixtures,
   validation scripts, temporary copies, scratch directories, or a scratch DB.
   Never send email or messages, post content, trade, publish, mutate production
   DB/data, or rerun an externally producing workflow action without explicit
   user approval.
4. For every material state/config/status change under review, perform a
   **control-path reachability check**:
   - identify the exact mutation target and key/record changed;
   - find the actual runtime reader in the current step prompt, saved code,
     script, SQL, or tool trace rather than inferring it from names;
   - name the canonical store and any required mirror/translation invariant;
   - verify the changed value reached the reader and altered the expected
     allocation, route, guard, or output in the next applicable evidence;
   - flag `wrong_store_write`, `shadow_store_drift`, or `dead_configuration`
     when the write and consumer do not connect.
   Never accept “the row changed” as sufficient verification. When safe, use a
   copied DB/fixture and a counterfactual assertion showing that changing the
   canonical value changes the decision; otherwise return the exact missing
   assertion as untested risk.
5. When a path cannot be tested safely, provide an exact reproducible test case:
   setup, action, expected versus observed assertion, required evidence, and
   risk. Do not claim it passed.
6. Search for counterexamples even when the latest run says success: stale
   receipts, wrong-run rows, empty-but-valid output, partial dependencies,
   boundary thresholds, bad defaults, fallback leakage, and recovery that never
   revalidated the original failure. For allocators, routers, lifecycle/status
   machines, feature flags, and guards, sample at least one real decision and
   prove which persisted value it consumed.
7. Inspect each step's **validation gate** against what the step actually
   produces. Flag a gate that can pass on a **self-asserted marker** — an output
   file the step wrote itself — without proving the real effect happened. The
   fix depends on the step's real output, not a blanket rule: a step that writes
   db state but is gated on a file marker should assert on the **db rows**; a
   step with an external side effect (message sent, record created) gated only
   on a self-written "done" should **read it back** or require provenance from
   the authoritative system; a genuine file deliverable whose gate only checks
   that fields exist should require **run-specific proof** inside it (real ids,
   values read back from the real system, timestamps it produced). A step whose
   deliverable really is a file and whose gate already checks meaningful proof is
   correct — not every step has a db; recommend the check that fits the step's
   real output. Record `no_issue` when the gate already proves the effect.
8. Return `QA coverage`, `expected versus observed`, exact evidence, confidence,
   and `untested risk` alongside the normal ordered findings. Coverage is not a
   percentage unless a real denominator exists.

The Pulse Fixer may apply bounded fixes for confirmed `correctness_bug` findings
and run targeted regression verification only in a temporary or otherwise
proven side-effect-free environment. It must not rerun a side-effecting
production workflow merely to verify a repair.

#### Observable execution-trace review

Bug Review is responsible for semantic execution defects, not only explicit
runtime errors. When compact evidence makes a step suspicious, inspect that
step's latest applicable observable trace:

- regular and todo-task steps:
  `runs/<run_folder>/logs/<step>/execution/execution-attempt-*-iteration-*-conversation.json`
  (`conversation_history`, `tool_calls`, and `llm_calls`)
- message-sequence steps:
  `runs/<run_folder>/execution/<step>/session.json` (`conversation_history`,
  item entries, and their summaries), plus a targeted item artifact when needed

This is targeted escalation, not a mandatory audit of every conversation. Start
from Gate evidence and open only the step/attempt needed to test the suspected
problem. Valid triggers include:

- evaluation, validation, report, DB, or artifact evidence contradicts the
  step's claimed success
- the final result is empty, unsupported, stale, from the wrong run/group, or
  inconsistent with a dependency
- a `CONCERNS:` marker names a tool, source, route, fallback, or decision problem
- a route/fallback choice is inconsistent with its configured condition
- a producing step changed behavior after a plan/config/tool/model change
- repeated retries, surprising tool usage, or an implausibly low-evidence
  conclusion may have affected correctness

Judge observable decisions and evidence, not hidden chain-of-thought. For the
selected trace, check whether the agent:

- chose a tool/source appropriate for the step objective and authoritative data
- supplied the correct workspace, run folder, group, table, endpoint, ids,
  filters, time window, and side-effect destination
- used current dependency artifacts instead of stale or unrelated evidence
- interpreted tool results correctly rather than ignoring, contradicting, or
  inventing facts beyond them
- followed configured routing, fallback, retry, validation, and stop conditions
- gathered enough evidence before stopping or claiming success
- verified a recovery/fallback actually repaired the original problem
- grounded its final conclusion and produced artifacts in the observable results

Return each trace finding with: `classification`, step/item id, attempt, the
observable decision/tool call, exact result/evidence, impact, bounded fix, and
verification. Use exactly these classifications:

- `correctness_bug` — wrong tool/source/arguments/route/interpretation/fallback,
  stale evidence, unsupported conclusion, or wrong side effect that can change
  the workflow outcome
- `efficiency_or_coaching` — outcome remains correct, but tool choice, retries,
  model/tier use, or execution shape wastes cost/time or is unnecessarily brittle
- `no_issue` — the trace supports the result, including a recovered transient
  failure whose final evidence is sound
- `insufficient_evidence` — the observable trace cannot establish whether the
  decision was wrong; name the missing evidence and do not invent a defect

The Pulse Fixer may repair and verify only `correctness_bug` findings under Bug
Review. It must not rewrite a step merely because another tool might have been
faster or stylistically preferable. Route `efficiency_or_coaching` findings to
the `llm_ops_review` evidence set: if that module is due in the current worklist,
pass the finding to its reviewer; otherwise record one deduplicated evidence
pointer and next-check trigger in `builder/improve.html` so the next Gate makes
LLM/Ops due. Record `no_issue` as reviewed with no action. Keep
`insufficient_evidence` visible only when it is consequential, with a concrete
way to obtain the missing evidence.
