package browser

import "github.com/manishiitg/coding-agent-loop/workspace/browserconfig"

const SharedSessionName = browserconfig.SharedSession

func SharedBrowserEnabled() bool   { return browserconfig.SharedEnabled() }
func HeadlessLaunchArgs() []string { return browserconfig.HeadlessArgs() }
