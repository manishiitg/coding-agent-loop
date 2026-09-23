package agentworksclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigTokenProviderSerializesRefreshAcrossProviders(t *testing.T) {
	var refreshes atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/cli/token" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "aw_cli_refresh_original" {
			t.Errorf("unexpected refresh credential")
		}
		refreshes.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"access_token":"aw_cli_rotated","refresh_token":"aw_cli_refresh_rotated","expires_in":3600}`))
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, Config{Server: s.URL, Token: "aw_cli_expired", RefreshToken: "aw_cli_refresh_original", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	providers := []func(context.Context) (string, error){ConfigTokenProvider(s.URL, path), ConfigTokenProvider(s.URL, path)}
	var wg sync.WaitGroup
	for _, provider := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := provider(context.Background())
			if err != nil || got != "aw_cli_rotated" {
				t.Errorf("provider result %q: %v", got, err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refresh requests=%d, want 1", refreshes.Load())
	}
}
