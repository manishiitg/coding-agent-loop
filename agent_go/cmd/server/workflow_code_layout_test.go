package server

import (
	"encoding/json"
	"testing"
)

func TestWorkflowCodeLayoutCreationAndRoundTrip(t *testing.T) {
	fresh := NewWorkflowManifest("New workflow")
	if fresh.CodeLayoutVersion != 1 {
		t.Fatal("new workflow must use code layout v1")
	}
	for _, version := range []int{0, 1} {
		source := NewWorkflowManifest("Source")
		source.CodeLayoutVersion = version
		data, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		var imported WorkflowManifest
		if err := json.Unmarshal(data, &imported); err != nil {
			t.Fatal(err)
		}
		applyManifestDefaults(&imported)
		clone := imported // duplicate route preserves source contract
		if clone.CodeLayoutVersion != version {
			t.Fatalf("import/clone changed layout %d", version)
		}
	}
	fresh.CodeLayoutVersion = 2
	if ValidateManifest(fresh) == nil {
		t.Fatal("unknown layout accepted")
	}
}
