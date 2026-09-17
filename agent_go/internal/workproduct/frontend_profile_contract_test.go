package workproduct

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

func TestFrontendWorkProfileVersionMatchesManifest(t *testing.T) {
	manifest, err := WorkManifest()
	if err != nil {
		t.Fatal(err)
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate workproduct test source")
	}
	frontendData := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../frontend/src/products/work/workData.ts"))
	content, err := os.ReadFile(frontendData)
	if err != nil {
		t.Fatalf("read frontend Work profile constants: %v", err)
	}
	match := regexp.MustCompile(`WORK_PROFILE_VERSION\s*=\s*([0-9]+)`).FindSubmatch(content)
	if len(match) != 2 {
		t.Fatalf("%s does not declare WORK_PROFILE_VERSION", frontendData)
	}
	frontendVersion, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("parse frontend Work profile version: %v", err)
	}
	if frontendVersion != manifest.Profile.Version {
		t.Fatalf("Crew profile version drift: frontend=%d backend=%d", frontendVersion, manifest.Profile.Version)
	}
}
