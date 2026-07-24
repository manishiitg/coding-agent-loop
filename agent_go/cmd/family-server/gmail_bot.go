package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Gmail is wired through the `gws` CLI (Google Workspace CLI) already
// authenticated on this machine — the same pattern as every other native
// host tool this app shells out to (see main.go's NATIVE_WORKSPACE note).
// There is no OAuth flow for family-server to own: gws already holds a real,
// authenticated Gmail session for whichever Google account the parent signed
// gws into on this computer. This is intentionally narrow — a connectivity
// status check plus an explicit, parent-clicked "send test email" action —
// NOT a standing tool that lets Quill send arbitrary email on its own
// initiative. Sending a message on the user's behalf always needs their
// explicit action for that specific send.

type gmailProfile struct {
	EmailAddress string `json:"emailAddress"`
}

func gmailProfileNow() (*gmailProfile, error) {
	cmd := exec.Command("gws", "gmail", "users", "getProfile", "--params", `{"userId":"me"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gws not connected: %s", strings.TrimSpace(string(out)))
	}
	var p gmailProfile
	if err := json.Unmarshal(out, &p); err != nil || p.EmailAddress == "" {
		return nil, fmt.Errorf("unexpected gws response")
	}
	return &p, nil
}

// GET /api/gmail/status
func handleGmailStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p, err := gmailProfileNow()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"connected": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"connected": true, "email": p.EmailAddress})
}

// buildRawEmail builds an RFC 2822 message and base64url-encodes it, the shape
// the Gmail API's messages.send expects in its "raw" field. When htmlBody is
// set, it emits a multipart/alternative with BOTH a text/plain fallback (body)
// and a text/html part (htmlBody) — the same shape AgentWorks' notify email
// uses. Gmail strips <style>/<head>/class CSS, so htmlBody must be inline-styled.
func buildRawEmail(to, subject, body, htmlBody string) string {
	// RFC 2047-encode the subject: header fields are 7-bit ASCII only, so any
	// non-ASCII character (an em dash, curly quotes, Hindi text, an emoji —
	// whatever the model happens to write) sitting raw in the header breaks
	// it for mail clients (mojibake or a literal "=?..." sequence shown to
	// the parent). mime.BEncoding.Encode is a no-op for already-ASCII input.
	subject = mime.BEncoding.Encode("UTF-8", subject)
	if strings.TrimSpace(htmlBody) == "" {
		msg := "To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
			"\r\n" + body
		return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(msg))
	}
	const boundary = "==SparkQuillAlt=="
	var b strings.Builder
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
	b.WriteString(body + "\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
	b.WriteString(htmlBody + "\r\n\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(b.String()))
}

// sendGmailMessage sends one email via the gws CLI — plain text, or a
// plain+HTML multipart when htmlBody is non-empty. Used by the test-send button
// and the multi-channel notification path.
func sendGmailMessage(to, subject, body, htmlBody string) error {
	raw := buildRawEmail(to, subject, body, htmlBody)
	payload, _ := json.Marshal(map[string]string{"raw": raw})
	cmd := exec.Command("gws", "gmail", "users", "messages", "send", "--params", `{"userId":"me"}`, "--json", string(payload))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// gmailNotify sends a notification email to the connected account's own
// address PLUS any extra recipients the parent configured (Pulse
// NotifyEmails, e.g. a second parent). Returns (false, nil) if Gmail isn't
// connected — so a notification fan-out treats "no Gmail" as a skip, not a
// failure. Recipients are de-duplicated case-insensitively.
func gmailNotify(subject, body, htmlBody string, extraTo []string) (sent bool, err error) {
	p, perr := gmailProfileNow()
	if perr != nil {
		return false, nil // not connected — skip, not an error
	}
	recipients := []string{p.EmailAddress}
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(p.EmailAddress)): true}
	for _, e := range extraTo {
		e = strings.TrimSpace(e)
		key := strings.ToLower(e)
		if e == "" || seen[key] {
			continue
		}
		seen[key] = true
		recipients = append(recipients, e)
	}
	if serr := sendGmailMessage(strings.Join(recipients, ", "), subject, body, htmlBody); serr != nil {
		return false, serr
	}
	return true, nil
}

// POST /api/gmail/test — sends ONE test email to the connected account's own
// address (never a third party) — a real send, but only ever run when the
// parent clicks the button themselves.
func handleGmailTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p, err := gmailProfileNow()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}
	if err := sendGmailMessage(p.EmailAddress, "SparkQuill test email",
		"This is a test email from SparkQuill, sent "+time.Now().Format("2006-01-02 15:04:05")+".\n\nIf you got this, the Gmail connector is working.", ""); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sent_to": p.EmailAddress})
}
