package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
	"github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	vertexadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

// Simple logger implementation
type simpleLogger struct{}

func (l *simpleLogger) Infof(format string, v ...any) { fmt.Printf("INFO: "+format+"\n", v...) }
func (l *simpleLogger) Errorf(format string, v ...any) { fmt.Printf("ERROR: "+format+"\n", v...) }
func (l *simpleLogger) Debugf(format string, v ...any) { fmt.Printf("DEBUG: "+format+"\n", v...) }

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found")
	}

	// 1. Configuration
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

	os.Setenv("VERTEX_PROJECT_ID", projectID)
	os.Setenv("VERTEX_LOCATION_ID", location)

	fmt.Printf("Initializing Multimodal Agent with Workspace API: %s\n", workspaceAPIURL)

	// 2. Initialize Workspace Client & Tools
	wsClient := workspace.NewClient(workspaceAPIURL)
	
	// Construct tool list: pick only the tool categories needed (no duplication in workspace package).
	var allTools []llmtypes.Tool
	allTools = append(allTools, workspace.GetShellToolDefinitions()...)
	allTools = append(allTools, workspace.GetImageToolDefinitions()...)
	allTools = append(allTools, browser.GetToolDefinition())
	// Omit workspace.GetWebToolDefinitions() and Git tools unless needed.

	// Initialize Executor (handles all tool types including browser/shell)
	executors := workspace.NewBasicExecutor(wsClient)

	// 3. Initialize LLM
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

	// 4. Define Goal
	prompt := `
	1. Use the shell to check the current directory contents (ls -la).
	2. Use the browser to visit "https://example.com" and take a screenshot.
	3. Use read_image to describe what you see in the screenshot.
	`
	fmt.Printf("\n--- Goal: %s ---\n", prompt)

	// 5. Run Agent Loop
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, prompt),
	}

	maxTurns := 10
	for i := 0; i < maxTurns; i++ {
		fmt.Printf("\n--- Turn %d ---\n", i+1)

		response, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(allTools))
		if err != nil {
			log.Printf("LLM generation failed: %v", err)
			// Retry once if failed? No, let's just log and maybe break or continue
			break
		}

		choice := response.Choices[0]
		
		if choice.Content != "" {
			fmt.Printf("Agent: %s\n", choice.Content)
			messages = append(messages, llmtypes.TextParts(llmtypes.ChatMessageTypeAI, choice.Content))
		}

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

				// Execute tool
				start := time.Now()
				result, err := executor(ctx, argsMap)
				duration := time.Since(start)
				
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
					fmt.Printf("Tool Failed (%v): %s\n", duration, err)
				} else {
					// Truncate result for display if too long
					displayResult := result
					if len(result) > 200 {
						displayResult = result[:200] + "..."
					}
					fmt.Printf("Tool Success (%v): %s\n", duration, displayResult)
				}

				// Add result to history
				messages = append(messages, llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{
						llmtypes.ToolCallResponse{
							ToolCallID: toolCall.ID,
							Name:       functionCall.Name,
							Content:    result,
						},
					},
				})
			}
		} else if choice.Content != "" {
			fmt.Println("Agent finished turn with text response.")
			if i > 2 { 
				break 
			}
		} else {
			fmt.Println("Agent stopped.")
			break
		}
	}
	
	fmt.Println("\nMultimodal Agent execution completed.")
}