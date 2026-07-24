package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWorkspaceExecutionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(workspaceAPITokenEnv, "server-only-token")
	router := gin.New()
	router.POST("/api/execute", requireWorkspaceAPIToken(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/execute", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorizedRecorder.Code)
	}

	authorized := httptest.NewRequest(http.MethodPost, "/api/execute", nil)
	authorized.Header.Set("X-Workspace-Token", "server-only-token")
	authorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", authorizedRecorder.Code)
	}
}
