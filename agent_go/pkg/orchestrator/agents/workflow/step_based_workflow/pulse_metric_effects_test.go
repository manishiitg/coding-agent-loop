package step_based_workflow

import (
	"context"
	"testing"
)

func TestMultiMetricEffectsPreserveTradeoffsAndRejectPrematureAdoption(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	lead := testGoalMetric()
	cost := testGoalMetric()
	cost.ID = "cost"
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{lead, cost}); err != nil {
		t.Fatal(err)
	}
	item := PulseIntervention{InterventionID: "change", Title: "Improve responsiveness", CriterionID: lead.CriterionID, Metric: lead.ID, ExpectedDirection: "increase", ImpactType: "direct_goal", Kind: "strategy_experiment", Status: "proposed", BaselineWindow: "before", Checkpoint: "after", Guardrails: []string{"cost"}, RollbackCondition: "cost regression", Effects: []PulseMetricEffect{{Metric: cost.ID, ExpectedDirection: "maintain"}}}
	ledger, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Interventions[0].Effects) != 1 {
		t.Fatal("effect not returned")
	}
	item.Effects = nil // old-client lifecycle update must not erase the cost effect
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}}); err != nil {
		t.Fatal(err)
	}
	assessment := PulseImpactAssessment{InterventionID: item.InterventionID, Metric: lead.ID, Verdict: "improved", BeforeWindow: "before", AfterWindow: "after", Confidence: "medium", AssessedAt: "2026-09-12T00:00:00Z"}
	costAssessment := assessment
	costAssessment.Metric = cost.ID
	costAssessment.Verdict = "regressed"
	ledger, err = RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Assessments: []PulseImpactAssessment{assessment, costAssessment}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Assessments) != 2 || ledger.Assessments[0].AssessmentID == ledger.Assessments[1].AssessmentID {
		t.Fatal("metric assessments collided")
	}
	item.Status = "adopted"
	item.TerminalOutcome = "Latency improved"
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}}); err == nil {
		t.Fatal("adopted despite cost regression")
	}
	costAssessment.Verdict = "unchanged"
	costAssessment.AssessedAt = "2026-09-13T00:00:00Z"
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Assessments: []PulseImpactAssessment{costAssessment}}); err != nil {
		t.Fatal(err)
	}
	ledger, err = RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Interventions[0].Effects) != 1 {
		t.Fatal("old-client update lost cost effect")
	}
	costAssessment.Metric = "unknown"
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Assessments: []PulseImpactAssessment{costAssessment}}); err == nil {
		t.Fatal("unrelated assessment accepted")
	}
}

func TestMultiMetricEffectsMissingAssessmentAndInvalidReferences(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	m := testGoalMetric()
	other := m
	other.ID = "cost"
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{m, other}); err != nil {
		t.Fatal(err)
	}
	item := PulseIntervention{InterventionID: "change", Title: "Change", CriterionID: m.CriterionID, Metric: m.ID, ExpectedDirection: "increase", ImpactType: "direct_goal", Effects: []PulseMetricEffect{{Metric: "unknown", ExpectedDirection: "maintain"}}}
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}}); err == nil {
		t.Fatal("unknown effect accepted")
	}
	item.Effects[0].Metric = other.ID
	ledger, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Interventions) != 1 {
		t.Fatal("failed transaction leaked intervention")
	}
	db, err := openRunConcernsDB(ctx, ws, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := requirePulseMetricAssessments(ctx, tx, item); err == nil {
		t.Fatal("missing assessments accepted")
	}
	tx.Rollback()
	item.Effects[0].ExpectedDirection = "decrease"
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}}); err == nil {
		t.Fatal("effect meaning changed")
	}
}

func TestLegacyAssessmentMigrationAndRetry(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	item := PulseIntervention{InterventionID: "old", Title: "Legacy", CriterionID: "goal", Metric: "latency", ExpectedDirection: "decrease", ImpactType: "direct_goal"}
	a := PulseImpactAssessment{InterventionID: item.InterventionID, Verdict: "improved", BeforeWindow: "before", AfterWindow: "after", Confidence: "medium", AssessedAt: "2026-09-12T00:00:00Z"}
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}, Assessments: []PulseImpactAssessment{a}}); err != nil {
		t.Fatal(err)
	}
	db, err := openRunConcernsDB(ctx, ws, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE pulse_impact_assessments DROP COLUMN metric"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	ledger, err := LoadPulseImpactLedger(ctx, ws, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Assessments[0].Metric != item.Metric {
		t.Fatal("legacy assessment lost its lead metric")
	}
	ledger, err = RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Assessments: []PulseImpactAssessment{a}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Assessments) != 1 {
		t.Fatal("migration duplicated legacy retry")
	}
}

func TestMultiMetricFixBundleWaitsForEveryEffect(t *testing.T) {
	ctx := context.Background()
	ws := concernsWorkspace(t)
	lead := testGoalMetric()
	cost := lead
	cost.ID = "cost"
	if err := ConfigureGoalMetrics(ctx, ws, []GoalMetric{lead, cost}); err != nil {
		t.Fatal(err)
	}
	item := PulseIntervention{InterventionID: "fix", Title: "Fix", CriterionID: lead.CriterionID, Metric: lead.ID, ExpectedDirection: "increase", ImpactType: "direct_goal", Effects: []PulseMetricEffect{{Metric: cost.ID, ExpectedDirection: "maintain"}}}
	a := PulseImpactAssessment{AssessmentID: "lead-assessment", InterventionID: item.InterventionID, Metric: lead.ID, Verdict: "improved", BeforeWindow: "before", AfterWindow: "after", Confidence: "medium", AssessedAt: "2026-09-12T00:00:00Z"}
	ledger, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Interventions: []PulseIntervention{item}, Assessments: []PulseImpactAssessment{a}})
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Interventions[0].Status != "measuring" {
		t.Fatal("partial bundle marked assessed")
	}
	a.Metric = cost.ID
	if _, err := RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Assessments: []PulseImpactAssessment{a}}); err == nil {
		t.Fatal("assessment ID collision across metrics accepted")
	}
	a.AssessmentID = "cost-assessment"
	a.Verdict = "regressed"
	ledger, err = RecordPulseImpactUpdate(ctx, ws, PulseImpactUpdate{Assessments: []PulseImpactAssessment{a}})
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Interventions[0].Status != "assessed" || len(ledger.Assessments) != 2 {
		t.Fatal("complete assessment missing", ledger)
	}
}
