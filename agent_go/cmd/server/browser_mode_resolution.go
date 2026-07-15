package server

import (
	"strings"
)

const defaultCDPPort = 9222

func configuredCDPPorts(primary *int, additional []int) []int {
	return getCdpPorts(QueryRequest{
		BrowserMode: "cdp",
		CdpPort:     primary,
		CdpPorts:    additional,
	})
}

func configuredCDPPortsForMode(mode string, primary *int, additional []int) []int {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto", "cdp":
		return configuredCDPPorts(primary, additional)
	default:
		return nil
	}
}
