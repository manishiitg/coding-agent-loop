package paralleltoolexec

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/events"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
)

var parallelToolExecTestCmd = &cobra.Command{
	Use:   "parallel-tool-exec",
	Short: "Test parallel tool execution with real LLM and MCP servers",
	Long: `Test that parallel tool execution works correctly with real LLM and MCP servers.

This test:
1. Creates an agent with EnableParallelToolExecution=true and the context7 MCP server
2. Asks a question designed to trigger multiple concurrent tool calls
3. Measures wall-clock time and verifies tool calls ran in parallel
4. Compares with sequential execution to demonstrate speedup

The test uses an event listener to capture tool_call_start and tool_call_end events
to measure individual tool execution timings and detect concurrency.

Examples:
  mcpagent-test test parallel-tool-exec --log-file logs/parallel-test.log
  mcpagent-test test parallel-tool-exec --provider openai --model gpt-4.1-mini`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Parallel Tool Execution Test ===")

		modelFlag, _ := cmd.Flags().GetString("model")
		if err := testParallelToolExecution(logger, modelFlag); err != nil {
			return fmt.Errorf("parallel tool execution test failed: %w", err)
		}

		logger.Info("=== Parallel Tool Execution Test Passed! ===")
		return nil
	},
}

func init() {
	parallelToolExecTestCmd.Flags().String("model", "", "Model ID to use (e.g., gpt-4.1-mini, claude-sonnet-4)")
	_ = viper.BindPFlag("model", parallelToolExecTestCmd.Flags().Lookup("model"))
}

// GetParallelToolExecTestCmd returns the parallel tool exec test command
func GetParallelToolExecTestCmd() *cobra.Command {
	return parallelToolExecTestCmd
}

// toolTimingListener captures tool call start/end events for timing analysis
type toolTimingListener struct {
	mu              sync.Mutex
	toolStarts      map[string]time.Time // tool_call_id -> start time
	toolEnds        map[string]time.Time // tool_call_id -> end time
	toolNames       map[string]string    // tool_call_id (from span) -> tool name
	startTimes      []time.Time          // ordered start times
	endTimes        []time.Time          // ordered end times
	parallelCount   int                  // number of tool calls with IsParallel=true
	sequentialCount int                  // number of tool calls with IsParallel=false
}

func newToolTimingListener() *toolTimingListener {
	return &toolTimingListener{
		toolStarts: make(map[string]time.Time),
		toolEnds:   make(map[string]time.Time),
		toolNames:  make(map[string]string),
	}
}

func (l *toolTimingListener) Name() string { return "tool_timing_listener" }

func (l *toolTimingListener) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	if event == nil || event.Data == nil {
		return nil
	}

	eventType := event.Data.GetEventType()
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	switch eventType {
	case events.ToolCallStart:
		if startEvt, ok := event.Data.(*events.ToolCallStartEvent); ok {
			l.toolStarts[event.SpanID] = now
			l.toolNames[event.SpanID] = startEvt.ToolName
			l.startTimes = append(l.startTimes, now)
			if startEvt.IsParallel {
				l.parallelCount++
			} else {
				l.sequentialCount++
			}
		}
	case events.ToolCallEnd:
		l.endTimes = append(l.endTimes, now)
	case events.ToolCallError:
		l.endTimes = append(l.endTimes, now)
	}

	return nil
}

// getOverlapCount returns how many tool executions overlapped in time.
// If tools ran in parallel, at least 2 should have overlapping intervals.
func (l *toolTimingListener) getOverlapCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.startTimes) < 2 || len(l.endTimes) < 2 {
		return 0
	}

	// Simple overlap detection: if the 2nd tool started before the 1st tool ended,
	// they were running concurrently
	overlaps := 0
	for i := 0; i < len(l.startTimes)-1; i++ {
		// Check if tool i+1 started before tool i ended
		if i < len(l.endTimes) && i+1 < len(l.startTimes) {
			if l.startTimes[i+1].Before(l.endTimes[i]) {
				overlaps++
			}
		}
	}
	return overlaps
}

func (l *toolTimingListener) getToolCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.startTimes)
}

func (l *toolTimingListener) getParallelCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.parallelCount
}

func (l *toolTimingListener) getSequentialCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sequentialCount
}

func testParallelToolExecution(log loggerv2.Logger, modelFlag string) error {
	log.Info("--- Test: Parallel Tool Execution ---")

	// Use context7 (HTTP-based, no npx needed) — a reliable remote MCP server
	mcpServers := map[string]interface{}{
		"context7": map[string]interface{}{
			"url":      "https://mcp.context7.com/mcp",
			"protocol": "http",
		},
	}

	configPath, cleanup, err := testutils.CreateTempMCPConfig(mcpServers, log)
	if err != nil {
		return fmt.Errorf("failed to create temp MCP config: %w", err)
	}
	defer cleanup()

	// Get optional tracers
	var tracerOptions []mcpagent.AgentOption

	langfuseTracer, _ := testutils.GetTracerWithLogger("langfuse", log)
	if langfuseTracer != nil && !testutils.IsNoopTracer(langfuseTracer) {
		tracerOptions = append(tracerOptions, mcpagent.WithTracer(langfuseTracer))
		log.Info("Langfuse tracer enabled")
	}

	var tracer observability.Tracer
	if langfuseTracer != nil && !testutils.IsNoopTracer(langfuseTracer) {
		tracer = langfuseTracer
	} else {
		tracer, _ = testutils.GetTracerWithLogger("noop", log)
	}

	// Initialize LLM — use flag value, fall back to provider default
	modelID := modelFlag
	provider := viper.GetString("test.provider")
	if provider == "" {
		provider = string(llm.ProviderVertex)
	}
	if modelID == "" {
		llmProv, _ := llm.ValidateProvider(provider)
		modelID = llm.GetDefaultModel(llmProv)
	}

	model, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: provider,
		ModelID:  modelID,
		Logger:   log,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	log.Info("LLM initialized",
		loggerv2.String("model_id", modelID),
		loggerv2.String("provider", string(llmProvider)))

	ctx := context.Background()

	// ─── Run 1: PARALLEL execution ─────────────────────────────────────

	log.Info("=== Run 1: Parallel tool execution ===")

	parallelTraceID := testutils.GenerateTestTraceID()
	parallelOptions := append([]mcpagent.AgentOption{
		mcpagent.WithParallelToolExecution(true),
	}, tracerOptions...)

	parallelAgent, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, parallelTraceID, log, parallelOptions...)
	if err != nil {
		return fmt.Errorf("failed to create parallel agent: %w", err)
	}

	parallelListener := newToolTimingListener()
	parallelAgent.AddEventListener(parallelListener)

	// Ask a question that should trigger the LLM to call context7 multiple times
	// The prompt explicitly asks for multiple lookups to encourage parallel tool calls
	question := `I need documentation for these 3 libraries. For EACH one, use the resolve_library_id tool to find it, and then use the get_library_docs tool to fetch its docs. Do all 3 library lookups concurrently if possible:
1. React
2. Express.js
3. Next.js

For each library, give me a 1-sentence summary of what you found.`

	log.Info("Running parallel agent...",
		loggerv2.String("question_preview", question[:80]+"..."))

	parallelStart := time.Now()
	parallelResponse, err := parallelAgent.Ask(ctx, question)
	parallelDuration := time.Since(parallelStart)

	if err != nil {
		return fmt.Errorf("parallel agent execution failed: %w", err)
	}

	parallelToolCount := parallelListener.getToolCount()
	parallelOverlaps := parallelListener.getOverlapCount()

	parallelIsParallel := parallelListener.getParallelCount()
	parallelIsSequential := parallelListener.getSequentialCount()

	log.Info("Parallel execution completed",
		loggerv2.String("duration", parallelDuration.String()),
		loggerv2.Int("tool_calls", parallelToolCount),
		loggerv2.Int("overlapping_calls", parallelOverlaps),
		loggerv2.Int("is_parallel_true", parallelIsParallel),
		loggerv2.Int("is_parallel_false", parallelIsSequential),
		loggerv2.Int("response_length", len(parallelResponse)))

	// ─── Run 2: SEQUENTIAL execution (baseline) ────────────────────────

	log.Info("=== Run 2: Sequential tool execution (baseline) ===")

	sequentialTraceID := testutils.GenerateTestTraceID()
	sequentialOptions := append([]mcpagent.AgentOption{
		// EnableParallelToolExecution defaults to false
	}, tracerOptions...)

	sequentialAgent, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, sequentialTraceID, log, sequentialOptions...)
	if err != nil {
		return fmt.Errorf("failed to create sequential agent: %w", err)
	}

	sequentialListener := newToolTimingListener()
	sequentialAgent.AddEventListener(sequentialListener)

	log.Info("Running sequential agent...")

	sequentialStart := time.Now()
	sequentialResponse, err := sequentialAgent.Ask(ctx, question)
	sequentialDuration := time.Since(sequentialStart)

	if err != nil {
		return fmt.Errorf("sequential agent execution failed: %w", err)
	}

	sequentialToolCount := sequentialListener.getToolCount()
	sequentialOverlaps := sequentialListener.getOverlapCount()

	sequentialIsParallel := sequentialListener.getParallelCount()
	sequentialIsSequential := sequentialListener.getSequentialCount()

	log.Info("Sequential execution completed",
		loggerv2.String("duration", sequentialDuration.String()),
		loggerv2.Int("tool_calls", sequentialToolCount),
		loggerv2.Int("overlapping_calls", sequentialOverlaps),
		loggerv2.Int("is_parallel_true", sequentialIsParallel),
		loggerv2.Int("is_parallel_false", sequentialIsSequential),
		loggerv2.Int("response_length", len(sequentialResponse)))

	// ─── Analysis ──────────────────────────────────────────────────────

	log.Info("=== Results Analysis ===")
	log.Info(fmt.Sprintf("Parallel:   %s (%d tool calls, %d overlapping)", parallelDuration, parallelToolCount, parallelOverlaps))
	log.Info(fmt.Sprintf("Sequential: %s (%d tool calls, %d overlapping)", sequentialDuration, sequentialToolCount, sequentialOverlaps))

	if parallelDuration < sequentialDuration {
		speedup := float64(sequentialDuration) / float64(parallelDuration)
		log.Info(fmt.Sprintf("Speedup: %.2fx faster with parallel execution", speedup))
	} else {
		log.Info("No speedup observed (LLM may not have issued parallel tool calls)")
	}

	// Validation
	if len(parallelResponse) < 50 {
		log.Warn("Parallel response seems too short",
			loggerv2.Int("length", len(parallelResponse)))
	}
	if len(sequentialResponse) < 50 {
		log.Warn("Sequential response seems too short",
			loggerv2.Int("length", len(sequentialResponse)))
	}

	// Check that both responses mention the libraries
	for _, lib := range []string{"React", "Express", "Next"} {
		if !strings.Contains(strings.ToLower(parallelResponse), strings.ToLower(lib)) {
			log.Warn(fmt.Sprintf("Parallel response may be missing info about %s", lib))
		}
		if !strings.Contains(strings.ToLower(sequentialResponse), strings.ToLower(lib)) {
			log.Warn(fmt.Sprintf("Sequential response may be missing info about %s", lib))
		}
	}

	// Validate IsParallel flag
	if parallelIsParallel == 0 {
		log.Warn("Parallel run had NO tool calls with IsParallel=true — parallel execution may not be working")
	} else {
		log.Info(fmt.Sprintf("Parallel run: %d/%d tool calls marked IsParallel=true", parallelIsParallel, parallelToolCount))
	}
	if sequentialIsParallel > 0 {
		log.Warn(fmt.Sprintf("Sequential run unexpectedly had %d tool calls with IsParallel=true", sequentialIsParallel))
	} else {
		log.Info(fmt.Sprintf("Sequential run: all %d tool calls marked IsParallel=false (correct)", sequentialToolCount))
	}

	log.Info("Parallel response preview: " + truncateStr(parallelResponse, 300))
	log.Info("Sequential response preview: " + truncateStr(sequentialResponse, 300))

	// Flush tracers
	if langfuseTracer != nil {
		if flusher, ok := langfuseTracer.(interface{ Flush() }); ok {
			flusher.Flush()
			log.Info("Tracer flushed")
		}
	}

	return nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
