package workspace

import (
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/livefeed"
)

// Dashboards and their data change only through these tools (file writes
// under db/reports/ and the workflow-database tools), never by hand. Every
// successful write here tells open Report views to refresh via the live feed
// (GET /api/live), so a dashboard updates within ~1s of its HTML or data
// changing. See docs/design/live_update_feed.md.

func isReportFilePath(p string) bool {
	return strings.Contains("/"+strings.Trim(strings.TrimSpace(p), "/"), "/db/reports/")
}

// noteReportFileWrite publishes a report notice when p is a dashboard file.
func noteReportFileWrite(paths ...string) {
	for _, p := range paths {
		if isReportFilePath(p) {
			livefeed.PublishWorkflow(livefeed.Report, p)
		}
	}
}

// noteWorkflowDBWrite publishes a report notice for the workflow owning dbPath.
func noteWorkflowDBWrite(dbPath string) {
	livefeed.PublishWorkflow(livefeed.Report, dbPath)
}
