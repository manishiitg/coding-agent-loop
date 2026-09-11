package virtualtools

import (
	"context"
	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"strings"
	"testing"
)

func TestPulseNotificationGoalProgressLeadsRichEmail(t *testing.T) {
	ctx := WithGoalProgressProvider(context.Background(), func(_ context.Context, ws string) ([]services.NotificationSummarySection, error) {
		if ws != "Workflow/example" {
			t.Fatal(ws)
		}
		return []services.NotificationSummarySection{{Heading: "Goal progress", Body: "Followers: 290 < 1,000"}}, nil
	})
	summary := &services.NotificationSummary{Sections: []services.NotificationSummarySection{{Heading: "Goal progress", Body: "invented"}, {Heading: "Reviews", Body: "One review"}}}
	gmail := &services.GmailContent{HTMLBody: "<p>Review details</p>"}
	addGoalProgressToNotification(ctx, "pulse_summary", &services.NotificationDestination{WorkspacePath: "Workflow/example"}, summary, gmail)
	if len(summary.Sections) != 2 || summary.Sections[0].Body != "Followers: 290 < 1,000" {
		t.Fatalf("bad sections: %+v", summary.Sections)
	}
	if !strings.Contains(gmail.HTMLBody, "290 &lt; 1,000") || strings.Contains(gmail.HTMLBody, "invented") || strings.Index(gmail.HTMLBody, "Goal progress") > strings.Index(gmail.HTMLBody, "Review details") {
		t.Fatal(gmail.HTMLBody)
	}
}
