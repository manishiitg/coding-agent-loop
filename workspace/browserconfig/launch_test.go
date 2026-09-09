package browserconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestSharedProfileLaunchIdentity(t *testing.T) {
	t.Setenv(ProfileEnv, "/data/browser-profile")
	args := HeadlessArgs()
	if !SharedEnabled() || SharedProfile() != "/data/browser-profile" {
		t.Fatal("shared profile disabled")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--user-agent") || !strings.Contains(joined, "--profile /data/browser-profile") || !strings.Contains(joined, "--idle-timeout 0") {
		t.Fatalf("wrong shared launch: %v", args)
	}
	if !reflect.DeepEqual(args, HeadlessArgs()) {
		t.Fatal("unstable launch options")
	}
	t.Setenv(ProfileEnv, "")
	if SharedEnabled() || !strings.Contains(strings.Join(HeadlessArgs(), " "), "--user-agent") {
		t.Fatal("isolated behavior changed")
	}
	for _, path := range []string{"relative", "/", "/tmp/.."} {
		t.Setenv(ProfileEnv, path)
		if SharedEnabled() {
			t.Fatalf("unsafe profile %q", path)
		}
	}
}
