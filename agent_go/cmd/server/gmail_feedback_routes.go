package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"mcp-agent-builder-go/agent_go/cmd/server/services"

	"github.com/gorilla/mux"
)

// Gmail is an outbound-only notification channel. The UI manages it entirely
// through these endpoints — enable/disable, set the default recipient, and see
// live auth status — so the user never edits config/gmail-config.json by hand.

// GmailConfigRequest is the body the UI posts to enable/disable Gmail.
type GmailConfigRequest struct {
	Enabled           bool     `json:"enabled"`
	DefaultTo         string   `json:"default_to"`
	AllowedRecipients []string `json:"allowed_recipients,omitempty"`
	GwsPath           string   `json:"gws_path,omitempty"`
	ConfigHome        string   `json:"config_home,omitempty"`
	CredentialsFile   string   `json:"credentials_file,omitempty"`
	Token             string   `json:"token,omitempty"`
}

// GmailConfigResponse is the saved config plus live, auto-detected auth state,
// so the UI can render the toggle and a "Connected / Connect Gmail" badge
// without the user inspecting anything on the host.
type GmailConfigResponse struct {
	Enabled           bool                     `json:"enabled"`
	DefaultTo         string                   `json:"default_to,omitempty"`
	AllowedRecipients []string                 `json:"allowed_recipients,omitempty"`
	Auth              services.GmailAuthStatus `json:"auth"`
	// Ready is the bottom-line "this channel will actually send" signal:
	// enabled + a default recipient + gws installed/authenticated with a Gmail scope.
	Ready bool `json:"ready"`
}

// GmailTestResponse is the result of the "send test email" button.
type GmailTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// GmailFeedbackRoutes wires up the Gmail config/status/test API.
func GmailFeedbackRoutes(router *mux.Router, api *StreamingAPI) {
	r := router.PathPrefix("/api/human-feedback/gmail").Subrouter()
	r.HandleFunc("/config", getGmailConfigHandler(api)).Methods("GET")
	r.HandleFunc("/config", updateGmailConfigHandler(api)).Methods("POST", "OPTIONS")
	r.HandleFunc("/status", getGmailStatusHandler(api)).Methods("GET")
	r.HandleFunc("/test", testGmailConnectionHandler(api)).Methods("POST", "OPTIONS")
}

// ensureGmailService returns the global Gmail service, initializing it lazily.
func ensureGmailService() (*services.GmailService, error) {
	if svc := services.GetGmailService(); svc != nil {
		return svc, nil
	}
	return services.InitGmailService()
}

func buildGmailConfigResponse(svc *services.GmailService, cfg *services.GmailConfig, auth services.GmailAuthStatus) GmailConfigResponse {
	return GmailConfigResponse{
		Enabled:           cfg.Enabled,
		DefaultTo:         cfg.DefaultTo,
		AllowedRecipients: cfg.AllowedRecipients,
		Auth:              auth,
		Ready:             cfg.Enabled && cfg.DefaultTo != "" && auth.Authenticated && auth.HasGmailScope,
	}
}

func getGmailConfigHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := ensureGmailService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Gmail service: %v", err), http.StatusInternalServerError)
			return
		}
		cfg := svc.GetConfig()
		auth := svc.AuthStatus(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildGmailConfigResponse(svc, cfg, auth))
	}
}

// getGmailStatusHandler returns just the auto-detected auth state — useful for
// the UI to poll/refresh the connection badge without re-fetching config.
func getGmailStatusHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := ensureGmailService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Gmail service: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc.AuthStatus(r.Context()))
	}
}

func updateGmailConfigHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req GmailConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		svc, err := ensureGmailService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Gmail service: %v", err), http.StatusInternalServerError)
			return
		}

		cfg := &services.GmailConfig{
			Enabled:           req.Enabled,
			DefaultTo:         req.DefaultTo,
			AllowedRecipients: req.AllowedRecipients,
			GwsPath:           req.GwsPath,
			ConfigHome:        req.ConfigHome,
			CredentialsFile:   req.CredentialsFile,
			Token:             req.Token,
		}
		if err := svc.SaveConfig(r.Context(), cfg); err != nil {
			http.Error(w, fmt.Sprintf("failed to save config: %v", err), http.StatusInternalServerError)
			return
		}

		// Reflect the toggle on the live NotificationManager so it takes effect
		// without a restart: register when enabled, unregister when disabled.
		nm := services.GetNotificationManager()
		if nm != nil {
			if svc.IsEnabled() {
				nm.RegisterConnector(svc)
			} else {
				nm.UnregisterConnector("gmail")
			}
		}

		saved := svc.GetConfig()
		auth := svc.AuthStatus(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildGmailConfigResponse(svc, saved, auth))
	}
}

func testGmailConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		svc, err := ensureGmailService()
		if err != nil {
			writeGmailTest(w, false, fmt.Sprintf("failed to initialize Gmail service: %v", err))
			return
		}

		// Optional override recipient (test before saving). Falls back to default_to.
		to := ""
		if r.ContentLength > 0 {
			var req GmailConfigRequest
			if json.NewDecoder(r.Body).Decode(&req) == nil {
				to = req.DefaultTo
			}
		}

		msgID, err := svc.SendTest(r.Context(), to)
		if err != nil {
			log.Printf("[GMAIL] test send failed: %v", err)
			writeGmailTest(w, false, fmt.Sprintf("Test failed: %v", err))
			return
		}
		writeGmailTest(w, true, fmt.Sprintf("Test email sent (id: %s). Check the recipient inbox.", msgID))
	}
}

func writeGmailTest(w http.ResponseWriter, success bool, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GmailTestResponse{Success: success, Message: message})
}
