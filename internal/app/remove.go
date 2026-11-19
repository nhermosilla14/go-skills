package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/fsutil"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// ErrNoMatch reports that a removal selection matched no installed skill and no lock source. The
// CLI maps it to exit code 1.
var ErrNoMatch = errors.New("no matching skills")

// RemoveOptions are the inputs of the remove use case.
type RemoveOptions struct {
	Skills []string // skill names, an exact lock source, or "*"; empty asks
	Agents []string // agent identifiers, or "*"; empty targets every agent that has the skill
	All    bool     // every lock-tracked skill; implies --agent '*' and -y
	Yes    bool     // remove without confirming
	Global bool     // remove from the global scope
}

// RemoveStatus is the outcome of removing one skill from one agent.
type RemoveStatus string

const (
	// RemoveRemoved means the skill directory or lock entry was removed.
	RemoveRemoved RemoveStatus = "removed"
	// RemoveSkipped means the target was left in place.
	RemoveSkipped RemoveStatus = "skipped"
	// RemoveFailed means the target could not be removed.
	RemoveFailed RemoveStatus = "failed"
)

// RemoveResult is the outcome of one skill for one agent. A canonical directory, shared by every
// universal agent, is reported once under the reserved universal label.
type RemoveResult struct {
	Skill   string
	Agent   string
	Path    string
	Status  RemoveStatus
	Message string
}

// RemoveReport is the outcome of a remove run, for the CLI to render.
type RemoveReport struct {
	Results  []RemoveResult // one per skill and agent, in skill then registry order
	Notes    []string       // kept canonical directories, lock entries and skipped untracked skills
	LockPath string         // set when the lock file was written
}

// Failed reports whether any target could not be removed.
func (r RemoveReport) Failed() bool {
	for _, res := range r.Results {
		if res.Status == RemoveFailed {
			return true
		}
	}
	return false
}

// Counts returns the number of removed, skipped and failed targets.
func (r RemoveReport) Counts() (removed, skipped, failed int) {
	for _, res := range r.Results {
		switch res.Status {
		case RemoveRemoved:
			removed++
		case RemoveSkipped:
			skipped++
		case RemoveFailed:
			failed++
		}
	}
	return removed, skipped, failed
}

// removeTarget is one skill selected for removal, whether or not a directory still exists.
type removeTarget struct {
	Name    string // lock key when tracked, otherwise the installed name
	DirName string // sanitized directory or flat file name
	LockKey string // empty for an untracked skill
	Source  string
	Path    string // installed path, for the selector hint
	Agents  []agents.Agent
}

func (t removeTarget) dedupeKey() string {
	if t.LockKey != "" {
		return "lock:" + t.LockKey
	}
	return "name:" + t.DirName
}

// Remove removes installed skills from their agent directories, and removes the canonical copy and
// the lock entry only when nothing else still uses them.
func Remove(opts RemoveOptions, env Env, prompters ...Prompter) (RemoveReport, error) {
	var prompter Prompter
	if len(prompters) > 0 {
		prompter = prompters[0]
	}
	if env.Host.ID != "" {
		// A host agent cannot answer a prompt: removals run non-interactively (upstream forces -y).
		opts.Yes = true
	}
	allMode, names, err := removeSelection(opts)
	if err != nil {
		return RemoveReport{}, err
	}
	agentEnv := env.agentsEnv()
	selected, err := removalAgents(opts.Agents)
	if err != nil {
		return RemoveReport{}, err
	}
	explicitFilter := !everyAgentRequested(opts.Agents)

	lock, err := loadRemoveLock(env, agentEnv, opts.Global)
	if err != nil {
		return RemoveReport{}, err
	}
	installed := scanInstalled(agentEnv, agents.All(), false, env.IncludeInternal, opts.Global)
	var matched []comparedSkill
	var missing []missingSkill
	if opts.Global {
		globalLock, _ := lock.globalFile()
		matched, missing = compareGlobalLock(globalLock, installed)
	} else {
		matched, missing = compareLock(lock.projectFile(), installed)
	}
	catalog := removeCatalog(matched, missing)

	report := RemoveReport{}
	if allMode {
		report.Notes = untrackedNotes(matched)
		if len(lock.keys()) == 0 {
			return report, fmt.Errorf("%w: no lock-tracked skills found to remove", ErrNoMatch)
		}
	}

	var targets []removeTarget
	switch {
	case allMode:
		targets = lockTargets(lock, catalog)
	case len(names) > 0:
		var unmatched []string
		targets, unmatched = resolveRemoveTargets(names, catalog, lock)
		if len(unmatched) > 0 {
			return report, fmt.Errorf("%w: no installed skill or lock source matches: %s",
				ErrNoMatch, strings.Join(unmatched, ", "))
		}
	default:
		choices := removeChoices(matched)
		if len(choices) == 0 {
			return report, fmt.Errorf("%w: no installed lock-tracked skills to remove", ErrNoMatch)
		}
		answers, askErr := askRemove(removeQuestions(choices, "Remove the selected skills?", opts.Yes), env, prompter)
		if askErr != nil {
			return report, askErr
		}
		targets = targetsByLockKey(catalog, answers.Skills)
	}
	if len(targets) == 0 {
		return report, fmt.Errorf("%w: no skills selected", ErrNoMatch)
	}

	if len(names) > 0 || allMode {
		if !opts.Yes {
			answers, askErr := askRemove(removeConfirmQuestion(removeConfirmMessage(targets)), env, prompter)
			if askErr != nil {
				return report, askErr
			}
			if !answers.Proceed {
				return report, ErrCancelled
			}
		}
	}

	universal := universalSelected(selected)
	return executeRemove(agentEnv, lock, targets, selected, universal, explicitFilter, opts.Global,
		len(names) > 0, report)
}

// removeLock is the lock of the scope, so removal code does not care which version it is.
type removeLock struct {
	project *lockfile.File
	global  *lockfile.GlobalFile
	path    string
}

// loadRemoveLock reads the selected scope's lock.
func loadRemoveLock(env Env, agentEnv agents.Environment, global bool) (removeLock, error) {
	if global {
		path := agents.GlobalLockPath(agentEnv)
		lock, err := lockfile.LoadGlobal(path)
		return removeLock{global: lock, path: path}, err
	}
	path := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(path)
	return removeLock{project: lock, path: path}, err
}

func (l removeLock) projectFile() *lockfile.File { return l.project }

func (l removeLock) globalFile() (*lockfile.GlobalFile, bool) { return l.global, l.global != nil }

// keys returns the lock keys in sorted order.
func (l removeLock) keys() []string {
	if l.global != nil {
		return globalLockKeys(l.global)
	}
	return lockKeys(l.project)
}

// source returns the recorded source of a key.
func (l removeLock) source(key string) (string, bool) {
	if l.global != nil {
		entry, ok := l.global.Get(key)
		return entry.Source, ok
	}
	entry, ok := l.project.Get(key)
	return entry.Source, ok
}

// subagents returns the Eve placement recorded for a key. The global lock never records one.
func (l removeLock) subagents(key string) []string {
	if l.global != nil {
		return nil
	}
	entry, ok := l.project.Get(key)
	if !ok {
		return nil
	}
	return entry.Subagents
}

// delete removes one key from the lock.
func (l removeLock) delete(key string) {
	if l.global != nil {
		l.global.Delete(key)
		return
	}
	l.project.Delete(key)
}

// save writes the lock.
func (l removeLock) save() error {
	if l.global != nil {
		return l.global.Save(l.path)
	}
	return l.project.Save(l.path)
}

// removeSelection normalises --all and --skill '*' and refuses to combine them with names.
func removeSelection(opts RemoveOptions) (bool, []string, error) {
	all := opts.All
	var names []string
	for _, name := range opts.Skills {
		if name == "*" {
			all = true
			continue
		}
		names = append(names, name)
	}
	if all && len(names) > 0 {
		return false, nil, fmt.Errorf("%w: cannot combine --all (or --skill '*') with specific skill names", ErrUsage)
	}
	return all, names, nil
}

// removalAgents resolves the agent filter. No filter targets every agent that has the skill.
func removalAgents(ids []string) ([]agents.Agent, error) {
	if len(ids) == 0 {
		return agents.All(), nil
	}
	selected, invalid := agents.Expand(ids)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("%w: invalid agents: %s (valid: %s)", ErrUsage,
			strings.Join(invalid, ", "), strings.Join(agents.IDs(), ", "))
	}
	return selected, nil
}

// everyAgentRequested reports whether the filter is absent or names '*'.
func everyAgentRequested(ids []string) bool {
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		if id == "*" {
			return true
		}
	}
	return false
}

// universalSelected reports whether the run removes from the canonical directory's agents.
func universalSelected(selected []agents.Agent) bool {
	for _, a := range selected {
		if a.Universal {
			return true
		}
	}
	return false
}

// removeCatalog lists every skill that can be selected, tracked or not. Tracked entries win, so a
// sanitized lock key and the folder it names collapse into one target.
func removeCatalog(matched []comparedSkill, missing []missingSkill) []removeTarget {
	index := map[string]int{}
	var out []removeTarget
	add := func(t removeTarget) {
		key := t.dedupeKey()
		if i, ok := index[key]; ok {
			out[i].Agents = mergeAgents(out[i].Agents, t.Agents)
			if out[i].Path == "" {
				out[i].Path = t.Path
			}
			return
		}
		index[key] = len(out)
		out = append(out, t)
	}
	for _, c := range matched {
		name := c.Name
		if c.LockKey != "" {
			name = c.LockKey
		}
		add(removeTarget{
			Name:    name,
			DirName: skill.SanitizeName(name),
			LockKey: c.LockKey,
			Source:  c.Ref.Source,
			Path:    c.Path,
			Agents:  c.Agents,
		})
	}
	for _, m := range missing {
		add(removeTarget{
			Name:    m.Key,
			DirName: skill.SanitizeName(m.Key),
			LockKey: m.Key,
			Source:  m.Ref.Source,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].LockKey < out[j].LockKey
	})
	return out
}

func mergeAgents(into, extra []agents.Agent) []agents.Agent {
	seen := map[string]bool{}
	for _, a := range into {
		seen[a.ID] = true
	}
	for _, a := range extra {
		if !seen[a.ID] {
			seen[a.ID] = true
			into = append(into, a)
		}
	}
	return into
}

// untrackedNotes reports the installed directories that --all leaves alone.
func untrackedNotes(matched []comparedSkill) []string {
	seen := map[string]bool{}
	var notes []string
	for _, c := range matched {
		if c.LockKey != "" || c.Path == "" {
			continue
		}
		name := skill.SanitizeName(c.Name)
		if seen[name] {
			continue
		}
		seen[name] = true
		notes = append(notes, fmt.Sprintf("skipped %s: not recorded in the lock file", c.Name))
	}
	return notes
}

// lockTargets returns one target per lock key, including stale entries with no directory.
func lockTargets(lock removeLock, catalog []removeTarget) []removeTarget {
	byKey := catalogByLockKey(catalog)
	var out []removeTarget
	for _, key := range lock.keys() {
		if t, ok := byKey[key]; ok {
			out = append(out, t)
			continue
		}
		source, _ := lock.source(key)
		out = append(out, removeTarget{
			Name:    key,
			DirName: skill.SanitizeName(key),
			LockKey: key,
			Source:  source,
		})
	}
	return out
}

func catalogByLockKey(catalog []removeTarget) map[string]removeTarget {
	out := make(map[string]removeTarget, len(catalog))
	for _, t := range catalog {
		if t.LockKey != "" {
			out[t.LockKey] = t
		}
	}
	return out
}

// targetsByLockKey resolves selector answers, which are lock keys.
func targetsByLockKey(catalog []removeTarget, keys []string) []removeTarget {
	byKey := catalogByLockKey(catalog)
	var out []removeTarget
	for _, key := range keys {
		if t, ok := byKey[key]; ok {
			out = append(out, t)
		}
	}
	return out
}

// resolveRemoveTargets matches every requested name against installed names, sanitized names and
// lock keys, then falls back to an exact lock source. It returns the names that matched nothing.
func resolveRemoveTargets(names []string, catalog []removeTarget, lock removeLock) ([]removeTarget, []string) {
	byKey := catalogByLockKey(catalog)
	seen := map[string]bool{}
	var out []removeTarget
	add := func(t removeTarget) {
		key := t.dedupeKey()
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	var unmatched []string
	for _, query := range names {
		matches := matchRemoveQuery(query, catalog)
		if len(matches) > 0 {
			for _, t := range matches {
				add(t)
			}
			continue
		}
		found := false
		for _, key := range lock.keys() {
			source, _ := lock.source(key)
			if source != query {
				continue
			}
			found = true
			if t, ok := byKey[key]; ok {
				add(t)
				continue
			}
			add(removeTarget{
				Name:    key,
				DirName: skill.SanitizeName(key),
				LockKey: key,
				Source:  source,
			})
		}
		if !found {
			unmatched = append(unmatched, query)
		}
	}
	return out, unmatched
}

// matchRemoveQuery follows the add matcher: display names case-insensitively first, then
// normalized and sanitized forms. Folder names and lock keys are matched too.
func matchRemoveQuery(query string, catalog []removeTarget) []removeTarget {
	var exact []removeTarget
	for _, t := range catalog {
		if removeNamesMatch(t, query, strings.EqualFold) {
			exact = append(exact, t)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	norm, sanitized := skill.NormalizeName(query), skill.SanitizeName(query)
	var normalized []removeTarget
	for _, t := range catalog {
		if matchesNormalized(t, norm, sanitized) {
			normalized = append(normalized, t)
		}
	}
	return normalized
}

// matchesNormalized compares the normalized and sanitized query with the same forms of a target.
func matchesNormalized(t removeTarget, norm, sanitized string) bool {
	names := []string{t.Name, t.LockKey}
	if t.Path != "" {
		names = append(names, filepath.Base(t.Path))
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if skill.NormalizeName(name) == norm || skill.SanitizeName(name) == sanitized {
			return true
		}
	}
	return false
}

// removeNamesMatch compares a query with the three names a target answers to.
func removeNamesMatch(t removeTarget, query string, eq func(a, b string) bool) bool {
	if eq(t.Name, query) || (t.LockKey != "" && eq(t.LockKey, query)) {
		return true
	}
	return t.Path != "" && eq(filepath.Base(t.Path), query)
}

// removeChoices lists the installed lock-tracked skills for the selector, sorted by lock key.
func removeChoices(matched []comparedSkill) []SkillChoice {
	var choices []SkillChoice
	for _, c := range matched {
		if c.LockKey == "" || c.Path == "" {
			continue
		}
		source := c.Ref.Source
		if source == "" {
			source = "untracked"
		}
		detail := "Source: " + source
		if names := displayNames(c.Agents); len(names) > 0 {
			detail += "  Agents: " + strings.Join(names, ", ")
		}
		choices = append(choices, SkillChoice{Name: c.LockKey, Description: detail, Path: c.Path})
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].Name < choices[j].Name })
	return choices
}

// removeQuestions is the single prompt session of an interactive removal: pick skills, then
// confirm unless -y was given.
func removeQuestions(choices []SkillChoice, message string, yes bool) Questions {
	steps := []StepID{StepRemoveSkills}
	if !yes {
		steps = append(steps, StepRemoveConfirm)
	}
	names := make([]string, 0, len(choices))
	for _, c := range choices {
		names = append(names, c.Name)
	}
	return Questions{
		Steps:    steps,
		Skills:   choices,
		Message:  message,
		Defaults: Answers{Skills: names, Proceed: true},
	}
}

// removeConfirmQuestion is the confirmation of a selection made from the command line.
func removeConfirmQuestion(message string) Questions {
	return Questions{
		Steps:    []StepID{StepRemoveConfirm},
		Message:  message,
		Defaults: Answers{Proceed: true},
	}
}

func removeConfirmMessage(targets []removeTarget) string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.Name)
	}
	if len(names) == 1 {
		return "Remove " + names[0] + "?"
	}
	return fmt.Sprintf("Remove %d skills: %s?", len(names), strings.Join(names, ", "))
}

// askRemove runs a removal prompt, turning a missing prompter into the documented diagnostic.
func askRemove(questions Questions, env Env, prompter Prompter) (Answers, error) {
	if len(questions.Steps) == 0 {
		return Answers{}, nil
	}
	if prompter == nil {
		return Answers{}, removePromptRequired(env.IsTTY)
	}
	answers, err := prompter.Ask(questions)
	if errors.Is(err, ErrPromptRequired) {
		return Answers{}, removePromptRequired(env.IsTTY)
	}
	return answers, err
}

// nonTTYRemovePrompt is the diagnostic of a removal that needs an answer but cannot ask.
const nonTTYRemovePrompt = "Interactive prompt required but stdin is not a TTY. Nothing was removed. " +
	"Pass skill names or --all to run non-interactively."

func removePromptRequired(isTTY bool) error {
	if isTTY {
		return &promptError{msg: "no interactive prompt is available; pass -y and name the skills, or use --all"}
	}
	return &promptError{msg: nonTTYRemovePrompt}
}

// executeRemove deletes the selected directories, then the canonical copy and lock entry when
// nothing else uses them. Failures are collected per target, never rolled back.
//
// A named removal refuses to delete a directory that lives inside a non-dot agent skills
// directory of the project (OpenClaw's skills/, AstrBot's data/skills, Eve's agent/skills) when
// the lock does not track it, or when its content is not the unmodified copy the lock describes:
// those directories double as source folders, so the lock decides what may be deleted from them.
// --all and --skill '*' keep their contract and never delete an untracked directory at all.
func executeRemove(agentEnv agents.Environment, lock removeLock,
	targets []removeTarget, selected []agents.Agent, universal, explicitFilter, global, named bool,
	report RemoveReport) (RemoveReport, error) {
	canonicalDir := scopeCanonicalDir(agentEnv, global)
	lockChanged := false
	for _, t := range targets {
		for _, a := range selected {
			if a.Universal && sameDir(a.BaseDir(agentEnv, global), canonicalDir) {
				continue
			}
			path := agentSkillPath(a, agentEnv, t.DirName, global)
			if _, err := os.Lstat(path); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					if explicitFilter {
						report.Results = append(report.Results, RemoveResult{
							Skill: t.Name, Agent: a.ID, Path: path, Status: RemoveSkipped, Message: "not present",
						})
					}
					continue
				}
				report.Results = append(report.Results, RemoveResult{
					Skill: t.Name, Agent: a.ID, Path: path, Status: RemoveFailed, Message: err.Error(),
				})
				continue
			}
			if named && !global && unmanagedNonDotDir(a, path, t, lock, agentEnv) {
				report.Results = append(report.Results, RemoveResult{
					Skill: t.Name, Agent: a.ID, Path: path, Status: RemoveFailed,
					Message: "refusing to delete it: it is not an unmodified copy recorded in the lock file",
				})
				continue
			}
			report.Results = append(report.Results, removeAtResult(t, a.ID, path,
				a.BaseDir(agentEnv, global)))
		}

		report.Results = append(report.Results,
			removeEveSubagents(agentEnv, lock, t, selected, explicitFilter)...)

		canonicalPath := filepath.Join(canonicalDir, t.DirName)
		canonicalExists := pathExistsForRemoval(canonicalPath)
		switch {
		case canonicalExists && universal:
			users := remainingUsers(agents.All(), agentEnv, t.DirName, canonicalPath, global)
			res := RemoveResult{Skill: t.Name, Agent: "universal", Path: canonicalPath}
			if len(users) > 0 {
				res.Status = RemoveSkipped
				res.Message = "still linked by " + strings.Join(users, ", ")
			} else {
				res = removeAtResult(t, "universal", canonicalPath, canonicalDir)
			}
			report.Results = append(report.Results, res)
		case canonicalExists:
			report.Notes = append(report.Notes,
				fmt.Sprintf("kept %s: still used by universal agents", canonicalPath))
		case universal && explicitFilter:
			report.Results = append(report.Results, RemoveResult{
				Skill: t.Name, Agent: "universal", Path: canonicalPath,
				Status: RemoveSkipped, Message: "not present",
			})
		}

		if t.LockKey == "" {
			continue
		}
		if !skillRemains(agents.All(), agentEnv, t.DirName, canonicalPath, global) {
			lock.delete(t.LockKey)
			lockChanged = true
			report.Notes = append(report.Notes, "removed lock entry "+t.LockKey)
		}
	}
	if lockChanged {
		if err := lock.save(); err != nil {
			return report, err
		}
		report.LockPath = lock.path
	}
	return report, nil
}

// removeEveSubagents deletes the Eve subagent files a lock entry records. The root agent file is
// handled by the agent loop, and subagent copies the entry does not record are left alone.
func removeEveSubagents(env agents.Environment, lock removeLock, t removeTarget,
	selected []agents.Agent, explicitFilter bool) []RemoveResult {
	if t.LockKey == "" || !targetsEve(selected) {
		return nil
	}
	root := filepath.Join(env.Cwd, "agent", "subagents")
	seen := map[string]bool{}
	var results []RemoveResult
	for _, sub := range lock.subagents(t.LockKey) {
		if sub == "" {
			continue
		}
		path := install.EveSubagentFile(env, sub, t.DirName)
		if seen[path] {
			continue
		}
		seen[path] = true
		if _, err := os.Lstat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				if explicitFilter {
					results = append(results, RemoveResult{
						Skill: t.Name, Agent: "eve", Path: path,
						Status: RemoveSkipped, Message: "not present",
					})
				}
				continue
			}
			results = append(results, RemoveResult{
				Skill: t.Name, Agent: "eve", Path: path,
				Status: RemoveFailed, Message: err.Error(),
			})
			continue
		}
		results = append(results, removeAtResult(t, "eve", path, root))
	}
	return results
}

// sameDir reports whether two paths name the same directory without touching the filesystem.
func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// agentSkillPath is where an install puts the skill for an agent. Eve consumes a flat file.
func agentSkillPath(a agents.Agent, env agents.Environment, dirName string, global bool) string {
	if a.ID == "eve" {
		return filepath.Join(a.BaseDir(env, global), dirName+".md")
	}
	return filepath.Join(a.BaseDir(env, global), dirName)
}

// remainingUsers lists the agents whose directory still holds the skill, ignoring the canonical
// directory itself, which every universal agent shares.
func remainingUsers(all []agents.Agent, env agents.Environment, dirName, canonicalPath string, global bool) []string {
	var users []string
	for _, a := range all {
		path := agentSkillPath(a, env, dirName, global)
		if filepath.Clean(path) == filepath.Clean(canonicalPath) {
			continue
		}
		if pathExistsForRemoval(path) {
			users = append(users, a.DisplayName)
		}
	}
	return users
}

// skillRemains reports whether any agent directory, legacy project skills directory or the
// canonical directory still has the skill.
func skillRemains(all []agents.Agent, env agents.Environment, dirName, canonicalPath string, global bool) bool {
	if pathExistsForRemoval(canonicalPath) {
		return true
	}
	for _, a := range all {
		if pathExistsForRemoval(agentSkillPath(a, env, dirName, global)) {
			return true
		}
	}
	if global {
		return false
	}
	for _, rel := range agents.ProjectSkillDirs() {
		if pathExistsForRemoval(filepath.Join(env.Cwd, filepath.FromSlash(rel), dirName)) {
			return true
		}
	}
	return false
}

// computedHash is the content hash the lock records for a key, if any.
func (l removeLock) computedHash(key string) (string, bool) {
	if l.global != nil {
		entry, ok := l.global.Get(key)
		return entry.SkillFolderHash, ok
	}
	entry, ok := l.project.Get(key)
	return entry.ComputedHash, ok
}

// unmanagedNonDotDir reports whether a named removal must keep a directory: it sits in a non-dot
// agent skills directory of the project and either the lock does not track it or its content is
// not the copy the lock describes. Symlinks and flat files are never guarded, because unlinking
// them cannot destroy a source folder.
func unmanagedNonDotDir(a agents.Agent, path string, t removeTarget, lock removeLock,
	env agents.Environment) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	if !nonDotProjectDir(env.Cwd, a.BaseDir(env, false)) {
		return false
	}
	if t.LockKey == "" {
		return true
	}
	want, ok := lock.computedHash(t.LockKey)
	if !ok || want == "" {
		return true
	}
	got, err := hashInstalled(path)
	return err != nil || !strings.EqualFold(got, want)
}

// nonDotProjectDir reports whether dir is a project directory whose first segment is not dotted:
// OpenClaw's skills/, AstrBot's data/skills and Eve's agent/skills double as source folders.
func nonDotProjectDir(project, dir string) bool {
	rel, err := filepath.Rel(project, dir)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	first := rel
	if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
		first = rel[:i]
	}
	return !strings.HasPrefix(first, ".")
}

// removeAtResult removes path below root and reports the outcome. Category directories are never
// removed.
func removeAtResult(t removeTarget, agentID, path, root string) RemoveResult {
	res := RemoveResult{Skill: t.Name, Agent: agentID, Path: path}
	if install.IsCategoryDir(filepath.Base(path)) {
		res.Status = RemoveSkipped
		res.Message = "category directory"
		return res
	}
	if err := removeWithin(root, path); err != nil {
		res.Status = RemoveFailed
		res.Message = err.Error()
		return res
	}
	res.Status = RemoveRemoved
	return res
}

// removeWithin deletes path only when it stays inside root. A symlink is unlinked, never followed,
// and a directory is checked after resolving its real path so a link cannot escape the root.
func removeWithin(root, path string) error {
	if filepath.Clean(root) == filepath.Clean(path) || !fsutil.Within(root, path) {
		return fmt.Errorf("refusing to remove %s: %w", path, fsutil.ErrOutsideRoot)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	realRoot, err := fsutil.RealPath(root)
	if err != nil {
		return err
	}
	realPath, err := fsutil.RealPath(path)
	if err != nil {
		return err
	}
	if filepath.Clean(realRoot) == filepath.Clean(realPath) || !fsutil.Within(realRoot, realPath) {
		return fmt.Errorf("refusing to remove %s: %w", path, fsutil.ErrOutsideRoot)
	}
	return os.RemoveAll(path)
}

// pathExistsForRemoval is the existence probe removal uses, including broken symlinks.
func pathExistsForRemoval(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
