package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// kbGraphMutex serializes reads and writes of knowledgebase/graph.json across
// all concurrent kb_upsert_* tool invocations within the process.
var kbGraphMutex sync.Mutex

type kbSource struct {
	Step string `json:"step"`
	Run  string `json:"run"`
}

type kbEntity struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Label      string                 `json:"label"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
	Source     kbSource               `json:"source"`
}

type kbRelationship struct {
	ID         string                 `json:"id"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
	Source     kbSource               `json:"source"`
}

type kbGraph struct {
	Version       string           `json:"version"`
	UpdatedAt     string           `json:"updated_at"`
	Entities      []kbEntity       `json:"entities"`
	Relationships []kbRelationship `json:"relationships"`
}

type kbIndex struct {
	EntityCount       int      `json:"entity_count"`
	RelationshipCount int      `json:"relationship_count"`
	EntityTypes       []string `json:"entity_types"`
	RelationshipTypes []string `json:"relationship_types"`
	LastUpdated       string   `json:"last_updated"`
	LastUpdatedBy     kbSource `json:"last_updated_by"`
}

func kbRelationshipID(from, typ, to string) string {
	return fmt.Sprintf("rel-%s-%s-%s", from, typ, to)
}

func readKBGraph(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*kbGraph, error) {
	path := normalizePathForWorkspaceAPI(filepath.Join("knowledgebase", "graph.json"), workspacePath)
	content, err := readFile(ctx, path)
	if err != nil || content == "" {
		return &kbGraph{Version: "1", Entities: []kbEntity{}, Relationships: []kbRelationship{}}, nil
	}
	var g kbGraph
	if err := json.Unmarshal([]byte(content), &g); err != nil {
		return nil, fmt.Errorf("failed to parse knowledgebase/graph.json: %w", err)
	}
	if g.Entities == nil {
		g.Entities = []kbEntity{}
	}
	if g.Relationships == nil {
		g.Relationships = []kbRelationship{}
	}
	if g.Version == "" {
		g.Version = "1"
	}
	return &g, nil
}

// writeKBGraphAndIndex writes graph.json + index.json. Callers must already hold kbGraphMutex.
func writeKBGraphAndIndex(ctx context.Context, workspacePath string, g *kbGraph, source kbSource, writeFile func(context.Context, string, string) error) error {
	now := time.Now().UTC().Format(time.RFC3339)
	g.UpdatedAt = now

	if err := validateKBGraph(g); err != nil {
		return err
	}

	graphBytes, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal graph: %w", err)
	}
	graphPath := normalizePathForWorkspaceAPI(filepath.Join("knowledgebase", "graph.json"), workspacePath)
	if err := writeFile(ctx, graphPath, string(graphBytes)); err != nil {
		return fmt.Errorf("failed to write knowledgebase/graph.json: %w", err)
	}

	idx := buildKBIndex(g, source, now)
	indexBytes, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}
	indexPath := normalizePathForWorkspaceAPI(filepath.Join("knowledgebase", "index.json"), workspacePath)
	if err := writeFile(ctx, indexPath, string(indexBytes)); err != nil {
		return fmt.Errorf("failed to write knowledgebase/index.json: %w", err)
	}
	return nil
}

func buildKBIndex(g *kbGraph, source kbSource, now string) *kbIndex {
	return &kbIndex{
		EntityCount:       len(g.Entities),
		RelationshipCount: len(g.Relationships),
		EntityTypes:       collectEntityTypes(g),
		RelationshipTypes: collectRelationshipTypes(g),
		LastUpdated:       now,
		LastUpdatedBy:     source,
	}
}

// validateKBGraph enforces invariants expected by consumers of graph.json: every
// entity/relationship has a unique id, every relationship endpoint resolves to an
// existing entity, and required fields are present.
func validateKBGraph(g *kbGraph) error {
	entityIDs := map[string]struct{}{}
	for i, e := range g.Entities {
		if e.ID == "" {
			return fmt.Errorf("entities[%d]: missing id", i)
		}
		if e.Type == "" {
			return fmt.Errorf("entities[%d] (id=%s): missing type", i, e.ID)
		}
		if _, dup := entityIDs[e.ID]; dup {
			return fmt.Errorf("entities[%d]: duplicate id %q", i, e.ID)
		}
		entityIDs[e.ID] = struct{}{}
	}
	relIDs := map[string]struct{}{}
	for i, r := range g.Relationships {
		if r.ID == "" {
			return fmt.Errorf("relationships[%d]: missing id", i)
		}
		if r.From == "" || r.To == "" || r.Type == "" {
			return fmt.Errorf("relationships[%d] (id=%s): missing required field (from/to/type)", i, r.ID)
		}
		if _, dup := relIDs[r.ID]; dup {
			return fmt.Errorf("relationships[%d]: duplicate id %q", i, r.ID)
		}
		relIDs[r.ID] = struct{}{}
		if _, ok := entityIDs[r.From]; !ok {
			return fmt.Errorf("relationships[%d] (id=%s): from=%q references missing entity", i, r.ID, r.From)
		}
		if _, ok := entityIDs[r.To]; !ok {
			return fmt.Errorf("relationships[%d] (id=%s): to=%q references missing entity", i, r.ID, r.To)
		}
	}
	return nil
}

// deepMerge applies the merge policy between two JSON-decoded values:
//   - Both maps: recurse per-key
//   - Both slices: union-dedupe (objects with an "id" are merged by id, others by deep-equality)
//   - Otherwise: incoming wins (last-write-wins for scalars and type mismatches)
func deepMerge(existing, incoming interface{}) interface{} {
	if existingMap, ok := existing.(map[string]interface{}); ok {
		if incomingMap, ok2 := incoming.(map[string]interface{}); ok2 {
			out := make(map[string]interface{}, len(existingMap)+len(incomingMap))
			for k, v := range existingMap {
				out[k] = v
			}
			for k, v := range incomingMap {
				if prev, had := out[k]; had {
					out[k] = deepMerge(prev, v)
				} else {
					out[k] = v
				}
			}
			return out
		}
	}
	if existingArr, ok := existing.([]interface{}); ok {
		if incomingArr, ok2 := incoming.([]interface{}); ok2 {
			return unionDedupe(existingArr, incomingArr)
		}
	}
	return incoming
}

func unionDedupe(a, b []interface{}) []interface{} {
	byID := map[string]map[string]interface{}{}
	idOrder := []string{}
	other := []interface{}{}

	addObject := func(obj map[string]interface{}) {
		if id, ok := obj["id"].(string); ok && id != "" {
			if prev, exists := byID[id]; exists {
				if merged, ok := deepMerge(prev, obj).(map[string]interface{}); ok {
					byID[id] = merged
				} else {
					byID[id] = obj
				}
			} else {
				byID[id] = obj
				idOrder = append(idOrder, id)
			}
			return
		}
		for _, o := range other {
			if reflect.DeepEqual(o, obj) {
				return
			}
		}
		other = append(other, obj)
	}

	addValue := func(v interface{}) {
		if obj, isObj := v.(map[string]interface{}); isObj {
			addObject(obj)
			return
		}
		for _, existing := range other {
			if reflect.DeepEqual(existing, v) {
				return
			}
		}
		other = append(other, v)
	}

	for _, v := range a {
		addValue(v)
	}
	for _, v := range b {
		addValue(v)
	}

	out := make([]interface{}, 0, len(idOrder)+len(other))
	for _, id := range idOrder {
		out = append(out, byID[id])
	}
	out = append(out, other...)
	return out
}

func mergeProperties(existing, incoming map[string]interface{}) map[string]interface{} {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}
	if merged, ok := deepMerge(existing, incoming).(map[string]interface{}); ok {
		return merged
	}
	return incoming
}

func diffPropertyKeys(existing, incoming map[string]interface{}) []string {
	changed := []string{}
	for k, v := range incoming {
		prev, had := existing[k]
		if !had || !reflect.DeepEqual(prev, v) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

func collectEntityTypes(g *kbGraph) []string {
	seen := map[string]struct{}{}
	for _, e := range g.Entities {
		if e.Type != "" {
			seen[e.Type] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func collectRelationshipTypes(g *kbGraph) []string {
	seen := map[string]struct{}{}
	for _, r := range g.Relationships {
		if r.Type != "" {
			seen[r.Type] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func marshalToolResult(result map[string]interface{}) (string, error) {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool result: %w", err)
	}
	return string(b), nil
}

func getKBUpsertEntitySchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable, content-derived entity id using the convention \"{type}-{slug}\" (e.g. \"company-acme\", \"person-jane-doe\", \"product-widget-v2\"). Reuse the exact existing id when updating — id is the match key; a different id creates a new entity."
			},
			"type": {
				"type": "string",
				"description": "REQUIRED: Entity kind (e.g. company, person, product, event). Check known_types in the tool response to keep vocabulary consistent across steps — prefer existing types over new variants (e.g. reuse \"company\" rather than introducing \"organization\")."
			},
			"label": {
				"type": "string",
				"description": "REQUIRED: Short human-readable name. Last-write-wins on updates; pass the canonical form."
			},
			"properties": {
				"type": "object",
				"description": "OPTIONAL: Arbitrary key/value properties. On update, deep-merged into existing properties: scalars are last-write-wins, arrays union-dedupe, nested objects recurse.",
				"additionalProperties": true
			}
		},
		"required": ["id", "type", "label"]
	}`
}

func getKBUpsertRelationshipSchema() string {
	return `{
		"type": "object",
		"properties": {
			"from": {
				"type": "string",
				"description": "REQUIRED: Source entity id. Must reference an entity that already exists in knowledgebase/graph.json — upsert it first if needed."
			},
			"to": {
				"type": "string",
				"description": "REQUIRED: Target entity id. Must reference an entity that already exists in knowledgebase/graph.json — upsert it first if needed."
			},
			"type": {
				"type": "string",
				"description": "REQUIRED: Verb-phrase relationship type (e.g. has_contact, owns, competes_with, paid_to). Check known_relationship_types in the tool response to keep vocabulary consistent."
			},
			"id": {
				"type": "string",
				"description": "OPTIONAL: Relationship id. If omitted, auto-derived as \"rel-<from>-<type>-<to>\"."
			},
			"properties": {
				"type": "object",
				"description": "OPTIONAL: Arbitrary key/value properties. On update, deep-merged into existing properties: scalars are last-write-wins, arrays union-dedupe, nested objects recurse.",
				"additionalProperties": true
			}
		},
		"required": ["from", "to", "type"]
	}`
}

func createKBUpsertEntityExecutor(
	workspacePath, stepID, runFolder string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["id"].(string)
		entType, _ := args["type"].(string)
		label, _ := args["label"].(string)
		if id == "" {
			return "", fmt.Errorf("id is required")
		}
		if entType == "" {
			return "", fmt.Errorf("type is required")
		}
		if label == "" {
			return "", fmt.Errorf("label is required")
		}
		var incomingProps map[string]interface{}
		if raw, ok := args["properties"].(map[string]interface{}); ok {
			incomingProps = raw
		}

		kbGraphMutex.Lock()
		defer kbGraphMutex.Unlock()

		graph, err := readKBGraph(ctx, workspacePath, readFile)
		if err != nil {
			return "", err
		}

		source := kbSource{Step: stepID, Run: runFolder}
		now := time.Now().UTC().Format(time.RFC3339)

		op := "created"
		var changed []string
		found := -1
		for i := range graph.Entities {
			if graph.Entities[i].ID == id {
				found = i
				break
			}
		}
		if found >= 0 {
			existing := &graph.Entities[found]
			if existing.Type != entType {
				return "", fmt.Errorf("type_mismatch: entity %q already has type %q; refusing to change to %q (use a different id for a distinct entity, or keep the type)", id, existing.Type, entType)
			}
			op = "updated"
			changed = diffPropertyKeys(existing.Properties, incomingProps)
			if label != "" && label != existing.Label {
				if !containsString(changed, "label") {
					changed = append(changed, "label")
					sort.Strings(changed)
				}
				existing.Label = label
			}
			existing.Properties = mergeProperties(existing.Properties, incomingProps)
			existing.UpdatedAt = now
			existing.Source = source
		} else {
			graph.Entities = append(graph.Entities, kbEntity{
				ID:         id,
				Type:       entType,
				Label:      label,
				Properties: incomingProps,
				CreatedAt:  now,
				UpdatedAt:  now,
				Source:     source,
			})
		}

		if err := writeKBGraphAndIndex(ctx, workspacePath, graph, source, writeFile); err != nil {
			return "", err
		}

		logger.Info(fmt.Sprintf("🧠 kb_upsert_entity %s id=%s type=%s (total entities=%d)", op, id, entType, len(graph.Entities)))

		return marshalToolResult(map[string]interface{}{
			"op":                 op,
			"id":                 id,
			"properties_changed": changed,
			"known_types":        collectEntityTypes(graph),
		})
	}
}

func createKBUpsertRelationshipExecutor(
	workspacePath, stepID, runFolder string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		from, _ := args["from"].(string)
		to, _ := args["to"].(string)
		relType, _ := args["type"].(string)
		if from == "" {
			return "", fmt.Errorf("from is required")
		}
		if to == "" {
			return "", fmt.Errorf("to is required")
		}
		if relType == "" {
			return "", fmt.Errorf("type is required")
		}
		id, _ := args["id"].(string)
		if id == "" {
			id = kbRelationshipID(from, relType, to)
		}
		var incomingProps map[string]interface{}
		if raw, ok := args["properties"].(map[string]interface{}); ok {
			incomingProps = raw
		}

		kbGraphMutex.Lock()
		defer kbGraphMutex.Unlock()

		graph, err := readKBGraph(ctx, workspacePath, readFile)
		if err != nil {
			return "", err
		}

		entityIndex := make(map[string]struct{}, len(graph.Entities))
		for _, e := range graph.Entities {
			entityIndex[e.ID] = struct{}{}
		}
		missing := []string{}
		if _, ok := entityIndex[from]; !ok {
			missing = append(missing, from)
		}
		if _, ok := entityIndex[to]; !ok && to != from {
			missing = append(missing, to)
		}
		if len(missing) > 0 {
			return marshalToolResult(map[string]interface{}{
				"error":   "missing_entity",
				"missing": missing,
				"message": fmt.Sprintf("relationship requires existing entities for from=%q and to=%q; upsert the missing entity(ies) first: %v", from, to, missing),
			})
		}

		source := kbSource{Step: stepID, Run: runFolder}
		now := time.Now().UTC().Format(time.RFC3339)

		op := "created"
		var changed []string
		found := -1
		for i := range graph.Relationships {
			if graph.Relationships[i].ID == id {
				found = i
				break
			}
		}
		if found >= 0 {
			existing := &graph.Relationships[found]
			if existing.Type != relType {
				return "", fmt.Errorf("type_mismatch: relationship %q already has type %q; refusing to change to %q", id, existing.Type, relType)
			}
			if existing.From != from || existing.To != to {
				return "", fmt.Errorf("endpoint_mismatch: relationship %q already has from=%q to=%q; refusing to change endpoints (use a different id for a distinct edge)", id, existing.From, existing.To)
			}
			op = "updated"
			changed = diffPropertyKeys(existing.Properties, incomingProps)
			existing.Properties = mergeProperties(existing.Properties, incomingProps)
			existing.UpdatedAt = now
			existing.Source = source
		} else {
			graph.Relationships = append(graph.Relationships, kbRelationship{
				ID:         id,
				From:       from,
				To:         to,
				Type:       relType,
				Properties: incomingProps,
				CreatedAt:  now,
				UpdatedAt:  now,
				Source:     source,
			})
		}

		if err := writeKBGraphAndIndex(ctx, workspacePath, graph, source, writeFile); err != nil {
			return "", err
		}

		logger.Info(fmt.Sprintf("🔗 kb_upsert_relationship %s id=%s type=%s (total relationships=%d)", op, id, relType, len(graph.Relationships)))

		return marshalToolResult(map[string]interface{}{
			"op":                       op,
			"id":                       id,
			"properties_changed":       changed,
			"known_relationship_types": collectRelationshipTypes(graph),
		})
	}
}

// BuildStepKBGuidance returns the detailed KB contribution guidance to splice into
// a step agent's system prompt when it's responsible for writing KB itself.
//
// Returns an empty string unless writeMethod is "direct" AND kbAccess allows writes —
// in every other case (read-only, agent-mode, disabled) the step is not the writer
// and this block must not appear. When returned, the block is scoped to contribType:
// graph_only hides notes guidance, notes_only hides tool/ID-convention guidance,
// both emits the full block. Per-step knowledgebase_contribution is appended as a
// contract when non-empty.
//
// Schema details deliberately stay in the kb_upsert_* tool descriptions so the
// prompt block stays compact instead of duplicating JSON structure.
func BuildStepKBGuidance(kbAccess, writeMethod, contribType, kbContribution string) string {
	if writeMethod != KBWriteMethodDirect {
		return ""
	}
	if !kbAccessAllowsWrite(kbAccess) {
		return ""
	}

	graphEnabled := kbContributionAllowsGraph(contribType)
	notesEnabled := kbContributionAllowsNotes(contribType)
	if !graphEnabled && !notesEnabled {
		return ""
	}

	var b strings.Builder
	switch {
	case graphEnabled && notesEnabled:
		b.WriteString("\n### Knowledgebase contribution (DIRECT write — graph + notes)\n")
	case graphEnabled:
		b.WriteString("\n### Knowledgebase contribution (DIRECT write — graph only)\n")
	case notesEnabled:
		b.WriteString("\n### Knowledgebase contribution (DIRECT write — notes only)\n")
	}
	b.WriteString("You are the sole writer of KB for this step — the post-step KB update agent does NOT run. Contribute inline, then finish the step.\n\n")

	b.WriteString("**Scope:**\n")
	if graphEnabled {
		b.WriteString("- **Atomic facts** (entities, typed relationships) → `kb_upsert_entity` / `kb_upsert_relationship` tools. They write `graph.json` + `index.json` atomically. You cannot write those files via shell — the folder guard blocks it; tools are the only path.\n")
	} else {
		b.WriteString("- **Atomic facts (graph)** — OUT OF SCOPE for this step. The `kb_upsert_*` tools are not registered and `graph.json` / `index.json` are not writable.\n")
	}
	if notesEnabled {
		b.WriteString("- **Narrative analysis** (paragraphs of context, hypotheses, cross-cutting patterns) → per-topic markdown under `knowledgebase/notes/`. Write with shell + `diff_patch_workspace_file`. Keep `notes/_index.json` in sync — bump `size_bytes`, `section_count`, `last_updated`, `last_updated_by`, and merge `covers[]`.\n")
	} else {
		b.WriteString("- **Narrative notes** — OUT OF SCOPE for this step. The `notes/` folder is not writable; do not attempt to shell into it.\n")
	}
	b.WriteString("\n")

	if graphEnabled {
		b.WriteString("**ID conventions (stable, content-derived — match keys for upsert):**\n")
		b.WriteString("- Entity id: `{type}-{slug}` (e.g. `company-acme`, `person-jane-doe`, `product-widget-v2`). Reuse the exact existing id when updating.\n")
		b.WriteString("- Relationship id: auto-derived `rel-{from}-{type}-{to}` unless you supply one.\n")
		if notesEnabled {
			b.WriteString("- Topic id: the entity id for per-entity narrative (`company-acme.md`), or `pattern-<slug>` for cross-cutting patterns (`pattern-tax-cycle.md`).\n")
		}
		b.WriteString("\n")
	} else if notesEnabled {
		b.WriteString("**Topic ID conventions:**\n")
		b.WriteString("- Entity-scoped narrative: topic id = entity id (e.g. `company-acme.md`). Reference graph entities by id inside the note so reorganize/consolidation can resolve the link.\n")
		b.WriteString("- Cross-cutting pattern: topic id = `pattern-<slug>` (e.g. `pattern-tax-cycle.md`).\n\n")
	}

	b.WriteString("**Discipline:**\n")
	discipline := []string{}
	discipline = append(discipline, "**Read first.** Before any write, `cat` / `jq` the current KB state you plan to touch — reuse existing ids, types, and topic names to avoid drift (`company` vs `organization`).")
	if graphEnabled {
		discipline = append(discipline, "**Merge, don't clobber.** `kb_upsert_*` deep-merges properties: scalars last-write-wins, arrays union-dedupe, nested objects recurse. Entity `type` cannot change on update — use a different id for a distinct entity.")
		discipline = append(discipline, "**Relationship endpoints must exist.** `kb_upsert_relationship` returns `{error: \"missing_entity\", missing: [...]}` if `from` / `to` aren't in the graph. Upsert the missing entity first, then the relationship.")
		discipline = append(discipline, "**Provenance is automatic.** Source `{step, run}` is stamped by the tool — don't put those fields in arguments. Check `known_types` / `known_relationship_types` in every tool response.")
	}
	if notesEnabled {
		discipline = append(discipline, "**Append, don't rewrite.** For existing notes, append a new `## <date-or-section>` block via `diff_patch_workspace_file` rather than replacing the file. Update `notes/_index.json` after every note write.")
	}
	discipline = append(discipline, "**No deletes.** Never remove entries from earlier steps/runs. Refinement only.")
	for i, d := range discipline {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, d))
	}

	if trimmed := strings.TrimSpace(kbContribution); trimmed != "" {
		b.WriteString("\n**For this step specifically, contribute:**\n")
		b.WriteString(trimmed)
		b.WriteString("\n\nIf the contribution above names something you cannot verify from the step's work, skip it — do not invent entities or fabricate narrative. Partial output is fine; hallucinated output is not.\n")
	} else {
		b.WriteString("\n(No `knowledgebase_contribution` was declared for this step, so contribute whatever atomic facts the step surfaced that a future step would want to query.)\n")
	}

	return b.String()
}

// BuildKBContributionReviewMessage returns the one-shot user message injected
// after a step's first successful completion in direct-write mode, asking the
// step agent to verify its KB contributions against the author's contract.
//
// Returns an empty string when a review is not warranted:
//   - writeMethod != "direct" (nothing to self-verify in agent mode)
//   - kbAccess does not permit writes
//   - contribType is empty / unknown
//   - contribution is empty (no contract to check against)
//
// The message is scope-aware: graph_only asks about graph only, notes_only asks
// about notes only, both asks about both. It explicitly tells the agent this is
// the final turn for KB work — no self-nudging loop.
func BuildKBContributionReviewMessage(kbAccess, writeMethod, contribType, contribution string) string {
	if writeMethod != KBWriteMethodDirect {
		return ""
	}
	if !kbAccessAllowsWrite(kbAccess) {
		return ""
	}
	trimmed := strings.TrimSpace(contribution)
	if trimmed == "" {
		return ""
	}
	graphEnabled := kbContributionAllowsGraph(contribType)
	notesEnabled := kbContributionAllowsNotes(contribType)
	if !graphEnabled && !notesEnabled {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Knowledgebase Contribution Self-Review (final turn)\n\n")
	b.WriteString("The step's output validation passed — before it's accepted, verify you fulfilled your `knowledgebase_contribution` contract.\n\n")

	b.WriteString("**Enumerate what you contributed:**\n")
	if graphEnabled {
		b.WriteString("- Entities you upserted (list their ids and types).\n")
		b.WriteString("- Relationships you upserted (list their ids, or `from → type → to`).\n")
	}
	if notesEnabled {
		b.WriteString("- Topics you wrote or updated under `notes/` (list the markdown filenames and which sections you added).\n")
	}
	b.WriteString("\n")

	b.WriteString("**Compare against the contract.** If anything required is missing:\n")
	if graphEnabled {
		b.WriteString("- Call `kb_upsert_entity` / `kb_upsert_relationship` now to close the gap.\n")
	}
	if notesEnabled {
		b.WriteString("- Use shell + `diff_patch_workspace_file` under `notes/` to add the missing narrative, and update `notes/_index.json`.\n")
	}
	b.WriteString("If every requirement is already covered, reply with a short summary of what you contributed — no further tool calls needed.\n\n")

	b.WriteString("**Contract:**\n")
	b.WriteString(trimmed)
	b.WriteString("\n\n")

	b.WriteString("**Important:** this is your final turn for KB work on this step. After this response, the step will be accepted regardless of any further gaps — there is no second review. Do not invent facts the step did not actually establish; partial coverage is better than fabricated coverage.\n")

	return b.String()
}

// RegisterKBTools registers kb_upsert_entity and kb_upsert_relationship on the
// given MCP agent. Call from agent factories when kbAccess == "read-write";
// stepID and runFolder are captured in the executor closures so every write is
// stamped with source {step, run} automatically — callers must NOT expose those
// fields as tool parameters.
func RegisterKBTools(
	mcpAgent *mcpagent.Agent,
	workspacePath, stepID, runFolder string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) error {
	entitySchema := getKBUpsertEntitySchema()
	entityParams, err := parseSchemaForToolParameters(entitySchema)
	if err != nil {
		return fmt.Errorf("failed to parse kb_upsert_entity schema: %w", err)
	}
	entityDesc := "Create or update an entity in knowledgebase/graph.json. Upsert semantics: if an entity with the given id exists, properties are deep-merged (scalars last-write-wins, arrays union-dedupe, nested objects recurse) and label is updated; otherwise a new entity is created. The step_id and run_folder are stamped automatically as source — do not include them in arguments. Type cannot change on update (returns a type_mismatch error). Entity id should be stable and content-derived (e.g. \"company-acme\") so future steps upsert to the same id rather than duplicating. Returns op (created|updated), id, properties_changed, and known_types (existing entity types in the graph — prefer reusing these to avoid drift)."
	if err := mcpAgent.RegisterCustomTool(
		"kb_upsert_entity",
		entityDesc,
		entityParams,
		createKBUpsertEntityExecutor(workspacePath, stepID, runFolder, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register kb_upsert_entity: %w", err)
	}

	relSchema := getKBUpsertRelationshipSchema()
	relParams, err := parseSchemaForToolParameters(relSchema)
	if err != nil {
		return fmt.Errorf("failed to parse kb_upsert_relationship schema: %w", err)
	}
	relDesc := "Create or update a relationship in knowledgebase/graph.json. Upsert semantics: if a relationship with the derived/given id exists, properties are deep-merged; otherwise a new relationship is created. Id defaults to \"rel-<from>-<type>-<to>\" when not provided. from and to must reference existing entities — if either is missing the tool returns a structured {error: \"missing_entity\", missing: [...]} so you can upsert the missing endpoint(s) first. Source (step, run) is stamped automatically. Returns op, id, properties_changed, and known_relationship_types."
	if err := mcpAgent.RegisterCustomTool(
		"kb_upsert_relationship",
		relDesc,
		relParams,
		createKBUpsertRelationshipExecutor(workspacePath, stepID, runFolder, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register kb_upsert_relationship: %w", err)
	}

	return nil
}
