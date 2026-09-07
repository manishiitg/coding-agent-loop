package services

import (
	"errors"
	"testing"
)

// A successful transcription (even an empty one — a silent clip) is never
// treated as a setup problem; any other failure logs and falls back to the
// generic file description rather than asking the parent to install
// anything they already have.
func TestVoiceTranscriptionOutcome(t *testing.T) {
	if text, setup := voiceTranscriptionOutcome("  Hi, checking in.  ", nil); text != "Hi, checking in." || setup {
		t.Fatalf("successful transcription = (%q, %v)", text, setup)
	}
	if text, setup := voiceTranscriptionOutcome("", nil); text != "" || setup {
		t.Fatalf("silent clip = (%q, %v), want no setup flag", text, setup)
	}
	if text, setup := voiceTranscriptionOutcome("", ErrVoiceNotInstalled); text != "" || !setup {
		t.Fatalf("not-installed error = (%q, %v), want setupNeeded", text, setup)
	}
	if text, setup := voiceTranscriptionOutcome("", errors.New("decode failed")); text != "" || setup {
		t.Fatalf("a genuine transcription failure = (%q, %v), want no setup flag", text, setup)
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
	if got := whatsappMessageTextForMedia("", setupNeeded); got == "" {
		t.Fatal("a voice note needing setup produced no text at all — the note would be silently dropped")
	}
}
