package security

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const BrowserUploadStagingDirName = "agentworks-browser-uploads"

func BrowserUploadStagingDir() string {
	return filepath.Join(string(filepath.Separator), "tmp", BrowserUploadStagingDirName)
}

// StageBrowserUpload copies one file authorized for the current request into a
// short-lived path readable by a persistent agent-browser daemon. Relative
// sources are workspace-root-relative by contract; a working-directory-relative
// fallback preserves simple filename uploads from the run Downloads directory.
func StageBrowserUpload(sourcePath, stagedPath, baseDir, workingDir string, readPaths, writePaths, blockedPaths []string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(stagedPath) == "" {
		return fmt.Errorf("browser upload source and staged paths are required")
	}

	source, sourceInfo, err := resolveBrowserUploadSource(sourcePath, baseDir, workingDir, readPaths, writePaths, blockedPaths)
	if err != nil {
		return err
	}
	staged, err := prepareBrowserUploadDestination(stagedPath)
	if err != nil {
		return err
	}

	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open browser upload source: %w", err)
	}
	defer src.Close()
	openedInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat opened browser upload source: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(sourceInfo, openedInfo) {
		return fmt.Errorf("browser upload source changed before it could be staged")
	}

	dst, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create staged browser upload: %w", err)
	}
	removeStaged := true
	defer func() {
		_ = dst.Close()
		if removeStaged {
			_ = os.Remove(staged)
		}
	}()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy browser upload into staging: %w", err)
	}
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("sync staged browser upload: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close staged browser upload: %w", err)
	}
	removeStaged = false
	return nil
}

func CleanupBrowserUploadStaging(stagedPath string) {
	staged := filepath.Clean(stagedPath)
	root := filepath.Clean(BrowserUploadStagingDir())
	parent := filepath.Dir(staged)
	if filepath.Dir(parent) != root {
		return
	}
	_ = os.Remove(staged)
	_ = os.Remove(parent)
}

func resolveBrowserUploadSource(sourcePath, baseDir, workingDir string, readPaths, writePaths, blockedPaths []string) (string, os.FileInfo, error) {
	requested := filepath.Clean(sourcePath)
	var candidates []string
	if filepath.IsAbs(requested) {
		candidates = []string{requested}
	} else {
		candidates = append(candidates, filepath.Join(baseDir, requested))
		if strings.TrimSpace(workingDir) != "" {
			fallback := filepath.Join(workingDir, requested)
			if fallback != candidates[0] {
				candidates = append(candidates, fallback)
			}
		}
	}

	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("stat browser upload source: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("browser upload source must be a regular non-symlink file")
		}
		canonical := canonicalPath(candidate)
		if pathCoveredByGuard(canonical, baseDir, blockedPaths) {
			return "", nil, fmt.Errorf("browser upload source is blocked for reads")
		}
		if !pathAllowedForBrowserUploadRead(canonical, baseDir, readPaths, writePaths) {
			return "", nil, fmt.Errorf("browser upload source is not covered by this session's read paths")
		}
		return canonical, info, nil
	}
	return "", nil, fmt.Errorf("browser upload source %q does not exist", sourcePath)
}

func prepareBrowserUploadDestination(stagedPath string) (string, error) {
	if !filepath.IsAbs(stagedPath) {
		return "", fmt.Errorf("browser upload staging path must be absolute")
	}
	staged := filepath.Clean(stagedPath)
	root := filepath.Clean(BrowserUploadStagingDir())
	parent := filepath.Dir(staged)
	if filepath.Dir(parent) != root || filepath.Base(staged) == "." || filepath.Base(staged) == ".." {
		return "", fmt.Errorf("browser upload staging path must be one named file inside one managed staging slot")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", fmt.Errorf("create browser upload staging root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("browser upload staging root must be a real directory")
	}
	if err := os.Chmod(root, 0700); err != nil {
		return "", fmt.Errorf("secure browser upload staging root: %w", err)
	}
	if err := os.Mkdir(parent, 0700); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("create browser upload staging slot: %w", err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("browser upload staging slot must be a real directory")
	}
	if canonicalPath(filepath.Dir(parent)) != canonicalPath(root) {
		return "", fmt.Errorf("browser upload staging slot escapes managed staging root")
	}
	return staged, nil
}

func pathAllowedForBrowserUploadRead(path, base string, readPaths, writePaths []string) bool {
	for _, allowed := range append(append([]string(nil), readPaths...), writePaths...) {
		root := allowed
		if !filepath.IsAbs(root) {
			root = filepath.Join(base, root)
		}
		if pathWithin(path, canonicalPath(root)) {
			return true
		}
	}
	return false
}
