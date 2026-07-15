package step_based_workflow

import "testing"

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
