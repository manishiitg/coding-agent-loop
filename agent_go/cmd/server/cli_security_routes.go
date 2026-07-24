package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/clisecurity"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

type cliSecurityStatus struct {
	Config   clisecurity.UserConfig                    `json:"config"`
	Profiles []llmproviders.CodingAgentSecurityProfile `json:"profiles"`
}

func CLISecurityRoutes(router *mux.Router, store *clisecurity.Store) {
	sub := router.PathPrefix("/cli-security").Subrouter()
	sub.HandleFunc("", getCLISecurityHandler(store)).Methods("GET", "OPTIONS")
	sub.HandleFunc("", updateCLISecurityHandler(store)).Methods("PUT", "OPTIONS")
}

func getCLISecurityHandler(store *clisecurity.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == http.MethodOptions {
			return
		}
		user := GetUserFromContext(r.Context())
		if user == nil || user.UserID == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		config, err := store.Read(user.UserID)
		if err != nil {
			http.Error(w, "read CLI security config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cliSecurityStatus{
			Config:   config,
			Profiles: llmproviders.CodingAgentSecurityProfiles(),
		})
	}
}

func updateCLISecurityHandler(store *clisecurity.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == http.MethodOptions {
			return
		}
		user := GetUserFromContext(r.Context())
		if user == nil || user.UserID == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		var config clisecurity.UserConfig
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		config, err := clisecurity.ValidateConfig(config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if config.Mode != llmtypes.CLISecurityModeCompatibility {
			if err := validateStrictCLISecurityConfig(config); err != nil {
				status := http.StatusConflict
				if !errors.Is(err, clisecurity.ErrModeNotEnforceable) {
					status = http.StatusBadRequest
				}
				http.Error(w, err.Error(), status)
				return
			}
		}
		saved, err := store.Write(user.UserID, config)
		if err != nil {
			http.Error(w, "save CLI security config: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cliSecurityStatus{
			Config:   saved,
			Profiles: llmproviders.CodingAgentSecurityProfiles(),
		})
	}
}

func validateStrictCLISecurityConfig(config clisecurity.UserConfig) error {
	profiles := llmproviders.CodingAgentSecurityProfiles()
	profileByProvider := make(map[string]llmproviders.CodingAgentSecurityProfile, len(profiles))
	for _, profile := range profiles {
		profileByProvider[string(profile.Provider)] = profile
	}
	if config.Mode == llmtypes.CLISecurityModeIsolated {
		for _, profile := range profiles {
			if profile.Certified && profile.SupportsPrivateHome {
				return nil
			}
		}
		return clisecurity.ErrModeNotEnforceable
	}
	if len(config.ApprovedProfiles) == 0 {
		return errors.New("verified mode requires at least one approved provider profile")
	}
	for provider, approval := range config.ApprovedProfiles {
		profile, ok := profileByProvider[provider]
		if !ok || !profile.Certified {
			return fmt.Errorf("%w: provider %q has no certified profile", clisecurity.ErrModeNotEnforceable, provider)
		}
		if approval.ProfileVersion != profile.Version {
			return fmt.Errorf("provider %s approval is for profile %s, current profile is %s", provider, approval.ProfileVersion, profile.Version)
		}
		required := map[string]struct{}{}
		for _, capability := range profile.Capabilities {
			required[capability.ID] = struct{}{}
		}
		for _, capability := range approval.Capabilities {
			delete(required, capability)
		}
		if len(required) != 0 {
			return fmt.Errorf("provider %s approval is missing required baseline capabilities", provider)
		}
	}
	return nil
}
