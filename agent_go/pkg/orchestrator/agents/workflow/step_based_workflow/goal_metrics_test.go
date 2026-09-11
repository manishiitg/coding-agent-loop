package step_based_workflow

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func testGoalMetric() GoalMetric {
	return GoalMetric{ID: "followers", CriterionID: "audience", Name: "Followers", Role: "primary", Unit: "followers", Direction: "increase", Definition: "Observed total followers", Source: "daily_metrics.follower_count", Window: "instant", CollectionFrequency: "daily", FreshnessHours: 48}
}
func TestGoalMetricsSetupPreservesHistoryAndRejectsSemanticChanges(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	m := testGoalMetric()
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m}); err != nil {
		t.Fatal(err)
	}
	o := PulseGoalObservation{CriterionID: m.CriterionID, Metric: m.ID, Unit: m.Unit, RunID: "run-1", ObservedAt: "2026-09-11T00:00:00Z", Value: pulseImpactFloat(290), Evidence: []string{"daily_metrics:2026-09-11"}}
	for i := 0; i < 2; i++ {
		if _, err := RecordGoalObservations(ctx, ws, []PulseGoalObservation{o}); err != nil {
			t.Fatal(err)
		}
	}
	m.Target = pulseImpactFloat(1000)
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m}); err != nil {
		t.Fatal(err)
	}
	m.Unit = "percent"
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m}); err == nil {
		t.Fatal("meaning changed under same ID")
	}
	ledger, err := LoadPulseImpactLedger(ctx, ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Metrics) != 1 || ledger.Metrics[0].Unit != "followers" || len(ledger.Observations) != 1 {
		t.Fatalf("history/config lost: %+v", ledger)
	}
	o.Value = pulseImpactFloat(999)
	if _, err = RecordGoalObservations(ctx, ws, []PulseGoalObservation{o}); err == nil {
		t.Fatal("accepted conflicting historical value")
	}
}
func TestGoalMetricsRequirePrimaryAndComparableMeasurements(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	m := testGoalMetric()
	m.Role = "supporting"
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m}); err == nil {
		t.Fatal("no primary accepted")
	}
	m.Role = "primary"
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m}); err != nil {
		t.Fatal(err)
	}
	o := PulseGoalObservation{CriterionID: m.CriterionID, Metric: m.ID, Unit: "percent", RunID: "run-1", ObservedAt: "2026-09-11T00:00:00Z", Value: pulseImpactFloat(5), Evidence: []string{"source"}}
	if _, err := RecordGoalObservations(ctx, ws, []PulseGoalObservation{o}); err == nil {
		t.Fatal("unit mismatch accepted")
	}
	o.Unit = m.Unit
	o.Value = nil
	o.Status = "unavailable"
	ledger, err := RecordGoalObservations(ctx, ws, []PulseGoalObservation{o})
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Observations[0].Value != nil {
		t.Fatal("missing became zero")
	}
	snapshot := GoalMetricSnapshots(ledger, time.Now())[0]
	if snapshot.Value != nil || snapshot.State != "Measurement unavailable" {
		t.Fatalf("dishonest summary: %+v", snapshot)
	}
}
func TestGoalMetricSnapshotsLatestFailureAndStaleness(t *testing.T) {
	m := testGoalMetric()
	m.Target = pulseImpactFloat(100)
	ledger := &PulseImpactLedger{Metrics: []GoalMetric{m}, Observations: []PulseGoalObservation{
		{Metric: m.ID, CriterionID: m.CriterionID, Unit: m.Unit, Value: pulseImpactFloat(290), ObservedAt: "2026-09-09T00:00:00Z"},
		{Metric: m.ID, CriterionID: m.CriterionID, Unit: m.Unit, Status: "blocked", ObservedAt: "2026-09-10T00:00:00Z"},
	}}
	now, _ := time.Parse(time.RFC3339, "2026-09-11T12:00:00Z")
	p := GoalMetricSnapshots(ledger, now)[0]
	if p.Value != nil || p.State != "Measurement unavailable" {
		t.Fatal(p)
	}
	ledger.Observations = ledger.Observations[:1]
	p = GoalMetricSnapshots(ledger, now)[0]
	if p.State != "Measurement stale" {
		t.Fatal(p)
	}
}

func TestGoalHistoryDoesNotDisappearBehindUnrelatedPulseRows(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	m := testGoalMetric()
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m}); err != nil {
		t.Fatal(err)
	}
	good := PulseGoalObservation{Metric: m.ID, CriterionID: m.CriterionID, Unit: m.Unit, RunID: "goal-run", ObservedAt: "2026-09-01T00:00:00Z", Value: pulseImpactFloat(10), Evidence: []string{"source"}}
	if _, err := RecordGoalObservations(ctx, ws, []PulseGoalObservation{good}); err != nil {
		t.Fatal(err)
	}
	unrelated := []PulseGoalObservation{}
	for i := 0; i < 5; i++ {
		unrelated = append(unrelated, PulseGoalObservation{Metric: "other", CriterionID: "ops", RunID: fmt.Sprint(i), ObservedAt: "2026-09-10T00:00:00Z", Value: pulseImpactFloat(1)})
	}
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Observations: unrelated}); err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadPulseImpactLedger(ctx, ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(GoalMetricSnapshots(ledger, time.Now())) != 1 || GoalMetricSnapshots(ledger, time.Now())[0].Value == nil {
		t.Fatal("configured metric dropped by unrelated-row limit")
	}
	replacement := m
	replacement.ID = "followers-v2"
	replacement.Definition = "New sampling method"
	if err = ConfigureGoalMetrics(ctx, ws, []GoalMetric{replacement}); err != nil {
		t.Fatal(err)
	}
	ledger, err = LoadPulseImpactLedger(ctx, ws, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Metrics) != 1 || ledger.Metrics[0].ID != "followers-v2" || len(ledger.Observations) != 6 {
		t.Fatal("retirement erased history")
	}
}
