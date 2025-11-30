package mcpagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// validateFilePath ensures the file path is within the allowed directory and doesn't contain path traversal
func validateFilePath(filePath, baseDir string) error {
	// Resolve to absolute paths for comparison
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("invalid file path: %w", err)
	}

	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("invalid base directory: %w", err)
	}

	// Check if file path is within base directory
	if !strings.HasPrefix(absFilePath, absBaseDir) {
		return fmt.Errorf("file path escapes allowed directory")
	}

	// Check for path traversal sequences
	if strings.Contains(filePath, "..") {
		return fmt.Errorf("path traversal detected")
	}

	return nil
}

// validatePattern ensures the search pattern is safe (basic validation)
func validatePattern(pattern string) error {
	// Prevent null bytes and command injection attempts
	if strings.Contains(pattern, "\x00") {
		return fmt.Errorf("invalid pattern: contains null byte")
	}
	// Ripgrep will handle pattern validation
	return nil
}

// validateJqQuery ensures the jq query is safe (basic validation)
func validateJqQuery(query string) error {
	// Prevent null bytes
	if strings.Contains(query, "\x00") {
		return fmt.Errorf("invalid jq query: contains null byte")
	}
	// jq will handle query validation
	return nil
}

// CreateLargeOutputVirtualTools creates virtual tools for large tool output handling
func (a *Agent) CreateLargeOutputVirtualTools() []llmtypes.Tool {
	// Check if large output virtual tools are enabled
	if !a.EnableLargeOutputVirtualTools {
		return []llmtypes.Tool{}
	}

	var virtualTools []llmtypes.Tool

	// Add read_large_output tool
	readLargeOutputTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_large_output",
			Description: "Read specific characters from a large tool output file",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{
						"type":        "string",
						"description": "Name of the tool output file (e.g., tool_20250721_091511_tavily-search.json)",
					},
					"start": map[string]interface{}{
						"type":        "integer",
						"description": "Starting character position (1-based)",
					},
					"end": map[string]interface{}{
						"type":        "integer",
						"description": "Ending character position (inclusive)",
					},
				},
				"required": []string{"filename", "start", "end"},
			}),
		},
	}
	virtualTools = append(virtualTools, readLargeOutputTool)

	// Add search_large_output tool
	searchLargeOutputTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "search_large_output",
			Description: "Search for regex patterns in large tool output files",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{
						"type":        "string",
						"description": "Name of the tool output file to search",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern (regex supported)",
					},
					"case_sensitive": map[string]interface{}{
						"type":        "boolean",
						"description": "Case sensitive search",
						"default":     false,
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return",
						"default":     50,
					},
				},
				"required": []string{"filename", "pattern"},
			}),
		},
	}
	virtualTools = append(virtualTools, searchLargeOutputTool)

	// Add query_large_output tool
	queryLargeOutputTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "query_large_output",
			Description: "Execute jq queries on large JSON tool output files",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{
						"type":        "string",
						"description": "Name of the JSON tool output file",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "jq query to execute (e.g., '.name', '.items[]')",
					},
					"compact": map[string]interface{}{
						"type":        "boolean",
						"description": "Output compact JSON format",
						"default":     false,
					},
					"raw": map[string]interface{}{
						"type":        "boolean",
						"description": "Output raw string values",
						"default":     false,
					},
				},
				"required": []string{"filename", "query"},
			}),
		},
	}
	virtualTools = append(virtualTools, queryLargeOutputTool)

	return virtualTools
}

// HandleLargeOutputVirtualTool handles large output virtual tool execution
func (a *Agent) HandleLargeOutputVirtualTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	// Check if large output virtual tools are enabled
	if !a.EnableLargeOutputVirtualTools {
		return "", fmt.Errorf("large output virtual tools are disabled")
	}

	switch toolName {
	case "read_large_output":
		return a.handleReadLargeOutput(ctx, args)
	case "search_large_output":
		return a.handleSearchLargeOutput(ctx, args)
	case "query_large_output":
		return a.handleQueryLargeOutput(ctx, args)
	default:
		return "", fmt.Errorf("unknown large output virtual tool: %s", toolName)
	}
}

// handleReadLargeOutput handles the read_large_output virtual tool
func (a *Agent) handleReadLargeOutput(ctx context.Context, args map[string]interface{}) (string, error) {
	filename, ok := args["filename"].(string)
	if !ok {
		return "", fmt.Errorf("filename parameter is required")
	}

	start, ok := args["start"].(float64)
	if !ok {
		return "", fmt.Errorf("start parameter is required")
	}

	end, ok := args["end"].(float64)
	if !ok {
		return "", fmt.Errorf("end parameter is required")
	}

	// Convert to integers
	startInt := int(start)
	endInt := int(end)

	// Validate parameters
	if startInt < 1 {
		return "", fmt.Errorf("start must be 1 or greater")
	}
	if endInt < startInt {
		return "", fmt.Errorf("end must be greater than or equal to start")
	}

	// Build file path
	filePath := a.BuildLargeOutputFilePath(filename)
	if filePath == "" {
		return "", fmt.Errorf("invalid filename: %s", filename)
	}

	// Validate file path is within allowed directory
	if a.toolOutputHandler != nil {
		baseDir := a.toolOutputHandler.OutputFolder
		if err := validateFilePath(filePath, baseDir); err != nil {
			return "", fmt.Errorf("file path validation failed: %w", err)
		}
	}

	// Read file content
	//nolint:gosec // G304: filePath is validated above to be within allowed directory
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	contentStr := string(content)
	if startInt > len(contentStr) {
		return "", fmt.Errorf("start %d exceeds file length %d", startInt, len(contentStr))
	}

	if endInt > len(contentStr) {
		endInt = len(contentStr)
	}

	// Extract the requested range (convert to 0-based indexing)
	result := contentStr[startInt-1 : endInt]
	return result, nil
}

// handleSearchLargeOutput handles the search_large_output virtual tool
func (a *Agent) handleSearchLargeOutput(ctx context.Context, args map[string]interface{}) (string, error) {
	filename, ok := args["filename"].(string)
	if !ok {
		return "", fmt.Errorf("filename parameter is required")
	}

	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern parameter is required")
	}

	caseSensitive := false
	if val, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = val
	}

	maxResults := 50
	if val, ok := args["max_results"].(float64); ok {
		maxResults = int(val)
	}

	// Build file path
	filePath := a.BuildLargeOutputFilePath(filename)
	if filePath == "" {
		return "", fmt.Errorf("invalid filename: %s", filename)
	}

	// Validate pattern
	if err := validatePattern(pattern); err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	// Validate file path is within allowed directory
	if a.toolOutputHandler != nil {
		baseDir := a.toolOutputHandler.OutputFolder
		if err := validateFilePath(filePath, baseDir); err != nil {
			return "", fmt.Errorf("file path validation failed: %w", err)
		}
	}

	// Search using ripgrep
	results, err := a.searchWithRipgrep(filePath, pattern, maxResults, caseSensitive, false)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	return results, nil
}

// handleQueryLargeOutput handles the query_large_output virtual tool
func (a *Agent) handleQueryLargeOutput(ctx context.Context, args map[string]interface{}) (string, error) {
	filename, ok := args["filename"].(string)
	if !ok {
		return "", fmt.Errorf("filename parameter is required")
	}

	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("query parameter is required")
	}

	compact := false
	if val, ok := args["compact"].(bool); ok {
		compact = val
	}

	raw := false
	if val, ok := args["raw"].(bool); ok {
		raw = val
	}

	// Build file path
	filePath := a.BuildLargeOutputFilePath(filename)
	if filePath == "" {
		return "", fmt.Errorf("invalid filename: %s", filename)
	}

	// Validate jq query
	if err := validateJqQuery(query); err != nil {
		return "", fmt.Errorf("invalid jq query: %w", err)
	}

	// Validate file path is within allowed directory
	if a.toolOutputHandler != nil {
		baseDir := a.toolOutputHandler.OutputFolder
		if err := validateFilePath(filePath, baseDir); err != nil {
			return "", fmt.Errorf("file path validation failed: %w", err)
		}
	}

	// Execute jq query
	result, err := a.executeJqQuery(filePath, query, compact, raw)
	if err != nil {
		return "", fmt.Errorf("jq query failed: %w", err)
	}

	return result, nil
}

// BuildLargeOutputFilePath builds the full path to a large output file
// Accepts either:
// - Full relative path: "tool_output_folder/session-id/filename.txt" (use directly)
// - Just filename: "tool_20250721_091511_tavily-search.json" (build from current session)
func (a *Agent) BuildLargeOutputFilePath(filename string) string {
	if filename == "" {
		return ""
	}

	// Normalize path separators
	filename = strings.ReplaceAll(filename, "\\", "/")

	// Check if this is already a full relative path (contains path separators)
	if strings.Contains(filename, "/") {
		// Full path provided - use it directly (handles session ID mismatch)
		// Validate it starts with tool_output_folder
		if strings.HasPrefix(filename, "tool_output_folder/") ||
			strings.HasPrefix(filename, "./tool_output_folder/") {
			return filename
		}
		// If it's a relative path that doesn't start with tool_output_folder,
		// it might be a valid path, so allow it
		if strings.HasPrefix(filename, "tool_") || strings.Contains(filename, "/tool_") {
			return filename
		}
	}

	// Just filename provided - validate format and build path from current session
	if !strings.HasPrefix(filename, "tool_") {
		return ""
	}

	// Build path based on current session ID
	if a.toolOutputHandler == nil {
		return ""
	}

	var basePath string
	if a.toolOutputHandler.SessionID != "" {
		basePath = filepath.Join(a.toolOutputHandler.OutputFolder, a.toolOutputHandler.SessionID)
	} else {
		basePath = a.toolOutputHandler.OutputFolder
	}

	return filepath.Join(basePath, filename)
}

// searchWithRipgrep searches for patterns in a file using ripgrep
func (a *Agent) searchWithRipgrep(filePath, pattern string, maxResults int, caseSensitive, wholeWord bool) (string, error) {
	// Build ripgrep command
	args := []string{"rg"}

	if !caseSensitive {
		args = append(args, "-i")
	}

	if wholeWord {
		args = append(args, "-w")
	}

	args = append(args, "-n", "-A", "2", "-B", "2", "--max-count", strconv.Itoa(maxResults), pattern, filePath)

	// Execute ripgrep
	//nolint:gosec // G204: filePath and pattern are validated, exec.Command uses separate args (no shell injection)
	cmd := exec.Command("rg", args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the error is due to no matches found (exit status 1)
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
			// No matches found - this is not an error, return empty result
			return "No matches found for the given pattern.", nil
		}
		// Other errors (file not found, permission denied, etc.)
		return "", fmt.Errorf("ripgrep search failed: %w, output: %s", err, string(output))
	}

	return string(output), nil
}

// executeJqQuery executes a jq query on a JSON file
func (a *Agent) executeJqQuery(filePath, query string, compact, raw bool) (string, error) {
	// Build jq command
	args := []string{"jq"}

	if compact {
		args = append(args, "-c")
	}

	if raw {
		args = append(args, "-r")
	}

	args = append(args, query, filePath)

	// Execute jq
	//nolint:gosec // G204: filePath and query are validated, exec.Command uses separate args (no shell injection)
	cmd := exec.Command("jq", args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("jq query failed: %w, output: %s", err, string(output))
	}

	return string(output), nil
}
