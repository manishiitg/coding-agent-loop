package services

import (
	"strings"
	"testing"
)

func TestFormatWhatsAppSlugChoicesSortsConfiguredRoutes(t *testing.T) {
	message := formatWhatsAppSlugChoices(WhatsAppRouting{
		"testing":    {WorkflowID: "wf-testing"},
		"confida":    {WorkflowID: "wf-confida"},
		"Confida-QA": {WorkflowID: "wf-confida-qa"},
	})

	wantOrder := []string{"1. @confida", "2. @confida-qa", "3. @testing"}
	last := -1
	for _, want := range wantOrder {
		at := strings.Index(message, want)
		if at < 0 {
			t.Fatalf("choice message %q does not contain %q", message, want)
		}
		if at <= last {
			t.Fatalf("choice message %q does not keep sorted order %v", message, wantOrder)
		}
		last = at
	}
	if !strings.Contains(message, "until you send @off") {
		t.Fatalf("choice message %q does not explain route memory", message)
	}
}

func TestFormatWhatsAppSlugChoicesExplainsEmptyRouting(t *testing.T) {
	message := formatWhatsAppSlugChoices(nil)
	if !strings.Contains(message, "No WhatsApp workflows are configured") {
		t.Fatalf("empty-route message = %q", message)
	}
}
