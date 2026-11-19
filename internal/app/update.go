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
	Checks          []UpdateCheck
	Results         []UpdateResult
	Uncheckable     []UncheckableSkill
	Notes           []string
	NothingToUpdate bool
	LockPath        string // set when the lock file was written
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

// Update refreshes the installed project skills from their recorded sources. Global scope is not
// supported yet: -g is rejected by the parser, an explicit name filter updates the project and
// reports the global limitation, and a scope prompt that picks Global or Both fails after asking.
func Update(opts UpdateOptions, env Env, prompters ...Prompter) (UpdateReport, error) {
	var prompter Prompter
	if len(prompters) > 0 {
		prompter = prompters[0]
	}
	rep := UpdateReport{}
	if err := chooseUpdateScope(opts, env, prompter, &rep); err != nil {
		return rep, err
	}
	if rep.NothingToUpdate {
		return rep, nil
	}
	agentEnv := env.agentsEnv()
	lockPath := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		return rep, err
	}
	tasks, groups := planUpdates(lock, opts.Skills, &rep)
	if len(tasks) == 0 && len(rep.Results) == 0 && len(rep.Uncheckable) == 0 {
		rep.NothingToUpdate = true
		rep.Notes = append(rep.Notes, nothingToUpdateNotes()...)
		return rep, nil
	}
	defer removeUpdateTrees(groups)
	acquireUpdateGroups(groups, env, opts.Runner, &rep)
	changed := false
	for _, t := range tasks {
		if groups[t.group].err != "" {
			continue // reported once, as the group check failure
		}
		res, entryChanged := updateSkill(env, agentEnv, lock, t, groups[t.group])
		if entryChanged {
			changed = true
		}
		rep.Results = append(rep.Results, res)
	}
	if changed {
		if err := lock.Save(lockPath); err != nil {
			return rep, err
		}
		rep.LockPath = lockPath
	}
	return rep, nil
}

// chooseUpdateScope mirrors upstream's project half. It sets NothingToUpdate when the automatic
// scope finds no project skills, and returns a usage error for a global answer to the prompt.
func chooseUpdateScope(opts UpdateOptions, env Env, prompter Prompter, rep *UpdateReport) error {
	switch {
	case opts.Project:
		return nil
	case len(opts.Skills) > 0:
		rep.Notes = append(rep.Notes,
			"note: global scope is not supported yet; updating project skills only")
		return nil
	case opts.Yes || !env.IsTTY:
		if !hasProjectSkills(env) {
			rep.NothingToUpdate = true
			rep.Notes = append(rep.Notes, nothingToUpdateNotes()...)
		}
		return nil
	}
	if prompter == nil {
		return promptRequired(true)
	}
	answers, err := prompter.Ask(Questions{
		Steps:    []StepID{StepUpdateScope},
		Defaults: Answers{Scope: ScopeProject},
	})
	if errors.Is(err, ErrPromptRequired) {
		return promptRequired(true)
	}
	if err != nil {
		return err
	}
	if answers.Scope != ScopeProject {
		return fmt.Errorf("%w: global scope is not supported yet", ErrUsage)
	}
	return nil
}

// nothingToUpdateNotes is what a run with no project skills reports.
func nothingToUpdateNotes() []string {
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

// updateTask is one lock entry to check, with the group that acquires its source.
type updateTask struct {
	key   string
	entry lockfile.Entry
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

// planUpdates classifies the lock entries: uncheckable ones are reported, the rest are grouped by
// sourceUrl or source plus ref, in lock key order.
func planUpdates(lock *lockfile.File, names []string, rep *UpdateReport) ([]updateTask, []*updateGroup) {
	selected, unmatched := selectUpdateKeys(lock, names)
	for _, name := range unmatched {
		rep.Results = append(rep.Results, UpdateResult{
			Name: name, Status: UpdateFailed, Reason: "no installed skill matches this name",
		})
	}
	groupIndex := map[string]int{}
	var tasks []updateTask
	var groups []*updateGroup
	for _, key := range lockKeys(lock) {
		if len(names) > 0 && !selected[key] {
			continue
		}
		entry, _ := lock.Get(key)
		if entry.SourceType == "node_modules" {
			continue
		}
		if reason := uncheckableReason(entry); reason != "" {
			rep.Uncheckable = append(rep.Uncheckable, UncheckableSkill{
				Name: key, Reason: reason, Source: source.RedactCredentials(entrySource(entry)),
			})
			continue
		}
		raw := source.RedactCredentials(entrySource(entry))
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
func uncheckableReason(e lockfile.Entry) string {
	switch e.SourceType {
	case "local":
		return "Local path"
	case "github", "gitlab", "git", "azure":
	default:
		if e.SourceType == "" {
			return "No source type recorded"
		}
		return e.SourceType + " sources cannot be checked automatically"
	}
	if e.SkillPath == "" {
		return "No skill path recorded"
	}
	if entrySource(e) == "" {
		return "No source recorded"
	}
	return ""
}

// entrySource is the location a lock entry is fetched from: the fetch URL when recorded, the
// shorthand otherwise.
func entrySource(e lockfile.Entry) string {
	if e.SourceURL != "" {
		return e.SourceURL
	}
	return e.Source
}

// selectUpdateKeys resolves the optional name filter against the lock keys: an exact
// case-insensitive match first, then the normalized and sanitized forms. It returns the keys to
// update and the names that matched nothing.
func selectUpdateKeys(lock *lockfile.File, names []string) (map[string]bool, []string) {
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
func matchUpdateKeys(lock *lockfile.File, name string) []string {
	var exact []string
	for _, key := range lockKeys(lock) {
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
	for _, key := range lockKeys(lock) {
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
func updateSkill(env Env, agentEnv agents.Environment, lock *lockfile.File, t updateTask, g *updateGroup) (UpdateResult, bool) {
	installed := skill.SanitizeName(t.key)
	found, res, ok := locateUpdateSkill(g.found.Skills, updateRecordedName(t.entry, t.key), t.entry.SkillPath)
	if !ok {
		res.Name = t.key
		return res, false
	}
	locations := installedAgents(agentEnv, installed)
	digest, err := hash.Dir(found.Dir)
	if err != nil {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: err.Error()}, false
	}
	moved := normalizeSkillPath(found.SkillPath()) != normalizeSkillPath(t.entry.SkillPath)
	if !moved && len(locations) > 0 && t.entry.ComputedHash != "" && digest == t.entry.ComputedHash {
		return UpdateResult{Name: t.key, Status: UpdateCurrent}, backfillCommit(lock, t.entry, t.key, g.tree.Commit)
	}
	return reinstallSkill(env, agentEnv, lock, t, g, found, installed, locations)
}

// backfillCommit records the fetched HEAD of an up-to-date git skill, so a following check does not
// report the moved ref as an available update. It reports whether the lock entry changed.
func backfillCommit(lock *lockfile.File, entry lockfile.Entry, key, commit string) bool {
	if commit == "" || strings.EqualFold(commit, entry.Commit) {
		return false
	}
	entry.Commit = commit
	lock.Put(key, entry)
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
func reinstallSkill(env Env, agentEnv agents.Environment, lock *lockfile.File, t updateTask,
	g *updateGroup, found skill.Skill, installed string, locations []agents.Agent) (UpdateResult, bool) {
	rename := ""
	if skill.SanitizeName(found.Name) != installed {
		rename = installed
	}
	mode := updateInstallMode(t.entry, agentEnv, installed, locations)
	targets := locations
	if len(targets) == 0 {
		targets = universalTargets()
	}
	entries := install.Plan(env.WorkDir, []skill.Skill{found}, targets, install.PlanOptions{
		Env: agentEnv, Mode: mode, Rename: rename,
	})
	results := install.Apply(env.WorkDir, entries)
	if message, failed := firstFailure(results); failed {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: message}, false
	}
	loc, ok := installedPath(results, entries[0].Name)
	if !ok {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: "no location accepted the skill"}, false
	}
	digest, err := hashInstalled(loc)
	if err != nil {
		return UpdateResult{Name: t.key, Status: UpdateFailed, Reason: err.Error()}, false
	}
	updated := t.entry
	updated.ComputedHash = digest
	if g.tree.Commit != "" {
		updated.Commit = g.tree.Commit
	}
	updated.SkillPath = found.SkillPath()
	lock.Put(t.key, updated)
	return UpdateResult{Name: t.key, Status: UpdateUpdated}, true
}

// updateInstallMode keeps the shape the skill was installed with: the recorded installMode wins;
// otherwise the current locations decide, and an existing link is proof of symlink mode.
func updateInstallMode(entry lockfile.Entry, env agents.Environment, name string, locations []agents.Agent) install.Mode {
	if entry.InstallMode == "copy" {
		return install.ModeCopy
	}
	for _, a := range locations {
		if info, err := os.Lstat(agentSkillPath(a, env, name)); err == nil && info.Mode()&os.ModeSymlink != 0 {
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
func installedAgents(env agents.Environment, name string) []agents.Agent {
	var out []agents.Agent
	for _, a := range agents.All() {
		if pathExistsForRemoval(agentSkillPath(a, env, name)) {
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
func updateRecordedName(e lockfile.Entry, key string) string {
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
