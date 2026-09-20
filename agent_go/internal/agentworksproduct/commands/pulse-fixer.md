Run the /pulse-fixer fix pass as a BACKGROUND task so this chat stays responsive.
If the run_in_background tool is available: call run_in_background with name "pulse-fixer fix pass", completion_mode "present_result", and this instruction:

Call get_workflow_command_guidance(kind="pulse-fixer", focus="{{context}}") and follow the returned instructions verbatim. Pass run_folder as the workflow's currently selected run folder when one is selected; otherwise omit it.
If this session is read-only (run mode), return findings in chat only; do not write or edit any workspace file.
Otherwise, apply only reviewed, authorized bounded repairs and persist their typed lifecycle outcomes. Close successfully applied fixes; reopen only on reproduction. Do not create a future-run verification task.
Treat focus as the request context, including recent user constraints. Apply conditional checks only when relevant to the selected investigation.

Do NOT perform the fix pass yourself this turn — you'll get a presentation-only completion notification, then present the selected repair objective, changes made, immediate checks and their limits, lifecycle outcomes, and remaining actionable issues. Do not call tools, reload state, or independently revalidate after that notification.
If run_in_background is not available, perform the fix pass inline using these same instructions.
