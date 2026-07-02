package virtualtools

import "testing"

// TestGmailContentHTMLRescue verifies HTML put in email_body (a common agent
// mistake) is detected and routed to the HTML body, and that email_html works.
func TestGmailContentHTMLRescue(t *testing.T) {
	// HTML in email_body → rescued into HTMLBody, plain body cleared.
	gc, err := gmailContentFromArgs(map[string]interface{}{
		"email_subject": "Test",
		"email_body":    "<html><body><h2>Hi</h2><p>hello</p></body></html>",
	})
	if err != nil || gc == nil {
		t.Fatalf("unexpected: gc=%v err=%v", gc, err)
	}
	if gc.HTMLBody == "" {
		t.Error("HTML in email_body was not rescued into HTMLBody")
	}
	if gc.Body != "" {
		t.Errorf("plain Body should be cleared when body was HTML, got %q", gc.Body)
	}

	// Explicit email_html is used as-is; plain email_body stays plain.
	gc2, _ := gmailContentFromArgs(map[string]interface{}{
		"email_body": "plain text update",
		"email_html": "<h1>Designed</h1>",
	})
	if gc2 == nil || gc2.HTMLBody != "<h1>Designed</h1>" || gc2.Body != "plain text update" {
		t.Errorf("explicit html/body wrong: %+v", gc2)
	}

	// Plain text only → no HTML.
	gc3, _ := gmailContentFromArgs(map[string]interface{}{"email_body": "just a normal sentence."})
	if gc3 == nil || gc3.HTMLBody != "" {
		t.Errorf("plain text misdetected as HTML: %+v", gc3)
	}
	gc4, _ := gmailContentFromArgs(map[string]interface{}{
		"email_cc": []interface{}{" CC@Example.com ", "other@example.com,cc@example.com"},
	})
	if gc4 == nil {
		t.Fatal("email_cc should create GmailContent")
	}
	if len(gc4.CC) != 2 || gc4.CC[0] != "cc@example.com" || gc4.CC[1] != "other@example.com" {
		t.Fatalf("email_cc parsed as %#v, want cc@example.com and other@example.com", gc4.CC)
	}

	// Deterministic: prose that merely contains "<" must NOT be treated as HTML.
	for _, prose := range []string{"a < b is true", "score <3", "use x<y here", "1 < 2 < 3"} {
		if gc, _ := gmailContentFromArgs(map[string]interface{}{"email_body": prose}); gc != nil && gc.HTMLBody != "" {
			t.Errorf("prose %q wrongly detected as HTML", prose)
		}
	}

}
