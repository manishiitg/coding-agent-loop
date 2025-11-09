package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/pkg/external"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var readImageTestCmd = &cobra.Command{
	Use:   "read-image",
	Short: "Test read_image tool with workspace images",
	Long: `Test the read_image tool that reads images from workspace and processes them with LLM.

This test:
1. Creates an agent with workspace tools (including read_image)
2. Tests read_image tool with a specific image file
3. Verifies that the image is processed correctly and sent to LLM`,
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

		logger.Infof("=== Read Image Tool Test ===")

		// Get provider and model from viper or use defaults
		// The provider flag is bound as "test.provider" in testing.go
		provider := viper.GetString("test.provider")
		if provider == "" {
			provider = "openai"
		}
		model := viper.GetString("model")
		if model == "" {
			if provider == "openai" {
				model = "gpt-4o-mini"
			} else if provider == "bedrock" {
				model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
			} else {
				model = "claude-haiku-4-5-20251001"
			}
		}

		// Get image path from flag or use default
		// This should be the workspace-relative path (e.g., "Downloads/hdfc_after_password_attempt_1.png")
		imagePath := viper.GetString("image-path")
		if imagePath == "" {
			// Default to Downloads/hdfc_after_password_attempt_1.png (workspace-relative path)
			imagePath = "Downloads/hdfc_after_password_attempt_1.png"
		}

		logger.Infof("Using workspace image path: %s", imagePath)

		// Convert provider string to llm.Provider type
		llmProvider := llm.Provider(provider)

		logger.Infof("Provider string: '%s', Provider type: '%s'", provider, string(llmProvider))

		// Create agent configuration
		agentConfig := external.DefaultConfig().
			WithAgentMode(external.SimpleAgent).
			WithServer("fileserver", "configs/mcp_servers_simple.json").
			WithLLM(llmProvider, model, 0.7).
			WithMaxTurns(10).
			WithLogger(logger)

		logger.Infof("Agent configuration created - provider: %s, model: %s, image: %s", provider, model, imagePath)

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

		// Test read_image tool
		// Use the workspace-relative path directly (e.g., "Downloads/hdfc_after_password_attempt_1.png")
		// The agent should automatically detect it's an image and use read_image tool
		prompt := fmt.Sprintf("Please read the file '%s' and describe what you see in it.", imagePath)

		logger.Infof("Testing read_image tool - image_file: %s", imagePath)

		// Invoke the agent
		response, err := agent.Invoke(ctx, prompt)
		if err != nil {
			logger.Errorf("❌ Read image test failed: %v", err)
			return fmt.Errorf("read image test failed: %w", err)
		}

		logger.Infof("✅ Read image test successful")
		logger.Infof("Response length: %d characters", len(response))

		// Show response
		fmt.Printf("\n🖼️ Image: %s\n", imagePath)
		fmt.Printf("📝 Response: %s\n", response)

		// Show agent capabilities
		capabilities := agent.GetCapabilities()
		fmt.Printf("\n📊 Agent Capabilities:\n%s\n", capabilities)

		logger.Infof("✅ Read image test completed successfully")
		fmt.Println("\n🎉 Read image test completed successfully!")

		return nil
	},
}

func init() {
	readImageTestCmd.Flags().String("image-path", "", "Path to image file to test (default: Downloads/hdfc_after_password_attempt_1.png)")
	viper.BindPFlag("image-path", readImageTestCmd.Flags().Lookup("image-path"))
}
