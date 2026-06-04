package step_based_workflow

// clearStepConfigField clears a named field on the given StepConfig so the step
// falls back to preset/default behavior on next execution. All struct fields
// that back the update_step_config tool use `omitempty` + pointer-types (or
// strings / nil-able slices), so resetting to the zero value makes the JSON
// marshaler drop the key entirely — which is what agents observe as "removed".
//
// Returns true when the name matched a known field, false otherwise.
// Keep the switch in lockstep with update_step_config's JSON schema: every name
// listed in the clear_fields description must have a case here, and vice versa.
func clearStepConfigField(sc *StepConfig, name string) bool {
	if sc == nil {
		return false
	}
	// validation_schema lives on StepConfig itself, not AgentConfigs.
	if name == "validation_schema" {
		sc.ValidationSchema = nil
		return true
	}
	if sc.AgentConfigs == nil {
		// Nothing to clear below this point; agent-scoped config doesn't exist.
		// Still report the name as recognized so callers don't see a false negative.
		return isKnownAgentConfigClearField(name)
	}
	ac := sc.AgentConfigs
	switch name {
	// LLM overrides
	case "execution_llm":
		ac.ExecutionLLM = nil
	case "execution_tier":
		ac.ExecutionTier = ""
	case "learning_llm":
		ac.LearningLLM = nil

	// Slice selections
	case "servers":
		ac.SelectedServers = nil
	case "tools":
		ac.SelectedTools = nil
	case "enabled_custom_tools":
		ac.EnabledCustomTools = nil
	case "enabled_skills":
		ac.EnabledSkills = nil

	// Pointer-bool flags — only the ones with a corresponding setter in update_step_config.
	// The tool intentionally omits fields without a setter (e.g. enable_context_offloading,
	// keep_learning_full) since clearing what can't be set is a no-op from the agent's side.
	case "learning_objective":
		ac.LearningObjective = ""
	case "lock_learnings":
		ac.LockLearnings = nil
	case "lock_code":
		ac.LockCode = nil
	case "use_code_execution_mode":
		ac.UseCodeExecutionMode = nil
	case "disable_parallel_tool_execution":
		ac.DisableParallelToolExecution = nil
	case "description_reviewed":
		ac.DescriptionReviewed = nil

	// String fields (empty string + omitempty drops the key)
	case "learnings_access":
		ac.LearningsAccess = ""
	case "learnings_write_method":
		ac.LearningsWriteMethod = ""
	case "knowledgebase_access":
		ac.KnowledgebaseAccess = ""
	case "knowledgebase_contribution":
		ac.KnowledgebaseContribution = ""
	case "knowledgebase_write_method":
		ac.KnowledgebaseWriteMethod = ""
	case "db_access":
		ac.DBAccess = ""
	case "review_notes":
		ac.ReviewNotes = ""
	case "declared_execution_mode":
		ac.DeclaredExecutionMode = ""
	case "declared_execution_mode_reason":
		ac.DeclaredExecutionModeReason = ""
	case "global_skill_objective":
		ac.GlobalSkillObjective = ""
	case "coding_agent_tmux_lifecycle":
		ac.CodingAgentTmuxLifecycle = ""
	case "transport":
		ac.Transport = ""

	default:
		return false
	}
	return true
}

// isKnownAgentConfigClearField mirrors the switch in clearStepConfigField and is
// used when AgentConfigs is nil — callers still get a truthful "is this name
// valid" answer without needing to allocate the config just to clear fields on
// an already-empty struct.
func isKnownAgentConfigClearField(name string) bool {
	switch name {
	case "execution_llm", "execution_tier", "learning_llm",
		"servers", "tools", "enabled_custom_tools", "enabled_skills",
		"learning_objective", "lock_learnings", "lock_code",
		"use_code_execution_mode",
		"disable_parallel_tool_execution",
		"description_reviewed",
		"learning_mode", "knowledgebase_access", "knowledgebase_contribution",
		"knowledgebase_write_method",
		"learnings_write_method", "db_access",
		"review_notes", "declared_execution_mode", "declared_execution_mode_reason",
		"global_skill_objective", "coding_agent_tmux_lifecycle",
		"transport":
		return true
	}
	return false
}
