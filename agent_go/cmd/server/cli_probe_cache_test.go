package server

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodingCLIVersionCachedUntilBinaryChanges(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fakecli")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevProbe, prevLook := cliVersionProbeFn, cliLookPathFn
	var probes atomic.Int32
	cliVersionProbeFn = func(context.Context, string) (string, error) {
		probes.Add(1)
		return "1.2.3", nil
	}
	cliLookPathFn = func(string) (string, error) { return bin, nil }
	cliVersionCacheMu.Lock()
	delete(cliVersionCache, "fake")
	cliVersionCacheMu.Unlock()
	t.Cleanup(func() {
		cliVersionProbeFn, cliLookPathFn = prevProbe, prevLook
		cliVersionCacheMu.Lock()
		delete(cliVersionCache, "fake")
		cliVersionCacheMu.Unlock()
	})

	for i := 0; i < 5; i++ {
		if v, err := cachedCodingCLIVersion(context.Background(), "fake", "fakecli"); err != nil || v != "1.2.3" {
			t.Fatalf("version = %q, %v", v, err)
		}
	}
	if probes.Load() != 1 {
		t.Fatalf("probes = %d, want 1 while the binary is unchanged", probes.Load())
	}

	// An update rewrites the binary: new size and mtime invalidate the entry.
	future := time.Now().Add(time.Hour)
	if err := os.WriteFile(bin, []byte("v2-updated"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(bin, future, future)
	_, _ = cachedCodingCLIVersion(context.Background(), "fake", "fakecli")
	if probes.Load() != 2 {
		t.Fatalf("probes = %d, want a re-probe after the binary changed", probes.Load())
	}
}

func TestCLIAuthProbeServesStaleAndRefreshesOnceInBackground(t *testing.T) {
	var cache cliAuthProbeCache
	var probes atomic.Int32
	release := make(chan struct{})
	probe := func() (bool, bool, error) {
		if probes.Add(1) > 1 {
			<-release // hold the background refresh open
		}
		return true, true, nil
	}

	if a, k := cache.serve(probe, true); !a || !k {
		t.Fatalf("first probe = %v/%v", a, k)
	}
	cache.Lock()
	cache.checkedAt = time.Now().Add(-2 * cliAuthProbeTTL)
	cache.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			if a, _ := cache.serve(probe, true); !a {
				t.Error("stale read lost the cached login")
			}
			if time.Since(start) > 200*time.Millisecond {
				t.Error("stale read blocked on the probe")
			}
		}()
	}
	wg.Wait()
	close(release)
	waitForAuthRefresh(t, &cache)
	if got := probes.Load(); got != 2 {
		t.Fatalf("probes = %d, want exactly one background refresh", got)
	}
}
