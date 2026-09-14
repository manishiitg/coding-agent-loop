package guidance

import (
	"strings"
	"testing"
)

func TestPulseRolesUnderstandScheduleCoordinationPolicy(t *testing.T) {
	wants := map[string][]string{
		"pulse-gate": {
			"do not infer safety or throughput from cron spacing",
			"workflow-wide active-execution lock",
			"`after_schedule_ids`",
			"same local calendar date",
			"Architecture Review",
			"Technical Review",
		},
		"architecture-review": {
			"Schedule topology and throughput",
			"workflow-wide active-execution lock",
			"two schedules work together through a directional chain",
			"fan-in",
			"this review remains read-only",
		},
		"technical-review": {
			"load `references/schedules.md`",
			"Preserve the workflow-wide single-active-execution safety lock",
			"directional all-of edges",
			"typed schedule tools",
		},
		"pulse-fixer-practices": {
			"Cron spacing is not a concurrency guarantee",
			"directional `after_schedule_ids` edges",
			"never introduce a cycle",
		},
	}

	for kind, phrases := range wants {
		rendered, err := renderFromRegistry(kind, tmplData{}, referenceKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, phrase := range phrases {
			if !containsNormalizedText(rendered, phrase) {
				t.Errorf("%s missing schedule coordination contract %q", kind, phrase)
			}
		}
	}
}

func TestPulseArchitectureReferenceAdvertisesScheduleTopology(t *testing.T) {
	description := referenceKinds["architecture-review"].Description
	for _, want := range []string{"schedule dependency", "queue", "runtime topology"} {
		if !strings.Contains(description, want) {
			t.Errorf("architecture-review discovery description missing %q", want)
		}
	}
}
