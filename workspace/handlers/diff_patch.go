package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"workspace/models"
	"workspace/utils"

	"github.com/gin-gonic/gin"
	// "github.com/sergi/go-diff/diffmatchpatch" // Available for future use
	"github.com/spf13/viper"
)

// DiffPatchDocument handles PATCH /api/documents/*filepath/diff
func DiffPatchDocument(c *gin.Context) {
	filePathParam := c.Param("filepath")
	var req models.DiffPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input path to ensure it's relative
	filePathParam = utils.SanitizeInputPath(filePathParam, docsDir)

	filePath := filepath.Join(docsDir, filePathParam)

	// Validate file path for security
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Document not found",
			Error:   "Document not found: " + filePathParam,
		})
		return
	}

	// Read current file content
	currentContent, err := os.ReadFile(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to read document",
			Error:   err.Error(),
		})
		return
	}

	// Apply diff patch - try flexible approach first, fallback to strict patch command
	newContent, err := applyDiffPatchFlexible(string(currentContent), req.Diff)
	if err != nil {
		// Provide comprehensive error details with suggestions
		errorDetails := map[string]interface{}{
			"error":         err.Error(),
			"filepath":      filePathParam,
			"diff_provided": req.Diff,
		}

		// Add helpful suggestions based on common errors
		var suggestions []string
		if strings.Contains(err.Error(), "malformed patch") {
			suggestions = []string{
				"Use read_workspace_file first to see exact current content",
				"Context lines (starting with SPACE) must exactly match the file",
				"Hunk headers (@@) must show correct line numbers",
				"Use proper unified diff format with ---/+++ headers",
				"Generate diffs like 'diff -U0' would produce",
				"Ensure diff ends with a newline character",
				"CRITICAL: Context lines must start with SPACE ( ), not minus (-)!",
			}
		} else if strings.Contains(err.Error(), "unexpected end") {
			suggestions = []string{
				"All context lines are included",
				"The diff ends properly with a newline",
				"No truncated lines in the diff",
				"Generate complete unified diff format",
				"Use read_workspace_file to get exact file content",
			}
		} else if strings.Contains(err.Error(), "diff validation failed") {
			suggestions = []string{
				"Diff has proper headers (--- a/file, +++ b/file)",
				"At least one hunk header (@@ -start,count +start,count @@)",
				"Diff ends with a newline character",
				"Use read_workspace_file first to get exact content",
			}
		} else if strings.Contains(err.Error(), "patch hunk failed to apply") {
			suggestions = []string{
				"Use read_workspace_file first to see exact current content",
				"Copy context lines EXACTLY from the file (including spaces/tabs)",
				"Verify line numbers in hunk headers match actual file",
				"Ensure no extra whitespace or missing characters",
				"Test with a simple single-line addition first",
			}
		} else {
			suggestions = []string{
				"Use read_workspace_file first to see exact current content",
				"Ensure diff format follows unified diff standard",
				"Check that context lines match file content exactly",
				"Verify hunk headers have correct line numbers",
			}
		}

		errorDetails["suggestions"] = suggestions

		c.JSON(http.StatusBadRequest, models.APIResponse[models.DiffPatchResponse]{
			Success: false,
			Message: "Failed to apply diff patch",
			Error:   fmt.Sprintf("Failed to apply diff patch: %s", err.Error()),
			Data: models.DiffPatchResponse{
				Applied:      false,
				Suggestions:  suggestions,
				ErrorDetails: errorDetails,
			},
		})
		return
	}

	// Write updated content back to file
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to update document",
			Error:   err.Error(),
		})
		return
	}

	// Queue file for semantic processing (update embeddings)
	if fileProcessor := GetFileProcessor(); fileProcessor != nil {
		go fileProcessor.QueueJob(filePathParam, newContent, "update")
	}

	// Handle git operations if commit message provided
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	// Return simple success response
	c.JSON(http.StatusOK, models.APIResponse[models.DiffPatchResponse]{
		Success: true,
		Message: "Document diff-patched successfully",
		Data:    models.DiffPatchResponse{Applied: true},
	})
}

// normalizeLineEndings converts all line endings to LF for consistent patch processing
func normalizeLineEndings(content string) string {
	// Replace CRLF (\r\n) with LF (\n)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	// Replace CR (\r) with LF (\n)
	content = strings.ReplaceAll(content, "\r", "\n")
	return content
}

// validateDiffFormat performs basic validation on the diff format
func validateDiffFormat(diffContent string) error {
	lines := strings.Split(diffContent, "\n")
	if len(lines) < 3 {
		return fmt.Errorf("diff too short - must have at least headers and one hunk")
	}

	// Check for proper headers
	if !strings.HasPrefix(lines[0], "--- ") || !strings.HasPrefix(lines[1], "+++ ") {
		return fmt.Errorf("missing or malformed diff headers (---/+++)")
	}

	// Check for at least one hunk header
	foundHunk := false
	inHunk := false
	for i, line := range lines {
		if strings.HasPrefix(line, "@@") && strings.HasSuffix(line, "@@") {
			foundHunk = true
			inHunk = true
			continue
		}

		// Check diff lines within hunks
		if inHunk && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+")) {
			// This is a valid diff line
			continue
		} else if inHunk && line == "" {
			// Empty line ends the hunk
			inHunk = false
		} else if inHunk && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "+") && line != "" {
			// Invalid line in hunk
			return fmt.Errorf("malformed diff line %d: %q - diff lines must start with space (context), - (removal), or + (addition)", i+1, line)
		}
	}

	if !foundHunk {
		return fmt.Errorf("no hunk headers found (lines starting with @@)")
	}

	// Check that diff ends with newline
	if !strings.HasSuffix(diffContent, "\n") {
		return fmt.Errorf("diff must end with a newline character")
	}

	return nil
}

// applyDiffPatch applies a unified diff to the file content using the standard patch command
func applyDiffPatch(currentContent, diffContent string) (string, error) {
	// Normalize line endings for consistent processing
	currentContent = normalizeLineEndings(currentContent)
	diffContent = normalizeLineEndings(diffContent)

	// Ensure diff ends with a newline
	if !strings.HasSuffix(diffContent, "\n") {
		diffContent += "\n"
	}

	// Validate diff format before applying
	if err := validateDiffFormat(diffContent); err != nil {
		return "", fmt.Errorf("diff validation failed: %w", err)
	}

	fmt.Printf("🔍 Applying diff patch with normalized line endings\n")

	// Create temporary files for the patch command
	tempFile, err := os.CreateTemp("", "file_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	patchFile, err := os.CreateTemp("", "patch_*.diff")
	if err != nil {
		return "", fmt.Errorf("failed to create temp patch file: %w", err)
	}
	defer os.Remove(patchFile.Name())

	// Write current content to temp file
	if _, err := tempFile.WriteString(currentContent); err != nil {
		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}
	tempFile.Close()

	// Write diff content to patch file
	if _, err := patchFile.WriteString(diffContent); err != nil {
		return "", fmt.Errorf("failed to write to patch file: %w", err)
	}
	patchFile.Close()

	// Apply patch using the standard patch command
	// Use -F 3 to be more lenient with context matches (fuzz factor)
	cmd := exec.Command("patch", "-u", "-F", "3", tempFile.Name(), patchFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Provide more specific error messages based on patch output
		outputStr := string(output)
		if strings.Contains(outputStr, "malformed patch") {
			return "", fmt.Errorf("malformed patch: %s", outputStr)
		} else if strings.Contains(outputStr, "unexpected end") {
			return "", fmt.Errorf("unexpected end of file in patch: %s", outputStr)
		} else if strings.Contains(outputStr, "Hunk") && strings.Contains(outputStr, "FAILED") {
			return "", fmt.Errorf("patch hunk failed to apply: %s", outputStr)
		}
		return "", fmt.Errorf("patch command failed: %w, output: %s", err, outputStr)
	}

	// Read the patched content
	patchedContent, err := os.ReadFile(tempFile.Name())
	if err != nil {
		return "", fmt.Errorf("failed to read patched file: %w", err)
	}

	return string(patchedContent), nil
}

// correctAgentGeneratedDiff attempts to fix common agent-generated diff patterns
func correctAgentGeneratedDiff(diffContent, currentContent string) string {
	lines := strings.Split(diffContent, "\n")
	corrected := make([]string, 0, len(lines))
	currentLines := strings.Split(currentContent, "\n")

	type hunkInfo struct {
		index    int
		oldStart string
		newStart string
		oldCount int
		newCount int
	}

	var currentHunk *hunkInfo

	for i, line := range lines {
		// Check if we're entering a hunk
		if strings.HasPrefix(line, "@@") {
			// Finalize previous hunk if any
			if currentHunk != nil {
				corrected[currentHunk.index] = fmt.Sprintf("@@ -%s,%d +%s,%d @@",
					currentHunk.oldStart, currentHunk.oldCount,
					currentHunk.newStart, currentHunk.newCount)
			}

			// Parse new hunk header
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				oldRange := strings.TrimPrefix(parts[1], "-")
				newRange := strings.TrimPrefix(parts[2], "+")

				oldStart := oldRange
				if commaIdx := strings.Index(oldRange, ","); commaIdx != -1 {
					oldStart = oldRange[:commaIdx]
				}
				newStart := newRange
				if commaIdx := strings.Index(newRange, ","); commaIdx != -1 {
					newStart = newRange[:commaIdx]
				}

				// Fix invalid line references like "last", "end", etc.
				if oldStart == "last" || oldStart == "end" || oldStart == "start" {
					oldStart = "1"
				}
				if newStart == "last" || newStart == "end" || newStart == "start" {
					newStart = "1"
				}

				currentHunk = &hunkInfo{
					index:    len(corrected),
					oldStart: oldStart,
					newStart: newStart,
					oldCount: 0,
					newCount: 0,
				}
				corrected = append(corrected, line) // Placeholder to be updated
			} else {
				corrected = append(corrected, line)
				currentHunk = nil
			}
			continue
		}

		if currentHunk != nil {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
				if strings.HasPrefix(line, " ") {
					// Try to find matching line in file to fix whitespace/indentation
					content := line[1:]
					trimmedContent := strings.TrimSpace(content)
					if len(trimmedContent) > 0 {
						for _, fl := range currentLines {
							if strings.TrimSpace(fl) == trimmedContent {
								line = " " + fl // Use the actual line from the file
								break
							}
						}
					}
					currentHunk.oldCount++
					currentHunk.newCount++
				} else if strings.HasPrefix(line, "-") {
					currentHunk.oldCount++
				} else if strings.HasPrefix(line, "+") {
					currentHunk.newCount++
				}
				corrected = append(corrected, line)
			} else if len(strings.TrimSpace(line)) > 0 && !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "@@") {
				// Line without prefix - try to guess if it's context or addition
				trimmedContent := strings.TrimSpace(line)
				foundInFile := false
				for _, fl := range currentLines {
					if strings.TrimSpace(fl) == trimmedContent {
						foundInFile = true
						line = " " + fl // Treat as context
						break
					}
				}
				if !foundInFile {
					if !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") {
						line = "+ " + line // Treat as addition
					}
					currentHunk.newCount++
				} else {
					currentHunk.oldCount++
					currentHunk.newCount++
				}
				corrected = append(corrected, line)
			} else if line == "" && i < len(lines)-1 {
				// Treat empty line as context if it's inside a hunk
				currentHunk.oldCount++
				currentHunk.newCount++
				corrected = append(corrected, " ")
			} else {
				// Non-diff line ends the hunk
				corrected[currentHunk.index] = fmt.Sprintf("@@ -%s,%d +%s,%d @@",
					currentHunk.oldStart, currentHunk.oldCount,
					currentHunk.newStart, currentHunk.newCount)
				currentHunk = nil
				corrected = append(corrected, line)
			}
		} else {
			corrected = append(corrected, line)
		}
	}

	// Finalize last hunk
	if currentHunk != nil {
		corrected[currentHunk.index] = fmt.Sprintf("@@ -%s,%d +%s,%d @@",
			currentHunk.oldStart, currentHunk.oldCount,
			currentHunk.newStart, currentHunk.newCount)
	}

	result := strings.Join(corrected, "\n")
	// Ensure the result ends with a newline
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

// applyDiffPatchFlexible tries multiple approaches to apply diffs
func applyDiffPatchFlexible(currentContent, diffContent string) (string, error) {
	fmt.Printf("🔍 Attempting flexible diff patch approach\n")

	var result string
	var err error

	// First, try to correct common agent-generated patterns
	correctedDiff := correctAgentGeneratedDiff(diffContent, currentContent)
	if correctedDiff != diffContent {
		fmt.Printf("🔧 Applied automatic corrections to agent-generated diff\n")
		fmt.Printf("🔍 Corrected diff:\n%s\n", correctedDiff)
		// Try the corrected diff first
		result, err = applyDiffPatch(currentContent, correctedDiff)
		if err == nil {
			fmt.Printf("✅ Corrected diff applied successfully\n")
			return validateAndRepairJSON(result), nil
		}
		fmt.Printf("⚠️ Corrected diff failed, trying original: %v\n", err)
	}

	// Try the original diff
	result, err = applyDiffPatch(currentContent, diffContent)
	if err == nil {
		fmt.Printf("✅ Original diff applied successfully\n")
		return validateAndRepairJSON(result), nil
	}

	fmt.Printf("⚠️ Original diff failed, trying fallback: %v\n", err)

	// Fallback approach
	result, err = applyAgentGeneratedDiffFallback(currentContent, diffContent)
	if err != nil {
		return "", fmt.Errorf("fallback approach failed: %w", err)
	}

	fmt.Printf("✅ Fallback approach succeeded\n")
	return validateAndRepairJSON(result), nil
}

// validateAndRepairJSON attempts to validate and repair JSON content
func validateAndRepairJSON(content string) string {
	// Clean markdown artifacts
	cleaned := strings.TrimSpace(content)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) > 2 {
			// Remove first and last lines (the ``` markers)
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
			cleaned = strings.TrimSpace(cleaned)
		}
	}

	if !couldBeJSON(cleaned) {
		return content
	}

	var finalJs interface{}
	if err := json.Unmarshal([]byte(cleaned), &finalJs); err == nil {
		if indented, err := json.MarshalIndent(finalJs, "", "  "); err == nil {
			fmt.Printf("✅ Result is valid JSON and has been re-formatted\n")
			return string(indented) + "\n"
		}
	}

	// Try to repair common JSON issues
	reTrailingComma := regexp.MustCompile(`,\s*([}\]])`)
	repaired := reTrailingComma.ReplaceAllString(cleaned, "$1")

	// Try to add missing commas between lines that look like they should be separated by commas
	// Broad version: match line ending in alphanumeric, quote, brace, or bracket 
	// and next line starting with alphanumeric, quote, brace, or bracket
	reMissingComma := regexp.MustCompile(`([a-zA-Z\d"\}\]])\s*\n\s*([a-zA-Z\d"\{\[])`)
	repaired = reMissingComma.ReplaceAllString(repaired, "$1,\n$2")

	// Fix double commas that might have been introduced
	reDoubleComma := regexp.MustCompile(`,\s*,`)
	repaired = reDoubleComma.ReplaceAllString(repaired, ",")

	var repairErr error
	if err := json.Unmarshal([]byte(repaired), &finalJs); err == nil {
		if indented, err := json.MarshalIndent(finalJs, "", "  "); err == nil {
			fmt.Printf("✅ Repaired and re-formatted result as valid JSON\n")
			return string(indented) + "\n"
		}
		repairErr = err
	} else {
		repairErr = err
	}

	fmt.Printf("⚠️ Failed to repair JSON: %v\n", repairErr)
	return cleaned
}

// applyAgentGeneratedDiffFallback handles agent-generated diffs by parsing the intent

func applyAgentGeneratedDiffFallback(currentContent, diffContent string) (string, error) {

	fmt.Printf("🔍 Trying fallback approach for agent-generated diffs\n")

	

	lines := strings.Split(diffContent, "\n")

	resultLines := strings.Split(currentContent, "\n")

	

	type hunk struct {

		lines []string

	}

	

	var hunks []hunk

	var currentHunk *hunk

	

	for _, line := range lines {

		if strings.HasPrefix(line, "@@") {

			if currentHunk != nil {

				hunks = append(hunks, *currentHunk)

			}

			currentHunk = &hunk{}

			continue

		}

		

		if currentHunk != nil {

			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {

				currentHunk.lines = append(currentHunk.lines, line)

			} else if line == "" && len(currentHunk.lines) > 0 {

				// Empty line inside hunk is treated as context

				currentHunk.lines = append(currentHunk.lines, " ")

			}

		}

	}

	if currentHunk != nil {

		hunks = append(hunks, *currentHunk)

	}



	if len(hunks) == 0 {

		return "", fmt.Errorf("no hunks found in diff")

	}



	// Apply hunks one by one

	for _, h := range hunks {

		// For each hunk, try to find a match in the current resultLines

		// A match is where all ' ' and '-' lines in the hunk match the lines in the file

		

		matchIndex := -1

		

		// Collect expected lines (context and removals)

		var expectedLines []string

		for _, hl := range h.lines {

			if !strings.HasPrefix(hl, "+") {

				expectedLines = append(expectedLines, hl[1:])

			}

		}

		

				if len(expectedLines) == 0 {

		

					// Pure addition hunk, apply to bottom

		

					var additions []string

		

					for _, hl := range h.lines {

		

						if strings.HasPrefix(hl, "+") {

		

							additions = append(additions, hl[1:])

		

						}

		

					}

		

					newResult, _ := applyAdditionsToBottom(strings.Join(resultLines, "\n"), additions)

		

					resultLines = strings.Split(newResult, "\n")

		

					continue

		

				}

		

		

		

						fmt.Printf("🔍 Attempting to match hunk with %d expected lines against %d file lines\n", len(expectedLines), len(resultLines))

		

		

		

				

		

		

		

						// Fuzzy match: find position with minimum mismatches

		

		

		

						bestMatchIndex := -1

		

		

		

						minMismatches := len(expectedLines) + 1

		

		

		

						

		

		

		

								maxAllowedMismatches := 0

		

		

		

						

		

		

		

								if len(expectedLines) >= 4 {

		

		

		

						

		

		

		

									// For larger hunks, allow ~15-20% mismatch

		

		

		

						

		

		

		

									maxAllowedMismatches = len(expectedLines) / 6

		

		

		

						

		

		

		

									if maxAllowedMismatches < 1 {

		

		

		

						

		

		

		

										maxAllowedMismatches = 1

		

		

		

						

		

		

		

									}

		

		

		

						

		

		

		

									if maxAllowedMismatches > 3 {

		

		

		

						

		

		

		

										maxAllowedMismatches = 3 // Cap at 3 mismatches max

		

		

		

						

		

		

		

									}

		

		

		

						

		

		

		

								}

		

		

		

						

		

		

		

						

		

		

		

				

		

		

		

						for i := 0; i <= len(resultLines)-len(expectedLines); i++ {

		

		

		

							mismatches := 0

		

		

		

							for j, el := range expectedLines {

		

		

		

								if strings.TrimSpace(resultLines[i+j]) != strings.TrimSpace(el) {

		

		

		

									mismatches++

		

		

		

									if mismatches > maxAllowedMismatches {

		

		

		

										break

		

		

		

									}

		

		

		

								}

		

		

		

							}

		

		

		

							if mismatches < minMismatches {

		

		

		

								minMismatches = mismatches

		

		

		

								bestMatchIndex = i

		

		

		

							}

		

		

		

							if mismatches == 0 {

		

		

		

								break // Perfect match!

		

		

		

							}

		

		

		

						}

		

		

		

						

		

		

		

						if minMismatches <= maxAllowedMismatches {

		

		

		

							matchIndex = bestMatchIndex

		

		

		

							fmt.Printf("✅ Found fuzzy match at line %d with %d mismatches\n", matchIndex+1, minMismatches)

		

		

		

						}

		

		

		

				

		

		

		

						if matchIndex != -1 {

		

		

		

				

			// Found a match! Apply changes

			var newResultLines []string

			newResultLines = append(newResultLines, resultLines[:matchIndex]...)

			

			// Track which expected line we are on

			expIdx := 0

			for _, hl := range h.lines {

				if strings.HasPrefix(hl, " ") {

					// Context line, keep it

					newResultLines = append(newResultLines, resultLines[matchIndex+expIdx])

					expIdx++

				} else if strings.HasPrefix(hl, "-") {

					// Removal line, skip it

					expIdx++;

				} else if strings.HasPrefix(hl, "+") {

					// Addition line, add it

					newResultLines = append(newResultLines, hl[1:])

				}

			}

			

			newResultLines = append(newResultLines, resultLines[matchIndex+expIdx:]...)

			resultLines = newResultLines

			fmt.Printf("✅ Applied hunk with %d lines via fallback match\n", len(h.lines))

		} else {

			// No match found for the whole hunk, try falling back to just additions

			fmt.Printf("⚠️ Could not find match for hunk, falling back to simple additions\n")

			var additions []string

			for _, hl := range h.lines {

				if strings.HasPrefix(hl, "+") {

					additions = append(additions, hl[1:])

				}

			}

			newResult, _ := applyAdditionsToBottom(strings.Join(resultLines, "\n"), additions)

			resultLines = strings.Split(newResult, "\n")

		}

	}



	result := strings.Join(resultLines, "\n")
	return result, nil
}

	

	

	

	// couldBeJSON is a helper to check if content might be JSON

	

	func couldBeJSON(content string) bool {

	

		trimmed := strings.TrimSpace(content)

	

		return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")

	

	}

	

	

	

	// isJSON is a helper to check if content is valid JSON

	

	func isJSON(content string) bool {

	

		var js interface{}

	

		return json.Unmarshal([]byte(content), &js) == nil

	

	}

	

	



// applyAdditionsToBottom appends additions to the end of the content, 

// but tries to be smart about JSON structure.

func applyAdditionsToBottom(content string, additions []string) (string, error) {

	if len(additions) == 0 {

		return content, nil

	}



	result := content

	

	// Check if it's JSON

	var js interface{}

	isJSON := json.Unmarshal([]byte(result), &js) == nil



	if isJSON {

		trimmedResult := strings.TrimSpace(result)

		if strings.HasSuffix(trimmedResult, "}") {

			// Insert before the last closing brace

			lastBraceIndex := strings.LastIndex(result, "}")

			prefix := result[:lastBraceIndex]

			suffix := result[lastBraceIndex:]



			// Try to see if we need a comma

			trimmedPrefix := strings.TrimSpace(prefix)

			firstAddition := ""

			if len(additions) > 0 {

				firstAddition = strings.TrimSpace(additions[0])

			}

			if len(trimmedPrefix) > 0 && !strings.HasSuffix(trimmedPrefix, "{") && !strings.HasSuffix(trimmedPrefix, ",") && !strings.HasSuffix(trimmedPrefix, "[") && !strings.HasPrefix(firstAddition, ",") {

				prefix += ","

			}

			if !strings.HasSuffix(prefix, "\n") {

				prefix += "\n"

			}



			for i, addition := range additions {

				prefix += addition

				// Add comma between additions if needed

				if i < len(additions)-1 {

					trimmedAddition := strings.TrimSpace(addition)

					if !strings.HasSuffix(trimmedAddition, ",") && !strings.HasSuffix(trimmedAddition, "{") && !strings.HasSuffix(trimmedAddition, "[") {

						prefix += ","

					}

				}

				prefix += "\n"

			}



			result = prefix + suffix

			fmt.Printf("🔧 Inserted %d lines before last '}' for JSON fallback\n", len(additions))

		} else if strings.HasSuffix(trimmedResult, "]") {

			// Similar for array

			lastBracketIndex := strings.LastIndex(result, "]")

			prefix := result[:lastBracketIndex]

			suffix := result[lastBracketIndex:]



			// Try to see if we need a comma

			trimmedPrefix := strings.TrimSpace(prefix)

			firstAddition := ""

			if len(additions) > 0 {

				firstAddition = strings.TrimSpace(additions[0])

			}

			if len(trimmedPrefix) > 0 && !strings.HasSuffix(trimmedPrefix, "[") && !strings.HasSuffix(trimmedPrefix, ",") && !strings.HasSuffix(trimmedPrefix, "{") && !strings.HasPrefix(firstAddition, ",") {

				prefix += ","

			}

			if !strings.HasSuffix(prefix, "\n") {

				prefix += "\n"

			}



			for i, addition := range additions {

				prefix += addition

				// Add comma between additions if needed

				if i < len(additions)-1 {

					trimmedAddition := strings.TrimSpace(addition)

					if !strings.HasSuffix(trimmedAddition, ",") && !strings.HasSuffix(trimmedAddition, "{") && !strings.HasSuffix(trimmedAddition, "[") {

						prefix += ","

					}

				}

				prefix += "\n"

			}



			result = prefix + suffix

			fmt.Printf("🔧 Inserted %d lines before last ']' for JSON fallback\n", len(additions))

		} else {

			// Fallback to appending

			if !strings.HasSuffix(result, "\n") {

				result += "\n"

			}

			for _, addition := range additions {

				result += addition + "\n"

			}

			fmt.Printf("🔧 Appended %d lines to non-object/array JSON via fallback approach\n", len(additions))

		}



				// Final attempt to validate and pretty-print if it's JSON



				var finalJs interface{}



				if err := json.Unmarshal([]byte(result), &finalJs); err == nil {



					if indented, err := json.MarshalIndent(finalJs, "", "  "); err == nil {



						result = string(indented) + "\n"



						fmt.Printf("✅ Re-formatted fallback result as valid JSON\n")



					}



				} else {
					// Try to repair common JSON issues
					reTrailingComma := regexp.MustCompile(`,\s*([}\]])`)
					repaired := reTrailingComma.ReplaceAllString(result, "$1")

					// Try to add missing commas between lines that look like they should be separated by commas
					// This matches a line ending in a value (not comma, brace, or bracket) 
					// followed by a line starting with a new value or key (not closing brace or bracket)
					reMissingComma := regexp.MustCompile(`([^,\[{\s])\n\s*([^}\]\s])`)
					repaired = reMissingComma.ReplaceAllString(repaired, "$1,\n$2")

					if err := json.Unmarshal([]byte(repaired), &finalJs); err == nil {
						if indented, err := json.MarshalIndent(finalJs, "", "  "); err == nil {
							result = string(indented) + "\n"
							fmt.Printf("✅ Repaired and re-formatted fallback result as valid JSON\n")
						}
					} else {
						fmt.Printf("⚠️ Failed to repair JSON: %v\n", err)
					}
				}



		

	} else {

		// Not JSON, just append

		if !strings.HasSuffix(result, "\n") {

			result += "\n"

		}

		for _, addition := range additions {

			result += addition + "\n"

		}

		fmt.Printf("🔧 Added %d lines via fallback approach\n", len(additions))

	}

	

	return result, nil

}



// ApplyDiffPatchDirect is an exported function for testing that applies a diff patch directly
// without going through the HTTP API. This allows tests to use the same diff patching logic.
func ApplyDiffPatchDirect(currentContent, diffContent string) (string, error) {
	return applyDiffPatchFlexible(currentContent, diffContent)
}
