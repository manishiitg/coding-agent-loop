package step_based_workflow

import "testing"

func TestGoalWorkPlatformToolsFollowPermissions(t *testing.T) {
	if !goalWorkToolAllowed("search_platform", goalWorkPermissions{}) {
		t.Fatal("platform search is read-only context and must always be allowed")
	}
	if goalWorkToolAllowed("ask_platform_crew", goalWorkPermissions{}) {
		t.Fatal("Crew work must need the Run permission")
	}
	if !goalWorkToolAllowed("ask_platform_crew", goalWorkPermissions{Run: true}) {
		t.Fatal("Crew work must be allowed with the Run permission")
	}
}
