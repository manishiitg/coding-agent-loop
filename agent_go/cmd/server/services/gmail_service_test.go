package services

import (
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGmailMIME(t *testing.T) {
	dir := t.TempDir()
	att := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(att, []byte("hello attachment"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := buildGmailMIME("you@example.com", "Subject ☂", "body text", []string{att})
	if err != nil {
		t.Fatalf("buildGmailMIME: %v", err)
	}
	s := string(raw)

	if !strings.Contains(s, "To: you@example.com\r\n") {
		t.Error("missing To header")
	}
	if !strings.Contains(s, "Content-Type: multipart/mixed;") {
		t.Error("missing multipart content-type")
	}
	// Subject with a non-ASCII rune must be RFC 2047 encoded.
	if !strings.Contains(s, "Subject: =?") {
		t.Errorf("subject not encoded: %q", s)
	}

	// Parse the multipart body and confirm a text part + an attachment part.
	_, params, err := mime.ParseMediaType(headerValue(s, "Content-Type"))
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	body := s[strings.Index(s, "\r\n\r\n")+4:]
	mr := multipart.NewReader(strings.NewReader(body), params["boundary"])
	var sawText, sawAttach bool
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if strings.HasPrefix(p.Header.Get("Content-Type"), "text/plain") {
			sawText = true
		}
		if strings.Contains(p.Header.Get("Content-Disposition"), `filename="report.txt"`) {
			sawAttach = true
			if p.Header.Get("Content-Transfer-Encoding") != "base64" {
				t.Error("attachment not base64")
			}
		}
	}
	if !sawText || !sawAttach {
		t.Errorf("expected text+attachment parts, got text=%v attach=%v", sawText, sawAttach)
	}
}

// headerValue pulls a top-level header value out of the raw message.
func headerValue(msg, key string) string {
	for _, line := range strings.Split(msg, "\r\n") {
		if strings.HasPrefix(line, key+": ") {
			return strings.TrimPrefix(line, key+": ")
		}
		if line == "" {
			break
		}
	}
	return ""
}

func TestGmailSubject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "   ", "[Agent] Action needed"},
		{"single line", "Approve deploy?", "[Agent] Approve deploy?"},
		{"first line only", "Approve deploy?\nmore detail here", "[Agent] Approve deploy?"},
		{"crlf", "Need input\r\nbody", "[Agent] Need input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gmailSubject(tc.in); got != tc.want {
				t.Errorf("gmailSubject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	long := ""
	for i := 0; i < 200; i++ {
		long += "x"
	}
	got := gmailSubject(long)
	if len([]rune(got)) > len("[Agent] ")+121 { // 120 chars + ellipsis
		t.Errorf("gmailSubject did not truncate long input: len=%d", len([]rune(got)))
	}
}

func TestRenderGmailButtonOptions(t *testing.T) {
	if got := renderGmailButtonOptions(nil); got != "" {
		t.Errorf("nil opts = %q, want empty", got)
	}
	if got := renderGmailButtonOptions(&ButtonOptions{YesNoOnly: true}); got != "Options: Approve / Reject" {
		t.Errorf("default yes/no = %q", got)
	}
	if got := renderGmailButtonOptions(&ButtonOptions{YesNoOnly: true, YesLabel: "Ship", NoLabel: "Hold"}); got != "Options: Ship / Hold" {
		t.Errorf("custom yes/no = %q", got)
	}
	if got := renderGmailButtonOptions(&ButtonOptions{Options: []string{"A", "B", "C"}}); got != "Options: A / B / C" {
		t.Errorf("options = %q", got)
	}
}

func TestParseGwsMessageID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "sent"},
		{"id field", `{"id":"abc123"}`, "abc123"},
		{"messageId field", `{"messageId":"m_99"}`, "m_99"},
		{"threadId fallback", `{"threadId":"t_7"}`, "t_7"},
		{"non-json", "Message sent.", "sent"},
		{"json no id", `{"status":"ok"}`, "sent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseGwsMessageID([]byte(tc.in)); got != tc.want {
				t.Errorf("parseGwsMessageID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGmailPickRecipient(t *testing.T) {
	g := &GmailService{defaultTo: "fallback@example.com"}

	// explicit hint wins
	dest := &NotificationDestination{Gmail: &GmailDest{Email: "hint@example.com"}}
	if got := g.pickRecipient(dest); got != "hint@example.com" {
		t.Errorf("explicit hint = %q, want hint@example.com", got)
	}

	// no hint, no user -> workspace default
	if got := g.pickRecipient(nil); got != "fallback@example.com" {
		t.Errorf("default = %q, want fallback@example.com", got)
	}

	// disabled service still resolves recipient via fields (enablement gates at SendNotification)
	if got := g.pickRecipient(&NotificationDestination{}); got != "fallback@example.com" {
		t.Errorf("empty dest = %q, want fallback@example.com", got)
	}
}
