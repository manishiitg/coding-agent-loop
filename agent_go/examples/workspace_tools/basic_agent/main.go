package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
	"github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	vertexadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

// Simple logger implementation matching interfaces.Logger
type simpleLogger struct{}

func (l *simpleLogger) Infof(format string, v ...any) {
	fmt.Printf("INFO: "+format+"\n", v...)
}
func (l *simpleLogger) Errorf(format string, v ...any) {
	fmt.Printf("ERROR: "+format+"\n", v...)
}
func (l *simpleLogger) Debugf(format string, args ...interface{}) {
	fmt.Printf("DEBUG: "+format+"\n", args...)
}

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found")
	}

	// 1. Configuration from environment
	workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
	if workspaceAPIURL == "" {
		workspaceAPIURL = "http://localhost:8083"
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "mcp-agent-platform"
	}
	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	// Set environment variables required for LLM provider
	os.Setenv("VERTEX_PROJECT_ID", projectID)
	os.Setenv("VERTEX_LOCATION_ID", location)

	fmt.Printf("Initializing Basic Agent with Workspace API: %s\n", workspaceAPIURL)

	// 2. Initialize Workspace Client & Tools
	wsClient := workspace.NewClient(workspaceAPIURL)
	
	basicTools := workspace.GetAdvancedToolDefinitions()
	// NewBasicExecutor returns all executors, which is fine
	executors := workspace.NewBasicExecutor(wsClient)

	// 3. Initialize LLM Provider using Constants

	
	config := llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     vertexadapter.ModelGemini3FlashPreview,
		Temperature: 0,
		Logger:      &simpleLogger{},
	}

	llm, err := llmproviders.InitializeLLM(config)
	if err != nil {
		log.Fatalf("Failed to initialize LLM: %v", err)
	}

	// 4. Define the Agent's Goal
	prompt := "list all files in the workspace and tell me what you see"
	fmt.Printf("\n--- Goal: %s ---\n", prompt)

	// 5. Run the Agent Loop
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, prompt),
	}

	// Simple turn-based loop
	response, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(basicTools))
	if err != nil {
		log.Fatalf("LLM generation failed: %v", err)
	}

	choice := response.Choices[0]
	if len(choice.ToolCalls) > 0 {
		for _, toolCall := range choice.ToolCalls {
			functionCall := toolCall.FunctionCall
			fmt.Printf("Executing Tool: %s\n", functionCall.Name)
			
			executor, exists := executors[functionCall.Name]
			if !exists {
				log.Printf("Error: Tool %s not found\n", functionCall.Name)
				continue
			}
				var argsMap map[string]interface{}
			json.Unmarshal([]byte(functionCall.Arguments), &argsMap)

			result, err := executor(ctx, argsMap)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			fmt.Printf("Tool Result: %s\n", result)

			// Add result to history and get final response
			toolResultMessage := llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeTool,
				Parts: []llmtypes.ContentPart{
					llmtypes.ToolCallResponse{
						ToolCallID: toolCall.ID,
						Name:       functionCall.Name,
						Content:    result,
					},
				},
			}
			messages = append(messages, toolResultMessage)
		}

		// Final response
		finalResponse, err := llm.GenerateContent(ctx, messages)
		if err != nil {
			log.Fatalf("LLM final generation failed: %v", err)
		}
		fmt.Printf("\nAgent: %s\n", finalResponse.Choices[0].Content)
	} else {
		fmt.Printf("\nAgent: %s\n", choice.Content)
	}

	fmt.Println("\nBasic Agent execution completed.")
}
