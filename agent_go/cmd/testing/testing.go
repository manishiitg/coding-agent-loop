package testing

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestingCmd represents the testing command group
var TestingCmd = &cobra.Command{
	Use:   "test",
	Short: "Testing framework for MCP Agent with comprehensive validation",
	Long: `Testing framework for MCP Agent with comprehensive validation.

Features:
- Agent conversation testing (with all LLM providers)
- MCP server connection testing
- SSE streaming testing
- Langfuse trace retrieval
- Connection pooling validation
- Context cancellation testing

Note: For comprehensive LLM provider testing (tool calls, structured output, 
streaming, embeddings, etc.), use the llm-providers test suite:
  cd llm-providers && ./bin/llm-test --help

Examples:
  # Test agent with different providers
  orchestrator test agent --simple --provider bedrock
  orchestrator test agent --simple --provider openai  
  orchestrator test agent --simple --provider anthropic
  orchestrator test agent --simple --provider openrouter

  # Test with specific config
  orchestrator test agent --simple --provider openrouter --config configs/mcp_servers_simple.json

  # Comprehensive testing
  orchestrator test agent --comprehensive-aws --provider bedrock
  orchestrator test agent --complex --provider openai
  
  # Simple comprehensive testing
  orchestrator test comprehensive-simple --provider openrouter
  orchestrator test comprehensive-simple --provider bedrock --verbose
  
  # Max tokens flexibility testing
  orchestrator test max-tokens-flexibility --provider bedrock --verbose
  
  # Context cancellation testing
  orchestrator test context-cancellation --provider bedrock --log-file logs/context-cancellation.log`,
}

// Common flags for all testing commands
var (
	verbose    bool
	showOutput bool
	timeout    string
	provider   string
	config     string
	// Remove duplicate logFile, logLevel, logFormat variables - let them inherit from root
)

func init() {
	// Don't initialize logger here - let individual commands handle it
	// The logger will be initialized in each test command based on the log-file parameter

	// Add common flags for all testing commands
	TestingCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose test output")
	TestingCmd.PersistentFlags().BoolVar(&showOutput, "show-output", true, "show detailed test output")
	TestingCmd.PersistentFlags().StringVar(&timeout, "timeout", "5m", "test timeout duration")
	TestingCmd.PersistentFlags().StringVar(&provider, "provider", "bedrock", "LLM provider for tests")
	TestingCmd.PersistentFlags().StringVar(&config, "config", "mcp.yaml", "MCP config file to use for tests")

	// Remove duplicate logging flag definitions - let them inherit from root command
	// The root command already defines and binds these flags:
	// --log-file, --log-level, --log-format, --test.log-file

	// Bind to viper for configuration
	viper.BindPFlag("test.verbose", TestingCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("test.show-output", TestingCmd.PersistentFlags().Lookup("show-output"))
	viper.BindPFlag("test.timeout", TestingCmd.PersistentFlags().Lookup("timeout"))
	viper.BindPFlag("test.provider", TestingCmd.PersistentFlags().Lookup("provider"))
	viper.BindPFlag("config", TestingCmd.PersistentFlags().Lookup("config"))
	// Remove duplicate viper bindings for logging flags - they're already bound in root command

	// Initialize all subcommands
	initTestingCommands()
}

// initTestingCommands initializes all testing subcommands
func initTestingCommands() {
	// Don't initialize logger here - let individual commands handle it
	// The logger will be initialized in each test command based on the log-file parameter

	// Add subcommands explicitly to ensure they're registered
	// Note: LLM provider tests (anthropic, bedrock, openai, vertex, etc.) are now in llm-providers/
	// Use ./bin/llm-test for comprehensive provider testing
	TestingCmd.AddCommand(agentCmd)
	TestingCmd.AddCommand(comprehensiveSimpleCmd)
	TestingCmd.AddCommand(langfuseCmd)
	TestingCmd.AddCommand(awsTestCmd)
	TestingCmd.AddCommand(mcpCacheTestCmd) // MCP Connection Caching Test
	TestingCmd.AddCommand(exaTestCmd)
	TestingCmd.AddCommand(sseCmd)
	// TestingCmd.AddCommand(structuredOutputTestCmd) // Removed - replaced by agentStructuredOutputTestCmd
	TestingCmd.AddCommand(agentStructuredOutputTestCmd)
	TestingCmd.AddCommand(maxTokensFlexibilityCmd)
	TestingCmd.AddCommand(openaiEmptyParamsTestCmd) // Agent/MCP integration test for empty parameter schemas
	TestingCmd.AddCommand(genaiMultiTurnToolTestCmd)
	TestingCmd.AddCommand(genaiMultiToolComplexTestCmd)
	TestingCmd.AddCommand(debugExternalCmd)
	TestingCmd.AddCommand(customToolsTestCmd)
	TestingCmd.AddCommand(streamingTracerCmd)
	TestingCmd.AddCommand(contextCancellationTestCmd)
	TestingCmd.AddCommand(bufioScannerBugTestCmd)
	TestingCmd.AddCommand(codegenTestCmd)
	TestingCmd.AddCommand(codeExecutionTestCmd)
	TestingCmd.AddCommand(readImageTestCmd)
	TestingCmd.AddCommand(readSecureAccessTestCmd)
	TestingCmd.AddCommand(toolFilterTestCmd) // Unified ToolFilter test
}
