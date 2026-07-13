package step_based_workflow

import "github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"

func (hcpo *StepBasedWorkflowOrchestrator) effectiveBrowserModeForHostDownloads() string {
	if mode := hcpo.GetBrowserMode(); mode != "" {
		return mode
	}
	if hcpo.GetCdpPort() > 0 {
		return "cdp"
	}
	return ""
}

func (hcpo *StepBasedWorkflowOrchestrator) appendCDPHostDownloadsReadPath(readPaths []string) []string {
	hostDownloads := common.CDPHostDownloadsReadPath(hcpo.effectiveBrowserModeForHostDownloads())
	if hostDownloads == "" {
		return common.DeduplicateStrings(readPaths)
	}
	return common.DeduplicateStrings(append(readPaths, hostDownloads))
}

func (hcpo *StepBasedWorkflowOrchestrator) grantSessionCDPHostDownloadsReadOnly(sessionID string) {
	hostDownloads := common.GrantSessionCDPHostDownloadsReadOnly(sessionID, hcpo.effectiveBrowserModeForHostDownloads())
	if hostDownloads != "" && hcpo.GetLogger() != nil {
		hcpo.GetLogger().Info("Added read-only CDP host Downloads grant: " + hostDownloads)
	}
}
