package security

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Isolator struct {
	ReadPaths    []string
	WritePaths   []string
	BlockedPaths []string // Paths to explicitly deny READ AND WRITE (deny-list, takes precedence)
	// BlockedWritePaths denies writes only — reads still pass through. Used for paths
	// the agent should inspect but never modify (e.g. a workflow's planning/ folder).
	// macOS: emitted as `(deny file-write*)` scoped to these subpaths.
	// Linux: bind-mounted read-only over the writable subtree so writes return EROFS.
	BlockedWritePaths []string
	WorkDir           string
	BaseDir           string // Workspace base directory (default: /app/workspace-docs)
	// StrictAllowlist flips the sandbox profile from "allow everything except
	// one denied directory" (the default below — a reasonable model for
	// AgentWorks' own workflows, which trust the rest of the host since it's
	// the same developer's machine) to a genuine deny-by-default allow-list:
	// only ReadPaths/WritePaths and the minimal system paths needed to exec a
	// shell are visible. Use this for untrusted callers (e.g. a child's own
	// chat) where the real home directory, credentials, and everything else
	// on the machine must be invisible, not just the app's own project root.
	// Opt-in and additive: every existing caller that leaves this false gets
	// byte-for-byte the same profile as before.
	StrictAllowlist bool
}

const defaultBaseDir = "/app/workspace-docs"

// getBaseDir returns the configured base directory or the default
func (iso *Isolator) getBaseDir() string {
	if iso.BaseDir != "" {
		return iso.BaseDir
	}
	return defaultBaseDir
}

// canonicalPath returns the kernel-visible spelling of a path. macOS exposes
// /var and /tmp through symlinks to /private; sandbox-exec rules written with
// the unresolved spelling do not protect the resolved path.
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved
	}

	// Write targets may not exist yet. Resolve the nearest existing ancestor,
	// then append the missing suffix without changing its meaning.
	ancestor := absPath
	var suffix []string
	for {
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return filepath.Clean(absPath)
		}
		suffix = append([]string{filepath.Base(ancestor)}, suffix...)
		ancestor = parent
		if resolved, err := filepath.EvalSymlinks(ancestor); err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Join(parts...)
		}
	}
}

func sandboxQuoted(path string) string {
	path = strings.ReplaceAll(path, `\`, `\\`)
	return strings.ReplaceAll(path, `"`, `\"`)
}

func (iso *Isolator) sandboxPath(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(iso.getBaseDir(), strings.TrimSuffix(path, "/"))
	}
	return canonicalPath(path)
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// sandboxAllowedPath resolves an allow-list entry without letting a path that
// was granted inside the workspace escape through a symlink. Absolute paths
// outside BaseDir remain valid because callers use them for explicit host
// grants such as Downloads access.
func (iso *Isolator) sandboxAllowedPath(path string) (string, bool) {
	baseDir := filepath.Clean(iso.getBaseDir())
	canonicalBaseDir := canonicalPath(baseDir)
	workspaceScoped := !filepath.IsAbs(path)

	resolvedPath := path
	if workspaceScoped {
		resolvedPath = filepath.Join(baseDir, strings.TrimSuffix(path, "/"))
	} else {
		resolvedPath = filepath.Clean(path)
		workspaceScoped = pathWithin(resolvedPath, baseDir) || pathWithin(resolvedPath, canonicalBaseDir)
	}

	canonicalAllowedPath := canonicalPath(resolvedPath)
	if workspaceScoped && !pathWithin(canonicalAllowedPath, canonicalBaseDir) {
		return "", false
	}
	return canonicalAllowedPath, true
}

// ExecuteIsolated runs a command with filesystem restrictions
// Uses unshare on Linux, sandbox-exec on macOS
// Returns the command, a cleanup function, and an error
func (iso *Isolator) ExecuteIsolated(ctx context.Context, command string, args []string) (*exec.Cmd, func(), error) {
	if runtime.GOOS == "darwin" {
		// macOS: Use sandbox-exec with sandbox profile
		return iso.executeIsolatedMacOS(ctx, command, args)
	}

	// Linux: Use unshare with mount namespaces
	return iso.executeIsolatedLinux(ctx, command, args)
}

// executeIsolatedLinux uses unshare for filesystem isolation on Linux
func (iso *Isolator) executeIsolatedLinux(ctx context.Context, command string, args []string) (*exec.Cmd, func(), error) {
	// Generate mount script for isolation
	mountScript := iso.generateMountScript(command, args)

	// Create unique temp script file (PID + timestamp to avoid collisions)
	scriptPath := filepath.Join("/tmp", fmt.Sprintf("exec-%d-%d.sh", os.Getpid(), time.Now().UnixNano()))

	// Write script with error handling
	if err := os.WriteFile(scriptPath, []byte(mountScript), 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to write mount script: %w", err)
	}

	// Cleanup function to remove script after execution
	cleanup := func() {
		os.Remove(scriptPath)
	}

	// Execute using unshare (creates new mount namespace)
	// -m: mount namespace
	// --propagation private: don't propagate mounts to parent namespace
	cmd := exec.CommandContext(ctx, "unshare", "-m", "--propagation", "private", "sh", scriptPath)

	// Set working directory for proper error messages
	cmd.Dir = iso.WorkDir

	// CRITICAL: Set safe environment (no secrets)
	cmd.Env = BuildSafeEnvironment()

	return cmd, cleanup, nil
}

// executeIsolatedMacOS uses sandbox-exec for filesystem isolation on macOS
func (iso *Isolator) executeIsolatedMacOS(ctx context.Context, command string, args []string) (*exec.Cmd, func(), error) {
	// Generate sandbox profile
	profile := iso.generateSandboxProfile()

	// Create unique temp profile file
	profilePath := filepath.Join("/tmp", fmt.Sprintf("sandbox-%d-%d.sb", os.Getpid(), time.Now().UnixNano()))

	// Write profile with error handling
	if err := os.WriteFile(profilePath, []byte(profile), 0644); err != nil {
		return nil, nil, fmt.Errorf("failed to write sandbox profile: %w", err)
	}

	// Cleanup function to remove profile after execution
	cleanup := func() {
		os.Remove(profilePath)
	}

	// Build full command
	fullCmd := command
	if len(args) > 0 {
		fullCmd = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}

	// Execute using sandbox-exec
	cmd := exec.CommandContext(ctx, "sandbox-exec", "-f", profilePath, "sh", "-c", fullCmd)

	// Set working directory
	cmd.Dir = iso.WorkDir

	// CRITICAL: Set safe environment (no secrets)
	cmd.Env = BuildSafeEnvironment()

	return cmd, cleanup, nil
}

// generateSandboxProfile creates a macOS sandbox profile for filesystem isolation
func (iso *Isolator) generateSandboxProfile() string {
	if iso.StrictAllowlist {
		return iso.generateStrictSandboxProfile()
	}

	var sb strings.Builder

	sb.WriteString("(version 1)\n")
	sb.WriteString("(allow default)\n\n")

	baseDir := canonicalPath(iso.getBaseDir())

	// Mode 1: BlockedPaths only (deny-list mode for chat)
	if len(iso.BlockedPaths) > 0 && len(iso.ReadPaths) == 0 && len(iso.WritePaths) == 0 {
		sb.WriteString("; Deny-list mode: block specific paths\n")
		sb.WriteString("(deny file-read* file-write*\n")
		for _, path := range iso.BlockedPaths {
			fullPath := iso.sandboxPath(path)
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n\n")

		return sb.String()
	}

	// Mode 2: Allow-list mode (workflow mode with ReadPaths/WritePaths)
	//
	// Strategy: deny the project root directory (source code, server configs,
	// .env files) then re-allow only workspace-docs subpaths within it.
	// Everything else (home dir tool configs, system paths) stays accessible
	// so CLIs like aws, gcloud, kubectl, docker etc. work normally.
	projectRoot := canonicalPath(filepath.Dir(baseDir))
	sb.WriteString("; Deny project root (source code, server configs, .env)\n")
	sb.WriteString(fmt.Sprintf("(deny file-read* file-write* (subpath \"%s\"))\n\n", sandboxQuoted(projectRoot)))

	// Homebrew SQLite resolves an absolute database path by reading metadata for
	// every ancestor, including projectRoot. Without this literal metadata-only
	// grant, sqlite3_open() returns EPERM even when the database itself is inside
	// an allowed path. This does not permit listing or reading projectRoot files.
	sb.WriteString("; Allow project root metadata for canonical path resolution\n")
	sb.WriteString(fmt.Sprintf("(allow file-read-metadata (literal \"%s\"))\n\n", sandboxQuoted(projectRoot)))

	// getcwd() needs real read access (not just metadata/stat) on the working
	// directory and each ancestor up to baseDir — verified empirically: with a
	// stripped-down environment (no inherited $PWD, e.g. BuildSafeEnvironment),
	// the shell's own startup getcwd() call fails under metadata-only grants
	// with "cannot access parent directories: Operation not permitted", even
	// though the directory is otherwise fully accessible. (literal ...), not
	// (subpath ...), so this still only grants each named directory's own
	// listing/stat — never its subtree — and does not bypass the allow-list.
	workDir := canonicalPath(iso.WorkDir)
	if workDir != "" {
		sb.WriteString("; Allow working directory read access for getcwd\n")
		sb.WriteString(fmt.Sprintf("(allow file-read* (literal \"%s\"))\n", sandboxQuoted(workDir)))
		for dir := filepath.Dir(workDir); strings.HasPrefix(dir, baseDir+string(filepath.Separator)); dir = filepath.Dir(dir) {
			sb.WriteString(fmt.Sprintf("(allow file-read* (literal \"%s\"))\n", sandboxQuoted(dir)))
		}
		sb.WriteString(fmt.Sprintf("(allow file-read* (literal \"%s\"))\n", sandboxQuoted(baseDir)))
		sb.WriteString("\n")
	}

	// Allow read-only access to configured read paths (within workspace-docs)
	if len(iso.ReadPaths) > 0 {
		sb.WriteString("; Allowed read paths\n")
		sb.WriteString("(allow file-read*\n")
		for _, path := range iso.ReadPaths {
			fullPath, ok := iso.sandboxAllowedPath(path)
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n\n")
	}

	// Allow read+write access to configured write paths
	if len(iso.WritePaths) > 0 {
		sb.WriteString("; Allowed write paths\n")
		sb.WriteString("(allow file-read* file-write*\n")
		for _, path := range iso.WritePaths {
			fullPath, ok := iso.sandboxAllowedPath(path)
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n\n")
	}

	// Explicit deny for blocked paths (takes precedence, added last)
	if len(iso.BlockedPaths) > 0 {
		sb.WriteString("; Explicit deny for blocked paths (overrides allows)\n")
		sb.WriteString("(deny file-read* file-write*\n")
		for _, path := range iso.BlockedPaths {
			fullPath := iso.sandboxPath(path)
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n")
	}

	// Explicit deny for write-only blocked paths — reads pass through, writes denied.
	// Used for paths agents must be able to inspect but not modify (planning/, etc.).
	if len(iso.BlockedWritePaths) > 0 {
		sb.WriteString("; Explicit deny for write-only blocked paths (reads allowed)\n")
		sb.WriteString("(deny file-write*\n")
		for _, path := range iso.BlockedWritePaths {
			fullPath := iso.sandboxPath(path)
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n")
	}

	return sb.String()
}

// strictSystemReadPaths are read-only, non-personal system paths a shell needs
// to exec ordinary POSIX tools (the shell binary itself, dyld, ls/cat/mkdir,
// locale data). None of these hold user/family data — everything that does
// (the real home directory, credentials, browser profiles, and so on) is
// simply absent from this list, which is the point of deny-by-default: unlike
// generateSandboxProfile's default mode, there is no implicit "allow" for
// anything not named here.
var strictSystemReadPaths = []string{
	"/usr", "/bin", "/sbin", "/System", "/Library", "/opt",
	"/private/etc", "/private/var/db", "/private/var/select",
}

// strictSystemDevices are device nodes ordinary shell commands rely on.
var strictSystemDevices = []string{
	"/dev/null", "/dev/zero", "/dev/urandom", "/dev/random", "/dev/tty",
}

// generateStrictSandboxProfile builds a genuine deny-by-default macOS sandbox
// profile (StrictAllowlist mode): only ReadPaths/WritePaths and the minimal
// system paths above are visible. Everything else on the machine — the real
// home directory, SSH/cloud credentials, browser data, anything outside the
// allow-list — is invisible, unlike generateSandboxProfile's default mode,
// which only denies the app's own project root and allows everything else.
func (iso *Isolator) generateStrictSandboxProfile() string {
	var sb strings.Builder
	sb.WriteString("(version 1)\n")
	sb.WriteString("(deny default)\n\n")

	sb.WriteString("; Minimal process/IPC permissions so the shell can run at all\n")
	sb.WriteString("(allow process-exec*)\n")
	sb.WriteString("(allow process-fork)\n")
	sb.WriteString("(allow signal (target self))\n")
	sb.WriteString("(allow sysctl-read)\n")
	sb.WriteString("(allow mach-lookup)\n")
	sb.WriteString("(allow iokit-open)\n\n")

	// The shell itself (and ordinary tools resolving relative/absolute paths)
	// needs to list "/"'s own top-level entries — standard macOS directory
	// names (Users, System, bin, etc.), not their contents. Without this,
	// sandbox-exec's kernel-level denial of "file-read-data /" aborts the
	// process outright rather than failing the specific command cleanly.
	sb.WriteString("; Root directory's own listing (top-level names only, not descendants)\n")
	sb.WriteString("(allow file-read-data (literal \"/\"))\n\n")

	sb.WriteString("; Read-only system paths needed for ordinary shell tools to run — no user data here\n")
	sb.WriteString("(allow file-read*\n")
	for _, p := range strictSystemReadPaths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(canonicalPath(p))))
	}
	sb.WriteString(")\n\n")

	sb.WriteString("; Device nodes ordinary shell commands rely on\n")
	sb.WriteString("(allow file-read* file-write-data\n")
	for _, p := range strictSystemDevices {
		sb.WriteString(fmt.Sprintf("  (literal \"%s\")\n", sandboxQuoted(p)))
	}
	sb.WriteString(")\n\n")

	sb.WriteString("; Scratch space for ordinary temp files (compiler/interpreter caches, etc.)\n")
	sb.WriteString("(allow file-read* file-write*\n")
	sb.WriteString("  (subpath \"/private/tmp\")\n")
	sb.WriteString("  (subpath \"/private/var/folders\")\n")
	sb.WriteString(")\n\n")

	workDir := canonicalPath(iso.WorkDir)
	if workDir != "" {
		// Deliberately still metadata-only here, unlike the default profile's
		// equivalent grant: (literal dir) + file-read* turns out to also permit
		// *listing* that directory's entries (verified empirically), and this
		// chain runs all the way to "/" — upgrading it would expose the real
		// filenames in the real home directory and above, which is exactly what
		// StrictAllowlist exists to prevent. So getcwd() stays cosmetically
		// broken here (a stat-only warning, not a functional block — real
		// commands against allowed paths still work) in exchange for genuinely
		// not leaking anything outside the allow-list.
		sb.WriteString("; Directory metadata along the path to WorkDir, needed for getcwd()\n")
		for dir := workDir; ; dir = filepath.Dir(dir) {
			sb.WriteString(fmt.Sprintf("(allow file-read-metadata (literal \"%s\"))\n", sandboxQuoted(dir)))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
			sb.WriteString(fmt.Sprintf("(allow file-read-metadata (literal \"%s\"))\n", sandboxQuoted(dir)))
			if dir == string(filepath.Separator) {
				break
			}
		}
		sb.WriteString("\n")
	}

	if len(iso.ReadPaths) > 0 {
		sb.WriteString("; Allowed read paths\n")
		sb.WriteString("(allow file-read*\n")
		for _, path := range iso.ReadPaths {
			fullPath, ok := iso.sandboxAllowedPath(path)
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n\n")
	}

	if len(iso.WritePaths) > 0 {
		sb.WriteString("; Allowed write paths\n")
		sb.WriteString("(allow file-read* file-write*\n")
		for _, path := range iso.WritePaths {
			fullPath, ok := iso.sandboxAllowedPath(path)
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n\n")
	}

	if len(iso.BlockedPaths) > 0 {
		sb.WriteString("; Explicit deny for blocked paths (overrides allows)\n")
		sb.WriteString("(deny file-read* file-write*\n")
		for _, path := range iso.BlockedPaths {
			fullPath := iso.sandboxPath(path)
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n")
	}

	if len(iso.BlockedWritePaths) > 0 {
		sb.WriteString("; Explicit deny for write-only blocked paths (reads allowed)\n")
		sb.WriteString("(deny file-write*\n")
		for _, path := range iso.BlockedWritePaths {
			fullPath := iso.sandboxPath(path)
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", sandboxQuoted(fullPath)))
		}
		sb.WriteString(")\n")
	}

	return sb.String()
}

// generateMountScript creates a shell script for filesystem isolation
// Strategy: Bind workspace to temp, hide with tmpfs, then selectively expose allowed paths
func (iso *Isolator) generateMountScript(command string, args []string) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("set -e\n\n")

	baseDir := iso.getBaseDir()

	// Mode 1: BlockedPaths only (deny-list mode for chat)
	// Hide blocked paths with tmpfs.
	if len(iso.BlockedPaths) > 0 && len(iso.ReadPaths) == 0 && len(iso.WritePaths) == 0 {
		sb.WriteString("# Deny-list mode: hide specific blocked paths\n")

		// Hide blocked paths with tmpfs
		for _, path := range iso.BlockedPaths {
			fullPath := path
			if !strings.HasPrefix(path, "/") {
				fullPath = filepath.Join(baseDir, strings.TrimSuffix(path, "/"))
			}
			sb.WriteString(fmt.Sprintf("mount -t tmpfs tmpfs \"%s\" 2>/dev/null || true\n", fullPath))
		}
		sb.WriteString("\n")

		// Change to working directory
		sb.WriteString(fmt.Sprintf("cd \"%s\"\n", iso.WorkDir))

		// Build and execute command
		fullCmd := command
		if len(args) > 0 {
			fullCmd = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
		}

		// Escape single quotes in command for shell execution
		escapedCmd := strings.ReplaceAll(fullCmd, "'", "'\\''")
		sb.WriteString(fmt.Sprintf("exec sh -c '%s'\n", escapedCmd))

		return sb.String()
	}

	// Mode 2: Allow-list mode (workflow mode with ReadPaths/WritePaths)
	tempDir := "/tmp/workspace-original-$$"

	// Step 1: Create temp directory and bind mount workspace there (preserve original)
	sb.WriteString("# Preserve original workspace in temp location\n")
	sb.WriteString(fmt.Sprintf("mkdir -p \"%s\"\n", tempDir))
	sb.WriteString(fmt.Sprintf("mount --bind \"%s\" \"%s\"\n\n", baseDir, tempDir))

	// Step 2: Create write path directories in temp (= real filesystem via bind mount)
	// These must exist before tmpfs hides them.
	if len(iso.WritePaths) > 0 {
		sb.WriteString("# Create write path dirs in original workspace (via temp bind mount)\n")
		for _, path := range iso.WritePaths {
			relPath := strings.TrimPrefix(path, baseDir+"/")
			if relPath == path && !strings.HasPrefix(path, "/") {
				relPath = path
			}
			logicalTempPath := filepath.Join(tempDir, relPath)
			sb.WriteString(fmt.Sprintf("mkdir -p \"%s\"\n", logicalTempPath))
		}
		sb.WriteString("\n")
	}

	// Step 3: Hide workspace with tmpfs overlay
	sb.WriteString("# Hide workspace with tmpfs overlay\n")
	sb.WriteString(fmt.Sprintf("mount -t tmpfs tmpfs \"%s\"\n\n", baseDir))

	// Step 4: Bind-mount read paths from temp (read-only)
	// Includes write path dirs created in step 2 (they exist in the original filesystem)
	if len(iso.ReadPaths) > 0 {
		sb.WriteString("# Mount read-only paths from original workspace\n")
		for _, path := range iso.ReadPaths {
			relPath := strings.TrimPrefix(path, baseDir+"/")
			if relPath == path && !strings.HasPrefix(path, "/") {
				relPath = path
			}
			tempPath := filepath.Join(tempDir, relPath)
			absPath := path
			if !strings.HasPrefix(path, "/") {
				absPath = filepath.Join(baseDir, path)
			}
			sb.WriteString(fmt.Sprintf("if [ -e \"%s\" ]; then\n", tempPath))
			sb.WriteString(fmt.Sprintf("  mkdir -p \"%s\"\n", absPath))
			sb.WriteString(fmt.Sprintf("  mount --bind -o ro \"%s\" \"%s\"\n", tempPath, absPath))
			sb.WriteString("fi\n")
		}
		sb.WriteString("\n")
	}

	// Step 5: Bind-mount write paths from temp (read-write, overrides read-only)
	if len(iso.WritePaths) > 0 {
		sb.WriteString("# Mount write paths read-write (overrides read-only for these subtrees)\n")
		for _, path := range iso.WritePaths {
			relPath := strings.TrimPrefix(path, baseDir+"/")
			if relPath == path && !strings.HasPrefix(path, "/") {
				relPath = path
			}
			tempPath := filepath.Join(tempDir, relPath)
			absPath := path
			if !strings.HasPrefix(path, "/") {
				absPath = filepath.Join(baseDir, path)
			}
			sb.WriteString(fmt.Sprintf("if [ -e \"%s\" ]; then\n", tempPath))
			sb.WriteString(fmt.Sprintf("  mkdir -p \"%s\"\n", absPath))
			sb.WriteString(fmt.Sprintf("  mount --bind \"%s\" \"%s\"\n", tempPath, absPath))
			sb.WriteString("fi\n")
		}
		sb.WriteString("\n")
	}

	// Step 6: Overlay write-blocked subpaths as read-only. This runs AFTER step 5 so
	// the read-only bind-mount wins for these subtrees even when their parent is
	// bound read-write. Writes to these paths return EROFS; reads pass through
	// unchanged so the agent can still cat/jq planning files.
	if len(iso.BlockedWritePaths) > 0 {
		sb.WriteString("# Re-mount write-blocked paths as read-only (reads allowed, writes denied)\n")
		for _, path := range iso.BlockedWritePaths {
			relPath := strings.TrimPrefix(path, baseDir+"/")
			if relPath == path && !strings.HasPrefix(path, "/") {
				relPath = path
			}
			tempPath := filepath.Join(tempDir, relPath)
			absPath := path
			if !strings.HasPrefix(path, "/") {
				absPath = filepath.Join(baseDir, path)
			}
			sb.WriteString(fmt.Sprintf("if [ -e \"%s\" ]; then\n", tempPath))
			sb.WriteString(fmt.Sprintf("  mkdir -p \"%s\"\n", absPath))
			sb.WriteString(fmt.Sprintf("  mount --bind -o ro \"%s\" \"%s\"\n", tempPath, absPath))
			sb.WriteString("fi\n")
		}
		sb.WriteString("\n")
	}

	// Change to working directory
	sb.WriteString(fmt.Sprintf("cd \"%s\"\n", iso.WorkDir))

	// Build and execute command
	fullCmd := command
	if len(args) > 0 {
		fullCmd = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}

	// Escape single quotes in command for shell execution
	escapedCmd := strings.ReplaceAll(fullCmd, "'", "'\\''")
	sb.WriteString(fmt.Sprintf("exec sh -c '%s'\n", escapedCmd))

	return sb.String()
}
