package step_based_workflow

import (
	"encoding/json"
	"testing"
)

func TestBuildStepOutputContentWrapsFlatJSON(t *testing.T) {
	out := buildStepOutputContent("evaluation/runs/iteration-0/default/execution/eval-step/output_content.json", `{"score":7,"follows_today_count":17}`)
	if out == nil {
		t.Fatal("expected output content")
	}
	if out.FilePath != "evaluation/runs/iteration-0/default/execution/eval-step/output_content.json" {
		t.Fatalf("unexpected file path: %q", out.FilePath)
	}
	if !out.IsJSON {
		t.Fatal("expected JSON output")
	}
	content, ok := out.Content.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map content, got %T", out.Content)
	}
	if content["score"] != float64(7) {
		t.Fatalf("expected score 7, got %#v", content["score"])
	}
	if content["follows_today_count"] != float64(17) {
		t.Fatalf("expected follows_today_count 17, got %#v", content["follows_today_count"])
	}
}

func TestBuildStepOutputContentUnwrapsLegacyEnvelope(t *testing.T) {
	out := buildStepOutputContent("output_content.json", `{"file_path":"old","is_json":true,"content":{"score":3,"verified_count":2}}`)
	if out == nil {
		t.Fatal("expected output content")
	}
	if !out.IsJSON {
		t.Fatal("expected JSON output")
	}
	content, ok := out.Content.(map[string]interface{})
	if !ok {
		t.Fatalf("expected unwrapped map content, got %T", out.Content)
	}
	if _, nested := content["content"]; nested {
		t.Fatalf("expected legacy envelope to be unwrapped, got %#v", content)
	}
	if content["verified_count"] != float64(2) {
		t.Fatalf("expected verified_count 2, got %#v", content["verified_count"])
	}
}

func TestBuildStepOutputContentKeepsTextOutput(t *testing.T) {
	out := buildStepOutputContent("output.txt", "plain result")
	if out == nil {
		t.Fatal("expected output content")
	}
	if out.IsJSON {
		t.Fatal("expected non-JSON output")
	}
	if out.Content != "plain result" {
		t.Fatalf("unexpected content: %#v", out.Content)
	}
}

func TestEvaluationOutputContentCandidatesPreferDeclaredEvalArtifacts(t *testing.T) {
	step := &EvaluationStep{
		ID: "eval-variety-coverage",
		PreValidation: &ValidationSchema{Files: []FileValidationRule{{
			FileName:  "eval_result.json",
			MustExist: true,
		}}},
	}
	candidates := evaluationOutputContentCandidates("evaluation/runs/iteration-0/test-run/execution", "eval-variety-coverage", step)

	expected := []string{
		"evaluation/runs/iteration-0/test-run/execution/eval-variety-coverage/output_content.json",
		"evaluation/runs/iteration-0/test-run/execution/eval-variety-coverage/context_output.json",
		"evaluation/runs/iteration-0/test-run/execution/eval-variety-coverage/eval_result.json",
	}
	if len(candidates) != len(expected) {
		t.Fatalf("expected %d candidates, got %d: %#v", len(expected), len(candidates), candidates)
	}
	for i := range expected {
		if candidates[i] != expected[i] {
			t.Fatalf("candidate[%d]: expected %q, got %q", i, expected[i], candidates[i])
		}
	}
}

func TestEvaluationStepDefaultsContextOutput(t *testing.T) {
	step := &EvaluationStep{ID: "eval-empty-output"}
	if got := step.GetContextOutput().String(); got != defaultEvaluationContextOutput {
		t.Fatalf("expected default evaluation output %q, got %q", defaultEvaluationContextOutput, got)
	}
	if got := step.GetCommonFields().ContextOutput.String(); got != defaultEvaluationContextOutput {
		t.Fatalf("expected common fields to use default evaluation output %q, got %q", defaultEvaluationContextOutput, got)
	}
}

func TestIsValidationSchemaLikeJSON(t *testing.T) {
	schema := `{"files":[{"file_name":"eval_result.json","must_exist":true,"json_checks":[{"path":"$.score","must_exist":true}]}]}`
	if !isValidationSchemaLikeJSON(schema) {
		t.Fatal("expected validation schema stub to be detected")
	}
	result := `{"score":1,"category_distinct_30d":6}`
	if isValidationSchemaLikeJSON(result) {
		t.Fatal("did not expect normal result JSON to be treated as validation schema")
	}
}

func TestEvaluationStepLoadsCanonicalValidationSchema(t *testing.T) {
	var step EvaluationStep
	err := json.Unmarshal([]byte(`{
		"id":"eval-result",
		"title":"Evaluate result",
		"description":"Check the result",
		"validation_schema":{"files":[{"file_name":"eval_result.json","must_exist":true}]}
	}`), &step)
	if err != nil {
		t.Fatalf("unmarshal evaluation step: %v", err)
	}
	if step.PreValidation == nil || len(step.PreValidation.Files) != 1 {
		t.Fatalf("expected canonical validation_schema to load, got %#v", step.PreValidation)
	}
	if got := step.PreValidation.Files[0].FileName; got != "eval_result.json" {
		t.Fatalf("expected eval_result.json, got %q", got)
	}
	if step.GetValidationSchema() == nil {
		t.Fatal("expected validation schema to be available to execution")
	}
}

func TestEvaluationStepLoadsLegacyPreValidation(t *testing.T) {
	var step EvaluationStep
	err := json.Unmarshal([]byte(`{
		"id":"eval-result",
		"title":"Evaluate result",
		"description":"Check the result",
		"pre_validation":{"files":[{"file_name":"legacy_result.json","must_exist":true}]}
	}`), &step)
	if err != nil {
		t.Fatalf("unmarshal legacy evaluation step: %v", err)
	}
	if step.PreValidation == nil || len(step.PreValidation.Files) != 1 {
		t.Fatalf("expected legacy pre_validation to load, got %#v", step.PreValidation)
	}
	if got := step.PreValidation.Files[0].FileName; got != "legacy_result.json" {
		t.Fatalf("expected legacy_result.json, got %q", got)
	}
}

func TestEvaluationStepMarshalsCanonicalValidationSchema(t *testing.T) {
	step := &EvaluationStep{
		ID:            "eval-result",
		Title:         "Evaluate result",
		Description:   "Check the result",
		PreValidation: &ValidationSchema{Files: []FileValidationRule{{FileName: "eval_result.json", MustExist: true}}},
	}
	raw, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal evaluation step: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode marshaled evaluation step: %v", err)
	}
	if _, ok := fields["validation_schema"]; !ok {
		t.Fatalf("expected canonical validation_schema in %s", raw)
	}
	if _, ok := fields["pre_validation"]; ok {
		t.Fatalf("did not expect legacy pre_validation in %s", raw)
	}
}
