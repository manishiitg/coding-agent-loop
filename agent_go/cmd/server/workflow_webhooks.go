package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
)

const maxWebhookBodyBytes = 1024 * 1024

// The plaintext secret is returned only on creation/rotation. Ciphertext uses
// the existing server secrets key, bound to this workflow and trigger.
type WorkflowWebhookConfig struct {
	AuthMode        string `json:"auth_mode"`
	EncryptedSecret string `json:"encrypted_secret"`
}

type WorkflowWebhookDelivery struct {
	RunID      string          `json:"run_id"`
	DeliveryID string          `json:"delivery_id"`
	Event      string          `json:"event,omitempty"`
	ReceivedAt time.Time       `json:"received_at"`
	Payload    json.RawMessage `json:"payload"`
}

// WebhookRunMetadata is safe delivery context for history; never includes payloads or secrets.
type WebhookRunMetadata struct {
	TriggerName string    `json:"trigger_name"`
	DeliveryID  string    `json:"delivery_id"`
	Event       string    `json:"event,omitempty"`
	ReceivedAt  time.Time `json:"received_at"`
}

func webhookRunMetadata(sctx *ScheduleContext) *WebhookRunMetadata {
	if sctx == nil || sctx.WebhookInput == nil {
		return nil
	}
	return &WebhookRunMetadata{TriggerName: sctx.Schedule.Name, DeliveryID: sctx.WebhookInput.DeliveryID, Event: sctx.WebhookInput.Event, ReceivedAt: sctx.WebhookInput.ReceivedAt}
}

type webhookRouteOption struct {
	StepID    string `json:"step_id"`
	StepTitle string `json:"step_title"`
	RouteID   string `json:"route_id"`
	RouteName string `json:"route_name"`
}

type workflowWebhookResponse struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	AuthMode        string            `json:"auth_mode"`
	Path            string            `json:"path"`
	RouteSelections map[string]string `json:"route_selections"`
	GroupNames      []string          `json:"group_names"`
	Secret          string            `json:"secret,omitempty"`
}

type workflowWebhookRequest struct {
	WorkspacePath   string            `json:"workspace_path"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	AuthMode        string            `json:"auth_mode"`
	RouteSelections map[string]string `json:"route_selections"`
	GroupNames      []string          `json:"group_names"`
	RotateSecret    bool              `json:"rotate_secret"`
}

func validateWebhookSchedule(s WorkflowSchedule) error {
	if s.ScheduleType != "webhook" {
		if s.Webhook != nil {
			return errors.New("webhook configuration requires schedule_type=webhook")
		}
		return nil
	}
	if s.Webhook == nil || s.Webhook.EncryptedSecret == "" {
		return errors.New("Create API triggers with manage_workflow_webhook in Builder chat to generate a secret")
	}
	if s.Webhook.AuthMode != "bearer" && s.Webhook.AuthMode != "github" {
		return errors.New("webhook auth_mode must be bearer or github")
	}
	if s.CronExpression != "" || len(s.CalendarItems) > 0 || len(s.TriggerPayload) > 0 || len(s.Messages) > 0 || s.Query != "" || s.PulseReviewOnly || s.ShouldResumePrevious() {
		return errors.New("API triggers accept delivery input only; clock settings, request overrides, messages, and session resume are not supported")
	}
	if (s.CollisionPolicy != "" && s.CollisionPolicy != "skip") || s.AfterScheduleID != "" {
		return errors.New("API triggers return a retryable busy response; schedule queues and dependencies are not supported")
	}
	if s.WorkshopMode != "run" {
		return errors.New("API triggers require workshop_mode=run")
	}
	return nil
}

func webhookAAD(workflowID, id string) []byte {
	return []byte("workflow-webhook:" + workflowID + ":" + id)
}
func webhookInputPath(workspacePath, runID string) string {
	return workspacePath + "/webhooks/deliveries/" + runID + ".json"
}
func webhookRunInputInstruction(path string) string {
	return stepworkflow.WebhookInputInstruction(path)
}

func workflowWebhookDTO(s WorkflowSchedule) workflowWebhookResponse {
	authMode := ""
	if s.Webhook != nil {
		authMode = s.Webhook.AuthMode
	}
	return workflowWebhookResponse{ID: s.ID, Name: s.Name, Enabled: s.Enabled, AuthMode: authMode, Path: "/api/hooks/workflow/" + s.ID, RouteSelections: s.RouteSelections, GroupNames: s.GroupNames}
}

func workflowWebhookRoutes(ctx context.Context, workspacePath string) ([]webhookRouteOption, error) {
	plan, err := readPlanFromWorkspace(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	options := []webhookRouteOption{}
	// Only top-level deterministic switches can be selected by run_full_workflow.
	for _, step := range plan.Steps {
		if step == nil {
			continue
		}
		if sw, ok := step.(interface {
			GetRoutes() []stepworkflow.RoutingRoute
		}); ok {
			for _, route := range sw.GetRoutes() {
				options = append(options, webhookRouteOption{StepID: step.GetID(), StepTitle: step.GetTitle(), RouteID: route.RouteID, RouteName: route.RouteName})
			}
		}
	}
	return options, nil
}

func validateWebhookRoutes(ctx context.Context, workspacePath string, selections map[string]string) error {
	options, err := workflowWebhookRoutes(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("load API trigger routes: %w", err)
	}
	for stepID, routeID := range selections {
		found := false
		for _, option := range options {
			if option.StepID == stepID && option.RouteID == routeID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("API trigger route %q on step %q no longer exists", routeID, stepID)
		}
	}
	return nil
}

var workflowWebhookConfigMu sync.Mutex

func WorkflowWebhookRoutes(router *mux.Router, svc *SchedulerService) {
	router.HandleFunc("/api/workflow-webhooks", svc.listWorkflowWebhooks).Methods("GET")
	router.HandleFunc("/api/workflow-webhooks", requireWorkflowWriteAccess(svc.saveWorkflowWebhook)).Methods("POST")
	router.HandleFunc("/api/workflow-webhooks/{id}", requireWorkflowWriteAccess(svc.saveWorkflowWebhook)).Methods("PUT")
	router.HandleFunc("/api/workflow-webhooks/{id}", requireWorkflowWriteAccess(svc.deleteWorkflowWebhook)).Methods("DELETE")
	receiver := webhookReceiver{find: findScheduleByIDAny, start: svc.triggerSavedSchedule, existing: svc.existingWebhookRun}
	router.HandleFunc("/api/hooks/workflow/{id}", receiver.receive).Methods("POST")
	router.HandleFunc("/api/hooks/workflow/{id}/runs/{run}", svc.pollWebhookRun).Methods("GET")
	router.HandleFunc("/api/hooks/workflow/{id}/runs/{run}/artifact", svc.downloadWebhookArtifact).Methods("GET", "HEAD")
}

func (s *SchedulerService) listWorkflowWebhooks(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("workspace_path")
	if path == "" {
		http.Error(w, "workspace_path is required", 400)
		return
	}
	if currentUserWorkflowAccess(r, path) == WorkflowAccessNone {
		writeWorkflowPermissionDenied(w, "read")
		return
	}
	manifest, found, err := ReadWorkflowManifest(r.Context(), path)
	if err != nil || !found {
		http.Error(w, "workflow not found", 404)
		return
	}
	hooks := []workflowWebhookResponse{}
	for _, sched := range manifest.Schedules {
		if sched.ScheduleType == "webhook" {
			hooks = append(hooks, workflowWebhookDTO(sched))
		}
	}
	routes, routeErr := workflowWebhookRoutes(r.Context(), path)
	routeError := ""
	if routeErr != nil {
		routeError = routeErr.Error()
		routes = []webhookRouteOption{}
	}
	groups := []string{}
	content, exists, err := readFileFromWorkspace(r.Context(), path+"/variables/variables.json")
	if err == nil && exists {
		var vars VariablesManifest
		if json.Unmarshal([]byte(content), &vars) == nil {
			for _, g := range vars.Groups {
				groups = append(groups, g.Name)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"triggers": hooks, "routes": routes, "groups": groups, "route_error": routeError})
}

func (s *SchedulerService) saveWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	var req workflowWebhookRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", 400)
		return
	}
	if !requireWorkflowOwner(w, r, req.WorkspacePath) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", 400)
		return
	}
	if req.AuthMode != "bearer" && req.AuthMode != "github" {
		http.Error(w, "auth_mode must be bearer or github", 400)
		return
	}
	groups := normalizeScheduleGroupNames(req.GroupNames)
	var err error
	if req.Enabled {
		if err = validateWebhookRoutes(r.Context(), req.WorkspacePath, req.RouteSelections); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		groups, err = validateScheduleGroupNamesForWorkspace(r.Context(), req.WorkspacePath, groups)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	workflowWebhookConfigMu.Lock()
	defer workflowWebhookConfigMu.Unlock()
	manifest, found, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil || !found {
		http.Error(w, "workflow not found", 404)
		return
	}
	id := mux.Vars(r)["id"]
	index := -1
	for i, sched := range manifest.Schedules {
		if sched.ID == id && sched.ScheduleType == "webhook" {
			index = i
			break
		}
	}
	if id != "" && index < 0 {
		http.Error(w, "API trigger not found", 404)
		return
	}
	cfg := &WorkflowWebhookConfig{AuthMode: req.AuthMode}
	secret := ""
	if index >= 0 && manifest.Schedules[index].Webhook != nil {
		*cfg = *manifest.Schedules[index].Webhook
	}
	if index < 0 {
		id = uuid.NewString()
	}
	if index < 0 || req.RotateSecret || cfg.AuthMode != req.AuthMode || cfg.EncryptedSecret == "" {
		random := make([]byte, 32)
		if _, err = rand.Read(random); err == nil {
			secret = hex.EncodeToString(random)
			cfg.EncryptedSecret, err = encryptSecretValueWithAAD(secret, webhookAAD(manifest.ID, id))
		}
		if err != nil {
			http.Error(w, "cannot generate trigger secret", 500)
			return
		}
	}
	cfg.AuthMode = req.AuthMode
	sched := WorkflowSchedule{ID: id, Name: strings.TrimSpace(req.Name), ScheduleType: "webhook", Timezone: "UTC", Enabled: req.Enabled, RouteSelections: req.RouteSelections, GroupNames: groups, Mode: "workshop", WorkshopMode: "run", CollisionPolicy: "skip", Webhook: cfg, PulseMode: "off", PulseModeReason: "Webhook deliveries skip Pulse, backup and publish."}
	if index >= 0 {
		sched.Description = manifest.Schedules[index].Description
		sched.ExecutionMode = manifest.Schedules[index].ExecutionMode
		manifest.Schedules[index] = sched
	} else {
		manifest.Schedules = append(manifest.Schedules, sched)
	}
	if err = ValidateManifest(manifest); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err = WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, "cannot save API trigger", 500)
		return
	}
	s.InvalidateWorkflowManifestCache()
	if err = s.LoadSchedule(buildScheduleContext(req.WorkspacePath, manifest, sched)); err != nil {
		scheduleLogf("[WEBHOOK] Failed to register trigger %s: %v", id, err)
	}
	response := workflowWebhookDTO(sched)
	response.Secret = secret
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if index < 0 {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *SchedulerService) deleteWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("workspace_path")
	if path == "" {
		http.Error(w, "workspace_path is required", 400)
		return
	}
	if !requireWorkflowOwner(w, r, path) {
		return
	}
	workflowWebhookConfigMu.Lock()
	defer workflowWebhookConfigMu.Unlock()
	manifest, found, err := ReadWorkflowManifest(r.Context(), path)
	if err != nil || !found {
		http.Error(w, "workflow not found", 404)
		return
	}
	id := mux.Vars(r)["id"]
	for i, sched := range manifest.Schedules {
		if sched.ID != id || sched.ScheduleType != "webhook" {
			continue
		}
		manifest.Schedules = append(manifest.Schedules[:i], manifest.Schedules[i+1:]...)
		if err = WriteWorkflowManifest(r.Context(), path, manifest); err != nil {
			http.Error(w, "cannot delete API trigger", 500)
			return
		}
		s.InvalidateWorkflowManifestCache()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "API trigger not found", 404)
}

func verifyWebhookRequest(cfg *WorkflowWebhookConfig, secret string, r *http.Request, body []byte) bool {
	if cfg == nil || secret == "" {
		return false
	}
	switch cfg.AuthMode {
	case "bearer":
		supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if supplied == r.Header.Get("Authorization") {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(supplied), []byte(secret)) == 1
	case "github":
		signature := r.Header.Get("X-Hub-Signature-256")
		if !strings.HasPrefix(signature, "sha256=") {
			return false
		}
		supplied, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
		if err != nil {
			return false
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		return hmac.Equal(supplied, mac.Sum(nil))
	}
	return false
}

type webhookReceiver struct {
	find     func(context.Context, string) (*ScheduleSearchResult, error)
	start    func(string, string, string, *WorkflowWebhookDelivery) (string, error)
	existing func(context.Context, string) (schedulerstate.Run, error)
}

func webhookDeliveryRunID(workflowID, id, deliveryID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(workflowID+"\x00"+id+"\x00"+deliveryID)).String()
}

func (s *SchedulerService) existingWebhookRun(ctx context.Context, runID string) (schedulerstate.Run, error) {
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return schedulerstate.Run{}, errors.New("API trigger run store is unavailable")
	}
	return s.stateStore.GetRun(ctx, runID)
}

func (receiver webhookReceiver) receive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := mux.Vars(r)["id"]
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "API trigger not found", 404)
		return
	}
	result, err := receiver.find(r.Context(), id)
	if err != nil {
		http.Error(w, "API trigger not found", 404)
		return
	}
	sched := result.Manifest.Schedules[result.Index]
	if sched.ScheduleType != "webhook" || sched.Webhook == nil {
		http.Error(w, "API trigger not found", 404)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			http.Error(w, "payload exceeds 1 MiB", 413)
		} else {
			http.Error(w, "cannot read payload", 400)
		}
		return
	}
	secret, err := decryptSecretValueWithAAD(sched.Webhook.EncryptedSecret, webhookAAD(result.Manifest.ID, id))
	if err != nil || !verifyWebhookRequest(sched.Webhook, secret, r, body) {
		http.Error(w, "invalid webhook credentials", 401)
		return
	}
	if !sched.Enabled {
		http.Error(w, "API trigger is disabled", 410)
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (contentType != "application/json" && !strings.HasSuffix(contentType, "+json")) {
		http.Error(w, "Content-Type must be application/json", 415)
		return
	}
	if !json.Valid(body) {
		http.Error(w, "invalid JSON payload", 400)
		return
	}
	if sched.Webhook.AuthMode == "github" && r.Header.Get("X-GitHub-Event") == "ping" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"pong"}`))
		return
	}
	deliveryID := r.Header.Get("Idempotency-Key")
	event := r.Header.Get("X-Webhook-Event")
	if sched.Webhook.AuthMode == "github" {
		deliveryID = r.Header.Get("X-GitHub-Delivery")
		event = r.Header.Get("X-GitHub-Event")
	}
	if len(deliveryID) > 256 || len(event) > 256 {
		http.Error(w, "delivery ID and event must be at most 256 bytes", 400)
		return
	}
	if deliveryID == "" {
		deliveryID = uuid.NewString()
	}
	runID := webhookDeliveryRunID(result.Manifest.ID, id, deliveryID)
	respondExisting := func() bool {
		run, lookupErr := receiver.existing(r.Context(), runID)
		if lookupErr == nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": run.State, "run_id": run.RunID, "duplicate": true, "status_url": webhookStatusPath(id, run.RunID)})
			return true
		}
		if !errors.Is(lookupErr, schedulerstate.ErrRunNotFound) {
			http.Error(w, "run storage is unavailable", 503)
			return true
		}
		return false
	}
	if respondExisting() {
		return
	}
	input := &WorkflowWebhookDelivery{RunID: runID, DeliveryID: deliveryID, Event: event, ReceivedAt: time.Now().UTC(), Payload: append(json.RawMessage(nil), body...)}
	acceptedID, err := receiver.start(result.WorkspacePath, id, "", input)
	if err != nil {
		// A concurrent retry may have claimed this delivery since the first lookup.
		if respondExisting() {
			return
		}
		w.Header().Set("Retry-After", "30")
		http.Error(w, "workflow could not start; retry the delivery or check API trigger configuration", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "accepted", "run_id": acceptedID, "delivery_id": deliveryID, "status_url": webhookStatusPath(id, acceptedID)})
}
