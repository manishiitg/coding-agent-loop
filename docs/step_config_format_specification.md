# step_config.json Format Specification

## 📋 Overview

The `step_config.json` file stores step-specific agent configurations (LLM models, tool selections, execution settings) separate from the main plan structure. This separation allows plan.json to focus on workflow structure while step_config.json manages execution details.

**Key Benefits:**
- **Separation of concerns**: Plan structure vs execution configuration
- **Consistent format**: Object format with `steps` array (both frontend and backend)
- **Backward compatibility**: Legacy array format supported during migration

---

## 📁 Key Files & Locations

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| **Frontend Parser** | [`frontend/src/components/workflow/hooks/usePlanData.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/hooks/usePlanData.ts) | `normalizeStepConfigFile()`, `saveStepConfig()` |
| **Backend Parser** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/step_config.go`](file:///Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/step_config.go) | `ParseStepConfigContent()`, `StepConfigFile` |
| **Type Definitions** | [`frontend/src/utils/stepConfigMatching.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/utils/stepConfigMatching.ts) | `StepConfig`, `AgentConfigs` |

---

## 🔄 Format Structure

### Object Format (Only Supported Format)

Both frontend and backend **read and write** only the object format with `steps` field:

```json
{
  "steps": [
    {
      "id": "step-id-here",
      "title": "Optional step title",
      "agent_configs": {
        "selected_servers": ["server1", "server2"],
        "selected_tools": ["server1:tool1", "server1:tool2", "server2:*"],
        "enabled_custom_tools": ["workspace_tools:read_file", "workspace_tools:write_file"],
        "execution_llm": {
          "provider": "openai",
          "model_id": "gpt-4o"
        },
        "execution_max_turns": 25,
        "use_code_execution_mode": true,
        "disable_validation": false,
        "disable_learning": false,
        "lock_learnings": false,
        "learning_detail_level": "general",
        "enable_large_output_virtual_tools": false,
        "enable_prerequisite_detection": true,
        "prerequisite_rules": [
          {
            "depends_on_step": "step-0",
            "description": "If login session is missing or expired, go back to step 0"
          }
        ]
      }
    }
  ]
}
```

### Field Structure

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `steps` | ✅ | `array` | Root array containing step configurations |
| `steps[].id` | ✅ | `string` | Stable step ID from plan.json |
| `steps[].title` | ❌ | `string` | Step title for reference/display only |
| `steps[].agent_configs` | ❌ | `object` | Nested configuration object |

---

## 🔄 Implementation Details

### Frontend Implementation

**File:** [`frontend/src/components/workflow/hooks/usePlanData.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/hooks/usePlanData.ts)

**Read:**
```typescript
function normalizeStepConfigFile(rawContent: unknown): StepConfig[] {
  // Handles object format: { "steps": [...] }
  if (rawContent && typeof rawContent === 'object' && !Array.isArray(rawContent)) {
    const obj = rawContent as Record<string, unknown>
    if ('steps' in obj && Array.isArray(obj.steps)) {
      return obj.steps as StepConfig[]
    }
  }
  // Legacy array format support (with warning)
  if (Array.isArray(rawContent)) {
    return rawContent as StepConfig[]
  }
  return []
}
```

**Write:**
```typescript
const content = JSON.stringify({ steps: stepConfigs }, null, 2)
await agentApi.updatePlannerFile(stepConfigPath, content, `Updated step config for step ${stepId}`)
```

### Backend Implementation

**File:** [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/step_config.go`](file:///Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/step_config.go)

**Read:**
```go
type StepConfigFile struct {
    Steps []StepConfig `json:"steps"`
}

func ParseStepConfigContent(content string) ([]StepConfig, error) {
    var file StepConfigFile
    if err := json.Unmarshal([]byte(content), &file); err != nil {
        return nil, err
    }
    return file.Steps, nil
}
```

**Write:**
```go
file := StepConfigFile{Steps: configs}
content, err := json.MarshalIndent(file, "", "  ")
```

---

## ⚙️ Configuration Fields

| Field | Type | Default | Purpose |
|-------|------|---------|---------|
| `selected_servers` | `string[]` | `[]` | MCP servers to use for this step |
| `selected_tools` | `string[]` | `[]` | Specific tools (format: `"server:tool"` or `"server:*"`) |
| `enabled_custom_tools` | `string[]` | `[]` | Custom tools to enable (e.g., `workspace_tools:read_file`) |
| `execution_llm` | `object` | Preset default | LLM config for execution agent |
| `validation_llm` | `object` | Preset default | LLM config for validation agent |
| `learning_llm` | `object` | Preset default | LLM config for learning agent |
| `execution_max_turns` | `number` | Preset default | Maximum conversation turns for execution |
| `use_code_execution_mode` | `boolean` | `false` | Enable code execution mode |
| `disable_validation` | `boolean` | `false` | Disable validation for this step |
| `disable_learning` | `boolean` | `false` | Disable learning capture for this step |
| `lock_learnings` | `boolean` | `false` | Prevent learning updates for this step |
| `learning_detail_level` | `"exact"\|"general"` | `"general"` | Level of detail in learnings |
| `enable_large_output_virtual_tools` | `boolean` | `false` | Enable virtual tools for large outputs |
| `enable_prerequisite_detection` | `boolean` | `false` | Enable prerequisite failure detection |
| `prerequisite_rules` | `array` | `[]` | Rules for prerequisite failure handling |

---

## 🛠️ Common Issues & Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| `"steps" field not found` | File uses legacy array format | Use `normalizeStepConfigFile()` to handle both formats |
| `Step ID mismatch` | Step ID doesn't match plan.json | Ensure step IDs are stable and match plan.json exactly |
| `agent_configs is null` | Step has no configuration | This is valid - step will use preset defaults |
| `Parse error` | Invalid JSON structure | Verify file uses object format: `{ "steps": [...] }` |

---

## 🔍 For LLMs: Quick Reference

**Constraints:**
- ✅ **Allowed**: Object format `{ "steps": [...] }`
- ✅ **Allowed**: Empty `steps` array `[]`
- ✅ **Allowed**: Missing `agent_configs` (uses defaults)
- ❌ **Forbidden**: Root-level array format (legacy, deprecated)
- ❌ **Forbidden**: Missing `id` field in step config

**Example:**
```json
{
  "steps": [
    {
      "id": "step-1",
      "agent_configs": {
        "selected_servers": ["aws"],
        "execution_llm": {
          "provider": "openai",
          "model_id": "gpt-4o"
        }
      }
    }
  ]
}
```

**File Operations:**
- **Read**: Always parse object format, extract `steps` array
- **Write**: Always write object format: `JSON.stringify({ steps: stepConfigs }, null, 2)`
- **Update**: Find step by `id`, update `agent_configs`, save entire file

---

## 📖 Related Documentation

- [Workflow Orchestrator](workflow_orchestrator.md) - Overall system architecture
- [Code Execution Mode](code_execution_mode.md) - Code execution configuration
- [Step Config Matching](../frontend/src/utils/stepConfigMatching.ts) - Type definitions and matching logic
