// Package browserconfig keeps all managed-browser launch paths consistent.
package browserconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const SharedSession = "shared-browser"
const ProfileEnv = "AGENT_BROWSER_SHARED_PROFILE"

func SharedProfile() string {
	profile := strings.TrimSpace(os.Getenv(ProfileEnv))
	if !filepath.IsAbs(profile) || filepath.Clean(profile) == string(filepath.Separator) {
		return ""
	}
	return filepath.Clean(profile)
}

func SharedEnabled() bool { return SharedProfile() != "" }

// HeadlessArgs must be used for automation, viewer actions and recording alike.
// Shared Chrome keeps its native user agent and uses one persistent profile.
func HeadlessArgs() []string { return HeadlessArgsForSession("") }

// IsUserSession recognizes canonical managed user/guest browsers, including a deployment prefix.
var userSession = regexp.MustCompile(`^(?:[A-Za-z0-9_-]+--)?(?:user|guest)-[a-f0-9]{16}--browser$`)

func IsUserSession(session string) bool { return userSession.MatchString(session) }

func HeadlessArgsForSession(session string) []string {
	// Servers have no physical media devices. Keep these options identical in
	// automation, live controls, capture and the persistent browser supervisor.
	const mediaArgs = ",--use-fake-device-for-media-stream,--use-fake-ui-for-media-stream"
	if profile := SharedProfile(); profile != "" {
		if IsUserSession(session) {
			profile = filepath.Join(profile+"-users", session)
		}
		return []string{"--profile", profile, "--idle-timeout", "0", "--args", "--no-sandbox,--disable-gpu,--disable-blink-features=AutomationControlled,--lang=en-US,--restore-last-session" + mediaArgs}
	}
	return []string{"--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "--args", "--no-sandbox,--disable-gpu,--disable-blink-features=AutomationControlled" + mediaArgs}
}
