package costledger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// NewTestServer creates a mock workspace API server for cost ledger tests.
// The server stores files in memory and supports GET/PUT operations.
func NewTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	files := map[string]string{}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/documents/")
		path = strings.ReplaceAll(path, "%2F", "/")

		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			content, ok := files[path]
			mu.Unlock()
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true,
					"message": "File does not exist",
					"data":    map[string]any{},
					"error":   "File not found",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": "Document retrieved successfully",
				"data": map[string]any{
					"content": content,
				},
			})
		case http.MethodPut:
			var req struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			files[path] = req.Content
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": "Document updated successfully",
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
}
