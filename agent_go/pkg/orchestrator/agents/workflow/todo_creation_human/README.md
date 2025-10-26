# Human-Controlled Todo Creation Multi-Agent Orchestrator

## 📋 Overview

The Human-Controlled Todo Creation workflow is a multi-agent system that creates high-quality, validated todo lists by executing a plan step-by-step, learning from successes and failures, and synthesizing optimal execution strategies.

**Key Features:**
- 🎯 **Human-in-the-Loop**: Human approval at critical decision points
- 🔄 **Learning-Based**: Captures success patterns and failure anti-patterns
- 📊 **Validation-Driven**: Every step is validated before proceeding
- 🤖 **Multi-Agent**: Specialized agents for planning, execution, validation, learning, and writing
- 📝 **Markdown-Based**: Uses structured markdown for plans and outputs

---

## 🏗️ Architecture Overview

### **Agent Flow Diagram**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    HUMAN-CONTROLLED TODO CREATION                    │
│                         Multi-Agent Workflow                         │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  Phase 1: PLANNING                                                   │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────┐
    │ Planning Agent   │  Creates initial plan
    │                  │  → Writes plan.md
    └────────┬─────────┘
             │
             ▼
    ┌──────────────────┐
    │ 👤 HUMAN REVIEW  │  Approves/Rejects/Modifies plan
    └────────┬─────────┘
             │
             ▼
    ┌──────────────────┐
    │ Plan Reader      │  Converts plan.md → JSON
    │    Agent         │  → Returns structured data
    └────────┬─────────┘
             │
             ▼

┌─────────────────────────────────────────────────────────────────────┐
│  Phase 2: STEP-BY-STEP EXECUTION (For Each Step)                    │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────┐
    │ Execution Agent  │  Executes step using MCP tools
    │                  │  → Returns results
    └────────┬─────────┘
             │
             ▼
    ┌──────────────────┐
    │ Validation Agent │  Validates success criteria
    │                  │  → Creates validation report
    └────────┬─────────┘
             │
        ┌────┴────┐
        │         │
    PASS│         │FAIL
        │         │
        ▼         ▼
    ┌────────┐  ┌──────────────────┐
    │Success │  │Failure Learning  │  Analyzes failure
    │Learning│  │     Agent        │  → Updates plan.md
    │ Agent  │  │                  │  → Provides retry guidance
    └───┬────┘  └────────┬─────────┘
        │                │
        │                ▼
        │       ┌──────────────────┐
        │       │ 👤 HUMAN REVIEW  │  Reviews failure analysis
        │       └────────┬─────────┘
        │                │
        │                ▼
        │       ┌──────────────────┐
        │       │ Execution Agent  │  Retries with improvements
        │       │    (Retry)       │
        │       └────────┬─────────┘
        │                │
        └────────────────┘
                         │
                         ▼
              [Continue to next step]

┌─────────────────────────────────────────────────────────────────────┐
│  Phase 3: SYNTHESIS                                                  │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────┐
    │  Writer Agent    │  Reads all validation + learning
    │                  │  → Creates todo_final.md
    └────────┬─────────┘
             │
             ▼
    ┌──────────────────┐
    │ 👤 HUMAN REVIEW  │  Reviews final todo list
    │    & EXECUTE     │
    └──────────────────┘
```

---

## 🤖 Agent Roles & Responsibilities

### **1. Planning Agent**
- **Role**: Create initial execution plan
- **Input**: User objective, workspace path
- **Output**: `planning/plan.md` (structured markdown plan)
- **Responsibilities**:
  - Break down objective into executable steps
  - Define success criteria for each step
  - Specify context dependencies and outputs
  - Create logical execution order

**File Permissions:**
- **WRITE**: `planning/plan.md`

---

### **2. Plan Reader Agent**
- **Role**: Convert markdown plan to structured JSON
- **Input**: `planning/plan.md`
- **Output**: Structured JSON (PlanningResponse)
- **Responsibilities**:
  - Parse markdown plan structure
  - Extract step details (title, description, success criteria, etc.)
  - Convert to JSON for execution orchestrator
  - Read-only agent (no file writing)

**File Permissions:**
- **READ**: `planning/plan.md`
- **OUTPUT**: Returns JSON (no files written)

---

### **3. Execution Agent**
- **Role**: Execute individual plan steps using MCP tools
- **Input**: Step details, context dependencies
- **Output**: Execution results (in response)
- **Responsibilities**:
  - Use MCP tools to accomplish step objective
  - Read context files from previous steps
  - Create context output files for next steps
  - Provide detailed execution summary
  - Document tool usage and results

**File Permissions:**
- **READ**: `planning/plan.md`, context files from previous steps
- **WRITE**: Context output files (if specified in step)

---

### **4. Validation Agent**
- **Role**: Verify step completion and success criteria
- **Input**: Execution history, step details
- **Output**: `validation/step_X_validation_report.md`, ValidationResponse (JSON)
- **Responsibilities**:
  - Check if success criteria was met
  - Analyze execution evidence
  - Identify issues and provide feedback
  - Document validation results
  - Return structured JSON verdict

**File Permissions:**
- **READ**: `planning/plan.md`, context files, execution output
- **WRITE**: `validation/step_X_validation_report.md`

---

### **5. Success Learning Agent**
- **Role**: Capture best practices from successful executions
- **Input**: Execution history, validation results (success)
- **Output**: `learnings/success_patterns.md`, `learnings/step_X_learning.md`, updated `planning/plan.md`
- **Responsibilities**:
  - Identify what worked well
  - Extract success patterns (tools, approaches)
  - Update plan.md with success patterns
  - Document best practices
  - Improve step descriptions for future reference

**File Permissions:**
- **READ**: `planning/plan.md`, `validation/step_X_validation_report.md`
- **WRITE**: `learnings/success_patterns.md`, `learnings/step_X_learning.md`, `planning/plan.md`

---

### **6. Failure Learning Agent**
- **Role**: Analyze failures and provide retry guidance
- **Input**: Execution history, validation results (failure)
- **Output**: `learnings/failure_analysis.md`, `learnings/step_X_learning.md`, updated `planning/plan.md`, refined task description
- **Responsibilities**:
  - Root cause analysis of failures
  - Identify failure patterns (tools to avoid)
  - Update plan.md with failure patterns
  - Provide refined task description for retry
  - Suggest alternative approaches

**File Permissions:**
- **READ**: `planning/plan.md`, `validation/step_X_validation_report.md`
- **WRITE**: `learnings/failure_analysis.md`, `learnings/step_X_learning.md`, `planning/plan.md`

---

### **7. Writer Agent**
- **Role**: Synthesize final optimized todo list
- **Input**: All validation reports, learning files, plan.md
- **Output**: `todo_final.md` (workspace root)
- **Responsibilities**:
  - Read all execution data and learnings
  - Extract success patterns and failure anti-patterns
  - Create structured, executable todo list
  - Include specific tool recommendations
  - Provide execution guidelines for next LLM

**File Permissions:**
- **READ**: `planning/plan.md`, `validation/*.md`, `learnings/*.md`
- **WRITE**: `todo_final.md` (workspace root)

---

## 📁 Workspace Structure

```
{{WorkspacePath}}/
├── todo_creation_human/              (Planning workspace - temporary)
│   ├── planning/
│   │   └── plan.md                   (Execution plan - created by Planning Agent)
│   │
│   ├── validation/
│   │   ├── step_1_validation_report.md    (Created by Validation Agent)
│   │   ├── step_2_validation_report.md
│   │   └── step_N_validation_report.md
│   │
│   └── learnings/
│       ├── success_patterns.md            (Created by Success Learning Agent)
│       ├── failure_analysis.md            (Created by Failure Learning Agent)
│       └── step_X_learning.md             (Per-step learning details)
│
└── todo_final.md                     (Final todo list - created by Writer Agent)
```

---

## 🔄 Detailed Workflow

### **Phase 1: Planning**

1. **Planning Agent** creates initial plan
   - Analyzes user objective
   - Breaks down into executable steps
   - Defines success criteria, context dependencies
   - Writes `planning/plan.md`

2. **Human Reviews Plan**
   - Approves: Continue to execution
   - Rejects: Planning agent creates new plan
   - Modifies: Human edits plan.md directly

3. **Plan Reader Agent** converts plan to JSON
   - Parses markdown structure
   - Extracts all step details
   - Returns structured JSON for orchestrator

---

### **Phase 2: Step-by-Step Execution**

For each step in the plan:

#### **2.1 Execution**
- **Execution Agent** receives step details
- Uses MCP tools to accomplish objective
- Creates context output files if needed
- Returns detailed execution summary

#### **2.2 Validation**
- **Validation Agent** receives execution history
- Checks if success criteria was met
- Analyzes tool usage and results
- Creates validation report
- Returns JSON verdict: PASS/FAIL/PARTIAL/INCOMPLETE

#### **2.3a If Validation PASSES → Success Learning**
- **Success Learning Agent** analyzes what worked
- Extracts success patterns (specific tools, approaches)
- Updates `plan.md` with success patterns
- Documents learnings in `learnings/` folder
- Continue to next step

#### **2.3b If Validation FAILS → Failure Learning**
- **Failure Learning Agent** performs root cause analysis
- Identifies what went wrong and why
- Updates `plan.md` with failure patterns
- Provides refined task description for retry
- **Human Reviews** failure analysis and retry guidance
- **Execution Agent Retries** with improvements
- Loop back to Validation

---

### **Phase 3: Synthesis**

1. **Writer Agent** synthesizes final todo list
   - Reads all validation reports
   - Reads all learning files
   - Extracts success patterns and failure anti-patterns
   - Creates structured `todo_final.md` with:
     - Detailed step descriptions
     - Success criteria
     - Context dependencies
     - Success patterns (what worked)
     - Failure patterns (what to avoid)
     - Execution guidelines

2. **Human Reviews** final todo list
   - Ready for execution by another LLM
   - Can be used as a template for similar tasks

---

## 🔑 Key Design Principles

### **1. Separation of Concerns**
Each agent has a single, well-defined responsibility:
- Planning ≠ Execution ≠ Validation ≠ Learning ≠ Writing

### **2. Context Sharing via Files**
Agents communicate through workspace files:
- No direct agent-to-agent output passing
- All agents read from workspace independently
- Clear file ownership and permissions

### **3. Learning-Based Improvement**
- Success patterns captured and propagated
- Failure patterns identified and avoided
- Plan continuously improves with learnings

### **4. Human-in-the-Loop**
Human intervention at critical points:
- Plan approval/modification
- Failure analysis review
- Final todo list review

### **5. Validation-Driven**
Every step must pass validation:
- Success criteria verification
- Evidence-based validation
- Retry with improvements on failure

---

## 📊 Data Flow

### **Forward Flow (Planning → Execution → Writing)**
```
Objective
    │
    ▼
Planning Agent → plan.md
    │
    ▼
Plan Reader Agent → JSON
    │
    ▼
Execution Agent → Execution Results
    │
    ▼
Validation Agent → validation_report.md
    │
    ▼
Learning Agents → learnings/*.md + updated plan.md
    │
    ▼
Writer Agent → todo_final.md
```

### **Feedback Loop (Failure → Retry)**
```
Validation FAIL
    │
    ▼
Failure Learning Agent
    │
    ├─→ learnings/failure_analysis.md
    ├─→ learnings/step_X_learning.md
    └─→ Updated plan.md with failure patterns
    │
    ▼
Human Review
    │
    ▼
Execution Agent (Retry with improvements)
    │
    ▼
Validation Agent
```

---

## 🎯 Example Walkthrough

### **Objective**: "Extract database URLs from config files"

### **Step 1: Planning**
**Planning Agent** creates:
```markdown
# Plan: Extract Database URLs

## Steps

### Step 1: Read Configuration Files
- **Description**: Use fileserver tools to read database config files
- **Success Criteria**: All config files read and database sections identified
- **Why This Step**: Need to access config data before extracting URLs
- **Context Dependencies**: none
- **Context Output**: step_1_config_contents.md
```

### **Step 2: Execution**
**Execution Agent** executes:
- Uses `fileserver.read_file` on `config/database.json`
- Finds 3 database connection strings
- Creates `step_1_config_contents.md` with results

### **Step 3: Validation**
**Validation Agent** checks:
- ✅ Files were read successfully
- ✅ Config contents documented
- ✅ Context output file created
- **Verdict**: PASS

### **Step 4: Success Learning**
**Success Learning Agent**:
- Identifies: `fileserver.read_file` worked well
- Updates `plan.md` with success pattern:
  - "Use fileserver.read_file for config files (fast, reliable)"
- Documents in `learnings/success_patterns.md`

### **Step 5: Final Synthesis**
**Writer Agent** creates `todo_final.md`:
```markdown
### Step 1: Read Configuration Files

**Success Patterns (What Worked):**
- Used fileserver.read_file with path="config/database.json"
- Read operation completed in < 1 second
- Successfully extracted 245 lines of config data
```

---

## 🛠️ Configuration

### **Shared Memory Requirements** (`memory.go`)
- Workspace directory structure
- Core principles (relative paths, file discovery)
- Available to all agents

### **Agent-Specific Requirements**
- File permissions (read/write specific to each agent)
- Evidence collection guidelines
- Output format specifications
- Embedded in individual agent prompts

---

## 🚀 Benefits

### **For Users**
- 🎯 **High-Quality Todos**: Validated, tested, and optimized
- 🧠 **Learning-Based**: Captures what works and what doesn't
- 👥 **Human Control**: Approval at critical decision points
- 📊 **Evidence-Based**: All claims backed by execution evidence

### **For LLMs**
- 📝 **Clear Instructions**: Structured todo with success patterns
- 🔧 **Tool Recommendations**: Specific MCP tools that worked
- ⚠️ **Anti-Patterns**: Know what to avoid
- 🎯 **Validated Approach**: Based on actual execution, not theory

### **For Development**
- 🔄 **Maintainable**: Each agent has single responsibility
- 🧪 **Testable**: Clear inputs/outputs for each agent
- 📦 **Modular**: Agents can be improved independently
- 📊 **Observable**: File-based communication is easy to debug

---

## 📚 File Format Specifications

### **plan.md Format**
```markdown
# Plan: [Objective Title]

## Steps

### Step 1: [Step Name]
- **Description**: [Detailed description]
- **Success Criteria**: [How to verify completion]
- **Why This Step**: [Purpose and contribution]
- **Context Dependencies**: [Files from previous steps]
- **Context Output**: [File this step creates]
- **Success Patterns**: [Optional - tools/approaches that worked]
- **Failure Patterns**: [Optional - approaches to avoid]
```

### **validation_report.md Format**
Contains:
- Step details
- Execution conversation history
- Success criteria analysis
- Validation verdict (PASS/FAIL/PARTIAL/INCOMPLETE)
- Feedback and recommendations

### **todo_final.md Format**
Structured todo list with:
- Objective and context
- Step-by-step execution plan
- Success criteria for each step
- Success patterns (what worked)
- Failure patterns (what to avoid)
- Execution guidelines for next LLM

---

## 🔍 Debugging & Observability

### **Workspace Inspection**
All agent activities leave traces in workspace:
- `planning/plan.md` - Shows current plan with learnings
- `validation/*.md` - Shows execution history and validation results
- `learnings/*.md` - Shows accumulated patterns and insights

### **Human Review Points**
1. After planning - Review `plan.md`
2. After failure - Review failure analysis and retry guidance
3. After synthesis - Review `todo_final.md`

### **Common Issues & Solutions**

| Issue | Cause | Solution |
|-------|-------|----------|
| Step fails validation | Success criteria not met | Failure learning agent analyzes and provides retry guidance |
| Missing context files | Context dependencies incorrect | Update plan.md with correct dependencies |
| Tool selection wrong | Incorrect approach | Learning agents update plan with correct tools |
| Ambiguous success criteria | Criteria too vague | Human reviews and clarifies in plan.md |

---

## 📖 Usage Example

```bash
# Orchestrator creates todo list
./orchestrator todo-create \
  --objective "Extract database URLs from config files" \
  --workspace "./workspace" \
  --mode human-controlled

# Workflow executes:
# 1. Planning Agent creates plan.md
# 2. Human reviews and approves
# 3. Plan Reader converts to JSON
# 4. Each step: Execute → Validate → Learn
# 5. Writer Agent creates todo_final.md
# 6. Human reviews final todo list
```

---

## 🤝 Contributing

When adding new agents or modifying prompts:

1. **Update memory.go** only for shared requirements
2. **Add agent-specific content** to individual agent prompts
3. **Update this README** with new agent roles
4. **Test with real objectives** to validate workflow
5. **Document new patterns** in learnings/

---

## 📝 License

Part of the MCP Agent project.

