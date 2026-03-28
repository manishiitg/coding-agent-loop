package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
	"github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	vertexadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

type simpleLogger struct{}

func (l *simpleLogger) Infof(format string, v ...any) { fmt.Printf("INFO: "+format+"\n", v...) }
func (l *simpleLogger) Errorf(format string, v ...any) { fmt.Printf("ERROR: "+format+"\n", v...) }
func (l *simpleLogger) Debugf(format string, v ...any) { fmt.Printf("DEBUG: "+format+"\n", v...) }

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found")
	}

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

	// Folder guard: restrict agent to read from "docs", write only to "output".
	// Paths are workspace-relative (under /app/workspace-docs in the container).
	folderGuard := &workspace.FolderGuardConfig{
		Enabled:      true,
		ReadPaths:    []string{"docs"},
		WritePaths:   []string{"output"},
		BlockedPaths: nil, // optional deny list
	}

	wsClient := workspace.NewClient(workspaceAPIURL, workspace.WithFolderGuard(folderGuard))
	basicTools := workspace.GetAdvancedToolDefinitions()
	shellTools := workspace.GetShellToolDefinitions()
	var allTools []llmtypes.Tool
	allTools = append(allTools, basicTools...)
	allTools = append(allTools, shellTools...)
	executors := workspace.NewBasicExecutor(wsClient)
	for k, v := range workspace.NewAdvancedExecutor(wsClient) {
		executors[k] = v
	}

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

	prompt := `List files in the docs folder and create a short summary file (summary.txt) in the output folder. Do not read or write outside docs/ or output/.`
	fmt.Printf("Initializing Folder Guard Agent with Workspace API: %s\n", workspaceAPIURL)
	fmt.Printf("Folder guard: read=%v write=%v\n", folderGuard.ReadPaths, folderGuard.WritePaths)
	fmt.Printf("\n--- Goal: %s ---\n", prompt)

	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, prompt),
	}

	maxTurns := 8
	for i := 0; i < maxTurns; i++ {
		fmt.Printf("\n--- Turn %d ---\n", i+1)
		response, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(allTools))
		if err != nil {
			log.Printf("LLM generation failed: %v", err)
			break
		}
		choice := response.Choices[0]
		if choice.Content != "" {
			fmt.Printf("Agent: %s\n", choice.Content)
			messages = append(messages, llmtypes.TextParts(llmtypes.ChatMessageTypeAI, choice.Content))
		}
		if len(choice.ToolCalls) > 0 {
			for _, toolCall := range choice.ToolCalls {
				fc := toolCall.FunctionCall
				fmt.Printf("Executing Tool: %s\n", fc.Name)
				executor, exists := executors[fc.Name]
				if !exists {
					log.Printf("Tool %s not found\n", fc.Name)
					continue
				}
				var argsMap map[string]interface{}
				json.Unmarshal([]byte(fc.Arguments), &argsMap)
				start := time.Now()
				result, err := executor(ctx, argsMap)
				duration := time.Since(start)
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
					fmt.Printf("Tool Failed (%v): %s\n", duration, err)
				} else {
					display := result
					if len(display) > 200 {
						display = display[:200] + "..."
					}
					fmt.Printf("Tool Success (%v): %s\n", duration, display)
				}
				messages = append(messages, llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{
						llmtypes.ToolCallResponse{
							ToolCallID: toolCall.ID,
							Name:       fc.Name,
							Content:    result,
						},
					},
				})
			}
		} else if choice.Content != "" {
			if i > 1 {
				break
			}
		} else {
			break
		}
	}
	fmt.Println("\nFolder Guard Agent execution completed.")
}
