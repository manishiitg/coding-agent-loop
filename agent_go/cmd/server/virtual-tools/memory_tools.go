package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workspace"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// MemoryFolderPath is the workspace folder for agent memories
const MemoryFolderPath = "Plans/memories"

// MemoryPromptFile is the optional user-editable file for custom memory instructions
const MemoryPromptFile = "Plans/memories/prompt.md"

// CreateMemoryTools creates the save_memory and recall_memory virtual tools
func CreateMemoryTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	saveMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "save_memory",
			Description: "Save important information to persistent memory. Spawns a background memory agent that intelligently categorizes and stores the memory in Plans/memories/. Returns immediately — the agent runs in the background. Use this for decisions, preferences, learnings, and important project context that should persist across sessions.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The information to save. Be specific — memories should be useful without additional context.",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional additional context about why this is being saved or how it relates to the current work.",
					},
				},
				"required": []string{"content"},
			}),
		},
	}
	tools = append(tools, saveMemoryTool)

	recallMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "recall_memory",
			Description: "Search and retrieve relevant memories from persistent storage. Spawns a background memory agent that searches Plans/memories/ and returns a synthesized summary of matching memories. Returns immediately — you will be notified when results are ready.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "What to search for in memories. Can be a topic, keyword, or question.",
					},
				},
				"required": []string{"query"},
			}),
		},
	}
	tools = append(tools, recallMemoryTool)

	return tools
}

// CreateMemoryToolExecutors creates the execution functions for memory tools
func CreateMemoryToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["save_memory"] = handleSaveMemory
	executors["recall_memory"] = handleRecallMemory

	return executors
}

// loadCustomMemoryPrompt reads Plans/memories/prompt.md via the workspace client.
// Returns empty string if the file doesn't exist or can't be read.
func loadCustomMemoryPrompt(ctx context.Context, wsClient *workspace.Client) string {
	if wsClient == nil {
		return ""
	}

	resultJSON, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: MemoryPromptFile,
	})
	if err != nil {
		// File doesn't exist — this is expected and normal
		return ""
	}

	var readData map[string]interface{}
	if json.Unmarshal([]byte(resultJSON), &readData) != nil {
		return ""
	}

	content, _ := readData["content"].(string)
	return strings.TrimSpace(content)
}

// handleSaveMemory spawns a background agent to save a memory to Plans/memories/
func handleSaveMemory(ctx context.Context, args map[string]interface{}) (string, error) {
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return "", fmt.Errorf("content is required")
	}
	additionalContext, _ := args["context"].(string)

	// Get workspace client for reading prompt.md
	wsClient, _ := ctx.Value(WorkspaceClientKey).(*workspace.Client)

	// Load custom instructions from prompt.md
	customPrompt := loadCustomMemoryPrompt(ctx, wsClient)

	// Build sub-agent instruction
	var sb strings.Builder

	if customPrompt != "" {
		sb.WriteString("## Custom Memory Instructions\n")
		sb.WriteString(customPrompt)
		sb.WriteString("\n\n")
	}

	now := time.Now()
	currentMonth := now.Format("2006-01")
	currentTimestamp := now.Format("2006-01-02 15:04")
	monthDir := fmt.Sprintf("Plans/memories/%s", currentMonth)

	sb.WriteString(`## Your Task: Save a Memory

You are a memory management agent. Save the following information to the persistent memory system.

### Folder Structure
Memories are organized by month and category:
` + "```" + `
Plans/memories/
  prompt.md              ← Custom instructions (do not modify)
  ` + currentMonth + `/           ← Current month folder
    general.md           ← General memories
    decisions.md         ← Important decisions
    preferences.md       ← User preferences
    {custom}.md          ← Any category you decide
  2026-01/               ← Older months (example)
    ...
` + "```" + `

### Steps:
1. Create the month directory: execute_shell_command(command: "mkdir -p ` + monthDir + `", working_directory: ".")
2. List existing files in the current month: execute_shell_command(command: "ls ` + monthDir + `/ 2>/dev/null || echo 'empty'", working_directory: ".")
3. If a relevant category file exists, read it to check for duplicates: execute_shell_command(command: "cat ` + monthDir + `/{file}.md", working_directory: ".")
4. Decide which category file to use (or create a new one)
5. Append the memory entry with a timestamp heading

### Writing:
To append to an existing file:
execute_shell_command(command: "cat >> ` + monthDir + `/{category}.md << 'MEMEOF'\n\n## ` + currentTimestamp + `\n{content}\nMEMEOF", working_directory: ".")

To create a new category file, write with a top-level heading first:
execute_shell_command(command: "cat > ` + monthDir + `/{category}.md << 'MEMEOF'\n# {Category Title}\n\n## ` + currentTimestamp + `\n{content}\nMEMEOF", working_directory: ".")

### Rules:
- Always write to the current month folder: ` + monthDir + `/
- Check for duplicates before saving
- Each entry must have a heading: ## YYYY-MM-DD HH:MM
- Keep entries concise but self-contained
- Prefer appending to existing category files
- Do NOT modify prompt.md or files in other month folders

### Current timestamp: ` + currentTimestamp + `

### Memory to save:
`)
	sb.WriteString(content)

	if additionalContext != "" {
		sb.WriteString("\n\n### Additional context:\n")
		sb.WriteString(additionalContext)
	}

	// Use background delegate for async execution
	bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc)
	if !ok || bgDelegate == nil {
		return "", fmt.Errorf("background delegation not available — memory tools require multi-agent mode")
	}

	// Set low reasoning level for memory ops (simple read/write)
	bgCtx := context.WithValue(ctx, ReasoningLevelKey, "low")
	// Restrict writes to Plans folder
	bgCtx = context.WithValue(bgCtx, PlanFolderKey, "Plans")

	agentName := "Save Memory"
	log.Printf("[MEMORY] Starting background save_memory agent: %s", truncateString(content, 80))

	agentID, err := bgDelegate(bgCtx, agentName, sb.String())
	if err != nil {
		return "", fmt.Errorf("failed to start memory save agent: %w", err)
	}

	log.Printf("[MEMORY] Started background save_memory agent (ID: %s)", agentID)

	result := map[string]interface{}{
		"async":    true,
		"agent_id": agentID,
		"name":     agentName,
		"status":   "running",
		"message":  fmt.Sprintf("Memory save agent started in background. You'll be notified when it completes."),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleRecallMemory spawns a background agent to search and retrieve memories
func handleRecallMemory(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}

	// Get workspace client for reading prompt.md
	wsClient, _ := ctx.Value(WorkspaceClientKey).(*workspace.Client)

	// Load custom instructions from prompt.md
	customPrompt := loadCustomMemoryPrompt(ctx, wsClient)

	// Build sub-agent instruction
	var sb strings.Builder

	if customPrompt != "" {
		sb.WriteString("## Custom Memory Instructions\n")
		sb.WriteString(customPrompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString(`## Your Task: Recall Memories

You are a memory retrieval agent. Search the persistent memory system at Plans/memories/ and return relevant information.

### Folder Structure
Memories are organized by month (YYYY-MM/) with category files inside each month.
` + "```" + `
Plans/memories/
  2026-02/general.md, decisions.md, ...
  2026-01/general.md, ...
  ...
` + "```" + `

### Steps:
1. List month folders: execute_shell_command(command: "ls -d Plans/memories/*/ 2>/dev/null | sort -r || echo 'no memories'", working_directory: ".")
2. Search across ALL months for matching content: execute_shell_command(command: "grep -ril '{keyword}' Plans/memories/ 2>/dev/null", working_directory: ".")
3. Read matching files: execute_shell_command(command: "cat Plans/memories/{month}/{file}.md", working_directory: ".")
4. Synthesize findings into a clear summary
5. If no relevant memories found, say so clearly

### Guidelines:
- Search ALL month folders, not just the current one — older memories may be relevant
- Use grep -ri for case-insensitive recursive search
- For broad queries, try multiple keywords or patterns
- Include the date/month of each memory when reporting
- Prioritize recent memories but include older ones if relevant
- If the memories folder doesn't exist or is empty, report that no memories have been saved yet

### Search query:
`)
	sb.WriteString(query)

	// Use background delegate for async execution
	bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc)
	if !ok || bgDelegate == nil {
		return "", fmt.Errorf("background delegation not available — memory tools require multi-agent mode")
	}

	// Set low reasoning level for memory ops (simple search/read)
	bgCtx := context.WithValue(ctx, ReasoningLevelKey, "low")
	// Restrict writes to Plans folder (recall mostly reads, but keep consistent)
	bgCtx = context.WithValue(bgCtx, PlanFolderKey, "Plans")

	agentName := "Recall Memory"
	log.Printf("[MEMORY] Starting background recall_memory agent: %s", truncateString(query, 80))

	agentID, err := bgDelegate(bgCtx, agentName, sb.String())
	if err != nil {
		return "", fmt.Errorf("failed to start memory recall agent: %w", err)
	}

	log.Printf("[MEMORY] Started background recall_memory agent (ID: %s)", agentID)

	result := map[string]interface{}{
		"async":    true,
		"agent_id": agentID,
		"name":     agentName,
		"status":   "running",
		"message":  fmt.Sprintf("Memory recall agent started in background. You'll be notified when results are ready."),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// GetMemoryInstructions returns system prompt instructions for the memory system
func GetMemoryInstructions() string {
	return `
## Memory System

You have a persistent memory system that survives across sessions. Use it to build knowledge over time.

### Tools
- **save_memory(content, context?)** — Starts a background agent to save important information. Returns immediately.
- **recall_memory(query)** — Starts a background agent to search and retrieve relevant memories. You will be notified when results are ready.

### Storage Structure
Memories are organized by month in Plans/memories/:
- Plans/memories/YYYY-MM/{category}.md — e.g., Plans/memories/2026-02/decisions.md
- Plans/memories/prompt.md — Optional user-editable custom instructions for memory agents
- Old months naturally separate from recent ones, making it easy to manage memory over time

### When to Use
- **Save**: decisions made, user preferences, learnings, important project context, patterns discovered
- **Recall**: start of session (check for relevant context), before planning, when you need past decisions

### Guidelines
- Save only important, non-trivial information
- Be specific in content — memories should be useful without additional context
- Use recall before making decisions that might have been made before
- Both tools return immediately — you will be notified when the background agent completes
`
}
