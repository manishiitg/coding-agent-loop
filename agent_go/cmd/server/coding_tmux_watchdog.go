package server

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

// Coding-CLI rate-limit watchdog.
//
// When an interactive coding CLI hits its provider usage/session limit, it
// prints a limit wall and then parks at an idle prompt. The backend session can
// stay marked "running" with a live-but-useless tmux pane, so the UI refuses
// new input and scheduled runs wedge. Adapter-level detection covers only some
// paths; this watchdog sits above them by watching the panes directly.
const (
	codingWatchdogInterval = 30 * time.Second
	// codingWatchdogConfirmChecks is how many consecutive rate-limited
	// observations are required before force-stopping.
	codingWatchdogConfirmChecks = 2
)

var captureTmuxPanePlainForWatchdog = captureTmuxPanePlain

func (api *StreamingAPI) startCodingTmuxRateLimitWatchdog() {
	if api == nil || api.terminalStore == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(codingWatchdogInterval)
		defer ticker.Stop()
		streak := make(map[string]int)
		for range ticker.C {
			api.reapRateLimitedCodingSessionsOnce(streak)
		}
	}()
	log.Printf("[CODING_WATCHDOG] Started coding-CLI rate-limit watchdog (interval=%s, confirm=%d checks)",
		codingWatchdogInterval, codingWatchdogConfirmChecks)
}

func (api *StreamingAPI) reapRateLimitedCodingSessionsOnce(streak map[string]int) {
	// Metadata listing deliberately avoids Store's content reconciliation. Actual
	// tmux pane state below is authoritative for whether a terminal is still live.
	snapshots := api.terminalStore.ListMetadata("")
	stillLimited := make(map[string]bool)

	for _, snap := range snapshots {
		tmux := strings.TrimSpace(snap.TmuxSession)
		sessionID := strings.TrimSpace(snap.SessionID)
		if tmux == "" || sessionID == "" {
			continue
		}
		// Confirmation belongs to one real pane, not the app session. A workflow
		// session can host several terminals; keying by tmux keeps two limited
		// panes satisfy a two-tick streak during the same poll.
		watchdogKey := tmux
		switch inspectCodingTmuxPaneState(tmux) {
		case codingTmuxPaneMissing:
			api.terminalStore.MarkStale(snap.TerminalID)
			if registry := api.ensureTerminalLeaseRegistry(); registry != nil {
				registry.MarkClosed(tmux, "tmux pane missing", time.Now())
			}
			delete(streak, watchdogKey)
			continue
		case codingTmuxPaneDead:
			if snap.Active || !strings.EqualFold(strings.TrimSpace(snap.State), "completed") {
				api.terminalStore.MarkCompleted(snap.TerminalID)
			}
			api.terminalStore.MarkProcessClosed(snap.TerminalID, "tmux pane exited")
			if registry := api.ensureTerminalLeaseRegistry(); registry != nil {
				registry.MarkClosed(tmux, "tmux pane exited", time.Now())
			}
			delete(streak, watchdogKey)
			continue
		case codingTmuxPaneUnknown:
			// A transient tmux command failure is not evidence of completion.
			continue
		}
		captured := captureTmuxPanePlainForWatchdog(tmux)
		if !terminals.DetectRateLimit(captured) {
			continue
		}
		stillLimited[watchdogKey] = true
		streak[watchdogKey]++
		if streak[watchdogKey] < codingWatchdogConfirmChecks {
			log.Printf("[CODING_WATCHDOG] session %s tmux %s looks rate-limited (%d/%d) - waiting to confirm",
				sessionID, tmux, streak[watchdogKey], codingWatchdogConfirmChecks)
			continue
		}

		log.Printf("[CODING_WATCHDOG] session %s tmux %s parked on a usage/rate-limit wall - force-stopping",
			sessionID, tmux)
		api.markSessionStopped(sessionID)
		api.updateSessionStatus(sessionID, "failed")
		gracefulCloseCodingCLITmuxByName(tmux, "provider usage/rate limit reached")
		killCtx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
		_ = runTmuxKill(killCtx, tmux)
		cancel()
		api.terminalStore.MarkFailed(snap.TerminalID)
		api.terminalStore.MarkProcessClosed(snap.TerminalID, "provider usage/rate limit reached")
		if registry := api.ensureTerminalLeaseRegistry(); registry != nil {
			registry.MarkClosed(tmux, "provider usage/rate limit reached", time.Now())
		}
		delete(streak, watchdogKey)
	}

	for watchdogKey := range streak {
		if !stillLimited[watchdogKey] {
			delete(streak, watchdogKey)
		}
	}
}

type codingTmuxPaneState int

const (
	codingTmuxPaneUnknown codingTmuxPaneState = iota
	codingTmuxPaneAlive
	codingTmuxPaneDead
	codingTmuxPaneMissing
)

func inspectCodingTmuxPaneState(tmuxSession string) codingTmuxPaneState {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return codingTmuxPaneMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	defer cancel()
	out, err := runTerminalTmuxOutputCommand(ctx, "display-message", "-p", "-t", tmuxSession, "#{pane_dead}")
	if err != nil {
		if isMissingTmuxTargetError(err) {
			return codingTmuxPaneMissing
		}
		return codingTmuxPaneUnknown
	}
	switch strings.TrimSpace(out) {
	case "0":
		return codingTmuxPaneAlive
	case "1":
		return codingTmuxPaneDead
	default:
		return codingTmuxPaneUnknown
	}
}

func captureTmuxPanePlain(tmuxSession string) string {
	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-S", "-200", "-t", tmuxSession)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

func runTmuxKill(ctx context.Context, tmuxSession string) error {
	return exec.CommandContext(ctx, "tmux", "kill-session", "-t", tmuxSession).Run()
}
