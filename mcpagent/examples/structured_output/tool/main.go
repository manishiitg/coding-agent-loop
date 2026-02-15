package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

// Person represents a simple person profile
type Person struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

// OrderItem represents an item in an order
type OrderItem struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

// Order represents a complete order
type Order struct {
	OrderID    string      `json:"order_id"`
	CustomerID string      `json:"customer_id"`
	Items      []OrderItem `json:"items"`
	TotalPrice float64     `json:"total_price"`
	Status     string      `json:"status"`
}

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
	agentLogFile := filepath.Join(logDir, "structured-output-tool.log")

	// Clear existing log files to start fresh for this run
	if err := os.Truncate(llmLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear LLM log file: %v\n", err)
	}
	if err := os.Truncate(agentLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear agent log file: %v\n", err)
	}

	// Create logger for LLM operations (API calls, token usage, etc.)
	llmLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",     // Log level: debug, info, warn, error
		Format:     "text",     // Output format: text or json
		Output:     llmLogFile, // Write logs to file
		EnableFile: false,      // Output already set to file, no need for dual output
		FilePath:   "",         // Not needed when Output is set to file path
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create LLM logger: %v\n", err)
		os.Exit(1)
	}
	defer llmLogger.Close() // Ensure log file is closed on exit

	fmt.Printf("LLM logging to: %s (cleared)\n", llmLogFile)

	// Step 3: Initialize OpenAI LLM with file logger
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41,
		Temperature: 0.7,
		Logger:      llmLogger, // Use file logger for LLM operations
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Create logger for agent operations (MCP connections, tool execution, etc.)
	agentLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",       // Log level: debug, info, warn, error
		Format:     "text",       // Output format: text or json
		Output:     agentLogFile, // Write logs to file
		EnableFile: false,        // Output already set to file, no need for dual output
		FilePath:   "",           // Not needed when Output is set to file path
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent logger: %v\n", err)
		os.Exit(1)
	}
	defer agentLogger.Close() // Ensure log file is closed on exit

	fmt.Printf("Agent logging to: %s (cleared)\n", agentLogFile)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 7: Create the agent with logger
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger), // Use file logger for agent operations
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Structured Output as Tool Example ===")
	fmt.Println("This example demonstrates structured output using AskWithHistoryStructuredViaTool")
	fmt.Println("Method: Dynamic tool registration → LLM calls tool → Extract from arguments")
	fmt.Println()

	// Log start to agent log file
	agentLogger.Info("Structured output tool example started")

	// Example 1: Simple Person via tool
	fmt.Println("--- Example 1: Simple Person Profile via Tool ---")
	personSchema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"},
			"email": {"type": "string"}
		},
		"required": ["name", "age", "email"]
	}`

	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Submit a person profile for Alice Smith, age 28, email alice.smith@example.com using the submit_person_profile tool."}},
		},
	}

	result, err := mcpagent.AskWithHistoryStructuredViaTool[Person](
		agent, ctx, messages,
		"submit_person_profile",
		"Submit a person profile with name, age, and email",
		personSchema,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get structured output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Has structured output: %v\n", result.HasStructuredOutput)

	if result.HasStructuredOutput {
		fmt.Printf("✅ Person Profile Extracted from Tool Call:\n")
		fmt.Printf("   Name: %s\n", result.StructuredResult.Name)
		fmt.Printf("   Age: %d\n", result.StructuredResult.Age)
		fmt.Printf("   Email: %s\n", result.StructuredResult.Email)

		personJSON, _ := json.MarshalIndent(result.StructuredResult, "", "  ")
		fmt.Printf("\nJSON Output:\n%s\n\n", string(personJSON))
	} else {
		fmt.Printf("⚠️  Tool not called - LLM provided text response instead:\n")
		fmt.Printf("%s\n\n", result.TextResponse)
		fmt.Println("Note: This is acceptable behavior - the LLM chose a conversational response")
	}

	// Example 2: Complex Order via tool
	fmt.Println("--- Example 2: Complex Order via Tool ---")
	orderSchema := `{
		"type": "object",
		"properties": {
			"order_id": {"type": "string"},
			"customer_id": {"type": "string"},
			"items": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"product_id": {"type": "string"},
						"name": {"type": "string"},
						"quantity": {"type": "integer"},
						"price": {"type": "number"}
					},
					"required": ["product_id", "name", "quantity", "price"]
				}
			},
			"total_price": {"type": "number"},
			"status": {"type": "string"}
		},
		"required": ["order_id", "customer_id", "items", "total_price", "status"]
	}`

	orderMessages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Submit order ORD456 for customer CUST123 with 2 items: Laptop (PROD001) qty 1 at $999.99, Mouse (PROD002) qty 2 at $29.99 each. Total $1059.97, status pending. Use submit_order tool."}},
		},
	}

	orderResult, err := mcpagent.AskWithHistoryStructuredViaTool[Order](
		agent, ctx, orderMessages,
		"submit_order",
		"Submit an order with customer ID, items, and total price",
		orderSchema,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get structured output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Has structured output: %v\n", orderResult.HasStructuredOutput)

	if orderResult.HasStructuredOutput {
		fmt.Printf("✅ Order Extracted from Tool Call:\n")
		fmt.Printf("   Order ID: %s\n", orderResult.StructuredResult.OrderID)
		fmt.Printf("   Customer ID: %s\n", orderResult.StructuredResult.CustomerID)
		fmt.Printf("   Total Price: $%.2f\n", orderResult.StructuredResult.TotalPrice)
		fmt.Printf("   Status: %s\n", orderResult.StructuredResult.Status)
		fmt.Printf("   Items: %d\n", len(orderResult.StructuredResult.Items))

		for i, item := range orderResult.StructuredResult.Items {
			fmt.Printf("   Item %d: %s (Qty: %d, Price: $%.2f)\n",
				i+1, item.Name, item.Quantity, item.Price)
		}

		orderJSON, _ := json.MarshalIndent(orderResult.StructuredResult, "", "  ")
		fmt.Printf("\nJSON Output:\n%s\n\n", string(orderJSON))
	} else {
		fmt.Printf("⚠️  Tool not called - LLM provided text response instead:\n")
		fmt.Printf("%s\n\n", orderResult.TextResponse)
		fmt.Println("Note: This is acceptable behavior - the LLM chose a conversational response")
	}

	fmt.Println("=== Example Complete ===")
	fmt.Println("Note: This method uses 1 LLM call (tool call during conversation)")
	fmt.Println("Pros: Faster and cheaper (single LLM call)")
	fmt.Println("Cons: LLM may not call tool (graceful fallback to text)")

	// Log completion to agent log file
	agentLogger.Info("Structured output tool example completed")
}
