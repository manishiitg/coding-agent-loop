// Dispatched on window to refresh the selected report and its document catalog.
// Lives in its own module so the workflow store can raise it without
// importing the view component.
export const WORKFLOW_REPORT_REFRESH_EVENT = 'workflow-report-refresh-requested'
