package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

type Person struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

type OrderItem struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type Order struct {
	OrderID    string      `json:"order_id"`
	CustomerID string      `json:"customer_id"`
	Items      []OrderItem `json:"items"`
	TotalPrice float64     `json:"total_price"`
	Status     string      `json:"status"`
}

var structuredOutputToolTestCmd = &cobra.Command{
	Use:   "structured-output-tool",
	Short: "Test Model 2: Tool-Based Model (AskWithHistoryStructuredViaTool)",
	Long: `Test structured output using Tool-Based Model.

Model 2: Dynamic tool registration → LLM calls tool → Extract from arguments
- Pros: Single LLM call (faster, cheaper)
- Cons: LLM may not call tool (graceful fallback)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStructuredOutputToolTest()
	},
}

func GetStructuredOutputToolTestCmd() *cobra.Command {
	return structuredOutputToolTestCmd
}

func runStructuredOutputToolTest() error {
	logger := testutils.NewTestLoggerFromViper()

	logger.Info("=== Structured Output Tool Test (Model 2) ===")
	logger.Info("Testing Tool-Based Model: AskWithHistoryStructuredViaTool")

	llm, llmProvider, err := testutils.CreateTestLLMFromViper(logger)
	if err != nil {
		return fmt.Errorf("failed to create LLM: %w", err)
	}

	tracerProvider := viper.GetString("tracer")
	tracer, _ := testutils.GetTracerWithLogger(tracerProvider, logger)
	traceID := testutils.GenerateTestTraceID()

	ctx := context.Background()
	agent, err := testutils.CreateMinimalAgent(ctx, llm, llmProvider, tracer, traceID, logger)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	logger.Info("✅ Agent created successfully")

	testsPassed := 0
	testsFailed := 0

	if err := TestSimpleToolBasedStructuredOutput(agent, ctx, logger); err != nil {
		logger.Error("❌ TestSimpleToolBasedStructuredOutput failed", err)
		testsFailed++
	} else {
		testsPassed++
	}

	if err := TestComplexToolBasedStructuredOutput(agent, ctx, logger); err != nil {
		logger.Error("❌ TestComplexToolBasedStructuredOutput failed", err)
		testsFailed++
	} else {
		testsPassed++
	}

	logger.Info("=== Structured Output Tool Test Complete ===")
	logger.Info(fmt.Sprintf("📊 Tests passed: %d, Tests failed: %d", testsPassed, testsFailed))

	return nil
}

func TestSimpleToolBasedStructuredOutput(agent *mcpagent.Agent, ctx context.Context, logger loggerv2.Logger) error {
	logger.Info("🧪 Test 1: Simple Tool-Based - Person via submit_person_profile")

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
		logger.Error("❌ AskWithHistoryStructuredViaTool failed", err)
		return err
	}

	logger.Info("✅ AskWithHistoryStructuredViaTool successful")
	logger.Info(fmt.Sprintf("Has structured output: %v", result.HasStructuredOutput))

	if result.HasStructuredOutput {
		logger.Info(fmt.Sprintf("Person: %s, Age: %d, Email: %s",
			result.StructuredResult.Name, result.StructuredResult.Age, result.StructuredResult.Email))

		if result.StructuredResult.Name == "" || result.StructuredResult.Age == 0 {
			return fmt.Errorf("person struct validation failed")
		}

		jsonBytes, _ := json.MarshalIndent(result.StructuredResult, "", "  ")
		logger.Info(fmt.Sprintf("Person JSON:\n%s", string(jsonBytes)))
		logger.Info("✅ Test 1 passed: Tool-based extraction successful")
	} else {
		logger.Warn("⚠️ Tool not called - text response instead (acceptable)")
		logger.Info(fmt.Sprintf("Text response: %s", result.TextResponse))
		logger.Info("✅ Test 1 passed: Graceful fallback")
	}

	return nil
}

func TestComplexToolBasedStructuredOutput(agent *mcpagent.Agent, ctx context.Context, logger loggerv2.Logger) error {
	logger.Info("🧪 Test 2: Complex Tool-Based - Order via submit_order")

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

	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Submit order ORD456 for customer CUST123 with 2 items: Laptop (PROD001) qty 1 at $999.99, Mouse (PROD002) qty 2 at $29.99 each. Total $1059.97, status pending. Use submit_order tool."}},
		},
	}

	result, err := mcpagent.AskWithHistoryStructuredViaTool[Order](
		agent, ctx, messages,
		"submit_order",
		"Submit an order with customer ID, items, and total price",
		orderSchema,
	)

	if err != nil {
		logger.Error("❌ AskWithHistoryStructuredViaTool failed", err)
		return err
	}

	logger.Info("✅ AskWithHistoryStructuredViaTool successful")
	logger.Info(fmt.Sprintf("Has structured output: %v", result.HasStructuredOutput))

	if result.HasStructuredOutput {
		logger.Info(fmt.Sprintf("Order ID: %s, Customer: %s, Items: %d, Total: $%.2f",
			result.StructuredResult.OrderID, result.StructuredResult.CustomerID,
			len(result.StructuredResult.Items), result.StructuredResult.TotalPrice))

		if result.StructuredResult.OrderID == "" || len(result.StructuredResult.Items) == 0 {
			return fmt.Errorf("order struct validation failed")
		}

		for i, item := range result.StructuredResult.Items {
			logger.Info(fmt.Sprintf("Item %d: %s (Qty: %d, Price: $%.2f)",
				i+1, item.Name, item.Quantity, item.Price))
		}

		jsonBytes, _ := json.MarshalIndent(result.StructuredResult, "", "  ")
		logger.Info(fmt.Sprintf("Order JSON:\n%s", string(jsonBytes)))
		logger.Info("✅ Test 2 passed: Complex structure via tool successful")
	} else {
		logger.Warn("⚠️ Tool not called - text response instead (acceptable)")
		logger.Info("✅ Test 2 passed: Graceful fallback")
	}

	return nil
}
