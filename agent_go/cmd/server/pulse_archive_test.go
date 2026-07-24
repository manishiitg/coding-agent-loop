package server

import (
	"os"
	"strings"
	"testing"
	"time"
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
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	content := `<!doctype html><html><head><style>.entry{display:block}</style></head><body>` +
		`<script>const fake = '<div class="entry" data-date="2020-01-01">';</script>` +
		`<div class="decision entry major" data-date="2026-07-05"></div>` +
		`<div class="decision entry major" data-date="2026-07-06"></div>` +
		`<div class="entry open" data-date="2020-01-01"></div>` +
		`<div class="entry resolved"></div>` +
		`</body></html>`

	assessment := assessPulseImproveArchiveAt(content, now)
	if !assessment.Due {
		t.Fatalf("resolved card older than 15 days should require archive: %+v", assessment)
	}
	if assessment.TimelineEntries != 4 || assessment.ArchivableEntries != 3 || assessment.ExpiredEntries != 1 {
		t.Fatalf("unexpected age-based assessment: %+v", assessment)
	}
}

func TestAssessPulseImproveArchiveUsesStrictFifteenDayBoundary(t *testing.T) {
	now := time.Date(2026, 7, 21, 23, 59, 0, 0, time.UTC)
	content := `<article class="entry resolved" data-date="2026-07-06"></article>` +
		`<div class="run" data-date="invalid"></div>` +
		`<div class="run"></div>`
	assessment := assessPulseImproveArchiveAt(content, now)
	if assessment.Due || assessment.ExpiredEntries != 0 {
		t.Fatalf("items exactly 15 days old, invalid-dated, or undated must stay active: %+v", assessment)
	}

	assessment = assessPulseImproveArchiveAt(`<div class="run" data-date="2026-07-05"></div>`, now)
	if !assessment.Due || assessment.ExpiredEntries != 1 {
		t.Fatalf("routine run older than 15 days should require archive: %+v", assessment)
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
		strings.Repeat(`<article class="entry decision" data-date="2020-01-01"></article>`, 25) +
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
		Due:            true,
		ExpiredEntries: 3,
		RecentRunRows:  12,
	})
	if step.label != "archive-improve-log" {
		t.Fatalf("archive step label = %q", step.label)
	}
	if !strings.Contains(step.query, `get_reference_doc(kind="pulse-archive")`) {
		t.Fatalf("archive step must load the focused archive contract:\n%s", step.query)
	}
	raw, err := os.ReadFile("guidance/templates/system/pulse-archive.md")
	if err != nil {
		t.Fatalf("read pulse archive guidance: %v", err)
	}
	contract := string(raw)
	for _, want := range []string{
		"authoritative current Pulse view",
		"latest 15 calendar days",
		"strictly older than 15 calendar days",
		"undated history is never",
		"builder/improve-archive/YYYY-MM.html",
		"complete renderable HTML document",
		"temporary files",
		"appears exactly once",
		`href="improve-archive/YYYY-MM.html"`,
		"Never truncate",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("pulse archive contract missing %q:\n%s", want, contract)
		}
	}
	if len(step.query) > 500 {
		t.Fatalf("archive scheduler prompt is not compact: %d bytes", len(step.query))
	}
}
