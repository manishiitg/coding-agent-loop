Run the /review-artifact-drift review as a BACKGROUND task so this chat stays responsive.
If the run_in_background tool is available: call run_in_background with name "review-artifact-drift review", completion_mode "present_result", and this instruction:

Call get_workflow_command_guidance(kind="review-artifact-drift", focus="{{context}}") and follow the returned instructions verbatim.
If this session is read-only (run mode), return findings in chat only; do not write or edit any workspace file.
Otherwise, follow the Plan Drift authority in the returned guidance: Part 1 may apply bounded safe compatibility and prompt repairs; Part 2 remains read-only. Persist typed review and repair outcomes; do not write a separate review file.
Treat focus as the request context, including recent user constraints. Apply conditional checks only when relevant to the selected investigation.

Do NOT perform the review yourself this turn — you'll get a presentation-only completion notification, then present the selected repair objective, changes made, immediate checks and their limits, lifecycle outcomes, and remaining actionable issues. Do not call tools, reload state, or independently revalidate after that notification.
If run_in_background is not available, perform the review inline using these same instructions.
