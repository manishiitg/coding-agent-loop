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

// WithStreamingFunc sets the streaming callback function
func WithStreamingFunc(fn func(string)) CallOption {
	return func(opts *CallOptions) {
		opts.StreamingFunc = fn
	}
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
