package step_based_workflow

import (
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"testing"
)

func TestWorkshopBrowserMatchesBuilderAcrossGroupsAndWorkflows(t *testing.T) {
	const parent = "browser-test-parent"
	common.BindSessionBrowserIsolation(parent, "alice")
	defer common.ClearSessionShellConfig(parent)
	namespace := common.GetSessionShellConfig(parent).BrowserSessionNamespace
	expected := common.ResolveBrowserSessionID(parent, "main")
	for _, workspace := range []string{"Workflow/one", "Workflow/two"} {
		for _, group := range []string{"default", "ai-news", "parallel-worker"} {
			got := common.PrefixBrowserSessionID(workshopBrowserSessionID(namespace, workspace, group))
			if got != expected {
				t.Fatalf("%s/%s got %s; builder %s", workspace, group, got, expected)
			}
		}
	}
	other := workshopBrowserSessionID(common.BrowserSessionNamespace("bob", "chat"), "Workflow/one", "default")
	if common.PrefixBrowserSessionID(other) == expected {
		t.Fatal("different users share workflow browser")
	}
}
