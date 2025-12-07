package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	mcpagent "mcpagent/agent"
	"mcpagent/llm"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// WeatherTool provides weather information for a given location
type WeatherTool struct {
	APIKey string
}

// WeatherRequest represents the input parameters for the weather tool
type WeatherRequest struct {
	Location string `json:"location"`
	Units    string `json:"units,omitempty"`
}

// Name returns the name of the tool
func (w *WeatherTool) Name() string {
	return "get_weather"
}

// Call executes the weather tool
func (w *WeatherTool) Call(ctx context.Context, input string) (string, error) {
	var req WeatherRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if req.Location == "" {
		return "", fmt.Errorf("location is required")
	}

	// Mock weather data for demo
	weatherData := map[string]interface{}{
		"location":    req.Location,
		"temperature": 22.5,
		"description": "partly cloudy",
		"humidity":    65,
		"wind_speed":  12.3,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	result, err := json.MarshalIndent(weatherData, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal weather data: %w", err)
	}

	return string(result), nil
}

var customToolsTestCmd = &cobra.Command{
	Use:   "custom-tools",
	Short: "Test custom tools integration with external agent",
	Long: `Test custom tools integration with external agent using the WithCustomTools functional option pattern.

This test demonstrates:
1. Creating custom tools (weather tool)
2. Using WithCustomTools in agent configuration
3. Verifying custom tools are available to the agent`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info(fmt.Sprintf("=== Custom Tools Test ==="))

		// Create custom weather tool
		weatherTool := &WeatherTool{APIKey: "demo-key"}

		// Initialize LLM
		openAIKey := os.Getenv("OPENAI_API_KEY")
		if openAIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY environment variable is not set")
		}

		llmModel, err := llm.InitializeLLM(llm.Config{
			Provider:    llm.ProviderOpenAI,
			ModelID:     "gpt-4.1",
			Temperature: 0.2,
			Logger:      nil, // Use default logger
			APIKeys: &llm.ProviderAPIKeys{
				OpenAI: &openAIKey,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to initialize LLM: %w", err)
		}

		logger.Info(fmt.Sprintf("Agent configuration created"))

		// Create the agent
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		agent, err := mcpagent.NewAgent(
			ctx,
			llmModel,
			"fileserver",                      // server name
			"configs/mcp_servers_simple.json", // config path
			"gpt-4.1",                         // model ID
			nil,                               // tracer
			"",                                // trace ID
			nil,                               // logger (use default)
			mcpagent.WithMaxTurns(5),
		)
		if err != nil {
			return fmt.Errorf("failed to create agent: %w", err)
		}

		// Now register the weather tool directly with the external agent
		// This maintains proper encapsulation while allowing custom tool registration
		agent.RegisterCustomTool(
			"get_weather",
			"Get current weather information for a specific location. This tool provides real-time weather data including temperature, humidity, wind speed, pressure, and weather conditions. Use this tool when users ask about weather, temperature, or weather forecasts for any city or location.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "The city name or location to get weather for (e.g., 'New York City', 'London', 'Tokyo', 'Paris')",
					},
					"units": map[string]interface{}{
						"type":        "string",
						"description": "Temperature units: 'metric' (Celsius) or 'imperial' (Fahrenheit). Defaults to 'metric' if not specified.",
						"enum":        []string{"metric", "imperial"},
					},
				},
				"required": []string{"location"},
			},
			func(ctx context.Context, args map[string]interface{}) (string, error) {
				location, ok := args["location"].(string)
				if !ok {
					return "", fmt.Errorf("location parameter is required for get_weather")
				}

				units := "metric" // default
				if unitsVal, ok := args["units"].(string); ok {
					units = unitsVal
				}

				// Call the actual weather tool
				return weatherTool.Call(ctx, fmt.Sprintf(`{"location": "%s", "units": "%s"}`, location, units))
			},
		)

		logger.Info(fmt.Sprintf("✅ Registered weather tool with external agent"))

		logger.Info(fmt.Sprintf("✅ Agent created successfully with custom tools"))

		// Test the weather tool
		weatherQuestion := "What's the weather like in New York City?"
		logger.Info(fmt.Sprintf("Testing weather tool with question: %s", weatherQuestion))

		response, err := agent.Ask(ctx, weatherQuestion)
		if err != nil {
			logger.Error(fmt.Sprintf("❌ Weather tool test failed: %w", err), nil)
			return fmt.Errorf("weather tool test failed: %w", err)
		}

		logger.Info(fmt.Sprintf("✅ Weather tool test successful"))
		logger.Info(fmt.Sprintf("Response length: %d characters", len(response)))

		// Show response
		fmt.Printf("\n🌤️ Question: %s\n", weatherQuestion)
		fmt.Printf("📝 Response: %s\n", response)

		// Show agent capabilities
		toolNames := make([]string, 0, len(agent.Tools))
		for _, tool := range agent.Tools {
			if tool.Function != nil {
				toolNames = append(toolNames, tool.Function.Name)
			}
		}
		fmt.Printf("\n📊 Available Tools: %v\n", toolNames)

		// Show connected servers
		serverNames := agent.GetServerNames()
		fmt.Printf("\n🛠️ Connected Servers: %v\n", serverNames)

		// Close the agent
		agent.Close()

		logger.Info(fmt.Sprintf("✅ Custom tools test completed successfully"))
		fmt.Println("\n🎉 Custom tools test completed successfully!")

		return nil
	},
}
