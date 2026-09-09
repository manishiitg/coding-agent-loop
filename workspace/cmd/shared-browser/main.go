// shared-browser supervises the deployment's persistent browser independently
// of workflow and workspace service lifecycles.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/manishiitg/coding-agent-loop/workspace/browserconfig"
	"github.com/manishiitg/coding-agent-loop/workspace/security"
)

func command(ctx context.Context, args ...string) error {
	argv := append(browserconfig.HeadlessArgs(), "--session", browserconfig.SharedSession)
	argv = append(argv, args...)
	argv = append(argv, "--json")
	cmd := exec.CommandContext(ctx, "agent-browser", argv...)
	cmd.Env = security.BuildSafeEnvironment()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("browser command failed: %w: %s", err, output)
	}
	return nil
}

func run() error {
	if !browserconfig.SharedEnabled() {
		return fmt.Errorf("an absolute AGENT_BROWSER_SHARED_PROFILE is required")
	}
	if err := os.MkdirAll(browserconfig.SharedProfile(), 0700); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	start, cancel := context.WithTimeout(ctx, 60*time.Second)
	err := command(start, "tab", "list")
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = command(closeCtx, "close")
	}()
	fmt.Println("Shared browser ready")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			data, err := os.ReadFile(filepath.Join("/tmp/.agent-browser", browserconfig.SharedSession+".pid"))
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || pid <= 1 {
				return fmt.Errorf("invalid browser daemon pid")
			}
			if err := syscall.Kill(pid, 0); err != nil {
				return fmt.Errorf("browser daemon stopped: %w", err)
			}
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
