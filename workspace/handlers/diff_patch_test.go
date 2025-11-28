package handlers

import (
	"testing"
)

func TestApplyDiffPatchFlexible(t *testing.T) {
	tests := []struct {
		name           string
		currentContent string
		diffContent    string
		expectedError  bool
		expectedResult string
	}{
		{
			name: "Traditional unified diff",
			currentContent: `# Todo List

## Objective
- Complete project analysis
- Generate comprehensive report

## Notes
- Leverages tavily-search for comprehensive research`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
+**Patch Test**: This was added via unified diff.

 ## Objective
`,
			expectedError: false,
			expectedResult: `# Todo List
**Patch Test**: This was added via unified diff.

## Objective
- Complete project analysis
- Generate comprehensive report

## Notes
- Leverages tavily-search for comprehensive research`,
		},
		{
			name:           "Line ending normalization test (CRLF to LF)",
			currentContent: "# Todo List\r\n\r\n## Objective\r\n- Complete project analysis",
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
+**CRLF Test**: Added with normalized line endings

 ## Objective
`,
			expectedError: false,
			expectedResult: `# Todo List
**CRLF Test**: Added with normalized line endings

## Objective
- Complete project analysis`,
		},
		{
			name: "Diff without newline ending (should fail validation)",
			currentContent: `# Todo List

## Objective`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,2 +1,3 @@
 # Todo List
+**No Newline**: This should fail validation`,
			expectedError: true, // Should fail validation due to missing newline
		},
		{
			name: "Diff with proper newline ending",
			currentContent: `# Todo List

## Objective`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,2 +1,3 @@
 # Todo List
+**With Newline**: This should work

## Objective
`,
			expectedError: false,
			expectedResult: `# Todo List
**With Newline**: This should work

## Objective`,
		},
		{
			name: "Missing diff headers (should fail validation)",
			currentContent: `# Todo List

## Objective`,
			diffContent: `@@ -1,2 +1,3 @@
 # Todo List
+**No Headers**: This should fail

## Objective
`,
			expectedError: true, // Should fail validation due to missing headers
		},
		{
			name: "Context mismatch (should fail patch)",
			currentContent: `# Todo List

## Objective
- Complete project analysis`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
+**Context Mismatch**: This should fail

## Different Content
`,
			expectedError: true, // Should fail due to context mismatch
		},
		{
			name: "Simplified diff format (should fail with patch command)",
			currentContent: `# Todo List

## Objective
- Complete project analysis
- Generate comprehensive report

## Notes
- Leverages tavily-search for comprehensive research`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ ... @@
 # Todo List
+**Simplified Patch**: This was added via simplified diff.

 ## Objective
`,
			expectedError: true, // Simplified diffs should fail - this is expected
		},
		{
			name: "Single addition only (simplified format - should fail)",
			currentContent: `# Todo List

## Objective
- Complete project analysis
- Generate comprehensive report

## Notes
- Leverages tavily-search for comprehensive research`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ ... @@
 # Todo List
+**Single Addition**: This is a test.

## Objective
- Complete project analysis
- Generate comprehensive report

## Notes
- Leverages tavily-search for comprehensive research
`,
			expectedError: true, // Simplified diffs should fail - this is expected
		},
		{
			name: "Empty diff (should fail validation)",
			currentContent: `# Todo List

## Objective
- Complete project analysis`,
			diffContent:   "",
			expectedError: true, // Empty diff should fail validation
		},
		{
			name: "Malformed diff",
			currentContent: `# Todo List

## Objective`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,2 +1,3 @@
 # Todo List
+**Malformed**: This should fail
## Objective
`,
			expectedError: true,
		},
		{
			name: "Context lines with minus instead of space (should be auto-corrected)",
			currentContent: `# Todo List

## Objective
- Complete project analysis`,
			diffContent: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
- Complete project analysis
+- Test patch: Added via AI agent
`,
			expectedError: false, // Should be auto-corrected and succeed
			expectedResult: `# Todo List

## Objective
- Complete project analysis
- Test patch: Added via AI agent`,
		},
		{
			name: "Real agent-generated diff pattern (should be auto-corrected)",
			currentContent: `## Notes
- Each todo builds on previous research to create comprehensive analysis
- Success criteria are measurable and tied to specific deliverables
- Dependencies ensure logical progression of analysis depth`,
			diffContent: `--- a/Tasks/Workflow-Testing/todo.md
+++ b/Tasks/Workflow-Testing/todo.md
@@ -200,3 +200,4 @@ - Each todo builds on previous research to create comprehensive analysis
 - Success criteria are measurable and tied to specific deliverables
 - Dependencies ensure logical progression of analysis depth
+- Test patch: Added via diff tool
`,
			expectedError: false, // Should be auto-corrected and succeed
			expectedResult: `## Notes
- Each todo builds on previous research to create comprehensive analysis
- Success criteria are measurable and tied to specific deliverables
- Dependencies ensure logical progression of analysis depth
- Test patch: Added via diff tool`,
		},
		{
			name: "Agent diff with invalid line references (should be auto-corrected)",
			currentContent: `## Notes
- Each todo builds on previous research to create comprehensive analysis
- Success criteria are measurable and tied to specific deliverables
- Dependencies ensure logical progression of analysis depth
-Updated for testing.`,
			diffContent: `--- a/Tasks/Workflow-Testing/todo.md
+++ b/Tasks/Workflow-Testing/todo.md
@@ -last,2 +last-1,1 @@
 - Dependencies ensure logical progression of analysis depth
-Updated for testing.`,
			expectedError: false, // Should be auto-corrected and succeed
			expectedResult: `## Notes
- Each todo builds on previous research to create comprehensive analysis
- Success criteria are measurable and tied to specific deliverables
- Dependencies ensure logical progression of analysis depth`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := applyDiffPatchFlexible(tt.currentContent, tt.diffContent)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.expectedResult != "" && result != tt.expectedResult {
				t.Errorf("Result mismatch.\nExpected:\n%s\n\nGot:\n%s", tt.expectedResult, result)
			}
		})
	}
}

// TestNormalizeLineEndings tests the line ending normalization function
func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "LF endings (no change)",
			input:    "line1\nline2\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "CRLF endings",
			input:    "line1\r\nline2\r\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "CR endings",
			input:    "line1\rline2\rline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "Mixed endings",
			input:    "line1\nline2\r\nline3\r",
			expected: "line1\nline2\nline3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLineEndings(tt.input)
			if result != tt.expected {
				t.Errorf("Expected: %q, Got: %q", tt.expected, result)
			}
		})
	}
}

// TestValidateDiffFormat tests the diff format validation function
func TestValidateDiffFormat(t *testing.T) {
	tests := []struct {
		name        string
		diffContent string
		expectError bool
	}{
		{
			name: "Valid diff format",
			diffContent: `--- a/file.md
+++ b/file.md
@@ -1,3 +1,4 @@
 # Header
+New line

 ## Section
`,
			expectError: false,
		},
		{
			name:        "Empty diff",
			diffContent: "",
			expectError: true,
		},
		{
			name: "Missing headers",
			diffContent: `@@ -1,2 +1,3 @@
 # Header
+New line`,
			expectError: true,
		},
		{
			name: "Missing hunk headers",
			diffContent: `--- a/file.md
+++ b/file.md
 # Header
+New line`,
			expectError: true,
		},
		{
			name: "Diff without newline ending",
			diffContent: `--- a/file.md
+++ b/file.md
@@ -1,2 +1,3 @@
 # Header
+New line`,
			expectError: true,
		},
		{
			name: "Too short diff",
			diffContent: `--- a/file.md
+++ b/file.md`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDiffFormat(tt.diffContent)
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestCorrectAgentGeneratedDiff tests the automatic correction of agent-generated diffs
func TestCorrectAgentGeneratedDiff(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Correct context lines with minus instead of space",
			input: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
- Complete project analysis
+- Test patch: Added via AI agent
`,
			expected: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
  Complete project analysis
+- Test patch: Added via AI agent
`,
		},
		{
			name: "No changes needed for correct diff",
			input: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
+**New addition**: Added via unified diff.

 ## Objective
`,
			expected: `--- a/todo.md
+++ b/todo.md
@@ -1,3 +1,4 @@
 # Todo List
+**New addition**: Added via unified diff.

 ## Objective
`,
		},
		{
			name: "Multiple context line corrections",
			input: `--- a/todo.md
+++ b/todo.md
@@ -1,4 +1,5 @@
 # Todo List
- Complete project analysis
- Generate comprehensive report
+- Test patch: Added via AI agent
`,
			expected: `--- a/todo.md
+++ b/todo.md
@@ -1,4 +1,5 @@
 # Todo List
  Complete project analysis
  Generate comprehensive report
+- Test patch: Added via AI agent
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a simple current content for testing
			currentContent := `# Todo List

## Objective
- Complete project analysis
- Generate comprehensive report`

			result := correctAgentGeneratedDiff(tt.input, currentContent)
			if result != tt.expected {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

// Tests for removed functions have been removed since we're using the simplified approach
