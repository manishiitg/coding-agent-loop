package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

const evaluationPlanRelPath = "evaluation/evaluation_plan.json"

// evaluationPlanEditableFields are the step fields update_evaluation_plan may
// set. Everything else on a step is preserved untouched.
var evaluationPlanEditableFields = []string{
	"title",
	"description",
	"context_output",
	"context_dependencies",
	"max_score",
	"applies_to_routes",
	"validation_schema",
	"pre_validation",
	"db_write",
	"execution_mode",
}

// UpdateEvaluationPlanStep edits one step in evaluation/evaluation_plan.json and
// records the change in planning/changelog.
//
// It edits the decoded JSON rather than the EvaluationStep struct, because the
// struct is not a faithful mirror of the file: social-media's plan carries
// max_score and context_dependencies on every step and EvaluationStep has
// neither, so a read-modify-write through it would delete both from any step it
// touched — silently, producing valid JSON. max_score is the subject of four of
// that workflow's open findings, so a struct-based tool would have deepened the
// exact problem it was built to fix. Editing the map preserves every key it does
// not recognize, including fields added after this code was written.
//
// This is also why the eval plan had no tool at all until now, and therefore no
// changelog entries: Artifact Review reads planning/changelog to detect drift,
// and social-media's newest eval-step entry was 2026-05-29 while git showed four
// later evaluation-plan commits (AR-20260729-2).
func UpdateEvaluationPlanStep(
	ctx context.Context,
	workspacePath string,
	stepID string,
	updates map[string]interface{},
	reason string,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	logger loggerv2.Logger,
) (string, error) {
	stepID = strings.TrimSpace(stepID)
	reason = strings.TrimSpace(reason)
	if stepID == "" {
		return "", fmt.Errorf("step_id is required")
	}
	if reason == "" {
		return "", fmt.Errorf("reason is required: the changelog records why an evaluation step changed, and drift review cannot judge a change without it")
	}
	if len(updates) == 0 {
		return "", fmt.Errorf("no fields to update; provide at least one of %s", strings.Join(evaluationPlanEditableFields, ", "))
	}
	allowed := map[string]bool{}
	for _, field := range evaluationPlanEditableFields {
		allowed[field] = true
	}
	unknown := make([]string, 0, len(updates))
	for field := range updates {
		if !allowed[field] {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", fmt.Errorf("cannot set %s; editable fields are %s", strings.Join(unknown, ", "), strings.Join(evaluationPlanEditableFields, ", "))
	}

	// validation_schema/pre_validation are accepted here as raw decoded JSON
	// (see the function doc for why this tool edits the map, not a struct),
	// which meant they never passed through the same schema validators every
	// other schema-writing tool in this file does -- PLAT-236 found this only
	// after the exact unsatisfiable value_type/pattern shape it fixed
	// elsewhere could still be authored through this specific tool
	// untouched. Validate both by round-tripping through ValidationSchema.
	for _, field := range []string{"validation_schema", "pre_validation"} {
		raw, ok := updates[field]
		if !ok {
			continue
		}
		if err := validateSchemaLikeUpdateField(field, raw); err != nil {
			return "", err
		}
	}

	// execution_mode is the one place an evaluation step's execution model
	// lives (PLAT-287): "scripted" runs learnings/<step-id>/main.py through
	// the scripted executor, "agentic" (or unset) runs the step conversationally.
	if raw, ok := updates["execution_mode"]; ok {
		mode, isString := raw.(string)
		if !isString {
			return "", fmt.Errorf("execution_mode must be a string (\"scripted\" or \"agentic\"), got %T", raw)
		}
		canonical := canonicalDeclaredExecutionMode(mode)
		if canonical != StepModeScripted && canonical != StepModeAgentic && canonical != "" {
			return "", fmt.Errorf("execution_mode must be \"scripted\" or \"agentic\" (empty clears it), got %q", mode)
		}
		updates["execution_mode"] = canonical
	}

	path, document, stepsKey, rawSteps, err := loadEvaluationPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return "", err
	}

	changes := make([]PlanFieldChange, 0, len(updates))
	found := false
	for _, entry := range rawSteps {
		step, isObject := entry.(map[string]interface{})
		if !isObject {
			continue
		}
		if id, _ := step["id"].(string); strings.TrimSpace(id) != stepID {
			continue
		}
		found = true
		fields := make([]string, 0, len(updates))
		for field := range updates {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			previous, existed := step[field]
			next := updates[field]
			if existed && jsonEqual(previous, next) {
				continue
			}
			step[field] = next
			if !existed {
				previous = nil
			}
			changes = append(changes, PlanFieldChange{
				StepID:   stepID,
				Field:    field,
				OldValue: previous,
				NewValue: next,
			})
		}
		break
	}
	if !found {
		return "", fmt.Errorf("no evaluation step with id %q", stepID)
	}
	if len(changes) == 0 {
		return fmt.Sprintf("No change: evaluation step %q already holds these values.", stepID), nil
	}

	document[stepsKey] = rawSteps
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", evaluationPlanRelPath, err)
	}
	// Prove the result still loads as an evaluation plan before writing it. A
	// tool that writes a file the runtime cannot read is worse than one that
	// refuses.
	var check EvaluationPlan
	if err := json.Unmarshal(encoded, &check); err != nil {
		return "", fmt.Errorf("refusing to write: the edited plan no longer parses as an evaluation plan: %w", err)
	}
	if len(check.Steps) != len(rawSteps) {
		return "", fmt.Errorf("refusing to write: the edited plan parses to %d steps, expected %d", len(check.Steps), len(rawSteps))
	}
	if err := writeFile(ctx, path, string(encoded)); err != nil {
		return "", fmt.Errorf("write %s: %w", evaluationPlanRelPath, err)
	}

	logPlanChange(ctx, workspacePath, PlanChangelogEntry{
		Tool:    "update_evaluation_plan",
		Reason:  reason,
		StepIDs: []string{stepID},
		Changes: changes,
	}, readFile, writeFile, logger)

	changed := make([]string, 0, len(changes))
	for _, change := range changes {
		changed = append(changed, change.Field)
	}
	return fmt.Sprintf("Updated evaluation step %q (%s) and recorded it in planning/changelog.", stepID, strings.Join(changed, ", ")), nil
}

// AddEvaluationPlanStep appends one governed evaluation step and records the
// full added object in planning/changelog. This closes the empty-plan gap left
// by update_evaluation_plan: an update tool cannot create the first step.
func AddEvaluationPlanStep(
	ctx context.Context,
	workspacePath string,
	step map[string]interface{},
	reason string,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	logger loggerv2.Logger,
) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("reason is required: the changelog records why an evaluation step was added")
	}
	stepID, _ := step["id"].(string)
	title, _ := step["title"].(string)
	description, _ := step["description"].(string)
	stepID, title, description = strings.TrimSpace(stepID), strings.TrimSpace(title), strings.TrimSpace(description)
	if stepID == "" || title == "" || description == "" {
		return "", fmt.Errorf("id, title, and description are required")
	}
	step["id"], step["title"], step["description"] = stepID, title, description

	for _, field := range []string{"validation_schema", "pre_validation"} {
		if raw, ok := step[field]; ok && raw != nil {
			if err := validateSchemaLikeUpdateField(field, raw); err != nil {
				return "", err
			}
		}
	}
	if raw, ok := step["execution_mode"]; ok {
		mode, isString := raw.(string)
		if !isString {
			return "", fmt.Errorf("execution_mode must be a string (\"scripted\" or \"agentic\"), got %T", raw)
		}
		canonical := canonicalDeclaredExecutionMode(mode)
		if canonical != StepModeScripted && canonical != StepModeAgentic && canonical != "" {
			return "", fmt.Errorf("execution_mode must be \"scripted\" or \"agentic\" (empty clears it), got %q", mode)
		}
		step["execution_mode"] = canonical
	}

	path, document, stepsKey, rawSteps, err := loadEvaluationPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return "", err
	}
	for _, entry := range rawSteps {
		if existing, ok := entry.(map[string]interface{}); ok {
			if id, _ := existing["id"].(string); strings.TrimSpace(id) == stepID {
				return "", fmt.Errorf("evaluation step id %q already exists", stepID)
			}
		}
	}

	// Execution and evaluation steps share one identity namespace. Refuse the
	// collision before writing so the operation remains atomic.
	execPlan, readErr := readPlanFromFile(ctx, workspacePath, readFile)
	if readErr != nil {
		return "", fmt.Errorf("read execution plan for cross-plan ID validation: %w", readErr)
	}
	if execPlan != nil {
		execIDs := make(map[string]string)
		if err := collectStepIDsRecursive(execPlan.Steps, "plan.steps", execIDs); err != nil {
			return "", err
		}
		if err := collectStepIDsRecursive(execPlan.OrphanSteps, "plan.orphan_steps", execIDs); err != nil {
			return "", err
		}
		if location, exists := execIDs[stepID]; exists {
			return "", fmt.Errorf("step ID %q collides with execution step at %s", stepID, location)
		}
	}

	rawSteps = append(rawSteps, step)
	document[stepsKey] = rawSteps
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", evaluationPlanRelPath, err)
	}
	var check EvaluationPlan
	if err := json.Unmarshal(encoded, &check); err != nil {
		return "", fmt.Errorf("refusing to write: the edited plan no longer parses as an evaluation plan: %w", err)
	}
	if len(check.Steps) != len(rawSteps) {
		return "", fmt.Errorf("refusing to write: the edited plan parses to %d steps, expected %d", len(check.Steps), len(rawSteps))
	}
	addedJSON, err := json.Marshal(step)
	if err != nil {
		return "", fmt.Errorf("encode added evaluation step: %w", err)
	}
	if err := writeFile(ctx, path, string(encoded)); err != nil {
		return "", fmt.Errorf("write %s: %w", evaluationPlanRelPath, err)
	}
	logPlanChange(ctx, workspacePath, PlanChangelogEntry{
		Tool:       "add_evaluation_step",
		Reason:     reason,
		StepIDs:    []string{stepID},
		AddedSteps: []json.RawMessage{addedJSON},
	}, readFile, writeFile, logger)
	return fmt.Sprintf("Added evaluation step %q and recorded it in planning/changelog.", stepID), nil
}

// loadEvaluationPlanDocument reads and decodes evaluation/evaluation_plan.json
// into a generic container (unrecognized keys survive a later write) and
// resolves whether this file uses "steps" or the legacy "eval_steps" key,
// honoring whichever it actually has rather than rewriting it into the other
// shape. Shared by every mutating tool in this file.
func loadEvaluationPlanDocument(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (path string, document map[string]interface{}, stepsKey string, rawSteps []interface{}, err error) {
	path = strings.Trim(strings.TrimSpace(workspacePath), "/") + "/" + evaluationPlanRelPath
	raw, err := readFile(ctx, path)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("read %s: %w", evaluationPlanRelPath, err)
	}
	if strings.TrimSpace(raw) == "" {
		return "", nil, "", nil, fmt.Errorf("%s is empty or missing", evaluationPlanRelPath)
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return "", nil, "", nil, fmt.Errorf("parse %s: %w", evaluationPlanRelPath, err)
	}
	stepsKey = "steps"
	steps, ok := document[stepsKey].([]interface{})
	if !ok {
		if alt, altOK := document["eval_steps"].([]interface{}); altOK {
			stepsKey, steps, ok = "eval_steps", alt, true
		}
	}
	if !ok {
		return "", nil, "", nil, fmt.Errorf("%s has no steps array", evaluationPlanRelPath)
	}
	return path, document, stepsKey, steps, nil
}

// DeleteEvaluationPlanSteps removes one or more steps from
// evaluation/evaluation_plan.json by id and records the deletion in
// planning/changelog, mirroring delete_plan_steps for the regular plan.
//
// Before this, evaluation_plan.json had update_evaluation_plan but no way to
// remove a step at all -- the only path was a direct file write, which is
// exactly what update_evaluation_plan's own doc comment (and PLAT-282) warns
// against: a direct write leaves no changelog entry, so artifact drift
// review cannot see or judge the change. Eval steps carry no next_step_id or
// route graph between each other (applies_to_routes references a ROUTING
// STEP IN THE MAIN PLAN, never another eval step), so unlike
// delete_plan_steps this needs no structural graph revalidation after
// filtering -- only that every requested id actually exists.
func DeleteEvaluationPlanSteps(
	ctx context.Context,
	workspacePath string,
	stepIDs []string,
	reason string,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	logger loggerv2.Logger,
) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("reason is required: the changelog records why an evaluation step was removed, and drift review cannot judge a deletion without it")
	}

	orderedIDs := make([]string, 0, len(stepIDs))
	deletedSet := make(map[string]bool, len(stepIDs))
	for _, id := range stepIDs {
		id = strings.TrimSpace(id)
		if id == "" || deletedSet[id] {
			continue
		}
		deletedSet[id] = true
		orderedIDs = append(orderedIDs, id)
	}
	if len(orderedIDs) == 0 {
		return "", fmt.Errorf("step_ids is required and must contain at least one non-empty evaluation step id")
	}

	path, document, stepsKey, rawSteps, err := loadEvaluationPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return "", err
	}

	existing := make(map[string]bool, len(rawSteps))
	for _, entry := range rawSteps {
		if step, isObject := entry.(map[string]interface{}); isObject {
			if id, _ := step["id"].(string); id != "" {
				existing[id] = true
			}
		}
	}
	missing := make([]string, 0)
	for _, id := range orderedIDs {
		if !existing[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		available := make([]string, 0, len(existing))
		for id := range existing {
			available = append(available, id)
		}
		sort.Strings(available)
		return "", fmt.Errorf("evaluation step id(s) not found: %s. Available step IDs: %v", strings.Join(missing, ", "), available)
	}

	// Capture the full JSON of every deleted step before filtering, same as
	// delete_plan_steps, so the changelog entry can support a manual revert.
	deletedSteps := make([]json.RawMessage, 0, len(orderedIDs))
	kept := make([]interface{}, 0, len(rawSteps))
	for _, entry := range rawSteps {
		step, isObject := entry.(map[string]interface{})
		if isObject {
			if id, _ := step["id"].(string); deletedSet[id] {
				if stepJSON, err := json.Marshal(step); err == nil {
					deletedSteps = append(deletedSteps, stepJSON)
				} else {
					logger.Warn(fmt.Sprintf("⚠️ Failed to marshal deleted evaluation step %s for changelog: %v", id, err))
				}
				continue
			}
		}
		kept = append(kept, entry)
	}

	document[stepsKey] = kept
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", evaluationPlanRelPath, err)
	}
	// Same write-time proof update_evaluation_plan applies: refuse to write a
	// file the runtime cannot read back as an evaluation plan.
	var check EvaluationPlan
	if err := json.Unmarshal(encoded, &check); err != nil {
		return "", fmt.Errorf("refusing to write: the edited plan no longer parses as an evaluation plan: %w", err)
	}
	if len(check.Steps) != len(kept) {
		return "", fmt.Errorf("refusing to write: the edited plan parses to %d steps, expected %d", len(check.Steps), len(kept))
	}
	if err := writeFile(ctx, path, string(encoded)); err != nil {
		return "", fmt.Errorf("write %s: %w", evaluationPlanRelPath, err)
	}

	logPlanChange(ctx, workspacePath, PlanChangelogEntry{
		Tool:         "delete_evaluation_step",
		Reason:       reason,
		StepIDs:      orderedIDs,
		DeletedSteps: deletedSteps,
	}, readFile, writeFile, logger)

	return fmt.Sprintf("Deleted %d evaluation step(s) (%s) and recorded it in planning/changelog.", len(orderedIDs), strings.Join(orderedIDs, ", ")), nil
}

// validateSchemaLikeUpdateField round-trips a raw decoded validation_schema
// or pre_validation value through the shared ValidationSchema struct so it
// gets the same write-time checks every other schema-writing tool applies:
// regex pattern validity, JSONPath syntax, array_length consistency, and
// value_type/pattern compatibility (PLAT-236).
func validateSchemaLikeUpdateField(field string, raw interface{}) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	var schema ValidationSchema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return fmt.Errorf("%s does not match the expected schema shape: %w", field, err)
	}
	if err := validateRegexPatternsInSchema(&schema); err != nil {
		return fmt.Errorf("%s has invalid regex patterns: %w", field, err)
	}
	if err := validateJSONPathSyntax(&schema); err != nil {
		return fmt.Errorf("%s has invalid JSONPath syntax: %w", field, err)
	}
	if err := validateArrayLengthConsistencyChecks(&schema); err != nil {
		return fmt.Errorf("%s has invalid array_length consistency checks: %w", field, err)
	}
	if err := validateValueTypePatternCompatibility(&schema); err != nil {
		return fmt.Errorf("%s has an invalid schema: %w", field, err)
	}
	return nil
}

// jsonEqual compares two decoded JSON values so an update that sets a field to
// what it already holds is not recorded as a change.
func jsonEqual(a, b interface{}) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(left) == string(right)
}

func getUpdateEvaluationPlanSchema() string {
	return `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "step_id": {"type": "string", "description": "ID of the evaluation step to update."},
    "reason": {"type": "string", "description": "Why this change is being made. Recorded in planning/changelog; drift review cannot judge a change without it."},
    "title": {"type": "string"},
    "description": {"type": "string"},
    "context_output": {"type": "string", "description": "Filename this step writes."},
    "context_dependencies": {"type": "array", "items": {"type": "string"}},
    "max_score": {"type": "integer", "description": "Score scale for this step. Steps without one cannot be compared run to run."},
    "db_write": {"type": "boolean"},
    "validation_schema": {"type": "object", "description": "Validation schema for this step's OWN OUTPUT, checked after it runs."},
    "pre_validation": {"type": "object", "description": "Files required to exist BEFORE this step runs, e.g. {\"files\":[{\"file_name\":\"...\",\"must_exist\":true}]}. Distinct from validation_schema, which checks this step's own output afterward. Gate route/producer alignment against this together with applies_to_routes so a step's evidence requirement never outlives the route that actually produces it."},
    "applies_to_routes": {
      "type": "array",
      "description": "Gate this step to specific routes chosen by a routing step. Omit to run it on every route.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "routing_step_id": {"type": "string"},
          "route_ids": {"type": "array", "items": {"type": "string"}}
        },
        "required": ["routing_step_id", "route_ids"]
      }
    }
  },
  "required": ["step_id", "reason"]
}`
}

func parseSchemaForToolParametersMust(schema string) map[string]interface{} {
	parsed, err := parseSchemaForToolParameters(schema)
	if err != nil {
		// The schema is a compile-time constant in this file; a parse failure is
		// a code defect, and an empty surface is clearer than a silent partial.
		return map[string]interface{}{"type": "object"}
	}
	return parsed
}

func createUpdateEvaluationPlanExecutor(
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, _ := args["step_id"].(string)
		reason, _ := args["reason"].(string)
		updates := map[string]interface{}{}
		for _, field := range evaluationPlanEditableFields {
			if value, ok := args[field]; ok && value != nil {
				updates[field] = value
			}
		}
		return UpdateEvaluationPlanStep(ctx, workspacePath, stepID, updates, reason, readFile, writeFile, logger)
	}
}

func getAddEvaluationPlanStepSchema() string {
	return `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "id": {"type": "string", "description": "Unique evaluation step ID; must not collide with an execution step."},
    "title": {"type": "string"},
    "description": {"type": "string", "description": "Complete evaluation rubric, including what passing and failing mean."},
    "reason": {"type": "string", "description": "Why this evaluation step is being added. Recorded in planning/changelog."},
    "context_output": {"type": "string"},
    "context_dependencies": {"type": "array", "items": {"type": "string"}},
    "max_score": {"type": "integer"},
    "db_write": {"type": "boolean"},
    "execution_mode": {"type": "string", "enum": ["agentic", "scripted"]},
    "validation_schema": {"type": "object"},
    "pre_validation": {"type": "object"},
    "applies_to_routes": {"type": "array", "items": {"type": "object"}}
  },
  "required": ["id", "title", "description", "reason"]
}`
}

func createAddEvaluationPlanStepExecutor(
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, _ := args["reason"].(string)
		step := make(map[string]interface{}, len(args)-1)
		for key, value := range args {
			if key != "reason" && value != nil {
				step[key] = value
			}
		}
		return AddEvaluationPlanStep(ctx, workspacePath, step, reason, readFile, writeFile, logger)
	}
}

func getDeleteEvaluationPlanStepsSchema() string {
	return `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "step_ids": {
      "type": "array",
      "description": "IDs of the evaluation step(s) to delete from evaluation/evaluation_plan.json.",
      "items": {"type": "string"},
      "minItems": 1
    },
    "reason": {"type": "string", "description": "Why these evaluation step(s) are being removed. Recorded in planning/changelog; drift review cannot judge a deletion without it."}
  },
  "required": ["step_ids", "reason"]
}`
}

func createDeleteEvaluationPlanStepsExecutor(
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepIDs, err := extractStringArray(args, "step_ids")
		if err != nil {
			return "", err
		}
		reason, _ := args["reason"].(string)
		return DeleteEvaluationPlanSteps(ctx, workspacePath, stepIDs, reason, readFile, writeFile, logger)
	}
}
