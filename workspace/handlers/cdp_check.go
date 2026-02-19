package handlers

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/models"

	"github.com/gin-gonic/gin"
)

// CheckCdpConnection checks if a Chrome CDP port is reachable from this container.
// This is the correct place to check because agent-browser runs inside this container.
// GET /api/cdp-check?port=9222
func CheckCdpConnection(c *gin.Context) {
	portStr := c.Query("port")
	if portStr == "" {
		portStr = "9222"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid port number",
			Error:   "Port must be between 1 and 65535",
		})
		return
	}

	// Resolve host.docker.internal to its IP (Chrome rejects non-IP Host headers)
	host := "host.docker.internal"
	addrs, err := net.LookupHost(host)
	if err == nil && len(addrs) > 0 {
		host = addrs[0]
	}

	// Try TCP connection to Chrome's CDP port on the host
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"connected": false,
			"error":     fmt.Sprintf("Cannot reach CDP on port %d: %v", port, err),
		})
		return
	}
	conn.Close()

	c.JSON(http.StatusOK, gin.H{
		"connected": true,
	})
}
