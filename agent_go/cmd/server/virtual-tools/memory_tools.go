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

// MemoryFolderPath is the fallback per-user memories folder when session context is unavailable.
// Always prefer getMemoryFolder(ctx) which reads the session-scoped per-user path.
const MemoryFolderPath = "_users/default/memories"

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
			Description: "Save important information to persistent memory with full detail. Spawns a background memory agent that intelligently categorizes and stores the memory in memories/. Returns immediately — the agent runs in the background. Use this for decisions (including reasoning and alternatives), preferences, learnings, debugging insights, architectural context, and important project context that should persist across sessions. Include relevant code snippets, file paths, and commands.",
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
			Description: "Search and retrieve relevant memories from persistent storage. Spawns a background memory agent that searches memories/ and returns a synthesized summary of matching memories. Returns immediately — you will be notified when results are ready.",
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

	enrichMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "enrich_memory",
			Description: "Enrich persistent memory by distilling past chats into memories, then consolidating all memory files. Spawns a background agent that (1) reads every session in chat_history/, extracts insights into today's date folder and entity files, then deletes chat sessions older than delete_older_than_days, and (2) reads all memory files, merges related/duplicate entries, removes superseded ones, and regenerates index.md. Returns immediately — the agent runs in the background.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"focus": map[string]interface{}{
						"type":        "string",
						"description": "Optional topic to focus on. If provided, only memories related to this topic are consolidated. Chat-history extraction still runs for all sessions. If omitted, all memories are processed.",
					},
					"delete_older_than_days": map[string]interface{}{
						"type":        "number",
						"description": "Delete chat sessions older than this many days after extraction (default 7). Set to 0 to disable deletion.",
					},
				},
				"required": []string{},
			}),
		},
	}
	tools = append(tools, enrichMemoryTool)

	return tools
}

// CreateMemoryToolExecutors creates the execution functions for memory tools
func CreateMemoryToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["save_memory"] = handleSaveMemory
	executors["recall_memory"] = handleRecallMemory
	executors["enrich_memory"] = handleEnrichMemory

	return executors
}

// loadCustomMemoryPrompt reads {memoryFolder}/prompt.md via the workspace client.
// Returns empty string if the file doesn't exist or can't be read.
func loadCustomMemoryPrompt(ctx context.Context, wsClient *workspace.Client) string {
	if wsClient == nil {
		return ""
	}

	readResult, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: getMemoryPromptFile(ctx),
	})
	if err != nil {
		// File doesn't exist — this is expected and normal
		return ""
	}

	return strings.TrimSpace(readResult.Content)
}

// handleSaveMemory spawns a background agent to save a memory to memories/
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
- **Do NOT store facts queryable from workflows, MCP servers, or APIs** (e.g. PR status, channel lists, live metrics, calendar events). Memory is for user preferences, communication style, recurring use cases, dislikes, patterns, decisions/reasoning, and project context — things that cannot be rediscovered from live data.

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
   Read index.md: execute_shell_command(command: "cat ` + memoryFolder + `/index.md 2>/dev/null || echo 'no index yet — run enrich_memory to generate one'")
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

// handleEnrichMemory spawns a background agent that (a) distils chat history into memories
// and then (b) consolidates and deduplicates the memory files.
func handleEnrichMemory(ctx context.Context, args map[string]interface{}) (string, error) {
	focus, _ := args["focus"].(string)

	deleteOlderThanDays := 7
	if v, ok := args["delete_older_than_days"].(float64); ok {
		deleteOlderThanDays = int(v)
	}
	if deleteOlderThanDays < 0 {
		deleteOlderThanDays = 0
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
	// chat_history lives as a sibling of the memory folder (e.g. _users/{id}/chat_history).
	chatHistoryFolder := strings.TrimSuffix(memoryFolder, "/memories") + "/chat_history"
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	dateDir := fmt.Sprintf("%s/%s", memoryFolder, currentDate)

	sb.WriteString(`## Your Task: Enrich Memory (Distil Chats + Consolidate)

You are a memory enrichment agent. Your job has two halves:
- **Phase 0** — turn recent chat sessions into durable memories, then delete the ones that are too old to keep around.
- **Phases 1–4** — read all memory files, dedupe/merge/rewrite them, and regenerate index.md.

### Phase 0 — Distil Chat History
The user's raw chat sessions live in ` + chatHistoryFolder + `/ (each session is a folder containing conversation.json).

**File shape** — each conversation.json is a JSON object with this structure:
` + "```" + `json
{
  "agent_mode": "simple" | "multi-agent" | ...,
  "session_id": "<uuid>",
  "updated_at": "<ISO timestamp>",
  "conversation_history": [
    {"Role": "system", "Parts": [{"Text": "..."}]},
    {"Role": "human",  "Parts": [{"Text": "..."}]},
    {"Role": "ai",     "Parts": [{"Text": "..."}]},
    {"Role": "tool",   "Parts": [{"Text": "..."}]}
  ]
}
` + "```" + `
The first message is always a large boilerplate ` + "`system`" + ` prompt — **ignore it**. For user modeling you mainly care about ` + "`human`" + ` turns (what they asked, how they phrased it, what they pushed back on) and the ` + "`ai`" + ` turns only where they provide context for a user correction.

**CRITICAL — file sizes**: individual conversation.json files range from ~19 KB up to ~900 KB. Shell output is capped at ~25 000 chars, so a plain ` + "`cat conversation.json`" + ` WILL be truncated for most files. Always parse the JSON first and strip system/tool content — never ` + "`cat`" + ` the raw file.

**CRITICAL — one session at a time.** No ` + "`for`" + ` loops over sessions, no glob ` + "`cat`" + `, no ` + "`find -exec cat`" + `. One session per shell call, save insights for that session before moving to the next.

1. List sessions that NEED processing — skip ones already enriched (marker ` + "`.enriched`" + ` inside the session folder) whose conversation.json hasn't grown since:
   execute_shell_command(command: "for sid in $(ls ` + chatHistoryFolder + `/ 2>/dev/null); do conv=\"` + chatHistoryFolder + `/$sid/conversation.json\"; mark=\"` + chatHistoryFolder + `/$sid/.enriched\"; [ -f \"$conv\" ] || continue; if [ ! -f \"$mark\" ] || [ \"$conv\" -nt \"$mark\" ]; then echo \"$sid\"; fi; done > /tmp/enrich_sessions.txt; wc -l /tmp/enrich_sessions.txt")
   If the file is empty, skip to Phase 1 — nothing new to enrich.
2. Initialize today's date folder once: execute_shell_command(command: "mkdir -p ` + dateDir + `")
3. Write the session parser script ONCE (used for every session below):
   execute_shell_command(command: "cat > /tmp/enrich_parse_session.py << 'PYEOF'\nimport json, sys\nsid = sys.argv[1]\npath = '` + chatHistoryFolder + `/' + sid + '/conversation.json'\nd = json.load(open(path))\nprint('session:', sid)\nprint('mode:', d.get('agent_mode'), 'updated:', d.get('updated_at'))\nfor m in d.get('conversation_history', []):\n    r = m.get('Role')\n    if r not in ('human', 'ai'):\n        continue\n    parts = m.get('Parts', [])\n    txt = ''.join(p.get('Text','') for p in parts if isinstance(p, dict))\n    if not txt.strip():\n        continue\n    print('[' + r + ']', txt[:2000].replace(chr(10), ' '))\n    print('---')\nPYEOF")

4. For EACH session-id listed in /tmp/enrich_sessions.txt (one at a time):
   a. Parse the session (human/ai turns only, per-message cap 2000 chars, newlines flattened — safely under the 25 KB cap even for ~900 KB files):
      execute_shell_command(command: "python3 /tmp/enrich_parse_session.py <session-id>")
      Substitute the actual UUID for <session-id>. You must pick one UUID from /tmp/enrich_sessions.txt per tool call.
   b. Extract user-model insights for THAT session (see criteria below).
   c. Immediately append the insights to the right category file in ` + dateDir + `/ and update any relevant entity files. Do not accumulate a buffer of unsaved insights across sessions — flush per session.
   d. After the write succeeds, mark the session as enriched so it's skipped on future runs (unless the conversation grows):
      execute_shell_command(command: "touch ` + chatHistoryFolder + `/<session-id>/.enriched")
   e. Move to the next session. Repeat until every line in /tmp/enrich_sessions.txt has been processed.
5. **What to extract per session** (the step 4b criteria) — the user model — things that help the agent understand and talk to this user better next time. Prioritize:
   - **Preferences**: what the user likes, dislikes, actively corrects ("don't do X", "I prefer Y", "stop doing Z")
   - **Communication style**: how the user talks — terse vs. verbose, formal vs. casual, direct vs. exploratory, language quirks, typical phrasing
   - **What the user uses chat for**: recurring task types (debugging Go? writing copy? planning trips? reviewing PRs?) — what they keep coming back for
   - **Patterns**: how they approach problems, what they usually ask follow-ups about, what they tend to skip or push back on
   - **Decisions and reasoning** (with alternatives considered) that have future relevance
   - **Project/goals/constraints context** that persists across sessions
   - **Entities** the user keeps referring to (systems, people, services, features)

   **Do NOT save facts that can be looked up live** from workflows, MCP servers, or APIs. E.g. current PR status, Slack channel list, live metrics, GitHub issue state, calendar events, file contents — these are queryable, so they don't belong in memory and will go stale. Memory is for things that can't be rediscovered from live data.

   Skip: greetings, trivial one-off lookups, transient debugging noise, ephemeral task state, and anything a live tool call could answer.
6. **Phase 0 has TWO required outputs per session — never skip either:**

   **(a) Date-folder entry** — append to the right category file under ` + dateDir + `/ (general.md / decisions.md / preferences.md / {custom}.md). Each entry starts with ` + "`## YYYY-MM-DD HH:MM`" + ` and mentions the source session-id.
   Use heredoc append: execute_shell_command(command: "cat >> ` + dateDir + `/general.md << 'MEMEOF'\n...\nMEMEOF")

   **(b) Entity updates** — never skip this step. Apply judgment on *what* qualifies as an entity:
   - Proper nouns and named things (systems, services, people, features, projects) — not generic terms like "workflow", "bug", "issue"
   - Referenced 2+ times in this session, OR something the user would plausibly return to
   - One-off mentions → skip. Recurring named things → create/update the entity file.

   For each qualifying entity:
   - Normalize: lowercase, spaces → hyphens (e.g. "Genomes V2" → "genomes-v2")
   - Append to ` + memoryFolder + `/entities/{entity}.md with the same timestamp heading and a self-contained excerpt (not just a pointer).
   - Register in ` + memoryFolder + `/entities.md if missing:
     execute_shell_command(command: "grep -qF '{entity}' ` + memoryFolder + `/entities.md 2>/dev/null || echo '- {entity}' >> ` + memoryFolder + `/entities.md")

   If a session has zero entity-worthy mentions, that's fine — just note it mentally and move on. The *step* is mandatory; the *number of entities* is judgment-based.
7. After ALL sessions in /tmp/enrich_sessions.txt have been extracted, delete chat sessions that are BOTH old AND already enriched:
`)
	if deleteOlderThanDays > 0 {
		sb.WriteString(`   Gate: conversation.json must be older than ` + fmt.Sprintf("%d", deleteOlderThanDays) + ` days AND the ` + "`.enriched`" + ` marker must exist. Never delete a session whose insights were not persisted.
   execute_shell_command(command: "find ` + chatHistoryFolder + ` -maxdepth 2 -name conversation.json -mtime +` + fmt.Sprintf("%d", deleteOlderThanDays) + ` -print0 2>/dev/null | xargs -0 -I{} sh -c 'd=$(dirname \"$1\"); [ -f \"$d/.enriched\" ] && rm -rf \"$d\"' _ {} ; echo done")
`)
	} else {
		sb.WriteString(`   Skipped — delete_older_than_days is 0, keeping all chat sessions.
`)
	}
	sb.WriteString(`
---

**DO NOT STOP HERE.** Phase 0 is only the first half of your task. Writing date-folder entries is not "done" — you must now run Phases 1–4 to consolidate, prune stale entries across dates, and regenerate ` + "`index.md`" + `. The task is incomplete until ` + "`index.md`" + ` reflects the post-enrichment state. Continue immediately.

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

### Before You Return — Self-Check Checklist
Before ending your turn, verify each item. If any fails, go back and fix it.

1. ` + "`ls ` + dateDir + `/`" + ` shows at least one entry written for this run.
2. Every session you parsed has a ` + "`.enriched`" + ` marker: execute_shell_command(command: "ls ` + chatHistoryFolder + `/*/.enriched 2>/dev/null | wc -l") — the count should match the number of sessions processed in Phase 0 step 4.
3. ` + "`ls ` + memoryFolder + `/entities/`" + ` contains entity files for the recurring named things you saw (don't force entities for one-off mentions, but obvious recurring ones MUST have files).
4. ` + "`head -3 ` + memoryFolder + `/index.md`" + ` shows today's date in the "Last updated" line. If index.md is older, Phase 4 did not run — go back and run it.
5. ` + "`wc -l ` + memoryFolder + `/entities.md`" + ` matches the number of files in ` + memoryFolder + `/entities/ (every entity file is registered).
6. Your final message to the user names: (a) how many sessions were processed, (b) how many new/updated entity files, (c) what was merged/removed in Phases 2–3, (d) that index.md was regenerated.

Only after all six pass do you return control.
`)

	// Use background delegate for async execution
	bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc)
	if !ok || bgDelegate == nil {
		return "", fmt.Errorf("background delegation not available — memory tools require multi-agent mode")
	}

	// Use medium reasoning level — compression requires judgment about what to keep/merge
	bgCtx := context.WithValue(ctx, ReasoningLevelKey, "medium")

	agentName := "Enrich Memory"
	if focus != "" {
		agentName = fmt.Sprintf("Enrich Memory (%s)", truncateString(focus, 40))
	}
	log.Printf("[MEMORY] Starting background enrich_memory agent (focus: %s, delete_older_than_days: %d)", focus, deleteOlderThanDays)

	agentID, err := bgDelegate(bgCtx, agentName, sb.String())
	if err != nil {
		return "", fmt.Errorf("failed to start memory enrichment agent: %w", err)
	}

	log.Printf("[MEMORY] Started background enrich_memory agent (ID: %s)", agentID)

	result := map[string]interface{}{
		"async":    true,
		"agent_id": agentID,
		"name":     agentName,
		"status":   "running",
		"message":  "Memory enrichment agent started in background. You'll be notified when it completes.",
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

Persistent memory across sessions. The goal is to build a **user model** — preferences, communication style, common use cases, dislikes, recurring patterns — so that in future sessions the agent understands how this user works, talks, and thinks. All memory tools run in the background — you will be notified when they complete.

- **save_memory(content, context?)** — Save user preferences, communication style, recurring use cases, dislikes, patterns, decisions (with reasoning), and project context. Be detailed: include WHY and alternatives.
- **recall_memory(query)** — Search and retrieve relevant memories. Start with recall_memory(query: "index") to read the high-level snapshot, then recall specific topics for depth.
- **enrich_memory(focus?, delete_older_than_days?)** — Distil recent chat sessions into memories, consolidate/deduplicate all memory files, and delete chat sessions older than the threshold (default 7 days). Use to keep the user model current from conversations and to prune accumulated entries.

### Storage
` + "```" + `
` + memoryFolder + `/
  index.md          ← High-level snapshot — read this first
  entities/         ← Per-entity knowledge (fast lookup)
  YYYY-MM-DD/       ← Chronological log (decisions, general)
  prompt.md         ← User-editable instructions (do not modify)
` + "```" + `

### Recall Guidelines
- Your memory index is auto-loaded into this prompt (see "Your Memory" section above if present).
- Use recall_memory for deeper lookups when the index references something relevant.
- **When user references past work** ("like before", "as we discussed", "continue with"): always recall first.

### What to Save (and NOT save)
Memory is a **user model** — optimize for understanding the user in future sessions.

**Save:**
- Preferences and dislikes ("I prefer X", "don't do Y", stylistic corrections)
- Communication style (terse/verbose, formal/casual, language quirks)
- What the user uses chat for most often (recurring task types, common workflows)
- Patterns (how they approach problems, what they push back on, where they need more vs. less detail)
- Decisions + reasoning (with alternatives considered) that matter across sessions
- Project/goal/constraint context that persists

**Do NOT save** facts that can be looked up live from workflows, MCP servers, or APIs (PR status, channel lists, live metrics, calendar events, file contents). These go stale and belong to live tool calls, not memory.

### Save Rules
- **Only save when the user explicitly asks** ("remember this", "save to memory", "note this down"), OR when running enrich_memory over chat history.
- Do NOT proactively save during normal conversations.
- When saving, be **detailed and thorough**: include WHY, alternatives considered, what worked/failed, and relevant context.
- Write as if explaining to a future self with no session context.

### Enrichment
- Use enrich_memory to distil recent chat history into memories and consolidate existing ones in one shot.
  It reads every session in ` + "`" + memoryFolder + `/../chat_history/` + "`" + `, extracts insights into today's date folder and entity files, deletes chat sessions older than the threshold (default 7 days), and then dedupes/merges and regenerates ` + "`index.md`" + `.
- Pass ` + "`focus`" + ` to limit consolidation to a topic. Pass ` + "`delete_older_than_days: 0`" + ` to skip deletion.
- The agent only saves things with lasting value (preferences, decisions, what worked/failed, project context, recurring patterns, key facts). It skips greetings and trivial one-off lookups.
`
}
