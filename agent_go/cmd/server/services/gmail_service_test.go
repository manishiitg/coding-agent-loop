package services

import (
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildGmailMIME(t *testing.T) {
	dir := t.TempDir()
	att := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(att, []byte("hello attachment"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := buildGmailMIME("you@example.com", []string{"cc@example.com"}, "Subject ☂", "body text", "", []string{att})
	if err != nil {
		t.Fatalf("buildGmailMIME: %v", err)
	}
	s := string(raw)

	if !strings.Contains(s, "To: you@example.com\r\n") {
		t.Error("missing To header")
	}
	if !strings.Contains(s, "Cc: cc@example.com\r\n") {
		t.Error("missing Cc header")
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

func TestBuildGmailMIMEHTML(t *testing.T) {
	raw, err := buildGmailMIME("you@example.com", nil, "Subj", "plain fallback", "<h1>Hello</h1>", nil)
	if err != nil {
		t.Fatalf("buildGmailMIME html: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, "multipart/alternative") {
		t.Error("HTML email should use multipart/alternative")
	}
	if !strings.Contains(s, "text/html; charset=UTF-8") {
		t.Error("missing text/html part")
	}
	if !strings.Contains(s, "<h1>Hello</h1>") {
		t.Error("missing HTML body content")
	}
	if !strings.Contains(s, "plain fallback") {
		t.Error("missing plain-text fallback part")
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
	if len([]rune(got)) > len("[Agent] ")+151 { // 150 runes + ellipsis
		t.Errorf("gmailSubject did not truncate long input: len=%d", len([]rune(got)))
	}

	// Rune-safe truncation: a long run of multi-byte chars must never be cut
	// mid-rune into invalid UTF-8 (the bug that produced a broken subject char).
	emDashes := strings.Repeat("—", 200) // 3 bytes each
	if s := gmailSubject(emDashes); !utf8.ValidString(s) {
		t.Errorf("gmailSubject produced invalid UTF-8 on multi-byte input: %q", s)
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
	g := &GmailService{
		defaultTo: "fallback@example.com",
		config: &GmailConfig{
			DefaultTo:         "fallback@example.com",
			AllowedRecipients: []string{"fallback@example.com", "hint@example.com"},
		},
	}

	// explicit hint wins
	dest := &NotificationDestination{Gmail: &GmailDest{Email: "hint@example.com"}}
	if got, err := g.pickRecipient(dest); err != nil || got != "hint@example.com" {
		t.Fatalf("explicit hint = %q, err=%v, want hint@example.com", got, err)
	}

	// explicit hint outside the allowlist is blocked before any send happens
	if got, err := g.pickRecipient(&NotificationDestination{Gmail: &GmailDest{Email: "outside@example.com"}}); err == nil || got != "" {
		t.Fatalf("outside hint = %q, err=%v, want blocked recipient error", got, err)
	}

	// no hint, no user -> workspace default
	if got, err := g.pickRecipient(nil); err != nil || got != "fallback@example.com" {
		t.Errorf("default = %q, err=%v, want fallback@example.com", got, err)
	}

	// disabled service still resolves recipient via fields (enablement gates at SendNotification)
	if got, err := g.pickRecipient(&NotificationDestination{}); err != nil || got != "fallback@example.com" {
		t.Errorf("empty dest = %q, err=%v, want fallback@example.com", got, err)
	}
}

func TestGmailValidateCCRecipients(t *testing.T) {
	g := &GmailService{
		defaultTo: "fallback@example.com",
		config: &GmailConfig{
			DefaultTo:         "fallback@example.com",
			AllowedRecipients: []string{"fallback@example.com", "cc@example.com", "other@example.com"},
		},
	}

	got, err := g.validateCCRecipients([]string{" CC@Example.com ", "other@example.com,cc@example.com"})
	if err != nil {
		t.Fatalf("validateCCRecipients returned error: %v", err)
	}
	want := []string{"cc@example.com", "other@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("validateCCRecipients = %#v, want %#v", got, want)
	}

	if got, err := g.validateCCRecipients([]string{"outside@example.com"}); err == nil || len(got) != 0 {
		t.Fatalf("outside cc = %#v, err=%v, want blocked recipient error", got, err)
	}
}

func TestNormalizeGmailConfigIncludesDefaultAndDedupes(t *testing.T) {
	cfg := normalizeGmailConfig(&GmailConfig{
		DefaultTo:         " Owner@Example.COM ",
		AllowedRecipients: []string{"other@example.com", "owner@example.com", "a@b.com, C@D.com", "other@example.com"},
	})

	wantAllowed := []string{"owner@example.com", "other@example.com", "a@b.com", "c@d.com"}
	if cfg.DefaultTo != "Owner@Example.COM" {
		t.Fatalf("DefaultTo = %q, want trimmed original case", cfg.DefaultTo)
	}
	if !reflect.DeepEqual(cfg.AllowedRecipients, wantAllowed) {
		t.Fatalf("AllowedRecipients = %#v, want %#v", cfg.AllowedRecipients, wantAllowed)
	}
}

func TestGmailAllowlistDefaultsToDefaultRecipient(t *testing.T) {
	g := &GmailService{defaultTo: "fallback@example.com"}

	if got, err := g.pickRecipient(nil); err != nil || got != "fallback@example.com" {
		t.Fatalf("default = %q, err=%v, want fallback@example.com", got, err)
	}

	if got, err := g.pickRecipient(&NotificationDestination{Gmail: &GmailDest{Email: "hint@example.com"}}); err == nil || got != "" {
		t.Fatalf("outside hint = %q, err=%v, want blocked recipient error", got, err)
	}
}
