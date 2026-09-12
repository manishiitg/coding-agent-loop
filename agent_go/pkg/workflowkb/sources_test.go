package workflowkb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func fixture(t *testing.T, root, name, id, owner string, sources []workflowtypes.KnowledgebaseSource) string {
	t.Helper()
	ws := "Workflow/" + name
	if err := os.MkdirAll(filepath.Join(root, ws, "knowledgebase", "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Manifest{ID: id, Label: name, CreatedBy: owner, Sources: sources})
	if err := os.WriteFile(filepath.Join(root, ws, "workflow.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ws, "knowledgebase", "notes", "fact.md"), []byte(name), 0644); err != nil {
		t.Fatal(err)
	}
	return ws
}
func ref(id, alias string) workflowtypes.KnowledgebaseSource {
	return workflowtypes.KnowledgebaseSource{WorkflowID: id, Alias: alias, Access: "read"}
}

func TestSourcesLiveReadsMultipleDirectSourcesAndMove(t *testing.T) {
	root := t.TempDir()
	a := fixture(t, root, "consumer", "a", "owner", []workflowtypes.KnowledgebaseSource{ref("b", "rts"), ref("c", "security")})
	b := fixture(t, root, "rts", "b", "owner", []workflowtypes.KnowledgebaseSource{ref("d", "private")})
	fixture(t, root, "security", "c", "owner", nil)
	fixture(t, root, "private", "d", "owner", nil)
	sources, err := Resolve(root, a, nil)
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	for _, s := range sources {
		if !s.Available {
			t.Fatal(s)
		}
	}
	data, err := ReadFile(sources[0], "notes/fact.md")
	if err != nil || string(data) != "rts" {
		t.Fatal(string(data), err)
	}
	os.WriteFile(filepath.Join(root, b, "knowledgebase", "notes", "fact.md"), []byte("updated"), 0644)
	data, err = ReadFile(sources[0], "notes/fact.md")
	if err != nil || string(data) != "updated" {
		t.Fatal("source update was not live", err)
	}
	if err := os.Rename(filepath.Join(root, b), filepath.Join(root, "Workflow", "renamed")); err != nil {
		t.Fatal(err)
	}
	sources, err = Resolve(root, a, nil)
	if err != nil || !sources[0].Available || sources[0].WorkspacePath != "Workflow/renamed" {
		t.Fatal("stable ID did not follow rename", sources, err)
	}
	if _, err := ReadFile(sources[0], "../workflow.json"); err == nil {
		t.Fatal("sibling file exposed")
	}
	fixture(t, root, "clone", "b", "owner", nil)
	sources, err = Resolve(root, a, nil)
	if err != nil || sources[0].Available {
		t.Fatal("ambiguous source was accepted")
	}
}

func TestSourcesOwnershipRevocationAndMissing(t *testing.T) {
	root := t.TempDir()
	a := fixture(t, root, "consumer", "a", "alice", []workflowtypes.KnowledgebaseSource{ref("b", "rts")})
	fixture(t, root, "rts", "b", "bob", nil)
	sources, err := Resolve(root, a, nil)
	if err != nil || sources[0].Available {
		t.Fatal("private source exposed", err)
	}
	raw := []byte(`{"id":"b","access":{"owners":["bob"],"readers":["alice"]}}`)
	os.WriteFile(filepath.Join(root, "Workflow/rts/workflow.json"), raw, 0644)
	sources, err = Resolve(root, a, nil)
	if err != nil || !sources[0].Available {
		t.Fatal("shared reader denied", err)
	}
	fixture(t, root, "rts", "b", "bob", nil)
	sources, err = Resolve(root, a, nil)
	if err != nil || sources[0].Available {
		t.Fatal("source revocation ignored", err)
	}
	fixture(t, root, "rts", "b", "alice", nil)
	os.RemoveAll(filepath.Join(root, "Workflow/rts/knowledgebase"))
	sources, err = Resolve(root, a, nil)
	if err != nil || sources[0].Available || sources[0].Reason == "" {
		t.Fatal("missing source not surfaced", err)
	}
}

func TestSourcesRejectInvalidReferencesAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	a := fixture(t, root, "consumer", "a", "", nil)
	b := fixture(t, root, "rts", "b", "", nil)
	for _, refs := range [][]workflowtypes.KnowledgebaseSource{
		{ref("a", "self")}, {ref("b", "../escape")}, {ref("b", "access")}, {ref("b", "x"), ref("b", "y")}, {ref("b", "x"), ref("c", "x")}, {{WorkflowID: "b", Alias: "x", Access: "write"}},
	} {
		if err := Validate(root, a, refs); err == nil {
			t.Fatal("invalid references accepted", refs)
		}
	}
	if err := os.Symlink(filepath.Join(root, b, "workflow.json"), filepath.Join(root, b, "knowledgebase", "leak")); err != nil {
		t.Fatal(err)
	}
	if err := Validate(root, a, []workflowtypes.KnowledgebaseSource{ref("b", "rts")}); err == nil {
		t.Fatal("escaping symlink accepted")
	}
	if _, err := Resolve(root, "../outside", nil); err == nil {
		t.Fatal("consumer traversal accepted")
	}
}
