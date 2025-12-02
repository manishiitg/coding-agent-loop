package todo_creation_human

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/shared"

	"mcpagent/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeConditionalStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	depth int,
	progress *StepProgress,
	previousContextFiles []string, // Context files from previous steps
	iteration int, // Current iteration number
) error {
	const maxDepth = 2
	if depth > maxDepth {
		return fmt.Errorf("nesting depth %d exceeds maximum allowed depth of %d", depth, maxDepth)
	}

	hcpo.GetLogger().Infof("🔀 Executing conditional step %d (depth %d): %s", stepIndex+1, depth, step.Title)

	// Check for existing branch progress
	var existingBranchProgress *BranchStepProgress
	var conditionResult bool
	var conditionReason string
	var resumeFromBranchStep int = 0 // 0 means start from beginning
	var updatedContextFiles []string // Context files from conditional step execution (if executed)

	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
	}

	if branchProgress, exists := progress.BranchSteps[stepIndex]; exists {
		existingBranchProgress = &branchProgress
		hcpo.GetLogger().Infof("📋 Found existing branch progress for step %d: branch=%s, completed_steps=%d", stepIndex+1, branchProgress.BranchExecuted, len(branchProgress.CompletedSteps))
		// Use stored branch execution result
		conditionResult = (branchProgress.BranchExecuted == "if_true")
		conditionReason = fmt.Sprintf("Resuming from saved branch progress: %s", branchProgress.BranchExecuted)
		hcpo.GetLogger().Infof("✅ Using stored branch execution: %s (result=%t, reason: %s)", branchProgress.BranchExecuted, conditionResult, conditionReason)

		// Determine which branch steps to execute based on stored branch
		var branchStepsToCheck []TodoStep
		if conditionResult {
			branchStepsToCheck = step.IfTrueSteps
		} else {
			branchStepsToCheck = step.IfFalseSteps
		}

		// Find first incomplete branch step
		for branchIdx := range branchStepsToCheck {
			branchStepPath := fmt.Sprintf("step-%d-%s-%d", stepIndex+1, branchProgress.BranchExecuted, branchIdx)
			completed := false
			for _, completedPath := range branchProgress.CompletedSteps {
				if completedPath == branchStepPath {
					completed = true
					break
				}
			}
			if !completed {
				resumeFromBranchStep = branchIdx
				hcpo.GetLogger().Infof("🔍 Resuming from branch step %d (path: %s)", branchIdx, branchStepPath)
				break
			}
		}
	} else {
		// No existing branch progress - execute conditional step and evaluate condition
		// First, execute the conditional step itself to get execution result
		stepPath := fmt.Sprintf("step-%d-conditional", stepIndex+1)
		conditionalExecutionResult, updatedContextFiles, err := hcpo.executeSingleStep(
			ctx,
			step,
			stepIndex,
			stepPath,
			1, // totalSteps = 1 for conditional step itself
			iteration,
			previousContextFiles,
			progress,
			false, // isBranchStep = false (conditional step is a main step)
		)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to execute conditional step %d: %v", stepIndex+1, err)
			return fmt.Errorf("failed to execute conditional step: %w", err)
		}

		hcpo.GetLogger().Infof("✅ Conditional step execution completed, evaluating condition based on execution result")

		// Build context for ConditionalLLM
		contextBuilder := strings.Builder{}

		// Add execution result from the conditional step
		contextBuilder.WriteString("Current Step Execution Result:\n")
		contextBuilder.WriteString(conditionalExecutionResult)
		contextBuilder.WriteString("\n\n")

		// Add condition context if provided
		if step.ConditionContext != "" {
			contextBuilder.WriteString("Condition Context:\n")
			contextBuilder.WriteString(step.ConditionContext)
			contextBuilder.WriteString("\n\n")
		}

		// Add context from previous step outputs (using updated context files from step execution)
		if len(updatedContextFiles) > 0 {
			contextBuilder.WriteString("Previous Step Context Files:\n")
			for _, contextFile := range updatedContextFiles {
				// Try to read the context file
				runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
				executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
				contextFilePath := filepath.Join(executionWorkspacePath, contextFile)

				content, err := hcpo.ReadWorkspaceFile(ctx, contextFilePath)
				if err == nil {
					contextBuilder.WriteString(fmt.Sprintf("- %s:\n%s\n\n", contextFile, content))
				} else {
					contextBuilder.WriteString(fmt.Sprintf("- %s: (file not found or error reading)\n", contextFile))
				}
			}
		}

		conditionContext := contextBuilder.String()

		// Evaluate condition using ConditionalLLM
		hcpo.GetLogger().Infof("🤔 Evaluating condition for step %d (depth %d): %s", stepIndex+1, depth, step.ConditionQuestion)
		hcpo.GetLogger().Infof("📋 Condition context length: %d characters", len(conditionContext))

		conditionalResponse, err := hcpo.conditionalLLM.Decide(ctx, conditionContext, step.ConditionQuestion, stepIndex, 0)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to evaluate condition for step %d: %v", stepIndex+1, err)
			// Emit error event if event bridge is available
			eventBridge := hcpo.GetContextAwareBridge()
			if eventBridge != nil {
				errorEvent := &events.OrchestratorAgentErrorEvent{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					AgentType: "conditional",
					AgentName: "conditional-step-evaluation",
					Objective: fmt.Sprintf("Evaluate condition: %s", step.ConditionQuestion),
					Error:     err.Error(),
					StepIndex: stepIndex,
					Iteration: 0,
				}
				eventBridge.HandleEvent(ctx, &events.AgentEvent{
					Type:      events.OrchestratorAgentError,
					Timestamp: time.Now(),
					Data:      errorEvent,
				})
			}
			return fmt.Errorf("failed to evaluate condition: %w", err)
		}

		// Store result
		conditionResult = conditionalResponse.Result
		conditionReason = conditionalResponse.Reason

		hcpo.GetLogger().Infof("✅ Condition evaluated for step %d: result=%t, reason=%s", stepIndex+1, conditionResult, conditionReason)

		// Initialize branch progress
		branchExecuted := "if_false"
		if conditionResult {
			branchExecuted = "if_true"
		}
		progress.BranchSteps[stepIndex] = BranchStepProgress{
			BranchExecuted: branchExecuted,
			CompletedSteps: []string{},
		}
		hcpo.GetLogger().Infof("📝 Initialized branch progress for step %d: branch=%s", stepIndex+1, branchExecuted)
	}

	// Log decision details
	hcpo.GetLogger().Infof("📊 Conditional decision details - Step: %s, Question: %s, Result: %t, Depth: %d",
		step.Title, step.ConditionQuestion, conditionResult, depth)

	// Determine which branch to execute
	var branchSteps []TodoStep
	if conditionResult {
		branchSteps = step.IfTrueSteps
		hcpo.GetLogger().Infof("📋 Executing TRUE branch with %d steps", len(branchSteps))
	} else {
		branchSteps = step.IfFalseSteps
		hcpo.GetLogger().Infof("📋 Executing FALSE branch with %d steps", len(branchSteps))
	}

	// Track context files for branch steps
	branchContextFiles := make([]string, 0)
	if existingBranchProgress == nil {
		// New execution - use updated context files from conditional step execution
		branchContextFiles = append(branchContextFiles, updatedContextFiles...)
	} else {
		// Resuming - use previous context files (from previousContextFiles parameter)
		branchContextFiles = append(branchContextFiles, previousContextFiles...)
	}

	// Add conditional step's context output to branch context files if it exists
	if step.ContextOutput != "" {
		branchContextFiles = append(branchContextFiles, step.ContextOutput)
	}

	// Get branch executed string for path generation
	branchExecutedStr := map[bool]string{true: "if-true", false: "if-false"}[conditionResult]

	// Execute each step in the chosen branch
	for branchIdx, branchStep := range branchSteps {
		// Skip if resuming and this branch step is already completed
		if branchIdx < resumeFromBranchStep {
			hcpo.GetLogger().Infof("⏭️ Skipping branch step %d/%d (already completed): %s", branchIdx+1, len(branchSteps), branchStep.Title)
			continue
		}

		// Check if branch step is already completed (for resume case)
		branchStepPath := fmt.Sprintf("step-%d-%s-%d", stepIndex+1, branchExecutedStr, branchIdx)
		if existingBranchProgress != nil {
			completed := false
			for _, completedPath := range existingBranchProgress.CompletedSteps {
				if completedPath == branchStepPath {
					completed = true
					break
				}
			}
			if completed {
				hcpo.GetLogger().Infof("⏭️ Skipping branch step %d/%d (marked as completed): %s", branchIdx+1, len(branchSteps), branchStep.Title)
				continue
			}
		}

		hcpo.GetLogger().Infof("📋 Executing branch step %d/%d (depth %d): %s", branchIdx+1, len(branchSteps), depth+1, branchStep.Title)

		// Check if branch step is conditional (nested conditional)
		if branchStep.HasCondition {
			// Recursively execute nested conditional step
			hcpo.GetLogger().Infof("🔀 Executing nested conditional step in branch: %s (depth %d)", branchStep.Title, depth+1)
			if err := hcpo.executeConditionalStep(ctx, branchStep, stepIndex, depth+1, progress, branchContextFiles, iteration); err != nil {
				hcpo.GetLogger().Errorf("❌ Failed to execute nested conditional step '%s' at depth %d: %v", branchStep.Title, depth+1, err)
				return fmt.Errorf("failed to execute nested conditional step '%s': %w", branchStep.Title, err)
			}
			hcpo.GetLogger().Infof("✅ Completed nested conditional step: %s", branchStep.Title)
		} else {
			// Execute regular branch step using extracted execution logic
			branchExecutionResult, updatedBranchContextFiles, err := hcpo.executeSingleStep(
				ctx,
				branchStep,
				stepIndex, // Use parent step index for now
				branchStepPath,
				len(branchSteps), // Total steps in branch
				iteration,
				branchContextFiles,
				progress,
				true, // isBranchStep = true
			)
			if err != nil {
				hcpo.GetLogger().Errorf("❌ Failed to execute branch step '%s': %v", branchStep.Title, err)
				return fmt.Errorf("failed to execute branch step '%s': %w", branchStep.Title, err)
			}

			// Track branch step completion
			branchProgress := progress.BranchSteps[stepIndex]
			branchProgress.CompletedSteps = append(branchProgress.CompletedSteps, branchStepPath)
			progress.BranchSteps[stepIndex] = branchProgress
			// Save progress after each branch step completion
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save branch step progress: %w", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved branch step progress: %s completed", branchStepPath)
			}

			// Update context files with branch step's output
			branchContextFiles = updatedBranchContextFiles

			hcpo.GetLogger().Infof("✅ Completed branch step: %s (execution result length: %d chars)", branchStep.Title, len(branchExecutionResult))
		}
	}

	// Verify all branch steps are completed
	branchProgress := progress.BranchSteps[stepIndex]
	expectedBranchSteps := len(branchSteps)
	completedBranchSteps := len(branchProgress.CompletedSteps)
	if completedBranchSteps < expectedBranchSteps {
		hcpo.GetLogger().Warnf("⚠️ Conditional step %d: only %d/%d branch steps completed", stepIndex+1, completedBranchSteps, expectedBranchSteps)
		// Don't mark as completed - will resume from incomplete branch steps
	} else {
		hcpo.GetLogger().Infof("✅ All %d branch steps completed for conditional step %d", expectedBranchSteps, stepIndex+1)
	}

	hcpo.GetLogger().Infof("✅ Completed conditional step %d: executed %s branch", stepIndex+1, map[bool]string{true: "TRUE", false: "FALSE"}[conditionResult])
	return nil
}

// executeSingleStep executes a single step with full functionality (execution, validation, learning, human feedback)
// This is a reusable function extracted from runExecutionPhase to support both regular steps and branch steps
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeSingleStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	stepPath string, // e.g., "step-1" or "step-1-if-true-0" for branch steps
	totalSteps int,
	iteration int,
	previousContextFiles []string,
	progress *StepProgress,
	isBranchStep bool, // true if this is a branch step (affects progress tracking)
) (executionResult string, updatedContextFiles []string, err error) {
	// Initialize updated context files as copy of previous context files
	updatedContextFiles = make([]string, len(previousContextFiles))
	copy(updatedContextFiles, previousContextFiles)

	// Initialize variables for step execution
	maxRetryAttempts := 5
	var executionConversationHistory []llmtypes.MessageContent // Only used for learning agents after execution
	stepCompleted := false

	// Outer loop: Handle re-execution with human feedback
	for !stepCompleted {

		// Prepare template variables for this specific step with individual fields
		// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
		// Execution agent workspace path includes run folder: workspacePath/runs/{selectedRunFolder}/execution
		runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
		// Determine code execution mode: Priority: step config > preset default
		var isCodeExecutionMode bool
		if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
			isCodeExecutionMode = *step.AgentConfigs.UseCodeExecutionMode
			hcpo.GetLogger().Infof("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode)
		} else {
			isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
			hcpo.GetLogger().Infof("🔧 Using preset code execution mode: %v", isCodeExecutionMode)
		}
		// Use learning_code_exec folder if code execution mode is enabled, otherwise use learnings folder
		var learningsPath string
		if isCodeExecutionMode {
			learningsPath = fmt.Sprintf("%s/learning_code_exec", hcpo.GetWorkspacePath())
		} else {
			learningsPath = fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())
		}
		templateVars := map[string]string{
			"StepTitle":           ResolveVariables(step.Title, hcpo.variableValues),
			"StepDescription":     ResolveVariables(step.Description, hcpo.variableValues),
			"StepSuccessCriteria": ResolveVariables(step.SuccessCriteria, hcpo.variableValues),
			"StepContextOutput":   ResolveVariables(step.ContextOutput, hcpo.variableValues),
			"WorkspacePath":       executionWorkspacePath,                 // Execution subdirectory (folder guard validates against this)
			"LearningsPath":       learningsPath,                          // Learnings folder path for reading learning files and scripts/code
			"IsCodeExecutionMode": fmt.Sprintf("%v", isCodeExecutionMode), // Code execution mode flag (step-specific or preset)
		}

		// Add context dependencies as a comma-separated string (also resolve variables)
		if len(step.ContextDependencies) > 0 {
			resolvedDeps := ResolveVariablesArray(step.ContextDependencies, hcpo.variableValues)
			templateVars["StepContextDependencies"] = strings.Join(resolvedDeps, ", ")
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Add variable names if available (same format as other agents)
		if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
			templateVars["VariableNames"] = variableNames
		}

		// Add variable values if available (name = value - description format)
		if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
			templateVars["VariableValues"] = variableValues
		}

		// Validate loop condition is provided when has_loop is true
		if step.HasLoop {
			if step.LoopCondition == "" {
				return "", updatedContextFiles, fmt.Errorf("step %d has has_loop=true but loop_condition is empty (required)", stepIndex+1)
			}
			// Set default max_iterations if not provided
			if step.MaxIterations == 0 {
				step.MaxIterations = 10
				hcpo.GetLogger().Infof("⚠️ Step %d has loop but no max_iterations specified, using default: 10", stepIndex+1)
			}
		}

		// Inner loop: Automatic retry logic
		var validationResponse *ValidationResponse

		// Loop handling: if step has loop, wrap execution in loop that checks loop condition
		var loopConditionMet bool
		var loopIterationCount int
		// Store previous iteration's execution and validation outputs for loop feedback
		var previousIterationExecutionOutput string
		var previousIterationValidationOutput string
		// Cache learning history for loop steps (reuse across iterations when LearningAfterLoopIteration is false)
		var cachedLearningHistory string
		var learningHistoryInitialized bool

		// Main execution loop (either single execution or loop iterations)
		// For non-loop steps, this executes once. For loop steps, it iterates until condition is met.
		// NOTE: No conversation history is passed to execution agent - all context via template variables
		for loopIteration := 0; ; loopIteration++ {
			// Initialize loop state on first iteration
			if loopIteration == 0 && step.HasLoop {
				loopConditionMet = false
				loopIterationCount = 0
				previousIterationExecutionOutput = ""
				previousIterationValidationOutput = ""
				hcpo.GetLogger().Infof("🔄 Step %d loop starting (max iterations: %d, condition: %s)", stepIndex+1, step.MaxIterations, step.LoopCondition)
			} else if loopIteration > 0 && step.HasLoop {
				// Previous iteration outputs are passed via template variables (PreviousIterationOutput)
				// Execution conversation history will be captured fresh from this iteration for learning agents
				hcpo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d starting", stepIndex+1, loopIterationCount, step.MaxIterations)
			}

			// Check loop exit conditions (only for loop steps)
			if step.HasLoop {
				if loopConditionMet {
					hcpo.GetLogger().Infof("✅ Step %d loop condition met after %d iterations, exiting loop", stepIndex+1, loopIterationCount)
					// Skip validation, mark as completed
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            fmt.Sprintf("Loop condition met after %d iterations. Validation skipped per loop exit.", loopIterationCount),
					}
					break // Exit main loop - proceed to mark as completed
				}
				if loopIterationCount >= step.MaxIterations {
					hcpo.GetLogger().Errorf("❌ Step %d reached max iterations (%d) without meeting loop condition, requesting human intervention", stepIndex+1, step.MaxIterations)
					// Request human intervention immediately, skip validation
					var err error
					var approved bool
					approved, _, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps,
						fmt.Sprintf("Loop reached max iterations (%d) without meeting condition: %s", step.MaxIterations, step.LoopCondition))
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %w", err)
						// Default to not approved so step doesn't complete
						approved = false
					}
					if approved {
						// User approved - treat as completed despite max iterations
						hcpo.GetLogger().Infof("✅ User approved step %d despite max iterations, marking as completed", stepIndex+1)
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "User approved completion despite max iterations reached",
						}
						loopConditionMet = true // Mark condition as met so loop exits
						break                   // Exit main loop
					} else {
						// User rejected - will re-execute step
						hcpo.GetLogger().Infof("🔄 User rejected approval, will re-execute step %d", stepIndex+1)
						break // Exit main loop; outer loop will re-execute since stepCompleted is still false
					}
				}
				loopIterationCount++
				hcpo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d", stepIndex+1, loopIterationCount, step.MaxIterations)
			}

			// Add loop context to template variables if in loop mode
			if step.HasLoop {
				templateVars["HasLoop"] = "true"
				templateVars["LoopCondition"] = step.LoopCondition
				templateVars["LoopDescription"] = step.LoopDescription
				templateVars["CurrentIteration"] = fmt.Sprintf("%d", loopIterationCount)
				templateVars["MaxIterations"] = fmt.Sprintf("%d", step.MaxIterations)
				// Add previous iteration execution and validation outputs for loop steps (after iteration 1)
				if loopIterationCount > 1 && (previousIterationExecutionOutput != "" || previousIterationValidationOutput != "") {
					var combinedOutput strings.Builder
					if previousIterationExecutionOutput != "" {
						combinedOutput.WriteString("## Previous Loop Iteration Execution Output:\n")
						combinedOutput.WriteString(previousIterationExecutionOutput)
						combinedOutput.WriteString("\n\n")
					}
					if previousIterationValidationOutput != "" {
						combinedOutput.WriteString("## Previous Loop Iteration Validation Output:\n")
						combinedOutput.WriteString(previousIterationValidationOutput)
					}
					templateVars["PreviousIterationOutput"] = combinedOutput.String()
					hcpo.GetLogger().Infof("📝 Added previous iteration outputs to template variables for step %d (loop iteration %d)", stepIndex+1, loopIterationCount)
				} else {
					templateVars["PreviousIterationOutput"] = ""
				}
			} else {
				templateVars["HasLoop"] = "false"
				templateVars["LoopCondition"] = ""
				templateVars["LoopDescription"] = ""
				templateVars["CurrentIteration"] = ""
				templateVars["MaxIterations"] = ""
				templateVars["PreviousIterationOutput"] = ""
			}

			// Check if SPLIT_EXECUTION_LEARNING feature flag is enabled
			splitExecutionLearning := os.Getenv("SPLIT_EXECUTION_LEARNING") == "true"

			// Resolve variables in step title before using in agent name (needed for both paths)
			resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
			sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)

			// Run learning reading agent ONCE per main loop iteration (before retry loop)
			// This ensures learning is only discovered once, even if validation fails and we retry
			// For loop steps: cache learning from first iteration and reuse when LearningAfterLoopIteration is false
			var formattedLearningHistory string
			if splitExecutionLearning {
				// Determine if we should read learning or reuse cached version
				shouldReadLearning := true
				if step.HasLoop {
					learningAfterLoopIteration := step.AgentConfigs != nil && step.AgentConfigs.LearningAfterLoopIteration
					if learningHistoryInitialized && !learningAfterLoopIteration {
						// Reuse cached learning from first iteration
						shouldReadLearning = false
						formattedLearningHistory = cachedLearningHistory
						hcpo.GetLogger().Infof("🔄 Reusing cached learning history from first iteration (loop iteration %d, LearningAfterLoopIteration=false)", loopIterationCount)
					} else if learningAfterLoopIteration {
						hcpo.GetLogger().Infof("🔀 SPLIT_EXECUTION_LEARNING enabled - running learning reading agent for loop iteration %d (LearningAfterLoopIteration=true, will get fresh learnings)", loopIterationCount)
					} else {
						hcpo.GetLogger().Infof("🔀 SPLIT_EXECUTION_LEARNING enabled - running learning reading agent for first loop iteration (will cache for reuse)")
					}
				} else {
					hcpo.GetLogger().Infof("🔀 SPLIT_EXECUTION_LEARNING enabled - running learning reading agent once (before retry loop)")
				}

				if shouldReadLearning {
					// Step 1: Create and execute Learning Reading Agent (ONCE per main loop iteration)
					// Include mode in agent name: "code-exec" or "simple"
					modeLabel := "simple"
					if isCodeExecutionMode {
						modeLabel = "code-exec"
					}
					learningAgentName := fmt.Sprintf("%s-learning-%s-%s", stepPath, modeLabel, sanitizedTitle)
					// Add loop iteration to agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						learningAgentName = fmt.Sprintf("%s-loop-%d", learningAgentName, loopIterationCount)
					}

					// Pass the already-computed isCodeExecutionMode to ensure consistency with execution agent
					// Also pass executionWorkspacePath for reading context dependencies in code execution mode
					var learningAgent agents.OrchestratorAgent
					learningAgent, err = hcpo.createLearningReadingAgent(ctx, "learning_reading", stepIndex+1, iteration, learningAgentName, step.AgentConfigs, isCodeExecutionMode, executionWorkspacePath)
					if err != nil {
						return "", updatedContextFiles, fmt.Errorf("failed to create learning reading agent for step %d: %w", stepIndex+1, err)
					}

					// Prepare template vars for learning reading agent
					// Use the same isCodeExecutionMode value that was computed for execution agent (already in templateVars)
					// Include context dependencies and workspace path for code execution mode
					learningTemplateVars := map[string]string{
						"StepTitle":               templateVars["StepTitle"],
						"StepDescription":         templateVars["StepDescription"],
						"LearningsPath":           templateVars["LearningsPath"],
						"IsCodeExecutionMode":     templateVars["IsCodeExecutionMode"],     // Use same value as execution agent
						"StepContextDependencies": templateVars["StepContextDependencies"], // Context files from previous steps
						"WorkspacePath":           templateVars["WorkspacePath"],           // Workspace path for reading context files
					}

					// Execute learning reading agent ONCE (not in retry loop)
					var learningConversationHistory []llmtypes.MessageContent
					_, learningConversationHistory, err = learningAgent.Execute(ctx, learningTemplateVars, []llmtypes.MessageContent{})
					if err != nil {
						hcpo.GetLogger().Errorf("❌ Step %d learning reading failed: %v - will proceed without learning history", stepIndex+1, err)
						// Continue without learning history - execution agent can still work without it
						formattedLearningHistory = ""
					} else {
						hcpo.GetLogger().Infof("✅ Step %d learning reading completed successfully (will reuse for all retry attempts)", stepIndex+1)

						// Format learning conversation history for execution-only agent
						formattedLearningHistory = hcpo.formatLearningHistoryForExecution(learningConversationHistory)

						// Cache the result for loop steps
						if step.HasLoop {
							learningAfterLoopIteration := step.AgentConfigs != nil && step.AgentConfigs.LearningAfterLoopIteration
							if !learningHistoryInitialized {
								// First iteration: always cache
								cachedLearningHistory = formattedLearningHistory
								learningHistoryInitialized = true
								if learningAfterLoopIteration {
									hcpo.GetLogger().Infof("💾 Cached learning history from first iteration (will refresh each iteration)")
								} else {
									hcpo.GetLogger().Infof("💾 Cached learning history for reuse in subsequent iterations (LearningAfterLoopIteration=false)")
								}
							} else if learningAfterLoopIteration {
								// LearningAfterLoopIteration=true: update cache with fresh learnings each iteration
								cachedLearningHistory = formattedLearningHistory
								hcpo.GetLogger().Infof("💾 Updated cached learning history with fresh learnings from iteration %d", loopIterationCount)
							}
						}
					}
				}
			}

			// Track if validation failed after exhausting all retry attempts
			validationFailedAfterMaxRetries := false

			// Retry loop: Execute with validation feedback, reusing the same learning history
			for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
				hcpo.GetLogger().Infof("🔄 Executing step %d/%d (attempt %d/%d): %s", stepIndex+1, totalSteps, retryAttempt, maxRetryAttempts, step.Title)

				// Add validation feedback to template variables if this is a retry or loop iteration
				if (retryAttempt > 1 || (step.HasLoop && loopIterationCount > 1)) && validationResponse != nil {
					var contextStr string
					if retryAttempt > 1 {
						contextStr = fmt.Sprintf("Validation Feedback (Retry Attempt %d)", retryAttempt)
					} else if step.HasLoop && loopIterationCount > 1 {
						contextStr = fmt.Sprintf("Validation Feedback (Loop Iteration %d)", loopIterationCount-1)
					} else {
						contextStr = "Validation Feedback"
					}
					templateVars["ValidationFeedback"] = hcpo.formatValidationResponseForTemplate(validationResponse, contextStr)
					hcpo.GetLogger().Infof("📝 Added validation feedback to template variables for step %d (retry: %d, loop iteration: %d)", stepIndex+1, retryAttempt, loopIterationCount)
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt/first iteration
				}

				if splitExecutionLearning {
					// Step 2: Create and execute Execution-Only Agent with learning history (reused from above)
					executionAgentName := fmt.Sprintf("%s-execution-%s", stepPath, sanitizedTitle)
					// Add loop iteration to agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						executionAgentName = fmt.Sprintf("%s-loop-%d", executionAgentName, loopIterationCount)
					}

					// Add learning history to template vars for execution-only agent (reused for all retry attempts)
					templateVars["LearningHistory"] = formattedLearningHistory

					var executionAgent agents.OrchestratorAgent
					executionAgent, err = hcpo.createExecutionOnlyAgent(ctx, "execution_only", stepIndex+1, iteration, executionAgentName, step.AgentConfigs)
					if err != nil {
						return "", updatedContextFiles, fmt.Errorf("failed to create execution-only agent for step %d: %w", stepIndex+1, err)
					}

					// Execute execution-only agent with learning history (reused from learning reading above)
					executionResult, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
				} else {
					// Normal flow: Single execution agent (discovers learnings + executes)
					agentName := fmt.Sprintf("%s-%s", stepPath, sanitizedTitle)
					// Add loop iteration to agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						agentName = fmt.Sprintf("%s-loop-%d", agentName, loopIterationCount)
					}
					var executionAgent agents.OrchestratorAgent
					executionAgent, err = hcpo.createExecutionAgent(ctx, "execution", stepIndex+1, iteration, agentName, step.AgentConfigs)
					if err != nil {
						return "", updatedContextFiles, fmt.Errorf("failed to create execution agent for step %d: %w", stepIndex+1, err)
					}

					// Execute this specific step - no conversation history needed, all context in template variables
					// Capture execution result and conversation history (conversation history for learning agents)
					executionResult, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
				}
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", stepIndex+1, retryAttempt, err)
					if retryAttempt >= maxRetryAttempts {
						hcpo.GetLogger().Errorf("❌ Step %d execution failed after %d attempts, exiting retry loop", stepIndex+1, maxRetryAttempts)
						break // Exit retry loop - will proceed to human feedback
					}
					continue // Retry on next attempt
				}

				hcpo.GetLogger().Infof("✅ Step %d execution completed successfully (attempt %d)", stepIndex+1, retryAttempt)

				// Check if validation is disabled for this step
				disableValidation := step.AgentConfigs != nil && step.AgentConfigs.DisableValidation != nil && *step.AgentConfigs.DisableValidation
				if disableValidation {
					hcpo.GetLogger().Infof("⏭️ Validation disabled for step %d - auto-approving", stepIndex+1)
					// Auto-approve: create a success validation response
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            "Validation disabled - step auto-approved",
					}
					if step.HasLoop {
						// For loop steps, mark condition as met when validation is disabled
						validationResponse.LoopConditionMet = true
						loopConditionMet = true
					}
				} else {
					// Always validate step execution
					hcpo.GetLogger().Infof("🔍 Validating step %d execution (attempt %d)", stepIndex+1, retryAttempt)

					// Reuse sanitized title from execution agent (already computed above)
					validationAgentName := fmt.Sprintf("%s-%s", stepPath, sanitizedTitle)
					// Add loop iteration to validation agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						validationAgentName = fmt.Sprintf("%s-loop-%d", validationAgentName, loopIterationCount)
					}
					validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, iteration, validationAgentName, step.AgentConfigs)
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", stepIndex+1, err)
						if retryAttempt >= maxRetryAttempts {
							break // Exit retry loop - will proceed to human feedback
						}
						continue // Retry on next attempt
					}

					// Prepare validation template variables with individual fields
					// Use run folder path if available
					var validationWorkspacePath string
					if hcpo.selectedRunFolder != "" {
						validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
					} else {
						validationWorkspacePath = hcpo.GetWorkspacePath()
					}
					validationTemplateVars := map[string]string{
						"StepTitle":           step.Title,
						"StepDescription":     step.Description,
						"StepSuccessCriteria": step.SuccessCriteria,
						"StepContextOutput":   step.ContextOutput,
						"WorkspacePath":       validationWorkspacePath,
						"ExecutionHistory":    shared.FormatConversationHistory(executionConversationHistory),
					}

					// Add context dependencies as a comma-separated string
					if len(step.ContextDependencies) > 0 {
						validationTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
					} else {
						validationTemplateVars["StepContextDependencies"] = ""
					}

					// If in loop mode, pass loop condition to validation agent
					if step.HasLoop {
						validationTemplateVars["LoopCondition"] = step.LoopCondition
						hcpo.GetLogger().Infof("🔍 Checking loop condition for step %d (iteration %d): %s", stepIndex+1, loopIterationCount, step.LoopCondition)
					} else {
						validationTemplateVars["LoopCondition"] = ""
					}

					// Validate this step's execution using structured output
					validationResponse, _, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", stepIndex+1, retryAttempt, err)
						if retryAttempt >= maxRetryAttempts {
							break // Exit retry loop - will proceed to human feedback with nil validationResponse
						}
						continue // Retry on next attempt
					}

					hcpo.GetLogger().Infof("✅ Step %d validation completed successfully (attempt %d)", stepIndex+1, retryAttempt)
					hcpo.GetLogger().Infof("📊 Validation result: Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
				}

				// If in loop mode, check loop condition instead of full validation
				if step.HasLoop {
					// Check loop condition from validation response
					if validationResponse.LoopConditionMet {
						hcpo.GetLogger().Infof("✅ Step %d loop condition met (iteration %d)", stepIndex+1, loopIterationCount)
						loopConditionMet = true

						// Run success learning when loop completes successfully (before breaking)
						// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
						isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
						// Check step-specific learning detail level
						isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning
						isLearningDetailLevelNone := false
						if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
							isLearningDetailLevelNone = true
						}
						isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
						// CODE EXECUTION MODE: Force learning enabled regardless of step config
						// Use step-level code execution mode (already computed above)
						if isCodeExecutionMode && isLearningDisabled {
							hcpo.GetLogger().Infof("🔧 Code execution mode enabled - forcing learning for step %d (overriding step config)", stepIndex+1)
							isLearningDisabled = false
						}
						hcpo.GetLogger().Infof("🔍 DEBUG: Step %d (loop) - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v, codeExecutionMode=%v)", stepIndex+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep, isCodeExecutionMode)
						if !isFastExecuteStep && !isLearningDisabled {
							// Success Learning Agent - analyze what worked well and update plan.json
							// Loop condition met means step completed successfully
							hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d (loop completed)", stepIndex+1)
							_, err := hcpo.runSuccessLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
							if err != nil {
								hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", stepIndex+1, err)
							} else {
								hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", stepIndex+1)
							}
						} else {
							if isFastExecuteStep {
								hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1)
							} else if isLearningDisabled {
								hcpo.GetLogger().Infof("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1)
							}
						}

						break // Exit retry loop, will exit main loop at top
					} else {
						hcpo.GetLogger().Infof("🔄 Step %d loop condition not met yet (iteration %d/%d), continuing loop", stepIndex+1, loopIterationCount, step.MaxIterations)

						// Check if learning should run after each loop iteration
						learningAfterLoopIteration := step.AgentConfigs != nil && step.AgentConfigs.LearningAfterLoopIteration
						if learningAfterLoopIteration {
							// Run learning after this loop iteration
							isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
							// Check step-specific learning detail level
							isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning
							isLearningDetailLevelNone := false
							if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
								isLearningDetailLevelNone = true
							}
							isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
							// CODE EXECUTION MODE: Force learning enabled regardless of step config
							// Use step-level code execution mode (already computed above)
							if isCodeExecutionMode && isLearningDisabled {
								hcpo.GetLogger().Infof("🔧 Code execution mode enabled - forcing learning for step %d loop iteration (overriding step config)", stepIndex+1)
								isLearningDisabled = false
							}

							if !isFastExecuteStep && !isLearningDisabled {
								hcpo.GetLogger().Infof("🧠 Running learning analysis after loop iteration %d for step %d", loopIterationCount, stepIndex+1)
								// Run learning even though condition not met (for iteration analysis)
								_, err := hcpo.runSuccessLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
								if err != nil {
									hcpo.GetLogger().Warnf("⚠️ Learning phase failed after loop iteration %d for step %d: %v", loopIterationCount, stepIndex+1, err)
								} else {
									hcpo.GetLogger().Infof("✅ Learning analysis completed after loop iteration %d for step %d", loopIterationCount, stepIndex+1)
								}
							}
						}

						// Capture execution result (final response) and validation outputs for next iteration
						previousIterationExecutionOutput = executionResult
						validationOutputParts := []string{}
						if validationResponse.Reasoning != "" {
							validationOutputParts = append(validationOutputParts, fmt.Sprintf("**Reasoning**: %s", validationResponse.Reasoning))
						}
						if validationResponse.LoopReasoning != "" {
							validationOutputParts = append(validationOutputParts, fmt.Sprintf("**Loop Reasoning**: %s", validationResponse.LoopReasoning))
						}
						if len(validationResponse.Feedback) > 0 {
							feedbackParts := []string{"**Feedback**: "}
							for _, fb := range validationResponse.Feedback {
								feedbackParts = append(feedbackParts, fmt.Sprintf("- [%s] %s: %s", fb.Severity, fb.Type, fb.Description))
							}
							validationOutputParts = append(validationOutputParts, strings.Join(feedbackParts, "\n"))
						}
						previousIterationValidationOutput = strings.Join(validationOutputParts, "\n\n")
						hcpo.GetLogger().Infof("📝 Captured execution and validation outputs for iteration %d (will be included in next iteration)", loopIterationCount)
						break // Exit retry loop, continue main loop for next iteration
					}
				}

				// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
				isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
				// Check step-specific learning detail level
				isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning
				isLearningDetailLevelNone := false
				if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
					isLearningDetailLevelNone = true
				}
				isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
				// CODE EXECUTION MODE: Force learning enabled regardless of step config
				// Use step-level code execution mode (already computed above)
				if isCodeExecutionMode && isLearningDisabled {
					hcpo.GetLogger().Infof("🔧 Code execution mode enabled - forcing learning for step %d (overriding step config)", stepIndex+1)
					isLearningDisabled = false
				}
				hcpo.GetLogger().Infof("🔍 DEBUG: Step %d - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v, codeExecutionMode=%v)", stepIndex+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep, isCodeExecutionMode)
				if isFastExecuteStep || isLearningDisabled {
					if isFastExecuteStep {
						hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1)
					} else if isLearningDisabled {
						hcpo.GetLogger().Infof("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1)
					}
				} else {
					// Run appropriate learning phase based on validation result
					if validationResponse.IsSuccessCriteriaMet {
						// Success Learning Agent - analyze what worked well and update plan.json
						hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d", stepIndex+1)
						_, err := hcpo.runSuccessLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", stepIndex+1, err)
						} else {
							hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", stepIndex+1)
						}
					} else {
						// Failure Learning Agent - analyze what went wrong and provide refined task description
						// SKIP failure learning for loop steps - loop steps only run success learning when condition is met
						if step.HasLoop {
							hcpo.GetLogger().Infof("🔄 Step %d is a loop step - skipping failure learning (loop steps only run success learning when condition is met)", stepIndex+1)
						} else {
							hcpo.GetLogger().Infof("🧠 Running failure learning analysis for step %d", stepIndex+1)
							refinedTaskDescription, _, err := hcpo.runFailureLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
							if err != nil {
								hcpo.GetLogger().Warnf("⚠️ Failure learning phase failed for step %d: %v", stepIndex+1, err)
							} else {
								hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d", stepIndex+1)

								// Update step description for retry
								if refinedTaskDescription != "" {
									step.Description = refinedTaskDescription
									templateVars["StepDescription"] = refinedTaskDescription
									hcpo.GetLogger().Infof("🔄 Updated step %d description with refined task for retry", stepIndex+1)
								}
							}
						}
					}
				}

				// Check if success criteria was met (only for non-loop steps or when loop handling is done)
				if !step.HasLoop {
					if validationResponse.IsSuccessCriteriaMet {
						hcpo.GetLogger().Infof("✅ Step %d passed validation - success criteria met", stepIndex+1)
						break // Exit retry loop and continue to next step
					} else {
						hcpo.GetLogger().Warnf("⚠️ Step %d failed validation - success criteria not met (attempt %d/%d)", stepIndex+1, retryAttempt, maxRetryAttempts)

						if retryAttempt >= maxRetryAttempts {
							hcpo.GetLogger().Errorf("❌ Step %d failed validation after %d attempts", stepIndex+1, maxRetryAttempts)
							// Mark that validation failed after exhausting all retries
							validationFailedAfterMaxRetries = true
							break
						} else {
							hcpo.GetLogger().Infof("🔄 Retrying step %d execution with validation feedback", stepIndex+1)
							// Note: conversation history is preserved from previous attempts for context
						}
					}
				}
			} // End of retry loop

			// Exit immediately if validation failed after exhausting all retry attempts
			if validationFailedAfterMaxRetries && !step.HasLoop {
				hcpo.GetLogger().Errorf("🛑 Step %d failed validation after %d attempts - exiting workflow", stepIndex+1, maxRetryAttempts)
				var validationDetails string
				if validationResponse != nil {
					validationDetails = fmt.Sprintf("Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
					if validationResponse.Reasoning != "" {
						validationDetails += fmt.Sprintf(", Reasoning: %s", validationResponse.Reasoning)
					}
				} else {
					validationDetails = "No validation response available"
				}
				return executionResult, updatedContextFiles, fmt.Errorf("step %d failed validation after %d retry attempts. %s. Please review the execution results and update the plan if needed", stepIndex+1, maxRetryAttempts, validationDetails)
			}

			// If in loop mode and condition not met, continue main loop
			if step.HasLoop && !loopConditionMet {
				continue // Continue main loop for next iteration
			}

			// Exit main loop if not in loop mode or loop condition met
			if !step.HasLoop {
				// Non-loop step: execute once and exit
				break // Exit main execution loop
			}
			if loopConditionMet {
				// Loop step with condition met: exit loop
				break // Exit main execution loop
			}
			// Loop step with condition not met: continue to next iteration
		} // End of main execution loop

		// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step
		// If user provides feedback (doesn't approve), stop workflow and ask user to manually update plan
		// FAST MODE: Skip human feedback and auto-approve
		// SKIP HUMAN INPUT MODE: Skip human feedback but keep learning enabled
		// NORMAL MODE & LOOP MODE: Always request human feedback before moving to next step
		isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
		isSkipHumanInput := hcpo.IsSkipHumanInput()
		hcpo.GetLogger().Infof("🔍 DEBUG: Step %d human feedback check - fastExecuteMode=%v, fastExecuteEndStep=%d, stepIndex=%d, isFastExecuteStep=%v, skipHumanInput=%v", stepIndex+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, stepIndex, isFastExecuteStep, isSkipHumanInput)
		var approved bool
		var feedback string

		// In fast execute mode or skip human input mode, always auto-approve without human feedback
		if isFastExecuteStep || isSkipHumanInput {
			if isFastExecuteStep {
				hcpo.GetLogger().Infof("⚡ Fast mode: Auto-approving step %d without human feedback (stepIndex=%d <= fastExecuteEndStep=%d)", stepIndex+1, stepIndex, hcpo.fastExecuteEndStep)
			} else {
				hcpo.GetLogger().Infof("⚡ Skip human input mode: Auto-approving step %d without human feedback (learning will still run)", stepIndex+1)
			}
			approved = true
			feedback = "" // No feedback in fast mode or skip human input mode
		} else {
			// Normal mode and loop mode: Request human feedback
			var validationSummary string
			if validationResponse != nil {
				validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", stepIndex+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
			} else {
				validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", stepIndex+1)
			}
			var err error
			approved, feedback, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps, validationSummary)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %w", err)
				// Default to continue if feedback fails
				approved = true
			}
		}

		// Store human feedback for future steps (even if approved, user might have provided guidance)
		if feedback != "" {
			feedbackEntry := fmt.Sprintf("Step %d/%d Feedback: %s", stepIndex+1, totalSteps, feedback)
			// Note: humanFeedbackHistory is not available in this function scope, so we skip storing it
			// It will be handled by the caller if needed
			hcpo.GetLogger().Infof("📝 Human feedback received for step %d: %s", stepIndex+1, feedbackEntry)
		}

		if approved {
			// User approved - mark step as completed and exit outer loop
			// Only update progress if this is not a branch step
			if !isBranchStep {
				progress.CompletedStepIndices = append(progress.CompletedStepIndices, stepIndex)
				// Always save progress after marking a step as completed (both fast and normal mode)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to save step progress: %w", err)
				} else {
					modeStr := "fast mode"
					if !isFastExecuteStep {
						modeStr = "normal mode"
					}
					hcpo.GetLogger().Infof("✅ Step %d/%d marked as completed and saved (%s) - Total completed: %d/%d", stepIndex+1, totalSteps, modeStr, len(progress.CompletedStepIndices), progress.TotalSteps)
				}

				// Emit step token usage summary
				stepTitle := step.Title
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				hcpo.EmitStepTokenUsage(ctx, "execution", stepIndex, stepTitle, false) // Don't clear - keep for potential future queries
			} else {
				hcpo.GetLogger().Infof("✅ Branch step %d completed (not updating main progress)", stepIndex+1)
			}
			stepCompleted = true
		} else {
			// User provided feedback (didn't approve) - stop workflow and ask user to manually update plan
			hcpo.GetLogger().Infof("🛑 User provided feedback - stopping workflow. Feedback: %s", feedback)
			planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
			return executionResult, updatedContextFiles, fmt.Errorf("workflow stopped: user feedback received. please manually update the plan at %s with the following feedback, then restart the workflow: %s", planPath, feedback)
		}
	} // End of outer loop for step execution

	// Append step's context output to context files if it exists
	if step.ContextOutput != "" {
		updatedContextFiles = append(updatedContextFiles, step.ContextOutput)
		hcpo.GetLogger().Infof("📝 Added step context output to context files: %s", step.ContextOutput)
	}

	return executionResult, updatedContextFiles, nil
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(
	ctx context.Context,
	breakdownSteps []TodoStep,
	iteration int,
	progress *StepProgress,
	startFromStep int,
) ([]llmtypes.MessageContent, error) {
	hcpo.GetLogger().Infof("🔄 Starting step-by-step execution of %d steps (starting from step %d)",
		len(breakdownSteps), startFromStep+1)

	// Learning detail level is now configured per-step via AgentConfigs
	// Each step can specify its own learning detail level, defaults to "general" if not set
	hcpo.GetLogger().Infof("📝 Using per-step learning detail level configuration")

	// Run folder should already be resolved early (after plan approval)
	if hcpo.selectedRunFolder == "" {
		return nil, fmt.Errorf("run folder not resolved - this should have been set after plan approval")
	}
	hcpo.GetLogger().Infof("📁 Using resolved run folder: %s", hcpo.selectedRunFolder)

	// Execute each step one by one
	for i, step := range breakdownSteps {
		// Reset fast execute mode if we've passed the fast execute range
		// This ensures normal execution (with learning and human feedback) for steps after fastExecuteEndStep
		if hcpo.fastExecuteMode && i > hcpo.fastExecuteEndStep {
			hcpo.GetLogger().Infof("🔄 Fast execute mode completed (steps 0-%d), resetting to normal execution mode for step %d+", hcpo.fastExecuteEndStep, i+1)
			hcpo.SetFastExecuteMode(false, -1)
			// Ensure progress is saved when transitioning from fast to normal mode
			// This catches any steps that were completed in fast mode but not yet saved
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save progress during fast→normal transition: %w", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved progress during fast→normal mode transition: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps)
			}
		}

		// Skip if step is already completed
		if i < startFromStep {
			hcpo.GetLogger().Infof("⏭️ Skipping step %d/%d (already completed): %s",
				i+1, len(breakdownSteps), step.Title)
			continue
		}

		// Check if step is in completed list
		// BUT: If we're in single-step mode and this is the target step, force execution even if completed
		isCompleted := false
		forceExecution := false
		if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
			// Force execution of target step even if completed
			forceExecution = true
			hcpo.GetLogger().Infof("🎯 Single-step mode: forcing execution of target step %d even if previously completed", i+1)
		} else {
			for _, completedIdx := range progress.CompletedStepIndices {
				if completedIdx == i {
					isCompleted = true
					break
				}
			}
		}
		if isCompleted && !forceExecution {
			hcpo.GetLogger().Infof("⏭️ Skipping step %d/%d (marked as completed): %s",
				i+1, len(breakdownSteps), step.Title)
			continue
		}

		hcpo.GetLogger().Infof("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title)

		// Build context files from previous steps
		previousContextFiles := make([]string, 0)
		for prevIdx := 0; prevIdx < i; prevIdx++ {
			if prevIdx < len(breakdownSteps) && breakdownSteps[prevIdx].ContextOutput != "" {
				previousContextFiles = append(previousContextFiles, breakdownSteps[prevIdx].ContextOutput)
			}
		}

		// Check if this is a conditional step
		if step.HasCondition {
			// Execute conditional step
			hcpo.GetLogger().Infof("🔀 Starting conditional step execution: %s", step.Title)
			if err := hcpo.executeConditionalStep(ctx, step, i, 0, progress, previousContextFiles, iteration); err != nil {
				hcpo.GetLogger().Errorf("❌ Conditional step %d execution failed: %v", i+1, err)
				// Emit error event
				eventBridge := hcpo.GetContextAwareBridge()
				if eventBridge != nil {
					errorEvent := &events.OrchestratorAgentErrorEvent{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						AgentType: "workflow",
						AgentName: "conditional-step-execution",
						Objective: fmt.Sprintf("Execute conditional step: %s", step.Title),
						Error:     err.Error(),
						StepIndex: i,
						Iteration: iteration,
					}
					eventBridge.HandleEvent(ctx, &events.AgentEvent{
						Type:      events.OrchestratorAgentError,
						Timestamp: time.Now(),
						Data:      errorEvent,
					})
				}
				return nil, fmt.Errorf("conditional step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Infof("✅ Conditional step %d completed successfully: %s", i+1, step.Title)

			// Mark conditional step as completed (executeConditionalStep handles progress internally)
			progress.CompletedStepIndices = append(progress.CompletedStepIndices, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save progress after conditional step: %w", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved progress: conditional step %d marked as completed", i+1)
			}

			// Check if we're in single step mode and should stop
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Infof("🎯 Single step mode: completed target step %d, stopping execution", i+1)
				hcpo.SetRunSingleStepMode(false, -1) // Reset mode
				break
			}

			// Continue to next step
			continue
		}

		// Execute regular step using executeSingleStep
		stepPath := fmt.Sprintf("step-%d", i+1)
		executionResult, _, err := hcpo.executeSingleStep(
			ctx,
			step,
			i,
			stepPath,
			len(breakdownSteps),
			iteration,
			previousContextFiles,
			progress,
			false, // isBranchStep = false
		)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Step %d execution failed: %v", i+1, err)
			return nil, fmt.Errorf("step %d execution failed: %w", i+1, err)
		}

		// Log execution result (for debugging)
		hcpo.GetLogger().Infof("✅ Step %d execution completed (result length: %d chars)", i+1, len(executionResult))

		// Check if we're in single step mode and should stop
		if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
			hcpo.GetLogger().Infof("🎯 Single step mode: completed target step %d, stopping execution", i+1)
			hcpo.SetRunSingleStepMode(false, -1) // Reset mode
			break
		}

		// Note: Progress tracking is handled inside executeSingleStep
		// Continue to next step
		continue
	}

	// Final save to ensure all completed steps are persisted
	// This is a safety measure to catch any steps that might have been missed
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to save final step progress: %w", err)
	} else {
		hcpo.GetLogger().Infof("💾 Final progress save completed: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps)
	}

	hcpo.GetLogger().Infof("✅ All steps execution completed")
	return nil, nil
}

// OLD CODE REMOVED - The following section was replaced by executeSingleStep() function above
// All that logic has been extracted into executeSingleStep() for reusability

// max returns the maximum value in a slice of integers
func max(slice []int) int {
	if len(slice) == 0 {
		return -1
	}
	maxVal := slice[0]
	for _, val := range slice {
		if val > maxVal {
			maxVal = val
		}
	}
	return maxVal
}

// sanitizeTitleForAgentName sanitizes a step title for use in agent names
// - Removes step number prefixes (e.g., "Step 4:", "Step 5 -", "Step 3.")
// - Removes/replaces special characters (colons, slashes, etc.)
// - Normalizes whitespace and converts to lowercase
// - Removes multiple consecutive dashes
func (hcpo *HumanControlledTodoPlannerOrchestrator) sanitizeTitleForAgentName(title string) string {
	sanitized := strings.TrimSpace(title)

	// Remove step number prefixes (case-insensitive)
	// Matches: "Step N:", "Step N -", "Step N.", "Step N ", etc.
	stepNumberPattern := regexp.MustCompile(`(?i)^step\s+\d+\s*[:.\-]*\s*`)
	sanitized = stepNumberPattern.ReplaceAllString(sanitized, "")

	// Replace spaces with dashes
	sanitized = strings.ReplaceAll(sanitized, " ", "-")

	// Remove or replace special characters that aren't safe for agent names
	// Keep: letters, numbers, dashes, underscores
	// Remove: colons, slashes, backslashes, pipes, etc.
	specialCharPattern := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	sanitized = specialCharPattern.ReplaceAllString(sanitized, "-")

	// Normalize multiple consecutive dashes to single dash
	multiDashPattern := regexp.MustCompile(`-+`)
	sanitized = multiDashPattern.ReplaceAllString(sanitized, "-")

	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")

	// Convert to lowercase for consistency
	sanitized = strings.ToLower(sanitized)

	// Ensure we have something left (fallback if everything was removed)
	if sanitized == "" {
		sanitized = "step"
	}

	return sanitized
}
