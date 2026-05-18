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
        "selected_servers": ["playwright"],
        "enabled_skills": ["lead-research-assistant"]
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
├── types.go          # Skill, SkillFrontmatter structs
├── parser.go         # Parse YAML frontmatter + markdown
├── validator.go      # Validate skill folder against spec
├── discovery.go      # Discover skills from workspace-docs/skills/
├── github.go         # Download skill folders from GitHub URLs
└── workspace_api.go  # Workspace file operations
```

### Key Types

```go
type SkillFrontmatter struct {
    Name         string   `yaml:"name" json:"name"`
    Description  string   `yaml:"description" json:"description"`
    ArgumentHint string   `yaml:"argument-hint,omitempty" json:"argument_hint,omitempty"`
    AllowedTools []string `yaml:"allowed-tools,omitempty" json:"allowed_tools,omitempty"`
    Model        string   `yaml:"model,omitempty" json:"model,omitempty"`
}

type Skill struct {
    Frontmatter SkillFrontmatter `json:"frontmatter"`
    Content     string           `json:"content"`      // Markdown after frontmatter
    FolderName  string           `json:"folder_name"`  // Skill folder name
    FilePath    string           `json:"file_path"`    // Relative path in workspace
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

// Build the system prompt section for skills
func BuildWorkflowSkillPrompt(ctx context.Context, selectedSkills []string, bo *BaseOrchestrator) string

// Build folder guard paths (skills are read-only)
func BuildSkillFolderGuardPaths(selectedSkills []string) (readPaths, writePaths []string)
```

### Execution Agent Prompt

When skills are enabled, the following is added to the execution agent's system prompt:

```
## Active Skills

### What is a Skill?
A skill is a reusable set of instructions that guides you on how to handle
specific tasks or workflows. Skills are stored in the workspace under the
"skills/" folder.

### How to Use Skills in Workflow:
1. Read the skill: Read the SKILL.md file from the workspace
2. Follow the instructions: Apply the skill's methodology to the current step
3. Check for additional files: List the skill folder to find supporting files

### Activated Skills:
- **lead-research-assistant**: Guides research workflows
  - Path: `skills/lead-research-assistant/SKILL.md`

**Action Required:** Read each skill's SKILL.md from the workspace before
executing the step.
```

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
