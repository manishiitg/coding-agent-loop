package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"

	"github.com/PaesslerAG/jsonpath"
)

const (
	MaxChecksPerFile  = 100              // Limit checks per file
	MaxFilesPerSchema = 20               // Limit files per schema
	MaxJSONFileSize   = 10 * 1024 * 1024 // 10MB max file size
)

// WorkspaceVerificationResult represents the result of pre-validation
type WorkspaceVerificationResult struct {
	OverallPass  bool
	FilesChecked []FileCheckResult
	Summary      ValidationSummary
}

// ValidationSummary provides a summary of validation checks
type ValidationSummary struct {
	TotalChecks  int
	PassedChecks int
	FailedChecks int
	Errors       []ValidationError
}

// ValidationError represents a single validation error
type ValidationError struct {
	File      string
	Path      string
	CheckType string // "must_exist", "value_type", "min_length", etc.
	Expected  string
	Actual    string
	Message   string
}

// FileCheckResult represents the result of checking a single file
type FileCheckResult struct {
	FileName   string
	Exists     bool
	IsJSON     bool
	JSONChecks []JSONCheckResult
}

// JSONCheckResult represents the result of a single JSON validation check
type JSONCheckResult struct {
	Path      string
	Passed    bool
	CheckType string
	Expected  interface{}
	Actual    interface{}
	ErrorMsg  string
}

// WorkspaceToolExecutors is a type alias for the workspace tool executors map
type WorkspaceToolExecutors map[string]interface{}

// RunPreValidation runs pre-validation on a step's output
// validationSchema can be passed directly (preferred) or extracted from PlanStepInterface
// If validationSchema is nil, pre-validation is skipped and a result indicating skip is returned
func RunPreValidation(
	ctx context.Context,
	validationSchema *ValidationSchema, // Validation schema (preferred - passed from TodoStep)
	workspacePath string,
	baseOrchestrator *orchestrator.BaseOrchestrator,
) (*WorkspaceVerificationResult, error) {
	// If schema is nil, skip pre-validation and return a result indicating skip
	if validationSchema == nil {
		return &WorkspaceVerificationResult{
			OverallPass:  true, // Pass so it doesn't block LLM validation
			FilesChecked: []FileCheckResult{},
			Summary: ValidationSummary{
				TotalChecks:  0,
				PassedChecks: 0,
				FailedChecks: 0,
				Errors:       []ValidationError{},
			},
		}, nil
	}

	// If schema is empty (no files), skip pre-validation
	if len(validationSchema.Files) == 0 {
		return &WorkspaceVerificationResult{
			OverallPass:  true, // Pass so it doesn't block LLM validation
			FilesChecked: []FileCheckResult{},
			Summary: ValidationSummary{
				TotalChecks:  0,
				PassedChecks: 0,
				FailedChecks: 0,
				Errors:       []ValidationError{},
			},
		}, nil
	}

	// Validate schema limits
	if err := validateSchemaLimits(validationSchema); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// Perform validation with schema
	return validateWithSchema(ctx, validationSchema, workspacePath, baseOrchestrator)
}

// validateSchemaLimits checks if the schema exceeds resource limits
func validateSchemaLimits(schema *ValidationSchema) error {
	if len(schema.Files) > MaxFilesPerSchema {
		return fmt.Errorf("schema exceeds max files (%d)", MaxFilesPerSchema)
	}

	for _, file := range schema.Files {
		if len(file.JSONChecks) > MaxChecksPerFile {
			return fmt.Errorf("file %s exceeds max checks (%d)", file.FileName, MaxChecksPerFile)
		}
	}

	return nil
}

// validateWithSchema performs validation using the provided schema
func validateWithSchema(
	ctx context.Context,
	schema *ValidationSchema,
	workspacePath string,
	baseOrchestrator *orchestrator.BaseOrchestrator,
) (*WorkspaceVerificationResult, error) {
	result := &WorkspaceVerificationResult{
		OverallPass:  true,
		FilesChecked: []FileCheckResult{},
		Summary: ValidationSummary{
			TotalChecks:  0,
			PassedChecks: 0,
			FailedChecks: 0,
			Errors:       []ValidationError{},
		},
	}

	// Validate each file in the schema
	for _, fileRule := range schema.Files {
		fileResult := validateFile(ctx, fileRule, workspacePath, baseOrchestrator)
		result.FilesChecked = append(result.FilesChecked, fileResult)

		// Update summary
		for _, jsonCheck := range fileResult.JSONChecks {
			result.Summary.TotalChecks++
			if jsonCheck.Passed {
				result.Summary.PassedChecks++
			} else {
				result.Summary.FailedChecks++
				result.OverallPass = false
				// Add error to summary
				result.Summary.Errors = append(result.Summary.Errors, ValidationError{
					File:      fileResult.FileName,
					Path:      jsonCheck.Path,
					CheckType: jsonCheck.CheckType,
					Expected:  fmt.Sprintf("%v", jsonCheck.Expected),
					Actual:    fmt.Sprintf("%v", jsonCheck.Actual),
					Message:   jsonCheck.ErrorMsg,
				})
			}
		}

		// Check file existence
		if fileRule.MustExist && !fileResult.Exists {
			result.OverallPass = false
			result.Summary.TotalChecks++
			result.Summary.FailedChecks++
			result.Summary.Errors = append(result.Summary.Errors, ValidationError{
				File:      fileResult.FileName,
				Path:      "",
				CheckType: "must_exist",
				Expected:  "file exists",
				Actual:    "file not found",
				Message:   fmt.Sprintf("File %s must exist but was not found", fileResult.FileName),
			})
		} else if fileRule.MustExist && fileResult.Exists {
			result.Summary.TotalChecks++
			result.Summary.PassedChecks++
		}
	}

	return result, nil
}

// validateFile validates a single file against its rules
func validateFile(
	ctx context.Context,
	fileRule FileValidationRule,
	workspacePath string,
	baseOrchestrator *orchestrator.BaseOrchestrator,
) FileCheckResult {
	result := FileCheckResult{
		FileName:   fileRule.FileName,
		Exists:     false,
		IsJSON:     false,
		JSONChecks: []JSONCheckResult{},
	}

	// Validate file path (prevent path traversal)
	fullPath, err := validateFilePath(workspacePath, fileRule.FileName)
	if err != nil {
		// Path validation failed - add error and return
		result.JSONChecks = append(result.JSONChecks, JSONCheckResult{
			Path:      "",
			Passed:    false,
			CheckType: "path_validation",
			Expected:  "valid file path",
			Actual:    err.Error(),
			ErrorMsg:  fmt.Sprintf("Invalid file path: %v", err),
		})
		return result
	}

	// Check if file exists
	exists, err := baseOrchestrator.CheckWorkspaceFileExists(ctx, fullPath)
	if err != nil {
		// Error checking file - add error and return
		result.JSONChecks = append(result.JSONChecks, JSONCheckResult{
			Path:      "",
			Passed:    false,
			CheckType: "file_check",
			Expected:  "file accessible",
			Actual:    err.Error(),
			ErrorMsg:  fmt.Sprintf("Error checking file existence: %v", err),
		})
		return result
	}

	result.Exists = exists

	// If file doesn't exist and we have JSON checks, we can't validate them
	if !exists {
		return result
	}

	// Read file content
	content, err := baseOrchestrator.ReadWorkspaceFile(ctx, fullPath)
	if err != nil {
		result.JSONChecks = append(result.JSONChecks, JSONCheckResult{
			Path:      "",
			Passed:    false,
			CheckType: "file_read",
			Expected:  "file readable",
			Actual:    err.Error(),
			ErrorMsg:  fmt.Sprintf("Error reading file: %v", err),
		})
		return result
	}

	// Check file size
	if len(content) > MaxJSONFileSize {
		result.JSONChecks = append(result.JSONChecks, JSONCheckResult{
			Path:      "",
			Passed:    false,
			CheckType: "file_size",
			Expected:  fmt.Sprintf("file size <= %d bytes", MaxJSONFileSize),
			Actual:    fmt.Sprintf("%d bytes", len(content)),
			ErrorMsg:  fmt.Sprintf("File size exceeds maximum (%d bytes)", MaxJSONFileSize),
		})
		return result
	}

	// Try to parse as JSON
	var jsonData interface{}
	if err := json.Unmarshal([]byte(content), &jsonData); err != nil {
		// Not valid JSON - add error
		result.JSONChecks = append(result.JSONChecks, JSONCheckResult{
			Path:      "",
			Passed:    false,
			CheckType: "json_parse",
			Expected:  "valid JSON",
			Actual:    err.Error(),
			ErrorMsg:  fmt.Sprintf("File is not valid JSON: %v", err),
		})
		return result
	}

	result.IsJSON = true

	// Validate JSON checks
	for _, check := range fileRule.JSONChecks {
		checkResult := validateJSONCheck(ctx, check, jsonData)
		result.JSONChecks = append(result.JSONChecks, checkResult)
	}

	return result
}

// validateJSONCheck validates a single JSON check
func validateJSONCheck(
	ctx context.Context,
	check JSONValidationCheck,
	jsonData interface{},
) JSONCheckResult {
	result := JSONCheckResult{
		Path:      check.Path,
		Passed:    false,
		CheckType: "",
		Expected:  nil,
		Actual:    nil,
		ErrorMsg:  "",
	}

	// Evaluate JSONPath
	values, err := jsonpath.Get(check.Path, jsonData)
	if err != nil {
		// Path doesn't exist or is invalid
		if check.MustExist {
			result.CheckType = "must_exist"
			result.Expected = "path exists"
			result.Actual = "path not found"
			result.ErrorMsg = fmt.Sprintf("Path %s must exist but was not found: %v", check.Path, err)
			return result
		}
		// Path doesn't need to exist, so this check passes
		result.Passed = true
		return result
	}

	// Path exists - if MustExist is true, this check passes
	if check.MustExist {
		result.Passed = true
		result.CheckType = "must_exist"
	}

	// If path exists but MustExist is false, we still need to validate other checks
	// Handle JSONPath return value correctly:
	// - If path points to an array field directly (like $.missing_months), jsonpath.Get returns the array itself
	// - If path uses wildcards/filters, jsonpath.Get returns a slice of matching results
	// - If path points to a scalar, jsonpath.Get returns the scalar value
	var value interface{}
	if valuesSlice, ok := values.([]interface{}); ok {
		// If we're expecting an array and got a slice, the slice IS the array value
		// (not a collection of results to take the first element from)
		if check.ValueType == "array" {
			value = valuesSlice
		} else if len(valuesSlice) > 0 {
			// For non-array types, if we got multiple results, take the first one
			value = valuesSlice[0]
		} else {
			// Empty slice - use as is (will fail validation if expected type is not array)
			value = valuesSlice
		}
	} else {
		value = values
	}

	// Validate value type
	if check.ValueType != "" {
		typeResult := validateValueType(check.Path, value, check.ValueType)
		if !typeResult.Passed {
			return typeResult
		}
		result.CheckType = "value_type"
		result.Passed = true
	}

	// Validate min/max length for strings and arrays
	if check.MinLength != nil || check.MaxLength != nil {
		lengthResult := validateLength(check.Path, value, check.MinLength, check.MaxLength)
		if !lengthResult.Passed {
			return lengthResult
		}
		if result.CheckType == "" {
			result.CheckType = "length"
		}
		result.Passed = true
	}

	// Validate min/max value for numbers
	if check.MinValue != nil || check.MaxValue != nil {
		valueResult := validateValueRange(check.Path, value, check.MinValue, check.MaxValue)
		if !valueResult.Passed {
			return valueResult
		}
		if result.CheckType == "" {
			result.CheckType = "value_range"
		}
		result.Passed = true
	}

	// Validate pattern (regex) for strings
	if check.Pattern != "" {
		patternResult := validatePattern(check.Path, value, check.Pattern)
		if !patternResult.Passed {
			return patternResult
		}
		if result.CheckType == "" {
			result.CheckType = "pattern"
		}
		result.Passed = true
	}

	// Validate consistency check
	if check.ConsistencyCheck != nil {
		// Skip consistency check if compare_with_path is empty (malformed schema)
		// This allows other checks (like value_type) to still pass
		comparePath := strings.TrimSpace(check.ConsistencyCheck.CompareWithPath)
		if comparePath == "" {
			// Malformed consistency check - skip it but don't fail
			// The path exists and other checks can still validate it
		} else {
			consistencyResult := validateConsistency(ctx, check, jsonData)
			if !consistencyResult.Passed {
				return consistencyResult
			}
			if result.CheckType == "" {
				result.CheckType = "consistency"
			}
			result.Passed = true
		}
	}

	// If no specific checks were performed but path exists and MustExist is true, it passes
	if result.CheckType == "" && check.MustExist {
		result.Passed = true
		result.CheckType = "must_exist"
	}

	return result
}

// validateValueType validates that a value matches the expected type
func validateValueType(path string, value interface{}, expectedType string) JSONCheckResult {
	result := JSONCheckResult{
		Path:      path,
		CheckType: "value_type",
		Passed:    false,
		Expected:  expectedType,
		Actual:    fmt.Sprintf("%T", value),
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); ok {
			result.Passed = true
		} else {
			result.ErrorMsg = fmt.Sprintf("Expected string, got %T", value)
		}
	case "number":
		switch value.(type) {
		case float64, int, int64, float32:
			result.Passed = true
		default:
			result.ErrorMsg = fmt.Sprintf("Expected number, got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); ok {
			result.Passed = true
		} else {
			result.ErrorMsg = fmt.Sprintf("Expected boolean, got %T", value)
		}
	case "array":
		if _, ok := value.([]interface{}); ok {
			result.Passed = true
		} else {
			result.ErrorMsg = fmt.Sprintf("Expected array, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); ok {
			result.Passed = true
		} else {
			result.ErrorMsg = fmt.Sprintf("Expected object, got %T", value)
		}
	default:
		result.ErrorMsg = fmt.Sprintf("Unknown value type: %s", expectedType)
	}

	return result
}

// validateLength validates min/max length for strings and arrays
func validateLength(path string, value interface{}, minLength *int, maxLength *int) JSONCheckResult {
	result := JSONCheckResult{
		Path:      path,
		CheckType: "length",
		Passed:    false,
	}

	var length int
	var valueType string

	switch v := value.(type) {
	case string:
		length = len(v)
		valueType = "string"
	case []interface{}:
		length = len(v)
		valueType = "array"
	default:
		result.ErrorMsg = "Length validation only applies to strings and arrays"
		result.Expected = "string or array"
		result.Actual = fmt.Sprintf("%T", value)
		return result
	}

	if minLength != nil && length < *minLength {
		result.ErrorMsg = fmt.Sprintf("%s length %d is less than minimum %d", valueType, length, *minLength)
		result.Expected = fmt.Sprintf("length >= %d", *minLength)
		result.Actual = fmt.Sprintf("length = %d", length)
		return result
	}

	if maxLength != nil && length > *maxLength {
		result.ErrorMsg = fmt.Sprintf("%s length %d exceeds maximum %d", valueType, length, *maxLength)
		result.Expected = fmt.Sprintf("length <= %d", *maxLength)
		result.Actual = fmt.Sprintf("length = %d", length)
		return result
	}

	result.Passed = true
	return result
}

// validateValueRange validates min/max value for numbers
func validateValueRange(path string, value interface{}, minValue *float64, maxValue *float64) JSONCheckResult {
	result := JSONCheckResult{
		Path:      path,
		CheckType: "value_range",
		Passed:    false,
	}

	var numValue float64
	switch v := value.(type) {
	case float64:
		numValue = v
	case int:
		numValue = float64(v)
	case int64:
		numValue = float64(v)
	case float32:
		numValue = float64(v)
	default:
		result.ErrorMsg = "Value range validation only applies to numbers"
		result.Expected = "number"
		result.Actual = fmt.Sprintf("%T", value)
		return result
	}

	if minValue != nil && numValue < *minValue {
		result.ErrorMsg = fmt.Sprintf("Value %f is less than minimum %f", numValue, *minValue)
		result.Expected = fmt.Sprintf("value >= %f", *minValue)
		result.Actual = fmt.Sprintf("value = %f", numValue)
		return result
	}

	if maxValue != nil && numValue > *maxValue {
		result.ErrorMsg = fmt.Sprintf("Value %f exceeds maximum %f", numValue, *maxValue)
		result.Expected = fmt.Sprintf("value <= %f", *maxValue)
		result.Actual = fmt.Sprintf("value = %f", numValue)
		return result
	}

	result.Passed = true
	return result
}

// validatePattern validates a string against a regex pattern
func validatePattern(path string, value interface{}, pattern string) JSONCheckResult {
	result := JSONCheckResult{
		Path:      path,
		CheckType: "pattern",
		Passed:    false,
		Expected:  pattern,
	}

	strValue, ok := value.(string)
	if !ok {
		result.ErrorMsg = "Pattern validation only applies to strings"
		result.Actual = fmt.Sprintf("%T", value)
		return result
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Invalid regex pattern: %v", err)
		result.Actual = pattern
		return result
	}

	if !regex.MatchString(strValue) {
		result.ErrorMsg = fmt.Sprintf("String '%s' does not match pattern '%s'", strValue, pattern)
		result.Actual = strValue
		return result
	}

	result.Passed = true
	return result
}

// validateConsistency validates consistency checks between fields
func validateConsistency(
	ctx context.Context,
	check JSONValidationCheck,
	jsonData interface{},
) JSONCheckResult {
	result := JSONCheckResult{
		Path:      check.Path,
		CheckType: "consistency",
		Passed:    false,
	}

	// Get the value at the current path
	currentValues, err := jsonpath.Get(check.Path, jsonData)
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Failed to get value at path %s: %v", check.Path, err)
		return result
	}

	var currentValue interface{}
	if valuesSlice, ok := currentValues.([]interface{}); ok {
		// For consistency checks, if we need the full array (like array_length), keep it
		// Otherwise, take the first element
		if check.ConsistencyCheck.Type == "array_length" {
			// For array_length, if Path points to an array, use its length
			// Otherwise, currentValue should be a number, so take first element
			currentValue = valuesSlice
		} else if len(valuesSlice) > 0 {
			currentValue = valuesSlice[0]
		} else {
			currentValue = valuesSlice
		}
	} else {
		currentValue = currentValues
	}

	// Validate comparison path before using it
	comparePath := strings.TrimSpace(check.ConsistencyCheck.CompareWithPath)
	if comparePath == "" {
		result.ErrorMsg = fmt.Sprintf("Consistency check requires a valid 'compare_with_path', but it is empty or whitespace. Check type: %s", check.ConsistencyCheck.Type)
		result.Expected = "non-empty JSONPath string"
		result.Actual = "empty or whitespace"
		return result
	}

	// Get the value at the comparison path
	compareValues, err := jsonpath.Get(comparePath, jsonData)
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Failed to get value at comparison path '%s': %v", comparePath, err)
		result.Expected = fmt.Sprintf("valid path that exists in JSON: %s", comparePath)
		result.Actual = fmt.Sprintf("path error: %v", err)
		return result
	}

	var compareValue interface{}
	if valuesSlice, ok := compareValues.([]interface{}); ok {
		// For array_length check, compareValue should be the full array
		// For other checks, take the first element
		if check.ConsistencyCheck.Type == "array_length" {
			compareValue = valuesSlice
		} else if len(valuesSlice) > 0 {
			compareValue = valuesSlice[0]
		} else {
			compareValue = valuesSlice
		}
	} else {
		compareValue = compareValues
	}

	// Perform consistency check based on type
	switch check.ConsistencyCheck.Type {
	case "equals":
		if fmt.Sprintf("%v", currentValue) != fmt.Sprintf("%v", compareValue) {
			result.ErrorMsg = fmt.Sprintf("Values do not match: %v != %v", currentValue, compareValue)
			result.Expected = fmt.Sprintf("equals %v", compareValue)
			result.Actual = fmt.Sprintf("%v", currentValue)
			return result
		}
	case "array_length":
		// Current value can be either:
		// 1. A number (count field) - compare with array length
		// 2. An array (if Path incorrectly points to array) - use its length
		var currentNum float64

		// Check if currentValue is an array (Path points to array itself)
		if currentArray, ok := currentValue.([]interface{}); ok {
			// Path points to an array, use its length
			currentNum = float64(len(currentArray))
		} else {
			// Try to extract number from currentValue
			switch v := currentValue.(type) {
			case float64:
				currentNum = v
			case int:
				currentNum = float64(v)
			case int64:
				currentNum = float64(v)
			case float32:
				currentNum = float64(v)
			default:
				result.ErrorMsg = fmt.Sprintf("Array length check requires number or array at %s, got %T", check.Path, currentValue)
				return result
			}
		}

		var compareArray []interface{}
		if arr, ok := compareValue.([]interface{}); ok {
			compareArray = arr
		} else {
			result.ErrorMsg = fmt.Sprintf("Array length check requires array at %s, got %T", comparePath, compareValue)
			return result
		}

		if int(currentNum) != len(compareArray) {
			result.ErrorMsg = fmt.Sprintf("Count %v does not match array length %d", currentNum, len(compareArray))
			result.Expected = fmt.Sprintf("equals array length (%d)", len(compareArray))
			result.Actual = fmt.Sprintf("%v", currentNum)
			return result
		}
	case "greater_than":
		currentNum, ok1 := getNumericValue(currentValue)
		compareNum, ok2 := getNumericValue(compareValue)
		if !ok1 || !ok2 {
			result.ErrorMsg = "Greater than check requires numeric values"
			return result
		}
		if currentNum <= compareNum {
			result.ErrorMsg = fmt.Sprintf("Value %v is not greater than %v", currentNum, compareNum)
			result.Expected = fmt.Sprintf("> %v", compareNum)
			result.Actual = fmt.Sprintf("%v", currentNum)
			return result
		}
	case "less_than":
		currentNum, ok1 := getNumericValue(currentValue)
		compareNum, ok2 := getNumericValue(compareValue)
		if !ok1 || !ok2 {
			result.ErrorMsg = "Less than check requires numeric values"
			return result
		}
		if currentNum >= compareNum {
			result.ErrorMsg = fmt.Sprintf("Value %v is not less than %v", currentNum, compareNum)
			result.Expected = fmt.Sprintf("< %v", compareNum)
			result.Actual = fmt.Sprintf("%v", currentNum)
			return result
		}
	case "in_array":
		// Current value should be in the comparison array
		var compareArray []interface{}
		if arr, ok := compareValue.([]interface{}); ok {
			compareArray = arr
		} else {
			result.ErrorMsg = fmt.Sprintf("In array check requires array at %s, got %T", comparePath, compareValue)
			return result
		}

		found := false
		for _, item := range compareArray {
			if fmt.Sprintf("%v", item) == fmt.Sprintf("%v", currentValue) {
				found = true
				break
			}
		}

		if !found {
			result.ErrorMsg = fmt.Sprintf("Value %v not found in array", currentValue)
			result.Expected = "value in array"
			result.Actual = fmt.Sprintf("%v", currentValue)
			return result
		}
	default:
		result.ErrorMsg = fmt.Sprintf("Unknown consistency check type: %s", check.ConsistencyCheck.Type)
		return result
	}

	result.Passed = true
	return result
}

// getNumericValue extracts a numeric value from an interface{}
func getNumericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	default:
		return 0, false
	}
}

// validateFilePath validates and sanitizes a file path to prevent path traversal
func validateFilePath(workspacePath string, fileName string) (string, error) {
	// Reject absolute paths
	if filepath.IsAbs(fileName) {
		return "", fmt.Errorf("absolute paths not allowed")
	}

	// Reject paths with ".."
	if strings.Contains(fileName, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}

	// Clean the path
	cleanPath := filepath.Clean(fileName)

	// Join with workspace path
	fullPath := filepath.Join(workspacePath, cleanPath)

	// Verify the result is still within workspace
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute workspace path: %w", err)
	}

	fullPathAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute file path: %w", err)
	}

	if !strings.HasPrefix(fullPathAbs, workspaceAbs) {
		return "", fmt.Errorf("path outside workspace")
	}

	return fullPath, nil
}

// formatWorkspaceResults formats the pre-validation results for the validation agent
func formatWorkspaceResults(results *WorkspaceVerificationResult) string {
	// Check if pre-validation was skipped (no checks performed)
	if results.Summary.TotalChecks == 0 && len(results.FilesChecked) == 0 {
		return `
⏭️ PRE-VALIDATION SKIPPED

No validation schema was provided for this step. Pre-validation was skipped.

Your task: Verify the execution history proves this output is authentic.
Focus on:
1. Did the agent actually read/process the claimed data sources?
2. Do tool calls in execution history match the output values?
3. Is the timeline consistent?
4. Are there any suspicious patterns (e.g., round numbers, fake data)?
`
	}

	if results.OverallPass {
		return fmt.Sprintf(`
✅ PRE-VALIDATION PASSED

Files Checked: %d
Checks Passed: %d/%d
All structural checks passed.

Your task: Verify the execution history proves this output is authentic.
Focus on:
1. Did the agent actually read/process the claimed data sources?
2. Do tool calls in execution history match the output values?
3. Is the timeline consistent?
4. Are there any suspicious patterns (e.g., round numbers, fake data)?
`, len(results.FilesChecked), results.Summary.PassedChecks, results.Summary.TotalChecks)
	} else {
		var errDetails strings.Builder
		errDetails.WriteString("❌ PRE-VALIDATION FAILED\n\n")
		errDetails.WriteString(fmt.Sprintf("Checks: %d passed, %d failed\n\n",
			results.Summary.PassedChecks, results.Summary.FailedChecks))

		for _, err := range results.Summary.Errors {
			errDetails.WriteString(fmt.Sprintf("❌ %s [%s]: %s\n",
				err.File, err.Path, err.Message))
			if err.Expected != "" {
				errDetails.WriteString(fmt.Sprintf("   Expected: %s, Actual: %s\n",
					err.Expected, err.Actual))
			}
		}

		errDetails.WriteString("\nThe execution agent did not produce valid output structure.\n")
		errDetails.WriteString("Validation FAILED - structural issues must be fixed.\n")

		return errDetails.String()
	}
}
