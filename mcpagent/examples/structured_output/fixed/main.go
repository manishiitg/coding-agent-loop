package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

// Person represents a simple person profile
type Person struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

// Project represents a complex project with nested structures
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

	// Step 2: Set up file loggers
	// Create logs directory if it doesn't exist
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logs directory: %v\n", err)
		os.Exit(1)
	}

	// Define log file paths
	llmLogFile := filepath.Join(logDir, "llm.log")
	agentLogFile := filepath.Join(logDir, "structured-output-fixed.log")

	// Clear existing log files to start fresh for this run
	if err := os.Truncate(llmLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear LLM log file: %v\n", err)
	}
	if err := os.Truncate(agentLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear agent log file: %v\n", err)
	}

	// Create logger for LLM operations (API calls, token usage, etc.)
	llmLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",     // Log level: debug, info, warn, error
		Format:     "text",     // Output format: text or json
		Output:     llmLogFile, // Write logs to file
		EnableFile: false,      // Output already set to file, no need for dual output
		FilePath:   "",         // Not needed when Output is set to file path
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create LLM logger: %v\n", err)
		os.Exit(1)
	}
	defer llmLogger.Close() // Ensure log file is closed on exit

	fmt.Printf("LLM logging to: %s (cleared)\n", llmLogFile)

	// Step 3: Initialize OpenAI LLM with file logger
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41,
		Temperature: 0.7,
		Logger:      llmLogger, // Use file logger for LLM operations
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Create logger for agent operations (MCP connections, tool execution, etc.)
	agentLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",       // Log level: debug, info, warn, error
		Format:     "text",       // Output format: text or json
		Output:     agentLogFile, // Write logs to file
		EnableFile: false,        // Output already set to file, no need for dual output
		FilePath:   "",           // Not needed when Output is set to file path
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent logger: %v\n", err)
		os.Exit(1)
	}
	defer agentLogger.Close() // Ensure log file is closed on exit

	fmt.Printf("Agent logging to: %s (cleared)\n", agentLogFile)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 7: Create the agent with logger
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger), // Use file logger for agent operations
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Fixed Structured Output Example ===")
	fmt.Println("This example demonstrates structured output using AskStructured")
	fmt.Println("Method: Text response → JSON conversion via second LLM call")
	fmt.Println()

	// Log start to agent log file
	agentLogger.Info("Structured output fixed example started")

	// Example 1: Simple Person struct
	fmt.Println("--- Example 1: Simple Person Profile ---")
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
		fmt.Fprintf(os.Stderr, "Failed to get structured output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Person Profile Created:\n")
	fmt.Printf("   Name: %s\n", person.Name)
	fmt.Printf("   Age: %d\n", person.Age)
	fmt.Printf("   Email: %s\n", person.Email)

	personJSON, _ := json.MarshalIndent(person, "", "  ")
	fmt.Printf("\nJSON Output:\n%s\n\n", string(personJSON))

	// Example 2: Complex Project struct
	fmt.Println("--- Example 2: Complex Project with Nested Structures ---")
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
		fmt.Fprintf(os.Stderr, "Failed to get structured output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Project Created:\n")
	fmt.Printf("   Name: %s\n", project.Name)
	fmt.Printf("   Status: %s\n", project.Status)
	fmt.Printf("   Budget: $%.2f\n", project.Budget)
	fmt.Printf("   Team Members: %d\n", len(project.Members))
	fmt.Printf("   Milestones: %d\n", len(project.Milestones))

	for i, member := range project.Members {
		fmt.Printf("   Member %d: %s (%s) - %s\n", i+1, member.Name, member.Role, member.Email)
	}

	for i, milestone := range project.Milestones {
		fmt.Printf("   Milestone %d: %s (Status: %s, Progress: %d%%)\n",
			i+1, milestone.Title, milestone.Status, milestone.Progress)
	}

	projectJSON, _ := json.MarshalIndent(project, "", "  ")
	fmt.Printf("\nJSON Output:\n%s\n\n", string(projectJSON))

	fmt.Println("=== Example Complete ===")
	fmt.Println("Note: This method uses 2 LLM calls (text response + JSON conversion)")
	fmt.Println("Pros: Always works, reliable for complex schemas")
	fmt.Println("Cons: Slower and more expensive (2x LLM calls)")

	// Log completion to agent log file
	agentLogger.Info("Structured output fixed example completed")
}
