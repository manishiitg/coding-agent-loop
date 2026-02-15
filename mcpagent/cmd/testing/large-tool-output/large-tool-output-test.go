package largetooloutput

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

var largeToolOutputTestCmd = &cobra.Command{
	Use:   "large-tool-output",
	Short: "Test large tool output handling (file writing and virtual tools)",
	Long: `Test large tool output handling feature.

This test:
1. Creates a custom tool that generates large output (JSON or text)
2. Sets a lower threshold for testing (default: 1000 tokens)
3. Verifies that large tool outputs are written to files
4. Verifies that the agent receives file messages with previews
5. Tests virtual tools: read_large_output, search_large_output, query_large_output

Note: This test doesn't use traditional asserts. Logs are analyzed (manually or by LLM) to verify success.
See criteria.md in the large-tool-output folder for detailed log analysis criteria.

Examples:
  mcpagent-test test large-tool-output --log-file logs/large-tool-output-test.log
  mcpagent-test test large-tool-output --threshold 2000 --log-file logs/large-tool-output-test.log
  mcpagent-test test large-tool-output --output-type json --log-file logs/large-tool-output-test.log`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Large Tool Output Test ===")

		// Get test parameters
		threshold := viper.GetInt("threshold")
		if threshold == 0 {
			threshold = 1000 // Default threshold for testing
		}
		outputType := viper.GetString("output-type")
		if outputType == "" {
			outputType = "json" // Default to JSON
		}
		outputSize := viper.GetInt("output-size")
		if outputSize == 0 {
			// Generate output that will exceed threshold in tokens
			// Rough estimate: 1 token ≈ 4 characters for English text
			// To be safe, generate 5x threshold in characters to ensure we exceed token threshold
			outputSize = threshold * 5 // Generate output 5x the threshold in characters
		}

		logger.Info("Test configuration",
			loggerv2.Int("threshold", threshold),
			loggerv2.String("output_type", outputType),
			loggerv2.Int("output_size", outputSize))

		// Run the test
		if err := testLargeToolOutput(logger, threshold, outputType, outputSize); err != nil {
			return fmt.Errorf("large tool output test failed: %w", err)
		}

		logger.Info("✅ Large tool output test passed!")
		return nil
	},
}

func init() {
	largeToolOutputTestCmd.Flags().Int("threshold", 1000, "Threshold in tokens for large output detection")
	largeToolOutputTestCmd.Flags().String("output-type", "json", "Output type: 'json' or 'text'")
	largeToolOutputTestCmd.Flags().Int("output-size", 0, "Size of output to generate (default: 2x threshold)")

	_ = viper.BindPFlag("threshold", largeToolOutputTestCmd.Flags().Lookup("threshold"))     //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("output-type", largeToolOutputTestCmd.Flags().Lookup("output-type")) //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("output-size", largeToolOutputTestCmd.Flags().Lookup("output-size")) //nolint:gosec // BindPFlag errors are non-critical in test init
}

// GetLargeToolOutputTestCmd returns the large tool output test command
func GetLargeToolOutputTestCmd() *cobra.Command {
	return largeToolOutputTestCmd
}

// testLargeToolOutput tests the large tool output handling feature
func testLargeToolOutput(log loggerv2.Logger, threshold int, outputType string, outputSize int) error {
	log.Info("--- Test: Large Tool Output Handling ---")

	ctx := context.Background()
	traceID := testutils.GenerateTestTraceID()

	// Create minimal MCP config (no servers needed for custom tools)
	mcpServers := map[string]interface{}{}
	configPath, cleanup, err := testutils.CreateTempMCPConfig(mcpServers, log)
	if err != nil {
		return fmt.Errorf("failed to create temp MCP config: %w", err)
	}
	defer cleanup()

	// Get optional tracer
	tracer, _ := testutils.GetTracerWithLogger("langfuse", log)
	if tracer == nil {
		tracer, _ = testutils.GetTracerWithLogger("noop", log)
	}

	// Initialize LLM
	modelID := viper.GetString("model")
	if modelID == "" {
		modelID = openai.ModelGPT41Mini // Default
	}
	model, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: "",      // Empty to use viper/flags
		ModelID:  modelID, // Use model from flag if provided
		Logger:   log,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	log.Info("✅ LLM initialized", loggerv2.String("model_id", modelID), loggerv2.String("provider", string(llmProvider)))

	// Create agent
	ag, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, traceID, log)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	// Set lower threshold for testing
	handler := ag.GetToolOutputHandler()
	if handler != nil {
		handler.SetThreshold(threshold)
		log.Info("✅ Set large output threshold",
			loggerv2.Int("threshold", threshold))
	}

	// Register custom tool that generates large output
	err = registerLargeOutputTool(ag, log)
	if err != nil {
		return fmt.Errorf("failed to register large output tool: %w", err)
	}

	log.Info("✅ Registered custom tool: generate_large_output")

	// Test 1: Generate large JSON output
	log.Info("--- Test 1: Large JSON Output ---")
	question1 := fmt.Sprintf("Call generate_large_output with size %d and type json", outputSize)
	log.Info("Running agent with question to generate large JSON output...",
		loggerv2.String("question", question1))

	startTime := time.Now()
	response1, err := ag.Ask(ctx, question1)
	duration1 := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}

	log.Info("✅ Agent executed successfully",
		loggerv2.String("response_preview", truncateString(response1, 300)),
		loggerv2.Int("response_length", len(response1)),
		loggerv2.String("duration", duration1.String()))

	// Test 2: Generate large text output
	log.Info("--- Test 2: Large Text Output ---")
	question2 := fmt.Sprintf("Call generate_large_output with size %d and type text", outputSize)
	log.Info("Running agent with question to generate large text output...",
		loggerv2.String("question", question2))

	startTime = time.Now()
	response2, err := ag.Ask(ctx, question2)
	duration2 := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}

	log.Info("✅ Agent executed successfully",
		loggerv2.String("response_preview", truncateString(response2, 300)),
		loggerv2.Int("response_length", len(response2)),
		loggerv2.String("duration", duration2.String()))

	// Test 3: Test virtual tools with JSON file
	log.Info("--- Test 3: Virtual Tools (read_large_output) ---")
	question3 := "Use read_large_output to read the first 200 characters from the most recent large output file"
	log.Info("Running agent to test read_large_output virtual tool...",
		loggerv2.String("question", question3))

	startTime = time.Now()
	response3, err := ag.Ask(ctx, question3)
	duration3 := time.Since(startTime)

	if err != nil {
		log.Warn("Virtual tool test failed (this is okay if no file was created)",
			loggerv2.String("error", err.Error()))
	} else {
		log.Info("✅ Virtual tool test executed",
			loggerv2.String("response_preview", truncateString(response3, 300)),
			loggerv2.String("duration", duration3.String()))
	}

	// Test 4: Test search_large_output (if JSON file exists)
	if outputType == "json" {
		log.Info("--- Test 4: Virtual Tools (search_large_output) ---")
		question4 := "Use search_large_output to search for 'Item' in the most recent large output file"
		log.Info("Running agent to test search_large_output virtual tool...",
			loggerv2.String("question", question4))

		startTime = time.Now()
		response4, err := ag.Ask(ctx, question4)
		duration4 := time.Since(startTime)

		if err != nil {
			log.Warn("Search virtual tool test failed (this is okay if no file was created)",
				loggerv2.String("error", err.Error()))
		} else {
			log.Info("✅ Search virtual tool test executed",
				loggerv2.String("response_preview", truncateString(response4, 300)),
				loggerv2.String("duration", duration4.String()))
		}

		// Test 5: Test query_large_output (if JSON file exists)
		log.Info("--- Test 5: Virtual Tools (query_large_output) ---")
		question5 := "Use query_large_output to query '.items[0].name' from the most recent large output JSON file"
		log.Info("Running agent to test query_large_output virtual tool...",
			loggerv2.String("question", question5))

		startTime = time.Now()
		response5, err := ag.Ask(ctx, question5)
		duration5 := time.Since(startTime)

		if err != nil {
			log.Warn("Query virtual tool test failed (this is okay if no file was created)",
				loggerv2.String("error", err.Error()))
		} else {
			log.Info("✅ Query virtual tool test executed",
				loggerv2.String("response_preview", truncateString(response5, 300)),
				loggerv2.String("duration", duration5.String()))
		}
	}

	// Wait for events to be processed
	if flusher, ok := tracer.(interface{ Flush() }); ok {
		log.Info("Flushing tracer...")
		flusher.Flush()
		log.Info("✅ Tracer flushed")
	}

	logFile := viper.GetString("log-file")

	log.Info("✅ Large tool output test completed",
		loggerv2.String("trace_id", string(traceID)),
		loggerv2.String("duration_total", time.Since(startTime).String()))

	log.Info("")
	if logFile != "" {
		log.Info("📋 Log file saved", loggerv2.String("path", logFile))
		log.Info("   See criteria.md in large-tool-output folder for log analysis criteria")
	} else {
		log.Info("📋 See criteria.md in large-tool-output folder for log analysis criteria")
		log.Info("   Tip: Use --log-file to save logs for analysis")
	}
	log.Info("   These tests don't use traditional asserts - logs are analyzed by LLM to verify success")

	return nil
}

// registerLargeOutputTool registers a custom tool that generates large output
func registerLargeOutputTool(agent *mcpagent.Agent, log loggerv2.Logger) error {
	generateLargeOutput := func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Get size parameter
		var size int
		if s, ok := args["size"].(float64); ok {
			size = int(s)
		} else {
			return "", fmt.Errorf("size parameter is required and must be a number")
		}

		// Get output type
		outputType := "json"
		if t, ok := args["type"].(string); ok && t != "" {
			outputType = t
		}

		log.Info("Generating large output",
			loggerv2.Int("size", size),
			loggerv2.String("type", outputType))

		// Generate output based on type
		if outputType == "json" {
			return generateLargeJSON(size)
		}
		return generateLargeText(size)
	}

	// Register the tool
	return agent.RegisterCustomTool(
		"generate_large_output",
		"Generates large output for testing large tool output handling. Use this tool to test the large output file writing feature.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"size": map[string]interface{}{
					"type":        "integer",
					"description": "Size of output to generate in characters",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Output type: 'json' or 'text'",
					"enum":        []string{"json", "text"},
					"default":     "json",
				},
			},
			"required": []string{"size"},
		},
		generateLargeOutput,
		"custom", // Category
	)
}

// generateLargeJSON generates a large JSON output
func generateLargeJSON(size int) (string, error) {
	items := make([]map[string]interface{}, 0)
	itemSize := 150 // Each item is approximately 150 characters when serialized
	numItems := size / itemSize
	if numItems < 1 {
		numItems = 1
	}

	for i := 0; i < numItems; i++ {
		items = append(items, map[string]interface{}{
			"id":          i,
			"name":        fmt.Sprintf("Item %d", i),
			"description": fmt.Sprintf("This is a test item with ID %d for large output testing", i),
			"data":        strings.Repeat("x", 50),
			"value":       i * 10,
			"timestamp":   time.Now().Format(time.RFC3339),
			"metadata": map[string]interface{}{
				"category": "test",
				"index":    i,
			},
		})
	}

	jsonData, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	result := string(jsonData)

	// Pad if needed to reach exact size
	if len(result) < size {
		padding := strings.Repeat(" ", size-len(result))
		// Try to add padding in a way that keeps JSON valid
		// Add as whitespace in the last item
		if len(items) > 0 {
			items[len(items)-1]["padding"] = padding
			jsonData, err = json.MarshalIndent(items, "", "  ")
			if err == nil {
				result = string(jsonData)
			} else {
				// Fallback: just append spaces
				result += padding
			}
		} else {
			result += padding
		}
	}

	// Truncate if too large
	if len(result) > size {
		result = result[:size]
	}

	return result, nil
}

// generateLargeText generates a large text output
func generateLargeText(size int) (string, error) {
	line := "This is a test line for large output testing. "
	lineSize := len(line)
	numLines := size / lineSize
	if numLines < 1 {
		numLines = 1
	}

	var builder strings.Builder
	for i := 0; i < numLines; i++ {
		builder.WriteString(fmt.Sprintf("Line %d: %s\n", i+1, line))
	}

	result := builder.String()

	// Pad or truncate to exact size
	if len(result) < size {
		result += strings.Repeat(" ", size-len(result))
	} else if len(result) > size {
		result = result[:size]
	}

	return result, nil
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
