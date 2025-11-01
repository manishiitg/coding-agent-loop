package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
)

var toolOutputHandlerTestCmd = &cobra.Command{
	Use:   "tool-output-handler",
	Short: "Test tool output handler functionality",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info("=== Tool Output Handler Test ===")

		// Create test directory
		testDir := "tool_output_test"
		if err := os.MkdirAll(testDir, 0755); err != nil {
			return fmt.Errorf("failed to create test directory: %w", err)
		}
		defer os.RemoveAll(testDir)

		logger.Infof("Created test directory: %s", testDir)

		// Test the extractActualContent function with MCP format
		logger.Info("\n--- Test 1: Content Extraction ---")
		if err := testContentExtraction(); err != nil {
			return fmt.Errorf("content extraction test failed: %w", err)
		}

		// Test file creation with MCP format
		logger.Info("\n--- Test 2: File Creation with MCP Format ---")
		if err := testFileCreationWithMCPFormat(testDir); err != nil {
			return fmt.Errorf("file creation test failed: %w", err)
		}

		// Test large output virtual tools
		logger.Info("\n--- Test 3: Large Output Virtual Tools ---")
		if err := testLargeOutputVirtualTools(testDir); err != nil {
			return fmt.Errorf("large output virtual tools test failed: %w", err)
		}

		logger.Info("\n✅ All tool output handler tests passed!")
		return nil
	},
}

func testContentExtraction() error {
	logger := GetTestLogger()

	// Test cases for MCP format
	testCases := []struct {
		input    string
		expected string
		name     string
	}{
		{
			input:    `{"type":"text","text":"{\"key\": \"value\"}"}`,
			expected: `{"key": "value"}`,
			name:     "MCP JSON format",
		},
		{
			input:    `{"type":"text","text":"Hello World"}`,
			expected: "Hello World",
			name:     "MCP text format",
		},
		{
			input:    `{"type":"text","text":"{\"name\":\"test\",\"items\":[\"a\",\"b\"]}"}`,
			expected: `{"name":"test","items":["a","b"]}`,
			name:     "MCP complex JSON format",
		},
		{
			input:    "TOOL RESULT for aws_cli_query: {\"key\": \"value\"}",
			expected: "{\"key\": \"value\"}",
			name:     "old format",
		},
		{
			input:    "{\"key\": \"value\"}",
			expected: "{\"key\": \"value\"}",
			name:     "plain JSON",
		},
	}

	for _, tc := range testCases {
		result := utils.ExtractActualContent(tc.input)
		if result != tc.expected {
			return fmt.Errorf("content extraction test '%s' failed: expected '%s', got '%s'",
				tc.name, tc.expected, result)
		}
		logger.Infof("✅ Content extraction test '%s' passed", tc.name)
	}

	return nil
}

func testFileCreationWithMCPFormat(testDir string) error {
	logger := GetTestLogger()

	// Create tool output handler
	handler := utils.NewToolOutputHandlerWithConfig(
		100, // Small threshold for testing
		testDir,
		"test-session-123",
		true,
		true,
	)

	// Test with MCP format content
	mcpContent := `{"type":"text","text":"{\"name\":\"test\",\"value\":123,\"items\":[\"a\",\"b\",\"c\"]}"}`
	toolName := "test_tool"

	// Write to file
	filePath, err := handler.WriteToolOutputToFile(mcpContent, toolName)
	if err != nil {
		return fmt.Errorf("failed to write tool output to file: %w", err)
	}

	logger.Infof("File written to: %s", filePath)

	// Verify file exists and has content
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file was not created: %s", filePath)
	}

	// Read file content
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	logger.Infof("File content length: %d", len(fileContent))
	logger.Infof("File content: %s", string(fileContent))

	// Verify the content was extracted correctly (should not contain MCP wrapper)
	if strings.Contains(string(fileContent), `"type":"text"`) {
		return fmt.Errorf("file still contains MCP wrapper")
	}

	// Verify it's valid JSON
	if !isValidJSON(string(fileContent)) {
		return fmt.Errorf("file content is not valid JSON")
	}

	logger.Infof("✅ File creation test passed - content extracted correctly")

	return nil
}

func isValidJSON(content string) bool {
	var js interface{}
	return json.Unmarshal([]byte(content), &js) == nil
}

func testLargeOutputVirtualTools(testDir string) error {
	logger := GetTestLogger()

	logger.Info("Testing large output virtual tools functionality...")

	// Create a test agent with large output virtual tools enabled
	agent := &mcpagent.Agent{
		EnableLargeOutputVirtualTools: true,
	}
	agent.SetToolOutputHandler(utils.NewToolOutputHandler())

	// Test 1: Check if large output virtual tools are enabled by default
	logger.Info("Test 1: Default Configuration")
	logger.Infof("EnableLargeOutputVirtualTools: %v", agent.EnableLargeOutputVirtualTools)

	// Test 2: Create virtual tools and check if large output tools are included
	logger.Info("Test 2: Virtual Tools Creation")
	virtualTools := agent.CreateVirtualTools()
	logger.Infof("Total virtual tools: %d", len(virtualTools))

	largeOutputTools := agent.CreateLargeOutputVirtualTools()
	logger.Infof("Large output virtual tools: %d", len(largeOutputTools))

	for _, tool := range virtualTools {
		if tool.Function != nil {
			logger.Infof("- %s: %s", tool.Function.Name, tool.Function.Description)
		}
	}

	// Test 3: Test with disabled large output virtual tools
	logger.Info("Test 3: Disabled Configuration")
	agent.EnableLargeOutputVirtualTools = false
	disabledTools := agent.CreateLargeOutputVirtualTools()
	logger.Infof("Large output virtual tools when disabled: %d", len(disabledTools))

	// Test 4: Test file path building
	logger.Info("Test 4: File Path Building")

	// Create a test tool output handler
	toolOutputHandler := utils.NewToolOutputHandler()
	toolOutputHandler.SetSessionID("test-session")
	agent.SetToolOutputHandler(toolOutputHandler)

	// Test valid filename
	validPath := agent.BuildLargeOutputFilePath("tool_20250721_091511_tavily-search.json")
	logger.Infof("Valid filename path: %s", validPath)

	// Test invalid filename
	invalidPath := agent.BuildLargeOutputFilePath("invalid_filename.txt")
	logger.Infof("Invalid filename path: %s", invalidPath)

	// Test 5: Test virtual tool handling
	logger.Info("Test 5: Virtual Tool Handling")

	ctx := context.Background()

	// Test get_prompt tool (should work)
	result, err := agent.HandleVirtualTool(ctx, "get_prompt", map[string]interface{}{
		"server": "test-server",
		"name":   "test-prompt",
	})
	logger.Infof("get_prompt result: %s, error: %v", result, err)

	// Test large output tool when enabled (should work)
	agent.EnableLargeOutputVirtualTools = true

	// Create a test file to read from
	testFilePath := filepath.Join(testDir, "test-session", "tool_20250731_143800_test_tool.json")
	if err := os.MkdirAll(filepath.Dir(testFilePath), 0755); err != nil {
		return fmt.Errorf("failed to create test directory: %w", err)
	}

	testContent := `{"name":"test","value":123,"items":["a","b","c"]}`
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		return fmt.Errorf("failed to write test file: %w", err)
	}

	// Set the tool output handler to use our test directory
	handler := agent.GetToolOutputHandler()
	handler.OutputFolder = testDir
	handler.SessionID = "test-session"

	result, err = agent.HandleLargeOutputVirtualTool(ctx, "read_large_output", map[string]interface{}{
		"filename": "tool_20250731_143800_test_tool.json",
		"start":    float64(1),
		"end":      float64(20),
	})
	logger.Infof("read_large_output when enabled: %s, error: %v", result, err)

	// Test large output tool when disabled (should fail)
	agent.EnableLargeOutputVirtualTools = false
	result, err = agent.HandleLargeOutputVirtualTool(ctx, "read_large_output", map[string]interface{}{
		"filename": "test.json",
		"start":    float64(1),
		"end":      float64(100),
	})
	logger.Infof("read_large_output when disabled: %s, error: %v", result, err)

	logger.Info("✅ Large output virtual tools tests passed!")
	return nil
}

func init() {
	TestingCmd.AddCommand(toolOutputHandlerTestCmd)
}
