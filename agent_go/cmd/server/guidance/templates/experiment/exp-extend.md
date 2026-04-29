Extend the active experiment's measurement window. Use shell + file primitives — no API call.{{if .Focus}}

Focus / why: {{.Focus}}{{end}}

DISCOVERY
1. Read experiments/active.json. List active experiments. If multiple, ask the user which one.
2. Ask the user how many additional runs to add (default = workflow's default_measurement_runs from planning/experiments/config.json).

ACTION
3. Use `diff_patch_workspace_file` on experiments/active.json:
   - Find the experiment's record.
   - Bump `measurement.target_runs` by the additional run count.
   - If `status` is "evaluating", flip it back to "measuring" (the verdict computer will re-fire when target_runs is hit again).
   - Re-marshal the JSON cleanly.
4. Append one line to builder/decisions.jsonl:
   {"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"user","trigger":"exp-extend","linked_experiment_id":"<id>","rationale":"extend by <N> runs: <user reason>"}

REPORT
- New target_runs.
- Status after the edit (back to "measuring" if it was "evaluating").
- Decisions entry id.
