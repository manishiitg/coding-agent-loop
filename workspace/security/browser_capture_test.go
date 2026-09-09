package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureDirectoryPermissions(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Workflow/demo"), 0700)
	os.Symlink(t.TempDir(), filepath.Join(root, "Workflow/demo/escape"))
	for _, tt := range []struct {
		name, target     string
		allowed, blocked []string
		want             bool
	}{
		{"workflow", "Workflow/demo/browser-recordings", []string{"Workflow/demo"}, nil, true},
		{"other workflow", "Workflow/other/browser-recordings", []string{"Workflow/demo"}, nil, false},
		{"no grants", "Workflow/demo/browser-recordings", nil, nil, false},
		{"blocked parent", "Workflow/demo/browser-recordings", []string{"Workflow/demo"}, []string{"Workflow/demo"}, false},
		{"blocked child", "Workflow/demo/browser-recordings", []string{"Workflow/demo"}, []string{"Workflow/demo/browser-recordings/private"}, false},
		{"symlink escape", "Workflow/demo/escape/capture", []string{"Workflow/demo"}, nil, false},
		{"step output", "Workflow/demo/runs/one/browser-recordings", []string{"Workflow/demo/runs/one"}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := AuthorizeBrowserCaptureDirectory(tt.target, root, tt.allowed, tt.blocked)
			if (err == nil) != tt.want {
				t.Fatalf("got %v want allowed=%v", err, tt.want)
			}
		})
	}
}
