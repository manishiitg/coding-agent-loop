package workflowtypes

import "strings"

// MaxImageIconLength caps an uploaded identity icon stored inline in a
// manifest. The UI downscales uploads to a 128px thumbnail, so real icons
// stay far below this; the cap only guards against pasted bulk.
const MaxImageIconLength = 100000

var imageIconPrefixes = []string{
	"data:image/png;base64,",
	"data:image/jpeg;base64,",
	"data:image/gif;base64,",
	"data:image/webp;base64,",
}

// IsImageIcon reports whether an identity icon value is an uploaded image
// (a base64 data URL) rather than an emoji or short glyph.
func IsImageIcon(icon string) bool {
	trimmed := strings.TrimSpace(icon)
	for _, prefix := range imageIconPrefixes {
		if strings.HasPrefix(trimmed, prefix) && len(trimmed) > len(prefix) {
			return true
		}
	}
	return false
}

// ValidateIcon accepts an emoji/short glyph (8 characters or fewer) or an
// uploaded image data URL within MaxImageIconLength. It returns a non-empty
// reason when the value is invalid.
func ValidateIcon(icon string) string {
	trimmed := strings.TrimSpace(icon)
	if IsImageIcon(trimmed) {
		if len(trimmed) > MaxImageIconLength {
			return "icon image is too large"
		}
		return ""
	}
	if len([]rune(trimmed)) > 8 {
		return "icon must be 8 characters or fewer"
	}
	return ""
}
