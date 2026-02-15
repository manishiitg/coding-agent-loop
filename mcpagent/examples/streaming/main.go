package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

func main() {
	// Load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load .env file: %v\n", err)
		}
	}

	// Step 1: Get OpenAI API key from environment
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		fmt.Fprintf(os.Stderr, "Please set OPENAI_API_KEY environment variable\n")
		os.Exit(1)
	}

	// Step 2: Initialize OpenAI LLM
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41,
		Temperature: 0.7,
		Logger:      nil, // nil logger - will use default logger internally
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 3: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 4: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 5: Create the agent with streaming enabled
	// This example demonstrates two ways to handle streaming:
	// 1. Using StreamingCallback (simpler, direct)
	// 2. Using event subscription (more flexible, integrates with event system)

	// Option 1: Using StreamingCallback (commented out - uncomment to use)
	/*
		agent, err := mcpagent.NewAgent(
			ctx,
			llmModel,
			configPath,
			mcpagent.WithStreaming(true),
			mcpagent.WithStreamingCallback(func(chunk llmtypes.StreamChunk) {
				// Only process content chunks (text fragments)
				if chunk.Type == llmtypes.StreamChunkTypeContent && chunk.Content != "" {
					// Print text as it arrives (token by token)
					fmt.Print(chunk.Content)
					os.Stdout.Sync() // Flush immediately for real-time display
				}
				// Tool calls are processed normally (not streamed)
			}),
		)
	*/

	// Option 2: Using event subscription (active in this example)
	// Use NewAgentWithObservability to automatically get a streaming tracer
	// This creates a noop tracer wrapped in StreamingTracer for event streaming
	agent, err := mcpagent.NewAgentWithObservability(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithStreaming(true), // Enable streaming
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Subscribe to streaming events
	eventChan, unsubscribe, ok := agent.SubscribeToEvents(ctx)
	if !ok {
		fmt.Fprintf(os.Stderr, "Failed to subscribe to events (streaming tracer not available)\n")
		os.Exit(1)
	}
	defer unsubscribe()

	// Start goroutine to handle streaming events and tool calls
	var wg sync.WaitGroup
	wg.Add(1)
	var streamedContent strings.Builder
	var chunkCount int
	var toolCallCount int
	go func() {
		defer wg.Done()
		for event := range eventChan {
			if event.Data == nil {
				continue
			}

			eventType := event.Data.GetEventType()

			// Handle streaming events
			switch eventType {
			case events.StreamingStart:
				fmt.Println("\n=== Streaming Started ===")
			case events.StreamingChunk:
				chunkEvent, ok := event.Data.(*events.StreamingChunkEvent)
				if ok && !chunkEvent.IsToolCall {
					// This is a text content chunk
					chunkCount++
					streamedContent.WriteString(chunkEvent.Content)

					// Print text as it arrives (token by token)
					fmt.Print(chunkEvent.Content)
					os.Stdout.Sync() // Flush immediately for real-time display

					// Optional: Show progress indicator
					if chunkCount%10 == 0 {
						fmt.Fprintf(os.Stderr, "\n[Streamed %d chunks so far...]\n", chunkCount)
					}
				}
			case events.StreamingEnd:
				endEvent, ok := event.Data.(*events.StreamingEndEvent)
				if ok {
					fmt.Printf("\n\n=== Streaming Complete ===\n")
					fmt.Printf("Total chunks: %d\n", endEvent.TotalChunks)
					if endEvent.TotalTokens > 0 {
						fmt.Printf("Total tokens: %d\n", endEvent.TotalTokens)
					}
					if endEvent.Duration != "" {
						fmt.Printf("Duration: %s\n", endEvent.Duration)
					}
					fmt.Println("========================")
				}
			}

			// Handle tool call events
			switch eventType {
			case events.ToolCallStart:
				toolEvent, ok := event.Data.(*events.ToolCallStartEvent)
				if ok {
					toolCallCount++
					fmt.Fprintf(os.Stderr, "\n\n🔧 [Tool Call #%d] %s from %s\n", toolCallCount, toolEvent.ToolName, toolEvent.ServerName)
					if toolEvent.ToolParams.Arguments != "" {
						// Truncate long arguments for display
						args := toolEvent.ToolParams.Arguments
						if len(args) > 200 {
							args = args[:200] + "..."
						}
						fmt.Fprintf(os.Stderr, "   Parameters: %s\n", args)
					}
				}
			case events.ToolCallEnd:
				toolEvent, ok := event.Data.(*events.ToolCallEndEvent)
				if ok {
					fmt.Fprintf(os.Stderr, "✅ [Tool Call Complete] %s (duration: %v)\n", toolEvent.ToolName, toolEvent.Duration)
					// Truncate long results for display
					result := toolEvent.Result
					if len(result) > 300 {
						result = result[:300] + "..."
					}
					if result != "" {
						fmt.Fprintf(os.Stderr, "   Result: %s\n", result)
					}
				}
			case events.ToolCallError:
				toolEvent, ok := event.Data.(*events.ToolCallErrorEvent)
				if ok {
					fmt.Fprintf(os.Stderr, "❌ [Tool Call Error] %s: %s\n", toolEvent.ToolName, toolEvent.Error)
				}
			}
		}
	}()

	// Step 6: Ask the agent a question that will trigger tool calls
	question := "Get React documentation from context7, use sequential thinking to analyze the key concepts, and then write a summary explaining React's core principles. Use the everything server to calculate how many days are in a year."
	if len(os.Args) > 2 {
		question = os.Args[2]
	}

	fmt.Printf("Question: %s\n\n", question)
	fmt.Println("Streaming response (text appears as it's generated):")
	fmt.Println("Tool calls will be shown in stderr (🔧) while streaming happens in stdout")
	fmt.Println("---")

	// Ask the agent - response will be streamed in real-time via events
	answer, err := agent.Ask(ctx, question)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nFailed to get answer: %v\n", err)
		os.Exit(1)
	}

	// Wait for the event handler goroutine to finish processing all events.
	// NOTE: This example uses a WaitGroup for proper synchronization instead of time.Sleep;
	// in production, always prefer synchronization primitives over arbitrary sleeps.
	wg.Wait()

	// Step 7: Print the final answer (for comparison)
	fmt.Println("\n=== Final Complete Response ===")
	fmt.Println(answer)
	fmt.Println("===============================")

	// Verify streamed content matches final response
	if streamedContent.Len() > 0 {
		streamedText := streamedContent.String()
		if strings.TrimSpace(streamedText) == strings.TrimSpace(answer) {
			fmt.Println("\n✅ Streamed content matches final response!")
		} else {
			fmt.Println("\n⚠️  Streamed content differs from final response (this is normal if tool calls were made)")
			fmt.Printf("Streamed length: %d chars\n", len(streamedText))
			fmt.Printf("Final length: %d chars\n", len(answer))
		}
	}
}
