 Task Agent Mode — Full Extraction Plan                                                                                                                         
                                                                                                                                                                
 Goal                                                                                                                                                           
                                                                                                                                                                
 Extract the TodoTask execution engine into a standalone pkg/todotask/ package that can be called from three contexts:                                          
 1. Workflow mode — as a step type within a workflow plan (existing behavior, refactored)                                                                       
 2. Task Agent mode — new 4th frontend mode, direct execution with no planning phase                                                                            
 3. External library — any Go project imports pkg/todotask and implements the interfaces                                                                        
                                                                                                                                                                
 The TodoTask logic currently lives inside step_based_workflow/controller_todo_task.go (~1069 lines) tightly coupled to StepBasedWorkflowOrchestrator. This     
 plan moves the core loop, templates, and types into a dependency-free package.                                                                                 
                                                                                                                                                                
 ---                                                                                                                                                            
 Architecture                                                                                                                                                   
                                                                                                                                                                
 pkg/todotask/                         ← NEW standalone package                                                                                                 
   types.go                            ← Config, Result, RouteConfig, interfaces                                                                                
   interfaces.go                       ← SubAgentExecutor, AgentFactory, EventEmitter, PreValidator                                                             
   run_loop.go                         ← RunLoop() — the core iteration engine                                                                                  
   templates.go                        ← System + User prompt templates (moved from agent)                                                                      
   template_vars.go                    ← BuildTemplateVars() helper                                                                                             
   events.go                           ← Event data structs (plain Go, no framework deps)                                                                       
   logging.go                          ← LogRoutingDecision(), SaveExecutionLog()                                                                               
                                                                                                                                                                
 Callers:                                                                                                                                                       
   step_based_workflow/controller_todo_task.go  ← Refactored: thin adapter → todotask.RunLoop()                                                                 
   pkg/orchestrator/types/task_agent_orchestrator.go  ← NEW: standalone orchestrator                                                                            
   cmd/server/server.go                ← Routes task_agent to TaskAgentOrchestrator                                                                             
   External Go code                    ← Implements interfaces, calls todotask.RunLoop()                                                                        
                                                                                                                                                                
 How RunLoop Works                                                                                                                                              
                                                                                                                                                                
 RunLoop(ctx, config) → *Result                                                                                                                                 
   ├── emit StepStarted                                                                                                                                         
   ├── for iteration < maxIterations:                                                                                                                           
   │     ├── BuildTemplateVars(config, lastResult, lastAgentName, lastTodoID)                                                                                   
   │     ├── AgentFactory.CreateAgent(ctx, params)  ← caller creates LLM agent with tools                                                                       
   │     ├── agent.Execute(ctx, templateVars, history)  ← multi-turn tool execution                                                                             
   │     │     └── (agent calls shell tools, call_sub_agent, call_generic_agent internally)                                                                     
   │     ├── LogRoutingDecision() + SaveExecutionLog()                                                                                                          
   │     ├── emit RouteSelected                                                                                                                                 
   │     ├── if PreValidator != nil → Validate()                                                                                                                
   │     │     ├── passed → emit StepCompleted → return Success                                                                                                 
   │     │     └── failed → lastResult = feedback, continue                                                                                                     
   │     └── switch response.NextAction (legacy path):                                                                                                          
   │           ├── "complete" + AllTasksComplete → return Success                                                                                               
   │           ├── "delegate" → SubAgentExecutor.Execute*() → store result                                                                                      
   │           └── "continue" → next iteration                                                                                                                  
   └── return MaxIterationsReached                                                                                                                              
                                                                                                                                                                
 How Sub-Agent Delegation Works                                                                                                                                 
                                                                                                                                                                
 Sub-agents are NOT called directly by RunLoop. They're called by tool executors during the agent's multi-turn execution:                                       
                                                                                                                                                                
 1. AgentFactory.CreateAgent() receives SubAgentExecutor in its params                                                                                          
 2. Factory registers call_sub_agent + call_generic_agent tools on the agent                                                                                    
 3. Tool executors delegate to SubAgentExecutor.ExecutePredefined() / .ExecuteGeneric()                                                                         
 4. Agent gets tool response back and continues its execution                                                                                                   
 5. RunLoop only sees the final state after agent returns                                                                                                       
                                                                                                                                                                
 The legacy "delegate" switch case handles backward-compatible structured output responses.                                                                     
                                                                                                                                                                
 ---                                                                                                                                                            
 Implementation Phases                                                                                                                                          
                                                                                                                                                                
 Phase 1: Create pkg/todotask/ Package                                                                                                                          
                                                                                                                                                                
 File: agent_go/pkg/todotask/types.go                                                                                                                           
 package todotask                                                                                                                                               
                                                                                                                                                                
 // StepConfig defines the todo task step (maps from TodoTaskPlanStep)                                                                                          
 type StepConfig struct {                                                                                                                                       
     StepID, StepTitle, StepDescription, StepSuccessCriteria string                                                                                             
     ContextDependencies []string                                                                                                                               
     PredefinedRoutes    []RouteConfig                                                                                                                          
     EnableGenericAgent  bool                                                                                                                                   
     NextStepID          string                                                                                                                                 
     HasValidationSchema bool           // opaque — caller passes schema to PreValidator                                                                        
     AgentMaxIterations  *int                                                                                                                                   
 }                                                                                                                                                              
                                                                                                                                                                
 type RouteConfig struct {                                                                                                                                      
     RouteID, RouteName, Condition, Description string                                                                                                          
 }                                                                                                                                                              
                                                                                                                                                                
 // Response from one agent execution iteration                                                                                                                 
 type Response struct {                                                                                                                                         
     NextAction, SelectedRouteID              string                                                                                                            
     UseGenericAgent                          bool                                                                                                              
     TodoIDToExecute                          string                                                                                                            
     InstructionsToSubAgent, SuccessCriteria  string                                                                                                            
     AllTasksComplete                         bool                                                                                                              
     ProgressSummary, CompletionReason        string                                                                                                            
 }                                                                                                                                                              
                                                                                                                                                                
 // RunLoopConfig holds all dependencies injected by the caller                                                                                                 
 type RunLoopConfig struct {                                                                                                                                    
     Step             StepConfig                                                                                                                                
     StepIndex        int                                                                                                                                       
     StepPath         string                                                                                                                                    
     MaxIterations    int                                                                                                                                       
     WorkspacePath    string                                                                                                                                    
     RunFolder        string            // empty if no runs                                                                                                     
     VariableNames    string                                                                                                                                    
     VariableValues   string                                                                                                                                    
     IsCodeExecMode   bool                                                                                                                                      
     UseKnowledgebase bool                                                                                                                                      
                                                                                                                                                                
     // Path config (computed by caller based on workspace + run folder)                                                                                        
     StepExecutionPath      string                                                                                                                              
     ExecutionWorkspacePath string                                                                                                                              
     ShellWorkingDirectory  string                                                                                                                              
                                                                                                                                                                
     // Injected dependencies                                                                                                                                   
     AgentFactory  AgentFactory                                                                                                                                 
     SubAgentExec  SubAgentExecutor    // wired into agent tools by factory                                                                                     
     PreValidator  PreValidator        // nil = skip validation                                                                                                 
     EventEmitter  EventEmitter                                                                                                                                 
     Workspace     WorkspaceIO                                                                                                                                  
     Logger        Logger                                                                                                                                       
 }                                                                                                                                                              
                                                                                                                                                                
 type RunLoopResult struct {                                                                                                                                    
     Success        bool                                                                                                                                        
     NextStepID     string                                                                                                                                      
     Iterations     int                                                                                                                                         
 }                                                                                                                                                              
                                                                                                                                                                
 File: agent_go/pkg/todotask/interfaces.go                                                                                                                      
 package todotask                                                                                                                                               
                                                                                                                                                                
 type SubAgentExecutor interface {                                                                                                                              
     ExecutePredefined(ctx, routeID, todoID, instructions, criteria string) (string, error)                                                                     
     ExecuteGeneric(ctx, todoID, instructions, criteria string) (string, error)                                                                                 
     ValidateTodoExists(ctx, todoID string) (exists bool, total int, path string, err error)                                                                    
 }                                                                                                                                                              
                                                                                                                                                                
 type AgentFactory interface {                                                                                                                                  
     // CreateAgent builds an LLM agent with all tools (workspace, MCP, sub-agent).                                                                             
     // SubAgentExecutor is wired into the call_sub_agent/call_generic_agent tool executors.                                                                    
     CreateAgent(ctx context.Context, params AgentCreateParams) (Agent, error)                                                                                  
 }                                                                                                                                                              
                                                                                                                                                                
 type AgentCreateParams struct {                                                                                                                                
     AgentName  string                                                                                                                                          
     StepIndex  int                                                                                                                                             
     StepID     string                                                                                                                                          
     StepPath   string                                                                                                                                          
 }                                                                                                                                                              
                                                                                                                                                                
 type Agent interface {                                                                                                                                         
     Execute(ctx, templateVars map[string]string, history []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error)                                 
     Close() error                                                                                                                                              
 }                                                                                                                                                              
                                                                                                                                                                
 type PreValidator interface {                                                                                                                                  
     RunPreValidation(ctx, stepExecutionPath string) (passed bool, reason string)                                                                               
 }                                                                                                                                                              
                                                                                                                                                                
 type EventEmitter interface {                                                                                                                                  
     EmitStepStarted(ctx, event StepStartedEvent)                                                                                                               
     EmitStepFinished(ctx, event StepFinishedEvent)                                                                                                             
     EmitRouteSelected(ctx, event RouteSelectedEvent)                                                                                                           
     EmitStepCompleted(ctx, event StepCompletedEvent)                                                                                                           
     EmitPreValidationCompleted(ctx, event PreValidationEvent)                                                                                                  
 }                                                                                                                                                              
                                                                                                                                                                
 type WorkspaceIO interface {                                                                                                                                   
     ReadFile(ctx, path string) (string, error)                                                                                                                 
     WriteFile(ctx, path, content string) error                                                                                                                 
 }                                                                                                                                                              
                                                                                                                                                                
 type Logger interface {                                                                                                                                        
     Info(msg string)                                                                                                                                           
     Warn(msg string)                                                                                                                                           
 }                                                                                                                                                              
                                                                                                                                                                
 File: agent_go/pkg/todotask/templates.go                                                                                                                       
 - Move todoTaskOrchestratorSystemTemplate from todo_task_orchestrator_agent.go:18                                                                              
 - Move todoTaskOrchestratorUserTemplate from todo_task_orchestrator_agent.go:262                                                                               
 - Export as var SystemTemplate and var UserTemplate (*template.Template)                                                                                       
                                                                                                                                                                
 File: agent_go/pkg/todotask/run_loop.go                                                                                                                        
 - func RunLoop(ctx context.Context, cfg RunLoopConfig) (*RunLoopResult, error)                                                                                 
 - Extracted from executeTodoTaskStep() (controller_todo_task.go:35-273)                                                                                        
                                                                                                                                                                
 File: agent_go/pkg/todotask/template_vars.go                                                                                                                   
 - func BuildTemplateVars(cfg RunLoopConfig, lastResult, lastName, lastTodoID string) map[string]string                                                         
 - Extracted from buildTodoTaskOrchestratorTemplateVars() (controller_todo_task.go:275-351)                                                                     
                                                                                                                                                                
 File: agent_go/pkg/todotask/events.go                                                                                                                          
 - Plain data structs: RouteSelectedEvent, StepCompletedEvent, StepStartedEvent, StepFinishedEvent                                                              
 - No dependency on baseevents — the EventEmitter adapter in the caller wraps these into framework events                                                       
                                                                                                                                                                
 File: agent_go/pkg/todotask/logging.go                                                                                                                         
 - func LogRoutingDecision(workspace WorkspaceIO, ...) — extracted from logTodoTaskRoutingDecision()                                                            
 - func SaveExecutionLog(workspace WorkspaceIO, ...) — extracted from saveTodoTaskExecutionLog()                                                                
                                                                                                                                                                
 Phase 2: Refactor controller_todo_task.go → Thin Adapter                                                                                                       
                                                                                                                                                                
 File: agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go                                                                    
                                                                                                                                                                
 The 270-line executeTodoTaskStep() becomes ~60 lines:                                                                                                          
 1. Cast step to TodoTaskPlanStep                                                                                                                               
 2. Build path config (folder guard, execution paths — stays here, controller-specific)                                                                         
 3. Set folder guard (hcpo.SetWorkspacePathForFolderGuard())                                                                                                    
 4. Map TodoTaskPlanStep → todotask.StepConfig                                                                                                                  
 5. Create adapter structs implementing todotask interfaces:                                                                                                    
   - workflowAgentFactory → delegates to hcpo.createTodoTaskOrchestratorAgent()                                                                                 
   - workflowSubAgentExecutor → delegates to hcpo.executePredefinedSubAgent() / executeGenericAgent()                                                           
   - workflowPreValidator → delegates to hcpo.runTodoTaskPreValidation()                                                                                        
   - workflowEventEmitter → wraps events in baseevents.AgentEvent and emits via bridge                                                                          
   - workflowWorkspaceIO → delegates to hcpo.ReadWorkspaceFile() / WriteWorkspaceFile()                                                                         
 6. Call todotask.RunLoop(ctx, config)                                                                                                                          
 7. Return result                                                                                                                                               
                                                                                                                                                                
 The adapter structs are private types in the same file. Existing methods (executeGenericAgent, executePredefinedSubAgent, runTodoTaskPreValidation, emit       
 methods) stay on the controller — they're called by the adapters.                                                                                              
                                                                                                                                                                
 File: agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/todo_task_orchestrator_agent.go                                                            
 - Remove template declarations (lines 18-260 and 262-316)                                                                                                      
 - Import templates from pkg/todotask:                                                                                                                          
 import "mcp-agent-builder-go/agent_go/pkg/todotask"                                                                                                            
 // Use todotask.SystemTemplate and todotask.UserTemplate                                                                                                       
 - Agent struct and Execute() method stay here (still depends on BaseOrchestratorAgent)                                                                         
                                                                                                                                                                
 Phase 3: Create TaskAgentOrchestrator                                                                                                                          
                                                                                                                                                                
 File: agent_go/pkg/orchestrator/types/task_agent_orchestrator.go (NEW)                                                                                         
 type TaskAgentOrchestrator struct {                                                                                                                            
     *orchestrator.BaseOrchestrator                                                                                                                             
     sessionID string                                                                                                                                           
 }                                                                                                                                                              
                                                                                                                                                                
 func (tao *TaskAgentOrchestrator) Execute(ctx, objective, workspacePath string) (string, error) {                                                              
     // 1. Set workspace path                                                                                                                                   
     // 2. Initialize MCP session                                                                                                                               
     // 3. Create synthetic plan via CreateTaskAgentPlan(objective)                                                                                             
     // 4. Create StepBasedWorkflowOrchestrator (same constructor as runHumanControlledPlanning)                                                                
     // 5. Set approved plan directly (no file loading)                                                                                                         
     // 6. Set execution options: FastExecuteAll, create_new_runs_always                                                                                        
     // 7. Call Execute() on the controller                                                                                                                     
     // 8. Skip emitBlockingHumanFeedback (no human approval)                                                                                                   
     // 9. Emit completion events                                                                                                                               
 }                                                                                                                                                              
                                                                                                                                                                
 This reuses the existing StepBasedWorkflowOrchestrator infrastructure for agent creation and sub-agent execution, but bypasses the planning phase entirely.    
                                                                                                                                                                
 File: agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go                                                                              
                                                                                                                                                                
 Modify CreateTodoList() (line 369-377) to accept pre-injected plan:                                                                                            
 if hcpo.approvedPlan != nil {                                                                                                                                  
     existingPlan = hcpo.approvedPlan                                                                                                                           
 } else {                                                                                                                                                       
     planPath := "planning/plan.json"                                                                                                                           
     planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)                                                                                     
     // ... existing error handling                                                                                                                             
 }                                                                                                                                                              
                                                                                                                                                                
 Skip variables.json loading when approvedPlan is pre-set (lines 346-367).                                                                                      
                                                                                                                                                                
 Phase 4: Server Routing                                                                                                                                        
                                                                                                                                                                
 File: agent_go/pkg/database/models.go                                                                                                                          
 - Add: AgentModeTaskAgent = "task_agent"                                                                                                                       
                                                                                                                                                                
 File: agent_go/cmd/server/server.go                                                                                                                            
 - Change if req.AgentMode == "workflow" → if req.AgentMode == "workflow" || req.AgentMode == "task_agent"                                                      
 - Inside the goroutine, before execution:                                                                                                                      
 if req.AgentMode == "task_agent" {                                                                                                                             
     workflowStatus = database.WorkflowStatusPreVerification                                                                                                    
     if workflowWorkspacePath == "" || workflowWorkspacePath == "default_workspace" {                                                                           
         workflowWorkspacePath = fmt.Sprintf("TaskAgent/%s", sessionID)                                                                                         
     }                                                                                                                                                          
     syntheticPlan := orchestrator.CreateTaskAgentPlan(workflowObjective)                                                                                       
     workflowOrchestrator.SetVirtualPlan(syntheticPlan)                                                                                                         
     workflowOrchestrator.SetExecutionOptions(&todo_creation_human.ExecutionOptions{                                                                            
         RunMode:           "create_new_runs_always",                                                                                                           
         ExecutionStrategy: todo_creation_human.ExecutionStrategyFastExecuteAll,                                                                                
     })                                                                                                                                                         
 }                                                                                                                                                              
                                                                                                                                                                
 File: agent_go/pkg/orchestrator/types/workflow_orchestrator.go                                                                                                 
                                                                                                                                                                
 Modify runPlanning() (line 823):                                                                                                                               
 func (wo *WorkflowOrchestrator) runPlanning(ctx, objective, selectedOptions) (string, error) {                                                                 
     if wo.virtualPlan != nil {                                                                                                                                 
         return wo.runVirtualPlanExecution(ctx, objective)                                                                                                      
     }                                                                                                                                                          
     return wo.runHumanControlledPlanning(ctx, objective)                                                                                                       
 }                                                                                                                                                              
                                                                                                                                                                
 Add runVirtualPlanExecution():                                                                                                                                 
 - Same StepBasedWorkflowOrchestrator creation as runHumanControlledPlanning (lines 843-870)                                                                    
 - todoPlannerAgent.SetApprovedPlan(wo.virtualPlan) — inject plan                                                                                               
 - todoPlannerAgent.SetMCPSessionID(wo.getSessionID())                                                                                                          
 - Pass execution options                                                                                                                                       
 - Call todoPlannerAgent.Execute(ctx, objective, wo.GetWorkspacePath(), nil)                                                                                    
 - Skip emitBlockingHumanFeedback — no human approval needed                                                                                                    
 - Emit completion events                                                                                                                                       
                                                                                                                                                                
 Phase 5: Frontend Changes                                                                                                                                      
                                                                                                                                                                
 Type definitions:                                                                                                                                              
 - frontend/src/stores/types.ts — add 'task_agent' to AgentMode union                                                                                           
 - frontend/src/services/api-types.ts — add 'task_agent' to agent_mode union                                                                                    
                                                                                                                                                                
 Mode store:                                                                                                                                                    
 - frontend/src/stores/useModeStore.ts — add 'task_agent' to ModeCategory, add mappings in both switch statements, add 'task_agent': null to lastSelectedPreset 
                                                                                                                                                                
 Mode info:                                                                                                                                                     
 - frontend/src/constants/modeInfo.tsx — add task_agent entry (icon, title, description, features)                                                              
                                                                                                                                                                
 UI components:                                                                                                                                                 
 - frontend/src/components/ModeSelectionModal.tsx — add ModeCard; in handleModeSelect treat like chat (no preset)                                               
 - frontend/src/components/ModeSwitchSection.tsx — add to modes array                                                                                           
 - frontend/src/components/ModePresetBar.tsx — add button + switch cases in local helpers                                                                       
                                                                                                                                                                
 Event display:                                                                                                                                                 
 - frontend/src/components/ChatArea.tsx — at workflow event display checks, add || selectedModeCategory === 'task_agent'. Only for event rendering — NOT for    
 folder validation, preset servers, or WorkflowModeHandler                                                                                                      
                                                                                                                                                                
 ---                                                                                                                                                            
 Files Summary                                                                                                                                                  
 #: 1                                                                                                                                                           
 File: agent_go/pkg/todotask/types.go                                                                                                                           
 Action: NEW — Config, Result, RouteConfig                                                                                                                      
 ────────────────────────────────────────                                                                                                                       
 #: 2                                                                                                                                                           
 File: agent_go/pkg/todotask/interfaces.go                                                                                                                      
 Action: NEW — SubAgentExecutor, AgentFactory, EventEmitter, PreValidator                                                                                       
 ────────────────────────────────────────                                                                                                                       
 #: 3                                                                                                                                                           
 File: agent_go/pkg/todotask/run_loop.go                                                                                                                        
 Action: NEW — RunLoop() core engine                                                                                                                            
 ────────────────────────────────────────                                                                                                                       
 #: 4                                                                                                                                                           
 File: agent_go/pkg/todotask/templates.go                                                                                                                       
 Action: NEW — System + User prompt templates (moved)                                                                                                           
 ────────────────────────────────────────                                                                                                                       
 #: 5                                                                                                                                                           
 File: agent_go/pkg/todotask/template_vars.go                                                                                                                   
 Action: NEW — BuildTemplateVars()                                                                                                                              
 ────────────────────────────────────────                                                                                                                       
 #: 6                                                                                                                                                           
 File: agent_go/pkg/todotask/events.go                                                                                                                          
 Action: NEW — Event data structs                                                                                                                               
 ────────────────────────────────────────                                                                                                                       
 #: 7                                                                                                                                                           
 File: agent_go/pkg/todotask/logging.go                                                                                                                         
 Action: NEW — LogRoutingDecision(), SaveExecutionLog()                                                                                                         
 ────────────────────────────────────────                                                                                                                       
 #: 8                                                                                                                                                           
 File: agent_go/pkg/orchestrator/types/task_agent_orchestrator.go                                                                                               
 Action: NEW — TaskAgentOrchestrator                                                                                                                            
 ────────────────────────────────────────                                                                                                                       
 #: 9                                                                                                                                                           
 File: agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go                                                                    
 Action: REFACTOR — thin adapter calling todotask.RunLoop()                                                                                                     
 ────────────────────────────────────────                                                                                                                       
 #: 10                                                                                                                                                          
 File: agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/todo_task_orchestrator_agent.go                                                            
 Action: MODIFY — import templates from todotask pkg                                                                                                            
 ────────────────────────────────────────                                                                                                                       
 #: 11                                                                                                                                                          
 File: agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go                                                                              
 Action: MODIFY — CreateTodoList() accepts pre-set plan                                                                                                         
 ────────────────────────────────────────                                                                                                                       
 #: 12                                                                                                                                                          
 File: agent_go/pkg/orchestrator/types/workflow_orchestrator.go                                                                                                 
 Action: MODIFY — runVirtualPlanExecution(), modify runPlanning()                                                                                               
 ────────────────────────────────────────                                                                                                                       
 #: 13                                                                                                                                                          
 File: agent_go/pkg/database/models.go                                                                                                                          
 Action: MODIFY — add AgentModeTaskAgent                                                                                                                        
 ────────────────────────────────────────                                                                                                                       
 #: 14                                                                                                                                                          
 File: agent_go/cmd/server/server.go                                                                                                                            
 Action: MODIFY — route task_agent                                                                                                                              
 ────────────────────────────────────────                                                                                                                       
 #: 15                                                                                                                                                          
 File: agent_go/pkg/orchestrator/task_agent_factory.go                                                                                                          
 Action: EXISTS — already creates synthetic plan                                                                                                                
 ────────────────────────────────────────                                                                                                                       
 #: 16                                                                                                                                                          
 File: frontend/src/stores/types.ts                                                                                                                             
 Action: MODIFY — add task_agent to AgentMode                                                                                                                   
 ────────────────────────────────────────                                                                                                                       
 #: 17                                                                                                                                                          
 File: frontend/src/services/api-types.ts                                                                                                                       
 Action: MODIFY — add task_agent to agent_mode                                                                                                                  
 ────────────────────────────────────────                                                                                                                       
 #: 18                                                                                                                                                          
 File: frontend/src/stores/useModeStore.ts                                                                                                                      
 Action: MODIFY — add ModeCategory + mappings                                                                                                                   
 ────────────────────────────────────────                                                                                                                       
 #: 19                                                                                                                                                          
 File: frontend/src/constants/modeInfo.tsx                                                                                                                      
 Action: MODIFY — add mode info                                                                                                                                 
 ────────────────────────────────────────                                                                                                                       
 #: 20                                                                                                                                                          
 File: frontend/src/components/ModeSelectionModal.tsx                                                                                                           
 Action: MODIFY — add ModeCard                                                                                                                                  
 ────────────────────────────────────────                                                                                                                       
 #: 21                                                                                                                                                          
 File: frontend/src/components/ModeSwitchSection.tsx                                                                                                            
 Action: MODIFY — add to dropdown                                                                                                                               
 ────────────────────────────────────────                                                                                                                       
 #: 22                                                                                                                                                          
 File: frontend/src/components/ModePresetBar.tsx                                                                                                                
 Action: MODIFY — add button                                                                                                                                    
 ────────────────────────────────────────                                                                                                                       
 #: 23                                                                                                                                                          
 File: frontend/src/components/ChatArea.tsx                                                                                                                     
 Action: MODIFY — event display routing                                                                                                                         
 ---                                                                                                                                                            
 Key Design Decisions                                                                                                                                           
                                                                                                                                                                
 1. RunLoop is a function, not a struct — trivially testable with mock interfaces, no hidden state                                                              
 2. SubAgentExecutor is wired via AgentFactory, not called by RunLoop — sub-agents execute as tool calls during agent's multi-turn execution                    
 3. ValidationSchema is opaque — passed to PreValidator as interface{}, keeps todotask free of step_based_workflow types                                        
 4. Event data structs are plain Go — no baseevents dependency; EventEmitter adapter wraps them                                                                 
 5. Template variables remain map[string]string — same convention as today, minimizes migration risk                                                            
 6. Phase 3 (TaskAgentOrchestrator) reuses StepBasedWorkflowOrchestrator internally — avoids duplicating agent creation, tool registration, and sub-agent       
 execution infrastructure                                                                                                                                       
                                                                                                                                                                
 ---                                                                                                                                                            
 Verification                                                                                                                                                   
                                                                                                                                                                
 1. Backend build: cd agent_go && go build ./...                                                                                                                
 2. Frontend build: cd frontend && npm run build                                                                                                                
 3. Workflow regression: Start server → run existing Workflow preset → verify identical behavior                                                                
 4. Task Agent E2E:                                                                                                                                             
   - Select "Task Agent" mode in UI                                                                                                                             
   - Submit: "Analyze this codebase and create a summary"                                                                                                       
   - Verify: request has agent_mode: "task_agent"                                                                                                               
   - Verify: backend creates synthetic plan, skips planning phase                                                                                               
   - Verify: TodoTask events (route selected, step completed) render in chat                                                                                    
   - Verify: tasks.md is created and managed by orchestrator                                                                                                    
 5. Library import test: Create a simple _test.go that calls todotask.RunLoop() with mock implementations of all interfaces

---
Delegation Mode Task Tracking (Future Enhancement)

This section describes how to add task tracking to delegation mode using shell-based task file management.

Concept

The delegation mode agent should create and manage a `tasks.md` file using shell commands to track progress as it delegates and completes work. This parallels the task agent mode's todo system but uses simpler shell-based management.

Task File Format

```markdown
# Tasks

## Plan
Brief description of what we're building

## Tasks
- [ ] Task 1: Description (assigned: sub-agent-1)
- [ ] Task 2: Description (assigned: sub-agent-2)
- [x] Task 3: Description (completed)
- [~] Task 4: Description (in progress)

## Status
- Total: 4
- Completed: 1
- In Progress: 1
- Pending: 2
```

Shell Commands for Task Management

```bash
# Create initial task file
cat > tasks.md << 'EOF'
# Tasks
## Plan
...
## Tasks
- [ ] Task 1...
EOF

# Mark task as in-progress (replace [ ] with [~])
sed -i '' 's/- \[ \] Task 1/- [~] Task 1/' tasks.md

# Mark task as complete (replace [ ] or [~] with [x])
sed -i '' 's/- \[.\] Task 1/- [x] Task 1/' tasks.md

# View current status
cat tasks.md
```

Workflow Integration

Update `GetDelegationInstructions()` in `delegation_tools.go` to include:

Phase 1: PLAN
- Analyze the request and understand the goal
- Create `tasks.md` with plan and task breakdown
- List all tasks with [ ] status

Phase 2: TASK BREAKDOWN
- Each task should be concrete and independent where possible
- Mark dependencies in task descriptions
- Identify parallel vs sequential tasks

Phase 3: EXECUTE VIA DELEGATION
- Mark task as [~] (in progress) before delegating
- Delegate with instruction to mark [x] when complete
- Delegate ALL independent tasks simultaneously
- Check tasks.md status between batches

Phase 4: VERIFY
- Run `cat tasks.md` to check all tasks completed
- Verify outputs work together
- Report final status

Sub-Agent Task Updates

When delegating, include the task ID and instruct the sub-agent:
"When complete, run: `sed -i '' 's/- \\[.\\] Task 1/- [x] Task 1/' tasks.md`"

Sub-agents already have shell access, so they can update the shared task file.

Benefits

| Aspect | Benefit |
|--------|---------|
| Simplicity | No new tools, uses existing shell |
| Visibility | tasks.md is human-readable |
| Persistence | File persists across conversation |
| Sub-agent updates | Shell commands work in sub-agents |

File to Update

**`/docs/refactor/task_agent_mode.md`**

Add this section at the end of the file (after the Verification section).                                     