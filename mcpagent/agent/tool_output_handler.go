package mcpagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/utils"
)

const (
	// DefaultLargeToolOutputThreshold is the default token threshold for context offloading
	// When tool outputs exceed this threshold (in tokens), they are offloaded to filesystem (offload context pattern)
	// Note: The threshold is compared against token count, not character count
	DefaultLargeToolOutputThreshold = 10000

	// DefaultToolOutputFolder is the default folder for storing offloaded tool outputs
	DefaultToolOutputFolder = "tool_output_folder"

	// DefaultToolOutputRetentionPeriod is the default retention period for offloaded tool output files
	// Files older than this duration will be automatically cleaned up
	DefaultToolOutputRetentionPeriod = 7 * 24 * time.Hour // 7 days

	// DefaultToolOutputCleanupInterval is the default interval for periodic cleanup of old tool output files
	DefaultToolOutputCleanupInterval = 1 * time.Hour // 1 hour

	// DefaultMaxToolOutputTokenLimit is the absolute maximum token limit for tool outputs
	// This applies even when context offloading is disabled to prevent API errors
	// Set to 100k tokens to leave room for system prompt and conversation history
	DefaultMaxToolOutputTokenLimit = 100000
)

// fileCounter is an atomic counter to ensure unique filenames during parallel tool execution
var fileCounter uint64

// ToolOutputHandler implements context offloading by writing large tool outputs to files
// This follows the "offload context" pattern where tool results are stored externally
// and accessed on-demand to prevent context window overflow
type ToolOutputHandler struct {
	Threshold            int
	OutputFolder         string
	SessionID            string              // Session ID for organizing files by conversation
	Enabled              bool
	ServerAvailable      bool                // Whether context offloading virtual tools are available
	LLM                  llmtypes.Model      // Optional LLM model for provider-aware token counting
	tokenCounter         *utils.TokenCounter // Cached token counter instance
	MaxToolOutputTokens  int                 // Absolute maximum token limit (applies even when offloading is disabled)
}

// NewToolOutputHandler creates a new tool output handler with default settings
func NewToolOutputHandler() *ToolOutputHandler {
	return &ToolOutputHandler{
		Threshold:           DefaultLargeToolOutputThreshold,
		OutputFolder:        DefaultToolOutputFolder,
		SessionID:           "",
		Enabled:             true,
		ServerAvailable:     false, // Will be set by agent
		tokenCounter:        utils.NewTokenCounter(),
		MaxToolOutputTokens: DefaultMaxToolOutputTokenLimit,
	}
}

// NewToolOutputHandlerWithConfig creates a new tool output handler with custom settings
func NewToolOutputHandlerWithConfig(threshold int, outputFolder string, sessionID string, enabled bool, serverAvailable bool) *ToolOutputHandler {
	return &ToolOutputHandler{
		Threshold:           threshold,
		OutputFolder:        outputFolder,
		SessionID:           sessionID,
		Enabled:             enabled,
		ServerAvailable:     serverAvailable,
		tokenCounter:        utils.NewTokenCounter(),
		MaxToolOutputTokens: DefaultMaxToolOutputTokenLimit,
	}
}

// IsLargeToolOutput checks if the tool output exceeds the threshold for context offloading
// Returns true if output should be offloaded to filesystem (offload context pattern)
func (h *ToolOutputHandler) IsLargeToolOutput(content string) bool {
	contentLength := len(content)

	if !h.Enabled {
		return false
	}
	// Context offloading uses virtual tools that handle file operations directly
	// No MCP server dependency needed for offloading

	return contentLength > h.Threshold
}

// IsLargeToolOutputWithModel checks if the tool output exceeds the threshold using token counting
// Used for context offloading to determine when to save outputs to filesystem
func (h *ToolOutputHandler) IsLargeToolOutputWithModel(content string, model string) bool {
	if !h.Enabled {
		return false
	}

	// Use token counting instead of character counting
	tokenCount := h.CountTokensForModel(content, model)
	return tokenCount > h.Threshold
}

// SetLLM sets the LLM model for provider-aware token counting
func (h *ToolOutputHandler) SetLLM(llm llmtypes.Model) {
	h.LLM = llm
}

// CountTokensForModel counts tokens for the given content using provider/model-specific encoding
// It uses the LLM model's metadata to determine the correct encoding, or falls back to provider-based encoding
func (h *ToolOutputHandler) CountTokensForModel(content string, modelID string) int {
	// Initialize token counter if not already initialized
	if h.tokenCounter == nil {
		h.tokenCounter = utils.NewTokenCounter()
	}

	// Try to use LLM model for accurate token counting
	if h.LLM != nil {
		// Get model metadata from LLM
		metadata, err := h.LLM.GetModelMetadata(modelID)
		if err == nil && metadata != nil {
			// Use provider-aware token counting
			tokenCount, err := h.tokenCounter.CountTokens(content, metadata)
			if err == nil {
				return tokenCount
			}
		}

		// Fallback: try to get provider from model ID or use CountTokensForModel with LLM
		// Extract provider from model metadata if available
		if metadata != nil && metadata.Provider != "" {
			tokenCount, err := h.tokenCounter.CountTokensForProvider(content, metadata.Provider, modelID)
			if err == nil {
				return tokenCount
			}
		}
	}

	// Fallback: use provider-based encoding detection from model ID
	// Try to infer provider from model ID
	provider := inferProviderFromModelID(modelID)
	tokenCount, err := h.tokenCounter.CountTokensForProvider(content, provider, modelID)
	if err == nil {
		return tokenCount
	}

	// If tiktoken fails completely, return 0 (character-based estimation removed)
	// This means large output detection may not work if tiktoken fails
	return 0
}

// inferProviderFromModelID attempts to infer the provider from the model ID
func inferProviderFromModelID(modelID string) string {
	modelIDLower := strings.ToLower(modelID)

	// Check for OpenAI/OpenRouter models
	if strings.HasPrefix(modelIDLower, "gpt-") || strings.HasPrefix(modelIDLower, "o1") || strings.HasPrefix(modelIDLower, "o3") {
		return string(llmproviders.ProviderOpenAI)
	}

	// Check for Anthropic models
	if strings.Contains(modelIDLower, "claude") {
		return string(llmproviders.ProviderAnthropic)
	}

	// Check for Google/Gemini models
	if strings.Contains(modelIDLower, "gemini") {
		return string(llmproviders.ProviderVertex)
	}

	// Check for Bedrock models (usually have specific prefixes)
	if strings.Contains(modelIDLower, "anthropic.claude") || strings.Contains(modelIDLower, "amazon.") {
		return string(llmproviders.ProviderBedrock)
	}

	// Default fallback
	return ""
}

// WriteToolOutputToFile offloads large tool output to filesystem (context offloading)
// Returns the file path where the content was saved
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
	if err := os.MkdirAll(sessionFolder, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for user-accessible directories
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate unique filename with appropriate extension
	filename := h.generateToolOutputFilename(toolName, actualContent)
	filePath := filepath.Join(sessionFolder, filename)

	// Write actual content to file (without prefix)
	if err := os.WriteFile(filePath, []byte(actualContent), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for user-accessible files
		return "", fmt.Errorf("failed to write tool output to file: %w", err)
	}

	return filePath, nil
}

// generateToolOutputFilename creates a unique filename for tool output.
// Uses nanosecond precision and an atomic counter to prevent collisions during parallel tool execution.
func (h *ToolOutputHandler) generateToolOutputFilename(toolName string, content string) string {
	now := time.Now()
	counter := atomic.AddUint64(&fileCounter, 1)
	timestamp := fmt.Sprintf("%s_%09d_%d", now.Format("20060102_150405"), now.Nanosecond(), counter)
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
// previewPercent: percentage of threshold to use for preview (e.g., 50 for 50%, 10 for 10%)
// isContextEditing: if true, creates a concise message for stale responses (context editing); if false, creates detailed message for new offloading
func (h *ToolOutputHandler) CreateToolOutputMessageWithPreview(toolCallID, filePath, content string, previewPercent int, isContextEditing bool) string {
	// Extract actual content from prefixed tool result
	actualContent := ExtractActualContent(content)

	// Extract first characters based on preview percentage
	previewLength := (h.Threshold * previewPercent) / 100
	preview := h.ExtractFirstNCharacters(actualContent, previewLength)

	// Use the full relative path so LLM knows which session folder to use
	// This fixes session ID mismatch issues when agent instances change
	fullRelativePath := filePath
	// Normalize path separators for cross-platform compatibility
	fullRelativePath = strings.ReplaceAll(fullRelativePath, "\\", "/")

	var instructions string
	if isContextEditing {
		// Concise message for context editing (stale responses) - LLM already knows how to use tools
		instructions = fmt.Sprintf(`Tool output saved to: %s

Preview (%d chars): %s

[Use search_large_output tool to access full content]`, fullRelativePath, previewLength, preview)
	} else {
		// Detailed message for context offloading (new large outputs)
		instructions = fmt.Sprintf(`
The tool output was too large and has been saved to: %s

FIRST %d CHARACTERS OF OUTPUT (%d%% of threshold):
%s

[Content truncated for display - full content available in file]

Make sure to use the virtual tool next to read contents of this file in an efficient manner:

Available virtual tool for context offloading:
- search_large_output - unified tool for accessing offloaded files. Use operation='read' to read character ranges, operation='search' for regex pattern matching, or operation='query' for jq JSON queries.

Example: Use search_large_output with operation='read' (start/end params), operation='search' (pattern param), or operation='query' (query param for jq)

NOTE: When using the virtual tool, you can provide either:
- The full path: "%s" (recommended - includes session folder)
- Or just the filename: "%s" (will use current session folder)
`, fullRelativePath, previewLength, previewPercent, preview, fullRelativePath, filepath.Base(filePath))
	}

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

// SetThreshold updates the threshold for context offloading (when to offload tool outputs)
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

// SetServerAvailable sets whether context offloading virtual tools are available
func (h *ToolOutputHandler) SetServerAvailable(available bool) {
	h.ServerAvailable = available
}

// IsServerAvailable returns whether context offloading virtual tools are available
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

// CleanupOldFiles deletes files older than the specified maxAge in the output folder
// It scans all session folders and removes files that are older than maxAge
func (h *ToolOutputHandler) CleanupOldFiles(maxAge time.Duration) error {
	if h.OutputFolder == "" {
		return fmt.Errorf("output folder is not set")
	}

	// Check if output folder exists
	if _, err := os.Stat(h.OutputFolder); os.IsNotExist(err) {
		// Folder doesn't exist, nothing to clean
		return nil
	}

	cutoffTime := time.Now().Add(-maxAge)
	var totalDeleted int
	var totalErrors int

	// Walk through all session folders
	err := filepath.Walk(h.OutputFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Continue on errors for individual files/dirs
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file is older than cutoff time
		if info.ModTime().Before(cutoffTime) {
			if err := os.Remove(path); err != nil {
				totalErrors++
				// Continue cleaning other files even if one fails
				return nil
			}
			totalDeleted++
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking output folder: %w", err)
	}

	// Clean up empty session directories after file deletion
	// h.cleanupEmptyDirectories() - REMOVED: potentially unsafe if multiple agents share folder

	if totalErrors > 0 {
		return fmt.Errorf("deleted %d files, but encountered %d errors", totalDeleted, totalErrors)
	}

	return nil
}

// CleanupSessionFolder deletes the entire session folder for a given session ID
func (h *ToolOutputHandler) CleanupSessionFolder(sessionID string) error {
	if h.OutputFolder == "" {
		return fmt.Errorf("output folder is not set")
	}

	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	sessionFolder := filepath.Join(h.OutputFolder, sessionID)

	// Check if session folder exists
	if _, err := os.Stat(sessionFolder); os.IsNotExist(err) {
		// Folder doesn't exist, nothing to clean
		return nil
	}

	// Remove the entire session folder
	if err := os.RemoveAll(sessionFolder); err != nil {
		return fmt.Errorf("failed to remove session folder: %w", err)
	}

	return nil
}

// CleanupCurrentSessionFolder deletes the current session's folder
func (h *ToolOutputHandler) CleanupCurrentSessionFolder() error {
	return h.CleanupSessionFolder(h.SessionID)
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

// ExceedsMaxTokenLimit checks if content exceeds the absolute max token limit
// This check applies regardless of whether context offloading is enabled
func (h *ToolOutputHandler) ExceedsMaxTokenLimit(content string, model string) bool {
	tokenCount := h.CountTokensForModel(content, model)
	return tokenCount > h.MaxToolOutputTokens
}

// TruncateToMaxTokenLimit returns an error message when content exceeds the max token limit
// Instead of truncating (which could lead to incorrect results), it returns a clear error
// Returns the error message and true if the limit was exceeded
func (h *ToolOutputHandler) TruncateToMaxTokenLimit(content string, model string, toolName string) (string, bool) {
	tokenCount := h.CountTokensForModel(content, model)
	if tokenCount <= h.MaxToolOutputTokens {
		return content, false
	}

	// Return error message instead of truncated content
	errorMessage := fmt.Sprintf(`[ERROR: TOOL OUTPUT TOO LARGE]

The tool '%s' returned %d tokens which exceeds the maximum limit of %d tokens.
The output was NOT included to prevent API errors.

To fix this, you need to make more targeted queries:
1. Use filters to narrow down the results (e.g., specific folder paths, file patterns)
2. Request specific fields or ranges instead of full data dumps
3. Break the request into smaller chunks
4. Use pagination if the tool supports it
5. Query for specific items by ID/name instead of listing everything

Example: Instead of listing all files recursively, list files in a specific subfolder.
`, toolName, tokenCount, h.MaxToolOutputTokens)

	return errorMessage, true
}

// SetMaxToolOutputTokens sets the maximum token limit for tool outputs
func (h *ToolOutputHandler) SetMaxToolOutputTokens(limit int) {
	h.MaxToolOutputTokens = limit
}

// GetMaxToolOutputTokens returns the current max token limit
func (h *ToolOutputHandler) GetMaxToolOutputTokens() int {
	return h.MaxToolOutputTokens
}

// DefaultMaxContextTokenLimit is the fallback maximum allowed tokens before sending to LLM API
// when model context window information is not available.
// This prevents "prompt is too long" errors from cumulative tool results.
const DefaultMaxContextTokenLimit = 800000

// GetMaxContextTokenLimit returns the max context token limit based on the model's context window.
// Uses 80% of the model's context window to leave room for system prompt and response.
// Falls back to DefaultMaxContextTokenLimit (800k) if model context window is unknown.
func GetMaxContextTokenLimit(modelContextWindow int) int {
	if modelContextWindow > 0 {
		limit := int(float64(modelContextWindow) * 0.8)
		return limit
	}
	return DefaultMaxContextTokenLimit
}

// EstimateMessagesTokenCount estimates total tokens across all messages
// This is used for pre-flight checks before calling the LLM API
func (h *ToolOutputHandler) EstimateMessagesTokenCount(messages []llmtypes.MessageContent, modelID string) int {
	totalTokens := 0
	for _, msg := range messages {
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				totalTokens += h.CountTokensForModel(p.Text, modelID)
			case llmtypes.ToolCallResponse:
				totalTokens += h.CountTokensForModel(p.Content, modelID)
			case string:
				totalTokens += h.CountTokensForModel(p, modelID)
			}
		}
	}
	return totalTokens
}

// ExceedsContextLimit checks if messages would exceed the safe context limit
func (h *ToolOutputHandler) ExceedsContextLimit(messages []llmtypes.MessageContent, modelID string, limit int) (bool, int) {
	totalTokens := h.EstimateMessagesTokenCount(messages, modelID)
	return totalTokens > limit, totalTokens
}
