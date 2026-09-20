Run the /strategy-auditor review as a BACKGROUND task so this chat stays responsive.
If the run_in_background tool is available: call run_in_background with name "strategy-auditor review", completion_mode "present_result", and this instruction:

Call get_workflow_command_guidance(kind="strategy-auditor", focus="{{context}}") and follow the returned instructions verbatim. Pass run_folder as the workflow's currently selected run folder when one is selected; otherwise omit it.
If this session is read-only (run mode), return findings in chat only; do not write or edit any workspace file.
Otherwise, persist findings, recommendations, and decisions through the typed Pulse tools required by the returned guidance; do not modify implementation files or write a separate review file.
Treat focus as the request context, including recent user constraints. Apply conditional checks only when relevant to the selected investigation.

Do NOT perform the review yourself this turn — you'll get a presentation-only completion notification, then present useful strategic insights and Needs your decision proposals, their expected value and tradeoffs, and evidence versus hypotheses. Do not call tools, reload state, or independently revalidate after that notification.
If run_in_background is not available, perform the review inline using these same instructions.
