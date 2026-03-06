package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	logger loggerv2.Logger
}

// NewLangchaingoStructuredOutputGenerator creates a new structured output generator using Langchaingo
func NewLangchaingoStructuredOutputGenerator(llm llmtypes.Model, config LangchaingoStructuredOutputConfig, logger loggerv2.Logger) *LangchaingoStructuredOutputGenerator {
	// Use logger directly (already v2.Logger)
	if logger == nil {
		logger = loggerv2.NewNoop()
	}

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

	sog.logger.Debug("Enhanced prompt prepared", loggerv2.Int("length", len(enhancedPrompt)))

	// Always use JSON mode for consistent output
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "You are a JSON extraction and formatting assistant. Your task is to extract or convert the provided input into valid JSON that matches the specified schema. If the input is already valid JSON matching the schema, extract and return it as-is. If the input is not valid JSON or doesn't match the schema, convert it to match. Always respond with valid JSON only, no additional text or explanations."},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: enhancedPrompt},
			},
		},
	}

	// Generate response with JSON mode
	opts := []llmtypes.CallOption{
		llmtypes.WithJSONMode(),
	}

	sog.logger.Debug("Generating structured output")
	response, err := sog.llm.GenerateContent(ctx, messages, opts...)
	if err != nil {
		sog.logger.Error("LLM call failed", err)
		return "", fmt.Errorf("failed to generate structured output: %w", err)
	}

	return sog.extractContent(response)
}

// extractContent extracts content from the LLM response
func (sog *LangchaingoStructuredOutputGenerator) extractContent(response *llmtypes.ContentResponse) (string, error) {
	// Check if we have a valid response
	if response == nil || len(response.Choices) == 0 {
		sog.logger.Error("No response or choices", nil)
		return "", fmt.Errorf("no response generated from LLM")
	}

	// Extract content from the first choice
	choice := response.Choices[0]
	if choice.Content == "" {
		sog.logger.Error("No content in first choice", nil)
		return "", fmt.Errorf("no content in LLM response")
	}

	// Get the text content
	content := choice.Content
	sog.logger.Debug("Found text content", loggerv2.Int("length", len(content)))

	// Clean the content by removing markdown and other formatting artifacts
	cleanedContent := sog.cleanContentForJSON(content)
	sog.logger.Debug("Content cleaned",
		loggerv2.Int("original_length", len(content)),
		loggerv2.Int("cleaned_length", len(cleanedContent)))

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

	// 1. Remove markdown code blocks (```json ... ```)
	if strings.Contains(cleaned, "```") {
		sog.logger.Debug("Detected markdown code blocks, extracting content")

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
				sog.logger.Debug("Extracted content from markdown code blocks")
			}
		}
	}

	// 2. Remove any remaining markdown artifacts using simple string operations
	cleaned = sog.removeMarkdownArtifacts(cleaned)

	// 3. Final trim and cleanup
	cleaned = strings.TrimSpace(cleaned)

	sog.logger.Debug("Content cleaning completed",
		loggerv2.Int("original_length", len(content)),
		loggerv2.Int("final_length", len(cleaned)))

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
		parts = append(parts, "\n\nTASK: Extract or convert the input above into valid JSON that exactly matches this schema.")
		parts = append(parts, "\n\nIMPORTANT: If the input is already valid JSON matching this schema, extract and return it as-is. If the input is not valid JSON or doesn't match, convert it to match the schema.")
		parts = append(parts, "\n\nSchema:")
		parts = append(parts, schema)
	} else {
		parts = append(parts, "\n\nTASK: Extract or convert the input above into valid JSON that matches the expected structure.")
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

	// Create retry generator with same logger (already v2.Logger)
	retryGenerator := NewLangchaingoStructuredOutputGenerator(sog.llm, retryConfig, sog.logger)

	return retryGenerator.GenerateStructuredOutput(ctx, retryPrompt, "")
}

// ConvertToStructuredOutput converts text output to structured format using the LLM
func ConvertToStructuredOutput[T any](a *Agent, ctx context.Context, textOutput string, schema T, schemaString string) (T, error) {
	logger := a.Logger

	// First, try to parse the textOutput directly as JSON (it might already be valid JSON)
	// Clean the content first to remove markdown code blocks if present
	generator := getOrCreateStructuredOutputGenerator(a)
	cleanedOutput := generator.cleanContentForJSON(textOutput)

	// Try to parse directly as JSON first
	var result T
	err := json.Unmarshal([]byte(cleanedOutput), &result)
	if err == nil {
		// Successfully parsed as JSON - no need for LLM call
		logger.Debug("Successfully parsed JSON directly without LLM call", loggerv2.Int("json_length", len(cleanedOutput)))
		return result, nil
	}

	// JSON parsing failed - use LLM to convert to structured JSON
	logger.Debug("Direct JSON parsing failed, using LLM to convert", loggerv2.String("error", err.Error()))

	jsonOutput, err := generator.GenerateStructuredOutput(ctx, textOutput, schemaString)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to convert to structured output: %w", err)
	}

	// Validate JSON before parsing (using interface{} to support both objects and arrays)
	logger.Debug("Starting JSON unmarshaling", loggerv2.Int("json_length", len(jsonOutput)))

	var jsonValidator interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &jsonValidator); err != nil {
		logger.Error("JSON validation failed", err, loggerv2.String("json_output", jsonOutput))
		var zero T
		return zero, fmt.Errorf("invalid JSON structure: %w", err)
	}
	logger.Debug("JSON validation passed")

	// Parse JSON back to the target type
	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		logger.Error("JSON unmarshaling failed", err, loggerv2.String("json_output", jsonOutput))
		var zero T
		return zero, fmt.Errorf("failed to parse structured output: %w", err)
	}

	logger.Debug("JSON unmarshaling successful", loggerv2.Any("result_type", fmt.Sprintf("%T", result)))

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
