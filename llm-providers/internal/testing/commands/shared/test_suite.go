package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	llmproviders "llm-providers"
	"llm-providers/interfaces"
	"llm-providers/internal/recorder"
	"llm-providers/internal/testing"
	"llm-providers/llmtypes"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var TestSuiteCmd = &cobra.Command{
	Use:   "test-suite",
	Short: "Run all recorded tests in replay mode for all models",
	Long: `Run all recorded tests in replay mode for all models.
This command automatically discovers all recorded test files and runs them in replay mode.
It groups tests by test type and model, then executes each test scenario.`,
	Run: runTestSuite,
}

type testSuiteFlags struct {
	testDir string
	verbose bool
}

var suiteFlags testSuiteFlags

func init() {
	TestSuiteCmd.Flags().StringVar(&suiteFlags.testDir, "test-dir", "testdata", "Directory containing recorded test files")
	TestSuiteCmd.Flags().BoolVar(&suiteFlags.verbose, "verbose", false, "Show detailed output for each test")
}

type testCase struct {
	TestName string
	ModelID  string
	Provider string
	File     string
	Hash     string
}

func runTestSuite(cmd *cobra.Command, args []string) {
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Initialize test registry
	initTestRegistry()

	// Discover all test files
	testCases, err := discoverTestFiles(suiteFlags.testDir)
	if err != nil {
		log.Fatalf("Failed to discover test files: %v", err)
	}

	if len(testCases) == 0 {
		log.Printf("❌ No recorded test files found in %s", suiteFlags.testDir)
		return
	}

	log.Printf("🚀 Test Suite: Running %d recorded test scenarios", len(testCases))
	log.Printf("📁 Test directory: %s", suiteFlags.testDir)
	log.Printf("📋 Registered test types: %v", getRegisteredTestNames())

	// Group by test type and model
	grouped := groupTests(testCases)

	// Get API key
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
	}
	os.Setenv("VERTEX_API_KEY", apiKey)

	// Run tests
	results := runTestGroup(grouped, logger)

	// Print summary
	printSummary(results)
}

// getRegisteredTestNames returns a sorted list of all registered test names
func getRegisteredTestNames() []string {
	names := make([]string, 0, len(testRegistry))
	for name := range testRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func discoverTestFiles(baseDir string) ([]testCase, error) {
	var testCases []testCase

	// Walk through testdata directory
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".json") {
			return nil
		}

		// Read JSON file
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		var recorded recorder.RecordedResponse
		if err := json.Unmarshal(data, &recorded); err != nil {
			return nil // Skip invalid JSON
		}

		// Extract hash from filename or use recorded hash
		hash := recorded.RequestHash
		if len(hash) > 16 {
			hash = hash[:16]
		}

		testCases = append(testCases, testCase{
			TestName: recorded.TestName,
			ModelID:  recorded.ModelID,
			Provider: recorded.Provider,
			File:     path,
			Hash:     hash,
		})

		return nil
	})

	return testCases, err
}

func groupTests(testCases []testCase) map[string]map[string][]testCase {
	grouped := make(map[string]map[string][]testCase)

	for _, tc := range testCases {
		if grouped[tc.TestName] == nil {
			grouped[tc.TestName] = make(map[string][]testCase)
		}
		grouped[tc.TestName][tc.ModelID] = append(grouped[tc.TestName][tc.ModelID], tc)
	}

	return grouped
}

type testResult struct {
	TestName string
	ModelID  string
	Passed   bool
	Duration time.Duration
	Error    string
}

// testRunner is a function type that runs a test with the given context, LLM instance, and model ID
type testRunner func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string)

// testRegistry maps test names to their runner functions
var testRegistry = make(map[string]testRunner)

// registerTest registers a test runner for a given test name
func registerTest(testName string, runner testRunner) {
	testRegistry[testName] = runner
}

// initTestRegistry initializes the test registry with all available tests
func initTestRegistry() {
	// Register plain text tests
	registerTest("plain_text", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
		RunPlainTextTestWithContext(ctx, llm, modelID)
		return true, ""
	})

	// Register tool call tests
	registerTest("tool_call", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
		RunToolCallTestWithContext(ctx, llm, modelID)
		return true, ""
	})

	// Register tool call events tests
	registerTest("tool_call_events", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
		// Create test event emitter to capture events
		testEmitter := NewTestEventEmitter()
		// Re-initialize LLM with event emitter
		var err error
		if provider == "openai" {
			llm, err = llmproviders.InitializeLLM(llmproviders.Config{
				Provider:     llmproviders.ProviderOpenAI,
				ModelID:      modelID,
				Temperature:  0.7,
				Logger:       logger,
				EventEmitter: testEmitter,
				Context:      ctx,
			})
		} else if provider == "bedrock" {
			llm, err = llmproviders.InitializeLLM(llmproviders.Config{
				Provider:     llmproviders.ProviderBedrock,
				ModelID:      modelID,
				Temperature:  0.7,
				Logger:       logger,
				EventEmitter: testEmitter,
				Context:      ctx,
			})
		} else if provider == "openrouter" {
			llm, err = llmproviders.InitializeLLM(llmproviders.Config{
				Provider:     llmproviders.ProviderOpenRouter,
				ModelID:      modelID,
				Temperature:  0.7,
				Logger:       logger,
				EventEmitter: testEmitter,
				Context:      ctx,
			})
		} else if provider == "anthropic" {
			llm, err = llmproviders.InitializeLLM(llmproviders.Config{
				Provider:     llmproviders.ProviderAnthropic,
				ModelID:      modelID,
				Temperature:  0.7,
				Logger:       logger,
				EventEmitter: testEmitter,
				Context:      ctx,
			})
		} else {
			// Default to Vertex
			llm, err = llmproviders.InitializeLLM(llmproviders.Config{
				Provider:     llmproviders.ProviderVertex,
				ModelID:      modelID,
				Temperature:  0.7,
				Logger:       logger,
				EventEmitter: testEmitter,
				Context:      ctx,
			})
		}
		if err != nil {
			return false, fmt.Sprintf("Failed to re-initialize LLM with event emitter: %v", err)
		}
		RunToolCallEventTestWithContext(ctx, llm, modelID, testEmitter)
		return true, ""
	})

	// Register token usage tests
	registerTest("token_usage", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
		messages := []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hello world"}},
			},
		}
		TestLLMTokenUsage(ctx, llm, messages, "Hello world")
		return true, ""
	})

	// Register token usage cache tests
	registerTest("token_usage_cache", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
		TestLLMTokenUsageWithCache(ctx, llm)
		return true, ""
	})

	// Register simple reasoning tests (for gpt-5.1 with reasoning_effort and verbosity)
	registerTest("simple_reasoning", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
		messages := []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
			},
		}
		// Use reasoning_effort and verbosity for reasoning models
		TestLLMTokenUsage(ctx, llm, messages, "Hi", llmtypes.WithReasoningEffort("high"), llmtypes.WithVerbosity("high"))
		return true, ""
	})
}

func runTestGroup(grouped map[string]map[string][]testCase, logger interfaces.Logger) []testResult {
	var results []testResult

	// Sort test names and models for consistent output
	testNames := make([]string, 0, len(grouped))
	for name := range grouped {
		testNames = append(testNames, name)
	}
	sort.Strings(testNames)

	for _, testName := range testNames {
		models := grouped[testName]
		modelIDs := make([]string, 0, len(models))
		for model := range models {
			modelIDs = append(modelIDs, model)
		}
		sort.Strings(modelIDs)

		for _, modelID := range modelIDs {
			testCases := models[modelID]
			if len(testCases) == 0 {
				continue
			}

			// Get provider from first test case
			provider := testCases[0].Provider

			// Run test based on test name
			startTime := time.Now()
			passed, errMsg := runTestWithProvider(testName, modelID, provider, logger)
			duration := time.Since(startTime)

			results = append(results, testResult{
				TestName: testName,
				ModelID:  modelID,
				Passed:   passed,
				Duration: duration,
				Error:    errMsg,
			})

			if suiteFlags.verbose {
				if passed {
					log.Printf("  ✅ %s | %s: PASSED (%v)", testName, modelID, duration)
				} else {
					log.Printf("  ❌ %s | %s: FAILED (%v) - %s", testName, modelID, duration, errMsg)
				}
			}
		}
	}

	return results
}

func runTestWithProvider(testName, modelID, provider string, logger interfaces.Logger) (bool, string) {
	ctx := context.Background()

	// Setup recorder for replay
	recConfig := recorder.RecordingConfig{
		Enabled:  false, // Replay mode
		TestName: testName,
		Provider: provider,
		ModelID:  modelID,
		BaseDir:  suiteFlags.testDir,
	}
	rec := recorder.NewRecorder(recConfig)
	rec.SetReplayMode(true)
	ctx = recorder.WithRecorder(ctx, rec)

	// Initialize LLM based on provider
	var llmInstance llmtypes.Model
	var err error

	if provider == "openai" {
		llmInstance, err = llmproviders.InitializeLLM(llmproviders.Config{
			Provider:    llmproviders.ProviderOpenAI,
			ModelID:     modelID,
			Temperature: 0.7,
			Logger:      logger,
			Context:     ctx,
		})
	} else if provider == "bedrock" {
		llmInstance, err = llmproviders.InitializeLLM(llmproviders.Config{
			Provider:    llmproviders.ProviderBedrock,
			ModelID:     modelID,
			Temperature: 0.7,
			Logger:      logger,
			Context:     ctx,
		})
	} else if provider == "openrouter" {
		llmInstance, err = llmproviders.InitializeLLM(llmproviders.Config{
			Provider:    llmproviders.ProviderOpenRouter,
			ModelID:     modelID,
			Temperature: 0.7,
			Logger:      logger,
			Context:     ctx,
		})
	} else if provider == "anthropic" {
		llmInstance, err = llmproviders.InitializeLLM(llmproviders.Config{
			Provider:    llmproviders.ProviderAnthropic,
			ModelID:     modelID,
			Temperature: 0.7,
			Logger:      logger,
			Context:     ctx,
		})
	} else {
		// Default to Vertex
		llmInstance, err = llmproviders.InitializeLLM(llmproviders.Config{
			Provider:    llmproviders.ProviderVertex,
			ModelID:     modelID,
			Temperature: 0.7,
			Logger:      logger,
			Context:     ctx,
		})
	}

	if err != nil {
		return false, fmt.Sprintf("Failed to initialize LLM: %v", err)
	}

	// Look up test runner in registry
	runner, exists := testRegistry[testName]
	if !exists {
		return false, fmt.Sprintf("Unknown test type: %s (available: %v)", testName, getRegisteredTestNames())
	}

	// Run the test using the registered runner
	return runner(ctx, llmInstance, modelID, provider, logger)
}

func printSummary(results []testResult) {
	log.Printf("\n" + strings.Repeat("=", 70))
	log.Printf("📊 TEST SUITE SUMMARY")
	log.Printf(strings.Repeat("=", 70))

	passed := 0
	failed := 0
	totalDuration := time.Duration(0)

	// Group by test name
	byTest := make(map[string][]testResult)
	for _, r := range results {
		byTest[r.TestName] = append(byTest[r.TestName], r)
		if r.Passed {
			passed++
		} else {
			failed++
		}
		totalDuration += r.Duration
	}

	log.Printf("\n📋 Results by Test Type:")
	for testName := range byTest {
		testResults := byTest[testName]
		testPassed := 0
		testFailed := 0
		for _, r := range testResults {
			if r.Passed {
				testPassed++
			} else {
				testFailed++
			}
		}
		log.Printf("\n  %s:", strings.ToUpper(testName))
		log.Printf("    ✅ Passed: %d", testPassed)
		if testFailed > 0 {
			log.Printf("    ❌ Failed: %d", testFailed)
		}
		for _, r := range testResults {
			status := "✅"
			if !r.Passed {
				status = "❌"
			}
			log.Printf("      %s %s (%v)", status, r.ModelID, r.Duration)
			if !r.Passed && r.Error != "" {
				log.Printf("         Error: %s", r.Error)
			}
		}
	}

	log.Printf("\n📈 Overall Statistics:")
	log.Printf("   Total tests: %d", len(results))
	log.Printf("   ✅ Passed: %d", passed)
	if failed > 0 {
		log.Printf("   ❌ Failed: %d", failed)
	}
	log.Printf("   Total duration: %v", totalDuration)
	if len(results) > 0 {
		log.Printf("   Average duration: %v", totalDuration/time.Duration(len(results)))
	}

	successRate := float64(passed) / float64(len(results)) * 100
	log.Printf("   Success rate: %.1f%%", successRate)

	if failed == 0 {
		log.Printf("\n🎉 All tests passed!")
	} else {
		log.Printf("\n⚠️  Some tests failed. Check output above for details.")
	}
}
