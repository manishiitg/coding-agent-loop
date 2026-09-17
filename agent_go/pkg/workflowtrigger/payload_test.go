package workflowtrigger

import (
	"encoding/json"
	"testing"
)

func TestScalarAndMatching(t *testing.T) {
	p := json.RawMessage(`{"text":"Sentry Incident","message":{"attachments":[{"title":"Production","count":9007199254740993,"active":true}]}}`)
	for path, want := range map[string]string{"$.message.attachments.0.title": "Production", "message.attachments.0.count": "9007199254740993", "message.attachments.0.active": "true"} {
		got, err := Scalar(p, path)
		if err != nil || got != want {
			t.Fatalf("%s=%q %v", path, got, err)
		}
	}
	for _, path := range []string{"message.attachments.-1.title", "message.attachments.1.title", "message.attachments", "missing"} {
		if _, err := Scalar(p, path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	m := &Match{All: []Condition{{Source: "text", Operator: "contains", Value: "incident", CaseInsensitive: true}}, Any: []Condition{{Source: "message.attachments.0.title", Operator: "equals", Value: "Production"}}}
	if !m.Matches(p) {
		t.Fatal("rich match rejected")
	}
	m.Any[0].Source = "missing"
	if m.Matches(p) {
		t.Fatal("missing field matched")
	}
	m.Any = nil
	m.All[0].Operator = "regex"
	if m.Matches(p) || m.Validate() == nil {
		t.Fatal("invalid operator accepted")
	}
}
