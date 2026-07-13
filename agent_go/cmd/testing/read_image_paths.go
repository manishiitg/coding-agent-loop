package testing

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
)

func workspaceDocsAbsoluteTestPath(relativePath string) string {
	relativePath = strings.TrimPrefix(strings.TrimSpace(relativePath), "/")
	return filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(relativePath))
}

func firstExistingWorkspaceDocsAbsoluteTestPath(candidates ...string) string {
	for _, candidate := range candidates {
		absolutePath := workspaceDocsAbsoluteTestPath(candidate)
		if _, err := os.Stat(absolutePath); err == nil {
			return absolutePath
		}
	}
	return ""
}
