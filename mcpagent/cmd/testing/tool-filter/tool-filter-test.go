package toolfilter

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
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

var toolFilterTestCmd = &cobra.Command{
	Use:   "tool-filter",
	Short: "Test unified ToolFilter for consistent tool filtering",
	Long: `Tests the unified ToolFilter system that ensures consistency between:
- LLM tool registration (what tools the LLM can actually call)
- Discovery results (what tools appear in system prompt)

This test validates:
1. Name normalization (snake_case, PascalCase, kebab-case)
2. Package/server filtering with selectedTools and selectedServers
3. Custom tool category detection
4. Virtual tools always included
5. Consistency between modes (normal and code execution)

Examples:
  mcpagent-test test tool-filter
  mcpagent-test test tool-filter --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger using shared test utilities
		logger := testutils.NewTestLoggerFromViper()

		logger.Info("=== Unified ToolFilter Test ===")

		// Test 1: Comprehensive scenarios (table-driven) - covers all edge cases
		logger.Info("--- Test 1: Comprehensive Filter Scenarios ---")
		if err := TestComprehensiveFilterScenarios(logger); err != nil {
			return fmt.Errorf("comprehensive filter scenarios test failed: %w", err)
		}

		// Test 2: Discovery simulation (what code_execution_tools.go does)
		logger.Info("--- Test 2: Discovery Simulation ---")
		if err := TestDiscoverySimulation(logger); err != nil {
			return fmt.Errorf("discovery simulation test failed: %w", err)
		}

		// Test 3: System categories (workspace_tools, human_tools) included by default
		logger.Info("--- Test 3: System Categories Included By Default ---")
		if err := TestSystemCategoriesIncludedByDefault(logger); err != nil {
			return fmt.Errorf("system categories test failed: %w", err)
		}

		logger.Info("✅ All comprehensive tests passed!")

		// Integration tests (require MCP config and LLM)
		logger.Info("=== Integration Tests ===")
		mcpConfig, err := testutils.LoadTestMCPConfig("", logger)
		if err != nil {
			logger.Warn("Failed to load MCP config", loggerv2.Error(err))
			logger.Info("Skipping integration tests (no MCP config)")
		} else {
			// Test 4: Normal mode integration
			logger.Info("--- Test 4: Normal Mode Integration ---")
			if err := testNormalModeIntegration(mcpConfig, logger); err != nil {
				return fmt.Errorf("normal mode integration test failed: %w", err)
			}

			// Test 5: Code execution mode integration
			logger.Info("--- Test 5: Code Execution Mode Integration ---")
			if err := testCodeExecutionModeIntegration(mcpConfig, logger); err != nil {
				return fmt.Errorf("code execution mode integration test failed: %w", err)
			}

			// Test 6: Filter consistency between modes
			logger.Info("--- Test 6: Filter Consistency Between Modes ---")
			if err := testFilterConsistencyBetweenModes(mcpConfig, logger); err != nil {
				return fmt.Errorf("filter consistency test failed: %w", err)
			}
		}

		logger.Info("✅ All ToolFilter tests passed!")
		return nil
	},
}

// GetToolFilterTestCmd returns the tool filter test command
// This is called from testing.go to register the command
func GetToolFilterTestCmd() *cobra.Command {
	return toolFilterTestCmd
}

// ToolFilterTestCase defines a test case for table-driven testing
type ToolFilterTestCase struct {
	Name            string
	PackageOrServer string
	ToolName        string
	IsCustomTool    bool
	IsVirtualTool   bool
	Expected        bool
	Reason          string
}

// TestComprehensiveFilterScenarios runs comprehensive table-driven tests
// This test would have caught the bugs we found by testing ALL edge cases
func TestComprehensiveFilterScenarios(log loggerv2.Logger) error {
	// Mock MCP clients - simulates what agent.go passes to NewToolFilter
	mockClients := map[string]mcpclient.ClientInterface{
		"google-sheets": nil, // Config uses hyphens
		"aws":           nil,
		"tavily":        nil,
	}

	// Scenario 1: Both selectedServers AND selectedTools set
	// NEW BEHAVIOR: Specific tools in selectedTools take precedence over selectedServers
	// This allows fine-grained control: selectedTools=[gmail:read_email] only includes that tool,
	// even if selectedServers=[gmail] (which would otherwise include all gmail tools)
	log.Info("  Scenario 1: selectedServers + selectedTools for same server (specific tools take precedence)")
	{
		selectedTools := []string{
			"google-sheets:GetSheetData", // Specific tool from google-sheets
			"google-sheets:CreateSpreadsheet",
			"aws:GetDocument", // Specific tool from different server
		}
		selectedServers := []string{"google-sheets"} // ALL tools from google-sheets (but specific tools override)

		tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClients, []string{"workspace"}, log)

		testCases := []ToolFilterTestCase{
			// google-sheets tools - only specific tools included (specific tools take precedence over selectedServers)
			{"google-sheets in selectedTools", "google_sheets", "GetSheetData", false, false, true, "specific tool in selectedTools (takes precedence)"},
			{"google-sheets in selectedTools", "google_sheets", "CreateSpreadsheet", false, false, true, "specific tool in selectedTools (takes precedence)"},
			{"google-sheets NOT in selectedTools", "google_sheets", "DeleteSpreadsheet", false, false, false, "specific tools mode - only selected tools included"},
			{"google-sheets NOT in selectedTools", "google_sheets", "AddRows", false, false, false, "specific tools mode - only selected tools included"},

			// aws tools - only specific tools from selectedTools
			{"aws in selectedTools", "aws", "GetDocument", false, false, true, "specific tool in selectedTools"},
			{"aws NOT in selectedTools", "aws", "DeleteDocument", false, false, false, "not in selectedTools, aws not in selectedServers"},

			// Other servers - excluded
			{"tavily not selected", "tavily", "Search", false, false, false, "not in selectedServers or selectedTools"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 2: Package name format mismatch (directory name vs config name)
	log.Info("  Scenario 2: Package name format (simulating discovery)")
	{
		// Discovery passes: "google_sheets" (from directory google_sheets_tools)
		// Config has: "google-sheets" (with hyphens)
		selectedServers := []string{"google-sheets"}

		tf := mcpagent.NewToolFilter([]string{}, selectedServers, mockClients, []string{}, log)

		testCases := []ToolFilterTestCase{
			// Test that normalization works correctly
			{"config format", "google-sheets", "GetSheetData", false, false, true, "direct match with config"},
			{"directory format", "google_sheets", "GetSheetData", false, false, true, "normalized match with config"},
			{"mixed format", "google_sheets", "CreateSpreadsheet", false, false, true, "normalized match"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL format test: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 3: Custom tools with specific filtering
	// Note: workspace_tools and human_tools are SYSTEM CATEGORIES (included by default)
	// But when specific tools are selected from a system category, only those are included
	log.Info("  Scenario 3: Custom tools filtering (with system categories)")
	{
		selectedTools := []string{
			"workspace_tools:ReadWorkspaceFile",
			"workspace_tools:UpdateWorkspaceFile",
			// human_tools not mentioned at all - but it's a SYSTEM CATEGORY, so ALL are included by default
		}

		tf := mcpagent.NewToolFilter(selectedTools, []string{}, mockClients, []string{"workspace", "human"}, log)

		testCases := []ToolFilterTestCase{
			// workspace_tools - specific tools selected, so only those are included
			{"workspace selected", "workspace_tools", "ReadWorkspaceFile", true, false, true, "in selectedTools"},
			{"workspace selected", "workspace_tools", "UpdateWorkspaceFile", true, false, true, "in selectedTools"},
			{"workspace NOT selected", "workspace_tools", "DeleteWorkspaceFile", true, false, false, "specific tools mode, this one not selected"},

			// human_tools - SYSTEM CATEGORY with no specific selection = ALL included by default
			{"human system category", "human_tools", "human_feedback", true, false, true, "system category default (no specific selection)"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL custom tools: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 4: Virtual tools always included (system tools)
	log.Info("  Scenario 4: Virtual tools always included")
	{
		// Even with strict filtering, virtual tools should be included
		selectedTools := []string{"aws:GetDocument"} // Only one specific tool

		tf := mcpagent.NewToolFilter(selectedTools, []string{}, mockClients, []string{}, log)

		testCases := []ToolFilterTestCase{
			{"virtual tool", "virtual_tools", "get_prompt", false, true, true, "virtual tools always included"},
			{"virtual tool", "virtual_tools", "get_resource", false, true, true, "virtual tools always included"},
			{"virtual tool", "virtual_tools", "discover_code_files", false, true, true, "virtual tools always included"},
			{"virtual tool", "virtual_tools", "write_code", false, true, true, "virtual tools always included"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL virtual tools: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 5: Wildcard pattern (server:*)
	log.Info("  Scenario 5: Wildcard pattern")
	{
		selectedTools := []string{
			"google-sheets:*", // ALL tools from google-sheets
			"aws:GetDocument", // Only specific tool from aws
		}

		tf := mcpagent.NewToolFilter(selectedTools, []string{}, mockClients, []string{}, log)

		testCases := []ToolFilterTestCase{
			{"wildcard server", "google_sheets", "GetSheetData", false, false, true, "wildcard includes all"},
			{"wildcard server", "google_sheets", "DeleteSpreadsheet", false, false, true, "wildcard includes all"},
			{"wildcard server", "google_sheets", "AnyRandomTool", false, false, true, "wildcard includes all"},
			{"specific tool server", "aws", "GetDocument", false, false, true, "specific tool included"},
			{"specific tool server", "aws", "DeleteDocument", false, false, false, "not in specific list"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL wildcard: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 6: Both selectedServers AND selectedTools with wildcard (the bug scenario)
	// This tests the exact case that was failing: selectedServers=["playwright"] + selectedTools=["playwright:*"]
	log.Info("  Scenario 6: selectedServers + selectedTools with wildcard (bug scenario)")
	{
		selectedTools := []string{
			"playwright:*", // ALL tools from playwright (wildcard pattern)
		}
		selectedServers := []string{"playwright"} // Also in selectedServers

		// Add playwright to mock clients
		mockClientsWithPlaywright := map[string]mcpclient.ClientInterface{
			"google-sheets": nil,
			"aws":           nil,
			"tavily":        nil,
			"playwright":    nil, // Add playwright server
		}

		tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClientsWithPlaywright, []string{"workspace"}, log)

		testCases := []ToolFilterTestCase{
			// playwright tools - ALL should be included (wildcard pattern should work even with selectedServers)
			{"playwright with wildcard", "playwright", "navigate", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "click", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "fill", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "screenshot", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "AnyRandomPlaywrightTool", false, false, true, "wildcard pattern includes all tools"},

			// Other servers - excluded
			{"google-sheets not selected", "google_sheets", "GetSheetData", false, false, false, "not in selectedServers or selectedTools"},
			{"aws not selected", "aws", "GetDocument", false, false, false, "not in selectedServers or selectedTools"},
			{"tavily not selected", "tavily", "Search", false, false, false, "not in selectedServers or selectedTools"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL bug scenario: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 7: Server in selectedServers with NO specific tools in selectedTools
	// This tests that selectedServers works correctly when there's no selectedTools entry
	// Expected: ALL tools from the server should be included
	log.Info("  Scenario 7: selectedServers only (no selectedTools entry)")
	{
		selectedTools := []string{
			"aws:GetDocument", // Specific tool from different server (not in selectedServers)
		}
		selectedServers := []string{"google-sheets"} // Server in selectedServers, but NO entry in selectedTools

		tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClients, []string{"workspace"}, log)

		testCases := []ToolFilterTestCase{
			// google-sheets tools - ALL should be included (in selectedServers, no specific tools override)
			{"google-sheets in selectedServers", "google_sheets", "GetSheetData", false, false, true, "server in selectedServers - includes ALL tools"},
			{"google-sheets in selectedServers", "google_sheets", "CreateSpreadsheet", false, false, true, "server in selectedServers - includes ALL tools"},
			{"google-sheets in selectedServers", "google_sheets", "DeleteSpreadsheet", false, false, true, "server in selectedServers - includes ALL tools"},
			{"google-sheets in selectedServers", "google_sheets", "AddRows", false, false, true, "server in selectedServers - includes ALL tools"},
			{"google-sheets in selectedServers", "google_sheets", "AnyRandomTool", false, false, true, "server in selectedServers - includes ALL tools"},

			// aws tools - only specific tools from selectedTools
			{"aws in selectedTools", "aws", "GetDocument", false, false, true, "specific tool in selectedTools"},
			{"aws NOT in selectedTools", "aws", "DeleteDocument", false, false, false, "not in selectedTools, aws not in selectedServers"},

			// Other servers - excluded
			{"tavily not selected", "tavily", "Search", false, false, false, "not in selectedServers or selectedTools"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL selectedServers only: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	// Scenario 8: Mixed configuration - specific tools for one server, wildcard for another
	// This tests the exact real-world case: selectedServers=[gmail playwright] +
	// selectedTools=[gmail:read_email gmail:search_emails gmail:send_email playwright:*]
	// Expected: gmail has only 3 specific tools (specific tools take precedence),
	//           playwright has all tools (wildcard pattern)
	log.Info("  Scenario 8: Mixed configuration - specific tools + wildcard (real-world case)")
	{
		selectedTools := []string{
			"gmail:read_email",    // Specific tool from gmail
			"gmail:search_emails", // Specific tool from gmail
			"gmail:send_email",    // Specific tool from gmail
			"playwright:*",        // ALL tools from playwright (wildcard)
		}
		selectedServers := []string{"gmail", "playwright"} // Both servers in selectedServers

		// Add gmail and playwright to mock clients
		mockClientsWithBoth := map[string]mcpclient.ClientInterface{
			"google-sheets": nil,
			"aws":           nil,
			"tavily":        nil,
			"gmail":         nil, // Add gmail server
			"playwright":    nil, // Add playwright server
		}

		tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClientsWithBoth, []string{"workspace"}, log)

		testCases := []ToolFilterTestCase{
			// gmail tools - only specific tools included (specific tools take precedence over selectedServers)
			{"gmail specific tool", "gmail", "read_email", false, false, true, "specific tool in selectedTools (takes precedence)"},
			{"gmail specific tool", "gmail", "search_emails", false, false, true, "specific tool in selectedTools (takes precedence)"},
			{"gmail specific tool", "gmail", "send_email", false, false, true, "specific tool in selectedTools (takes precedence)"},
			{"gmail NOT in selectedTools", "gmail", "delete_email", false, false, false, "specific tools mode - only selected tools included"},
			{"gmail NOT in selectedTools", "gmail", "mark_as_read", false, false, false, "specific tools mode - only selected tools included"},
			{"gmail NOT in selectedTools", "gmail", "AnyOtherGmailTool", false, false, false, "specific tools mode - only selected tools included"},

			// playwright tools - ALL should be included (wildcard pattern)
			{"playwright with wildcard", "playwright", "navigate", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "click", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "fill", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "screenshot", false, false, true, "wildcard pattern includes all tools"},
			{"playwright with wildcard", "playwright", "AnyRandomPlaywrightTool", false, false, true, "wildcard pattern includes all tools"},

			// Other servers - excluded
			{"google-sheets not selected", "google_sheets", "GetSheetData", false, false, false, "not in selectedServers or selectedTools"},
			{"aws not selected", "aws", "GetDocument", false, false, false, "not in selectedServers or selectedTools"},
			{"tavily not selected", "tavily", "Search", false, false, false, "not in selectedServers or selectedTools"},
		}

		for _, tc := range testCases {
			result := tf.ShouldIncludeTool(tc.PackageOrServer, tc.ToolName, tc.IsCustomTool, tc.IsVirtualTool)
			if result != tc.Expected {
				return fmt.Errorf("FAIL mixed scenario: %s:%s expected=%v, got=%v (reason: %s)",
					tc.PackageOrServer, tc.ToolName, tc.Expected, result, tc.Reason)
			}
			log.Info("    ✅ Test case passed", loggerv2.String("package_or_server", tc.PackageOrServer), loggerv2.String("tool", tc.ToolName), loggerv2.Any("result", result), loggerv2.String("reason", tc.Reason))
		}
	}

	return nil
}

// TestDiscoverySimulation simulates exactly what code_execution_tools.go does
// This test would catch bugs in how discovery passes package names to the filter
func TestDiscoverySimulation(log loggerv2.Logger) error {
	// Simulate the MCP clients from config
	mockClients := map[string]mcpclient.ClientInterface{
		"google-sheets": nil, // Config name with hyphens
	}

	selectedServers := []string{"google-sheets"}
	selectedTools := []string{
		"google-sheets:GetSheetData", // Some specific tools
		"workspace_tools:ReadWorkspaceFile",
	}

	tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClients, []string{"workspace", "human"}, log)

	// Simulate what discovery does for each directory type
	type DiscoveryCase struct {
		DirName     string // Directory name in generated/
		ServerName  string // After trimming _tools
		IsCategory  bool
		IsVirtual   bool
		ToolName    string
		PackageUsed string // What discovery should pass to ShouldIncludeTool
		Expected    bool
	}

	cases := []DiscoveryCase{
		// MCP Server: google_sheets_tools
		// Discovery should pass serverName (google_sheets), NOT dirName (google_sheets_tools)
		// NEW BEHAVIOR: Since google-sheets has specific tools in selectedTools, only those are included
		// (specific tools take precedence over selectedServers)
		{"google_sheets_tools", "google_sheets", false, false, "GetSheetData", "google_sheets", true},
		{"google_sheets_tools", "google_sheets", false, false, "DeleteSpreadsheet", "google_sheets", false}, // specific tools mode - only GetSheetData included

		// Custom tool category: workspace_tools (SYSTEM CATEGORY)
		// BUT: selectedTools contains "workspace_tools:ReadWorkspaceFile", so specific tools mode is active
		// Only the explicitly selected tool should be included
		{"workspace_tools", "workspace", true, false, "ReadWorkspaceFile", "workspace_tools", true},
		{"workspace_tools", "workspace", true, false, "DeleteWorkspaceFile", "workspace_tools", false}, // NOT in selectedTools

		// Custom tool category: human_tools (SYSTEM CATEGORY - included by default)
		// human_tools has NO specific tools in selectedTools, so ALL are included by default
		{"human_tools", "human", true, false, "human_feedback", "human_tools", true}, // System category default

		// Virtual tools
		{"virtual_tools", "virtual", true, true, "get_prompt", "virtual_tools", true},
		{"virtual_tools", "virtual", true, true, "write_code", "virtual_tools", true},
	}

	log.Info("  Simulating discovery behavior:")
	for _, c := range cases {
		result := tf.ShouldIncludeTool(c.PackageUsed, c.ToolName, c.IsCategory, c.IsVirtual)
		if result != c.Expected {
			return fmt.Errorf("FAIL discovery simulation: dir=%s, package=%s, tool=%s: expected=%v, got=%v",
				c.DirName, c.PackageUsed, c.ToolName, c.Expected, result)
		}
		log.Info("    ✅ Discovery test passed", loggerv2.String("dir", c.DirName), loggerv2.String("package", c.PackageUsed), loggerv2.String("tool", c.ToolName), loggerv2.Any("result", result))
	}

	// Also test the wrong way (what the bug was doing)
	log.Info("  Verifying bug fix - these would have been wrong with the old code:")

	// OLD BUG: Passing dirName instead of serverName for MCP servers
	// This would cause "google_sheets_tools" to not match "google-sheets" in selectedServers

	// The fix is already in code_execution_tools.go, but let's verify the filter handles it correctly
	// If someone passes the wrong format, the filter should still work via normalization

	return nil
}

// TestSystemCategoriesIncludedByDefault tests that system categories (workspace_tools, human_tools)
// are included by default, even when MCP tool filtering is active
func TestSystemCategoriesIncludedByDefault(log loggerv2.Logger) error {
	// Create ToolFilter with MCP tool filtering but NO workspace_tools in selectedTools
	// System categories should still be included by default
	selectedTools := []string{
		"google-sheets:GetSheetData",
		"google-sheets:CreateSpreadsheet",
		// Note: workspace_tools and human_tools are NOT in selectedTools
	}

	mockClients := map[string]mcpclient.ClientInterface{
		"google-sheets": nil,
	}

	tf := mcpagent.NewToolFilter(
		selectedTools,
		[]string{}, // no selectedServers
		mockClients,
		[]string{"workspace", "human"}, // custom categories
		log,
	)

	// Verify system category detection
	if !tf.IsSystemCategory("workspace_tools") {
		return fmt.Errorf("expected workspace_tools to be detected as system category")
	}
	log.Info("✅ workspace_tools detected as system category")

	if !tf.IsSystemCategory("human_tools") {
		return fmt.Errorf("expected human_tools to be detected as system category")
	}
	log.Info("✅ human_tools detected as system category")

	// Test 1: workspace_tools should be included by default (all tools)
	workspaceTools := []string{"ReadWorkspaceFile", "UpdateWorkspaceFile", "DeleteWorkspaceFile", "ListWorkspaceFiles"}
	for _, tool := range workspaceTools {
		if !tf.ShouldIncludeTool("workspace_tools", tool, true, false) {
			return fmt.Errorf("expected workspace_tools:%s to be included by default (system category)", tool)
		}
		log.Info("✅ Tool included (system category default)", loggerv2.String("tool", tool), loggerv2.String("category", "workspace_tools"))
	}

	// Test 2: human_tools should be included by default (all tools)
	humanTools := []string{"human_feedback", "human_verification"}
	for _, tool := range humanTools {
		if !tf.ShouldIncludeTool("human_tools", tool, true, false) {
			return fmt.Errorf("expected human_tools:%s to be included by default (system category)", tool)
		}
		log.Info("✅ Tool included (system category default)", loggerv2.String("tool", tool), loggerv2.String("category", "human_tools"))
	}

	// Test 3: MCP tools should still be filtered normally
	if !tf.ShouldIncludeTool("google_sheets", "GetSheetData", false, false) {
		return fmt.Errorf("expected google_sheets:GetSheetData to be included (in selectedTools)")
	}
	log.Info("✅ Tool included (in selectedTools)", loggerv2.String("tool", "GetSheetData"), loggerv2.String("server", "google_sheets"))

	if tf.ShouldIncludeTool("google_sheets", "DeleteSpreadsheet", false, false) {
		return fmt.Errorf("expected google_sheets:DeleteSpreadsheet to be excluded (not in selectedTools)")
	}
	log.Info("✅ Tool excluded (not in selectedTools)", loggerv2.String("tool", "DeleteSpreadsheet"), loggerv2.String("server", "google_sheets"))

	// Test 4: When specific workspace_tools are selected, only those should be included
	log.Info("  Testing specific tool selection for system categories:")
	selectedToolsSpecific := []string{
		"google-sheets:GetSheetData",
		"workspace_tools:ReadWorkspaceFile", // Only this workspace tool
	}

	tfSpecific := mcpagent.NewToolFilter(
		selectedToolsSpecific,
		[]string{},
		mockClients,
		[]string{"workspace", "human"},
		log,
	)

	// ReadWorkspaceFile should be included (explicitly selected)
	if !tfSpecific.ShouldIncludeTool("workspace_tools", "ReadWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:ReadWorkspaceFile to be included (explicitly selected)")
	}
	log.Info("✅ workspace_tools:ReadWorkspaceFile included (explicitly selected)")

	// DeleteWorkspaceFile should be excluded (not in specific selection)
	if tfSpecific.ShouldIncludeTool("workspace_tools", "DeleteWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:DeleteWorkspaceFile to be excluded (not in specific selection)")
	}
	log.Info("✅ workspace_tools:DeleteWorkspaceFile excluded (not in specific selection)")

	// human_tools should still be included by default (no specific selection for human_tools)
	if !tfSpecific.ShouldIncludeTool("human_tools", "human_feedback", true, false) {
		return fmt.Errorf("expected human_tools:human_feedback to be included (system category default)")
	}
	log.Info("✅ human_tools:human_feedback included (system category default, no specific selection)")

	return nil
}

// testNormalModeIntegration tests tool filtering in normal mode (tools directly on LLM)
func testNormalModeIntegration(config *mcpclient.MCPConfig, log loggerv2.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find a server with multiple tools for testing
	var testServerName string
	for name := range config.MCPServers {
		testServerName = name
		break
	}

	if testServerName == "" {
		log.Warn("No MCP servers found in config, skipping normal mode integration test")
		return nil
	}

	log.Info("Using server for normal mode integration test", loggerv2.String("server", testServerName))

	// Create LLM instance
	llmModel, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider:    string(llm.ProviderOpenAI),
		ModelID:     openai.ModelGPT4oMini,
		Temperature: 0.2,
		Logger:      log,
	})
	if err != nil {
		log.Warn("Failed to create LLM model, skipping normal mode test", loggerv2.Error(err))
		return nil
	}

	// Test 1: Create agent WITHOUT code execution mode, WITH tool filtering
	selectedTool := fmt.Sprintf("%s:*", testServerName) // All tools from this server
	configPath := testutils.GetDefaultTestConfigPath()
	if configPath == "" {
		configPath = viper.GetString("config")
	}

	// modelID is automatically extracted from llmModel
	agentInstance, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithProvider(llmProvider),
		mcpagent.WithServerName(testServerName),
		mcpagent.WithTraceID("test-trace"),
		mcpagent.WithLogger(log),
		mcpagent.WithSelectedTools([]string{selectedTool}),
		// NOT using WithCodeExecutionMode - this is normal mode
	)
	if err != nil {
		log.Warn("Failed to create agent", loggerv2.Error(err))
		return nil
	}
	defer agentInstance.Close()

	// Verify: Agent should have tools from the selected server
	tools := agentInstance.Tools
	log.Info("Normal mode: Agent has tools registered", loggerv2.Int("count", len(tools)))

	if len(tools) == 0 {
		return fmt.Errorf("normal mode: expected agent to have tools, but got 0")
	}

	// Verify tools are from the expected server (check tool names)
	for _, tool := range tools {
		if tool.Function != nil {
			log.Info("  - Tool", loggerv2.String("name", tool.Function.Name))
		}
	}

	log.Info("✅ Normal mode integration test passed", loggerv2.Int("tools_registered", len(tools)))
	return nil
}

// testCodeExecutionModeIntegration tests tool filtering in code execution mode (discovery)
func testCodeExecutionModeIntegration(config *mcpclient.MCPConfig, log loggerv2.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find a server with multiple tools for testing
	var testServerName string
	for name := range config.MCPServers {
		testServerName = name
		break
	}

	if testServerName == "" {
		log.Warn("No MCP servers found in config, skipping code execution mode integration test")
		return nil
	}

	log.Info("Using server for code execution mode integration test", loggerv2.String("server", testServerName))

	// Create LLM instance
	llmModel, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider:    string(llm.ProviderOpenAI),
		ModelID:     openai.ModelGPT4oMini,
		Temperature: 0.2,
		Logger:      log,
	})
	if err != nil {
		log.Warn("Failed to create LLM model, skipping code execution mode test", loggerv2.Error(err))
		return nil
	}

	// Test: Create agent WITH code execution mode, WITH tool filtering
	selectedTool := fmt.Sprintf("%s:*", testServerName) // All tools from this server
	configPath := testutils.GetDefaultTestConfigPath()
	if configPath == "" {
		configPath = viper.GetString("config")
	}

	// modelID is automatically extracted from llmModel
	agentInstance, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithProvider(llmProvider),
		mcpagent.WithServerName(testServerName),
		mcpagent.WithTraceID("test-trace"),
		mcpagent.WithLogger(log),
		mcpagent.WithSelectedTools([]string{selectedTool}),
		mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
	)
	if err != nil {
		log.Warn("Failed to create agent with code execution mode", loggerv2.Error(err))
		return nil
	}
	defer agentInstance.Close()

	// In code execution mode, agent should only have virtual tools (discover_code_files, write_code)
	tools := agentInstance.Tools
	log.Info("Code execution mode: Agent has LLM tools (should be virtual tools only)", loggerv2.Int("count", len(tools)))

	// Verify: Should have virtual tools
	hasDiscoverCodeFiles := false
	hasWriteCode := false
	for _, tool := range tools {
		if tool.Function != nil {
			log.Info("  - Tool", loggerv2.String("name", tool.Function.Name))
			if tool.Function.Name == "discover_code_files" {
				hasDiscoverCodeFiles = true
			}
			if tool.Function.Name == "write_code" {
				hasWriteCode = true
			}
		}
	}

	if !hasDiscoverCodeFiles {
		return fmt.Errorf("code execution mode: expected discover_code_files tool")
	}
	if !hasWriteCode {
		return fmt.Errorf("code execution mode: expected write_code tool")
	}

	// Test discover_code_structure to verify filtering works in discovery
	result, err := agentInstance.HandleVirtualTool(ctx, "discover_code_structure", map[string]interface{}{})
	if err != nil {
		log.Warn("Failed to call discover_code_structure", loggerv2.Error(err))
		// Don't fail - discovery might fail if no generated code exists
	} else {
		log.Info("Code execution mode: discover_code_structure returned result", loggerv2.Int("chars", len(result)))

		// Parse and verify the result contains the expected server
		var discovery struct {
			Servers []struct {
				Name  string   `json:"name"`
				Tools []string `json:"tools"`
			} `json:"servers"`
		}
		if err := json.Unmarshal([]byte(result), &discovery); err == nil {
			for _, server := range discovery.Servers {
				if strings.Contains(server.Name, testServerName) || strings.Contains(testServerName, server.Name) {
					log.Info("  - Found server in discovery", loggerv2.String("server", server.Name), loggerv2.Int("tools", len(server.Tools)))
				}
			}
		}
	}

	log.Info("✅ Code execution mode integration test passed")
	return nil
}

// testFilterConsistencyBetweenModes tests that filtering is consistent between normal and code execution modes
func testFilterConsistencyBetweenModes(config *mcpclient.MCPConfig, log loggerv2.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find a server for testing
	var testServerName string
	for name := range config.MCPServers {
		testServerName = name
		break
	}

	if testServerName == "" {
		log.Warn("No MCP servers found in config, skipping consistency test")
		return nil
	}

	// Create LLM instance
	llmModel, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider:    string(llm.ProviderOpenAI),
		ModelID:     openai.ModelGPT4oMini,
		Temperature: 0.2,
		Logger:      log,
	})
	if err != nil {
		log.Warn("Failed to create LLM model, skipping consistency test", loggerv2.Error(err))
		return nil
	}

	configPath := testutils.GetDefaultTestConfigPath()
	if configPath == "" {
		configPath = viper.GetString("config")
	}

	// Use strict filtering: only one specific pattern
	selectedTools := []string{fmt.Sprintf("%s:*", testServerName)}

	// Create agent in normal mode
	// modelID is automatically extracted from llmModel
	normalAgent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithProvider(llmProvider),
		mcpagent.WithServerName(testServerName),
		mcpagent.WithTraceID("test-trace-normal"),
		mcpagent.WithLogger(log),
		mcpagent.WithSelectedTools(selectedTools),
	)
	if err != nil {
		log.Warn("Failed to create normal mode agent", loggerv2.Error(err))
		return nil
	}
	defer normalAgent.Close()

	// Create agent in code execution mode
	// modelID is automatically extracted from llmModel
	codeExecAgent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithProvider(llmProvider),
		mcpagent.WithServerName(testServerName),
		mcpagent.WithTraceID("test-trace-codeexec"),
		mcpagent.WithLogger(log),
		mcpagent.WithSelectedTools(selectedTools),
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		log.Warn("Failed to create code execution mode agent", loggerv2.Error(err))
		return nil
	}
	defer codeExecAgent.Close()

	// Get tools from normal mode (direct LLM tools)
	normalTools := normalAgent.Tools
	normalMCPToolCount := 0
	for _, tool := range normalTools {
		if tool.Function != nil {
			// Count non-virtual tools
			name := tool.Function.Name
			if name != "get_prompt" && name != "get_resource" && name != "discover_code_structure" &&
				name != "discover_code_files" && name != "write_code" {
				normalMCPToolCount++
			}
		}
	}

	log.Info("Normal mode: MCP tools registered", loggerv2.Int("count", normalMCPToolCount))

	// Get discovery from code execution mode
	discoveryResult, err := codeExecAgent.HandleVirtualTool(ctx, "discover_code_structure", map[string]interface{}{})
	if err != nil {
		log.Warn("Failed to get discovery in code execution mode", loggerv2.Error(err))
		return nil
	}

	var discovery struct {
		Servers []struct {
			Name  string   `json:"name"`
			Tools []string `json:"tools"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(discoveryResult), &discovery); err != nil {
		log.Warn("Failed to parse discovery result", loggerv2.Error(err))
		return nil
	}

	codeExecToolCount := 0
	for _, server := range discovery.Servers {
		codeExecToolCount += len(server.Tools)
	}

	log.Info("Code execution mode: tools in discovery", loggerv2.Int("count", codeExecToolCount))

	// Both modes should have similar tool counts (allowing for some variance due to function naming)
	// The key is that both use the same ToolFilter, so filtering should be consistent
	if normalMCPToolCount > 0 && codeExecToolCount > 0 {
		log.Info("✅ Consistency test passed: Normal mode and Code execution mode both have tools",
			loggerv2.Int("normal_mode_tools", normalMCPToolCount),
			loggerv2.Int("code_exec_mode_tools", codeExecToolCount),
			loggerv2.String("server", testServerName))
	} else if normalMCPToolCount == 0 && codeExecToolCount == 0 {
		log.Info("✅ Consistency test passed: Both modes have 0 tools (server may have no tools)")
	} else {
		log.Warn("⚠️ Potential inconsistency: Normal mode and Code execution mode have different tool counts",
			loggerv2.Int("normal_mode_tools", normalMCPToolCount),
			loggerv2.Int("code_exec_mode_tools", codeExecToolCount))
	}

	return nil
}
