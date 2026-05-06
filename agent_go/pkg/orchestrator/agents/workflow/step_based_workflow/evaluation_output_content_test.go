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
