package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
)

// Status is the outcome of one skill for one agent.
type Status string

const (
	Installed Status = "installed"
	Skipped   Status = "skipped"
	Failed    Status = "failed"
)

// Result is the outcome of one skill for one agent.
type Result struct {
	Skill   string // installed name
	Agent   string // agent identifier
	Path    string // absolute destination
	Mode    Mode   // how the skill actually reached the destination
	Status  Status
	Message string // why it was skipped or failed, or a fallback notice
}

// Apply executes the plan entry by entry and returns one result per skill and agent. A failure
// is reported, not rolled back.
func Apply(projectDir string, entries []Entry) []Result {
	var results []Result
	for _, e := range entries {
		if e.Mode == ModeCopy {
			for _, t := range e.Targets {
				results = append(results, copyTarget(projectDir, e, t))
			}
			continue
		}
		results = append(results, applySymlinkEntry(projectDir, e)...)
	}
	return results
}

func applySymlinkEntry(projectDir string, e Entry) []Result {
	results := make([]Result, 0, len(e.Targets))
	if err := replaceDir(projectDir, e.Skill.Dir, e.Canonical); err != nil {
		for _, t := range e.Targets {
			results = append(results, failure(newResult(e, t, ModeSymlink), err))
		}
		return results
	}
	for _, t := range e.Targets {
		results = append(results, linkTarget(projectDir, e, t))
	}
	return results
}

func newResult(e Entry, t Target, mode Mode) Result {
	return Result{Skill: e.Name, Agent: t.Agent.ID, Path: t.Path, Mode: mode}
}

// failure marks res as skipped when the source and destination overlap, and failed otherwise.
func failure(res Result, err error) Result {
	res.Message = err.Error()
	res.Status = Failed
	if errors.Is(err, ErrOverlap) {
		res.Status = Skipped
	}
	return res
}

func copyTarget(projectDir string, e Entry, t Target) Result {
	res := newResult(e, t, ModeCopy)
	if err := replaceDir(projectDir, e.Skill.Dir, t.Path); err != nil {
		return failure(res, err)
	}
	res.Status = Installed
	return res
}

// linkTarget points the agent directory at the canonical copy. When links are not available it
// copies the skill instead and says so in the message.
func linkTarget(projectDir string, e Entry, t Target) Result {
	res := newResult(e, t, ModeSymlink)
	if filepath.Clean(t.Path) == filepath.Clean(e.Canonical) {
		res.Status = Installed
		return res
	}
	same, err := samePlace(t.Path, e.Canonical)
	if err != nil {
		return failure(res, err)
	}
	if same {
		res.Status = Installed
		return res
	}
	overlap, err := overlaps(e.Skill.Dir, t.Path)
	if err != nil {
		return failure(res, err)
	}
	if overlap {
		return failure(res, fmt.Errorf("%s: %w", t.Path, ErrOverlap))
	}
	if err := linkOrCopy(projectDir, e, t, &res); err != nil {
		return failure(res, err)
	}
	res.Status = Installed
	return res
}

// linkOrCopy replaces t.Path with a relative symlink to the canonical copy, falling back to a copy
// when the platform or the filesystem refuses links. res.Mode and res.Message record the fallback.
func linkOrCopy(projectDir string, e Entry, t Target, res *Result) error {
	if err := fsutil.EnsureDir(filepath.Dir(t.Path)); err != nil {
		return err
	}
	if err := fsutil.RemoveAllWithin(projectDir, t.Path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		err := createRelativeLink(t.Path, e.Canonical)
		if err == nil {
			return nil
		}
		res.Message = fmt.Sprintf("symlink failed (%v); copied instead", err)
	} else {
		res.Message = "symlinks are not used on Windows; copied instead"
	}
	res.Mode = ModeCopy
	return replaceDir(projectDir, e.Skill.Dir, t.Path)
}

// createRelativeLink links link to target with a path relative to the link's real parent, so the
// link still resolves after the project is moved.
func createRelativeLink(link, target string) error {
	realParent, err := fsutil.RealPath(filepath.Dir(link))
	if err != nil {
		return err
	}
	realTarget, err := fsutil.RealPath(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realParent, realTarget)
	if err != nil {
		return err
	}
	return os.Symlink(rel, link)
}

// samePlace reports whether two paths resolve to the same real location.
func samePlace(a, b string) (bool, error) {
	ra, err := fsutil.RealPath(a)
	if err != nil {
		return false, err
	}
	rb, err := fsutil.RealPath(b)
	if err != nil {
		return false, err
	}
	return ra == rb, nil
}
