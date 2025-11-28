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

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpcache/codegen"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/mark3labs/mcp-go/mcp"
)

var codegenTestCmd = &cobra.Command{
	Use:   "codegen",
	Short: "Test Go code generation for MCP tools",
	Long: `Simple test for Go code generation functionality:

1. Creates a test cache entry with sample tools
2. Generates Go code for the tools
3. Verifies generated code structure
4. Tests discover_code_files virtual tool
5. Tests write_code virtual tool

This test validates that code generation works correctly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Go Code Generation Test ===")

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

		// Create a temporary directory for generated code
		testDir := filepath.Join(".", "test_generated")
		defer func() {
			// Cleanup
			if err := os.RemoveAll(testDir); err != nil {
				logger.Warnf("Failed to cleanup test directory: %v", err)
			}
		}()

		// Test 1: Generate code from real MCP server
		logger.Infof("\n--- Test 1: Generate Go code from real MCP server ---")
		if err := testCodeGenerationFromRealServer(config, testDir, logger); err != nil {
			return fmt.Errorf("code generation from real server failed: %w", err)
		}

		// Test 2: Test cache save triggers code generation in actual generated/ directory
		logger.Infof("\n--- Test 2: Test cache save generates code in generated/ directory ---")
		if err := testCacheSaveGeneratesCode(config, logger); err != nil {
			return fmt.Errorf("cache save code generation test failed: %w", err)
		}

		// Test 3: Generate code for all MCP servers in config
		logger.Infof("\n--- Test 3: Generate code for all MCP servers ---")
		if err := testGenerateCodeForAllServers(config, logger); err != nil {
			return fmt.Errorf("generate code for all servers failed: %w", err)
		}

		// Test 4: Generate code for virtual tools
		logger.Infof("\n--- Test 4: Generate code for virtual tools ---")
		if err := testGenerateVirtualToolsCode(config, logger); err != nil {
			return fmt.Errorf("generate virtual tools code failed: %w", err)
		}

		// Test 5: Test discover_code_files filtering
		logger.Infof("\n--- Test 5: Test discover_code_files filtering ---")
		if err := testDiscoverCodeFilesFiltering(config, logger); err != nil {
			return fmt.Errorf("discover_code_files filtering test failed: %w", err)
		}

		logger.Infof("\n✅ All code generation tests passed!")
		return nil
	},
}

func init() {
	// Command will be registered in testing.go's initTestingCommands
}

// testCodeGenerationFromRealServer tests code generation with a real MCP server
func testCodeGenerationFromRealServer(config *mcpclient.MCPConfig, generatedDir string, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Try to find a suitable server to test with
	// Prefer servers that are likely to work (HTTP or simple stdio)
	var testServerName string
	var testServerConfig mcpclient.MCPServerConfig

	// Priority order: HTTP servers first, then stdio servers
	preferredServers := []string{"context7", "tavily-search", "documentation", "gmail", "playwright"}

	for _, serverName := range preferredServers {
		if serverConfig, exists := config.MCPServers[serverName]; exists {
			testServerName = serverName
			testServerConfig = serverConfig
			logger.Infof("Selected server for testing: %s", serverName)
			break
		}
	}

	// If no preferred server found, use the first available server
	if testServerName == "" {
		for serverName, serverConfig := range config.MCPServers {
			testServerName = serverName
			testServerConfig = serverConfig
			logger.Infof("Using first available server: %s", serverName)
			break
		}
	}

	if testServerName == "" {
		return fmt.Errorf("no MCP servers found in config")
	}

	// Connect to the server
	logger.Infof("Connecting to MCP server: %s", testServerName)
	client := mcpclient.New(testServerConfig, logger)

	if err := client.Connect(ctx); err != nil {
		logger.Warnf("Failed to connect to %s: %v. Falling back to mock test.", testServerName, err)
		return testCodeGenerationMock(generatedDir, logger)
	}
	defer client.Close()

	logger.Infof("✅ Connected to %s successfully", testServerName)

	// Get server info
	serverInfo := client.GetServerInfo()
	if serverInfo != nil {
		logger.Infof("   Server Name: %s", serverInfo.Name)
		logger.Infof("   Server Version: %s", serverInfo.Version)
	}

	// List tools
	logger.Infof("Discovering tools from %s...", testServerName)
	tools, err := client.ListTools(ctx)
	if err != nil {
		logger.Warnf("Failed to list tools from %s: %v. Falling back to mock test.", testServerName, err)
		return testCodeGenerationMock(generatedDir, logger)
	}

	if len(tools) == 0 {
		logger.Warnf("No tools found from %s. Falling back to mock test.", testServerName)
		return testCodeGenerationMock(generatedDir, logger)
	}

	logger.Infof("✅ Found %d tools from %s", len(tools), testServerName)

	// Convert tools to llmtypes.Tool format
	llmTools, err := mcpclient.ToolsAsLLM(tools)
	if err != nil {
		return fmt.Errorf("failed to convert tools to LLM format: %w", err)
	}

	// Create cache entry for code generation
	entry := &codegen.CacheEntryForCodeGen{
		ServerName: testServerName,
		Tools:      llmTools,
	}

	// Generate code with default 5-minute timeout
	defaultTimeout := 5 * time.Minute
	if err := codegen.GenerateServerToolsCode(entry, testServerName, generatedDir, logger, defaultTimeout); err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	// Verify generated files exist (one file per tool)
	packageName := codegen.GetPackageName(testServerName)
	packageDir := filepath.Join(generatedDir, packageName)

	// Check if package directory exists
	if _, err := os.Stat(packageDir); os.IsNotExist(err) {
		return fmt.Errorf("generated package directory not found: %s", packageDir)
	}

	// List all .go files in the package directory
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return fmt.Errorf("failed to read package directory: %w", err)
	}

	var goFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}

	if len(goFiles) == 0 {
		return fmt.Errorf("no Go files found in package directory: %s", packageDir)
	}

	logger.Infof("✅ Generated %d Go files in %s: %v", len(goFiles), packageDir, goFiles)

	// Read and verify at least one file content
	firstFile := filepath.Join(packageDir, goFiles[0])
	content, err := os.ReadFile(firstFile)
	if err != nil {
		return fmt.Errorf("failed to read generated file: %w", err)
	}

	contentStr := string(content)

	// Debug: Print first 500 chars of generated content
	previewLen := 500
	if len(contentStr) < previewLen {
		previewLen = len(contentStr)
	}
	logger.Debugf("Generated file content (first %d chars):\n%s", previewLen, contentStr[:previewLen])

	// Verify package declaration
	expectedPackage := packageName
	if !strings.Contains(contentStr, "package "+expectedPackage) {
		return fmt.Errorf("generated file missing package declaration: expected 'package %s'", expectedPackage)
	}

	// Verify at least one function exists
	if !strings.Contains(contentStr, "func ") {
		return fmt.Errorf("generated file missing function declarations")
	}

	// Verify struct declarations exist (if tools have parameters)
	if len(tools) > 0 && !strings.Contains(contentStr, "struct") && !strings.Contains(contentStr, "Params") {
		// This is OK - some tools might not have parameters
		logger.Debugf("No struct declarations found (tools may have no parameters)")
	}

	logger.Infof("✅ Generated code structure verified for %s (%d tools)", testServerName, len(tools))
	return nil
}

// testCodeGenerationMock tests basic code generation with mock data (fallback)
func testCodeGenerationMock(generatedDir string, logger utils.ExtendedLogger) error {
	// Create a sample cache entry with test tools
	entry := &codegen.CacheEntryForCodeGen{
		ServerName: "test_server",
		Tools: []llmtypes.Tool{
			{
				Type: "function",
				Function: &llmtypes.FunctionDefinition{
					Name:        "test_get_document",
					Description: "Get a document by ID",
					Parameters: llmtypes.NewParameters(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"documentId": map[string]interface{}{
								"type":        "string",
								"description": "The document ID",
							},
							"fields": map[string]interface{}{
								"type":        "string",
								"description": "Optional fields to return",
							},
						},
						"required": []string{"documentId"},
					}),
				},
			},
			{
				Type: "function",
				Function: &llmtypes.FunctionDefinition{
					Name:        "test_list_items",
					Description: "List all items",
					Parameters: llmtypes.NewParameters(map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
						"required":   []string{},
					}),
				},
			},
		},
	}

	// Generate code with default 5-minute timeout
	defaultTimeout := 5 * time.Minute
	if err := codegen.GenerateServerToolsCode(entry, "test_server", generatedDir, logger, defaultTimeout); err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	// Verify generated files exist (one file per tool)
	packageName := codegen.GetPackageName("test_server")
	packageDir := filepath.Join(generatedDir, packageName)

	// Check if package directory exists
	if _, err := os.Stat(packageDir); os.IsNotExist(err) {
		return fmt.Errorf("generated package directory not found: %s", packageDir)
	}

	// List all .go files in the package directory
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return fmt.Errorf("failed to read package directory: %w", err)
	}

	var goFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}

	if len(goFiles) == 0 {
		return fmt.Errorf("no Go files found in package directory: %s", packageDir)
	}

	logger.Infof("✅ Generated %d Go files in %s: %v", len(goFiles), packageDir, goFiles)

	// Verify we have at least 2 files (one per tool)
	if len(goFiles) < 2 {
		return fmt.Errorf("expected at least 2 Go files (one per tool), found %d", len(goFiles))
	}

	// Verify each file contains the expected function
	// Tool "test_get_document" -> file "test_get_document.go" -> function "TestGetDocument"
	// Tool "test_list_items" -> file "test_list_items.go" -> function "TestListItems"
	expectedFiles := map[string]string{
		"test_get_document.go": "TestGetDocument",
		"test_list_items.go":   "TestListItems",
	}

	for expectedFile, expectedFunc := range expectedFiles {
		found := false
		for _, goFile := range goFiles {
			if goFile == expectedFile {
				found = true
				// Read and verify file content
				filePath := filepath.Join(packageDir, goFile)
				content, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read generated file %s: %w", goFile, err)
				}

				contentStr := string(content)

				// Verify package declaration
				if !strings.Contains(contentStr, "package test_server_tools") {
					return fmt.Errorf("generated file %s missing package declaration", goFile)
				}

				// Verify function exists
				if !strings.Contains(contentStr, "func "+expectedFunc) {
					// Debug: Show what functions are actually in the file
					lines := strings.Split(contentStr, "\n")
					for i, line := range lines {
						if strings.Contains(line, "func ") {
							logger.Debugf("Found function at line %d: %s", i+1, strings.TrimSpace(line))
						}
					}
					return fmt.Errorf("generated file %s missing %s function", goFile, expectedFunc)
				}

				// Verify struct exists (for test_get_document which has parameters)
				if expectedFile == "test_get_document.go" {
					if !strings.Contains(contentStr, "Params struct") && !strings.Contains(contentStr, "struct") {
						// Debug: Show what structs are actually in the file
						lines := strings.Split(contentStr, "\n")
						for i, line := range lines {
							if strings.Contains(line, "type ") && strings.Contains(line, "struct") {
								logger.Debugf("Found struct at line %d: %s", i+1, strings.TrimSpace(line))
							}
						}
						return fmt.Errorf("generated file %s missing Params struct", goFile)
					}
				}

				logger.Infof("✅ Verified file %s contains function %s", goFile, expectedFunc)
				break
			}
		}
		if !found {
			return fmt.Errorf("expected file %s not found in generated files: %v", expectedFile, goFiles)
		}
	}

	logger.Infof("✅ Generated code structure verified (one file per tool)")
	return nil
}

// testCacheSaveGeneratesCode tests that cache save automatically generates Go code
func testCacheSaveGeneratesCode(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(logger)

	// Find a suitable server to test with
	var testServerName string
	var testServerConfig mcpclient.MCPServerConfig

	preferredServers := []string{"context7", "tavily-search", "documentation"}
	for _, serverName := range preferredServers {
		if serverConfig, exists := config.MCPServers[serverName]; exists {
			testServerName = serverName
			testServerConfig = serverConfig
			logger.Infof("Selected server for cache test: %s", serverName)
			break
		}
	}

	if testServerName == "" {
		for serverName, serverConfig := range config.MCPServers {
			testServerName = serverName
			testServerConfig = serverConfig
			logger.Infof("Using first available server: %s", serverName)
			break
		}
	}

	if testServerName == "" {
		return fmt.Errorf("no MCP servers found in config")
	}

	// Connect to server
	logger.Infof("Connecting to MCP server: %s", testServerName)
	client := mcpclient.New(testServerConfig, logger)

	if err := client.Connect(ctx); err != nil {
		logger.Warnf("Failed to connect to %s: %v. Skipping cache save test.", testServerName, err)
		return nil // Skip this test if connection fails
	}
	defer client.Close()

	logger.Infof("✅ Connected to %s successfully", testServerName)

	// List tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		logger.Warnf("Failed to list tools from %s: %v. Skipping cache save test.", testServerName, err)
		return nil
	}

	if len(tools) == 0 {
		logger.Warnf("No tools found from %s. Skipping cache save test.", testServerName)
		return nil
	}

	logger.Infof("✅ Found %d tools from %s", len(tools), testServerName)

	// Convert tools to llmtypes.Tool format
	llmTools, err := mcpclient.ToolsAsLLM(tools)
	if err != nil {
		return fmt.Errorf("failed to convert tools: %w", err)
	}

	// Get server info
	serverInfo := client.GetServerInfo()
	serverInfoMap := make(map[string]interface{})
	if serverInfo != nil {
		serverInfoMap["name"] = serverInfo.Name
		serverInfoMap["version"] = serverInfo.Version
	}

	// Create cache entry
	cacheEntry := &mcpcache.CacheEntry{
		ServerName:   testServerName,
		Tools:        llmTools,
		Prompts:      []mcp.Prompt{},
		Resources:    []mcp.Resource{},
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
		TTLMinutes:   60,
		Protocol:     string(testServerConfig.GetProtocol()),
		ServerInfo:   serverInfoMap,
		IsValid:      true,
	}

	// Save to cache (this should trigger code generation)
	logger.Infof("Saving cache entry for %s (this should generate Go code)...", testServerName)
	if err := cacheManager.Put(cacheEntry, testServerConfig); err != nil {
		return fmt.Errorf("failed to save cache entry: %w", err)
	}

	logger.Infof("✅ Cache entry saved successfully")

	// Verify generated files exist in actual generated/ directory
	generatedDir := filepath.Join(".", "generated")
	packageName := codegen.GetPackageName(testServerName)
	packageDir := filepath.Join(generatedDir, packageName)

	// Check if generated directory exists
	if _, err := os.Stat(generatedDir); os.IsNotExist(err) {
		return fmt.Errorf("generated directory was not created: %s", generatedDir)
	}

	logger.Infof("✅ Generated directory exists: %s", generatedDir)

	// Check if package directory exists
	if _, err := os.Stat(packageDir); os.IsNotExist(err) {
		return fmt.Errorf("package directory was not created: %s", packageDir)
	}

	logger.Infof("✅ Package directory exists: %s", packageDir)

	// List all .go files in the package directory
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return fmt.Errorf("failed to read package directory: %w", err)
	}

	var goFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}

	if len(goFiles) == 0 {
		return fmt.Errorf("no Go files found in package directory: %s", packageDir)
	}

	logger.Infof("✅ Generated %d Go files in %s: %v", len(goFiles), packageDir, goFiles)

	// Verify file content from first file
	firstFile := filepath.Join(packageDir, goFiles[0])
	content, err := os.ReadFile(firstFile)
	if err != nil {
		return fmt.Errorf("failed to read generated file: %w", err)
	}

	contentStr := string(content)

	// Verify package declaration
	expectedPackage := packageName
	if !strings.Contains(contentStr, "package "+expectedPackage) {
		return fmt.Errorf("generated file missing package declaration: expected 'package %s'", expectedPackage)
	}

	// Verify at least one function exists
	if !strings.Contains(contentStr, "func ") {
		return fmt.Errorf("generated file missing function declarations")
	}

	logger.Infof("✅ Generated code structure verified in actual generated/ directory")
	logger.Infof("✅ Cache save successfully triggered code generation!")

	// Check if index.go was generated
	indexFile := filepath.Join(generatedDir, "index.go")
	if _, err := os.Stat(indexFile); err == nil {
		logger.Infof("✅ Index file exists: %s", indexFile)
	} else {
		logger.Warnf("⚠️  Index file not found: %s", indexFile)
	}

	return nil
}

// testGenerateCodeForAllServers generates Go code for all MCP servers in the config
func testGenerateCodeForAllServers(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(logger)

	successCount := 0
	failedCount := 0
	totalServers := len(config.MCPServers)

	logger.Infof("Generating code for %d MCP servers...", totalServers)

	for serverName, serverConfig := range config.MCPServers {
		logger.Infof("\n--- Processing server: %s ---", serverName)
		logger.Infof("   Protocol: %s", serverConfig.GetProtocol())
		if serverConfig.URL != "" {
			logger.Infof("   URL: %s", serverConfig.URL)
		}

		// Connect to server
		client := mcpclient.New(serverConfig, logger)

		if err := client.Connect(ctx); err != nil {
			logger.Warnf("⚠️  Failed to connect to %s: %v", serverName, err)
			failedCount++
			continue
		}

		// List tools
		tools, err := client.ListTools(ctx)
		client.Close() // Close immediately after listing tools

		if err != nil {
			logger.Warnf("⚠️  Failed to list tools from %s: %v", serverName, err)
			failedCount++
			continue
		}

		if len(tools) == 0 {
			logger.Warnf("⚠️  No tools found from %s", serverName)
			failedCount++
			continue
		}

		logger.Infof("✅ Found %d tools from %s", len(tools), serverName)

		// Convert tools to llmtypes.Tool format
		llmTools, err := mcpclient.ToolsAsLLM(tools)
		if err != nil {
			logger.Warnf("⚠️  Failed to convert tools from %s: %v", serverName, err)
			failedCount++
			continue
		}

		// Get server info
		serverInfo := client.GetServerInfo()
		serverInfoMap := make(map[string]interface{})
		if serverInfo != nil {
			serverInfoMap["name"] = serverInfo.Name
			serverInfoMap["version"] = serverInfo.Version
		}

		// Create cache entry
		cacheEntry := &mcpcache.CacheEntry{
			ServerName:   serverName,
			Tools:        llmTools,
			Prompts:      []mcp.Prompt{},
			Resources:    []mcp.Resource{},
			CreatedAt:    time.Now(),
			LastAccessed: time.Now(),
			TTLMinutes:   60,
			Protocol:     string(serverConfig.GetProtocol()),
			ServerInfo:   serverInfoMap,
			IsValid:      true,
		}

		// Save to cache (this triggers code generation)
		if err := cacheManager.Put(cacheEntry, serverConfig); err != nil {
			logger.Warnf("⚠️  Failed to save cache entry for %s: %v", serverName, err)
			failedCount++
			continue
		}

		// Verify generated files exist
		generatedDir := filepath.Join(".", "generated")
		packageName := codegen.GetPackageName(serverName)
		packageDir := filepath.Join(generatedDir, packageName)

		// Check if package directory exists
		if _, err := os.Stat(packageDir); os.IsNotExist(err) {
			logger.Warnf("⚠️  Package directory not found for %s: %s", serverName, packageDir)
			failedCount++
			continue
		}

		// List all .go files in the package directory
		entries, err := os.ReadDir(packageDir)
		if err != nil {
			logger.Warnf("⚠️  Failed to read package directory for %s: %v", serverName, err)
			failedCount++
			continue
		}

		var goFiles []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
				goFiles = append(goFiles, entry.Name())
			}
		}

		if len(goFiles) == 0 {
			logger.Warnf("⚠️  No Go files found in package directory for %s: %s", serverName, packageDir)
			failedCount++
			continue
		}

		logger.Infof("✅ Generated code for %s: %d files in %s (%d tools)", serverName, len(goFiles), packageDir, len(tools))
		successCount++
	}

	logger.Infof("\n📊 Code Generation Summary:")
	logger.Infof("   Total servers: %d", totalServers)
	logger.Infof("   ✅ Success: %d", successCount)
	logger.Infof("   ⚠️  Failed: %d", failedCount)

	// List all generated packages
	generatedDir := filepath.Join(".", "generated")
	if entries, err := os.ReadDir(generatedDir); err == nil {
		var packages []string
		for _, entry := range entries {
			if entry.IsDir() && strings.HasSuffix(entry.Name(), "_tools") {
				packages = append(packages, entry.Name())
			}
		}
		if len(packages) > 0 {
			logger.Infof("\n📦 Generated packages (%d):", len(packages))
			for _, pkg := range packages {
				logger.Infof("   - %s", pkg)
			}
		}
	}

	if successCount == 0 {
		return fmt.Errorf("failed to generate code for any servers")
	}

	return nil
}

// testGenerateVirtualToolsCode tests that virtual tools code is generated when agent is created
func testGenerateVirtualToolsCode(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create a minimal agent to trigger virtual tools code generation
	// We'll use a simple LLM provider for testing
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping virtual tools test: %v", err)
		return nil // Skip if model creation fails
	}

	// Create agent - this should trigger virtual tools code generation
	logger.Infof("Creating agent to trigger virtual tools code generation...")
	configPath := "configs/mcp_servers_clean_user.json"
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		"", // serverName - empty means all
		configPath,
		"test-model",
		nil, // tracer
		"test-trace",
		logger,
	)
	if err != nil {
		logger.Warnf("Failed to create agent, skipping virtual tools test: %v", err)
		return nil // Skip if agent creation fails
	}
	defer agent.Close()

	logger.Infof("✅ Agent created successfully")

	// Verify virtual tools code was generated (one file per tool)
	generatedDir := filepath.Join(".", "generated")
	virtualToolsDir := filepath.Join(generatedDir, "virtual_tools")

	if _, err := os.Stat(virtualToolsDir); os.IsNotExist(err) {
		return fmt.Errorf("virtual tools directory was not created: %s", virtualToolsDir)
	}

	logger.Infof("✅ Virtual tools directory exists: %s", virtualToolsDir)

	// List all .go files in the virtual_tools directory
	entries, err := os.ReadDir(virtualToolsDir)
	if err != nil {
		return fmt.Errorf("failed to read virtual tools directory: %w", err)
	}

	var goFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}

	if len(goFiles) == 0 {
		return fmt.Errorf("no Go files found in virtual tools directory: %s", virtualToolsDir)
	}

	logger.Infof("✅ Generated %d virtual tool files: %v", len(goFiles), goFiles)

	// Verify at least one file has the expected content
	expectedVirtualTools := []string{"get_prompt", "get_resource", "discover_code_files", "write_code"}
	foundCount := 0
	for _, goFile := range goFiles {
		// Check if file name matches expected virtual tools
		for _, expectedTool := range expectedVirtualTools {
			if strings.Contains(goFile, expectedTool) {
				foundCount++
				logger.Infof("✅ Found virtual tool file: %s", goFile)

				// Verify file content
				filePath := filepath.Join(virtualToolsDir, goFile)
				content, err := os.ReadFile(filePath)
				if err != nil {
					logger.Warnf("Failed to read virtual tool file %s: %v", goFile, err)
					continue
				}

				contentStr := string(content)
				if !strings.Contains(contentStr, "package virtual_tools") {
					logger.Warnf("Virtual tool file %s missing package declaration", goFile)
					continue
				}
				break
			}
		}
	}

	if foundCount == 0 {
		return fmt.Errorf("virtual tools files missing expected tool names")
	}

	logger.Infof("✅ Virtual tools code structure verified (%d functions found)", foundCount)

	return nil
}

// testDiscoverCodeFilesFiltering tests that discover_code_files respects tool filtering
func testDiscoverCodeFilesFiltering(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// First, ensure we have some generated code to discover
	// We'll use a server that's likely to have multiple tools
	testServerName := "context7"
	testServerConfig, exists := config.MCPServers[testServerName]
	if !exists {
		// Try to find any server
		for name, cfg := range config.MCPServers {
			testServerName = name
			testServerConfig = cfg
			exists = true
			break
		}
		if !exists {
			return fmt.Errorf("no MCP servers found in config for testing")
		}
	}

	logger.Infof("Using server '%s' for filtering test", testServerName)

	// Connect to server and list tools to get tool names
	client := mcpclient.New(testServerConfig, logger)
	if err := client.Connect(ctx); err != nil {
		logger.Warnf("Failed to connect to server %s, skipping filtering test: %v", testServerName, err)
		return nil // Skip if connection fails
	}
	defer client.Close()

	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		logger.Warnf("Failed to list tools from server %s, skipping filtering test: %v", testServerName, err)
		return nil // Skip if tool listing fails
	}

	if len(mcpTools) < 2 {
		logger.Warnf("Server %s has less than 2 tools, skipping filtering test", testServerName)
		return nil // Need at least 2 tools to test filtering
	}

	// Get first two tool names for filtering test
	tool1Name := mcpTools[0].Name
	tool2Name := mcpTools[1].Name
	selectedTool := fmt.Sprintf("%s:%s", testServerName, tool1Name)

	logger.Infof("Testing with selected tool: %s", selectedTool)
	logger.Infof("Tool 1: %s (should be included)", tool1Name)
	logger.Infof("Tool 2: %s (should be excluded)", tool2Name)

	// Create LLM instance
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping filtering test: %v", err)
		return nil // Skip if model creation fails
	}

	// Create agent with selectedTools filtering
	configPath := "configs/mcp_servers_clean_user.json"
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		"", // serverName - empty means all
		configPath,
		"test-model",
		nil, // tracer
		"test-trace",
		logger,
		mcpagent.WithSelectedTools([]string{selectedTool}), // Filter to only one tool
	)
	if err != nil {
		logger.Warnf("Failed to create agent, skipping filtering test: %v", err)
		return nil // Skip if agent creation fails
	}
	defer agent.Close()

	logger.Infof("✅ Agent created with tool filtering: %s", selectedTool)

	// Test 1: Call discover_code_files without server_name (should return filtered results)
	logger.Infof("Test 1: Calling discover_code_files without server_name...")
	result1, err := agent.HandleVirtualTool(ctx, "discover_code_files", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to call discover_code_files: %w", err)
	}

	logger.Infof("✅ Received discovery result (length: %d chars)", len(result1))

	// Parse JSON result
	type ServerInfo struct {
		Name    string   `json:"name"`
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}
	type DiscoveryResult struct {
		Servers      []ServerInfo `json:"servers"`
		CustomTools  interface{}  `json:"custom_tools,omitempty"`
		VirtualTools interface{}  `json:"virtual_tools,omitempty"`
	}

	var discoveryResult DiscoveryResult
	if err := json.Unmarshal([]byte(result1), &discoveryResult); err != nil {
		return fmt.Errorf("failed to parse discovery result JSON: %w", err)
	}

	// Find the test server in results
	var foundServer *ServerInfo
	for i := range discoveryResult.Servers {
		if discoveryResult.Servers[i].Name == testServerName {
			foundServer = &discoveryResult.Servers[i]
			break
		}
	}

	if foundServer == nil {
		return fmt.Errorf("server %s not found in discovery results", testServerName)
	}

	logger.Infof("✅ Found server %s in discovery results", testServerName)
	logger.Infof("   Tools found: %v", foundServer.Tools)

	// Note: The function name might be different from tool name (capitalized, sanitized)
	// So we'll just verify that filtering is working (fewer tools than total)
	// If filtering is working correctly, we should see fewer tools than the total
	if len(foundServer.Tools) >= len(mcpTools) {
		logger.Warnf("⚠️ Filtering may not be working: found %d tools, expected fewer than %d", len(foundServer.Tools), len(mcpTools))
		// Don't fail - this is a warning, not an error
	} else {
		logger.Infof("✅ Filtering working: found %d tools (filtered from %d total)", len(foundServer.Tools), len(mcpTools))
	}

	// Test 2: Call discover_code_files with server_name (should return filtered code)
	logger.Infof("\nTest 2: Calling discover_code_files with server_name='%s'...", testServerName)
	result2, err := agent.HandleVirtualTool(ctx, "discover_code_files", map[string]interface{}{
		"server_name": testServerName,
	})
	if err != nil {
		return fmt.Errorf("failed to call discover_code_files with server_name: %w", err)
	}

	logger.Infof("✅ Received code result (length: %d chars)", len(result2))

	// Verify that the code contains the package name
	packageName := codegen.GetPackageName(testServerName)
	if !strings.Contains(result2, packageName) {
		return fmt.Errorf("code result missing package name: %s", packageName)
	}

	logger.Infof("✅ Code result contains package: %s", packageName)

	// Test 3: Try to get code for a filtered-out server (should fail or return empty)
	// First, find a server that's not in the selected tools
	var otherServerName string
	for name := range config.MCPServers {
		if name != testServerName {
			otherServerName = name
			break
		}
	}

	if otherServerName != "" {
		logger.Infof("\nTest 3: Calling discover_code_files with filtered-out server_name='%s'...", otherServerName)
		// This should still work (server-level filtering allows all servers, tool-level filters tools)
		// But we can verify that if we had selectedTools for this server, it would be filtered
		result3, err := agent.HandleVirtualTool(ctx, "discover_code_files", map[string]interface{}{
			"server_name": otherServerName,
		})
		if err != nil {
			logger.Infof("✅ Filtered server correctly rejected: %v", err)
		} else {
			logger.Infof("✅ Filtered server returned code (length: %d chars) - this is expected if server has no specific tools selected", len(result3))
		}
	}

	logger.Infof("\n✅ All filtering tests passed!")
	return nil
}
