package mcpagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkoukk/tiktoken-go"
)

const (
	// DefaultLargeToolOutputThreshold is the default character threshold for considering tool output as "large"
	DefaultLargeToolOutputThreshold = 20000

	// DefaultToolOutputFolder is the default folder for storing large tool outputs
	DefaultToolOutputFolder = "tool_output_folder"
)

// ToolOutputHandler handles large tool outputs by writing them to files
type ToolOutputHandler struct {
	Threshold       int
	OutputFolder    string
	SessionID       string // Session ID for organizing files by conversation
	Enabled         bool
	ServerAvailable bool // Whether the read_large_tool_output server is available
}

// NewToolOutputHandler creates a new tool output handler with default settings
func NewToolOutputHandler() *ToolOutputHandler {
	return &ToolOutputHandler{
		Threshold:       DefaultLargeToolOutputThreshold,
		OutputFolder:    DefaultToolOutputFolder,
		SessionID:       "",
		Enabled:         true,
		ServerAvailable: false, // Will be set by agent
	}
}

// NewToolOutputHandlerWithConfig creates a new tool output handler with custom settings
func NewToolOutputHandlerWithConfig(threshold int, outputFolder string, sessionID string, enabled bool, serverAvailable bool) *ToolOutputHandler {
	return &ToolOutputHandler{
		Threshold:       threshold,
		OutputFolder:    outputFolder,
		SessionID:       sessionID,
		Enabled:         enabled,
		ServerAvailable: serverAvailable,
	}
}

// IsLargeToolOutput checks if the tool output exceeds the threshold and server is available
func (h *ToolOutputHandler) IsLargeToolOutput(content string) bool {
	contentLength := len(content)

	if !h.Enabled {
		return false
	}
	// Note: Server availability check is no longer needed since we use virtual tools
	// Virtual tools handle large output processing directly without MCP server dependency

	return contentLength > h.Threshold
}

// IsLargeToolOutputWithModel checks if the tool output exceeds the threshold using token counting
func (h *ToolOutputHandler) IsLargeToolOutputWithModel(content string, model string) bool {
	if !h.Enabled {
		return false
	}

	// Use token counting instead of character counting
	tokenCount := h.CountTokensForModel(content, model)
	return tokenCount > h.Threshold
}

// CountTokensForModel counts tokens for the given content using o200k_base encoding
func (h *ToolOutputHandler) CountTokensForModel(content string, model string) int {
	// Use o200k_base encoding for all models for simplicity
	encoding, err := tiktoken.GetEncoding("o200k_base")
	if err != nil {
		// Fallback to character-based approximation if encoding fails
		return len(content) / 4
	}

	// Count tokens
	tokens := encoding.Encode(content, nil, nil)
	return len(tokens)
}

// WriteToolOutputToFile writes large tool output to a file and returns the file path
func (h *ToolOutputHandler) WriteToolOutputToFile(content, toolName string) (string, error) {
	if !h.Enabled {
		return "", fmt.Errorf("tool output handler is disabled")
	}

	// Extract actual content from prefixed tool result
	actualContent := ExtractActualContent(content)

	// Create session-based folder path
	var sessionFolder string
	if h.SessionID != "" {
		sessionFolder = filepath.Join(h.OutputFolder, h.SessionID)
	} else {
		sessionFolder = h.OutputFolder
	}

	// Ensure output directory exists
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate unique filename with appropriate extension
	filename := h.generateToolOutputFilename(toolName, actualContent)
	filePath := filepath.Join(sessionFolder, filename)

	// Write actual content to file (without prefix)
	if err := os.WriteFile(filePath, []byte(actualContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write tool output to file: %w", err)
	}

	return filePath, nil
}

// generateToolOutputFilename creates a unique filename for tool output
func (h *ToolOutputHandler) generateToolOutputFilename(toolName string, content string) string {
	timestamp := time.Now().Format("20060102_150405")
	// Sanitize tool name for filename
	sanitizedName := sanitizeFilename(toolName)
	return fmt.Sprintf("tool_%s_%s%s", timestamp, sanitizedName, h.getFileExtension(content))
}

// sanitizeFilename removes or replaces characters that are not safe for filenames
func sanitizeFilename(name string) string {
	// Replace unsafe characters with underscores
	unsafeChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	sanitized := name
	for _, char := range unsafeChars {
		sanitized = replaceAll(sanitized, char, "_")
	}

	// Limit length to avoid filesystem issues
	if len(sanitized) > 50 {
		sanitized = sanitized[:50]
	}

	return sanitized
}

// replaceAll is a simple string replacement function
func replaceAll(s, old, new string) string {
	result := ""
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += s[start:i] + new
			start = i + len(old)
			i += len(old) - 1
		}
	}
	result += s[start:]
	return result
}

// CreateToolOutputMessageWithPreview creates a message for the LLM with file path, first characters up to threshold, and instructions
func (h *ToolOutputHandler) CreateToolOutputMessageWithPreview(toolCallID, filePath, content string) string {
	// Extract actual content from prefixed tool result
	actualContent := ExtractActualContent(content)

	// Extract first characters up to 50% of the threshold
	previewLength := h.Threshold / 2
	preview := h.ExtractFirstNCharacters(actualContent, previewLength)

	// Use the full relative path so LLM knows which session folder to use
	// This fixes session ID mismatch issues when agent instances change
	fullRelativePath := filePath
	// Normalize path separators for cross-platform compatibility
	fullRelativePath = strings.ReplaceAll(fullRelativePath, "\\", "/")

	instructions := fmt.Sprintf(`
The tool output was too large and has been saved to: %s

FIRST %d CHARACTERS OF OUTPUT (50%% of threshold):
%s

[Content truncated for display - full content available in file]

Make sure to use the virtual tools next to read contents of this file in an efficient manner:

Available virtual tools:
- read_large_output - read specific characters from a large tool output file
- search_large_output - search for regex patterns in large tool output files
- query_large_output - execute jq queries on large JSON tool output files

Example: "Read characters 1-100 from %s" or "Search for 'error' in %s" or "Query '.name' from %s" (using jq)

NOTE: When using virtual tools, you can provide either:
- The full path: "%s" (recommended - includes session folder)
- Or just the filename: "%s" (will use current session folder)
`, fullRelativePath, previewLength, preview, fullRelativePath, fullRelativePath, fullRelativePath, fullRelativePath, filepath.Base(filePath))

	return instructions
}

// ExtractFirstNCharacters extracts the first n characters from content
func (h *ToolOutputHandler) ExtractFirstNCharacters(content string, n int) string {
	if len(content) <= n {
		return content
	}
	return content[:n]
}

// GetToolOutputFolder returns the current output folder path
func (h *ToolOutputHandler) GetToolOutputFolder() string {
	return h.OutputFolder
}

// SetThreshold updates the threshold for large tool outputs
func (h *ToolOutputHandler) SetThreshold(threshold int) {
	h.Threshold = threshold
}

// SetOutputFolder updates the output folder path
func (h *ToolOutputHandler) SetOutputFolder(folder string) {
	h.OutputFolder = folder
}

// SetEnabled enables or disables the tool output handler
func (h *ToolOutputHandler) SetEnabled(enabled bool) {
	h.Enabled = enabled
}

// SetServerAvailable sets whether the read_large_tool_output server is available
func (h *ToolOutputHandler) SetServerAvailable(available bool) {
	h.ServerAvailable = available
}

// IsServerAvailable returns whether the read_large_tool_output server is available
func (h *ToolOutputHandler) IsServerAvailable() bool {
	return h.ServerAvailable
}

// SetSessionID sets the session ID for organizing files by conversation
func (h *ToolOutputHandler) SetSessionID(sessionID string) {
	h.SessionID = sessionID
}

// GetSessionID returns the current session ID
func (h *ToolOutputHandler) GetSessionID() string {
	return h.SessionID
}

// isJSONContent checks if the given string is valid JSON
func isJSONContent(content string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(content), &js) == nil
}

// ExtractActualContent removes the "TOOL RESULT for toolname:" prefix and returns the actual content
func ExtractActualContent(prefixedContent string) string {
	// First, try to extract from MCP protocol format: {"type":"text","text":"actual_content"}
	if strings.HasPrefix(prefixedContent, `{"type":"text","text":"`) {
		// Find the closing quote and brace
		startIndex := len(`{"type":"text","text":"`)
		endIndex := strings.LastIndex(prefixedContent, `"}`)
		if endIndex > startIndex {
			// Extract the content between the quotes
			content := prefixedContent[startIndex:endIndex]
			// Unescape the content
			content = strings.ReplaceAll(content, `\"`, `"`)
			content = strings.ReplaceAll(content, `\n`, "\n")
			content = strings.ReplaceAll(content, `\t`, "\t")
			return content
		}
	}

	// Look for the pattern "TOOL RESULT for toolname: "
	prefixPattern := "TOOL RESULT for "
	if strings.HasPrefix(prefixedContent, prefixPattern) {
		// Find the colon after the tool name
		colonIndex := strings.Index(prefixedContent, ": ")
		if colonIndex != -1 {
			// Return everything after ": "
			return prefixedContent[colonIndex+2:]
		}
	}
	// If no prefix found, return the original content (this is now the normal case)
	return prefixedContent
}

// getFileExtension determines the appropriate file extension based on content type
func (h *ToolOutputHandler) getFileExtension(content string) string {
	if isJSONContent(content) {
		return ".json"
	}
	return ".txt"
}
