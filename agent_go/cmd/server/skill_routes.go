package server

import (
	"encoding/json"
	"net/http"

	"mcp-agent-builder-go/agent_go/pkg/skills"

	"github.com/gorilla/mux"
)

// Default workspace API URL
const defaultWorkspaceAPIURL = "http://localhost:8081"

// RegisterSkillRoutes sets up skill API routes
func RegisterSkillRoutes(router *mux.Router, api *StreamingAPI) {
	workspaceAPIURL := defaultWorkspaceAPIURL

	router.HandleFunc("/skills", listSkillsHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/skills/import", importSkillHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/validate", validateSkillHandler()).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/{name}", getSkillHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/skills/{name}", updateSkillHandler(workspaceAPIURL)).Methods("PUT", "OPTIONS")
	router.HandleFunc("/skills/{name}", deleteSkillHandler(workspaceAPIURL)).Methods("DELETE", "OPTIONS")
}

func listSkillsHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		skillList, err := skills.DiscoverSkills(workspaceAPIURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := skills.ListSkillsResponse{
			Skills: skillList,
			Total:  len(skillList),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func getSkillHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "skill name is required", http.StatusBadRequest)
			return
		}

		skill, err := skills.GetSkill(workspaceAPIURL, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skill)
	}
}

func importSkillHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req skills.ImportSkillRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.GitHubURL == "" {
			http.Error(w, "github_url is required", http.StatusBadRequest)
			return
		}

		result, err := skills.ImportGitHubSkill(workspaceAPIURL, req.GitHubURL)
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

func validateSkillHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req skills.ValidateSkillRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.GitHubURL == "" {
			http.Error(w, "github_url is required", http.StatusBadRequest)
			return
		}

		result, err := skills.ValidateGitHubSkill(req.GitHubURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func updateSkillHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "skill name is required", http.StatusBadRequest)
			return
		}

		var req skills.UpdateSkillRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		skill, err := skills.UpdateSkill(workspaceAPIURL, name, req.Content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skill)
	}
}

func deleteSkillHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "skill name is required", http.StatusBadRequest)
			return
		}

		if err := skills.DeleteSkill(workspaceAPIURL, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
