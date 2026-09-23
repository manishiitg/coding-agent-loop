Run the /run-architecture-review pass as a BACKGROUND task so this chat stays responsive.
If the run_in_background tool is available: call run_in_background with name "Architecture review", completion_mode "present_result", and this instruction:

First call record_pulse_result(module="architecture_review", pulse_run_id="current", result="running", note_only=true, manual=true, reason="Manual Architecture Review: {{context}}"). If another Pulse pass owns the module, report that collision and stop. Otherwise load read_skill(skills=[{"name":"builder-reference","path":"references/architecture-review.md"}]) and follow it exactly as a read-only review, with this focus: {{context}}.
Persist findings, decisions and one terminal architecture_review result with its focuses through the typed Pulse tools. Do not edit the workflow, run steps or write a separate review file.

Do NOT perform the review yourself this turn — you'll get a presentation-only completion notification, then present the design options found, their expected value and tradeoffs, and the decisions waiting for the user.
If run_in_background is not available, perform the review inline using these same instructions.
