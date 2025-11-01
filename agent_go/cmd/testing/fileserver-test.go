package testing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var fileserverTestCmd = &cobra.Command{
	Use:   "fileserver",
	Short: "Test MCP fileserver tools functionality",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info("=== MCP Fileserver Tools Test ===")

		// Load MCP server configurations
		configPath := viper.GetString("config")
		if configPath == "" {
			configPath = "configs/mcp_servers.json"
		}

		logger.Infof("Loading merged config from: %s", configPath)
		config, err := mcpclient.LoadMergedConfig(configPath, logger)
		if err != nil {
			return fmt.Errorf("failed to load merged config: %w", err)
		}

		// Find fileserver configuration
		var fileserverConfig *mcpclient.MCPServerConfig
		for serverName, serverConfig := range config.MCPServers {
			if strings.Contains(strings.ToLower(serverName), "fileserver") ||
				strings.Contains(strings.ToLower(serverName), "read_large_tool_output") {
				fileserverConfig = &serverConfig
				logger.Infof("Found fileserver config: %s", serverName)
				break
			}
		}

		if fileserverConfig == nil {
			return fmt.Errorf("no fileserver configuration found in config")
		}

		// Create direct connection to fileserver
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		client := mcpclient.New(*fileserverConfig, logger)

		if err := client.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect to fileserver: %w", err)
		}
		defer client.Close()

		logger.Info("✅ Connected to fileserver successfully")

		// Test 1: List available tools
		logger.Info("\n--- Test 1: List Tools ---")
		if err := testListTools(ctx, client, logger); err != nil {
			return fmt.Errorf("list tools test failed: %w", err)
		}

		// Test 2: Create test files and test fileserver tools
		logger.Info("\n--- Test 2: Fileserver Tools Integration ---")
		if err := testFileserverTools(ctx, client, logger); err != nil {
			return fmt.Errorf("fileserver tools test failed: %w", err)
		}

		// Test 3: Test with existing files from tool output handler
		logger.Info("\n--- Test 3: Test with Existing Tool Output Files ---")
		if err := testWithExistingFiles(ctx, client, logger); err != nil {
			return fmt.Errorf("existing files test failed: %w", err)
		}

		logger.Info("\n✅ All fileserver tests passed!")
		return nil
	},
}

func testListTools(ctx context.Context, client *mcpclient.Client, logger utils.ExtendedLogger) error {

	// List tools with details
	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	logger.Infof("Found %d tools:", len(tools))
	for i, tool := range tools {
		logger.Infof("  %d. %s", i+1, tool.Name)
		if tool.Description != "" {
			logger.Infof("     Description: %s", tool.Description)
		}
	}

	// Check for expected tools
	expectedTools := []string{"read_characters", "search_regex_in_file", "jq_query"}
	foundTools := make(map[string]bool)
	for _, tool := range tools {
		foundTools[tool.Name] = true
	}

	for _, expectedTool := range expectedTools {
		if !foundTools[expectedTool] {
			logger.Infof("⚠️  Expected tool '%s' not found", expectedTool)
		} else {
			logger.Infof("✅ Found expected tool: %s", expectedTool)
		}
	}

	return nil
}

func testFileserverTools(ctx context.Context, client *mcpclient.Client, logger utils.ExtendedLogger) error {

	// Create test directory and files
	testDir := "fileserver_test"
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return fmt.Errorf("failed to create test directory: %w", err)
	}
	defer os.RemoveAll(testDir)

	// Create test files
	testFiles := map[string]string{
		"test1.txt":  "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
		"test2.json": `{"name": "test", "value": 123, "items": ["a", "b", "c"]}`,
		"test3.txt":  "This is a longer file with multiple lines.\nSecond line here.\nThird line with some content.\nFourth line.\nFifth line.",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(testDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create test file %s: %w", filename, err)
		}
		logger.Infof("Created test file: %s", filePath)
	}

	// Test read_characters for each file
	for filename := range testFiles {
		filePath := filepath.Join(testDir, filename)
		logger.Infof("\nTesting read_characters on: %s (characters 1-50)", filePath)

		readCharsResult, err := client.CallTool(ctx, "read_characters", map[string]interface{}{
			"path":  filePath,
			"start": 1,
			"end":   50,
		})
		if err != nil {
			return fmt.Errorf("read_characters failed for %s: %w", filename, err)
		}
		logger.Infof("Read characters for %s: %s", filename, mcpclient.ToolResultAsString(readCharsResult, logger))
	}

	// Test search_regex_in_file
	logger.Info("\nTesting search_regex_in_file for 'line' in test1.txt")
	searchResult, err := client.CallTool(ctx, "search_regex_in_file", map[string]interface{}{
		"path":    filepath.Join(testDir, "test1.txt"),
		"pattern": "line",
	})
	if err != nil {
		return fmt.Errorf("search_regex_in_file failed: %w", err)
	}
	logger.Infof("Search result: %s", mcpclient.ToolResultAsString(searchResult, logger))

	// Test jq_query with JSON file
	logger.Info("\nTesting jq_query on test2.json")
	jqResult, err := client.CallTool(ctx, "jq_query", map[string]interface{}{
		"path":  filepath.Join(testDir, "test2.json"),
		"query": ".name",
	})
	if err != nil {
		return fmt.Errorf("jq_query failed: %w", err)
	}
	logger.Infof("jq query result for .name: %s", mcpclient.ToolResultAsString(jqResult, logger))

	// Test jq_query with array access
	logger.Info("\nTesting jq_query array access on test2.json")
	jqArrayResult, err := client.CallTool(ctx, "jq_query", map[string]interface{}{
		"path":  filepath.Join(testDir, "test2.json"),
		"query": ".items[]",
	})
	if err != nil {
		return fmt.Errorf("jq_query array access failed: %w", err)
	}
	logger.Infof("jq query result for .items[]: %s", mcpclient.ToolResultAsString(jqArrayResult, logger))

	// Test jq_query with compact output
	logger.Info("\nTesting jq_query array access on test2.json")
	jqCompactResult, err := client.CallTool(ctx, "jq_query", map[string]interface{}{
		"path":    filepath.Join(testDir, "test2.json"),
		"query":   ".",
		"compact": true,
	})
	if err != nil {
		return fmt.Errorf("jq_query compact output failed: %w", err)
	}
	logger.Infof("jq query result with compact output: %s", mcpclient.ToolResultAsString(jqCompactResult, logger))

	return nil
}

func testWithExistingFiles(ctx context.Context, client *mcpclient.Client, logger utils.ExtendedLogger) error {

	// Test with actual tool output files
	toolOutputDir := "tool_output_folder"
	if _, err := os.Stat(toolOutputDir); os.IsNotExist(err) {
		logger.Info("⚠️  Tool output directory not found, skipping existing files test")
		return nil
	}

	// Find a session directory
	sessionDirs, err := os.ReadDir(toolOutputDir)
	if err != nil {
		return fmt.Errorf("failed to read tool output directory: %w", err)
	}

	for _, sessionDir := range sessionDirs {
		if sessionDir.IsDir() {
			sessionPath := filepath.Join(toolOutputDir, sessionDir.Name())
			logger.Infof("\nTesting with session directory: %s", sessionPath)

			// Test with first file found
			files, err := os.ReadDir(sessionPath)
			if err != nil {
				logger.Infof("⚠️  Failed to read session directory: %w", err)
				continue
			}

			for _, file := range files {
				if !file.IsDir() {
					filePath := filepath.Join(sessionPath, file.Name())
					logger.Infof("\nTesting with file: %s", filePath)

					// Test read_characters (first few characters)
					readCharsResult, err := client.CallTool(ctx, "read_characters", map[string]interface{}{
						"path":  filePath,
						"start": 1,
						"end":   100,
					})
					if err != nil {
						logger.Infof("⚠️  read_characters failed for %s: %v", file.Name(), err)
						continue
					}
					logger.Infof("Read characters for %s: %s", file.Name(), mcpclient.ToolResultAsString(readCharsResult, logger))

					// Only test with first file to avoid too much output
					break
				}
			}
			break // Only test with first session directory
		}
	}

	return nil
}

func init() {
	TestingCmd.AddCommand(fileserverTestCmd)
}
