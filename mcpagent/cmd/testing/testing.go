package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	agentlangfuse "mcpagent/cmd/testing/agent-langfuse"
	agentmcp "mcpagent/cmd/testing/agent-mcp"
	executortest "mcpagent/cmd/testing/executor"
	humanfeedbackcodeexec "mcpagent/cmd/testing/human-feedback-code-exec"
	langfuse "mcpagent/cmd/testing/langfuse"
	largetooloutput "mcpagent/cmd/testing/large-tool-output"
	mcpagentcodeexec "mcpagent/cmd/testing/mcp-agent-code-exec"
	smartrouting "mcpagent/cmd/testing/smart-routing"
	"mcpagent/cmd/testing/structured-output/conversion"
	"mcpagent/cmd/testing/structured-output/tool"
	tokentracking "mcpagent/cmd/testing/token-tracking"
	toolfilter "mcpagent/cmd/testing/tool-filter"
)

// TestingCmd represents the testing command group
var TestingCmd = &cobra.Command{
	Use:   "test",
	Short: "Testing framework for MCP Agent",
	Long: `Testing framework for MCP Agent with comprehensive validation.

Features:
- Tool filter testing
- Agent functionality testing
- MCP integration testing

Examples:
  # Test tool filter
  mcpagent-test test tool-filter
  
  # Test with specific config
  mcpagent-test test tool-filter --config configs/mcp_servers_simple.json`,
}

// Common flags for all testing commands
var (
	verbose    bool
	showOutput bool
	timeout    string
	provider   string
	config     string
	logFile    string
	logLevel   string
)

func init() {
	// Add common flags for all testing commands
	TestingCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose test output")
	TestingCmd.PersistentFlags().BoolVar(&showOutput, "show-output", true, "show detailed test output")
	TestingCmd.PersistentFlags().StringVar(&timeout, "timeout", "5m", "test timeout duration")
	TestingCmd.PersistentFlags().StringVar(&provider, "provider", "openai", "LLM provider for tests")
	TestingCmd.PersistentFlags().StringVar(&config, "config", "", "MCP config file to use for tests")
	TestingCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path")
	TestingCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	// Bind to viper for configuration (with error handling)
	if err := viper.BindPFlag("test.verbose", TestingCmd.PersistentFlags().Lookup("verbose")); err != nil {
		// Log warning but don't fail - viper binding is not critical
	}
	if err := viper.BindPFlag("test.show-output", TestingCmd.PersistentFlags().Lookup("show-output")); err != nil {
		// Log warning but don't fail
	}
	if err := viper.BindPFlag("test.timeout", TestingCmd.PersistentFlags().Lookup("timeout")); err != nil {
		// Log warning but don't fail
	}
	if err := viper.BindPFlag("test.provider", TestingCmd.PersistentFlags().Lookup("provider")); err != nil {
		// Log warning but don't fail
	}
	if err := viper.BindPFlag("config", TestingCmd.PersistentFlags().Lookup("config")); err != nil {
		// Log warning but don't fail
	}
	if err := viper.BindPFlag("log-file", TestingCmd.PersistentFlags().Lookup("log-file")); err != nil {
		// Log warning but don't fail
	}
	if err := viper.BindPFlag("log-level", TestingCmd.PersistentFlags().Lookup("log-level")); err != nil {
		// Log warning but don't fail
	}

	// Initialize all subcommands
	initTestingCommands()
}

// initTestingCommands initializes all testing subcommands
func initTestingCommands() {
	TestingCmd.AddCommand(toolfilter.GetToolFilterTestCmd())
	TestingCmd.AddCommand(agentlangfuse.GetLangfuseTracerTestCmd())
	TestingCmd.AddCommand(agentmcp.GetAgentMCPTestCmd())
	TestingCmd.AddCommand(executortest.GetExecutorTestCmd())
	TestingCmd.AddCommand(mcpagentcodeexec.GetMCPAgentCodeExecTestCmd())
	TestingCmd.AddCommand(humanfeedbackcodeexec.GetHumanFeedbackCodeExecTestCmd())
	TestingCmd.AddCommand(langfuse.GetLangfuseReadTestCmd())
	TestingCmd.AddCommand(largetooloutput.GetLargeToolOutputTestCmd())
	TestingCmd.AddCommand(smartrouting.GetSmartRoutingTestCmd())
	TestingCmd.AddCommand(conversion.GetStructuredOutputConversionTestCmd())
	TestingCmd.AddCommand(tool.GetStructuredOutputToolTestCmd())
	TestingCmd.AddCommand(tokentracking.GetTokenTrackingTestCmd())
}

func main() {
	if err := TestingCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
