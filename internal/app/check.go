package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// CheckOptions are the inputs of the check use case.
type CheckOptions struct {
	Skills []string      // skill names to check; empty checks every tracked skill
	Global bool          // global scope; not supported yet
	Runner source.Runner // git runner; nil uses PATH, tests pass a fake
}

// CheckReport is the outcome of a check run.
type CheckReport struct {
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
	if opts.Global {
		return CheckReport{}, fmt.Errorf("%w: global scope is not supported yet", ErrUsage)
	}
	agentEnv := env.agentsEnv()
	lockPath := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		return CheckReport{}, err
	}
	installed := scanInstalled(agentEnv, agents.All(), false, env.IncludeInternal)
	matched, _ := compareLock(lock, installed)
	byKey := map[string]comparedSkill{}
	for _, c := range matched {
		if c.LockKey == "" {
			continue
		}
		if _, ok := byKey[c.LockKey]; !ok {
			byKey[c.LockKey] = c
		}
	}

	selected, unmatched := selectCheckKeys(lock, opts.Skills)
	report := CheckReport{}
	for _, name := range unmatched {
		report.Failures = append(report.Failures,
			fmt.Sprintf("%s: no installed skill matches this name", name))
	}
	for _, key := range lockKeys(lock) {
		if len(opts.Skills) > 0 && !selected[key] {
			continue
		}
		c, ok := byKey[key]
		if !ok {
			continue // in the lock but not on disk: nothing installed to check
		}
		if c.Status == StatusModified {
			report.Modified = append(report.Modified, key)
		}
		update, err := checkUpdate(ctx, c, opts.Runner)
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

// selectCheckKeys resolves the optional name filter, reporting the names that match no lock key.
func selectCheckKeys(lock *lockfile.File, names []string) (map[string]bool, []string) {
	if len(names) == 0 {
		return nil, nil
	}
	selected := map[string]bool{}
	var unmatched []string
	for _, name := range names {
		found := false
		for key := range lock.Skills {
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

// checkUpdate reports whether the recorded ref moved upstream. Local sources never have updates,
// and a source type this build cannot fetch is reported as uncheckable.
func checkUpdate(ctx context.Context, c comparedSkill, runner source.Runner) (bool, error) {
	switch c.Entry.SourceType {
	case "local":
		return false, nil
	case "github", "gitlab", "git":
	case "":
		return false, fmt.Errorf("the lock entry has no source type")
	default:
		return false, fmt.Errorf("cannot check a %s source", c.Entry.SourceType)
	}
	raw := c.Entry.Source
	if c.Entry.SourceURL != "" {
		raw = c.Entry.SourceURL
	}
	src, err := source.Parse(raw)
	if err != nil {
		return false, err
	}
	src.Ref = c.Entry.Ref
	head, err := source.RemoteHead(ctx, src, runner)
	if err != nil {
		return false, err
	}
	return c.Entry.Commit == "" || !strings.EqualFold(head, c.Entry.Commit), nil
}
