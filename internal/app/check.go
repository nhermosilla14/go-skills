package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// CheckOptions are the inputs of the check use case.
type CheckOptions struct {
	Skills []string      // skill names to check; empty checks every tracked skill
	Global bool          // check the global scope
	Runner source.Runner // git runner; nil uses PATH, tests pass a fake
}

// CheckReport is the outcome of a check run.
type CheckReport struct {
	Scope    string
	Updates  []string // skills with a newer commit upstream
	Modified []string // skills whose files differ from the lock
	Failures []string // sources or names that could not be checked
}

// Failed reports whether the run found anything that must make the command exit non-zero.
func (r CheckReport) Failed() bool {
	return len(r.Updates) > 0 || len(r.Modified) > 0 || len(r.Failures) > 0
}

// Check reports available updates and local changes without writing anything.
func Check(ctx context.Context, opts CheckOptions, env Env) (CheckReport, error) {
	agentEnv := env.agentsEnv()
	var (
		lock      *lockfile.File
		global    *lockfile.GlobalFile
		installed []installedSkill
		keys      []string
		matched   []comparedSkill
	)
	report := CheckReport{Scope: scopeName(opts.Global)}
	if opts.Global {
		globalLock, err := lockfile.LoadGlobal(agents.GlobalLockPath(agentEnv))
		if err != nil {
			return CheckReport{}, err
		}
		global = globalLock
		installed = scanInstalled(agentEnv, agents.All(), false, env.IncludeInternal, true)
		matched, _ = compareGlobalLock(global, installed)
		keys = globalLockKeys(global)
	} else {
		projectLock, err := lockfile.Load(filepath.Join(env.WorkDir, lockfile.FileName))
		if err != nil {
			return CheckReport{}, err
		}
		lock = projectLock
		installed = scanInstalled(agentEnv, agents.All(), false, env.IncludeInternal, false)
		matched, _ = compareLock(lock, installed)
		keys = lockKeys(lock)
	}
	byKey := map[string]comparedSkill{}
	for _, c := range matched {
		if c.LockKey == "" {
			continue
		}
		if _, ok := byKey[c.LockKey]; !ok {
			byKey[c.LockKey] = c
		}
	}

	selected, unmatched := selectCheckKeys(keys, opts.Skills)
	for _, name := range unmatched {
		report.Failures = append(report.Failures,
			fmt.Sprintf("%s: no installed skill matches this name", name))
	}
	for _, key := range keys {
		if len(opts.Skills) > 0 && !selected[key] {
			continue
		}
		ref, status, ok := checkTarget(agentEnv, lock, key, byKey)
		if !ok {
			continue // in the lock but not on disk: nothing installed to check
		}
		if status == StatusModified {
			report.Modified = append(report.Modified, key)
		}
		update, err := checkUpdate(ctx, key, ref, opts.Runner, env.IncludeInternal, agentEnv, opts.Global)
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		if update {
			report.Updates = append(report.Updates, key)
		}
	}
	return report, nil
}

// checkTarget resolves the recorded state of one lock key. A project entry that records an Eve
// placement is compared through the flat files the lock lists, so a directory the scan found but
// the placement did not write cannot hide a changed or missing copy.
func checkTarget(agentEnv agents.Environment, lock *lockfile.File, key string,
	byKey map[string]comparedSkill) (lockRef, Status, bool) {
	c, installed := byKey[key]
	if lock != nil {
		entry, tracked := lock.Get(key)
		if tracked && evePlaced(entry.Subagents) && !dirExists(scopeCanonicalDir(agentEnv, false), key) {
			status, present := eveFlatStatus(agentEnv, skill.SanitizeName(key), entry)
			if !present {
				return lockRef{}, StatusMissing, false
			}
			return refOf(entry), status, true
		}
	}
	if !installed {
		return lockRef{}, StatusMissing, false
	}
	return c.Ref, c.Status, true
}

// eveFlatStatus hashes the recorded Eve flat files. present is false when none of them is on disk;
// otherwise every recorded file must exist and the first one must still hash to the lock.
func eveFlatStatus(env agents.Environment, name string, entry lockfile.Entry) (Status, bool) {
	files := eveFlatFiles(env, name, entry.Subagents)
	present, missing := false, false
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			missing = true
			continue
		}
		present = true
	}
	if !present {
		return StatusMissing, false
	}
	if missing || entry.ComputedHash == "" {
		return StatusModified, true
	}
	digest, err := hash.SingleFile(files[0])
	if err != nil || !strings.EqualFold(digest, entry.ComputedHash) {
		return StatusModified, true
	}
	return StatusTracked, true
}

// dirExists reports whether the lock key names a skill directory that is on disk.
func dirExists(canonicalDir, key string) bool {
	_, err := os.Stat(filepath.Join(canonicalDir, skill.SanitizeName(key)))
	return err == nil
}

// refOf is the fetch information a project lock entry carries.
func refOf(entry lockfile.Entry) lockRef {
	return lockRef{
		Source:          entry.Source,
		SourceURL:       entry.SourceURL,
		SourceType:      entry.SourceType,
		SkillName:       entry.SkillName,
		Ref:             entry.Ref,
		Commit:          entry.Commit,
		SkillPath:       entry.SkillPath,
		SourceBaseURL:   entry.SourceBaseURL,
		WellKnownDigest: entry.WellKnownDigest,
		Hash:            entry.ComputedHash,
	}
}

// selectCheckKeys resolves the optional name filter, reporting the names that match no lock key.
func selectCheckKeys(keys []string, names []string) (map[string]bool, []string) {
	if len(names) == 0 {
		return nil, nil
	}
	selected := map[string]bool{}
	var unmatched []string
	for _, name := range names {
		found := false
		for _, key := range keys {
			if strings.EqualFold(key, name) {
				selected[key] = true
				found = true
			}
		}
		if !found {
			unmatched = append(unmatched, name)
		}
	}
	return selected, unmatched
}

// checkUpdate reports whether the recorded ref moved upstream, or a download's content changed.
// Local sources never have updates, and a source type this build cannot fetch is reported as
// uncheckable.
func checkUpdate(ctx context.Context, key string, entry lockRef, runner source.Runner,
	includeInternal bool, agentEnv agents.Environment, global bool) (bool, error) {
	switch entry.SourceType {
	case "local":
		return false, nil
	case "github", "gitlab", "git":
	case "download":
		return checkDownloadUpdate(ctx, key, entry, includeInternal)
	case "well-known":
		return checkWellKnownUpdate(ctx, key, entry, agentEnv, global)
	case "":
		return false, fmt.Errorf("the lock entry has no source type")
	default:
		return false, fmt.Errorf("cannot check a %s source", entry.SourceType)
	}
	raw := entry.Source
	if entry.SourceURL != "" {
		raw = entry.SourceURL
	}
	src, err := source.Parse(raw)
	if err != nil {
		return false, err
	}
	src.Ref = entry.Ref
	head, err := source.RemoteHead(ctx, src, runner)
	if err != nil {
		return false, err
	}
	return entry.Commit == "" || !strings.EqualFold(head, entry.Commit), nil
}

// checkDownloadUpdate re-fetches a download and compares the content of the recorded skill with the
// hash in the lock. A changed body is an available update.
func checkDownloadUpdate(ctx context.Context, key string, entry lockRef, includeInternal bool) (bool, error) {
	raw := entry.Source
	if entry.SourceURL != "" {
		raw = entry.SourceURL
	}
	if raw == "" {
		return false, errors.New("no source recorded")
	}
	if entry.Hash == "" {
		return false, nil // the installed drift is a Modified finding; there is nothing to compare
	}
	src, err := source.Parse(raw)
	if err != nil {
		return false, err
	}
	dir, err := os.MkdirTemp("", "skills-check-*")
	if err != nil {
		return false, fmt.Errorf("creating a temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tree, err := acquireUpdateTree(ctx, src, dir, nil)
	if err != nil {
		return false, err
	}
	found, err := skill.Discover(tree.Dir, skill.Options{IncludeInternal: includeInternal, FullDepth: true})
	if err != nil {
		return false, err
	}
	name := updateRecordedName(updateRecord{SkillName: entry.SkillName}, key)
	fetched, located, ok := locateUpdateSkill(found.Skills, name, entry.SkillPath)
	if !ok {
		return false, errors.New(located.Reason)
	}
	digest, err := hash.Dir(fetched.Dir)
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(digest, entry.Hash), nil
}

// checkWellKnownUpdate re-probes a provider, re-verifies its digest and compares the fetched
// content with the lock. An unreachable provider or a digest mismatch is a failure.
func checkWellKnownUpdate(ctx context.Context, key string, entry lockRef,
	agentEnv agents.Environment, global bool) (bool, error) {
	raw := entry.SourceBaseURL
	if raw == "" {
		raw = entry.Source
	}
	if raw == "" {
		return false, errors.New("no source recorded")
	}
	idx, err := source.ProbeWellKnown(ctx, raw, true)
	if err != nil {
		return false, err
	}
	if idx == nil {
		return false, fmt.Errorf("no well-known index found at %s", raw)
	}
	dir, err := os.MkdirTemp("", "skills-check-*")
	if err != nil {
		return false, fmt.Errorf("creating a temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	name := updateRecordedName(updateRecord{SkillName: entry.SkillName}, key)
	tree, err := idx.Fetch(ctx, dir, []string{name})
	if err != nil {
		return false, err
	}
	fetched, located, ok := locateUpdateSkill(tree.WellKnown.Skills(), name, entry.SkillPath)
	if !ok {
		return false, errors.New(located.Reason)
	}
	digest, err := hash.Dir(fetched.Dir)
	if err != nil {
		return false, err
	}
	recorded := entry.Hash
	if recorded == "" {
		recorded, err = installedContentHash(agentEnv, key, global)
		if err != nil {
			return false, nil // a missing copy is reported as Missing, not as an update
		}
	}
	return !strings.EqualFold(digest, recorded), nil
}

// installedContentHash is the content hash of a skill's first installed copy. It is what check
// compares a well-known global entry against, whose folder hash is deliberately empty.
func installedContentHash(env agents.Environment, key string, global bool) (string, error) {
	installed := skill.SanitizeName(key)
	for _, a := range installedAgents(env, installed, global) {
		return hashInstalled(agentSkillPath(a, env, installed, global))
	}
	return "", errors.New("not installed")
}
