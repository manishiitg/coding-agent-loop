Consolidate the durable Pulse backlog with this focus: {{context}}.
Load get_pulse_state(view="backlog", detail="compact") exactly once first. Work only in the typed Pulse lifecycle: do not edit workflow artifacts, run steps, or create a Markdown report.
Request detail="full" only for the bounded issue_ids whose semantic identity is genuinely uncertain; never reload the complete backlog merely to filter it differently.
Group issues by semantic root cause, repair owner, and verification boundary—not wording, module, evidence path, or repeated symptom.
For each proven duplicate group, call merge_pulse_issues with one canonical PUL issue ID and the duplicate PUL IDs. Do not merge uncertain cases.
Then give a compact receipt: active count before and after, duplicates merged, distinct root causes retained, and any ambiguous groups left for a later review.
