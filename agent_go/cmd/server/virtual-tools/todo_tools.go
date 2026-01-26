package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetTodoToolCategory returns the category name for todo tools
func GetTodoToolCategory() string {
	return "todo_tools"
}

// TodoItem represents a single todo task in the todo list
type TodoItem struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Priority      string    `json:"priority"` // high, medium, low
	Status        string    `json:"status"`   // open, in_progress, completed, blocked
	AssignedAgent string    `json:"assigned_agent,omitempty"` // route_id or "generic"
	Result        string    `json:"result,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TodoFile represents the structure of the todos.json file
type TodoFile struct {
	StepID    string     `json:"step_id"`
	Objective string     `json:"objective"`
	Todos     []TodoItem `json:"todos"`
	Summary   TodoSummary `json:"summary"`
}

// TodoSummary provides a quick count of todo statuses
type TodoSummary struct {
	Total      int `json:"total"`
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Blocked    int `json:"blocked"`
}

// updateTodoSummary recalculates the summary based on current todos
func (tf *TodoFile) updateSummary() {
	tf.Summary = TodoSummary{
		Total: len(tf.Todos),
	}
	for _, todo := range tf.Todos {
		switch todo.Status {
		case "open":
			tf.Summary.Open++
		case "in_progress":
			tf.Summary.InProgress++
		case "completed":
			tf.Summary.Completed++
		case "blocked":
			tf.Summary.Blocked++
		}
	}
}

// CreateTodoTools creates todo management virtual tools
func CreateTodoTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// create_todo tool
	createTodoTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_todo",
			Description: "Create a new todo task in the todo list. Use this to break down work into trackable tasks that can be assigned to sub-agents.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Brief title for the task (e.g., 'Fetch customer data from API', 'Transform data to required format')",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Detailed description of what needs to be done, including specific requirements, input files, expected outputs, etc.",
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"high", "medium", "low"},
						"description": "Priority level of the task (high, medium, low). Default: medium",
					},
					"assigned_agent": map[string]interface{}{
						"type":        "string",
						"description": "Route ID of the predefined sub-agent to assign this task to, or 'generic' for the generic execution agent. Leave empty to assign later.",
					},
				},
				"required": []string{"title", "description"},
			}),
		},
	}
	tools = append(tools, createTodoTool)

	// update_todo tool
	updateTodoTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "update_todo",
			Description: "Update an existing todo task's status, notes, or assigned agent. Use this to track progress and add notes about the task.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todo_id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the todo task to update (e.g., 'todo_1', 'todo_abc123')",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "in_progress", "completed", "blocked"},
						"description": "New status for the task. Use 'in_progress' when work begins, 'completed' when done, 'blocked' when stuck.",
					},
					"notes": map[string]interface{}{
						"type":        "string",
						"description": "Notes to add about the task progress, issues encountered, or important information.",
					},
					"assigned_agent": map[string]interface{}{
						"type":        "string",
						"description": "Change the assigned agent (route_id or 'generic'). Leave empty to keep current assignment.",
					},
				},
				"required": []string{"todo_id"},
			}),
		},
	}
	tools = append(tools, updateTodoTool)

	// complete_todo tool
	completeTodoTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "complete_todo",
			Description: "Mark a todo task as completed with a result summary. This sets the status to 'completed' and records what was accomplished.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todo_id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the todo task to complete (e.g., 'todo_1', 'todo_abc123')",
					},
					"result_summary": map[string]interface{}{
						"type":        "string",
						"description": "Summary of what was accomplished. Include specific outputs like file names created, data counts, or other evidence of completion.",
					},
				},
				"required": []string{"todo_id", "result_summary"},
			}),
		},
	}
	tools = append(tools, completeTodoTool)

	// list_todos tool
	listTodosTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "list_todos",
			Description: "List all todo tasks, optionally filtered by status. Returns the complete todo list with summary counts.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status_filter": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "in_progress", "completed", "blocked", "all"},
						"description": "Filter todos by status. Use 'all' or leave empty to show all todos.",
					},
				},
				"required": []string{},
			}),
		},
	}
	tools = append(tools, listTodosTool)

	// get_todo tool
	getTodoTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_todo",
			Description: "Get detailed information about a specific todo task by its ID.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todo_id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the todo task to retrieve (e.g., 'todo_1', 'todo_abc123')",
					},
				},
				"required": []string{"todo_id"},
			}),
		},
	}
	tools = append(tools, getTodoTool)

	return tools
}

// CreateTodoToolExecutors creates the execution functions for todo tools
func CreateTodoToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["create_todo"] = handleCreateTodo
	executors["update_todo"] = handleUpdateTodo
	executors["complete_todo"] = handleCompleteTodo
	executors["list_todos"] = handleListTodos
	executors["get_todo"] = handleGetTodo

	return executors
}

// handleCreateTodo creates a new todo task
func handleCreateTodo(ctx context.Context, args map[string]interface{}) (string, error) {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return "", fmt.Errorf("title is required")
	}

	description, ok := args["description"].(string)
	if !ok || description == "" {
		return "", fmt.Errorf("description is required")
	}

	priority := "medium"
	if p, ok := args["priority"].(string); ok && p != "" {
		priority = p
	}

	assignedAgent := ""
	if a, ok := args["assigned_agent"].(string); ok {
		assignedAgent = a
	}

	// Generate a unique ID
	shortUUID := uuid.New().String()[:8]
	todoID := fmt.Sprintf("todo_%s", shortUUID)

	// Load existing todos file
	todoFile, err := loadTodosFile(ctx)
	if err != nil {
		// If file doesn't exist, create new
		todoFile = &TodoFile{
			Todos: []TodoItem{},
		}
	}

	// Create new todo
	now := time.Now()
	newTodo := TodoItem{
		ID:            todoID,
		Title:         title,
		Description:   description,
		Priority:      priority,
		Status:        "open",
		AssignedAgent: assignedAgent,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Add to list
	todoFile.Todos = append(todoFile.Todos, newTodo)
	todoFile.updateSummary()

	// Save file
	if err := saveTodosFile(ctx, todoFile); err != nil {
		return "", fmt.Errorf("failed to save todos file: %w", err)
	}

	// Return result
	result := map[string]interface{}{
		"success": true,
		"todo_id": todoID,
		"message": fmt.Sprintf("Created todo '%s' with ID: %s", title, todoID),
		"todo":    newTodo,
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleUpdateTodo updates an existing todo task
func handleUpdateTodo(ctx context.Context, args map[string]interface{}) (string, error) {
	todoID, ok := args["todo_id"].(string)
	if !ok || todoID == "" {
		return "", fmt.Errorf("todo_id is required")
	}

	// Load existing todos file
	todoFile, err := loadTodosFile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load todos file: %w", err)
	}

	// Find the todo
	todoIndex := -1
	for i, todo := range todoFile.Todos {
		if todo.ID == todoID {
			todoIndex = i
			break
		}
	}

	if todoIndex == -1 {
		return "", fmt.Errorf("todo with ID '%s' not found", todoID)
	}

	// Update fields
	if status, ok := args["status"].(string); ok && status != "" {
		todoFile.Todos[todoIndex].Status = status
	}
	if notes, ok := args["notes"].(string); ok && notes != "" {
		if todoFile.Todos[todoIndex].Notes != "" {
			todoFile.Todos[todoIndex].Notes += "\n" + notes
		} else {
			todoFile.Todos[todoIndex].Notes = notes
		}
	}
	if assignedAgent, ok := args["assigned_agent"].(string); ok && assignedAgent != "" {
		todoFile.Todos[todoIndex].AssignedAgent = assignedAgent
	}

	todoFile.Todos[todoIndex].UpdatedAt = time.Now()
	todoFile.updateSummary()

	// Save file
	if err := saveTodosFile(ctx, todoFile); err != nil {
		return "", fmt.Errorf("failed to save todos file: %w", err)
	}

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Updated todo '%s'", todoID),
		"todo":    todoFile.Todos[todoIndex],
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleCompleteTodo marks a todo as completed
func handleCompleteTodo(ctx context.Context, args map[string]interface{}) (string, error) {
	todoID, ok := args["todo_id"].(string)
	if !ok || todoID == "" {
		return "", fmt.Errorf("todo_id is required")
	}

	resultSummary, ok := args["result_summary"].(string)
	if !ok || resultSummary == "" {
		return "", fmt.Errorf("result_summary is required")
	}

	// Load existing todos file
	todoFile, err := loadTodosFile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load todos file: %w", err)
	}

	// Find the todo
	todoIndex := -1
	for i, todo := range todoFile.Todos {
		if todo.ID == todoID {
			todoIndex = i
			break
		}
	}

	if todoIndex == -1 {
		return "", fmt.Errorf("todo with ID '%s' not found", todoID)
	}

	// Mark as completed
	todoFile.Todos[todoIndex].Status = "completed"
	todoFile.Todos[todoIndex].Result = resultSummary
	todoFile.Todos[todoIndex].UpdatedAt = time.Now()
	todoFile.updateSummary()

	// Save file
	if err := saveTodosFile(ctx, todoFile); err != nil {
		return "", fmt.Errorf("failed to save todos file: %w", err)
	}

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Completed todo '%s'", todoID),
		"todo":    todoFile.Todos[todoIndex],
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleListTodos lists all todos
func handleListTodos(ctx context.Context, args map[string]interface{}) (string, error) {
	statusFilter := "all"
	if s, ok := args["status_filter"].(string); ok && s != "" {
		statusFilter = s
	}

	// Load existing todos file
	todoFile, err := loadTodosFile(ctx)
	if err != nil {
		// Return empty list if file doesn't exist
		todoFile = &TodoFile{
			Todos: []TodoItem{},
		}
	}

	// Filter todos if needed
	var filteredTodos []TodoItem
	if statusFilter == "all" || statusFilter == "" {
		filteredTodos = todoFile.Todos
	} else {
		for _, todo := range todoFile.Todos {
			if todo.Status == statusFilter {
				filteredTodos = append(filteredTodos, todo)
			}
		}
	}

	result := map[string]interface{}{
		"success":       true,
		"total_count":   len(todoFile.Todos),
		"filtered_count": len(filteredTodos),
		"filter":        statusFilter,
		"todos":         filteredTodos,
		"summary":       todoFile.Summary,
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleGetTodo gets a specific todo by ID
func handleGetTodo(ctx context.Context, args map[string]interface{}) (string, error) {
	todoID, ok := args["todo_id"].(string)
	if !ok || todoID == "" {
		return "", fmt.Errorf("todo_id is required")
	}

	// Load existing todos file
	todoFile, err := loadTodosFile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load todos file: %w", err)
	}

	// Find the todo
	for _, todo := range todoFile.Todos {
		if todo.ID == todoID {
			result := map[string]interface{}{
				"success": true,
				"todo":    todo,
			}
			resultJSON, _ := json.MarshalIndent(result, "", "  ")
			return string(resultJSON), nil
		}
	}

	return "", fmt.Errorf("todo with ID '%s' not found", todoID)
}

// loadTodosFile loads the todos.json file from the step execution path
// The step execution path is extracted from context
func loadTodosFile(ctx context.Context) (*TodoFile, error) {
	// Get the step execution path from context (set by orchestrator)
	stepPath := ctx.Value("step_execution_path")
	if stepPath == nil {
		return nil, fmt.Errorf("step_execution_path not found in context")
	}

	todosPath := fmt.Sprintf("%s/todos.json", stepPath)

	// Use the workspace file reader from context
	readFile := ctx.Value("read_workspace_file")
	if readFile == nil {
		return nil, fmt.Errorf("read_workspace_file function not found in context")
	}

	readFunc, ok := readFile.(func(context.Context, string) (string, error))
	if !ok {
		return nil, fmt.Errorf("read_workspace_file function has invalid type")
	}

	content, err := readFunc(ctx, todosPath)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return nil, fmt.Errorf("todos.json not found")
		}
		return nil, fmt.Errorf("failed to read todos.json: %w", err)
	}

	var todoFile TodoFile
	if err := json.Unmarshal([]byte(content), &todoFile); err != nil {
		return nil, fmt.Errorf("failed to parse todos.json: %w", err)
	}

	return &todoFile, nil
}

// saveTodosFile saves the todos.json file to the step execution path
func saveTodosFile(ctx context.Context, todoFile *TodoFile) error {
	// Get the step execution path from context
	stepPath := ctx.Value("step_execution_path")
	if stepPath == nil {
		return fmt.Errorf("step_execution_path not found in context")
	}

	todosPath := fmt.Sprintf("%s/todos.json", stepPath)

	// Use the workspace file writer from context
	writeFile := ctx.Value("write_workspace_file")
	if writeFile == nil {
		return fmt.Errorf("write_workspace_file function not found in context")
	}

	writeFunc, ok := writeFile.(func(context.Context, string, string) error)
	if !ok {
		return fmt.Errorf("write_workspace_file function has invalid type")
	}

	content, err := json.MarshalIndent(todoFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal todos: %w", err)
	}

	if err := writeFunc(ctx, todosPath, string(content)); err != nil {
		return fmt.Errorf("failed to write todos.json: %w", err)
	}

	return nil
}
