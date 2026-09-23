package server

import (
	"strings"
	"testing"
)

func TestResolvePulseAutonomyDefaultsAndValidation(t *testing.T) {
	cases := []struct {
		in   *WorkflowPulseAutonomy
		want PulseAutonomyLevels
	}{
		{nil, PulseAutonomyLevels{Run: "auto", Outward: "ask", Change: "ask"}},
		{&WorkflowPulseAutonomy{}, PulseAutonomyLevels{Run: "auto", Outward: "ask", Change: "ask"}},
		{&WorkflowPulseAutonomy{Run: "ask"}, PulseAutonomyLevels{Run: "ask", Outward: "ask", Change: "ask"}},
		{&WorkflowPulseAutonomy{Outward: " AUTO ", Change: "auto"}, PulseAutonomyLevels{Run: "auto", Outward: "auto", Change: "auto"}},
	}
	for _, c := range cases {
		got, err := resolvePulseAutonomy(c.in)
		if err != nil || got != c.want {
			t.Errorf("resolvePulseAutonomy(%+v) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	for _, bad := range []*WorkflowPulseAutonomy{{Run: "always"}, {Outward: "yes"}, {Change: "sometimes"}} {
		if _, err := resolvePulseAutonomy(bad); err == nil {
			t.Errorf("resolvePulseAutonomy(%+v) should reject an unknown level", bad)
		}
	}
	if _, err := resolvePulseAutonomy(&WorkflowPulseAutonomy{Change: "yes"}); err == nil || !strings.Contains(err.Error(), "pulse.autonomy.change") {
		t.Errorf("error should name the field, got %v", err)
	}
}
