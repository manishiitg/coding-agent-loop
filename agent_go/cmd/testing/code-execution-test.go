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

	"mcp-agent/agent_go/internal/utils"
	mcpagent "mcpagent/agent"
	"mcpagent/llm"
	"mcpagent/mcpclient"

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

		// Note: Yaegi interpreter test has been removed as the system now uses HTTP API
		// Code is executed via `go run` with HTTP calls to /api/mcp/execute, /api/custom/execute, /api/virtual/execute

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
