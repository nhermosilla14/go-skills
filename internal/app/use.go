package app

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// UseOptions are the inputs of the use use case. Skill is the single name given with --skill, or
// empty; the source's own @skill suffix is read from Source.
type UseOptions struct {
	Source    string
	Skill     string
	FullDepth bool
}

// UseReport is the outcome of a use run: the exact prompt written to the caller's stream and the
// directory it points at. The directory is kept after Use returns.
type UseReport struct {
	Prompt string
	Dir    string
}

// Use resolves a source like Add, discovers its skills, selects exactly one, copies it into a new
// skills-use-* temporary directory and writes the agent prompt to out. Nothing is installed and no
// lock file is written. The materialized directory is kept: the prompt points the agent at its
// supporting files. Only an intermediate git checkout is removed.
func Use(opts UseOptions, env Env, out io.Writer) (UseReport, error) {
	src, err := source.Parse(opts.Source)
	if err != nil {
		return UseReport{}, err
	}
	tmp := ""
	if src.Kind != source.KindLocal {
		tmp, err = os.MkdirTemp("", "skills-*")
		if err != nil {
			return UseReport{}, fmt.Errorf("creating a temporary directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmp) }()
	}
	tree, err := source.Acquire(context.Background(), src, tmp)
	if err != nil {
		return UseReport{}, err
	}
	found, err := discover(tree.Dir, src, env.IncludeInternal, opts.FullDepth)
	if err != nil {
		return UseReport{}, err
	}
	chosen, err := selectUseSkill(found.Skills, src.SkillSelector, opts.Skill, opts.Source)
	if err != nil {
		return UseReport{}, err
	}

	root, err := os.MkdirTemp("", "skills-use-*")
	if err != nil {
		return UseReport{}, fmt.Errorf("creating a temporary directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	dir := filepath.Join(root, skill.SanitizeName(skillFolderName(chosen)))
	if err := copySkillDir(chosen.Dir, dir); err != nil {
		return UseReport{}, fmt.Errorf("copying %s: %w", skill.SanitizeDisplay(chosen.Name), err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return UseReport{}, fmt.Errorf("reading SKILL.md: %w", err)
	}
	supporting, err := hasSupportingFiles(dir)
	if err != nil {
		return UseReport{}, fmt.Errorf("reading the copied skill: %w", err)
	}
	report := UseReport{Prompt: buildUsePrompt(contents, dir, supporting), Dir: dir}
	if _, err := io.WriteString(out, report.Prompt); err != nil {
		return report, err
	}
	keep = true
	return report, nil
}

// selectUseSkill picks the one skill the run selected. The @skill suffix and --skill must agree
// when both are present; a single selector must match exactly one skill, and without a selector
// the source must hold exactly one skill.
func selectUseSkill(all []skill.Skill, selector, flag, sourceArg string) (skill.Skill, error) {
	if selector != "" && flag != "" && selector != flag {
		return skill.Skill{}, useErrorf(
			"Conflicting skill selectors: source selects %q but --skill selects %q. Provide one selector.",
			skill.SanitizeDisplay(selector), skill.SanitizeDisplay(flag))
	}
	name := selector
	if name == "" {
		name = flag
	}
	if name != "" {
		matches := skill.Filter(all, name)
		if len(matches) == 0 {
			return skill.Skill{}, useErrorf("No matching skill found for %q.%s",
				skill.SanitizeDisplay(name), availableSkills(all))
		}
		if len(matches) > 1 {
			return skill.Skill{}, useErrorf("Ambiguous skill selector %q. Candidates:%s",
				skill.SanitizeDisplay(name), skillList(matches))
		}
		return matches[0], nil
	}
	switch len(all) {
	case 0:
		return skill.Skill{}, useErrorf("No valid skills found in the source.")
	case 1:
		return all[0], nil
	default:
		example := exampleSource(sourceArg)
		first := skill.SanitizeDisplay(all[0].Name)
		return skill.Skill{}, useErrorf(
			"This source contains multiple skills. Specify exactly one skill:%s\n\n"+
				"Use %s@%s or %s --skill %s",
			skillList(all), example, first, example, first)
	}
}

// skillFolderName is the directory name a materialized skill takes: the source folder's own name,
// falling back to the skill name when the folder has no usable base.
func skillFolderName(s skill.Skill) string {
	name := filepath.Base(s.Dir)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return s.Name
	}
	return name
}

// availableSkills renders the candidate list appended to a no-match error.
func availableSkills(all []skill.Skill) string {
	if len(all) == 0 {
		return ""
	}
	return "\nAvailable skills:" + skillList(all)
}

// skillList renders one indented line per skill.
func skillList(list []skill.Skill) string {
	var b strings.Builder
	for _, s := range list {
		b.WriteString("\n  ")
		b.WriteString(skill.SanitizeDisplay(s.Name))
	}
	return b.String()
}

// exampleSource is the source spelling an example command should use.
func exampleSource(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<source>"
	}
	return skill.SanitizeDisplay(s)
}

// useError carries a user-facing diagnostic that reads as a sentence, so it can keep its capital
// letter and final period without tripping the error-string convention.
type useError struct {
	msg string
}

func (e *useError) Error() string { return e.msg }

func useErrorf(format string, args ...any) error {
	return &useError{msg: fmt.Sprintf(format, args...)}
}

// copySkillDir copies the skill folder into dst. Symlinks that stay inside the folder are
// dereferenced; broken links, links that escape it and cycles are skipped.
func copySkillDir(src, dst string) error {
	root, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return copySkillTree(root, root, dst, map[string]bool{root: true})
}

// copySkillTree copies the entries of dir, which lives under root, into dst.
func copySkillTree(root, dir, dst string, ancestors map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		resolved, err := filepath.EvalSymlinks(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // broken link
		}
		if resolved == root || !fsutil.Within(root, resolved) {
			continue // self link or escape
		}
		info, err := os.Stat(resolved)
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		target := filepath.Join(dst, entry.Name())
		if !info.IsDir() {
			if err := copySkillFile(resolved, target, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if ancestors[resolved] {
			continue // cycle
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		ancestors[resolved] = true
		err = copySkillTree(root, resolved, target, ancestors)
		delete(ancestors, resolved)
		if err != nil {
			return err
		}
	}
	return nil
}

// copySkillFile copies one regular file, preserving its permission bits.
func copySkillFile(from, to string, perm fs.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// hasSupportingFiles reports whether the materialized folder holds anything besides the root
// SKILL.md, compared case-insensitively and at any depth.
func hasSupportingFiles(dir string) (bool, error) {
	supporting := false
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if !strings.EqualFold(filepath.ToSlash(rel), "SKILL.md") {
			supporting = true
			return fs.SkipAll
		}
		return nil
	})
	return supporting, err
}

// buildUsePrompt renders the exact text handed to an agent: three sections, or four when the
// folder carries supporting files, joined by a blank line and ending with one newline.
func buildUsePrompt(skillMD []byte, dir string, supporting bool) string {
	sections := []string{
		"You are being given a Skill to execute for the user's next request.",
		"Use the following SKILL.md as your instructions:",
		"<SKILL.md>\n" + string(skillMD) + "\n</SKILL.md>",
	}
	if supporting {
		sections = append(sections, "Supporting files for this skill were downloaded to:\n"+dir+
			"\n\nWhen the SKILL.md references relative paths, read them from that directory.")
	}
	return strings.Join(sections, "\n\n") + "\n"
}
