package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// isCLIProvider returns true if the provider is a CLI wrapper (claude-code or gemini-cli).
func isCLIProvider(provider llm.Provider) bool {
	return provider == llm.ProviderClaudeCode || provider == llm.ProviderGeminiCLI
}

// buildCLIStructuredOutputInstruction builds an instruction string that tells the LLM
// to respond with JSON only, conforming to the given schema.
func buildCLIStructuredOutputInstruction(schemaString string) string {
	return fmt.Sprintf(`

IMPORTANT: You MUST respond with ONLY a valid JSON object that conforms to the following JSON schema. Do NOT include any text, explanations, or markdown formatting — output ONLY the raw JSON object.

JSON Schema:
%s`, schemaString)
}

// buildCLIStructuredOutputInstructionWithTool builds an instruction string for tool-based
// structured output that includes the tool name and description for context.
func buildCLIStructuredOutputInstructionWithTool(toolName, toolDescription, schema string) string {
	return fmt.Sprintf(`

IMPORTANT: You MUST respond with ONLY a valid JSON object. This JSON represents the arguments you would pass to a tool called "%s" (%s). Do NOT include any text, explanations, or markdown formatting — output ONLY the raw JSON object.

JSON Schema:
%s`, toolName, toolDescription, schema)
}

// injectStructuredOutputIntoLastUserMessage clones the messages slice and appends
// the structured output instruction as a new TextContent part to the last human message.
// It does not mutate the original messages slice or any of its elements.
func injectStructuredOutputIntoLastUserMessage(messages []llmtypes.MessageContent, instruction string) []llmtypes.MessageContent {
	// Clone the messages slice
	cloned := make([]llmtypes.MessageContent, len(messages))
	copy(cloned, messages)

	// Find the last human message and append the instruction
	for i := len(cloned) - 1; i >= 0; i-- {
		if cloned[i].Role == llmtypes.ChatMessageTypeHuman {
			// Clone the parts slice for this message to avoid mutating the original
			newParts := make([]llmtypes.ContentPart, len(cloned[i].Parts), len(cloned[i].Parts)+1)
			copy(newParts, cloned[i].Parts)
			newParts = append(newParts, llmtypes.TextContent{Text: instruction})
			cloned[i] = llmtypes.MessageContent{
				Role:  cloned[i].Role,
				Parts: newParts,
			}
			return cloned
		}
	}

	// No human message found — append a new one with just the instruction
	cloned = append(cloned, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: instruction}},
	})
	return cloned
}

// extractJSONFromCLIResponse attempts to extract valid JSON from a CLI text response
// using multiple strategies. Returns the extracted JSON bytes or an error.
func extractJSONFromCLIResponse(textResponse string) ([]byte, error) {
	trimmed := strings.TrimSpace(textResponse)
	if trimmed == "" {
		return nil, fmt.Errorf("empty response from CLI provider")
	}

	// Strategy 1: Direct parse — the entire response is valid JSON
	if json.Valid([]byte(trimmed)) {
		return []byte(trimmed), nil
	}

	// Strategy 2: Strip markdown code blocks (```json ... ``` or ``` ... ```)
	if stripped := stripMarkdownCodeBlock(trimmed); stripped != "" {
		if json.Valid([]byte(stripped)) {
			return []byte(stripped), nil
		}
	}

	// Strategy 3: Find outermost JSON object ({ ... })
	if obj := extractOutermost(trimmed, '{', '}'); obj != "" {
		if json.Valid([]byte(obj)) {
			return []byte(obj), nil
		}
	}

	// Strategy 4: Find outermost JSON array ([ ... ])
	if arr := extractOutermost(trimmed, '[', ']'); arr != "" {
		if json.Valid([]byte(arr)) {
			return []byte(arr), nil
		}
	}

	return nil, fmt.Errorf("no valid JSON found in CLI response (length %d)", len(trimmed))
}

// stripMarkdownCodeBlock strips markdown code block delimiters from the response.
// Handles ```json\n...\n``` and ```\n...\n```.
func stripMarkdownCodeBlock(s string) string {
	// Try ```json first, then plain ```
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, "```") {
			inner := s[len(prefix) : len(s)-3]
			return strings.TrimSpace(inner)
		}
	}
	return ""
}

// extractOutermost finds the substring from the first occurrence of open to the
// last occurrence of close in s.
func extractOutermost(s string, open, close byte) string {
	first := strings.IndexByte(s, open)
	if first == -1 {
		return ""
	}
	last := strings.LastIndexByte(s, close)
	if last == -1 || last <= first {
		return ""
	}
	return s[first : last+1]
}

// askWithHistoryStructuredCLI handles structured output for CLI providers by injecting
// schema instructions into the prompt, calling AskWithHistory, then extracting JSON locally.
func askWithHistoryStructuredCLI[T any](a *Agent, ctx context.Context, messages []llmtypes.MessageContent, schema T, schemaString string) (T, []llmtypes.MessageContent, error) {
	logger := a.Logger

	// Build and inject the structured output instruction
	instruction := buildCLIStructuredOutputInstruction(schemaString)
	injectedMessages := injectStructuredOutputIntoLastUserMessage(messages, instruction)

	logger.Debug("CLI structured output: injected schema instruction into messages",
		loggerv2.String("provider", string(a.provider)),
		loggerv2.Int("schema_length", len(schemaString)))

	// Call the normal AskWithHistory with injected messages
	textResponse, updatedMessages, err := a.AskWithHistory(ctx, injectedMessages)
	if err != nil {
		var zero T
		return zero, updatedMessages, fmt.Errorf("failed to get text response: %w", err)
	}

	logger.Debug("CLI structured output: got text response",
		loggerv2.Int("response_length", len(textResponse)))

	// Extract JSON from the text response
	jsonBytes, err := extractJSONFromCLIResponse(textResponse)
	if err != nil {
		var zero T
		return zero, updatedMessages, fmt.Errorf("failed to extract JSON from CLI response: %w", err)
	}

	// Unmarshal into the target type
	var result T
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		var zero T
		return zero, updatedMessages, fmt.Errorf("failed to unmarshal extracted JSON into target type: %w", err)
	}

	logger.Debug("CLI structured output: successfully parsed structured result")

	return result, updatedMessages, nil
}

// askWithHistoryStructuredViaToolCLI handles tool-based structured output for CLI providers.
// On extraction failure it returns HasStructuredOutput=false (matching API-provider behavior
// when the tool isn't called), not a hard error.
func askWithHistoryStructuredViaToolCLI[T any](
	a *Agent,
	ctx context.Context,
	messages []llmtypes.MessageContent,
	toolName string,
	toolDescription string,
	schema string,
) (StructuredOutputResult[T], error) {
	logger := a.Logger

	// Build and inject the structured output instruction (includes tool context)
	instruction := buildCLIStructuredOutputInstructionWithTool(toolName, toolDescription, schema)
	injectedMessages := injectStructuredOutputIntoLastUserMessage(messages, instruction)

	logger.Debug("CLI structured output via tool: injected schema instruction into messages",
		loggerv2.String("provider", string(a.provider)),
		loggerv2.String("tool_name", toolName),
		loggerv2.Int("schema_length", len(schema)))

	// Call the normal AskWithHistory with injected messages
	textResponse, _, err := a.AskWithHistory(ctx, injectedMessages)
	if err != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to get text response: %w", err)
	}

	logger.Debug("CLI structured output via tool: got text response",
		loggerv2.Int("response_length", len(textResponse)))

	// Try to extract JSON from the text response
	jsonBytes, err := extractJSONFromCLIResponse(textResponse)
	if err != nil {
		// Extraction failed — return as non-structured response (matches API-provider
		// behavior when the tool isn't called by the LLM)
		logger.Debug("CLI structured output via tool: JSON extraction failed, returning as text",
			loggerv2.String("error", err.Error()))

		return StructuredOutputResult[T]{
			HasStructuredOutput: false,
			TextResponse:        textResponse,
		}, nil
	}

	// Unmarshal into the target type
	var result T
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		// Unmarshal failed — return as non-structured response
		logger.Debug("CLI structured output via tool: JSON unmarshal failed, returning as text",
			loggerv2.String("error", err.Error()))

		return StructuredOutputResult[T]{
			HasStructuredOutput: false,
			TextResponse:        textResponse,
		}, nil
	}

	logger.Debug("CLI structured output via tool: successfully parsed structured result",
		loggerv2.String("tool_name", toolName))

	return StructuredOutputResult[T]{
		HasStructuredOutput: true,
		StructuredResult:    result,
		TextResponse:        "",
	}, nil
}
