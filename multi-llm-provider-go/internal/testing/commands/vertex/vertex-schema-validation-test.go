package vertex

import (
	"context"
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var VertexSchemaValidationTestCmd = &cobra.Command{
	Use:   "vertex-schema-validation",
	Short: "Test Vertex AI (Gemini) schema validation - reproduces Error 400 for invalid schema structures",
	Run:   runVertexSchemaValidationTest,
}

type vertexSchemaValidationTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var vertexSchemaValidationFlags vertexSchemaValidationTestFlags

func init() {
	VertexSchemaValidationTestCmd.Flags().StringVar(&vertexSchemaValidationFlags.model, "model", "", "Vertex AI model to test (default: gemini-3-flash-preview)")
	VertexSchemaValidationTestCmd.Flags().BoolVar(&vertexSchemaValidationFlags.record, "record", false, "Record LLM responses to testdata/")
	VertexSchemaValidationTestCmd.Flags().BoolVar(&vertexSchemaValidationFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	VertexSchemaValidationTestCmd.Flags().StringVar(&vertexSchemaValidationFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runVertexSchemaValidationTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get model ID
	modelID := vertexSchemaValidationFlags.model
	if modelID == "" {
		modelID = "gemini-3-flash-preview"
	}

	// Check for API key
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
	}

	// Set API key as environment variable
	_ = os.Setenv("VERTEX_API_KEY", apiKey) //nolint:errcheck // Test code, safe to ignore

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if vertexSchemaValidationFlags.record || vertexSchemaValidationFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  vertexSchemaValidationFlags.record,
			TestName: "schema_validation",
			Provider: "vertex",
			ModelID:  modelID,
			BaseDir:  vertexSchemaValidationFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if vertexSchemaValidationFlags.replay {
			rec.SetReplayMode(true)
		}

		if vertexSchemaValidationFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", vertexSchemaValidationFlags.testDir)
		}
		if vertexSchemaValidationFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", vertexSchemaValidationFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create Vertex AI LLM using our adapter
	vertexLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("❌ Failed to create Vertex AI LLM: %v", err)
	}

	logger.Infof("🧪 Testing schema validation error reproduction")
	logger.Infof("📋 This test reproduces the Error 400: 'only allowed for OBJECT type'")
	logger.Infof("   The issue occurs when schemas have properties/required/items without proper type constraints")

	// Create a tool with problematic schema that reproduces the error
	// This mimics a Notion-like API schema with complex nested structures
	// CRITICAL: This schema has properties but does NOT have type="object" at root level
	// This reproduces the error: "parameters.properties: only allowed for OBJECT type"
	problematicTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_page",
			Description: "Create a new page with properties, icon, and cover",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				// NOTE: Intentionally missing "type": "object" to reproduce the error
				"properties": map[string]interface{}{
					"properties": map[string]interface{}{
						// This nested "properties" property has properties but type is not "object"
						// This reproduces: "parameters.properties[properties].properties: only allowed for OBJECT type"
						"title": map[string]interface{}{
							// This has items but type is not "array"
							// This reproduces: "parameters.properties[properties].properties[title].items: field predicate failed: $type == Type.ARRAY"
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"text": map[string]interface{}{
										"type": "string",
									},
								},
							},
						},
					},
					"icon": map[string]interface{}{
						// This has properties but type is not "object"
						// This reproduces: "parameters.properties[icon].properties: only allowed for OBJECT type"
						"properties": map[string]interface{}{
							"type": map[string]interface{}{
								"type": "string",
							},
						},
						"required": []string{"type"}, // This also requires type="object"
					},
					"cover": map[string]interface{}{
						// This has properties but type is not "object"
						"properties": map[string]interface{}{
							"external": map[string]interface{}{
								// This has properties but type is not "object"
								"properties": map[string]interface{}{
									"url": map[string]interface{}{
										"type": "string",
									},
								},
								"required": []string{"url"}, // This also requires type="object"
							},
						},
						"required": []string{"external"}, // This also requires type="object"
					},
				},
				"required": []string{"properties"},
			}),
		},
	}

	// Try to use the tool - this should trigger the schema validation error
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Create a page with title 'Test Page'"},
			},
		},
	}

	logger.Infof("📤 Sending request with problematic schema...")
	logger.Infof("⚠️  Expected: Error 400 with 'only allowed for OBJECT type' messages")

	_, err = vertexLLM.GenerateContent(ctx, messages, llmtypes.WithTools([]llmtypes.Tool{problematicTool}))
	if err != nil {
		logger.Infof("❌ Error received (expected): %v", err)
		// Check if it's the specific error we're testing for
		if errorContains(err.Error(), "only allowed for OBJECT type") || errorContains(err.Error(), "field predicate failed") {
			logger.Infof("✅ Successfully reproduced the schema validation error!")
			logger.Infof("   This confirms the bug exists in the schema conversion logic")
		} else {
			logger.Infof("⚠️  Different error received - may need to adjust test schema")
		}
	} else {
		logger.Infof("⚠️  No error received - the schema might have been fixed or the test needs adjustment")
	}

	// Now test with a CORRECTED schema to show what should work
	logger.Infof("\n🧪 Testing with CORRECTED schema (should work)...")

	correctedTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_page_corrected",
			Description: "Create a new page with properties, icon, and cover (corrected schema)",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object", // CRITICAL: Must have type="object" when properties exist
				"properties": map[string]interface{}{
					"properties": map[string]interface{}{
						"type": "object", // CRITICAL: Must have type="object" when properties exist
						"properties": map[string]interface{}{
							"title": map[string]interface{}{
								"type": "array", // CRITICAL: Must have type="array" when items exist
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"text": map[string]interface{}{
											"type": "string",
										},
									},
								},
							},
						},
					},
					"icon": map[string]interface{}{
						"type": "object", // CRITICAL: Must have type="object" when properties exist
						"properties": map[string]interface{}{
							"type": map[string]interface{}{
								"type": "string",
							},
						},
						"required": []string{"type"},
					},
					"cover": map[string]interface{}{
						"type": "object", // CRITICAL: Must have type="object" when properties exist
						"properties": map[string]interface{}{
							"external": map[string]interface{}{
								"type": "object", // CRITICAL: Must have type="object" when properties exist
								"properties": map[string]interface{}{
									"url": map[string]interface{}{
										"type": "string",
									},
								},
								"required": []string{"url"},
							},
						},
						"required": []string{"external"},
					},
				},
				"required": []string{"properties"},
			}),
		},
	}

	messages2 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Create a page with title 'Test Page'"},
			},
		},
	}

	logger.Infof("📤 Sending request with corrected schema...")
	_, err = vertexLLM.GenerateContent(ctx, messages2, llmtypes.WithTools([]llmtypes.Tool{correctedTool}))
	if err != nil {
		logger.Infof("❌ Error with corrected schema: %v", err)
		logger.Infof("   This suggests the fix may not be complete")
	} else {
		logger.Infof("✅ Corrected schema works! No errors received")
	}
}

// Helper function to check if error message contains a substring
func errorContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			errorContainsSubstring(s, substr))))
}

func errorContainsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
