package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// parseJSONForCheck is a small helper for validateJSONCheck tests: they need
// a decoded interface{} document, not raw bytes.
func parseJSONForCheck(t *testing.T, raw string) interface{} {
	t.Helper()
	var data interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("parse fixture JSON: %v", err)
	}
	return data
}

// TestValueTypeCheckRejectsAnActualArrayValueOnADefinitePath reproduces
// LinkedIn PUL-61C84987: a definite (non-wildcard) path whose real value is
// a JSON array containing string elements silently passed a
// value_type=string check, because the extraction code could not tell "this
// one location's value is an array" from "a wildcard path matched several
// separate locations" and defaulted to unwrapping either shape's []interface{}
// to its first element for a scalar-typed check. A single-element string
// array's first element is itself a string, so the wrong container type
// slipped through undetected.
func TestValueTypeCheckRejectsAnActualArrayValueOnADefinitePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"non-empty array with a string element", `{"notes": ["some note"]}`},
		{"empty array", `{"notes": []}`},
		{"array with multiple string elements", `{"notes": ["a", "b"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jsonData := parseJSONForCheck(t, tc.raw)
			check := JSONValidationCheck{Path: "$.notes", ValueType: "string"}
			result := validateJSONCheck(context.Background(), check, jsonData)
			if result.Passed {
				t.Fatalf("an array value passed a value_type=string check: %+v", result)
			}
			if result.CheckType != "value_type" {
				t.Fatalf("wrong check type reported: %+v", result)
			}
		})
	}
}

// TestValueTypeCheckStillPassesARealString is the positive control from the
// finding's own reproduction: a genuine string value at a definite path
// must still pass, both before and after the fix.
func TestValueTypeCheckStillPassesARealString(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"notes": "some note"}`)
	check := JSONValidationCheck{Path: "$.notes", ValueType: "string"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if !result.Passed {
		t.Fatalf("a genuine string value failed value_type=string: %+v", result)
	}
}

// TestValueTypeCheckStillAcceptsAnArrayValueWhenArrayIsExpected proves the
// fix did not disturb the legitimate case the original code handled
// correctly: a definite path whose value really is an array, checked
// against value_type=array.
func TestValueTypeCheckStillAcceptsAnArrayValueWhenArrayIsExpected(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"missing_months": ["2026-01", "2026-02"]}`)
	check := JSONValidationCheck{Path: "$.missing_months", ValueType: "array"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if !result.Passed {
		t.Fatalf("a genuine array value failed value_type=array: %+v", result)
	}
}

// TestValueTypeCheckStillUnwrapsGenuineWildcardMatches proves the fix is
// scoped to definite paths only: a real multi-match wildcard path must still
// take its first result for a scalar-typed check, exactly as before. This is
// the behavior jsonPathHasMultipleMatches exists to preserve, not remove.
func TestValueTypeCheckStillUnwrapsGenuineWildcardMatches(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"checks":[{"name":"first"},{"name":"second"}]}`)
	check := JSONValidationCheck{Path: "$.checks[*].name", ValueType: "string"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if !result.Passed {
		t.Fatalf("a genuine wildcard multi-match regressed: %+v", result)
	}
}

// TestValueTypeCheckValidatesEveryWildcardMatchNotJustTheFirst is a code
// review finding on PLAT-229: the fix correctly rejected an array value on a
// definite path, but a genuine wildcard multi-match still only inspected its
// first result. A valid first item followed by an invalid later one
// (e.g. $.items[*].name where one item's name is a number, not a string)
// silently passed.
func TestValueTypeCheckValidatesEveryWildcardMatchNotJustTheFirst(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"items":[{"name":"first"},{"name":"second"},{"name":42}]}`)
	check := JSONValidationCheck{Path: "$.items[*].name", ValueType: "string"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if result.Passed {
		t.Fatalf("a later wildcard match with the wrong type passed unnoticed: %+v", result)
	}
	if !strings.Contains(result.ErrorMsg, "match 2 of 3") {
		t.Fatalf("error does not identify which match failed: %+v", result)
	}
}

// TestAdjacentPerValuePredicatesValidateEveryWildcardMatch is a PLAT-229
// follow-up: the review that caught the value_type gap found the same
// pattern still present in min_length/max_length, min_value/max_value, and
// pattern checks -- each only inspected the first wildcard match, so a valid
// first item followed by an invalid later one silently passed.
func TestAdjacentPerValuePredicatesValidateEveryWildcardMatch(t *testing.T) {
	minLen := 3

	t.Run("min_length", func(t *testing.T) {
		jsonData := parseJSONForCheck(t, `{"items":[{"name":"first"},{"name":"second"},{"name":"x"}]}`)
		check := JSONValidationCheck{Path: "$.items[*].name", MinLength: &minLen}
		result := validateJSONCheck(context.Background(), check, jsonData)
		if result.Passed {
			t.Fatalf("a later wildcard match shorter than min_length passed unnoticed: %+v", result)
		}
		if !strings.Contains(result.ErrorMsg, "match 2 of 3") {
			t.Fatalf("error does not identify which match failed: %+v", result)
		}
	})

	t.Run("value_range", func(t *testing.T) {
		minValue := 0.0
		maxValue := 100.0
		jsonData := parseJSONForCheck(t, `{"scores":[10,50,150]}`)
		check := JSONValidationCheck{Path: "$.scores[*]", MinValue: &minValue, MaxValue: &maxValue}
		result := validateJSONCheck(context.Background(), check, jsonData)
		if result.Passed {
			t.Fatalf("a later wildcard match outside min/max value passed unnoticed: %+v", result)
		}
		if !strings.Contains(result.ErrorMsg, "match 2 of 3") {
			t.Fatalf("error does not identify which match failed: %+v", result)
		}
	})

	t.Run("pattern", func(t *testing.T) {
		jsonData := parseJSONForCheck(t, `{"handles":["@alice","@bob","not-a-handle"]}`)
		check := JSONValidationCheck{Path: "$.handles[*]", Pattern: "^@"}
		result := validateJSONCheck(context.Background(), check, jsonData)
		if result.Passed {
			t.Fatalf("a later wildcard match not matching the pattern passed unnoticed: %+v", result)
		}
		if !strings.Contains(result.ErrorMsg, "match 2 of 3") {
			t.Fatalf("error does not identify which match failed: %+v", result)
		}
	})

	t.Run("all matches valid still passes", func(t *testing.T) {
		jsonData := parseJSONForCheck(t, `{"handles":["@alice","@bob","@carol"]}`)
		check := JSONValidationCheck{Path: "$.handles[*]", Pattern: "^@", MinLength: &minLen}
		result := validateJSONCheck(context.Background(), check, jsonData)
		if !result.Passed {
			t.Fatalf("all-valid wildcard matches unexpectedly failed: %+v", result)
		}
	})
}

// TestValueTypeArrayOnWildcardChecksEveryMatchNotTheCollection is a second
// PLAT-229 follow-up: value_type="array" on a genuine wildcard multi-match
// path used to special-case the whole multi-match result slice as "the"
// value, checking whether that Go []interface{} was itself an array --
// which is trivially always true regardless of what any individual matched
// value actually was, so a wildcard array check could never actually fail.
// value_type="array" must mean "every matched value is itself an array",
// same as every other per-value predicate on a multi-match path.
func TestValueTypeArrayOnWildcardChecksEveryMatchNotTheCollection(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"items":[{"tags":["a","b"]},{"tags":["c"]},{"tags":"not-an-array"}]}`)
	check := JSONValidationCheck{Path: "$.items[*].tags", ValueType: "array"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if result.Passed {
		t.Fatalf("a later wildcard match that is not an array passed unnoticed: %+v", result)
	}
	if !strings.Contains(result.ErrorMsg, "match 2 of 3") {
		t.Fatalf("error does not identify which match failed: %+v", result)
	}
}

func TestValueTypeArrayOnWildcardPassesWhenEveryMatchIsAnArray(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"items":[{"tags":["a","b"]},{"tags":["c"]},{"tags":[]}]}`)
	check := JSONValidationCheck{Path: "$.items[*].tags", ValueType: "array"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if !result.Passed {
		t.Fatalf("all-array wildcard matches unexpectedly failed: %+v", result)
	}
}

// TestValueTypeArrayOnDefinitePathStillUsesTheArrayItself pins that a
// definite (non-wildcard) path pointing directly at one array value is
// still checked against that array itself, not per-element -- only a
// genuine multi-match path means "every match", per
// jsonPathHasMultipleMatches's own contract.
func TestValueTypeArrayOnDefinitePathStillUsesTheArrayItself(t *testing.T) {
	jsonData := parseJSONForCheck(t, `{"notes":["a","b","c"]}`)
	check := JSONValidationCheck{Path: "$.notes", ValueType: "array"}
	result := validateJSONCheck(context.Background(), check, jsonData)
	if !result.Passed {
		t.Fatalf("a definite path's own array value should pass value_type=array: %+v", result)
	}
}

// TestJSONPathHasMultipleMatchesDistinguishesDefiniteFromWildcardPaths pins
// the exact boundary the fix depends on: a plain numeric index like [0]
// names one location (PLAT's JSONValidationCheck.Path doc comment gives
// "$.databases[0].name" as a definite-path example) and must not be treated
// as a multi-match collector merely because it contains brackets.
func TestJSONPathHasMultipleMatchesDistinguishesDefiniteFromWildcardPaths(t *testing.T) {
	for path, want := range map[string]bool{
		"$.notes":               false,
		"$.databases[0].name":   false,
		"$.a.b.c":               false,
		"$.checks[*].name":      true,
		"$..author":             true,
		"$.book[?(@.price<10)]": true,
		"$.book[0:2]":           true,
		"$.book[0,1]":           true,
	} {
		if got := jsonPathHasMultipleMatches(path); got != want {
			t.Errorf("jsonPathHasMultipleMatches(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestValidateFilePath(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/app/workspace-docs")
	stepExecutionPath := "Workflow/linkedin/runs/iteration-0/default/execution/step-global-research"

	testCases := []struct {
		name      string
		fileName  string
		want      string
		wantError bool
	}{
		{
			name:     "step local bare file",
			fileName: "auth_context.json",
			want:     "Workflow/linkedin/runs/iteration-0/default/execution/step-global-research/auth_context.json",
		},
		{
			name:     "workflow knowledgebase path",
			fileName: "knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:     "already workflow scoped relative path",
			fileName: "Workflow/linkedin/knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:     "absolute path inside current workflow",
			fileName: "/app/workspace-docs/Workflow/linkedin/knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:      "absolute path outside current workflow",
			fileName:  "/app/workspace-docs/Workflow/social-media/knowledgebase/research/global_trends.json",
			wantError: true,
		},
		{
			name:      "relative path outside current workflow",
			fileName:  "Workflow/social-media/knowledgebase/research/global_trends.json",
			wantError: true,
		},
		{
			name:      "path traversal rejected",
			fileName:  "../knowledgebase/research/global_trends.json",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateFilePath(stepExecutionPath, tc.fileName)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestDeriveAlternateValidationPath(t *testing.T) {
	stepExecutionPath := "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn"

	testCases := []struct {
		name         string
		fileName     string
		resolvedPath string
		want         string
	}{
		{
			name:         "workflow root file gets step local alternate",
			fileName:     "knowledgebase/research/current/hn_raw.json",
			resolvedPath: "Workflow/linkedin/knowledgebase/research/current/hn_raw.json",
			want:         "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/knowledgebase/research/current/hn_raw.json",
		},
		{
			name:         "step local file gets workflow root alternate",
			fileName:     "knowledgebase/research/current/hn_raw.json",
			resolvedPath: "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/knowledgebase/research/current/hn_raw.json",
			want:         "Workflow/linkedin/knowledgebase/research/current/hn_raw.json",
		},
		{
			name:         "bare step file gets workflow root alternate",
			fileName:     "auth_context.json",
			resolvedPath: "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/auth_context.json",
			want:         "Workflow/linkedin/auth_context.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveAlternateValidationPath(stepExecutionPath, tc.fileName, tc.resolvedPath)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildValidationPathHint(t *testing.T) {
	got := buildValidationPathHint(
		"Workflow/linkedin/knowledgebase/research/current/hn_raw.json",
		"Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/knowledgebase/research/current/hn_raw.json",
		true,
	)
	if got == "" || !strings.Contains(got, "Another copy also exists") || !strings.Contains(got, "validation read the other") {
		t.Fatalf("unexpected hint: %q", got)
	}
}

func TestRunPreValidationAllowsBinaryFileWhenOnlyMustExist(t *testing.T) {
	const stepPath = "Workflow/instagram/runs/iteration-0/test-run/execution/route-generate-voiceover"
	const voiceoverPath = stepPath + "/voiceover.mp3"

	var binaryReads int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/documents/"+voiceoverPath {
			atomic.AddInt32(&binaryReads, 1)
			fmt.Fprint(w, `{"success":true,"message":"Document retrieved successfully","data":{"filepath":"`+voiceoverPath+`","content":"","is_binary":true,"size":3,"mime_type":"audio/mpeg"}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"message":"File does not exist","data":{},"error":"File not found"}`)
	}))
	defer server.Close()

	t.Setenv("WORKSPACE_API_URL", server.URL)
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	result, err := RunPreValidation(context.Background(), &ValidationSchema{
		Files: []FileValidationRule{{
			FileName:  "voiceover.mp3",
			MustExist: true,
		}},
	}, stepPath, base)
	if err != nil {
		t.Fatalf("RunPreValidation returned error: %v", err)
	}
	if !result.OverallPass {
		t.Fatalf("expected validation to pass, got errors: %#v", result.Summary.Errors)
	}
	if result.Summary.TotalChecks != 1 || result.Summary.PassedChecks != 1 || result.Summary.FailedChecks != 0 {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
	if got := atomic.LoadInt32(&binaryReads); got != 1 {
		t.Fatalf("expected exactly one binary metadata read for existence, got %d", got)
	}
	if len(result.FilesChecked) != 1 || len(result.FilesChecked[0].JSONChecks) != 0 {
		t.Fatalf("binary must-exist validation should not add JSON/text read checks: %#v", result.FilesChecked)
	}
}

// PLAT-229: empty optional collections are legitimate no-action outcomes.
func TestEmptyWildcardChecksRespectExistenceAndParentConstraints(t *testing.T) {
	minLength := 1
	minValue := 0.0
	for _, check := range []JSONValidationCheck{
		{Path: "$.engagements[*].text", ValueType: "string"},
		{Path: "$.engagements[*].count", ValueType: "number", MinValue: &minValue},
		{Path: "$.engagements[*].text", Pattern: "^valid", MinLength: &minLength},
	} {
		data := parseJSONForCheck(t, `{"engagements":[]}`)
		if got := validateJSONCheck(context.Background(), check, data); !got.Passed {
			t.Fatalf("optional empty collection failed: %+v", got)
		}
		check.MustExist = true
		if got := validateJSONCheck(context.Background(), check, data); !got.Passed {
			t.Fatalf("per-item requirement rejected an empty parent: %+v", got)
		}
	}
	for _, raw := range []string{`{"engagements":[]}`, `{"engagements":[{"text":"valid"},{"text":42}]}`} {
		check := JSONValidationCheck{Path: "$.engagements", ValueType: "array", MinLength: &minLength}
		if raw != `{"engagements":[]}` {
			check = JSONValidationCheck{Path: "$.engagements[*].text", ValueType: "string"}
		}
		if got := validateJSONCheck(context.Background(), check, parseJSONForCheck(t, raw)); got.Passed {
			t.Fatalf("invalid populated value or required cardinality passed: %s", raw)
		}
	}
}

func TestWildcardRequiresFieldsInEveryExistingItem(t *testing.T) {
	for _, raw := range []string{`{}`, `{"items":null}`, `{"items":42}`, `{"items":{}}`, `{"items":[{}]}`, `{"items":[{"name":"ok"},{}]}`, `{"items":[{"name":"ok"},{"name":42}]}`} {
		check := JSONValidationCheck{Path: "$.items[*].name", MustExist: true, ValueType: "string"}
		if got := validateJSONCheck(context.Background(), check, parseJSONForCheck(t, raw)); got.Passed {
			t.Fatalf("missing/invalid required item accepted: %s", raw)
		}
	}
}
