package step_based_workflow

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Effects are additive and immutable within an intervention. A caller using an
// older schema cannot erase a cost/security effect by omitting it on an update.
func recordPulseMetricEffects(ctx context.Context, tx *sql.Tx, intervention PulseIntervention) error {
	var previousMetric, previousCriterion, previousDirection string
	err := tx.QueryRowContext(ctx, "SELECT metric, criterion_id, expected_direction FROM pulse_interventions WHERE intervention_id=?", intervention.InterventionID).Scan(&previousMetric, &previousCriterion, &previousDirection)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && (previousMetric != intervention.Metric || previousCriterion != intervention.CriterionID || previousDirection != intervention.ExpectedDirection) {
		return fmt.Errorf("intervention measurement meaning changed; use a new intervention_id")
	}
	if len(intervention.Effects) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, goalMetricsSchema); err != nil {
		return err
	}
	seen := map[string]bool{intervention.Metric: true}
	for _, effect := range intervention.Effects {
		if effect.Metric == "" || strings.TrimSpace(effect.Metric) != effect.Metric || seen[effect.Metric] || !pulseValueAllowed(effect.ExpectedDirection, pulseExpectedDirectionValues) {
			return fmt.Errorf("effects require unique additional metric IDs and valid expected_direction")
		}
		seen[effect.Metric] = true
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_goal_metrics WHERE metric_id=? AND active=1", effect.Metric).Scan(&active); err != nil {
			return err
		}
		if active != 1 {
			return fmt.Errorf("effect references unknown active metric %q", effect.Metric)
		}
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT expected_direction FROM pulse_intervention_effects WHERE intervention_id=? AND metric=?", intervention.InterventionID, effect.Metric).Scan(&previous)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && previous != effect.ExpectedDirection {
			return fmt.Errorf("effect %q meaning changed; use a new intervention_id", effect.Metric)
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO pulse_intervention_effects(intervention_id,metric,expected_direction) VALUES(?,?,?)", intervention.InterventionID, effect.Metric, effect.ExpectedDirection); err != nil {
			return err
		}
	}
	return nil
}

func requirePulseMetricAssessments(ctx context.Context, tx *sql.Tx, intervention PulseIntervention) error {
	rows, err := tx.QueryContext(ctx, "SELECT metric FROM pulse_intervention_effects WHERE intervention_id=?", intervention.InterventionID)
	if err != nil {
		return err
	}
	metrics := []string{}
	for rows.Next() {
		var metric string
		if err := rows.Scan(&metric); err != nil {
			rows.Close()
			return err
		}
		metrics = append(metrics, metric)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Preserve legacy single-metric lifecycle behavior.
	if len(metrics) == 0 {
		return nil
	}
	metrics = append(metrics, intervention.Metric)
	for _, metric := range metrics {
		var verdict string
		err := tx.QueryRowContext(ctx, `SELECT verdict FROM pulse_impact_assessments
   WHERE intervention_id=? AND (metric=? OR (metric='' AND ?=?))
   ORDER BY julianday(assessed_at) DESC, rowid DESC LIMIT 1`, intervention.InterventionID, metric, metric, intervention.Metric).Scan(&verdict)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows || (verdict != "improved" && verdict != "unchanged") {
			return fmt.Errorf("adoption requires an improved or unchanged assessment for every affected metric; %q is missing, regressed or inconclusive", metric)
		}
	}
	return nil
}
