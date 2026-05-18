package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var agentBrowseCDPDiagnoseFlags struct {
	cdpPort             int
	agentBrowserTimeout time.Duration
	targetTimeout       time.Duration
	maxTargets          int
}

var agentBrowseCDPDiagnoseCmd = &cobra.Command{
	Use:   "agent-browse-cdp-diagnose",
	Short: "Diagnose agent-browser CDP hangs against an existing Chrome port",
	Long: `Runs a local diagnostic for the shared-CDP agent_browser path.

It checks:
1. Chrome /json/version and /json/list on the requested CDP port
2. agent-browser --cdp <port> tab list with a bounded timeout
3. Raw CDP Target.attachToTarget + Runtime.evaluate for each current target

This is intended to explain cases where Chrome's CDP endpoint is alive but
agent-browser hangs while attaching to an existing browser profile.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		port := agentBrowseCDPDiagnoseFlags.cdpPort
		if port <= 0 {
			return fmt.Errorf("--cdp-port must be > 0")
		}

		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		httpClient := &http.Client{Timeout: 5 * time.Second}
		version, err := getCDPJSON[map[string]interface{}](ctx, httpClient, base+"/json/version")
		if err != nil {
			return fmt.Errorf("Chrome CDP /json/version failed on port %d: %w", port, err)
		}
		tabs, err := getCDPJSON[[]map[string]interface{}](ctx, httpClient, base+"/json/list")
		if err != nil {
			return fmt.Errorf("Chrome CDP /json/list failed on port %d: %w", port, err)
		}
		fmt.Printf("Chrome CDP OK: port=%d browser=%s targets_in_json_list=%d\n", port, version["Browser"], len(tabs))

		versionText, versionErr := runAgentBrowserPreflightCommand(ctx, 5*time.Second, "--version")
		versionText = strings.TrimSpace(versionText)
		if versionErr != nil {
			fmt.Printf("agent-browser --version FAILED: %v\n%s\n", versionErr, versionText)
		} else {
			fmt.Printf("agent-browser version: %s\n", versionText)
		}

		diagSession := fmt.Sprintf("agent-browse-diagnose-%d", time.Now().UnixNano())
		output, abErr := runAgentBrowserPreflightCommand(ctx, agentBrowseCDPDiagnoseFlags.agentBrowserTimeout,
			"--session", diagSession,
			"--cdp", strconv.Itoa(port),
			"tab", "list",
			"--json",
		)
		cleanupAgentBrowserPreflightSession(diagSession)
		if abErr != nil {
			fmt.Printf("agent-browser CDP tab list: FAIL after %s: %v\n", agentBrowseCDPDiagnoseFlags.agentBrowserTimeout, abErr)
			if strings.TrimSpace(output) != "" {
				fmt.Printf("agent-browser output:\n%s\n", strings.TrimSpace(output))
			}
		} else {
			fmt.Printf("agent-browser CDP tab list: PASS\n%s\n", strings.TrimSpace(output))
		}

		wsURL, _ := version["webSocketDebuggerUrl"].(string)
		if strings.TrimSpace(wsURL) == "" {
			return fmt.Errorf("Chrome CDP /json/version did not include webSocketDebuggerUrl")
		}

		diagnosis, err := diagnoseCDPTargets(ctx, wsURL, agentBrowseCDPDiagnoseFlags.targetTimeout, agentBrowseCDPDiagnoseFlags.maxTargets)
		if err != nil {
			return err
		}
		printCDPTargetDiagnosis(diagnosis)
		if abErr != nil && diagnosis.stalledCount > 0 {
			return fmt.Errorf("agent-browser CDP failed and raw CDP found %d target(s) that timed out; close/reload those tabs/workers or restart Chrome 9222", diagnosis.stalledCount)
		}
		if abErr != nil {
			return fmt.Errorf("agent-browser CDP failed, but raw CDP target probe did not find a stalled target")
		}
		return nil
	},
}

func init() {
	agentBrowseCDPDiagnoseCmd.Flags().IntVar(&agentBrowseCDPDiagnoseFlags.cdpPort, "cdp-port", 9222, "Chrome DevTools Protocol port to diagnose")
	agentBrowseCDPDiagnoseCmd.Flags().DurationVar(&agentBrowseCDPDiagnoseFlags.agentBrowserTimeout, "agent-browser-timeout", 15*time.Second, "timeout for agent-browser --cdp tab list")
	agentBrowseCDPDiagnoseCmd.Flags().DurationVar(&agentBrowseCDPDiagnoseFlags.targetTimeout, "target-timeout", 1500*time.Millisecond, "timeout for each raw CDP target operation")
	agentBrowseCDPDiagnoseCmd.Flags().IntVar(&agentBrowseCDPDiagnoseFlags.maxTargets, "max-targets", 0, "maximum CDP targets to probe; 0 probes all targets")
}

type cdpTargetDiagnosis struct {
	targets      []cdpTargetProbe
	stalledCount int
}

type cdpTargetProbe struct {
	Type   string
	ID     string
	Title  string
	URL    string
	Status string
	Error  string
}

func diagnoseCDPTargets(ctx context.Context, browserWSURL string, timeout time.Duration, maxTargets int) (cdpTargetDiagnosis, error) {
	targets, err := getCDPTargetInfos(ctx, browserWSURL, timeout)
	if err != nil {
		return cdpTargetDiagnosis{}, fmt.Errorf("raw CDP Target.getTargets failed: %w", err)
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Type != targets[j].Type {
			return targets[i].Type < targets[j].Type
		}
		return targets[i].ID < targets[j].ID
	})
	if maxTargets > 0 && len(targets) > maxTargets {
		targets = targets[:maxTargets]
	}

	var diagnosis cdpTargetDiagnosis
	for _, target := range targets {
		probe := cdpTargetProbe{
			Type:  target.Type,
			ID:    target.ID,
			Title: target.Title,
			URL:   target.URL,
		}
		if err := probeCDPTarget(ctx, browserWSURL, target.ID, timeout); err != nil {
			probe.Status = "STALL"
			probe.Error = err.Error()
			diagnosis.stalledCount++
		} else {
			probe.Status = "OK"
		}
		diagnosis.targets = append(diagnosis.targets, probe)
	}
	return diagnosis, nil
}

type cdpTargetInfo struct {
	ID    string `json:"targetId"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func getCDPTargetInfos(ctx context.Context, browserWSURL string, timeout time.Duration) ([]cdpTargetInfo, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, browserWSURL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var result struct {
		TargetInfos []cdpTargetInfo `json:"targetInfos"`
	}
	if err := callCDP(conn, 1, "", "Target.getTargets", nil, timeout, &result); err != nil {
		return nil, err
	}
	return result.TargetInfos, nil
}

func probeCDPTarget(ctx context.Context, browserWSURL, targetID string, timeout time.Duration) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, browserWSURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := callCDP(conn, 1, "", "Target.attachToTarget", map[string]interface{}{
		"targetId": targetID,
		"flatten":  true,
	}, timeout, &attached); err != nil {
		return fmt.Errorf("attach: %w", err)
	}
	defer func() {
		_ = callCDP(conn, 3, "", "Target.detachFromTarget", map[string]interface{}{
			"sessionId": attached.SessionID,
		}, timeout, nil)
	}()

	if attached.SessionID == "" {
		return fmt.Errorf("attach returned empty sessionId")
	}
	if err := callCDP(conn, 2, attached.SessionID, "Runtime.evaluate", map[string]interface{}{
		"expression":    "1+1",
		"returnByValue": true,
	}, timeout, nil); err != nil {
		return fmt.Errorf("runtime evaluate: %w", err)
	}
	return nil
}

type cdpEnvelope struct {
	ID        int                    `json:"id"`
	Method    string                 `json:"method,omitempty"`
	Params    map[string]interface{} `json:"params,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
	Result    json.RawMessage        `json:"result,omitempty"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func callCDP(conn *websocket.Conn, id int, sessionID, method string, params map[string]interface{}, timeout time.Duration, result interface{}) error {
	msg := cdpEnvelope{
		ID:        id,
		Method:    method,
		Params:    params,
		SessionID: sessionID,
	}
	deadline := time.Now().Add(timeout)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if err := conn.WriteJSON(msg); err != nil {
		return err
	}
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return err
		}
		var resp cdpEnvelope
		if err := conn.ReadJSON(&resp); err != nil {
			return err
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("CDP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return err
			}
		}
		return nil
	}
}

func printCDPTargetDiagnosis(diagnosis cdpTargetDiagnosis) {
	counts := make(map[string]int)
	for _, target := range diagnosis.targets {
		counts[target.Type]++
	}
	types := make([]string, 0, len(counts))
	for typ := range counts {
		types = append(types, typ)
	}
	sort.Strings(types)
	fmt.Printf("Raw CDP target probe: targets=%d stalled=%d", len(diagnosis.targets), diagnosis.stalledCount)
	for _, typ := range types {
		fmt.Printf(" %s=%d", typ, counts[typ])
	}
	fmt.Println()
	for _, target := range diagnosis.targets {
		if target.Status != "STALL" {
			continue
		}
		fmt.Printf("STALL type=%s id=%s title=%q url=%q err=%s\n", target.Type, target.ID, target.Title, target.URL, target.Error)
	}
}
