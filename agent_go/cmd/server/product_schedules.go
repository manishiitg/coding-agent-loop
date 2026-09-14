package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/productschedule"
)

// Product schedules are the recurring jobs a product declares in its
// product.yaml (profile.schedules). For every user who has the product, the
// platform runs each due schedule by sending its messages one at a time into
// that user's product conversation — the same conversation the product's
// surface shows — so a check-in reads as part of the chat, not a separate
// run. Each user keeps their own enable/disable override and run bookkeeping
// in _users/<id>/chat_history/product-schedules.json; run history goes to
// schedule-runs.json next to the conversation, the same file workflow
// schedules use.
//
// This deliberately stays outside SchedulerService: that service is built
// around workflow manifests (leases, dependency queues, capacity waits, Pulse
// reviews). A product schedule needs none of that; it needs the timing rule
// and the one-message-at-a-time send, which productschedule and
// startSessionInternal already provide.

const productScheduleJobPrefix = "product:"
const projectScheduleJobPrefix = "product-project:"
const productScheduleStateFile = "product-schedules.json"

// productScheduleUserState is one user's bookkeeping for one schedule.
type productScheduleUserState struct {
	// Enabled overrides the product's default when set.
	Enabled   *bool  `json:"enabled,omitempty"`
	LastRunAt string `json:"last_run_at,omitempty"`
	// LastAttemptAt is when a run last started, successful or not; with
	// ConsecutiveFailures it feeds the retry backoff (a failed check-in must
	// not re-fire on every tick because only success moves LastRunAt).
	LastAttemptAt       string `json:"last_attempt_at,omitempty"`
	LastStatus          string `json:"last_status,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	LastSessionID       string `json:"last_session_id,omitempty"`
	LastDurationMs      *int64 `json:"last_duration_ms,omitempty"`
	RunCount            int    `json:"run_count,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures,omitempty"`
}

// productScheduleJob is one (profile, schedule, user) triple.
type productScheduleJob struct {
	UserID        string
	Profile       agentprofiles.Profile
	Schedule      productschedule.Schedule
	State         productScheduleUserState
	ProjectID     string
	ProjectTitle  string
	WorkspacePath string
	ManifestPath  string
}

// ID is the job id exposed through /api/scheduler/jobs.
func (j productScheduleJob) ID() string {
	if j.ProjectID != "" {
		return projectScheduleJobID(j.Profile.ID, j.ProjectID, j.Schedule.ID)
	}
	return productScheduleJobID(j.Profile.ID, j.Schedule.ID)
}

// Effective returns the schedule with the user's enable override applied.
func (j productScheduleJob) Effective() productschedule.Schedule {
	s := j.Schedule
	if j.State.Enabled != nil {
		s.Enabled = *j.State.Enabled
	}
	return s
}

func (j productScheduleJob) lastRun() time.Time     { return parseRFC3339OrZero(j.State.LastRunAt) }
func (j productScheduleJob) lastAttempt() time.Time { return parseRFC3339OrZero(j.State.LastAttemptAt) }

func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func productScheduleJobID(profileID, scheduleID string) string {
	return productScheduleJobPrefix + strings.TrimSpace(profileID) + ":" + strings.TrimSpace(scheduleID)
}

func projectScheduleJobID(profileID, projectID, scheduleID string) string {
	return projectScheduleJobPrefix + strings.TrimSpace(profileID) + ":" + strings.TrimSpace(projectID) + ":" + strings.TrimSpace(scheduleID)
}

func parseProjectScheduleJobID(id string) (profileID, projectID, scheduleID string, ok bool) {
	if !strings.HasPrefix(id, projectScheduleJobPrefix) {
		return "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(id, projectScheduleJobPrefix), ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// parseProductScheduleJobID splits "product:<profile>:<schedule>".
func parseProductScheduleJobID(id string) (profileID, scheduleID string, ok bool) {
	if !strings.HasPrefix(id, productScheduleJobPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, productScheduleJobPrefix)
	i := strings.LastIndex(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

func productScheduleStatePath(userID string) string {
	return filepath.ToSlash(filepath.Join(chatHistoryRoot(userID), productScheduleStateFile))
}

func productScheduleStateKey(profileID, scheduleID string) string {
	return strings.TrimSpace(profileID) + "/" + strings.TrimSpace(scheduleID)
}

func projectScheduleStateKey(profileID, projectID, scheduleID string) string {
	return strings.TrimSpace(profileID) + "/" + strings.TrimSpace(projectID) + "/" + strings.TrimSpace(scheduleID)
}

func scheduleStateKey(job productScheduleJob) string {
	if job.ProjectID != "" {
		return projectScheduleStateKey(job.Profile.ID, job.ProjectID, job.Schedule.ID)
	}
	return productScheduleStateKey(job.Profile.ID, job.Schedule.ID)
}

// ProductScheduleService runs product schedules for every user.
type ProductScheduleService struct {
	api      *StreamingAPI
	registry *agentprofiles.Registry

	// users lists the user ids that may have product schedules; swapped in tests.
	users func(product string) []string
	// sinceInteractive is how long ago a user last used the product; nil
	// means the quiet rule never holds a run back on the platform.
	sinceInteractive func(userID string, profile agentprofiles.Profile) time.Duration
	// readFile / writeFile back the per-user state file; swapped in tests.
	readFile  func(context.Context, string) (string, bool, error)
	writeFile func(context.Context, string, string) error

	mu      sync.Mutex
	running map[string]*productScheduleRun // key: userID + "\x1f" + job id
	// deferred holds the quiet-rule reason for jobs currently held back, so
	// the UI can say "waiting for a quiet moment" instead of showing nothing.
	deferred map[string]string
	stateMu  sync.Mutex
}

// productInteractions is the shared record of who last used which product;
// the product chat handlers stamp it and the quiet rule reads it.
var productInteractions = newProductInteractionTracker()

type productScheduleRun struct {
	SessionID string
	StartedAt time.Time
	cancel    context.CancelFunc
}

// NewProductScheduleService wires the service to the live profile registry.
func NewProductScheduleService(api *StreamingAPI, registry *agentprofiles.Registry) *ProductScheduleService {
	svc := &ProductScheduleService{api: api, registry: registry, running: map[string]*productScheduleRun{}, deferred: map[string]string{}}
	svc.users = usersWithProduct
	svc.readFile = readFileFromWorkspace
	svc.writeFile = writeFileToWorkspace
	svc.sinceInteractive = func(userID string, profile agentprofiles.Profile) time.Duration {
		return productInteractions.SinceInteractive(context.Background(), userID, profile.Product)
	}
	return svc
}

func (s *ProductScheduleService) setDeferred(userID, jobID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deferred == nil {
		s.deferred = map[string]string{}
	}
	key := userID + "\x1f" + jobID
	if reason == "" {
		delete(s.deferred, key)
		return
	}
	s.deferred[key] = reason
}

func (s *ProductScheduleService) deferredReason(userID, jobID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deferred[userID+"\x1f"+jobID]
}

// usersWithProduct lists the users a product's schedules run for: every
// enabled directory user whose product access includes it, or the single
// local user when the server is not multi-user.
func usersWithProduct(product string) []string {
	if !IsMultiUserMode() {
		return []string{GetDefaultUserID()}
	}
	dir, err := loadUserDirectory()
	if err != nil || dir == nil {
		return nil
	}
	var out []string
	for i := range dir.Users {
		rec := &dir.Users[i]
		if rec.Disabled || strings.TrimSpace(rec.ID) == "" {
			continue
		}
		if userAccessAllowsProduct(accessForRecord(rec), product) {
			out = append(out, rec.ID)
		}
	}
	sort.Strings(out)
	return out
}

func userAccessAllowsProduct(acc UserAccess, product string) bool {
	if !acc.ProductsRestricted {
		return true
	}
	for _, p := range acc.Products {
		if strings.EqualFold(strings.TrimSpace(p), strings.TrimSpace(product)) {
			return true
		}
	}
	return false
}

func productAccessName(profile agentprofiles.Profile) string {
	if name := strings.TrimSpace(profile.Product); name != "" {
		return name
	}
	return strings.TrimSpace(profile.ID)
}

// profilesWithSchedules returns the built-in profiles that declare schedules.
func (s *ProductScheduleService) profilesWithSchedules() []agentprofiles.Profile {
	if s.registry == nil {
		return nil
	}
	var out []agentprofiles.Profile
	for _, p := range s.registry.List("") {
		if p.BuiltIn && len(p.Schedules) > 0 {
			out = append(out, p)
		}
	}
	return out
}

func (s *ProductScheduleService) profilesWithProjectSchedules() []agentprofiles.Profile {
	if s.registry == nil {
		return nil
	}
	var out []agentprofiles.Profile
	for _, p := range s.registry.List("") {
		if p.BuiltIn && p.UIPanels.Schedules && strings.EqualFold(strings.TrimSpace(p.Runtime.Conversation.Mode), agentprofiles.ConversationModeKeyed) {
			out = append(out, p)
		}
	}
	return out
}

func (s *ProductScheduleService) projectJobsForUser(ctx context.Context, userID string, profile agentprofiles.Profile, states map[string]productScheduleUserState) ([]productScheduleJob, error) {
	projectsRoot, err := cleanAgentProfileWorkspace(profile.Runtime.Workspace.ProjectsRoot, userID)
	if err != nil {
		return nil, err
	}
	runtimeRoot := agentProfileRuntimeWorkspace(userID, projectsRoot)
	store := defaultProductProjectStore()
	paths, exists, err := store.listPaths(ctx, runtimeRoot)
	if err != nil || !exists {
		return nil, err
	}
	rootPrefix := strings.TrimSuffix(filepath.ToSlash(runtimeRoot), "/") + "/"
	seen := map[string]struct{}{}
	var jobs []productScheduleJob
	for _, candidate := range paths {
		candidate = filepath.ToSlash(strings.TrimSpace(candidate))
		if !strings.HasPrefix(candidate, rootPrefix) || !strings.HasSuffix(candidate, "/product.json") {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		raw, found, readErr := store.read(ctx, candidate)
		if readErr != nil {
			return nil, readErr
		}
		if !found {
			continue
		}
		var manifest productProjectManifest
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil || manifest.Product != profile.ID || strings.TrimSpace(manifest.ID) == "" {
			continue
		}
		if err := productschedule.ValidateAll(manifest.Schedules); err != nil {
			return nil, fmt.Errorf("project %s schedules: %w", manifest.ID, err)
		}
		for _, sched := range manifest.Schedules {
			job := productScheduleJob{
				UserID: userID, Profile: profile, Schedule: sched,
				ProjectID: manifest.ID, ProjectTitle: manifest.Title,
				WorkspacePath: filepath.ToSlash(filepath.Dir(candidate)), ManifestPath: candidate,
			}
			job.State = states[scheduleStateKey(job)]
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (s *ProductScheduleService) loadState(ctx context.Context, userID string) (map[string]productScheduleUserState, error) {
	content, exists, err := s.readFile(ctx, productScheduleStatePath(userID))
	if err != nil {
		return nil, err
	}
	out := map[string]productScheduleUserState{}
	if !exists || strings.TrimSpace(content) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", productScheduleStateFile, err)
	}
	return out, nil
}

// updateState applies fn to one schedule's state under the state-file lock.
func (s *ProductScheduleService) updateState(ctx context.Context, userID, profileID, scheduleID string, fn func(*productScheduleUserState)) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	all, err := s.loadState(ctx, userID)
	if err != nil {
		return err
	}
	key := productScheduleStateKey(profileID, scheduleID)
	st := all[key]
	fn(&st)
	all[key] = st
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(ctx, productScheduleStatePath(userID), string(data))
}

// JobsForUser lists every product schedule visible to one user.
func (s *ProductScheduleService) JobsForUser(ctx context.Context, userID string) ([]productScheduleJob, error) {
	if s == nil || strings.TrimSpace(userID) == "" {
		return nil, nil
	}
	var jobs []productScheduleJob
	var states map[string]productScheduleUserState
	for _, profile := range s.profilesWithSchedules() {
		if !containsUserID(s.users(productAccessName(profile)), userID) {
			continue
		}
		if states == nil {
			var err error
			if states, err = s.loadState(ctx, userID); err != nil {
				return nil, err
			}
		}
		for _, sched := range profile.Schedules {
			jobs = append(jobs, productScheduleJob{
				UserID:   userID,
				Profile:  profile,
				Schedule: sched,
				State:    states[productScheduleStateKey(profile.ID, sched.ID)],
			})
		}
	}
	for _, profile := range s.profilesWithProjectSchedules() {
		if !containsUserID(s.users(productAccessName(profile)), userID) {
			continue
		}
		if states == nil {
			var err error
			if states, err = s.loadState(ctx, userID); err != nil {
				return nil, err
			}
		}
		projectJobs, err := s.projectJobsForUser(ctx, userID, profile, states)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, projectJobs...)
	}
	return jobs, nil
}

func containsUserID(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Job finds one product schedule for a user by job id.
func (s *ProductScheduleService) Job(ctx context.Context, userID, jobID string) (productScheduleJob, error) {
	profileID, scheduleID, ok := parseProductScheduleJobID(jobID)
	projectID := ""
	if projectProfileID, parsedProjectID, projectScheduleID, projectOK := parseProjectScheduleJobID(jobID); projectOK {
		profileID, projectID, scheduleID, ok = projectProfileID, parsedProjectID, projectScheduleID, true
	}
	if !ok {
		return productScheduleJob{}, fmt.Errorf("not a product schedule id: %q", jobID)
	}
	jobs, err := s.JobsForUser(ctx, userID)
	if err != nil {
		return productScheduleJob{}, err
	}
	for _, j := range jobs {
		if j.Profile.ID == profileID && j.ProjectID == projectID && j.Schedule.ID == scheduleID {
			return j, nil
		}
	}
	return productScheduleJob{}, fmt.Errorf("product schedule %s not found for this user", jobID)
}

// SetEnabled stores a user's enable override.
func (s *ProductScheduleService) SetEnabled(ctx context.Context, userID, jobID string, enabled bool) (productScheduleJob, error) {
	job, err := s.Job(ctx, userID, jobID)
	if err != nil {
		return job, err
	}
	if err := s.updateStateByKey(ctx, userID, scheduleStateKey(job), func(st *productScheduleUserState) {
		st.Enabled = &enabled
	}); err != nil {
		return job, err
	}
	return s.Job(ctx, userID, jobID)
}

func (s *ProductScheduleService) projectManifest(ctx context.Context, userID, profileID, projectID string) (agentprofiles.Profile, productConversationBinding, productProjectManifest, error) {
	if s.registry == nil {
		return agentprofiles.Profile{}, productConversationBinding{}, productProjectManifest{}, fmt.Errorf("product profiles are unavailable")
	}
	profile, err := s.registry.Resolve(profileID, 0, userID)
	if err != nil {
		return agentprofiles.Profile{}, productConversationBinding{}, productProjectManifest{}, err
	}
	if !profile.UIPanels.Schedules || !strings.EqualFold(strings.TrimSpace(profile.Runtime.Conversation.Mode), agentprofiles.ConversationModeKeyed) {
		return agentprofiles.Profile{}, productConversationBinding{}, productProjectManifest{}, fmt.Errorf("profile %q does not support project schedules", profileID)
	}
	binding, err := resolveProductConversationBinding(ctx, userID, profile, projectID)
	if err != nil {
		return agentprofiles.Profile{}, productConversationBinding{}, productProjectManifest{}, err
	}
	raw, found, err := s.readFile(ctx, binding.ManifestPath)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("project manifest not found")
		}
		return agentprofiles.Profile{}, productConversationBinding{}, productProjectManifest{}, err
	}
	var manifest productProjectManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return agentprofiles.Profile{}, productConversationBinding{}, productProjectManifest{}, err
	}
	return profile, binding, manifest, nil
}

func (s *ProductScheduleService) writeProjectManifest(ctx context.Context, binding productConversationBinding, manifest productProjectManifest) error {
	manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(ctx, binding.ManifestPath, string(data)+"\n")
}

func (s *ProductScheduleService) CreateProjectSchedule(ctx context.Context, userID, profileID, projectID string, schedule productschedule.Schedule) (productScheduleJob, error) {
	profile, binding, manifest, err := s.projectManifest(ctx, userID, profileID, projectID)
	if err != nil {
		return productScheduleJob{}, err
	}
	if strings.TrimSpace(schedule.ID) == "" {
		schedule.ID = uuid.NewString()
	}
	manifest.Schedules = append(manifest.Schedules, schedule)
	if err := productschedule.ValidateAll(manifest.Schedules); err != nil {
		return productScheduleJob{}, err
	}
	if err := s.writeProjectManifest(ctx, binding, manifest); err != nil {
		return productScheduleJob{}, err
	}
	return productScheduleJob{UserID: userID, Profile: profile, Schedule: schedule, ProjectID: manifest.ID, ProjectTitle: manifest.Title, WorkspacePath: binding.WorkspacePath, ManifestPath: binding.ManifestPath}, nil
}

func (s *ProductScheduleService) UpdateProjectSchedule(ctx context.Context, userID, jobID string, update func(*productschedule.Schedule)) (productScheduleJob, error) {
	job, err := s.Job(ctx, userID, jobID)
	if err != nil || job.ProjectID == "" {
		return job, firstError(err, fmt.Errorf("not a project schedule"))
	}
	_, binding, manifest, err := s.projectManifest(ctx, userID, job.Profile.ID, job.ProjectID)
	if err != nil {
		return job, err
	}
	found := false
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID == job.Schedule.ID {
			update(&manifest.Schedules[i])
			job.Schedule = manifest.Schedules[i]
			found = true
			break
		}
	}
	if !found {
		return job, fmt.Errorf("schedule not found")
	}
	if err := productschedule.ValidateAll(manifest.Schedules); err != nil {
		return job, err
	}
	if err := s.writeProjectManifest(ctx, binding, manifest); err != nil {
		return job, err
	}
	return s.Job(ctx, userID, jobID)
}

func (s *ProductScheduleService) DeleteProjectSchedule(ctx context.Context, userID, jobID string) error {
	job, err := s.Job(ctx, userID, jobID)
	if err != nil {
		return err
	}
	if job.ProjectID == "" {
		return fmt.Errorf("not a project schedule")
	}
	_, binding, manifest, err := s.projectManifest(ctx, userID, job.Profile.ID, job.ProjectID)
	if err != nil {
		return err
	}
	next := manifest.Schedules[:0]
	for _, schedule := range manifest.Schedules {
		if schedule.ID != job.Schedule.ID {
			next = append(next, schedule)
		}
	}
	if len(next) == len(manifest.Schedules) {
		return fmt.Errorf("schedule not found")
	}
	manifest.Schedules = next
	return s.writeProjectManifest(ctx, binding, manifest)
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ProductScheduleService) updateStateByKey(ctx context.Context, userID, key string, fn func(*productScheduleUserState)) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	all, err := s.loadState(ctx, userID)
	if err != nil {
		return err
	}
	st := all[key]
	fn(&st)
	all[key] = st
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(ctx, productScheduleStatePath(userID), string(data))
}

// Start runs the tick loop until ctx is done.
func (s *ProductScheduleService) Start(ctx context.Context) {
	if len(s.profilesWithSchedules()) == 0 {
		scheduleLogf("[PRODUCT-SCHEDULE] No product declares schedules; loop idle")
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.tick(ctx, now)
		}
	}
}

func (s *ProductScheduleService) tick(ctx context.Context, now time.Time) {
	users := map[string]struct{}{}
	profiles := append(s.profilesWithSchedules(), s.profilesWithProjectSchedules()...)
	for _, profile := range profiles {
		for _, userID := range s.users(productAccessName(profile)) {
			users[userID] = struct{}{}
		}
	}
	for userID := range users {
		jobs, err := s.JobsForUser(ctx, userID)
		if err != nil {
			scheduleLogf("[PRODUCT-SCHEDULE] %s: cannot discover schedules: %v", userID, err)
			continue
		}
		for _, job := range jobs {
			in := productschedule.Inputs{
				Now:                 now,
				LastRun:             job.lastRun(),
				LastAttempt:         job.lastAttempt(),
				ConsecutiveFailures: job.State.ConsecutiveFailures,
				SinceInteractive:    productInteractionNever,
			}
			if s.sinceInteractive != nil {
				in.SinceInteractive = s.sinceInteractive(userID, job.Profile)
			}
			d := productschedule.Decide(job.Effective(), in)
			if d.Deferred {
				s.setDeferred(userID, job.ID(), d.Reason)
			} else {
				s.setDeferred(userID, job.ID(), "")
			}
			if !d.Run {
				if d.Deferred {
					scheduleLogf("[PRODUCT-SCHEDULE] %s deferring for %s: %s", job.ID(), userID, d.Reason)
				}
				continue
			}
			go func(job productScheduleJob, scheduledFor time.Time) {
				if _, err := s.Run(context.Background(), job, "cron", scheduledFor); err != nil && !errors.Is(err, productschedule.ErrAlreadyRunning) {
					scheduleLogf("[PRODUCT-SCHEDULE] %s failed for %s: %v", job.ID(), job.UserID, err)
				}
			}(job, d.ScheduledFor)
		}
	}
}

// Trigger runs a product schedule now for one user, ignoring its enabled flag.
func (s *ProductScheduleService) Trigger(ctx context.Context, userID, jobID string) (string, error) {
	job, err := s.Job(ctx, userID, jobID)
	if err != nil {
		return "", err
	}
	type result struct {
		sessionID string
		err       error
	}
	started := make(chan result, 1)
	go func() {
		sessionID, err := s.Run(context.Background(), job, "manual", time.Time{}, func(sessionID string) {
			started <- result{sessionID: sessionID}
		})
		select {
		case started <- result{sessionID: sessionID, err: err}:
		default:
		}
	}()
	r := <-started
	return r.sessionID, r.err
}

// ResetHistory clears an isolated schedule's own conversation: the next run
// opens a fresh session, and the transcript a "view check-in history" reader
// was showing is gone. Refused on a schedule that isn't Isolated (there is
// no separate conversation to clear — resetting would touch the profile's
// own chat) and while a run is in progress (rotating the session out from
// under a live turn).
func (s *ProductScheduleService) ResetHistory(ctx context.Context, userID, jobID string) error {
	job, err := s.Job(ctx, userID, jobID)
	if err != nil {
		return err
	}
	if !job.Schedule.Isolated {
		return fmt.Errorf("schedule %q does not run its own conversation", jobID)
	}
	key := userID + "\x1f" + jobID
	s.mu.Lock()
	_, busy := s.running[key]
	s.mu.Unlock()
	if busy {
		return productschedule.ErrAlreadyRunning
	}
	binding, err := resolveIsolatedScheduleBinding(ctx, userID, job.Profile)
	if err != nil {
		return err
	}
	_, err = defaultProductConversationRegistryStore().rotate(ctx, userID, job.Profile, binding)
	return err
}

// Running reports the live run for a job, if any.
func (s *ProductScheduleService) Running(userID, jobID string) (productScheduleRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.running[userID+"\x1f"+jobID]
	if !ok {
		return productScheduleRun{}, false
	}
	return *r, true
}

// Stop cancels the live run for a job.
func (s *ProductScheduleService) Stop(userID, jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.running[userID+"\x1f"+jobID]
	if !ok {
		return false
	}
	r.cancel()
	return true
}

// Run executes one schedule for one user: resolve the product conversation,
// record a run, send every message in turn, then record the outcome.
// onStarted, when given, is called once the session id is known.
func (s *ProductScheduleService) Run(ctx context.Context, job productScheduleJob, triggerSource string, scheduledFor time.Time, onStarted ...func(sessionID string)) (string, error) {
	if s.api == nil {
		return "", fmt.Errorf("product schedules: server not ready")
	}
	key := job.UserID + "\x1f" + job.ID()
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if _, busy := s.running[key]; busy {
		s.mu.Unlock()
		cancel()
		return "", productschedule.ErrAlreadyRunning
	}
	run := &productScheduleRun{StartedAt: time.Now().UTC(), cancel: cancel}
	s.running[key] = run
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, key)
		s.mu.Unlock()
		cancel()
	}()

	var binding productConversationBinding
	var bindErr error
	if job.ProjectID != "" {
		binding, bindErr = resolveProductConversationBinding(runCtx, job.UserID, job.Profile, job.ProjectID)
	} else if job.Schedule.Isolated {
		binding, bindErr = resolveIsolatedScheduleBinding(runCtx, job.UserID, job.Profile)
	} else {
		binding, bindErr = resolveProductConversationBinding(runCtx, job.UserID, job.Profile, "")
	}
	if bindErr != nil {
		return "", fmt.Errorf("resolve product conversation: %w", bindErr)
	}
	conversation, err := defaultProductConversationRegistryStore().resolveOrCreate(runCtx, job.UserID, job.Profile, binding, "")
	if err != nil {
		return "", fmt.Errorf("open product conversation: %w", err)
	}
	sessionID := conversation.SessionID
	s.mu.Lock()
	run.SessionID = sessionID
	s.mu.Unlock()
	for _, cb := range onStarted {
		if cb != nil {
			cb(sessionID)
		}
	}

	runsWorkspace := agentProfileRuntimeWorkspace(job.UserID, conversation.WorkspacePath)
	startedAt := time.Now().UTC()
	entry := &ScheduleRunEntry{
		ID:            uuid.NewString(),
		ScheduleID:    job.ID(),
		TriggerSource: triggerSource,
		SessionID:     sessionID,
		Status:        "running",
		StartedAt:     startedAt,
	}
	if !scheduledFor.IsZero() {
		sf := scheduledFor.UTC()
		entry.ScheduledFor = &sf
	}
	if err := AppendScheduleRun(runCtx, runsWorkspace, entry); err != nil {
		scheduleLogf("[PRODUCT-SCHEDULE] %s: cannot record run for %s: %v", job.ID(), job.UserID, err)
	}
	_ = s.updateStateByKey(runCtx, job.UserID, scheduleStateKey(job), func(st *productScheduleUserState) {
		st.LastStatus = "running"
		st.LastSessionID = sessionID
		st.LastError = ""
		st.LastAttemptAt = startedAt.Format(time.RFC3339)
	})

	scheduleLogf("[PRODUCT-SCHEDULE] 🚀 %s (%s) for user %s: %d message(s), session %s", job.ID(), job.Schedule.Name, job.UserID, len(job.Schedule.Messages), sessionID)
	var runErr error
	for i, message := range job.Schedule.Messages {
		if err := runCtx.Err(); err != nil {
			runErr = err
			break
		}
		req, err := queryRequestForAgentProfileChat(job.Profile, AgentProfileChatRequest{Message: message}, conversation)
		if err != nil {
			runErr = err
			break
		}
		reqMap, err := queryRequestToMap(req)
		if err != nil {
			runErr = err
			break
		}
		reqMap["triggered_by"] = firstNonEmptyTrimmed(triggerSource, "cron")
		reqMap["session_title"] = firstNonEmptyTrimmed(conversation.Title, job.Profile.Name)
		scheduleLogf("[PRODUCT-SCHEDULE] %s turn %d/%d for %s", job.ID(), i+1, len(job.Schedule.Messages), job.UserID)
		if err := s.api.startSessionInternal(runCtx, reqMap, sessionID, job.UserID, nil); err != nil {
			runErr = fmt.Errorf("message %d/%d: %w", i+1, len(job.Schedule.Messages), err)
			break
		}
	}

	status := "success"
	errMsg := ""
	if runErr != nil {
		status = "error"
		if errors.Is(runErr, context.Canceled) {
			status = "stopped"
		}
		errMsg = runErr.Error()
	}
	duration := time.Since(startedAt).Milliseconds()
	_ = UpdateScheduleRun(context.Background(), runsWorkspace, entry.ID, status, errMsg, &duration, "", sessionID)
	_ = s.updateStateByKey(context.Background(), job.UserID, scheduleStateKey(job), func(st *productScheduleUserState) {
		st.LastStatus = status
		st.LastError = errMsg
		st.LastDurationMs = &duration
		st.RunCount++
		if runErr == nil {
			st.LastRunAt = time.Now().UTC().Format(time.RFC3339)
			st.ConsecutiveFailures = 0
		} else {
			st.ConsecutiveFailures++
		}
	})
	if runErr != nil {
		scheduleLogf("[PRODUCT-SCHEDULE] ❌ %s for %s: %v", job.ID(), job.UserID, runErr)
		return sessionID, runErr
	}
	scheduleLogf("[PRODUCT-SCHEDULE] ✅ %s for %s completed in %dms", job.ID(), job.UserID, duration)
	return sessionID, nil
}

func queryRequestToMap(req QueryRequest) (map[string]interface{}, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RunsWorkspace is where a job's schedule-runs.json lives for a user.
func (s *ProductScheduleService) RunsWorkspace(ctx context.Context, job productScheduleJob) (string, error) {
	if job.ProjectID != "" {
		return agentProfileRuntimeWorkspace(job.UserID, job.WorkspacePath), nil
	}
	binding, err := resolveProductConversationBinding(ctx, job.UserID, job.Profile, "")
	if err != nil {
		return "", err
	}
	return agentProfileRuntimeWorkspace(job.UserID, binding.WorkspacePath), nil
}

// jobResponse renders a product schedule in the shape the schedules UI reads.
func (s *ProductScheduleService) jobResponse(job productScheduleJob, runsWorkspace string) ScheduledJobResponse {
	sched := job.Effective()
	resp := ScheduledJobResponse{
		ID:                  job.ID(),
		Name:                sched.Name,
		Description:         sched.Description,
		EntityType:          "product",
		WorkspacePath:       runsWorkspace,
		WorkflowID:          firstNonEmptyTrimmed(job.ProjectID, job.Profile.ID),
		WorkflowLabel:       firstNonEmptyTrimmed(job.ProjectTitle, job.Profile.Name),
		Mode:                "workshop",
		Messages:            sched.Messages,
		ScheduleType:        "cron",
		CronExpression:      sched.CronExpression,
		Timezone:            sched.Timezone,
		Enabled:             sched.Enabled,
		LastSessionID:       job.State.LastSessionID,
		LastStatus:          job.State.LastStatus,
		LastError:           job.State.LastError,
		LastDurationMs:      job.State.LastDurationMs,
		RunCount:            job.State.RunCount,
		ConsecutiveFailures: job.State.ConsecutiveFailures,
		DeferredReason:      s.deferredReason(job.UserID, job.ID()),
	}
	if sched.CronExpression == "" && sched.CadenceHours > 0 {
		// The cadence form has no cron line; describe it so the UI shows something.
		resp.Description = strings.TrimSpace(resp.Description + fmt.Sprintf(" (every %dh)", sched.CadenceHours))
	}
	if last := job.lastRun(); !last.IsZero() {
		resp.LastRunAt = &last
	}
	if sched.Enabled {
		d := productschedule.Decide(sched, productschedule.Inputs{Now: time.Now(), LastRun: job.lastRun(), SinceInteractive: 365 * 24 * time.Hour})
		if !d.ScheduledFor.IsZero() {
			next := d.ScheduledFor.UTC()
			resp.NextRunAt = &next
		} else if sched.CronExpression != "" {
			resp.NextRunAt = getNextRunTime(sched.CronExpression, sched.Timezone)
		}
	}
	if r, ok := s.Running(job.UserID, job.ID()); ok {
		resp.LastStatus = "running"
		resp.LastSessionID = r.SessionID
		started := r.StartedAt
		resp.LastRunAt = &started
	}
	return resp
}
