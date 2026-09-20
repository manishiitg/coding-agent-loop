import { buildAskAIMessage } from "../../utils/askAIMessage";

// Ask AI starter for the goals widget (shown on the Dashboard): Builder
// receives the guided setup flow; chat shows only the plain-words summary.
export const GOAL_SETUP_MESSAGE = buildAskAIMessage({
  view: 'Dashboard',
  summary: 'Help me set up the goals and numbers for this helper. Use what already exists, keep past results and limits, and only ask about what is still undecided.',
  instructions: 'Call get_workflow_command_guidance(kind="setup-goals") and follow the shared goal and measurement setup flow for this workflow. Use existing goals and data, preserve constraints and history, and ask only unresolved decisions.',
});
