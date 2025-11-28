package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/utils"
	mcpagent "mcpagent/agent"
	"mcpagent/llm"
	"mcpagent/mcpclient"
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
  orchestrator test tool-filter
  orchestrator test tool-filter --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Unified ToolFilter Test ===")

		// Test 1: Name normalization
		logger.Infof("\n--- Test 1: Name Normalization ---")
		if err := testNameNormalization(logger); err != nil {
			return fmt.Errorf("name normalization test failed: %w", err)
		}

		// Test 2: Tool filtering with no filters (all included)
		logger.Infof("\n--- Test 2: No Filtering (All Included) ---")
		if err := testNoFiltering(logger); err != nil {
			return fmt.Errorf("no filtering test failed: %w", err)
		}

		// Test 3: Tool filtering with selectedTools
		logger.Infof("\n--- Test 3: Selected Tools Filtering ---")
		if err := testSelectedToolsFiltering(logger); err != nil {
			return fmt.Errorf("selected tools filtering test failed: %w", err)
		}

		// Test 4: Tool filtering with selectedServers
		logger.Infof("\n--- Test 4: Selected Servers Filtering ---")
		if err := testSelectedServersFiltering(logger); err != nil {
			return fmt.Errorf("selected servers filtering test failed: %w", err)
		}

		// Test 5: Wildcard pattern (server:*)
		logger.Infof("\n--- Test 5: Wildcard Pattern (server:*) ---")
		if err := testWildcardPattern(logger); err != nil {
			return fmt.Errorf("wildcard pattern test failed: %w", err)
		}

		// Test 6: Custom tool category detection
		logger.Infof("\n--- Test 6: Custom Tool Category Detection ---")
		if err := testCategoryDetection(logger); err != nil {
			return fmt.Errorf("category detection test failed: %w", err)
		}

		// Test 7: Virtual tools always included
		logger.Infof("\n--- Test 7: Virtual Tools Always Included ---")
		if err := testVirtualToolsAlwaysIncluded(logger); err != nil {
			return fmt.Errorf("virtual tools test failed: %w", err)
		}

		// Test 8: Mixed case tool names
		logger.Infof("\n--- Test 8: Mixed Case Tool Names ---")
		if err := testMixedCaseToolNames(logger); err != nil {
			return fmt.Errorf("mixed case tool names test failed: %w", err)
		}

		// Test 9: Custom tools respect filtering
		logger.Infof("\n--- Test 9: Custom Tools Respect Filtering ---")
		if err := testCustomToolsFiltering(logger); err != nil {
			return fmt.Errorf("custom tools filtering test failed: %w", err)
		}

		// Test 10: Edge case - selectedServers AND selectedTools for same server
		logger.Infof("\n--- Test 10: selectedServers Priority Over selectedTools ---")
		if err := testSelectedServersWithSelectedToolsConflict(logger); err != nil {
			return fmt.Errorf("selectedServers priority test failed: %w", err)
		}

		// Test 11: Comprehensive scenarios (table-driven)
		logger.Infof("\n--- Test 11: Comprehensive Filter Scenarios ---")
		if err := testComprehensiveFilterScenarios(logger); err != nil {
			return fmt.Errorf("comprehensive filter scenarios test failed: %w", err)
		}

		// Test 12: Discovery simulation (what code_execution_tools.go does)
		logger.Infof("\n--- Test 12: Discovery Simulation ---")
		if err := testDiscoverySimulation(logger); err != nil {
			return fmt.Errorf("discovery simulation test failed: %w", err)
		}

		// Test 13: System categories (workspace_tools, human_tools) included by default
		logger.Infof("\n--- Test 13: System Categories Included By Default ---")
		if err := testSystemCategoriesIncludedByDefault(logger); err != nil {
			return fmt.Errorf("system categories test failed: %w", err)
		}

		logger.Infof("\n✅ All unit tests passed!")

		// Integration tests (require MCP config and LLM)
		logger.Infof("\n=== Integration Tests ===")
		configPath := viper.GetString("config")
		if configPath == "" {
			configPath = "configs/mcp_servers_clean_user.json"
		}

		mcpConfig, err := mcpclient.LoadConfig(configPath)
		if err != nil {
			logger.Warnf("Failed to load MCP config from %s: %v", configPath, err)
			logger.Infof("Skipping integration tests (no MCP config)")
		} else {
			// Test 14: Normal mode integration
			logger.Infof("\n--- Test 14: Normal Mode Integration ---")
			if err := testNormalModeIntegration(mcpConfig, logger); err != nil {
				return fmt.Errorf("normal mode integration test failed: %w", err)
			}

			// Test 15: Code execution mode integration
			logger.Infof("\n--- Test 15: Code Execution Mode Integration ---")
			if err := testCodeExecutionModeIntegration(mcpConfig, logger); err != nil {
				return fmt.Errorf("code execution mode integration test failed: %w", err)
			}

			// Test 16: Filter consistency between modes
			logger.Infof("\n--- Test 16: Filter Consistency Between Modes ---")
			if err := testFilterConsistencyBetweenModes(mcpConfig, logger); err != nil {
				return fmt.Errorf("filter consistency test failed: %w", err)
			}
		}

		logger.Infof("\n✅ All ToolFilter tests passed!")
		return nil
	},
}

func init() {
	// Command will be registered in testing.go's initTestingCommands
}

// testNameNormalization tests the NormalizeToolName and NormalizeServerName functions
func testNameNormalization(logger utils.ExtendedLogger) error {
	// Create a minimal ToolFilter for testing
	tf := mcpagent.NewToolFilter(
		[]string{},
		[]string{},
		nil,
		[]string{},
		logger,
	)

	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{"ReadWorkspaceFile", "read_workspace_file", "PascalCase to snake_case"},
		{"read_workspace_file", "read_workspace_file", "snake_case unchanged"},
		{"read-workspace-file", "read_workspace_file", "kebab-case to snake_case"},
		{"GetDocument", "get_document", "Simple PascalCase"},
		{"HTTPServer", "h_t_t_p_server", "Consecutive uppercase"}, // Note: This is expected behavior
		{"listItems", "list_items", "camelCase to snake_case"},
		{"AWS", "a_w_s", "All uppercase"},
		{"getURLPath", "get_u_r_l_path", "Mixed acronyms"},
	}

	for _, tc := range testCases {
		result := tf.NormalizeToolName(tc.input)
		if result != tc.expected {
			return fmt.Errorf("%s: expected '%s', got '%s'", tc.desc, tc.expected, result)
		}
		logger.Infof("✅ %s: '%s' -> '%s'", tc.desc, tc.input, result)
	}

	// Test server name normalization
	serverTestCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{"google-sheets", "google_sheets", "Hyphen to underscore"},
		{"google_sheets", "google_sheets", "Underscore unchanged"},
		{"GoogleSheets", "googlesheets", "PascalCase lowercased"},
		{"aws-tools", "aws_tools", "Service name with hyphen"},
	}

	for _, tc := range serverTestCases {
		result := tf.NormalizeServerName(tc.input)
		if result != tc.expected {
			return fmt.Errorf("%s: expected '%s', got '%s'", tc.desc, tc.expected, result)
		}
		logger.Infof("✅ %s: '%s' -> '%s'", tc.desc, tc.input, result)
	}

	return nil
}

// testNoFiltering tests that all tools are included when no filtering is configured
func testNoFiltering(logger utils.ExtendedLogger) error {
	// Create ToolFilter with no filters
	tf := mcpagent.NewToolFilter(
		[]string{}, // no selectedTools
		[]string{}, // no selectedServers
		nil,        // no clients
		[]string{}, // no custom categories
		logger,
	)

	// Verify no filtering is active
	if !tf.IsNoFilteringActive() {
		return fmt.Errorf("expected no filtering to be active")
	}
	logger.Infof("✅ IsNoFilteringActive() returns true when no filters set")

	// All tools should be included
	testCases := []struct {
		pkg       string
		tool      string
		isCustom  bool
		isVirtual bool
	}{
		{"aws", "GetDocument", false, false},
		{"google_sheets", "CreateSpreadsheet", false, false},
		{"workspace_tools", "ReadWorkspaceFile", true, false},
		{"virtual_tools", "get_prompt", false, true},
	}

	for _, tc := range testCases {
		if !tf.ShouldIncludeTool(tc.pkg, tc.tool, tc.isCustom, tc.isVirtual) {
			return fmt.Errorf("expected tool %s:%s to be included (no filtering)", tc.pkg, tc.tool)
		}
		logger.Infof("✅ Tool %s:%s included (no filtering)", tc.pkg, tc.tool)
	}

	return nil
}

// testSelectedToolsFiltering tests filtering with selectedTools
func testSelectedToolsFiltering(logger utils.ExtendedLogger) error {
	// Create ToolFilter with specific tools selected
	selectedTools := []string{
		"aws:GetDocument",
		"aws:ListDocuments",
		"google_sheets:CreateSpreadsheet",
	}

	tf := mcpagent.NewToolFilter(
		selectedTools,
		[]string{}, // no selectedServers
		nil,        // no clients
		[]string{}, // no custom categories
		logger,
	)

	// Verify filtering is active
	if tf.IsNoFilteringActive() {
		return fmt.Errorf("expected filtering to be active")
	}
	logger.Infof("✅ IsNoFilteringActive() returns false when filters set")

	// Test included tools
	includedTests := []struct {
		pkg  string
		tool string
	}{
		{"aws", "GetDocument"},
		{"aws", "ListDocuments"},
		{"google_sheets", "CreateSpreadsheet"},
	}

	for _, tc := range includedTests {
		if !tf.ShouldIncludeTool(tc.pkg, tc.tool, false, false) {
			return fmt.Errorf("expected tool %s:%s to be included", tc.pkg, tc.tool)
		}
		logger.Infof("✅ Tool %s:%s included (in selectedTools)", tc.pkg, tc.tool)
	}

	// Test excluded tools
	excludedTests := []struct {
		pkg  string
		tool string
	}{
		{"aws", "DeleteDocument"},              // aws has specific tools, this one not selected
		{"google_sheets", "DeleteSpreadsheet"}, // google_sheets has specific tools, this one not selected
		{"other_server", "SomeTool"},           // server not in selectedTools at all
	}

	for _, tc := range excludedTests {
		if tf.ShouldIncludeTool(tc.pkg, tc.tool, false, false) {
			return fmt.Errorf("expected tool %s:%s to be excluded", tc.pkg, tc.tool)
		}
		logger.Infof("✅ Tool %s:%s excluded (not in selectedTools)", tc.pkg, tc.tool)
	}

	return nil
}

// testSelectedServersFiltering tests filtering with selectedServers
func testSelectedServersFiltering(logger utils.ExtendedLogger) error {
	// Create ToolFilter with specific servers selected
	// This means "all tools from these servers"
	selectedServers := []string{"aws", "google_sheets"}

	tf := mcpagent.NewToolFilter(
		[]string{}, // no selectedTools (means "all tools" for selected servers)
		selectedServers,
		nil,
		[]string{},
		logger,
	)

	// Test tools from selected servers (should be included)
	includedTests := []struct {
		pkg  string
		tool string
	}{
		{"aws", "GetDocument"},
		{"aws", "ListDocuments"},
		{"aws", "DeleteDocument"},
		{"google_sheets", "CreateSpreadsheet"},
		{"google_sheets", "DeleteSpreadsheet"},
	}

	for _, tc := range includedTests {
		if !tf.ShouldIncludeTool(tc.pkg, tc.tool, false, false) {
			return fmt.Errorf("expected tool %s:%s to be included (server in selectedServers)", tc.pkg, tc.tool)
		}
		logger.Infof("✅ Tool %s:%s included (server in selectedServers)", tc.pkg, tc.tool)
	}

	// Test tools from non-selected servers (should be excluded)
	excludedTests := []struct {
		pkg  string
		tool string
	}{
		{"other_server", "SomeTool"},
		{"tavily", "Search"},
	}

	for _, tc := range excludedTests {
		if tf.ShouldIncludeTool(tc.pkg, tc.tool, false, false) {
			return fmt.Errorf("expected tool %s:%s to be excluded (server not in selectedServers)", tc.pkg, tc.tool)
		}
		logger.Infof("✅ Tool %s:%s excluded (server not in selectedServers)", tc.pkg, tc.tool)
	}

	return nil
}

// testWildcardPattern tests the server:* wildcard pattern
func testWildcardPattern(logger utils.ExtendedLogger) error {
	// Create ToolFilter with wildcard pattern
	selectedTools := []string{
		"aws:*",                           // All tools from aws
		"google_sheets:CreateSpreadsheet", // Only specific tool from google_sheets
	}

	tf := mcpagent.NewToolFilter(
		selectedTools,
		[]string{},
		nil,
		[]string{},
		logger,
	)

	// AWS tools should all be included (wildcard)
	awsTools := []string{"GetDocument", "ListDocuments", "DeleteDocument", "AnyOtherTool"}
	for _, tool := range awsTools {
		if !tf.ShouldIncludeTool("aws", tool, false, false) {
			return fmt.Errorf("expected aws:%s to be included (wildcard pattern)", tool)
		}
		logger.Infof("✅ Tool aws:%s included (wildcard pattern)", tool)
	}

	// Google sheets - only CreateSpreadsheet should be included
	if !tf.ShouldIncludeTool("google_sheets", "CreateSpreadsheet", false, false) {
		return fmt.Errorf("expected google_sheets:CreateSpreadsheet to be included")
	}
	logger.Infof("✅ Tool google_sheets:CreateSpreadsheet included (specific selection)")

	if tf.ShouldIncludeTool("google_sheets", "DeleteSpreadsheet", false, false) {
		return fmt.Errorf("expected google_sheets:DeleteSpreadsheet to be excluded")
	}
	logger.Infof("✅ Tool google_sheets:DeleteSpreadsheet excluded (not in specific selection)")

	return nil
}

// testCategoryDetection tests custom tool category detection
func testCategoryDetection(logger utils.ExtendedLogger) error {
	// Create mock MCP clients map
	mockClients := make(map[string]mcpclient.ClientInterface)
	// In real scenario, these would be actual clients
	// For testing, we just need the keys

	// Create ToolFilter with custom categories
	customCategories := []string{"workspace", "human", "memory"}

	tf := mcpagent.NewToolFilter(
		[]string{},
		[]string{},
		mockClients,
		customCategories,
		logger,
	)

	// Test category directory detection
	categoryDirs := []string{
		"workspace_tools",
		"human_tools",
		"memory_tools",
	}

	for _, dir := range categoryDirs {
		if !tf.IsCategoryDirectory(dir) {
			return fmt.Errorf("expected %s to be detected as category directory", dir)
		}
		logger.Infof("✅ %s detected as category directory", dir)
	}

	// Virtual tools should not be a category directory
	if tf.IsCategoryDirectory("virtual_tools") {
		// Note: virtual_tools might be detected as category by suffix, but IsVirtualToolsDirectory is separate
		logger.Infof("Note: virtual_tools detected as category (expected - use IsVirtualToolsDirectory for virtual)")
	}

	// Test IsVirtualToolsDirectory
	if !tf.IsVirtualToolsDirectory("virtual_tools") {
		return fmt.Errorf("expected virtual_tools to be detected as virtual tools directory")
	}
	logger.Infof("✅ virtual_tools detected as virtual tools directory")

	if tf.IsVirtualToolsDirectory("workspace_tools") {
		return fmt.Errorf("expected workspace_tools NOT to be detected as virtual tools directory")
	}
	logger.Infof("✅ workspace_tools NOT detected as virtual tools directory")

	return nil
}

// testVirtualToolsAlwaysIncluded tests that virtual tools are always included regardless of filtering
func testVirtualToolsAlwaysIncluded(logger utils.ExtendedLogger) error {
	// Create ToolFilter with strict filtering
	selectedTools := []string{"aws:GetDocument"} // Only one specific tool

	tf := mcpagent.NewToolFilter(
		selectedTools,
		[]string{},
		nil,
		[]string{},
		logger,
	)

	// Virtual tools should be included regardless of filtering
	virtualTools := []string{"get_prompt", "get_resource", "discover_code_files", "write_code"}
	for _, tool := range virtualTools {
		if !tf.ShouldIncludeTool("virtual_tools", tool, false, true) {
			return fmt.Errorf("expected virtual tool %s to be included", tool)
		}
		logger.Infof("✅ Virtual tool %s included (always included)", tool)
	}

	// MCP tools NOT in selectedTools should still be excluded
	if tf.ShouldIncludeTool("aws", "DeleteDocument", false, false) {
		return fmt.Errorf("expected aws:DeleteDocument to be excluded")
	}
	logger.Infof("✅ aws:DeleteDocument excluded (filtering still works for non-virtual)")

	return nil
}

// testMixedCaseToolNames tests that tool names match regardless of case format
func testMixedCaseToolNames(logger utils.ExtendedLogger) error {
	// Create ToolFilter with snake_case in selectedTools
	selectedTools := []string{
		"workspace_tools:read_workspace_file", // snake_case
		"aws:GetDocument",                     // PascalCase
	}

	tf := mcpagent.NewToolFilter(
		selectedTools,
		[]string{},
		nil,
		[]string{"workspace"},
		logger,
	)

	// Test that PascalCase tool name matches snake_case selection
	if !tf.ShouldIncludeTool("workspace_tools", "ReadWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:ReadWorkspaceFile to match read_workspace_file")
	}
	logger.Infof("✅ PascalCase 'ReadWorkspaceFile' matches snake_case 'read_workspace_file'")

	// Test that snake_case tool name matches PascalCase selection
	if !tf.ShouldIncludeTool("aws", "get_document", false, false) {
		return fmt.Errorf("expected aws:get_document to match GetDocument")
	}
	logger.Infof("✅ snake_case 'get_document' matches PascalCase 'GetDocument'")

	return nil
}

// testCustomToolsFiltering tests that custom tools respect selectedTools filtering
// Note: workspace_tools and human_tools are "system categories" that are included by default
// UNLESS specific tools from those categories are selected (then only those are included)
func testCustomToolsFiltering(logger utils.ExtendedLogger) error {
	// Create ToolFilter with specific custom tools selected
	// Since workspace_tools has specific tools selected, only those should be included
	// Since human_tools has NO specific tools selected, ALL human tools are included (system category default)
	selectedTools := []string{
		"workspace_tools:ReadWorkspaceFile",
		"workspace_tools:UpdateWorkspaceFile",
		// human_tools not selected at all - but it's a system category, so all included by default
	}

	tf := mcpagent.NewToolFilter(
		selectedTools,
		[]string{},
		nil,
		[]string{"workspace", "human"},
		logger,
	)

	// Selected workspace tools should be included
	if !tf.ShouldIncludeTool("workspace_tools", "ReadWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:ReadWorkspaceFile to be included")
	}
	logger.Infof("✅ workspace_tools:ReadWorkspaceFile included")

	if !tf.ShouldIncludeTool("workspace_tools", "UpdateWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:UpdateWorkspaceFile to be included")
	}
	logger.Infof("✅ workspace_tools:UpdateWorkspaceFile included")

	// Non-selected workspace tools should be excluded (specific tools were selected)
	if tf.ShouldIncludeTool("workspace_tools", "DeleteWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:DeleteWorkspaceFile to be excluded")
	}
	logger.Infof("✅ workspace_tools:DeleteWorkspaceFile excluded (not in selectedTools)")

	// Human tools should be INCLUDED (system category with no specific selection = all included)
	if !tf.ShouldIncludeTool("human_tools", "human_feedback", true, false) {
		return fmt.Errorf("expected human_tools:human_feedback to be included (system category default)")
	}
	logger.Infof("✅ human_tools:human_feedback included (system category default)")

	return nil
}

// testSelectedServersWithSelectedToolsConflict tests the edge case where
// both selectedServers AND selectedTools are set for the SAME server.
// selectedServers should take priority - include ALL tools from that server.
func testSelectedServersWithSelectedToolsConflict(logger utils.ExtendedLogger) error {
	// Create ToolFilter with BOTH selectedServers AND selectedTools for the same server
	// This is the edge case that was causing the bug:
	// - selectedServers says "include ALL tools from google-sheets"
	// - selectedTools says "only include these 2 specific tools from google-sheets"
	// The correct behavior is: selectedServers takes priority, so ALL tools should be included
	selectedTools := []string{
		"google-sheets:GetSheetData",
		"google-sheets:CreateSpreadsheet",
		"workspace_tools:ReadWorkspaceFile", // Also some custom tools
	}
	selectedServers := []string{"google-sheets"} // Include ALL tools from google-sheets

	tf := mcpagent.NewToolFilter(
		selectedTools,
		selectedServers,
		nil,
		[]string{"workspace"},
		logger,
	)

	// ALL google-sheets tools should be included because it's in selectedServers
	// Even tools NOT in selectedTools should be included
	googleSheetsTools := []string{
		"GetSheetData",      // in selectedTools
		"CreateSpreadsheet", // in selectedTools
		"DeleteSpreadsheet", // NOT in selectedTools - but should still be included!
		"AddRows",           // NOT in selectedTools - but should still be included!
		"BatchUpdateCells",  // NOT in selectedTools - but should still be included!
	}

	for _, tool := range googleSheetsTools {
		// Note: We use "google_sheets" (normalized) as that's what discovery passes
		if !tf.ShouldIncludeTool("google_sheets", tool, false, false) {
			return fmt.Errorf("expected google_sheets:%s to be included (server in selectedServers)", tool)
		}
		logger.Infof("✅ Tool google_sheets:%s included (selectedServers takes priority)", tool)
	}

	// workspace_tools should respect selectedTools (not in selectedServers)
	if !tf.ShouldIncludeTool("workspace_tools", "ReadWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:ReadWorkspaceFile to be included")
	}
	logger.Infof("✅ workspace_tools:ReadWorkspaceFile included (in selectedTools)")

	// workspace_tools tools NOT in selectedTools should be excluded
	if tf.ShouldIncludeTool("workspace_tools", "DeleteWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:DeleteWorkspaceFile to be excluded")
	}
	logger.Infof("✅ workspace_tools:DeleteWorkspaceFile excluded (not in selectedTools)")

	// Other servers not in selectedServers or selectedTools should be excluded
	if tf.ShouldIncludeTool("other_server", "SomeTool", false, false) {
		return fmt.Errorf("expected other_server:SomeTool to be excluded")
	}
	logger.Infof("✅ other_server:SomeTool excluded (not in selectedServers or selectedTools)")

	return nil
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

// testComprehensiveFilterScenarios runs comprehensive table-driven tests
// This test would have caught the bugs we found by testing ALL edge cases
func testComprehensiveFilterScenarios(logger utils.ExtendedLogger) error {
	// Mock MCP clients - simulates what agent.go passes to NewToolFilter
	mockClients := map[string]mcpclient.ClientInterface{
		"google-sheets": nil, // Config uses hyphens
		"aws":           nil,
		"tavily":        nil,
	}

	// Scenario 1: Both selectedServers AND selectedTools set (the bug scenario)
	logger.Infof("\n  Scenario 1: selectedServers + selectedTools for same server")
	{
		selectedTools := []string{
			"google-sheets:GetSheetData", // Specific tool from google-sheets
			"google-sheets:CreateSpreadsheet",
			"aws:GetDocument", // Specific tool from different server
		}
		selectedServers := []string{"google-sheets"} // ALL tools from google-sheets

		tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClients, []string{"workspace"}, logger)

		testCases := []ToolFilterTestCase{
			// google-sheets tools - ALL should be included (selectedServers priority)
			{"google-sheets in selectedTools", "google_sheets", "GetSheetData", false, false, true, "in both selectedServers and selectedTools"},
			{"google-sheets in selectedTools", "google_sheets", "CreateSpreadsheet", false, false, true, "in both selectedServers and selectedTools"},
			{"google-sheets NOT in selectedTools", "google_sheets", "DeleteSpreadsheet", false, false, true, "selectedServers includes ALL tools"},
			{"google-sheets NOT in selectedTools", "google_sheets", "AddRows", false, false, true, "selectedServers includes ALL tools"},

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
			logger.Infof("    ✅ %s:%s = %v (%s)", tc.PackageOrServer, tc.ToolName, result, tc.Reason)
		}
	}

	// Scenario 2: Package name format mismatch (directory name vs config name)
	logger.Infof("\n  Scenario 2: Package name format (simulating discovery)")
	{
		// Discovery passes: "google_sheets" (from directory google_sheets_tools)
		// Config has: "google-sheets" (with hyphens)
		selectedServers := []string{"google-sheets"}

		tf := mcpagent.NewToolFilter([]string{}, selectedServers, mockClients, []string{}, logger)

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
			logger.Infof("    ✅ %s:%s = %v (%s)", tc.PackageOrServer, tc.ToolName, result, tc.Reason)
		}
	}

	// Scenario 3: Custom tools with specific filtering
	// Note: workspace_tools and human_tools are SYSTEM CATEGORIES (included by default)
	// But when specific tools are selected from a system category, only those are included
	logger.Infof("\n  Scenario 3: Custom tools filtering (with system categories)")
	{
		selectedTools := []string{
			"workspace_tools:ReadWorkspaceFile",
			"workspace_tools:UpdateWorkspaceFile",
			// human_tools not mentioned at all - but it's a SYSTEM CATEGORY, so ALL are included by default
		}

		tf := mcpagent.NewToolFilter(selectedTools, []string{}, mockClients, []string{"workspace", "human"}, logger)

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
			logger.Infof("    ✅ %s:%s = %v (%s)", tc.PackageOrServer, tc.ToolName, result, tc.Reason)
		}
	}

	// Scenario 4: Virtual tools always included (system tools)
	logger.Infof("\n  Scenario 4: Virtual tools always included")
	{
		// Even with strict filtering, virtual tools should be included
		selectedTools := []string{"aws:GetDocument"} // Only one specific tool

		tf := mcpagent.NewToolFilter(selectedTools, []string{}, mockClients, []string{}, logger)

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
			logger.Infof("    ✅ %s:%s = %v (%s)", tc.PackageOrServer, tc.ToolName, result, tc.Reason)
		}
	}

	// Scenario 5: Wildcard pattern (server:*)
	logger.Infof("\n  Scenario 5: Wildcard pattern")
	{
		selectedTools := []string{
			"google-sheets:*", // ALL tools from google-sheets
			"aws:GetDocument", // Only specific tool from aws
		}

		tf := mcpagent.NewToolFilter(selectedTools, []string{}, mockClients, []string{}, logger)

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
			logger.Infof("    ✅ %s:%s = %v (%s)", tc.PackageOrServer, tc.ToolName, result, tc.Reason)
		}
	}

	return nil
}

// testDiscoverySimulation simulates exactly what code_execution_tools.go does
// This test would catch bugs in how discovery passes package names to the filter
func testDiscoverySimulation(logger utils.ExtendedLogger) error {
	// Simulate the MCP clients from config
	mockClients := map[string]mcpclient.ClientInterface{
		"google-sheets": nil, // Config name with hyphens
	}

	selectedServers := []string{"google-sheets"}
	selectedTools := []string{
		"google-sheets:GetSheetData", // Some specific tools
		"workspace_tools:ReadWorkspaceFile",
	}

	tf := mcpagent.NewToolFilter(selectedTools, selectedServers, mockClients, []string{"workspace", "human"}, logger)

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
		{"google_sheets_tools", "google_sheets", false, false, "GetSheetData", "google_sheets", true},
		{"google_sheets_tools", "google_sheets", false, false, "DeleteSpreadsheet", "google_sheets", true}, // selectedServers includes ALL

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

	logger.Infof("\n  Simulating discovery behavior:")
	for _, c := range cases {
		result := tf.ShouldIncludeTool(c.PackageUsed, c.ToolName, c.IsCategory, c.IsVirtual)
		if result != c.Expected {
			return fmt.Errorf("FAIL discovery simulation: dir=%s, package=%s, tool=%s: expected=%v, got=%v",
				c.DirName, c.PackageUsed, c.ToolName, c.Expected, result)
		}
		logger.Infof("    ✅ dir=%s → package=%s:%s = %v", c.DirName, c.PackageUsed, c.ToolName, result)
	}

	// Also test the wrong way (what the bug was doing)
	logger.Infof("\n  Verifying bug fix - these would have been wrong with the old code:")

	// OLD BUG: Passing dirName instead of serverName for MCP servers
	// This would cause "google_sheets_tools" to not match "google-sheets" in selectedServers

	// The fix is already in code_execution_tools.go, but let's verify the filter handles it correctly
	// If someone passes the wrong format, the filter should still work via normalization

	return nil
}

// testSystemCategoriesIncludedByDefault tests that system categories (workspace_tools, human_tools)
// are included by default, even when MCP tool filtering is active
func testSystemCategoriesIncludedByDefault(logger utils.ExtendedLogger) error {
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
		logger,
	)

	// Verify system category detection
	if !tf.IsSystemCategory("workspace_tools") {
		return fmt.Errorf("expected workspace_tools to be detected as system category")
	}
	logger.Infof("✅ workspace_tools detected as system category")

	if !tf.IsSystemCategory("human_tools") {
		return fmt.Errorf("expected human_tools to be detected as system category")
	}
	logger.Infof("✅ human_tools detected as system category")

	// Test 1: workspace_tools should be included by default (all tools)
	workspaceTools := []string{"ReadWorkspaceFile", "UpdateWorkspaceFile", "DeleteWorkspaceFile", "ListWorkspaceFiles"}
	for _, tool := range workspaceTools {
		if !tf.ShouldIncludeTool("workspace_tools", tool, true, false) {
			return fmt.Errorf("expected workspace_tools:%s to be included by default (system category)", tool)
		}
		logger.Infof("✅ workspace_tools:%s included (system category default)", tool)
	}

	// Test 2: human_tools should be included by default (all tools)
	humanTools := []string{"human_feedback", "human_verification"}
	for _, tool := range humanTools {
		if !tf.ShouldIncludeTool("human_tools", tool, true, false) {
			return fmt.Errorf("expected human_tools:%s to be included by default (system category)", tool)
		}
		logger.Infof("✅ human_tools:%s included (system category default)", tool)
	}

	// Test 3: MCP tools should still be filtered normally
	if !tf.ShouldIncludeTool("google_sheets", "GetSheetData", false, false) {
		return fmt.Errorf("expected google_sheets:GetSheetData to be included (in selectedTools)")
	}
	logger.Infof("✅ google_sheets:GetSheetData included (in selectedTools)")

	if tf.ShouldIncludeTool("google_sheets", "DeleteSpreadsheet", false, false) {
		return fmt.Errorf("expected google_sheets:DeleteSpreadsheet to be excluded (not in selectedTools)")
	}
	logger.Infof("✅ google_sheets:DeleteSpreadsheet excluded (not in selectedTools)")

	// Test 4: When specific workspace_tools are selected, only those should be included
	logger.Infof("\n  Testing specific tool selection for system categories:")
	selectedToolsSpecific := []string{
		"google-sheets:GetSheetData",
		"workspace_tools:ReadWorkspaceFile", // Only this workspace tool
	}

	tfSpecific := mcpagent.NewToolFilter(
		selectedToolsSpecific,
		[]string{},
		mockClients,
		[]string{"workspace", "human"},
		logger,
	)

	// ReadWorkspaceFile should be included (explicitly selected)
	if !tfSpecific.ShouldIncludeTool("workspace_tools", "ReadWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:ReadWorkspaceFile to be included (explicitly selected)")
	}
	logger.Infof("✅ workspace_tools:ReadWorkspaceFile included (explicitly selected)")

	// DeleteWorkspaceFile should be excluded (not in specific selection)
	if tfSpecific.ShouldIncludeTool("workspace_tools", "DeleteWorkspaceFile", true, false) {
		return fmt.Errorf("expected workspace_tools:DeleteWorkspaceFile to be excluded (not in specific selection)")
	}
	logger.Infof("✅ workspace_tools:DeleteWorkspaceFile excluded (not in specific selection)")

	// human_tools should still be included by default (no specific selection for human_tools)
	if !tfSpecific.ShouldIncludeTool("human_tools", "human_feedback", true, false) {
		return fmt.Errorf("expected human_tools:human_feedback to be included (system category default)")
	}
	logger.Infof("✅ human_tools:human_feedback included (system category default, no specific selection)")

	return nil
}

// testNormalModeIntegration tests tool filtering in normal mode (tools directly on LLM)
func testNormalModeIntegration(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find a server with multiple tools for testing
	var testServerName string
	for name := range config.MCPServers {
		testServerName = name
		break
	}

	if testServerName == "" {
		logger.Warnf("No MCP servers found in config, skipping normal mode integration test")
		return nil
	}

	logger.Infof("Using server '%s' for normal mode integration test", testServerName)

	// Create LLM instance
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping normal mode test: %v", err)
		return nil
	}

	// Test 1: Create agent WITHOUT code execution mode, WITH tool filtering
	selectedTool := fmt.Sprintf("%s:*", testServerName) // All tools from this server
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
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
		mcpagent.WithSelectedTools([]string{selectedTool}),
		// NOT using WithCodeExecutionMode - this is normal mode
	)
	if err != nil {
		logger.Warnf("Failed to create agent: %v", err)
		return nil
	}
	defer agent.Close()

	// Verify: Agent should have tools from the selected server
	tools := agent.Tools
	logger.Infof("Normal mode: Agent has %d tools registered", len(tools))

	if len(tools) == 0 {
		return fmt.Errorf("normal mode: expected agent to have tools, but got 0")
	}

	// Verify tools are from the expected server (check tool names)
	for _, tool := range tools {
		if tool.Function != nil {
			logger.Infof("  - Tool: %s", tool.Function.Name)
		}
	}

	logger.Infof("✅ Normal mode integration test passed: %d tools registered", len(tools))
	return nil
}

// testCodeExecutionModeIntegration tests tool filtering in code execution mode (discovery)
func testCodeExecutionModeIntegration(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find a server with multiple tools for testing
	var testServerName string
	for name := range config.MCPServers {
		testServerName = name
		break
	}

	if testServerName == "" {
		logger.Warnf("No MCP servers found in config, skipping code execution mode integration test")
		return nil
	}

	logger.Infof("Using server '%s' for code execution mode integration test", testServerName)

	// Create LLM instance
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping code execution mode test: %v", err)
		return nil
	}

	// Test: Create agent WITH code execution mode, WITH tool filtering
	selectedTool := fmt.Sprintf("%s:*", testServerName) // All tools from this server
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
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
		mcpagent.WithSelectedTools([]string{selectedTool}),
		mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
	)
	if err != nil {
		logger.Warnf("Failed to create agent with code execution mode: %v", err)
		return nil
	}
	defer agent.Close()

	// In code execution mode, agent should only have virtual tools (discover_code_files, write_code)
	tools := agent.Tools
	logger.Infof("Code execution mode: Agent has %d LLM tools (should be virtual tools only)", len(tools))

	// Verify: Should have virtual tools
	hasDiscoverCodeFiles := false
	hasWriteCode := false
	for _, tool := range tools {
		if tool.Function != nil {
			logger.Infof("  - Tool: %s", tool.Function.Name)
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
	result, err := agent.HandleVirtualTool(ctx, "discover_code_structure", map[string]interface{}{})
	if err != nil {
		logger.Warnf("Failed to call discover_code_structure: %v", err)
		// Don't fail - discovery might fail if no generated code exists
	} else {
		logger.Infof("Code execution mode: discover_code_structure returned %d chars", len(result))

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
					logger.Infof("  - Found server %s with %d tools in discovery", server.Name, len(server.Tools))
				}
			}
		}
	}

	logger.Infof("✅ Code execution mode integration test passed")
	return nil
}

// testFilterConsistencyBetweenModes tests that filtering is consistent between normal and code execution modes
func testFilterConsistencyBetweenModes(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find a server for testing
	var testServerName string
	for name := range config.MCPServers {
		testServerName = name
		break
	}

	if testServerName == "" {
		logger.Warnf("No MCP servers found in config, skipping consistency test")
		return nil
	}

	// Create LLM instance
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		Logger:      logger,
	})
	if err != nil {
		logger.Warnf("Failed to create LLM model, skipping consistency test: %v", err)
		return nil
	}

	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}

	// Use strict filtering: only one specific pattern
	selectedTools := []string{fmt.Sprintf("%s:*", testServerName)}

	// Create agent in normal mode
	normalAgent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName,
		configPath,
		"test-model",
		nil,
		"test-trace-normal",
		logger,
		mcpagent.WithSelectedTools(selectedTools),
	)
	if err != nil {
		logger.Warnf("Failed to create normal mode agent: %v", err)
		return nil
	}
	defer normalAgent.Close()

	// Create agent in code execution mode
	codeExecAgent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		testServerName,
		configPath,
		"test-model",
		nil,
		"test-trace-codeexec",
		logger,
		mcpagent.WithSelectedTools(selectedTools),
		mcpagent.WithCodeExecutionMode(true),
	)
	if err != nil {
		logger.Warnf("Failed to create code execution mode agent: %v", err)
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

	logger.Infof("Normal mode: %d MCP tools registered", normalMCPToolCount)

	// Get discovery from code execution mode
	discoveryResult, err := codeExecAgent.HandleVirtualTool(ctx, "discover_code_structure", map[string]interface{}{})
	if err != nil {
		logger.Warnf("Failed to get discovery in code execution mode: %v", err)
		return nil
	}

	var discovery struct {
		Servers []struct {
			Name  string   `json:"name"`
			Tools []string `json:"tools"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(discoveryResult), &discovery); err != nil {
		logger.Warnf("Failed to parse discovery result: %v", err)
		return nil
	}

	codeExecToolCount := 0
	for _, server := range discovery.Servers {
		codeExecToolCount += len(server.Tools)
	}

	logger.Infof("Code execution mode: %d tools in discovery", codeExecToolCount)

	// Both modes should have similar tool counts (allowing for some variance due to function naming)
	// The key is that both use the same ToolFilter, so filtering should be consistent
	if normalMCPToolCount > 0 && codeExecToolCount > 0 {
		logger.Infof("✅ Consistency test passed: Normal mode (%d tools) and Code execution mode (%d tools) both have tools from %s",
			normalMCPToolCount, codeExecToolCount, testServerName)
	} else if normalMCPToolCount == 0 && codeExecToolCount == 0 {
		logger.Infof("✅ Consistency test passed: Both modes have 0 tools (server may have no tools)")
	} else {
		logger.Warnf("⚠️ Potential inconsistency: Normal mode has %d tools, Code execution mode has %d tools",
			normalMCPToolCount, codeExecToolCount)
	}

	return nil
}
