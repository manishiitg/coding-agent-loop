package server

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Crew functions (PLAT-357) declare their inputs and results with a small
// JSON Schema subset: object (properties, required), array (items), string,
// number, integer, boolean, and enum on any scalar. The subset is enough for
// tool parameters and keeps validation predictable for both sides.

var crewFunctionSchemaTypes = map[string]bool{
	"object": true, "array": true, "string": true, "number": true, "integer": true, "boolean": true,
}

// checkCrewFunctionSchema reports whether schema uses only the supported
// subset. An empty schema is allowed (no constraint).
func checkCrewFunctionSchema(schema map[string]interface{}, where string) error {
	if len(schema) == 0 {
		return nil
	}
	kind, _ := schema["type"].(string)
	if !crewFunctionSchemaTypes[kind] {
		return fmt.Errorf("%s: type must be one of object, array, string, number, integer, boolean", where)
	}
	if raw, ok := schema["enum"]; ok {
		values, ok := raw.([]interface{})
		if !ok || len(values) == 0 {
			return fmt.Errorf("%s: enum must be a non-empty array", where)
		}
	}
	switch kind {
	case "object":
		props := map[string]interface{}{}
		if raw, ok := schema["properties"]; ok {
			typed, ok := raw.(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s: properties must be an object", where)
			}
			props = typed
		}
		for name, raw := range props {
			child, ok := raw.(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s.%s: property schema must be an object", where, name)
			}
			if err := checkCrewFunctionSchema(child, where+"."+name); err != nil {
				return err
			}
		}
		if raw, ok := schema["required"]; ok {
			list, ok := raw.([]interface{})
			if !ok {
				return fmt.Errorf("%s: required must be an array of property names", where)
			}
			for _, item := range list {
				name, _ := item.(string)
				if _, declared := props[name]; !declared {
					return fmt.Errorf("%s: required property %q is not declared in properties", where, name)
				}
			}
		}
	case "array":
		if raw, ok := schema["items"]; ok {
			child, ok := raw.(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s: items must be a schema object", where)
			}
			return checkCrewFunctionSchema(child, where+"[]")
		}
	}
	return nil
}

// validateCrewFunctionValue checks value against schema and returns every
// problem found, so the side that produced it can fix all of them at once.
func validateCrewFunctionValue(schema map[string]interface{}, value interface{}) []string {
	var problems []string
	validateCrewFunctionValueAt(schema, value, "$", &problems)
	sort.Strings(problems)
	return problems
}

func validateCrewFunctionValueAt(schema map[string]interface{}, value interface{}, at string, problems *[]string) {
	if len(schema) == 0 {
		return
	}
	kind, _ := schema["type"].(string)
	if !crewFunctionValueHasType(kind, value) {
		*problems = append(*problems, fmt.Sprintf("%s: expected %s, got %s", at, kind, crewFunctionValueTypeName(value)))
		return
	}
	if raw, ok := schema["enum"].([]interface{}); ok && len(raw) > 0 {
		matched := false
		for _, allowed := range raw {
			if crewFunctionScalarEqual(allowed, value) {
				matched = true
				break
			}
		}
		if !matched {
			*problems = append(*problems, fmt.Sprintf("%s: %v is not one of %v", at, value, raw))
		}
	}
	switch kind {
	case "object":
		object := value.(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})
		if required, ok := schema["required"].([]interface{}); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, present := object[name]; !present || object[name] == nil {
					*problems = append(*problems, fmt.Sprintf("%s.%s: required", at, name))
				}
			}
		}
		for name, child := range object {
			childSchema, ok := props[name].(map[string]interface{})
			if !ok || child == nil {
				continue
			}
			validateCrewFunctionValueAt(childSchema, child, at+"."+name, problems)
		}
	case "array":
		items, _ := schema["items"].(map[string]interface{})
		for index, child := range value.([]interface{}) {
			validateCrewFunctionValueAt(items, child, fmt.Sprintf("%s[%d]", at, index), problems)
		}
	}
}

func crewFunctionValueHasType(kind string, value interface{}) bool {
	switch kind {
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := crewFunctionNumber(value)
		return ok
	case "integer":
		number, ok := crewFunctionNumber(value)
		return ok && number == math.Trunc(number)
	}
	return true
}

func crewFunctionNumber(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}

func crewFunctionScalarEqual(a, b interface{}) bool {
	if left, ok := crewFunctionNumber(a); ok {
		right, ok := crewFunctionNumber(b)
		return ok && left == right
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func crewFunctionValueTypeName(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	}
	if _, ok := crewFunctionNumber(value); ok {
		return "number"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", value), "*")
}
