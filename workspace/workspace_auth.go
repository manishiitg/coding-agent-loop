package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const workspaceAPITokenEnv = "WORKSPACE_API_TOKEN"

// requireWorkspaceAPIToken protects process-execution routes when AgentWorks
// launches the workspace service with a shared token. The token is deliberately
// not exposed to coding CLI environments, preventing a sandboxed CLI from
// asking this unsandboxed service to execute a broader command.
//
// Empty-token mode is retained for standalone workspace-server compatibility.
func requireWorkspaceAPIToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(os.Getenv(workspaceAPITokenEnv))
		if expected == "" {
			c.Next()
			return
		}
		actual := strings.TrimSpace(c.GetHeader("X-Workspace-Token"))
		if actual == "" {
			if authorization := strings.TrimSpace(c.GetHeader("Authorization")); strings.HasPrefix(authorization, "Bearer ") {
				actual = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "workspace execution authorization required"})
			return
		}
		c.Next()
	}
}
