package server

import "testing"

func TestParseRouteSelectionsArg(t *testing.T) {
	got, err := parseRouteSelectionsArg(map[string]interface{}{
		" route-by-mode ": " search ",
	})
	if err != nil {
		t.Fatalf("parseRouteSelectionsArg returned error: %v", err)
	}
	if got["route-by-mode"] != "search" {
		t.Fatalf("route selection was not trimmed: %#v", got)
	}
}

func TestParseRouteSelectionsArgRejectsNonStringValue(t *testing.T) {
	_, err := parseRouteSelectionsArg(map[string]interface{}{
		"route-by-mode": 12,
	})
	if err == nil {
		t.Fatal("expected error for non-string route selection value")
	}
}
