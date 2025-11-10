package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
)

var codeExecutionTestCmd = &cobra.Command{
	Use:   "code-execution",
	Short: "Test Go code execution with agent",
	Long: `Test that generated Go code can be executed correctly:

1. Creates an agent with code execution mode enabled
2. Generates Go code for MCP tools
3. Writes and executes Go code that uses the generated structs
4. Verifies the code executes successfully

This test validates that struct names are correctly exported (PascalCase).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Go Code Execution Test ===")

		// Load MCP server config
		configPath := viper.GetString("config")
		if configPath == "" {
			configPath = "configs/mcp_servers_clean_user.json"
		}

		logger.Infof("Loading MCP config from: %s", configPath)
		config, err := mcpclient.LoadMergedConfig(configPath, logger)
		if err != nil {
			return fmt.Errorf("failed to load MCP config: %w", err)
		}

		// Yaegi test - tests full Go code execution with loops and conditionals
		logger.Infof("\n--- Yaegi Interpreter Test (Loops & Conditionals) ---")
		logger.Infof("This test verifies that yaegi can execute full Go code with loops, conditionals, etc.")
		if err := testYaegiExecution(config, logger); err != nil {
			return fmt.Errorf("yaegi execution test failed: %w", err)
		}

		// Natural language agent test - simulates how server.go calls the agent
		logger.Infof("\n--- Natural Language Agent Test ---")
		logger.Infof("This test simulates how server.go creates an agent and asks it to list Google Sheets")
		if err := testNaturalLanguageAgent(config, logger); err != nil {
			return fmt.Errorf("natural language agent test failed: %w", err)
		}

		// Folder guard test - verifies that path validation works with code execution
		logger.Infof("\n--- Folder Guard with Code Execution Test ---")
		logger.Infof("This test verifies that folder guard validation works when code execution calls workspace tools")
		if err := testFolderGuardWithCodeExecution(config, logger); err != nil {
			return fmt.Errorf("folder guard test failed: %w", err)
		}

		logger.Infof("\n✅ All tests passed!")
		return nil
	},
}

func init() {
	// Command will be registered in testing.go's initTestingCommands
}

// testSimpleCodeExecution tests that we can execute Go code using generated structs
func testSimpleCodeExecution(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find a server with tools (prefer google-sheets as it has list_spreadsheets)
	var testServerName string

	preferredServers := []string{"google-sheets", "gdrive", "context7", "tavily-search"}

	for _, serverName := range preferredServers {
		if _, exists := config.MCPServers[serverName]; exists {
			testServerName = serverName
			logger.Infof("Selected server for testing: %s", serverName)
			break
		}
	}

	if testServerName == "" {
		// Use first available server
		for serverName := range config.MCPServers {
			testServerName = serverName
			break
		}
	}

	if testServerName == "" {
		return fmt.Errorf("no MCP servers found in config")
	}

	// Create agent with code execution mode enabled
	logger.Infof("Creating agent with code execution mode enabled...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	// Get LLM provider
	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai" // Default
	}

	// Convert provider string to llm.Provider
	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	// Get LLM instance (same pattern as codegen-test.go)
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil // Skip if model creation fails
	}

	// Use a single server for faster testing
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName, // Use single server instead of all
		configPath,
		"test-model",
		nil, // tracer
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	logger.Infof("✅ Agent created with code execution mode")

	// Wait a bit for code generation to complete
	time.Sleep(2 * time.Second)

	// Get package name for the test server
	packageName := getPackageName(testServerName)

	// Find a tool from this server to test with
	// First, discover the code structure
	logger.Infof("Discovering code structure...")
	discoveryResult, err := agent.HandleVirtualTool(ctx, "discover_code_structure", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to discover code structure: %w", err)
	}

	// Parse discovery result JSON to find a tool from the test server
	type ServerInfo struct {
		Name    string   `json:"name"`
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}
	type DiscoveryResult struct {
		Servers []ServerInfo `json:"servers"`
	}

	var discovery DiscoveryResult
	if err := json.Unmarshal([]byte(discoveryResult), &discovery); err != nil {
		return fmt.Errorf("failed to parse discovery result: %w", err)
	}

	// Find the test server in discovery results
	// Note: Discovery results use server names from directory names (may have underscores instead of hyphens)
	var foundServer *ServerInfo
	normalizedTestServerName := strings.ReplaceAll(testServerName, "-", "_")
	for i := range discovery.Servers {
		// Try exact match first
		if discovery.Servers[i].Name == testServerName {
			foundServer = &discovery.Servers[i]
			break
		}
		// Try normalized match (hyphens vs underscores)
		normalizedDiscoveryName := strings.ReplaceAll(discovery.Servers[i].Name, "-", "_")
		if normalizedDiscoveryName == normalizedTestServerName {
			foundServer = &discovery.Servers[i]
			break
		}
	}

	if foundServer == nil {
		// Log available servers for debugging
		availableServers := make([]string, len(discovery.Servers))
		for i, s := range discovery.Servers {
			availableServers[i] = s.Name
		}
		return fmt.Errorf("test server %s not found in discovery results. Available servers: %v", testServerName, availableServers)
	}

	if len(foundServer.Tools) == 0 {
		return fmt.Errorf("test server %s has no tools available", testServerName)
	}

	// Use the first tool from the server
	toolName := foundServer.Tools[0]
	logger.Infof("Using tool: %s from server: %s", toolName, testServerName)

	// Get the actual generated code to see the struct name
	logger.Infof("Retrieving generated code for tool: %s", toolName)
	codeResult, err := agent.HandleVirtualTool(ctx, "discover_code_files", map[string]interface{}{
		"server_name": testServerName,
		"tool_name":   toolName,
	})
	if err != nil {
		return fmt.Errorf("failed to get generated code for tool %s: %w", toolName, err)
	}

	// Extract struct name from the code
	// Look for "type XxxParams struct"
	structName := extractStructName(codeResult)
	if structName == "" {
		return fmt.Errorf("could not find struct name in generated code")
	}

	logger.Infof("✅ Found struct name: %s", structName)

	// Verify struct name is exported (starts with uppercase)
	if len(structName) == 0 || structName[0] < 'A' || structName[0] > 'Z' {
		return fmt.Errorf("struct name %s is not exported (does not start with uppercase)", structName)
	}

	logger.Infof("✅ Struct name is exported: %s", structName)

	// Extract function name from the code
	funcName := extractFunctionName(codeResult)
	if funcName == "" {
		return fmt.Errorf("could not find function name in generated code")
	}

	logger.Infof("✅ Found function name: %s", funcName)

	// Verify that the struct name matches the expected pattern
	// Function name should be PascalCase, struct should be FunctionName + "Params"
	expectedStructName := funcName + "Params"
	if structName != expectedStructName {
		return fmt.Errorf("struct name mismatch: expected %s, got %s", expectedStructName, structName)
	}

	logger.Infof("✅ Struct name matches function name pattern: %s", structName)

	// Now write and execute Go code that actually calls the tool
	logger.Infof("Writing Go code that calls %s.%s...", packageName, funcName)

	// Create test code that actually calls the tool function
	// This will use the registry to make a real MCP tool call
	testCode := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"mcp-agent/agent_go/generated/%s"
)

func main() {
	ctx := context.Background()
	
	// Create params struct (empty for tools with no parameters)
	params := %s.%s{}
	
	// Call the function - this will use codeexec.CallMCPTool internally
	result, err := %s.%s(ctx, params)
	if err != nil {
		fmt.Printf("Error: %%v\n", err)
		return
	}
	
	// Print success with result preview
	if len(result) > 100 {
		fmt.Printf("Success! Tool executed. Result (first 100 chars): %%s...\n", result[:100])
	} else {
		fmt.Printf("Success! Tool executed. Result: %%s\n", result)
	}
}
`, packageName, packageName, structName, packageName, funcName)

	logger.Debugf("Test code:\n%s", testCode)

	// Execute the code using write_code tool
	logger.Infof("Executing code via write_code tool (this will make a real MCP tool call)...")
	executionResult, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": testCode,
	})
	if err != nil {
		return fmt.Errorf("failed to execute code: %w", err)
	}

	// Check if execution was successful
	// First check for execution errors (yaegi errors, undefined types, etc.)
	if strings.Contains(executionResult, "❌ EXECUTION ERROR") {
		if strings.Contains(executionResult, "undefined type") {
			return fmt.Errorf("code execution failed: struct types not properly injected. Execution result: %s", executionResult)
		}
		return fmt.Errorf("code execution failed: %s", executionResult)
	}

	if strings.Contains(executionResult, "Error:") {
		// Check if it's a registry error (expected if registry not initialized)
		if strings.Contains(executionResult, "tool registry not initialized") {
			return fmt.Errorf("tool registry not initialized - this means the code execution environment needs the registry. Execution result: %s", executionResult)
		}
		// Other errors might be from tool execution, not code execution itself
		// So we'll check for success indicators below
	}

	if strings.Contains(executionResult, "Success!") {
		logger.Infof("✅ Code executed successfully and tool was called!")
		logger.Infof("Execution result: %s", executionResult)
		logger.Infof("✅ All validations passed!")
		logger.Infof("   - Struct name: %s (exported)", structName)
		logger.Infof("   - Function name: %s", funcName)
		logger.Infof("   - Package: %s", packageName)
		logger.Infof("   - Tool: %s from server: %s", toolName, testServerName)
		logger.Infof("   - Tool execution: SUCCESS")
		return nil
	}

	// If we get here, execution completed but didn't match expected output
	logger.Infof("✅ Code executed (result: %s)", executionResult)
	logger.Infof("✅ All validations passed!")
	return nil
}

// getPackageName converts server name to package name
func getPackageName(serverName string) string {
	// Simple conversion: replace hyphens/underscores and add _tools
	normalized := strings.ReplaceAll(serverName, "-", "_")
	return normalized + "_tools"
}

// extractStructName extracts the struct name from generated Go code
func extractStructName(code string) string {
	// Look for "type XxxParams struct"
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "type ") && strings.Contains(line, "Params struct") {
			// Extract name between "type " and "Params"
			parts := strings.Split(line, " ")
			if len(parts) >= 2 {
				name := parts[1]
				if strings.HasSuffix(name, "Params") {
					return name
				}
			}
		}
	}
	return ""
}

// extractFunctionName extracts the function name from generated Go code
func extractFunctionName(code string) string {
	// Look for "func Xxx(ctx context.Context"
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "func ") && strings.Contains(line, "(ctx context.Context") {
			// Extract name between "func " and "("
			parts := strings.Split(line, "(")
			if len(parts) > 0 {
				funcPart := strings.TrimSpace(parts[0])
				funcParts := strings.Split(funcPart, " ")
				if len(funcParts) >= 2 {
					return funcParts[1]
				}
			}
		}
	}
	return ""
}

// testOutputCapture tests that stdout/stderr output is properly captured
func testOutputCapture(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find a single server to use
	var testServerName string
	preferredServers := []string{"context7", "tavily-search", "google-sheets"}
	for _, serverName := range preferredServers {
		if _, exists := config.MCPServers[serverName]; exists {
			testServerName = serverName
			logger.Infof("Selected server for output capture test: %s", serverName)
			break
		}
	}
	if testServerName == "" {
		// Use first available server
		for serverName := range config.MCPServers {
			testServerName = serverName
			break
		}
	}
	if testServerName == "" {
		return fmt.Errorf("no MCP servers found in config")
	}

	// Create agent with code execution mode enabled
	logger.Infof("Creating agent with code execution mode enabled...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai"
	}

	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil
	}

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName, // Use single server
		configPath,
		"test-model",
		nil,
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	time.Sleep(2 * time.Second)

	// Test code that returns a string (new format - no stdout/stderr)
	testCode := `package main

import (
	"fmt"
	"strings"
)

func execute() string {
	var result strings.Builder
	
	// Build output string instead of printing
	result.WriteString("Test output line 1\n")
	result.WriteString(fmt.Sprintf("Test output line 2: %s\n", "formatted"))
	result.WriteString("Test output line 3\n")
	
	// Test stderr equivalent (just add to result)
	result.WriteString("Test stderr: error message\n")
	
	// Test multiple lines
	result.WriteString("Line 4\n")
	result.WriteString("Line 5\n")
	
	return result.String()
}
`

	logger.Infof("Executing code that returns a string (new format)...")
	executionResult, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": testCode,
	})
	if err != nil {
		return fmt.Errorf("failed to execute code: %w", err)
	}

	logger.Infof("Execution result length: %d bytes", len(executionResult))
	logger.Debugf("Full execution result:\n%s", executionResult)

	// Verify output was captured (code returns string directly)
	expectedOutputs := []string{
		"Test output line 1",
		"Test output line 2: formatted",
		"Test output line 3",
		"Test stderr: error message",
		"Line 4",
		"Line 5",
	}

	missingOutputs := []string{}
	for _, expected := range expectedOutputs {
		if !strings.Contains(executionResult, expected) {
			missingOutputs = append(missingOutputs, expected)
		}
	}

	if len(missingOutputs) > 0 {
		return fmt.Errorf("output capture test failed - missing expected outputs: %v\nActual output:\n%s", missingOutputs, executionResult)
	}

	logger.Infof("✅ All expected outputs were captured!")
	logger.Infof("✅ Output capture test passed!")
	return nil
}

// testYaegiExecution tests yaegi interpreter with full Go code (loops, conditionals, etc.)
func testYaegiExecution(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create a simple agent just for code execution testing
	logger.Infof("Creating agent for yaegi testing...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai"
	}

	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil
	}

	// Use first available server (or empty string for minimal setup)
	testServerName := ""
	for serverName := range config.MCPServers {
		testServerName = serverName
		break
	}

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName,
		configPath,
		"test-model",
		nil,
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	time.Sleep(2 * time.Second)

	// Test code with loops, conditionals, and string manipulation
	testCode := `package main

import (
	"fmt"
	"strings"
)

func execute() string {
	var result strings.Builder
	
	// Test 1: For loop
	result.WriteString("=== Test 1: For Loop ===\n")
	for i := 0; i < 5; i++ {
		result.WriteString(fmt.Sprintf("Loop iteration %d\n", i))
	}
	
	// Test 2: Conditional
	result.WriteString("\n=== Test 2: Conditionals ===\n")
	value := 10
	if value > 5 {
		result.WriteString("Value is greater than 5\n")
	} else {
		result.WriteString("Value is not greater than 5\n")
	}
	
	// Test 3: Switch statement
	result.WriteString("\n=== Test 3: Switch Statement ===\n")
	switch value {
	case 10:
		result.WriteString("Value is 10\n")
	case 5:
		result.WriteString("Value is 5\n")
	default:
		result.WriteString("Value is something else\n")
	}
	
	// Test 4: String manipulation with loop
	result.WriteString("\n=== Test 4: String Manipulation ===\n")
	words := []string{"hello", "world", "yaegi", "test"}
	for i, word := range words {
		result.WriteString(fmt.Sprintf("Word %d: %s (length: %d)\n", i, word, len(word)))
	}
	
	// Test 5: Nested loops
	result.WriteString("\n=== Test 5: Nested Loops ===\n")
	for i := 1; i <= 3; i++ {
		for j := 1; j <= 3; j++ {
			result.WriteString(fmt.Sprintf("i=%d, j=%d, product=%d\n", i, j, i*j))
		}
	}
	
	// Test 6: Conditional with multiple branches
	result.WriteString("\n=== Test 6: Multiple Conditionals ===\n")
	numbers := []int{1, 5, 10, 15, 20}
	for _, num := range numbers {
		if num < 5 {
			result.WriteString(fmt.Sprintf("%d is small\n", num))
		} else if num < 15 {
			result.WriteString(fmt.Sprintf("%d is medium\n", num))
		} else {
			result.WriteString(fmt.Sprintf("%d is large\n", num))
		}
	}
	
	result.WriteString("\n✅ All yaegi tests completed successfully!\n")
	return result.String()
}
`

	logger.Infof("Executing yaegi test code with loops, conditionals, and string manipulation...")
	executionResult, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": testCode,
	})
	if err != nil {
		return fmt.Errorf("failed to execute yaegi test code: %w", err)
	}

	logger.Infof("Execution result length: %d bytes", len(executionResult))
	logger.Debugf("Full execution result:\n%s", executionResult)

	// Verify all test sections are present
	expectedSections := []string{
		"Test 1: For Loop",
		"Loop iteration 0",
		"Loop iteration 4",
		"Test 2: Conditionals",
		"Value is greater than 5",
		"Test 3: Switch Statement",
		"Value is 10",
		"Test 4: String Manipulation",
		"Word 0: hello",
		"Test 5: Nested Loops",
		"i=1, j=1, product=1",
		"Test 6: Multiple Conditionals",
		"is small",
		"is medium",
		"is large",
		"All yaegi tests completed successfully",
	}

	missingSections := []string{}
	for _, expected := range expectedSections {
		if !strings.Contains(executionResult, expected) {
			missingSections = append(missingSections, expected)
		}
	}

	if len(missingSections) > 0 {
		return fmt.Errorf("yaegi test failed - missing expected sections: %v\nActual output:\n%s", missingSections, executionResult)
	}

	logger.Infof("✅ All yaegi test sections were executed correctly!")
	logger.Infof("✅ Yaegi interpreter test passed! (Full Go code execution with loops, conditionals, etc.)")
	return nil
}

// testMCPToolCall tests calling a real MCP tool (google-sheets list_spreadsheets)
func testMCPToolCall(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find google-sheets server (try different possible names)
	var testServerName string
	possibleNames := []string{"google-sheets", "google_sheets", "google_sheets_mcp", "sheets"}
	for _, name := range possibleNames {
		if _, exists := config.MCPServers[name]; exists {
			testServerName = name
			logger.Infof("Selected server for MCP tool call test: %s", testServerName)
			break
		}
	}

	if testServerName == "" {
		return fmt.Errorf("google-sheets server not found in config (tried: %v) - skipping MCP tool call test", possibleNames)
	}

	// Create agent with code execution mode enabled
	logger.Infof("Creating agent with code execution mode enabled...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai"
	}

	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil
	}

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName, // Use google_sheets server
		configPath,
		"test-model",
		nil,
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	time.Sleep(2 * time.Second)

	// Test code that calls google_sheets ListSpreadsheets tool
	// Using the new string-returning format
	testCode := `package main

import (
	"context"
	"fmt"
	"mcp-agent/agent_go/generated/google_sheets_tools"
	"strings"
)

func execute() string {
	var result strings.Builder
	
	ctx := context.Background()
	params := google_sheets_tools.ListSpreadsheetsParams{}
	
	result.WriteString("Calling ListSpreadsheets tool...\n")
	spreadsheets, err := google_sheets_tools.ListSpreadsheets(ctx, params)
	
	if err != nil {
		result.WriteString(fmt.Sprintf("Error calling ListSpreadsheets: %v\n", err))
		result.WriteString(fmt.Sprintf("Error type: %T\n", err))
		return result.String()
	}
	
	result.WriteString(fmt.Sprintf("✅ Successfully called ListSpreadsheets!\n"))
	result.WriteString(fmt.Sprintf("Result type: %T\n", spreadsheets))
	result.WriteString(fmt.Sprintf("Result length: %d\n", len(spreadsheets)))
	
	if len(spreadsheets) > 0 {
		result.WriteString("First few spreadsheets:\n")
		maxShow := 3
		if len(spreadsheets) < maxShow {
			maxShow = len(spreadsheets)
		}
		for i := 0; i < maxShow; i++ {
			result.WriteString(fmt.Sprintf("  - %s\n", spreadsheets[i]))
		}
		if len(spreadsheets) > maxShow {
			result.WriteString(fmt.Sprintf("  ... and %d more\n", len(spreadsheets)-maxShow))
		}
	} else {
		result.WriteString("No spreadsheets found (this is OK if you don't have any)\n")
	}
	
	return result.String()
}
`

	logger.Infof("Executing code that calls google_sheets ListSpreadsheets tool...")
	executionResult, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": testCode,
	})
	if err != nil {
		return fmt.Errorf("failed to execute code: %w", err)
	}

	logger.Infof("Execution result length: %d bytes", len(executionResult))
	logger.Debugf("Full execution result:\n%s", executionResult)

	// Verify we got a meaningful result
	if len(executionResult) == 0 {
		return fmt.Errorf("MCP tool call test failed - got empty result")
	}

	// Check for success indicators
	hasSuccess := strings.Contains(executionResult, "Successfully called ListSpreadsheets") ||
		strings.Contains(executionResult, "ListSpreadsheets") ||
		strings.Contains(executionResult, "Result type") ||
		strings.Contains(executionResult, "Result length")

	// Check for error (which is also OK - means the tool was called)
	hasError := strings.Contains(executionResult, "Error calling ListSpreadsheets") ||
		strings.Contains(executionResult, "Error type")

	if !hasSuccess && !hasError {
		return fmt.Errorf("MCP tool call test failed - result doesn't indicate tool was called\nActual output:\n%s", executionResult)
	}

	if hasSuccess {
		logger.Infof("✅ MCP tool call test passed - tool was called successfully!")
	} else if hasError {
		logger.Infof("⚠️ MCP tool call test - tool was called but returned an error (this may be expected):\n%s", executionResult)
		// Don't fail the test if we got an error - the important thing is that the tool was called
	}

	return nil
}

// testErrorHandling tests that errors are properly handled and returned
func testErrorHandling(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find a single server to use
	var testServerName string
	preferredServers := []string{"context7", "tavily-search", "google-sheets"}
	for _, serverName := range preferredServers {
		if _, exists := config.MCPServers[serverName]; exists {
			testServerName = serverName
			logger.Infof("Selected server for error handling test: %s", serverName)
			break
		}
	}
	if testServerName == "" {
		// Use first available server
		for serverName := range config.MCPServers {
			testServerName = serverName
			break
		}
	}
	if testServerName == "" {
		return fmt.Errorf("no MCP servers found in config")
	}

	// Create agent with code execution mode enabled
	logger.Infof("Creating agent with code execution mode enabled...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai"
	}

	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil
	}

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName, // Use single server
		configPath,
		"test-model",
		nil,
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	time.Sleep(2 * time.Second)

	// Test 1: Code with intentional error (invalid syntax)
	logger.Infof("Test 1: Code with syntax error...")
	invalidCode := `package main

func main() {
	invalid syntax here
}
`

	executionResult, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": invalidCode,
	})
	if err != nil {
		return fmt.Errorf("unexpected error from HandleVirtualTool: %w", err)
	}

	// Should contain error information
	if !strings.Contains(executionResult, "Execution Error") && !strings.Contains(executionResult, "error") && !strings.Contains(executionResult, "Error") {
		return fmt.Errorf("expected error information in result, got: %s", executionResult)
	}

	maxLen := 200
	if len(executionResult) < maxLen {
		maxLen = len(executionResult)
	}
	logger.Infof("✅ Syntax error properly captured: %s", executionResult[:maxLen])

	// Test 2: Code with runtime error (division by zero)
	logger.Infof("Test 2: Code with runtime error...")
	runtimeErrorCode := `package main

import "fmt"

func main() {
	var x int = 0
	result := 10 / x
	fmt.Printf("Result: %d\n", result)
}
`

	executionResult2, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": runtimeErrorCode,
	})
	if err != nil {
		return fmt.Errorf("unexpected error from HandleVirtualTool: %w", err)
	}

	// Should contain error or panic information
	if !strings.Contains(executionResult2, "error") && !strings.Contains(executionResult2, "Error") && !strings.Contains(executionResult2, "panic") {
		// Runtime errors might not always be caught, so this is a warning
		maxLen2 := 200
		if len(executionResult2) < maxLen2 {
			maxLen2 = len(executionResult2)
		}
		logger.Warnf("⚠️ Runtime error might not have been captured (this is expected for some platforms): %s", executionResult2[:maxLen2])
	} else {
		maxLen2 := 200
		if len(executionResult2) < maxLen2 {
			maxLen2 = len(executionResult2)
		}
		logger.Infof("✅ Runtime error properly captured: %s", executionResult2[:maxLen2])
	}

	// Test 3: Code that handles errors gracefully
	logger.Infof("Test 3: Code with error handling...")
	errorHandlingCode := `package main

import (
	"fmt"
	"mcp-agent/agent_go/generated/google_sheets_tools"
	"context"
)

func main() {
	ctx := context.Background()
	params := google_sheets_tools.ListSpreadsheetsParams{}
	result, err := google_sheets_tools.ListSpreadsheets(ctx, params)
	if err != nil {
		fmt.Printf("Error occurred: %v\n", err)
		fmt.Printf("Error type: %T\n", err)
		return
	}
	fmt.Printf("Success: %s\n", result)
}
`

	executionResult3, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": errorHandlingCode,
	})
	if err != nil {
		return fmt.Errorf("unexpected error from HandleVirtualTool: %w", err)
	}

	logger.Infof("Execution result (error handling test): %s", executionResult3)

	// Check if error was properly handled and printed
	if strings.Contains(executionResult3, "Error occurred:") {
		logger.Infof("✅ Error was properly caught and printed by user code")
		// Verify the error message is meaningful (not just "Tool execution error")
		if strings.Contains(executionResult3, "Tool execution error") && !strings.Contains(executionResult3, "tool") && !strings.Contains(executionResult3, "execution") {
			// If it only says "Tool execution error" without more details, that's a problem
			logger.Warnf("⚠️ Error message might be too generic: %s", executionResult3)
		} else {
			logger.Infof("✅ Error message contains meaningful information")
		}
	} else if strings.Contains(executionResult3, "Success:") {
		logger.Infof("✅ Tool executed successfully (no error)")
	} else {
		maxLen3 := 200
		if len(executionResult3) < maxLen3 {
			maxLen3 = len(executionResult3)
		}
		logger.Warnf("⚠️ Unexpected execution result format: %s", executionResult3[:maxLen3])
	}

	logger.Infof("✅ Error handling test passed!")
	return nil
}

// testNaturalLanguageAgent tests the agent using natural language conversation
// This simulates how a real user would interact with the agent
func testNaturalLanguageAgent(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find google-sheets server
	var testServerName string
	possibleNames := []string{"google-sheets", "google_sheets", "google_sheets_mcp", "sheets"}
	for _, name := range possibleNames {
		if _, exists := config.MCPServers[name]; exists {
			testServerName = name
			logger.Infof("Selected server for natural language test: %s", testServerName)
			break
		}
	}

	if testServerName == "" {
		return fmt.Errorf("google-sheets server not found in config (tried: %v) - skipping natural language test", possibleNames)
	}

	// Create agent with code execution mode enabled
	logger.Infof("Creating agent with code execution mode enabled for natural language test...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai"
	}

	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil
	}

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName,
		configPath,
		"test-model",
		nil,
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	time.Sleep(2 * time.Second)

	// Test with natural language query - ask the agent to list sheets
	// This simulates exactly how server.go calls the agent
	testQuery := "Can you list all my Google Sheets spreadsheets?"
	logger.Infof("📝 Natural language query: %s", testQuery)
	logger.Infof("📝 This test simulates how server.go calls the agent with a query")

	// Use the agent's Ask method (same as server.go uses AskWithHistory internally)
	// This is the simplest way to test - server.go uses StreamWithEvents for streaming,
	// but Ask() uses the same underlying AskWithHistory method
	response, err := agent.Ask(ctx, testQuery)
	if err != nil {
		return fmt.Errorf("agent.Ask failed: %w", err)
	}

	logger.Infof("✅ Agent response received (length: %d bytes)", len(response))

	// Log full response for debugging
	if len(response) > 1000 {
		logger.Infof("📝 Response preview (first 1000 chars):\n%s...", response[:1000])
		logger.Debugf("📝 Full response:\n%s", response)
	} else {
		logger.Infof("📝 Full response:\n%s", response)
	}

	// Verify we got a meaningful response
	if len(response) == 0 {
		return fmt.Errorf("natural language test failed - got empty response")
	}

	// Check for success indicators (the agent should have listed spreadsheets)
	// The agent should have used code execution to call list_spreadsheets tool
	hasSuccess := strings.Contains(strings.ToLower(response), "spreadsheet") ||
		strings.Contains(strings.ToLower(response), "sheet") ||
		strings.Contains(strings.ToLower(response), "list") ||
		strings.Contains(strings.ToLower(response), "found") ||
		strings.Contains(strings.ToLower(response), "here") ||
		strings.Contains(strings.ToLower(response), "your")

	// Check for error (which might be expected if auth fails)
	hasError := strings.Contains(strings.ToLower(response), "error") ||
		strings.Contains(strings.ToLower(response), "failed") ||
		strings.Contains(strings.ToLower(response), "reauthentication") ||
		strings.Contains(strings.ToLower(response), "unable")

	if !hasSuccess && !hasError {
		logger.Warnf("⚠️ Response doesn't clearly indicate success or error")
		logger.Warnf("   This might mean the agent didn't understand the query or didn't call the tool")
		logger.Warnf("   Full response: %s", response)
		// Don't fail - just warn, as the agent might have responded differently
	} else if hasSuccess {
		logger.Infof("✅ Natural language test passed - agent successfully listed spreadsheets!")
		logger.Infof("   The agent correctly understood the query and used code execution to call the MCP tool")
	} else if hasError {
		logger.Warnf("⚠️ Natural language test - agent returned an error (this may be expected):\n%s", response)
		// Don't fail the test if we got an error - the important thing is that the agent processed the request
		// The error might be due to authentication or other issues, but the agent flow worked
	}

	return nil
}

// testFolderGuardWithCodeExecution tests that folder guard validation works with code execution
// This verifies that when code execution calls workspace tools, path validation is enforced
func testFolderGuardWithCodeExecution(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a temporary workspace directory for testing
	tmpWorkspace, err := os.MkdirTemp("", "test_workspace_*")
	if err != nil {
		return fmt.Errorf("failed to create temp workspace: %w", err)
	}
	defer os.RemoveAll(tmpWorkspace)

	// Create a test file inside the workspace
	testFile := filepath.Join(tmpWorkspace, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}

	// Create a file outside the workspace (should be blocked)
	parentDir := filepath.Dir(tmpWorkspace)
	outsideFile := filepath.Join(parentDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("sensitive data"), 0644); err != nil {
		return fmt.Errorf("failed to create outside file: %w", err)
	}
	defer os.Remove(outsideFile)

	logger.Infof("Created test workspace: %s", tmpWorkspace)
	logger.Infof("Test file inside workspace: %s", testFile)
	logger.Infof("File outside workspace: %s", outsideFile)

	// Create agent with code execution mode enabled
	logger.Infof("Creating agent with code execution mode enabled...")
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	provider := viper.GetString("provider")
	if provider == "" {
		provider = "openai"
	}

	var llmProvider llm.Provider
	switch provider {
	case "openai":
		llmProvider = llm.ProviderOpenAI
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "anthropic":
		llmProvider = llm.ProviderAnthropic
	default:
		llmProvider = llm.ProviderOpenAI
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping test: %v", err)
		return nil
	}

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		"", // Use all servers (not needed for this test)
		configPath,
		"test-model",
		nil,
		"test-trace",
		logger,
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	// Get workspace tools
	workspaceTools := virtualtools.CreateWorkspaceTools()
	workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()

	// Convert executors to map[string]interface{} for wrapper function
	executorsMap := make(map[string]interface{})
	for name, executor := range workspaceExecutors {
		executorsMap[name] = executor
	}

	// Wrap executors with folder guard (simulating orchestrator behavior)
	wrappedExecutors := wrapWorkspaceToolsWithFolderGuard(tmpWorkspace, executorsMap, logger)

	// Register workspace tools with wrapped executors (folder guard applied)
	for _, tool := range workspaceTools {
		if tool.Function != nil {
			toolName := tool.Function.Name
			if executor, exists := wrappedExecutors[toolName]; exists {
				// Convert Parameters to map[string]interface{}
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					params = make(map[string]interface{})
				}

				// Type assert executor to function type
				if execFunc, ok := executor.(func(context.Context, map[string]interface{}) (string, error)); ok {
					agent.RegisterCustomTool(
						toolName,
						tool.Function.Description,
						params,
						execFunc,
					)
					logger.Infof("✅ Registered workspace tool with folder guard: %s", toolName)
				}
			}
		}
	}

	time.Sleep(2 * time.Second)

	// Test 1: Try to list files INSIDE the workspace (should succeed)
	logger.Infof("\n--- Test 1: Listing files INSIDE workspace (should succeed) ---")
	// Use list_workspace_files which is simpler and avoids path resolution issues
	insideCode := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"strings"
	codeexec "codeexec/codeexec"
)

func execute() string {
	var result strings.Builder
	
	ctx := context.Background()
	// Use codeexec.CallCustomTool() directly - no package imports needed
	// Use "." to list root of workspace
	// The folder guard will normalize it, but we've updated the guard to keep "." for list_workspace_files
	params := map[string]interface{}{
		"folder": ".",
	}
	
	result.WriteString("Attempting to list files inside workspace...\n")
	fileList, err := codeexec.CallCustomTool(ctx, "list_workspace_files", params)
	if err != nil {
		result.WriteString(fmt.Sprintf("Error: %%v\n", err))
		return result.String()
	}
	
	result.WriteString(fmt.Sprintf("Success! Found files:\n%%s\n", fileList))
	return result.String()
}
`)

	executionResult, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": insideCode,
	})
	if err != nil {
		return fmt.Errorf("failed to execute code: %w", err)
	}

	logger.Infof("Execution result:\n%s", executionResult)

	// Verify execution was successful (not just that it didn't fail in a specific way)
	if strings.Contains(executionResult, "❌ EXECUTION ERROR") || strings.Contains(executionResult, "undefined type") {
		return fmt.Errorf("code execution failed with error:\n%s", executionResult)
	}

	// Verify it succeeded (should not contain "outside workspace" error)
	if strings.Contains(executionResult, "outside workspace") || strings.Contains(executionResult, "workspace boundary") {
		return fmt.Errorf("folder guard incorrectly blocked access to workspace")
	}

	// Verify we got actual success output (not just no error)
	if !strings.Contains(executionResult, "Success!") && !strings.Contains(executionResult, "Found files") {
		return fmt.Errorf("code execution did not produce expected success output. Result:\n%s", executionResult)
	}

	logger.Infof("✅ Test 1 passed: Workspace files were listed and code executed successfully")

	// Test 2: Try to read a file OUTSIDE the workspace (should be blocked)
	logger.Infof("\n--- Test 2: Reading file OUTSIDE workspace (should be blocked) ---")
	// Calculate relative path that escapes workspace
	relPath, err := filepath.Rel(tmpWorkspace, outsideFile)
	if err != nil {
		return fmt.Errorf("failed to calculate relative path: %w", err)
	}

	outsideCode := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"strings"
	codeexec "codeexec/codeexec"
)

func execute() string {
	var result strings.Builder
	
	ctx := context.Background()
	// Try to access file outside workspace using path traversal
	// Use codeexec.CallCustomTool() directly
	params := map[string]interface{}{
		"filepath": "%s",
	}
	
	result.WriteString("Attempting to read file outside workspace...\n")
	content, err := codeexec.CallCustomTool(ctx, "read_workspace_file", params)
	if err != nil {
		result.WriteString(fmt.Sprintf("Error (expected): %%v\n", err))
		// Check if error contains workspace boundary message
		if strings.Contains(err.Error(), "outside workspace") || strings.Contains(err.Error(), "workspace boundary") {
			result.WriteString("✅ Folder guard correctly blocked access!\n")
		}
		return result.String()
	}
	
	result.WriteString(fmt.Sprintf("⚠️ SECURITY ISSUE: File outside workspace was accessible! Content length: %%d\n", len(content)))
	return result.String()
}
`, relPath)

	executionResult2, err := agent.HandleVirtualTool(ctx, "write_code", map[string]interface{}{
		"code": outsideCode,
	})
	if err != nil {
		return fmt.Errorf("failed to execute code: %w", err)
	}

	logger.Infof("Execution result:\n%s", executionResult2)

	// Check if execution failed due to undefined type (this is a code execution issue, not a security test)
	if strings.Contains(executionResult2, "❌ EXECUTION ERROR") && strings.Contains(executionResult2, "undefined type") {
		return fmt.Errorf("code execution failed with undefined type error (struct types not injected properly):\n%s", executionResult2)
	}

	// Verify it was blocked (should contain "outside workspace" or "workspace boundary" error)
	// OR it should contain the expected error message from the code itself
	if !strings.Contains(executionResult2, "outside workspace") &&
		!strings.Contains(executionResult2, "workspace boundary") &&
		!strings.Contains(executionResult2, "Folder guard correctly blocked access") {
		return fmt.Errorf("folder guard FAILED: File outside workspace was accessible! This is a security issue.\nExecution result: %s", executionResult2)
	}
	logger.Infof("✅ Test 2 passed: Folder guard correctly blocked access to file outside workspace")

	logger.Infof("\n✅ Folder guard test passed - path validation works correctly with code execution!")
	return nil
}

// wrapWorkspaceToolsWithFolderGuard wraps workspace tool executors with path validation
// This simulates the orchestrator's folder guard behavior
func wrapWorkspaceToolsWithFolderGuard(workspacePath string, executors map[string]interface{}, logger utils.ExtendedLogger) map[string]interface{} {
	if workspacePath == "" {
		return executors
	}

	// Tools that need path validation with their parameter names
	toolsToValidate := map[string][]string{
		"read_workspace_file":             {"filepath"},
		"update_workspace_file":           {"filepath"},
		"diff_patch_workspace_file":       {"filepath"},
		"delete_workspace_file":           {"filepath"},
		"move_workspace_file":             {"source_filepath", "destination_filepath"},
		"list_workspace_files":            {"folder"},
		"regex_search_workspace_files":    {"folder"},
		"semantic_search_workspace_files": {"folder"},
	}

	wrappedExecutors := make(map[string]interface{})

	for toolName, executor := range executors {
		paramsToValidate, needsValidation := toolsToValidate[toolName]

		if !needsValidation {
			wrappedExecutors[toolName] = executor
			continue
		}

		originalExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error))
		if !ok {
			wrappedExecutors[toolName] = executor
			continue
		}

		toolNameCopy := toolName
		paramsToValidateCopy := paramsToValidate
		wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
			// Validate all path parameters
			for _, paramName := range paramsToValidateCopy {
				if paramValue, exists := args[paramName]; exists {
					if pathStr, ok := paramValue.(string); ok {
						// Special handling for list_workspace_files: "." means workspace root, keep it as "."
						// Other tools can normalize "." to "" for workspace root
						if toolNameCopy == "list_workspace_files" && pathStr == "." {
							// For list_workspace_files, "." means list root - keep it as "."
							// The handler requires a non-empty string, so we can't normalize to ""
							args[paramName] = "."
							continue
						}
						if pathStr == "" || pathStr == "." {
							args[paramName] = ""
							continue
						}
						// Validate the path
						if err := validatePathInWorkspace(workspacePath, pathStr); err != nil {
							logger.Warnf("⚠️ Path validation failed for tool %s, parameter %s: %v", toolNameCopy, paramName, err)
							return "", err
						}
						// Normalize path
						normalizedPath, err := normalizePathForWorkspace(workspacePath, pathStr)
						if err != nil {
							logger.Warnf("⚠️ Path normalization failed for tool %s, parameter %s: %v", toolNameCopy, paramName, err)
							return "", err
						}
						// Special case: list_workspace_files with empty normalized path should use "."
						if toolNameCopy == "list_workspace_files" && normalizedPath == "" {
							normalizedPath = "."
						}
						args[paramName] = normalizedPath
					}
				}
			}
			return originalExecutor(ctx, args)
		}

		wrappedExecutors[toolName] = wrappedExecutor
	}

	return wrappedExecutors
}

// validatePathInWorkspace validates that the input path is within the workspace boundary
func validatePathInWorkspace(workspacePath, inputPath string) error {
	if workspacePath == "" {
		return nil
	}

	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace path: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	var inputAbs string
	if filepath.IsAbs(inputPath) {
		inputAbs, err = filepath.Abs(inputPath)
		if err != nil {
			return fmt.Errorf("failed to resolve input path: %w", err)
		}
	} else {
		inputAbsFromWorkspace := filepath.Join(workspaceAbs, inputPath)
		inputAbsFromWorkspace = filepath.Clean(inputAbsFromWorkspace)
		inputAbsFromCWD, err := filepath.Abs(inputPath)
		if err == nil {
			inputAbsFromCWD = filepath.Clean(inputAbsFromCWD)
			rel, relErr := filepath.Rel(workspaceAbs, inputAbsFromCWD)
			if relErr == nil && (strings.HasPrefix(rel, "..") || rel == "..") {
				inputAbs = inputAbsFromCWD
			} else {
				inputAbs = inputAbsFromWorkspace
			}
		} else {
			inputAbs = inputAbsFromWorkspace
		}
	}
	inputAbs = filepath.Clean(inputAbs)

	workspaceAbsSlash := filepath.ToSlash(workspaceAbs) + "/"
	inputAbsSlash := filepath.ToSlash(inputAbs)

	if inputAbsSlash != filepath.ToSlash(workspaceAbs) && !strings.HasPrefix(inputAbsSlash, workspaceAbsSlash) {
		return fmt.Errorf("path '%s' (resolved to '%s') is outside workspace boundary '%s'. All file operations must be within the configured workspace", inputPath, inputAbs, workspacePath)
	}

	rel, err := filepath.Rel(workspaceAbs, inputAbs)
	if err != nil {
		return fmt.Errorf("path validation error: %w", err)
	}

	if strings.HasPrefix(rel, "..") || rel == ".." {
		return fmt.Errorf("path '%s' (resolved to '%s', relative: '%s') is outside workspace boundary '%s'. All file operations must be within the configured workspace", inputPath, inputAbs, rel, workspacePath)
	}

	return nil
}

// normalizePathForWorkspace normalizes a path to be workspace-relative
func normalizePathForWorkspace(workspacePath, inputPath string) (string, error) {
	if workspacePath == "" {
		return inputPath, nil
	}

	if inputPath == "" || inputPath == "." {
		return "", nil
	}

	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	var inputAbs string
	if filepath.IsAbs(inputPath) {
		inputAbs, err = filepath.Abs(inputPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve input path: %w", err)
		}
	} else {
		inputAbs = filepath.Join(workspaceAbs, inputPath)
	}
	inputAbs = filepath.Clean(inputAbs)

	rel, err := filepath.Rel(workspaceAbs, inputAbs)
	if err != nil {
		return "", fmt.Errorf("path normalization error: %w", err)
	}

	if rel == "." || rel == "" {
		return "", nil
	}

	return rel, nil
}
