package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// migrate.go is the ONE-TIME migration from the old role-based workspace
// layout (shared/ + parent/ + child/) to the new activity-folder layout (see
// activity.go and the workspace-redesign plan). It runs once at startup,
// guarded by a .migrated-v2 marker file at the workspace root, and is
// best-effort throughout: anything it can't confidently place goes to
// _legacy/ rather than being lost. The whole workspace is backed up to a
// sibling workspace.pre-v2-backup/ directory before any move, as a
// non-destructive escape hatch.
const migrationMarkerName = ".migrated-v2"

// runWorkspaceMigrationIfNeeded is called once from main() before serving,
// after scaffoldFamilyFolders/seedSkills/seedWorkspace so the new-layout
// directories already exist as merge targets.
func runWorkspaceMigrationIfNeeded() {
	root := workspaceRoot()
	marker := filepath.Join(root, migrationMarkerName)
	if _, err := os.Stat(marker); err == nil {
		return // already migrated (or a fresh workspace that was marked done)
	}
	if !oldLayoutPresent(root) {
		markMigrated(marker)
		return
	}
	log.Printf("[migrate] old workspace layout detected — migrating to the activity-folder layout")
	if err := migrateWorkspace(root); err != nil {
		log.Printf("[migrate] FAILED, leaving the old layout in place (will retry next startup): %v", err)
		return // do NOT write the marker — retry on next startup
	}
	markMigrated(marker)
	log.Printf("[migrate] done")
}

func oldLayoutPresent(root string) bool {
	for _, d := range []string{"shared", "parent", "child"} {
		if info, err := os.Stat(filepath.Join(root, d)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func markMigrated(marker string) {
	_ = os.MkdirAll(filepath.Dir(marker), 0o700)
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func migrateWorkspace(root string) error {
	backupDir := root + ".pre-v2-backup"
	if _, err := os.Stat(backupDir); err != nil {
		log.Printf("[migrate] backing up workspace to %s", backupDir)
		if err := copyDirCmd(root, backupDir); err != nil {
			return fmt.Errorf("backup failed, aborting migration: %w", err)
		}
	} else {
		log.Printf("[migrate] backup already exists at %s, skipping", backupDir)
	}

	itemToActivityDir := map[string]string{}    // old item workspace-relative path -> new activity dir
	manifestToActivityDir := map[string]string{} // old shared/packages/*.json path -> new activity dir

	if err := migratePackages(root, itemToActivityDir, manifestToActivityDir); err != nil {
		return fmt.Errorf("migrating packages: %w", err)
	}
	if err := migrateLooseContent(root, itemToActivityDir); err != nil {
		return fmt.Errorf("migrating loose study/test files: %w", err)
	}
	if err := migrateMaterials(root); err != nil {
		return fmt.Errorf("migrating materials: %w", err)
	}
	if err := migrateInbox(root); err != nil {
		return fmt.Errorf("migrating inbox: %w", err)
	}
	if err := migrateReports(root); err != nil {
		return fmt.Errorf("migrating reports: %w", err)
	}
	if err := migrateAnswerKeys(root, itemToActivityDir); err != nil {
		return fmt.Errorf("migrating answer keys: %w", err)
	}
	if err := migrateMemoryAndParentConversations(root); err != nil {
		return fmt.Errorf("migrating memory/parent conversations: %w", err)
	}
	if err := migrateChildConversationsAndActive(root); err != nil {
		return fmt.Errorf("migrating child conversations: %w", err)
	}
	if err := migrateCurrentTask(root, manifestToActivityDir); err != nil {
		return fmt.Errorf("migrating current-task pointer: %w", err)
	}
	if err := migrateLeftovers(root); err != nil {
		return fmt.Errorf("sweeping leftovers: %w", err)
	}
	removeIfEmpty(filepath.Join(root, "shared"))
	removeIfEmpty(filepath.Join(root, "parent"))
	removeIfEmpty(filepath.Join(root, "child"))
	return nil
}

// --- generic filesystem helpers ---------------------------------------------

func copyDirCmd(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil // nothing to copy
	}
	cmd := exec.Command("cp", "-a", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cp -a %s %s: %w (%s)", src, dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// mergeMove moves src into dst. If dst doesn't exist, it's a fast rename.
// If both are directories, it recursively merges src's children into dst
// (so migration stays safe to retry / doesn't clobber anything already
// there). A name collision between two files is resolved by suffixing the
// incoming one rather than overwriting it — nothing is ever silently lost.
func mergeMove(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return nil // nothing to move
	}
	dstInfo, err := os.Lstat(dst)
	if err != nil {
		// dst doesn't exist — straightforward move.
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.Rename(src, dst)
	}
	if !srcInfo.IsDir() || !dstInfo.IsDir() {
		// Collision between two files (or a file and a dir) — never overwrite.
		alt := dst + ".migrated-dup"
		for i := 2; ; i++ {
			if _, err := os.Lstat(alt); err != nil {
				break
			}
			alt = fmt.Sprintf("%s.migrated-dup%d", dst, i)
		}
		log.Printf("[migrate] name collision moving %s — kept as %s", src, alt)
		return os.Rename(src, alt)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := mergeMove(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(src) // src is now empty
}

func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		_ = os.Remove(dir)
		return
	}
	log.Printf("[migrate] %s is not empty after migration, leaving it — check its contents", dir)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// copyFile copies src to dst (used when the same old file is claimed by more
// than one activity during migration — each stays self-contained).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// --- step: shared/packages/*.json -> <Subject>/<Topic>/<slug>/ -------------

// oldLearningPackage mirrors the pre-redesign shared/packages/*.json shape
// (see learning_package_tool.go's learningPackage, kept there for this).
type oldLearningPackage = learningPackage

// parseOldSharedPath splits an old "shared/study/<Subject>/<Topic>/<file>" (or
// shallower — some real packages have items with no topic, or even no
// subject, directly under shared/study|tests/) style path into its kind
// (study|tests), best-effort subject/topic, and bare filename.
func parseOldSharedPath(p string) (kind, subject, topic, filename string, ok bool) {
	p = filepath.ToSlash(strings.TrimSpace(p))
	parts := strings.Split(p, "/")
	if len(parts) < 3 || parts[0] != "shared" || (parts[1] != "study" && parts[1] != "tests") {
		return "", "", "", "", false
	}
	kind = parts[1]
	rest := parts[2:]
	filename = rest[len(rest)-1]
	mid := rest[:len(rest)-1]
	switch len(mid) {
	case 0:
		subject, topic = "General", "General"
	case 1:
		subject, topic = mid[0], "General"
	default:
		subject, topic = mid[0], mid[1]
	}
	return kind, subject, topic, filename, true
}

var dateStampRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func activityDirDateStamp(createdAt string, fallback time.Time) string {
	if m := dateStampRE.FindString(strings.TrimSpace(createdAt)); m != "" {
		return m
	}
	return fallback.Format("2006-01-02")
}

// migratePackages turns every shared/packages/*.json manifest into a real
// <Subject>/<Topic>/<slug>/ activity folder — moving its item files in and
// writing activity.json — per the plan (subject/topic taken from the FIRST
// item, since a package is one activity even on the rare occasion its items
// historically spanned more than one topic folder).
func migratePackages(root string, itemToActivityDir, manifestToActivityDir map[string]string) error {
	pkgDir := filepath.Join(root, "shared", "packages")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil // no packages to migrate
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		manifestRel := filepath.ToSlash(filepath.Join("shared", "packages", e.Name()))
		manifestPath := filepath.Join(pkgDir, e.Name())
		b, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Printf("[migrate] package %s: read failed: %v", e.Name(), err)
			continue
		}
		var pkg oldLearningPackage
		if err := json.Unmarshal(b, &pkg); err != nil || strings.TrimSpace(pkg.Title) == "" {
			log.Printf("[migrate] package %s: invalid manifest, skipping", e.Name())
			continue
		}
		// Dedupe items (real data has at least one package listing the same
		// file twice) while preserving first-seen order.
		seen := map[string]bool{}
		var items []string
		for _, it := range pkg.Items {
			it = strings.TrimSpace(it)
			if it == "" || seen[it] {
				continue
			}
			seen[it] = true
			items = append(items, it)
		}
		if len(items) == 0 {
			log.Printf("[migrate] package %s: no items, skipping", e.Name())
			continue
		}
		_, subject, topic, _, ok := parseOldSharedPath(items[0])
		if !ok {
			log.Printf("[migrate] package %s: couldn't parse first item %q, skipping", e.Name(), items[0])
			continue
		}
		fi, _ := os.Stat(manifestPath)
		mtime := time.Now()
		if fi != nil {
			mtime = fi.ModTime()
		}
		stamp := activityDirDateStamp(pkg.CreatedAt, mtime)
		slug := slugify(pkg.Title)
		newDir := filepath.Join(root, subject, topic, stamp+"-"+slug)
		newDir = uniqueDir(newDir)
		if err := os.MkdirAll(newDir, 0o700); err != nil {
			return err
		}
		relDir := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(newDir, root), "/"))
		var newItems []string
		for _, it := range items {
			absOld := filepath.Join(root, filepath.FromSlash(it))
			base := filepath.Base(it)
			destBase := uniqueBase(newDir, base)
			destPath := filepath.Join(newDir, destBase)
			if _, statErr := os.Lstat(absOld); statErr == nil {
				// First claimant — move the real file in.
				if err := os.Rename(absOld, destPath); err != nil {
					log.Printf("[migrate] package %s: item %q unmovable (%v), skipping it", e.Name(), it, err)
					continue
				}
			} else if ownerDir, already := itemToActivityDir[it]; already {
				// Another old package already claimed this exact file (real
				// data has items shared across packages) — copy it in so THIS
				// activity stays fully self-contained too, rather than
				// silently ending up with a missing/empty item.
				srcAbs := filepath.Join(root, filepath.FromSlash(ownerDir), base)
				if err := copyFile(srcAbs, destPath); err != nil {
					log.Printf("[migrate] package %s: item %q shared with %s, but copying it failed (%v), skipping it", e.Name(), it, ownerDir, err)
					continue
				}
				log.Printf("[migrate] package %s: item %q already claimed by %s — duplicated into this activity too", e.Name(), it, ownerDir)
			} else {
				log.Printf("[migrate] package %s: item %q missing, skipping it", e.Name(), it)
				continue
			}
			newItems = append(newItems, destBase)
			if _, already := itemToActivityDir[it]; !already {
				itemToActivityDir[it] = relDir
			}
		}
		if len(newItems) == 0 {
			log.Printf("[migrate] package %s: none of its items could be moved, leaving folder %s empty", e.Name(), newDir)
		}
		createdAt := pkg.CreatedAt
		if strings.TrimSpace(createdAt) == "" {
			createdAt = mtime.UTC().Format(time.RFC3339)
		}
		m := activityManifest{
			Title:     pkg.Title,
			Subject:   subject,
			Topic:     topic,
			Items:     newItems,
			GuideNote: pkg.GuideNote,
			CreatedAt: createdAt,
		}
		mb, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(filepath.Join(newDir, activityManifestName), mb, 0o600); err != nil {
			return err
		}
		_ = os.Remove(manifestPath)
		newDirRel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(newDir, root), "/"))
		manifestToActivityDir[manifestRel] = newDirRel
		log.Printf("[migrate] package %q -> %s (%d items)", pkg.Title, newDirRel, len(newItems))
	}
	removeIfEmpty(pkgDir)
	return nil
}

// uniqueDir returns dir, or dir with a numeric suffix if it already exists
// (two packages producing the same date+slug on the same day).
func uniqueDir(dir string) string {
	if !dirExists(dir) {
		return dir
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", dir, i)
		if !dirExists(candidate) {
			return candidate
		}
	}
}

// uniqueBase returns base, or base with a numeric suffix if that name
// already exists inside dir (two items in one package sharing a filename).
func uniqueBase(dir, base string) string {
	if _, err := os.Stat(filepath.Join(dir, base)); err != nil {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(filepath.Join(dir, candidate)); err != nil {
			return candidate
		}
	}
}

// --- step: loose (un-packaged) shared/study|tests files --------------------

var titleWordSepRE = regexp.MustCompile(`[-_]+`)

// titleFromFilename turns "2026-07-21-fractions-revision-worksheet.md" into
// "Fractions Revision Worksheet" — a readable title for a wrapped one-item
// activity, since these loose files never had one of their own.
func titleFromFilename(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = dateStampRE.ReplaceAllString(base, "")
	base = strings.Trim(base, "-_ ")
	words := titleWordSepRE.Split(base, -1)
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[0:1])) + string(r[1:])
	}
	title := strings.TrimSpace(strings.Join(words, " "))
	if title == "" {
		title = "Untitled"
	}
	return title
}

// migrateLooseContent wraps every shared/study|tests file NOT already moved
// by migratePackages (i.e. still present after that step) as its own
// one-item activity — "every piece of child-facing content is an activity"
// applies retroactively too.
func migrateLooseContent(root string, itemToActivityDir map[string]string) error {
	for _, kind := range []string{"study", "tests"} {
		base := filepath.Join(root, "shared", kind)
		if !dirExists(base) {
			continue
		}
		var files []string
		_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			files = append(files, p)
			return nil
		})
		sort.Strings(files)
		for _, abs := range files {
			rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(abs, root), "/"))
			_, subject, topic, filename, ok := parseOldSharedPath(rel)
			if !ok {
				continue
			}
			fi, err := os.Stat(abs)
			if err != nil {
				continue
			}
			stamp := fi.ModTime().Format("2006-01-02")
			title := titleFromFilename(filename)
			newDir := uniqueDir(filepath.Join(root, subject, topic, stamp+"-"+slugify(title)))
			if err := os.MkdirAll(newDir, 0o700); err != nil {
				return err
			}
			destBase := uniqueBase(newDir, filename)
			if err := os.Rename(abs, filepath.Join(newDir, destBase)); err != nil {
				log.Printf("[migrate] loose file %s: move failed: %v", rel, err)
				continue
			}
			relDir := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(newDir, root), "/"))
			itemToActivityDir[rel] = relDir
			m := activityManifest{
				Title:     title,
				Subject:   subject,
				Topic:     topic,
				Items:     []string{destBase},
				CreatedAt: fi.ModTime().UTC().Format(time.RFC3339),
			}
			mb, _ := json.MarshalIndent(m, "", "  ")
			if err := os.WriteFile(filepath.Join(newDir, activityManifestName), mb, 0o600); err != nil {
				return err
			}
			log.Printf("[migrate] loose file %s -> %s", rel, relDir)
		}
	}
	removeEmptyTree(filepath.Join(root, "shared", "study"))
	removeEmptyTree(filepath.Join(root, "shared", "tests"))
	return nil
}

// removeEmptyTree removes dir and any now-empty ancestor directories under
// it, deepest first, after everything meaningful has been moved out.
func removeEmptyTree(dir string) {
	if !dirExists(dir) {
		return
	}
	var dirs []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		_ = os.Remove(d) // no-op if not empty
	}
}

// --- step: materials, inbox, reports ----------------------------------------

func migrateMaterials(root string) error {
	src := filepath.Join(root, "shared", "materials")
	if !dirExists(src) {
		return nil
	}
	dst := filepath.Join(root, "materials")
	if err := mergeMove(src, dst); err != nil {
		return err
	}
	// Rewrite stored_path in every .meta.json from shared/materials/... to materials/...
	_ = filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".meta.json") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		s := string(b)
		if strings.Contains(s, `"shared/materials/`) {
			s = strings.ReplaceAll(s, `"shared/materials/`, `"materials/`)
			_ = os.WriteFile(p, []byte(s), 0o600)
		}
		return nil
	})
	return nil
}

func migrateInbox(root string) error {
	if err := mergeMove(filepath.Join(root, "shared", "inbox"), filepath.Join(root, "inbox")); err != nil {
		return err
	}
	return mergeMove(filepath.Join(root, "child", "inbox"), filepath.Join(root, "inbox"))
}

func migrateReports(root string) error {
	if err := mergeMove(filepath.Join(root, "shared", "reports"), filepath.Join(root, "reports")); err != nil {
		return err
	}
	mapPath := filepath.Join(root, "shared", "academic-map.html")
	if _, err := os.Stat(mapPath); err == nil {
		if err := mergeMove(mapPath, filepath.Join(root, "reports", "academic-map.html")); err != nil {
			return err
		}
	}
	return nil
}

// --- step: answer keys -------------------------------------------------

var nonAlnumRE = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeForMatch collapses a filename to a bare comparable string (used
// only for the high-confidence EXACT match case — token overlap below
// handles everything fuzzier).
func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	s = dateStampRE.ReplaceAllString(s, "")
	s = strings.TrimSuffix(s, filepath.Ext(s))
	s = strings.TrimSuffix(s, "-key")
	s = nonAlnumRE.ReplaceAllString(s, "")
	return s
}

// matchStopWords are common filler words dropped from token matching so they
// never inflate a match score — e.g. "Fractions AND Decimals" vs a key named
// "...fractions-decimals-quick-check-KEY..." should still overlap fully.
var matchStopWords = map[string]bool{"and": true, "the": true, "of": true, "a": true, "an": true, "in": true, "on": true, "for": true, "to": true}

// tokenizeForMatch splits a filename into a set of meaningful lowercase word
// tokens (date prefix, "-KEY" suffix, extension, filler words, and very short
// tokens all dropped) — the basis of the answer-key-to-activity matching
// below, since raw substring containment breaks on ordinary word variation
// ("Fractions and Decimals" vs "fractions-decimals").
func tokenizeForMatch(s string) map[string]bool {
	s = strings.ToLower(s)
	s = dateStampRE.ReplaceAllString(s, "")
	s = strings.TrimSuffix(s, filepath.Ext(s))
	s = strings.TrimSuffix(s, "-key")
	s = strings.TrimSuffix(s, "_key")
	out := map[string]bool{}
	for _, w := range nonAlnumRE.Split(s, -1) {
		if len(w) < 3 || matchStopWords[w] {
			continue
		}
		out[w] = true
	}
	return out
}

func tokenOverlap(a, b map[string]bool) int {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return n
}

// migrateAnswerKeys places each parent/answer-keys/*-KEY.* file into the
// activity folder whose item it best matches by filename (best-effort — the
// old naming convention has no formal link between a key and its test file).
// Unmatched keys go to _legacy/answer-keys/ rather than being lost.
func migrateAnswerKeys(root string, itemToActivityDir map[string]string) error {
	src := filepath.Join(root, "parent", "answer-keys")
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil
	}
	// Build a lookup of token sets per activity: its own item basenames
	// (generic — "quick-check.html" recurs across many unrelated activities)
	// plus its Subject+Topic name (a much more specific signal — "Fractions
	// and Decimals" is what actually distinguishes one quick-check from
	// another). Sorted so ties resolve deterministically instead of
	// depending on Go's randomized map iteration order.
	type candidate struct {
		itemExacts map[string]bool // normalizeForMatch of each item basename, for the exact-match special case
		tokens     map[string]bool
		dir        string
	}
	// Group by activity dir FIRST — a package's items all belong to the SAME
	// candidate, so their tokens must be unioned together (a 4-item activity
	// should be judged on all 4 items' combined signal, not have each item
	// compete as its own separate, weaker candidate).
	byDir := map[string]*candidate{}
	for oldPath, dir := range itemToActivityDir {
		c, ok := byDir[dir]
		if !ok {
			parts := strings.SplitN(dir, "/", 3)
			tokens := map[string]bool{}
			if len(parts) >= 2 {
				for w := range tokenizeForMatch(parts[0] + " " + parts[1]) {
					tokens[w] = true
				}
			}
			c = &candidate{itemExacts: map[string]bool{}, tokens: tokens, dir: dir}
			byDir[dir] = c
		}
		base := filepath.Base(oldPath)
		for w := range tokenizeForMatch(base) {
			c.tokens[w] = true
		}
		c.itemExacts[normalizeForMatch(base)] = true
	}
	var candidates []candidate
	for _, c := range byDir {
		candidates = append(candidates, *c)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dir < candidates[j].dir })
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		keyNorm := normalizeForMatch(e.Name())
		keyTokens := tokenizeForMatch(e.Name())
		best := ""
		bestScore := 0
		for _, c := range candidates {
			if keyNorm == "" {
				continue
			}
			score := tokenOverlap(keyTokens, c.tokens)
			if keyNorm != "" && c.itemExacts[keyNorm] {
				score += 1000 // exact item-name match wins outright
			}
			if score > bestScore {
				bestScore = score
				best = c.dir
			}
		}
		abs := filepath.Join(src, e.Name())
		if best == "" {
			dst := filepath.Join(root, "_legacy", "answer-keys", e.Name())
			if err := mergeMove(abs, dst); err != nil {
				return err
			}
			log.Printf("[migrate] answer key %s: no confident match, moved to _legacy/answer-keys/", e.Name())
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(best), e.Name())
		destName := uniqueBase(filepath.Join(root, filepath.FromSlash(best)), e.Name())
		dst = filepath.Join(root, filepath.FromSlash(best), destName)
		if err := os.Rename(abs, dst); err != nil {
			return err
		}
		log.Printf("[migrate] answer key %s -> %s/%s", e.Name(), best, destName)
	}
	removeIfEmpty(src)
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- step: memory + parent conversations -----------------------------------

func migrateMemoryAndParentConversations(root string) error {
	memory := filepath.Join(root, "memory")
	if err := os.MkdirAll(memory, 0o700); err != nil {
		return err
	}
	moves := map[string]string{
		filepath.Join(root, "parent", "preferences.md"):    filepath.Join(memory, "preferences.md"),
		filepath.Join(root, "parent", "browser-notes.md"):  filepath.Join(memory, "browser-notes.md"),
		filepath.Join(root, "child", "interests.md"):       filepath.Join(memory, "interests.md"),
		filepath.Join(root, "parent", "child-profile.json"): filepath.Join(memory, "child-profile.json"),
	}
	for src, dst := range moves {
		if err := mergeMove(src, dst); err != nil {
			return err
		}
	}

	// The canonical parent thread: parent/conversations/parent.json (+ its
	// session handle) -> conversations/parent.json. Everything else in that
	// folder is old debug/test conversation scratch — preserved, not lost,
	// under conversations/legacy-parent/ rather than treated as canonical.
	convDir := filepath.Join(root, "parent", "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		return nil
	}
	dstConv := filepath.Join(root, "conversations")
	if err := os.MkdirAll(dstConv, 0o700); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		src := filepath.Join(convDir, name)
		if name == "parent.json" || name == "parent.session.json" {
			if err := mergeMove(src, filepath.Join(dstConv, name)); err != nil {
				return err
			}
			continue
		}
		if err := mergeMove(src, filepath.Join(dstConv, "legacy-parent", name)); err != nil {
			return err
		}
	}
	removeIfEmpty(convDir)
	return nil
}

// --- step: child conversations + discard stale active/ copies --------------

func migrateChildConversationsAndActive(root string) error {
	// child/conversations/* are uuid-keyed transient tutoring chats with no
	// reliable link back to a specific activity — preserved best-effort under
	// conversations/legacy-child/ rather than lost or force-mapped.
	if err := mergeMove(filepath.Join(root, "child", "conversations"), filepath.Join(root, "conversations", "legacy-child")); err != nil {
		return err
	}
	// child/active/* were mirrored copies from the old copy-based sandbox
	// (removed earlier this project) — the real files already live in their
	// activity folders now, so these stale duplicates are discarded.
	active := filepath.Join(root, "child", "active")
	if dirExists(active) {
		if err := os.RemoveAll(active); err != nil {
			return err
		}
		log.Printf("[migrate] discarded stale child/active/ copies")
	}
	return nil
}

// --- step: current-task.json -> current-activity.json ----------------------

func migrateCurrentTask(root string, manifestToActivityDir map[string]string) error {
	oldPath := filepath.Join(root, "child", "current-task.json")
	defer os.Remove(oldPath)
	b, err := os.ReadFile(oldPath)
	if err != nil {
		return nil // nothing handed off
	}
	var old struct {
		Package string `json:"package"`
	}
	if json.Unmarshal(b, &old) != nil || strings.TrimSpace(old.Package) == "" {
		return nil
	}
	dir, ok := manifestToActivityDir[strings.TrimSpace(old.Package)]
	if !ok {
		log.Printf("[migrate] child/current-task.json referenced %q, which wasn't migrated to any activity — the parent will need to hand off again", old.Package)
		return nil
	}
	saveCurrentActivity(dir)
	log.Printf("[migrate] child's current handoff (%q) -> current-activity.json now points at %s", old.Package, dir)
	return nil
}

// --- step: sweep remaining known leftovers ----------------------------------

func migrateLeftovers(root string) error {
	// parent/approved-for-child.json is the old approval-list concept,
	// removed entirely by this redesign (child access is now "the current
	// activity folder", nothing to approve) — not migrated.
	_ = os.Remove(filepath.Join(root, "parent", "approved-for-child.json"))

	// parent/notes/, if it ever held anything, is preserved under _legacy/
	// rather than silently dropped (the new layout has no direct equivalent).
	if err := mergeMove(filepath.Join(root, "parent", "notes"), filepath.Join(root, "_legacy", "parent-notes")); err != nil {
		return err
	}
	removeIfEmpty(filepath.Join(root, "_legacy", "parent-notes"))

	// child/material-stars.json isn't read by any current code (superseded
	// by the in-the-moment celebrate tool) — preserved, not discarded.
	if err := mergeMove(filepath.Join(root, "child", "material-stars.json"), filepath.Join(root, "_legacy", "material-stars.json")); err != nil {
		return err
	}

	// child/attempts/ (old global scratch space) has no reliable per-activity
	// mapping either — preserve under _legacy/.
	if err := mergeMove(filepath.Join(root, "child", "attempts"), filepath.Join(root, "_legacy", "child-attempts")); err != nil {
		return err
	}
	removeIfEmpty(filepath.Join(root, "_legacy", "child-attempts"))

	return nil
}
