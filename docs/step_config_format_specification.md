# step_config.json Format Specification

## Canonical Format (Agreed Standard)

Both frontend and backend **read** multiple formats for backward compatibility, but **always write** in the canonical format:

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

## Supported Input Formats (Read-Only, for Backward Compatibility)

### Format 1: Canonical (Preferred)
```json
{
  "steps": [
    {
      "id": "step-id",
      "agent_configs": { ... }
    }
  ]
}
```

### Format 2: Array Format (Legacy)
```json
[
  {
    "step_id": "step-id",
    "selected_servers": ["server1"],
    "selected_tools": ["server1:tool1"],
    "enabled_custom_tools": ["workspace_tools:read_file"]
  }
]
```

### Format 3: Flat Object Format (Legacy, Single Step)
```json
{
  "step_id": "step-id",
  "selected_servers": ["server1"],
  "selected_tools": ["server1:tool1"],
  "enabled_custom_tools": ["workspace_tools:read_file"]
}
```

## Conversion Rules

When reading non-canonical formats:
1. `step_id` → `id`
2. Top-level `selected_servers`, `selected_tools`, `enabled_custom_tools` → nested in `agent_configs`
3. If both top-level and nested `agent_configs` exist, top-level fields take precedence

## Implementation

### Frontend (`usePlanData.ts`)
- **Read**: `normalizeStepConfigFile()` - converts all formats to canonical
- **Write**: Always writes canonical format via `JSON.stringify(stepConfigFile, null, 2)`

### Backend (`step_config.go`)
- **Read**: `ParseStepConfigContent()` - converts all formats to canonical
- **Write**: Always writes canonical format via `json.MarshalIndent(configs, "", "  ")`

## Key Points

1. ✅ **Both sides read** multiple formats (backward compatible)
2. ✅ **Both sides write** only canonical format (ensures consistency)
3. ✅ **Conversion is automatic** - no manual migration needed
4. ✅ **Top-level fields take precedence** when both exist in legacy formats

## Field Mapping

| Legacy Format | Canonical Format |
|--------------|------------------|
| `step_id` | `id` |
| `selected_servers` (top-level) | `agent_configs.selected_servers` |
| `selected_tools` (top-level) | `agent_configs.selected_tools` |
| `enabled_custom_tools` (top-level) | `agent_configs.enabled_custom_tools` |

## Notes

- The `has_config` field in legacy formats is ignored (computed from presence of configs)
- Empty arrays are preserved (not removed)
- `agent_configs` can be `null` or `undefined` if no config exists for a step

