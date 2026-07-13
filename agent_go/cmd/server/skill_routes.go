package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"

	"github.com/gorilla/mux"
)

// RegisterSkillRoutes sets up skill API routes
func RegisterSkillRoutes(router *mux.Router, api *StreamingAPI) {
	workspaceAPIURL := getWorkspaceAPIURL()

	router.HandleFunc("/skills", listSkillsHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/skills/import", importSkillHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/import-zip", importSkillZipHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/validate", validateSkillHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/validate-zip", validateSkillZipHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/{name}", getSkillHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/skills/{name}", updateSkillHandler(workspaceAPIURL)).Methods("PUT", "OPTIONS")
	router.HandleFunc("/skills/{name}", deleteSkillHandler(workspaceAPIURL)).Methods("DELETE", "OPTIONS")

	// CLI-based routes (Vercel skills CLI)
	router.HandleFunc("/skills/cli/install", cliInstallHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/cli/check-updates", cliCheckUpdatesHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/skills/cli/update", cliUpdateHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/skills/cli/available", cliAvailableHandler()).Methods("GET", "OPTIONS")
	router.HandleFunc("/skills/cli/search", cliSearchHandler()).Methods("GET", "OPTIONS")
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

		result, err := skills.ImportGitHubSkill(workspaceAPIURL, req.GitHubURL, req.GitHubToken)
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

func validateSkillHandler(workspaceAPIURL string) http.HandlerFunc {
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

		log.Printf("[VALIDATE] URL: %s, token provided: %v, token length: %d", req.GitHubURL, req.GitHubToken != "", len(req.GitHubToken))

		result, err := skills.ValidateGitHubSkill(workspaceAPIURL, req.GitHubURL, req.GitHubToken)
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

		// Also remove from lock file if tracked
		_ = skills.RemoveFromLockFile(workspaceAPIURL, name)

		w.WriteHeader(http.StatusNoContent)
	}
}

func validateSkillZipHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Parse multipart form with 10MB limit
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		result, err := skills.ValidateZipSkill(workspaceAPIURL, file, header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// --- CLI-based handlers ---

func cliAvailableHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"available": skills.IsAvailable()})
	}
}

func cliSearchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "query parameter 'q' is required", http.StatusBadRequest)
			return
		}

		results, err := skills.FindSkills(r.Context(), query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func cliInstallHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req struct {
			Source string `json:"source"` // owner/repo, URL, or local path
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Source == "" {
			http.Error(w, "source is required (e.g., 'owner/repo' or GitHub URL)", http.StatusBadRequest)
			return
		}

		result, err := skills.ImportToWorkspace(r.Context(), workspaceAPIURL, req.Source)
		if err != nil {
			log.Printf("[SKILLS CLI] Install failed for '%s': %v", req.Source, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func cliCheckUpdatesHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		updates, err := skills.CheckUpdates(r.Context(), workspaceAPIURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updates)
	}
}

func cliUpdateHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		result, err := skills.UpdateAll(r.Context(), workspaceAPIURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func importSkillZipHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Parse multipart form with 10MB limit
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		result, err := skills.ImportZipSkill(workspaceAPIURL, file, header)
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
