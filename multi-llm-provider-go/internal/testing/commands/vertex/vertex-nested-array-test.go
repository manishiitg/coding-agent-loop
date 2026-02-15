package vertex

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// This test reproduces the nested array items.items missing field issue
// Error: GenerateContentRequest.tools[0].function_declarations[X].parameters.properties[values].items.items: missing field

var VertexNestedArrayTestCmd = &cobra.Command{
	Use:   "vertex-nested-array-test",
	Short: "Test nested array (array of arrays) schema conversion for Gemini",
	Long: `This test reproduces the issue where nested arrays (arrays of arrays) 
are missing the items.items field, causing Gemini API to return:
Error 400: GenerateContentRequest.tools[0].function_declarations[X].parameters.properties[values].items.items: missing field`,
	Run: runVertexNestedArrayTest,
}

func runVertexNestedArrayTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	fmt.Println("🧪 Testing Nested Array Schema Conversion for Gemini")
	fmt.Println("====================================================")
	fmt.Println()

	// Test Case 1: Array of arrays (nested array) - MISSING items.items
	// This is the problematic case from the error log
	fmt.Println("Test Case 1: Array of arrays with missing items.items")
	fmt.Println("----------------------------------------------------")

	tool1 := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "test_nested_array_values",
			Description: "Test function with nested array (array of arrays) - missing items.items",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"values": map[string]interface{}{
						"type":        "array",
						"description": "Array of arrays - this is the problematic case",
						"items": map[string]interface{}{
							"type": "array",
							// MISSING: items.items field here!
							// This should have: "items": {"type": "string"} or similar
						},
					},
				},
			}),
		},
	}

	// Test Case 2: Another nested array case - initialData
	fmt.Println("\nTest Case 2: Array of arrays with missing items.items (initialData)")
	fmt.Println("-------------------------------------------------------------------")

	tool2 := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "test_nested_array_initialData",
			Description: "Test function with nested array in initialData - missing items.items",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"initialData": map[string]interface{}{
						"type":        "array",
						"description": "Array of arrays - missing items.items",
						"items": map[string]interface{}{
							"type": "array",
							// MISSING: items.items field here!
						},
					},
				},
			}),
		},
	}

	// Test Case 3: Properly formatted nested array (for comparison)
	fmt.Println("\nTest Case 3: Properly formatted nested array (should work)")
	fmt.Println("-----------------------------------------------------------")

	tool3 := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "test_nested_array_correct",
			Description: "Test function with properly formatted nested array",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"values": map[string]interface{}{
						"type":        "array",
						"description": "Array of arrays - properly formatted",
						"items": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string", // This is what's missing in the broken cases
							},
						},
					},
				},
			}),
		},
	}

	// Display the schemas
	tools := []llmtypes.Tool{tool1, tool2, tool3}

	for i, tool := range tools {
		fmt.Printf("\n📋 Tool %d: %s\n", i+1, tool.Function.Name)
		fmt.Println("Schema:")
		if tool.Function.Parameters != nil {
			// Convert to map for display
			schemaMap := make(map[string]interface{})
			if tool.Function.Parameters.Type != "" {
				schemaMap["type"] = tool.Function.Parameters.Type
			}
			if tool.Function.Parameters.Properties != nil {
				schemaMap["properties"] = tool.Function.Parameters.Properties
			}

			jsonBytes, err := json.MarshalIndent(schemaMap, "  ", "  ")
			if err != nil {
				log.Printf("Error marshaling schema: %v", err)
			} else {
				fmt.Println(string(jsonBytes))
			}

			// Check for the issue
			if i < 2 { // First two tools have the issue
				fmt.Println("❌ ISSUE DETECTED: This schema has nested array missing items.items")
				fmt.Println("   Expected structure:")
				fmt.Println("   properties.values.items.items: {type: \"string\"}")
				fmt.Println("   Actual structure:")
				fmt.Println("   properties.values.items: {type: \"array\"} (missing items.items)")
			} else {
				fmt.Println("✅ CORRECT: This schema has properly nested items.items")
			}
		}
	}

	fmt.Println("\n\n🔍 Validation Check")
	fmt.Println("===================")
	fmt.Println()

	// Check each tool's schema for nested array issues
	for i, tool := range tools {
		if tool.Function.Parameters == nil {
			continue
		}

		hasIssue := checkNestedArrayIssue(tool.Function.Parameters.Properties, tool.Function.Name)
		if hasIssue {
			fmt.Printf("❌ Tool %d (%s): Has nested array issue - missing items.items\n", i+1, tool.Function.Name)
		} else {
			fmt.Printf("✅ Tool %d (%s): No nested array issues detected\n", i+1, tool.Function.Name)
		}
	}

	fmt.Println("\n\n🧪 Testing with Actual LLM Adapter")
	fmt.Println("====================================")
	fmt.Println()

	// Try to actually use the adapter with problematic tools to reproduce the error
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		fmt.Println("⚠️  VERTEX_API_KEY or GOOGLE_API_KEY not set - skipping actual API test")
		fmt.Println("   Set the API key to test the actual Gemini API error")
	} else {
		fmt.Println("✅ API key found - attempting to reproduce the actual error...")
		fmt.Println()

		ctx := context.Background()

		// Create Vertex AI LLM
		vertexLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
			Provider:    llmproviders.ProviderVertex,
			ModelID:     "gemini-2.5-flash", // Use a model that supports tools
			Temperature: 0,
			Context:     ctx,
		})
		if err != nil {
			log.Printf("❌ Failed to create Vertex AI LLM: %v", err)
			fmt.Println("   (This is expected if credentials are not properly configured)")
		} else {
			// Test with the problematic tool
			fmt.Println("Testing with problematic nested array tool...")
			messages := []llmtypes.MessageContent{
				{
					Role: llmtypes.ChatMessageTypeHuman,
					Parts: []llmtypes.ContentPart{
						llmtypes.TextContent{Text: "Test"},
					},
				},
			}

			// Try to generate content with the problematic tool
			_, err := vertexLLM.GenerateContent(ctx, messages,
				llmtypes.WithTools([]llmtypes.Tool{tool1}),
			)

			if err != nil {
				fmt.Printf("❌ Error occurred (as expected): %v\n", err)
				if contains(err.Error(), "items.items") || contains(err.Error(), "missing field") {
					fmt.Println("✅ Successfully reproduced the nested array items.items error!")
				} else {
					fmt.Println("⚠️  Got an error, but it might not be the specific nested array error")
				}
			} else {
				fmt.Println("⚠️  No error occurred - the issue might have been fixed or the API accepted it")
			}
		}
	}

	fmt.Println("\n\n📝 Summary")
	fmt.Println("==========")
	fmt.Println("This test reproduces the Gemini API error:")
	fmt.Println("  Error 400: GenerateContentRequest.tools[0].function_declarations[X]")
	fmt.Println("  .parameters.properties[values].items.items: missing field")
	fmt.Println()
	fmt.Println("The issue occurs when:")
	fmt.Println("  1. A property is of type 'array'")
	fmt.Println("  2. The items field is also of type 'array'")
	fmt.Println("  3. The nested items.items field is missing")
	fmt.Println()
	fmt.Println("Fix required:")
	fmt.Println("  - Normalize nested arrays to ensure items.items exists")
	fmt.Println("  - When items.type == 'array', ensure items.items is defined")
	fmt.Println("  - Update normalizeArrayParameters to handle nested arrays recursively")
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// checkNestedArrayIssue recursively checks for nested array issues
func checkNestedArrayIssue(properties map[string]interface{}, path string) bool {
	if properties == nil {
		return false
	}

	hasIssue := false
	for propName, propValue := range properties {
		propMap, ok := propValue.(map[string]interface{})
		if !ok {
			continue
		}

		propType, ok := propMap["type"].(string)
		if !ok {
			continue
		}

		currentPath := path + "." + propName

		if propType == "array" {
			items, ok := propMap["items"].(map[string]interface{})
			if !ok {
				// Array without items - this is also an issue but different
				continue
			}

			itemsType, ok := items["type"].(string)
			if ok && itemsType == "array" {
				// This is a nested array - check if items.items exists
				if _, hasItemsItems := items["items"]; !hasItemsItems {
					fmt.Printf("   ⚠️  Found issue at %s: nested array missing items.items\n", currentPath)
					hasIssue = true
				} else {
					// Recursively check deeper nesting
					if nestedProps, ok := items["items"].(map[string]interface{}); ok {
						if checkNestedArrayIssue(map[string]interface{}{"nested": nestedProps}, currentPath+".items") {
							hasIssue = true
						}
					}
				}
			}
		} else if propType == "object" {
			// Recursively check nested objects
			if nestedProps, ok := propMap["properties"].(map[string]interface{}); ok {
				if checkNestedArrayIssue(nestedProps, currentPath) {
					hasIssue = true
				}
			}
		}
	}

	return hasIssue
}
