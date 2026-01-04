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
	ReadPaths  []string
	WritePaths []string
	WorkDir    string
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

	baseDir := "/app/workspace-docs"

	// CRITICAL: Deny BOTH read and write access to workspace by default
	sb.WriteString("(deny file-read* file-write*\n")
	sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", baseDir))
	sb.WriteString(")\n\n")

	// Allow read-only access to read paths
	if len(iso.ReadPaths) > 0 {
		sb.WriteString("(allow file-read*\n")
		for _, path := range iso.ReadPaths {
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", path))
		}
		sb.WriteString(")\n\n")
	}

	// Allow read AND write access to write paths (override the deny)
	if len(iso.WritePaths) > 0 {
		sb.WriteString("(allow file-read* file-write*\n")
		for _, path := range iso.WritePaths {
			sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", path))
		}
		sb.WriteString(")\n\n")
	}

	// Always allow Downloads folder (special exception per folder guard spec)
	downloadsPath := filepath.Join(baseDir, "Downloads")
	sb.WriteString("(allow file-read* file-write*\n")
	sb.WriteString(fmt.Sprintf("  (subpath \"%s\")\n", downloadsPath))
	sb.WriteString(")\n")

	return sb.String()
}

// generateMountScript creates a shell script for filesystem isolation
// Strategy: Bind workspace to temp, hide with tmpfs, then selectively expose allowed paths
func (iso *Isolator) generateMountScript(command string, args []string) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("set -e\n\n")

	baseDir := "/app/workspace-docs"
	tempDir := "/tmp/workspace-original-$$"

	// Step 1: Create temp directory and bind mount workspace there (preserve original)
	sb.WriteString("# Preserve original workspace in temp location\n")
	sb.WriteString(fmt.Sprintf("mkdir -p %s\n", tempDir))
	sb.WriteString(fmt.Sprintf("mount --bind %s %s\n\n", baseDir, tempDir))

	// Step 2: Hide workspace with tmpfs overlay
	sb.WriteString("# Hide workspace with tmpfs overlay\n")
	sb.WriteString(fmt.Sprintf("mount -t tmpfs tmpfs %s\n\n", baseDir))

	// Step 3: Bind-mount read paths from temp location (read-only)
	if len(iso.ReadPaths) > 0 {
		sb.WriteString("# Mount read-only paths from original workspace\n")
		for _, path := range iso.ReadPaths {
			// Get relative path
			relPath := strings.TrimPrefix(path, baseDir+"/")
			if relPath == path {
				relPath = filepath.Base(path)
			}
			tempPath := filepath.Join(tempDir, relPath)

			// Create directory structure in tmpfs
			sb.WriteString(fmt.Sprintf("mkdir -p %s\n", path))
			// Bind mount from temp location (read-only)
			sb.WriteString(fmt.Sprintf("mount --bind -o ro %s %s\n", tempPath, path))
		}
		sb.WriteString("\n")
	}

	// Step 4: Bind-mount write paths from temp location (read-write)
	if len(iso.WritePaths) > 0 {
		sb.WriteString("# Mount read-write paths from original workspace\n")
		for _, path := range iso.WritePaths {
			// Get relative path
			relPath := strings.TrimPrefix(path, baseDir+"/")
			if relPath == path {
				relPath = filepath.Base(path)
			}
			tempPath := filepath.Join(tempDir, relPath)

			// Create directory structure in tmpfs
			sb.WriteString(fmt.Sprintf("mkdir -p %s\n", path))
			// Bind mount from temp location (read-write)
			sb.WriteString(fmt.Sprintf("mount --bind %s %s\n", tempPath, path))
		}
		sb.WriteString("\n")
	}

	// Step 5: Always mount Downloads folder (special exception per folder guard spec)
	downloadsPath := filepath.Join(baseDir, "Downloads")
	tempDownloads := filepath.Join(tempDir, "Downloads")
	sb.WriteString("# Downloads always accessible\n")
	sb.WriteString(fmt.Sprintf("mkdir -p %s\n", downloadsPath))
	sb.WriteString(fmt.Sprintf("mkdir -p %s 2>/dev/null || true\n", tempDownloads))
	sb.WriteString(fmt.Sprintf("mount --bind %s %s\n\n", tempDownloads, downloadsPath))

	// Change to working directory
	sb.WriteString(fmt.Sprintf("cd %s\n", iso.WorkDir))

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
