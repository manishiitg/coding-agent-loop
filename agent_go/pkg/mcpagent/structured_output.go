package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/utils"
)

// LangchaingoStructuredOutputConfig contains configuration for structured output generation
type LangchaingoStructuredOutputConfig struct {
	// Always use JSON mode for consistent output
	UseJSONMode bool

	// Validation settings
	ValidateOutput bool
	MaxRetries     int
}

// LangchaingoStructuredOutputGenerator handles structured output generation using Langchaingo
type LangchaingoStructuredOutputGenerator struct {
	config LangchaingoStructuredOutputConfig
	llm    llmtypes.Model
	logger utils.ExtendedLogger
}

// NewLangchaingoStructuredOutputGenerator creates a new structured output generator using Langchaingo
func NewLangchaingoStructuredOutputGenerator(llm llmtypes.Model, config LangchaingoStructuredOutputConfig, logger utils.ExtendedLogger) *LangchaingoStructuredOutputGenerator {
	return &LangchaingoStructuredOutputGenerator{
		config: config,
		llm:    llm,
		logger: logger,
	}
}

// GenerateStructuredOutput generates structured JSON output from the LLM using Langchaingo
func (sog *LangchaingoStructuredOutputGenerator) GenerateStructuredOutput(ctx context.Context, prompt string, schema string) (string, error) {
	// Build the enhanced prompt with the provided schema
	enhancedPrompt := sog.buildStructuredPromptWithSchema(prompt, schema)

	sog.logger.Infof("Enhanced prompt length: %d chars", len(enhancedPrompt))

	// Always use JSON mode for consistent output
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "You are a helpful assistant that generates structured JSON output according to the specified schema. Always respond with valid JSON only, no additional text or explanations."},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: enhancedPrompt},
			},
		},
	}

	// Configure max_tokens for structured output (higher default due to complex prompts)
	maxTokens := 20000 // Higher default for structured output
	if maxTokensEnv := os.Getenv("ORCHESTRATOR_MAIN_LLM_MAX_TOKENS"); maxTokensEnv != "" {
		if parsed, err := strconv.Atoi(maxTokensEnv); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	// Generate response with JSON mode and max_tokens
	opts := []llmtypes.CallOption{
		llmtypes.WithJSONMode(),
		llmtypes.WithMaxTokens(maxTokens),
	}

	sog.logger.Infof("Structured output max_tokens: %d", maxTokens)
	response, err := sog.llm.GenerateContent(ctx, messages, opts...)
	if err != nil {
		sog.logger.Errorf("LLM call failed: %v", err)
		return "", fmt.Errorf("failed to generate structured output: %w", err)
	}

	return sog.extractContent(response)
}

// extractContent extracts content from the LLM response
func (sog *LangchaingoStructuredOutputGenerator) extractContent(response *llmtypes.ContentResponse) (string, error) {
	// Check if we have a valid response
	if response == nil || len(response.Choices) == 0 {
		sog.logger.Errorf("No response or choices")
		return "", fmt.Errorf("no response generated from LLM")
	}

	// Extract content from the first choice
	choice := response.Choices[0]
	if choice.Content == "" {
		sog.logger.Errorf("No content in first choice")
		return "", fmt.Errorf("no content in LLM response")
	}

	// Get the text content
	content := choice.Content
	sog.logger.Infof("Found text content, length: %d", len(content))

	// Log the full content for debugging
	sog.logger.Infof("🔍 Full LLM response content:")
	sog.logger.Infof("Content: %s", content)

	// Clean the content by removing markdown and other formatting artifacts
	cleanedContent := sog.cleanContentForJSON(content)
	sog.logger.Infof("Cleaned content length: %d chars", len(cleanedContent))
	sog.logger.Infof("Cleaned content: %s", cleanedContent)

	if sog.config.ValidateOutput {
		// Validate that the output is valid JSON
		if err := sog.validateJSON(cleanedContent, nil); err != nil {
			// If validation fails and we have retries, try again
			if sog.config.MaxRetries > 0 {
				return sog.retryGeneration(context.Background(), "", sog.config.MaxRetries-1)
			}
			return "", fmt.Errorf("invalid JSON output: %w", err)
		}
	}

	return cleanedContent, nil
}

// cleanContentForJSON cleans content by removing markdown and other formatting artifacts
func (sog *LangchaingoStructuredOutputGenerator) cleanContentForJSON(content string) string {
	cleaned := strings.TrimSpace(content)

	// Log the cleaning process
	sog.logger.Infof("🧹 Cleaning content for JSON parsing...")
	sog.logger.Infof("Original length: %d chars", len(content))

	// 1. Remove markdown code blocks (```json ... ```)
	if strings.Contains(cleaned, "```") {
		sog.logger.Infof("🔍 Detected markdown code blocks, extracting content...")

		// Find the start and end of code blocks
		startIdx := strings.Index(cleaned, "```")
		if startIdx != -1 {
			// Skip the opening ``` and any language identifier
			contentStart := startIdx + 3
			// Find the first newline after ```
			newlineIdx := strings.Index(cleaned[contentStart:], "\n")
			if newlineIdx != -1 {
				contentStart += newlineIdx + 1
			}

			// Find the closing ```
			endIdx := strings.LastIndex(cleaned, "```")
			if endIdx > contentStart {
				cleaned = cleaned[contentStart:endIdx]
				sog.logger.Infof("✅ Extracted content from markdown code blocks")
			}
		}
	}

	// 2. Remove any remaining markdown artifacts using simple string operations
	sog.logger.Infof("🔍 CONTENT CLEANING DEBUG: Before removeMarkdownArtifacts: %s", cleaned)
	cleaned = sog.removeMarkdownArtifacts(cleaned)
	sog.logger.Infof("🔍 CONTENT CLEANING DEBUG: After removeMarkdownArtifacts: %s", cleaned)

	// 3. Final trim and cleanup
	cleaned = strings.TrimSpace(cleaned)

	sog.logger.Infof("Final cleaned length: %d chars", len(cleaned))
	sog.logger.Infof("🔍 CONTENT CLEANING DEBUG: Final cleaned content: %s", cleaned)

	// Log the final cleaned content for debugging
	sog.logger.Infof("✅ Content cleaning completed successfully")

	return cleaned
}

// removeMarkdownArtifacts removes common markdown formatting artifacts using simple string operations
func (sog *LangchaingoStructuredOutputGenerator) removeMarkdownArtifacts(content string) string {
	cleaned := content

	// Remove common markdown patterns that might interfere with JSON
	// Using simple string operations instead of regex to avoid complexity

	// Remove markdown headers
	lines := strings.Split(cleaned, "\n")
	var cleanedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip lines that start with # (headers)
		if !strings.HasPrefix(trimmed, "#") {
			// Remove bold formatting **text** -> text
			trimmed = strings.ReplaceAll(trimmed, "**", "")
			// Remove italic formatting *text* -> text
			trimmed = strings.ReplaceAll(trimmed, "*", "")
			// Remove inline code formatting `text` -> text
			trimmed = strings.ReplaceAll(trimmed, "`", "")
			// Remove list markers
			trimmed = strings.TrimLeft(trimmed, " -+*0123456789.")
			cleanedLines = append(cleanedLines, trimmed)
		}
	}

	// Join lines back together
	cleaned = strings.Join(cleanedLines, "\n")

	// Normalize multiple newlines
	cleaned = strings.ReplaceAll(cleaned, "\n\n\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\n\n", "\n")

	return cleaned
}

// buildStructuredPromptWithSchema builds a prompt with the provided schema
func (sog *LangchaingoStructuredOutputGenerator) buildStructuredPromptWithSchema(basePrompt string, schema string) string {
	var parts []string

	// Add base prompt
	parts = append(parts, basePrompt)

	// Add the provided schema
	if schema != "" {
		parts = append(parts, "\n\nIMPORTANT: You must respond with valid JSON that exactly matches this schema:")
		parts = append(parts, "\nSchema:")
		parts = append(parts, schema)
	} else {
		parts = append(parts, "\n\nIMPORTANT: You must respond with valid JSON that matches the expected structure.")
	}

	// Add final instruction
	parts = append(parts, "\n\nCRITICAL: Return ONLY the JSON object that matches the schema exactly. No text, no explanations, no markdown. Just the JSON.")

	return strings.Join(parts, "")
}

// validateJSON validates that the output is valid JSON and matches the target type
func (sog *LangchaingoStructuredOutputGenerator) validateJSON(jsonStr string, targetType interface{}) error {
	// First, check if it's valid JSON
	var temp interface{}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return fmt.Errorf("invalid JSON format: %w", err)
	}

	// If target type is provided, try to unmarshal into it
	if targetType != nil {
		if err := json.Unmarshal([]byte(jsonStr), targetType); err != nil {
			return fmt.Errorf("JSON does not match expected structure: %w", err)
		}
	}

	return nil
}

// retryGeneration retries the generation with a more explicit prompt
func (sog *LangchaingoStructuredOutputGenerator) retryGeneration(ctx context.Context, prompt string, retriesLeft int) (string, error) {
	// Add more explicit instructions for retry
	retryPrompt := prompt + "\n\nCRITICAL: You must respond with ONLY valid JSON. No text, no explanations, no markdown. Just the JSON object."

	// Create a new generator with retry configuration
	retryConfig := sog.config
	retryConfig.MaxRetries = retriesLeft

	retryGenerator := NewLangchaingoStructuredOutputGenerator(sog.llm, retryConfig, sog.logger)

	return retryGenerator.GenerateStructuredOutput(ctx, retryPrompt, "")
}

// ConvertToStructuredOutput converts text output to structured format using the LLM
func ConvertToStructuredOutput[T any](a *Agent, ctx context.Context, textOutput string, schema T, schemaString string) (T, error) {
	// Use the LLM to convert the text output to structured JSON
	generator := getOrCreateStructuredOutputGenerator(a)

	jsonOutput, err := generator.GenerateStructuredOutput(ctx, textOutput, schemaString)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to convert to structured output: %w", err)
	}

	// Add detailed logging for JSON parsing
	a.Logger.Infof("🔍 JSON PARSING DEBUG: Starting JSON unmarshaling")
	a.Logger.Infof("🔍 JSON PARSING DEBUG: JSON output length: %d chars", len(jsonOutput))
	a.Logger.Infof("🔍 JSON PARSING DEBUG: JSON output content: %s", jsonOutput)

	// Validate JSON before parsing (using interface{} to support both objects and arrays)
	var jsonValidator interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &jsonValidator); err != nil {
		a.Logger.Errorf("❌ JSON PARSING DEBUG: JSON validation failed: %v", err)
		var zero T
		return zero, fmt.Errorf("invalid JSON structure: %w", err)
	}
	a.Logger.Infof("✅ JSON PARSING DEBUG: JSON validation passed")

	// Parse JSON back to the target type
	var result T
	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		a.Logger.Errorf("❌ JSON PARSING DEBUG: JSON unmarshaling failed: %v", err)
		var zero T
		return zero, fmt.Errorf("failed to parse structured output: %w", err)
	}

	// Log the parsed result for debugging
	a.Logger.Infof("✅ JSON PARSING DEBUG: JSON unmarshaling successful")
	a.Logger.Infof("🔍 JSON PARSING DEBUG: Parsed result type: %T", result)

	// Add detailed logging for struct field assignment
	if resultBytes, err := json.Marshal(result); err == nil {
		a.Logger.Infof("🔍 JSON PARSING DEBUG: Struct after unmarshaling: %s", string(resultBytes))
	}

	// Special logging for PlanningResponse to debug should_continue issue
	if planningResp, ok := any(result).(interface{ GetShouldContinue() bool }); ok {
		a.Logger.Infof("🔍 JSON PARSING DEBUG: PlanningResponse should_continue: %t", planningResp.GetShouldContinue())
	}

	// Try to extract should_continue using reflection for debugging
	if jsonData, err := json.Marshal(result); err == nil {
		var debugMap map[string]interface{}
		if err := json.Unmarshal(jsonData, &debugMap); err == nil {
			if shouldContinue, exists := debugMap["should_continue"]; exists {
				a.Logger.Infof("🔍 JSON PARSING DEBUG: should_continue from parsed result: %v (type: %T)", shouldContinue, shouldContinue)
			}
		}
	}

	return result, nil
}

// getOrCreateStructuredOutputGenerator creates a structured output generator if needed
func getOrCreateStructuredOutputGenerator(a *Agent) *LangchaingoStructuredOutputGenerator {
	// Create a new generator with default configuration
	config := LangchaingoStructuredOutputConfig{
		UseJSONMode:    true, // Always use JSON mode for consistent output
		ValidateOutput: true,
		MaxRetries:     2,
	}

	return NewLangchaingoStructuredOutputGenerator(a.LLM, config, a.Logger)
}
