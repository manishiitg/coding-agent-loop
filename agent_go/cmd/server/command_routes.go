package server

import (
	"encoding/json"
	"net/http"

	"mcp-agent-builder-go/agent_go/pkg/commands"

	"github.com/gorilla/mux"
)

// RegisterCommandRoutes sets up user command API routes
func RegisterCommandRoutes(router *mux.Router, api *StreamingAPI) {
	workspaceAPIURL := getWorkspaceAPIURL()

	router.HandleFunc("/commands", listCommandsHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/commands", createCommandHandler(workspaceAPIURL)).Methods("POST", "OPTIONS")
	router.HandleFunc("/commands/{name}", getCommandHandler(workspaceAPIURL)).Methods("GET", "OPTIONS")
	router.HandleFunc("/commands/{name}", updateCommandHandler(workspaceAPIURL)).Methods("PUT", "OPTIONS")
	router.HandleFunc("/commands/{name}", deleteCommandHandler(workspaceAPIURL)).Methods("DELETE", "OPTIONS")
}

func listCommandsHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		cmdList, err := commands.DiscoverCommands(workspaceAPIURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := commands.ListCommandsResponse{
			Commands: cmdList,
			Total:    len(cmdList),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func getCommandHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "command name is required", http.StatusBadRequest)
			return
		}

		cmd, err := commands.GetCommand(workspaceAPIURL, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cmd)
	}
}

func createCommandHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req commands.CreateCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		if req.Content == "" {
			http.Error(w, "content is required", http.StatusBadRequest)
			return
		}

		cmd, err := commands.CreateCommand(workspaceAPIURL, req.Name, req.Content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(cmd)
	}
}

func updateCommandHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "command name is required", http.StatusBadRequest)
			return
		}

		var req commands.UpdateCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cmd, err := commands.UpdateCommand(workspaceAPIURL, name, req.Content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cmd)
	}
}

func deleteCommandHandler(workspaceAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" {
			http.Error(w, "command name is required", http.StatusBadRequest)
			return
		}

		if err := commands.DeleteCommand(workspaceAPIURL, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
