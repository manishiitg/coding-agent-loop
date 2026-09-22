package step_based_workflow

import (
	"strings"
	"testing"
)

func TestMessageSequenceValidationSchemaJSONNilReturnsEmpty(t *testing.T) {
	if got := messageSequenceValidationSchemaJSON(nil); got != "" {
		t.Fatalf("messageSequenceValidationSchemaJSON(nil) = %q; want empty", got)
	}
}

func TestMessageSequenceValidationSchemaJSONRendersRequiredFiles(t *testing.T) {
	schema := &ValidationSchema{
		Files: []FileValidationRule{{
			FileName:  "results.json",
			MustExist: true,
			JSONChecks: []JSONValidationCheck{{
				Path:      "$.status",
				MustExist: true,
			}},
		}},
	}

	got := messageSequenceValidationSchemaJSON(schema)
	if !strings.Contains(got, "results.json") || !strings.Contains(got, "$.status") {
		t.Fatalf("schema JSON missing content: %q", got)
	}
}
