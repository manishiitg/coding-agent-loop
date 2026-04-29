Read planning/plan.json and analyze the context flow between steps.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Check for:
1. **Broken chain** — step depends on a context_output that no earlier step produces
2. **Orphaned outputs** — step produces context_output that no later step consumes
3. **Circular dependencies** — A depends on B depends on A
4. **Implicit dependencies** — step description references data from another step but context_dependencies doesn't list it
5. **Type mismatches** — upstream produces a JSON file but downstream expects CSV, or field names don't align
6. **Missing validation** — steps that produce context_output but have no validation_schema

Show me:
- A dependency graph: step-a (produces X) → step-b (consumes X, produces Y) → step-c (consumes Y)
- Any issues found with severity (CRITICAL / WARNING / INFO)
- Suggested fixes for each issue

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what was reviewed, the main findings ordered by severity, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.
