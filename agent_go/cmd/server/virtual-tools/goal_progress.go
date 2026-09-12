package virtualtools

import (
	"context"
	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"html"
	"strings"
)

type goalProgressProviderKey struct{}
type GoalProgressProvider func(context.Context, string) ([]services.NotificationSummarySection, error)

func WithGoalProgressProvider(ctx context.Context, provider GoalProgressProvider) context.Context {
	return context.WithValue(ctx, goalProgressProviderKey{}, provider)
}

// Typed platform facts lead the email even when the agent supplies custom HTML.
// The provider reads only the trusted notification destination's workspace.
func addGoalProgressToNotification(ctx context.Context, kind string, dest *services.NotificationDestination, summary *services.NotificationSummary, gmail *services.GmailContent) {
	if (kind != "pulse_summary" && kind != "run_summary") || dest == nil || dest.WorkspacePath == "" {
		return
	}
	provider, ok := ctx.Value(goalProgressProviderKey{}).(GoalProgressProvider)
	if !ok {
		return
	}
	sections, err := provider(ctx, dest.WorkspacePath)
	if err != nil {
		sections = []services.NotificationSummarySection{{Heading: "Goal progress", Body: "Measurements could not be loaded. Progress is unavailable."}}
	}
	if len(sections) == 0 {
		return
	}
	generatedHeadings := map[string]bool{}
	for _, section := range sections {
		generatedHeadings[section.Heading] = true
	}
	remaining := []services.NotificationSummarySection{}
	for _, section := range summary.Sections {
		if !generatedHeadings[section.Heading] && section.Heading != "Goal progress" && section.Heading != "Supporting metrics" && !strings.HasPrefix(section.Heading, "Goal progress: ") {
			remaining = append(remaining, section)
		}
	}
	summary.Sections = append(sections, remaining...)
	if gmail != nil && strings.TrimSpace(gmail.HTMLBody) != "" {
		var b strings.Builder
		for _, section := range sections {
			b.WriteString("<section style=\"padding:16px;border-bottom:1px solid #ddd\"><h2>" + html.EscapeString(section.Heading) + "</h2><p>" + strings.ReplaceAll(html.EscapeString(section.Body), "\n", "<br>") + "</p></section>")
		}
		// Respect full HTML documents as well as fragments.
		lower := strings.ToLower(gmail.HTMLBody)
		if bodyStart := strings.Index(lower, "<body"); bodyStart >= 0 {
			if close := strings.Index(lower[bodyStart:], ">"); close >= 0 {
				at := bodyStart + close + 1
				gmail.HTMLBody = gmail.HTMLBody[:at] + b.String() + gmail.HTMLBody[at:]
			} else {
				gmail.HTMLBody = b.String() + gmail.HTMLBody
			}
		} else {
			gmail.HTMLBody = b.String() + gmail.HTMLBody
		}
	}
}
