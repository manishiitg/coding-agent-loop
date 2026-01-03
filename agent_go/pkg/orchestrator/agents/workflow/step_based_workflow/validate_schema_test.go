package step_based_workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PaesslerAG/jsonpath"
)

// TestJSONPathValidationLogic verifies that our syntax check correctly distinguishes
// between valid-but-empty paths and actual syntax errors.
func TestJSONPathValidationLogic(t *testing.T) {
	dummyData := map[string]interface{}{}

	testCases := []struct {
		path        string
		shouldError bool
		desc        string
	}{
		{"$.count", false, "Valid path, missing key"},
		{"$.items[*]", false, "Valid wildcard"},
		{"$..name", false, "Valid recursive descent"},
		{"$.items[0].id", false, "Valid array indexing"},
		{"$.[", true, "Invalid syntax: malformed bracket"},
		{"$.items[?(@.id > 10)]", true, "Unsupported complex filter"},
		{"$.invalid-char!", true, "Invalid character in path"},
		{"", true, "Empty path"},
		{"$.", true, "Trailing dot"},
		{"plain_field", true, "Missing $. prefix"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			var err error
			path := strings.TrimSpace(tc.path)
			
			if path == "" {
				err = fmt.Errorf("empty path")
			} else if !strings.HasPrefix(path, "$.") {
				err = fmt.Errorf("missing prefix")
			} else {
				_, jsonErr := jsonpath.Get(path, dummyData)
				if jsonErr != nil && strings.Contains(jsonErr.Error(), "parsing error") {
					err = jsonErr
				}
			}

			if (err != nil) != tc.shouldError {
				t.Errorf("Case '%s' [%s]: expected error=%v, got err=%v", tc.desc, tc.path, tc.shouldError, err)
			}
		})
	}
}

// TestBidirectionalArrayLength verifies our logic for detecting ambiguous vs valid array_length checks
func TestBidirectionalArrayLengthValidation(t *testing.T) {
	testCases := []struct {
		path        string
		comparePath string
		shouldError bool
		desc        string
	}{
		{"$.count", "$.items", false, "Standard: count first"},
		{"$.items", "$.count", false, "Swapped: array first (Should now pass)"},
		{"$.total_files", "$.downloaded_files", false, "Names with indicators: number first"},
		{"$.downloaded_files", "$.total_files", false, "Names with indicators: array first"},
		{"$.items", "$.files", true, "Ambiguous: both look like arrays"},
		{"$.count", "$.total", true, "Ambiguous: both look like numbers"},
		{"$.unknown1", "$.unknown2", false, "Unknown types: should pass (runtime will handle it)"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			pathIsNumber, pathIsArray := detectFieldTypeFromPath(tc.path)
			compIsNumber, compIsArray := detectFieldTypeFromPath(tc.comparePath)

			var err error
			if pathIsArray && compIsArray {
				err = fmt.Errorf("both arrays")
			} else if pathIsNumber && compIsNumber {
				err = fmt.Errorf("both numbers")
			}

			if (err != nil) != tc.shouldError {
				t.Errorf("Case '%s': expected error=%v, got err=%v", tc.desc, tc.shouldError, err)
			}
		})
	}
}
