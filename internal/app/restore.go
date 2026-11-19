package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// RestoreOptions are the inputs of the restore use case, as parsed from the command line.
type RestoreOptions struct {
	Agents []string      // agent identifiers, or "*"; empty follows the fallback chain
	Force  bool          // reinstall even when the installed content matches the lock
	DryRun bool          // report the plan and write nothing
	Runner source.Runner // git runner; nil uses PATH, tests pass a fake
}

// RestoreStatus is the outcome of one locked skill.
type RestoreStatus string

const (
	// RestoreRestored means the skill was reinstalled from its pinned source.
	RestoreRestored RestoreStatus = "restored"
	// RestoreRelinked means the content was correct and only agent links were repaired.
	RestoreRelinked RestoreStatus = "relinked"
	// RestoreSkipped means the skill was left alone on purpose.
	RestoreSkipped RestoreStatus = "skipped"
	// RestoreFailed means fetching or installing the skill failed.
	RestoreFailed RestoreStatus = "failed"
)

// RestoreResult is the outcome of one locked skill.
type RestoreResult struct {
	Name   string
	Status RestoreStatus
	Reason string
}

// RestoreCheck is the outcome of fetching one source group.
type RestoreCheck struct {
	Source string // sourceUrl or source, as recorded in the lock, with credentials removed
	Err    string // empty when the group was fetched
}

// UnrestorableSkill is a lock entry this build cannot rebuild, with why.
type UnrestorableSkill struct {
	Name       string
	SourceType string
	Reason     string
}

// RestoreReport is the outcome of a restore run, for the CLI to render.
type RestoreReport struct {
	Results      []RestoreResult
	Checks       []RestoreCheck
	Unrestorable []UnrestorableSkill
	DryRun       bool
	LockPath     string // set when the lock file was written
}

// Failed reports whether a source or a skill failed, which makes the command exit non-zero.
func (r RestoreReport) Failed() bool {
	return r.FailedCount() > 0
}

// FailedCount counts the failed units: one per source that could not be fetched and one per skill
// that could not be restored.
func (r RestoreReport) FailedCount() int {
	count := 0
	for _, check := range r.Checks {
		if check.Err != "" {
			count++
		}
	}
	for _, res := range r.Results {
		if res.Status == RestoreFailed {
			count++
		}
	}
	return count
}

// restoreTask is one lock entry to restore, with the group that acquires its source.
type restoreTask struct {
	key   string
	entry lockfile.Entry
	mode  install.Mode
	group int
}

// restoreGroup is one fetch URL, pinned commit and install mode, acquired once for every entry it
// tracks.
type restoreGroup struct {
	source   string // fetch URL as reported, with credentials removed
	fetchURL string // fetch URL as recorded in the lock
	commit   string
	dir      string
	found    skill.Result
	err      string
}

// Restore rebuilds the project from skills-lock.json without prompting. Git entries are fetched at
// their recorded commit, grouped by source, commit and install mode so one checkout serves a group.
// Entries whose installed content already matches computedHash only get their agent links repaired;
// the rest are reinstalled with the recorded mode. The lock is written only when a hash was filled
// in or corrected, and never in a dry run.
func Restore(opts RestoreOptions, env Env) (RestoreReport, error) {
	rep := RestoreReport{DryRun: opts.DryRun}
	agentEnv := env.agentsEnv()
	targets, err := restoreAgents(opts.Agents, agentEnv)
	if err != nil {
		return rep, err
	}
	lockPath := filepath.Join(env.WorkDir, lockfile.FileName)
	if _, err := os.Stat(lockPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return rep, fmt.Errorf("no %s found in %s; install skills with skills add <source>",
				lockfile.FileName, env.WorkDir)
		}
		return rep, err
	}
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		return rep, err
	}
	if len(lock.Skills) == 0 {
		return rep, emptyLockError{file: lockfile.FileName}
	}
	tasks, groups := planRestore(lock, &rep)
	defer removeRestoreTrees(groups)
	acquireRestoreGroups(groups, env, opts.Runner, &rep)
	changed := false
	for _, task := range tasks {
		if groups[task.group].err != "" {
			continue // reported once, as the group check failure
		}
		res, entryChanged := restoreSkill(env, agentEnv, targets, lock, task, groups[task.group], opts)
		if entryChanged {
			changed = true
		}
		rep.Results = append(rep.Results, res)
	}
	if changed && !opts.DryRun {
		if err := lock.Save(lockPath); err != nil {
			return rep, err
		}
		rep.LockPath = lockPath
	}
	return rep, nil
}

// emptyLockError is the documented diagnostic of a lock with no skills. It is a type because the
// message keeps the capital letter of the upstream text.
type emptyLockError struct {
	file string
}

func (e emptyLockError) Error() string {
	return "No project skills found in " + e.file
}

// restoreAgents resolves the link targets: -a first, then the last selection saved by a global add,
// then the agents detected in the project, then none, which fills the canonical directory only.
func restoreAgents(ids []string, env agents.Environment) ([]agents.Agent, error) {
	if len(ids) > 0 {
		selected, invalid := agents.Expand(ids)
		if len(invalid) > 0 {
			return nil, fmt.Errorf("%w: invalid agents: %s (valid: %s)", ErrUsage,
				strings.Join(invalid, ", "), strings.Join(agents.IDs(), ", "))
		}
		return selected, nil
	}
	if saved := savedRestoreAgents(env); len(saved) > 0 {
		if selected, _ := agents.Expand(saved); len(selected) > 0 {
			return selected, nil
		}
	}
	return agents.Detect(env), nil
}

// savedRestoreAgents reads the last agent selection from the global lock. The lock is only ever
// read here: restore never writes it, and a malformed one simply yields no saved selection.
func savedRestoreAgents(env agents.Environment) []string {
	lock, err := lockfile.LoadGlobal(agents.GlobalLockPath(env))
	if err != nil {
		return nil
	}
	return lock.LastSelectedAgents
}

// planRestore classifies the lock entries in key order: unsupported source types are reported, and
// the rest are grouped by fetch URL, pinned commit and install mode.
func planRestore(lock *lockfile.File, rep *RestoreReport) ([]restoreTask, []*restoreGroup) {
	groupIndex := map[string]int{}
	var tasks []restoreTask
	var groups []*restoreGroup
	for _, key := range lockKeys(lock) {
		entry, _ := lock.Get(key)
		if reason := unrestorableReason(entry); reason != "" {
			rep.Unrestorable = append(rep.Unrestorable, UnrestorableSkill{
				Name: key, SourceType: entry.SourceType, Reason: reason,
			})
			continue
		}
		fetch := restoreFetchSource(entry)
		if fetch == "" {
			rep.Unrestorable = append(rep.Unrestorable, UnrestorableSkill{
				Name: key, SourceType: entry.SourceType, Reason: "No source recorded",
			})
			continue
		}
		mode := install.ModeSymlink
		if entry.InstallMode == "copy" {
			mode = install.ModeCopy
		}
		groupKey := fetch + "\x00" + entry.Commit + "\x00" + string(mode)
		index, ok := groupIndex[groupKey]
		if !ok {
			index = len(groups)
			groupIndex[groupKey] = index
			groups = append(groups, &restoreGroup{
				source: source.RedactCredentials(fetch), fetchURL: fetch,
				commit: entry.Commit,
			})
		}
		tasks = append(tasks, restoreTask{key: key, entry: entry, mode: mode, group: index})
	}
	return tasks, groups
}

// unrestorableReason explains why a lock entry cannot be rebuilt from its source, or returns "".
func unrestorableReason(e lockfile.Entry) string {
	switch e.SourceType {
	case "github", "gitlab", "git", "azure":
		return ""
	case "local":
		return "Local paths are not restored from the lock; install them with skills add <source>"
	case "download":
		return "Download sources cannot be restored automatically"
	case "well-known":
		return "Well-known skills cannot be restored automatically"
	case "node_modules":
		return "node_modules skills come from syncing the package, not from the lock"
	case "":
		return "No source type recorded"
	default:
		return e.SourceType + " sources cannot be restored automatically"
	}
}

// restoreFetchSource is the location a lock entry is fetched from: the recorded fetch URL when
// present, the shorthand otherwise.
func restoreFetchSource(e lockfile.Entry) string {
	if e.SourceURL != "" {
		return e.SourceURL
	}
	return e.Source
}

// acquireRestoreGroups fetches every group once, recording a check line per group. A group that
// cannot be fetched does not stop the others.
func acquireRestoreGroups(groups []*restoreGroup, env Env, runner source.Runner, rep *RestoreReport) {
	for _, g := range groups {
		g.err = fetchRestoreGroup(g, env, runner)
		rep.Checks = append(rep.Checks, RestoreCheck{Source: g.source, Err: g.err})
	}
}

// fetchRestoreGroup clones the group source at its pinned commit and discovers its skills. The
// temporary tree is kept until the run ends so every entry of the group shares it.
func fetchRestoreGroup(g *restoreGroup, env Env, runner source.Runner) string {
	src, err := source.Parse(g.fetchURL)
	if err != nil {
		return err.Error()
	}
	if g.commit != "" {
		src.Ref = g.commit
	}
	dir, err := os.MkdirTemp("", "skills-restore-*")
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
	g.found = found
	return ""
}

// removeRestoreTrees deletes the temporary checkouts of a run.
func removeRestoreTrees(groups []*restoreGroup) {
	for _, g := range groups {
		if g.dir != "" {
			_ = os.RemoveAll(g.dir)
		}
	}
}

// restoreSkill restores one lock entry: it skips the copy when the canonical content matches the
// lock and only repairs links, and reinstalls otherwise. It reports whether the lock entry changed.
func restoreSkill(env Env, agentEnv agents.Environment, targets []agents.Agent, lock *lockfile.File,
	t restoreTask, g *restoreGroup, opts RestoreOptions) (RestoreResult, bool) {
	found, located, ok := locateUpdateSkill(g.found.Skills,
		updateRecordedName(updateRecord{SkillName: t.entry.SkillName}, t.key), t.entry.SkillPath)
	if !ok {
		return RestoreResult{Name: t.key, Status: RestoreFailed, Reason: located.Reason}, false
	}
	installed := skill.SanitizeName(t.key)
	if t.mode == install.ModeSymlink && !opts.Force && t.entry.ComputedHash != "" {
		canonical := filepath.Join(scopeCanonicalDir(agentEnv, false), installed)
		if digest, err := hash.Dir(canonical); err == nil &&
			strings.EqualFold(digest, t.entry.ComputedHash) {
			return repairRestoreLinks(env, agentEnv, targets, t, found, installed, opts)
		}
	}
	if opts.DryRun {
		return RestoreResult{Name: t.key, Status: RestoreRestored}, false
	}
	rename := restoreRename(found, installed)
	entries := install.Plan(env.WorkDir, []skill.Skill{found}, restoreTargets(targets),
		install.PlanOptions{Env: agentEnv, Mode: t.mode, Rename: rename})
	results := install.Apply(env.WorkDir, entries)
	if message, failed := firstFailure(results); failed {
		return RestoreResult{Name: t.key, Status: RestoreFailed, Reason: message}, false
	}
	loc, ok := installedPath(results, entries[0].Name)
	if !ok {
		return RestoreResult{Name: t.key, Status: RestoreFailed,
			Reason: "no location accepted the skill"}, false
	}
	digest, err := hashInstalled(loc)
	if err != nil {
		return RestoreResult{Name: t.key, Status: RestoreFailed,
			Reason: "hashing the install: " + err.Error()}, false
	}
	if digest == t.entry.ComputedHash {
		return RestoreResult{Name: t.key, Status: RestoreRestored}, false
	}
	t.entry.ComputedHash = digest
	lock.Put(t.key, t.entry)
	return RestoreResult{Name: t.key, Status: RestoreRestored}, true
}

// repairRestoreLinks points missing agent targets at the canonical copy of a skill whose content
// already matches the lock. Nothing is copied over the canonical directory.
func repairRestoreLinks(env Env, agentEnv agents.Environment, targets []agents.Agent, t restoreTask,
	found skill.Skill, installed string, opts RestoreOptions) (RestoreResult, bool) {
	entries := install.Plan(env.WorkDir, []skill.Skill{found}, restoreTargets(targets),
		install.PlanOptions{Env: agentEnv, Mode: install.ModeSymlink,
			Rename: restoreRename(found, installed)})
	if linksPresent(entries[0]) {
		return RestoreResult{Name: t.key, Status: RestoreSkipped}, false
	}
	if opts.DryRun {
		return RestoreResult{Name: t.key, Status: RestoreRelinked}, false
	}
	results := install.Relink(env.WorkDir, entries)
	if message, failed := firstFailure(results); failed {
		return RestoreResult{Name: t.key, Status: RestoreFailed, Reason: message}, false
	}
	for _, res := range results {
		if res.Status == install.Installed {
			return RestoreResult{Name: t.key, Status: RestoreRelinked}, false
		}
	}
	return RestoreResult{Name: t.key, Status: RestoreSkipped, Reason: firstMessage(results)}, false
}

// restoreRename is the name written into SKILL.md: the installed key when it differs from the
// upstream name, so renamed skills stay renamed.
func restoreRename(found skill.Skill, installed string) string {
	if skill.SanitizeName(found.Name) == installed {
		return ""
	}
	return installed
}

// restoreTargets is the set of link targets; with none selected the canonical directory is the only
// destination.
func restoreTargets(targets []agents.Agent) []agents.Agent {
	if len(targets) == 0 {
		return universalTargets()
	}
	return targets
}

// linksPresent reports whether every agent target already holds the skill.
func linksPresent(e install.Entry) bool {
	for _, t := range e.Targets {
		if !pathExistsForRemoval(t.Path) {
			return false
		}
	}
	return true
}

// firstMessage returns the first non-empty message of a result list.
func firstMessage(results []install.Result) string {
	for _, res := range results {
		if res.Message != "" {
			return res.Message
		}
	}
	return ""
}
