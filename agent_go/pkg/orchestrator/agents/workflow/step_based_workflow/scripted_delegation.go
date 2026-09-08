package step_based_workflow

import (
	"context"
	"strings"
)

// Scripted sub-agent delegation is carried in the child context and copied into
// that child's process environment. It must never be written into the shared
// workspace environment: multiple todo routes can execute concurrently.
const (
	ScriptedDelegationInstructionsEnv = "STEP_DELEGATION_INSTRUCTIONS"
	ScriptedDelegationRouteIDEnv      = "STEP_DELEGATION_ROUTE_ID"
	ScriptedDelegationTodoIDEnv       = "STEP_DELEGATION_TODO_ID"
)

type scriptedDelegationContextKey struct{}

type scriptedDelegationContext struct {
	Instructions string
	RouteID      string
	TodoID       string
	Parameters   map[string]interface{}
}

func withScriptedDelegationContext(ctx context.Context, routeID, todoID, instructions string, parameters map[string]interface{}) context.Context {
	parameterCopy := make(map[string]interface{}, len(parameters))
	for key, value := range parameters {
		parameterCopy[key] = value
	}
	delegation := scriptedDelegationContext{
		Instructions: strings.TrimSpace(instructions),
		RouteID:      strings.TrimSpace(routeID),
		TodoID:       strings.TrimSpace(todoID),
		Parameters:   parameterCopy,
	}
	return context.WithValue(ctx, scriptedDelegationContextKey{}, delegation)
}

func scriptedDelegationFromContext(ctx context.Context) (scriptedDelegationContext, bool) {
	if ctx == nil {
		return scriptedDelegationContext{}, false
	}
	delegation, ok := ctx.Value(scriptedDelegationContextKey{}).(scriptedDelegationContext)
	if !ok {
		return scriptedDelegationContext{}, false
	}
	return delegation, delegation.Instructions != "" || delegation.RouteID != "" || delegation.TodoID != "" || len(delegation.Parameters) > 0
}

// appendScriptedDelegationEnv returns a copy so execution-local delegation
// values can never mutate the orchestrator's shared workspace env map.
func appendScriptedDelegationEnv(ctx context.Context, env map[string]string) map[string]string {
	result := make(map[string]string, len(env)+3)
	for key, value := range env {
		result[key] = value
	}
	delegation, ok := scriptedDelegationFromContext(ctx)
	if !ok {
		return result
	}
	result[ScriptedDelegationInstructionsEnv] = delegation.Instructions
	result[ScriptedDelegationRouteIDEnv] = delegation.RouteID
	result[ScriptedDelegationTodoIDEnv] = delegation.TodoID
	return appendScriptParametersEnv(result, delegation.Parameters)
}
