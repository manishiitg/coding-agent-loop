package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// PresetQueryRoutes sets up preset query API routes
func PresetQueryRoutes(router *mux.Router, db database.Database) {
	apiRouter := router.PathPrefix("/api/chat-history").Subrouter()

	// Preset Queries API routes
	apiRouter.HandleFunc("/presets", createPresetQueryHandler(db)).Methods("POST")
	apiRouter.HandleFunc("/presets", listPresetQueriesHandler(db)).Methods("GET")
	apiRouter.HandleFunc("/presets/{id}", getPresetQueryHandler(db)).Methods("GET")
	apiRouter.HandleFunc("/presets/{id}", updatePresetQueryHandler(db)).Methods("PUT")
	apiRouter.HandleFunc("/presets/{id}", deletePresetQueryHandler(db)).Methods("DELETE")
}

// createPresetQueryHandler creates a new preset query
func createPresetQueryHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req database.CreatePresetQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		preset, err := db.CreatePresetQuery(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(preset)
	}
}

// listPresetQueriesHandler lists all preset queries
func listPresetQueriesHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 50
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}

		presets, total, err := db.ListPresetQueries(r.Context(), limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := database.ListPresetQueriesResponse{
			Presets: presets,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getPresetQueryHandler retrieves a specific preset query
func getPresetQueryHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]
		if id == "" {
			http.Error(w, "preset query ID is required", http.StatusBadRequest)
			return
		}

		preset, err := db.GetPresetQuery(r.Context(), id)
		if err != nil {
			if err.Error() == "preset query not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(preset)
	}
}

// updatePresetQueryHandler updates a preset query
func updatePresetQueryHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]
		if id == "" {
			http.Error(w, "preset query ID is required", http.StatusBadRequest)
			return
		}

		var req database.UpdatePresetQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		preset, err := db.UpdatePresetQuery(r.Context(), id, &req)
		if err != nil {
			if err.Error() == "preset query not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(preset)
	}
}

// deletePresetQueryHandler deletes a preset query
func deletePresetQueryHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]
		if id == "" {
			http.Error(w, "preset query ID is required", http.StatusBadRequest)
			return
		}

		err := db.DeletePresetQuery(r.Context(), id)
		if err != nil {
			if err.Error() == "preset query not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
