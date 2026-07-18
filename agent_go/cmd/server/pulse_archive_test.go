package server

import (
	"strings"
	"testing"
)

func TestAssessPulseImproveArchiveSmallDocumentIsNotDue(t *testing.T) {
	assessment := assessPulseImproveArchive(`<!doctype html><html><body><div class="entry open"></div><div class="run"></div></body></html>`)
	if assessment.Due {
		t.Fatalf("small document should not require archive: %+v", assessment)
	}
	if assessment.TimelineEntries != 1 || assessment.RecentRunRows != 1 {
		t.Fatalf("unexpected structured counts: %+v", assessment)
	}
}

func TestAssessPulseImproveArchiveUsesStructuredTimelineCards(t *testing.T) {
	var content strings.Builder
	content.WriteString(`<!doctype html><html><head><style>.entry{display:block}</style></head><body>`)
	content.WriteString(`<script>const fake = '<div class="entry">';</script>`)
	for i := 0; i < pulseImproveArchiveMaxTimelineEntries+1; i++ {
		content.WriteString(`<div class="decision entry major"></div>`)
	}
	content.WriteString(`</body></html>`)

	assessment := assessPulseImproveArchive(content.String())
	if !assessment.Due {
		t.Fatalf("document with %d real timeline cards should require archive: %+v", pulseImproveArchiveMaxTimelineEntries+1, assessment)
	}
	if assessment.TimelineEntries != pulseImproveArchiveMaxTimelineEntries+1 {
		t.Fatalf("timeline entries = %d, want %d", assessment.TimelineEntries, pulseImproveArchiveMaxTimelineEntries+1)
	}
}

func TestAssessPulseImproveArchiveIgnoresByteAndLineCounts(t *testing.T) {
	large := assessPulseImproveArchive(`<html><body><article class="entry resolved"></article>` + strings.Repeat("x", 128*1024) + "</body></html>")
	if large.Due {
		t.Fatalf("byte count alone must not require archive: %+v", large)
	}

	manyLines := assessPulseImproveArchive(`<article class="entry resolved"></article>` + "\n" + strings.Repeat("line\n", 1600))
	if manyLines.Due {
		t.Fatalf("line count alone must not require archive: %+v", manyLines)
	}
}

func TestAssessPulseImproveArchiveIgnoresAgentHandoffAndNeedsCandidates(t *testing.T) {
	content := `<html><body><section id = 'pulse-agent-handoff'>` +
		strings.Repeat(`<article class="entry decision"></article>`, pulseImproveArchiveMaxTimelineEntries+5) +
		`</section>` + strings.Repeat("x", 128*1024) + `</body></html>`
	assessment := assessPulseImproveArchive(content)
	if assessment.TimelineEntries != 0 || assessment.ArchivableEntries != 0 {
		t.Fatalf("handoff entries leaked into archive counts: %+v", assessment)
	}
	if assessment.Due {
		t.Fatalf("large document with no safe archive candidate should not loop forever: %+v", assessment)
	}
}

func TestAssessPulseImproveArchiveCountsTrailingNewlineCorrectly(t *testing.T) {
	assessment := assessPulseImproveArchive("first\nsecond\n")
	if assessment.Lines != 2 {
		t.Fatalf("lines = %d, want 2", assessment.Lines)
	}
}

func TestPostRunMonitorArchiveStepPreservesCurrentTruthAndStagesWrites(t *testing.T) {
	step := postRunMonitorArchiveStep(pulseImproveArchiveAssessment{
		Due:             true,
		TimelineEntries: pulseImproveArchiveMaxTimelineEntries + 1,
		RecentRunRows:   12,
	})
	if step.label != "archive-improve-log" {
		t.Fatalf("archive step label = %q", step.label)
	}
	for _, want := range []string{
		"authoritative current Pulse view",
		"newest 20 timeline cards",
		"at least the newest 5",
		"builder/improve-archive/YYYY-MM.html",
		"complete renderable HTML document",
		"temporary files",
		"appears exactly once",
		`href="improve-archive/YYYY-MM.html"`,
		"Never truncate",
	} {
		if !strings.Contains(step.query, want) {
			t.Fatalf("archive step missing %q:\n%s", want, step.query)
		}
	}
}
