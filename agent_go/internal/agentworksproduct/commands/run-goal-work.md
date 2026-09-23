Run the /run-goal-work pass as a BACKGROUND task so this chat stays responsive.
If the run_in_background tool is available: call run_in_background with name "Goal Work pass", completion_mode "present_result", and this instruction:

Call get_workflow_command_guidance(kind="strategy-auditor", focus="{{context}}") and follow the returned instructions verbatim. Pass run_folder as the workflow's currently selected run folder when one is selected; otherwise omit it.
If this session is read-only (run mode), return findings in chat only; do not write or edit any workspace file.
Otherwise, do the Goal Work within this workflow's Pulse autonomy (workflow.json pulse.autonomy): write prepared work under pulse/work/<YYYY-MM-DD>/, and run steps, act outward or edit the workflow only where that level is auto; everything else becomes a decision. Never edit soul.md. Record every item and decision through the typed Pulse tools; do not write a separate review file.
Treat focus as the request context, including recent user constraints.

Do NOT do the pass yourself this turn — you'll get a presentation-only completion notification, then present what Pulse did for the user, why it should move the goal, what needs their decision, and any rule it is challenging. Do not call tools, reload state, or independently revalidate after that notification.
If run_in_background is not available, do the pass inline using these same instructions.
