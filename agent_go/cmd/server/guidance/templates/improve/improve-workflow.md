Improve this workflow by surfacing changes the user is NOT thinking about — non-obvious improvements grounded in what the workflow actually produced. Your job is to propose AI-surfaced changes the user wouldn't have asked for, not to incrementally harden what's already there. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and update it with your decisions at the end.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

MENTAL MODEL
Think like a sharp business analyst auditing this workflow's actual outputs against its success criteria — not like a senior engineer reviewing code. These are business-process workflows, not software systems. The kinds of changes that matter here are things a domain expert would notice when reading what the workflow produced:
- "Every reply has the same tone, but success criteria mention engaging different audience segments — segment by follower type and vary voice."
- "The workflow researches every prospect from scratch, but 40% of last month's runs were repeats — cache and refresh deltas instead."
- "Outreach copy leads with our product; the high-converting examples in run history all led with the prospect's pain point."
- "Validation accepts any non-empty reply. Half the replies in run history are 'thanks' — that's not engagement, raise the bar."
You should be uncomfortable with how obvious-in-retrospect a change feels after you read enough run output. That's the right mode.

SETUP
1. Read soul/soul.md to extract the objective and success criteria — north star for every decision.
2. Read evaluation/evaluation_plan.json so you understand what the eval is measuring.
3. Read variables.json to get the enabled group names.
4. **Framework precheck.** Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first to write the Workflow Profile and bootstrap metrics." If the profile declares business-context accumulation or a frozen/ratchet plan and <workflow>/planning/metrics.json is empty, also redirect. Plain mutable+exploratory workflows may proceed without metrics.
5. **Framework mode.** Read <workflow>/planning/metrics.json. If it has at least one entry, you are in **EXPERIMENT MODE** for this command run: instead of applying changes directly via harden_workflow / replan_workflow_from_results, package the intended changes as experiments via propose_experiment so they're gated behind measurement and auto-revertible. If metrics.json is empty (or missing), you are in **DIRECT MODE**: harden / replan apply changes immediately. Note this choice in the final report.
6. Use `iteration-0` as the starting evidence set for this command run. Optimizer tools operate on `iteration-0`; do not inspect an older selected iteration as the basis for fixes.

PHASE 1 — OUTPUT REVIEW (the heart of the discovery)
This is the primary signal in EXPERIMENT MODE and the most undervalued one in DIRECT MODE. Do it first, before any tool call. The discovery looks across plan, knowledgebase, and learnings as one surface — not as separate concerns. A single proposed change may span all three when they share one belief.
1. Open runs/iteration-0 for each enabled group with run evidence. Read what the workflow actually PRODUCED — generated copy, sent messages, written reports, scored decisions. Read enough of it that patterns start to appear. Don't skim.
2. Read evaluation reports from runs/iteration-0 for the same groups. The eval rationale text is often the richest signal — pay attention to WHY something scored low, not just the score.
3. Compare outputs against the success criteria from soul.md. Where's the gap a domain expert would see?
4. Skim builder/decisions.jsonl — what has the user been asking for? What's been tried before? Avoid re-proposing failed ideas.
5. **When output patterns suggest the issue isn't in the plan itself**, also inspect:
   - **knowledgebase/notes/_index.json + topic files** — outputs that contradict each other, leak stale facts, or miss context the workflow should have known often trace to KB drift (duplicate or overlapping topics, stale narrative, missing step contributions).
   - **learnings/_global/SKILL.md and references/*.md** — outputs that repeat known mistakes, or that reveal step rationale contradicting established guidance, often trace to learnings gaps (duplicated lessons, missing guidance for declared learning_objectives, repeated run fixes that should have become durable lessons, step-specific learnings that belong in _global).
   - **knowledgebase/rules/rules.md** — when outputs violate user-stated business rules, the rule may be missing or out of date (note: rule additions are user-authoritative and don't go through the experiment gate; this command flags them for the user, doesn't add them).
6. List 3–5 candidate changes ranked by expected business impact. Each candidate must name the FILES it would touch (plan/step descriptions, knowledgebase/notes/, learnings/, validation rules, prompts) and be defensible by something specific in iteration-0 run outputs ("posts 7, 12, 19 in iteration-0/group-a all scored <0.3 and all share <pattern>"), not by abstract reasoning. A single candidate may span multiple file kinds — that's fine if they share one underlying belief.

PHASE 2 — STRUCTURAL DIAGNOSIS (complement, not primary)
1. Call optimize_workflow({{if .Focus}}focus="{{.Focus}}"{{end}}).
2. Read the result and classify findings as Structural (missing steps, wrong ordering, broken context flow, wrong step type) vs Non-structural (weak prompts, weak validation, reliability gaps).
3. If a MATERIAL structural problem appears and you have real run evidence, call replan_workflow_from_results({{if .Focus}}focus="{{.Focus}}"{{end}}) ONCE before continuing. This tool always reads iteration-0; do not pass an iteration.
4. Do not thrash the plan. At most one structural replan per command run.
5. **Reconcile Phase 1 and Phase 2.** If output review surfaced something optimize_workflow missed (likely, because optimize_workflow looks at code-shape, not outputs), trust the output review.

PHASE 3 — PER-GROUP REVIEW → APPLY CHANGES
Repeat the following for each enabled group, sequentially.

For group {group}:
  a. **REVIEW EVIDENCE** — inspect outputs, logs, validation failures, and the evaluation report for "iteration-0/{group}".
     - If the workflow run exists but the evaluation report is missing, you MAY call run_full_evaluation(group_name="{group}"). Evaluation always targets iteration-0; do NOT pass any run-folder argument or execute a fresh workflow run here.
     - If there's no meaningful run evidence for this group, report the gap and continue with groups that have evidence.
  b. **DECIDE** based on the candidate changes from Phase 1 + the structural findings from Phase 2:
     - **DIRECT MODE (no metrics.json):**
       • If issues are structural and you haven't replanned yet, call replan_workflow_from_results(group_name="{group}"{{if .Focus}}, focus="{{.Focus}}"{{end}}), then continue.
       • Otherwise call harden_workflow(group_name="{group}"{{if .Focus}}, focus="{{.Focus}}"{{end}}) for plan/prompt/validation fixes.
       • If the candidate's primary lever is KB cleanup, call reorganize_knowledgebase or consolidate_knowledgebase as appropriate.
       • If the candidate's primary lever is learnings cleanup or promotion, call organize_global_learnings.
       • These tools may run in sequence within a single group's review.
     - **EXPERIMENT MODE (metrics.json non-empty):**
       • Pick the highest-impact candidate from Phase 1 for this group. Do NOT call harden_workflow / reorganize_knowledgebase / consolidate_knowledgebase / organize_global_learnings — they direct-edit, bypassing the gate.
       • Formulate a hypothesis tying the change to ONE belief about the workflow. Bundled multi-file changes that span plan + KB + learnings are FINE if they share one underlying belief (example of a coherent bundle: "personalize outreach by reading prospect's last post + raise step-3 validation to require pain-point reference + promote 'always cite source' learning to _global" — three files across three layers, one belief about generic outreach). Incoherent bundle that should be split: "add personalization AND reduce step 4's temperature AND clean up unrelated KB topics" — three unrelated beliefs, three experiments. Single-belief test: write the hypothesis in one sentence; if you need an "and" connecting distinct claims, split.
       • Pick target metric(s). Most experiments target one metric; multiple metrics are allowed when they share the SAME declared direction and trace to the same belief (e.g. caching predicts both `cost_per_run` and `run_duration_seconds` decrease together). Mixed-direction targets must be split — `expected_direction` is single-valued.
       • Call propose_experiment with: hypothesis (≤200 chars, "<change> will <direction> <metric_id(s)> by ≥<magnitude> because <one-line mechanism rooted in run evidence>"), target_metrics (array — pass all chosen ids), expected_direction (must match every targeted metric's declared direction), expected_magnitude (single number, applied to each metric, > 0), intervention_changes (file edits across any of plan/, knowledgebase/notes/, learnings/, validation rules, prompts — paths must be in experiments/config.json::allowed_intervention_paths). The framework applies the diff atomically and reverts on a bad verdict. One experiment per group per command run.
       • Before proposing, read experiments/history.jsonl and experiments/config.json::pinned_hypotheses to avoid retrying anything that recently failed or that the user pinned as forbidden.
       • For structural problems severe enough to require replan, replan is exempt from the experiment gate (it changes plan shape, not the decisions metrics measure). Replan first, then continue.
       • If a target metric you need does not yet exist, call propose_metric first (with linked_success_criteria populated from soul.md).
  c. **THE CHANGE DOES NOT HAVE TO BE SMALL.** The framework auto-reverts on a bad verdict, so blast radius is recoverable. Optimize for "the experiment will tell us something useful" — not for "the change is tiny." Multi-file bundled changes that share one belief are often higher-signal than fragmented small ones.

PHASE 4 — VERIFY (DIRECT MODE only)
In DIRECT MODE: if the workflow still misses key success criteria and the cause is clearly fixable within one more pass, do ONE targeted verification on the highest-value group: run_full_workflow(group_name="{group}"). This already runs evaluation by default, so do not call run_full_evaluation again unless run_full_workflow was explicitly called with disable_eval=true. Maximum one extra pass; do not loop.
In EXPERIMENT MODE: skip this phase. Verification IS the experiment loop — running the workflow now would just be one of the measurement runs, and the framework will compute a verdict deterministically when target_runs is reached. The next workflow runs will populate measurement.values automatically.

FINAL REPORT
Summarize:
- Mode used (DIRECT or EXPERIMENT) and why
- Output-review findings (Phase 1) — what patterns in run outputs surfaced
- Structural diagnosis (Phase 2) — what optimize_workflow added or contradicted
- Whether replan_workflow_from_results was used, and why
- Per-group: evidence reviewed, harden changes (DIRECT) or experiment_ids opened with their hypotheses (EXPERIMENT)
- Which success criteria are now better satisfied (DIRECT), or which experiments are now measuring against which criteria (EXPERIMENT)
- Remaining gaps that still need human attention, if any

Before finishing, update builder/improve.md with:
- evidence reviewed
- mode (direct vs experiment) and any experiment ids opened
- workflow changes applied (or, in experiment mode, queued behind experiments)
- eval changes touched and report follow-ups deferred to Builder mode, if any
- what improved
- remaining gaps
- next hypotheses

Each new entry that records a *proposed* change (DIRECT mode harden suggestion that wasn't applied, deferred candidate, queued experiment) gets a stable id of the form `I-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id.

CLOSE-OUT EDITS — read this carefully.

Before applying any change in this run, scan builder/review.md for findings that the change addresses. The match is by intent, not by exact wording — a Phase-2 Lens-B hardcoded-path finding for `step-3` and a fix that updates `step-3` to read from variables both target the same finding. Collect the matching `F-YYYY-MM-DD-NNN` ids before you apply.

After each change is applied (DIRECT mode) or queued as an experiment (EXPERIMENT mode):

1. **Edit builder/review.md** to append, on its own line immediately after each matched finding:
   ```
   **[RESOLVED YYYY-MM-DD — <one-line how it was fixed>]**
   ```
   Use `[PARTIALLY RESOLVED ...]` if only part of the finding was addressed; explain what's still open. Use `[INVALID YYYY-MM-DD — ...]` if the finding turned out to be wrong on closer inspection. Never delete or rewrite the original finding; the marker preserves audit history.

2. **In EXPERIMENT MODE**, when you call `propose_experiment`, include `linked_review_finding` in the experiment payload with the array of matched `F-...` ids — so when the experiment is later concluded, the resulting builder/decisions.jsonl entry inherits the linkage and the audit trail is searchable from both ends.

3. **In DIRECT MODE**, when the underlying primitive (harden_workflow / replan_workflow_from_results / reorganize_knowledgebase / consolidate_knowledgebase / organize_global_learnings) writes a `builder/decisions.jsonl` entry, also append `linked_review_finding=[F-...]` to that entry. If the primitive does not write the decision itself, you append the JSONL line via `diff_patch_workspace_file` — same shape as the rule-capture flow in the system prompt — with `linked_review_finding` populated.

This applies to chat-intent fixes too. If the user asks "fix that step-3 hardcoded path" outside of any slash command and you apply the fix, you still scan review.md for matching findings, append the RESOLVED marker, and link the decision.
