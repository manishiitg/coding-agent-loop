# Human-Controlled Todo Creation Orchestrator

Multi-agent system creating validated todo lists via step-by-step execution, learning, and synthesis.

**Features**: 🎯 Human-in-loop • 🔄 Learning-based • 📊 Validation-driven • 🤖 Multi-agent • 📝 Markdown-based • 🔀 Conditional Logic • ⚡ Fast Execution

---

## ⚡ Quick Reference

| Phase | Agent | Output | Human Decision | Manager |
|-------|-------|--------|---------------|---------|
| **0** | Variable Extraction | `variables.json` | Use/Extract new/Update | `VariableManager` ✅ |
| **1** | Planning | `plan.json` | Use/Create/Update (max 20 rev) | - |
| **2** | Execute → Validate → Learn | Step results | Approve/Re-execute/Stop | - |
| **2.5** | Anonymize Learnings | Anonymized learnings | Confirm replacements | `AnonymizationManager` ✅ |
| **2.6** | Plan Improvement | Feedback report | Review feedback | `PlanImprovementManager` ✅ |

**Retry Limits**: Execution (5), Plan (20)  
**Progress**: Auto-saved in `runs/{run_folder}/steps_done.json`  
**Loop Support**: Iterative execution until condition met (max iterations configurable)  
**Conditional Support**: Branching logic (If/Else) based on runtime conditions  
**Independence**: ✅ = Independent manager (no orchestrator dependency), ⚠️ = Uses full orchestrator

---

## 🏗️ Architecture

### Manager-Based Architecture

The orchestrator uses **dedicated managers** for independent workflow phases, enabling complete decoupling and reusability:

| Phase | Manager | Status | Description |
|-------|---------|--------|-------------|
| **Variable Extraction** | `VariableManager` | ✅ Independent | Manages variable extraction and validation independently |
| **Anonymization** | `AnonymizationManager` | ✅ Independent | Manages learnings anonymization independently |
| **Plan Improvement** | `PlanImprovementManager` | ✅ Independent | Manages plan improvement analysis independently |
| **Planning** | - | ⚠️ Orchestrator | Uses full orchestrator (complex dependencies) |
| **Execution** | - | ⚠️ Orchestrator | Main orchestrator method |

**Key Benefits**:
- **Decoupling**: Managers operate independently without creating full orchestrator
- **Reusability**: Managers can be used directly in `workflow_orchestrator.go`
- **Consistency**: All managers follow the same pattern and use `CreateAndSetupStandardAgentWithConfig`
- **LLM Config**: Proper preservation of `FallbackModels`, `CrossProviderFallback`, and `APIKeys`
- **No Dependencies**: Independent phases don't depend on each other's code

---

## 🤖 Agents Overview

### 1. Variable Extraction Agent

**Purpose**: Extracts variables from objective and converts to templated format

**Files**: `variable_extraction_agent.go`, `variable_management.go`

**Modes**:
- **CREATE**: Extract new variables from objective
- **UPDATE**: Update existing variables with human feedback

**Input**:
- Objective (raw text)
- Existing variables (UPDATE mode only)

**Output**:
- `variables.json` with extracted variables
- Templated objective with `{{VARIABLE}}` placeholders

**Tools** (UPDATE mode):
- `update_variable`: Modify existing variable
- `update_objective`: Update templated objective
- `human_feedback`: Request clarification

**Configuration**:
- LLM: Configurable via `presetVariableExtractionLLM`
- Max Revisions: 10
- No MCP servers (pure LLM extraction)

**Manager**: `VariableManager` (✅ Independent)

---

### 2. Planning Agent

**Purpose**: Creates execution plan with steps, dependencies, and configurations

**Files**: `planning_agent.go`, `planning_management.go`

**Modes**:
- **CREATE**: Generate new plan from scratch
- **UPDATE**: Modify existing plan with feedback

**Input**:
- Objective (templated with variables)
- Existing plan (UPDATE mode only)
- Variable names and values

**Output**:
- `plan.json` with structured steps
- Each step includes: title, description, success criteria, dependencies, loop config, conditional config, agent configs

**Tools** (UPDATE mode):
- `update_plan_steps`: Modify existing steps
- `add_plan_steps`: Insert new steps
- `delete_plan_steps`: Remove steps
- `human_feedback`: Request clarification

**Configuration**:
- LLM: Configurable via `presetPlanningLLM`
- Max Revisions: 20
- MCP Access: Yes (for capability awareness)

**Step Configuration** (`step_config.json`):
- Per-step agent LLM overrides
- Learning detail level
- Validation/learning toggles
- Custom tool selection
- Large output virtual tools toggle

---

### 3. Execution Agent

**Purpose**: Executes individual plan steps using MCP tools

**Files**: `execution_agent.go`

**Input**:
- Step details (title, description, success criteria)
- Context dependencies (previous step outputs)
- Variable values (resolved)
- Workspace path (execution folder)
- Learnings path (for reading patterns)
- Loop context (if in loop mode)
- Validation feedback (for retries)

**Output**:
- Execution result (final response)
- Conversation history (for learning agents)
- Context output files (in `runs/{run_folder}/execution/`)

**Features**:
- **MCP Tool Access**: Full access to configured MCP servers
- **Loop Support**: Receives previous iteration outputs
- **Retry Logic**: Max 5 attempts with validation feedback
- **Code Execution Mode**: Optional Python script execution
- **Learning Discovery**: Auto-discovers learning files and scripts

**Configuration**:
- LLM: Configurable per-step via `AgentConfigs.ExecutionLLM`
- Max Turns: Configurable per-step
- MCP Servers: Configurable per-step
- Custom Tools: Configurable per-step

---

### 4. Validation Agent

**Purpose**: Validates step execution against success criteria and loop conditions

**Files**: `validation_agent.go`

**Input**:
- Step details (title, description, success criteria)
- Execution history (conversation from execution agent)
- Workspace path (for file inspection)
- Loop condition (if in loop mode)

**Output** (Structured):
- `is_success_criteria_met`: Boolean
- `execution_status`: COMPLETED | PARTIAL | FAILED | INCOMPLETE
- `reasoning`: Validation explanation
- `feedback`: Array of issues/recommendations
- `loop_condition_met`: Boolean (loop mode only)
- `loop_reasoning`: Loop condition explanation (loop mode only)

**Features**:
- **Structured Output**: Uses tool-based structured response
- **Loop Validation**: Checks both success criteria AND loop condition
- **Feedback Generation**: Provides actionable feedback for retries
- **Workspace Inspection**: Can read files to verify results

**Configuration**:
- LLM: Configurable per-step via `AgentConfigs.ValidationLLM`
- Can be disabled per-step: `AgentConfigs.DisableValidation`

---

### 5. Learning Agent (Unified)

**Purpose**: Analyzes both successful and failed executions to capture patterns

**Files**: `learning_agent.go`, `learning_agent_code_execution.go`

**Input**:
- Step details
- Execution history
- Validation result
- Workspace path

**Output**:
- Learning analysis (success/failure patterns)
- Updates to `plan.json` (adds patterns to steps)
- Learning files in `learnings/` folder

**Modes**:
- **Success Learning**: Captures what worked well
- **Failure Learning**: Root cause analysis + retry guidance

**Features**:
- **Code Execution Learning**: Special mode for code execution steps
- **Pattern Extraction**: Identifies reusable patterns
- **Plan Enhancement**: Automatically updates plan with learnings
- **Detail Levels**: `exact` (with values) or `general` (anonymized)

**Configuration**:
- LLM: Configurable per-step via `AgentConfigs.LearningLLM`
- Detail Level: `AgentConfigs.LearningDetailLevel` (exact/general/none)
- Can be disabled: `AgentConfigs.DisableLearning`
- Loop Iteration Learning: `AgentConfigs.LearningAfterLoopIteration`

---

### 6. Conditional Agent (ConditionalLLM)

**Purpose**: Evaluates conditional branching decisions

**Files**: `controller.go` (uses `orchestratorllm.ConditionalLLM`)

**Input**:
- Condition question (from plan step)
- Execution context (step outputs, previous results)
- Condition context (additional info)

**Output**:
- `result`: Boolean (true/false)
- `reason`: Explanation for decision

**Features**:
- **Context-Aware**: Uses execution results to make decisions
- **Nested Support**: Supports up to 2 levels of nesting
- **Branch Tracking**: Stores decision in `steps_done.json`

**Configuration**:
- LLM: Uses orchestrator default (preserves API keys)
- Max Retries: 3

---

### 7. Anonymization Agent

**Purpose**: Replaces actual values in learnings with variable placeholders

**Files**: `anonymization_agent.go`

**Input**:
- Workspace path
- Variables JSON (for fuzzy matching)
- Variable names

**Output**:
- Anonymized learning files (`.md` and `.py`)
- Replacements report

**Features**:
- **Fuzzy Matching**: Finds values similar to known variables
- **Human Confirmation**: Requires approval before modifications
- **Multi-Format**: Handles both Markdown and Python files

**Configuration**:
- LLM: Configurable via `presetAnonymizationLLM`
- MCP Access: Yes (for file operations)

**Manager**: `AnonymizationManager` (✅ Independent)

---

### 8. Plan Improvement Agent

**Purpose**: Analyzes execution results and provides feedback for plan improvement

**Files**: `plan_improvement_agent.go`

**Input**:
- Workspace path
- Plan JSON
- Execution results summary
- Allowed paths (for file inspection)

**Output**:
- `plan_improvement_feedback.md` with improvement suggestions
- Feedback report

**Features**:
- **Execution Analysis**: Reviews execution results from `runs/` folder
- **Plan Review**: Analyzes plan structure and patterns
- **Human Feedback**: Uses `human_feedback` tool to ask clarifying questions
- **Comprehensive Report**: Generates detailed feedback document

**Configuration**:
- LLM: Configurable via `presetPlanImprovementLLM`
- MCP Access: Yes (for file reading)

**Manager**: `PlanImprovementManager` (✅ Independent)

---

## 📁 Workspace Structure

```
workspace/
├── todo_creation_human/
│   ├── variables/
│   │   └── variables.json          # Phase 0: Variable definitions
│   ├── planning/
│   │   ├── plan.json               # Phase 1: Execution plan
│   │   └── step_config.json        # Per-step agent configurations
│   ├── learnings/                   # Learning patterns
│   │   ├── success_patterns.md     # What worked
│   │   ├── failure_analysis.md     # What failed
│   │   ├── step_X_learning.md      # Per-step learnings
│   │   └── scripts/                # Python scripts from code execution
│   │       └── *.py
│   └── runs/                        # Execution runs
│       ├── iteration-same/          # Default run folder
│       │   ├── execution/           # Execution outputs
│       │   │   └── step_X_*.md
│       │   ├── validation/          # Validation reports
│       │   │   └── step_X_*.md
│       │   └── steps_done.json      # Progress tracking
│       └── iteration-N/             # Numbered run folders
```

---

## 🔄 Phase Details

### Phase 0: Variable Extraction

**Flow**: Extract → Verify → Use  

**Decision Points**:
1. **Use Existing**: Keep current `variables.json`
2. **Extract New**: Delete old → Extract fresh
3. **Update Existing**: Modify with feedback

**Agent**: Variable Extraction Agent  
**Manager**: `VariableManager` (✅ Independent)  
**Output**: `variables.json`, templated objective

---

### Phase 1: Planning

**Flow**: Create plan → Human choice → Approve (max 20 revisions)

**Decision Points**:
1. **Use Existing**: Continue with current `plan.json`
2. **Create New**: Delete old plan + artifacts → Create fresh
3. **Update Existing**: Keep artifacts → Update plan with feedback

**Agent**: Planning Agent  
**Features**:
- MCP server access for capability awareness
- Direct JSON output (no markdown intermediate)
- Iterative refinement with human feedback
- Supports loops, conditionals, and per-step configs

**Output**: `plan.json`, `step_config.json`

---

### Phase 2: Execution

**Flow**: Execute → Validate → Learn → Human feedback (per step)

**Run Modes**:
- **Use Same Run**: Reuses `runs/iteration-same` or latest folder
- **Create New Run**: Creates new `runs/iteration-N` folder

**Execution Modes**:
- **Normal**: Full execution with learning and human feedback
- **Fast Execute**: Skips learning and human feedback
- **Skip Human Input**: Runs learning but auto-approves steps

**Agents**:
1. **Execution Agent**: Executes step with MCP tools
2. **Validation Agent**: Validates against success criteria
3. **Learning Agent**: Captures success/failure patterns
4. **Conditional Agent**: Evaluates branching conditions (if applicable)

**Retry Logic**: Max 5 attempts with validation feedback

**Loop Support**:
- Iterative execution until loop condition met
- Max iterations configurable (default 10)
- Previous iteration outputs passed as context
- Learning can run after each iteration

**Conditional Support**:
- Branching based on runtime conditions
- Nested conditionals (up to depth 2)
- Branch progress tracked in `steps_done.json`

---

### Phase 2.5: Anonymize Learnings (Independent)

**Flow**: Scan learnings → Identify values → Confirm → Replace

**Agent**: Anonymization Agent  
**Manager**: `AnonymizationManager` (✅ Independent)  
**Purpose**: Replace actual values with `{{VARIABLE}}` placeholders  
**Output**: Anonymized learning files

---

### Phase 2.6: Plan Improvement (Independent)

**Flow**: Analyze execution → Review plan → Ask questions → Generate feedback

**Agent**: Plan Improvement Agent  
**Manager**: `PlanImprovementManager` (✅ Independent)  
**Purpose**: Provide feedback for improving the plan  
**Output**: `plan_improvement_feedback.md`

---

## 📚 File Formats

### variables.json
```json
{
  "objective": "Extract {{DATABASE_URL}} from {{CONFIG_PATH}}",
  "variables": [
    {
      "name": "DATABASE_URL",
      "value": "postgres://localhost:5432/db",
      "description": "Database connection URL"
    },
    {
      "name": "CONFIG_PATH",
      "value": "config/database.json",
      "description": "Path to config file"
    }
  ],
  "extraction_date": "2025-01-27T12:00:00Z"
}
```

### plan.json
```json
{
  "steps": [
    {
      "id": "step-1",
      "title": "Read config file",
      "description": "Read and parse config.json",
      "success_criteria": "File read successfully",
      "context_dependencies": [],
      "context_output": "config_content.md",
      "has_loop": false,
      "has_condition": false,
      "agent_configs": {
        "execution_llm": {
          "provider": "anthropic",
          "model_id": "claude-3-5-sonnet-20241022"
        },
        "learning_detail_level": "exact",
        "disable_validation": false
      }
    },
    {
      "id": "step-2",
      "title": "Wait for service",
      "description": "Poll until service is ready",
      "success_criteria": "Service responds",
      "has_loop": true,
      "loop_condition": "Health check returns 200 OK",
      "max_iterations": 10
    },
    {
      "id": "step-3",
      "title": "Check build status",
      "description": "Verify build completed",
      "has_condition": true,
      "condition_question": "Did the build succeed?",
      "if_true_steps": [
        {
          "id": "step-3-if-true-0",
          "title": "Deploy to production",
          "description": "Deploy the build"
        }
      ],
      "if_false_steps": [
        {
          "id": "step-3-if-false-0",
          "title": "Send failure notification",
          "description": "Notify team of failure"
        }
      ]
    }
  ]
}
```

### step_config.json
```json
{
  "steps": [
    {
      "id": "step-1",
      "title": "Read config file",
      "agent_configs": {
        "execution_llm": {
          "provider": "anthropic",
          "model_id": "claude-3-5-sonnet-20241022"
        },
        "validation_llm": {
          "provider": "openai",
          "model_id": "gpt-4"
        },
        "learning_llm": {
          "provider": "anthropic",
          "model_id": "claude-3-5-sonnet-20241022"
        },
        "execution_max_turns": 10,
        "learning_detail_level": "exact",
        "disable_validation": false,
        "disable_learning": false,
        "learning_after_loop_iteration": false,
        "enabled_mcp_servers": ["filesystem", "database"],
        "enabled_custom_tools": ["custom_tool_1"],
        "enable_large_output_virtual_tools": true
      }
    }
  ]
}
```

### steps_done.json
```json
{
  "completed_step_indices": [0, 1],
  "total_steps": 5,
  "last_updated": "2025-01-27T12:00:00Z",
  "branch_steps": {
    "2": {
      "branch_executed": "if_true",
      "completed_steps": ["step-3-if-true-0"]
    }
  }
}
```

---

## ⚙️ Configuration

### Agent LLM Configuration

Each agent can be configured with custom LLM settings:

**Preset Defaults** (orchestrator-level):
- `presetExecutionLLM`
- `presetValidationLLM`
- `presetLearningLLM`
- `presetPlanningLLM`
- `presetVariableExtractionLLM`
- `presetAnonymizationLLM`
- `presetPlanImprovementLLM`

**Per-Step Overrides** (`step_config.json`):
- `execution_llm`: Override for execution agent
- `validation_llm`: Override for validation agent
- `learning_llm`: Override for learning agent

**Priority**: Step config > Preset default > Orchestrator default

### Learning Configuration

**Detail Levels**:
- `exact`: Include actual values in learnings
- `general`: Anonymize values (use variable placeholders)
- `none`: Disable learning for this step

**Toggles**:
- `disable_learning`: Skip learning agents entirely
- `learning_after_loop_iteration`: Run learning after each loop iteration

**Code Execution Mode**:
- Forces learning enabled regardless of step config
- Special learning agent for code execution analysis

### Validation Configuration

**Toggles**:
- `disable_validation`: Skip validation agent (auto-approve)

**Loop Validation**:
- Always checks both success criteria AND loop condition
- Returns `loop_condition_met` and `loop_reasoning`

---

## 🔍 Troubleshooting

| Issue | Check | Solution |
|-------|-------|----------|
| Step fails | `runs/{run_folder}/validation/step_X_*.md` | Review validation feedback |
| Missing context | `plan.json` dependencies | Update context dependencies |
| Wrong tools | `learnings/*.md` | Learning agents enhance plan with patterns |
| Progress lost | `runs/{run_folder}/steps_done.json` | Auto-saved after each step |
| Loop never exits | `loop_condition` in plan.json | Ensure condition is specific and measurable |
| Config not applied | `step_config.json` | Verify step ID matches plan.json |

**Debug Files**:
- `planning/plan.json` - Current plan with patterns and configs
- `planning/step_config.json` - Per-step agent configurations
- `runs/{run_folder}/validation/*.md` - Validation reports with loop reasoning
- `runs/{run_folder}/execution/*.md` - Execution outputs
- `learnings/*.md` - Accumulated patterns
- `runs/{run_folder}/steps_done.json` - Progress tracking

---

## 📖 Usage

```bash
./orchestrator workflow \
  --objective "Build CI/CD pipeline" \
  --workspace "./workspace"

# Human decisions:
# 1. Variables: Use/Extract new/Update?
# 2. Plan: Use/Create/Update?
# 3. Run Mode: Use Same Run / Create New Run?
# 4. Execution Mode: Normal / Fast Execute / Skip Human?
# 5. Each step: Approve/Re-execute/Stop?
```

---

**Part of the MCP Agent project**
