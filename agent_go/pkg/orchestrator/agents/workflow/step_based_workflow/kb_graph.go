package step_based_workflow

// Knowledgebase graph files. The LLM KB update agent owns graph.json and index.json
// end-to-end — Go only seeds empty valid files on workspace init so the agent's first
// read sees parseable JSON. Schema, merging, index upkeep are all the agent's job;
// if the prompt's schema changes, update the seed literals below to match.

import (
	"context"
	"fmt"
	"path/filepath"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

const (
	KBGraphFileName      = "graph.json"
	KBIndexFileName      = "index.json"
	KBNotesFolderName    = "notes"      // sibling of graph.json — per-topic narrative markdown
	KBNotesIndexFileName = "_index.json" // registry of topic files inside notes/
)

const (
	emptyGraphJSON = `{
  "version": "1",
  "entities": [],
  "relationships": []
}
`
	emptyIndexJSON = `{
  "entity_count": 0,
  "relationship_count": 0,
  "entity_types": [],
  "relationship_types": []
}
`
	// emptyNotesIndexJSON seeds notes/_index.json — a registry of per-topic markdown
	// files inside knowledgebase/notes/. The KB update agent appends/updates entries
	// when it writes narrative analysis; consumers (steps with KB read access, builder
	// review tools) read this first to find relevant topic files without scanning the
	// whole folder.
	emptyNotesIndexJSON = `{
  "topics": []
}
`
)

// InitKBGraphFiles seeds the empty KB artifacts that the declared shape calls
// for. Safe to call repeatedly — existing files are left alone so agent-written
// content survives. Shape controls which artifacts exist:
//
//   - KBShapeGraphNotes (default): graph.json + index.json + notes/_index.json
//   - KBShapeNotesOnly:            notes/_index.json only
//
// For notes-only we intentionally do NOT touch any pre-existing graph.json/
// index.json — migration of shape is handled separately.
func InitKBGraphFiles(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspaceRoot, kbShape string) error {
	shape := workflowtypes.ResolveKBShape(kbShape)

	if shape == workflowtypes.KBShapeGraphNotes {
		graphPath := filepath.Join(workspaceRoot, KnowledgebaseFolderName, KBGraphFileName)
		if exists, _ := bo.CheckWorkspaceFileExists(ctx, graphPath); !exists {
			if err := bo.WriteWorkspaceFile(ctx, graphPath, emptyGraphJSON); err != nil {
				return fmt.Errorf("init graph.json: %w", err)
			}
		}
		indexPath := filepath.Join(workspaceRoot, KnowledgebaseFolderName, KBIndexFileName)
		if exists, _ := bo.CheckWorkspaceFileExists(ctx, indexPath); !exists {
			if err := bo.WriteWorkspaceFile(ctx, indexPath, emptyIndexJSON); err != nil {
				return fmt.Errorf("init index.json: %w", err)
			}
		}
	}

	// notes/_index.json is seeded for every non-empty shape — both shapes use narrative notes.
	notesIndexPath := filepath.Join(workspaceRoot, KnowledgebaseFolderName, KBNotesFolderName, KBNotesIndexFileName)
	if exists, _ := bo.CheckWorkspaceFileExists(ctx, notesIndexPath); !exists {
		if err := bo.WriteWorkspaceFile(ctx, notesIndexPath, emptyNotesIndexJSON); err != nil {
			return fmt.Errorf("init notes/_index.json: %w", err)
		}
	}
	return nil
}
