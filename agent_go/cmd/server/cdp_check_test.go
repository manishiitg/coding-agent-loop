package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func testServerPort(t *testing.T, rawURL string) int {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	_, portStr, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return port
}

func TestCheckLocalChromeCdpVersionAcceptsDevToolsMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("path = %q, want /json/version", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"Browser":"Chrome/136.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser/1"}`)
	}))
	t.Cleanup(server.Close)

	result, err := checkLocalChromeCdpVersion(testServerPort(t, server.URL))
	if err != nil {
		t.Fatalf("checkLocalChromeCdpVersion() error = %v", err)
	}
	if result["connected"] != true {
		t.Fatalf("connected = %#v, want true", result["connected"])
	}
}

func TestCheckLocalChromeCdpVersionRejectsPlainTCPHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(server.Close)

	if _, err := checkLocalChromeCdpVersion(testServerPort(t, server.URL)); err == nil {
		t.Fatal("checkLocalChromeCdpVersion() error = nil, want metadata error")
	}
}
