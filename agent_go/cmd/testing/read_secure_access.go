package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	"mcpagent/llm"
	"mcp-agent/agent_go/pkg/external"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var readSecureAccessTestCmd = &cobra.Command{
	Use:   "read-secure-access",
	Short: "Test read_image tool with Downloads/secure_access_check.png",
	Long: `Test the read_image tool specifically with Downloads/secure_access_check.png.

This test:
1. Creates an agent with workspace tools (including read_image)
2. Invokes the agent to read Downloads/secure_access_check.png
3. Shows the full conversation flow, tool calls, and LLM responses
4. Helps debug the read_image tool integration

Example:
  orchestrator test read-secure-access --provider bedrock
  orchestrator test read-secure-access --provider vertex`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load .env file if present
		if err := godotenv.Load("agent_go/.env"); err == nil {
			// Environment loaded successfully
		} else if err := godotenv.Load(".env"); err == nil {
			// Environment loaded successfully
		} else if err := godotenv.Load("../.env"); err == nil {
			// Environment loaded successfully
		}

		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Read Secure Access Check Image Test ===")

		// Hardcoded image path
		imagePath := "Downloads/secure_access_check.png"
		logger.Infof("Testing with image: %s", imagePath)

		// Get provider and model from viper or use defaults
		provider := viper.GetString("test.provider")
		if provider == "" {
			provider = "bedrock"
		}
		model := viper.GetString("model")
		if model == "" {
			if provider == "openai" {
				model = "gpt-4o-mini"
			} else if provider == "bedrock" {
				model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
			} else if provider == "vertex" {
				model = "claude-sonnet-4-5"
			} else {
				model = "claude-haiku-4-5-20251001"
			}
		}

		logger.Infof("Provider: %s, Model: %s", provider, model)

		// Convert provider string to llm.Provider type
		llmProvider := llm.Provider(provider)

		// Create agent configuration
		agentConfig := external.DefaultConfig().
			WithAgentMode(external.SimpleAgent).
			WithServer("fileserver", "configs/mcp_servers_simple.json").
			WithLLM(llmProvider, model, 0.7).
			WithMaxTurns(10).
			WithLogger(logger)

		logger.Infof("Agent configuration created - provider: %s, model: %s", provider, model)

		// Create the agent
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		agent, err := external.NewAgent(ctx, agentConfig)
		if err != nil {
			return fmt.Errorf("failed to create agent: %w", err)
		}
		defer agent.Close()

		logger.Infof("✅ Agent created successfully")

		// Register workspace tools (including read_image)
		workspaceTools := virtualtools.CreateWorkspaceTools()
		workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()

		logger.Infof("Registering %d workspace tools", len(workspaceTools))

		for _, tool := range workspaceTools {
			toolName := tool.Function.Name
			if executor, exists := workspaceExecutors[toolName]; exists {
				// Convert Parameters to map[string]interface{}
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					logger.Warnf("Warning: Failed to convert parameters for tool %s", toolName)
					continue
				}

				// Register the tool
				agent.RegisterCustomTool(
					toolName,
					tool.Function.Description,
					params,
					executor,
				)
				logger.Infof("✅ Registered workspace tool: %s", toolName)
			}
		}

		logger.Infof("✅ All workspace tools registered")

		// Test read_image tool with the specific image
		prompt := fmt.Sprintf("Please read the file '%s' and describe what you see in it.", imagePath)

		logger.Infof("📝 Invoking agent with prompt: %s", prompt)
		logger.Infof("🔍 This should trigger the read_image tool")

		// Invoke the agent
		response, err := agent.Invoke(ctx, prompt)
		if err != nil {
			logger.Errorf("❌ Read secure access image test failed: %v", err)
			return fmt.Errorf("read secure access image test failed: %w", err)
		}

		logger.Infof("✅ Read secure access image test successful")
		logger.Infof("Response length: %d characters", len(response))

		// Show response
		fmt.Printf("\n" + strings.Repeat("=", 80) + "\n")
		fmt.Printf("🖼️  Image: %s\n", imagePath)
		fmt.Printf("📝 LLM Response:\n")
		fmt.Printf(strings.Repeat("-", 80) + "\n")
		fmt.Printf("%s\n", response)
		fmt.Printf(strings.Repeat("=", 80) + "\n")

		// Show agent capabilities
		capabilities := agent.GetCapabilities()
		fmt.Printf("\n📊 Agent Capabilities:\n%s\n", capabilities)

		logger.Infof("✅ Read secure access image test completed successfully")
		fmt.Println("\n🎉 Read secure access image test completed successfully!")

		return nil
	},
}

