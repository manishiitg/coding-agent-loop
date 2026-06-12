package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func cdpTestHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portStr, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return host, port
}

func TestCheckChromeCdpVersionAcceptsDevToolsMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("path = %q, want /json/version", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"Browser":"Chrome/136.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser/1"}`)
	}))
	t.Cleanup(server.Close)

	host, port := cdpTestHostPort(t, server.URL)
	result, err := checkChromeCdpVersion(host, port)
	if err != nil {
		t.Fatalf("checkChromeCdpVersion() error = %v", err)
	}
	if result["connected"] != true {
		t.Fatalf("connected = %#v, want true", result["connected"])
	}
}

func TestCheckChromeCdpVersionRejectsPlainTCPHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(server.Close)

	host, port := cdpTestHostPort(t, server.URL)
	if _, err := checkChromeCdpVersion(host, port); err == nil {
		t.Fatal("checkChromeCdpVersion() error = nil, want metadata error")
	}
}
