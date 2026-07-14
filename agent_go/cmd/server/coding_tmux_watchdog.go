package server

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
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
	// Only inspect the current tail. Rate-limit text can remain in tmux scrollback
	// after a provider has recovered and resumed useful work.
	codingWatchdogRateLimitTailLines = 40
)

var captureTmuxPanePlainForWatchdog = captureTmuxPanePlain
var closeCodingCLITmuxForWatchdog = gracefulCloseCodingCLITmuxByName

type codingWatchdogObservation struct {
	evidence string
	count    int
}

func (api *StreamingAPI) startCodingTmuxRateLimitWatchdog() {
	if api == nil || api.terminalStore == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(codingWatchdogInterval)
		defer ticker.Stop()
		streak := make(map[string]codingWatchdogObservation)
		for range ticker.C {
			api.reapRateLimitedCodingSessionsOnce(streak)
		}
	}()
	log.Printf("[CODING_WATCHDOG] Started coding-CLI rate-limit watchdog (interval=%s, confirm=%d checks)",
		codingWatchdogInterval, codingWatchdogConfirmChecks)
}

func (api *StreamingAPI) reapRateLimitedCodingSessionsOnce(streak map[string]codingWatchdogObservation) {
	// Metadata listing deliberately avoids Store's content reconciliation. Actual
	// tmux pane state below is authoritative for whether a terminal is still live.
	// Process supervision must include terminals dismissed from presentation.
	// ListRaw avoids content reconciliation while retaining those ownership rows.
	snapshots := api.terminalStore.ListRaw("")
	stillLimited := make(map[string]bool)

	for _, snap := range snapshots {
		tmux := strings.TrimSpace(snap.TmuxSession)
		sessionID := strings.TrimSpace(snap.SessionID)
		if tmux == "" || sessionID == "" {
			continue
		}
		// Confirmation belongs to one real pane, not the app session. A workflow
		// session can host several terminals; keying by tmux keeps two limited
		// panes from satisfying a two-tick streak during the same poll.
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
			killCtx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
			killErr := runTmuxKill(killCtx, tmux)
			cancel()
			if killErr != nil && !isMissingTmuxTargetError(killErr) {
				log.Printf("[CODING_WATCHDOG] dead pane cleanup failed session=%s tmux=%s: %v", sessionID, tmux, killErr)
				continue
			}
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
		evidence := codingWatchdogRateLimitEvidence(captured)
		if evidence == "" {
			continue
		}
		stillLimited[watchdogKey] = true
		observation := streak[watchdogKey]
		if observation.evidence == evidence {
			observation.count++
		} else {
			observation = codingWatchdogObservation{evidence: evidence, count: 1}
		}
		streak[watchdogKey] = observation
		if observation.count < codingWatchdogConfirmChecks {
			log.Printf("[CODING_WATCHDOG] session %s tmux %s looks rate-limited (%d/%d) - waiting to confirm",
				sessionID, tmux, observation.count, codingWatchdogConfirmChecks)
			continue
		}

		isMainAgent := codingAgentSnapshotIsMainAgent(snap)
		if isMainAgent {
			log.Printf("[CODING_WATCHDOG] main session %s tmux %s parked on a usage/rate-limit wall - failing and canceling session runtime",
				sessionID, tmux)
			// Persist failure before closing panes: stream goroutines may unwind as
			// soon as cancellation starts and must not overwrite this as completed.
			api.updateSessionStatus(sessionID, "error")
			api.cancelSessionRuntimeWork(sessionID, "provider usage/rate limit reached")
			delete(streak, watchdogKey)
			continue
		} else {
			log.Printf("[CODING_WATCHDOG] child terminal %s session %s tmux %s parked on a usage/rate-limit wall - closing child only",
				snap.TerminalID, sessionID, tmux)
		}
		closeCodingCLITmuxForWatchdog(tmux, "provider usage/rate limit reached")
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

// codingWatchdogRateLimitEvidence returns the normalized visible tail when it
// contains a rate-limit marker. The watchdog confirms this entire value across
// polls, so any new output proves the pane is progressing and resets the check.
// Old rate-limit text outside the current tail is ignored.
func codingWatchdogRateLimitEvidence(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > codingWatchdogRateLimitTailLines {
		lines = lines[len(lines)-codingWatchdogRateLimitTailLines:]
	}
	normalized := make([]string, 0, len(lines))
	hasRateLimit := false
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		normalized = append(normalized, strings.ToLower(line))
		hasRateLimit = hasRateLimit || terminals.DetectRateLimit(line)
	}
	if !hasRateLimit {
		return ""
	}
	return strings.Join(normalized, "\n")
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

var runTmuxKill = func(ctx context.Context, tmuxSession string) error {
	return runTerminalTmuxCommand(ctx, "", "kill-session", "-t", tmuxSession)
}
