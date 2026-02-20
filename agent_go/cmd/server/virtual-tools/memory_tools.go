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
			Description: "Save important information to persistent memory with full detail. Spawns a background memory agent that intelligently categorizes and stores the memory in Plans/memories/. Returns immediately — the agent runs in the background. Use this for decisions (including reasoning and alternatives), preferences, learnings, debugging insights, architectural context, and important project context that should persist across sessions. Include relevant code snippets, file paths, and commands.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The information to save. Be detailed and thorough — include the reasoning, context, alternatives considered, relevant code/paths/commands, and the final outcome. Memories should be rich enough to fully reconstruct the situation later.",
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

	compressMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "compress_memory",
			Description: "Compress and consolidate persistent memories. Spawns a background agent that reads all memory files in Plans/memories/, identifies redundant/superseded/verbose entries, merges related content, and rewrites the files cleanly. Returns immediately — the agent runs in the background.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"focus": map[string]interface{}{
						"type":        "string",
						"description": "Optional topic to focus compression on. If provided, only memories related to this topic will be compressed. If omitted, all memories are compressed.",
					},
				},
				"required": []string{},
			}),
		},
	}
	tools = append(tools, compressMemoryTool)

	return tools
}

// CreateMemoryToolExecutors creates the execution functions for memory tools
func CreateMemoryToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["save_memory"] = handleSaveMemory
	executors["recall_memory"] = handleRecallMemory
	executors["compress_memory"] = handleCompressMemory

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
	currentDate := now.Format("2006-01-02")
	currentTimestamp := now.Format("2006-01-02 15:04")
	dateDir := fmt.Sprintf("Plans/memories/%s", currentDate)

	sb.WriteString(`## Your Task: Save a Memory

You are a memory management agent. Save the following information to the persistent memory system.

### Folder Structure
Memories are organized by date and category:
` + "```" + `
Plans/memories/
  prompt.md              ← Custom instructions (do not modify)
  ` + currentDate + `/        ← Current date folder
    general.md           ← General memories
    decisions.md         ← Important decisions
    preferences.md       ← User preferences
    {custom}.md          ← Any category you decide
  2026-02-18/            ← Older dates (example)
    ...
` + "```" + `

### Steps:
1. Create the date directory: execute_shell_command(command: "mkdir -p ` + dateDir + `", working_directory: ".")
2. List existing files for today: execute_shell_command(command: "ls ` + dateDir + `/ 2>/dev/null || echo 'empty'", working_directory: ".")
3. If a relevant category file exists, read it to check for duplicates: execute_shell_command(command: "cat ` + dateDir + `/{file}.md", working_directory: ".")
4. Decide which category file to use (or create a new one)
5. Append the memory entry with a timestamp heading

### Writing:
To append to an existing file:
execute_shell_command(command: "cat >> ` + dateDir + `/{category}.md << 'MEMEOF'\n\n## ` + currentTimestamp + `\n{content}\nMEMEOF", working_directory: ".")

To create a new category file, write with a top-level heading first:
execute_shell_command(command: "cat > ` + dateDir + `/{category}.md << 'MEMEOF'\n# {Category Title}\n\n## ` + currentTimestamp + `\n{content}\nMEMEOF", working_directory: ".")

### Rules:
- Always write to today's folder: ` + dateDir + `/
- Check for duplicates before saving
- Each entry must have a heading: ## YYYY-MM-DD HH:MM
- **Write detailed, thorough memories** — include the WHY, the reasoning, alternatives considered, and how the decision/learning connects to the broader project
- Include relevant code snippets, file paths, command examples, or configuration details when applicable
- Capture the full context: what was the situation, what was tried, what worked/failed, and what was the final outcome
- Structure longer entries with sub-headings (###) for readability
- Prefer appending to existing category files
- Do NOT modify prompt.md or files in other date folders

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

	// Set medium reasoning level for memory save — needs judgment to write detailed, well-structured memories
	bgCtx := context.WithValue(ctx, ReasoningLevelKey, "medium")
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
Memories are organized by date (YYYY-MM-DD/) with category files inside each folder.
` + "```" + `
Plans/memories/
  2026-02-19/general.md, decisions.md, ...
  2026-02-18/general.md, ...
  ...
` + "```" + `

### Steps:
1. List date folders: execute_shell_command(command: "ls -d Plans/memories/*/ 2>/dev/null | sort -r || echo 'no memories'", working_directory: ".")
2. Search across ALL dates for matching content: execute_shell_command(command: "grep -ril '{keyword}' Plans/memories/ 2>/dev/null", working_directory: ".")
3. Read matching files: execute_shell_command(command: "cat Plans/memories/{date}/{file}.md", working_directory: ".")
4. Synthesize findings into a clear summary
5. If no relevant memories found, say so clearly

### Guidelines:
- Search ALL date folders, not just today — older memories may be relevant
- Use grep -ri for case-insensitive recursive search
- For broad queries, try multiple keywords or patterns
- Include the date folder of each memory when reporting
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

// handleCompressMemory spawns a background agent to consolidate and deduplicate memories
func handleCompressMemory(ctx context.Context, args map[string]interface{}) (string, error) {
	focus, _ := args["focus"].(string)

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

	sb.WriteString(`## Your Task: Compress and Consolidate Memories

You are a memory compression agent. Your job is to read all memory files, identify redundancies, and rewrite them cleanly.

### Phase 1 — Inventory
1. List all date folders: execute_shell_command(command: "ls -d Plans/memories/*/ 2>/dev/null | sort || echo 'no memories'", working_directory: ".")
2. For each date folder, list files: execute_shell_command(command: "ls Plans/memories/{date}/ 2>/dev/null", working_directory: ".")
3. Read ALL .md files (skip prompt.md): execute_shell_command(command: "cat Plans/memories/{date}/{file}.md", working_directory: ".")

### Phase 2 — Analyze
Review all entries and identify:
- **Superseded entries**: Older decisions/preferences replaced by newer ones (keep the newer one only)
- **Duplicate entries**: Same information saved multiple times (keep the best version)
- **Verbose entries**: Entries that can be made more concise without losing meaning
- **Related entries**: Entries across files/dates that should be merged together
- **Misplaced categories**: Entries in the wrong category file
`)

	if focus != "" {
		sb.WriteString("\n### Focus Area\nOnly compress memories related to: ")
		sb.WriteString(focus)
		sb.WriteString("\nLeave unrelated memories untouched.\n")
	}

	sb.WriteString(`
### Phase 3 — Rewrite
For each file that needs changes:
1. Rewrite the entire file with consolidated content:
   execute_shell_command(command: "cat > Plans/memories/{date}/{category}.md << 'MEMEOF'\n# {Category Title}\n\n## {timestamp}\n{consolidated content}\nMEMEOF", working_directory: ".")
2. Remove empty files: execute_shell_command(command: "rm Plans/memories/{date}/{file}.md", working_directory: ".")
3. Remove empty date folders: execute_shell_command(command: "rmdir Plans/memories/{date} 2>/dev/null", working_directory: ".")

### Rules
- **NEVER modify prompt.md** — it contains user-editable instructions
- **Preserve the daily folder structure** (Plans/memories/YYYY-MM-DD/)
- **Preserve the timestamp heading format**: ## YYYY-MM-DD HH:MM
- **When merging entries across dates**, place in the most recent relevant date
- **When in doubt, keep the information** — it's better to be slightly verbose than to lose data
- **Maintain category file organization** — keep decisions.md, preferences.md, general.md etc. as separate files
- After all changes, provide a summary of what was compressed/merged/removed
`)

	// Use background delegate for async execution
	bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc)
	if !ok || bgDelegate == nil {
		return "", fmt.Errorf("background delegation not available — memory tools require multi-agent mode")
	}

	// Use medium reasoning level — compression requires judgment about what to keep/merge
	bgCtx := context.WithValue(ctx, ReasoningLevelKey, "medium")
	// Restrict writes to Plans folder
	bgCtx = context.WithValue(bgCtx, PlanFolderKey, "Plans")

	agentName := "Compress Memory"
	if focus != "" {
		agentName = fmt.Sprintf("Compress Memory (%s)", truncateString(focus, 40))
	}
	log.Printf("[MEMORY] Starting background compress_memory agent (focus: %s)", focus)

	agentID, err := bgDelegate(bgCtx, agentName, sb.String())
	if err != nil {
		return "", fmt.Errorf("failed to start memory compression agent: %w", err)
	}

	log.Printf("[MEMORY] Started background compress_memory agent (ID: %s)", agentID)

	result := map[string]interface{}{
		"async":    true,
		"agent_id": agentID,
		"name":     agentName,
		"status":   "running",
		"message":  "Memory compression agent started in background. You'll be notified when it completes.",
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
- **compress_memory(focus?)** — Starts a background agent to consolidate, deduplicate, and compress memories. Optionally focus on a specific topic.

### Storage Structure
Memories are organized by date in Plans/memories/:
- Plans/memories/YYYY-MM-DD/{category}.md — e.g., Plans/memories/2026-02-19/decisions.md
- Plans/memories/prompt.md — Optional user-editable custom instructions for memory agents
- Each day naturally separates memories over time, making history easier to scan

### Recall Guidelines
- **New conversations**: Check memory for relevant context when starting work on a topic that may have prior history. This is your only way to know what happened in previous sessions.
- **Before planning or decisions**: Check memory before making architectural choices or technology decisions — this may have been decided before.
- **When the user references past work**: Phrases like "like before", "as we discussed", "continue with", or mentions of specific project/feature names should trigger a recall.
- Recall is async and cheap — when in doubt, recall rather than miss important context.

### Save Rules
- Save decisions made, user preferences, learnings, important project context, debugging insights, and patterns discovered.
- Save only important, non-trivial information — but when you do save, **be detailed and thorough**.
- Include the full context: WHY a decision was made, what alternatives were considered, what worked/failed, relevant code snippets, file paths, commands, and configuration details.
- Write memories as if explaining to a future version of yourself that has no context about this session.
- A detailed memory today saves significant re-investigation time in future sessions.

### Compression
- Use compress_memory when memories have accumulated over multiple sessions and may contain redundant or superseded entries.
- The compression agent reads all files, merges related entries, removes outdated ones, and rewrites files cleanly.
- You can optionally focus compression on a specific topic.

### General
- All memory tools return immediately — you will be notified when the background agent completes.
`
}
