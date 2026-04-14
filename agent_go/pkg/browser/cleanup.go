package browser

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CleanupStaleRuntimeState removes stale agent-browser runtime files (PID, socket)
// that point to dead processes. This prevents "CDP response channel closed" errors
// caused by agent-browser trying to connect to a defunct runtime server.
//
// Call this at server startup before any browser commands are executed.
func CleanupStaleRuntimeState() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[BROWSER_CLEANUP] Could not determine home directory: %v", err)
		return
	}

	abDir := filepath.Join(homeDir, ".agent-browser")

	// Also check /tmp/.agent-browser (used when HOME=/tmp, e.g. Docker)
	dirs := []string{abDir}
	tmpABDir := "/tmp/.agent-browser"
	if tmpABDir != abDir {
		dirs = append(dirs, tmpABDir)
	}

	for _, dir := range dirs {
		cleanupDir(dir)
	}
}

func cleanupDir(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}

	// Find all .pid files (default.pid, rts.pid, etc.)
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[BROWSER_CLEANUP] Could not read %s: %v", dir, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".pid") {
			continue
		}

		pidFile := filepath.Join(dir, name)
		baseName := strings.TrimSuffix(name, ".pid")
		sockFile := filepath.Join(dir, baseName+".sock")

		pidBytes, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil {
			// Corrupt PID file — remove it
			log.Printf("[BROWSER_CLEANUP] Removing corrupt PID file: %s", pidFile)
			os.Remove(pidFile)
			os.Remove(sockFile)
			continue
		}

		// Check if the process is alive
		proc, err := os.FindProcess(pid)
		if err != nil {
			// Can't find process — stale
			removeStalePair(pidFile, sockFile, pid, "process not found")
			continue
		}

		// On Unix, FindProcess always succeeds. Use signal 0 to check if alive.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			removeStalePair(pidFile, sockFile, pid, err.Error())
			continue
		}

		log.Printf("[BROWSER_CLEANUP] Runtime %s (PID %d) is alive — keeping", baseName, pid)
	}
}

func removeStalePair(pidFile, sockFile string, pid int, reason string) {
	log.Printf("[BROWSER_CLEANUP] Removing stale runtime state: PID %d (%s)", pid, reason)

	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		log.Printf("[BROWSER_CLEANUP] Warning: could not remove %s: %v", pidFile, err)
	} else {
		fmt.Printf("🧹 Cleaned stale agent-browser PID file: %s\n", pidFile)
	}

	if err := os.Remove(sockFile); err != nil && !os.IsNotExist(err) {
		log.Printf("[BROWSER_CLEANUP] Warning: could not remove %s: %v", sockFile, err)
	} else if _, statErr := os.Stat(sockFile); statErr == nil {
		// Only log if file existed
		fmt.Printf("🧹 Cleaned stale agent-browser socket: %s\n", sockFile)
	}

	// Also clean related files (stream, engine, version)
	baseDir := filepath.Dir(pidFile)
	baseName := strings.TrimSuffix(filepath.Base(pidFile), ".pid")
	for _, ext := range []string{".stream", ".engine", ".version"} {
		extraFile := filepath.Join(baseDir, baseName+ext)
		os.Remove(extraFile) // Best-effort, ignore errors
	}
}
