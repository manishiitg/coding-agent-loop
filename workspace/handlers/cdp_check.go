package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

	// Resolve host.docker.internal to its IP (Chrome rejects non-IP Host headers).
	// Keep localhost fallbacks for native workspace-server development.
	hosts := []string{"host.docker.internal", "127.0.0.1", "localhost"}
	addrs, err := net.LookupHost("host.docker.internal")
	if err == nil && len(addrs) > 0 {
		hosts = append(addrs, hosts...)
	}

	var lastErr error
	for _, host := range hosts {
		result, err := checkChromeCdpVersion(host, port)
		if err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
		lastErr = err
	}

	c.JSON(http.StatusOK, gin.H{
		"connected": false,
		"error":     fmt.Sprintf("Cannot reach Chrome CDP /json/version on port %d: %v", port, lastErr),
	})
}

func checkChromeCdpVersion(host string, port int) (gin.H, error) {
	endpoint := (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/json/version",
	}).String()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
	}

	var payload struct {
		Browser              string `json:"Browser"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.Browser) == "" && strings.TrimSpace(payload.WebSocketDebuggerURL) == "" {
		return nil, fmt.Errorf("%s did not return Chrome DevTools metadata", endpoint)
	}

	return gin.H{
		"connected": true,
		"browser":   payload.Browser,
		"endpoint":  endpoint,
	}, nil
}
