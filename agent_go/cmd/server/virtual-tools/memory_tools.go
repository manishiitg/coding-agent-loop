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

// MemoryFolderPath is the default workspace folder for agent memories
const MemoryFolderPath = "Chats/memories"

// MemoryFolderKey is the context key for overriding the memory folder (e.g. per-project memories)
const MemoryFolderKey delegationContextKey = "memory_folder"

// getMemoryFolder returns the effective memory folder from context, falling back to the default.
func getMemoryFolder(ctx context.Context) string {
	if folder, ok := ctx.Value(MemoryFolderKey).(string); ok && folder != "" {
		return folder
	}
	return MemoryFolderPath
}

// getMemoryPromptFile returns the prompt.md path for the effective memory folder.
func getMemoryPromptFile(ctx context.Context) string {
	return getMemoryFolder(ctx) + "/prompt.md"
}

// CreateMemoryTools creates the save_memory and recall_memory virtual tools
func CreateMemoryTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	saveMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "save_memory",
			Description: "Save important information to persistent memory with full detail. Spawns a background memory agent that intelligently categorizes and stores the memory in Chats/memories/. Returns immediately — the agent runs in the background. Use this for decisions (including reasoning and alternatives), preferences, learnings, debugging insights, architectural context, and important project context that should persist across sessions. Include relevant code snippets, file paths, and commands.",
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
			Description: "Search and retrieve relevant memories from persistent storage. Spawns a background memory agent that searches Chats/memories/ and returns a synthesized summary of matching memories. Returns immediately — you will be notified when results are ready.",
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
			Description: "Compress and consolidate persistent memories. Spawns a background agent that reads all memory files in Chats/memories/, identifies redundant/superseded/verbose entries, merges related content, and rewrites the files cleanly. Returns immediately — the agent runs in the background.",
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

// loadCustomMemoryPrompt reads {memoryFolder}/prompt.md via the workspace client.
// Returns empty string if the file doesn't exist or can't be read.
func loadCustomMemoryPrompt(ctx context.Context, wsClient *workspace.Client) string {
	if wsClient == nil {
		return ""
	}

	resultJSON, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: getMemoryPromptFile(ctx),
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

// handleSaveMemory spawns a background agent to save a memory to Chats/memories/
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

	memoryFolder := getMemoryFolder(ctx)
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTimestamp := now.Format("2006-01-02 15:04")
	dateDir := fmt.Sprintf("%s/%s", memoryFolder, currentDate)

	sb.WriteString(`## Your Task: Save a Memory

You are a memory management agent. Save the following information to the persistent memory system.

### Folder Structure
Memories are organized by date (chronological log) and by entity (fast lookup):
` + "```" + `
` + memoryFolder + `/
  prompt.md              ← Custom instructions (do not modify)
  entities.md            ← Entity registry (list of known entity names)
  entities/              ← Per-entity knowledge files
    auth-service.md      ← Everything known about "auth-service"
    postgresql.md        ← Everything known about "postgresql"
    {entity-name}.md     ← Lowercase, hyphenated entity names
  ` + currentDate + `/        ← Current date folder (chronological log)
    general.md           ← General memories
    decisions.md         ← Important decisions
    preferences.md       ← User preferences
    {custom}.md          ← Any category you decide
  2026-02-18/            ← Older dates (example)
    ...
` + "```" + `

### Phase 1 — Save to Date Folder:
1. Create the date directory: execute_shell_command(command: "mkdir -p ` + dateDir + `")
2. List existing files for today: execute_shell_command(command: "ls ` + dateDir + `/ 2>/dev/null || echo 'empty'")
3. If a relevant category file exists, read it to check for duplicates: execute_shell_command(command: "cat ` + dateDir + `/{file}.md")
4. Decide which category file to use (or create a new one)
5. Append the memory entry with a timestamp heading

To append to an existing file:
execute_shell_command(command: "cat >> ` + dateDir + `/{category}.md << 'MEMEOF'\n\n## ` + currentTimestamp + `\n{content}\nMEMEOF")

To create a new category file, write with a top-level heading first:
execute_shell_command(command: "cat > ` + dateDir + `/{category}.md << 'MEMEOF'\n# {Category Title}\n\n## ` + currentTimestamp + `\n{content}\nMEMEOF")

### Phase 2 — Update Entity Files:
6. Identify 0-3 key entities in the memory content. Entities are **specific named things**: projects, systems, services, technologies, people, features. NOT generic terms like "code", "bug", "task", "issue".
7. For each entity identified:
   a. Normalize the name: lowercase, spaces → hyphens (e.g., "Auth Service" → "auth-service")
   b. Create entities dir if needed: execute_shell_command(command: "mkdir -p ` + memoryFolder + `/entities")
   c. Read existing entity file if it exists: execute_shell_command(command: "cat ` + memoryFolder + `/entities/{entity}.md 2>/dev/null || echo 'new entity'")
   d. Append a timestamped entry with the relevant excerpt from this memory:
      execute_shell_command(command: "cat >> ` + memoryFolder + `/entities/{entity}.md << 'MEMEOF'\n\n## ` + currentTimestamp + `\n{relevant excerpt}\nMEMEOF")
      Or create if new: execute_shell_command(command: "cat > ` + memoryFolder + `/entities/{entity}.md << 'MEMEOF'\n# {Entity Display Name}\n\n## ` + currentTimestamp + `\n{relevant excerpt}\nMEMEOF")
   e. Register in entity registry (only if not already present):
      execute_shell_command(command: "grep -qF '{entity}' ` + memoryFolder + `/entities.md 2>/dev/null || echo '- {entity}' >> ` + memoryFolder + `/entities.md")
      If entities.md doesn't exist yet, create it first:
      execute_shell_command(command: "[ -f ` + memoryFolder + `/entities.md ] || printf '# Entity Registry\n\nKnown entities (each has a file in entities/):\n' > ` + memoryFolder + `/entities.md")

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
- Entity excerpts should be self-contained summaries, not just pointers to the date entry

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
	// Restrict writes to the chat-backed plan folder root
	bgCtx = context.WithValue(bgCtx, PlanFolderKey, PlanFileFolderPath)

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

	memoryFolder := getMemoryFolder(ctx)
	sb.WriteString(`## Your Task: Recall Memories

You are a memory retrieval agent. Search the persistent memory system at ` + memoryFolder + `/ and return relevant information.

### Folder Structure
` + "```" + `
` + memoryFolder + `/
  index.md               ← High-level snapshot (key decisions, project state, active entities)
  entities.md            ← Entity registry (list of known entity names)
  entities/              ← Per-entity knowledge files (fast lookup)
    auth-service.md
    postgresql.md
    ...
  2026-02-19/            ← Date folders (chronological log)
    general.md, decisions.md, ...
  2026-02-18/
    ...
` + "```" + `

### Priority 0 — Index Check (if query is broad or orientation-seeking):
0. If the query is broad ("what do I know", "what was decided", "give me context", "index") or you need orientation:
   Read index.md: execute_shell_command(command: "cat ` + memoryFolder + `/index.md 2>/dev/null || echo 'no index yet — run compress_memory to generate one'")
   Return index.md contents directly. Only proceed to entity/date search if more detail is needed.

### Priority 1 — Entity Lookup (fast path):
1. Read entity registry: execute_shell_command(command: "cat ` + memoryFolder + `/entities.md 2>/dev/null || echo 'no entity registry'")
2. Check if the query matches or relates to any known entity name
3. If a match is found, read the entity file directly: execute_shell_command(command: "cat ` + memoryFolder + `/entities/{entity-name}.md")
4. Also try fuzzy match — list entity files and look for partial name matches: execute_shell_command(command: "ls ` + memoryFolder + `/entities/ 2>/dev/null | grep -i '{keyword}'")

### Priority 2 — Date Search (fallback and supplement):
5. List date folders: execute_shell_command(command: "ls -d ` + memoryFolder + `/[0-9]*/ 2>/dev/null | sort -r || echo 'no date memories'")
6. Search across ALL dates for matching content: execute_shell_command(command: "grep -ril '{keyword}' ` + memoryFolder + `/ --exclude-dir=entities 2>/dev/null")
7. Read matching date-folder files: execute_shell_command(command: "cat ` + memoryFolder + `/{date}/{file}.md")

### Synthesis:
8. Combine findings from entity files and date files into a clear summary
9. If no relevant memories found anywhere, say so clearly

### Guidelines:
- Always check entity files first — they are curated and faster to read
- Search ALL date folders, not just today — older memories may be relevant
- Use grep -ri for case-insensitive recursive search
- For broad queries, try multiple keywords or patterns
- Include the source (entity file or date folder) when reporting
- Prioritize entity files for "what do I know about X" queries
- Prioritize recent date entries for "what happened recently" queries
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
	// Restrict writes to the chat-backed plan folder root (recall mostly reads, but keep consistent)
	bgCtx = context.WithValue(bgCtx, PlanFolderKey, PlanFileFolderPath)

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

	memoryFolder := getMemoryFolder(ctx)
	sb.WriteString(`## Your Task: Compress and Consolidate Memories

You are a memory compression agent. Your job is to read all memory files, identify redundancies, and rewrite them cleanly.

### Phase 1 — Inventory
1. List all date folders: execute_shell_command(command: "ls -d ` + memoryFolder + `/[0-9]*/ 2>/dev/null | sort || echo 'no date memories'")
2. List entity files: execute_shell_command(command: "ls ` + memoryFolder + `/entities/ 2>/dev/null || echo 'no entity files'")
3. For each date folder, list files: execute_shell_command(command: "ls ` + memoryFolder + `/{date}/ 2>/dev/null")
4. Read ALL .md files in date folders (skip prompt.md): execute_shell_command(command: "cat ` + memoryFolder + `/{date}/{file}.md")
5. Read ALL entity files: execute_shell_command(command: "cat ` + memoryFolder + `/entities/{entity}.md")

### Phase 2 — Analyze Date Files
Review all date entries and identify:
- **Superseded entries**: Older decisions/preferences replaced by newer ones (keep the newer one only)
- **Duplicate entries**: Same information saved multiple times (keep the best version)
- **Verbose entries**: Entries that can be made more concise without losing meaning
- **Related entries**: Entries across files/dates that should be merged together
- **Misplaced categories**: Entries in the wrong category file

### Phase 3a — Analyze Entity Files
Review each entity file and identify:
- **Outdated facts**: Earlier entries contradicted by later ones (remove outdated, keep current)
- **Duplicate entries**: Same info saved to entity file multiple times (deduplicate)
- **Stale entities**: Entities with no meaningful content (remove from entities/ and from entities.md registry)
- **Missing entities**: Important named things mentioned repeatedly in date files but not yet in entities/ (create entity files for them)
`)

	if focus != "" {
		sb.WriteString("\n### Focus Area\nOnly compress memories related to: ")
		sb.WriteString(focus)
		sb.WriteString("\nLeave unrelated memories untouched.\n")
	}

	sb.WriteString(`
### Phase 3b — Rewrite Date Files
For each date file that needs changes:
1. Rewrite the entire file with consolidated content:
   execute_shell_command(command: "cat > ` + memoryFolder + `/{date}/{category}.md << 'MEMEOF'\n# {Category Title}\n\n## {timestamp}\n{consolidated content}\nMEMEOF")
2. Remove empty files: execute_shell_command(command: "rm ` + memoryFolder + `/{date}/{file}.md")
3. Remove empty date folders: execute_shell_command(command: "rmdir ` + memoryFolder + `/{date} 2>/dev/null")

### Phase 3c — Rewrite Entity Files
For each entity file that needs changes:
1. Rewrite with deduplicated, consolidated content:
   execute_shell_command(command: "cat > ` + memoryFolder + `/entities/{entity}.md << 'MEMEOF'\n# {Entity Display Name}\n\n## {timestamp}\n{consolidated content}\nMEMEOF")
2. Remove stale entity files: execute_shell_command(command: "rm ` + memoryFolder + `/entities/{entity}.md")
3. Update entities.md registry to remove stale entries:
   execute_shell_command(command: "grep -vF '{stale-entity}' ` + memoryFolder + `/entities.md > /tmp/entities_tmp.md && mv /tmp/entities_tmp.md ` + memoryFolder + `/entities.md")

### Phase 4 — Regenerate index.md
After all date and entity files are finalized, regenerate ` + memoryFolder + `/index.md from scratch.
This is the authoritative high-level snapshot the agent reads at session start and before decisions.

Read everything you've already processed above (no need to re-read files), then write:

execute_shell_command(command: "cat > ` + memoryFolder + `/index.md << 'MEMEOF'\n{content}\nMEMEOF")

The index.md must contain:

# Memory Index
Last updated: {current timestamp}

## Key Decisions
<!-- Settled decisions the agent must NOT re-litigate without checking memory first -->
- {decision}: {one-line summary} ({date})
- ...

## Project State
<!-- What is in progress, what is complete, current direction -->
- In progress: ...
- Completed: ...

## Active Entities
<!-- Comma-separated list of all entities in entities/ -->
{entity1}, {entity2}, ...

## What to Check Before Acting
<!-- Topics/entities worth recalling before making changes in those areas -->
- {topic} → recall "{keyword}" before making changes here

### Rules
- **NEVER modify prompt.md** — it contains user-editable instructions
- **Preserve the daily folder structure** (` + memoryFolder + `/YYYY-MM-DD/)
- **Preserve the timestamp heading format**: ## YYYY-MM-DD HH:MM
- **When merging entries across dates**, place in the most recent relevant date
- **When in doubt, keep the information** — it's better to be slightly verbose than to lose data
- **Maintain category file organization** — keep decisions.md, preferences.md, general.md etc. as separate files
- **Keep entities.md registry in sync** — if you add/remove entity files, update the registry
- **Always regenerate index.md as the final step** — it must reflect the post-compression state
- After all changes, provide a summary of what was compressed/merged/removed and what's now in index.md
`)

	// Use background delegate for async execution
	bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc)
	if !ok || bgDelegate == nil {
		return "", fmt.Errorf("background delegation not available — memory tools require multi-agent mode")
	}

	// Use medium reasoning level — compression requires judgment about what to keep/merge
	bgCtx := context.WithValue(ctx, ReasoningLevelKey, "medium")
	// Restrict writes to the chat-backed plan folder root
	bgCtx = context.WithValue(bgCtx, PlanFolderKey, PlanFileFolderPath)

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

// GetMemoryInstructions returns system prompt instructions for the memory system.
// Pass memoryFolder="" to use the default (Chats/memories).
func GetMemoryInstructions(memoryFolder string) string {
	if memoryFolder == "" {
		memoryFolder = MemoryFolderPath
	}
	return `
## Memory System

You have a persistent memory system that survives across sessions. Use it to build knowledge over time.

### Tools
- **save_memory(content, context?)** — Starts a background agent to save important information. Returns immediately.
- **recall_memory(query)** — Starts a background agent to search and retrieve relevant memories. You will be notified when results are ready.
- **compress_memory(focus?)** — Starts a background agent to consolidate, deduplicate, and compress memories. Optionally focus on a specific topic.

### Storage Structure
` + "```" + `
` + memoryFolder + `/
  index.md               ← HIGH-LEVEL SNAPSHOT — read this first (regenerated by compress_memory)
  entities.md            ← Entity registry
  entities/              ← Per-entity knowledge (fast lookup)
    auth-service.md
    postgresql.md
  YYYY-MM-DD/            ← Date folders (chronological log)
    decisions.md
    general.md
  prompt.md              ← User-editable custom instructions (do not modify)
` + "```" + `

### index.md — Read Before Acting
**index.md is your primary orientation file.** It contains:
- **Key Decisions** — settled decisions you must not re-litigate without checking memory
- **Project State** — what is in progress, what is complete
- **Active Entities** — all known entities (so you know what to recall)
- **What to Check Before Acting** — topics that warrant a deeper recall_memory before making changes

**When to read index.md:**
- At the start of a new session or task
- Before making architectural or technology decisions
- Before planning work that touches known entities or past decisions
- When the user references past work ("like before", "as we discussed", "continue with")

To read it: use recall_memory(query: "index") — the recall agent will check index.md directly.
Or reference it explicitly in a shell command if you have workspace access.

**index.md is only as fresh as the last compress_memory run.** For things saved after the last compression, use recall_memory to search date and entity files.

### Recall Guidelines
- **Read index.md first** — it tells you what decisions have been made and what entities exist, so you know what to look for.
- **Use recall_memory for depth** — after reading index.md, recall specific entities or topics for full detail.
- **Before planning or decisions**: If index.md lists a relevant entity or decision, always recall_memory for full context before proceeding.
- **When the user references past work**: Phrases like "like before", "as we discussed", "continue with", or specific project/feature names → read index.md, then recall.
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
