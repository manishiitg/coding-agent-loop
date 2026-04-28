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
	var transitionedToEvaluating []string
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
				ComputeVerdict(e, cfg, metricsFile)
				e.Status = ExpStatusEvaluating
				transitionedToEvaluating = append(transitionedToEvaluating, e.ID)
			}
		}
	}

	if mutated {
		if err := WriteActiveFile(ctx, workspacePath, file); err != nil {
			return err
		}
		// Auto-spawn the evaluator for any experiment that just transitioned
		// to evaluating. Spawned AFTER WriteActiveFile so the evaluator reads
		// the persisted state (verdict + status). Each spawn is async; failure
		// is logged inside the goroutine and does not block the loop.
		for _, expID := range transitionedToEvaluating {
			SpawnEvaluatorAgent(ctx, workspacePath, expID)
		}
	}
	return nil
}

// ComputeVerdict applies the heuristic rules from the experiments config to
// produce a verdict + evidence on the experiment record. Mutates rec in-place.
//
// metricsFile is consulted only to classify each target metric as outcome
// (linked_success_criteria non-empty) or telemetry (empty). The classifier
// makes the verdict combiner asymmetric: outcome metrics dominate, telemetry
// regressions only trigger a cost_warning on an otherwise-kept experiment.
// metricsFile may be nil — in that case every metric is treated as outcome
// (legacy behavior, conservative).
func ComputeVerdict(rec *ExperimentRecord, cfg ExperimentsConfig, metricsFile *MetricsFile) {
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
		PerMetricVerdict:  map[string]Verdict{},
	}

	entries := make([]metricEntry, 0, len(rec.TargetMetrics))
	for _, mid := range rec.TargetMetrics {
		isOutcome := metricIsOutcome(metricsFile, mid)

		baselineMean, hasBaseline := rec.Baseline.Mean[mid]
		baselineStd := rec.Baseline.Std[mid]
		samples := rec.Measurement.Values[mid]
		if len(samples) == 0 {
			entries = append(entries, metricEntry{id: mid, verdict: VerdictInconclusive, isOutcome: isOutcome})
			evidence.PerMetricVerdict[mid] = VerdictInconclusive
			continue
		}
		postMean := mean(samples)
		evidence.PostMean[mid] = postMean
		if !hasBaseline {
			entries = append(entries, metricEntry{id: mid, verdict: VerdictInconclusive, isOutcome: isOutcome})
			evidence.PerMetricVerdict[mid] = VerdictInconclusive
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
		var v Verdict
		switch {
		case directionOK && mag >= thr.KeptMagnitudePct*rec.ExpectedMagnitude && beatPct >= thr.KeptPerRunBeatPct:
			v = VerdictKept
		case (!directionOK || beatPct <= thr.RevertedPerRunBeatPct) && mag > thr.NoiseBandStdMultiplier*baselineStd:
			v = VerdictReverted
		case mag <= thr.NoiseBandStdMultiplier*baselineStd:
			v = VerdictInconclusive
		default:
			v = VerdictInconclusive
		}
		entries = append(entries, metricEntry{id: mid, verdict: v, isOutcome: isOutcome})
		evidence.PerMetricVerdict[mid] = v
	}

	final, costWarning := combineVerdictsWeighted(entries)
	if costWarning {
		evidence.CostWarning = true
	}

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

// metricEntry pairs a target metric with its computed verdict and the
// outcome/telemetry classification used by the weighted combiner.
type metricEntry struct {
	id        string
	verdict   Verdict
	isOutcome bool
}

// metricIsOutcome reports whether a target metric is an outcome metric
// (linked to at least one success_criteria entry) or a telemetry metric
// (cost_per_run, run_duration_seconds, or any other unanchored metric).
// metricsFile==nil yields the conservative answer: treat as outcome.
func metricIsOutcome(metricsFile *MetricsFile, metricID string) bool {
	if metricsFile == nil {
		return true
	}
	m := FindMetric(metricsFile, metricID)
	if m == nil {
		return true
	}
	return len(m.LinkedSuccessCriteria) > 0
}

// combineVerdictsWeighted resolves per-metric verdicts into one overall
// verdict, applying the rule that success_criteria are paramount: outcome
// metrics dominate, telemetry metrics can only flag a cost_warning on an
// otherwise-kept experiment.
//
// Resolution order:
//
//  1. If any OUTCOME metric is reverted → reverted (the change hurt a
//     user-facing criterion; back it out regardless of cost wins).
//  2. If at least one OUTCOME metric is kept and the rest are kept or
//     inconclusive → kept. Set cost_warning iff any TELEMETRY metric is
//     reverted (the operator should see "outcome held but cost regressed",
//     not have the change auto-reverted).
//  3. If no OUTCOME metric is decisive AND a TELEMETRY metric is reverted
//     → reverted (no outcome signal supports the change, and at least one
//     guardrail tripped).
//  4. Otherwise → inconclusive.
//
// If there are no outcome metrics at all, the experiment is purely
// telemetry — fall back to the legacy combine: any reverted → reverted,
// all kept → kept, else inconclusive.
//
// Returns (overall verdict, cost_warning).
func combineVerdictsWeighted(entries []metricEntry) (Verdict, bool) {
	if len(entries) == 0 {
		return VerdictInconclusive, false
	}
	hasOutcome := false
	for _, e := range entries {
		if e.isOutcome {
			hasOutcome = true
			break
		}
	}
	if !hasOutcome {
		return combineLegacy(verdictsOf(entries)), false
	}

	outcomeReverted := false
	outcomeKept := false
	outcomeAnyDecisive := false
	telemetryReverted := false

	for _, e := range entries {
		if e.isOutcome {
			switch e.verdict {
			case VerdictReverted:
				outcomeReverted = true
				outcomeAnyDecisive = true
			case VerdictKept:
				outcomeKept = true
				outcomeAnyDecisive = true
			}
		} else if e.verdict == VerdictReverted {
			telemetryReverted = true
		}
	}

	switch {
	case outcomeReverted:
		return VerdictReverted, false
	case outcomeKept:
		return VerdictKept, telemetryReverted
	case !outcomeAnyDecisive && telemetryReverted:
		return VerdictReverted, false
	default:
		return VerdictInconclusive, false
	}
}

func verdictsOf(entries []metricEntry) []Verdict {
	out := make([]Verdict, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.verdict)
	}
	return out
}

// combineLegacy is the original conservative AND. Used when there are no
// outcome metrics in the experiment (purely telemetry experiments).
func combineLegacy(vs []Verdict) Verdict {
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

// combineVerdicts kept as a thin wrapper for callers that don't have
// per-metric outcome classification handy. Applies the legacy conservative
// AND. New code should prefer combineVerdictsWeighted.
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
