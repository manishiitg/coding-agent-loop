module mcpagent

go 1.24.4

require (
	github.com/invopop/jsonschema v0.13.0
	github.com/mark3labs/mcp-go v0.42.0
	github.com/pkoukk/tiktoken-go v0.1.6
	github.com/sirupsen/logrus v1.9.3
	llm-providers v0.0.0
)

replace llm-providers => ../llm-providers
