package services

import (
	"errors"
	"testing"
)

// A successful transcription (even an empty one — a silent clip) is never
// treated as a setup problem. Missing setup and real STT failures remain
// distinct so the connector can answer directly without invoking the agent.
func TestVoiceTranscriptionOutcome(t *testing.T) {
	if text, setup, failed := voiceTranscriptionOutcome("  Hi, checking in.  ", nil); text != "Hi, checking in." || setup || failed {
		t.Fatalf("successful transcription = (%q, %v, %v)", text, setup, failed)
	}
	if text, setup, failed := voiceTranscriptionOutcome("", nil); text != "" || setup || failed {
		t.Fatalf("silent clip = (%q, %v, %v), want no error flags", text, setup, failed)
	}
	if text, setup, failed := voiceTranscriptionOutcome("", ErrVoiceNotInstalled); text != "" || !setup || failed {
		t.Fatalf("not-installed error = (%q, %v, %v), want setupNeeded", text, setup, failed)
	}
	if text, setup, failed := voiceTranscriptionOutcome("", errors.New("decode failed")); text != "" || setup || !failed {
		t.Fatalf("a genuine transcription failure = (%q, %v, %v), want failed", text, setup, failed)
	}
}

// A transcribed voice note reads exactly like a typed message — never the
// generic "WhatsApp upload received: Type: audio…" description — merged
// with anything the sender also typed. Everything else (no transcript: a
// photo, a document, a voice note that failed or needs setup) keeps the
// generic description.
func TestWhatsappMessageTextForMedia(t *testing.T) {
	voice := &whatsappDownloadedMedia{Kind: "audio", Transcript: "Can you check the maths assessment."}
	if got := whatsappMessageTextForMedia("", voice); got != "Can you check the maths assessment." {
		t.Fatalf("transcript only = %q", got)
	}
	if got := whatsappMessageTextForMedia("also:", voice); got != "also:\n\nCan you check the maths assessment." {
		t.Fatalf("typed text plus transcript = %q", got)
	}

	photo := &whatsappDownloadedMedia{Kind: "image", FilePath: "inbox/photo.jpg", FileName: "photo.jpg", MimeType: "image/jpeg"}
	if got := whatsappMessageTextForMedia("", photo); got == "" || got == "Can you check the maths assessment." {
		t.Fatalf("a photo with no transcript = %q, want the generic media description", got)
	}

	setupNeeded := &whatsappDownloadedMedia{Kind: "audio", VoiceSetupNeeded: true}
	if got := whatsappMessageTextForMedia("", setupNeeded); got != "" {
		t.Fatalf("voice note needing setup leaked into agent text: %q", got)
	}

	failed := &whatsappDownloadedMedia{Kind: "audio", VoiceTranscriptionFailed: true, FilePath: "inbox/note.oga"}
	if got := whatsappMessageTextForMedia("", failed); got != "" {
		t.Fatalf("failed voice note leaked into agent text: %q", got)
	}
	if got := whatsappMessageTextForMedia("caption must not bypass STT", failed); got != "" {
		t.Fatalf("failed voice note caption bypassed STT failure: %q", got)
	}
}
