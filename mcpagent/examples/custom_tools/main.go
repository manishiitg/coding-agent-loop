package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

func main() {
	// Load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load .env file: %v\n", err)
		}
	}

	// Step 1: Get OpenAI API key from environment
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		fmt.Fprintf(os.Stderr, "Please set OPENAI_API_KEY environment variable\n")
		os.Exit(1)
	}

	// Step 2: Set up file loggers
	// Create logs directory if it doesn't exist
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logs directory: %v\n", err)
		os.Exit(1)
	}

	// Define log file paths
	llmLogFile := filepath.Join(logDir, "llm.log")
	agentLogFile := filepath.Join(logDir, "custom-tools.log")

	// Clear existing log files to start fresh for this run
	if err := os.Truncate(llmLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear LLM log file: %v\n", err)
	}
	if err := os.Truncate(agentLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear agent log file: %v\n", err)
	}

	// Create logger for LLM operations
	llmLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",
		Format:     "text",
		Output:     llmLogFile,
		EnableFile: false,
		FilePath:   "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create LLM logger: %v\n", err)
		os.Exit(1)
	}
	defer llmLogger.Close()

	fmt.Printf("LLM logging to: %s (cleared)\n", llmLogFile)

	// Step 3: Initialize OpenAI LLM with file logger
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41,
		Temperature: 0.7,
		Logger:      llmLogger,
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Create logger for agent operations
	agentLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",
		Format:     "text",
		Output:     agentLogFile,
		EnableFile: false,
		FilePath:   "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent logger: %v\n", err)
		os.Exit(1)
	}
	defer agentLogger.Close()

	fmt.Printf("Agent logging to: %s (cleared)\n", agentLogFile)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 7: Create the agent
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Step 8: Register custom tools
	fmt.Println("=== Registering Custom Tools ===")
	fmt.Println()

	// Tool 1: Calculator - utility category
	calculatorParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"add", "subtract", "multiply", "divide", "power", "sqrt"},
				"description": "The mathematical operation to perform",
			},
			"a": map[string]interface{}{
				"type":        "number",
				"description": "First number",
			},
			"b": map[string]interface{}{
				"type":        "number",
				"description": "Second number (not required for sqrt)",
			},
		},
		"required": []string{"operation", "a"},
	}

	err = agent.RegisterCustomTool(
		"calculator",
		"Performs basic mathematical operations: add, subtract, multiply, divide, power, or sqrt",
		calculatorParams,
		calculatorTool,
		"utility",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register calculator tool: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Registered calculator tool (category: utility)")

	// Tool 2: Text Formatter - utility category
	textFormatterParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "The text to format",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"uppercase", "lowercase", "reverse", "title_case"},
				"description": "The formatting operation to apply",
			},
		},
		"required": []string{"text", "format"},
	}

	err = agent.RegisterCustomTool(
		"format_text",
		"Formats text in various ways: uppercase, lowercase, reverse, or title case",
		textFormatterParams,
		textFormatterTool,
		"utility",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register format_text tool: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Registered format_text tool (category: utility)")

	// Tool 3: Weather Simulator - data category
	weatherParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "The location to get weather for",
			},
			"unit": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"celsius", "fahrenheit"},
				"description": "Temperature unit",
			},
		},
		"required": []string{"location"},
	}

	err = agent.RegisterCustomTool(
		"get_weather",
		"Gets simulated weather data for a given location. Returns temperature, condition, and humidity.",
		weatherParams,
		weatherTool,
		"data",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register get_weather tool: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Registered get_weather tool (category: data)")

	// Tool 4: String Counter - utility category
	stringCounterParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "The text to analyze",
			},
			"count_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"characters", "words", "sentences", "paragraphs"},
				"description": "What to count in the text",
			},
		},
		"required": []string{"text", "count_type"},
	}

	err = agent.RegisterCustomTool(
		"count_text",
		"Counts characters, words, sentences, or paragraphs in a given text",
		stringCounterParams,
		stringCounterTool,
		"utility",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register count_text tool: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Registered count_text tool (category: utility)")

	fmt.Println()
	fmt.Println("=== Custom Tools Example ===")
	fmt.Println("The agent now has access to custom tools alongside MCP server tools")
	fmt.Println()

	// Log start to agent log file
	agentLogger.Info("Custom tools example started")

	// Step 9: Example questions that will use custom tools
	questions := []string{
		"Calculate 15 multiplied by 23",
		"Format the text 'Hello World' to uppercase",
		"What's the weather like in San Francisco?",
		"Count the number of words in 'The quick brown fox jumps over the lazy dog'",
		"Calculate the square root of 144",
		"Get weather for New York in fahrenheit and format the location name to title case",
	}

	// Allow custom questions via command line
	if len(os.Args) > 2 {
		questions = os.Args[2:]
	}

	// Step 10: Ask questions and demonstrate custom tool usage
	for i, question := range questions {
		fmt.Printf("--- Example %d ---\n", i+1)
		fmt.Printf("You: %s\n\n", question)

		answer, err := agent.Ask(ctx, question)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
			continue
		}

		fmt.Printf("Agent: %s\n\n", answer)
		fmt.Println("---")
		fmt.Println()
	}

	fmt.Println("=== Example Complete ===")
	fmt.Println("Custom tools were used alongside MCP server tools")
	fmt.Println("Check the logs directory for detailed execution logs")

	// Log completion to agent log file
	agentLogger.Info("Custom tools example completed")
}

// calculatorTool performs mathematical operations
func calculatorTool(ctx context.Context, args map[string]interface{}) (string, error) {
	operation, ok := args["operation"].(string)
	if !ok {
		return "", fmt.Errorf("operation must be a string")
	}

	a, ok := args["a"].(float64)
	if !ok {
		return "", fmt.Errorf("a must be a number")
	}

	var result float64

	switch operation {
	case "add":
		b, ok := args["b"].(float64)
		if !ok {
			return "", fmt.Errorf("b must be a number for addition")
		}
		result = a + b
	case "subtract":
		b, ok := args["b"].(float64)
		if !ok {
			return "", fmt.Errorf("b must be a number for subtraction")
		}
		result = a - b
	case "multiply":
		b, ok := args["b"].(float64)
		if !ok {
			return "", fmt.Errorf("b must be a number for multiplication")
		}
		result = a * b
	case "divide":
		b, ok := args["b"].(float64)
		if !ok {
			return "", fmt.Errorf("b must be a number for division")
		}
		if b == 0 {
			return "", fmt.Errorf("division by zero is not allowed")
		}
		result = a / b
	case "power":
		b, ok := args["b"].(float64)
		if !ok {
			return "", fmt.Errorf("b must be a number for power operation")
		}
		result = math.Pow(a, b)
	case "sqrt":
		if a < 0 {
			return "", fmt.Errorf("cannot calculate square root of negative number")
		}
		result = math.Sqrt(a)
	default:
		return "", fmt.Errorf("unknown operation: %s", operation)
	}

	return fmt.Sprintf("Result: %.2f", result), nil
}

// textFormatterTool formats text in various ways
func textFormatterTool(ctx context.Context, args map[string]interface{}) (string, error) {
	text, ok := args["text"].(string)
	if !ok {
		return "", fmt.Errorf("text must be a string")
	}

	format, ok := args["format"].(string)
	if !ok {
		return "", fmt.Errorf("format must be a string")
	}

	var result string

	switch format {
	case "uppercase":
		result = strings.ToUpper(text)
	case "lowercase":
		result = strings.ToLower(text)
	case "reverse":
		runes := []rune(text)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		result = string(runes)
	case "title_case":
		words := strings.Fields(text)
		for i, word := range words {
			if len(word) > 0 {
				words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
			}
		}
		result = strings.Join(words, " ")
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}

	return fmt.Sprintf("Formatted text: %s", result), nil
}

// weatherTool simulates weather data
func weatherTool(ctx context.Context, args map[string]interface{}) (string, error) {
	location, ok := args["location"].(string)
	if !ok {
		return "", fmt.Errorf("location must be a string")
	}

	unit := "celsius"
	if u, ok := args["unit"].(string); ok {
		unit = u
	}

	// Simulate weather data based on location hash
	locationHash := 0
	for _, char := range location {
		locationHash += int(char)
	}

	// Generate consistent "random" values based on location
	tempC := float64(locationHash%30) + 10 // 10-40°C
	if unit == "fahrenheit" {
		tempC = tempC*9/5 + 32
	}

	conditions := []string{"sunny", "cloudy", "rainy", "partly cloudy"}
	condition := conditions[locationHash%len(conditions)]

	humidity := locationHash%40 + 40 // 40-80%

	unitSymbol := "°C"
	if unit == "fahrenheit" {
		unitSymbol = "°F"
	}

	return fmt.Sprintf("Weather for %s: %.1f%s, %s, humidity %d%%", location, tempC, unitSymbol, condition, humidity), nil
}

// stringCounterTool counts various elements in text
func stringCounterTool(ctx context.Context, args map[string]interface{}) (string, error) {
	text, ok := args["text"].(string)
	if !ok {
		return "", fmt.Errorf("text must be a string")
	}

	countType, ok := args["count_type"].(string)
	if !ok {
		return "", fmt.Errorf("count_type must be a string")
	}

	var count int
	var label string

	switch countType {
	case "characters":
		count = len(text)
		label = "characters"
	case "words":
		words := strings.Fields(text)
		count = len(words)
		label = "words"
	case "sentences":
		// Simple sentence counting - count periods, exclamation marks, question marks
		count = 0
		for _, char := range text {
			if char == '.' || char == '!' || char == '?' {
				count++
			}
		}
		label = "sentences"
	case "paragraphs":
		paragraphs := strings.Split(strings.TrimSpace(text), "\n\n")
		count = len(paragraphs)
		label = "paragraphs"
	default:
		return "", fmt.Errorf("unknown count_type: %s", countType)
	}

	return fmt.Sprintf("Text contains %d %s", count, label), nil
}
