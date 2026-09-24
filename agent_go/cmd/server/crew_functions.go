package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	storeEvents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	todo_creation_human "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// Crew functions (PLAT-357): a Crew or workflow declares typed functions in
// <target>/functions.json; other Crews and workflows call them with
// call_function (or a generated per-function tool). Arguments and results
// are validated against the declared schemas. A call rides the existing
// internal-trigger binding (see trigger_link_tools.go), so it gets the same
// durable run, turn queue, and auto-notification path as any internal trigger.

const (
	crewFunctionsFileName          = "functions.json"
	crewFunctionMaxDepth           = 4
	crewFunctionChainBudget        = 20
	crewFunctionProgressKeep       = 10
	crewFunctionMaxInvalidResults  = 2
	crewFunctionEvent              = "agentworks.function_call"
	crewFunctionToolCategory       = "crew_function_tools"
	crewFunctionActivityTextLimit  = 600
	crewFunctionActivityEventsScan = 80
)

// crewFunctionFastWait is how long call_function waits for a result before
// returning {status:"running"} and notifying later. A var for tests.
var crewFunctionFastWait = 120 * time.Second

var crewFunctionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

type crewFunction struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]interface{} `json:"input_schema,omitempty"`
	ResultSchema map[string]interface{} `json:"result_schema,omitempty"`
	Instructions string                 `json:"instructions,omitempty"`
	CreatedBy    string                 `json:"created_by,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type crewFunctionsDoc struct {
	Version   int            `json:"version"`
	Functions []crewFunction `json:"functions"`
}

// crewFunctionRoot is the physical folder holding a target's functions. A
// Crew the requester owns may be addressed by its logical Chats/... path;
// store under the owner's physical tree so every caller sees one file.
func crewFunctionRoot(ctx context.Context, target triggerTarget) string {
	root := strings.TrimSuffix(strings.TrimSpace(target.Path), "/")
	if target.Kind == triggerCallerCrew {
		return agentProfileRuntimeWorkspace(target.ownerOr(GetUserIDFromContext(ctx)), root)
	}
	return root
}

func crewFunctionsPath(ctx context.Context, target triggerTarget) string {
	return crewFunctionRoot(ctx, target) + "/" + crewFunctionsFileName
}

func readCrewFunctions(ctx context.Context, target triggerTarget) ([]crewFunction, error) {
	raw, exists, err := readFileFromWorkspace(ctx, crewFunctionsPath(ctx, target))
	if err != nil {
		return nil, fmt.Errorf("read functions of %s %q: %w", target.Kind, target.Label, err)
	}
	if !exists || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var doc crewFunctionsDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("functions.json of %s %q is not valid JSON: %w", target.Kind, target.Label, err)
	}
	return doc.Functions, nil
}

func writeCrewFunctions(ctx context.Context, target triggerTarget, functions []crewFunction) error {
	sort.Slice(functions, func(i, j int) bool { return functions[i].Name < functions[j].Name })
	encoded, err := json.MarshalIndent(crewFunctionsDoc{Version: 1, Functions: functions}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileToWorkspace(ctx, crewFunctionsPath(ctx, target), string(encoded)+"\n")
}

// crewFunctionAskName is the implicit function every Crew and workflow
// offers: a free-text question answered by the target's final reply, over
// the standard inbound trigger. A declared function of the same name wins.
const crewFunctionAskName = "ask"

func defaultAskCrewFunction() crewFunction {
	return crewFunction{
		Name:        crewFunctionAskName,
		Description: "Ask this Crew or workflow anything in free text; the answer is its final reply. Available on every Crew and workflow without setup.",
		InputSchema: map[string]interface{}{"type": "object", "required": []interface{}{"message"}, "properties": map[string]interface{}{
			"message": map[string]interface{}{"type": "string", "description": "Self-contained question or task; the target does not see this conversation."},
		}},
		ResultSchema: map[string]interface{}{"type": "object", "required": []interface{}{"answer"}, "properties": map[string]interface{}{
			"answer": map[string]interface{}{"type": "string"},
		}},
		Instructions: "Answer the caller's message. Your final reply is returned to the caller as the answer.",
		CreatedBy:    "platform (default)",
	}
}

// withDefaultAskFunction returns the declared functions plus the implicit
// ask, unless the target declared its own ask.
func withDefaultAskFunction(functions []crewFunction) []crewFunction {
	if _, declared := findCrewFunction(functions, crewFunctionAskName); declared {
		return functions
	}
	return append(append([]crewFunction(nil), functions...), defaultAskCrewFunction())
}

func findCrewFunction(functions []crewFunction, name string) (crewFunction, bool) {
	for _, fn := range functions {
		if fn.Name == strings.TrimSpace(name) {
			return fn, true
		}
	}
	return crewFunction{}, false
}

func crewFunctionNames(functions []crewFunction) []string {
	names := make([]string, 0, len(functions))
	for _, fn := range functions {
		names = append(names, fn.Name)
	}
	return names
}

// crewFunctionToolName is the generated tool name for one function, e.g.
// "rts_flow_tester__run_login_flow" (tool-name charset, at most 64 chars).
func crewFunctionToolName(targetLabel, function string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(targetLabel) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	prefix := strings.Trim(b.String(), "_")
	if prefix == "" {
		prefix = "target"
	}
	name := prefix + "__" + function
	if len(name) > 64 {
		keep := 64 - len(function) - 2
		if keep < 1 {
			return name[:64]
		}
		name = strings.TrimRight(prefix[:keep], "_") + "__" + function
	}
	return name
}

// targetStampID is the ID a trigger caller stamp carries for this target.
func (t triggerTarget) stampID() string {
	if t.Kind == triggerCallerCrew {
		return strings.TrimSpace(t.CrewID)
	}
	if t.Manifest != nil {
		return strings.TrimSpace(t.Manifest.ID)
	}
	return ""
}

func crewFunctionKey(kind, id string) string {
	return strings.ToLower(strings.TrimSpace(kind)) + ":" + strings.TrimSpace(id)
}

// selfTriggerTarget addresses the calling Crew or workflow itself, for
// managing its own functions.
func selfTriggerTarget(caller triggerLinkCaller) triggerTarget {
	target := triggerTarget{Kind: caller.Stamp.Type, Path: caller.Path, Label: caller.Label}
	if target.Kind == triggerCallerCrew {
		target.CrewID = caller.Stamp.ID
		target.CrewProfile = "work"
	} else {
		target.Manifest = &WorkflowManifest{ID: caller.Stamp.ID}
	}
	return target
}

// --- call records ---

type crewFunctionProgress struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	Percent *float64  `json:"percent,omitempty"`
}

type crewFunctionCall struct {
	mu sync.Mutex

	ID             string                 `json:"call_id"`
	Function       string                 `json:"function"`
	UserID         string                 `json:"-"`
	CallerKind     string                 `json:"caller_kind"`
	CallerID       string                 `json:"caller_id"`
	CallerLabel    string                 `json:"caller_label"`
	TargetKind     string                 `json:"target_kind"`
	TargetID       string                 `json:"target_id"`
	TargetLabel    string                 `json:"target_label"`
	TargetPath     string                 `json:"target_path"`
	Chain          []string               `json:"chain"`
	Root           string                 `json:"root"`
	TriggerID      string                 `json:"trigger_id"`
	RunID          string                 `json:"run_id"`
	RunIDs         []string               `json:"run_ids"`
	Status         string                 `json:"status"`
	Result         interface{}            `json:"result,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Progress       []crewFunctionProgress `json:"progress,omitempty"`
	InvalidResults int                    `json:"invalid_results,omitempty"`
	// PartialResult / FinalReply keep what the target actually produced when
	// the call fails on the result contract, so the caller never loses real
	// work (e.g. test outcomes and video links) to a formatting mistake.
	PartialResult interface{} `json:"partial_result,omitempty"`
	FinalReply    string      `json:"final_reply,omitempty"`
	Retried       bool        `json:"retried,omitempty"`
	// FreeText marks the implicit ask: the result is the target's final
	// reply ({"answer": ...}) and return_function_result is optional.
	FreeText     bool                   `json:"free_text,omitempty"`
	ResultSchema map[string]interface{} `json:"result_schema,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`

	target triggerTarget
	caller triggerLinkCaller
	done   chan struct{}
	closed bool
	// poll is captured at start so a supervisor never reads the package
	// interval after it changes.
	poll time.Duration
}

var crewFunctionCalls = struct {
	sync.Mutex
	m map[string]*crewFunctionCall
}{m: map[string]*crewFunctionCall{}}

func lookupCrewFunctionCall(id string) *crewFunctionCall {
	crewFunctionCalls.Lock()
	defer crewFunctionCalls.Unlock()
	return crewFunctionCalls.m[strings.TrimSpace(id)]
}

func (c *crewFunctionCall) terminalLocked() bool {
	return c.Status == "completed" || c.Status == "failed"
}

// finish records the outcome once and releases every waiter.
func (c *crewFunctionCall) finish(status string, result interface{}, errText string) bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	c.Status, c.Result, c.Error, c.UpdatedAt = status, result, errText, time.Now().UTC()
	c.closed = true
	close(c.done)
	c.mu.Unlock()
	c.persist()
	return true
}

func (c *crewFunctionCall) snapshot() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]interface{}{
		"call_id": c.ID, "function": c.Function, "status": c.Status,
		"target":   map[string]interface{}{"kind": c.TargetKind, "name": c.TargetLabel, "workspace_path": c.TargetPath},
		"run_id":   c.RunID,
		"progress": append([]crewFunctionProgress(nil), c.Progress...),
	}
	if c.Result != nil {
		out["result"] = c.Result
	}
	if c.Error != "" {
		out["error"] = c.Error
	}
	if c.PartialResult != nil {
		out["partial_result"] = c.PartialResult
	}
	if c.FinalReply != "" {
		out["final_reply"] = c.FinalReply
	}
	return out
}

// crewFunctionFreeTextAnswer normalises an ask result to {"answer": text}:
// a string answer passes through; any other answer (or a bare value) is kept
// verbatim as indented JSON text.
func crewFunctionFreeTextAnswer(result interface{}) interface{} {
	asText := func(value interface{}) string {
		if text, ok := value.(string); ok {
			return text
		}
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(encoded)
	}
	if object, ok := result.(map[string]interface{}); ok {
		if answer, has := object["answer"]; has && len(object) == 1 {
			return map[string]interface{}{"answer": asText(answer)}
		}
	}
	return map[string]interface{}{"answer": asText(result)}
}

// crewFunctionFailureDetail renders what a failed call still produced, for
// the caller's auto-notification.
func crewFunctionFailureDetail(snapshot map[string]interface{}) string {
	var detail strings.Builder
	if partial, ok := snapshot["partial_result"]; ok && partial != nil {
		encoded, _ := json.MarshalIndent(partial, "", "  ")
		detail.WriteString("\n\nThe target's last (non-conforming) result — its work is not lost:\n" + truncateTriggerTargetResult(string(encoded)))
	}
	if reply, _ := snapshot["final_reply"].(string); strings.TrimSpace(reply) != "" {
		detail.WriteString("\n\nThe target's final reply:\n" + truncateTriggerTargetResult(reply))
	}
	return detail.String()
}

// persist keeps a JSON copy next to the target's functions, so
// get_function_call can still answer after a server restart.
func (c *crewFunctionCall) persist() {
	c.mu.Lock()
	encoded, err := json.MarshalIndent(c, "", "  ")
	path := strings.TrimSuffix(c.TargetPath, "/") + "/functions/calls/" + c.ID + ".json"
	userID := c.UserID
	c.mu.Unlock()
	if err != nil {
		return
	}
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: userID})
	if err := writeFileToWorkspace(ctx, path, string(encoded)+"\n"); err != nil {
		log.Printf("[CREW_FUNCTION] could not save call %s: %v", c.ID, err)
	}
}

// crewFunctionChainFor returns the call chain the caller is currently part
// of: the longest chain among in-flight calls whose target is the caller.
func crewFunctionChainFor(callerKey string) (chain []string, root string) {
	crewFunctionCalls.Lock()
	defer crewFunctionCalls.Unlock()
	for _, call := range crewFunctionCalls.m {
		call.mu.Lock()
		inFlight := !call.terminalLocked()
		matches := crewFunctionKey(call.TargetKind, call.TargetID) == callerKey
		if inFlight && matches && len(call.Chain) > len(chain) {
			chain, root = append([]string(nil), call.Chain...), call.Root
		}
		call.mu.Unlock()
	}
	return chain, root
}

func crewFunctionChainCalls(root string) int {
	crewFunctionCalls.Lock()
	defer crewFunctionCalls.Unlock()
	count := 0
	for _, call := range crewFunctionCalls.m {
		if call.Root == root {
			count++
		}
	}
	return count
}

// --- dispatch ---

// dispatchTargetTrigger fires one internal trigger delivery on target.
func (api *StreamingAPI) dispatchTargetTrigger(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget, triggerID, deliveryID, event string, body map[string]interface{}) (internalTriggerDeliveryResult, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return internalTriggerDeliveryResult{}, fmt.Errorf("invalid payload")
	}
	var delivery internalTriggerDeliveryResult
	switch target.Kind {
	case triggerCallerCrew:
		delivery, err = api.productSchedules.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
			UserID: userID, ProfileID: target.CrewProfile, ProjectID: target.CrewID, TriggerID: triggerID,
			Caller: caller.Stamp, DeliveryID: deliveryID, Event: event, Payload: data, CallerLabel: caller.Label,
		})
	case triggerCallerWorkflow:
		delivery, err = api.scheduler.dispatchInternalWorkflowTrigger(ctx, internalWorkflowTriggerCall{
			WorkflowID: target.Manifest.ID, TriggerID: triggerID, Caller: caller.Stamp,
			DeliveryID: deliveryID, Event: event, Payload: data,
		})
	default:
		return internalTriggerDeliveryResult{}, fmt.Errorf("unknown target kind %q", target.Kind)
	}
	if err != nil {
		return internalTriggerDeliveryResult{}, crewWorkflowRunError(err)
	}
	return delivery, nil
}

func crewFunctionTaskText(call *crewFunctionCall, fn crewFunction, args map[string]interface{}) string {
	if call.FreeText {
		message, _ := args["message"].(string)
		return fmt.Sprintf(`[Function call %[1]s] The %[2]s %[3]q asks:

%[4]s

Reply with your answer: your final reply in this turn is returned to the caller as the answer. For longer work, report milestones with report_function_progress(call_id=%[1]q, message=...). If you notice the same kind of ask arriving repeatedly, suggest to the user that it be exposed as a typed function (define_function).`,
			call.ID, call.CallerKind, call.CallerLabel, strings.TrimSpace(message))
	}
	encodedArgs, _ := json.MarshalIndent(args, "", "  ")
	resultShape := "any JSON value"
	if len(fn.ResultSchema) > 0 {
		encoded, _ := json.MarshalIndent(fn.ResultSchema, "", "  ")
		resultShape = "JSON matching this schema:\n" + string(encoded)
	}
	instructions := strings.TrimSpace(fn.Instructions)
	if instructions == "" {
		instructions = strings.TrimSpace(fn.Description)
	}
	return fmt.Sprintf(`[Function call %[1]s] The %[2]s %[3]q called your function %[4]q.

What to do:
%[5]s

Arguments (validated against the function's input schema):
%[6]s

Report progress at meaningful milestones with report_function_progress(call_id=%[1]q, message=..., percent=...), so the caller can follow along without interrupting you.
When you are done you MUST call return_function_result(call_id=%[1]q, result=<%[7]s>). If you cannot do it, call return_function_result(call_id=%[1]q, error="<why>"). The caller receives exactly that result, not your chat reply.`,
		call.ID, call.CallerKind, call.CallerLabel, fn.Name, instructions, string(encodedArgs), resultShape)
}

// startCrewFunctionCall validates, records and dispatches one call and
// starts its supervisor.
func (api *StreamingAPI) startCrewFunctionCall(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget, fn crewFunction, args map[string]interface{}, timeout time.Duration) (*crewFunctionCall, error) {
	if caller.isTarget(target) {
		return nil, fmt.Errorf("a %s cannot call its own function", target.Kind)
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if problems := validateCrewFunctionValue(fn.InputSchema, args); len(problems) > 0 {
		return nil, fmt.Errorf("arguments do not match %s's input schema: %s", fn.Name, strings.Join(problems, "; "))
	}
	callerKey := crewFunctionKey(caller.Stamp.Type, caller.Stamp.ID)
	targetKey := crewFunctionKey(target.Kind, target.stampID())
	chain, root := crewFunctionChainFor(callerKey)
	if len(chain) == 0 {
		chain = []string{callerKey}
	} else if chain[len(chain)-1] != callerKey {
		chain = append(chain, callerKey)
	}
	for _, key := range chain {
		if key == targetKey {
			return nil, fmt.Errorf("refused: %s %q is already in this call chain (%s); calling it again would loop", target.Kind, target.Label, strings.Join(chain, " -> "))
		}
	}
	if len(chain) >= crewFunctionMaxDepth {
		return nil, fmt.Errorf("refused: call depth limit %d reached (%s)", crewFunctionMaxDepth, strings.Join(chain, " -> "))
	}
	id := "fn-" + uuid.NewString()
	if root == "" {
		root = id
	}
	if crewFunctionChainCalls(root) >= crewFunctionChainBudget {
		return nil, fmt.Errorf("refused: this call chain already made %d function calls", crewFunctionChainBudget)
	}
	triggerID, _, err := api.connectTriggerTarget(ctx, userID, caller, target)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	call := &crewFunctionCall{
		ID: id, Function: fn.Name, UserID: userID,
		CallerKind: caller.Stamp.Type, CallerID: caller.Stamp.ID, CallerLabel: caller.Label,
		TargetKind: target.Kind, TargetID: target.stampID(), TargetLabel: target.Label, TargetPath: crewFunctionRoot(ctx, target),
		Chain: append(chain, targetKey), Root: root, TriggerID: triggerID, Status: "queued",
		ResultSchema: fn.ResultSchema, CreatedAt: now, UpdatedAt: now,
		FreeText: fn.Name == crewFunctionAskName && fn.CreatedBy == defaultAskCrewFunction().CreatedBy,
		target:   target, caller: caller, done: make(chan struct{}), poll: triggerTargetPollInterval,
	}
	body := map[string]interface{}{
		"task":    crewFunctionTaskText(call, fn, args),
		"from":    map[string]interface{}{"kind": caller.Stamp.Type, "name": caller.Label, "workspace_path": caller.Path},
		"payload": map[string]interface{}{"function": fn.Name, "call_id": id, "args": args},
	}
	crewFunctionCalls.Lock()
	crewFunctionCalls.m[id] = call
	crewFunctionCalls.Unlock()
	delivery, err := api.dispatchTargetTrigger(ctx, userID, caller, target, triggerID, id, crewFunctionEvent, body)
	if err != nil {
		crewFunctionCalls.Lock()
		delete(crewFunctionCalls.m, id)
		crewFunctionCalls.Unlock()
		return nil, err
	}
	call.mu.Lock()
	call.RunID, call.RunIDs = delivery.RunID, []string{delivery.RunID}
	call.mu.Unlock()
	call.persist()
	go api.superviseCrewFunctionCall(call, timeout)
	return call, nil
}

// superviseCrewFunctionCall watches the call's trigger run. A workflow run's
// outcome is the result. A Crew run must deliver its result through
// return_function_result; one that ends without it gets one retry turn,
// then the call fails. Timeouts fail the call.
func (api *StreamingAPI) superviseCrewFunctionCall(call *crewFunctionCall, timeout time.Duration) {
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: call.UserID})
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(call.poll)
	defer ticker.Stop()
	for {
		select {
		case <-call.done:
			return
		default:
		}
		call.mu.Lock()
		runID := call.RunID
		call.mu.Unlock()
		state, err := api.readTriggerTargetRun(ctx, call.UserID, call.caller, call.target, call.TriggerID, runID)
		if err == nil {
			call.mu.Lock()
			if !call.terminalLocked() && strings.EqualFold(state.Status, "running") {
				call.Status = "running"
			}
			call.mu.Unlock()
			if state.Terminal {
				if api.settleCrewFunctionRun(ctx, call, state) {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			call.finish("failed", nil, fmt.Sprintf("timed out after %s waiting for %s %q", timeout, call.TargetKind, call.TargetLabel))
			return
		}
		select {
		case <-call.done:
			return
		case <-ticker.C:
		}
	}
}

// settleCrewFunctionRun handles a terminal trigger run. It returns true when
// the call is settled (or already was).
func (api *StreamingAPI) settleCrewFunctionRun(ctx context.Context, call *crewFunctionCall, state triggerTargetRunState) bool {
	select {
	case <-call.done:
		return true
	default:
	}
	if call.TargetKind == triggerCallerWorkflow {
		if state.Failed {
			return call.finish("failed", nil, truncateTriggerTargetResult(state.Result))
		}
		if call.FreeText {
			return call.finish("completed", map[string]interface{}{"answer": truncateTriggerTargetResult(state.Result)}, "")
		}
		var decoded interface{}
		if json.Unmarshal([]byte(state.Result), &decoded) != nil {
			decoded = state.Result
		}
		return call.finish("completed", decoded, "")
	}
	// Give a result delivered at the very end of the turn a moment to land.
	select {
	case <-call.done:
		return true
	case <-time.After(minDuration(call.poll, 2*time.Second)):
	}
	if state.Failed {
		return call.finish("failed", nil, fmt.Sprintf("%s %q run ended %s: %s", call.TargetKind, call.TargetLabel, state.Status, truncateTriggerTargetResult(state.Result)))
	}
	if call.FreeText {
		answer := strings.TrimSpace(state.Result)
		if answer == "" {
			return call.finish("failed", nil, "the target finished without a reply")
		}
		return call.finish("completed", map[string]interface{}{"answer": truncateTriggerTargetResult(answer)}, "")
	}
	call.mu.Lock()
	retried := call.Retried
	call.Retried = true
	call.mu.Unlock()
	if retried {
		reason := "the target finished without returning a valid result"
		if final := strings.TrimSpace(state.Result); final != "" {
			call.mu.Lock()
			call.FinalReply = final
			call.mu.Unlock()
			reason += "; its final reply is included"
		}
		return call.finish("failed", nil, reason)
	}
	retry := map[string]interface{}{
		"task": fmt.Sprintf("[Function call %[1]s — result missing] You finished the %[2]q call from %[3]q without calling return_function_result. Call return_function_result(call_id=%[1]q, result=...) now with the result of the work you already did (or error=\"...\" if it failed). Do not redo the work.", call.ID, call.Function, call.CallerLabel),
		"from": map[string]interface{}{"kind": call.CallerKind, "name": call.CallerLabel},
		"payload": map[string]interface{}{
			"function": call.Function, "call_id": call.ID, "retry": true,
		},
	}
	delivery, err := api.dispatchTargetTrigger(ctx, call.UserID, call.caller, call.target, call.TriggerID, call.ID+"-retry", crewFunctionEvent, retry)
	if err != nil {
		return call.finish("failed", nil, "the target finished without a result and the retry could not be sent: "+err.Error())
	}
	call.mu.Lock()
	call.RunID = delivery.RunID
	call.RunIDs = append(call.RunIDs, delivery.RunID)
	call.mu.Unlock()
	call.persist()
	return false
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// startCrewFunctionWatch resumes the caller's chat with the call's outcome
// through the auto-notification pipeline.
func (api *StreamingAPI) startCrewFunctionWatch(parentReq QueryRequest, sessionID, userID string, call *crewFunctionCall, timeout time.Duration) (string, error) {
	if api == nil || api.bgAgentRegistry == nil {
		return "", fmt.Errorf("auto-notification is unavailable in this chat")
	}
	name := "Function " + call.TargetLabel + "." + call.Function
	executionID := "function-call-" + api.bgAgentRegistry.NextID(name)
	runCtx, cancel := context.WithTimeout(context.Background(), timeout+time.Minute)
	parentExecutionID := api.currentConversationTurnExecutionID(sessionID)
	if strings.TrimSpace(parentExecutionID) == "" {
		parentExecutionID = "session:" + sessionID
	}
	notifier := &workshopExecutionBgNotifier{
		api: api, sessionID: sessionID, workspacePath: parentReq.SelectedFolder,
		presetQueryID: parentReq.PresetQueryID, userID: userID,
	}
	notifier.OnExecutionStart(todo_creation_human.WorkshopExecutionStart{
		ID: executionID, ParentExecutionID: parentExecutionID, Name: name,
		Kind: "trigger_auto_notify", Cancel: cancel,
		Metadata: map[string]string{
			"execution_type": "function-call-auto-notify",
			"call_id":        call.ID,
			"target_kind":    call.TargetKind,
			"target_path":    call.TargetPath,
		},
	})
	registered := api.bgAgentRegistry.Get(sessionID, executionID)
	if registered == nil || registered.GetStatus() == BGAgentCanceled {
		cancel()
		return "", fmt.Errorf("auto-notification could not be registered")
	}
	go func() {
		defer cancel()
		select {
		case <-call.done:
		case <-runCtx.Done():
			notifier.OnExecutionComplete(executionID, name, "", nil, fmt.Errorf("function call %s did not settle; check it with get_function_call", call.ID))
			return
		}
		snapshot := call.snapshot()
		header := fmt.Sprintf("Function call %s (%s %q, %s)", call.ID, call.TargetKind, call.TargetLabel, call.Function)
		if snapshot["status"] == "failed" {
			errText, _ := snapshot["error"].(string)
			notifier.OnExecutionComplete(executionID, name, "", nil, fmt.Errorf("%s failed: %s%s", header, errText, crewFunctionFailureDetail(snapshot)))
			return
		}
		encoded, _ := json.MarshalIndent(snapshot["result"], "", "  ")
		notifier.OnExecutionComplete(executionID, name, header+" completed.\n\nResult:\n"+truncateTriggerTargetResult(string(encoded)), nil, nil)
	}()
	return executionID, nil
}

// --- activity tail ---

// crewFunctionActivity summarises what a target session is doing right now
// from its recent events: its latest assistant text and any tool it started
// after that text. It never interrupts the session.
func (api *StreamingAPI) crewFunctionActivity(sessionID string) map[string]interface{} {
	if api == nil || api.eventStore == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	result := api.eventStore.GetEvents(sessionID, storeGetEventsAll())
	events := result.Events
	if len(events) > crewFunctionActivityEventsScan {
		events = events[len(events)-crewFunctionActivityEventsScan:]
	}
	activity := map[string]interface{}{}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Data == nil {
			continue
		}
		fields := map[string]interface{}{}
		if encoded, err := json.Marshal(event.Data.Data); err == nil {
			_ = json.Unmarshal(encoded, &fields)
		}
		kind := strings.ToLower(string(event.Data.Type))
		if _, seen := activity["current_tool"]; !seen && strings.Contains(kind, "tool_call_start") {
			if tool, _ := fields["tool_name"].(string); tool != "" {
				activity["current_tool"] = tool
				activity["tool_started_at"] = event.Timestamp
			}
		}
		if text := crewFunctionEventText(fields); text != "" && (strings.Contains(kind, "llm_generation_end") || strings.Contains(kind, "streaming_chunk") || strings.Contains(kind, "assistant")) {
			if len(text) > crewFunctionActivityTextLimit {
				text = "…" + text[len(text)-crewFunctionActivityTextLimit:]
			}
			activity["last_text"] = text
			activity["last_text_at"] = event.Timestamp
			break
		}
	}
	if len(activity) == 0 {
		return nil
	}
	return activity
}

func storeGetEventsAll() storeEvents.GetEventsOptions {
	return storeEvents.GetEventsOptions{SinceIndex: -1, IncludeStreaming: true}
}

func crewFunctionEventText(fields map[string]interface{}) string {
	for _, key := range []string{"content", "text", "final_response", "response", "chunk"} {
		if value, ok := fields[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// crewTargetRunSessionID is the session a Crew trigger run executes in.
func crewTargetRunSessionID(state triggerTargetRunState) string {
	if status, ok := state.Raw.(productWebhookRunStatus); ok {
		return strings.TrimSpace(status.SessionID)
	}
	return ""
}

// --- tools ---

// registerCrewFunctionTools registers the function tools for one calling
// Crew or workflow Builder chat, plus one generated tool per function of the
// Crews and workflows tagged or attached to this chat. declare, when set,
// admits generated tool names through the product tool gate.
func (api *StreamingAPI) registerCrewFunctionTools(registrar definitionToolRegistrar, userID, sessionID string, parentReq QueryRequest, resolveCaller func(context.Context) (triggerLinkCaller, error), declare func(string)) error {
	if api == nil || api.scheduler == nil || api.productSchedules == nil {
		return nil
	}
	claims := &UserClaims{UserID: strings.TrimSpace(userID)}
	withClaims := func(ctx context.Context) context.Context {
		copy := *claims
		return context.WithValue(ctx, UserContextKey, &copy)
	}
	register := func(name, description string, parameters map[string]interface{}, execute func(context.Context, map[string]interface{}) (string, error)) error {
		return registrar.RegisterCustomTool(name, description, parameters, execute, crewFunctionToolCategory)
	}
	jsonOut := func(value interface{}) (string, error) {
		encoded, err := json.MarshalIndent(value, "", "  ")
		return string(encoded), err
	}
	// resolveOptionalTarget returns the named target, or the caller itself.
	resolveOptionalTarget := func(ctx context.Context, caller triggerLinkCaller, raw interface{}) (triggerTarget, error) {
		name, _ := raw.(string)
		if strings.TrimSpace(name) == "" {
			return selfTriggerTarget(caller), nil
		}
		return resolveTriggerTarget(ctx, claims, name)
	}
	targetSchema := map[string]interface{}{"type": "string", "description": "The Crew or workflow: its name, a #crew:<name> / #workflow:<name> tag, or its exact workspace_path."}
	callFunction := func(ctx context.Context, caller triggerLinkCaller, target triggerTarget, function string, args map[string]interface{}, notify bool, timeout time.Duration) (string, error) {
		functions, err := readCrewFunctions(ctx, target)
		if err != nil {
			return "", err
		}
		functions = withDefaultAskFunction(functions)
		fn, ok := findCrewFunction(functions, function)
		if !ok {
			return "", fmt.Errorf("%s %q has no function %q; it has: %s", target.Kind, target.Label, function, strings.Join(crewFunctionNames(functions), ", "))
		}
		call, err := api.startCrewFunctionCall(ctx, userID, caller, target, fn, args, timeout)
		if err != nil {
			return "", err
		}
		timer := time.NewTimer(crewFunctionFastWait)
		defer timer.Stop()
		select {
		case <-call.done:
			return jsonOut(call.snapshot())
		case <-timer.C:
		case <-ctx.Done():
		}
		response := call.snapshot()
		response["status"] = "running"
		response["note"] = "Still running. Poll with get_function_call, or ask a Crew target for an update with ask_function_update."
		if notify {
			executionID, watchErr := api.startCrewFunctionWatch(parentReq, sessionID, userID, call, timeout)
			if watchErr != nil {
				response["auto_notification"] = "unavailable: " + watchErr.Error() + "; poll with get_function_call"
			} else {
				response["auto_notification"] = map[string]interface{}{"execution_id": executionID}
				response["next"] = "Tell the user what was called and end your turn; the result arrives as an [AUTO-NOTIFICATION] in this chat."
			}
		}
		return jsonOut(response)
	}
	schemaSchema := map[string]interface{}{"type": "object", "description": "JSON Schema subset: type object|array|string|number|integer|boolean, properties, required, items, enum."}

	if err := register("define_function", "Declare or update a typed function on this Crew/workflow (omit target) or on another Crew (any Crew) or a workflow you can edit. Other Crews and workflows then call it with call_function (or a generated tool) and get back a result validated against result_schema. instructions tell the target what to do when called.", map[string]interface{}{
		"type": "object", "required": []string{"name", "description", "instructions"}, "properties": map[string]interface{}{
			"target":        targetSchema,
			"name":          map[string]interface{}{"type": "string", "description": "snake_case name, e.g. run_login_flow."},
			"description":   map[string]interface{}{"type": "string", "description": "What the function does, for callers."},
			"instructions":  map[string]interface{}{"type": "string", "description": "What the target Crew does when this function is called."},
			"input_schema":  schemaSchema,
			"result_schema": schemaSchema,
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		caller, err := resolveCaller(ctx)
		if err != nil {
			return "", err
		}
		target, err := resolveOptionalTarget(ctx, caller, args["target"])
		if err != nil {
			return "", err
		}
		name, _ := args["name"].(string)
		name = strings.TrimSpace(name)
		if !crewFunctionNamePattern.MatchString(name) {
			return "", fmt.Errorf("name must be snake_case: a lowercase letter, then lowercase letters, digits or _ (max 48)")
		}
		description, _ := args["description"].(string)
		instructions, _ := args["instructions"].(string)
		if strings.TrimSpace(description) == "" || strings.TrimSpace(instructions) == "" {
			return "", fmt.Errorf("description and instructions are required")
		}
		inputSchema, _ := args["input_schema"].(map[string]interface{})
		resultSchema, _ := args["result_schema"].(map[string]interface{})
		if len(inputSchema) > 0 && inputSchema["type"] != "object" {
			return "", fmt.Errorf("input_schema must have type object (named arguments)")
		}
		if err := checkCrewFunctionSchema(inputSchema, "input_schema"); err != nil {
			return "", err
		}
		if err := checkCrewFunctionSchema(resultSchema, "result_schema"); err != nil {
			return "", err
		}
		functions, err := readCrewFunctions(ctx, target)
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		creator := caller.Stamp.Type + ":" + caller.Stamp.ID + " (" + caller.Label + ")"
		updated := false
		for i := range functions {
			if functions[i].Name == name {
				functions[i].Description, functions[i].Instructions = strings.TrimSpace(description), strings.TrimSpace(instructions)
				functions[i].InputSchema, functions[i].ResultSchema, functions[i].UpdatedAt = inputSchema, resultSchema, now
				updated = true
			}
		}
		if !updated {
			functions = append(functions, crewFunction{Name: name, Description: strings.TrimSpace(description), Instructions: strings.TrimSpace(instructions), InputSchema: inputSchema, ResultSchema: resultSchema, CreatedBy: creator, CreatedAt: now, UpdatedAt: now})
		}
		if err := writeCrewFunctions(ctx, target, functions); err != nil {
			return "", err
		}
		return jsonOut(map[string]interface{}{"target": target.describe(), "function": name, "updated": updated, "tool_name_for_callers": crewFunctionToolName(target.Label, name)})
	}); err != nil {
		return err
	}

	if err := register("delete_function", "Remove a function from this Crew/workflow (omit target) or from another Crew or editable workflow.", map[string]interface{}{
		"type": "object", "required": []string{"name"}, "properties": map[string]interface{}{"target": targetSchema, "name": map[string]interface{}{"type": "string"}},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		caller, err := resolveCaller(ctx)
		if err != nil {
			return "", err
		}
		target, err := resolveOptionalTarget(ctx, caller, args["target"])
		if err != nil {
			return "", err
		}
		name, _ := args["name"].(string)
		functions, err := readCrewFunctions(ctx, target)
		if err != nil {
			return "", err
		}
		kept := functions[:0]
		removed := false
		for _, fn := range functions {
			if fn.Name == strings.TrimSpace(name) {
				removed = true
				continue
			}
			kept = append(kept, fn)
		}
		if !removed {
			return "", fmt.Errorf("%s %q has no function %q", target.Kind, target.Label, name)
		}
		if err := writeCrewFunctions(ctx, target, kept); err != nil {
			return "", err
		}
		return jsonOut(map[string]interface{}{"target": target.describe(), "removed": strings.TrimSpace(name)})
	}); err != nil {
		return err
	}

	if err := register("list_functions", "List the typed functions a Crew or workflow offers (name, description, input and result schemas). Omit target for this Crew/workflow's own functions.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"target": targetSchema},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		caller, err := resolveCaller(ctx)
		if err != nil {
			return "", err
		}
		target, err := resolveOptionalTarget(ctx, caller, args["target"])
		if err != nil {
			return "", err
		}
		functions, err := readCrewFunctions(ctx, target)
		if err != nil {
			return "", err
		}
		functions = withDefaultAskFunction(functions)
		listed := make([]map[string]interface{}, 0, len(functions))
		for _, fn := range functions {
			listed = append(listed, map[string]interface{}{"name": fn.Name, "description": fn.Description, "input_schema": fn.InputSchema, "result_schema": fn.ResultSchema, "created_by": fn.CreatedBy})
		}
		return jsonOut(map[string]interface{}{"target": target.describe(), "functions": listed, "call_with": "call_function(target, function, args)"})
	}); err != nil {
		return err
	}

	if err := register("call_function", "Call a typed function of another Crew or workflow. Arguments are validated against its input schema; the target does the work in its own chat (queued if busy) and returns a result validated against its result schema. If it finishes within about 2 minutes the result is returned here directly; otherwise this returns status=running with a call_id and the result arrives later as an [AUTO-NOTIFICATION] (unless notify=false). Follow a long call with get_function_call; ask a Crew target for an update with ask_function_update.", map[string]interface{}{
		"type": "object", "required": []string{"target", "function"}, "properties": map[string]interface{}{
			"target":          targetSchema,
			"function":        map[string]interface{}{"type": "string", "description": "Function name from list_functions."},
			"args":            map[string]interface{}{"type": "object", "description": "Arguments matching the function's input schema."},
			"notify":          map[string]interface{}{"type": "boolean", "description": "Resume this chat with the result if it takes longer than the fast window (default true)."},
			"timeout_minutes": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": int(triggerTargetMaxTimeout / time.Minute), "description": "How long the call may take before it fails (default 60)."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		caller, err := resolveCaller(ctx)
		if err != nil {
			return "", err
		}
		raw, _ := args["target"].(string)
		target, err := resolveTriggerTarget(ctx, claims, raw)
		if err != nil {
			return "", err
		}
		function, _ := args["function"].(string)
		callArgs, _ := args["args"].(map[string]interface{})
		if raw, present := args["args"]; present && raw != nil && callArgs == nil {
			return "", fmt.Errorf("args must be a JSON object")
		}
		notify := true
		if value, ok := args["notify"].(bool); ok {
			notify = value
		}
		timeout, err := triggerTargetTimeout(args["timeout_minutes"])
		if err != nil {
			return "", err
		}
		return callFunction(ctx, caller, target, function, callArgs, notify, timeout)
	}); err != nil {
		return err
	}

	callRecord := func(ctx context.Context, args map[string]interface{}) (*crewFunctionCall, triggerLinkCaller, error) {
		caller, err := resolveCaller(ctx)
		if err != nil {
			return nil, triggerLinkCaller{}, err
		}
		id, _ := args["call_id"].(string)
		call := lookupCrewFunctionCall(id)
		if call == nil {
			return nil, caller, fmt.Errorf("unknown function call %q (calls are tracked while the server runs)", id)
		}
		return call, caller, nil
	}
	isCaller := func(call *crewFunctionCall, caller triggerLinkCaller) bool {
		return crewFunctionKey(call.CallerKind, call.CallerID) == crewFunctionKey(caller.Stamp.Type, caller.Stamp.ID)
	}
	isTargetOf := func(call *crewFunctionCall, caller triggerLinkCaller) bool {
		return crewFunctionKey(call.TargetKind, call.TargetID) == crewFunctionKey(caller.Stamp.Type, caller.Stamp.ID)
	}
	callIDSchema := map[string]interface{}{"type": "string", "description": "call_id from call_function, or from the [Function call ...] task you received."}

	if err := register("get_function_call", "Check a function call you made: status, result or error, the target's latest progress reports, and a short tail of what the target is doing right now. Does not interrupt the target.", map[string]interface{}{
		"type": "object", "required": []string{"call_id"}, "properties": map[string]interface{}{"call_id": callIDSchema},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		call, caller, err := callRecord(ctx, args)
		if err != nil {
			return "", err
		}
		if !isCaller(call, caller) && !isTargetOf(call, caller) {
			return "", fmt.Errorf("function call %s belongs to another caller", call.ID)
		}
		out := call.snapshot()
		call.mu.Lock()
		terminal, runID := call.terminalLocked(), call.RunID
		call.mu.Unlock()
		if !terminal && call.TargetKind == triggerCallerCrew {
			if state, stateErr := api.readTriggerTargetRun(ctx, userID, call.caller, call.target, call.TriggerID, runID); stateErr == nil {
				out["run_status"] = state.Status
				if activity := api.crewFunctionActivity(crewTargetRunSessionID(state)); activity != nil {
					out["recent_activity"] = activity
				}
			}
		}
		return jsonOut(out)
	}); err != nil {
		return err
	}

	if err := register("ask_function_update", "Ask a Crew that is running your function call how it is going. The question is delivered into its running turn (Crew targets only; workflow runs do not take mid-run messages). It answers with report_function_progress; read the answer with get_function_call, or it arrives with the final result.", map[string]interface{}{
		"type": "object", "required": []string{"call_id", "question"}, "properties": map[string]interface{}{
			"call_id":  callIDSchema,
			"question": map[string]interface{}{"type": "string", "description": "What you want to know, self-contained."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		call, caller, err := callRecord(ctx, args)
		if err != nil {
			return "", err
		}
		if !isCaller(call, caller) {
			return "", fmt.Errorf("only the caller of function call %s can ask for an update", call.ID)
		}
		if call.TargetKind != triggerCallerCrew {
			return "", fmt.Errorf("workflow runs do not accept mid-run messages; use get_function_call for status and progress")
		}
		question, _ := args["question"].(string)
		if strings.TrimSpace(question) == "" {
			return "", fmt.Errorf("question is required")
		}
		call.mu.Lock()
		terminal, runID := call.terminalLocked(), call.RunID
		call.mu.Unlock()
		if terminal {
			return "", fmt.Errorf("function call %s already finished; read it with get_function_call", call.ID)
		}
		message := fmt.Sprintf("[Update request for function call %s] %s\nAnswer with report_function_progress(call_id=%q, message=...) and keep working on the call.", call.ID, strings.TrimSpace(question), call.ID)
		result, err := api.sendToCrewTriggerRun(ctx, userID, call.caller, call.target, call.TriggerID, runID, message)
		if err != nil {
			return "", err
		}
		result["call_id"] = call.ID
		result["next"] = "The answer arrives as a progress report: read it with get_function_call."
		return jsonOut(result)
	}); err != nil {
		return err
	}

	if err := register("report_function_progress", "While working on a function call you received ([Function call <call_id>] task), report a milestone or answer an update request. The caller sees the latest reports without interrupting you.", map[string]interface{}{
		"type": "object", "required": []string{"call_id", "message"}, "properties": map[string]interface{}{
			"call_id": callIDSchema,
			"message": map[string]interface{}{"type": "string"},
			"percent": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 100},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		call, caller, err := callRecord(ctx, args)
		if err != nil {
			return "", err
		}
		if !isTargetOf(call, caller) {
			return "", fmt.Errorf("only the target of function call %s can report its progress", call.ID)
		}
		message, _ := args["message"].(string)
		if strings.TrimSpace(message) == "" {
			return "", fmt.Errorf("message is required")
		}
		entry := crewFunctionProgress{At: time.Now().UTC(), Message: strings.TrimSpace(message)}
		if percent, ok := crewFunctionNumber(args["percent"]); ok {
			entry.Percent = &percent
		}
		call.mu.Lock()
		if call.terminalLocked() {
			call.mu.Unlock()
			return "", fmt.Errorf("function call %s already finished", call.ID)
		}
		call.Progress = append(call.Progress, entry)
		if len(call.Progress) > crewFunctionProgressKeep {
			call.Progress = call.Progress[len(call.Progress)-crewFunctionProgressKeep:]
		}
		if call.Status == "queued" {
			call.Status = "running"
		}
		call.UpdatedAt = entry.At
		call.mu.Unlock()
		call.persist()
		return jsonOut(map[string]interface{}{"call_id": call.ID, "recorded": true})
	}); err != nil {
		return err
	}

	if err := register("return_function_result", "Finish a function call you received ([Function call <call_id>] task): pass result as JSON matching the function's result schema, or error to report that it could not be done. The caller receives exactly this, not your chat reply. An invalid result is rejected with the reason so you can fix it.", map[string]interface{}{
		"type": "object", "required": []string{"call_id"}, "properties": map[string]interface{}{
			"call_id": callIDSchema,
			"result":  map[string]interface{}{"description": "The result, matching the function's result schema."},
			"error":   map[string]interface{}{"type": "string", "description": "Set instead of result when the call could not be completed."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		call, caller, err := callRecord(ctx, args)
		if err != nil {
			return "", err
		}
		if !isTargetOf(call, caller) {
			return "", fmt.Errorf("only the target of function call %s can return its result", call.ID)
		}
		if errText, _ := args["error"].(string); strings.TrimSpace(errText) != "" {
			if !call.finish("failed", nil, strings.TrimSpace(errText)) {
				return "", fmt.Errorf("function call %s already finished", call.ID)
			}
			return jsonOut(map[string]interface{}{"call_id": call.ID, "status": "failed"})
		}
		result, present := args["result"]
		if !present {
			return "", fmt.Errorf("pass result (or error)")
		}
		call.mu.Lock()
		schema := call.ResultSchema
		freeText := call.FreeText
		call.mu.Unlock()
		if freeText {
			// The implicit ask is free text by contract: never reject a real
			// answer over its shape. Structured answers are kept as JSON text.
			result = crewFunctionFreeTextAnswer(result)
		}
		if problems := validateCrewFunctionValue(schema, result); len(problems) > 0 {
			call.mu.Lock()
			call.InvalidResults++
			invalid := call.InvalidResults
			call.PartialResult = result
			call.mu.Unlock()
			reason := "result does not match the result schema: " + strings.Join(problems, "; ")
			if invalid >= crewFunctionMaxInvalidResults {
				call.finish("failed", nil, "the target returned invalid results twice; last: "+reason)
				return "", fmt.Errorf("%s. The call has now failed", reason)
			}
			return "", fmt.Errorf("%s. Your submission is kept for the caller; fix the shape and call return_function_result again (put anything that doesn't fit, like extra links, into an existing string field or error)", reason)
		}
		if !call.finish("completed", result, "") {
			return "", fmt.Errorf("function call %s already finished", call.ID)
		}
		return jsonOut(map[string]interface{}{"call_id": call.ID, "status": "completed"})
	}); err != nil {
		return err
	}

	// Generated tools: one per function of every Crew/workflow tagged or
	// attached to this chat.
	ctx := withClaims(context.Background())
	caller, err := resolveCaller(ctx)
	if err != nil {
		return nil
	}
	paths := append([]string(nil), parentReq.WorkflowContextPaths...)
	if caller.Stamp.Type == triggerCallerCrew {
		if attached, attachedErr := readWorkWorkflowReferences(ctx, caller.Path); attachedErr == nil {
			paths = append(paths, attached...)
		}
	}
	seen := map[string]bool{}
	for _, raw := range paths {
		target, err := resolveTriggerTarget(ctx, claims, raw)
		if err != nil || caller.isTarget(target) {
			continue
		}
		functions, err := readCrewFunctions(ctx, target)
		if err != nil {
			continue
		}
		for _, fn := range withDefaultAskFunction(functions) {
			toolName := crewFunctionToolName(target.Label, fn.Name)
			if seen[toolName] {
				continue
			}
			seen[toolName] = true
			params := fn.InputSchema
			if len(params) == 0 {
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			description := fmt.Sprintf("Function %q of %s %q: %s Returns a result validated against its result schema; long calls return status=running and notify this chat later.", fn.Name, target.Kind, target.Label, fn.Description)
			if declare != nil {
				declare(toolName)
			}
			fnTarget, fnName := target, fn.Name
			if err := register(toolName, description, params, func(ctx context.Context, args map[string]interface{}) (string, error) {
				ctx = withClaims(ctx)
				caller, err := resolveCaller(ctx)
				if err != nil {
					return "", err
				}
				return callFunction(ctx, caller, fnTarget, fnName, args, true, triggerTargetDefaultTimeout)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
