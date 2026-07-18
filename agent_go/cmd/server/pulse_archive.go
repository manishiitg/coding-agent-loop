package server

import (
	"context"
	"fmt"
	"path"
	"strings"

	"golang.org/x/net/html"
)

const (
	pulseImproveArchiveMaxTimelineEntries = 20
)

type pulseImproveArchiveAssessment struct {
	Due               bool
	Bytes             int
	Lines             int
	TimelineEntries   int
	ArchivableEntries int
	RecentRunRows     int
}

func assessPulseImproveArchive(content string) pulseImproveArchiveAssessment {
	assessment := pulseImproveArchiveAssessment{Bytes: len(content)}
	if content == "" {
		return assessment
	}
	assessment.Lines = strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		assessment.Lines++
	}

	tokenizer := html.NewTokenizer(strings.NewReader(content))
	handoffDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			assessment.Due = assessment.TimelineEntries > pulseImproveArchiveMaxTimelineEntries &&
				assessment.ArchivableEntries > 0
			return assessment
		case html.EndTagToken:
			if handoffDepth > 0 {
				handoffDepth--
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if handoffDepth > 0 {
				if token.Type == html.StartTagToken {
					handoffDepth++
				}
				continue
			}
			if htmlTokenHasID(token, "pulse-agent-handoff") {
				if token.Type == html.StartTagToken {
					handoffDepth = 1
				}
				continue
			}
			if htmlTokenHasClass(token, "entry") {
				assessment.TimelineEntries++
				if pulseArchiveEntryIsResolved(token) {
					assessment.ArchivableEntries++
				}
			}
			if htmlTokenHasClass(token, "run") {
				assessment.RecentRunRows++
			}
		}
	}
}

func htmlTokenHasID(token html.Token, want string) bool {
	for _, attr := range token.Attr {
		if strings.EqualFold(attr.Key, "id") && strings.EqualFold(strings.TrimSpace(attr.Val), want) {
			return true
		}
	}
	return false
}

func pulseArchiveEntryIsResolved(token html.Token) bool {
	for _, className := range []string{"decision", "resolved", "closed", "superseded", "artifact", "monitor", "note", "agent"} {
		if htmlTokenHasClass(token, className) {
			return true
		}
	}
	for _, attr := range token.Attr {
		if attr.Key != "data-status" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(attr.Val)) {
		case "done", "changed", "resolved", "closed", "superseded", "dismissed", "consumed":
			return true
		}
	}
	return false
}

func htmlTokenHasClass(token html.Token, want string) bool {
	for _, attr := range token.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, className := range strings.Fields(attr.Val) {
			if className == want {
				return true
			}
		}
	}
	return false
}

func pulseImproveArchiveAssessmentForWorkspace(ctx context.Context, workspacePath string) (pulseImproveArchiveAssessment, error) {
	workspacePath = strings.Trim(strings.TrimSpace(workspacePath), "/")
	if workspacePath == "" {
		return pulseImproveArchiveAssessment{}, nil
	}
	content, exists, err := readFileFromWorkspace(ctx, path.Join(workspacePath, "builder/improve.html"))
	if err != nil {
		return pulseImproveArchiveAssessment{}, err
	}
	if !exists {
		return pulseImproveArchiveAssessment{}, nil
	}
	return assessPulseImproveArchive(content), nil
}

func (assessment pulseImproveArchiveAssessment) triggerSummary() string {
	return fmt.Sprintf("%d timeline entries > %d entries", assessment.TimelineEntries, pulseImproveArchiveMaxTimelineEntries)
}

func postRunMonitorArchiveStep(assessment pulseImproveArchiveAssessment) postRunMonitorStep {
	return postRunMonitorStep{
		label: "archive-improve-log",
		query: fmt.Sprintf("PULSE PREFLIGHT — ARCHIVE IMPROVE HISTORY. builder/improve.html is over its active-log limit (%s; recent run rows=%d). Do only this archive task, then stop.\n\n"+
			"Call get_reference_doc(kind=\"review-improve-log\") and follow its Keep the active file small contract. builder/improve.html remains the authoritative current Pulse view. Preserve its complete top dashboard, current metrics and freshness labels, all open findings, user rules, current notes, unresolved or unconfirmed decisions, unanswered or not-yet-consumed human questions, the newest 20 timeline cards, and enough recent-run rows for current comparison (at least the newest 5).\n\n"+
			"Move only older resolved findings, superseded confirmed decisions, and routine old run rows into self-contained monthly HTML files at builder/improve-archive/YYYY-MM.html. If a monthly file already exists, merge without duplicates and keep newest first. Each archive must be a complete renderable HTML document, not a fragment. Add or update one compact Archive Index link in builder/improve.html using href=\"improve-archive/YYYY-MM.html\", with its date range and moved-item count.\n\n"+
			"Make this safe: stage new archive and active documents in temporary files under builder/, verify they are non-empty HTML with html/head/body, verify every moved card/run appears exactly once across active plus archive, and only then replace the final files. Never truncate or overwrite the original active file before the archive copies validate. If there are no entries that are safe to archive, leave builder/improve.html unchanged and say so. Do not change workflow logic, verdicts, plans, module cadence, reports, or any non-Pulse artifact in this step.",
			assessment.triggerSummary(), assessment.RecentRunRows),
	}
}
