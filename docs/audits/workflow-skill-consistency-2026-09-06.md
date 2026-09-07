# Workflow skill consistency review — 2026-09-06

## Scope and method

Reviewed the 65 canonical workflow guidance templates under
`agent_go/cmd/server/guidance/templates/`, together with their registry descriptions
and Workshop/Run/step materialization tests. This covers the platform skill bundle;
it does not audit every installed third-party skill or every workflow's generated
`learnings/_global/` package. The inventory below includes ignored files such as
`secret-management.md`, not just files returned by Git-aware searches.

Cross-checked instructions about description/schema ownership, store access,
authority, human input, routing, reviews, execution, scheduling, reports, tools,
paths, and secrets. Checked concrete API/runtime behavior for human choices,
knowledge-base defaults, review invalidation, and managed DB access. This is a
source consistency review, not a guarantee of runtime correctness or live UI behavior.

## Conflicts corrected

| Area | Conflicting instructions | Current guidance |
| --- | --- | --- |
| Step descriptions | Design guidance separated WHAT/HOW, but optimization/debugging still copied field lists and procedures into descriptions. | Description states objective, evidence, scope, binding constraints, success, and output destination. Reusable HOW stays in accessible skills/learnings; output structure stays in schema/canonical contracts. |
| Validation | Optimization suggested increasingly exhaustive schemas could prove freshness and that a passing schema meant approval. | Validate meaningful consumer requirements and provenance; semantic evidence and human authorization remain separate. |
| Human checkpoints | Older patterns recommended new yes/no or multiple-choice human_input steps; the runtime rejects them. | New human_input captures text; a human-decided branch represents a fixed choice. Preserve legacy steps. |
| Tool discovery | add_human_input_step's schema advertised response types that its executor rejects. | Creation advertises text only. Update still advertises all supported legacy types; the existing runtime rejection test now checks both discovery contracts. |
| Branch/routing | Some sections claimed all mechanics were identical and missing answers always failed. Route-review summaries prohibited even deliberate convergence. | Human branches can prompt interactively; unattended runs use a safe non-approval default or fail. Explicit route convergence is valid; accidental sharing of exclusive route interiors is not. Eval pairing is conditional on an eval plan existing. |
| Review metadata | Optimization said description_reviewed was never invalidated automatically. | Plan tools clear it on contract changes; review the final contract before marking it current. |
| Knowledge base | stores.md said both default-none and default-read. | Default read; staged contributions can promote unset access, while explicit none wins. |
| DB access | DB/report reviews and publish instructions used raw sqlite3 despite the managed DB boundary. Message-sequence guidance implied direct file access. | Agentic reads/writes use managed tools; scripted compatibility remains distinct. |
| Reviewer roles | Lower checklist wording sounded like mutation permission despite read-only wrappers. | Child reviewers return recommendations; the coordinating phase has only its explicitly assigned persistence/repair authority. |
| Schedules | Unattended instructions said to decide everything autonomously and use defaults for uncertainty. Backup setup also ignored an existing off policy. | Execute within existing authorization, leave missing approvals pending, and honor the chosen backup/Pulse policy. |
| Parallelism | “Run all groups” counted as parallel permission despite the sequential default. | Group scope alone does not request parallelism. |
| Environment and files | Required-env rules prohibited documented optional flags; uploads prescribed a builder-only folder to workflow steps. | Required variables fail closed; explicitly optional flags may default. Use the current session's authorized output paths. |
| Report evals | Generic guidance assumed every workflow's soul.md contained a specific no-aggregate rule. | Keep criteria and coverage visible; aggregate only according to an explicitly defined workflow rule. |
| Secrets and backup | One section recommended plaintext secrets in private Git while the secret guide and backup matrix prohibited it. Secret-save guidance also literally prohibited the designated save tool's arguments. | Keep secrets out of Git. Designated secret-save tools may receive the requested value; unrelated tools and plaintext exports may not. |

## Intentional distinctions retained

- Schedule `mode="workshop"` chooses the execution path; `workshop_mode="run"`
  controls normal run authority. This is not permission to edit the workflow.
- Workshop edits versus Run operations, and parent fixer versus read-only child,
  are different capabilities rather than competing instructions.
- Live reports use the report bridge; published snapshots use baked data and
  cannot dispatch live approvals/chat. A saved approval and queued agent work
  are different events.
- Scripted code retains its documented absolute DB_PATH compatibility;
  agentic tool access does not gain raw SQLite access from that exception.
- A major route and a small branch may deliberately converge. Existing legacy
  input/route forms remain readable without becoming the new authoring default.

## Validation and deployment limits

- `go test ./cmd/server/guidance -count=1`: passed, including all-template
  rendering, materialization, reference paths, and mode/tool surface checks.
- Focused step-based-workflow tests: passed for human branch resolution,
  unattended behavior, input creation/legacy discovery, KB access defaults,
  and description-review invalidation.
- `git diff --check`: passed.
- No server was started or restarted. These are source changes; the running
  deployment and already-loaded conversation instructions were not refreshed.
- No live workflow, external action, secret export, or schedule was executed.

## Reviewed template inventory

- `builder/design-plan.md`
- `db/improve-database.md`
- `improve/define-success.md`
- `improve/engineering-review.md`
- `improve/goal-advisor.md`
- `improve/improve-evaluation.md`
- `improve/pulse-fixer.md`
- `improve/specialize-advisors.md`
- `kb/improve-knowledge.md`
- `learning/improve-learnings.md`
- `report/design-reporting-ui.md`
- `report/improve-report.md`
- `review/migrate-routing-to-branch.md`
- `review/ops-review.md`
- `review/review-artifact-drift.md`
- `review/strategy-auditor.md`
- `review/verify-branch-step.md`
- `system/assumption-audit.md`
- `system/backup-strategy.md`
- `system/branch.md`
- `system/browser-usage.md`
- `system/code-authoring.md`
- `system/debugging-flow.md`
- `system/deployed-channel.md`
- `system/evaluation-plan.md`
- `system/execution-policy.md`
- `system/file-layout.md`
- `system/fix-verification.md`
- `system/html-output.md`
- `system/human-in-the-loop.md`
- `system/human-input.md`
- `system/llm-provider-config.md`
- `system/llm-selection.md`
- `system/mcp-bridge.md`
- `system/message-sequence.md`
- `system/optimize-playbook.md`
- `system/orchestrator.md`
- `system/plan-change-impact.md`
- `system/plan-design.md`
- `system/plan-drift-review.md`
- `system/planning-steps.md`
- `system/publish-strategy.md`
- `system/pulse-bug-review.md`
- `system/pulse-finalizer.md`
- `system/pulse-fixer-practices.md`
- `system/pulse-gate.md`
- `system/pulse-review-fixer.md`
- `system/report-plan.md`
- `system/reporting-policy.md`
- `system/routing.md`
- `system/running-steps.md`
- `system/runtime-context.md`
- `system/schedules.md`
- `system/scripted.md`
- `system/secret-management.md`
- `system/skill-management.md`
- `system/step-config.md`
- `system/step-description.md`
- `system/stores.md`
- `system/strategy-auditor.md`
- `system/workflow-patterns.md`
- `system/workflow-tools.md`
- `system/workshop-mode-flow.md`
- `system/workspace-media-tools.md`
- `system/workspace-views.md`
