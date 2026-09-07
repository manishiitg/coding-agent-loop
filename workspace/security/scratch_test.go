package security

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScratchSupportsBrowserSocketAndIndependentCleanup(t *testing.T) {
	a, cleanupA, err := allocateScratch()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupA()
	b, cleanupB, err := allocateScratch()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupB()
	if a == b {
		t.Fatal("commands share scratch")
	}
	profile := filepath.Join(a, ".org.chromium.Chromium."+strings.Repeat("x", 6))
	if err := os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(profile, "SingletonSocket"))
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	cleanupA()
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("scratch was not removed")
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatal("other command scratch removed")
	}
}
