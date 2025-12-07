package conversion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "mcpagent/agent"
	testutils "mcpagent/cmd/testing/testutils"
	loggerv2 "mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Struct definitions for structured output testing

// Simple types for basic testing
type Person struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

// TodoList types for structured output testing
type Subtask struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Priority       string    `json:"priority"`
	Description    string    `json:"description,omitempty"`
	EstimatedHours int       `json:"estimated_hours,omitempty"`
	Subtasks       []Subtask `json:"subtasks,omitempty"`
	Dependencies   []string  `json:"dependencies,omitempty"`
}

type TodoList struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Tasks       []Subtask `json:"tasks"`
	Status      string    `json:"status"`
}

// Project Management types
type ProjectMember struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	Capacity int    `json:"capacity_hours_per_week"`
}

type ProjectMilestone struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DueDate     time.Time `json:"due_date"`
	Status      string    `json:"status"`
	Progress    int       `json:"progress_percentage"`
}

type Project struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Status      string             `json:"status"`
	StartDate   time.Time          `json:"start_date"`
	EndDate     time.Time          `json:"end_date"`
	Budget      float64            `json:"budget"`
	Members     []ProjectMember    `json:"members"`
	Milestones  []ProjectMilestone `json:"milestones"`
	Risks       []string           `json:"risks"`
	Tags        []string           `json:"tags"`
}

var structuredOutputConversionTestCmd = &cobra.Command{
	Use:   "structured-output-conversion",
	Short: "Test Model 1: Text Conversion Model (AskStructured, AskWithHistoryStructured)",
	Long: `Test the agent's structured output using the Text Conversion Model.

**Model 1: Text Conversion Model**
- How it works: Gets text response → Converts to JSON using second LLM call
- Methods tested: AskStructured, AskWithHistoryStructured
- Pros: Always works, reliable for complex schemas
- Cons: Requires 2 LLM calls (slower, more expensive)

This test validates:
1. AskStructured - Single-question structured output
2. AskWithHistoryStructured - Multi-turn conversation with structured output
3. Simple structures (Person)
4. Nested structures (TodoList with tasks)
5. Complex nested structures (Project with members and milestones)
6. JSON schema extraction and conversion
7. Text-to-JSON conversion reliability

See criteria.md in this folder for detailed log analysis criteria.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStructuredOutputConversionTest()
	},
}

// GetStructuredOutputConversionTestCmd returns the test command
func GetStructuredOutputConversionTestCmd() *cobra.Command {
	return structuredOutputConversionTestCmd
}

func runStructuredOutputConversionTest() error {
	// Load .env file if it exists (check multiple paths)
	envPaths := []string{".env", "../../../.env", "mcpagent/.env"}
	for _, path := range envPaths {
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
			break
		}
	}

	// Initialize logger using shared utilities
	logger := testutils.NewTestLoggerFromViper()

	logger.Info("=== Structured Output Conversion Test (Model 1) ===")
	logger.Info("Testing Text Conversion Model: AskStructured and AskWithHistoryStructured")
	logger.Info("How it works: Text response → JSON conversion via second LLM call")

	// Create LLM
	llm, err := testutils.CreateTestLLMFromViper(logger)
	if err != nil {
		return fmt.Errorf("failed to create LLM: %w", err)
	}

	// Get tracer
	tracerProvider := viper.GetString("tracer")
	tracer, _ := testutils.GetTracerWithLogger(tracerProvider, logger)
	traceID := testutils.GenerateTestTraceID()

	// Create agent
	ctx := context.Background()
	agent, err := testutils.CreateMinimalAgent(ctx, llm, tracer, traceID, logger)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	logger.Info("✅ Agent created successfully")

	// Run all tests
	testsPassed := 0
	testsFailed := 0

	if err := TestAskStructured(agent, ctx, logger); err != nil {
		logger.Error("❌ TestAskStructured failed", err)
		testsFailed++
	} else {
		testsPassed++
	}

	if err := TestAskWithHistoryStructured(agent, ctx, logger); err != nil {
		logger.Error("❌ TestAskWithHistoryStructured failed", err)
		testsFailed++
	} else {
		testsPassed++
	}

	if err := TestComplexNestedStructures(agent, ctx, logger); err != nil {
		logger.Error("❌ TestComplexNestedStructures failed", err)
		testsFailed++
	} else {
		testsPassed++
	}

	logger.Info("=== Structured Output Conversion Test Complete ===")
	logger.Info(fmt.Sprintf("📊 Tests passed: %d, Tests failed: %d", testsPassed, testsFailed))
	logger.Info("📋 Review the logs above to verify test success")
	logger.Info("📄 See criteria.md for detailed log analysis criteria")

	return nil
}

// TestAskStructured tests the AskStructured method
func TestAskStructured(agent *mcpagent.Agent, ctx context.Context, logger loggerv2.Logger) error {
	logger.Info("🧪 Test 1: AskStructured - Simple Person struct")
	logger.Info("Method: AskStructured (single question → text → JSON conversion)")

	personSchema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"},
			"email": {"type": "string"}
		},
		"required": ["name", "age", "email"]
	}`

	person, err := mcpagent.AskStructured(
		agent,
		ctx,
		"Create a person profile for a software engineer named John Doe, age 30, with email john.doe@example.com",
		Person{},
		personSchema,
	)

	if err != nil {
		logger.Error("❌ AskStructured failed", err)
		return err
	}

	logger.Info("✅ AskStructured successful")
	logger.Info(fmt.Sprintf("Person: %s, Age: %d, Email: %s", person.Name, person.Age, person.Email))

	// Validate the result
	if person.Name == "" || person.Age == 0 || person.Email == "" {
		logger.Error("❌ Person struct has empty fields", nil)
		return fmt.Errorf("person struct validation failed")
	}

	// Log JSON output
	jsonBytes, _ := json.MarshalIndent(person, "", "  ")
	logger.Info(fmt.Sprintf("Person JSON:\n%s", string(jsonBytes)))

	logger.Info("✅ Test 1 passed: Text → JSON conversion successful")

	return nil
}

// TestAskWithHistoryStructured tests the AskWithHistoryStructured method
func TestAskWithHistoryStructured(agent *mcpagent.Agent, ctx context.Context, logger loggerv2.Logger) error {
	logger.Info("🧪 Test 2: AskWithHistoryStructured - TodoList with conversation history")
	logger.Info("Method: AskWithHistoryStructured (multi-turn → text → JSON conversion)")

	todoSchema := `{
		"type": "object",
		"properties": {
			"title": {"type": "string"},
			"description": {"type": "string"},
			"tasks": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string"},
						"title": {"type": "string"},
						"status": {"type": "string"},
						"priority": {"type": "string"},
						"description": {"type": "string"},
						"estimated_hours": {"type": "integer"}
					},
					"required": ["id", "title", "status", "priority"]
				}
			},
			"status": {"type": "string"}
		},
		"required": ["title", "description", "tasks", "status"]
	}`

	// Create conversation history
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "I need to create a todo list for learning Go programming."}},
		},
		{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "I can help you create a structured todo list. What specific topics would you like to cover?"}},
		},
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Include basics like syntax, data structures, and concurrency. Create a todo list with 3 tasks."}},
		},
	}

	logger.Info(fmt.Sprintf("Conversation history: %d messages", len(messages)))

	todoList, updatedMessages, err := mcpagent.AskWithHistoryStructured(
		agent,
		ctx,
		messages,
		TodoList{},
		todoSchema,
	)

	if err != nil {
		logger.Error("❌ AskWithHistoryStructured failed", err)
		return err
	}

	logger.Info("✅ AskWithHistoryStructured successful")
	logger.Info(fmt.Sprintf("TodoList: %s", todoList.Title))
	logger.Info(fmt.Sprintf("Description: %s", todoList.Description))
	logger.Info(fmt.Sprintf("Status: %s", todoList.Status))
	logger.Info(fmt.Sprintf("Number of tasks: %d", len(todoList.Tasks)))
	logger.Info(fmt.Sprintf("Updated message history length: %d", len(updatedMessages)))

	// Validate the result
	if todoList.Title == "" || len(todoList.Tasks) == 0 {
		logger.Error("❌ TodoList struct has empty fields", nil)
		return fmt.Errorf("todoList struct validation failed")
	}

	// Log tasks
	for i, task := range todoList.Tasks {
		logger.Info(fmt.Sprintf("Task %d: %s (Priority: %s, Status: %s)", i+1, task.Title, task.Priority, task.Status))
	}

	// Log JSON output
	jsonBytes, _ := json.MarshalIndent(todoList, "", "  ")
	logger.Info(fmt.Sprintf("TodoList JSON:\n%s", string(jsonBytes)))

	logger.Info("✅ Test 2 passed: Multi-turn conversation → JSON conversion successful")

	return nil
}

// TestComplexNestedStructures tests complex nested structures
func TestComplexNestedStructures(agent *mcpagent.Agent, ctx context.Context, logger loggerv2.Logger) error {
	logger.Info("🧪 Test 3: Complex Nested Structures - Project with members and milestones")
	logger.Info("Method: AskStructured (complex nested arrays → JSON conversion)")

	projectSchema := `{
		"type": "object",
		"properties": {
			"id": {"type": "string"},
			"name": {"type": "string"},
			"description": {"type": "string"},
			"status": {"type": "string"},
			"start_date": {"type": "string", "format": "date-time"},
			"end_date": {"type": "string", "format": "date-time"},
			"budget": {"type": "number"},
			"members": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string"},
						"name": {"type": "string"},
						"role": {"type": "string"},
						"email": {"type": "string"},
						"capacity_hours_per_week": {"type": "integer"}
					},
					"required": ["id", "name", "role", "email", "capacity_hours_per_week"]
				}
			},
			"milestones": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"due_date": {"type": "string", "format": "date-time"},
						"status": {"type": "string"},
						"progress_percentage": {"type": "integer"}
					},
					"required": ["id", "title", "description", "due_date", "status", "progress_percentage"]
				}
			},
			"risks": {"type": "array", "items": {"type": "string"}},
			"tags": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["id", "name", "description", "status", "start_date", "end_date", "budget", "members", "milestones"]
	}`

	project, err := mcpagent.AskStructured(
		agent,
		ctx,
		"Create a project plan for developing a new mobile app with 3 team members (product manager, developer, designer) and 4 milestones (requirements, design, development, testing). Budget is $100,000.",
		Project{},
		projectSchema,
	)

	if err != nil {
		logger.Error("❌ Complex nested structure test failed", err)
		return err
	}

	logger.Info("✅ Complex nested structure test successful")
	logger.Info(fmt.Sprintf("Project: %s", project.Name))
	logger.Info(fmt.Sprintf("Status: %s", project.Status))
	logger.Info(fmt.Sprintf("Budget: $%.2f", project.Budget))
	logger.Info(fmt.Sprintf("Team Members: %d", len(project.Members)))
	logger.Info(fmt.Sprintf("Milestones: %d", len(project.Milestones)))

	// Validate the result
	if project.Name == "" || len(project.Members) == 0 || len(project.Milestones) == 0 {
		logger.Error("❌ Project struct has empty fields", nil)
		return fmt.Errorf("project struct validation failed")
	}

	// Log members
	for i, member := range project.Members {
		logger.Info(fmt.Sprintf("Member %d: %s (%s) - %s", i+1, member.Name, member.Role, member.Email))
	}

	// Log milestones
	for i, milestone := range project.Milestones {
		logger.Info(fmt.Sprintf("Milestone %d: %s (Status: %s, Progress: %d%%)",
			i+1, milestone.Title, milestone.Status, milestone.Progress))
	}

	// Log JSON output
	jsonBytes, _ := json.MarshalIndent(project, "", "  ")
	logger.Info(fmt.Sprintf("Project JSON:\n%s", string(jsonBytes)))

	logger.Info("✅ Test 3 passed: Complex nested structure → JSON conversion successful")

	return nil
}
