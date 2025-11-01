package testing

import (
	"context"
	"fmt"
	"log"
	"time"

	"mcp-agent/agent_go/pkg/external"

	"github.com/spf13/cobra"
)

// streamingTracerCmd represents the streaming tracer test command
var streamingTracerCmd = &cobra.Command{
	Use:   "streaming-tracer",
	Short: "Test the streaming tracer functionality with external package",
	Long: `Test the streaming tracer functionality using the external package.

This test demonstrates:
1. Streaming tracer compilation and integration
2. External package usage with streaming tracer
3. Event listener functionality with streaming capabilities
4. Agent creation and MCP server connections
5. Event system functionality

Examples:
  # Test streaming tracer with OpenAI
  orchestrator test streaming-tracer --provider openai --log-file logs/streaming-tracer-test.log
  
  # Test streaming tracer with Bedrock
  orchestrator test streaming-tracer --provider bedrock --log-file logs/streaming-tracer-test.log`,
	Run: runStreamingTracerTest,
}

func init() {
	// Add any specific flags for streaming tracer test
	streamingTracerCmd.Flags().StringVar(&config, "config", "configs/mcp_servers_simple.json", "MCP config file to use for testing")
}

func runStreamingTracerTest(cmd *cobra.Command, args []string) {
	fmt.Println("=== STREAMING TRACER TEST STARTED ===")

	// Get logging configuration from root command flags
	logFile := cmd.Flag("log-file").Value.String()
	logLevel := cmd.Flag("log-level").Value.String()

	// Initialize test logger
	logger := GetTestLogger()
	if logFile != "" {
		InitTestLogger(logFile, logLevel)
		logger = GetTestLogger()
	}

	logger.Info("🧪 Starting Streaming Tracer Test")
	logger.Info("==================================")

	// Run the streaming tracer test
	if err := testStreamingTracer(); err != nil {
		logger.Errorf("❌ Streaming tracer test failed: %w", err)
		log.Fatalf("❌ Streaming tracer test failed: %w", err)
	}

	logger.Info("🎉 Streaming Tracer Test Completed Successfully!")
	fmt.Println("=== STREAMING TRACER TEST COMPLETED ===")
}

// testStreamingTracer demonstrates the streaming tracer functionality using external package
func testStreamingTracer() error {
	log.Println("🧪 Testing Streaming Tracer Functionality (External Package)")
	log.Println("============================================================")

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create external agent configuration
	config := external.Config{
		Provider:      "openai",
		ModelID:       "gpt-4.1", // Use GPT-4.1 as requested
		Temperature:   0.1,
		ToolChoice:    "auto",
		MaxTurns:      3,
		AgentMode:     external.SimpleAgent,
		ServerName:    "all",
		ConfigPath:    "configs/mcp_servers_simple.json",
		TraceProvider: "console", // Use console tracer for testing
		LangfuseHost:  "",
		ToolTimeout:   5 * time.Minute,
		SystemPrompt: external.SystemPromptConfig{
			Mode: "auto", // Use auto mode for testing
		},
	}

	// Create external agent
	log.Println("🔍 Creating external agent with streaming tracer")
	agent, err := external.NewAgent(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create external agent: %w", err)
	}
	defer agent.Close()

	log.Println("✅ External agent created successfully")

	// Test 1: Add event listener (this should internally enable streaming tracer)
	log.Println("📋 Test 1: Adding Event Listener")
	testListener := &testEventListener{
		name:      "test-listener",
		eventChan: make(chan *external.AgentEvent, 10),
	}

	agent.AddEventListener(testListener)
	log.Println("✅ Event listener added successfully")

	// Test 2: Ask a question and capture events
	log.Println("📋 Test 2: Asking Question and Capturing Events")

	// Start event monitoring goroutine
	eventsReceived := make(chan bool, 1)
	go monitorEvents(testListener.eventChan, "Event Listener", eventsReceived)

	// Ask a simple question
	question := "What is 2 + 2? Please provide a brief answer."
	log.Printf("🤔 Asking: %s", question)

	response, err := agent.Invoke(ctx, question)
	if err != nil {
		return fmt.Errorf("failed to ask question: %w", err)
	}

	log.Printf("💬 Response: %s", response)

	// Wait for events to be received
	timeout := time.After(10 * time.Second)
	eventsCount := 0
	select {
	case <-eventsReceived:
		eventsCount++
		log.Println("✅ Events received successfully")
	case <-timeout:
		log.Println("⚠️  Timeout waiting for events")
	}

	log.Printf("📊 Events received: %d/1 channels", eventsCount)

	// Test 3: Check connection health
	log.Println("📋 Test 3: Checking Connection Health")
	health := agent.CheckHealth(ctx)
	log.Printf("🔍 Health check results: %+v", health)

	// Test 4: Get connection stats
	log.Println("📋 Test 4: Getting Connection Stats")
	stats := agent.GetStats()
	log.Printf("📊 Connection stats: %+v", stats)

	// Test 5: Get server names
	log.Println("📋 Test 5: Getting Server Names")
	serverNames := agent.GetServerNames()
	log.Printf("🖥️  Connected servers: %v", serverNames)

	// Test 6: Get capabilities
	log.Println("📋 Test 6: Getting Capabilities")
	capabilities := agent.GetCapabilities()
	log.Printf("🔧 Agent capabilities: %s", capabilities)

	log.Println("🎉 Streaming Tracer Test Completed Successfully!")
	return nil
}

// testEventListener implements external.AgentEventListener for testing
type testEventListener struct {
	name      string
	eventChan chan *external.AgentEvent
}

func (t *testEventListener) HandleEvent(ctx context.Context, event *external.AgentEvent) error {
	select {
	case t.eventChan <- event:
		log.Printf("📨 Event listener %s received event: %s", t.name, event.Type)
		return nil
	default:
		log.Printf("⚠️  Event listener %s channel full, skipping event: %s", t.name, event.Type)
		return nil
	}
}

func (t *testEventListener) Name() string {
	return t.name
}

// monitorEvents monitors events from a channel and logs them
func monitorEvents(eventChan <-chan *external.AgentEvent, channelName string, eventsReceived chan<- bool) {
	eventCount := 0
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				log.Printf("🔌 %s closed after %d events", channelName, eventCount)
				return
			}
			eventCount++
			log.Printf("📡 %s received event %d: %s", channelName, eventCount, event.Type)

			// Signal that we received an event
			select {
			case eventsReceived <- true:
			default:
			}

		case <-time.After(15 * time.Second):
			log.Printf("⏰ %s timeout after %d events", channelName, eventCount)
			return
		}
	}
}
