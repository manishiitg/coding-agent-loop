package server

import (
	"crypto/md5" //nolint:gosec // mirrors cursor CLI's own chat-store directory naming
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
)

// One-time move of every crew from its owner's tree to the shared Crew/ root
// (docs/design/crew_shared_root.md). Deploy scripts run it while the agent is
// stopped; the server also runs it at startup (desktop / dedicated installs).
// Idempotent: a marker records completion, and a crew already at Crew/ is
// never touched again.
//
// Per crew _users/<owner>/Chats/Work/projects/<dir> -> Crew/<dir>:
//  1. product.json gains owner_id; the folder is renamed (same filesystem).
//  2. _system/crew-path-aliases.json maps the old root to the new one, so any
//     reference this pass does not rewrite still resolves (resolveCrewPath).
//  3. Stored references are rewritten: platform JSON files (chat history,
//     workflow/crew manifests and transcripts, Slack config) and TEXT columns
//     in platform SQLite stores. Every spelling is covered: physical,
//     user-relative, absolute, and Claude's encoded project directory.
//  4. Coding-CLI native sessions are copied to the new working directory's
//     store (Claude ~/.claude/projects/<slug>, Cursor ~/.cursor/chats/<md5>
//     and ~/.cursor/projects/<slug>),
//     for $HOME and every provider-connection account home, so resumed crews
//     keep their native context. Pi keeps its sessions inside the crew folder.

const crewRootMigrationMarker = "crew-shared-root-v1.done"

var migrateCrewRootCmd = &cobra.Command{
	Use:   "migrate-crew-root",
	Short: "Move crews from owners' trees to the shared Crew/ root",
	RunE: func(cmd *cobra.Command, _ []string) error {
		docsRoot, _ := cmd.Flags().GetString("docs-root")
		stateRoot, _ := cmd.Flags().GetString("state-root")
		if strings.TrimSpace(docsRoot) == "" {
			docsRoot = fsutil.WorkspaceDocsRoot()
		}
		if strings.TrimSpace(stateRoot) == "" {
			var err error
			if stateRoot, err = workflowCLIStateRoot(); err != nil {
				return err
			}
		}
		apply, _ := cmd.Flags().GetBool("apply")
		force, _ := cmd.Flags().GetBool("force")
		homes, _ := cmd.Flags().GetStringSlice("cli-home")
		report, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docsRoot, StateRoot: stateRoot, Apply: apply, Force: force, CLIHomes: homes})
		if report != nil {
			encoded, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(encoded))
		}
		return err
	},
}

func init() {
	migrateCrewRootCmd.Flags().String("docs-root", "", "absolute workspace documents root")
	migrateCrewRootCmd.Flags().String("state-root", "", "absolute AgentWorks state root")
	migrateCrewRootCmd.Flags().Bool("apply", false, "perform the migration (default: report the plan only)")
	migrateCrewRootCmd.Flags().Bool("force", false, "run even when the one-time marker exists")
	migrateCrewRootCmd.Flags().StringSlice("cli-home", nil, "extra home directories holding coding-CLI session stores (default: $HOME and provider-connection homes)")
}

type crewRootMigrationOptions struct {
	DocsRoot  string
	StateRoot string
	Apply     bool
	Force     bool
	// CLIHomes overrides the discovered coding-CLI homes (tests).
	CLIHomes []string
}

type crewRootMove struct {
	Owner string `json:"owner"`
	Dir   string `json:"dir"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type crewRootMigrationReport struct {
	Applied         bool           `json:"applied"`
	AlreadyDone     bool           `json:"already_done,omitempty"`
	Moves           []crewRootMove `json:"moves"`
	Conflicts       []string       `json:"conflicts,omitempty"`
	JSONFiles       int            `json:"json_files_rewritten"`
	SQLiteRows      int64          `json:"sqlite_rows_rewritten"`
	CLISessionsCopy int            `json:"cli_session_dirs_copied"`
	Warnings        []string       `json:"warnings,omitempty"`
}

// migrateCrewRootAtStartup covers launches without a deploy script. Never
// fatal: a failed pass leaves the marker unwritten and retries next start.
func migrateCrewRootAtStartup(stateRoot string) {
	docsRoot, err := filepath.Abs(fsutil.WorkspaceDocsRoot())
	if err != nil {
		log.Printf("[CREW_ROOT_MIGRATION] cannot resolve docs root: %v", err)
		return
	}
	report, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docsRoot, StateRoot: stateRoot, Apply: true})
	if err != nil {
		log.Printf("[CREW_ROOT_MIGRATION] failed; will retry on next start: %v", err)
		return
	}
	if report != nil && !report.AlreadyDone {
		log.Printf("[CREW_ROOT_MIGRATION] moved %d crew(s), %d conflict(s), rewrote %d JSON file(s) and %d SQLite row(s), copied %d CLI session dir(s)",
			len(report.Moves), len(report.Conflicts), report.JSONFiles, report.SQLiteRows, report.CLISessionsCopy)
	}
}

func migrateCrewRoot(opts crewRootMigrationOptions) (*crewRootMigrationReport, error) {
	docsRoot, err := filepath.Abs(strings.TrimSpace(opts.DocsRoot))
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(docsRoot); err == nil {
		docsRoot = resolved
	}
	if info, err := os.Stat(docsRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workspace docs root %s does not exist", docsRoot)
	}
	stateRoot := filepath.Clean(opts.StateRoot)
	migrationDir := filepath.Join(stateRoot, "migrations")
	marker := filepath.Join(migrationDir, crewRootMigrationMarker)
	report := &crewRootMigrationReport{Applied: opts.Apply}
	if _, err := os.Stat(marker); err == nil && !opts.Force {
		report.AlreadyDone = true
		return report, nil
	}
	if opts.Apply {
		if err := os.MkdirAll(migrationDir, 0o700); err != nil {
			return nil, err
		}
		unlock, err := lockCrewRootMigration(filepath.Join(migrationDir, "crew-shared-root.lock"))
		if err != nil {
			return nil, err
		}
		defer unlock()
	}

	moves, conflicts, err := planCrewRootMoves(docsRoot)
	if err != nil {
		return nil, err
	}
	report.Moves, report.Conflicts = moves, conflicts
	if !opts.Apply {
		return report, nil
	}

	moved := make([]crewRootMove, 0, len(moves))
	for _, move := range moves {
		if err := moveCrewToSharedRoot(docsRoot, move); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", move.From, err))
			continue
		}
		// Recorded per crew: an interrupted pass never leaves a moved crew
		// without its alias.
		if err := recordCrewPathAliases(docsRoot, []crewRootMove{move}); err != nil {
			return report, fmt.Errorf("record crew path alias for %s: %w", move.From, err)
		}
		moved = append(moved, move)
	}
	report.Moves = moved

	// Rewrite for every crew ever moved (the alias file), not only this
	// pass's: a rerun after an interrupted pass finishes the earlier crews.
	// Every step below is idempotent.
	allMoved := crewMovesFromAliasFile(docsRoot)
	replacements := crewRootReplacements(docsRoot, allMoved)
	jsonCount, warnings := rewriteCrewRootJSON(docsRoot, stateRoot, replacements)
	report.JSONFiles = jsonCount
	report.Warnings = append(report.Warnings, warnings...)
	rows, warnings := rewriteCrewRootSQLite(docsRoot, stateRoot, replacements)
	report.SQLiteRows = rows
	report.Warnings = append(report.Warnings, warnings...)
	homes := opts.CLIHomes
	if len(homes) == 0 {
		homes = crewRootCLIHomes()
	}
	copied, warnings := copyCrewCLISessions(docsRoot, homes, allMoved, replacements)
	report.CLISessionsCopy = copied
	report.Warnings = append(report.Warnings, warnings...)

	stamp, _ := json.MarshalIndent(map[string]interface{}{"completed_at": time.Now().UTC().Format(time.RFC3339), "moves": moved, "conflicts": conflicts}, "", "  ")
	if err := writeFileAtomic(marker, append(stamp, '\n'), 0o600); err != nil {
		return report, err
	}
	return report, nil
}

func lockCrewRootMigration(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, nil
}

// planCrewRootMoves finds every work crew still in an owner's tree. A crew
// whose folder name is already taken at Crew/ is a conflict: never merged,
// reported, left where it is (still reachable at its old path).
func planCrewRootMoves(docsRoot string) ([]crewRootMove, []string, error) {
	manifests, err := filepath.Glob(filepath.Join(docsRoot, "_users", "*", "Chats", "Work", "projects", "*", "product.json"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(manifests)
	var moves []crewRootMove
	var conflicts []string
	claimed := map[string]string{}
	for _, manifestPath := range manifests {
		crewDir := filepath.Dir(manifestPath)
		dir := filepath.Base(crewDir)
		owner := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(crewDir)))))
		if strings.HasPrefix(dir, ".") || owner == "" {
			continue
		}
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest struct {
			Product string `json:"product"`
			ID      string `json:"id"`
		}
		if json.Unmarshal(raw, &manifest) != nil || !strings.EqualFold(strings.TrimSpace(manifest.Product), "work") || strings.TrimSpace(manifest.ID) == "" {
			continue
		}
		from := legacyCrewRoot(owner, dir)
		to := crewSharedRootName + "/" + dir
		if _, err := os.Stat(filepath.Join(docsRoot, filepath.FromSlash(to))); err == nil {
			conflicts = append(conflicts, from+" -> "+to+" (target exists)")
			continue
		}
		if other, taken := claimed[dir]; taken {
			conflicts = append(conflicts, from+" -> "+to+" (also claimed by "+other+")")
			continue
		}
		claimed[dir] = from
		moves = append(moves, crewRootMove{Owner: owner, Dir: dir, From: from, To: to})
	}
	return moves, conflicts, nil
}

func moveCrewToSharedRoot(docsRoot string, move crewRootMove) error {
	from := filepath.Join(docsRoot, filepath.FromSlash(move.From))
	to := filepath.Join(docsRoot, filepath.FromSlash(move.To))
	manifestPath := filepath.Join(from, "product.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if owner, _ := manifest["owner_id"].(string); strings.TrimSpace(owner) == "" {
		manifest["owner_id"] = move.Owner
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		info, _ := os.Stat(manifestPath)
		mode := fs.FileMode(0o644)
		if info != nil {
			mode = info.Mode().Perm()
		}
		if err := writeFileAtomic(manifestPath, append(encoded, '\n'), mode); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}

func recordCrewPathAliases(docsRoot string, moves []crewRootMove) error {
	if len(moves) == 0 {
		return nil
	}
	path := filepath.Join(docsRoot, filepath.FromSlash(crewPathAliasFile))
	file := struct {
		Aliases map[string]string `json:"aliases"`
	}{Aliases: map[string]string{}}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &file)
		if file.Aliases == nil {
			file.Aliases = map[string]string{}
		}
	}
	for _, move := range moves {
		file.Aliases[move.From] = move.To
	}
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, append(encoded, '\n'), 0o644)
}

// crewMovesFromAliasFile lists every recorded legacy -> Crew/ move.
func crewMovesFromAliasFile(docsRoot string) []crewRootMove {
	raw, err := os.ReadFile(filepath.Join(docsRoot, filepath.FromSlash(crewPathAliasFile)))
	if err != nil {
		return nil
	}
	var file struct {
		Aliases map[string]string `json:"aliases"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return nil
	}
	var moves []crewRootMove
	for from, to := range file.Aliases {
		legacy, ok := parseCrewPath("", from)
		target, targetOK := parseCrewPath("", to)
		if !ok || legacy.Shared || legacy.Rest != "" || !targetOK || !target.Shared || target.Rest != "" {
			continue
		}
		moves = append(moves, crewRootMove{Owner: legacy.OwnerID, Dir: filepath.Base(legacy.Root), From: legacy.Root, To: target.Root})
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].From < moves[j].From })
	return moves
}

type crewRootReplacement struct {
	old, new string
	pattern  *regexp.Regexp
}

// crewRootReplacements lists, per crew, every spelling a stored reference may
// use, longest first (the physical form contains the user-relative one).
// Folder names end in the crew's 8-character id, so one crew's root is never
// a prefix of another's; the pattern still refuses a match that continues the
// folder name.
func crewRootReplacements(docsRoot string, moves []crewRootMove) []crewRootReplacement {
	var out []crewRootReplacement
	add := func(old, new string) {
		if old == "" || old == new {
			return
		}
		out = append(out, crewRootReplacement{old: old, new: new, pattern: regexp.MustCompile(regexp.QuoteMeta(old) + `(?:$|[^A-Za-z0-9_-])`)})
	}
	for _, move := range moves {
		oldAbs := filepath.Join(docsRoot, filepath.FromSlash(move.From))
		newAbs := filepath.Join(docsRoot, filepath.FromSlash(move.To))
		add(claudeProjectDirName(oldAbs), claudeProjectDirName(newAbs))
		add(move.From, move.To)
		add("Chats/Work/projects/"+move.Dir, move.To)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].old) > len(out[j].old) })
	return out
}

func applyCrewRootReplacements(text string, replacements []crewRootReplacement) (string, bool) {
	changed := false
	for _, r := range replacements {
		if !strings.Contains(text, r.old) {
			continue
		}
		next := r.pattern.ReplaceAllStringFunc(text, func(match string) string {
			return r.new + match[len(r.old):]
		})
		if next != text {
			text, changed = next, true
		}
	}
	return text, changed
}

// rewriteCrewRootJSON rewrites platform JSON files. Crew and workflow working
// files are user content and are left alone; only their manifests and the
// crews' builder transcripts are platform-owned.
func rewriteCrewRootJSON(docsRoot, stateRoot string, replacements []crewRootReplacement) (int, []string) {
	if len(replacements) == 0 {
		return 0, nil
	}
	var files []string
	walkJSON := func(root string) {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if name := d.Name(); name == "node_modules" || name == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".json") || strings.HasSuffix(d.Name(), ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
	}
	for _, root := range []string{filepath.Join(docsRoot, "_system"), filepath.Join(docsRoot, "config"), stateRoot} {
		walkJSON(root)
	}
	userHistories, _ := filepath.Glob(filepath.Join(docsRoot, "_users", "*", "chat_history"))
	for _, root := range userHistories {
		walkJSON(root)
	}
	for _, pattern := range []string{"Workflow/*/workflow.json", "Crew/*/workflow.json", "Crew/*/product.json"} {
		matches, _ := filepath.Glob(filepath.Join(docsRoot, filepath.FromSlash(pattern)))
		files = append(files, matches...)
	}
	for _, pattern := range []string{"Workflow/*/builder", "Crew/*/builder"} {
		matches, _ := filepath.Glob(filepath.Join(docsRoot, filepath.FromSlash(pattern)))
		for _, root := range matches {
			walkJSON(root)
		}
	}
	sort.Strings(files)
	var warnings []string
	count := 0
	// The alias file maps old roots to new ones; rewriting it would erase them.
	seen := map[string]bool{filepath.Join(docsRoot, filepath.FromSlash(crewPathAliasFile)): true}
	for _, path := range files {
		if seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		next, changed := applyCrewRootReplacements(string(raw), replacements)
		if !changed {
			continue
		}
		if err := writeFileAtomic(path, []byte(next), info.Mode().Perm()); err != nil {
			warnings = append(warnings, fmt.Sprintf("rewrite %s: %v", path, err))
			continue
		}
		count++
	}
	return count, warnings
}

// rewriteCrewRootSQLite rewrites TEXT columns in platform SQLite stores
// (cost ledger, bot/WhatsApp routing, chat events). User databases inside a
// crew or workflow (db/db.sqlite) are not touched.
func rewriteCrewRootSQLite(docsRoot, stateRoot string, replacements []crewRootReplacement) (int64, []string) {
	if len(replacements) == 0 {
		return 0, nil
	}
	var dbs []string
	collect := func(root string) {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				rel, _ := filepath.Rel(docsRoot, path)
				rel = filepath.ToSlash(rel)
				if strings.HasPrefix(rel, "Workflow") || strings.HasPrefix(rel, "Crew") || strings.Contains(rel, "/Chats") || d.Name() == "node_modules" || d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if name := d.Name(); strings.HasSuffix(name, ".sqlite") || strings.HasSuffix(name, ".db") {
				dbs = append(dbs, path)
			}
			return nil
		})
	}
	collect(filepath.Join(docsRoot, "_system"))
	collect(filepath.Join(docsRoot, "_users"))
	collect(stateRoot)
	sort.Strings(dbs)
	var total int64
	var warnings []string
	for _, path := range dbs {
		rows, err := rewriteSQLiteTextColumns(path, replacements)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("sqlite %s: %v", path, err))
		}
		total += rows
	}
	return total, warnings
}

func rewriteSQLiteTextColumns(path string, replacements []crewRootReplacement) (int64, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	tables, err := sqliteTableNames(db)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, table := range tables {
		columns, err := sqliteTextColumns(db, table)
		if err != nil {
			return total, err
		}
		for _, column := range columns {
			for _, r := range replacements {
				// The SQL REPLACE has no boundary check; folder names end in
				// the crew's unique id, so a root never prefixes another's.
				result, err := db.Exec(fmt.Sprintf(`UPDATE %q SET %q = REPLACE(%q, ?, ?) WHERE %q LIKE '%%' || ? || '%%'`, table, column, column, column), r.old, r.new, r.old)
				if err != nil {
					return total, fmt.Errorf("%s.%s: %w", table, column, err)
				}
				if n, err := result.RowsAffected(); err == nil {
					total += n
				}
			}
		}
	}
	return total, nil
}

func sqliteTableNames(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func sqliteTextColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var (
			cid        int
			name, kind string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		upper := strings.ToUpper(kind)
		if kind == "" || strings.Contains(upper, "TEXT") || strings.Contains(upper, "CHAR") || strings.Contains(upper, "CLOB") || strings.Contains(upper, "JSON") {
			columns = append(columns, name)
		}
	}
	return columns, rows.Err()
}

// crewRootCLIHomes: $HOME and every provider-connection account home, where
// the coding CLIs keep per-working-directory session stores.
func crewRootCLIHomes() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	homes := []string{home}
	accounts, _ := filepath.Glob(filepath.Join(home, ".local", "state", "agentworks", "provider-connections", "*", "home"))
	return append(homes, accounts...)
}

// copyCrewCLISessions copies each crew's Claude and Cursor session stores to
// the directory the CLI derives from the new working directory. The originals
// stay as a rollback copy. Copied Claude transcripts get their recorded cwd
// rewritten so the resumed session sees the crew's new folder.
func copyCrewCLISessions(docsRoot string, homes []string, moves []crewRootMove, replacements []crewRootReplacement) (int, []string) {
	count := 0
	var warnings []string
	for _, move := range moves {
		oldAbs := filepath.Join(docsRoot, filepath.FromSlash(move.From))
		newAbs := filepath.Join(docsRoot, filepath.FromSlash(move.To))
		for _, home := range homes {
			pairs := [][2]string{
				{filepath.Join(home, ".claude", "projects", claudeProjectDirName(oldAbs)), filepath.Join(home, ".claude", "projects", claudeProjectDirName(newAbs))},
				{filepath.Join(home, ".cursor", "chats", cursorChatsDirName(oldAbs)), filepath.Join(home, ".cursor", "chats", cursorChatsDirName(newAbs))},
				{filepath.Join(home, ".cursor", "projects", cursorProjectDirName(oldAbs)), filepath.Join(home, ".cursor", "projects", cursorProjectDirName(newAbs))},
			}
			for _, pair := range pairs {
				if info, err := os.Stat(pair[0]); err != nil || !info.IsDir() {
					continue
				}
				if err := copyDirRewriting(pair[0], pair[1], replacements); err != nil {
					warnings = append(warnings, fmt.Sprintf("copy %s: %v", pair[0], err))
					continue
				}
				count++
			}
		}
	}
	return count, warnings
}

// claudeProjectDirName mirrors Claude Code's transcript directory naming
// (multi-llm-provider-go claudeTranscriptProjectSlug).
func claudeProjectDirName(workingDir string) string {
	return strings.NewReplacer("/", "-", "\\", "-", "_", "-", ".", "-", ":", "-").Replace(filepath.Clean(workingDir))
}

var cursorProjectDirSeparators = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// cursorProjectDirName mirrors Cursor Agent's per-project store naming
// (agent transcripts, tool output): every run of characters other than
// letters, digits and "-" becomes one "-", ends trimmed. Observed on RTS:
// /data/video-studio/docs/_users/<id>/Chats/Work/projects/<dir> ->
// data-video-studio-docs-users-<id>-Chats-Work-projects-<dir>.
func cursorProjectDirName(workingDir string) string {
	return strings.Trim(cursorProjectDirSeparators.ReplaceAllString(filepath.Clean(workingDir), "-"), "-")
}

// cursorChatsDirName mirrors Cursor Agent's chat-store naming: md5 of the
// absolute working directory.
func cursorChatsDirName(workingDir string) string {
	sum := md5.Sum([]byte(filepath.Clean(workingDir))) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

// copyDirRewriting copies a directory tree without overwriting existing
// files. Text transcripts (.jsonl/.json) get crew paths rewritten; anything
// else (Cursor's store.db) is copied byte for byte.
func copyDirRewriting(from, to string, replacements []crewRootReplacement) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if _, err := os.Stat(target); err == nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".jsonl") || strings.HasSuffix(path, ".json") {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			next, _ := applyCrewRootReplacements(string(raw), replacements)
			return writeFileAtomic(target, []byte(next), info.Mode().Perm())
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return nil
			}
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			return err
		}
		return dst.Close()
	})
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
