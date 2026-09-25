package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

var externalAgentTools = map[string]bool{"list_agents": true, "ask": true, "call_function": true, "get_call": true}

func isExternalAgentTool(name string) bool { return externalAgentTools[name] }

func externalAgentWait(args map[string]any) time.Duration {
	seconds := 20
	if raw, ok := args["wait_seconds"].(float64); ok && raw >= 0 {
		seconds = min(int(raw), 25)
	}
	return time.Duration(seconds) * time.Second
}

type externalAgent struct {
	ID        string
	Name      string
	Kind      string
	About     string
	CanCall   bool
	Target    triggerTarget
	Functions []crewFunction
}

func externalAgentCaller(claims *UserClaims) triggerLinkCaller {
	caller := externalCrewCaller(claims)
	if claims.AccessToken != nil {
		caller.Stamp = triggerCaller{Type: triggerCallerConnection, ID: claims.AccessToken.ID}
	}
	return caller
}

func externalAgentNameKey(s string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func externalAgentFunctionAllowed(target triggerTarget, fn crewFunction, caller triggerCaller) bool {
	if target.Kind == triggerCallerCrew {
		return crewFunctionCallerAllowed(fn, caller)
	}
	if fn.Name == crewFunctionAskName {
		return true
	}
	for _, sched := range target.Manifest.Schedules {
		if sched.IsFunctionTrigger() && sched.Enabled && sched.Function.Name == fn.Name {
			return workflowFunctionCallerAllowed(sched.Function, caller)
		}
	}
	return false
}

func (api *StreamingAPI) visibleExternalAgents(ctx context.Context, claims *UserClaims) ([]externalAgent, error) {
	var agents []externalAgent
	caller := externalAgentCaller(claims).Stamp
	crewVisible := claims.AccessToken == nil || claims.AccessToken.Allows("crews:read") || claims.AccessToken.Allows("crews:run")
	if crewVisible {
		crews, err := api.externalCrewsVisible(ctx, claims, "")
		if err != nil {
			return nil, err
		}
		for _, summary := range crews {
			id := fmt.Sprint(summary["id"])
			crew, manifest, _, ok := api.externalCrewResolve(ctx, claims, id)
			if !ok {
				continue
			}
			name := firstNonEmptyTrimmed(fmt.Sprint(summary["name"]), id)
			target := triggerTarget{Kind: triggerCallerCrew, Path: crew.Binding.WorkspacePath, Label: name, CrewID: manifest.ID, CrewProfile: "work", CrewOwner: crew.OwnerID}
			functions, err := callableFunctions(ctx, target)
			if err != nil {
				functions = nil
			}
			canCall := claims.AccessToken != nil && claims.AccessToken.Allows("crews:run") || claims.AccessToken == nil && crew.OwnerID == claims.UserID
			allowed := make([]crewFunction, 0, len(functions))
			if canCall {
				for _, fn := range functions {
					if externalAgentFunctionAllowed(target, fn, caller) && fn.Name != crewFunctionAskName {
						allowed = append(allowed, fn)
					}
				}
			}
			agents = append(agents, externalAgent{ID: id, Name: name, Kind: triggerCallerCrew, About: manifest.Description, CanCall: canCall, Target: target, Functions: allowed})
		}
	}
	workflowVisible := claims.AccessToken == nil || claims.AccessToken.Allows("workflows:read") || claims.AccessToken.Allows("runs:execute")
	if workflowVisible {
		discovered, err := DiscoverWorkflowManifests(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range filterWorkflowManifestsForUser(claims, discovered) {
			if item.Manifest == nil || (claims.AccessToken != nil && !claims.AccessToken.AllowsWorkflow(item.Manifest.ID)) {
				continue
			}
			access := workflowAccessForManifest(claims, item.Manifest)
			canCall := (access == WorkflowAccessOwner || access == WorkflowAccessWrite) && (claims.AccessToken == nil || claims.AccessToken.Allows("runs:execute"))
			name := firstNonEmptyTrimmed(item.Manifest.Label, item.Manifest.ID)
			target := triggerTarget{Kind: triggerCallerWorkflow, Path: item.WorkspacePath, Label: name, Manifest: item.Manifest}
			var allowed []crewFunction
			if canCall {
				for _, fn := range workflowFunctions(item.Manifest) {
					if externalAgentFunctionAllowed(target, fn, caller) {
						allowed = append(allowed, fn)
					}
				}
			}
			agents = append(agents, externalAgent{ID: item.Manifest.ID, Name: name, Kind: triggerCallerWorkflow, CanCall: canCall, Target: target, Functions: allowed})
		}
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Name == agents[j].Name {
			return agents[i].ID < agents[j].ID
		}
		return strings.ToLower(agents[i].Name) < strings.ToLower(agents[j].Name)
	})
	return agents, nil
}

func externalFunctionSignature(fn crewFunction) string {
	props, _ := fn.InputSchema["properties"].(map[string]interface{})
	required := map[string]bool{}
	if raw, ok := fn.InputSchema["required"].([]interface{}); ok {
		for _, item := range raw {
			required[fmt.Sprint(item)] = true
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		prop, _ := props[name].(map[string]interface{})
		kind, _ := prop["type"].(string)
		if values, ok := prop["enum"].([]interface{}); ok && len(values) > 0 {
			choices := make([]string, 0, len(values))
			for _, value := range values {
				choices = append(choices, fmt.Sprint(value))
			}
			kind = strings.Join(choices, "|")
		}
		marker := ""
		if !required[name] || prop["default"] != nil {
			marker = "?"
		}
		defaultMark := ""
		if _, present := prop["default"]; present {
			defaultMark = "=default"
		}
		parts = append(parts, name+marker+": "+kind+defaultMark)
	}
	return fn.Name + "(" + strings.Join(parts, ", ") + ")"
}

func externalFunctionRepair(fn crewFunction, supplied map[string]interface{}, sched *WorkflowSchedule) (map[string]interface{}, []string) {
	props, _ := fn.InputSchema["properties"].(map[string]interface{})
	required := map[string]bool{}
	if raw, ok := fn.InputSchema["required"].([]interface{}); ok {
		for _, name := range raw {
			required[fmt.Sprint(name)] = true
		}
	}
	valid := map[string]interface{}{}
	missing := []string{}
	for name, rawSchema := range props {
		schema, ok := rawSchema.(map[string]interface{})
		if !ok {
			continue
		}
		value, suppliedHere := supplied[name]
		if !suppliedHere {
			value, ok = schema["default"]
			if !ok {
				if required[name] {
					missing = append(missing, name)
				}
				continue
			}
		}
		value = normalizeCrewFunctionInput(schema, value)
		if len(validateCrewFunctionValue(schema, value)) > 0 {
			continue
		}
		if sched != nil && name != "group" {
			usable := false
			for _, input := range sched.Function.Inputs {
				if input.Name == name {
					_, err := workflowFunctionInputValue(input, value)
					usable = err == nil
					break
				}
			}
			if !usable {
				continue
			}
		}
		valid[name] = value
	}
	sort.Strings(missing)
	return valid, missing
}

func (api *StreamingAPI) externalAgentCall(w http.ResponseWriter, r *http.Request, name string, args map[string]any) {
	claims := GetUserFromContext(r.Context())
	str := func(key string) string { value, _ := args[key].(string); return strings.TrimSpace(value) }
	if name == "get_call" {
		api.externalGetCall(w, r, str("call_id"), externalAgentWait(args))
		return
	}
	agents, err := api.visibleExternalAgents(r.Context(), claims)
	if err != nil {
		externalError(w, 502, "workspace_unavailable", "Could not discover agents.")
		return
	}
	if name == "list_agents" {
		kind, rawQuery := str("kind"), str("query")
		query := externalAgentNameKey(rawQuery)
		exactID := false
		for _, agent := range agents {
			if agent.ID == rawQuery && (kind == "" || kind == agent.Kind) {
				exactID = true
				break
			}
		}
		filtered := make([]externalAgent, 0, len(agents))
		for _, agent := range agents {
			if kind != "" && kind != agent.Kind || exactID && agent.ID != rawQuery || !exactID && query != "" && !strings.Contains(externalAgentNameKey(agent.Name+agent.ID), query) {
				continue
			}
			filtered = append(filtered, agent)
		}
		limit, offset := externalInt(args, "limit", 100), externalInt(args, "offset", 0)
		start := min(offset, len(filtered))
		end := min(start+limit, len(filtered))
		items := make([]map[string]any, 0, end-start)
		for _, agent := range filtered[start:end] {
			item := map[string]any{"id": agent.ID, "name": agent.Name, "kind": agent.Kind, "about": agent.About, "busy": externalAgentBusy(agent.Target)}
			if agent.CanCall {
				item["can"] = "call"
			} else {
				item["can"] = "read"
			}
			if len(agent.Functions) > 0 {
				signatures := make([]string, 0, len(agent.Functions))
				for _, fn := range agent.Functions {
					signatures = append(signatures, externalFunctionSignature(fn))
				}
				item["functions"] = signatures
			}
			items = append(items, item)
		}
		externalJSON(w, map[string]any{"agents": items, "total": len(filtered), "has_more": end < len(filtered)})
		return
	}
	rawTarget := str("target")
	var matches []externalAgent
	for _, agent := range agents {
		if agent.ID == rawTarget {
			matches = []externalAgent{agent}
			break
		}
		if externalAgentNameKey(agent.Name) == externalAgentNameKey(rawTarget) {
			matches = append(matches, agent)
		}
	}
	if len(matches) == 0 {
		externalJSON(w, map[string]any{"status": "refused", "code": "not_found", "reason": "Agent not found or unavailable to this connection."})
		return
	}
	if len(matches) > 1 {
		choices := make([]map[string]string, 0, len(matches))
		for _, match := range matches {
			choices = append(choices, map[string]string{"id": match.ID, "name": match.Name, "kind": match.Kind})
		}
		externalJSON(w, map[string]any{"status": "refused", "code": "ambiguous_target", "matches": choices})
		return
	}
	agent := matches[0]
	if !agent.CanCall {
		externalJSON(w, map[string]any{"status": "refused", "code": "forbidden", "reason": "This connection cannot run the agent."})
		return
	}
	caller := externalAgentCaller(claims)
	var fn crewFunction
	var callArgs map[string]interface{}
	if name == "ask" {
		if str("message") == "" {
			externalJSON(w, map[string]any{"status": "refused", "code": "invalid_inputs", "problems": []string{"message is required"}})
			return
		}
		if agent.Kind == triggerCallerWorkflow {
			fn = workflowAskFunction()
		} else {
			functions, readErr := callableFunctions(r.Context(), agent.Target)
			if readErr != nil {
				externalJSON(w, map[string]any{"status": "refused", "code": "target_unavailable", "reason": "Could not read the Crew functions."})
				return
			}
			var found bool
			fn, found = findCrewFunction(functions, crewFunctionAskName)
			if !found || !externalAgentFunctionAllowed(agent.Target, fn, caller.Stamp) {
				externalJSON(w, map[string]any{"status": "refused", "code": "forbidden", "reason": "This connection cannot ask the Crew."})
				return
			}
		}
		callArgs = map[string]interface{}{"message": str("message")}
	} else {
		function := str("function")
		found := false
		for _, offered := range agent.Functions {
			if offered.Name == function {
				fn, found = offered, true
				break
			}
		}
		if !found {
			signatures := make([]string, 0, len(agent.Functions))
			for _, offered := range agent.Functions {
				signatures = append(signatures, externalFunctionSignature(offered))
			}
			externalJSON(w, map[string]any{"status": "refused", "code": "unknown_function", "functions": signatures})
			return
		}
		callArgs, _ = args["args"].(map[string]interface{})
		if callArgs == nil {
			callArgs = map[string]interface{}{}
		}
		var problems []string
		var workflowSchedule *WorkflowSchedule
		if agent.Kind == triggerCallerWorkflow {
			for i := range agent.Target.Manifest.Schedules {
				sched := &agent.Target.Manifest.Schedules[i]
				if sched.IsFunctionTrigger() && sched.Function.Name == fn.Name {
					workflowSchedule = sched
					_, _, checkErr := workflowFunctionArgs(*sched, callArgs)
					if checkErr != nil {
						problems = strings.Split(strings.TrimPrefix(checkErr.Error(), fn.Name+": "), "; ")
					}
					break
				}
			}
		} else {
			_, problems = prepareCrewFunctionArgs(fn.InputSchema, callArgs)
		}
		if len(problems) > 0 {
			retryArgs, missing := externalFunctionRepair(fn, callArgs, workflowSchedule)
			externalJSON(w, map[string]any{"status": "refused", "code": "invalid_inputs", "problems": problems,
				"function":   map[string]any{"name": fn.Name, "input_schema": fn.InputSchema},
				"retry_with": map[string]any{"target": agent.ID, "function": fn.Name, "args": retryArgs}, "missing": missing})
			return
		}
	}
	call, err := api.startCrewFunctionCall(context.WithoutCancel(r.Context()), claims.UserID, caller, agent.Target, fn, callArgs, externalCrewCallTimeout)
	if err != nil {
		externalJSON(w, map[string]any{"status": "refused", "code": "call_refused", "reason": err.Error()})
		return
	}
	externalAgentCallWait(r.Context(), call, externalAgentWait(args))
	externalJSON(w, externalAgentCallSnapshot(call))
}

func externalAgentBusy(target triggerTarget) bool {
	crewFunctionCalls.Lock()
	calls := make([]*crewFunctionCall, 0, len(crewFunctionCalls.m))
	for _, call := range crewFunctionCalls.m {
		calls = append(calls, call)
	}
	crewFunctionCalls.Unlock()
	for _, call := range calls {
		call.mu.Lock()
		busy := call.TargetKind == target.Kind && call.TargetID == target.stampID() && !call.terminalLocked()
		call.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}

func externalAgentCallWait(ctx context.Context, call *crewFunctionCall, wait time.Duration) {
	if wait <= 0 {
		return
	}
	call.mu.Lock()
	updated := call.UpdatedAt
	done := call.done
	if call.acceptsLateLocked() {
		done = nil
	}
	call.mu.Unlock()
	timer := time.NewTimer(wait)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			call.mu.Lock()
			changed := call.UpdatedAt.After(updated)
			call.mu.Unlock()
			if changed {
				return
			}
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

func externalAgentCallSnapshot(call *crewFunctionCall) map[string]interface{} {
	out := call.snapshot()
	call.mu.Lock()
	out["target"] = map[string]interface{}{"id": call.TargetID, "kind": call.TargetKind, "name": call.TargetLabel}
	out["since"] = call.CreatedAt
	out["last_activity_at"] = call.UpdatedAt
	out["timed_out"] = call.TimedOut
	out["late"] = call.Late
	latePending := call.acceptsLateLocked()
	call.mu.Unlock()
	if progress, ok := out["progress"].([]crewFunctionProgress); ok && len(progress) > 5 {
		out["progress"] = progress[len(progress)-5:]
	}
	status, _ := out["status"].(string)
	switch status {
	case "running":
		out["status"] = "working"
	case "failed":
		if latePending {
			out["status"] = "working"
			out["timed_out"] = true
		}
		if message, _ := out["error"].(string); strings.HasPrefix(message, "interrupted:") {
			out["status"] = "interrupted"
		}
	}
	if _, exists := out["late"]; !exists {
		out["late"] = false
	}
	if _, exists := out["timed_out"]; !exists {
		out["timed_out"] = false
	}
	if answer, ok := out["result"].(map[string]interface{}); ok && call.FreeText {
		out["answer"] = answer["answer"]
	}
	if out["status"] == "working" || out["status"] == "queued" {
		out["next"] = "Poll get_call with this call_id for progress and the result."
	}
	return out
}

func (api *StreamingAPI) externalGetCall(w http.ResponseWriter, r *http.Request, id string, wait time.Duration) {
	claims := GetUserFromContext(r.Context())
	call := lookupCrewFunctionCall(id)
	if call == nil {
		externalError(w, 404, "not_found", "Call not found.")
		return
	}
	caller := externalAgentCaller(claims).Stamp
	call.mu.Lock()
	owned := call.UserID == claims.UserID && call.CallerKind == caller.Type && call.CallerID == caller.ID
	targetKind, targetID := call.TargetKind, call.TargetID
	call.mu.Unlock()
	if !owned || claims.AccessToken != nil && (targetKind == triggerCallerCrew && !claims.AccessToken.AllowsCrew(targetID) || targetKind == triggerCallerWorkflow && !claims.AccessToken.AllowsWorkflow(targetID)) {
		externalError(w, 404, "not_found", "Call not found.")
		return
	}
	visible, err := api.visibleExternalAgents(r.Context(), claims)
	if err != nil {
		externalError(w, 502, "workspace_unavailable", "Could not check call access.")
		return
	}
	found := false
	for _, agent := range visible {
		if agent.Kind == targetKind && agent.ID == targetID {
			found = true
			break
		}
	}
	if !found {
		externalError(w, 404, "not_found", "Call not found.")
		return
	}
	externalAgentCallWait(r.Context(), call, wait)
	externalJSON(w, externalAgentCallSnapshot(call))
}
