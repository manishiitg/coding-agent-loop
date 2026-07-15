# Skills System

Skills are reusable instruction sets that guide workflow agents on how to handle specific tasks. They provide domain expertise and methodology that agents can apply during step execution.

## Overview

- **Storage**: Skills are stored in `workspace-docs/skills/`. 
  - **Standard Skills**: Imported skills are stored in `skills/<skill-name>/`.
  - **Custom Skills**: Skills created via Skill Builder are stored in `skills/custom/<skill-name>/`.
- **Main File**: Each skill must have a `SKILL.md` file with YAML frontmatter.
- **Read-Only**: Skills are read-only during workflow execution (but writable in Skill Builder mode).
- **Selection**: Skills can be selected at preset level or overridden per-step.

## Skill Builder Mode

The Skill Builder is a specialized agent mode designed to help you create, test, and refine skills.
- **Access**: Select "Skill Builder" from the mode switcher or use `Ctrl+5`.
- **Theme**: Identified by the Emerald (Green) theme.
- **Capabilities**:
  - Automatically creates skills in the `skills/custom/` directory.
  - Can read all existing skills for reference.
  - Restricted to writing only within `skills/custom/`.
  - Optimized for creating API wrappers and automation scripts (Python/Bash).

## Skill File Format

A valid skill folder must contain `SKILL.md`:

```markdown
---
name: code-review
description: Performs a comprehensive code review
argument-hint: <file-path>
allowed-tools: ["execute_shell_command", "diff_patch_workspace_file"]
model: openrouter/anthropic/claude-sonnet-4
---

Review the code at `$ARGUMENTS` focusing on:
1. Code quality
2. Potential bugs
3. Security issues
```

### Frontmatter Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Display name of the skill |
| `description` | Yes | Brief description of what the skill does |
| `argument-hint` | No | Hint for arguments the skill accepts |
| `allowed-tools` | No | List of tools the skill is allowed to use. For current workflow agents, prefer exposed tools such as `execute_shell_command` and `diff_patch_workspace_file`; legacy workspace-basic names like `read_workspace_file`, `update_workspace_file`, and `delete_workspace_file` are internal/API wrappers unless explicitly exposed by the session. |
| `model` | No | Preferred LLM model for this skill |
| `disable-model-invocation` | No | When true, the model is not offered this skill for automatic invocation |
| `user-invocable` | No | Whether the skill appears as user-invocable (e.g. as a slash command); auto-generated learnings skills set this to `false` |
| `effort` | No | Effort hint for the skill's execution |
| `context` | No | Context hint for skill matching |
| `agent` | No | Agent hint for skill routing |

## Configuration Hierarchy

Skills follow a priority hierarchy:

```
PresetQuery.selected_skills (workflow-wide default)
    └── Step.AgentConfigs.enabled_skills (per-step override)
```

1. **Preset Level**: Default skills for all steps in the workflow
2. **Step Level**: Override in `step_config.json` for specific steps

### Example step_config.json

```json
{
  "steps": [
    {
      "id": "research-step",
      "agent_configs": {
        "selected_servers": ["parallel-search"],
        "enabled_skills": ["lead-research-assistant", "agent-browser"]
      }
    },
    {
      "id": "mcp-building-step",
      "agent_configs": {
        "selected_servers": ["github"],
        "enabled_skills": ["mcp-builder"]
      }
    }
  ]
}
```

## Backend Implementation

### Package Structure

```
agent_go/pkg/skills/
├── types.go                   # Skill, SkillFrontmatter structs
├── parser.go                  # Parse YAML frontmatter + markdown
├── validator.go               # Validate skill folder against spec
├── discovery.go               # Discover skills from workspace-docs/skills/ (incl. custom/)
├── github.go                  # Download skill folders from GitHub URLs
├── workspace_api.go           # Workspace file operations
├── runtime_loader.go          # LoadAttachable / LoadGlobalSkill — build attachable skills for agents
├── builtin_browser_skills.go  # Builtin agent-browser skill (served from code, never on disk)
├── cli.go                     # skills CLI integration (npx skills), lock-file update detection
└── zip.go                     # Zip import/export of skill folders
```

### Key Types

```go
type SkillFrontmatter struct {
    Name                   string   `yaml:"name,omitempty"`
    Description            string   `yaml:"description"`
    ArgumentHint           string   `yaml:"argument-hint,omitempty"`
    AllowedTools           []string `yaml:"allowed-tools,omitempty"`
    Model                  string   `yaml:"model,omitempty"`
    DisableModelInvocation bool     `yaml:"disable-model-invocation,omitempty"`
    UserInvocable          *bool    `yaml:"user-invocable,omitempty"`
    Effort                 string   `yaml:"effort,omitempty"`
    Context                string   `yaml:"context,omitempty"`
    Agent                  string   `yaml:"agent,omitempty"`
}

type Skill struct {
    Frontmatter SkillFrontmatter `json:"frontmatter"`
    Content     string           `json:"content"`               // Markdown after frontmatter
    FolderName  string           `json:"folder_name"`           // Skill folder name
    FilePath    string           `json:"file_path"`             // Relative path in workspace
    SourceURL   string           `json:"source_url,omitempty"`  // Source URL (from lock file)
    LockInfo    *CLILockEntry    `json:"lock_info,omitempty"`   // Version tracking from skills-lock.json
}
```

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/skills` | List all skills in workspace-docs/skills/ |
| GET | `/api/skills/{name}` | Get skill details and content |
| POST | `/api/skills/import` | Import skill from GitHub folder URL |
| POST | `/api/skills/validate` | Validate a GitHub URL before import |
| DELETE | `/api/skills/{name}` | Delete a skill folder |
| PUT | `/api/skills/{name}` | Update skill content |

### Workflow Integration

Skills are integrated into workflow execution in `skills_integration.go`:

```go
// Get effective skills for a step (step override > preset default)
func GetEffectiveSkills(stepConfig *AgentConfigs, orchestrator *BaseOrchestrator) []string

// Build folder guard paths (skills are read-only)
func BuildSkillFolderGuardPaths(selectedSkills []string) (readPaths, writePaths []string)
```

### How Skills Reach the Agent

There is no hand-assembled "Active Skills" prompt section any more. Skill
surfacing lives in the transport layer:

1. Builders call `skills.LoadAttachable(workspaceAPIURL, selectedSkills)`
   (`pkg/skills/runtime_loader.go`), which parses each SKILL.md into an
   attachable skill — full markdown body plus `SupportingFiles` (everything
   under the skill folder except SKILL.md, e.g. `references/`, `scripts/`).
2. Each skill is registered with `agent.AttachSkill(skill)`. The mcpagent
   library injects a progressive-disclosure skill listing into the outgoing
   system prompt at `ensureSystemPrompt()` time; CLI transports additionally
   project the skill folder to disk via the SkillProjector contract.
3. **Builtin skills**: `agent-browser` is served from code
   (`builtin_browser_skills.go`), not from the skills/ folder. The loader
   checks builtins first, so a disk folder with the same name would be
   silently shadowed — never create one.
4. **Global learnings pointer**: `LoadGlobalSkill()` attaches a tiny
   "workflow-learnings" pointer skill that directs the agent to read
   `learnings/_global/` from the workflow folder on demand, instead of
   copying the (large, growing) learnings bundle into every session.

### Folder Guard

Skills folders are added to the read-only paths in the folder guard:

```go
// In controller_agent_factory.go
effectiveSkills := GetEffectiveSkills(stepConfig, orchestrator)
if len(effectiveSkills) > 0 {
    skillReadPaths, _ := BuildSkillFolderGuardPaths(effectiveSkills)
    readPaths = append(readPaths, skillReadPaths...)
}
```

This ensures agents can read skill files but cannot modify them.

## Frontend Implementation

### Components

```
frontend/src/components/skills/
├── SkillsManager.tsx       # Main panel (sidebar)
├── SkillsList.tsx          # List of installed skills
├── SkillCard.tsx           # Individual skill display
├── SkillImportDialog.tsx   # Dialog to paste GitHub URL
├── SkillEditor.tsx         # View/edit skill content
└── SkillSelectionSection.tsx  # Multi-select for preset editor
```

### API Client

```typescript
// frontend/src/api/skills.ts
export const skillsApi = {
  listSkills(): Promise<{ skills: Skill[] }>
  getSkill(folderName: string): Promise<Skill>
  importSkill(githubUrl: string): Promise<ImportSkillResponse>
  validateSkill(githubUrl: string): Promise<ValidateSkillResponse>
  deleteSkill(folderName: string): Promise<void>
  updateSkill(folderName: string, content: string): Promise<Skill>
}
```

### Preset Integration

Skills are stored in presets via `selected_skills` field:

```typescript
interface CustomPreset {
  // ... other fields
  selectedSkills?: string[];  // Array of skill folder names
}
```

The `SkillSelectionSection` component allows selecting skills when creating/editing workflow presets.

## GitHub Import

Skills can be imported from GitHub folder URLs:

1. User provides URL like `https://github.com/user/repo/tree/main/skills/my-skill`
2. Backend parses URL to extract owner, repo, branch, path
3. Fetches folder contents via GitHub API
4. Validates: checks for SKILL.md, parses frontmatter, validates required fields
5. Downloads files to `workspace-docs/skills/{skill-name}/`
6. Returns success/error to frontend

## Storage Structure

```
workspace-docs/
├── skills/
│   ├── code-review/           # Standard/Imported Skill
│   │   └── SKILL.md
│   ├── custom/                # User-created Skills
│   │   └── my-new-skill/
│   │       └── SKILL.md
│   └── lead-research-assistant/
│       ├── SKILL.md
│       └── templates/
│           └── research-template.md
└── ... other workspace files
```

## Usage Flow

1. **Import Skills**: Use Skills Manager to import from GitHub or create manually
2. **Create Preset**: Select skills in the preset editor for workflow-wide defaults
3. **Configure Steps**: Optionally override skills per-step in step_config.json
4. **Execution**: Agent reads skill instructions and applies methodology to step
