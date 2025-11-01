# 🎯 Orchestrator System - Code Patterns & Implementation Guide

## 🚀 **Core Vision: Dual Control System**

**Two orchestrators implementing the same multi-agent system with different control mechanisms:**

| **Aspect** | **Planner (AI-Controlled)** | **Workflow (Human-Controlled)** |
|------------|----------------------------|----------------------------------|
| **Control** | LLM decides `should_continue` | Human sets `workflow_status` |
| **Loop** | `for iteration` with `break` | `switch workflowStatus` with `return` |
| **State** | `OrchestratorState` (memory) | `workflows.workflow_status` (database) |
| **Decision** | `conditionalLLM.Decide()` | Database flags + UI buttons |
| **Objective** | **Always what user types in chat area** | **Always what user types in chat area** |

## 🎯 **Multi-Agent TodoPlannerAgent Architecture** ✅ **NEW**

### **Revolutionary Approach: Iterative Optimization Workflow**

The TodoPlannerAgent has been transformed into a **multi-agent orchestrator** that uses an iterative optimization approach where each iteration focuses on optimizing 2-3 specific steps to find the best methods, then creates an optimal todo list based on comprehensive optimization experience.

### **Centralized Workspace Path Management** ✅ **NEW**

**All multi-agent orchestrators now use centralized workspace path extraction:**

- **Single Source of Truth**: Workspace path is extracted once at `WorkflowOrchestrator` level
- **Parameter Passing**: Workspace path is passed as a clean parameter to all sub-orchestrators
- **Template Consistency**: All prompts use `{{.WorkspacePath}}` template variables
- **No Hardcoded Paths**: Eliminated hardcoded `Workflow/[FolderName]` references
- **Clean Architecture**: Each orchestrator receives workspace path as a parameter

**Implementation Pattern**:
```go
// WorkflowOrchestrator extracts workspace path once
workspacePath := extractWorkspacePathFromObjective(objective)

// Passes to all sub-orchestrators
todoPlannerAgent.CreateTodoList(ctx, objective, workspacePath)
todoExecutionAgent.ExecuteTodos(ctx, objective, workspacePath, runOption)
todoOptimizationAgent.ExecuteRefinement(ctx, objective, workspacePath)
todoReporterAgent.ExecuteReportGeneration(ctx, objective, workspacePath)
```

**Benefits**:
- ✅ **Consistent Pattern**: All orchestrators follow the same workspace path pattern
- ✅ **Centralized Management**: Single source of truth for workspace path extraction
- ✅ **Cleaner Code**: No more hardcoded path references in prompts
- ✅ **Better Maintainability**: Changes to workspace path logic only need to be made in one place
- ✅ **Template Consistency**: All prompts use `{{.WorkspacePath}}` template variables

### **Multi-Agent TodoPlannerAgent Structure**

```
TodoPlannerAgent (Multi-Agent Orchestrator)
├── TodoPlannerPlanningAgent     # Creates/refines step-wise plan with optimization focus
├── TodoPlannerExecutionAgent    # Optimizes specific steps (2-3 per iteration)
├── TodoPlannerValidationAgent   # Validates optimization results and evidence
├── TodoPlannerWriterAgent       # Creates optimal todo list based on optimization experience
├── TodoPlannerCritiqueAgent     # Critiques todo list quality and reproducibility
└── TodoPlannerCleanupAgent      # Manages workspace cleanup
```

### **Execution Flow**

```
Phase 1: Planning → Phase 2: Optimization → Phase 3: Validation → Phase 4: Writing → Phase 5: Critique → Phase 6: Cleanup → Save todo.md
```

#### **Phase 1: Planning Agent**
- **Purpose**: Create/refine comprehensive step-wise plan with optimization focus
- **Approach**: Iterative optimization where each iteration focuses on 2-3 specific steps
- **Output**: Refined plan with optimized steps and methods from previous iterations
- **Workspace**: `Workflow/[FolderName]/todo_creation/planning/`

#### **Phase 2: Execution Agent**
- **Purpose**: Optimize specific steps identified by planning agent (2-3 steps per iteration)
- **Approach**: Test different MCP tools to find optimal methods for selected steps
- **Output**: Optimization results with optimal methods, commands, and evidence
- **Workspace**: `Workflow/[FolderName]/todo_creation/optimization/`

#### **Phase 3: Validation Agent**
- **Purpose**: Validate optimization results and verify optimization evidence
- **Focus**: Verify optimization claims, validate optimal methods, check evidence quality
- **Output**: Optimization validation report with evidence verification
- **Workspace**: `Workflow/[FolderName]/todo_creation/validation/`

#### **Phase 4: Writer Agent**
- **Purpose**: Create comprehensive todo list based on optimization experience
- **Input**: Refined plan + optimization results + validation feedback
- **Output**: Comprehensive todo list with reproducible steps using optimized methods
- **Workspace**: `Workflow/[FolderName]/todo_creation/writing/`

#### **Phase 5: Critique Agent**
- **Purpose**: Critique todo list quality and reproducibility based on optimization experience
- **Focus**: Optimization learning integration, challenge mitigation, optimized step leverage
- **Output**: Comprehensive critique with optimization experience assessment
- **Workspace**: `Workflow/[FolderName]/todo_creation/critique/`

#### **Phase 6: Cleanup Agent**
- **Purpose**: Clean up planning workspace and organize files
- **Tasks**: Archive planning artifacts, preserve important optimization information
- **Output**: Clean workspace ready for execution phase
- **Workspace**: `Workflow/[FolderName]/todo_creation/cleanup/`

### **Workspace Structure**

```
Workflow/[FolderName]/
├── todo_creation/              # Iterative optimization workspace
│   ├── planning/                # Planning agent outputs
│   │   ├── plan.md             # Current plan with optimization focus
│   │   ├── plan_refined.md     # Refined plan with optimized steps
│   │   └── planning_report.md  # Planning summary
│   ├── optimization/            # Execution agent outputs
│   │   ├── optimization_results.md  # Comprehensive optimization results
│   │   ├── optimized_steps.md      # Steps with optimal methods
│   │   ├── challenges.md           # Optimization obstacles
│   │   └── evidence/              # Optimization evidence files
│   ├── validation/             # Validation agent outputs
│   │   ├── optimization_validation_report.md
│   │   └── plan_vs_optimization_analysis.md
│   ├── writing/                # Writer agent outputs
│   │   ├── todo_draft.md       # Initial todo list
│   │   ├── todo_refined.md     # Refined todo list
│   │   └── todo_final.md       # Final todo list
│   ├── critique/               # Critique agent outputs
│   │   ├── critique_report.md  # Quality and reproducibility assessment
│   │   └── optimization_assessment.md
│   └── cleanup/                # Cleanup agent outputs
│       ├── cleanup_report.md
│       └── archived/           # Archived optimization files
├── todo.md                     # FINAL: Saved after optimization completes
└── runs/                       # Execution phase (existing)
```

### **Key Benefits**

- ✅ **Iterative Optimization**: Each iteration focuses on optimizing 2-3 specific steps for manageable progress
- ✅ **Optimal Method Discovery**: Finds the best methods and commands for each step through testing
- ✅ **Comprehensive Optimization Experience**: Builds optimization knowledge across multiple iterations
- ✅ **Evidence-Based Validation**: Validates optimization claims with solid evidence
- ✅ **Reproducible Todo Lists**: Creates todo lists with proven optimal methods
- ✅ **Quality Assurance**: Critique ensures optimization insights are high quality
- ✅ **Clean Workspace**: Organized workspace ready for execution phase
- ✅ **🆕 NEW**: **Iterative Learning**: Each iteration builds on previous optimization experience
- ✅ **🆕 NEW**: **Optimization Progress Tracking**: Uses conditional LLM to determine if enough steps optimized

### **Conditional LLM Integration** ✅ **NEW**

The TodoPlannerOrchestrator uses the `true_false.go` conditional LLM to determine if sufficient optimization progress has been achieved:

#### **Optimization Progress Assessment**
```go
question := `Based on the plan, execution results, validation, and enhanced critique analysis, have we optimized enough steps to create a comprehensive todo list?

Consider:
1. **Optimization Progress**: Have we optimized enough steps to create a complete todo list?
2. **Step Coverage**: Do we have optimized methods for the majority of critical steps?
3. **Todo List Readiness**: Can we create a comprehensive, reproducible todo list with the current optimization results?

If we have optimized enough steps to create a comprehensive todo list, return true. If we need more optimization iterations, return false.`
```

#### **Iterative Loop Logic**
- **Max Iterations**: 10 optimization iterations to prevent infinite loops
- **Loop Condition**: If `objectiveAchieved = false`, loop back to Phase 1 (Planning)
- **Exit Condition**: If `objectiveAchieved = true` or max iterations reached
- **Progressive Optimization**: Each iteration builds on previous optimization experience

#### **Decision Criteria**
The conditional LLM evaluates optimization progress across three key dimensions:
1. **Optimization Progress**: Sufficient steps optimized for comprehensive todo list
2. **Step Coverage**: Optimized methods available for majority of critical steps
3. **Todo List Readiness**: Can create comprehensive, reproducible todo list with current results

### **Fallback Mechanism**

The TodoPlannerAgent now operates exclusively with the multi-agent orchestrator. If the orchestrator fails to initialize, the agent creation will fail with a clear error message, ensuring that only the robust multi-agent approach is used.

## 🎯 **Unified Orchestrator Architecture** ✅ **MAJOR UPDATE**

### **Single Base Orchestrator Pattern**

The orchestrator system has been unified into a single `BaseOrchestrator` that serves both planner and workflow orchestrators, eliminating duplication and ensuring consistent behavior:

#### **Unified Architecture**
```
BaseOrchestrator (Unified Base Class)
├── PlannerOrchestrator           # AI-controlled orchestrator
├── WorkflowOrchestrator          # Human-controlled orchestrator
├── TodoPlannerOrchestrator       # Planning & todo creation
├── TodoExecutionOrchestrator     # Execution & validation  
├── TodoOptimizationOrchestrator  # Refinement only
└── TodoReporterOrchestrator      # Report generation
```

#### **Unified Base Orchestrator Features**

**Core Functionality**:
- **Event Emission**: Automatic orchestrator and agent event emission
- **State Management**: Iteration tracking, step management, result storage
- **Tool Management**: Workspace tools, custom tools, tool registration
- **Conditional LLM**: Shared factory for consistent decision making
- **Context Management**: Orchestrator context for event metadata

**Orchestrator Type Differentiation**:
```go
type OrchestratorType string

const (
    OrchestratorTypePlanner  OrchestratorType = "planner"
    OrchestratorTypeWorkflow OrchestratorType = "workflow"
)
```

**Unified Structure**:
```go
type BaseOrchestrator struct {
    *agents.BaseOrchestratorAgent
    eventBridge            interface{}
    fallbackLogger         utils.ExtendedLogger
    WorkspaceTools         []llmtypes.Tool
    WorkspaceToolExecutors map[string]interface{}
    conditionalLLM         *conditional.ConditionalLLM
    orchestratorType       OrchestratorType
    startTime              time.Time
    
    // Planner-specific state management
    currentIteration int
    currentStepIndex int
    maxIterations    int
    planningResults     []string
    executionResults    []string
    validationResults   []string
    organizationResults []string
    
    // Workflow-specific state management
    objective     string
    workspacePath string
}
```

#### **Automatic Event Emission**

**Orchestrator Events**:
- `OrchestratorStartEvent` - Emitted at orchestrator start
- `OrchestratorEndEvent` - Emitted at orchestrator completion
- `OrchestratorErrorEvent` - Emitted on orchestrator errors

**Agent Events**:
- `OrchestratorAgentStartEvent` - Emitted when agents start
- `OrchestratorAgentEndEvent` - Emitted when agents complete
- `OrchestratorAgentErrorEvent` - Emitted on agent errors

**Automatic Duration Calculation**:
- Start time captured in `Execute()` method
- Duration calculated automatically in `BaseOrchestratorAgent.ExecuteWithInputProcessor()`
- Duration included in all end events

#### **Conditional LLM Factory**

**Centralized Creation**:
```go
// Shared factory ensures consistent conditional LLM setup
func NewConditionalLLMFactory(config *OrchestratorConfig, eventBridge interface{}) *ConditionalLLMFactory {
    return &ConditionalLLMFactory{
        config:      config,
        eventBridge: eventBridge,
    }
}

// Automatic event emission configuration
func (f *ConditionalLLMFactory) CreateConditionalLLM() *ConditionalLLM {
    return &ConditionalLLM{
        llm:         f.createLLM(),
        eventBridge: f.eventBridge,
    }
}
```

**Event Emission**:
- Only emits end events (`OrchestratorAgentEndEvent`)
- No start events to reduce noise
- Automatic error event emission on failures

#### **Interface Compliance**

**OrchestratorAgent Interface**:
```go
type OrchestratorAgent interface {
    Initialize(ctx context.Context) error
    Execute(ctx context.Context, templateVars map[string]string, conversationHistory []string) (string, error)
    Close() error
    GetBaseAgent() *agents.BaseAgent
    SetOrchestratorContext(stepIndex, iteration int, objective, agentName string)
}
```

**Required Methods**:
- `SetOrchestratorContext()` - Added to comply with interface requirements
- All other methods inherited from `BaseOrchestratorAgent`

#### **Benefits of Unification**

- ✅ **Eliminated Duplication**: Removed separate `BasePlannerOrchestrator` and `BaseWorkflowOrchestrator`
- ✅ **Consistent Behavior**: All orchestrators use same event emission patterns
- ✅ **Automatic Event Emission**: No manual event calls needed
- ✅ **Reduced Code**: ~2000+ lines of duplicate code removed
- ✅ **Easier Maintenance**: Single base class to maintain
- ✅ **Future-Proof**: New orchestrators automatically inherit all functionality
- ✅ **Interface Compliance**: All orchestrators implement required interfaces

## 🎯 **Agent Architecture Patterns**

### **Planner Orchestrator (AI-Controlled)**
- **5 Main Agents**: Planning, Execution, Validation, Organizer, Report
- **Control**: AI decides `should_continue` via conditional LLM
- **Pattern**: `for iteration` loop with AI-controlled `break`
- **Sub-Agents**: None (all single-purpose agents)
- **Creation**: All agents created upfront during initialization

### **Workflow Orchestrator (Human-Controlled)**
- **3 Main Orchestrators**: Todo Planner, Todo Execution, Todo Optimization
- **Control**: Human sets `workflow_status` via database flags
- **Pattern**: `switch workflowStatus` with human-controlled phases
- **Sub-Agents**: 6 specialized (Planning, Execution, Validation, Writer, Cleanup, Critique, Refine Planner, Data Critique, Workspace Update)
- **Creation**: Orchestrators created on-demand per phase
- **Advanced**: Iterative refinement with critique feedback system
- **🆕 NEW**: **Multi-Agent TodoPlannerAgent**: TodoPlannerAgent is now a multi-agent orchestrator with 6 sub-agents implementing iterative optimization workflow
- **🆕 NEW**: **Separated Orchestrators**: Clean separation of responsibilities with dedicated orchestrators for each workflow phase
- **🆕 REMOVED**: **Report Generation**: Report generation step removed from workflow - now focuses on Planning → Execution → Refinement

## 🎯 **Objective Handling: User Input Only**

**CRITICAL**: The objective parameter in both orchestrators must **ALWAYS** be what the user types in the chat area:

- ✅ **Source**: User input from chat area (`currentQuery` state)
- ✅ **Processing**: File context appended during execution (`queryWithContext`)
- ✅ **Passing**: Direct parameter passing through execution chain
- ❌ **NOT**: Stored in database or retrieved from stored data
- ❌ **NOT**: Modified or transformed before execution

**Flow**:
```
User types in chat → currentQuery → req.Query → objective parameter → All agents
```

## 🗄️ **Database Architecture: No Objective Storage**

**IMPORTANT**: Objectives are **NOT** stored in the database:

- ❌ **Removed**: `objective` field from `workflows` table
- ✅ **Clean**: Workflows table only stores `workflow_status` and metadata
- ✅ **Dynamic**: Objectives come from user input during execution
- ✅ **Migration**: `003_remove_objective_from_workflows.sql` handles cleanup

**Database Schema**:
```sql
-- Workflows table (NO objective field)
CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    preset_query_id TEXT NOT NULL,
    workflow_status TEXT DEFAULT 'pre-verification',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## 📁 **Essential File Structure**

### **Core Orchestrator Files**
```
agent_go/pkg/orchestrator/types/
├── planner_orchestrator.go      # AI-controlled orchestrator (1291 lines)
├── workflow_orchestrator.go     # Human-controlled orchestrator (1697 lines)
├── orchestrator_utils.go        # Shared utilities (187 lines)
└── context_aware_bridge.go      # Event bridge wrapper
```

### **Agent Implementation Files**
```
agent_go/pkg/orchestrator/agents/
├── planner/                     # Planner agents
│   ├── planning_agent.go
│   ├── execution_agent.go
│   ├── validation_agent.go
│   └── organizer_agent.go
├── workflow/                    # Workflow orchestrators (refactored)
│   ├── todo_creation/           # Todo planning orchestrator
│   │   └── todo_planner_orchestrator.go
│   ├── todo_execution/          # Todo execution orchestrator
│   │   └── todo_execution_orchestrator.go
│   ├── todo_optimization/       # Todo refinement orchestrator
│   │   ├── todo_optimization_orchestrator.go
│   │   ├── data_critique_agent.go
│   │   ├── report_generation_agent.go
│   │   └── todo_refine_planner_agent.go
│   └── base_workflow_orchestrator.go  # Shared base orchestrator
└── prompts/                     # Agent prompts
    ├── planning.go
    ├── execution.go
    ├── validation.go
    └── memory.go
```

## 🔧 **Core Implementation Patterns**

### **1. Centralized Event Bridge Connection** ✅ **NEW**

#### **Automatic Event Emission for All Multi-Agent Orchestrators**

All multi-agent orchestrators now automatically emit detailed events to the frontend through centralized logic in `orchestrator_utils.go`:

```go
// Centralized event bridge connection in orchestrator_utils.go
func (ou *OrchestratorUtils) setupAgent(
    agent agents.OrchestratorAgent,
    agentType, agentName string,
    customTools []llmtypes.Tool,
    customToolExecutors map[string]interface{},
    eventBridge EventBridge,
) error {
    ctx := context.Background()
    if err := agent.Initialize(ctx); err != nil {
        return fmt.Errorf("failed to initialize %s: %w", agentName, err)
    }

    // Connect event bridge and emit actual agent events
    if eventBridge != nil {
        baseAgent := agent.GetBaseAgent()
        if baseAgent == nil {
            ou.config.Logger.Infof("ℹ️ Agent %s is a pure orchestrator (no BaseAgent) - skipping agent event connection", agentName)
        } else {
            mcpAgent := baseAgent.Agent()
            if mcpAgent != nil {
                // Create a context-aware event bridge for this sub-agent
                contextAwareBridge := NewContextAwareEventBridge(eventBridge, ou.config.Logger)
                contextAwareBridge.SetOrchestratorContext(agentType, 0, 0, agentName)

                // Connect the event bridge to receive detailed agent events
                mcpAgent.AddEventListener(contextAwareBridge)
                ou.config.Logger.Infof("✅ Context-aware bridge connected to %s", agentName)

                // Emit actual agent session start event
                mcpAgent.StartAgentSession(ctx)
                ou.config.Logger.Infof("📤 Called StartAgentSession for %s", agentName)
            }
        }
    }

    return nil
}
```

#### **Event Data Preservation**

The centralized system preserves original event structure while adding orchestrator context:

```go
// ContextAwareEventBridge preserves event data structure
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
    // Add orchestrator context to metadata
    if eventData, ok := event.Data.(interface {
        GetBaseEventData() *events.BaseEventData
    }); ok {
        baseData := eventData.GetBaseEventData()
        if baseData.Metadata == nil {
            baseData.Metadata = make(map[string]interface{})
        }
        baseData.Metadata["orchestrator_phase"] = c.currentPhase
        baseData.Metadata["orchestrator_step"] = c.currentStep
        baseData.Metadata["orchestrator_iteration"] = c.currentIteration
        baseData.Metadata["orchestrator_agent_name"] = c.currentAgentName
    }

    // Forward to underlying bridge
    return c.underlyingBridge.HandleEvent(ctx, event)
}
```

#### **Universal Coverage**

**All orchestrators automatically get event emission:**

- ✅ **WorkflowOrchestrator**: TodoPlannerOrchestrator, TodoExecutionOrchestrator, TodoOptimizationOrchestrator, TodoReporterOrchestrator
- ✅ **PlannerOrchestrator**: OrchestratorPlanningAgent, OrchestratorExecutionAgent, OrchestratorValidationAgent, PlanOrganizerAgent, OrchestratorReportAgent
- ✅ **Any Future Orchestrator**: Automatically inherits event emission through `setupAgent()`

#### **Frontend Benefits**

- ✅ **Complete Event Visibility**: Orchestrator-level + sub-agent detailed events
- ✅ **Event Hierarchy**: Proper parent-child relationship display
- ✅ **Real-time Updates**: Live streaming of LLM generation and tool calls
- ✅ **Context Metadata**: Phase, step, iteration, agent name information
- ✅ **Preserved Data Structure**: Frontend receives expected event formats

### **2. Iterative Optimization Workflow Pattern** ✅ **NEW**

#### **Iterative Optimization Loop with Structured Feedback**

```go
// Iterative optimization loop with structured execution results
func (tpo *TodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective string) (string, error) {
    maxExecutionIterations := 10
    var finalExecutionResult string
    var finalValidationResult string
    var planResult string

    for iteration := 1; iteration <= maxExecutionIterations; iteration++ {
        // Phase 1: Create/Refine comprehensive plan with optimization focus
        structuredExecutionResult := tpo.structureExecutionResults(finalExecutionResult)
        planResult, err := tpo.runPlanningPhase(ctx, structuredExecutionResult, finalValidationResult, iteration)

        // Phase 2: Optimize specific steps (2-3 per iteration)
        executionResult, err := tpo.runExecutionPhase(ctx, planResult, iteration)

        // Phase 3: Validate optimization results and evidence
        validationResult, err := tpo.runValidationPhase(ctx, planResult, iteration, executionResult)

        // Phase 4: Create todo list based on optimization experience
        _, err = tpo.runWriterPhase(ctx, planResult, executionResult, validationResult, "")

        // Phase 5: Critique todo list quality and reproducibility
        critiqueResult, err := tpo.runTodoListCritiquePhase(ctx, objective, iteration)

        // Store results for next iteration
        finalExecutionResult = executionResult
        finalValidationResult = validationResult

        // Check if we have optimized enough steps
        objectiveAchieved, reason, err := tpo.checkObjectiveAchievement(ctx, planResult, executionResult, validationResult, critiqueResult)
        if objectiveAchieved {
            break // Sufficient optimization achieved
        }
    }

    return finalResult, nil
}
```

#### **Structured Execution Results for Planning Agent**

```go
// Enhanced execution results formatting for planning agent
func (tpo *TodoPlannerOrchestrator) structureExecutionResults(executionResult string) string {
    if executionResult == "" {
        return ""
    }

    structuredResult := fmt.Sprintf(`# STRUCTURED EXECUTION RESULTS FOR PLANNING REFINEMENT

## RAW EXECUTION RESULTS
%s

## OPTIMIZATION PARSING INSTRUCTIONS
The planning agent should extract the following from the execution results above:
1. Steps marked as OPTIMIZED - these have optimal methods and should NOT be re-planned
2. Steps marked as COMPLETED - these are done and should be skipped
3. Steps marked as IN_PROGRESS - these are being optimized and should continue
4. Steps marked as PENDING - these need optimization in the next iteration

## KEY SECTIONS TO PARSE
- "OPTIMIZED STEPS SUMMARY" - Contains steps with optimal methods
- "Step Optimization Status" - Shows which steps are optimized
- "Step Optimization Details" - Contains optimal methods and commands
- "Accomplished Steps" - Shows completed steps with evidence

Focus on preserving optimized methods and only refining steps that need improvement.`, executionResult)

    return structuredResult
}
```

#### **Planning Agent with Optimization Focus**

```go
// Planning agent now focuses on iterative optimization
type TodoPlannerPlanningTemplate struct {
    Objective              string
    StructuredExecutionResult string  // ← NEW: Structured optimization results
    ValidationResult       string
    Iteration              int
}

func (tppa *TodoPlannerPlanningAgent) RefinePlan(ctx context.Context, objective, structuredExecutionResult, validationResult string, iteration int) {
    templateVars := map[string]string{
        "Objective":              objective,
        "StructuredExecutionResult": structuredExecutionResult,  // ← NEW: Passed to template
        "ValidationResult":       validationResult,
        "Iteration":              fmt.Sprintf("%d", iteration),
    }
    return tppa.ExecuteWithInputProcessor(ctx, templateVars, tppa.planRefinementInputProcessor, nil)
}
```

#### **Execution Agent with Step Optimization Focus**

```go
// Execution agent now focuses on optimizing specific steps
type TodoPlannerExecutionTemplate struct {
    Objective    string
    PlanResult   string
    Iteration    int
}

func (tpea *TodoPlannerExecutionAgent) OptimizeSteps(ctx context.Context, objective, planResult string, iteration int) {
    // Focus on:
    // 1. Step Optimization: Test different approaches for 2-3 specific steps
    // 2. Method Discovery: Find optimal methods and commands
    // 3. Evidence Collection: Document optimization results and evidence
    // 4. Status Tracking: Mark steps as OPTIMIZED, IN_PROGRESS, PENDING, COMPLETED
}
```

#### **Enhanced Conditional Decision Making**

```go
// Improved question for determining if optimization is sufficient
question := `Based on the plan, execution results, validation, and enhanced critique analysis, have we optimized enough steps to create a comprehensive todo list?

Consider:
1. **Optimization Progress**: Have we optimized enough steps to create a complete todo list?
2. **Step Coverage**: Do we have optimized methods for the majority of critical steps?
3. **Todo List Readiness**: Can we create a comprehensive, reproducible todo list with the current optimization results?

If we have optimized enough steps to create a comprehensive todo list, return true. If we need more optimization iterations, return false.`

context := fmt.Sprintf(`Objective: %s

Plan: %s
Execution Results: %s
Basic Validation Results: %s
Enhanced Critique Results: %s

Assessment Context:
- This is an iterative optimization workflow where we optimize steps through multiple cycles
- Each iteration focuses on optimizing 2-3 specific steps to find the best methods
- The execution phase tests different approaches to find optimal methods for selected steps
- We want to know if we have optimized enough steps to create a comprehensive todo list
- The goal is to determine if we can proceed to create a comprehensive todo list with optimized methods
- Enhanced critique provides factual accuracy validation and step quality assessment`,
    objective, planResult, executionResult, validationResult, critiqueResult)

objectiveAchieved, reason := tpo.conditionalLLM.Decide(ctx, context, question)
```

#### **Data Flow Architecture**

```
User Input → Objective Parameter → Planning Agent
                                      ↓
Structured Execution Results → Template → Refined Plan
                                      ↓
2-3 Steps Selected → Execution Agent → Optimization Results
                                      ↓
Optimization Evidence → Validation Agent → Validation Report
                                      ↓
Optimization Experience → Writer Agent → Todo List
                                      ↓
Quality Assessment → Critique Agent → Critique Report
                                      ↓
Sufficient Optimization? → Conditional LLM → Decision (continue/exit)
                                      ↓
Yes: Loop Back → No: Exit with Result
```

#### **Key Benefits**
- ✅ **Iterative Optimization**: Each iteration focuses on 2-3 specific steps for manageable progress
- ✅ **Optimal Method Discovery**: Finds the best methods and commands through testing
- ✅ **Structured Feedback**: Planning agent receives structured optimization results
- ✅ **Evidence-Based Validation**: Validates optimization claims with solid evidence
- ✅ **Comprehensive Experience**: Builds optimization knowledge across multiple iterations
- ✅ **Self-Improving**: System learns and improves optimization methods across iterations

### **3. Agent Creation Pattern** (Centralized)

#### **Simplified Agent Creation with Automatic Event Emission**

All orchestrators now use the centralized `setupAgent()` method for automatic event bridge connection:

```go
// WorkflowOrchestrator - automatic event emission
func (wo *WorkflowOrchestrator) createTodoPlannerAgent() (*todo_creation.TodoPlannerOrchestrator, error) {
    config := wo.createAgentConfig("todo_planner", "workflow-todo-planner", 100)
    agent, err := todo_creation.NewTodoPlannerOrchestrator(config, wo.logger, wo.tracer, wo.agentEventBridge)
    if err != nil {
        return nil, fmt.Errorf("todo planner orchestrator creation failed: %w", err)
    }
    
    // ✅ This automatically connects event bridge and emits events
    if err := wo.setupAgent(agent, "todo_planner", "todo planner orchestrator"); err != nil {
        return nil, err
    }
    
    return agent, nil
}

// PlannerOrchestrator - automatic event emission
func (po *PlannerOrchestrator) setupAgent(agent agents.OrchestratorAgent, agentType, agentName string) error {
    config := &OrchestratorConfig{
        Provider:        po.provider,
        Model:           po.model,
        MCPConfigPath:   po.mcpConfigPath,
        Temperature:     po.temperature,
        SelectedServers: po.selectedServers,
        AgentMode:       po.agentMode,
        Logger:          po.logger,
    }

    utils := newOrchestratorUtils(config)

    // ✅ Use shared setup function with automatic event emission
    return utils.setupAgent(
        agent,
        agentType,
        agentName,
        nil, // customTools
        po.customToolExecutors,
        po.contextAwareBridge,
        nil, // Context setting function
    )
}
```

#### **Centralized Setup Logic**

The `orchestrator_utils.go` handles all agent setup automatically:

```go
// Centralized setup with automatic event bridge connection
func (ou *OrchestratorUtils) setupAgent(
    agent agents.OrchestratorAgent,
    agentType, agentName string,
    customTools []llmtypes.Tool,
    customToolExecutors map[string]interface{},
    eventBridge EventBridge,
) error {
    ctx := context.Background()
    
    // Initialize agent
    if err := agent.Initialize(ctx); err != nil {
        return fmt.Errorf("failed to initialize %s: %w", agentName, err)
    }

    // ✅ Automatic event bridge connection
    if eventBridge != nil {
        baseAgent := agent.GetBaseAgent()
        if baseAgent != nil {
            mcpAgent := baseAgent.Agent()
            if mcpAgent != nil {
                // Create context-aware event bridge
                contextAwareBridge := NewContextAwareEventBridge(eventBridge, ou.config.Logger)
                contextAwareBridge.SetOrchestratorContext(agentType, 0, 0, agentName)

                // Connect event bridge
                mcpAgent.AddEventListener(contextAwareBridge)
                
                // Start agent session
                mcpAgent.StartAgentSession(ctx)
            }
        }
    }

    // Register custom tools
    if customTools != nil && customToolExecutors != nil {
        // Tool registration logic...
    }

    return nil
}
```

#### **Key Benefits**

- ✅ **Automatic Event Emission**: No manual event bridge setup needed
- ✅ **Consistent Behavior**: All orchestrators get same event handling
- ✅ **Reduced Code Duplication**: Centralized logic eliminates repetition
- ✅ **Future-Proof**: New orchestrators automatically inherit event emission
- ✅ **Event Data Preservation**: Maintains original event structure

### **4. Event Emission Pattern** (Fully Automated) ✅ **MAJOR UPDATE**

#### **Automatic Event Emission Through BaseOrchestrator**

Event emission is now fully automated through the unified `BaseOrchestrator`:

```go
// Automatic orchestrator event emission in BaseOrchestrator.Execute()
func (bo *BaseOrchestrator) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []string) (string, error) {
    // Automatically emit orchestrator start event
    bo.EmitOrchestratorStart(ctx, templateVars)
    
    // Execute orchestrator logic
    result, err := bo.executeOrchestratorLogic(ctx, templateVars, conversationHistory)
    
    // Automatically emit orchestrator end event
    bo.EmitOrchestratorEnd(ctx, result, err)
    
    return result, err
}

// Automatic agent event emission in BaseOrchestratorAgent.ExecuteWithInputProcessor()
func (boa *BaseOrchestratorAgent) ExecuteWithInputProcessor(ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) (string, error), conversationHistory []string) (string, error) {
    startTime := time.Now()
    
    // Automatically emit agent start event
    boa.EmitAgentStart(ctx, templateVars)
    
    // Execute agent logic
    result, err := inputProcessor(templateVars)
    
    // Automatically emit agent end event with duration
    duration := time.Since(startTime)
    boa.EmitAgentEnd(ctx, result, duration, err)
    
    return result, err
}
```

#### **Redundant Event Emission Removal** ✅ **COMPLETED**

**Issue**: Individual orchestrator files contained redundant manual `EmitAgentEnd` calls that duplicated automatic emission.

**Solution Applied**:
- **Removed 22 redundant calls** across workflow orchestrator files
- **Removed unused variables**: `duration`, `startTime` variables that were no longer needed
- **Automatic emission**: All agent events now emitted automatically through base classes

**Files Cleaned**:
- `todo_planner_orchestrator.go` - Removed 6 redundant `EmitAgentEnd` calls
- `todo_reporter_orchestrator.go` - Removed 4 redundant `EmitAgentEnd` calls  
- `todo_optimization_orchestrator.go` - Removed 4 redundant `EmitAgentEnd` calls
- `todo_execution_orchestrator.go` - Removed 8 redundant `EmitAgentEnd` calls

**Before (Redundant)**:
```go
// Manual event emission - REDUNDANT
planningStartTime := time.Now()
result := planningAgent.Execute(ctx, templateVars, conversationHistory)
planningDuration := time.Since(planningStartTime)
wo.EmitAgentEnd(ctx, "planning", result, planningDuration, nil) // ← REDUNDANT
```

**After (Automatic)**:
```go
// Automatic event emission through base class
result := planningAgent.Execute(ctx, templateVars, conversationHistory)
// Events emitted automatically by BaseOrchestratorAgent.ExecuteWithInputProcessor()
```

#### **Event Flow Architecture**

```
Orchestrator Creation
         ↓
BaseOrchestrator.Execute()
         ↓
Automatic OrchestratorStartEvent
         ↓
Sub-Agent Execution
         ↓
BaseOrchestratorAgent.ExecuteWithInputProcessor()
         ↓
Automatic AgentStartEvent
         ↓
Agent Logic Execution
         ↓
Automatic AgentEndEvent (with duration)
         ↓
BaseOrchestrator.Execute() Completion
         ↓
Automatic OrchestratorEndEvent
         ↓
Frontend Display
```

#### **Event Types Covered**

- ✅ **Orchestrator Events**: `orchestrator_start`, `orchestrator_end`, `orchestrator_error` (automatic)
- ✅ **Agent Events**: `orchestrator_agent_start`, `orchestrator_agent_end`, `orchestrator_agent_error` (automatic)
- ✅ **Conditional LLM Events**: `orchestrator_agent_end` (automatic, end events only)
- ✅ **Duration Calculation**: Automatic duration calculation and inclusion in end events
- ✅ **Context Metadata**: Phase, step, iteration, agent name (automatic)

### **5. Execution Loop Pattern** (With Automatic Events) ✅ **UPDATED**

#### **AI-Controlled (Planner)**
```go
for iteration := startIteration; iteration < maxIterations; iteration++ {
    // Planning phase - automatic event emission through BaseOrchestratorAgent
    planningResult := planningAgent.Execute(ctx, templateVars, conversationHistory)
    
    // Check if should continue
    shouldContinue, err := o.conditionalLLM.Decide(ctx, planningResult.Response)
    if err != nil || !shouldContinue {
        break
    }
    
    // Execution phase - automatic event emission through BaseOrchestratorAgent
    executionResult := executionAgent.Execute(ctx, templateVars, conversationHistory)
    
    // Validation phase - automatic event emission through BaseOrchestratorAgent
    validationResult := validationAgent.Execute(ctx, templateVars, conversationHistory)
}
```

#### **Human-Controlled (Workflow)**
```go
switch workflowStatus {
case "pre-verification":
    // TodoPlannerOrchestrator - automatic event emission for all 6 sub-agents
    result := todoPlannerAgent.Execute(ctx, templateVars, conversationHistory)
    // Emits request_human_feedback event for user approval

case "post-verification":
    // TodoExecutionOrchestrator - automatic event emission through BaseOrchestrator
    result := todoExecutionAgent.Execute(ctx, templateVars, conversationHistory)
    // Human approved - proceed with execution

case "post-verification-todo-refinement":
    // TodoOptimizationOrchestrator - iterative refinement with critique feedback
    // All agents (DataCritiqueAgent, TodoRefinePlannerAgent) get automatic event emission
    result := wo.runRefinement(ctx, objective)
    // Uses DataCritiqueAgent for analytical quality assessment
    // Implements iterative learning with critique feedback
}
```

#### **Event Emission in Execution Loops**

- ✅ **Automatic Sub-Agent Events**: All agent executions emit detailed events automatically
- ✅ **Automatic Orchestrator Events**: Loop start/end events emitted automatically by BaseOrchestrator
- ✅ **Context Preservation**: Each agent gets proper orchestrator context (phase, step, iteration)
- ✅ **Event Hierarchy**: Clear parent-child relationship between orchestrator and sub-agents
- ✅ **Duration Calculation**: Automatic duration calculation for all events

### **6. Critique Agent Pattern** (With Centralized Events)

#### **DataCritiqueAgent Implementation**

```go
// Enhanced critique with analytical quality focus
func (wo *WorkflowOrchestrator) runDataCritiqueIteration(ctx context.Context, objective, inputData, inputPrompt string, iteration int) (string, error) {
    // Create DataCritiqueAgent (replaces TodoCritiqueAgent)
    critiqueAgent, err := wo.createDataCritiqueAgent()
    if err != nil {
        return "", fmt.Errorf("failed to create critique agent: %w", err)
    }

    // Prepare template variables with actual prompt used
    templateVars := map[string]string{
        "objective":          objective,
        "input_data":         inputData,
        "input_prompt":       inputPrompt,  // ← NEW: Actual prompt that generated the data
        "refinement_history": "No refinement history available for first iteration",
        "iteration":          fmt.Sprintf("%d", iteration),
    }

    // Execute critique with focus on analytical quality
    critiqueResult, err := critiqueAgent.Execute(ctx, templateVars, nil)
    return critiqueResult, err
}
```

#### **Critique Report Structure**

```markdown
# Data Critique Report

## Overall Assessment
- **Data Quality**: [Excellent/Good/Fair/Poor]
- **Analytical Quality**: [Excellent/Good/Fair/Poor]  ← NEW: Focus on reasoning depth
- **Factual Accuracy**: [Excellent/Good/Fair/Poor]    ← NEW: MCP server validation
- **Prompt Alignment**: [Excellent/Good/Fair/Poor]    ← NEW: Output matches prompt

## Refinement History Analysis
- Evolution patterns and recurring issues

## Prompt-Output Alignment Assessment
- Requirements met vs. missed
- Instruction compliance assessment

## Factual Accuracy Assessment
- MCP server validation results
- Source quality and reliability

## Analytical Quality Evaluation  ← NEW: Critical Focus
- Depth of analysis assessment
- Reasoning quality evaluation
- Evidence usage analysis
- Critical thinking assessment

## Issues Found
- Structure, content, analytical, factual, and alignment issues

## Specific Feedback
- High/medium/low priority improvements

## Satisfaction Assessment
- Overall quality and refinement effectiveness
```

#### **Enhanced Conditional Decision Making**

```go
// Improved decision criteria for refinement continuation
question := `Analyze the critique report and determine if another refinement iteration is needed.

Focus on these critical criteria:
- FACTUAL ERRORS: Major factual inaccuracies that need correction
- ANALYTICAL GAPS: Significant missing analysis or weak reasoning
- PROMPT MISALIGNMENT: Major deviations from the required task
- QUALITY ISSUES: Issues that would significantly impact the objective achievement

If the critique identifies ANY of these critical issues that would benefit from another refinement iteration, return true. Otherwise, return false.`

context := critiqueResult  // Full critique report with analytical assessment
needsMoreWork, reason := wo.conditionalLLM.Decide(ctx, context, question)
```

### **4. Agent Creation Pattern**

## 👤 **Human Verification: Simple Event-Based Approval**

### **Human Verification Pattern**

Human verification in the workflow orchestrator is a **simple event-based approval system**:

#### **Event Flow**
```
Todo Planner → request_human_feedback Event → HumanVerificationDisplay → User Clicks Approve → Database Update → Next State
```

#### **Implementation**
- **Event Type**: `request_human_feedback` 
- **UI Component**: `HumanVerificationDisplay` with "Approve & Continue" button
- **Action**: Updates `workflow_status` in database to next state
- **Purpose**: Simple checkpoint before execution phase

#### **Key Points**
- **Not Complex**: Just a simple approve/continue mechanism
- **Event-Driven**: Uses existing event system for UI display
- **State-Based**: Database flags control workflow progression
- **User-Friendly**: Clean UI with loading states and clear messaging

## 🧠 **Memory & Workspace Architecture**

### **Dual Memory System**

#### **🧠 Long-term Memory (Graph RAG)**
**Purpose**: Quick facts, decisions, and insights for cross-agent communication

**Tools Available**:
- **`add_memory`**: Store important findings, decisions, and insights in knowledge graph
- **`search_memory`**: Search knowledge graph for relevant past information and context
- **`delete_memory`**: Delete outdated or incorrect memories from the knowledge graph

**Use Cases**:
- **Quick Facts**: Important numbers, dates, key findings
- **Decisions**: Agent decisions, reasoning, conclusions
- **Context**: What other agents discovered, shared insights
- **Temporary Storage**: Information needed for current task flow
- **Cross-Agent Communication**: Sharing findings between agents

**Best Practices**:
- Store concise, factual information
- Use descriptive titles for easy retrieval
- Include relevant context and timestamps
- Store inter-agent coordination details
- Clean up outdated information to maintain memory quality
- Delete memories that are no longer accurate or relevant

#### **📁 Workspace Memory (File System)**
**Purpose**: Detailed documentation, execution evidence, and structured storage

**Folder Structure**:
```
Tasks/[TaskName]/                    # Planner Orchestrator (AI-controlled)
Workflow/[ProjectName]/              # Workflow Orchestrator (Human-controlled)
├── index.md                         # Task overview, objectives, progress
├── plan.md                          # Current execution plan and steps
├── report.md                        # Findings, results, and conclusions
├── evidence/                        # Agent playground/notebook
│   ├── step_[N]_[Description].md    # Step execution details
│   ├── tool_outputs/                # Raw tool outputs and responses
│   ├── screenshots/                 # Visual evidence if applicable
│   └── logs/                        # Execution logs and timestamps
├── progress/                        # Progress tracking
│   ├── completed_steps.md           # List of completed steps
│   ├── pending_steps.md             # List of pending steps
│   └── validation_results.md        # Validation outcomes
└── context/                         # Context and background information
    ├── requirements.md              # Task requirements
    ├── constraints.md               # Limitations and constraints
    └── resources.md                 # Available resources and tools
```

**Use Cases**:
- **Detailed Documentation**: Step-by-step execution evidence
- **Large Outputs**: Tool responses, detailed analysis
- **Structured Data**: Plans, reports, organized findings
- **Permanent Storage**: Long-term task documentation
- **Evidence Trail**: Complete audit trail of work performed

### **Memory Integration in Agent Prompts**

#### **Long-term Memory Requirements**
```go
// Include in all agent prompts
ltm := memory.NewLongTermMemory()
prompt += ltm.GetLongTermMemoryRequirements()
```

#### **Workspace Requirements**
```go
// Include workspace guidance in agent prompts
prompt += `
## 📁 Workspace Memory (File System) - Detailed Documentation
1. **Check Previous Work**: get_workspace_file_nested → read_workspace_file
2. **Store Plan**: Update plan.md with current step
3. **Update Progress**: Mark step in progress tracking
4. **Store Evidence**: Create step_[N]_[Description].md in evidence folder
5. **Git Sync**: Use sync_workspace_to_github tool to sync changes
6. **Basic Cleanup**: Remove duplicates, organize structure
`
```

### **Memory System Selection Guide**

#### **Use Long-term Memory (Graph RAG) for:**
- Quick facts, key decisions, important findings
- Context from other agents, cross-agent communication
- Temporary storage for current task flow
- Memory cleanup and maintenance

#### **Use Workspace Memory (File System) for:**
- Detailed documentation and execution evidence
- Large outputs and structured data
- Permanent storage and evidence trails
- Agent playground/notebook functionality

### **Multi-Agent System Awareness**
All agents are part of a multi-agent orchestrator system:
- **Planning Agent**: Creates execution plans and step definitions
- **Execution Agent**: Executes planned steps using MCP tools
- **Validation Agent**: Validates execution results and quality
- **Organizer Agent**: Manages memory organization and cleanup

**Inter-Agent Coordination**:
- **Read Previous Work**: Check evidence/ folder for outputs from other agents
- **Share Your Work**: Document file paths in your output for other agents
- **Context Continuity**: Reference and build upon other agents' work

### **Frontend Integration**

#### **Folder Validation**
The frontend validates folder selection based on orchestrator type:
- **Orchestrator Mode**: Requires `Tasks/` folder selection
- **Workflow Mode**: Requires `Workflow/` folder selection

#### **User Experience**
- **Visual Indicators**: Orange warning colors when required folders not selected
- **Clear Messaging**: Mode-specific guidance for folder selection
- **Validation Logic**: Prevents submission without proper folder context

#### **Implementation**
```typescript
// Frontend validation logic
const isRequiredFolderSelected = useMemo(() => {
  if (agentMode === 'orchestrator') {
    return chatFileContext.some(file => 
      file.type === 'folder' && file.path.startsWith('Tasks/')
    );
  } else if (agentMode === 'workflow') {
    return chatFileContext.some(file => 
      file.type === 'folder' && file.path.startsWith('Workflow/')
    );
  }
  return true;
}, [agentMode, chatFileContext]);
```

## 📡 **Event System Integration**

### **Event Flow**
```
Orchestrator → ContextAwareEventBridge → EventObserver → EventStore → Frontend Polling
```

### **Key Event Types**
- **Orchestrator Events**: `orchestrator_start`, `orchestrator_end`, `orchestrator_error`
- **Agent Events**: `orchestrator_agent_start`, `orchestrator_agent_end`, `orchestrator_agent_error`
- **Workflow Events**: `workflow_start`, `workflow_end`, `request_human_feedback`
- **Human Verification**: `request_human_feedback` - Simple approve/continue checkpoint

## 🚀 **Quick Start Template**

```go
// 1. Create orchestrator struct
type MyOrchestrator struct {
    provider      string
    model         string
    conditionalLLM *conditional.ConditionalLLM  // or controlStatus string
    agentEventBridge interface{}
    contextAwareBridge *ContextAwareEventBridge
    logger utils.ExtendedLogger
}

// 2. Implement execution method
func (o *MyOrchestrator) Execute(ctx context.Context, objective string) (string, error) {
    // Choose pattern: AI-controlled loop OR Human-controlled switch
}

// 3. Create agents
func (o *MyOrchestrator) createAgent(agentType string) (agents.OrchestratorAgent, error) {
    // Use agent creation pattern
}

// 4. Emit events
func (o *MyOrchestrator) emitOrchestratorAgentEvent(...) {
    // Use event emission pattern
}
```

## ✅ **Success Criteria Checklist** ✅ **UPDATED**

- [ ] **Unified Base Orchestrator (CRITICAL)**:
  - [ ] Single `BaseOrchestrator` class serving both planner and workflow orchestrators
  - [ ] `OrchestratorType` enum for differentiation (`planner`, `workflow`)
  - [ ] All orchestrators embed `*orchestrator.BaseOrchestrator`
  - [ ] Automatic event emission through base class
  - [ ] Interface compliance with `SetOrchestratorContext()` method
- [ ] **Automatic Event Emission (CRITICAL)**:
  - [ ] Orchestrator events (`orchestrator_start`, `orchestrator_end`, `orchestrator_error`) emitted automatically
  - [ ] Agent events (`orchestrator_agent_start`, `orchestrator_agent_end`, `orchestrator_agent_error`) emitted automatically
  - [ ] Duration calculation automatic in all end events
  - [ ] No manual event emission calls in individual orchestrator files
  - [ ] Conditional LLM emits only end events (no start events)
- [ ] **Redundant Code Removal (CRITICAL)**:
  - [ ] Removed all redundant `EmitAgentEnd` calls from individual orchestrator files
  - [ ] Removed unused `duration` and `startTime` variables
  - [ ] Cleaned up 22+ redundant event emission calls across workflow orchestrators
  - [ ] No duplicate event emission between orchestrator and sub-agents
- [ ] **Conditional LLM Factory (CRITICAL)**:
  - [ ] Centralized `ConditionalLLMFactory` for consistent creation
  - [ ] Automatic event emission configuration
  - [ ] Shared factory eliminates code duplication
  - [ ] Only end events emitted (no start events)
- [ ] **Centralized Workspace Path Management (CRITICAL)**:
  - [ ] Workspace path extracted once at `WorkflowOrchestrator` level
  - [ ] All sub-orchestrators accept `workspacePath` parameter
  - [ ] All prompts use `{{.WorkspacePath}}` template variables
  - [ ] No hardcoded `Workflow/[FolderName]` references in prompts
  - [ ] Template structs include `WorkspacePath` field
  - [ ] Agent methods accept `workspacePath` parameter
  - [ ] Execute methods extract `WorkspacePath` from templateVars
- [ ] **Clean Orchestrator Architecture (CRITICAL)**:
  - [ ] Single responsibility principle - each orchestrator has one clear purpose
  - [ ] Extends unified `BaseOrchestrator` for common functionality
  - [ ] No mixed responsibilities (e.g., refinement + report generation)
  - [ ] All routing logic handled by `WorkflowOrchestrator`
  - [ ] Consistent pattern across all orchestrators
  - [ ] Easy to add new orchestrators following the same pattern
- [ ] **Objective Handling (CRITICAL)**:
  - [ ] Objective comes from user input (`currentQuery` state)
  - [ ] File context appended during execution (`queryWithContext`)
  - [ ] Direct parameter passing through execution chain
  - [ ] NO database storage of objectives
  - [ ] NO retrieval of objectives from stored data
- [ ] **Iterative Optimization Workflow**:
  - [ ] `TodoPlannerPlanningAgent` focuses on iterative optimization (2-3 steps per iteration)
  - [ ] `TodoPlannerExecutionAgent` optimizes specific steps with method discovery
  - [ ] `TodoPlannerValidationAgent` validates optimization results and evidence
  - [ ] `TodoPlannerWriterAgent` creates todo list based on optimization experience
  - [ ] `TodoPlannerCritiqueAgent` critiques quality based on optimization experience
  - [ ] Structured execution results formatting for planning agent feedback
  - [ ] Enhanced conditional decision making for optimization progress assessment
- [ ] **Long-term Memory Integration**:
  - [ ] `add_memory` tool registration
  - [ ] `search_memory` tool registration
  - [ ] `delete_memory` tool registration
  - [ ] Memory requirements in agent prompts
- [ ] **Workspace Memory Integration**:
  - [ ] Correct folder structure (Tasks/ for planner, Workflow/ for workflow)
  - [ ] File operations (read_workspace_file, update_workspace_file)
  - [ ] Evidence folder management
  - [ ] Progress tracking files
- [ ] **Database Architecture**:
  - [ ] NO `objective` field in `workflows` table
  - [ ] Migration applied (`003_remove_objective_from_workflows.sql`)
  - [ ] Clean workflow status management only
- [ ] **Human Verification (Workflow Only)**:
  - [ ] `request_human_feedback` event emission
  - [ ] `HumanVerificationDisplay` UI component
  - [ ] Database status update on approval
  - [ ] Simple approve/continue flow
- [ ] **Event System Integration**:
  - [ ] `ContextAwareEventBridge` properly configured
  - [ ] Event hierarchy maintained (orchestrator → sub-agent → tool calls)
  - [ ] Event data structure preserved for frontend compatibility
  - [ ] Real-time event streaming to frontend
- [ ] Custom tool registration
- [ ] Error handling and logging
- [ ] Frontend integration (if needed)

## 🔄 **Evolution: From Basic to Advanced Critique System**

### **Original Pattern (Basic)**
```
Refinement Agent → Basic Critique → Simple Decision
- Limited feedback loop
- Surface-level analysis
- Generic satisfaction check
```

### **New Pattern (Advanced)**
```
Refinement Agent → DataCritiqueAgent → Enhanced Decision → Learning Loop
- Factual validation with MCP servers
- Analytical quality assessment
- Prompt-output alignment verification
- Iterative learning with critique feedback
- Multi-dimensional quality evaluation
```

### **Key Improvements**
- ✅ **Factual Accuracy**: MCP server integration for fact-checking
- ✅ **Analytical Quality**: Focus on reasoning depth and evidence quality
- ✅ **Prompt Alignment**: Verification that output matches input requirements
- ✅ **Iterative Learning**: Each refinement learns from previous critique
- ✅ **Enhanced Decision Making**: Specific criteria for refinement continuation
- ✅ **Comprehensive Feedback**: Multi-dimensional critique reports

## 🎯 **Key Files to Reference** ✅ **UPDATED**

### **Core Orchestrator Files**
1. **`planner_orchestrator.go`** - AI-controlled pattern (716 lines) ✅ **UPDATED**
2. **`workflow_orchestrator.go`** - Human-controlled pattern (869 lines) ✅ **UPDATED**
3. **`base_orchestrator.go`** - Unified base orchestrator (657 lines) ✅ **NEW**
4. **`orchestrator_utils.go`** - Centralized event bridge connection (217 lines) ✅ **CRITICAL**
5. **`context_aware_bridge.go`** - Event bridge wrapper with context injection ✅ **CRITICAL**

### **Unified Base Orchestrator Files** ✅ **NEW**
- **`base_orchestrator.go`**: Single unified base class for all orchestrators
- **`conditional/factory.go`**: Shared conditional LLM factory
- **`conditional/conditional_llm.go`**: Conditional LLM with automatic event emission

### **Workflow Orchestrator Files** ✅ **UPDATED**
6. **`agents/workflow/todo_creation/todo_planner_orchestrator.go`** - Multi-agent TodoPlannerOrchestrator (699 lines)
7. **`agents/workflow/todo_execution/todo_execution_orchestrator.go`** - Todo execution orchestrator (363 lines)
8. **`agents/workflow/todo_optimization/todo_optimization_orchestrator.go`** - Todo refinement orchestrator (260 lines)
9. **`agents/workflow/todo_reporter/todo_reporter_orchestrator.go`** - Todo report generation orchestrator (269 lines)

### **Event System Files** ✅ **UPDATED**
- **`orchestrator_utils.go`**: Contains `setupAgent()` method that automatically connects event bridge
- **`context_aware_bridge.go`**: Preserves event data structure while adding orchestrator context
- **`internal/events/`**: Event system integration
- **`frontend/src/components/events/`**: Frontend event handling

### **Architecture Changes Summary** ✅ **NEW**
- **Unified Base**: All orchestrators now use single `BaseOrchestrator` class
- **Automatic Events**: All event emission is automatic through base classes
- **Redundant Code Removed**: 22+ redundant event emission calls removed
- **Interface Compliance**: All orchestrators implement `OrchestratorAgent` interface
- **Conditional LLM Factory**: Centralized factory for consistent conditional LLM creation

## 🔧 **Recent Architecture Changes** ✅ **MAJOR UPDATE**

### **Base Orchestrator Unification** ✅ **COMPLETED**

**Issue**: Separate `BasePlannerOrchestrator` and `BaseWorkflowOrchestrator` classes created code duplication and inconsistent behavior.

**Solution Applied**:
1. **Unified Base Class**: Created single `BaseOrchestrator` serving both planner and workflow orchestrators
2. **Type Differentiation**: Added `OrchestratorType` enum (`planner`, `workflow`) for behavior differentiation
3. **Interface Compliance**: Added missing `SetOrchestratorContext()` method for `OrchestratorAgent` interface
4. **State Management**: Combined planner-specific and workflow-specific state management in single class

**Files Modified**:
- **Created**: `agent_go/pkg/orchestrator/base_orchestrator.go` - Unified base orchestrator
- **Deleted**: `agent_go/pkg/orchestrator/agents/planner/base_planner_orchestrator.go`
- **Deleted**: `agent_go/pkg/orchestrator/agents/workflow/base_workflow_orchestrator.go`
- **Updated**: All orchestrator files to use unified `BaseOrchestrator`

**Key Changes**:
```go
// Unified BaseOrchestrator structure
type BaseOrchestrator struct {
    *agents.BaseOrchestratorAgent
    eventBridge            interface{}
    fallbackLogger         utils.ExtendedLogger
    WorkspaceTools         []llmtypes.Tool
    WorkspaceToolExecutors map[string]interface{}
    conditionalLLM         *conditional.ConditionalLLM
    orchestratorType       OrchestratorType
    startTime              time.Time
    
    // Planner-specific state management
    currentIteration int
    currentStepIndex int
    maxIterations    int
    planningResults     []string
    executionResults    []string
    validationResults   []string
    organizationResults []string
    
    // Workflow-specific state management
    objective     string
    workspacePath string
}

// Orchestrator type differentiation
type OrchestratorType string
const (
    OrchestratorTypePlanner  OrchestratorType = "planner"
    OrchestratorTypeWorkflow OrchestratorType = "workflow"
)
```

**Benefits Achieved**:
- ✅ **Eliminated Duplication**: Removed ~2000+ lines of duplicate code
- ✅ **Consistent Behavior**: All orchestrators use same event emission patterns
- ✅ **Easier Maintenance**: Single base class to maintain
- ✅ **Interface Compliance**: All orchestrators implement required interfaces
- ✅ **Future-Proof**: New orchestrators automatically inherit all functionality

### **Automatic Event Emission Standardization** ✅ **COMPLETED**

**Issue**: Event emission was inconsistent across orchestrators with manual calls and redundant emissions.

**Solution Applied**:
1. **Automatic Orchestrator Events**: `BaseOrchestrator.Execute()` automatically emits start/end events
2. **Automatic Agent Events**: `BaseOrchestratorAgent.ExecuteWithInputProcessor()` automatically emits agent events
3. **Automatic Duration Calculation**: Duration calculated and included in all end events
4. **Redundant Call Removal**: Removed 22+ redundant `EmitAgentEnd` calls from individual orchestrator files

**Files Cleaned**:
- `todo_planner_orchestrator.go` - Removed 6 redundant `EmitAgentEnd` calls
- `todo_reporter_orchestrator.go` - Removed 4 redundant `EmitAgentEnd` calls  
- `todo_optimization_orchestrator.go` - Removed 4 redundant `EmitAgentEnd` calls
- `todo_execution_orchestrator.go` - Removed 8 redundant `EmitAgentEnd` calls

**Event Flow**:
```
Orchestrator Creation
         ↓
BaseOrchestrator.Execute()
         ↓
Automatic OrchestratorStartEvent
         ↓
Sub-Agent Execution
         ↓
BaseOrchestratorAgent.ExecuteWithInputProcessor()
         ↓
Automatic AgentStartEvent
         ↓
Agent Logic Execution
         ↓
Automatic AgentEndEvent (with duration)
         ↓
BaseOrchestrator.Execute() Completion
         ↓
Automatic OrchestratorEndEvent
```

**Benefits Achieved**:
- ✅ **No Manual Calls**: All event emission is automatic
- ✅ **Consistent Timing**: All events include proper duration calculation
- ✅ **Reduced Noise**: Eliminated redundant event emissions
- ✅ **Cleaner Code**: Removed unused duration and startTime variables

### **Conditional LLM Factory Centralization** ✅ **COMPLETED**

**Issue**: Duplicate `createConditionalLLM` functions in separate base orchestrator classes.

**Solution Applied**:
1. **Shared Factory**: Created `ConditionalLLMFactory` in `agent_go/pkg/orchestrator/conditional/factory.go`
2. **Automatic Event Emission**: Factory automatically configures conditional LLM with event bridge
3. **Consistent Creation**: All orchestrators use same factory for conditional LLM creation
4. **End Events Only**: Conditional LLM emits only end events (no start events)

**Files Modified**:
- **Created**: `agent_go/pkg/orchestrator/conditional/factory.go` - Shared factory
- **Updated**: `agent_go/pkg/orchestrator/conditional/conditional_llm.go` - Removed start event emission
- **Updated**: All orchestrator files to use shared factory

**Key Changes**:
```go
// Shared factory for consistent conditional LLM creation
func NewConditionalLLMFactory(config *OrchestratorConfig, eventBridge interface{}) *ConditionalLLMFactory {
    return &ConditionalLLMFactory{
        config:      config,
        eventBridge: eventBridge,
    }
}

// Automatic event emission configuration
func (f *ConditionalLLMFactory) CreateConditionalLLM() *ConditionalLLM {
    return &ConditionalLLM{
        llm:         f.createLLM(),
        eventBridge: f.eventBridge,
    }
}
```

**Benefits Achieved**:
- ✅ **Eliminated Duplication**: Removed duplicate conditional LLM creation code
- ✅ **Consistent Behavior**: All conditional LLMs use same event emission pattern
- ✅ **Automatic Setup**: Event bridge automatically configured
- ✅ **Reduced Noise**: Only end events emitted (no start events)

### **Interface Compliance Fix** ✅ **COMPLETED**

**Issue**: `BaseOrchestrator` was missing `SetOrchestratorContext()` method required by `OrchestratorAgent` interface.

**Solution Applied**:
1. **Added Missing Method**: Implemented `SetOrchestratorContext()` in `BaseOrchestrator`
2. **Interface Compliance**: All orchestrators now properly implement `OrchestratorAgent` interface
3. **Placeholder Implementation**: Method provides interface compliance while actual context setting handled by `ContextAwareEventBridge`

**Key Changes**:
```go
// Added to BaseOrchestrator for interface compliance
func (bo *BaseOrchestrator) SetOrchestratorContext(stepIndex, iteration int, objective, agentName string) {
    // This method is required by the OrchestratorAgent interface
    // The actual context setting is handled by the ContextAwareEventBridge
    // This is a placeholder implementation for interface compliance
    bo.getLogger().Infof("🎯 SetOrchestratorContext called: step %d, iteration %d, objective: %s, agent: %s",
        stepIndex, iteration, objective, agentName)
}
```

**Benefits Achieved**:
- ✅ **Interface Compliance**: All orchestrators implement required interface
- ✅ **Build Success**: No more compilation errors
- ✅ **Clean Architecture**: Proper interface implementation

## 🔧 **Recent Refactoring Changes** ✅ **NEW**

### **PlannerOrchestrator Type Issues Resolution** ✅ **COMPLETED**

**Issue**: Multiple compilation errors in `planner_orchestrator.go` after refactoring to use `BasePlannerOrchestrator` pattern.

**Root Causes Identified**:
1. **Missing Type Definitions**: `PlannerOrchestrator` struct and `LLMConfig` type were accidentally removed
2. **Logger Initialization Order**: `createAgentConfig` was called before `BasePlannerOrchestrator` initialization
3. **Method Signature Mismatches**: Calling code expected different parameter counts for `InitializeAgents` and `ExecuteFlow`

**Solution Applied**:
1. **Restored Type Definitions**: Re-added `PlannerOrchestrator` struct, `LLMConfig` type, and `NewPlannerOrchestrator` constructor
2. **Fixed Logger Initialization**: Modified `createAgentConfig` to accept `logger` parameter instead of using `po.AgentTemplate.GetLogger()`
3. **Updated Method Calls**: All calls to `createAgentConfig` now pass the `logger` instance directly

**Files Modified**:
- `agent_go/pkg/orchestrator/types/planner_orchestrator.go` - Restored missing types and fixed logger initialization
- `agent_go/pkg/orchestrator/agents/base_orchestrator_agent.go` - Added `PlannerOrchestratorAgentType` constant

**Key Changes Made**:
```go
// Restored PlannerOrchestrator struct definition
type PlannerOrchestrator struct {
    *planner.BasePlannerOrchestrator
    // ... other fields
}

// Restored LLMConfig type
type LLMConfig struct {
    Provider              string                        `json:"provider"`
    ModelID               string                        `json:"model_id"`
    FallbackModels        []string                      `json:"fallback_models"`
    CrossProviderFallback *agents.CrossProviderFallback `json:"cross_provider_fallback,omitempty"`
}

// Fixed createAgentConfig signature
func (po *PlannerOrchestrator) createAgentConfig(agentType, agentName string, maxTurns int, logger utils.ExtendedLogger) *agents.OrchestratorAgentConfig {
    // Now uses passed logger instead of po.AgentTemplate.GetLogger()
    Logger: logger,
}

// Updated all calls to pass logger
config := po.createAgentConfig("planner", "planner-orchestrator", 100, logger)
```

**Testing Results**:
- ✅ **Types Package Compiles**: `agent_go/pkg/orchestrator/types/` compiles successfully
- ✅ **No More Undefined Types**: `PlannerOrchestrator` and `LLMConfig` are properly defined
- ✅ **Logger Initialization Fixed**: No more nil pointer dereference errors
- ✅ **Method Signatures Correct**: All method calls match their definitions

**Remaining Work**:
- **External Calling Code**: `cmd/server/server.go` and `cmd/testing/orchestrator-flow-test.go` still have method signature mismatches
- **Next Steps**: Update calling code to match new `InitializeAgents` and `ExecuteFlow` signatures

### **Centralized Workspace Path Management** ✅ **MAJOR**
- **Added**: Centralized workspace path extraction at `WorkflowOrchestrator` level
- **Updated**: All multi-agent orchestrators to accept `workspacePath` parameter
- **Replaced**: Hardcoded `Workflow/[FolderName]` references with `{{.WorkspacePath}}` template variables
- **Consistent**: All orchestrators now follow the same workspace path pattern
- **Clean**: Single source of truth for workspace path extraction

#### **Files Updated for Workspace Path Centralization**
- **`workflow_orchestrator.go`**: Added `extractWorkspacePathFromObjective()` and passes workspace path to all sub-orchestrators
- **`todo_creation/todo_planner_orchestrator.go`**: Updated to accept `workspacePath` parameter (already implemented)
- **`todo_execution/todo_execution_orchestrator.go`**: Updated to accept `workspacePath` parameter and pass to all agents
- **`todo_execution/todo_execution_agent.go`**: Updated method signature and prompt template to use `{{.WorkspacePath}}`
- **`todo_execution/todo_validation_agent.go`**: Updated method signature and prompt template to use `{{.WorkspacePath}}`
- **`todo_execution/todo_workspace_update_agent.go`**: Updated method signature and prompt template to use `{{.WorkspacePath}}`
- **`todo_optimization/todo_optimization_orchestrator.go`**: Updated to pass `WorkspacePath` in templateVars
- **`todo_optimization/todo_refine_planner_agent.go`**: Updated method signature, prompt template, and Execute method
- **`todo_optimization/report_generation_agent.go`**: Updated method signatures, prompt template, and Execute method
- **`todo_reporter/todo_reporter_orchestrator.go`**: Updated to pass `WorkspacePath` in templateVars ✅ **REMOVED**

#### **Key Changes Made**
1. **Template Variables**: Added `WorkspacePath` to all template structs
2. **Method Signatures**: Updated all agent methods to accept `workspacePath` parameter
3. **Prompt Templates**: Replaced hardcoded `Workflow/[FolderName]` with `{{.WorkspacePath}}`
4. **Template Data**: Updated all template data creation to include `WorkspacePath`
5. **Orchestrator Calls**: Updated all orchestrator methods to pass `GetWorkspacePath()` parameter
6. **Execute Methods**: Updated Execute methods to extract `WorkspacePath` from templateVars

#### **Benefits Achieved**
- ✅ **Consistent Pattern**: All orchestrators now follow the same workspace path pattern as `todo_creation`
- ✅ **Centralized Management**: Workspace path is extracted once at `WorkflowOrchestrator` level and passed down
- ✅ **Cleaner Code**: No more hardcoded `Workflow/[FolderName]` references in prompts
- ✅ **Better Maintainability**: Single source of truth for workspace path extraction
- ✅ **Template Consistency**: All prompts now use `{{.WorkspacePath}}` template variables

### **Centralized Event System** ✅ **MAJOR**
- **Added**: `orchestrator_utils.go` with centralized `setupAgent()` method
- **Added**: `context_aware_bridge.go` for event data preservation and context injection
- **Centralized**: All event bridge connection logic in one place
- **Automatic**: Event emission now happens automatically for all multi-agent orchestrators
- **Preserved**: Original event data structure while adding orchestrator context
- **🆕 NEW**: **Multi-Agent Orchestrator Event Control**: Prevents duplicate event emission by having orchestrators return `nil` from `GetBaseAgent()` (only sub-agents emit events)

#### **Duplicate Event Emission Fix** ✅ **RESOLVED**
**Issue**: `todo_planner_execution` agent was appearing twice in events because both the orchestrator and its sub-agent were emitting events.

**Root Cause**: 
- **Path 1**: `WorkflowOrchestrator.setupAgent()` → `orchestrator_utils.go` → `mcpAgent.StartAgentSession()` for TodoPlannerOrchestrator
- **Path 2**: `TodoPlannerOrchestrator.connectAgentToEventBridge()` → `mcpAgent.StartAgentSession()` for TodoPlannerExecutionAgent

**Solution Applied**:
1. **Clean Interface Override**: Modified `TodoPlannerOrchestrator.GetBaseAgent()` to return `nil`
2. **setupAgent() Skip**: When `GetBaseAgent()` returns `nil`, `setupAgent()` skips event emission
3. **Sub-Agent Events Only**: Only sub-agents emit events through their individual `connectAgentToEventBridge()` calls
4. **No Complex Detection**: No need for pattern matching or type checking in utility functions

**Files Modified**:
- `agent_go/pkg/orchestrator/agents/workflow/todo_creation/todo_planner_orchestrator.go` - Override `GetBaseAgent()` to return `nil`
- `orchestrator-patterns.md` - Documented the fix

**Testing Results**:
- ✅ **No Duplicate Events**: `todo_planner_execution` now appears only once
- ✅ **Clean Architecture**: Multi-agent orchestrators don't emit their own events
- ✅ **Sub-Agent Events**: Sub-agents still emit their events correctly
- ✅ **Event Hierarchy**: Maintains proper parent-child relationship
- ✅ **Simple Solution**: No complex detection logic needed

### **Code Simplification**
- **Removed**: Unused agent config functions (`createTodoPlannerAgentConfig`, etc.)
- **Removed**: Unused `registerCustomToolsForAgent` functions
- **Removed**: `ToolExecutor` interface (now uses `map[string]interface{}`)
- **Removed**: Manual event bridge setup from individual orchestrators
- **Simplified**: Agent creation and setup patterns

### **File Organization**
- **Workflow agents**: Organized into `todo_creation/` (planning), `todo_execution/` (execution, validation, workspace updates), `todo_optimization/` (refinement, critique)
- **Shared utilities**: Centralized in `orchestrator_utils.go`
- **Event handling**: Centralized with `context_aware_bridge.go`

### **Type Safety Improvements**
- **EventBridge**: Now uses `*events.AgentEvent` instead of `interface{}`
- **Tool executors**: Simplified to `map[string]interface{}`
- **Custom tools**: Streamlined registration process
- **Event data**: Preserved original structure with context injection

### **Benefits**
- ✅ **Universal Event Emission**: All orchestrators automatically get event emission
- ✅ **Reduced code duplication**: ~300+ lines removed
- ✅ **Better maintainability**: Centralized shared logic
- ✅ **Cleaner architecture**: Simplified type system
- ✅ **Event data preservation**: Frontend receives expected event formats
- ✅ **Future-proof**: New orchestrators automatically inherit event emission

### **Report Generation Step Removal** ✅ **COMPLETED**

**Change**: Removed report generation step from workflow orchestrator to simplify the workflow.

**Files Modified**:
- `agent_go/pkg/orchestrator/types/workflow_orchestrator.go` - Removed report generation phase, case, methods, and imports

**Changes Made**:
1. **Removed Report Phase**: Deleted report generation phase from `GetWorkflowConstants()`
2. **Removed Report Case**: Deleted `case database.WorkflowStatusPostVerificationReportGeneration:` from `ExecuteWorkflow`
3. **Removed Methods**: Deleted `runReportGeneration()` and `createTodoReporterOrchestrator()` methods
4. **Removed Import**: Deleted unused `todo_reporter` import
5. **Updated Event**: Modified execution completion event to only mention refinement

**Current Workflow Phases**:
- **📝 Planning & Todo Creation** - Create comprehensive todo list
- **🚀 Execution & Review** - Execute the approved todo list  
- **🔄 Todo Refinement** - Refine todo list based on execution results

**Benefits**:
- ✅ **Simplified Workflow**: Reduced from 4 phases to 3 phases
- ✅ **Cleaner Architecture**: Removed unused report generation functionality
- ✅ **Better Focus**: Workflow now focuses on core planning, execution, and refinement
- ✅ **Reduced Complexity**: Less code to maintain and fewer potential failure points

**Build Status**:
- ✅ **Build Successful**: All changes compile without errors
- ✅ **No Linting Errors**: Clean code with no warnings
- ✅ **Import Cleanup**: Removed unused `todo_reporter` import