package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultCDPPort = 9222

type cdpReachabilityCacheEntry struct {
	connected bool
	expiresAt time.Time
}

var runtimeCDPReachabilityCache = struct {
	sync.Mutex
	entries map[int]cdpReachabilityCacheEntry
}{entries: make(map[int]cdpReachabilityCacheEntry)}

func configuredCDPPorts(primary *int, additional []int) []int {
	return getCdpPorts(QueryRequest{
		BrowserMode: "cdp",
		CdpPort:     primary,
		CdpPorts:    additional,
	})
}

func resolveRuntimeBrowserModeWithPorts(ctx context.Context, mode string, requestedPorts []int) (string, []int) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	requestedPorts = configuredCDPPorts(nil, requestedPorts)
	switch mode {
	case "auto":
		if reachable := reachableCDPPorts(ctx, requestedPorts); len(reachable) > 0 {
			return "cdp", reachable
		}
		return "headless", nil
	case "cdp":
		return "cdp", requestedPorts
	case "headless", "none":
		return mode, nil
	default:
		return mode, nil
	}
}

func applyRuntimeBrowserMode(ctx context.Context, req QueryRequest) QueryRequest {
	requestedMode := strings.ToLower(strings.TrimSpace(req.BrowserMode))
	if requestedMode != "auto" {
		return req
	}

	requestedPorts := configuredCDPPorts(req.CdpPort, req.CdpPorts)
	resolvedMode, ports := resolveRuntimeBrowserModeWithPorts(ctx, requestedMode, requestedPorts)
	req.BrowserMode = resolvedMode
	enabled := resolvedMode == "cdp" || resolvedMode == "headless"
	req.EnableBrowserAccess = &enabled
	if resolvedMode == "cdp" {
		req.CdpPorts = ports
		req.CdpPort = &ports[0]
		log.Printf("[BROWSER_AUTO] Chrome CDP is reachable; resolved auto mode to cdp on port(s) %v", ports)
	} else {
		req.CdpPort = nil
		req.CdpPorts = nil
		log.Printf("[BROWSER_AUTO] Chrome CDP is unavailable; resolved auto mode to headless")
	}
	return req
}

func reachableCDPPorts(ctx context.Context, ports []int) []int {
	type result struct {
		index     int
		connected bool
	}
	results := make(chan result, len(ports))
	var wg sync.WaitGroup
	for index, port := range ports {
		wg.Add(1)
		go func(index, port int) {
			defer wg.Done()
			results <- result{index: index, connected: workspaceCDPReachable(ctx, port)}
		}(index, port)
	}
	wg.Wait()
	close(results)

	connected := make([]bool, len(ports))
	for item := range results {
		connected[item.index] = item.connected
	}
	reachable := make([]int, 0, len(ports))
	for index, port := range ports {
		if connected[index] {
			reachable = append(reachable, port)
		}
	}
	return reachable
}

func workspaceCDPReachable(ctx context.Context, port int) bool {
	now := time.Now()
	runtimeCDPReachabilityCache.Lock()
	if cached, ok := runtimeCDPReachabilityCache.entries[port]; ok && now.Before(cached.expiresAt) {
		runtimeCDPReachabilityCache.Unlock()
		return cached.connected
	}
	runtimeCDPReachabilityCache.Unlock()

	connected := checkWorkspaceCDPReachability(ctx, port)
	runtimeCDPReachabilityCache.Lock()
	runtimeCDPReachabilityCache.entries[port] = cdpReachabilityCacheEntry{
		connected: connected,
		expiresAt: now.Add(5 * time.Second),
	}
	runtimeCDPReachabilityCache.Unlock()
	return connected
}

func checkWorkspaceCDPReachability(ctx context.Context, port int) bool {
	checkURL := strings.TrimRight(getWorkspaceAPIURL(), "/") + "/api/cdp-check?port=" + url.QueryEscape(strconv.Itoa(port))
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	var payload struct {
		Connected bool `json:"connected"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		log.Printf("[BROWSER_AUTO] Could not decode workspace CDP check for port %d: %v", port, err)
		return false
	}
	return payload.Connected
}

func resetRuntimeCDPReachabilityCacheForTest() {
	runtimeCDPReachabilityCache.Lock()
	defer runtimeCDPReachabilityCache.Unlock()
	runtimeCDPReachabilityCache.entries = make(map[int]cdpReachabilityCacheEntry)
}
