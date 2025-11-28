package handlers

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command("patch", "-u", tempFile.Name(), patchFile.Name())
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

	inHunk := false
	hunkStartIndex := 0
	contextLineCount := 0

	for i, line := range lines {
		// Check if we're entering a hunk
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			hunkStartIndex = len(corrected)
			contextLineCount = 0

			// Fix malformed hunk headers (missing closing @@ or content appended)
			if !strings.HasSuffix(line, "@@") {
				// Extract the hunk header part (before any appended content)
				// Look for the second @@ in the line
				firstAt := strings.Index(line, "@@")
				if firstAt != -1 {
					secondAt := strings.Index(line[firstAt+2:], "@@")
					if secondAt != -1 {
						hunkHeader := line[:firstAt+2+secondAt+2] // Include both @@
						corrected = append(corrected, hunkHeader)
						fmt.Printf("🔧 Fixed malformed hunk header: '%s' -> '%s'\n", line, hunkHeader)
					} else {
						// No second @@ found, just use the line as-is
						corrected = append(corrected, line)
					}
				} else {
					corrected = append(corrected, line)
				}
			} else {
				// Check for invalid line references like "last", "end", etc.
				if strings.Contains(line, "last") || strings.Contains(line, "end") || strings.Contains(line, "start") {
					// Replace invalid references with reasonable defaults
					fixedHeader := strings.ReplaceAll(line, "last", "1")
					fixedHeader = strings.ReplaceAll(fixedHeader, "end", "1")
					fixedHeader = strings.ReplaceAll(fixedHeader, "start", "1")
					corrected = append(corrected, fixedHeader)
					fmt.Printf("🔧 Fixed invalid line references: '%s' -> '%s'\n", line, fixedHeader)
				} else {
					corrected = append(corrected, line)
				}
			}
			continue
		}

		// Check if we're exiting a hunk (empty line or new hunk)
		if inHunk && (line == "" || strings.HasPrefix(line, "@@")) {
			// Update the hunk header with the correct context line count
			if contextLineCount > 0 {
				hunkHeader := corrected[hunkStartIndex]
				// Parse and update the hunk header
				// Format: @@ -startLine,lineCount +startLine,lineCount @@
				parts := strings.Fields(hunkHeader)
				if len(parts) >= 3 {
					oldRange := strings.TrimPrefix(parts[1], "-")
					newRange := strings.TrimPrefix(parts[2], "+")

					// Update the line count to match actual context lines
					if commaIndex := strings.Index(oldRange, ","); commaIndex != -1 {
						startLine := oldRange[:commaIndex]
						oldRange = startLine + "," + fmt.Sprintf("%d", contextLineCount)
					}
					if commaIndex := strings.Index(newRange, ","); commaIndex != -1 {
						startLine := newRange[:commaIndex]
						newRange = startLine + "," + fmt.Sprintf("%d", contextLineCount+1) // +1 for the addition
					}

					corrected[hunkStartIndex] = fmt.Sprintf("@@ -%s +%s @@", oldRange, newRange)
					fmt.Printf("🔧 Updated hunk header: %s\n", corrected[hunkStartIndex])
				}
			}

			inHunk = false
			if line != "" {
				corrected = append(corrected, line)
			}
			continue
		}

		// Within a hunk, try to correct common patterns
		if inHunk {
			// If line starts with '-' but exists in current content, it's likely a context line
			if strings.HasPrefix(line, "-") && len(line) > 1 {
				content := line[1:] // Remove the '-'

				// Check if this content exists in the current file (indicating it's context, not removal)
				foundInFile := false
				for _, fileLine := range currentLines {
					// Check if the content (without leading space) matches the file line (without leading minus)
					contentTrimmed := strings.TrimSpace(content)
					// Remove leading minus from file line if present
					fileLineToCheck := fileLine
					if strings.HasPrefix(fileLine, "-") {
						fileLineToCheck = fileLine[1:]
					}
					fileLineTrimmed := strings.TrimSpace(fileLineToCheck)
					if contentTrimmed == fileLineTrimmed {
						foundInFile = true
						break
					}
				}

				if foundInFile {
					// This is likely a context line that agent marked as removal
					corrected = append(corrected, " "+content)
					contextLineCount++
					fmt.Printf("🔧 Corrected line %d: '-%s' -> ' %s' (found in current file)\n", i+1, content, content)
					continue
				}
			}

			// Count context lines (lines starting with space)
			if strings.HasPrefix(line, " ") {
				contextLineCount++
			}
		}

		corrected = append(corrected, line)
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

	// First, try to correct common agent-generated patterns
	correctedDiff := correctAgentGeneratedDiff(diffContent, currentContent)
	if correctedDiff != diffContent {
		fmt.Printf("🔧 Applied automatic corrections to agent-generated diff\n")
		fmt.Printf("🔍 Corrected diff:\n%s\n", correctedDiff)
		// Try the corrected diff first
		result, err := applyDiffPatch(currentContent, correctedDiff)
		if err == nil {
			fmt.Printf("✅ Corrected diff applied successfully\n")
			return result, nil
		}
		fmt.Printf("⚠️ Corrected diff failed, trying original: %v\n", err)
	}

	// If correction didn't help, try a fallback approach for agent-generated diffs
	fmt.Printf("🔍 Trying fallback approach for agent-generated diffs\n")
	fallbackResult, err := applyAgentGeneratedDiffFallback(currentContent, diffContent)
	if err == nil {
		fmt.Printf("✅ Fallback approach succeeded\n")
		return fallbackResult, nil
	}
	fmt.Printf("⚠️ Fallback approach failed: %v\n", err)

	// If all else fails, try original
	fmt.Printf("🔍 Trying original diff format\n")
	return applyDiffPatch(currentContent, diffContent)
}

// applyAgentGeneratedDiffFallback handles agent-generated diffs by parsing the intent
func applyAgentGeneratedDiffFallback(currentContent, diffContent string) (string, error) {
	lines := strings.Split(diffContent, "\n")

	// Find additions and removals in the diff
	var additions []string
	var removals []string
	inHunk := false

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}

		if inHunk && strings.HasPrefix(line, "+") {
			// This is an addition
			content := line[1:] // Remove the '+'
			additions = append(additions, content)
		} else if inHunk && strings.HasPrefix(line, "-") {
			// This is a removal
			content := line[1:] // Remove the '-'
			removals = append(removals, content)
		} else if inHunk && line == "" {
			inHunk = false
		}
	}

	if len(additions) == 0 && len(removals) == 0 {
		return "", fmt.Errorf("no additions or removals found in diff")
	}

	// Start with current content
	result := currentContent

	// Apply removals first
	if len(removals) > 0 {
		resultLines := strings.Split(result, "\n")
		var filteredLines []string

		for _, line := range resultLines {
			shouldRemove := false
			for _, removal := range removals {
				if strings.TrimSpace(line) == strings.TrimSpace(removal) {
					shouldRemove = true
					break
				}
			}
			if !shouldRemove {
				filteredLines = append(filteredLines, line)
			}
		}

		result = strings.Join(filteredLines, "\n")
		fmt.Printf("🔧 Removed %d lines via fallback approach\n", len(removals))
	}

	// Apply additions
	if len(additions) > 0 {
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
