package step_based_workflow

import (
	"strings"
	"testing"
)

func TestParseRouteSelectionPayloadAcceptsCanonicalAndLegacyFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "canonical select_route",
			content: `{"select_route":"search"}`,
			want:    "search",
		},
		{
			name:    "legacy route_id",
			content: `{"route_id":"route-search"}`,
			want:    "route-search",
		},
		{
			name:    "selected_route_id compatibility",
			content: `{"selected_route_id":"route-save"}`,
			want:    "route-save",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRouteSelectionPayload(tt.content)
			if err != nil {
				t.Fatalf("parseRouteSelectionPayload returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseRouteSelectionPayload = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRouteSelectionPayloadRejectsMissingSelection(t *testing.T) {
	_, err := parseRouteSelectionPayload(`{"mode":"search"}`)
	if err == nil {
		t.Fatal("expected error for payload without route selection field")
	}
	if !strings.Contains(err.Error(), "select_route") {
		t.Fatalf("expected error to mention accepted fields, got %v", err)
	}
}

func TestResolveRouteSelectionValueMatchesRouteID(t *testing.T) {
	routes := deterministicRoutingTestRoutes()

	got, err := resolveRouteSelectionValue(routes, "route-search")
	if err != nil {
		t.Fatalf("resolveRouteSelectionValue returned error: %v", err)
	}
	if got != "route-search" {
		t.Fatalf("resolveRouteSelectionValue = %q, want route-search", got)
	}
}

func TestResolveRouteSelectionValueMatchesUniqueNextStepID(t *testing.T) {
	routes := deterministicRoutingTestRoutes()

	got, err := resolveRouteSelectionValue(routes, "step-save")
	if err != nil {
		t.Fatalf("resolveRouteSelectionValue returned error: %v", err)
	}
	if got != "route-save" {
		t.Fatalf("resolveRouteSelectionValue = %q, want route-save", got)
	}
}

func TestResolveRouteSelectionValueRejectsAmbiguousNextStepID(t *testing.T) {
	routes := []RoutingRoute{
		{RouteID: "route-a", NextStepID: "step-shared"},
		{RouteID: "route-b", NextStepID: "step-shared"},
	}

	_, err := resolveRouteSelectionValue(routes, "step-shared")
	if err == nil {
		t.Fatal("expected ambiguous next_step_id error")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestResolveRouteSelectionValueRejectsUnknownValue(t *testing.T) {
	routes := deterministicRoutingTestRoutes()

	_, err := resolveRouteSelectionValue(routes, "step-unknown")
	if err == nil {
		t.Fatal("expected unknown route selection error")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func deterministicRoutingTestRoutes() []RoutingRoute {
	return []RoutingRoute{
		{RouteID: "route-search", RouteName: "Search", NextStepID: "step-search"},
		{RouteID: "route-save", RouteName: "Save", NextStepID: "step-save"},
	}
}
