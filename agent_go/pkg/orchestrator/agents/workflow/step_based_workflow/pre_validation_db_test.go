package step_based_workflow

import (
	"context"
	"testing"
)

func intp(n int) *int { return &n }

func passed(checks []JSONCheckResult) (pass, fail int) {
	for _, c := range checks {
		if c.Passed {
			pass++
		} else {
			fail++
		}
	}
	return
}

func TestEvaluateDBRule(t *testing.T) {
	ctx := context.Background()
	rows3 := []map[string]interface{}{
		{"status": "PASS"}, {"status": "PASS"}, {"status": "PASS"},
	}

	// min_rows satisfied
	if _, fail := passed(evaluateDBRule(ctx, DBValidationRule{MinRows: intp(1)}, rows3)); fail != 0 {
		t.Errorf("min_rows>=1 on 3 rows should pass, got %d failures", fail)
	}
	// min_rows violated
	if _, fail := passed(evaluateDBRule(ctx, DBValidationRule{MinRows: intp(5)}, rows3)); fail != 1 {
		t.Errorf("min_rows>=5 on 3 rows should fail once, got %d", fail)
	}
	// max_rows violated (e.g. "no failures" with rows present)
	if _, fail := passed(evaluateDBRule(ctx, DBValidationRule{MaxRows: intp(0)}, rows3)); fail != 1 {
		t.Errorf("max_rows<=0 on 3 rows should fail once, got %d", fail)
	}
	// max_rows satisfied on empty result
	if _, fail := passed(evaluateDBRule(ctx, DBValidationRule{MaxRows: intp(0)}, nil)); fail != 0 {
		t.Errorf("max_rows<=0 on 0 rows should pass, got %d failures", fail)
	}

	// first-row check passes
	rule := DBValidationRule{Checks: []JSONValidationCheck{{Path: "$.status", MustExist: true, Pattern: "^PASS$"}}}
	if _, fail := passed(evaluateDBRule(ctx, rule, rows3)); fail != 0 {
		t.Errorf("first-row status==PASS should pass, got %d failures", fail)
	}
	// first-row check fails on wrong value
	badRows := []map[string]interface{}{{"status": "FAIL"}}
	if _, fail := passed(evaluateDBRule(ctx, rule, badRows)); fail != 1 {
		t.Errorf("first-row status==PASS should fail on FAIL, got %d", fail)
	}
	// row checks but no rows -> a failure (can't evaluate)
	if _, fail := passed(evaluateDBRule(ctx, rule, nil)); fail != 1 {
		t.Errorf("row checks with 0 rows should fail once, got %d", fail)
	}
}

func TestApplyCheckedToSummaryDBFailureFailsOverall(t *testing.T) {
	result := &WorkspaceVerificationResult{OverallPass: true}
	fr := validateDBRuleResultStub("db: no failures", false)
	applyCheckedToSummary(result, fr)
	if result.OverallPass {
		t.Error("a failed db check should flip OverallPass to false")
	}
	if result.Summary.FailedChecks != 1 || len(result.Summary.Errors) != 1 {
		t.Errorf("expected 1 failed check + 1 error, got failed=%d errors=%d", result.Summary.FailedChecks, len(result.Summary.Errors))
	}
}

// validateDBRuleResultStub builds a FileCheckResult with one pass/fail check.
func validateDBRuleResultStub(name string, pass bool) FileCheckResult {
	return FileCheckResult{FileName: name, Exists: true, IsJSON: true, JSONChecks: []JSONCheckResult{
		{CheckType: "max_rows", Passed: pass, Expected: "<= 0 row(s)", Actual: "2 row(s)", ErrorMsg: "expected <= 0 row(s), got 2 row(s)"},
	}}
}
