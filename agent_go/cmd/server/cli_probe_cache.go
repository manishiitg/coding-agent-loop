package server

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"

	llm "github.com/manishiitg/multi-llm-provider-go"
)

// /api/llm-config/providers ran every installed coding CLI's --version on
// each request (seconds on a small box). A CLI's version only changes when
// its binary does, so cache it keyed on the resolved binary path plus its
// mtime and size: an npm/installer update changes the stat and is re-probed
// automatically. Failures are not cached.
type cliVersionCacheEntry struct {
	path    string
	modTime time.Time
	size    int64
	version string
}

var (
	cliVersionCacheMu sync.Mutex
	cliVersionCache   = map[string]cliVersionCacheEntry{}

	cliVersionProbeFn = func(ctx context.Context, provider string) (string, error) {
		return llm.CodingAgentCLIVersion(ctx, llm.Provider(provider))
	}
	cliLookPathFn = exec.LookPath
	cliStatFn     = os.Stat
)

func cachedCodingCLIVersion(ctx context.Context, provider, binary string) (string, error) {
	resolved, err := cliLookPathFn(binary)
	if err != nil {
		return cliVersionProbeFn(ctx, provider) // fast not-found error with install hint
	}
	info, err := cliStatFn(resolved) // follows symlinks, so a re-pointed shim changes the key
	if err != nil {
		return cliVersionProbeFn(ctx, provider)
	}
	cliVersionCacheMu.Lock()
	entry, ok := cliVersionCache[provider]
	cliVersionCacheMu.Unlock()
	if ok && entry.path == resolved && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
		return entry.version, nil
	}
	version, err := cliVersionProbeFn(ctx, provider)
	if err != nil {
		return "", err
	}
	cliVersionCacheMu.Lock()
	cliVersionCache[provider] = cliVersionCacheEntry{path: resolved, modTime: info.ModTime(), size: info.Size(), version: version}
	cliVersionCacheMu.Unlock()
	return version, nil
}

// cliAuthProbeTTL is how long a login probe result is considered fresh. After
// it, callers still get the last known state immediately while one background
// refresh runs; only the first-ever probe (or one after an explicit
// invalidation, e.g. a login) blocks the request.
const cliAuthProbeTTL = 30 * time.Second

// serve returns the cached state, refreshing it in the background when stale.
// keepConfirmedOnError preserves a previously confirmed login when a refresh
// fails inconclusively (a timeout is not evidence of logging out).
func (c *cliAuthProbeCache) serve(probe func() (bool, bool, error), keepConfirmedOnError bool) (bool, bool) {
	c.Lock()
	if !c.checkedAt.IsZero() {
		authenticated, conclusive := c.authenticated, c.conclusive
		if time.Since(c.checkedAt) >= cliAuthProbeTTL && !c.refreshing {
			c.refreshing = true
			go func() {
				a, k, err := probe()
				c.Lock()
				c.storeLocked(a, k, err, keepConfirmedOnError)
				c.refreshing = false
				c.Unlock()
			}()
		}
		c.Unlock()
		return authenticated, conclusive
	}
	// First probe: serialise concurrent callers behind this one.
	a, k, err := probe()
	c.storeLocked(a, k, err, keepConfirmedOnError)
	authenticated, conclusive := c.authenticated, c.conclusive
	c.Unlock()
	return authenticated, conclusive
}

func (c *cliAuthProbeCache) storeLocked(authenticated, conclusive bool, err error, keepConfirmedOnError bool) {
	if keepConfirmedOnError && err != nil && !conclusive && c.conclusive && c.authenticated {
		authenticated, conclusive = true, true
	}
	c.checkedAt = time.Now()
	c.authenticated = authenticated
	c.conclusive = conclusive
}
