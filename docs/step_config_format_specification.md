# step_config.json Format Specification

## Object Format (Only Supported Format)

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
        "learning_detail_level": "general",
        "enable_large_output_virtual_tools": false,
        "enable_prerequisite_detection": true,
        "prerequisite_rules": [
          {
            "depends_on_step": "step-0",
            "description": "If login session is missing or expired, go back to step 0"
          },
          {
            "depends_on_step": "step-1",
            "description": "If config file is missing, go back to step 1"
          }
        ]
      }
    }
  ]
}
```

## Format Details

### Object Format Structure
- **Root**: Always an object `{}` with a `steps` field
- **Steps Field**: An array `[]` containing step configurations
- **Items**: Each array item represents one step's configuration
- **Identifier**: Each item must have an `id` field (required)
- **Fields**: All configuration is nested in `agent_configs` object

### Field Structure

Each step in the `steps` array:
- **`id`** (required): Stable step ID from plan.json
- **`title`** (optional): Step title for reference/display only
- **`agent_configs`** (optional): Nested configuration object containing all step-specific settings

## Implementation

### Frontend (`usePlanData.ts`)
- **Read**: `normalizeStepConfigFile()` - parses object format with `steps` field and extracts `StepConfig[]` array
- **Write**: Always writes object format via `JSON.stringify({ steps: stepConfigs }, null, 2)`

### Backend (`step_config.go`)
- **Read**: `ParseStepConfigContent()` - parses object format with `steps` field and extracts `[]StepConfig` array
- **Write**: Always writes object format via `json.MarshalIndent(StepConfigFile{Steps: configs}, "", "  ")`

## Key Points

1. ✅ **Object format only** - `{ "steps": [...] }` format is the only supported format
2. ✅ **Both sides read and write** object format consistently
3. ✅ **Backward compatibility** - Legacy array format is still supported during migration (with warning)
4. ✅ **Nested structure** - All configuration is in `agent_configs` object

## Field Mapping

| Object Format (File) | Internal Structure |
|---------------------|-------------------|
| `steps[].id` | `StepConfig.id` |
| `steps[].title` | `StepConfig.title` |
| `steps[].agent_configs` | `StepConfig.agent_configs` |

## Notes

- The `steps` field is required in the root object
- Empty `steps` array `[]` is valid (no step configs)
- `agent_configs` can be `null` or `undefined` if no config exists for a step
- When writing, the file always uses object format with `steps` field
