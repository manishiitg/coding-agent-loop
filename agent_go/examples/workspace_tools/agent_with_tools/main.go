package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
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
	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	// Set environment variables required for LLM provider
	os.Setenv("VERTEX_PROJECT_ID", projectID)
	os.Setenv("VERTEX_LOCATION_ID", location)

	fmt.Printf("Initializing Agent with Workspace API: %s\n", workspaceAPIURL)

	// 2. Initialize Workspace Client & Tools from our new library
	wsClient := workspace.NewClient(workspaceAPIURL)
	basicTools := workspace.GetBasicToolDefinitions()
	basicExecutors := workspace.NewBasicExecutor(wsClient)

	// 3. Initialize LLM Provider using Constants
	config := llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     vertexadapter.ModelGemini3FlashPreview, // Use Constant from adapter package
		Temperature: 0,
		Logger:      &simpleLogger{},
	}

	llm, err := llmproviders.InitializeLLM(config)
	if err != nil {
		log.Fatalf("Failed to initialize LLM: %v", err)
	}

	// 4. Define the Agent's Goal
	prompt := "create dummy files in workspace and list those files"
	fmt.Printf("\n--- Goal: %s ---\n", prompt)

	// 5. Run the Agent Loop
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, prompt),
	}

	maxTurns := 5
	for i := 0; i < maxTurns; i++ {
		fmt.Printf("\n--- Turn %d ---\n", i+1)

		// Generate response from LLM with tools
		response, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(basicTools))
		if err != nil {
			log.Fatalf("LLM generation failed: %v", err)
		}

		choice := response.Choices[0]
		
		if choice.Content != "" {
			fmt.Printf("Agent: %s\n", choice.Content)
			messages = append(messages, llmtypes.TextParts(llmtypes.ChatMessageTypeAI, choice.Content))
		}

		if len(choice.ToolCalls) > 0 {
			for _, toolCall := range choice.ToolCalls {
				functionCall := toolCall.FunctionCall
				if functionCall == nil {
					continue
				}

				fmt.Printf("Executing Tool: %s\n", functionCall.Name)
				
				executor, exists := basicExecutors[functionCall.Name]
				if !exists {
					log.Printf("Error: Tool %s not found\n", functionCall.Name)
					continue
				}

				var argsMap map[string]interface{}
				json.Unmarshal([]byte(functionCall.Arguments), &argsMap)

				// Execute tool
				result, err := executor(ctx, argsMap)
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
				}
				fmt.Printf("Tool Result Success: %v\n", err == nil)

				// Add result to history
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
		} else if choice.Content != "" {
			// Check if agent is done or asking a question
			fmt.Println("Agent interaction complete or awaiting feedback.")
			break
		} else {
			fmt.Println("Agent finished task.")
			break
		}
	}
}