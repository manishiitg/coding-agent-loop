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

func TestManagedHeadlessMediaFlags(t *testing.T) {
	for _, profile := range []string{"", "/data/browser-profile"} {
		t.Setenv(ProfileEnv, profile)
		args := HeadlessArgs()
		launch := ""
		for i, arg := range args {
			if arg == "--args" && i+1 < len(args) {
				launch = args[i+1]
			}
		}
		flags := strings.Split(launch, ",")
		for _, want := range []string{"--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"} {
			count := 0
			for _, flag := range flags {
				if flag == want {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("profile %q: expected one %s in %v", profile, want, args)
			}
		}
	}
}
