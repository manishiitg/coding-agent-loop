package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/workproduct"
)

// Crew paths: one parser, one resolver, one access rule.
//
// Crews live at a shared root, the way workflows live at Workflow/<name>
// (docs/design/crew_shared_root.md):
//
//	Crew/<id>                               shared root; access from the server's crew access record
//	Chats/Work/projects/<id>                legacy, user-relative: the caller's own crew
//	_users/<owner>/Chats/Work/projects/<id> legacy, physical: any owner's crew
//
// Every API edge that receives a crew path goes through resolveCrewPath, so a
// new endpoint cannot accept one spelling and reject another. After the
// migration, the legacy spellings resolve to the crew's Crew/<id> root through
// the alias map it recorded.
//
// Who may do what with a shared crew is the server's crew access record
// (<state>/crew-root/owners.json): a creator, a list of owners (co-owners have
// full access), and a private flag (hidden from everyone but its owners). It
// lives in the agent's own state directory, never in the documents root, so no
// browser request, agent tool, or shell command can edit it. product.json
// owner_id is read exactly once, the first time the server sees a crew it has
// no record for, to seed the record with its creator.

const (
	crewSharedRootName = "Crew"
	crewStateDirName   = "crew-root"
	crewAccessFileName = "owners.json"
	crewAliasFileName  = "aliases.json"
	crewCacheTTL       = 30 * time.Second
)

func init() {
	services.SharedCrewOwner = func(workspacePath string) string {
		ref, ok := resolveCrewPath(context.Background(), "", workspacePath)
		if !ok || !ref.Shared {
			return ""
		}
		return ref.OwnerID
	}
	services.SharedCrewOwnedBy = func(workspacePath, userID string) bool {
		return crewRootOwnedBy(context.Background(), workspacePath, userID)
	}
	workproduct.RecordCrewCreator = func(_ context.Context, workspacePath, userID string) error {
		acl, err := crewAccessRecords.claim(workspacePath, userID)
		if err != nil {
			return err
		}
		if acl.Creator != sanitizeUserIDForPath(userID) {
			return fmt.Errorf("crew %s is already recorded for another owner", workspacePath)
		}
		return nil
	}
}

// crewPathRef is one crew path split into the crew's root and the part below it.
type crewPathRef struct {
	// Root is the crew's root as addressed: "Crew/<id>" or
	// "_users/<owner>/Chats/Work/projects/<id>".
	Root string
	// Rest is the path below Root ("" for the root itself).
	Rest string
	// OwnerID is the crew's creator: its schedules, webhooks and bot routes
	// run as this account. Empty when unknown.
	OwnerID string
	// Owners are every account with full owner access (creator included).
	Owners []string
	// Private hides the crew from everyone but its owners.
	Private bool
	// Shared reports a Crew/<id> root.
	Shared bool
}

// Path is the full workspace path the ref addresses.
func (r crewPathRef) Path() string {
	if r.Rest == "" {
		return r.Root
	}
	return r.Root + "/" + r.Rest
}

// IsOwner reports whether userID has full owner access to the crew.
func (r crewPathRef) IsOwner(userID string) bool {
	if strings.TrimSpace(userID) == "" || !accountMayOwnCrew(userID) {
		return false
	}
	id := sanitizeUserIDForPath(userID)
	for _, owner := range r.Owners {
		if owner == id {
			return true
		}
	}
	return false
}

// parseCrewPath recognizes the three crew spellings without any I/O. A
// user-relative path resolves under the caller, as the workspace service does.
func parseCrewPath(callerID, raw string) (crewPathRef, bool) {
	clean := strings.Trim(path.Clean("/"+strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")), "/")
	if clean == "" || strings.Contains(strings.TrimSpace(raw), "..") {
		return crewPathRef{}, false
	}
	segments := strings.Split(clean, "/")
	rest := func(from int) string { return strings.Join(segments[from:], "/") }
	validID := func(id string) bool { return id != "" && !strings.HasPrefix(id, ".") }
	switch {
	case segments[0] == crewSharedRootName:
		if len(segments) < 2 || !validID(segments[1]) {
			return crewPathRef{}, false
		}
		return crewPathRef{Root: crewSharedRootName + "/" + segments[1], Rest: rest(2), Shared: true}, true
	case len(segments) >= 4 && segments[0] == "Chats" && segments[1] == "Work" && segments[2] == "projects" && validID(segments[3]):
		if strings.TrimSpace(callerID) == "" {
			return crewPathRef{}, false
		}
		owner := sanitizeUserIDForPath(callerID)
		return crewPathRef{Root: legacyCrewRoot(owner, segments[3]), Rest: rest(4), OwnerID: owner, Owners: []string{owner}}, true
	case len(segments) >= 6 && segments[0] == "_users" && segments[1] != "" && segments[2] == "Chats" && segments[3] == "Work" && segments[4] == "projects" && validID(segments[5]):
		return crewPathRef{Root: legacyCrewRoot(segments[1], segments[5]), Rest: rest(6), OwnerID: segments[1], Owners: []string{segments[1]}}, true
	}
	return crewPathRef{}, false
}

func legacyCrewRoot(owner, id string) string {
	return "_users/" + owner + "/Chats/Work/projects/" + id
}

// resolveCrewPath is the one entry point for a crew path from a request or a
// stored reference: it parses any spelling, follows the migration alias to the
// crew's current root, and fills in its access record. ok=false means not a
// crew path. A shared crew without a record has no owners: it is nobody's.
func resolveCrewPath(ctx context.Context, callerID, raw string) (crewPathRef, bool) {
	ref, ok := parseCrewPath(callerID, raw)
	if !ok {
		return crewPathRef{}, false
	}
	if !ref.Shared {
		if moved := crewPathAliases.lookup(ref.Root); moved != "" {
			ref.Root, ref.Shared = moved, true
		}
	}
	if ref.Shared {
		ref.OwnerID, ref.Owners, ref.Private = "", nil, false
		if acl, ok := crewAccessRecords.forRoot(ctx, ref.Root); ok {
			ref.OwnerID, ref.Owners, ref.Private = acl.Creator, append([]string(nil), acl.Owners...), acl.Private
		}
	}
	return ref, true
}

// ownedCrewRoot returns the root of the crew at workspacePath when userID
// owns it (the crew's own folder, where its owners' transcripts live). Any
// spelling is accepted; someone else's crew or a non-crew path is false.
func ownedCrewRoot(userID, workspacePath string) (string, bool) {
	ref, ok := resolveCrewPath(context.Background(), userID, workspacePath)
	if !ok || !ref.IsOwner(userID) {
		return "", false
	}
	return ref.Root, true
}

type crewAccessLevel int

const (
	crewAccessNone crewAccessLevel = iota
	crewAccessReader
	crewAccessOwner
)

// accountMayOwnCrew caps crew ownership by account role: a view-only or
// disabled account listed as an owner acts as a reader. An identity without
// a directory record (single-user, legacy) keeps its listed ownership.
func accountMayOwnCrew(userID string) bool {
	access := userAccessForClaims(&UserClaims{UserID: strings.TrimSpace(userID)})
	if !access.Known {
		return true
	}
	return !access.Disabled && (access.Admin || access.CanEdit)
}

// crewAccessFor is Crew Run mode: owners (creator and co-owners) have full
// access; any other user with the Crew product reads a crew that is not
// private; everyone else has none.
func crewAccessFor(claims *UserClaims, ref crewPathRef) crewAccessLevel {
	if claims == nil || len(ref.Owners) == 0 {
		return crewAccessNone
	}
	if ref.IsOwner(claims.UserID) {
		return crewAccessOwner
	}
	if ref.Private {
		return crewAccessNone
	}
	if userAllowedProduct(claims, "work") {
		return crewAccessReader
	}
	return crewAccessNone
}

// crewRootOwnedBy reports whether userID is an owner of the crew at root.
func crewRootOwnedBy(ctx context.Context, root, userID string) bool {
	ref, ok := resolveCrewPath(ctx, userID, root)
	return ok && ref.IsOwner(userID)
}

// crewRootCreatedBy reports whether userID created the crew at root. A crew's
// schedules and webhooks run once, as its creator.
func crewRootCreatedBy(ctx context.Context, root, userID string) bool {
	ref, ok := resolveCrewPath(ctx, userID, root)
	return ok && ref.OwnerID != "" && ref.OwnerID == sanitizeUserIDForPath(userID)
}

// crewManifestOwner is the owner a crew manifest names, or "" when it names
// none or an invalid id. sanitizeUserIDForPath maps anything invalid to
// "default"; a crew must never fall to that account by accident.
func crewManifestOwner(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" || len(ownerID) > 128 || !safeUserIDForPath.MatchString(ownerID) {
		return ""
	}
	return ownerID
}

// ---- crew access records ----------------------------------------------------

// crewAccess is one shared crew's access record.
type crewAccess struct {
	Creator string   `json:"creator"`
	Owners  []string `json:"owners"`
	Private bool     `json:"private,omitempty"`
}

type crewAccessFile struct {
	Crews map[string]crewAccess `json:"crews"`
}

func normalizeCrewAccess(acl crewAccess) (crewAccess, bool) {
	creator := crewManifestOwner(acl.Creator)
	if creator == "" {
		return crewAccess{}, false
	}
	owners := []string{creator}
	seen := map[string]bool{creator: true}
	for _, owner := range acl.Owners {
		if owner = crewManifestOwner(owner); owner != "" && !seen[owner] {
			seen[owner] = true
			owners = append(owners, owner)
		}
	}
	return crewAccess{Creator: creator, Owners: owners, Private: acl.Private}, true
}

func crewStateDir(stateRoot string) string { return filepath.Join(stateRoot, crewStateDirName) }

func serverCrewStateDir() string {
	root, err := workflowCLIStateRoot()
	if err != nil {
		return ""
	}
	return crewStateDir(root)
}

// readCrewAccessFile reads a record file. A missing file is an empty,
// definite answer; any other failure is an error (never "no owners").
func readCrewAccessFile(file string) (map[string]crewAccess, error) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]crewAccess{}, nil
	}
	if err != nil {
		return nil, err
	}
	var parsed crewAccessFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	out := make(map[string]crewAccess, len(parsed.Crews))
	for root, acl := range parsed.Crews {
		if normalized, ok := normalizeCrewAccess(acl); ok {
			out[strings.Trim(root, "/")] = normalized
		}
	}
	return out, nil
}

// updateCrewAccessFile applies change to the record file under an exclusive
// lock: a fresh read (failures abort), then an atomic write.
func updateCrewAccessFile(dir string, change func(map[string]crewAccess) error) (map[string]crewAccess, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "owners.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	file := filepath.Join(dir, crewAccessFileName)
	records, err := readCrewAccessFile(file)
	if err != nil {
		return nil, err
	}
	if err := change(records); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(crewAccessFile{Crews: records}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(file, append(encoded, '\n'), 0o600); err != nil {
		return nil, err
	}
	return records, nil
}

// crewAccessRecords is the server's view of the record file, refreshed on a
// short TTL. A crew without a record is seeded once from its product.json
// owner_id (creation paths record it immediately).
var crewAccessRecords = &crewAccessStore{}

type crewAccessStore struct {
	mu      sync.Mutex
	loaded  time.Time
	records map[string]crewAccess
	misses  map[string]time.Time
	// Test hooks.
	dir          func() string
	seedFromDisk func(ctx context.Context, root string) string
}

func (s *crewAccessStore) stateDir() string {
	if s.dir != nil {
		return s.dir()
	}
	return serverCrewStateDir()
}

func (s *crewAccessStore) invalidate() {
	s.mu.Lock()
	s.loaded, s.records, s.misses = time.Time{}, nil, nil
	s.mu.Unlock()
}

// snapshotLocked returns the records, re-reading them when stale. On a read
// failure the last good view is kept; with none, ok is false.
func (s *crewAccessStore) snapshotLocked() (map[string]crewAccess, bool) {
	if s.records != nil && time.Since(s.loaded) < crewCacheTTL {
		return s.records, true
	}
	dir := s.stateDir()
	if dir == "" {
		return s.records, s.records != nil
	}
	records, err := readCrewAccessFile(filepath.Join(dir, crewAccessFileName))
	if err != nil {
		return s.records, s.records != nil
	}
	s.records, s.loaded = records, time.Now()
	return records, true
}

// forRoot returns a shared crew's access record, seeding a missing one from
// product.json exactly once. ok=false: no owners (nobody's crew), or the
// records could not be read.
func (s *crewAccessStore) forRoot(ctx context.Context, root string) (crewAccess, bool) {
	root = strings.Trim(filepath.ToSlash(root), "/")
	s.mu.Lock()
	records, readable := s.snapshotLocked()
	if acl, ok := records[root]; ok {
		s.mu.Unlock()
		return acl, true
	}
	if !readable {
		s.mu.Unlock()
		return crewAccess{}, false
	}
	if at, missed := s.misses[root]; missed && time.Since(at) < crewCacheTTL {
		s.mu.Unlock()
		return crewAccess{}, false
	}
	seed := s.seedFromDisk
	s.mu.Unlock()
	if seed == nil {
		seed = readCrewManifestOwner
	}
	creator := seed(ctx, root)
	if creator == "" {
		s.noteMiss(root)
		return crewAccess{}, false
	}
	acl, err := s.claim(root, creator)
	if err != nil {
		return crewAccess{}, false
	}
	return acl, true
}

func (s *crewAccessStore) noteMiss(root string) {
	s.mu.Lock()
	if s.misses == nil || len(s.misses) >= 4096 { // any caller can probe Crew/<random>
		s.misses = map[string]time.Time{}
	}
	s.misses[root] = time.Now()
	s.mu.Unlock()
}

// claim records creator as the crew's creator and only owner unless a record
// exists, and returns the record held afterwards.
func (s *crewAccessStore) claim(root, creator string) (crewAccess, error) {
	root = strings.Trim(filepath.ToSlash(root), "/")
	creator = crewManifestOwner(creator)
	if creator == "" {
		return crewAccess{}, fmt.Errorf("invalid crew creator")
	}
	var held crewAccess
	records, err := s.update(func(records map[string]crewAccess) error {
		if existing, ok := records[root]; ok {
			held = existing
			return nil
		}
		held = crewAccess{Creator: creator, Owners: []string{creator}}
		records[root] = held
		return nil
	})
	if err != nil {
		return crewAccess{}, err
	}
	_ = records
	return held, nil
}

// update applies change to the record file and refreshes the cached view.
func (s *crewAccessStore) update(change func(map[string]crewAccess) error) (map[string]crewAccess, error) {
	dir := s.stateDir()
	if dir == "" {
		return nil, fmt.Errorf("server state directory unavailable")
	}
	records, err := updateCrewAccessFile(dir, change)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.records, s.loaded, s.misses = records, time.Now(), nil
	s.mu.Unlock()
	return records, nil
}

// readCrewManifestOwner seeds a record: the owner_id product.json names, for
// a crew the server has never recorded (created before records existed, or by
// a tool that could not record it).
func readCrewManifestOwner(ctx context.Context, root string) string {
	raw, found, err := readFileFromWorkspace(ctx, root+"/product.json")
	if err != nil || !found {
		return ""
	}
	var manifest struct {
		Product string `json:"product"`
		OwnerID string `json:"owner_id"`
	}
	if json.Unmarshal([]byte(raw), &manifest) != nil || !strings.EqualFold(strings.TrimSpace(manifest.Product), "work") {
		return ""
	}
	return crewManifestOwner(manifest.OwnerID)
}

// crewOwners is kept for callers that need only a crew's creator.
var crewOwners = crewCreatorLookup{}

type crewCreatorLookup struct{}

func (crewCreatorLookup) owner(ctx context.Context, root string) string {
	acl, ok := crewAccessRecords.forRoot(ctx, root)
	if !ok {
		return ""
	}
	return acl.Creator
}

// ---- migration aliases ------------------------------------------------------

// crewPathAliases maps a migrated legacy crew root to its Crew/<id> root. The
// migration writes the file in the server state directory; until then it is
// absent and nothing maps.
var crewPathAliases = &crewAliasStore{}

type crewAliasStore struct {
	mu      sync.Mutex
	loaded  time.Time
	aliases map[string]string
	// Test hook.
	read func() map[string]string
}

func (c *crewAliasStore) snapshot() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.aliases != nil && time.Since(c.loaded) < crewCacheTTL {
		return c.aliases
	}
	read := c.read
	if read == nil {
		read = func() map[string]string { return readCrewAliasFile(serverCrewStateDir()) }
	}
	c.aliases, c.loaded = read(), time.Now()
	return c.aliases
}

func (c *crewAliasStore) invalidate() {
	c.mu.Lock()
	c.aliases, c.loaded = nil, time.Time{}
	c.mu.Unlock()
}

func (c *crewAliasStore) lookup(legacyRoot string) string {
	return c.snapshot()[strings.Trim(legacyRoot, "/")]
}

// legacyRoot is the reverse: the owner-tree root a migrated crew came from
// ("" for a crew created at Crew/). Keys derived from a crew's path before the
// move (its browser profile) stay stable through it.
func (c *crewAliasStore) legacyRoot(sharedRoot string) string {
	for from, to := range c.snapshot() {
		if to == sharedRoot {
			return from
		}
	}
	return ""
}

func readCrewAliasFile(dir string) map[string]string {
	aliases := map[string]string{}
	if dir == "" {
		return aliases
	}
	raw, err := os.ReadFile(filepath.Join(dir, crewAliasFileName))
	if err != nil {
		return aliases
	}
	var file struct {
		Aliases map[string]string `json:"aliases"`
	}
	if json.Unmarshal(raw, &file) == nil {
		for from, to := range file.Aliases {
			if ref, ok := parseCrewPath("", to); ok && ref.Shared && ref.Rest == "" {
				aliases[strings.Trim(from, "/")] = ref.Root
			}
		}
	}
	return aliases
}

// ---- catalog ----------------------------------------------------------------

// crewCatalogEntry is one crew found on disk.
type crewCatalogEntry struct {
	// Root is the crew's folder: Crew/<id>, or a not-yet-migrated
	// _users/<owner>/Chats/Work/projects/<id>.
	Root         string
	OwnerID      string // creator
	Owners       []string
	Private      bool
	ManifestPath string
	Manifest     productProjectManifest
}

// IsOwner reports whether userID is one of the crew's owners.
func (e crewCatalogEntry) IsOwner(userID string) bool {
	return crewPathRef{Owners: e.Owners}.IsOwner(userID)
}

// VisibleTo reports whether userID may see the crew at all.
func (e crewCatalogEntry) VisibleTo(userID string) bool {
	return !e.Private || e.IsOwner(userID)
}

// listCrewCatalog lists every crew: the shared Crew/ root (access from its
// record) and any crew still in an owner's tree (owner from the path). Callers
// filter by ownership and visibility. A crew without owners is nobody's and
// is skipped, as are crews whose creator's account is disabled.
func listCrewCatalog(ctx context.Context, store productProjectStore) ([]crewCatalogEntry, error) {
	var entries []crewCatalogEntry
	seen := map[string]bool{}
	disabled := map[string]bool{}
	if dir, err := loadUserDirectory(); err == nil && dir != nil {
		for _, rec := range dir.Users {
			if rec.Disabled {
				disabled[sanitizeUserIDForPath(strings.TrimSpace(rec.ID))] = true
			}
		}
	}
	scan := func(root, pathOwner string) error {
		paths, exists, err := store.listPaths(ctx, root)
		if err != nil || !exists {
			return err
		}
		sort.Strings(paths)
		prefix := strings.TrimSuffix(root, "/") + "/"
		for _, candidate := range paths {
			candidate = filepath.ToSlash(strings.TrimSpace(candidate))
			rel := strings.TrimPrefix(candidate, prefix)
			// Exactly <root>/<crew>/product.json.
			if rel == candidate || strings.Count(rel, "/") != 1 || !strings.HasSuffix(rel, "/product.json") || seen[candidate] {
				continue
			}
			seen[candidate] = true
			raw, found, readErr := store.read(ctx, candidate)
			if readErr != nil {
				return fmt.Errorf("read Crew manifest %s: %w", candidate, readErr)
			}
			if !found {
				continue
			}
			var manifest productProjectManifest
			if json.Unmarshal([]byte(raw), &manifest) != nil || !strings.EqualFold(strings.TrimSpace(manifest.Product), "work") || strings.TrimSpace(manifest.ID) == "" {
				continue
			}
			entry := crewCatalogEntry{Root: filepath.ToSlash(filepath.Dir(candidate)), ManifestPath: candidate, Manifest: manifest}
			if pathOwner != "" {
				entry.OwnerID, entry.Owners = pathOwner, []string{pathOwner}
			} else {
				acl, ok := crewAccessRecords.forRoot(ctx, entry.Root)
				if !ok {
					continue
				}
				entry.OwnerID, entry.Owners, entry.Private = acl.Creator, append([]string(nil), acl.Owners...), acl.Private
			}
			if disabled[entry.OwnerID] {
				continue // a disabled account's crews are not listed
			}
			entries = append(entries, entry)
		}
		return nil
	}
	if err := scan(crewSharedRootName, ""); err != nil {
		return nil, err
	}
	for _, owner := range allCrewOwnerCandidates() {
		if err := scan(legacyCrewProjectsRoot(owner), owner); err != nil {
			continue // one unreadable tree must not hide every other crew
		}
	}
	return entries, nil
}

func legacyCrewProjectsRoot(owner string) string {
	return "_users/" + sanitizeUserIDForPath(owner) + "/Chats/Work/projects"
}

// allCrewOwnerCandidates is every account that may still hold crews in its own
// tree: the directory's enabled users plus the single-user default.
func allCrewOwnerCandidates() []string {
	seen := map[string]bool{}
	var owners []string
	add := func(raw string) {
		if segment := sanitizeUserIDForPath(strings.TrimSpace(raw)); !seen[segment] {
			seen[segment] = true
			owners = append(owners, segment)
		}
	}
	if dir, err := loadUserDirectory(); err == nil && dir != nil {
		for _, rec := range dir.Users {
			if !rec.Disabled {
				add(rec.ID)
			}
		}
	}
	add(GetDefaultUserID())
	sort.Strings(owners)
	return owners
}
