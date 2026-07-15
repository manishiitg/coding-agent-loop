package browser

import (
	"strings"
	"sync"
)

// BrowserRuntimeConfig stores browser intent, not browser availability.
//
// Mode is the user/workflow configuration (auto, cdp, headless, or none) and
// CDPPorts is the configured allow-list. Whether any configured CDP endpoint is
// reachable is deliberately resolved by Executor at tool-call time and is never
// written back into this object or persisted with chat history.
type BrowserRuntimeConfig struct {
	mu       sync.RWMutex
	mode     string
	cdpPorts []int
}

func NewBrowserRuntimeConfig(mode string, cdpPorts []int) *BrowserRuntimeConfig {
	cfg := &BrowserRuntimeConfig{}
	cfg.Update(mode, cdpPorts)
	return cfg
}

// Update replaces configured browser intent without recording a resolved
// cdp/headless availability result.
func (c *BrowserRuntimeConfig) Update(mode string, cdpPorts []int) {
	if c == nil {
		return
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "headless"
	}
	c.mu.Lock()
	c.mode = mode
	c.cdpPorts = normalizeCDPPorts(cdpPorts)
	c.mu.Unlock()
}

// Snapshot returns the configured mode and candidate CDP ports. The returned
// values say nothing about current reachability.
func (c *BrowserRuntimeConfig) Snapshot() (string, []int) {
	if c == nil {
		return "", nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode, append([]int(nil), c.cdpPorts...)
}
