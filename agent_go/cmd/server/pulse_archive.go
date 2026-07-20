package server

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	pulseImproveArchiveRetentionDays = 15
)

type pulseImproveArchiveAssessment struct {
	Due               bool
	Bytes             int
	Lines             int
	TimelineEntries   int
	ArchivableEntries int
	ExpiredEntries    int
	RecentRunRows     int
}

func assessPulseImproveArchive(content string) pulseImproveArchiveAssessment {
	return assessPulseImproveArchiveAt(content, time.Now())
}

func assessPulseImproveArchiveAt(content string, now time.Time) pulseImproveArchiveAssessment {
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
			assessment.Due = assessment.ExpiredEntries > 0
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
					if pulseArchiveTokenIsOlderThan(token, now, pulseImproveArchiveRetentionDays) {
						assessment.ExpiredEntries++
					}
				}
			}
			if htmlTokenHasClass(token, "run") {
				assessment.RecentRunRows++
				if pulseArchiveTokenIsOlderThan(token, now, pulseImproveArchiveRetentionDays) {
					assessment.ExpiredEntries++
				}
			}
		}
	}
}

func pulseArchiveTokenIsOlderThan(token html.Token, now time.Time, retentionDays int) bool {
	dateValue := ""
	for _, attr := range token.Attr {
		if strings.EqualFold(attr.Key, "data-date") {
			dateValue = strings.TrimSpace(attr.Val)
			break
		}
	}
	if dateValue == "" {
		return false
	}
	location := now.Location()
	entryDate, err := time.ParseInLocation("2006-01-02", dateValue, location)
	if err != nil {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	cutoff := today.AddDate(0, 0, -retentionDays)
	return entryDate.Before(cutoff)
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
	return fmt.Sprintf("%d eligible dated items older than %d days", assessment.ExpiredEntries, pulseImproveArchiveRetentionDays)
}

func postRunMonitorArchiveStep(assessment pulseImproveArchiveAssessment) postRunMonitorStep {
	return postRunMonitorStep{
		label: "archive-improve-log",
		query: fmt.Sprintf("PULSE ARCHIVE PREFLIGHT. builder/improve.html crossed its retention window (%s; recent run rows=%d). Load get_reference_doc(kind=\"pulse-archive\") and follow it exactly. Archive only safe resolved history, validate staged complete HTML before replacement, preserve all active/open state, then stop.",
			assessment.triggerSummary(), assessment.RecentRunRows),
	}
}
