# Slash Commands System

Slash commands are quick actions triggered by typing `/` in the chat input. The system supports built-in commands for multi-agent chat, workflow guidance, and user-defined prompt shortcut commands stored as workspace files.

## Overview

- **Trigger**: Type `/` in the chat input to open the command picker dialog.
- **Built-in Commands**: Commands covering workflow guidance, skill import/building, MCP management, model settings, workflow extraction, chat resume, and memory enrichment.
- **User Commands**: Custom prompt shortcuts stored in `workspace-docs/commands/custom/`.
- **Registry**: A unified command registry (`frontend/src/commands/`) so adding a command is a single-file change.

## Command Registry Architecture

All commands — built-in and user-defined — share a single `CommandDefinition` interface:

```typescript
interface CommandDefinition {
  command: string           // Slash command name (e.g. "design-plan")
  description: string       // Shown in the picker dialog
  icon: ReactNode           // Lucide icon
  modes?: ModeCategory[]    // If set, only visible in these modes (empty = all)
  requiredWorkflowMode?: 'plan' | 'eval' | 'output'
  requiredWorkshopMode?: WorkshopMode | WorkshopMode[]
  validate?: (ctx: CommandContext) => string | null
  hidden?: boolean          // Executable but not shown in picker (e.g. "compact")
  source: 'builtin' | 'user'
  execute: (ctx: CommandContext) => void
}
```

The `CommandContext` provides everything an execute function needs:

```typescript
interface CommandContext {
  beforeSlash: string           // Text typed before the / trigger
  activeTabId: string
  tabSessionId: string | null
  tabConfig: any
  isSummarizing: boolean
  isStreaming: boolean
  onSubmit: (msg: string) => void
  openDialog: (name: string) => void
  openResumeDialog?: () => void
  setTabConfig: (tabId: string, config: any) => void
  addToast: (msg: string, type: 'success' | 'error' | 'info') => void
  handleSummarize: (ctx?: string) => void
  handleCompact: (ctx?: string) => void
  getAppStore: () => any
  getWorkspaceStore: () => any
  getWorkflowStore: () => any
  workflowMode?: 'plan' | 'eval' | 'output'
  workshopMode?: 'builder' | 'optimizer' | 'run' | 'reporting'
  workflowPhaseId?: string
}
```

### Registry API

```typescript
// Get all visible commands, optionally filtered by chat mode and workshop mode
getCommands(mode?: ModeCategory, workshopMode?: WorkshopMode): CommandDefinition[]

// Find a visible command by name for the current mode
findCommand(name: string): CommandDefinition | undefined

// Replace user commands (called after loading from API)
setUserCommands(cmds: CommandDefinition[]): void

// Fetch user commands from API and register them
loadAndRegisterUserCommands(): Promise<void>
```

## Built-in Commands: Multi-Agent Chat

| Command | Description | Modes | Hidden |
|---------|-------------|-------|--------|
| `/build-skill` | Build a new skill using the skill-creator | Multi-Agent | No |
| `/add-skill` | Import a skill from GitHub | Multi-Agent | No |
| `/mcp` | View MCP server details and tools | Multi-Agent | No |
| `/mcp-add` | Add or edit MCP server configuration | Multi-Agent | No |
| `/models` | Open LLM model configuration | Multi-Agent | No |
| `/workflow-builder` | Build a workflow from existing plans | Multi-Agent | No |
| `/enrich-memory` | Distil recent chats into memory, consolidate, and delete chats older than 7 days | Multi-Agent | No |

## Built-in Commands: Workflow Chat

Workflow slash commands are wrappers around the backend `get_workflow_command_guidance` tool. The frontend submits a message asking the agent to call that tool with the matching `kind`, and the backend returns the canonical guided-flow text from `agent_go/cmd/server/guidance/templates/`.

| Command | Description | Workshop Modes | Backend Kind |
|---------|-------------|----------------|--------------|
| `/resume` | Attach a previous chat conversation as context | Builder, Optimizer, Run | N/A |
| `/design-plan` | Comprehensively review plan structure, dependent artifacts, and better design options | Workshop, Run | `design-plan` |
| `/ready-to-optimize` | Check if workflow is ready to move to optimizer mode | Builder | `ready-to-optimize` |
| `/review-speed` | Review workflow latency and how to make it faster | Optimizer | `review-speed` |
| `/review-cost` | Review workflow cost and how to reduce it safely | Optimizer | `review-cost` |
| `/review-artifact-drift` | Check whether artifacts drifted from recent plan changes | Builder, Optimizer | `review-artifact-drift` |
| `/review-code` | Review saved scripts (`main.py`) against step descriptions to detect drift | Optimizer | `review-code` |
| `/improve-knowledge` | Improve knowledge notes with targeted cleanup or cross-step consolidation | Builder, Optimizer | `improve-knowledge` |
| `/improve-learnings` | Improve global learnings with targeted cleanup or current-plan consolidation | Builder, Optimizer | `improve-learnings` |
| `/improve-data` | Improve durable data contracts, schemas, and report compatibility | Builder, Optimizer | `improve-data` |
| `/improve-report` | Validate `reports/report_plan.json` and suggest layout/color improvements | Builder, Optimizer, Reporting | `improve-report` |
| `/define-success` | Confirm Goal and success criteria in `soul/soul.md`; record the operating-model assessment in Pulse Reflection history | Workshop | `define-success` |
| `/improve-evaluation` | Validate `evaluation/evaluation_plan.json` and improve goal/criteria coverage | Optimizer | `improve-evaluation` |
| `/auto-improve` | Set up recurring workflow run + frequent lightweight optimizer improvement | Optimizer | `auto-improve` |

The workflow command source of truth is split across:

- Frontend registry: `frontend/src/commands/builtin-commands.tsx`
- Backend guidance kind registry: `agent_go/cmd/server/guidance/guidance.go`
- Backend guidance templates: `agent_go/cmd/server/guidance/templates/**/<kind>.md`

When adding or removing a workflow guidance command, keep those three places in sync.

## Adding a New Built-in Command

Add a single entry to `frontend/src/commands/builtin-commands.tsx`:

```tsx
{
  command: 'my-command',
  description: 'Does something useful',
  icon: <Sparkles className="w-4 h-4" />,
  modes: ['multi-agent'],  // optional: restrict to specific modes
  source: 'builtin',
  execute: (ctx) => {
    ctx.onSubmit('Do the thing')
  }
}
```

For non-guidance commands, this is usually the only required frontend change. For workflow guidance commands, also add the backend guidance kind in `agent_go/cmd/server/guidance/guidance.go` and the markdown template under `agent_go/cmd/server/guidance/templates/`.

## User-Defined Commands

Users can create custom prompt shortcut commands from the UI. These are stored as workspace files and appear alongside built-in commands in the `/` picker.

### Command File Format

Stored at `commands/custom/{name}/COMMAND.md`:

```markdown
---
name: quick-review
description: Review current code changes
icon: eye
modes: []
---
Review my current code changes for bugs, security issues, and performance problems.

{{context}}
```

### Frontmatter Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Command name (alphanumeric, hyphens, underscores) |
| `description` | Yes | Brief description shown in the command picker |
| `icon` | No | Icon name from available set (default: `terminal`) |
| `modes` | No | Array of modes to restrict visibility. Empty `[]` = all modes |

### Available Icons

`terminal`, `zap`, `eye`, `code`, `file-text`, `message-circle`, `search`, `bookmark`, `star`

### Context Placeholder

Use `{{context}}` in the prompt template to include any text the user typed before the `/` trigger. For example, if the user types:

```
fix the login bug /quick-review
```

The text `fix the login bug` is substituted into the `{{context}}` placeholder. If no text was typed before the slash, `{{context}}` is replaced with an empty string.

### Creating Commands via UI

1. Type `/` in the chat input to open the command picker
2. Click the **+ New** button in the picker footer
3. Fill in the command editor form:
   - **Name**: Auto-slugified for the folder name
   - **Description**: Shown in the picker
   - **Icon**: Select from the icon grid
   - **Modes**: Toggle which modes the command is visible in (empty = all)
   - **Prompt template**: The message to submit, with optional `{{context}}`
4. Click **Create**

### Editing and Deleting Commands

- Hover over a user command in the picker to reveal **edit** and **delete** buttons
- Editing opens the same form pre-filled with current values
- Deleting removes the command folder from the workspace

## Storage Structure

```
workspace-docs/
├── commands/
│   └── custom/
│       ├── quick-review/
│       │   └── COMMAND.md
│       └── explain-code/
│           └── COMMAND.md
└── ... other workspace files
```

## Backend Implementation

### Package Structure

```
agent_go/pkg/commands/
├── types.go          # Command, CommandFrontmatter, request/response types
├── parser.go         # Parse/serialize YAML frontmatter + markdown
└── discovery.go      # CRUD operations via workspace API
```

### Key Types

```go
type CommandFrontmatter struct {
    Name        string   `yaml:"name" json:"name"`
    Description string   `yaml:"description" json:"description"`
    Icon        string   `yaml:"icon,omitempty" json:"icon,omitempty"`
    Modes       []string `yaml:"modes,omitempty" json:"modes,omitempty"`
}

type Command struct {
    Frontmatter CommandFrontmatter `json:"frontmatter"`
    Content     string             `json:"content"`     // Prompt template after frontmatter
    FolderName  string             `json:"folder_name"`
    FilePath    string             `json:"file_path"`
}
```

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/commands` | List all user-defined commands |
| POST | `/api/commands` | Create a new command (body: `{ name, content }`) |
| GET | `/api/commands/{name}` | Get a specific command |
| PUT | `/api/commands/{name}` | Update a command's content |
| DELETE | `/api/commands/{name}` | Delete a command folder |

### Workspace API Integration

The commands package reuses `skills.WorkspaceAPIClient` for all file operations (list, read, write, create folder, delete folder). Commands are stored under `commands/custom/` in the workspace.

## Frontend Implementation

### File Structure

```
frontend/src/commands/
├── types.ts              # CommandDefinition, CommandContext interfaces
├── builtin-commands.tsx   # Built-in commands with execute functions
├── registry.ts           # getCommands(), findCommand(), setUserCommands()
├── user-commands.ts      # Load user commands from API, icon mapping
└── index.ts              # Re-exports

frontend/src/components/
├── CommandSelectionDialog.tsx          # Slash-command picker
└── commands/CommandEditorDialog.tsx    # Create/edit command modal form

frontend/src/api/commands.ts     # CRUD API client
frontend/src/types/commands.ts   # Backend model types
```

### API Client

```typescript
// frontend/src/api/commands.ts
export const commandsApi = {
  listCommands(): Promise<ListCommandsResponse>
  getCommand(name: string): Promise<UserCommand>
  createCommand(request: CreateCommandRequest): Promise<UserCommand>
  updateCommand(name: string, request: UpdateCommandRequest): Promise<UserCommand>
  deleteCommand(name: string): Promise<void>
}
```

### How User Commands Are Loaded

1. When the command picker dialog opens, `loadAndRegisterUserCommands()` is called
2. This fetches `GET /api/commands` from the backend
3. Each `UserCommand` is converted to a `CommandDefinition` with:
   - Icon mapped from string name to lucide-react component
   - `execute` function that substitutes `{{context}}` and calls `onSubmit()`
4. User commands are registered via `setUserCommands()` and merged with built-in commands

### Component Integration

- **`CommandSelectionDialog.tsx`**: Uses `getCommands(mode, workshopMode)` from the registry. Shows edit/delete buttons on hover for user commands. Has a `New` button in the footer.
- **`ChatInput.tsx`**: Uses `findCommand(name)` to look up and execute commands. Builds a `CommandContext` from component state and passes it to `cmd.execute(ctx)`. Manages the command editor dialog state.

## Usage Flow

1. **Discover**: Type `/` in chat input → command picker appears with all available commands
2. **Filter**: Continue typing to search commands by name or description
3. **Execute**: Select a command → it runs immediately (opens a dialog, submits a message, toggles a setting, etc.)
4. **Create**: Click `+ New` → fill in the editor form → command is saved to workspace and immediately available
5. **Manage**: Hover over user commands to edit or delete them
