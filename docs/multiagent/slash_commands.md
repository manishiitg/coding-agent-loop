# Slash Commands System

Slash commands are quick actions triggered by typing `/` in the chat input. The system supports built-in commands (summarize, spawn, build-skill, etc.) and user-defined prompt shortcut commands stored as workspace files.

## Overview

- **Trigger**: Type `/` in the chat input to open the command picker dialog.
- **Built-in Commands**: Commands covering summarization, skill building, MCP management, workflow tooling, etc.
- **User Commands**: Custom prompt shortcuts stored in `workspace-docs/commands/custom/`.
- **Registry**: A unified command registry (`frontend/src/commands/`) so adding a command is a single-file change.

## Command Registry Architecture

All commands — built-in and user-defined — share a single `CommandDefinition` interface:

```typescript
interface CommandDefinition {
  command: string           // Slash command name (e.g. "summarize")
  description: string       // Shown in the picker dialog
  icon: ReactNode           // Lucide icon
  modes?: ModeCategory[]    // If set, only visible in these modes (empty = all)
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
  setTabConfig: (tabId: string, config: any) => void
  addToast: (msg: string, type: 'success' | 'error' | 'info') => void
  handleSummarize: (ctx?: string) => void
  handleCompact: (ctx?: string) => void
  getAppStore: () => any
  getWorkspaceStore: () => any
}
```

### Registry API

```typescript
// Get all visible commands, optionally filtered by mode
getCommands(mode?: ModeCategory): CommandDefinition[]

// Find a command by name (includes hidden commands)
findCommand(name: string): CommandDefinition | undefined

// Replace user commands (called after loading from API)
setUserCommands(cmds: CommandDefinition[]): void

// Fetch user commands from API and register them
loadAndRegisterUserCommands(): Promise<void>
```

## Built-in Commands

| Command | Description | Modes | Hidden |
|---------|-------------|-------|--------|
| `/build-skill` | Build a new skill using skill-creator | All | No |
| `/build-subagent` | Build a new sub-agent template | All | No |
| `/add-skill` | Import a skill from GitHub | All | No |
| `/mcp` | View MCP server details and tools | All | No |
| `/mcp-add` | Add or edit MCP server configuration | All | No |
| `/models` | Open LLM model configuration | All | No |
| `/resume` | Resume a previous conversation | All | No |
| `/workflow-builder` | Build a workflow from existing plans | Multi-Agent | No |
| `/compress-memory` | Compress and clean up agent memories | Multi-Agent | No |

## Adding a New Built-in Command

Add a single entry to `frontend/src/commands/builtin-commands.tsx`:

```tsx
{
  command: 'my-command',
  description: 'Does something useful',
  icon: <Sparkles className="w-4 h-4" />,
  modes: ['chat'],       // optional: restrict to specific modes
  source: 'builtin',
  execute: (ctx) => {
    ctx.onSubmit('Do the thing')
  }
}
```

No other files need to be changed.

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

frontend/src/components/commands/
└── CommandEditorDialog.tsx   # Create/edit command modal form

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

- **`CommandSelectionDialog.tsx`**: Uses `getCommands(mode)` from the registry. Shows edit/delete buttons on hover for user commands. Has a `+ New` button in the footer.
- **`ChatInput.tsx`**: Uses `findCommand(name)` to look up and execute commands. Builds a `CommandContext` from component state and passes it to `cmd.execute(ctx)`. Manages the command editor dialog state.

## Usage Flow

1. **Discover**: Type `/` in chat input → command picker appears with all available commands
2. **Filter**: Continue typing to search commands by name or description
3. **Execute**: Select a command → it runs immediately (opens a dialog, submits a message, toggles a setting, etc.)
4. **Create**: Click `+ New` → fill in the editor form → command is saved to workspace and immediately available
5. **Manage**: Hover over user commands to edit or delete them
