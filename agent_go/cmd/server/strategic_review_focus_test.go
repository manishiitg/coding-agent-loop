package server

import (
	"context"
	"strings"
	"testing"
)

func TestStrategicReviewCustomFocusSurvivesAgendaAndDeferral(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ctx := context.Background()
	ws := "Workflow/strategy-advisor"
	saved, err := recordPulseReviewFocus(ctx, ws, "review-1", pulseModuleStrategicReview,
		"recommendation_usefulness", "latency", "new_or_changed",
		"Investigate whether developer recommendations are actionable", "opportunity to test", "", "",
		[]string{"Reports contain repeated recommendations; usefulness remains untested"}, nil, []string{"recipient_experience"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.FocusKey != "recommendation_usefulness" {
		t.Fatalf("unexpected saved focus: %+v", saved)
	}
	agenda, err := getPulseReviewFocusAgenda(ctx, ws, pulseModuleStrategicReview, "latency", 50)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, deferred := false, false
	for _, f := range agenda {
		if f.FocusKey == "recommendation_usefulness" {
			reviewed = f.ReviewCount == 1 && f.RouteReviewCount == 1
		}
		if f.FocusKey == "recipient_experience" {
			deferred = f.ReviewCount == 0 && f.LastReviewedAt == ""
		}
	}
	if !reviewed || !deferred {
		t.Fatalf("custom coverage lost or deferred lens marked reviewed: %+v", agenda)
	}
}

func TestStrategicReviewFocusValidationKeepsTechnicalCatalogClosed(t *testing.T) {
	for _, key := range []string{"", "Needs a decision", "bad/path", "_leading", "trailing_", "double__underscore", strings.Repeat("a", 65)} {
		if validPulseReviewFocusKey(pulseModuleStrategicReview, key) {
			t.Errorf("accepted invalid key %q", key)
		}
	}
	if !validPulseReviewFocusKey(pulseModuleStrategicReview, "recommendation_usefulness") {
		t.Fatal("rejected new strategic lens")
	}
	if validPulseReviewFocusKey(pulseModuleTechnicalReview, "recommendation_usefulness") {
		t.Fatal("technical catalog unexpectedly opened")
	}
	if !validPulseReviewFocusKey(pulseModuleTechnicalReview, "execution_health") {
		t.Fatal("existing technical lens rejected")
	}
}
