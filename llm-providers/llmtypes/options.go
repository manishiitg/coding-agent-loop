package llmtypes

// WithModel sets the model ID
func WithModel(model string) CallOption {
	return func(opts *CallOptions) {
		opts.Model = model
	}
}

// WithTemperature sets the temperature
func WithTemperature(temperature float64) CallOption {
	return func(opts *CallOptions) {
		opts.Temperature = temperature
	}
}

// WithMaxTokens sets the maximum tokens
func WithMaxTokens(maxTokens int) CallOption {
	return func(opts *CallOptions) {
		opts.MaxTokens = maxTokens
	}
}

// WithJSONMode enables JSON mode
func WithJSONMode() CallOption {
	return func(opts *CallOptions) {
		opts.JSONMode = true
	}
}

// WithJSONSchema enables JSON Schema structured outputs
// schema: The JSON Schema definition as a map
// name: The name of the schema
// description: Description of what the schema represents
// strict: Whether to enforce strict schema compliance (default: true)
func WithJSONSchema(schema map[string]interface{}, name, description string, strict bool) CallOption {
	return func(opts *CallOptions) {
		opts.JSONSchema = &JSONSchemaConfig{
			Name:        name,
			Description: description,
			Schema:      schema,
			Strict:      strict,
		}
	}
}

// WithTools sets the tools available for the LLM
func WithTools(tools []Tool) CallOption {
	return func(opts *CallOptions) {
		opts.Tools = tools
	}
}

// WithToolChoice sets the tool choice strategy
func WithToolChoice(toolChoice *ToolChoice) CallOption {
	return func(opts *CallOptions) {
		opts.ToolChoice = toolChoice
	}
}

// WithToolChoiceString creates a ToolChoice from a string type ("auto", "none", "required") and sets it
func WithToolChoiceString(choiceType string) CallOption {
	return func(opts *CallOptions) {
		opts.ToolChoice = &ToolChoice{Type: choiceType}
	}
}

// WithStreamingChan sets the streaming channel for receiving chunks
// The channel receives structured StreamChunk objects that can be either content or tool calls
// The channel will be closed when streaming completes
func WithStreamingChan(ch chan<- StreamChunk) CallOption {
	return func(opts *CallOptions) {
		opts.StreamChan = ch
	}
}

// WithStreamingFunc is a convenience function that creates a channel and callback
// This maintains backward compatibility for simple use cases
// For better control, use WithStreamingChan directly
func WithStreamingFunc(fn func(StreamChunk)) CallOption {
	ch := make(chan StreamChunk, 100) // Buffered channel to avoid blocking
	go func() {
		for chunk := range ch {
			fn(chunk)
		}
	}()
	return WithStreamingChan(ch)
}

// TextPart creates a single text part message content
func TextPart(role ChatMessageType, text string) MessageContent {
	return MessageContent{
		Role:  role,
		Parts: []ContentPart{TextContent{Text: text}},
	}
}

// TextParts creates a message content with multiple text parts
func TextParts(role ChatMessageType, texts ...string) MessageContent {
	parts := make([]ContentPart, len(texts))
	for i, text := range texts {
		parts[i] = TextContent{Text: text}
	}
	return MessageContent{
		Role:  role,
		Parts: parts,
	}
}

// ImagePart creates a message content with a single image part
// sourceType should be "base64" or "url"
// For base64: mediaType is required (e.g., "image/jpeg"), data is base64-encoded string
// For url: mediaType is ignored, data is the image URL
func ImagePart(role ChatMessageType, sourceType, mediaType, data string) MessageContent {
	return MessageContent{
		Role: role,
		Parts: []ContentPart{
			ImageContent{
				SourceType: sourceType,
				MediaType:  mediaType,
				Data:       data,
			},
		},
	}
}

// ImagePartBase64 creates a message content with a base64-encoded image
func ImagePartBase64(role ChatMessageType, mediaType, base64Data string) MessageContent {
	return ImagePart(role, "base64", mediaType, base64Data)
}

// ImagePartURL creates a message content with an image URL
func ImagePartURL(role ChatMessageType, imageURL string) MessageContent {
	return ImagePart(role, "url", "", imageURL)
}

// WithEmbeddingModel sets the embedding model ID
func WithEmbeddingModel(model string) EmbeddingOption {
	return func(opts *EmbeddingOptions) {
		opts.Model = model
	}
}

// WithDimensions sets the dimensions parameter for embedding generation
// This is only supported for text-embedding-3 models
func WithDimensions(dimensions int) EmbeddingOption {
	return func(opts *EmbeddingOptions) {
		opts.Dimensions = &dimensions
	}
}

// WithReasoningEffort sets the reasoning effort level for models that support it (e.g., gpt-5.1)
// Valid values: "minimal", "low", "medium", "high"
// When set to "minimal", the model uses minimal reasoning effort
// Higher values enable deeper reasoning for complex problems
func WithReasoningEffort(effort string) CallOption {
	return func(opts *CallOptions) {
		opts.ReasoningEffort = effort
	}
}

// WithVerbosity sets the verbosity level for the model's response (for reasoning models)
// Valid values: "low", "medium", "high"
// Lower values result in more concise responses, higher values result in more verbose responses
func WithVerbosity(verbosity string) CallOption {
	return func(opts *CallOptions) {
		opts.Verbosity = verbosity
	}
}

// WithThinkingLevel sets the thinking level for models that support it (e.g., Gemini 3 Pro)
// Valid values: "low", "high"
// "low" reduces latency for simpler tasks, "high" enables deeper reasoning for complex tasks.
// Default is "high" for Gemini 3 Pro.
func WithThinkingLevel(level string) CallOption {
	return func(opts *CallOptions) {
		opts.ThinkingLevel = level
	}
}
