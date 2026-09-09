package security

import (
	"fmt"
	"path/filepath"
)

// AuthorizeBrowserCaptureDirectory checks the complete capture directory,
// including canonical ancestors and denied descendants, before trusted exports.
func AuthorizeBrowserCaptureDirectory(directory, baseDir string, allowed, blocked []string) error {
	base := canonicalPath(baseDir)
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(base, directory)
	}
	directory = canonicalPath(directory)
	if directory == base || !pathWithin(directory, base) || !pathAllowedForArtifactWrite(directory, base, allowed) {
		return fmt.Errorf("capture directory is outside granted workspace paths")
	}
	for _, value := range blocked {
		if !filepath.IsAbs(value) {
			value = filepath.Join(base, value)
		}
		value = canonicalPath(value)
		if pathWithin(directory, value) || pathWithin(value, directory) {
			return fmt.Errorf("capture directory intersects a blocked workspace path")
		}
	}
	return nil
}
