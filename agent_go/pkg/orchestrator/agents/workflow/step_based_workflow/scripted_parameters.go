package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

const ScriptedParametersEnv = "STEP_PARAMS_JSON"

var scriptParameterNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func scriptedParameterDefinitions(step PlanStepInterface) map[string]ScriptParameterDefinition {
	if step == nil {
		return nil
	}
	return step.GetCommonFields().ScriptParameters
}

func validateScriptParameterDefinitions(definitions map[string]ScriptParameterDefinition) error {
	for name, definition := range definitions {
		if !scriptParameterNamePattern.MatchString(name) {
			return fmt.Errorf("script parameter %q must match %s", name, scriptParameterNamePattern.String())
		}
		if strings.TrimSpace(definition.Description) == "" {
			return fmt.Errorf("script parameter %q requires a description", name)
		}
		switch definition.Type {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			return fmt.Errorf("script parameter %q has unsupported type %q", name, definition.Type)
		}
		if definition.Default != nil {
			if err := validateScriptParameterValue(name, definition, definition.Default); err != nil {
				return fmt.Errorf("invalid default: %w", err)
			}
		}
		for _, allowed := range definition.Enum {
			if err := validateScriptParameterValue(name, definition, allowed); err != nil {
				return fmt.Errorf("invalid enum value: %w", err)
			}
		}
	}
	return nil
}

func validateAndResolveScriptParameters(step PlanStepInterface, supplied map[string]interface{}) (map[string]interface{}, error) {
	definitions := scriptedParameterDefinitions(step)
	if err := validateScriptParameterDefinitions(definitions); err != nil {
		return nil, err
	}
	if len(definitions) == 0 {
		if len(supplied) > 0 {
			return nil, fmt.Errorf("scripted route %q does not declare script_parameters; remove parameters or update the route contract", step.GetID())
		}
		return nil, nil
	}
	for name := range supplied {
		if _, ok := definitions[name]; !ok {
			return nil, fmt.Errorf("unknown parameter %q for scripted route %q; accepted parameters: %s", name, step.GetID(), strings.Join(sortedScriptParameterNames(definitions), ", "))
		}
	}
	resolved := make(map[string]interface{}, len(definitions))
	for name, definition := range definitions {
		value, present := supplied[name]
		if !present && definition.Default != nil {
			value, present = definition.Default, true
		}
		if !present {
			if definition.Required {
				return nil, fmt.Errorf("missing required parameter %q for scripted route %q", name, step.GetID())
			}
			continue
		}
		if err := validateScriptParameterValue(name, definition, value); err != nil {
			return nil, err
		}
		resolved[name] = value
	}
	return resolved, nil
}

// applyWorkshopScriptParameters gives direct execute_step calls the same typed
// input contract as orchestrator-dispatched scripted routes. Values stay in the
// execution context, so concurrent calls cannot overwrite shared workspace env.
func applyWorkshopScriptParameters(ctx context.Context, step PlanStepInterface, opts *WorkshopExecuteOptions) (context.Context, error) {
	provided := opts != nil && opts.ScriptParametersSet
	if !isScriptedStep(step, getAgentConfigs(step)) {
		if provided {
			return ctx, fmt.Errorf("script_parameters is only supported for scripted steps")
		}
		return ctx, nil
	}

	var supplied map[string]interface{}
	if opts != nil {
		supplied = opts.ScriptParameters
	}
	resolved, err := validateAndResolveScriptParameters(step, supplied)
	if err != nil {
		return ctx, err
	}
	if len(resolved) == 0 {
		return ctx, nil
	}
	return withScriptedDelegationContext(ctx, "", "", "", resolved), nil
}

func validateScriptParameterValue(name string, definition ScriptParameterDefinition, value interface{}) error {
	valid := false
	switch definition.Type {
	case "string":
		_, valid = value.(string)
	case "number":
		if reflect.TypeOf(value) != nil {
			switch reflect.TypeOf(value).Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
				reflect.Float32, reflect.Float64:
				valid = true
			}
		}
	case "integer":
		switch typed := value.(type) {
		case float64:
			valid = math.Trunc(typed) == typed
		default:
			if reflect.TypeOf(value) != nil {
				switch reflect.TypeOf(value).Kind() {
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					valid = true
				}
			}
		}
	case "boolean":
		_, valid = value.(bool)
	case "array":
		kind := reflect.Invalid
		if reflect.TypeOf(value) != nil {
			kind = reflect.TypeOf(value).Kind()
		}
		valid = kind == reflect.Array || kind == reflect.Slice
	case "object":
		kind := reflect.Invalid
		if reflect.TypeOf(value) != nil {
			kind = reflect.TypeOf(value).Kind()
		}
		valid = kind == reflect.Map || kind == reflect.Struct
	}
	if !valid {
		return fmt.Errorf("parameter %q must be %s (got %T)", name, definition.Type, value)
	}
	if len(definition.Enum) > 0 {
		matched := false
		for _, allowed := range definition.Enum {
			if reflect.DeepEqual(value, allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("parameter %q must be one of %v", name, definition.Enum)
		}
	}
	return nil
}

func sortedScriptParameterNames(definitions map[string]ScriptParameterDefinition) []string {
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func formatScriptParameterContract(definitions map[string]ScriptParameterDefinition) string {
	if len(definitions) == 0 {
		return ""
	}
	payload, _ := json.MarshalIndent(definitions, "", "  ")
	return string(payload)
}

func appendScriptParametersEnv(env map[string]string, parameters map[string]interface{}) map[string]string {
	result := make(map[string]string, len(env)+1)
	for key, value := range env {
		result[key] = value
	}
	if len(parameters) == 0 {
		return result
	}
	payload, _ := json.Marshal(parameters)
	result[ScriptedParametersEnv] = string(payload)
	return result
}
