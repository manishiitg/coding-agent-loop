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
	BlockedPaths []string // Paths to explicitly deny (deny-list, takes precedence)
	WorkDir      string
	BaseDir      string // Workspace base directory (default: /app/workspace-docs)
}


const defaultBaseDir = "/app/workspace-docs"

// getBaseDir returns the configured base directory or the default
func (iso *Isolator) getBaseDir() string {
	if iso.BaseDir != "" {
		return iso.BaseDir
	}
	return defaultBaseDir
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
	var sb strings.Builder

	sb.WriteString("(version 1)\n")
	sb.WriteString("(allow default)\n\n")

	baseDir := iso.getBaseDir()

	// Mode 1: BlockedPaths only (deny-list mode for chat)
	if len(iso.BlockedPaths) > 0 && len(iso.ReadPaths) == 0 && len(iso.WritePaths) == 0 {
		sb.WriteString("; Deny-list mode: block specific paths\n")
		sb.WriteString("(deny file-read* file-write*\n")
		for _, path := range iso.BlockedPaths {
			fullPath := path
			if !strings.HasPrefix(path, "/") {
				fullPath = filepath.Join(baseDir, strings.TrimSuffix(path, "/"))
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", fullPath))
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
	projectRoot := filepath.Dir(baseDir)
	sb.WriteString("; Deny project root (source code, server configs, .env)\n")
	sb.WriteString(fmt.Sprintf("(deny file-read* file-write* (subpath \"%s\"))\n\n", projectRoot))

	// Allow the shell to read the working directory and its parents for getcwd().
	workDir := iso.WorkDir
	if workDir != "" {
		sb.WriteString("; Allow working directory access\n")
		sb.WriteString(fmt.Sprintf("(allow file-read* (subpath \"%s\"))\n", workDir))
		for dir := filepath.Dir(workDir); strings.HasPrefix(dir, baseDir) && dir != baseDir; dir = filepath.Dir(dir) {
			sb.WriteString(fmt.Sprintf("(allow file-read-data (literal \"%s\"))\n", dir))
		}
		sb.WriteString(fmt.Sprintf("(allow file-read-data (literal \"%s\"))\n", baseDir))
		sb.WriteString("\n")
	}

	// Allow read-only access to configured read paths (within workspace-docs)
	if len(iso.ReadPaths) > 0 {
		sb.WriteString("; Allowed read paths\n")
		sb.WriteString("(allow file-read*\n")
		for _, path := range iso.ReadPaths {
			fullPath := path
			if !strings.HasPrefix(path, "/") {
				fullPath = filepath.Join(baseDir, strings.TrimSuffix(path, "/"))
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", fullPath))
		}
		sb.WriteString(")\n\n")
	}

	// Allow read+write access to configured write paths
	if len(iso.WritePaths) > 0 {
		sb.WriteString("; Allowed write paths\n")
		sb.WriteString("(allow file-read* file-write*\n")
		for _, path := range iso.WritePaths {
			fullPath := path
			if !strings.HasPrefix(path, "/") {
				fullPath = filepath.Join(baseDir, strings.TrimSuffix(path, "/"))
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", fullPath))
		}
		sb.WriteString(")\n\n")
	}

	// Explicit deny for blocked paths (takes precedence, added last)
	if len(iso.BlockedPaths) > 0 {
		sb.WriteString("; Explicit deny for blocked paths (overrides allows)\n")
		sb.WriteString("(deny file-read* file-write*\n")
		for _, path := range iso.BlockedPaths {
			fullPath := path
			if !strings.HasPrefix(path, "/") {
				fullPath = filepath.Join(baseDir, strings.TrimSuffix(path, "/"))
			}
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", fullPath))
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
