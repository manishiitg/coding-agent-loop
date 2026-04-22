package step_based_workflow

// Knowledgebase files. Notes-only KB model: the knowledgebase is a set of
// per-topic markdown files under knowledgebase/notes/ plus notes/_index.json
// as a registry. There is no graph.json / index.json surface. Go only seeds
// an empty _index.json on workspace init so the agent's first read sees
// parseable JSON; all subsequent writes (content + _index.json sync) are the
// agent's job (either the post-step KB update agent in agent mode, or the
// step agent itself in direct mode).

import (
	"context"
	"fmt"
	"path/filepath"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

const (
	KBNotesFolderName    = "notes"       // per-topic narrative markdown under knowledgebase/
	KBNotesIndexFileName = "_index.json" // registry of topic files inside notes/
)

// emptyNotesIndexJSON seeds notes/_index.json — a registry of per-topic markdown
// files inside knowledgebase/notes/. Agents append/update entries when they write
// narrative analysis; consumers (steps with KB read access, builder review tools)
// read this first to find relevant topic files without scanning the whole folder.
const emptyNotesIndexJSON = `{
  "topics": []
}
`

// InitKBGraphFiles seeds knowledgebase/notes/_index.json so the first agent
// read sees a parseable registry. Safe to call repeatedly — existing files are
// left alone. The kbShape parameter is accepted for call-site compatibility
// but ignored: notes-only is the only shape now.
func InitKBGraphFiles(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspaceRoot, kbShape string) error {
	_ = kbShape // retained for compatibility; shape is always notes-only
	notesIndexPath := filepath.Join(workspaceRoot, KnowledgebaseFolderName, KBNotesFolderName, KBNotesIndexFileName)
	if exists, _ := bo.CheckWorkspaceFileExists(ctx, notesIndexPath); !exists {
		if err := bo.WriteWorkspaceFile(ctx, notesIndexPath, emptyNotesIndexJSON); err != nil {
			return fmt.Errorf("init notes/_index.json: %w", err)
		}
	}
	return nil
}
