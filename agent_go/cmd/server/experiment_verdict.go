package server

import (
	"context"
	"fmt"
)

// =====================================================================
// experiment_verdict.go — heuristic verdict computer + run-completion hook.
//
// Verdict is system-computed (deterministic), not LLM-judged. The evaluator
// LLM only narrates the verdict via conclude_experiment.
// =====================================================================

// RecordMeasurement is the run-completion hook. Called after each workflow
// run finishes. For every active "measuring" experiment, resolves each
// target metric's value for this run and appends to measurement.values.
// When completed_runs reaches target_runs, transitions status to
// "evaluating" and computes the verdict (which the evaluator then narrates).
func RecordMeasurement(ctx context.Context, workspacePath, runFolder string) error {
	file, exists, err := ReadActiveFile(ctx, workspacePath)
	if err != nil {
		return err
	}
	if !exists || file == nil || len(file.Experiments) == 0 {
		return nil
	}
	metricsFile, _, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return err
	}
	if metricsFile == nil {
		return nil
	}

	cfg, err := ReadExperimentsConfig(ctx, workspacePath)
	if err != nil {
		return err
	}

	mutated := false
	for i := range file.Experiments {
		e := &file.Experiments[i]
		if e.Status != ExpStatusMeasuring {
			continue
		}
		// Dedupe: skip if this run folder has already contributed to this
		// experiment. Keeps RecordMeasurement idempotent under re-eval scoring.
		alreadyContributed := false
		for _, contributed := range e.Measurement.ContributedRuns {
			if contributed == runFolder {
				alreadyContributed = true
				break
			}
		}
		if alreadyContributed {
			continue
		}

		if e.Measurement.Values == nil {
			e.Measurement.Values = map[string][]float64{}
		}
		appendedAny := false
		allResolved := true
		for _, mid := range e.TargetMetrics {
			m := FindMetric(metricsFile, mid)
			if m == nil {
				continue
			}
			// Delayed sources: enqueue rather than block. v1 simply skips them
			// here; the pending-evaluation queue lands in a follow-up.
			if m.EvaluableAtLag != "" || m.Source.Type == MetricSourceDelayedGroundTruth {
				allResolved = false
				continue
			}
			v, ok, err := ResolveMetricValue(ctx, workspacePath, runFolder, m)
			if err != nil {
				continue
			}
			if !ok {
				allResolved = false
				continue
			}
			e.Measurement.Values[mid] = append(e.Measurement.Values[mid], v)
			appendedAny = true
		}
		_ = allResolved // reserved for the pending-eval queue logic later

		// Only record this run's contribution and bump the counter if at least
		// one metric resolved. Runs where every metric was delayed/unavailable
		// don't count toward target_runs.
		if appendedAny {
			e.Measurement.CompletedRuns++
			e.Measurement.ContributedRuns = append(e.Measurement.ContributedRuns, runFolder)
			mutated = true

			// Verdict trigger: fire EXACTLY when crossing the threshold, not
			// every subsequent run. Without this guard, an experiment that
			// gets extended past target_runs (or naturally accrues more runs
			// while in "measuring") would re-compute the verdict on every
			// subsequent run.
			if e.Measurement.CompletedRuns == e.Measurement.TargetRuns && e.Measurement.TargetRuns > 0 {
				ComputeVerdict(e, cfg)
				e.Status = ExpStatusEvaluating
			}
		}
	}

	if mutated {
		return WriteActiveFile(ctx, workspacePath, file)
	}
	return nil
}

// ComputeVerdict applies the heuristic rules from the experiments config to
// produce a verdict + evidence on the experiment record. Mutates rec in-place.
func ComputeVerdict(rec *ExperimentRecord, cfg ExperimentsConfig) {
	if rec == nil {
		return
	}
	thr := cfg.VerdictThresholds
	if thr == nil {
		def := DefaultExperimentsConfig()
		thr = def.VerdictThresholds
	}

	evidence := &ExperimentEvidence{
		PostMean:          map[string]float64{},
		MagnitudeObserved: map[string]float64{},
		PerRunBeatPct:     map[string]float64{},
	}

	verdicts := make([]Verdict, 0, len(rec.TargetMetrics))
	for _, mid := range rec.TargetMetrics {
		baselineMean, hasBaseline := rec.Baseline.Mean[mid]
		baselineStd := rec.Baseline.Std[mid]
		samples := rec.Measurement.Values[mid]
		if len(samples) == 0 {
			verdicts = append(verdicts, VerdictInconclusive)
			continue
		}
		postMean := mean(samples)
		evidence.PostMean[mid] = postMean
		if !hasBaseline {
			verdicts = append(verdicts, VerdictInconclusive)
			continue
		}
		signed := postMean - baselineMean
		mag := signed
		if mag < 0 {
			mag = -mag
		}
		evidence.MagnitudeObserved[mid] = mag

		// per-run beat: count how many samples beat baselineMean in the
		// expected direction.
		beats := 0
		for _, v := range samples {
			if isBeatingBaseline(v, baselineMean, rec.ExpectedDirection) {
				beats++
			}
		}
		beatPct := float64(beats) / float64(len(samples))
		evidence.PerRunBeatPct[mid] = beatPct

		// Direction check.
		directionOK := directionMatches(signed, rec.ExpectedDirection)

		// Apply heuristics.
		switch {
		case directionOK && mag >= thr.KeptMagnitudePct*rec.ExpectedMagnitude && beatPct >= thr.KeptPerRunBeatPct:
			verdicts = append(verdicts, VerdictKept)
		case (!directionOK || beatPct <= thr.RevertedPerRunBeatPct) && mag > thr.NoiseBandStdMultiplier*baselineStd:
			verdicts = append(verdicts, VerdictReverted)
		case mag <= thr.NoiseBandStdMultiplier*baselineStd:
			verdicts = append(verdicts, VerdictInconclusive)
		default:
			verdicts = append(verdicts, VerdictInconclusive)
		}
	}

	final := combineVerdicts(verdicts)

	// Drift flag (placeholder; real drift detection requires concluded_at
	// snapshot to be filled before this runs).
	if rec.WorldState.ConcludedAt == nil {
		rec.WorldState.ConcludedAt = &WorldStateSnapshot{CapturedAt: nowUTC()}
	}
	if rec.WorldState.StartedAt != nil && rec.WorldState.ConcludedAt != nil {
		if hasMaterialDrift(rec.WorldState.StartedAt, rec.WorldState.ConcludedAt) {
			evidence.DriftFlagged = true
		}
	}

	rec.Conclusion = &ExperimentConclusion{
		Verdict:  final,
		Evidence: evidence,
	}
}

// isBeatingBaseline returns true if the sample is "better" than baseline given
// the expected direction (decrease = sample < baseline, increase = >, maintain
// = within noise — treated here as not strictly beating).
func isBeatingBaseline(sample, baseline float64, dir ExpectedDirection) bool {
	switch dir {
	case DirectionIncrease:
		return sample > baseline
	case DirectionDecrease:
		return sample < baseline
	case DirectionMaintain:
		return false
	}
	return false
}

func directionMatches(signed float64, dir ExpectedDirection) bool {
	switch dir {
	case DirectionIncrease:
		return signed > 0
	case DirectionDecrease:
		return signed < 0
	case DirectionMaintain:
		return signed == 0
	}
	return false
}

// combineVerdicts applies a conservative AND across per-metric verdicts:
//   - all kept     -> kept
//   - any reverted -> reverted
//   - else         -> inconclusive
func combineVerdicts(vs []Verdict) Verdict {
	if len(vs) == 0 {
		return VerdictInconclusive
	}
	allKept := true
	for _, v := range vs {
		if v == VerdictReverted {
			return VerdictReverted
		}
		if v != VerdictKept {
			allKept = false
		}
	}
	if allKept {
		return VerdictKept
	}
	return VerdictInconclusive
}

// hasMaterialDrift compares two world snapshots and returns true if model
// versions, MCP versions, or source-data hashes differ in any way. v1 is
// permissive; richer drift accounting can land later.
func hasMaterialDrift(a, b *WorldStateSnapshot) bool {
	if a == nil || b == nil {
		return false
	}
	if !mapsEqual(a.ModelVersions, b.ModelVersions) {
		return true
	}
	if !mapsEqual(a.MCPVersions, b.MCPVersions) {
		return true
	}
	if !mapsEqual(a.SourceDataHashes, b.SourceDataHashes) {
		return true
	}
	return false
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// ConfigForVerdict is a small helper for callers that already have a config in
// hand and want to invoke ComputeVerdict against an existing record.
func ConfigForVerdict(cfg *ExperimentsConfig) (*VerdictThresholds, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if cfg.VerdictThresholds == nil {
		def := DefaultExperimentsConfig()
		return def.VerdictThresholds, nil
	}
	return cfg.VerdictThresholds, nil
}
