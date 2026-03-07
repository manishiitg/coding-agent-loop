package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var structuredOutputTestCmd = &cobra.Command{
	Use:   "structured-output",
	Short: "Test structured output with JSON schema via Claude Code CLI",
	Long: `Tests the --json-schema flag support for Claude Code CLI adapter.

This test:
1. Initializes a Claude Code LLM adapter
2. Sends a prompt with WithJSONSchema to enforce structured output
3. Validates that the response is valid JSON matching the schema

Examples:
  go run . test structured-output
  go run . test structured-output --provider claude-code --verbose`,
	RunE: runStructuredOutputTest,
}

func init() {
	structuredOutputTestCmd.Flags().String("model", "", "Model to use (default: claude-code)")
	viper.BindPFlag("structured-output.model", structuredOutputTestCmd.Flags().Lookup("model"))
}

// RecipeOutput represents the expected structured output
type RecipeOutput struct {
	Recipes []Recipe `json:"recipes"`
}

type Recipe struct {
	RecipeName  string   `json:"recipeName"`
	Ingredients []string `json:"ingredients"`
}

func runStructuredOutputTest(cmd *cobra.Command, args []string) error {
	// Load .env
	if err := godotenv.Load("agent_go/.env"); err != nil {
		if err := godotenv.Load(".env"); err != nil {
			_ = godotenv.Load("../.env")
		}
	}

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	logger.Info("=== Structured Output (JSON Schema) Test ===")

	provider := viper.GetString("test.provider")
	if provider == "" {
		provider = "claude-code"
	}

	model := viper.GetString("structured-output.model")
	if model == "" {
		if provider == "claude-code" {
			model = "claude-code"
		} else {
			model = llm.GetDefaultModel(llm.Provider(provider))
		}
	}

	logger.Info(fmt.Sprintf("Provider: %s, Model: %s", provider, model))

	// Initialize LLM
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.Provider(provider),
		ModelID:     model,
		Temperature: 0.7,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Define the JSON schema for structured output
	recipeSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"recipes": map[string]interface{}{
				"type":        "array",
				"description": "List of cookie recipes",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"recipeName": map[string]interface{}{
							"type":        "string",
							"description": "The name of the cookie recipe",
						},
						"ingredients": map[string]interface{}{
							"type":        "array",
							"description": "List of ingredients with amounts",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"required":             []string{"recipeName", "ingredients"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"recipes"},
		"additionalProperties": false,
	}

	// Build messages
	messages := []llmtypes.MessageContent{
		llmtypes.TextPart(llmtypes.ChatMessageTypeHuman,
			"List 3 popular cookie recipes with their ingredients and amounts."),
	}

	logger.Info("Sending request with --json-schema flag...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	startTime := time.Now()
	resp, err := llmModel.GenerateContent(ctx, messages,
		llmtypes.WithJSONSchema(recipeSchema, "cookie_recipes", "A list of cookie recipes with ingredients", true),
	)
	duration := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("GenerateContent failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("no choices in response")
	}

	content := resp.Choices[0].Content
	logger.Info(fmt.Sprintf("Response received in %s (%d chars)", duration, len(content)))

	// Validate it's valid JSON
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		fmt.Printf("\nRaw response:\n%s\n", content)
		return fmt.Errorf("response is not valid JSON: %w", err)
	}
	logger.Info("JSON validation: PASSED")

	// Validate it matches the schema structure
	var output RecipeOutput
	if err := json.Unmarshal([]byte(content), &output); err != nil {
		fmt.Printf("\nRaw JSON:\n%s\n", content)
		return fmt.Errorf("JSON does not match expected schema: %w", err)
	}
	logger.Info("Schema validation: PASSED")

	// Validate content
	if len(output.Recipes) == 0 {
		return fmt.Errorf("no recipes in response")
	}

	fmt.Printf("\nStructured Output Results:\n")
	fmt.Printf("  Recipes returned: %d\n", len(output.Recipes))
	for i, recipe := range output.Recipes {
		fmt.Printf("  [%d] %s (%d ingredients)\n", i+1, recipe.RecipeName, len(recipe.Ingredients))
		if recipe.RecipeName == "" {
			return fmt.Errorf("recipe %d has empty name", i+1)
		}
		if len(recipe.Ingredients) == 0 {
			return fmt.Errorf("recipe %d (%s) has no ingredients", i+1, recipe.RecipeName)
		}
	}

	// Print token usage if available
	if resp.Usage != nil {
		fmt.Printf("\n  Token Usage: input=%d, output=%d, total=%d\n",
			resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	}

	fmt.Printf("\n  Duration: %s\n", duration)
	fmt.Println("\nStructured output test completed successfully!")

	return nil
}
