# STANDALONE PULSE BUG REVIEW

Run the same read-only QA and logic-bug review used by Pulse, without running
Pulse Gate and without applying fixes.{{if .Focus}}

Focus especially on: {{.Focus}}.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary run folder.{{end}}

1. Load `get_reference_doc(kind="post-run-monitor")`,
   `get_reference_doc(kind="assumption-audit")`, and
   `get_reference_doc(kind="review-improve-log")`. Use the `bug_review`,
   Exploratory QA, observable execution-trace, and control-path reachability
   sections as the audit contract.
2. Choose the latest meaningful retained run when no run folder was supplied.
   Read its compact step results first. Deep-read only suspect attempts and the
   artifacts needed to prove or reject a defect.
3. Launch exactly one generic reviewer with a prompt beginning
   `READ-ONLY REVIEW`. It may inspect artifacts, copied fixtures, scratch DBs,
   and side-effect-free tests. It must not edit workflow files, send external
   messages, publish, trade, post, mutate production data, ask the user, or
   launch another agent.
4. Require: behavioral contract, QA coverage, expected versus observed,
   findings classified as `correctness_bug`, `efficiency_or_coaching`,
   `no_issue`, or `insufficient_evidence`, exact evidence, bounded recommended
   fix, verification, confidence, and untested risk.
5. Validate and deduplicate the result against `builder/improve.html`. As the
   parent, append one compact newest-first Bug Review entry with
   `data-pulse-section="signals" data-module="bug_review"` to that HTML. Do not
   modify the workflow, close findings, call Pulse module-state tools, or claim
   that a recommendation was fixed.

Finish with a short executive summary followed by all confirmed bugs in severity
order, what was tested, what remains untested, and which findings are ready for
`/pulse-fixer`. Do not truncate the findings to a Top 3.
