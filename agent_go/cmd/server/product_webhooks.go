package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/productschedule"
)

// productWebhookTrigger is the message-only product counterpart of an
// AgentWorks workflow webhook. It deliberately has no route/step fields: an
// authenticated delivery becomes one turn in the product's durable chat.
type productWebhookTrigger struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Enabled bool                   `json:"enabled"`
	Message string                 `json:"message"`
	Webhook *WorkflowWebhookConfig `json:"webhook,omitempty"`
}

type productWebhookRequest struct {
	ProfileID    string `json:"profile_id"`
	ProjectID    string `json:"project_id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Message      string `json:"message"`
	AuthMode     string `json:"auth_mode"`
	RotateSecret bool   `json:"rotate_secret"`
}

type productWebhookResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Message  string `json:"message"`
	AuthMode string `json:"auth_mode"`
	Path     string `json:"path"`
	Secret   string `json:"secret,omitempty"`
}

func productWebhookDTO(trigger productWebhookTrigger) productWebhookResponse {
	authMode := ""
	if trigger.Webhook != nil {
		authMode = trigger.Webhook.AuthMode
	}
	return productWebhookResponse{
		ID: trigger.ID, Name: trigger.Name, Enabled: trigger.Enabled,
		Message: trigger.Message, AuthMode: authMode,
		Path: "/api/hooks/product/" + trigger.ID,
	}
}

func productWebhookAAD(projectID, triggerID string) []byte {
	return []byte("product-webhook:" + projectID + ":" + triggerID)
}

func validateProductWebhook(trigger productWebhookTrigger) error {
	if _, err := uuid.Parse(trigger.ID); err != nil {
		return fmt.Errorf("trigger id must be a UUID")
	}
	if strings.TrimSpace(trigger.Name) == "" {
		return fmt.Errorf("trigger name is required")
	}
	if strings.TrimSpace(trigger.Message) == "" {
		return fmt.Errorf("trigger message is required")
	}
	if trigger.Webhook == nil || trigger.Webhook.EncryptedSecret == "" {
		return fmt.Errorf("trigger secret is required")
	}
	if trigger.Webhook.AuthMode != "bearer" && trigger.Webhook.AuthMode != "github" {
		return fmt.Errorf("auth_mode must be bearer or github")
	}
	return nil
}

var productWebhookConfigMu sync.Mutex
var productWebhookDeliveries sync.Map

func ProductWebhookRoutes(router *mux.Router, svc *ProductScheduleService) {
	router.HandleFunc("/api/product-webhooks", svc.listProductWebhooks).Methods("GET")
	router.HandleFunc("/api/product-webhooks", svc.saveProductWebhook).Methods("POST")
	router.HandleFunc("/api/product-webhooks/{id}", svc.saveProductWebhook).Methods("PUT")
	router.HandleFunc("/api/product-webhooks/{id}", svc.deleteProductWebhook).Methods("DELETE")
	router.HandleFunc("/api/hooks/product/{id}", svc.receiveProductWebhook).Methods("POST")
}

func productWebhookCoordinates(r *http.Request) (string, string) {
	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		return strings.TrimSpace(r.URL.Query().Get("profile_id")), strings.TrimSpace(r.URL.Query().Get("project_id"))
	}
	return "", ""
}

func (s *ProductScheduleService) listProductWebhooks(w http.ResponseWriter, r *http.Request) {
	profileID, projectID := productWebhookCoordinates(r)
	if profileID == "" {
		profileID = "work"
	}
	_, _, manifest, err := s.projectManifest(r.Context(), productWorkspaceUserID(r.Context()), profileID, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	responses := make([]productWebhookResponse, 0, len(manifest.Triggers))
	for _, trigger := range manifest.Triggers {
		responses = append(responses, productWebhookDTO(trigger))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"triggers": responses})
}

func (s *ProductScheduleService) projectWebhookConfigs(ctx context.Context, userID, profileID, projectID string) ([]productWebhookTrigger, error) {
	_, _, manifest, err := s.projectManifest(ctx, userID, profileID, projectID)
	if err != nil {
		return nil, err
	}
	return append([]productWebhookTrigger(nil), manifest.Triggers...), nil
}

func (s *ProductScheduleService) saveProductWebhook(w http.ResponseWriter, r *http.Request) {
	var req productWebhookRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.ProfileID == "" {
		req.ProfileID = "work"
	}
	response, created, err := s.saveProductWebhookConfig(r.Context(), productWorkspaceUserID(r.Context()), req, mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if created {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *ProductScheduleService) saveProductWebhookConfig(ctx context.Context, userID string, req productWebhookRequest, id string) (productWebhookResponse, bool, error) {
	productWebhookConfigMu.Lock()
	defer productWebhookConfigMu.Unlock()
	profile, binding, manifest, err := s.projectManifest(ctx, userID, req.ProfileID, req.ProjectID)
	if err != nil {
		return productWebhookResponse{}, false, err
	}
	if !agentprofiles.HasFeature(profile, "triggers") {
		return productWebhookResponse{}, false, fmt.Errorf("product triggers are not enabled")
	}
	index := -1
	for i := range manifest.Triggers {
		if manifest.Triggers[i].ID == id {
			index = i
			break
		}
	}
	if id != "" && index < 0 {
		return productWebhookResponse{}, false, fmt.Errorf("trigger not found")
	}
	if id == "" {
		id = uuid.NewString()
	}
	trigger := productWebhookTrigger{ID: id, Name: strings.TrimSpace(req.Name), Enabled: req.Enabled, Message: strings.TrimSpace(req.Message)}
	if index >= 0 {
		trigger.Webhook = manifest.Triggers[index].Webhook
	}
	if trigger.Webhook == nil {
		trigger.Webhook = &WorkflowWebhookConfig{}
	}
	if req.AuthMode == "" {
		req.AuthMode = trigger.Webhook.AuthMode
	}
	if req.AuthMode == "" {
		req.AuthMode = "bearer"
	}
	secret := ""
	if index < 0 || req.RotateSecret || trigger.Webhook.AuthMode != req.AuthMode || trigger.Webhook.EncryptedSecret == "" {
		random := make([]byte, 32)
		if _, err = rand.Read(random); err == nil {
			secret = hex.EncodeToString(random)
			trigger.Webhook.EncryptedSecret, err = encryptSecretValueWithAAD(secret, productWebhookAAD(manifest.ID, id))
		}
		if err != nil {
			return productWebhookResponse{}, false, fmt.Errorf("cannot generate trigger secret: %w", err)
		}
	}
	trigger.Webhook.AuthMode = req.AuthMode
	if err := validateProductWebhook(trigger); err != nil {
		return productWebhookResponse{}, false, err
	}
	if index >= 0 {
		manifest.Triggers[index] = trigger
	} else {
		manifest.Triggers = append(manifest.Triggers, trigger)
	}
	if err := s.writeProjectManifest(ctx, binding, manifest); err != nil {
		return productWebhookResponse{}, false, fmt.Errorf("cannot save trigger: %w", err)
	}
	response := productWebhookDTO(trigger)
	response.Secret = secret
	return response, index < 0, nil
}

func (s *ProductScheduleService) deleteProductWebhook(w http.ResponseWriter, r *http.Request) {
	profileID, projectID := productWebhookCoordinates(r)
	if profileID == "" {
		profileID = "work"
	}
	productWebhookConfigMu.Lock()
	defer productWebhookConfigMu.Unlock()
	_, binding, manifest, err := s.projectManifest(r.Context(), productWorkspaceUserID(r.Context()), profileID, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	id := mux.Vars(r)["id"]
	for i := range manifest.Triggers {
		if manifest.Triggers[i].ID != id {
			continue
		}
		manifest.Triggers = append(manifest.Triggers[:i], manifest.Triggers[i+1:]...)
		if err := s.writeProjectManifest(r.Context(), binding, manifest); err != nil {
			http.Error(w, "cannot delete trigger", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "trigger not found", http.StatusNotFound)
}

func (s *ProductScheduleService) deleteProductWebhookConfig(ctx context.Context, userID, profileID, projectID, id string) error {
	productWebhookConfigMu.Lock()
	defer productWebhookConfigMu.Unlock()
	_, binding, manifest, err := s.projectManifest(ctx, userID, profileID, projectID)
	if err != nil {
		return err
	}
	for i := range manifest.Triggers {
		if manifest.Triggers[i].ID != id {
			continue
		}
		manifest.Triggers = append(manifest.Triggers[:i], manifest.Triggers[i+1:]...)
		return s.writeProjectManifest(ctx, binding, manifest)
	}
	return fmt.Errorf("trigger not found")
}

type productWebhookMatch struct {
	UserID   string
	Profile  agentprofiles.Profile
	Binding  productConversationBinding
	Manifest productProjectManifest
	Trigger  productWebhookTrigger
}

func (s *ProductScheduleService) findProductWebhook(ctx context.Context, id string) (*productWebhookMatch, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("product profiles unavailable")
	}
	for _, profile := range s.registry.List("") {
		if !agentprofiles.HasFeature(profile, "triggers") {
			continue
		}
		for _, userID := range s.users(productAccessName(profile)) {
			root, err := cleanAgentProfileWorkspace(profile.Runtime.Workspace.ProjectsRoot, userID)
			if err != nil {
				continue
			}
			paths, exists, err := defaultProductProjectStore().listPaths(ctx, agentProfileRuntimeWorkspace(userID, root))
			if err != nil || !exists {
				continue
			}
			for _, candidate := range paths {
				candidate = filepath.ToSlash(strings.TrimSpace(candidate))
				if !strings.HasSuffix(candidate, "/product.json") {
					continue
				}
				raw, found, err := s.readFile(ctx, candidate)
				if err != nil || !found {
					continue
				}
				var manifest productProjectManifest
				if json.Unmarshal([]byte(raw), &manifest) != nil || manifest.Product != profile.ID {
					continue
				}
				runtimePath := candidate
				if strings.EqualFold(profile.ID, "work") {
					runtimePath = projectRuntimeManifestPath(profile.ID, filepath.ToSlash(filepath.Dir(candidate)))
					runtimeRaw, runtimeFound, runtimeErr := s.readFile(ctx, runtimePath)
					if runtimeErr != nil {
						continue
					}
					if runtimeFound {
						var runtimeManifest productProjectManifest
						if json.Unmarshal([]byte(runtimeRaw), &runtimeManifest) != nil {
							continue
						}
						manifest.Triggers = runtimeManifest.Triggers
					} else {
						runtimePath = candidate
					}
				}
				for _, trigger := range manifest.Triggers {
					if trigger.ID != id {
						continue
					}
					return &productWebhookMatch{UserID: userID, Profile: profile, Binding: productConversationBinding{WorkspacePath: filepath.ToSlash(filepath.Dir(candidate)), ManifestPath: runtimePath}, Manifest: manifest, Trigger: trigger}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("trigger not found")
}

func (s *ProductScheduleService) receiveProductWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := mux.Vars(r)["id"]
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "trigger not found", http.StatusNotFound)
		return
	}
	match, err := s.findProductWebhook(r.Context(), id)
	if err != nil || match.Trigger.Webhook == nil {
		http.Error(w, "trigger not found", http.StatusNotFound)
		return
	}
	body, err := readWebhookJSONBody(w, r)
	if err != nil {
		return
	}
	secret, err := decryptSecretValueWithAAD(match.Trigger.Webhook.EncryptedSecret, productWebhookAAD(match.Manifest.ID, id))
	if err != nil || !verifyWebhookRequest(match.Trigger.Webhook, secret, r, body) {
		http.Error(w, "invalid webhook credentials", http.StatusUnauthorized)
		return
	}
	if match.Trigger.Webhook.AuthMode == "github" && r.Header.Get("X-GitHub-Event") == "ping" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"pong"}`))
		return
	}
	if !match.Trigger.Enabled {
		http.Error(w, "trigger is disabled", http.StatusGone)
		return
	}
	deliveryID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if match.Trigger.Webhook.AuthMode == "github" {
		deliveryID = strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	}
	if deliveryID == "" {
		deliveryID = uuid.NewString()
	}
	runID := webhookDeliveryRunID(match.Manifest.ID, id, deliveryID)
	if _, loaded := productWebhookDeliveries.LoadOrStore(runID, true); loaded {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "accepted", "run_id": runID, "duplicate": true})
		return
	}
	relativePayloadPath := "triggers/deliveries/" + runID + ".json"
	payloadPath := filepath.ToSlash(filepath.Join(match.Binding.WorkspacePath, relativePayloadPath))
	if err := s.writeFile(r.Context(), payloadPath, string(body)+"\n"); err != nil {
		productWebhookDeliveries.Delete(runID)
		http.Error(w, "cannot persist trigger payload", http.StatusInternalServerError)
		return
	}
	message := strings.TrimSpace(match.Trigger.Message) + "\n\nThis turn was started by an authenticated webhook. Read its JSON payload from `" + relativePayloadPath + "` and use it as input."
	job := productScheduleJob{UserID: match.UserID, Profile: match.Profile, ProjectID: match.Manifest.ID, ProjectTitle: match.Manifest.Title, WorkspacePath: match.Binding.WorkspacePath, ManifestPath: match.Binding.ManifestPath, Schedule: productschedule.Schedule{ID: match.Trigger.ID, Name: match.Trigger.Name, Enabled: true, Messages: []string{message}}}
	go func() {
		_, _ = s.Run(context.Background(), job, "webhook", time.Time{})
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "accepted", "run_id": runID})
}

func readWebhookJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			http.Error(w, "payload exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "cannot read payload", http.StatusBadRequest)
		}
		return nil, err
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (contentType != "application/json" && !strings.HasSuffix(contentType, "+json")) {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return nil, fmt.Errorf("invalid content type")
	}
	if !json.Valid(body) {
		http.Error(w, "valid JSON payload required", http.StatusBadRequest)
		return nil, fmt.Errorf("invalid JSON")
	}
	return body, nil
}
