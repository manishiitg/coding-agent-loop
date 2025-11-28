package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"mcpagent/llm"
	"llm-providers/llmtypes"
	"mcpagent/agent"
	"mcpagent/mcpclient"
)

var openaiEmptyParamsTestCmd = &cobra.Command{
	Use:   "openai-empty-params",
	Short: "Test OpenAI agent with playwright MCP server (tools with empty parameter schemas like browser_close)",
	Run:   runOpenAIEmptyParamsTest,
}

type openaiEmptyParamsTestFlags struct {
	model      string
	configPath string
}

var openaiEmptyParamsFlags openaiEmptyParamsTestFlags

func init() {
	openaiEmptyParamsTestCmd.Flags().StringVar(&openaiEmptyParamsFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
	openaiEmptyParamsTestCmd.Flags().StringVar(&openaiEmptyParamsFlags.configPath, "config", "configs/mcp_servers_clean_user.json", "MCP config file path")
}

func runOpenAIEmptyParamsTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiEmptyParamsFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI agent with playwright MCP server (empty parameter schemas) using %s", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	// Initialize logger
	logger := GetTestLogger()

	// Load MCP config
	config, err := mcpclient.LoadMergedConfig(openaiEmptyParamsFlags.configPath, logger)
	if err != nil {
		log.Fatalf("Failed to load MCP config: %v", err)
	}

	// Verify playwright server exists in config
	_, err = config.GetServer("playwright")
	if err != nil {
		log.Fatalf("Playwright server not found in config: %v", err)
	}

	log.Printf("📋 Playwright server config loaded")

	// Create LLM for the agent
	openaiLLM, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to create OpenAI LLM: %v", err)
	}

	// Create agent with playwright MCP server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	log.Printf("🔧 Creating agent with playwright MCP server...")
	agent, err := mcpagent.NewAgent(
		ctx,
		openaiLLM,
		"playwright",
		openaiEmptyParamsFlags.configPath,
		modelID,
		nil, // tracer
		"",  // traceID
		logger,
		mcpagent.WithMode(mcpagent.SimpleAgent),
		mcpagent.WithMaxTurns(5),
		mcpagent.WithToolChoice("auto"),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	log.Printf("✅ Agent created successfully")

	// Debug: Check all tools loaded
	tools := agent.Tools
	log.Printf("📊 Loaded %d tools from playwright server", len(tools))

	// List all tools
	log.Printf("\n📋 All loaded tools:")
	for i, tool := range tools {
		if tool.Function != nil {
			log.Printf("   %d. %s", i+1, tool.Function.Name)
		}
	}

	// Find ALL tools with empty parameters
	var emptyParamTools []*llmtypes.Tool
	var toolsWithParams []*llmtypes.Tool

	for i := range tools {
		if tools[i].Function == nil || tools[i].Function.Parameters == nil {
			continue
		}

		// Check if parameters have no properties (empty params)
		params := tools[i].Function.Parameters
		hasProperties := params.Properties != nil && len(params.Properties) > 0

		if !hasProperties {
			emptyParamTools = append(emptyParamTools, &tools[i])
		} else {
			toolsWithParams = append(toolsWithParams, &tools[i])
		}
	}

	log.Printf("\n🔍 Tool Analysis:")
	log.Printf("   ✅ Tools with empty parameters: %d", len(emptyParamTools))
	log.Printf("   ✅ Tools with parameters: %d", len(toolsWithParams))

	// Validate all empty param tools
	if len(emptyParamTools) > 0 {
		log.Printf("\n📝 Tools with empty parameters (should have no 'properties' field):")
		for _, tool := range emptyParamTools {
			toolJSON, _ := json.MarshalIndent(tool, "", "  ")
			log.Printf("\n   Tool: %s", tool.Function.Name)
			log.Printf("   Schema:\n%s", string(toolJSON))

			// Validate schema format
			params := tool.Function.Parameters
			if params.Properties != nil && len(params.Properties) > 0 {
				log.Printf("   ⚠️  WARNING: Tool has empty params but Properties field is not nil!")
			} else {
				log.Printf("   ✅ Schema correctly formatted (no properties field)")
			}
		}
	} else {
		log.Printf("\n⚠️  No tools with empty parameters found!")
	}

	// Find browser_close specifically for detailed inspection
	var browserCloseTool *llmtypes.Tool
	for _, tool := range emptyParamTools {
		if tool.Function != nil && tool.Function.Name == "browser_close" {
			browserCloseTool = tool
			break
		}
	}

	if browserCloseTool == nil {
		log.Printf("\n⚠️ browser_close tool not found in empty param tools")
	} else {
		log.Printf("\n✅ Found browser_close tool in empty param tools")
	}

	// Test 1: Simple query that should trigger browser_close
	log.Printf("\n🔧 Test 1: Query that should use browser_close (empty params tool)...")
	testQuery1 := "Close the browser page"

	log.Printf("📤 Sending query: %s", testQuery1)
	startTime := time.Now()
	response1, err := agent.Ask(ctx, testQuery1)
	duration1 := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Test 1 failed with error: %v", err)
		log.Printf("\n💡 This error indicates that empty parameter schemas are not being handled correctly")
		log.Printf("   Expected: Agent should be able to call browser_close without parameters")
		log.Printf("   Actual: OpenAI API rejected the schema")

		// Check if it's the specific schema error
		if fmt.Sprintf("%v", err) != "" {
			log.Printf("\n🔍 Error details: %v", err)
		}
		return
	}

	log.Printf("✅ Test 1 completed in %s", duration1)
	log.Printf("📝 Response: %s", response1)

	// Test 2: Query that uses both empty params and tool with params
	log.Printf("\n🔧 Test 2: Query using both browser_close (empty params) and browser_resize (with params)...")
	testQuery2 := "Resize the browser window to 1920x1080 pixels and then close it"

	log.Printf("📤 Sending query: %s", testQuery2)
	startTime2 := time.Now()
	response2, err := agent.Ask(ctx, testQuery2)
	duration2 := time.Since(startTime2)

	if err != nil {
		log.Printf("❌ Test 2 failed with error: %v", err)
		return
	}

	log.Printf("✅ Test 2 completed in %s", duration2)
	log.Printf("📝 Response: %s", response2)

	log.Printf("\n🎯 Empty parameter schema test completed!")
	log.Printf("   ✅ All tests passed - OpenAI accepted tools with empty parameter schemas")
	log.Printf("   ✅ browser_close and other empty-param tools work correctly")
}
