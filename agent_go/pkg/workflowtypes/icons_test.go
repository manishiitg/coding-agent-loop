package workflowtypes

import (
	"strings"
	"testing"
)

func TestValidateIcon(t *testing.T) {
	if got := ValidateIcon("📊"); got != "" {
		t.Fatalf("emoji icon rejected: %q", got)
	}
	if got := ValidateIcon(""); got != "" {
		t.Fatalf("empty icon rejected: %q", got)
	}
	if got := ValidateIcon(strings.Repeat("x", 9)); got != "icon must be 8 characters or fewer" {
		t.Fatalf("long glyph error = %q", got)
	}
	image := "data:image/png;base64," + strings.Repeat("A", 1024)
	if !IsImageIcon(image) {
		t.Fatalf("uploaded image not detected")
	}
	if got := ValidateIcon(image); got != "" {
		t.Fatalf("uploaded image icon rejected: %q", got)
	}
	oversized := "data:image/png;base64," + strings.Repeat("A", MaxImageIconLength)
	if got := ValidateIcon(oversized); got == "" {
		t.Fatalf("oversized image icon accepted")
	}
	if IsImageIcon("data:image/png;base64,") {
		t.Fatalf("empty data URL detected as image")
	}
	if IsImageIcon("data:text/plain;base64,AAAA") {
		t.Fatalf("non-image data URL detected as image")
	}
}
