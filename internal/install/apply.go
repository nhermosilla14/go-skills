package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
	"github.com/nhermosilla14/go-skills/internal/skill"
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

// linker creates a directory link from link to target. POSIX uses a relative symlink and Windows
// an absolute junction; a failure makes the caller copy instead.
type linker func(link, target string) error

// Apply executes the plan entry by entry and returns one result per skill and agent. Targets that
// share a directory are written once and reported once per agent. A failure is reported, not
// rolled back.
func Apply(projectDir string, entries []Entry) []Result {
	return applyWith(projectDir, entries, platformLink)
}

func applyWith(projectDir string, entries []Entry, link linker) []Result {
	var results []Result
	for _, e := range entries {
		results = append(results, applyEntry(projectDir, e, link)...)
	}
	return results
}

func applyEntry(projectDir string, e Entry, link linker) []Result {
	if e.Mode == ModeCopy {
		return applyCopies(projectDir, e)
	}
	return applySymlinks(projectDir, e, link)
}

// applyCopies writes one copy per distinct destination. Eve's flat target is written as a file.
func applyCopies(projectDir string, e Entry) []Result {
	return forEachDistinctTarget(e, func(t Target) Result {
		return copyTarget(projectDir, e, t)
	})
}

// applySymlinks writes the canonical copy, then links every agent directory to it. Eve is always
// a flat copy and never a link.
func applySymlinks(projectDir string, e Entry, link linker) []Result {
	prepare := func(err error) []Result {
		results := make([]Result, 0, len(e.Targets))
		for _, t := range e.Targets {
			results = append(results, failure(newResult(e, t, ModeSymlink), err))
		}
		return results
	}
	if e.Canonical == "" {
		return prepare(errors.New("symlink install without a canonical directory"))
	}
	if err := replaceDir(projectDir, e.Skill.Dir, e.Canonical); err != nil {
		return prepare(err)
	}
	if err := renameInstalled(e.RenameTo, e.Canonical); err != nil {
		return prepare(err)
	}
	return forEachDistinctTarget(e, func(t Target) Result {
		return linkTarget(projectDir, e, t, link)
	})
}

// forEachDistinctTarget applies write to one target per distinct real path and repeats the outcome
// for every agent that shares it, so a skill is never written twice into the same directory while
// every agent still gets its own result.
func forEachDistinctTarget(e Entry, write func(Target) Result) []Result {
	results := make([]Result, 0, len(e.Targets))
	done := map[string]Result{}
	for _, t := range e.Targets {
		key := targetKey(t.Path)
		res, ok := done[key]
		if !ok {
			res = write(t)
			done[key] = res
		}
		res.Agent = t.Agent.ID
		res.Path = t.Path
		results = append(results, res)
	}
	return results
}

// targetKey identifies a destination by its real location, so a dotfiles symlink such as
// .claude/skills -> .agents/skills still deduplicates with the canonical directory.
func targetKey(path string) string {
	if resolved, err := fsutil.RealPath(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
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

// missingProjectDir reports whether a project symlink target belongs to an agent whose top-level
// project directory does not exist. Universal agents share .agents/skills, Claude Code creates its
// directory on demand, and flat targets are copies, so none of them is skipped.
func missingProjectDir(projectDir string, t Target) bool {
	if t.Flat || t.Agent.Universal || t.Agent.CreateProjectDirByDefault {
		return false
	}
	root := strings.SplitN(t.Agent.ProjectDir, "/", 2)[0]
	if root == "" || root == "." {
		return false
	}
	info, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(root)))
	return err != nil || !info.IsDir()
}

func copyTarget(projectDir string, e Entry, t Target) Result {
	res := newResult(e, t, ModeCopy)
	if t.Flat {
		if err := writeFlatSkill(projectDir, e, t.Path); err != nil {
			return failure(res, err)
		}
		res.Status = Installed
		return res
	}
	if err := replaceDir(projectDir, e.Skill.Dir, t.Path); err != nil {
		return failure(res, err)
	}
	if err := renameInstalled(e.RenameTo, t.Path); err != nil {
		return failure(res, err)
	}
	res.Status = Installed
	return res
}

// linkTarget points the agent directory at the canonical copy. Eve flat files and failed links
// fall back to copies, with the reason in the message.
func linkTarget(projectDir string, e Entry, t Target, link linker) Result {
	res := newResult(e, t, ModeSymlink)
	if t.Flat {
		return copyTarget(projectDir, e, t)
	}
	if categoryDirs[filepath.Base(t.Path)] {
		return failure(res, fmt.Errorf("%s: %w", t.Path, ErrCategoryDir))
	}
	if missingProjectDir(projectDir, t) {
		res.Status = Skipped
		res.Message = "project directory not found"
		return res
	}
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
	if err := linkOrCopy(projectDir, e, t, &res, link); err != nil {
		return failure(res, err)
	}
	res.Status = Installed
	return res
}

// linkOrCopy replaces t.Path with a link to the canonical copy, falling back to a copy when the
// platform or the filesystem refuses links. res.Mode and res.Message record the fallback.
func linkOrCopy(projectDir string, e Entry, t Target, res *Result, link linker) error {
	if err := fsutil.EnsureDir(filepath.Dir(t.Path)); err != nil {
		return err
	}
	if err := fsutil.RemoveAllWithin(projectDir, t.Path); err != nil {
		return err
	}
	err := link(t.Path, e.Canonical)
	if err == nil {
		return nil
	}
	res.Message = fmt.Sprintf("link failed (%v); copied instead", err)
	res.Mode = ModeCopy
	if err := replaceDir(projectDir, e.Skill.Dir, t.Path); err != nil {
		return err
	}
	return renameInstalled(e.RenameTo, t.Path)
}

// writeFlatSkill writes Eve's flat <name>.md: the skill's SKILL.md with the frontmatter reduced to
// the keys Eve keeps, and the installed name when --rename was used.
func writeFlatSkill(projectDir string, e Entry, dst string) error {
	overlap, err := overlaps(e.Skill.Dir, dst)
	if err != nil {
		return err
	}
	if overlap {
		return fmt.Errorf("%s: %w", dst, ErrOverlap)
	}
	raw, err := os.ReadFile(filepath.Join(e.Skill.Dir, "SKILL.md"))
	if err != nil {
		return err
	}
	out := skill.ReduceFrontmatter(raw)
	if e.RenameTo != "" {
		out = skill.SetFrontmatterName(out, e.RenameTo)
	}
	if err := fsutil.EnsureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	if err := fsutil.RemoveAllWithin(projectDir, dst); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(dst, out, 0o644)
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
