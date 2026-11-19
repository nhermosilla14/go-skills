package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// UpdateOptions are the inputs of the update use case.
type UpdateOptions struct {
	Skills  []string      // installed names to update; empty updates every tracked skill
	Project bool          // -p: update the project scope without asking
	Global  bool          // -g: update the global scope
	Yes     bool          // -y: never prompt
	Runner  source.Runner // git runner; nil uses PATH, tests pass a fake
}

// UpdateStatus is the outcome of one tracked skill.
type UpdateStatus string

const (
	// UpdateCurrent means the fetched content matches the lock and the skill is still on disk.
	UpdateCurrent UpdateStatus = "current"
	// UpdateUpdated means the skill was reinstalled.
	UpdateUpdated UpdateStatus = "updated"
	// UpdateSkipped means the entry was left alone on purpose.
	UpdateSkipped UpdateStatus = "skipped"
	// UpdateFailed means checking or reinstalling the skill failed.
	UpdateFailed UpdateStatus = "failed"
)

// UpdateResult is the outcome of one tracked skill.
type UpdateResult struct {
	Name   string
	Status UpdateStatus
	Reason string
}

// UpdateCheck is the outcome of fetching one source group.
type UpdateCheck struct {
	Source string // sourceUrl or source, as recorded in the lock
	Err    string // empty when the group was fetched
}

// UncheckableSkill is a lock entry update cannot verify, with its reason and recorded source.
type UncheckableSkill struct {
	Name   string
	Reason string
	Source string
}

// UpdateReport is the outcome of an update run, for the CLI to render.
type UpdateReport struct {
	Scope           string
	Checks          []UpdateCheck
	Results         []UpdateResult
	Uncheckable     []UncheckableSkill
	Notes           []string
	NothingToUpdate bool
	LockPath        string // set when the lock file was written

	// Phases holds one report per executed scope when a run updates both. The fields above
	// aggregate the phases so Failed, Changed and the exit code see the whole run; the CLI prints
	// each phase under its own header.
	Phases []UpdateReport
}

// Failed reports whether a source or a skill failed, which makes the command exit non-zero.
func (r UpdateReport) Failed() bool {
	for _, c := range r.Checks {
		if c.Err != "" {
			return true
		}
	}
	for _, res := range r.Results {
		if res.Status == UpdateFailed {
			return true
		}
	}
	return false
}

// Changed reports whether at least one skill was reinstalled.
func (r UpdateReport) Changed() bool {
	for _, res := range r.Results {
		if res.Status == UpdateUpdated {
			return true
		}
	}
	return false
}

// Skipped reports whether an entry was left alone or cannot be checked automatically, which
// suppresses the all-up-to-date summary.
func (r UpdateReport) Skipped() bool {
	if len(r.Uncheckable) > 0 {
		return true
	}
	for _, res := range r.Results {
		if res.Status == UpdateSkipped {
			return true
		}
	}
	return false
}

// Update refreshes the installed skills of the selected scopes from their recorded sources. The
// project lock and the global lock never mix: each phase reads and writes only its own.
func Update(opts UpdateOptions, env Env, prompters ...Prompter) (UpdateReport, error) {
	var prompter Prompter
	if len(prompters) > 0 {
		prompter = prompters[0]
	}
	scopes, err := chooseUpdateScopes(opts, env, prompter)
	if err != nil {
		return UpdateReport{}, err
	}
	unmatched := unmatchedUpdateNames(opts.Skills, scopes, env)
	if len(scopes) == 1 {
		return updateScope(opts, env, scopes[0], unmatched)
	}
	rep := UpdateReport{Scope: string(ScopeBoth), NothingToUpdate: true}
	for i, scope := range scopes {
		// A name that matches neither lock is reported once, by the last phase.
		report := []string{}
		if i == len(scopes)-1 {
			report = unmatched
		}
		phase, err := updateScope(opts, env, scope, report)
		if err != nil {
			return rep, err
		}
		rep.Phases = append(rep.Phases, phase)
		rep.merge(phase)
	}
	return rep, nil
}

// merge folds one phase of a both-scope run into the aggregate report the exit code reads.
func (r *UpdateReport) merge(phase UpdateReport) {
	r.Checks = append(r.Checks, phase.Checks...)
	r.Results = append(r.Results, phase.Results...)
	r.Uncheckable = append(r.Uncheckable, phase.Uncheckable...)
	if phase.LockPath != "" {
		r.LockPath = phase.LockPath
	}
	r.NothingToUpdate = r.NothingToUpdate && phase.NothingToUpdate
}

// updateScope runs one phase of a run. unmatched holds the requested names this phase reports as
// matching nothing, resolved once for the whole run.
func updateScope(opts UpdateOptions, env Env, scope Scope, unmatched []string) (UpdateReport, error) {
	rep := UpdateReport{Scope: string(scope)}
	agentEnv := env.agentsEnv()
	lock, err := openUpdateLock(env, agentEnv, scope == ScopeGlobal)
	if err != nil {
		return rep, err
	}
	selected, _ := selectUpdateKeys(lock, opts.Skills)
	tasks, groups := planUpdates(lock, selected, unmatched, &rep)
	if len(tasks) == 0 && len(rep.Results) == 0 && len(rep.Uncheckable) == 0 {
		rep.NothingToUpdate = true
		rep.Notes = append(rep.Notes, nothingToUpdateNotes(lock.global())...)
		return rep, nil
	}
	defer removeUpdateTrees(groups)
	acquireUpdateGroups(groups, env, opts.Runner, &rep)
	changed := false
	for _, t := range tasks {
		if groups[t.group].err != "" {
			continue // reported once, as the group check failure
		}
		res, entryChanged, note := updateSkill(env, agentEnv, lock, t, groups[t.group], opts.Runner)
		if entryChanged {
			changed = true
		}
		if note != "" {
			rep.Notes = append(rep.Notes, note)
		}
		rep.Results = append(rep.Results, res)
	}
	if changed {
		if err := lock.save(); err != nil {
			return rep, err
		}
		rep.LockPath = lock.path()
	}
	return rep, nil
}

// chooseUpdateScopes resolves the scopes of the run: -g and -p select their own phases, names
// without a flag update both (global first, then project, as upstream does), and a run without
// names follows upstream's non-interactive default or asks on a terminal.
func chooseUpdateScopes(opts UpdateOptions, env Env, prompter Prompter) ([]Scope, error) {
	switch {
	case opts.Global && opts.Project:
		return []Scope{ScopeGlobal, ScopeProject}, nil
	case opts.Global:
		return []Scope{ScopeGlobal}, nil
	case opts.Project:
		return []Scope{ScopeProject}, nil
	case len(opts.Skills) > 0:
		return []Scope{ScopeGlobal, ScopeProject}, nil
	case opts.Yes || !env.IsTTY:
		if hasProjectSkills(env) {
			return []Scope{ScopeProject}, nil
		}
		return []Scope{ScopeGlobal}, nil
	}
	if prompter == nil {
		return nil, promptRequired(true)
	}
	answers, err := prompter.Ask(Questions{
		Steps:    []StepID{StepUpdateScope},
		Defaults: Answers{Scope: ScopeProject},
	})
	if errors.Is(err, ErrPromptRequired) {
		return nil, promptRequired(true)
	}
	if err != nil {
		return nil, err
	}
	switch answers.Scope {
	case ScopeProject:
		return []Scope{ScopeProject}, nil
	case ScopeGlobal:
		return []Scope{ScopeGlobal}, nil
	case ScopeBoth:
		return []Scope{ScopeGlobal, ScopeProject}, nil
	default:
		return nil, fmt.Errorf("%w: unknown update scope %q", ErrUsage, answers.Scope)
	}
}

// unmatchedUpdateNames reports the requested names that no lock of the run tracks. Resolving them
// before the phases lets a both-scope run report each name once, and only when both locks miss it.
func unmatchedUpdateNames(names []string, scopes []Scope, env Env) []string {
	if len(names) == 0 {
		return nil
	}
	agentEnv := env.agentsEnv()
	matched := make(map[string]bool, len(names))
	for _, scope := range scopes {
		lock, err := openUpdateLock(env, agentEnv, scope == ScopeGlobal)
		if err != nil {
			continue // the phase reports the broken lock itself
		}
		for _, name := range names {
			if len(matchUpdateKeys(lock, name)) > 0 {
				matched[name] = true
			}
		}
	}
	var out []string
	for _, name := range names {
		if !matched[name] {
			out = append(out, name)
		}
	}
	return out
}

// nothingToUpdateNotes is what a run with no tracked skills reports.
func nothingToUpdateNotes(global bool) []string {
	if global {
		return []string{
			"No global skills tracked in lock file.",
			"Install skills with skills add <source> -g.",
		}
	}
	return []string{
		"No project skills to update.",
		"Install project skills with skills add <source>.",
	}
}

// hasProjectSkills reports whether the project has a lock file or at least one installed canonical
// skill, the test upstream uses to pick the project scope without asking.
func hasProjectSkills(env Env) bool {
	if _, err := os.Stat(filepath.Join(env.WorkDir, lockfile.FileName)); err == nil {
		return true
	}
	dir := agents.CanonicalSkillsDir(env.agentsEnv(), false)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if info, err := os.Stat(filepath.Join(dir, entry.Name(), "SKILL.md")); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// updateRecord is the union of the fields update reads and writes, so the two lock versions share
// one update path.
type updateRecord struct {
	Source          string
	SourceURL       string
	SourceType      string
	Ref             string
	Commit          string
	SkillPath       string
	SkillName       string
	Subagents       []string // Eve placement: "" is the root agent
	InstallMode     string
	ComputedHash    string // project lock content hash
	SkillFolderHash string // global lock folder hash
	InstalledAt     string
	UpdatedAt       string
	PluginName      string
	SourceBaseURL   string
	WellKnownDigest string
}

// recordedHash is the hash update compares against: the content hash of a project entry, or the
// folder hash of a global one.
func (e updateRecord) recordedHash() string {
	if e.ComputedHash != "" {
		return e.ComputedHash
	}
	return e.SkillFolderHash
}

// updateLock adapts a project or global lock to the update flow.
type updateLock interface {
	keys() []string
	entry(key string) (updateRecord, bool)
	put(key string, e updateRecord)
	save() error
	path() string
	global() bool
}

// openUpdateLock reads the lock of the selected scope.
func openUpdateLock(env Env, agentEnv agents.Environment, global bool) (updateLock, error) {
	if global {
		path := agents.GlobalLockPath(agentEnv)
		lock, err := lockfile.LoadGlobal(path)
		return &globalUpdateLock{lock: lock, lockPath: path}, err
	}
	path := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(path)
	return &projectUpdateLock{lock: lock, lockPath: path}, err
}

// projectUpdateLock adapts the project lock.
type projectUpdateLock struct {
	lock     *lockfile.File
	lockPath string
}

func (l *projectUpdateLock) keys() []string { return lockKeys(l.lock) }

func (l *projectUpdateLock) entry(key string) (updateRecord, bool) {
	e, ok := l.lock.Get(key)
	return updateRecord{
		Source: e.Source, SourceURL: e.SourceURL, SourceType: e.SourceType, Ref: e.Ref,
		Commit: e.Commit, SkillPath: e.SkillPath, SkillName: e.SkillName,
		Subagents: e.Subagents, InstallMode: e.InstallMode, ComputedHash: e.ComputedHash,
	}, ok
}

func (l *projectUpdateLock) put(key string, e updateRecord) {
	l.lock.Put(key, lockfile.Entry{
		Source: e.Source, SourceURL: e.SourceURL, SourceType: e.SourceType, Ref: e.Ref,
		Commit: e.Commit, SkillPath: e.SkillPath, SkillName: e.SkillName,
		Subagents: e.Subagents, InstallMode: e.InstallMode, ComputedHash: e.ComputedHash,
	})
}

func (l *projectUpdateLock) save() error  { return l.lock.Save(l.lockPath) }
func (l *projectUpdateLock) path() string { return l.lockPath }
func (l *projectUpdateLock) global() bool { return false }

// globalUpdateLock adapts the global lock.
type globalUpdateLock struct {
	lock     *lockfile.GlobalFile
	lockPath string
}

func (l *globalUpdateLock) keys() []string { return globalLockKeys(l.lock) }

func (l *globalUpdateLock) entry(key string) (updateRecord, bool) {
	e, ok := l.lock.Get(key)
	return updateRecord{
		Source: e.Source, SourceURL: e.SourceURL, SourceType: e.SourceType, Ref: e.Ref,
		Commit: e.Commit, SkillPath: e.SkillPath, SkillName: e.SkillName,
		InstallMode: e.InstallMode, SkillFolderHash: e.SkillFolderHash,
		InstalledAt: e.InstalledAt, UpdatedAt: e.UpdatedAt, PluginName: e.PluginName,
		SourceBaseURL: e.SourceBaseURL, WellKnownDigest: e.WellKnownDigest,
	}, ok
}

func (l *globalUpdateLock) put(key string, e updateRecord) {
	l.lock.Put(key, lockfile.GlobalEntry{
		Source: e.Source, SourceType: e.SourceType, SourceURL: e.SourceURL, Ref: e.Ref,
		Commit: e.Commit, SkillPath: e.SkillPath, SkillName: e.SkillName,
		SkillFolderHash: e.SkillFolderHash, InstalledAt: e.InstalledAt, UpdatedAt: e.UpdatedAt,
		PluginName: e.PluginName, SourceBaseURL: e.SourceBaseURL,
		WellKnownDigest: e.WellKnownDigest, InstallMode: e.InstallMode,
	})
}

func (l *globalUpdateLock) save() error  { return l.lock.Save(l.lockPath) }
func (l *globalUpdateLock) path() string { return l.lockPath }
func (l *globalUpdateLock) global() bool { return true }

// updateTask is one lock entry to check, with the group that acquires its source.
type updateTask struct {
	key   string
	entry updateRecord
	group int
}

// updateGroup is one source and ref, acquired once for every entry it tracks.
type updateGroup struct {
	source string
	ref    string
	dir    string
	tree   source.Tree
	found  skill.Result
	err    string
}

// planUpdates classifies the lock entries: the names that match nothing are reported, the rest are
// grouped by sourceUrl or source plus ref, in lock key order.
func planUpdates(lock updateLock, selected map[string]bool, unmatched []string,
	rep *UpdateReport) ([]updateTask, []*updateGroup) {
	for _, name := range unmatched {
		rep.Results = append(rep.Results, UpdateResult{
			Name: name, Status: UpdateFailed, Reason: "no installed skill matches this name",
		})
	}
	groupIndex := map[string]int{}
	var tasks []updateTask
	var groups []*updateGroup
	for _, key := range lock.keys() {
		if selected != nil && !selected[key] {
			continue
		}
		entry, _ := lock.entry(key)
		if entry.SourceType == "node_modules" {
			if lock.global() {
				rep.Uncheckable = append(rep.Uncheckable, UncheckableSkill{
					Name: key, Reason: "node_modules sources cannot be checked automatically",
					Source: source.RedactCredentials(updateRecordSource(entry)),
				})
			}
			continue
		}
		if reason := uncheckableReason(entry); reason != "" {
			rep.Uncheckable = append(rep.Uncheckable, UncheckableSkill{
				Name: key, Reason: reason, Source: source.RedactCredentials(updateRecordSource(entry)),
			})
			continue
		}
		raw := source.RedactCredentials(updateRecordSource(entry))
		groupKey := raw + "\x00" + entry.Ref
		index, ok := groupIndex[groupKey]
		if !ok {
			index = len(groups)
			groupIndex[groupKey] = index
			groups = append(groups, &updateGroup{source: raw, ref: entry.Ref})
		}
		tasks = append(tasks, updateTask{key: key, entry: entry, group: index})
	}
	return tasks, groups
}

// uncheckableReason explains why a lock entry cannot be checked automatically, or returns "".
func uncheckableReason(e updateRecord) string {
	switch e.SourceType {
	case "local":
		return "Local path"
	case "github", "gitlab", "git", "azure":
	case "download":
		return "Download sources cannot be checked automatically"
	case "well-known":
		return "Well-known skill"
	default:
		if e.SourceType == "" {
			return "No source type recorded"
		}
		return e.SourceType + " sources cannot be checked automatically"
	}
	if e.SkillPath == "" {
		return "No skill path recorded"
	}
	if updateRecordSource(e) == "" {
		return "No source recorded"
	}
	return ""
}

// updateRecordSource is the location a lock entry is fetched from: the fetch URL when recorded, the
// shorthand otherwise.
func updateRecordSource(e updateRecord) string {
	if e.SourceURL != "" {
		return e.SourceURL
	}
	return e.Source
}

// selectUpdateKeys resolves the optional name filter against the lock keys: an exact
// case-insensitive match first, then the normalized and sanitized forms. It returns the keys to
// update and the names that matched nothing.
func selectUpdateKeys(lock updateLock, names []string) (map[string]bool, []string) {
	if len(names) == 0 {
		return nil, nil
	}
	selected := map[string]bool{}
	var unmatched []string
	for _, name := range names {
		keys := matchUpdateKeys(lock, name)
		if len(keys) == 0 {
			unmatched = append(unmatched, name)
			continue
		}
		for _, key := range keys {
			selected[key] = true
		}
	}
	return selected, unmatched
}

// matchUpdateKeys returns the lock keys a requested name resolves to.
func matchUpdateKeys(lock updateLock, name string) []string {
	var exact []string
	for _, key := range lock.keys() {
		if strings.EqualFold(key, name) {
			exact = append(exact, key)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	want := skill.NormalizeName(name)
	sanitized := skill.SanitizeName(name)
	var matched []string
	for _, key := range lock.keys() {
		if skill.NormalizeName(key) == want || skill.SanitizeName(key) == sanitized {
			matched = append(matched, key)
		}
	}
	return matched
}

// acquireUpdateGroups fetches every group once, recording a check line per group. A group that
// cannot be fetched does not stop the others.
func acquireUpdateGroups(groups []*updateGroup, env Env, runner source.Runner, rep *UpdateReport) {
	for _, g := range groups {
		g.err = fetchUpdateGroup(g, env, runner)
		rep.Checks = append(rep.Checks, UpdateCheck{Source: g.source, Err: g.err})
	}
}

// fetchUpdateGroup clones the group source and discovers its skills. The temporary tree is kept
// until the run ends so every entry of the group shares it.
func fetchUpdateGroup(g *updateGroup, env Env, runner source.Runner) string {
	src, err := source.Parse(g.source)
	if err != nil {
		return err.Error()
	}
	src.Ref = g.ref
	dir, err := os.MkdirTemp("", "skills-update-*")
	if err != nil {
		return fmt.Sprintf("creating a temporary directory: %v", err)
	}
	g.dir = dir
	tree, err := acquireUpdateTree(context.Background(), src, dir, runner)
	if err != nil {
		return err.Error()
	}
	found, err := discover(tree.Dir, src, env.IncludeInternal, true)
	if err != nil {
		return err.Error()
	}
	g.tree, g.found = tree, found
	return ""
}

// acquireUpdateTree clones with the given runner, keeping the production PATH check when the run
// uses the real git binary.
func acquireUpdateTree(ctx context.Context, src source.Source, dir string, runner source.Runner) (source.Tree, error) {
	if runner == nil {
		return source.Acquire(ctx, src, dir)
	}
	return source.AcquireWith(ctx, src, dir, runner)
}

// removeUpdateTrees deletes the temporary checkouts of a run.
func removeUpdateTrees(groups []*updateGroup) {
	for _, g := range groups {
		if g.dir != "" {
			_ = os.RemoveAll(g.dir)
		}
	}
}

// updateSkill compares one lock entry with its fetched tree and reinstalls it when the content,
// the location or the recorded path changed. It reports the result and whether the lock entry
// changed in memory.
func updateSkill(env Env, agentEnv agents.Environment, lock updateLock, t updateTask,
	g *updateGroup, runner source.Runner) (UpdateResult, bool, string) {
	installed := skill.SanitizeName(t.key)
	found, res, ok := locateUpdateSkill(g.found.Skills, updateRecordedName(t.entry, t.key), t.entry.SkillPath)
	if !ok {
		res.Name = t.key
		return res, false, ""
	}
	locations := installedAgents(agentEnv, installed, lock.global())
	upstream, err := fetchedHash(g, t.entry, found, lock.global(), runner)
	if err != nil {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: err.Error()}, false, ""
	}
	moved := normalizeSkillPath(found.SkillPath()) != normalizeSkillPath(t.entry.SkillPath)
	if evePlaced(t.entry.Subagents) {
		if !moved && eveCurrent(agentEnv, installed, t.entry.Subagents, found) {
			return UpdateResult{Name: t.key, Status: UpdateCurrent},
				backfillCommit(lock, t.entry, t.key, g.tree.Commit, env), ""
		}
	} else if !moved && len(locations) > 0 && t.entry.recordedHash() != "" &&
		strings.EqualFold(upstream, t.entry.recordedHash()) {
		return UpdateResult{Name: t.key, Status: UpdateCurrent},
			backfillCommit(lock, t.entry, t.key, g.tree.Commit, env), ""
	}
	return reinstallSkill(env, agentEnv, lock, t, g, found, installed, locations)
}

// fetchedHash is the hash of the fetched skill folder in the shape the lock records: a content
// hash for the project lock and for content-hashed global entries, and a Git tree SHA for GitHub
// global entries recorded as 40-hex.
func fetchedHash(g *updateGroup, entry updateRecord, found skill.Skill, global bool,
	runner source.Runner) (string, error) {
	if global && isTreeHash(entry.SkillFolderHash) && entry.SourceType == string(source.KindGitHub) {
		sha, err := source.TreeSHA(context.Background(), g.tree.Dir, g.tree.Commit,
			skillFolderDir(entry.SkillPath), runner)
		if err != nil {
			return "", err
		}
		return sha, nil
	}
	return hash.Dir(found.Dir)
}

// backfillCommit records the fetched HEAD of an up-to-date git skill, so a following check does not
// report the moved ref as an available update. It reports whether the lock entry changed.
func backfillCommit(lock updateLock, entry updateRecord, key, commit string, env Env) bool {
	if commit == "" || strings.EqualFold(commit, entry.Commit) {
		return false
	}
	entry.Commit = commit
	if lock.global() {
		entry.UpdatedAt = timestamp(env.now())
	}
	lock.put(key, entry)
	return true
}

// locateUpdateSkill finds the fetched skill by its recorded name, falling back to the recorded
// SKILL.md path. Several same-name candidates are ambiguous and fail closed.
func locateUpdateSkill(all []skill.Skill, name, skillPath string) (skill.Skill, UpdateResult, bool) {
	want := skill.NormalizeName(name)
	var candidates []skill.Skill
	for _, s := range all {
		if skill.NormalizeName(s.Name) == want {
			candidates = append(candidates, s)
		}
	}
	switch {
	case len(candidates) > 1:
		return skill.Skill{}, UpdateResult{
			Status: UpdateSkipped, Reason: "several skills match this name; nothing was changed",
		}, false
	case len(candidates) == 1:
		return candidates[0], UpdateResult{}, true
	}
	wantPath := normalizeSkillPath(skillPath)
	if wantPath != "" {
		for _, s := range all {
			if normalizeSkillPath(s.SkillPath()) == wantPath {
				return s, UpdateResult{}, true
			}
		}
	}
	return skill.Skill{}, UpdateResult{Status: UpdateFailed, Reason: "not found in the source"}, false
}

// reinstallSkill installs the fetched skill over every location that currently holds it, keeping
// the recorded install mode and name, then rewrites the lock entry from what landed on disk.
func reinstallSkill(env Env, agentEnv agents.Environment, lock updateLock, t updateTask,
	g *updateGroup, found skill.Skill, installed string, locations []agents.Agent) (UpdateResult, bool, string) {
	rename := ""
	if skill.SanitizeName(found.Name) != installed {
		rename = installed
	}
	mode := updateInstallMode(t.entry, agentEnv, installed, locations, lock.global())
	targets := locations
	if evePlaced(t.entry.Subagents) {
		targets = withEve(targets)
	} else if len(targets) == 0 {
		targets = universalTargets()
	}
	entries := install.Plan(env.WorkDir, []skill.Skill{found}, targets, install.PlanOptions{
		Env: agentEnv, Mode: mode, Rename: rename, Global: lock.global(),
		Subagents: t.entry.Subagents,
	})
	results := install.Apply(applyRoot(env, agentEnv, lock.global()), entries)
	if message, failed := firstFailure(results); failed {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: message}, false, ""
	}
	loc, ok := installedPath(results, entries[0].Name)
	if !ok {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: "no location accepted the skill"}, false, ""
	}
	updated := t.entry
	updated.SkillPath = found.SkillPath()
	if g.tree.Commit != "" {
		updated.Commit = g.tree.Commit
	}
	warning, err := refreshEntryHash(&updated, loc, lock.global())
	if err != nil {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: err.Error()}, false, ""
	}
	if lock.global() {
		updated.UpdatedAt = timestamp(env.now())
		if updated.InstalledAt == "" {
			updated.InstalledAt = updated.UpdatedAt
		}
	}
	lock.put(t.key, updated)
	return UpdateResult{Name: t.key, Status: UpdateUpdated}, true, warning
}

// refreshEntryHash writes the hash of the new install in the shape the lock records.
func refreshEntryHash(entry *updateRecord, installed string, global bool) (string, error) {
	if !global {
		digest, err := hashInstalled(installed)
		if err != nil {
			return "", err
		}
		entry.ComputedHash = digest
		return "", nil
	}
	digest, warning, err := globalFolderHash(globalEntryFromUpdate(*entry), installed)
	if err != nil {
		return "", err
	}
	entry.SkillFolderHash = digest
	return warning, nil
}

// globalEntryFromUpdate converts an update entry into the global lock shape, for hashing.
func globalEntryFromUpdate(e updateRecord) lockfile.GlobalEntry {
	return lockfile.GlobalEntry{
		Source: e.Source, SourceType: e.SourceType, SourceURL: e.SourceURL, Ref: e.Ref,
		Commit: e.Commit, SkillPath: e.SkillPath, SkillName: e.SkillName,
		SkillFolderHash: e.SkillFolderHash, InstalledAt: e.InstalledAt, UpdatedAt: e.UpdatedAt,
	}
}

// applyRoot is the directory installs must stay inside: the project for a project run, home for a
// global one.
func applyRoot(env Env, agentEnv agents.Environment, global bool) string {
	if global {
		return agentEnv.Home
	}
	return env.WorkDir
}

// updateInstallMode keeps the shape the skill was installed with: the recorded installMode wins;
// otherwise the current locations decide, and an existing link is proof of symlink mode.
func updateInstallMode(entry updateRecord, env agents.Environment, name string,
	locations []agents.Agent, global bool) install.Mode {
	if entry.InstallMode == "copy" {
		return install.ModeCopy
	}
	for _, a := range locations {
		if info, err := os.Lstat(agentSkillPath(a, env, name, global)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return install.ModeSymlink
		}
	}
	return install.ResolveMode(locations, false)
}

// firstFailure returns the first failed install message, if any.
func firstFailure(results []install.Result) (string, bool) {
	for _, r := range results {
		if r.Status == install.Failed {
			return r.Message, true
		}
	}
	return "", false
}

// installedAgents lists the agents whose directory currently holds the skill, including the
// universal agents that point at the canonical directory.
func installedAgents(env agents.Environment, name string, global bool) []agents.Agent {
	var out []agents.Agent
	for _, a := range agents.All() {
		if pathExistsForRemoval(agentSkillPath(a, env, name, global)) {
			out = append(out, a)
		}
	}
	return out
}

// universalTargets is the fallback of a skill that is nowhere on disk: the canonical directory.
func universalTargets() []agents.Agent {
	selected, _ := agents.Expand([]string{"universal"})
	return selected
}

// updateRecordedName is the upstream name a lock entry was installed from: skillName for a renamed
// skill, the lock key otherwise.
func updateRecordedName(e updateRecord, key string) string {
	if e.SkillName != "" {
		return e.SkillName
	}
	return key
}

// normalizeSkillPath compares recorded SKILL.md paths across separators and duplicate slashes.
func normalizeSkillPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(strings.ReplaceAll(p, `\`, "/"))
}
