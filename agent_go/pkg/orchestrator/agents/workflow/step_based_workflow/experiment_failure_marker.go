package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// =====================================================================
// experiment_failure_marker.go — count failed workflow runs toward each
// active experiment's measurement.completed_runs so a broken intervention
// doesn't hang an experiment forever.
//
// Called inline from the failure path of controller_batch_execution.go.
// Reads experiments/active.json directly (no hooks, no callbacks) and
// bumps completed_runs without appending to measurement.values (a failed
// run produced no metric values to append). The verdict-on-success-path
// will pick up the elevated counter on the next successful run and
// likely come back "inconclusive" if too many runs failed.
//
// Types are duplicated minimally from cmd/server's auto_improvement_types
// to avoid an import cycle. The on-disk JSON shape is the contract.
// =====================================================================

type activeExperimentsForMarker struct {
	Experiments []experimentForMarker `json:"experiments"`
}

type experimentForMarker struct {
	ID          string                 `json:"id"`
	Status      string                 `json:"status"`
	Measurement measurementForMarker   `json:"measurement"`
	// Pass-through for everything else so we round-trip cleanly without
	// dropping fields we don't model here.
	Extra map[string]json.RawMessage `json:"-"`
}

type measurementForMarker struct {
	TargetRuns      int                  `json:"target_runs"`
	CompletedRuns   int                  `json:"completed_runs"`
	Values          map[string][]float64 `json:"values"`
	ContributedRuns []string             `json:"contributed_runs"`
}

// markExperimentRunFailure increments completed_runs on every active
// "measuring" experiment that hasn't already counted this runFolder. Best
// effort — errors are logged via the orchestrator's logger and swallowed
// so a failure here cannot fail an already-failing run.
func (hcpo *StepBasedWorkflowOrchestrator) markExperimentRunFailure(ctx context.Context, runFolder string) {
	runFolder = strings.Trim(strings.TrimSpace(runFolder), "/")
	if runFolder == "" {
		return
	}

	activePath := "experiments/active.json"
	raw, err := hcpo.ReadWorkspaceFile(ctx, activePath)
	if err != nil || strings.TrimSpace(raw) == "" {
		return // no active experiments file → no-op
	}

	// Round-trip the entire object as a generic map so unknown fields are
	// preserved. We mutate only the known measurement counters.
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[EXP_FAIL_MARK] parse %s failed: %v", activePath, err))
		return
	}

	expsRaw, _ := doc["experiments"].([]interface{})
	if len(expsRaw) == 0 {
		return
	}

	mutated := false
	for i, raw := range expsRaw {
		exp, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if status, _ := exp["status"].(string); status != "measuring" {
			continue
		}
		measurement, _ := exp["measurement"].(map[string]interface{})
		if measurement == nil {
			continue
		}

		// Dedupe — skip if this runFolder already contributed.
		contributed, _ := measurement["contributed_runs"].([]interface{})
		alreadyCounted := false
		for _, c := range contributed {
			if s, _ := c.(string); s == runFolder {
				alreadyCounted = true
				break
			}
		}
		if alreadyCounted {
			continue
		}

		completed, _ := measurement["completed_runs"].(float64)
		measurement["completed_runs"] = completed + 1
		measurement["contributed_runs"] = append(contributed, runFolder)
		exp["measurement"] = measurement
		expsRaw[i] = exp
		mutated = true
	}
	if !mutated {
		return
	}
	doc["experiments"] = expsRaw

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[EXP_FAIL_MARK] marshal failed: %v", err))
		return
	}
	if err := hcpo.WriteWorkspaceFile(ctx, activePath, string(body)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[EXP_FAIL_MARK] write %s failed: %v", activePath, err))
		return
	}
	hcpo.GetLogger().Info(fmt.Sprintf("[EXP_FAIL_MARK] counted failed run %s toward active experiments", runFolder))
}

// Suppress "declared but not used" warnings if the structured types above
// end up unused (the implementation walks the doc as map[string]interface{}
// for safe round-tripping). They document the wire contract.
var _ = activeExperimentsForMarker{}
var _ = experimentForMarker{}
var _ = measurementForMarker{}
