package server

import (
	"encoding/json"
	"log"
	"net/http"

	"mcp-agent-builder-go/agent_go/pkg/subagents"

	"github.com/gorilla/mux"
)

// RegisterSubAgentRoutes sets up sub-agent template API routes
func RegisterSubAgentRoutes(router *mux.Router, api *StreamingAPI) {
	workspaceAPIURL := getWorkspaceAPIURL()

	router.HandleFunc("/subagents", listSubAgentsHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/subagents/import", importSubAgentHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/subagents/validate", validateSubAgentHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/subagents/{name}", getSubAgentHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/subagents/{name}", updateSubAgentHandler(workspaceAPIURL)).Methods("PUT", "OPTIONS")
	router.HandleFunc("/subagents/{name}", deleteSubAgentHandler(workspaceAPIURL)).Methods("DELETE", "OPTIONS")
}

func listSubAgentsHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		list, err := subagents.DiscoverSubAgents(workspaceAPIURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := subagents.ListSubAgentsResponse{
			SubAgents: list,
			Total:     len(list),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func getSubAgentHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "sub-agent name is required", http.StatusBadRequest)
			return
		}

		sa, err := subagents.GetSubAgent(workspaceAPIURL, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sa)
	}
}

func updateSubAgentHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "sub-agent name is required", http.StatusBadRequest)
			return
		}

		var req subagents.UpdateSubAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		sa, err := subagents.UpdateSubAgent(workspaceAPIURL, name, req.Content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sa)
	}
}

func deleteSubAgentHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "sub-agent name is required", http.StatusBadRequest)
			return
		}

		if err := subagents.DeleteSubAgent(workspaceAPIURL, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func importSubAgentHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req subagents.ImportSubAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.GitHubURL == "" {
			http.Error(w, "github_url is required", http.StatusBadRequest)
			return
		}

		result, err := subagents.ImportGitHubSubAgent(workspaceAPIURL, req.GitHubURL, req.GitHubToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if !result.Success {
			w.WriteHeader(http.StatusBadRequest)
		}
		json.NewEncoder(w).Encode(result)
	}
}

func validateSubAgentHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req subagents.ValidateSubAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.GitHubURL == "" {
			http.Error(w, "github_url is required", http.StatusBadRequest)
			return
		}

		log.Printf("[VALIDATE] URL: %s, token provided: %v, token length: %d", req.GitHubURL, req.GitHubToken != "", len(req.GitHubToken))

		result, err := subagents.ValidateGitHubSubAgent(workspaceAPIURL, req.GitHubURL, req.GitHubToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
