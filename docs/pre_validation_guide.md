# 🔍 Deterministic JSON Pre-Validation Guide

AgentForge implements a **Two-Layer Validation Architecture**. While LLMs provide high-level qualitative assessment, the **Pre-Validation Layer** provides high-speed, deterministic, code-based checks to ensure execution artifacts meet specific structural and consistency requirements.

## 🚀 Why Pre-Validation?

*   **Zero Token Cost**: Validates artifacts locally without LLM calls.
*   **Instant Feedback**: Fails immediately if files are missing or malformed.
*   **Anti-Gaming**: Prevents agents from "hallucinating" success by requiring evidence-based consistency (e.g., ensuring a `count` field matches the actual length of a `results` array).
*   **Auto-Approval**: When a step has a `validation_schema`, the system can be configured to auto-approve the step if pre-validation passes, drastically increasing workflow speed.

---

## 🛠️ Configuration Structure

Pre-validation is driven by a `validation_schema` object defined within a step's configuration (usually in `plan.json` or a template).

### Schema Definition

```json
{
  "validation_schema": {
    "files": [
      {
        "file_name": "artifacts/results.json",
        "must_exist": true,
        "json_checks": [
          {
            "path": "$.users",
            "must_exist": true,
            "value_type": "array",
            "min_length": 1
          },
          {
            "path": "$.metadata.total_count",
            "consistency_check": {
              "type": "array_length",
              "compare_with_path": "$.users"
            }
          }
        ]
      }
    ]
  }
}
```

### Supported Check Types

| Feature | Description |
| :--- | :--- |
| **File Existence** | Ensures a specific file was created/modified in the workspace. |
| **JSONPath Query** | Targets specific fields using standard JSONPath syntax (e.g., `$.items[*].id`). |
| **Type Validation** | Verifies field types: `string`, `number`, `boolean`, `array`, `object`. |
| **Range Checks** | Ensures numbers are within `min_value` and `max_value`. |
| **Length Checks** | Ensures strings or arrays meet `min_length` and `max_length`. |
| **Format Validation** | Uses Regex `pattern` to validate emails, dates, or custom IDs. |
| **Consistency Rules** | Cross-references two fields (e.g., `fieldA == fieldB` or `fieldA == len(arrayB)`). |

---

## 🧩 Consistency Rules (The "Anti-Gaming" Secret)

The most powerful part of pre-validation is the `consistency_check`. This prevents an agent from simply outputting `{"status": "success"}` without doing the work.

| Rule Type | Description |
| :--- | :--- |
| `equals` | Field A must exactly match Field B. |
| `greater_than` | Field A must be numerically greater than Field B. |
| `less_than` | Field A must be numerically less than Field B. |
| `array_length` | Field A (number) must equal the length of the array at Field B. |
| `in_array` | Field A must be a value found within the array at Field B. |

---

## 📖 Best Practices

1.  **Require Evidence**: Don't just check for a success flag. Check for the *data* that proves success (e.g., a non-empty array of records).
2.  **Cross-Check Counts**: If your agent generates a summary report, use `array_length` consistency checks to ensure the summary's count matches the raw data.
3.  **Validate Ranges**: Use `min_value` and `max_value` for things like percentages (0-100) or dates (1-31) to catch obvious hallucinations.
4.  **Graceful Failures**: If pre-validation fails, the orchestrator provides the exact error (e.g., `"Path $.users[0].id not found"`) to the LLM so it can self-correct in the next turn.

## 🔗 Related Documentation
*   **[Learning & Validation Architecture](learnings_and_validation_architecture.md)**: Deep dive into how pre-validation integrates with the "Explore vs. Exploit" engine.
*   **[Step Configuration Specification](step_config_format_specification.md)**: How to apply schemas to different node types.
