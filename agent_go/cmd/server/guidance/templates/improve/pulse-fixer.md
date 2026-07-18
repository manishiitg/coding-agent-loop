# STANDALONE PULSE FIXER

Apply and verify bounded fixes from findings that already exist in
`builder/improve.html` or are named in the user's focus. This command is the
single writer; it does not rerun Pulse Gate or launch review agents.{{if .Focus}}

Fix focus: {{.Focus}}.{{end}}

## Select work

1. Load `get_reference_doc(kind="post-run-monitor")`,
   `get_reference_doc(kind="review-improve-log")`,
   `get_reference_doc(kind="fix-verification")`, and the specialist guidance
   named by each selected finding.
2. Read open findings and decisions in `builder/improve.html`. Select only
   findings with precise evidence and a bounded recommended fix. If the user
   named a finding, prioritize it. Do not infer a fix from a vague historical
   note.
3. Recheck the cited evidence before mutation. If the evidence is stale,
   contradictory, unsafe to verify, or no longer reproducible, record that
   disposition instead of forcing a change.

Before each mutation, establish a **post-change evidence boundary** per
`get_reference_doc(kind="fix-verification")`: record the mutation start time,
canonical target identity, pre-change hash or version, and the latest relevant
pre-change run/artifact ids. Old artifacts are baseline only, never proof.

## Apply safely

- Apply operational correctness, stale-path, validation, current-run binding,
  artifact-wiring, report/eval truthfulness, and other semantics-preserving
  repairs directly when evidence is strong.
- Strategy, goal meaning, thresholds, rubric semantics, LLM/provider choices,
  recipients, destinations, credentials, and broad plan changes require the
  existing exact approved human-input request. A free-form or unrelated answer
  is not approval.
- Use normal direct plan/config/file/report/eval tools. Do not delegate mutation
  to another agent and do not run an externally side-effecting workflow merely
  to verify a repair.
- Run targeted side-effect-free validation after every change and accept it only
  under the `fix-verification` contract: verify the real runtime consumer reads
  the changed canonical store; a successful write alone is not proof.
- If verification requires an externally side-effecting run or the next
  scheduled producing run, do not trigger it merely to verify. Record
  `changed_unverified` with reason `awaiting_next_valid_run`, the exact next
  evidence boundary, and do not claim the finding is fixed.

## Close out

Update `builder/improve.html` once after all selected fixes. Preserve each
original finding and add `Resolved`, `Partially resolved - changed_unverified`,
`Blocked`, or `Invalid` with date, exact fix, verification, and remaining risk.
Do not call
`record_pulse_worklist`, `mark_pulse_module_result`, or final-command status
tools: this standalone command must not impersonate or complete an automatic
Pulse run.

Add one compact **Improvements / Kaizen** result card for the fixer batch using
`data-pulse-section="improvements"` and `data-module="pulse_fixer"`. Link it to
the original Signal finding anchors and state what verification passed. Do not
rewrite the read-only Signal cards as if those reviewers applied the fixes.

Finish with changes applied, verification performed, findings not changed and
why, approvals still needed, and the next real-run evidence required.
