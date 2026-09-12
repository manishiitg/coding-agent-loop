package step_based_workflow

import "testing"

func TestWebhookRunFolderAndGroupIsolation(t *testing.T) {
	for _, p := range []string{"iteration-3-hook", "iteration-3-hook/dev"} {
		if got := workshopInternalRunFolderForTarget(p); got != p {
			t.Fatalf("hook folder normalized away: %s", got)
		}
	}
	if got := workshopInternalRunFolderForTarget("iteration-3/dev"); got != "iteration-0/dev" {
		t.Fatalf("ordinary behavior changed: %s", got)
	}
	binding := &WebhookInvocation{RunFolder: "iteration-3-hook"}
	if err := binding.ClaimGroup("dev"); err != nil {
		t.Fatal(err)
	}
	if err := binding.ClaimGroup("dev"); err == nil {
		t.Fatal("same group can overwrite outputs")
	}
	if err := binding.ClaimGroup("prod"); err != nil {
		t.Fatal(err)
	}
}
